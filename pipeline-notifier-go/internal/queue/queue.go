package queue

import (
	"pipeline-notifier/internal/models"
	"pipeline-notifier/internal/processor"
)

var eventChannel = make(chan models.Event, 100)

func StartWorker() {
	go func() {
		for event := range eventChannel {
			processor.ProcessEvent(event)
		}
	}()
}

func Enqueue(event models.Event) {
	eventChannel <- event
}
