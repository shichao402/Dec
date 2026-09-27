package secrets

import (
	"strings"
	"testing"
)

func TestPasswordTypeRegistered(t *testing.T) {
	p, ok := LookupProcessor("password")
	if !ok {
		t.Fatal("password processor missing")
	}
	if p.Dir != ".password" {
		t.Fatalf("password dir = %q", p.Dir)
	}
	if p.UserCreatable {
		t.Fatal(".password must not be user creatable")
	}
	if len(p.SourceModes) != 0 {
		t.Fatalf(".password must have no manual source modes, got %v", p.SourceModes)
	}
	if !p.WritesSecureNote() {
		t.Fatal(".password should write secure note")
	}
}

func TestParseTypePathPassword(t *testing.T) {
	tp, ok, err := ParseTypePath(".password/dev-box")
	if err != nil {
		t.Fatalf("parse .password path: %v", err)
	}
	if !ok || tp.Type.ID != SecretTypePassword {
		t.Fatalf("want password type, got ok=%t id=%s", ok, tp.Type.ID)
	}
	if tp.Instance != "dev-box" {
		t.Fatalf("instance = %q", tp.Instance)
	}
	// 未知点目录仍硬失败
	if _, _, err := ParseTypePath(".unknown/x"); err == nil {
		t.Fatal("unknown dot dir must fail")
	}
}

func TestPasswordNormalizeNameRejected(t *testing.T) {
	p, _ := LookupProcessor("password")
	if _, err := p.NormalizeName(".password/dev-box"); err == nil {
		t.Fatal(".password normalize must reject manual registration")
	}
	// 普通 note 落 .password/ 路径也要被挡：路径属于已注册点类型但类型不匹配
	note, _ := LookupProcessor("note")
	if _, err := note.NormalizeName(".password/dev-box"); err == nil {
		t.Fatal("plain note must reject .password path")
	}
}

func TestPasswordUserCreatableFilter(t *testing.T) {
	// LocalAssetKinds 在 app 包，这里校验表级语义：人工可创建类型不含 password
	for _, p := range RegisteredProcessors() {
		if p.ID == SecretTypePassword && p.UserCreatable {
			t.Fatal(".password leaked into user-creatable set")
		}
	}
	// 既有四类不受影响
	creatable := 0
	for _, p := range RegisteredProcessors() {
		if p.UserCreatable {
			creatable++
		}
	}
	if creatable != 4 {
		t.Fatalf("want 4 user-creatable processors, got %d", creatable)
	}
}

func TestPasswordPullShape(t *testing.T) {
	// pull 落地路径与其他 note 一致：相对同步根的 .password/<alias>
	tp, ok, err := ParseTypePath(".password/dev-box")
	if err != nil || !ok {
		t.Fatalf("parse: %v ok=%t", err, ok)
	}
	if !strings.HasPrefix(tp.Full, ".password/") {
		t.Fatalf("full = %q", tp.Full)
	}
}
