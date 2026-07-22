package models

import (
	"fmt"
	"strings"
)

type PipelineStatus string

const (
	PipelineStatusFailed    PipelineStatus = "failed"
	PipelineStatusSuccess   PipelineStatus = "success"
	PipelineStatusCancelled PipelineStatus = "cancelled"
	PipelineStatusRunning   PipelineStatus = "running"
)

func (status PipelineStatus) Priority() int {
	switch status {
	case PipelineStatusFailed:
		return 3
	case PipelineStatusSuccess:
		return 2
	case PipelineStatusCancelled:
		return 1
	case PipelineStatusRunning:
		return 0
	default:
		return 0
	}
}

func NewPipelineStatus(value string) (PipelineStatus, bool) {
	status := PipelineStatus(value)

	switch status {
	case PipelineStatusFailed, PipelineStatusSuccess, PipelineStatusCancelled, PipelineStatusRunning:
		return status, true
	default:
		return "", false
	}
}

func NormalizeWorkflowRunStatus(workflowStatus string, conclusion *string) (PipelineStatus, string, error) {
	workflowStatus = strings.TrimSpace(strings.ToLower(workflowStatus))
	if workflowStatus != "completed" {
		switch workflowStatus {
		case "created", "queued", "in_progress", "requested", "waiting", "pending":
			return PipelineStatusRunning, "", nil
		default:
			return "", "", fmt.Errorf("unsupported workflow status %q", workflowStatus)
		}
	}

	if conclusion == nil || strings.TrimSpace(*conclusion) == "" {
		return PipelineStatusRunning, "", nil
	}

	normalizedConclusion := strings.TrimSpace(strings.ToLower(*conclusion))
	switch normalizedConclusion {
	case "success", "neutral", "skipped":
		return PipelineStatusSuccess, normalizedConclusion, nil
	case "cancelled", "stale":
		return PipelineStatusCancelled, normalizedConclusion, nil
	case "failure", "timed_out", "startup_failure", "action_required":
		return PipelineStatusFailed, normalizedConclusion, nil
	default:
		return "", "", fmt.Errorf("unsupported workflow conclusion %q", normalizedConclusion)
	}
}
