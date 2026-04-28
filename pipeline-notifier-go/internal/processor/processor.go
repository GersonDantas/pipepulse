package processor

import (
	"fmt"
	"pipeline-notifier/internal/models"
	"pipeline-notifier/internal/repository"
)

func ProcessEvent(event models.Event) {
	current := repository.GetState(event.PipelineID)

	// 🔁 Idempotência
	if current != nil && current.LastEventID == event.EventID {
		fmt.Println("⚠️ Evento duplicado")
		return
	}

	// ⏳ Timestamp (simplificado)
	if current != nil && event.Timestamp < current.Timestamp {
		fmt.Println("⏳ Evento antigo")
		return
	}

	// ⚖️ Prioridade
	if current != nil && event.Timestamp == current.Timestamp {
		if getPriority(event.Status) <= getPriority(current.Status) {
			fmt.Println("⚖️ Prioridade menor")
			return
		}
	}

	repository.SaveState(repository.State{
		PipelineID:  event.PipelineID,
		Status:      event.Status,
		Timestamp:   event.Timestamp,
		LastEventID: event.EventID,
	})

	fmt.Println("✅ Estado atualizado:", event.Status)

	notify(event)
}

func getPriority(status string) int {
	switch status {
	case "failed":
		return 3
	case "success":
		return 2
	case "running":
		return 1
	default:
		return 0
	}
}

func notify(event models.Event) {
	fmt.Println("🔔 Notificação:", event.Status)
}
