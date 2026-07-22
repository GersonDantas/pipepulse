package repository

import (
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

type StateRepository interface {
	GetState(pipelineID string) *State
	SaveState(state State)
}

type MemoryStateRepository struct {
	states map[string]State
	mu     sync.RWMutex
}

func NewMemoryStateRepository() *MemoryStateRepository {
	return &MemoryStateRepository{states: make(map[string]State)}
}

func (repository *MemoryStateRepository) GetState(pipelineID string) *State {
	repository.mu.RLock()
	defer repository.mu.RUnlock()

	state, ok := repository.states[pipelineID]
	if !ok {
		return nil
	}
	return &state
}

func (repository *MemoryStateRepository) SaveState(state State) {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	repository.states[state.PipelineID] = state
}
