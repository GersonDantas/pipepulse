package repository

import (
	"context"
	"errors"

	"pipeline-notifier/internal/models"
)

var ErrDeliveryNotFound = errors.New("delivery not found")
var ErrDeliveryAlreadyProcessed = errors.New("delivery already processed")

type ProcessingRepository interface {
	BeginProcessing(context.Context, models.Event) (EventTransaction, error)
}

type EventTransaction interface {
	CurrentState() *State
	SaveState(context.Context, State) error
	CreateFailure(context.Context, models.Event) (string, bool, error)
	CreateNotifications(context.Context, string) (int64, error)
	CompleteDelivery(context.Context, string) error
	Commit(context.Context) error
	Rollback(context.Context) error
}
