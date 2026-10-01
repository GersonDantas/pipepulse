package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type GithubClient struct {
	httpClient   *http.Client
	clientID     string
	clientSecret string
	callbackURL  string
	tokenURL     string
	userURL      string
}

func NewGithubClient(httpClient *http.Client, clientID, clientSecret, callbackURL string) *GithubClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &GithubClient{
		httpClient:   httpClient,
		clientID:     clientID,
		clientSecret: clientSecret,
		callbackURL:  callbackURL,
		tokenURL:     "https://github.com/login/oauth/access_token",
		userURL:      "https://api.github.com/user",
	}
}

func (client *GithubClient) Authenticate(ctx context.Context, code, verifier string) (GithubUser, error) {
	form := url.Values{
		"client_id":     {client.clientID},
		"client_secret": {client.clientSecret},
		"code":          {code},
		"redirect_uri":  {client.callbackURL},
		"code_verifier": {verifier},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return GithubUser{}, fmt.Errorf("create GitHub token request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", "PipePulse")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return GithubUser{}, fmt.Errorf("exchange GitHub authorization code: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return GithubUser{}, fmt.Errorf("GitHub token endpoint returned %d", response.StatusCode)
	}
	var tokenResponse struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&tokenResponse); err != nil {
		return GithubUser{}, fmt.Errorf("decode GitHub token response: %w", err)
	}
	if tokenResponse.AccessToken == "" || tokenResponse.Error != "" {
		return GithubUser{}, errorsForGithubResponse(tokenResponse.Error)
	}

	userRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, client.userURL, nil)
	if err != nil {
		return GithubUser{}, fmt.Errorf("create GitHub user request: %w", err)
	}
	userRequest.Header.Set("Accept", "application/vnd.github+json")
	userRequest.Header.Set("Authorization", "Bearer "+tokenResponse.AccessToken)
	userRequest.Header.Set("User-Agent", "PipePulse")
	userResponse, err := client.httpClient.Do(userRequest)
	tokenResponse.AccessToken = ""
	if err != nil {
		return GithubUser{}, fmt.Errorf("load GitHub user: %w", err)
	}
	defer userResponse.Body.Close()
	if userResponse.StatusCode < 200 || userResponse.StatusCode >= 300 {
		return GithubUser{}, fmt.Errorf("GitHub user endpoint returned %d", userResponse.StatusCode)
	}
	var user GithubUser
	if err := json.NewDecoder(io.LimitReader(userResponse.Body, 1<<20)).Decode(&user); err != nil {
		return GithubUser{}, fmt.Errorf("decode GitHub user response: %w", err)
	}
	return user, nil
}

func errorsForGithubResponse(code string) error {
	if code == "" {
		return fmt.Errorf("GitHub token response did not include an access token")
	}
	return fmt.Errorf("GitHub rejected authorization code: %s", code)
}
