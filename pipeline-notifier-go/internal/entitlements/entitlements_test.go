package entitlements

import (
	"encoding/json"
	"testing"
)

func TestForPlanReturnsMVPFreeEntitlements(t *testing.T) {
	got, err := ForPlan("free")
	if err != nil {
		t.Fatalf("ForPlan() error = %v", err)
	}
	payload, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	want := `{"max_repositories":3,"max_workflows_per_repository":1,"max_devices":1,"history_days":7,"sponsor_enabled":false,"recovery_alerts":false,"integrations":false,"exports":false}`
	if string(payload) != want {
		t.Fatalf("entitlements JSON = %s, want %s", payload, want)
	}
}

func TestForPlanRejectsUnknownPlan(t *testing.T) {
	if _, err := ForPlan("enterprise"); err == nil {
		t.Fatal("ForPlan() error = nil, want unsupported plan error")
	}
}
