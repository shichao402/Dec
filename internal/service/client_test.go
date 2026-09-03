package service

import "testing"

func TestInteractiveAuthValue(t *testing.T) {
	old := isTestProcess
	isTestProcess = func() bool { return false }
	t.Cleanup(func() { isTestProcess = old })
	t.Setenv("CI", "")
	t.Setenv("DEC_NO_CONSOLE_LAUNCH", "")

	if got := interactiveAuthValue("mcp"); got != "1" {
		t.Fatalf("interactiveAuthValue(mcp) = %q", got)
	}
	if got := interactiveAuthValue("console"); got != "0" {
		t.Fatalf("interactiveAuthValue(console) = %q", got)
	}
	t.Setenv("CI", "true")
	if got := interactiveAuthValue("mcp"); got != "0" {
		t.Fatalf("interactiveAuthValue(mcp in CI) = %q", got)
	}
	t.Setenv("CI", "")
	t.Setenv("DEC_NO_CONSOLE_LAUNCH", "1")
	if got := interactiveAuthValue("mcp"); got != "0" {
		t.Fatalf("interactiveAuthValue(mcp disabled) = %q", got)
	}
}
