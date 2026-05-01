package freshness

import (
	"io"
	"os"
	"os/exec"
	"time"
)

// freshnessSubcommand 是内部 hidden 子命令的名字，供主 dec 进程在 PostRun 中 fork 自己。
// 以两个下划线开头表示它不对外，用户手册不列。
const freshnessSubcommand = "__freshness-check"

// fetchRemoteHead 间接层，方便单测替换掉真实网络 fetch。
var fetchRemoteHead = FetchRemoteHead

// StartBackgroundCheck 让调用方（通常是 PersistentPostRunE）零阻塞发起一次 staleness 检查。
//
// 行为：
//   - DEC_FRESHNESS_CHECK=off 时直接返回
//   - Windows 暂不支持 detached subprocess，直接返回（后续可以单独上）
//   - 解析当前可执行文件路径，fork 出 `dec __freshness-check --project-root <abs>` 子进程
//   - 子进程脱离父进程会话（Setsid），父进程立即返回，不 Wait
//
// 任何错误都被吞掉：这是辅助特性，失败不应影响主命令体感。
func StartBackgroundCheck(projectRoot string) {
	if IsDisabled() {
		return
	}
	exe, err := os.Executable()
	if err != nil || exe == "" {
		// 开发环境 `go run` 的临时二进制路径可能不稳；失败就 skip，不硬塞 os.Args[0]。
		return
	}
	cmd := exec.Command(exe, freshnessSubcommand, "--project-root", projectRoot)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	setDetached(cmd)
	_ = cmd.Start()
	// 不 Wait：子进程和父进程的生命周期已经解耦。
}

// RunBackgroundCheck 是子进程的主体逻辑，也被单测直接调用。
//
// 流程：
//  1. DEC_FRESHNESS_CHECK=off → 静默返回
//  2. 抢 lock；抢不到说明另一个后台 fetch 正在跑，直接返回
//  3. 读 .dec/.version；没 pull 过的项目不写 cache
//  4. throttle 未过窗口 → 不做 fetch，不覆盖已有 cache
//  5. 调 fetchRemoteHead，把结果（含错误）写进 cache
//  6. RecordCheck 保持与旧同步路径一致的 throttle 语义
//
// 返回的 error 只给测试用；生产环境子进程 main 函数拿到也是扔掉。
func RunBackgroundCheck(projectRoot string) error {
	if IsDisabled() {
		return nil
	}
	release, err := acquireFreshnessLock()
	if err != nil {
		return err
	}
	if release == nil {
		// 另一个后台 worker 正在跑，让它写 cache 就行。
		return nil
	}
	defer release()

	meta, err := LoadVersionMeta(projectRoot)
	if err != nil || meta == nil || meta.Commit == "" {
		// 项目没 pull 过，写空 cache 反而会让 PreRun 读到 Stale=false 的噪声。
		return nil
	}

	stateFile, err := StateFilePath(projectRoot)
	if err != nil {
		return err
	}
	if !ShouldCheck(stateFile, Interval()) {
		// 最近已经检查过，cache 由上一轮维护；不重复 fetch 节省带宽。
		return nil
	}

	remote, fetchErr := fetchRemoteHead()
	cached := CachedResult{
		LocalCommit:  meta.Commit,
		RemoteCommit: remote,
		CheckedAt:    time.Now(),
	}
	if fetchErr != nil {
		cached.Err = fetchErr.Error()
	}
	if writeErr := WriteCachedResult(projectRoot, cached); writeErr != nil {
		return writeErr
	}
	// 无论 fetch 成功与否都 touch state file，避免失败的 fetch 反复重试。
	_ = RecordCheck(stateFile)
	return nil
}

// EmitCachedHint 给调用方（通常是 PersistentPreRun）打印上一次后台检查的结论。
//
// 行为：
//   - DEC_FRESHNESS_CHECK=off / w 为 nil / 无 cache / cache 过期 / 结果 fresh 都静默
//   - cache 中 Err 非空（上次 fetch 失败）也静默——不能假设项目落后了就打扰用户
//   - stale → 复用 FormatHint/WriteHint 保持与旧同步路径一致的文案
func EmitCachedHint(w io.Writer, projectRoot string) {
	if w == nil {
		return
	}
	if IsDisabled() {
		return
	}
	r, err := ReadCachedResult(projectRoot)
	if err != nil || r == nil {
		return
	}
	if r.Err != "" {
		// 上次 fetch 出错，我们不知道真实远端状态，不打扰用户。
		return
	}
	if !r.IsStale() {
		return
	}
	WriteHint(w, CheckResult{
		LocalCommit:  r.LocalCommit,
		RemoteCommit: r.RemoteCommit,
		Stale:        true,
	})
}
