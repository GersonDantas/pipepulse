package models

type Event struct {
	EventID    string
	PipelineID string
	Status     PipelineStatus
	Timestamp  string
}
