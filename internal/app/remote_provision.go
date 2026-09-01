package app

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/shichao402/Dec/internal/config"
)

// RemoteProvisionPort 是远端 dec-server 的固定 loopback 端口。
//
// 固定端口是「Console 不必让人手填端口」的前提：远端拿不到 ~/.dec/run/server.json，
// 只能靠约定。因为只监听 loopback，端口冲突风险可接受。
const RemoteProvisionPort = 47653

// RemoteProvisionListen 是远端 management_listen 的目标值。
const RemoteProvisionListen = "127.0.0.1:47653"

// remoteProbeTimeout 限制单次探测总时长，避免 SSH 卡死拖住调用方。
const remoteProbeTimeout = 25 * time.Second

// RemoteTarget 描述一台待置备设备的 SSH 落点。
//
// 只存引用（主机 / 用户 / ssh_config 别名），不存私钥内容与口令：
// 凭据解析交给系统 ssh 与 ~/.ssh/config，Dec 不接管。
type RemoteTarget struct {
	// Alias 为 ssh_config 中的 Host 别名；给定时 Host/User/Port 均可留空。
	Alias string
	Host  string
	User  string
	Port  int
}

// SSHDestination 返回登记与展示用的规范目标（含 host:port）。
func (t RemoteTarget) SSHDestination() string {
	ref, err := t.sshRef()
	if err != nil {
		if alias := strings.TrimSpace(t.Alias); alias != "" {
			return alias
		}
		return strings.TrimSpace(t.Host)
	}
	return ref.Canonical()
}

func (t RemoteTarget) sshDialTarget() string {
	ref, err := t.sshRef()
	if err != nil {
		return t.SSHDestination()
	}
	return ref.DialHost()
}

func (t RemoteTarget) sshPort() int {
	ref, err := t.sshRef()
	if err != nil {
		return t.Port
	}
	return ref.Port
}

func (t RemoteTarget) sshRef() (config.SSHTargetRef, error) {
	if t.Port < 0 || t.Port > 65535 {
		return config.SSHTargetRef{}, fmt.Errorf("SSH 端口 %d 无效", t.Port)
	}
	raw := strings.TrimSpace(t.Alias)
	if raw == "" {
		host := strings.TrimSpace(t.Host)
		user := strings.TrimSpace(t.User)
		if host == "" {
			return config.SSHTargetRef{}, fmt.Errorf("必须提供 SSH 主机或 ssh_config 别名")
		}
		if user != "" {
			raw = user + "@" + host
		} else {
			raw = host
		}
	}
	ref, err := config.ParseSSHTarget(raw)
	if err != nil {
		return config.SSHTargetRef{}, err
	}
	if t.Port > 0 {
		ref.Port = t.Port
	}
	return ref, nil
}

// DisplayName 是日志与 typed confirm 使用的稳定人类可读名。
func (t RemoteTarget) DisplayName() string {
	return t.SSHDestination()
}

func (t RemoteTarget) validate() error {
	_, err := t.sshRef()
	return err
}

// deviceOperationKeyPrefix 是设备级操作互斥键的前缀。
//
// broker 的互斥键当前是 project_root，而置备不属于任何项目。这里用合成键占位，
// 保证同一台目标机不被并发置备。该键**不是路径**：不得落盘、不得参与项目解析，
// servicehost 侧的 projectKey / ensureProjectRepaired 必须显式识别并放行。
const deviceOperationKeyPrefix = "device:"

// DeviceOperationKey 返回目标设备的操作互斥键。
func DeviceOperationKey(target RemoteTarget) string {
	return deviceOperationKeyPrefix + strings.ToLower(target.DisplayName())
}

// IsDeviceOperationKey 判断给定的 project_root 实为设备合成键。
func IsDeviceOperationKey(key string) bool {
	return strings.HasPrefix(strings.TrimSpace(key), deviceOperationKeyPrefix)
}

// RemoteHostProbe 是远端置备的只读前置检查结果。
//
// 它同时服务两个目的：让 Console 在人点「部署」之前就能判断这台机器行不行，
// 以及作为后续每一步置备的前置校验。本结构体不含任何副作用。
type RemoteHostProbe struct {
	Target string

	Reachable bool
	// SSHError 为 SSH 层失败原因；非空时其余字段无意义。
	SSHError string

	OS   string
	Arch string
	// Supported 为 false 时 Blockers 说明原因（如 Windows 远端）。
	Supported bool

	DecInstalled bool
	DecVersion   string
	// MissingBinaries 列出四件套中缺失者。
	MissingBinaries []string

	HasGit       bool
	HasSSHKeygen bool
	HasCurl      bool
	// HasBash 决定能否注入 scripts/install.sh——该脚本用了 bash 数组与 <<<，sh 跑不了。
	HasBash bool

	HomeWritable bool

	// SpawnCapable 表示非交互 SSH 能拉起脱离会话的后台进程。
	//
	// 这是「按需拉起」的前提（ADR 0019）：远端不做常驻，每次连接前由发起端
	// 经 SSH 拉起 dec-server，与本机门面 startServerProcess 等价。
	SpawnCapable bool
	// SpawnError 为拉起能力探测失败的原因。
	SpawnError string

	ManagementListen string
	// ListenReady 表示远端已配好置备约定的固定端口。
	ListenReady bool

	// ServerRunning 表示远端此刻已有 dec-server 在跑（存在 run/server.json）。
	//
	// 按需拉起模型下它为 false 是**正常状态**，不是故障：空闲即退出，连接时再拉起。
	ServerRunning bool

	// Blockers 是必须解决才能置备的问题；非空即不可继续。
	Blockers []string
	// Warnings 是不阻断但需要让人知道的后果。
	Warnings []string
	// NextAction 是给 Console 的建议动作。
	NextAction string
}

// decSuiteBinaries 与 scripts/install.sh 的 binaries 数组保持一致。
var decSuiteBinaries = []string{"dec", "dec-server", "dec-mcp", "dec-exec"}

// ProbeRemoteHost 只读探测目标机是否具备被置备的条件。
//
// 全程不修改目标机任何状态：只跑 uname / command -v / test / --version 之类的读命令。
func ProbeRemoteHost(ctx context.Context, target RemoteTarget, reporter Reporter) (*RemoteHostProbe, error) {
	reporter = defaultReporter(reporter)
	if err := target.validate(); err != nil {
		return nil, err
	}
	probe := &RemoteHostProbe{Target: target.DisplayName()}

	emit(reporter, EventInfo, "provision.probe", "探测 "+probe.Target, nil)

	ctx, cancel := context.WithTimeout(ctx, remoteProbeTimeout)
	defer cancel()

	out, err := runSSH(ctx, target, remoteProbeScript)
	if err != nil {
		probe.SSHError = summarizeSSHError(out, err)
		probe.Blockers = append(probe.Blockers, "SSH 连接失败: "+probe.SSHError)
		probe.NextAction = "先确认 ssh 能手动登录该主机（免密或已配置 ssh-agent）"
		emit(reporter, EventError, "provision.probe", probe.SSHError, nil)
		return probe, nil
	}
	probe.Reachable = true
	parseRemoteProbeOutput(out, probe)
	evaluateRemoteProbe(probe)

	for _, blocker := range probe.Blockers {
		emit(reporter, EventError, "provision.probe", blocker, nil)
	}
	for _, warning := range probe.Warnings {
		emit(reporter, EventWarn, "provision.probe", warning, nil)
	}
	if len(probe.Blockers) == 0 {
		emit(reporter, EventInfo, "provision.probe", "探测通过: "+probe.NextAction, nil)
	}
	return probe, nil
}

// remoteProbeScript 输出 key=value 行，全部为读操作。
//
// 用 sh 而非 bash：探测阶段还不能假设目标机有 bash。
const remoteProbeScript = `
echo "os=$(uname -s 2>/dev/null)"
echo "arch=$(uname -m 2>/dev/null)"
dec_home="${DEC_HOME:-$HOME/.dec}"
echo "dec_home=${dec_home}"
bin_dir="${dec_home}/bin"
for b in dec dec-server dec-mcp dec-exec; do
  if [ -x "${bin_dir}/${b}" ]; then echo "binary=${b}"; fi
done
if [ -x "${bin_dir}/dec" ]; then
  echo "dec_version=$("${bin_dir}/dec" --version 2>/dev/null | head -1)"
fi
for c in git ssh-keygen curl bash; do
  if command -v "$c" >/dev/null 2>&1; then echo "cmd=$c"; fi
done
if mkdir -p "${dec_home}" 2>/dev/null && [ -w "${dec_home}" ]; then echo "home_writable=1"; fi
if command -v nohup >/dev/null 2>&1 && command -v setsid >/dev/null 2>&1; then
  echo "spawn=both"
elif command -v nohup >/dev/null 2>&1; then
  echo "spawn=nohup"
else
  echo "spawn=none"
fi
if [ -f "${dec_home}/config.yaml" ]; then
  listen=$(awk '/^management_listen:/ { sub(/^management_listen:[[:space:]]*/, ""); print; exit }' "${dec_home}/config.yaml")
  echo "listen=${listen}"
fi
if [ -f "${dec_home}/run/server.json" ]; then echo "server_running=1"; fi
`

func parseRemoteProbeOutput(out string, probe *RemoteHostProbe) {
	found := map[string]struct{}{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "os":
			probe.OS = normalizeRemoteOS(value)
		case "arch":
			probe.Arch = normalizeRemoteArch(value)
		case "binary":
			found[value] = struct{}{}
		case "dec_version":
			probe.DecVersion = extractVersion(value)
		case "cmd":
			switch value {
			case "git":
				probe.HasGit = true
			case "ssh-keygen":
				probe.HasSSHKeygen = true
			case "curl":
				probe.HasCurl = true
			case "bash":
				probe.HasBash = true
			}
		case "home_writable":
			probe.HomeWritable = value == "1"
		case "spawn":
			switch value {
			case "both", "nohup":
				probe.SpawnCapable = true
			default:
				probe.SpawnCapable = false
				probe.SpawnError = "目标机缺少 nohup：无法拉起脱离 SSH 会话的后台进程"
			}
		case "server_running":
			probe.ServerRunning = value == "1"
		case "listen":
			probe.ManagementListen = strings.Trim(value, " \t\"'")
		}
	}
	for _, name := range decSuiteBinaries {
		if _, ok := found[name]; !ok {
			probe.MissingBinaries = append(probe.MissingBinaries, name)
		}
	}
	probe.DecInstalled = len(probe.MissingBinaries) == 0
	probe.ListenReady = strings.TrimSpace(probe.ManagementListen) == RemoteProvisionListen
}

// evaluateRemoteProbe 把原始事实翻译成阻断项、后果警告与建议动作。
func evaluateRemoteProbe(probe *RemoteHostProbe) {
	switch probe.OS {
	case "linux", "darwin":
		probe.Supported = true
	case "windows":
		probe.Blockers = append(probe.Blockers,
			"暂不支持 Windows 远端置备：SSH 服务端与服务注册（SCM / 计划任务）是另一套实现，见 ADR 0019")
	case "":
		probe.Blockers = append(probe.Blockers, "无法识别目标机操作系统（uname 无输出）")
	default:
		probe.Blockers = append(probe.Blockers, "不支持的操作系统: "+probe.OS)
	}
	if probe.Supported && probe.Arch == "" {
		probe.Blockers = append(probe.Blockers, "无法识别目标机架构")
	}
	if !probe.Supported {
		probe.NextAction = "改用受支持的 Linux / macOS 目标机"
		return
	}
	if !probe.HasCurl {
		probe.Blockers = append(probe.Blockers, "目标机缺少 curl：安装脚本无法下载产物")
	}
	if !probe.HasBash {
		probe.Blockers = append(probe.Blockers, "目标机缺少 bash：安装脚本依赖 bash 语法，无法执行")
	}
	if !probe.HomeWritable {
		probe.Blockers = append(probe.Blockers, "目标机 ~/.dec 不可写")
	}
	if !probe.HasGit {
		probe.Blockers = append(probe.Blockers, "目标机缺少 git：Vault 同步不可用")
	}
	if !probe.HasSSHKeygen {
		probe.Warnings = append(probe.Warnings, "目标机缺少 ssh-keygen：SSH 类 secrets 落地会失败")
	}
	if !probe.SpawnCapable {
		reason := probe.SpawnError
		if reason == "" {
			reason = "目标机无法拉起脱离 SSH 会话的后台进程"
		}
		probe.Blockers = append(probe.Blockers, reason+"：按需拉起需要它（ADR 0019）")
	}
	if probe.DecInstalled && strings.TrimSpace(probe.ManagementListen) != "" && !probe.ListenReady {
		probe.Warnings = append(probe.Warnings, fmt.Sprintf(
			"目标机已配置 management_listen=%s，置备会改为约定的 %s",
			probe.ManagementListen, RemoteProvisionListen))
	}

	switch {
	case len(probe.Blockers) > 0:
		probe.NextAction = "先解决阻断项，再执行置备"
	case !probe.DecInstalled:
		probe.NextAction = "可以置备：将安装 Dec 程序组并配置固定监听端口"
	case !probe.ListenReady:
		probe.NextAction = "程序组已就位，仍需配置固定监听端口（置备会自动完成）"
	default:
		probe.NextAction = "已完成置备，可直接连接（连接时会按需拉起远端服务）"
	}
}

func normalizeRemoteOS(raw string) string {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case value == "":
		return ""
	case strings.HasPrefix(value, "darwin"):
		return "darwin"
	case strings.HasPrefix(value, "linux"):
		return "linux"
	case strings.Contains(value, "mingw"), strings.Contains(value, "msys"),
		strings.Contains(value, "cygwin"), strings.Contains(value, "windows"):
		return "windows"
	default:
		return value
	}
}

func normalizeRemoteArch(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "x86_64", "amd64":
		return "amd64"
	case "arm64", "aarch64":
		return "arm64"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

// extractVersion 从 `dec --version` 输出里取出 vX.Y.Z。
func extractVersion(raw string) string {
	for _, field := range strings.Fields(raw) {
		trimmed := strings.TrimSpace(field)
		if len(trimmed) < 2 || trimmed[0] != 'v' {
			continue
		}
		parts := strings.Split(trimmed[1:], ".")
		if len(parts) < 3 {
			continue
		}
		numeric := true
		for _, part := range parts[:3] {
			if _, err := strconv.Atoi(strings.TrimSuffix(part, "\r")); err != nil {
				numeric = false
				break
			}
		}
		if numeric {
			return trimmed
		}
	}
	return ""
}

// sshArgs 组装 ssh 命令参数。
//
// 走系统 ssh 而非内置 SSH 库：与 Console 的隧道实现（client/src-tauri）复用同一条
// 信任链——~/.ssh/config、ssh-agent、known_hosts 全部生效，Dec 不接管主机校验与密钥管理。
func sshArgs(target RemoteTarget, command string) []string {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=accept-new",
	}
	if port := target.sshPort(); port > 0 {
		args = append(args, "-p", strconv.Itoa(port))
	}
	args = append(args, target.sshDialTarget(), command)
	return args
}

// runSSH 在目标机以 sh 执行脚本并返回合并输出。脚本经 stdin 注入，不落地到目标机磁盘。
func runSSH(ctx context.Context, target RemoteTarget, script string) (string, error) {
	return runSSHCommand(ctx, target, "sh -s", script)
}

// runSSHCommand 在目标机执行指定命令，脚本内容经 stdin 注入。
func runSSHCommand(ctx context.Context, target RemoteTarget, command, script string) (string, error) {
	cmd := exec.CommandContext(ctx, "ssh", sshArgs(target, command)...)
	cmd.Stdin = strings.NewReader(script)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// summarizeSSHError 把 ssh 的多行输出压成一句可读原因。
func summarizeSSHError(out string, err error) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "permission denied") ||
			strings.Contains(lower, "could not resolve") ||
			strings.Contains(lower, "connection refused") ||
			strings.Contains(lower, "connection timed out") ||
			strings.Contains(lower, "host key verification failed") ||
			strings.Contains(lower, "no route to host") {
			return line
		}
	}
	trimmed := strings.TrimSpace(out)
	if trimmed != "" {
		lines := strings.Split(trimmed, "\n")
		return strings.TrimSpace(lines[len(lines)-1])
	}
	if err != nil {
		return err.Error()
	}
	return "未知错误"
}

// LocalPlatformSupportsProvisioning 报告发起端能否执行置备。
//
// 置备由发起端 dec-server 作为 SSH 客户端完成，故只要求本机有 ssh 可执行文件；
// Windows 作为发起端是允许的，被置备的目标机才限制为 Linux / macOS。
func LocalPlatformSupportsProvisioning() (bool, string) {
	if _, err := exec.LookPath("ssh"); err != nil {
		if runtime.GOOS == "windows" {
			return false, "未找到 ssh 命令：请安装 Windows 可选功能「OpenSSH 客户端」"
		}
		return false, "未找到 ssh 命令：请安装 OpenSSH 客户端"
	}
	return true, ""
}
