package service

import (
	"context"
	"os"
	"testing"
)

func TestRuntimeMetadataRoundTripAndServerLock(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteMetadata("127.0.0.1:43210", token); err != nil {
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

// 服务退出只能删自己写出的发现文件。MetadataPath 依赖 DEC_HOME，进程内该值换过
// 之后再解析一次，会把后一个服务刚写的文件删掉——CI 上表现为下一个用例等不到
// server.json 而超时。
func TestRemoveMetadataAtIgnoresLaterHomeSwitch(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	firstPath, err := WriteMetadata("127.0.0.1:43210", token)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("DEC_HOME", t.TempDir())
	secondPath, err := WriteMetadata("127.0.0.1:43211", token)
	if err != nil {
		t.Fatal(err)
	}
	if firstPath == secondPath {
		t.Fatal("两个 DEC_HOME 应解析出不同路径")
	}

	RemoveMetadataAt(firstPath)

	if _, err := ReadMetadata(); err != nil {
		t.Fatalf("后一个 DEC_HOME 的发现文件被误删: %v", err)
	}
}

func TestWaitUntilStoppedCleansStaleMetadataWhenLockIsFree(t *testing.T) {
	t.Setenv("DEC_HOME", t.TempDir())
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteMetadata("127.0.0.1:43210", token); err != nil {
		t.Fatal(err)
	}

	if err := WaitUntilStopped(context.Background()); err != nil {
		t.Fatalf("WaitUntilStopped() = %v", err)
	}
	if _, err := ReadMetadata(); !os.IsNotExist(err) {
		t.Fatalf("残留 metadata 未清理: %v", err)
	}
}
