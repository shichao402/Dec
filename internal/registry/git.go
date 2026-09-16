package registry

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/shichao402/Dec/internal/sysproc"
)

func Git(ctx context.Context, dir string, args ...string) (string, error) {
	return GitEnv(ctx, dir, nil, args...)
}

func GitEnv(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	gitArgs := append([]string{"-c", "core.autocrlf=false", "-c", "core.eol=lf"}, args...)
	cmd := sysproc.CommandContext(ctx, "git", gitArgs...)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// TokenEnv 把 PAT 编进 https extraheader，避免交互凭据。
func TokenEnv(token string) []string {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	return []string{
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=never",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.extraHeader",
		"GIT_CONFIG_VALUE_0=Authorization: Bearer " + token,
	}
}
