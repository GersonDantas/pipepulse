package notifications

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFirebaseSendsStableIdentifiersWithoutPrivatePipelineContent(t *testing.T) {
	sender := &Firebase{projectID: "pipepulse-test", token: &oauth2.Token{AccessToken: "test-access", Expiry: time.Now().Add(time.Hour)}}
	sender.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://fcm.googleapis.com/v1/projects/pipepulse-test/messages:send" || r.Header.Get("Authorization") != "Bearer test-access" {
			t.Fatalf("unexpected request headers/URL")
		}
		var payload struct {
			Message struct {
				Token        string
				Data         map[string]string
				Notification map[string]string
				Android      struct{ Notification struct{ Tag string } }
				APNS         struct{ Headers map[string]string }
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Message.Token != "device-token" || payload.Message.Data["notification_id"] != "n-1" || payload.Message.Data["failure_id"] != "f-1" || payload.Message.Android.Notification.Tag != "n-1" || payload.Message.APNS.Headers["apns-collapse-id"] != "n-1" {
			t.Fatalf("payload = %#v", payload)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"name":"projects/test/messages/1"}`)), Header: make(http.Header)}, nil
	})}
	if err := sender.Send(context.Background(), Message{Token: "device-token", NotificationID: "n-1", FailureID: "f-1", RepositoryID: "r-1"}); err != nil {
		t.Fatal(err)
	}
}

func testCredentials(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyBytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	value, err := json.Marshal(map[string]string{"type": "service_account", "project_id": "test-project", "client_email": "test@example.invalid", "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes})), "token_uri": googleTokenURL})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestFirebaseCredentialsAndOAuthRefresh(t *testing.T) {
	for _, value := range [][]byte{nil, []byte(`{}`), []byte(`{"type":"service_account","project_id":"test","client_email":"test","private_key":"bad"}`)} {
		if _, err := NewFirebase(value); err == nil {
			t.Fatal("invalid credentials accepted")
		}
	}
	sender, err := NewFirebase(testCredentials(t))
	if err != nil {
		t.Fatal(err)
	}
	tokenCalls := 0
	sender.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == googleTokenURL {
			tokenCalls++
			if r.Context().Err() != nil {
				t.Fatal("OAuth context cancelled unexpectedly")
			}
			if err := r.ParseForm(); err != nil || r.Form.Get("assertion") == "" {
				t.Fatal("missing JWT assertion")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"access_token":"test-access","expires_in":3600,"token_type":"Bearer"}`))}, nil
		}
		if r.Header.Get("Authorization") != "Bearer test-access" {
			t.Fatal("missing refreshed access token")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"name":"projects/test/messages/1"}`))}, nil
	})
	for range 2 {
		if err := sender.Send(context.Background(), Message{Token: "token", NotificationID: "n"}); err != nil {
			t.Fatal(err)
		}
	}
	if tokenCalls != 1 {
		t.Fatalf("token requests = %d, want cache reuse", tokenCalls)
	}
	sender.token.Expiry = time.Now().Add(-time.Hour)
	if err := sender.Send(context.Background(), Message{Token: "token", NotificationID: "n"}); err != nil {
		t.Fatal(err)
	}
	if tokenCalls != 2 {
		t.Fatalf("token requests = %d, want refresh", tokenCalls)
	}
}

func TestFirebaseOAuthSharesAttemptDeadline(t *testing.T) {
	sender, err := NewFirebase(testCredentials(t))
	if err != nil {
		t.Fatal(err)
	}
	sender.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if _, ok := r.Context().Deadline(); !ok {
			t.Error("OAuth request has no attempt deadline")
		}
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := sender.Send(ctx, Message{Token: "token"}); err == nil {
		t.Fatal("cancelled OAuth request succeeded")
	}
	if time.Since(started) > time.Second {
		t.Fatal("OAuth ignored the attempt deadline")
	}
}

func TestRetryAfterHTTPDate(t *testing.T) {
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if got := retryAfter(now.Add(2*time.Hour).Format(http.TimeFormat), now); got != 2*time.Hour {
		t.Fatalf("Retry-After = %s", got)
	}
	for _, value := range []string{"bad", "-1", now.Add(-time.Minute).Format(http.TimeFormat)} {
		if got := retryAfter(value, now); got != 0 {
			t.Fatalf("invalid Retry-After = %s", got)
		}
	}
}

func TestFirebaseClassifiesErrorsWithoutLeakingProviderMessage(t *testing.T) {
	tests := []struct {
		status             int
		body, code         string
		permanent, invalid bool
	}{
		{404, `{"error":{"message":"secret-token","details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"UNREGISTERED"}]}}`, "unregistered", true, true},
		{404, `{"error":{"message":"secret-token","status":"NOT_FOUND"}}`, "fcm_rejected", true, false},
		{400, `{"error":{"details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"INVALID_ARGUMENT"}]}}`, "invalid_argument", true, false},
		{503, `{"error":{"message":"secret-token","status":"UNAVAILABLE"}}`, "fcm_unavailable", false, false},
		{429, `{}`, "fcm_quota_exceeded", false, false},
		{401, `{"error":{"details":[{"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"THIRD_PARTY_AUTH_ERROR"}]}}`, "third_party_auth_error", true, false},
	}
	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			sender := &Firebase{projectID: "test", token: &oauth2.Token{AccessToken: "access", Expiry: time.Now().Add(time.Hour)}, client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.status, Header: http.Header{"Retry-After": []string{"900"}}, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})}}
			err := sender.Send(context.Background(), Message{Token: "token", NotificationID: "n"})
			var sendErr *SendError
			if !errors.As(err, &sendErr) || sendErr.Code != test.code || sendErr.Permanent != test.permanent || sendErr.InvalidToken != test.invalid || strings.Contains(err.Error(), "secret") {
				t.Fatalf("error = %#v", err)
			}
			if !test.permanent && sendErr.RetryAfter != 15*time.Minute {
				t.Fatalf("RetryAfter = %s", sendErr.RetryAfter)
			}
		})
	}
}
