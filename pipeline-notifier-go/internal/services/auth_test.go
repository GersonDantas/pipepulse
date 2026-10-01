package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/secrets"
)

type fakeAuthRepository struct {
	request      repository.OAuthRequest
	exchangeHash []byte
	githubUser   GithubUser
	principal    repository.Principal
	session      repository.SessionTokens
}

func TestGithubClientExchangesCodeAndLoadsUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/login/oauth/access_token":
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if request.Form.Get("client_id") != "client-id" || request.Form.Get("client_secret") != "client-secret" || request.Form.Get("code_verifier") != "verifier" {
				t.Fatalf("token form = %#v", request.Form)
			}
			_ = json.NewEncoder(writer).Encode(map[string]string{"access_token": "github-token"})
		case "/user":
			if request.Header.Get("Authorization") != "Bearer github-token" {
				t.Fatalf("Authorization = %q", request.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"id": 1, "login": "octocat"})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := NewGithubClient(server.Client(), "client-id", "client-secret", "https://api.pipepulse.app/v1/auth/github/callback")
	client.tokenURL = server.URL + "/login/oauth/access_token"
	client.userURL = server.URL + "/user"
	user, err := client.Authenticate(context.Background(), "github-code", "verifier")
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if user.ID != 1 || user.Login != "octocat" {
		t.Fatalf("user = %#v", user)
	}
}

func (repo *fakeAuthRepository) CreateOAuthRequest(_ context.Context, request repository.OAuthRequest) error {
	repo.request = request
	return nil
}

func (repo *fakeAuthRepository) ConsumeOAuthRequest(_ context.Context, stateHash []byte, now time.Time) (repository.OAuthRequest, error) {
	if !hashesEqual(stateHash, repo.request.StateHash) || !repo.request.ExpiresAt.After(now) {
		return repository.OAuthRequest{}, repository.ErrOAuthRequestNotFound
	}
	return repo.request, nil
}

func (repo *fakeAuthRepository) CompleteOAuthRequest(_ context.Context, id string, exchangeHash []byte, githubUserID int64, githubLogin string, _ time.Time) error {
	repo.exchangeHash = exchangeHash
	repo.githubUser = GithubUser{ID: githubUserID, Login: githubLogin}
	return nil
}

func (repo *fakeAuthRepository) ExchangeOAuthSession(_ context.Context, exchangeHash, verifierHash []byte, tokens repository.SessionTokens, _ time.Time) (repository.Principal, error) {
	if !hashesEqual(exchangeHash, repo.exchangeHash) || !hashesEqual(verifierHash, repo.request.ExchangeVerifierHash) {
		return repository.Principal{}, repository.ErrOAuthExchangeNotFound
	}
	repo.session = tokens
	return repo.principal, nil
}

func (repo *fakeAuthRepository) RotateSession(_ context.Context, refreshHash []byte, tokens repository.SessionTokens, _ time.Time) (repository.Principal, error) {
	if !hashesEqual(refreshHash, repo.session.RefreshTokenHash) {
		return repository.Principal{}, repository.ErrSessionNotFound
	}
	repo.session = tokens
	return repo.principal, nil
}

func (repo *fakeAuthRepository) Authenticate(_ context.Context, accessHash []byte, _ time.Time) (repository.Principal, error) {
	if !hashesEqual(accessHash, repo.session.AccessTokenHash) {
		return repository.Principal{}, repository.ErrSessionNotFound
	}
	return repo.principal, nil
}

func (repo *fakeAuthRepository) RevokeSession(_ context.Context, accessHash []byte, _ time.Time) error {
	if !hashesEqual(accessHash, repo.session.AccessTokenHash) {
		return repository.ErrSessionNotFound
	}
	repo.session = repository.SessionTokens{}
	return nil
}

type fakeGithubClient struct {
	code     string
	verifier string
	user     GithubUser
}

func (client *fakeGithubClient) Authenticate(_ context.Context, code, verifier string) (GithubUser, error) {
	client.code = code
	client.verifier = verifier
	return client.user, nil
}

func TestAuthServiceCompletesPKCEAndCreatesRotatingSession(t *testing.T) {
	now := time.Date(2026, 8, 19, 15, 0, 0, 0, time.UTC)
	repo := &fakeAuthRepository{principal: repository.Principal{UserID: "user-1", WorkspaceID: "workspace-1", GithubLogin: "octocat", PlanCode: "free"}}
	github := &fakeGithubClient{user: GithubUser{ID: 1, Login: "octocat"}}
	box, err := secrets.NewBox([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewAuthService(repo, github, box, AuthConfig{
		GithubClientID:    "client-id",
		CallbackURL:       "https://api.pipepulse.app/v1/auth/github/callback",
		MobileRedirectURI: "com.pipepulse.app://oauth",
		Now:               func() time.Time { return now },
	})

	start, err := service.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	authorizationURL := start.AuthorizationURL
	parsed, err := url.Parse(authorizationURL)
	if err != nil {
		t.Fatalf("authorization URL = %q: %v", authorizationURL, err)
	}
	if parsed.Host != "github.com" || parsed.Query().Get("client_id") != "client-id" || parsed.Query().Get("code_challenge_method") != "S256" {
		t.Fatalf("authorization URL = %q, want GitHub PKCE URL", authorizationURL)
	}
	state := parsed.Query().Get("state")
	if state == "" || parsed.Query().Get("code_challenge") == "" || start.ExchangeVerifier == "" {
		t.Fatalf("authorization URL = %q, want state and challenge", authorizationURL)
	}
	if strings.Contains(authorizationURL, start.ExchangeVerifier) {
		t.Fatal("authorization URL exposed client exchange verifier")
	}

	redirect, err := service.Callback(context.Background(), state, "github-code")
	if err != nil {
		t.Fatalf("Callback() error = %v", err)
	}
	if github.code != "github-code" || github.verifier == "" || repo.githubUser != github.user {
		t.Fatalf("GitHub exchange = code %q verifier %q user %#v", github.code, github.verifier, repo.githubUser)
	}
	if !strings.HasPrefix(redirect, "com.pipepulse.app://oauth?") {
		t.Fatalf("redirect = %q, want mobile redirect", redirect)
	}
	exchangeCode, err := url.Parse(redirect)
	if err != nil || exchangeCode.Query().Get("code") == "" {
		t.Fatalf("redirect = %q, want one-time exchange code", redirect)
	}
	if strings.Contains(redirect, start.ExchangeVerifier) {
		t.Fatal("mobile redirect exposed client exchange verifier")
	}

	if _, err := service.Exchange(context.Background(), exchangeCode.Query().Get("code"), "wrong-verifier"); err != ErrInvalidExchangeCode {
		t.Fatalf("exchange with wrong verifier error = %v, want ErrInvalidExchangeCode", err)
	}
	tokens, err := service.Exchange(context.Background(), exchangeCode.Query().Get("code"), start.ExchangeVerifier)
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.AccessExpiresAt.Sub(now) != 15*time.Minute || tokens.RefreshExpiresAt.Sub(now) != 30*24*time.Hour {
		t.Fatalf("tokens = %#v, want 15 minute access and 30 day refresh", tokens)
	}
	if _, err := service.Authenticate(context.Background(), tokens.AccessToken); err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	rotated, err := service.Refresh(context.Background(), tokens.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if rotated.AccessToken == tokens.AccessToken || rotated.RefreshToken == tokens.RefreshToken {
		t.Fatal("Refresh() reused access or refresh token")
	}
	if _, err := service.Refresh(context.Background(), tokens.RefreshToken); err == nil {
		t.Fatal("old refresh token remained valid after rotation")
	}
}

func hashesEqual(left, right []byte) bool {
	return string(left) == string(right)
}
