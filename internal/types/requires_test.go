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
	if _, err := NormalizeRequiresSpec(RequiresSpec{"notes": RequiresVault}); err == nil {
		t.Fatal("vault pin is not a subscription")
	}
	stored, err := NormalizeStoredRequires(RequiresSpec{"notes": RequiresVault, "relkit": RequiresLatest})
	if err != nil {
		t.Fatal(err)
	}
	if stored["notes"] != RequiresVault || stored["relkit"] != RequiresLatest {
		t.Fatalf("stored = %#v", stored)
	}
}

func TestDropVaultPins(t *testing.T) {
	got := RequiresSpec{"relkit": RequiresLatest, "notes": RequiresVault, "dec": RequiresVault}.DropVaultPins()
	if len(got) != 1 || got["relkit"] != RequiresLatest {
		t.Fatalf("drop = %#v", got)
	}
	if (RequiresSpec{"notes": RequiresVault}).DropVaultPins() != nil {
		t.Fatal("only vault pins should become nil")
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

func TestFoldPublishedVaultPins(t *testing.T) {
	published := map[string]struct{}{"relkit": {}, "playbook": {}}
	got := RequiresSpec{
		"relkit":   RequiresVault,
		"playbook": "v0.2.0",
		"notes":    RequiresVault,
		"dec":      RequiresVault,
	}.FoldPublishedVaultPins(published, "dec")
	if got["relkit"] != RequiresLatest {
		t.Fatalf("已发布项目的 vault pin 应收成 latest，got %#v", got)
	}
	if got["playbook"] != "v0.2.0" || got["notes"] != RequiresVault || got["dec"] != RequiresVault {
		t.Fatalf("只改已发布且非本仓项目的 vault pin，got %#v", got)
	}
	same := RequiresSpec{"notes": RequiresVault}
	if folded := same.FoldPublishedVaultPins(nil, ""); folded["notes"] != RequiresVault {
		t.Fatalf("没确认注册表时不得改 pin，got %#v", folded)
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
