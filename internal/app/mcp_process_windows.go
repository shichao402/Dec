//go:build windows

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shichao402/Dec/internal/sysproc"
)

func listMCPProcessesOS() ([]mcpProcess, error) {
	// 只查 dec-exec 映像，避免全机 Win32_Process 扫描。
	script := `Get-CimInstance Win32_Process -Filter "Name='dec-exec.exe'" | Select-Object ProcessId,CommandLine | ConvertTo-Json -Compress`
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := sysproc.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("列举进程失败: %w", err)
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" || raw == "null" {
		return nil, nil
	}
	type row struct {
		ProcessId   json.Number `json:"ProcessId"`
		CommandLine *string     `json:"CommandLine"`
	}
	var rows []row
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		var one row
		if err2 := json.Unmarshal([]byte(raw), &one); err2 != nil {
			return nil, fmt.Errorf("解析进程列表失败: %w", err)
		}
		rows = []row{one}
	}
	outProcs := make([]mcpProcess, 0, len(rows))
	for _, r := range rows {
		pid64, err := r.ProcessId.Int64()
		if err != nil || pid64 <= 0 {
			continue
		}
		cmd := ""
		if r.CommandLine != nil {
			cmd = *r.CommandLine
		}
		if strings.TrimSpace(cmd) == "" {
			continue
		}
		outProcs = append(outProcs, mcpProcess{PID: int(pid64), CmdLine: cmd})
	}
	return outProcs, nil
}

func killMCPProcessOS(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("无效 pid")
	}
	cmd := sysproc.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("taskkill pid=%d: %w (%s)", pid, err, strings.TrimSpace(string(out)))
	}
	return nil
}
