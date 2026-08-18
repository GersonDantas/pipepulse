package repository

import (
	"context"
	"sync"
	"time"

	"pipeline-notifier/internal/models"
)

type State struct {
	PipelineID     string                `json:"pipeline_id"`
	RepositoryID   int64                 `json:"repository_id"`
	WorkflowID     int64                 `json:"workflow_id"`
	WorkflowRunID  int64                 `json:"workflow_run_id"`
	RunAttempt     int                   `json:"run_attempt"`
	Status         models.PipelineStatus `json:"status"`
	Conclusion     string                `json:"conclusion"`
	Timestamp      time.Time             `json:"timestamp"`
	LastDeliveryID string                `json:"last_delivery_id"`
}

type StateReader interface {
	GetState(context.Context, string) (*State, error)
}

type StateRepository interface {
	StateReader
	SaveState(context.Context, State) error
}

type MemoryStateRepository struct {
	states map[string]State
	mu     sync.RWMutex
}

func NewMemoryStateRepository() *MemoryStateRepository {
	return &MemoryStateRepository{states: make(map[string]State)}
}

func (repository *MemoryStateRepository) GetState(_ context.Context, pipelineID string) (*State, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()

	state, ok := repository.states[pipelineID]
	if !ok {
		return nil, nil
	}
	return &state, nil
}

func (repository *MemoryStateRepository) SaveState(_ context.Context, state State) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	repository.states[state.PipelineID] = state
	return nil
}
