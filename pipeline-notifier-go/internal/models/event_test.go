package models

import (
	"testing"
	"time"
)

func TestNewWorkflowRunEventNormalizesCompletedFailure(t *testing.T) {
	conclusion := "failure"
	payload := GithubWebhookPayload{
		Repository: GithubRepository{ID: 10},
		WorkflowRun: GithubWorkflowRun{
			ID:         20,
			WorkflowID: 30,
			RunAttempt: 2,
			Status:     "completed",
			Conclusion: &conclusion,
			UpdatedAt:  "2026-05-16T12:00:00-03:00",
			HeadBranch: "main",
			HeadSHA:    "abc123",
			HTMLURL:    "https://github.com/example/repo/actions/runs/20",
		},
	}

	event, err := NewWorkflowRunEvent("delivery-1", payload)
	if err != nil {
		t.Fatalf("NewWorkflowRunEvent() error = %v", err)
	}

	if event.DeliveryID != "delivery-1" {
		t.Fatalf("DeliveryID = %q, want delivery-1", event.DeliveryID)
	}
	if event.RepositoryID != 10 || event.WorkflowID != 30 || event.WorkflowRunID != 20 || event.RunAttempt != 2 {
		t.Fatalf("event identifiers = %#v, want repository=10 workflow=30 run=20 attempt=2", event)
	}
	if event.Status != PipelineStatusFailed {
		t.Fatalf("Status = %q, want failed", event.Status)
	}
	if event.Conclusion != "failure" {
		t.Fatalf("Conclusion = %q, want failure", event.Conclusion)
	}
	wantTimestamp := time.Date(2026, time.May, 16, 15, 0, 0, 0, time.UTC)
	if !event.Timestamp.Equal(wantTimestamp) {
		t.Fatalf("Timestamp = %s, want %s", event.Timestamp, wantTimestamp)
	}
	if event.PipelineKey() != "10:30" {
		t.Fatalf("PipelineKey() = %q, want 10:30", event.PipelineKey())
	}
}

func TestNewWorkflowRunEventTreatsInProgressRunAsRunning(t *testing.T) {
	payload := GithubWebhookPayload{
		Repository: GithubRepository{ID: 10},
		WorkflowRun: GithubWorkflowRun{
			ID:         20,
			WorkflowID: 30,
			RunAttempt: 1,
			Status:     "in_progress",
			UpdatedAt:  "2026-05-16T12:00:00Z",
		},
	}

	event, err := NewWorkflowRunEvent("delivery-1", payload)
	if err != nil {
		t.Fatalf("NewWorkflowRunEvent() error = %v", err)
	}

	if event.Status != PipelineStatusRunning {
		t.Fatalf("Status = %q, want running", event.Status)
	}
}

func TestNewWorkflowRunEventMapsCancelledConclusion(t *testing.T) {
	conclusion := "cancelled"
	payload := GithubWebhookPayload{
		Repository: GithubRepository{ID: 10},
		WorkflowRun: GithubWorkflowRun{
			ID:         20,
			WorkflowID: 30,
			RunAttempt: 1,
			Status:     "completed",
			Conclusion: &conclusion,
			UpdatedAt:  "2026-05-16T12:00:00Z",
		},
	}

	event, err := NewWorkflowRunEvent("delivery-1", payload)
	if err != nil {
		t.Fatalf("NewWorkflowRunEvent() error = %v", err)
	}

	if event.Status != PipelineStatusCancelled {
		t.Fatalf("Status = %q, want cancelled", event.Status)
	}
}
