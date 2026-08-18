package entitlements

import "fmt"

const PlanFree = "free"

type Entitlements struct {
	MaxRepositories           int  `json:"max_repositories"`
	MaxWorkflowsPerRepository int  `json:"max_workflows_per_repository"`
	MaxDevices                int  `json:"max_devices"`
	HistoryDays               int  `json:"history_days"`
	SponsorEnabled            bool `json:"sponsor_enabled"`
	RecoveryAlerts            bool `json:"recovery_alerts"`
	Integrations              bool `json:"integrations"`
	Exports                   bool `json:"exports"`
}

func ForPlan(planCode string) (Entitlements, error) {
	if planCode != PlanFree {
		return Entitlements{}, fmt.Errorf("unsupported plan %q", planCode)
	}
	return Entitlements{
		MaxRepositories:           3,
		MaxWorkflowsPerRepository: 1,
		MaxDevices:                1,
		HistoryDays:               7,
		SponsorEnabled:            false,
		RecoveryAlerts:            false,
		Integrations:              false,
		Exports:                   false,
	}, nil
}
