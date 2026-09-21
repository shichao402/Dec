package types

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNormalizeRequiresSpec(t *testing.T) {
	got, err := NormalizeRequiresSpec(RequiresSpec{"relkit": "", "tencent-cloud": "v0.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if got["relkit"] != RequiresLatest || got["tencent-cloud"] != "v0.1.0" {
		t.Fatalf("%#v", got)
	}
	if _, err := NormalizeRequiresSpec(RequiresSpec{"Bad": "v1"}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := NormalizeRequiresSpec(RequiresSpec{"relkit": "1.0"}); err == nil {
		t.Fatal("expected v prefix")
	}
}

func TestAddVaultProjectsKeepsExistingLatestPin(t *testing.T) {
	got := RequiresSpec{"agent-dev-playbook": RequiresLatest, "notes": RequiresVault}.AddVaultProjects([]string{"agent-dev-playbook", "woa"})
	if got["agent-dev-playbook"] != RequiresLatest {
		t.Fatalf("latest pin overwritten: %#v", got)
	}
	if got["woa"] != RequiresVault || got["notes"] != RequiresVault {
		t.Fatalf("vault pins: %#v", got)
	}
}

func TestRequiresSpecRejectsYAMLList(t *testing.T) {
	var cfg struct {
		Requires RequiresSpec `yaml:"requires"`
	}
	if err := yaml.Unmarshal([]byte("requires:\n  - relkit\n"), &cfg); err == nil {
		t.Fatal("expected list rejection")
	}
}
