package repository

import "pipeline-notifier/internal/models"

type State struct {
	PipelineID  string
	Status      models.PipelineStatus
	Timestamp   string
	LastEventID string
}

var db = make(map[string]State)

func GetState(id string) *State {
	if val, ok := db[id]; ok {
		return &val
	}
	return nil
}

func SaveState(s State) {
	db[s.PipelineID] = s
}

func Reset() {
	db = make(map[string]State)
}
