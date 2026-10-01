package notifications

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/jwt"
)

const googleTokenURL = "https://oauth2.googleapis.com/token"

type Firebase struct {
	projectID   string
	credentials *jwt.Config
	client      *http.Client
	mu          sync.Mutex
	token       *oauth2.Token
}

// oauth2/jwt calls PostForm without attaching its TokenSource context.
// Bind that request explicitly so shutdown also cancels credential refresh.
type attemptTransport struct {
	context context.Context
	base    http.RoundTripper
}

func (t attemptTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return t.base.RoundTrip(request.Clone(t.context))
}

func NewFirebase(credentialsJSON []byte) (*Firebase, error) {
	var account struct {
		Type         string `json:"type"`
		ProjectID    string `json:"project_id"`
		ClientEmail  string `json:"client_email"`
		PrivateKey   string `json:"private_key"`
		PrivateKeyID string `json:"private_key_id"`
		TokenURI     string `json:"token_uri"`
	}
	invalid := errors.New("FCM credentials must contain a valid service account with an RSA private key")
	if json.Unmarshal(credentialsJSON, &account) != nil || account.Type != "service_account" || account.ProjectID == "" || account.ClientEmail == "" || (account.TokenURI != "" && account.TokenURI != googleTokenURL) {
		return nil, invalid
	}
	block, _ := pem.Decode([]byte(account.PrivateKey))
	if block == nil {
		return nil, invalid
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if err != nil || !ok || rsaKey.N.BitLen() < 2048 || rsaKey.Validate() != nil {
		return nil, invalid
	}
	return &Firebase{
		projectID:   account.ProjectID,
		credentials: &jwt.Config{Email: account.ClientEmail, PrivateKey: []byte(account.PrivateKey), PrivateKeyID: account.PrivateKeyID, TokenURL: googleTokenURL, Scopes: []string{"https://www.googleapis.com/auth/firebase.messaging"}},
		client:      &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}, nil
}

func (f *Firebase) accessToken(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", &SendError{Code: "transport_error"}
	}
	if !f.token.Valid() {
		// Refresh uses this attempt's deadline, including the OAuth HTTP request.
		client := *f.client
		base := client.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		client.Transport = attemptTransport{context: ctx, base: base}
		tokenContext := context.WithValue(ctx, oauth2.HTTPClient, &client)
		token, err := f.credentials.TokenSource(tokenContext).Token()
		if err != nil {
			return "", &SendError{Code: "fcm_auth_failed"}
		}
		f.token = token
	}
	return f.token.AccessToken, nil
}

func (f *Firebase) Send(ctx context.Context, message Message) error {
	token, err := f.accessToken(ctx)
	if err != nil {
		return err
	}
	// Private repository names, branches and URLs never appear on the lock screen.
	payload := map[string]any{"message": map[string]any{
		"token":        message.Token,
		"notification": map[string]string{"title": "PipePulse", "body": "Uma execução de pipeline falhou. Abra o app para consultar."},
		"data":         map[string]string{"notification_id": message.NotificationID, "failure_id": message.FailureID, "repository_id": message.RepositoryID},
		"android":      map[string]any{"notification": map[string]string{"tag": message.NotificationID}},
		"apns":         map[string]any{"headers": map[string]string{"apns-collapse-id": message.NotificationID}},
	}}
	body, err := json.Marshal(payload)
	if err != nil {
		return &SendError{Code: "invalid_payload", Permanent: true}
	}
	endpoint := "https://fcm.googleapis.com/v1/projects/" + url.PathEscape(f.projectID) + "/messages:send"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return &SendError{Code: "invalid_payload", Permanent: true}
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := f.client.Do(request)
	if err != nil {
		return &SendError{Code: "transport_error"}
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusOK {
		var result struct {
			Name string `json:"name"`
		}
		if json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&result) != nil || result.Name == "" {
			return &SendError{Code: "invalid_response"}
		}
		return nil
	}
	failure := &SendError{Code: "fcm_rejected", Permanent: true}
	switch {
	case response.StatusCode == 429:
		failure.Code = "fcm_quota_exceeded"
		failure.Permanent = false
	case response.StatusCode >= 500:
		failure.Code = "fcm_unavailable"
		failure.Permanent = false
	case response.StatusCode == 401:
		// Force a fresh OAuth token on the next bounded attempt.
		f.mu.Lock()
		f.token = nil
		f.mu.Unlock()
		failure.Code = "fcm_auth_failed"
		failure.Permanent = false
	}
	var remote struct {
		Error struct {
			Details []struct {
				Type      string `json:"@type"`
				ErrorCode string `json:"errorCode"`
			} `json:"details"`
		} `json:"error"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&remote) == nil {
		for _, detail := range remote.Error.Details {
			if detail.Type != "type.googleapis.com/google.firebase.fcm.v1.FcmError" {
				continue
			}
			switch detail.ErrorCode {
			case "UNREGISTERED":
				failure.Code = "unregistered"
				failure.InvalidToken = true
				failure.Permanent = true
			case "INVALID_ARGUMENT":
				failure.Code = "invalid_argument"
				failure.Permanent = true
			case "THIRD_PARTY_AUTH_ERROR":
				failure.Code = "third_party_auth_error"
				failure.Permanent = true
			}
		}
	}
	if !failure.Permanent {
		failure.RetryAfter = retryAfter(response.Header.Get("Retry-After"), time.Now())
	}
	return failure
}

func retryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 32); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil && date.After(now) {
		return date.Sub(now)
	}
	return 0
}
