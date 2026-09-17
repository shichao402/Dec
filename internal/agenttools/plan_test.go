package agenttools

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildManifest_HasAllToolsAndSchemas(t *testing.T) {
	m, err := BuildManifest("v1.13.73")
	if err != nil {
		t.Fatal(err)
	}
	if m.Protocol != Protocol {
		t.Fatalf("protocol = %d", m.Protocol)
	}
	names := ToolNames()
	if len(m.Tools) != len(names) {
		t.Fatalf("tools = %d, want %d", len(m.Tools), len(names))
	}
	for i, listing := range m.Tools {
		if listing.Name != names[i] {
			t.Fatalf("tool[%d] = %s, want %s", i, listing.Name, names[i])
		}
		if listing.Owner != OwnerConsole && listing.Owner != OwnerServer {
			t.Fatalf("%s owner = %q", listing.Name, listing.Owner)
		}
		var schema map[string]any
		if err := json.Unmarshal(listing.InputSchema, &schema); err != nil {
			t.Fatalf("%s schema: %v", listing.Name, err)
		}
		if schema["type"] != "object" {
			t.Fatalf("%s schema type = %v", listing.Name, schema["type"])
		}
	}
	if OwnerOf("dec_console_status") != OwnerConsole {
		t.Fatal("console_status should be console-owned")
	}
	if OwnerOf("dec_pull") != OwnerServer {
		t.Fatal("pull should be server-owned")
	}
}

func TestPlan_BothPullSplitsPlanes(t *testing.T) {
	plan := Plan("dec_pull", json.RawMessage(`{"project_root":"D:/work","plane":"both"}`))
	if plan.Error != "" {
		t.Fatal(plan.Error)
	}
	if plan.Shape != ShapePlanes {
		t.Fatalf("shape = %s", plan.Shape)
	}
	if len(plan.Steps) != 2 {
		t.Fatalf("steps = %d", len(plan.Steps))
	}
	if plan.Steps[0].Kind != StepRun || plan.Steps[0].Method != "pull" || plan.Steps[0].Plane != "local" {
		t.Fatalf("step0 = %+v", plan.Steps[0])
	}
	if plan.Steps[1].Plane != "global" || plan.Steps[1].ProjectRoot != "" {
		t.Fatalf("step1 = %+v", plan.Steps[1])
	}
}

func TestPlan_InitProjectOptionalVault(t *testing.T) {
	plan := Plan("dec_init_project", json.RawMessage(`{"project_root":"D:/work","apply_vault_project":true}`))
	if plan.Error != "" || plan.Shape != ShapeKeyed || len(plan.Steps) != 2 {
		t.Fatalf("%+v", plan)
	}
	if plan.Steps[0].Key != "init" || plan.Steps[1].Key != "vault_apply" {
		t.Fatalf("keys = %s,%s", plan.Steps[0].Key, plan.Steps[1].Key)
	}
}

func TestPlan_DeleteRejectsBoth(t *testing.T) {
	plan := Plan("dec_delete", json.RawMessage(`{"plane":"both","confirmed":true,"project_root":"D:/work","items":[]}`))
	if plan.Error == "" || !strings.Contains(plan.Error, "both") {
		t.Fatalf("want both error, got %+v", plan)
	}
}

func TestPlan_LocalNeedsRoot(t *testing.T) {
	plan := Plan("dec_status", json.RawMessage(`{}`))
	if plan.Error == "" || !strings.Contains(plan.Error, "project_root") {
		t.Fatalf("%+v", plan)
	}
}

func TestPlan_ProvisionRequiresTarget(t *testing.T) {
	plan := Plan("dec_provision_remote", json.RawMessage(`{"confirmed":true}`))
	if plan.Error == "" || !strings.Contains(plan.Error, "ssh_target") {
		t.Fatalf("%+v", plan)
	}
}

func TestPlan_ConsoleOwnedHasNoSteps(t *testing.T) {
	plan := Plan("dec_console_status", json.RawMessage(`{}`))
	if plan.Owner != OwnerConsole || len(plan.Steps) != 0 || plan.Error != "" {
		t.Fatalf("%+v", plan)
	}
}
