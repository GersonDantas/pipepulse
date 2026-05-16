package processor

import (
	"testing"

	"pipeline-notifier/internal/models"
	"pipeline-notifier/internal/repository"
)

func captureNotifications(t *testing.T) *[]models.Event {
	t.Helper()

	original := notifyFn
	notifications := make([]models.Event, 0)

	notifyFn = func(event models.Event) {
		notifications = append(notifications, event)
	}

	t.Cleanup(func() {
		notifyFn = original
	})

	return &notifications
}

func TestProcessEventSavesNewPipelineState(t *testing.T) {
	repository.Reset()
	notifications := captureNotifications(t)

	event := models.Event{
		EventID:    "evt-1",
		PipelineID: "pipeline-1",
		Status:     "running",
		Timestamp:  "2026-01-01T10:00:00Z",
	}

	ProcessEvent(event)

	state := repository.GetState(event.PipelineID)
	if state == nil {
		t.Fatal("expected state to be saved")
	}

	if state.Status != event.Status {
		t.Fatalf("status = %q, want %q", state.Status, event.Status)
	}
	if state.Timestamp != event.Timestamp {
		t.Fatalf("timestamp = %q, want %q", state.Timestamp, event.Timestamp)
	}
	if state.LastEventID != event.EventID {
		t.Fatalf("last event id = %q, want %q", state.LastEventID, event.EventID)
	}
	if len(*notifications) != 1 {
		t.Fatalf("notifications = %d, want 1", len(*notifications))
	}
}

func TestProcessEventIgnoresDuplicateEvent(t *testing.T) {
	repository.Reset()
	notifications := captureNotifications(t)

	event := models.Event{
		EventID:    "evt-1",
		PipelineID: "pipeline-1",
		Status:     "running",
		Timestamp:  "2026-01-01T10:00:00Z",
	}

	ProcessEvent(event)
	ProcessEvent(models.Event{
		EventID:    "evt-1",
		PipelineID: "pipeline-1",
		Status:     "failed",
		Timestamp:  "2026-01-01T10:01:00Z",
	})

	state := repository.GetState(event.PipelineID)
	if state == nil {
		t.Fatal("expected state to exist")
	}

	if state.Status != "running" {
		t.Fatalf("status = %q, want running", state.Status)
	}
	if state.Timestamp != "2026-01-01T10:00:00Z" {
		t.Fatalf("timestamp = %q, want original timestamp", state.Timestamp)
	}
	if len(*notifications) != 1 {
		t.Fatalf("notifications = %d, want 1", len(*notifications))
	}
}

func TestProcessEventIgnoresOlderEvent(t *testing.T) {
	repository.Reset()
	notifications := captureNotifications(t)

	ProcessEvent(models.Event{
		EventID:    "evt-1",
		PipelineID: "pipeline-1",
		Status:     "failed",
		Timestamp:  "2026-01-01T10:00:00Z",
	})

	ProcessEvent(models.Event{
		EventID:    "evt-2",
		PipelineID: "pipeline-1",
		Status:     "running",
		Timestamp:  "2026-01-01T09:59:00Z",
	})

	state := repository.GetState("pipeline-1")
	if state == nil {
		t.Fatal("expected state to exist")
	}

	if state.Status != "failed" {
		t.Fatalf("status = %q, want failed", state.Status)
	}
	if state.LastEventID != "evt-1" {
		t.Fatalf("last event id = %q, want evt-1", state.LastEventID)
	}
	if len(*notifications) != 1 {
		t.Fatalf("notifications = %d, want 1", len(*notifications))
	}
}

func TestProcessEventUsesStatusPriorityWhenTimestampMatches(t *testing.T) {
	repository.Reset()
	notifications := captureNotifications(t)

	ProcessEvent(models.Event{
		EventID:    "evt-1",
		PipelineID: "pipeline-1",
		Status:     "running",
		Timestamp:  "2026-01-01T10:00:00Z",
	})

	ProcessEvent(models.Event{
		EventID:    "evt-2",
		PipelineID: "pipeline-1",
		Status:     "success",
		Timestamp:  "2026-01-01T10:00:00Z",
	})

	state := repository.GetState("pipeline-1")
	if state == nil {
		t.Fatal("expected state to exist")
	}

	if state.Status != "success" {
		t.Fatalf("status = %q, want success", state.Status)
	}
	if state.LastEventID != "evt-2" {
		t.Fatalf("last event id = %q, want evt-2", state.LastEventID)
	}
	if len(*notifications) != 2 {
		t.Fatalf("notifications = %d, want 2", len(*notifications))
	}
}

func TestProcessEventIgnoresLowerPriorityWhenTimestampMatches(t *testing.T) {
	repository.Reset()
	notifications := captureNotifications(t)

	ProcessEvent(models.Event{
		EventID:    "evt-1",
		PipelineID: "pipeline-1",
		Status:     "failed",
		Timestamp:  "2026-01-01T10:00:00Z",
	})

	ProcessEvent(models.Event{
		EventID:    "evt-2",
		PipelineID: "pipeline-1",
		Status:     "success",
		Timestamp:  "2026-01-01T10:00:00Z",
	})

	state := repository.GetState("pipeline-1")
	if state == nil {
		t.Fatal("expected state to exist")
	}

	if state.Status != "failed" {
		t.Fatalf("status = %q, want failed", state.Status)
	}
	if state.LastEventID != "evt-1" {
		t.Fatalf("last event id = %q, want evt-1", state.LastEventID)
	}
	if len(*notifications) != 1 {
		t.Fatalf("notifications = %d, want 1", len(*notifications))
	}
}

func TestProcessEventUpdatesStateWithoutNotifyingWhenStatusDoesNotChange(t *testing.T) {
	repository.Reset()
	notifications := captureNotifications(t)

	ProcessEvent(models.Event{
		EventID:    "evt-1",
		PipelineID: "pipeline-1",
		Status:     "running",
		Timestamp:  "2026-01-01T10:00:00Z",
	})

	ProcessEvent(models.Event{
		EventID:    "evt-2",
		PipelineID: "pipeline-1",
		Status:     "running",
		Timestamp:  "2026-01-01T10:01:00Z",
	})

	state := repository.GetState("pipeline-1")
	if state == nil {
		t.Fatal("expected state to exist")
	}

	if state.Timestamp != "2026-01-01T10:01:00Z" {
		t.Fatalf("timestamp = %q, want updated timestamp", state.Timestamp)
	}
	if state.LastEventID != "evt-2" {
		t.Fatalf("last event id = %q, want evt-2", state.LastEventID)
	}
	if len(*notifications) != 1 {
		t.Fatalf("notifications = %d, want 1", len(*notifications))
	}
}
