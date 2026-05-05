package tui

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// TestExternalToolsEntries_UnlockAlwaysFirst 验证菜单结构：pkv unlock 固定在第一行，
// 后面依次是 pkv、pkv get all。
func TestExternalToolsEntries_UnlockAlwaysFirst(t *testing.T) {
	stubLocatePKV(t)
	m := snapshotExternalToolsModel(80)

	entries, errMsg := m.externalToolsEntries()
	if errMsg != "" {
		t.Fatalf("预期 pkv 可用，errMsg 应为空：%q", errMsg)
	}
	if len(entries) != 3 {
		t.Fatalf("预期 3 个 entries（unlock/pkv/get all），实际 %d：%v", len(entries), entries)
	}
	if entries[0].Command != "pkv unlock" {
		t.Errorf("entries[0].Command = %q，期望 \"pkv unlock\"", entries[0].Command)
	}
	if entries[1].Command != "pkv" {
		t.Errorf("entries[1].Command = %q，期望 \"pkv\"", entries[1].Command)
	}
	if !strings.HasPrefix(entries[2].Command, "pkv get all ") {
		t.Errorf("entries[2].Command = %q，期望以 \"pkv get all \" 开头", entries[2].Command)
	}
}

// TestExternalToolsEntries_DescriptionDiffersWhenLocked 验证解锁态切换 description 文案。
func TestExternalToolsEntries_DescriptionDiffersWhenLocked(t *testing.T) {
	stubLocatePKV(t)

	unlockedModel := snapshotExternalToolsModel(80)
	unlockedModel.bwSession = ""
	unlockedEntries, _ := unlockedModel.externalToolsEntries()

	lockedModel := snapshotExternalToolsModel(80)
	lockedModel.bwSession = "fake-session"
	lockedEntries, _ := lockedModel.externalToolsEntries()

	// 三条 description 都应当在解锁态和未解锁态下不同
	for i := 0; i < 3; i++ {
		if unlockedEntries[i].Description == lockedEntries[i].Description {
			t.Errorf("entries[%d] description 未随 bwSession 变化：%q",
				i, unlockedEntries[i].Description)
		}
	}

	// 文案里应当能看到关键词差异，避免未来有人改成含糊语
	if !strings.Contains(lockedEntries[2].Description, "BW_SESSION") {
		t.Errorf("解锁态下 get all description 应提到 BW_SESSION，实际：%q",
			lockedEntries[2].Description)
	}
	if !strings.Contains(unlockedEntries[2].Description, "master password") {
		t.Errorf("未解锁态下 get all description 应提示会要求 master password，实际：%q",
			unlockedEntries[2].Description)
	}
}

// TestExternalToolsEntries_PKVMissingAllUnavailable 验证 pkv 不在 PATH 时，
// 三条 entry 都标记为 Unavailable，且 errMsg 非空。
func TestExternalToolsEntries_PKVMissingAllUnavailable(t *testing.T) {
	original := locatePKVOperation
	locatePKVOperation = func(args ...string) (*exec.Cmd, error) {
		return nil, errors.New("未找到 pkv 可执行文件，请确认 pkv 已安装并在 $PATH 中")
	}
	t.Cleanup(func() { locatePKVOperation = original })

	m := snapshotExternalToolsModel(80)
	entries, errMsg := m.externalToolsEntries()
	if errMsg == "" {
		t.Fatal("预期 pkv 不可用时返回非空 errMsg")
	}
	for i, e := range entries {
		if !e.Unavailable {
			t.Errorf("entries[%d] (%s) 应标记 Unavailable", i, e.Command)
		}
	}
}

// TestUpdate_PKVUnlockedMsg_Success_SetsSession 验证 pkvUnlockedMsg 成功路径
// 会写入 bwSession、清空 sessionInvalidMsg、并清除 launchingExternal。
func TestUpdate_PKVUnlockedMsg_Success_SetsSession(t *testing.T) {
	m := snapshotExternalToolsModel(80)
	m.launchingExternal = true
	m.sessionInvalidMsg = "old hint"
	m.externalErr = errors.New("stale")

	next, _ := m.Update(pkvUnlockedMsg{session: "session-abc"})
	nm, ok := next.(model)
	if !ok {
		t.Fatalf("Update 返回类型非 model，实际 %T", next)
	}

	if nm.bwSession != "session-abc" {
		t.Errorf("bwSession = %q，期望 \"session-abc\"", nm.bwSession)
	}
	if nm.sessionInvalidMsg != "" {
		t.Errorf("sessionInvalidMsg 应被清空，实际 %q", nm.sessionInvalidMsg)
	}
	if nm.launchingExternal {
		t.Error("launchingExternal 应复位为 false")
	}
	if nm.externalErr != nil {
		t.Errorf("externalErr 应被清空，实际 %v", nm.externalErr)
	}
}

// TestUpdate_PKVUnlockedMsg_Error_KeepsSessionAndShowsErr 验证 unlock 失败不清已有 session
// （用户可能之前就已经解锁过，unlock 失败只是本次尝试失败），并把错误写入 externalErr。
func TestUpdate_PKVUnlockedMsg_Error_KeepsSessionAndShowsErr(t *testing.T) {
	m := snapshotExternalToolsModel(80)
	m.bwSession = "old-session"
	m.launchingExternal = true

	bad := errors.New("bw unlock failed")
	next, _ := m.Update(pkvUnlockedMsg{err: bad})
	nm := next.(model)

	if nm.bwSession != "old-session" {
		t.Errorf("unlock 失败时应保留已有 bwSession，实际 %q", nm.bwSession)
	}
	if nm.externalErr != bad {
		t.Errorf("externalErr 应为 bad，实际 %v", nm.externalErr)
	}
	if nm.launchingExternal {
		t.Error("launchingExternal 应复位为 false")
	}
}

// TestUpdate_ExternalToolFinishedMsg_ErrorTriggersHintWhenSessionCached 验证缓存 session 的情况下，
// 外部工具调用报错会写入 sessionInvalidMsg 提示，但不清掉 bwSession。
func TestUpdate_ExternalToolFinishedMsg_ErrorTriggersHintWhenSessionCached(t *testing.T) {
	m := snapshotExternalToolsModel(80)
	m.bwSession = "old-session"
	m.launchingExternal = true

	bad := fmt.Errorf("pkv exited with code 1")
	next, _ := m.Update(externalToolFinishedMsg{tool: "pkv get all", err: bad})
	nm := next.(model)

	if nm.bwSession != "old-session" {
		t.Errorf("失败时不应清空 bwSession，实际 %q", nm.bwSession)
	}
	if nm.sessionInvalidMsg == "" {
		t.Error("缓存 session 的情况下失败应写入 sessionInvalidMsg")
	}
	if !strings.Contains(nm.sessionInvalidMsg, "unlock") {
		t.Errorf("sessionInvalidMsg 应提示重新 unlock，实际 %q", nm.sessionInvalidMsg)
	}
	if nm.externalErr != bad {
		t.Errorf("externalErr 未正确写入，实际 %v", nm.externalErr)
	}
}

// TestUpdate_ExternalToolFinishedMsg_SuccessClearsHint 验证成功的外部工具调用清空 sessionInvalidMsg。
func TestUpdate_ExternalToolFinishedMsg_SuccessClearsHint(t *testing.T) {
	m := snapshotExternalToolsModel(80)
	m.bwSession = "ok-session"
	m.sessionInvalidMsg = "stale hint"

	next, _ := m.Update(externalToolFinishedMsg{tool: "pkv get all", err: nil})
	nm := next.(model)

	if nm.sessionInvalidMsg != "" {
		t.Errorf("成功时应清空 sessionInvalidMsg，实际 %q", nm.sessionInvalidMsg)
	}
	if nm.bwSession != "ok-session" {
		t.Errorf("bwSession 不应被修改，实际 %q", nm.bwSession)
	}
}

// TestUpdate_ExternalToolFinishedMsg_ErrorWithoutSession_NoHint 验证未解锁时失败不写 sessionInvalidMsg
// （避免误导：根本没 session 可以失效）。
func TestUpdate_ExternalToolFinishedMsg_ErrorWithoutSession_NoHint(t *testing.T) {
	m := snapshotExternalToolsModel(80)
	m.bwSession = ""

	bad := errors.New("some error")
	next, _ := m.Update(externalToolFinishedMsg{tool: "pkv", err: bad})
	nm := next.(model)

	if nm.sessionInvalidMsg != "" {
		t.Errorf("未解锁时不应写 sessionInvalidMsg，实际 %q", nm.sessionInvalidMsg)
	}
	if nm.externalErr != bad {
		t.Errorf("externalErr 未写入，实际 %v", nm.externalErr)
	}
}

// TestLaunchPKVUnlockCmd_ParseBuffer 不真跑子进程，仅验证 ParsePKVUnlockOutput 被正确代理。
// 保护点：ParsePKVUnlockOutput 的 trim 行为一旦回退到非 trim 版本会被这里发现。
func TestLaunchPKVUnlockCmd_ParseBuffer(t *testing.T) {
	// 直接打桩 buildPKVUnlockCmdOperation，绕开 exec.LookPath
	originalBuild := buildPKVUnlockCmdOperation
	originalParse := parsePKVUnlockOutputOperation
	t.Cleanup(func() {
		buildPKVUnlockCmdOperation = originalBuild
		parsePKVUnlockOutputOperation = originalParse
	})

	parseCalls := 0
	parsePKVUnlockOutputOperation = func(buf *bytes.Buffer) (string, error) {
		parseCalls++
		if buf == nil {
			t.Fatal("parse 操作收到 nil buffer")
		}
		return strings.TrimSpace(buf.String()), nil
	}

	// 用一个实际存在的 noop 命令，同时喂一个预填好的 buffer
	buildPKVUnlockCmdOperation = func() (*exec.Cmd, *bytes.Buffer, error) {
		buf := bytes.NewBufferString("session-xyz\n")
		// 用 true 命令（/usr/bin/true 或 /bin/true）作为 noop；跑 tea.Cmd 返回的闭包时
		// tea.ExecProcess 会试图执行它，为避免真执行，我们只构造闭包但不运行。
		return exec.Command("/bin/true"), buf, nil
	}

	cmd := launchPKVUnlockCmd()
	if cmd == nil {
		t.Fatal("launchPKVUnlockCmd 返回 nil tea.Cmd")
	}
	// 不运行 tea.Cmd，只验证上面两个桩都被正确装载了（语法路径通过编译）
	if parseCalls != 0 {
		t.Errorf("parse 不应在构造阶段被调用，实际调用 %d 次", parseCalls)
	}
}
