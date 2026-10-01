package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"pipeline-notifier/internal/entitlements"
	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/secrets"
)

var ErrInvalidProductRequest = errors.New("invalid product request")
var ErrInvalidCursor = errors.New("invalid cursor")

type Bootstrap struct {
	User struct {
		ID          string `json:"id"`
		GithubLogin string `json:"github_login"`
	} `json:"user"`
	Workspace struct {
		ID string `json:"id"`
	} `json:"workspace"`
	Plan         string                    `json:"plan"`
	Entitlements entitlements.Entitlements `json:"entitlements"`
}

type FailurePage struct {
	Failures   []repository.Failure `json:"failures"`
	NextCursor string               `json:"next_cursor,omitempty"`
}

type WebhookCredentials struct {
	WebhookURL    string `json:"webhook_url"`
	WebhookSecret string `json:"webhook_secret"`
}

type ProductService struct {
	repository repository.ProductStore
	secretBox  *secrets.Box
	appBaseURL string
}

func NewProductService(productRepository repository.ProductStore, secretBox *secrets.Box, appBaseURL string) *ProductService {
	return &ProductService{repository: productRepository, secretBox: secretBox, appBaseURL: strings.TrimRight(appBaseURL, "/")}
}

func (service *ProductService) Bootstrap(principal repository.Principal) (Bootstrap, error) {
	limits, err := entitlements.ForPlan(principal.PlanCode)
	if err != nil {
		return Bootstrap{}, err
	}
	var bootstrap Bootstrap
	bootstrap.User.ID = principal.UserID
	bootstrap.User.GithubLogin = principal.GithubLogin
	bootstrap.Workspace.ID = principal.WorkspaceID
	bootstrap.Plan = principal.PlanCode
	bootstrap.Entitlements = limits
	return bootstrap, nil
}

func (service *ProductService) CreateRepository(ctx context.Context, principal repository.Principal, input repository.CreateRepositoryInput) (repository.ProductRepository, error) {
	input.Owner = strings.TrimSpace(input.Owner)
	input.Name = strings.TrimSpace(input.Name)
	input.Workflow.Name = strings.TrimSpace(input.Workflow.Name)
	if input.GithubRepositoryID <= 0 || input.Owner == "" || input.Name == "" || input.Workflow.GithubWorkflowID <= 0 || input.Workflow.Name == "" {
		return repository.ProductRepository{}, ErrInvalidProductRequest
	}
	limits, err := entitlements.ForPlan(principal.PlanCode)
	if err != nil {
		return repository.ProductRepository{}, err
	}
	secret, ciphertext, err := service.newSecret()
	if err != nil {
		return repository.ProductRepository{}, err
	}
	created, err := service.repository.CreateRepository(ctx, principal.WorkspaceID, input, ciphertext, limits.MaxRepositories)
	if err != nil {
		return repository.ProductRepository{}, err
	}
	created.WebhookURL = service.webhookURL(created.EndpointID)
	created.WebhookSecret = secret
	return created, nil
}

func (service *ProductService) ListRepositories(ctx context.Context, principal repository.Principal) ([]repository.ProductRepository, error) {
	repositories, err := service.repository.ListRepositories(ctx, principal.WorkspaceID)
	if err != nil {
		return nil, err
	}
	for index := range repositories {
		repositories[index].WebhookURL = service.webhookURL(repositories[index].EndpointID)
	}
	return repositories, nil
}

func (service *ProductService) GetRepository(ctx context.Context, principal repository.Principal, id string) (repository.ProductRepository, error) {
	result, err := service.repository.GetRepository(ctx, principal.WorkspaceID, id)
	if err != nil {
		return repository.ProductRepository{}, err
	}
	result.WebhookURL = service.webhookURL(result.EndpointID)
	return result, nil
}

func (service *ProductService) RotateRepositorySecret(ctx context.Context, principal repository.Principal, id string) (WebhookCredentials, error) {
	secret, ciphertext, err := service.newSecret()
	if err != nil {
		return WebhookCredentials{}, err
	}
	result, err := service.repository.GetRepository(ctx, principal.WorkspaceID, id)
	if err != nil {
		return WebhookCredentials{}, err
	}
	if err := service.repository.RotateRepositorySecret(ctx, principal.WorkspaceID, id, ciphertext); err != nil {
		return WebhookCredentials{}, err
	}
	return WebhookCredentials{WebhookURL: service.webhookURL(result.EndpointID), WebhookSecret: secret}, nil
}

func (service *ProductService) DeleteRepository(ctx context.Context, principal repository.Principal, id string) error {
	return service.repository.DeleteRepository(ctx, principal.WorkspaceID, id)
}

func (service *ProductService) ListFailures(ctx context.Context, principal repository.Principal, encodedCursor string, limit int) (FailurePage, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	var cursor *repository.FailureCursor
	if encodedCursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(encodedCursor)
		if err != nil {
			return FailurePage{}, ErrInvalidCursor
		}
		var value repository.FailureCursor
		if err := json.Unmarshal(decoded, &value); err != nil || value.ID == "" || value.EventTimestamp.IsZero() {
			return FailurePage{}, ErrInvalidCursor
		}
		cursor = &value
	}
	failures, err := service.repository.ListFailures(ctx, principal.WorkspaceID, cursor, limit+1)
	if err != nil {
		return FailurePage{}, err
	}
	page := FailurePage{Failures: failures}
	if len(failures) > limit {
		page.Failures = failures[:limit]
		last := page.Failures[len(page.Failures)-1]
		encoded, _ := json.Marshal(repository.FailureCursor{EventTimestamp: last.EventTimestamp, ID: last.ID})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(encoded)
	}
	return page, nil
}

func (service *ProductService) PutDevice(ctx context.Context, principal repository.Principal, token, platform string) error {
	token = strings.TrimSpace(token)
	platform = strings.ToLower(strings.TrimSpace(platform))
	if token == "" || len(token) > 4096 || (platform != "android" && platform != "ios") {
		return ErrInvalidProductRequest
	}
	limits, err := entitlements.ForPlan(principal.PlanCode)
	if err != nil {
		return err
	}
	if limits.MaxDevices < 1 {
		return repository.ErrRepositoryLimit
	}
	ciphertext, err := service.secretBox.Encrypt([]byte(token))
	if err != nil {
		return fmt.Errorf("encrypt device token: %w", err)
	}
	return service.repository.PutDevice(ctx, principal, tokenHash(token), ciphertext, platform)
}

func (service *ProductService) DeleteDevice(ctx context.Context, principal repository.Principal) error {
	return service.repository.DeleteDevice(ctx, principal.UserID)
}

func (service *ProductService) DeleteAccount(ctx context.Context, principal repository.Principal) error {
	return service.repository.DeleteAccount(ctx, principal.UserID)
}

func (service *ProductService) newSecret() (string, []byte, error) {
	secret, err := randomToken()
	if err != nil {
		return "", nil, err
	}
	ciphertext, err := service.secretBox.Encrypt([]byte(secret))
	if err != nil {
		return "", nil, fmt.Errorf("encrypt webhook secret: %w", err)
	}
	return secret, ciphertext, nil
}

func (service *ProductService) webhookURL(endpointID string) string {
	return service.appBaseURL + "/webhooks/github/" + endpointID
}
