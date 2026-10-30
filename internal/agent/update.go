package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/shichao402/Dec/internal/release"
)

// 自动更新（ADR 0058）。
//
// 分两段，跑在两个进程里：
//
//  1. 代理进程（Updater）：定时、或握手发现版本不对齐时，拉取签名的发布清单，
//     校验通过且本机版本低于要求版本时，把新二进制下载到暂存目录并逐个校验
//     sha256，然后把自身复制成 update/dec-updater，以脱离服务的方式拉起它。
//     这一段全程不碰正在运行的文件，失败了代理照常运行。
//  2. 更新进程（ApplyUpdate，dec-updater update-apply）：停服务 → 换文件 →
//     起服务 → 确认新版本真的起来了；没起来就换回旧文件再起一次。
//
// 为什么要第二个进程：代理是被服务管理器托管的，它不能「停掉自己再把自己
// 启动起来」；Windows 上运行中的 exe 也不能被覆盖。更新进程用的是旧版本
// 二进制的一份拷贝——它在这台机器上已经证明能跑，比用刚下载的新版本来执行
// 替换更可靠。
//
// 为什么验签放在代理里而不是脚本里：Windows PowerShell 5.1 和 macOS 自带的
// LibreSSL 都没有现成的 ed25519 校验，脚本做不到；Go 标准库可以。

const (
	// updateDirName 是状态目录下的更新工作目录。
	updateDirName = "update"
	// updateFailBackoff 是同一版本更新失败后的冷却时间，防止反复停服务。
	updateFailBackoff = 6 * time.Hour
	// updateHealthTimeout 是等新版本在本机页面报到的最长时间。
	updateHealthTimeout = 90 * time.Second
	// updateLockStale 是更新锁的过期时间：更新进程崩溃时锁不能永远留着。
	updateLockStale = 15 * time.Minute
)

// UpdateDir 返回更新工作目录。
func UpdateDir(stateDir string) string { return filepath.Join(stateDir, updateDirName) }

func updatePlanPath(stateDir string) string   { return filepath.Join(UpdateDir(stateDir), "plan.json") }
func updateResultPath(stateDir string) string { return filepath.Join(UpdateDir(stateDir), "last.json") }
func updateLockPath(stateDir string) string   { return filepath.Join(UpdateDir(stateDir), "apply.lock") }

// UpdaterBinaryPath 是更新进程可执行文件的位置。
func UpdaterBinaryPath(stateDir string) string {
	name := "dec-updater"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(UpdateDir(stateDir), name)
}

// UpdateItem 是一个要替换的文件。
type UpdateItem struct {
	// Kind 是 agent 或 tray。
	Kind string `json:"kind"`
	// Target 是本机正在使用的文件。
	Target string `json:"target"`
	// Staged 是已下载并校验过的新文件。
	Staged string `json:"staged"`
}

// UpdatePlan 是代理交给更新进程的全部信息。
//
// 只有路径与版本号：更新进程不联网、不验签，它信任的唯一输入是代理已经
// 校验过的暂存文件，所以计划文件放在只有用户自己能写的状态目录里。
type UpdatePlan struct {
	From     string       `json:"from"`
	Version  string       `json:"version"`
	StateDir string       `json:"state_dir"`
	Items    []UpdateItem `json:"items"`
	Created  time.Time    `json:"created"`
}

// UpdateResult 是最近一次更新的结果，由更新进程写、代理读。
type UpdateResult struct {
	From     string    `json:"from"`
	Version  string    `json:"version"`
	OK       bool      `json:"ok"`
	Reason   string    `json:"reason,omitempty"`
	At       time.Time `json:"at"`
	Reported bool      `json:"reported"`
}

// Updater 是代理进程里的自动更新循环。
type Updater struct {
	// ServerURL 是服务地址，清单与二进制从它的下载通道取。
	ServerURL string
	// Version 是当前代理版本。
	Version string
	// StateDir 是状态目录。
	StateDir string
	// Interval 是定时检查间隔，<=0 用 1 小时。
	Interval time.Duration
	// InitialDelay 是启动后第一次检查前的等待，<0 表示不等。
	InitialDelay time.Duration
	// Logf 是日志出口。
	Logf func(format string, args ...any)
	// Notifier 用于在更新失败时通知用户，可为空。
	Notifier Notifier

	// 以下为测试替身，生产留空。
	HTTP       *http.Client
	Keys       []string
	Executable func() (string, error)
	Launch     func(updater string, args []string) error
	GOOS       string
	GOARCH     string
	Now        func() time.Time

	kickOnce sync.Once
	kick     chan struct{}
}

func (u *Updater) init() {
	u.kickOnce.Do(func() { u.kick = make(chan struct{}, 1) })
}

func (u *Updater) logf(format string, args ...any) {
	if u.Logf != nil {
		u.Logf(format, args...)
	}
}

func (u *Updater) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now()
}

// Kick 请求尽快检查一次，不阻塞。
func (u *Updater) Kick() {
	u.init()
	select {
	case u.kick <- struct{}{}:
	default:
	}
}

// Run 是更新循环，阻塞到 ctx 结束。
func (u *Updater) Run(ctx context.Context) {
	u.init()
	u.reportLast()
	interval := u.Interval
	if interval <= 0 {
		interval = time.Hour
	}
	delay := u.InitialDelay
	if delay < 0 {
		delay = 0
	}
	u.logf("自动更新已启用：%s 后首次检查，之后每 %s 检查一次", delay, interval)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-u.kick:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}
		started, err := u.CheckOnce(ctx)
		if err != nil {
			u.logf("自动更新检查失败: %v", err)
		}
		if started {
			// 更新进程马上会停掉本进程；在此之前不再发起第二次。
			return
		}
		timer.Reset(interval)
	}
}

// reportLast 把上一次更新的结果写进日志，失败时通知一次。
func (u *Updater) reportLast() {
	p := updateResultPath(u.StateDir)
	data, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var r UpdateResult
	if json.Unmarshal(data, &r) != nil || r.Reported {
		return
	}
	if r.OK {
		u.logf("自动更新完成：%s -> %s", r.From, r.Version)
	} else {
		u.logf("自动更新到 %s 失败，已回滚到 %s：%s", r.Version, r.From, r.Reason)
		if u.Notifier != nil {
			_ = u.Notifier.Notify("Dec 代理自动更新失败",
				fmt.Sprintf("更新到 %s 失败，已回滚到 %s。%s", r.Version, r.From, r.Reason))
		}
	}
	r.Reported = true
	if out, err := json.MarshalIndent(r, "", "  "); err == nil {
		_ = writeFileAtomic(p, out, 0o600)
	}
}

// CheckOnce 检查一次；需要更新时下载、暂存并拉起更新进程。
//
// 返回 started=true 表示更新进程已经启动。
func (u *Updater) CheckOnce(ctx context.Context) (bool, error) {
	if _, ok := release.ParseVersion(u.Version); !ok {
		// 本地构建（dev 等）不参与自动更新，否则会被线上版本覆盖掉。
		return false, nil
	}
	m, err := u.fetchManifest(ctx)
	if err != nil {
		return false, err
	}
	if cmp, ok := release.CompareVersion(u.Version, m.RequiredClient); !ok || cmp >= 0 {
		// 已经满足要求版本。只改服务端的发布会沿用旧的要求版本，
		// 这时不更新：客户端代码没变，下载一遍没有意义。
		return false, nil
	}
	// 防降级：清单是签过名的，但旧清单同样签过名。被回放一份旧清单时
	// 不能把代理「更新」到更旧的版本。
	if cmp, ok := release.CompareVersion(m.Version, u.Version); !ok || cmp <= 0 {
		return false, fmt.Errorf("清单版本 %s 不比本机 %s 新，拒绝更新", m.Version, u.Version)
	}
	if cmp, ok := release.CompareVersion(m.Version, m.RequiredClient); !ok || cmp < 0 {
		return false, fmt.Errorf("清单版本 %s 低于它自己的要求版本 %s", m.Version, m.RequiredClient)
	}
	if last := u.readResult(); last != nil && !last.OK && last.Version == m.Version &&
		u.now().Sub(last.At) < updateFailBackoff {
		return false, nil
	}

	exe, err := u.executable()
	if err != nil {
		return false, err
	}
	items, err := u.planItems(m, exe)
	if err != nil {
		return false, err
	}
	u.logf("发现新版本 %s（本机 %s，要求 %s），开始下载", m.Version, u.Version, m.RequiredClient)

	stage := filepath.Join(UpdateDir(u.StateDir), "staged", m.Version)
	if err := os.RemoveAll(stage); err != nil {
		return false, err
	}
	if err := os.MkdirAll(stage, 0o700); err != nil {
		return false, err
	}
	for i := range items {
		f, _ := m.Find(items[i].Staged)
		dst := filepath.Join(stage, f.Name)
		if err := u.download(ctx, f, dst); err != nil {
			return false, err
		}
		items[i].Staged = dst
	}

	updater := UpdaterBinaryPath(u.StateDir)
	if err := copyFile(exe, updater, 0o755); err != nil {
		return false, fmt.Errorf("准备更新进程失败: %w", err)
	}
	plan := UpdatePlan{
		From:     u.Version,
		Version:  m.Version,
		StateDir: u.StateDir,
		Items:    items,
		Created:  u.now().UTC(),
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return false, err
	}
	planPath := updatePlanPath(u.StateDir)
	if err := writeFileAtomic(planPath, data, 0o600); err != nil {
		return false, err
	}
	launch := u.Launch
	if launch == nil {
		launch = launchDetached
	}
	if err := launch(updater, []string{"update-apply", "--plan", planPath}); err != nil {
		return false, fmt.Errorf("启动更新进程失败: %w", err)
	}
	u.logf("已拉起更新进程，即将更新到 %s", m.Version)
	return true, nil
}

func (u *Updater) readResult() *UpdateResult {
	data, err := os.ReadFile(updateResultPath(u.StateDir))
	if err != nil {
		return nil
	}
	var r UpdateResult
	if json.Unmarshal(data, &r) != nil {
		return nil
	}
	return &r
}

func (u *Updater) executable() (string, error) {
	get := u.Executable
	if get == nil {
		get = os.Executable
	}
	p, err := get()
	if err != nil {
		return "", err
	}
	// 装在符号链接后面时替换的是真实文件，链接保持不动。
	if real, err := filepath.EvalSymlinks(p); err == nil {
		p = real
	}
	return filepath.Abs(p)
}

func (u *Updater) goos() string {
	if u.GOOS != "" {
		return u.GOOS
	}
	return runtime.GOOS
}

func (u *Updater) goarch() string {
	if u.GOARCH != "" {
		return u.GOARCH
	}
	return runtime.GOARCH
}

// planItems 决定要换哪些文件。Staged 字段暂存的是清单里的文件名。
func (u *Updater) planItems(m *release.Manifest, exe string) ([]UpdateItem, error) {
	osName, arch := u.goos(), u.goarch()
	agentName := fmt.Sprintf("dec-agent-%s-%s", osName, arch)
	if osName == "windows" {
		agentName += ".exe"
	}
	if _, ok := m.Find(agentName); !ok {
		return nil, fmt.Errorf("发布清单里没有本平台的代理 %s", agentName)
	}
	items := []UpdateItem{{Kind: "agent", Target: exe, Staged: agentName}}

	// 托盘：本机装了、清单里也有才换。CI 允许某次发布缺托盘（构建降级），
	// 那就保留旧托盘——它和代理之间只有本机页面这一层接口，旧托盘照样能用。
	var trayName string
	switch osName {
	case "darwin":
		trayName = "dec-tray-darwin-" + arch
	case "windows":
		trayName = "dec-tray-windows-amd64.exe"
	}
	if trayName != "" {
		if _, ok := m.Find(trayName); ok {
			if tray, err := FindTrayBinary(exe); err == nil {
				if real, err := filepath.EvalSymlinks(tray); err == nil {
					tray = real
				}
				items = append(items, UpdateItem{Kind: "tray", Target: tray, Staged: trayName})
			}
		}
	}
	return items, nil
}

func (u *Updater) httpClient() *http.Client {
	if u.HTTP != nil {
		return u.HTTP
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

func (u *Updater) get(ctx context.Context, name string, limit int64) ([]byte, error) {
	url := strings.TrimRight(u.ServerURL, "/") + "/download/" + name
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载 %s 返回 %d", name, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s 超过 %d 字节", name, limit)
	}
	return data, nil
}

func (u *Updater) fetchManifest(ctx context.Context) (*release.Manifest, error) {
	data, err := u.get(ctx, release.ManifestName, 1<<20)
	if err != nil {
		return nil, err
	}
	sig, err := u.get(ctx, release.SignatureName, 4<<10)
	if err != nil {
		return nil, err
	}
	keys := u.Keys
	if keys == nil {
		keys = release.TrustedKeys
	}
	return release.ParseVerified(keys, data, string(sig))
}

// download 下载一个产物并按清单校验长度与 sha256；不通过时不留下文件。
func (u *Updater) download(ctx context.Context, f release.File, dst string) error {
	url := strings.TrimRight(u.ServerURL, "/") + "/download/" + f.Name
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := u.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载 %s 返回 %d", f.Name, resp.StatusCode)
	}
	tmp := dst + ".part"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(resp.Body, f.Size+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("下载 %s 失败: %w", f.Name, err)
	}
	if n != f.Size {
		os.Remove(tmp)
		return fmt.Errorf("%s 长度不符：清单 %d，实际 %d", f.Name, f.Size, n)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, f.SHA256) {
		os.Remove(tmp)
		return fmt.Errorf("%s 的 sha256 不符：清单 %s，实际 %s", f.Name, f.SHA256, got)
	}
	return os.Rename(tmp, dst)
}

// copyFile 复制文件（先写临时文件再改名）。
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		os.Remove(tmp)
		return err
	}
	// Windows 上目标存在且可能正被运行（上一次的 dec-updater）：先挪开。
	if _, err := os.Stat(dst); err == nil {
		_ = os.Remove(dst + ".old")
		if err := os.Rename(dst, dst+".old"); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	return os.Rename(tmp, dst)
}

// ---- 更新进程 ----

// ServiceControl 是更新进程停、起服务的方式，测试里替换。
type ServiceControl interface {
	Stop(items []UpdateItem) error
	Start(items []UpdateItem) error
}

// ApplyOptions 是更新进程的参数。
type ApplyOptions struct {
	PlanPath string
	Logf     func(format string, args ...any)
	// 以下为测试替身。
	Control ServiceControl
	// Health 确认新版本已经起来；为空用本机页面探测。
	Health func(ctx context.Context, plan *UpdatePlan) error
	// ReplaceTimeout 是等文件解除占用的最长时间。
	ReplaceTimeout time.Duration
	Now            func() time.Time
}

// ErrUpdateBusy 表示另一个更新进程正在运行。
var ErrUpdateBusy = errors.New("另一个更新进程正在运行")

// ApplyUpdate 执行一次替换。任何一步失败都尽力回到旧版本并把服务起来。
func ApplyUpdate(ctx context.Context, opts ApplyOptions) error {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	data, err := os.ReadFile(opts.PlanPath)
	if err != nil {
		return err
	}
	var plan UpdatePlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return fmt.Errorf("更新计划解析失败: %w", err)
	}
	if plan.StateDir == "" || len(plan.Items) == 0 {
		return fmt.Errorf("更新计划不完整")
	}
	unlock, err := acquireUpdateLock(plan.StateDir, now)
	if err != nil {
		return err
	}
	defer unlock()

	ctl := opts.Control
	if ctl == nil {
		ctl = platformServiceControl{}
	}
	health := opts.Health
	if health == nil {
		health = probeUpdatedAgent
	}
	timeout := opts.ReplaceTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	result := UpdateResult{From: plan.From, Version: plan.Version}
	finish := func(ok bool, reason string) {
		result.OK, result.Reason, result.At = ok, reason, now().UTC()
		if out, err := json.MarshalIndent(result, "", "  "); err == nil {
			_ = writeFileAtomic(updateResultPath(plan.StateDir), out, 0o600)
		}
	}

	logf("[更新] %s -> %s 开始：停止服务", plan.From, plan.Version)
	if err := ctl.Stop(plan.Items); err != nil {
		// 停不下来就不换：文件还在被占用，硬换只会换出半套。
		logf("[更新] 停止服务失败，放弃本次更新: %v", err)
		_ = ctl.Start(plan.Items)
		finish(false, "停止服务失败: "+err.Error())
		return err
	}

	var replaced []UpdateItem
	rollback := func(reason string) error {
		logf("[更新] %s，回滚到 %s", reason, plan.From)
		_ = ctl.Stop(plan.Items)
		var errs []string
		for _, it := range replaced {
			if err := restoreOld(it.Target, timeout); err != nil {
				errs = append(errs, err.Error())
			}
		}
		if err := ctl.Start(plan.Items); err != nil {
			errs = append(errs, "启动旧版本失败: "+err.Error())
		}
		if len(errs) > 0 {
			reason += "；回滚时出错: " + strings.Join(errs, "; ")
		}
		finish(false, reason)
		return errors.New(reason)
	}

	for _, it := range plan.Items {
		if err := replaceFile(it.Target, it.Staged, timeout); err != nil {
			return rollback(fmt.Sprintf("替换 %s 失败: %v", it.Target, err))
		}
		replaced = append(replaced, it)
		logf("[更新] 已替换 %s", it.Target)
	}

	if err := ctl.Start(plan.Items); err != nil {
		return rollback("启动新版本失败: " + err.Error())
	}
	if err := health(ctx, &plan); err != nil {
		return rollback("新版本没有正常起来: " + err.Error())
	}

	for _, it := range replaced {
		_ = os.Remove(it.Target + ".old")
	}
	_ = os.RemoveAll(filepath.Join(UpdateDir(plan.StateDir), "staged"))
	finish(true, "")
	logf("[更新] 完成：%s -> %s", plan.From, plan.Version)
	return nil
}

// replaceFile 把 staged 换到 target，旧文件留在 target.old 供回滚。
//
// 先改名再放新文件：Windows 允许给运行中的 exe 改名，但不允许覆盖或删除它；
// Unix 上改名对正在运行的进程也没有影响。
func replaceFile(target, staged string, timeout time.Duration) error {
	old := target + ".old"
	if err := retryFor(timeout, func() error {
		if err := os.Remove(old); err != nil && !os.IsNotExist(err) {
			return err
		}
		return os.Rename(target, old)
	}); err != nil {
		return err
	}
	tmp := target + ".new"
	if err := copyStaged(staged, tmp); err != nil {
		_ = os.Rename(old, target)
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		_ = os.Rename(old, target)
		return err
	}
	return nil
}

// restoreOld 用 target.old 换回 target。
func restoreOld(target string, timeout time.Duration) error {
	old := target + ".old"
	if _, err := os.Stat(old); err != nil {
		return fmt.Errorf("找不到 %s", old)
	}
	return retryFor(timeout, func() error {
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			// 新文件还被占着：挪开它，旧文件才能放回原名。
			if err2 := os.Rename(target, target+".bad"); err2 != nil {
				return err
			}
		}
		return os.Rename(old, target)
	})
}

func copyStaged(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	return os.Chmod(dst, 0o755)
}

func retryFor(timeout time.Duration, fn func() error) error {
	deadline := time.Now().Add(timeout)
	for {
		err := fn()
		if err == nil || time.Now().After(deadline) {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// acquireUpdateLock 保证同一时刻只有一个更新进程在换文件。
func acquireUpdateLock(stateDir string, now func() time.Time) (func(), error) {
	p := updateLockPath(stateDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			fmt.Fprintf(f, "%d %s\n", os.Getpid(), now().UTC().Format(time.RFC3339))
			f.Close()
			return func() { os.Remove(p) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		st, serr := os.Stat(p)
		if serr != nil || now().Sub(st.ModTime()) < updateLockStale {
			return nil, ErrUpdateBusy
		}
		// 过期的锁：上一个更新进程大概率已经崩溃。
		os.Remove(p)
	}
	return nil, ErrUpdateBusy
}

// probeUpdatedAgent 确认新版本真的起来了：先直接跑一次 version（架构不对、
// 文件损坏会在这里暴露），再等本机页面报出新版本号。
func probeUpdatedAgent(ctx context.Context, plan *UpdatePlan) error {
	var agentBin string
	for _, it := range plan.Items {
		if it.Kind == "agent" {
			agentBin = it.Target
		}
	}
	if agentBin == "" {
		return fmt.Errorf("计划里没有代理")
	}
	vctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := commandOutput(vctx, agentBin, "version")
	if err != nil {
		return fmt.Errorf("执行 %s version 失败: %v", agentBin, err)
	}
	if !strings.Contains(out, plan.Version) {
		return fmt.Errorf("新文件报告的版本不是 %s: %s", plan.Version, strings.TrimSpace(out))
	}

	deadline := time.Now().Add(updateHealthTimeout)
	client := &http.Client{Timeout: 3 * time.Second}
	var last string
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if addr, err := os.ReadFile(filepath.Join(plan.StateDir, AddrFileName)); err == nil {
			url := "http://" + strings.TrimSpace(string(addr)) + "/api/status"
			if resp, err := client.Get(url); err == nil {
				var st struct {
					Version string `json:"version"`
				}
				_ = json.NewDecoder(resp.Body).Decode(&st)
				resp.Body.Close()
				if st.Version == plan.Version {
					return nil
				}
				last = "本机页面报告版本 " + st.Version
			} else {
				last = err.Error()
			}
		}
		time.Sleep(time.Second)
	}
	if last == "" {
		last = "没有读到监听地址"
	}
	return fmt.Errorf("%s 内没有等到新版本：%s", updateHealthTimeout, last)
}

// AutoUpdateEligible 判断本进程是否应当自动更新，返回不更新的原因。
//
// 只有「以已注册服务身份运行的默认实例」才更新：前台调试、隔离状态目录、
// 本地构建都不该被线上版本覆盖，更新进程也没有办法把它们重新拉起来。
func AutoUpdateEligible(stateDir, version string) (bool, string) {
	if os.Getenv("DEC_AUTO_UPDATE") == "0" {
		return false, "DEC_AUTO_UPDATE=0"
	}
	if _, ok := release.ParseVersion(version); !ok {
		return false, "本地构建版本 " + version
	}
	if !IsPrimaryStateDir(stateDir) {
		return false, "非默认状态目录"
	}
	exe, err := os.Executable()
	if err != nil {
		return false, err.Error()
	}
	real := exe
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		real = r
	}
	if !serviceRunsBinary(exe, real) {
		return false, "当前进程不是已注册服务所指向的可执行文件"
	}
	return true, ""
}
