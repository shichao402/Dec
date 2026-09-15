package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"runtime"
	"strings"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
)

const (
	productName = "dec"
	channelName = "dev"
	keyID       = "dec-2026"
)

func entryURLs() []string {
	var cfg struct {
		Directory struct {
			EntryURLs []string `json:"entryUrls"`
		} `json:"directory"`
	}
	if err := json.Unmarshal(embeddedRelkitJSON, &cfg); err == nil && len(cfg.Directory.EntryURLs) > 0 {
		return append([]string(nil), cfg.Directory.EntryURLs...)
	}
	return []string{
		"https://raw.firoyang.com/rup/directory/dec.pb",
	}
}

func stateDir() (string, error) {
	return repo.GetRootDir()
}

// ManualInstallCommand returns the primary first-install command (CNB raw scripts).
// This is for fresh installs / docs — not an update-failure escape hatch.
func ManualInstallCommand() string {
	return manualInstallCommand(runtime.GOOS, false)
}

// MirrorInstallCommand returns an optional GitHub mirror install command.
// Prefer ManualInstallCommand; GitHub is a documentation backup, not required for self-update.
func MirrorInstallCommand() string {
	return manualInstallCommand(runtime.GOOS, true)
}

func manualInstallCommand(goos string, githubMirror bool) string {
	cfg := config.GetSystemConfig()
	branch := cfg.UpdateBranch
	if branch == "" {
		branch = "main"
	}
	owner := cfg.RepoOwner
	if owner == "" {
		owner = "shichao402"
	}
	name := cfg.RepoName
	if name == "" {
		name = "Dec"
	}
	var base string
	if githubMirror {
		base = fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/scripts", owner, name, branch)
	} else {
		base = fmt.Sprintf("https://cnb.cool/%s/%s/-/git/raw/%s/scripts", owner, name, branch)
	}
	if goos == "windows" {
		return fmt.Sprintf("iwr -useb %s/install.ps1 | iex", base)
	}
	return fmt.Sprintf("curl -fsSL %s/install.sh | bash", base)
}

// NetworkHelp returns self-update troubleshooting hints (RUP/COS only).
// Update failure is not an invitation to reinstall via GitHub/CNB install scripts.
func NetworkHelp() string {
	var sb strings.Builder
	sb.WriteString("自更新检查/下载只走 https://updates.firoyang.com/ ，与首次安装无关。\n")
	sb.WriteString("网络不可达时可以尝试：\n")
	sb.WriteString("  1. 确认本机可访问 https://updates.firoyang.com/\n")
	sb.WriteString("  2. 若使用代理客户端，需显式设置环境变量（Dec 不读取系统代理/PAC）：\n")
	if runtime.GOOS == "windows" {
		sb.WriteString("     $env:HTTPS_PROXY=\"http://127.0.0.1:<端口>\"\n")
	} else {
		sb.WriteString("     export HTTPS_PROXY=http://127.0.0.1:<端口>\n")
	}
	sb.WriteString("  3. 修好网络后重试；不必为此重装 Dec")
	return sb.String()
}

func describeRequestError(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if urlErr.Timeout() {
			return "请求超时"
		}
		if urlErr.Err != nil {
			return urlErr.Err.Error()
		}
	}
	return err.Error()
}
