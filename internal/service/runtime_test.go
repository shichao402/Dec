package service

import (
	"os"
	"testing"
)

func TestRuntimeMetadataRoundTripAndServerLock(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteMetadata("127.0.0.1:43210", token); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadMetadata()
	if err != nil {
		t.Fatal(err)
	}
	if meta.Endpoint != "127.0.0.1:43210" || meta.Token != token || meta.PID != os.Getpid() {
		t.Fatalf("metadata round trip = %#v", meta)
	}

	first, err := AcquireServerLock()
	if err != nil {
		t.Fatal(err)
	}
	defer first.Unlock()
	if _, err := AcquireServerLock(); err == nil {
		t.Fatal("同一 DEC_HOME 不应同时取得两个 server lock")
	}
}

func TestNewTokenIsRandom(t *testing.T) {
	first, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 || first == second {
		t.Fatalf("token 不符合 256-bit 随机要求: %q / %q", first, second)
	}
}
