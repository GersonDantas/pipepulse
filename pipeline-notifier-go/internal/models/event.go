package models

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrInvalidDeliveryID = errors.New("invalid delivery id")
var ErrInvalidWorkflowRun = errors.New("invalid workflow run")
var ErrInvalidEventTimestamp = errors.New("invalid event timestamp")

type Event struct {
	DeliveryID    string
	RepositoryID  int64
	WorkflowID    int64
	WorkflowRunID int64
	RunAttempt    int
	Status        PipelineStatus
	Conclusion    string
	Timestamp     time.Time
	Branch        string
	SHA           string
	RunURL        string
}

func NewWorkflowRunEvent(deliveryID string, payload GithubWebhookPayload) (Event, error) {
	deliveryID = strings.TrimSpace(deliveryID)
	if deliveryID == "" {
		return Event{}, ErrInvalidDeliveryID
	}

	workflowRun := payload.WorkflowRun
	if payload.Repository.ID <= 0 || workflowRun.ID <= 0 || workflowRun.WorkflowID <= 0 || workflowRun.RunAttempt <= 0 {
		return Event{}, fmt.Errorf("%w: identifiers must be positive", ErrInvalidWorkflowRun)
	}

	timestamp, err := time.Parse(time.RFC3339Nano, workflowRun.UpdatedAt)
	if err != nil {
		return Event{}, fmt.Errorf("%w: %v", ErrInvalidEventTimestamp, err)
	}

	status, conclusion, err := NormalizeWorkflowRunStatus(workflowRun.Status, workflowRun.Conclusion)
	if err != nil {
		return Event{}, err
	}

	return Event{
		DeliveryID:    deliveryID,
		RepositoryID:  payload.Repository.ID,
		WorkflowID:    workflowRun.WorkflowID,
		WorkflowRunID: workflowRun.ID,
		RunAttempt:    workflowRun.RunAttempt,
		Status:        status,
		Conclusion:    conclusion,
		Timestamp:     timestamp.UTC(),
		Branch:        workflowRun.HeadBranch,
		SHA:           workflowRun.HeadSHA,
		RunURL:        workflowRun.HTMLURL,
	}, nil
}

func (event Event) PipelineKey() string {
	return fmt.Sprintf("%d:%d", event.RepositoryID, event.WorkflowID)
}
