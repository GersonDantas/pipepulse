package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/secrets"
)

const (
	accessTokenLifetime  = 15 * time.Minute
	refreshTokenLifetime = 30 * 24 * time.Hour
	oauthRequestLifetime = 10 * time.Minute
	exchangeCodeLifetime = 5 * time.Minute
)

var ErrInvalidOAuthRequest = errors.New("invalid oauth request")
var ErrInvalidExchangeCode = errors.New("invalid exchange code")
var ErrInvalidSession = errors.New("invalid session")

type GithubUser struct {
	ID    int64
	Login string
}

type GithubAuthenticator interface {
	Authenticate(context.Context, string, string) (GithubUser, error)
}

type AuthConfig struct {
	GithubClientID    string
	CallbackURL       string
	MobileRedirectURI string
	Now               func() time.Time
}

type Tokens struct {
	AccessToken      string    `json:"access_token"`
	RefreshToken     string    `json:"refresh_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
}

type OAuthStart struct {
	AuthorizationURL string `json:"authorization_url"`
	ExchangeVerifier string `json:"exchange_verifier"`
}

type AuthService struct {
	repository repository.AuthStore
	github     GithubAuthenticator
	secretBox  *secrets.Box
	config     AuthConfig
}

func NewAuthService(authRepository repository.AuthStore, github GithubAuthenticator, secretBox *secrets.Box, config AuthConfig) *AuthService {
	if config.Now == nil {
		config.Now = time.Now
	}
	return &AuthService{repository: authRepository, github: github, secretBox: secretBox, config: config}
}

func (service *AuthService) Start(ctx context.Context) (OAuthStart, error) {
	state, err := randomToken()
	if err != nil {
		return OAuthStart{}, err
	}
	verifier, err := randomToken()
	if err != nil {
		return OAuthStart{}, err
	}
	exchangeVerifier, err := randomToken()
	if err != nil {
		return OAuthStart{}, err
	}
	ciphertext, err := service.secretBox.Encrypt([]byte(verifier))
	if err != nil {
		return OAuthStart{}, fmt.Errorf("encrypt PKCE verifier: %w", err)
	}
	if err := service.repository.CreateOAuthRequest(ctx, repository.OAuthRequest{
		StateHash:              tokenHash(state),
		CodeVerifierCiphertext: ciphertext,
		ExchangeVerifierHash:   tokenHash(exchangeVerifier),
		RedirectURI:            service.config.MobileRedirectURI,
		ExpiresAt:              service.config.Now().Add(oauthRequestLifetime),
	}); err != nil {
		return OAuthStart{}, fmt.Errorf("create oauth request: %w", err)
	}

	challenge := sha256.Sum256([]byte(verifier))
	query := url.Values{
		"client_id":             {service.config.GithubClientID},
		"redirect_uri":          {service.config.CallbackURL},
		"scope":                 {"read:user"},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
	}
	return OAuthStart{AuthorizationURL: "https://github.com/login/oauth/authorize?" + query.Encode(), ExchangeVerifier: exchangeVerifier}, nil
}

func (service *AuthService) Callback(ctx context.Context, state, code string) (string, error) {
	if strings.TrimSpace(state) == "" || strings.TrimSpace(code) == "" {
		return "", ErrInvalidOAuthRequest
	}
	request, err := service.repository.ConsumeOAuthRequest(ctx, tokenHash(state), service.config.Now())
	if errors.Is(err, repository.ErrOAuthRequestNotFound) {
		return "", ErrInvalidOAuthRequest
	}
	if err != nil {
		return "", fmt.Errorf("consume oauth request: %w", err)
	}
	verifier, err := service.secretBox.Decrypt(request.CodeVerifierCiphertext)
	if err != nil {
		return "", fmt.Errorf("decrypt PKCE verifier: %w", err)
	}
	defer clear(verifier)
	user, err := service.github.Authenticate(ctx, code, string(verifier))
	if err != nil {
		return "", fmt.Errorf("authenticate with GitHub: %w", err)
	}
	if user.ID <= 0 || strings.TrimSpace(user.Login) == "" {
		return "", errors.New("GitHub returned an invalid user")
	}
	exchangeCode, err := randomToken()
	if err != nil {
		return "", err
	}
	if err := service.repository.CompleteOAuthRequest(ctx, request.ID, tokenHash(exchangeCode), user.ID, user.Login, service.config.Now().Add(exchangeCodeLifetime)); err != nil {
		return "", fmt.Errorf("complete oauth request: %w", err)
	}
	redirect, err := url.Parse(request.RedirectURI)
	if err != nil {
		return "", fmt.Errorf("parse mobile redirect URI: %w", err)
	}
	query := redirect.Query()
	query.Set("code", exchangeCode)
	redirect.RawQuery = query.Encode()
	return redirect.String(), nil
}

func (service *AuthService) Exchange(ctx context.Context, exchangeCode, exchangeVerifier string) (Tokens, error) {
	if strings.TrimSpace(exchangeCode) == "" || strings.TrimSpace(exchangeVerifier) == "" {
		return Tokens{}, ErrInvalidExchangeCode
	}
	tokens, stored, err := service.newTokens()
	if err != nil {
		return Tokens{}, err
	}
	if _, err := service.repository.ExchangeOAuthSession(ctx, tokenHash(exchangeCode), tokenHash(exchangeVerifier), stored, service.config.Now()); errors.Is(err, repository.ErrOAuthExchangeNotFound) {
		return Tokens{}, ErrInvalidExchangeCode
	} else if err != nil {
		return Tokens{}, fmt.Errorf("exchange oauth session: %w", err)
	}
	return tokens, nil
}

func (service *AuthService) Refresh(ctx context.Context, refreshToken string) (Tokens, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return Tokens{}, ErrInvalidSession
	}
	tokens, stored, err := service.newTokens()
	if err != nil {
		return Tokens{}, err
	}
	if _, err := service.repository.RotateSession(ctx, tokenHash(refreshToken), stored, service.config.Now()); errors.Is(err, repository.ErrSessionNotFound) {
		return Tokens{}, ErrInvalidSession
	} else if err != nil {
		return Tokens{}, fmt.Errorf("rotate session: %w", err)
	}
	return tokens, nil
}

func (service *AuthService) Authenticate(ctx context.Context, accessToken string) (repository.Principal, error) {
	if strings.TrimSpace(accessToken) == "" {
		return repository.Principal{}, ErrInvalidSession
	}
	principal, err := service.repository.Authenticate(ctx, tokenHash(accessToken), service.config.Now())
	if errors.Is(err, repository.ErrSessionNotFound) {
		return repository.Principal{}, ErrInvalidSession
	}
	if err != nil {
		return repository.Principal{}, fmt.Errorf("authenticate session: %w", err)
	}
	return principal, nil
}

func (service *AuthService) Logout(ctx context.Context, accessToken string) error {
	if strings.TrimSpace(accessToken) == "" {
		return ErrInvalidSession
	}
	if err := service.repository.RevokeSession(ctx, tokenHash(accessToken), service.config.Now()); errors.Is(err, repository.ErrSessionNotFound) {
		return ErrInvalidSession
	} else if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (service *AuthService) newTokens() (Tokens, repository.SessionTokens, error) {
	accessToken, err := randomToken()
	if err != nil {
		return Tokens{}, repository.SessionTokens{}, err
	}
	refreshToken, err := randomToken()
	if err != nil {
		return Tokens{}, repository.SessionTokens{}, err
	}
	now := service.config.Now()
	tokens := Tokens{
		AccessToken:      accessToken,
		RefreshToken:     refreshToken,
		AccessExpiresAt:  now.Add(accessTokenLifetime),
		RefreshExpiresAt: now.Add(refreshTokenLifetime),
	}
	stored := repository.SessionTokens{
		AccessTokenHash:  tokenHash(accessToken),
		RefreshTokenHash: tokenHash(refreshToken),
		AccessExpiresAt:  tokens.AccessExpiresAt,
		RefreshExpiresAt: tokens.RefreshExpiresAt,
	}
	return tokens, stored, nil
}

func randomToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func tokenHash(token string) []byte {
	hash := sha256.Sum256([]byte(token))
	return hash[:]
}
