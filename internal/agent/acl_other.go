//go:build !windows

package agent

// restrictToOwner 在类 Unix 上不需要额外动作：写入时已按 0600 落盘。
func restrictToOwner(string) error { return nil }
