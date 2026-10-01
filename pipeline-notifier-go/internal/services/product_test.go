package services

import (
	"context"
	"testing"
	"time"

	"pipeline-notifier/internal/repository"
	"pipeline-notifier/internal/secrets"
)

type fakeProductRepository struct {
	createInput      repository.CreateRepositoryInput
	secretCiphertext []byte
	repositoryLimit  int
	failures         []repository.Failure
	failureCursor    *repository.FailureCursor
	failureLimit     int
}

func (repo *fakeProductRepository) CreateRepository(_ context.Context, _ string, input repository.CreateRepositoryInput, secret []byte, limit int) (repository.ProductRepository, error) {
	repo.createInput = input
	repo.secretCiphertext = append([]byte(nil), secret...)
	repo.repositoryLimit = limit
	return repository.ProductRepository{ID: "repository-1", EndpointID: "endpoint-1", Owner: input.Owner, Name: input.Name}, nil
}

func (repo *fakeProductRepository) ListRepositories(context.Context, string) ([]repository.ProductRepository, error) {
	return nil, nil
}

func (repo *fakeProductRepository) GetRepository(context.Context, string, string) (repository.ProductRepository, error) {
	return repository.ProductRepository{}, nil
}

func (repo *fakeProductRepository) RotateRepositorySecret(context.Context, string, string, []byte) error {
	return nil
}

func (repo *fakeProductRepository) DeleteRepository(context.Context, string, string) error {
	return nil
}

func (repo *fakeProductRepository) ListFailures(_ context.Context, _ string, cursor *repository.FailureCursor, limit int) ([]repository.Failure, error) {
	repo.failureCursor = cursor
	repo.failureLimit = limit
	return repo.failures, nil
}

func (repo *fakeProductRepository) PutDevice(context.Context, repository.Principal, []byte, []byte, string) error {
	return nil
}

func (repo *fakeProductRepository) DeleteDevice(context.Context, string) error  { return nil }
func (repo *fakeProductRepository) DeleteAccount(context.Context, string) error { return nil }

func TestProductServiceAppliesFreeEntitlementsAndOneTimeSecret(t *testing.T) {
	repo := &fakeProductRepository{}
	box, err := secrets.NewBox([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewProductService(repo, box, "https://api.pipepulse.app")
	principal := repository.Principal{UserID: "user-1", WorkspaceID: "workspace-1", GithubLogin: "octocat", PlanCode: "free"}

	bootstrap, err := service.Bootstrap(principal)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if bootstrap.Plan != "free" || bootstrap.Entitlements.MaxRepositories != 3 || bootstrap.User.GithubLogin != "octocat" {
		t.Fatalf("bootstrap = %#v", bootstrap)
	}
	created, err := service.CreateRepository(context.Background(), principal, repository.CreateRepositoryInput{
		GithubRepositoryID: 10, Owner: "acme", Name: "api",
		Workflow: repository.CreateWorkflowInput{GithubWorkflowID: 20, Name: "CI"},
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}
	if repo.repositoryLimit != 3 || created.WebhookSecret == "" || created.WebhookURL != "https://api.pipepulse.app/webhooks/github/endpoint-1" {
		t.Fatalf("created = %#v, limit = %d", created, repo.repositoryLimit)
	}
	plaintext, err := box.Decrypt(repo.secretCiphertext)
	if err != nil || string(plaintext) != created.WebhookSecret {
		t.Fatalf("stored secret does not decrypt to returned one-time secret")
	}
}

func TestProductServicePaginatesFailuresWithOpaqueCursor(t *testing.T) {
	repo := &fakeProductRepository{}
	for index := 0; index < 21; index++ {
		repo.failures = append(repo.failures, repository.Failure{ID: string(rune('a' + index)), EventTimestamp: time.Date(2026, 8, 19, 15, index, 0, 0, time.UTC)})
	}
	box, _ := secrets.NewBox([]byte("0123456789abcdef0123456789abcdef"))
	service := NewProductService(repo, box, "https://api.pipepulse.app")
	principal := repository.Principal{WorkspaceID: "workspace-1", PlanCode: "free"}

	page, err := service.ListFailures(context.Background(), principal, "", 20)
	if err != nil {
		t.Fatalf("ListFailures() error = %v", err)
	}
	if len(page.Failures) != 20 || page.NextCursor == "" || repo.failureLimit != 21 {
		t.Fatalf("page = %#v, repository limit = %d", page, repo.failureLimit)
	}
	if _, err := service.ListFailures(context.Background(), principal, page.NextCursor, 50); err != nil {
		t.Fatalf("ListFailures(cursor) error = %v", err)
	}
	if repo.failureCursor == nil || repo.failureCursor.ID != page.Failures[19].ID {
		t.Fatalf("decoded cursor = %#v", repo.failureCursor)
	}
	if _, err := service.ListFailures(context.Background(), principal, "invalid", 20); err == nil {
		t.Fatal("invalid cursor was accepted")
	}
}
