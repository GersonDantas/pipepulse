package models

type PipelineStatus string

const (
	PipelineStatusFailed  PipelineStatus = "failed"
	PipelineStatusSuccess PipelineStatus = "success"
	PipelineStatusRunning PipelineStatus = "running"
)

func (status PipelineStatus) Priority() int {
	switch status {
	case PipelineStatusFailed:
		return 3
	case PipelineStatusSuccess:
		return 2
	case PipelineStatusRunning:
		return 1
	default:
		return 0
	}
}

func NewPipelineStatus(value string) (PipelineStatus, bool) {
	status := PipelineStatus(value)

	switch status {
	case PipelineStatusFailed, PipelineStatusSuccess, PipelineStatusRunning:
		return status, true
	default:
		return "", false
	}
}
