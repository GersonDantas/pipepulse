package repository

import (
	"pipeline-notifier/internal/models"
	"sync"
)

type State struct {
	PipelineID  string                `json:"pipeline_id"`
	Status      models.PipelineStatus `json:"status"`
	Timestamp   string                `json:"timestamp"`
	LastEventID string                `json:"last_event_id"`
}

var db = make(map[string]State)
var mu = sync.RWMutex{}

func GetState(id string) *State {
	mu.RLock()
	defer mu.RUnlock()

	if val, ok := db[id]; ok {
		return &val
	}
	return nil
}

func SaveState(s State) {
	mu.Lock()
	defer mu.Unlock()

	db[s.PipelineID] = s
}

func Reset() {
	mu.Lock()
	defer mu.Unlock()

	db = make(map[string]State)
}
