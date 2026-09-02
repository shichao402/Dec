package app

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// 按需拉起的时间预算。
//
// 远端与本机生命周期一致：空闲即退出，需要时由请求方拉起（ADR 0019）。
// 这里的等待时长比本机（internal/service.Connect 的 10s）宽一档——每次轮询都要
// 付一次完整 SSH 握手，而不是本机的一次本地文件读取。
const (
	remoteSetupTimeout   = 30 * time.Second
	remoteSpawnTimeout   = 20 * time.Second
	remoteReadyTimeout   = 25 * time.Second
	remoteReadyPollDelay = 700 * time.Millisecond
)

// RemoteServiceSetupResult 是远端配置固定监听端口的结果。
type RemoteServiceSetupResult struct {
	Target string
	// Listen 是写入后生效的监听地址。
	Listen string
	// Changed 为 false 表示远端已是目标值，本次未落盘。
	Changed bool
	// PreviousListen 是写入前的值，空表示此前未配置。
	PreviousListen string
	// ConfigPath 是远端被写入的配置文件路径。
	ConfigPath string
}

// ConfigureRemoteService 在目标机上幂等写入固定管理监听端口。
//
// 实际写入由远端的 `dec __service-setup` 完成，本函数只负责调用与解析：
// 配置合并规则必须只有一份实现，见 ADR 0019。
func ConfigureRemoteService(ctx context.Context, target RemoteTarget, reporter Reporter) (*RemoteServiceSetupResult, error) {
	reporter = defaultReporter(reporter)
	if err := target.validate(); err != nil {
		return nil, err
	}
	if ok, reason := LocalPlatformSupportsProvisioning(); !ok {
		return nil, fmt.Errorf("%s", reason)
	}

	emit(reporter, EventInfo, "provision.setup",
		fmt.Sprintf("配置 %s 的固定监听端口 %s", target.DisplayName(), RemoteProvisionListen), nil)

	setupCtx, cancel := context.WithTimeout(ctx, remoteSetupTimeout)
	defer cancel()

	out, err := runSSH(setupCtx, target, remoteServiceSetupScript)
	if err != nil {
		return nil, fmt.Errorf("远端配置失败: %s", summarizeSSHError(out, err))
	}

	result := &RemoteServiceSetupResult{Target: target.DisplayName()}
	ok := false
	for _, line := range strings.Split(out, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "listen":
			result.Listen = value
		case "changed":
			result.Changed = value == "true"
		case "previous":
			result.PreviousListen = value
		case "config":
			result.ConfigPath = value
		case "service-setup":
			ok = value == "ok"
		}
	}
	if !ok {
		return nil, fmt.Errorf("远端配置未确认成功: %s", lastMeaningfulLine(out))
	}
	if result.Listen != RemoteProvisionListen {
		return nil, fmt.Errorf("远端监听地址为 %q，期望 %q", result.Listen, RemoteProvisionListen)
	}

	if result.Changed {
		message := "已写入 " + result.Listen
		if result.PreviousListen != "" {
			message = fmt.Sprintf("已把监听地址由 %s 改为 %s", result.PreviousListen, result.Listen)
		}
		emit(reporter, EventInfo, "provision.setup", message, nil)
	} else {
		emit(reporter, EventInfo, "provision.setup", "监听地址已是 "+result.Listen+"，无需改动", nil)
	}
	return result, nil
}

// remoteServiceSetupScript 调用远端自己的 dec 写配置。
//
// 不用 `dec`（PATH 里可能没有）而是显式走 ${DEC_HOME:-$HOME/.dec}/bin/dec：
// 置备后的产物就在那里，而非交互 SSH 的 PATH 往往不含它。
const remoteServiceSetupScript = `
set -e
dec_home="${DEC_HOME:-$HOME/.dec}"
dec_bin="${dec_home}/bin/dec"
if [ ! -x "${dec_bin}" ]; then
  echo "missing-dec=${dec_bin}"
  exit 1
fi
"${dec_bin}" __service-setup
`

// RemoteServiceStatus 描述远端 dec-server 此刻的运行状态。
type RemoteServiceStatus struct {
	Target string
	// Running 表示远端 run/server.json 存在且可解析。
	Running bool
	// Endpoint 是远端服务自报的监听地址。
	Endpoint string
	// PID 是远端服务进程号。
	PID int
	// Spawned 表示本次调用真的拉起了一个新进程。
	Spawned bool
	// WaitedMS 是等待就绪耗时，便于判断是否值得调整超时。
	WaitedMS int64
}

// EnsureRemoteServiceRunning 保证远端 dec-server 正在运行，必要时经 SSH 拉起。
//
// 这是「远端不常驻」的另一半（ADR 0019）：与本机门面的
// internal/service.Connect 完全同构——先读 metadata，读不到才拉起，然后轮询就绪。
// 远端进程不在运行是**正常状态**，不是故障。
func EnsureRemoteServiceRunning(ctx context.Context, target RemoteTarget, reporter Reporter) (*RemoteServiceStatus, error) {
	reporter = defaultReporter(reporter)
	if err := target.validate(); err != nil {
		return nil, err
	}
	if ok, reason := LocalPlatformSupportsProvisioning(); !ok {
		return nil, fmt.Errorf("%s", reason)
	}

	status := &RemoteServiceStatus{Target: target.DisplayName()}

	if existing, err := readRemoteServiceStatus(ctx, target); err == nil && existing.Running {
		status.Running = true
		status.Endpoint = existing.Endpoint
		status.PID = existing.PID
		emit(reporter, EventInfo, "provision.spawn",
			fmt.Sprintf("远端服务已在运行（%s，pid %d）", status.Endpoint, status.PID), nil)
		return status, nil
	}

	emit(reporter, EventInfo, "provision.spawn", "远端服务未运行，经 SSH 拉起", nil)
	spawnCtx, cancel := context.WithTimeout(ctx, remoteSpawnTimeout)
	defer cancel()
	out, err := runSSH(spawnCtx, target, remoteSpawnScript)
	if err != nil {
		return nil, fmt.Errorf("拉起远端服务失败: %s", summarizeSSHError(out, err))
	}
	if strings.Contains(out, "missing-dec-server=") {
		return nil, fmt.Errorf("远端缺少 dec-server，请先完成置备")
	}
	status.Spawned = true

	started := time.Now()
	deadline := started.Add(remoteReadyTimeout)
	var lastErr error
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(remoteReadyPollDelay):
		}
		ready, err := readRemoteServiceStatus(ctx, target)
		if err == nil && ready.Running {
			status.Running = true
			status.Endpoint = ready.Endpoint
			status.PID = ready.PID
			status.WaitedMS = time.Since(started).Milliseconds()
			emit(reporter, EventInfo, "provision.spawn",
				fmt.Sprintf("远端服务就绪（%s，pid %d，等待 %dms）",
					status.Endpoint, status.PID, status.WaitedMS), nil)
			return status, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			break
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("远端服务拉起后未就绪: %w", lastErr)
	}
	return nil, fmt.Errorf("远端服务拉起后 %s 内未就绪", remoteReadyTimeout)
}

// remoteSpawnScript 拉起远端 dec-server 并使其脱离本次 SSH 会话。
//
// setsid + nohup 双保险：SSH 会话结束时会向进程组发 SIGHUP，只靠 & 放后台的进程
// 会跟着死。dec-server 自身的 detachedProcessAttributes 只作用于它拉起的子进程，
// 管不到它自己被谁拉起。
const remoteSpawnScript = `
dec_home="${DEC_HOME:-$HOME/.dec}"
server_bin="${dec_home}/bin/dec-server"
if [ ! -x "${server_bin}" ]; then
  echo "missing-dec-server=${server_bin}"
  exit 1
fi
if command -v setsid >/dev/null 2>&1; then
  setsid nohup "${server_bin}" >/dev/null 2>&1 < /dev/null &
else
  nohup "${server_bin}" >/dev/null 2>&1 < /dev/null &
fi
echo "spawn-requested=1"
exit 0
`

// readRemoteServiceStatus 读取远端 run/server.json。
//
// 只取 endpoint 与 pid，**不取 token**：那是远端本机 RPC 凭据，
// 隧道建立后由 Authenticate 换取 control token（0018），置备链路不搬运它。
func readRemoteServiceStatus(ctx context.Context, target RemoteTarget) (*RemoteServiceStatus, error) {
	readCtx, cancel := context.WithTimeout(ctx, remoteProbeTimeout)
	defer cancel()
	out, err := runSSH(readCtx, target, remoteServerStatusScript)
	if err != nil {
		return nil, fmt.Errorf("读取远端服务状态失败: %s", summarizeSSHError(out, err))
	}
	status := &RemoteServiceStatus{Target: target.DisplayName()}
	for _, line := range strings.Split(out, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "endpoint":
			status.Endpoint = value
		case "pid":
			status.PID = parsePositiveInt(value)
		case "alive":
			status.Running = value == "1"
		}
	}
	if status.Running && status.Endpoint == "" {
		return nil, fmt.Errorf("远端服务发现文件缺少 endpoint")
	}
	return status, nil
}

// remoteServerStatusScript 只读 run/server.json，并用 kill -0 确认进程真的活着。
//
// 单看文件存在不够：进程被 kill -9 后 server.json 会残留（正常退出才清理），
// 那时报 running 会让上层跳过拉起、直接去连一个已经没人监听的端口。
const remoteServerStatusScript = `
dec_home="${DEC_HOME:-$HOME/.dec}"
meta="${dec_home}/run/server.json"
if [ ! -f "${meta}" ]; then
  echo "alive=0"
  exit 0
fi
endpoint=$(sed -n 's/.*"endpoint"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "${meta}" | head -1)
pid=$(sed -n 's/.*"pid"[[:space:]]*:[[:space:]]*\([0-9]*\).*/\1/p' "${meta}" | head -1)
echo "endpoint=${endpoint}"
echo "pid=${pid}"
if [ -n "${pid}" ] && kill -0 "${pid}" 2>/dev/null; then
  echo "alive=1"
else
  echo "alive=0"
fi
`

// stopRemoteServiceForUpgrade 只在已建立信任的设备升级时使用。
// 先停旧进程，避免 Unix 上虽然能替换可执行文件，但旧进程继续用旧协议接收连接。
func stopRemoteServiceForUpgrade(ctx context.Context, target RemoteTarget) error {
	stopCtx, cancel := context.WithTimeout(ctx, remoteSpawnTimeout)
	defer cancel()
	out, err := runSSH(stopCtx, target, remoteStopForUpgradeScript)
	if err != nil {
		return fmt.Errorf("停止远端旧服务失败: %s", summarizeSSHError(out, err))
	}
	if !strings.Contains(out, "stopped=1") {
		return fmt.Errorf("远端旧服务未确认停止: %s", lastMeaningfulLine(out))
	}
	return nil
}

const remoteStopForUpgradeScript = `
set -e
dec_home="${DEC_HOME:-$HOME/.dec}"
meta="${dec_home}/run/server.json"
if [ ! -f "${meta}" ]; then
  echo "stopped=1"
  exit 0
fi
pid=$(sed -n 's/.*"pid"[[:space:]]*:[[:space:]]*\([0-9]*\).*/\1/p' "${meta}" | head -1)
if [ -n "${pid}" ] && kill -0 "${pid}" 2>/dev/null; then
  kill "${pid}"
  i=0
  while kill -0 "${pid}" 2>/dev/null && [ "${i}" -lt 50 ]; do
    sleep 0.1
    i=$((i + 1))
  done
  if kill -0 "${pid}" 2>/dev/null; then
    echo "stopped=0"
    exit 1
  fi
fi
rm -f "${meta}"
echo "stopped=1"
`

func parsePositiveInt(raw string) int {
	value := 0
	for _, r := range strings.TrimSpace(raw) {
		if r < '0' || r > '9' {
			return 0
		}
		value = value*10 + int(r-'0')
		if value > 1<<31 {
			return 0
		}
	}
	return value
}

func lastMeaningfulLine(out string) string {
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if trimmed := strings.TrimSpace(lines[i]); trimmed != "" {
			return trimmed
		}
	}
	return "远端无输出"
}
