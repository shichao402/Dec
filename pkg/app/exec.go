package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/pkg/secrets"
)

// ExecWithSecretsInput 描述一次 dec exec 注入启动。
type ExecWithSecretsInput struct {
	ProjectRoot string
	Bundle      string
	Command     []string // 目标命令 argv；不可为空
	Environ     []string // 可选基环境；nil 则用 os.Environ()
}

// BuildExecEnviron 构造注入后的环境变量列表（不打印值）。
func BuildExecEnviron(projectRoot, bundle string, base []string) ([]string, error) {
	vars, err := secrets.LoadEnvForBundle(projectRoot, bundle)
	if err != nil {
		return nil, err
	}
	if base == nil {
		base = os.Environ()
	}
	// 先去掉将被覆盖的键，再追加。
	deny := make(map[string]struct{}, len(vars))
	for k := range vars {
		deny[k] = struct{}{}
	}
	out := make([]string, 0, len(base)+len(vars))
	for _, kv := range base {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			out = append(out, kv)
			continue
		}
		if _, drop := deny[kv[:eq]]; drop {
			continue
		}
		out = append(out, kv)
	}
	for k, v := range vars {
		out = append(out, k+"="+v)
	}
	return out, nil
}

// RunExecWithSecrets 注入 env 后执行命令，返回进程退出码。
func RunExecWithSecrets(input ExecWithSecretsInput) (int, error) {
	if strings.TrimSpace(input.ProjectRoot) == "" {
		return 1, fmt.Errorf("project-root 不能为空")
	}
	if len(input.Command) == 0 {
		return 1, fmt.Errorf("必须指定要执行的命令")
	}
	env, err := BuildExecEnviron(input.ProjectRoot, input.Bundle, input.Environ)
	if err != nil {
		return 1, err
	}
	cmd := exec.Command(input.Command[0], input.Command[1:]...)
	cmd.Dir = input.ProjectRoot
	cmd.Env = env
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err == nil {
		return 0, nil
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), nil
	}
	return 1, err
}

// WrapMCPServerWithExec 把 MCP server 启动命令包进 dec exec。
// 原 command/args 整体后移；清掉会泄露的 env 占位符注入。
func WrapMCPServerWithExec(projectRoot, bundle, decBin string, command string, args []string, env map[string]string) (string, []string, map[string]string) {
	if strings.TrimSpace(decBin) == "" {
		decBin = "dec"
	}
	root, _ := filepath.Abs(projectRoot)
	wrappedArgs := []string{
		"exec",
		"--project-root", root,
	}
	if strings.TrimSpace(bundle) != "" {
		wrappedArgs = append(wrappedArgs, "--bundle", bundle)
	}
	wrappedArgs = append(wrappedArgs, "--", command)
	for _, a := range args {
		wrappedArgs = append(wrappedArgs, strings.ReplaceAll(a, "${workspaceFolder}", root))
	}
	// 保留非密钥类显式 env；${workspaceFolder} 写成绝对项目根；其它 ${VAR} 占位去掉（由 dec exec 注入）。
	cleanEnv := make(map[string]string)
	for k, v := range env {
		v = strings.ReplaceAll(v, "${workspaceFolder}", root)
		if strings.Contains(v, "${") {
			continue
		}
		cleanEnv[k] = v
	}
	return decBin, wrappedArgs, cleanEnv
}
