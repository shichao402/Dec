// Package audit 实现只追加的审计日志（需求 15.3、18.11、22.3）。
//
// 审计与业务数据的区别：
//
//   - 业务数据是“现在是什么样”，允许被回滚改写。
//   - 审计是“发生过什么”，只能追加。所以它不进 SSOT 仓库，而是自己一个仓库。
//
// 每条记录是一个 JSON Lines 行，按日期分文件。这样即使某天写入中断，
// 也只影响那一天的文件尾部，不会让整份审计不可读。
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/shichao402/Dec/internal/cloud/ssot"
)

// Event 是审计事件的类型。
//
// 取值刻意用短字符串而不是数字：审计日志需要被人直接读，
// 数字编码会让“事后取证”多一层翻译。
type Event string

const (
	// EventAssetWrite 资产写入。
	EventAssetWrite Event = "asset.write"
	// EventAssetDelete 资产删除。
	EventAssetDelete Event = "asset.delete"
	// EventSecretWrite 密钥写入。
	EventSecretWrite Event = "secret.write"
	// EventSecretDelete 密钥删除。
	EventSecretDelete Event = "secret.delete"
	// EventSecretRead 密钥明文查看。
	EventSecretRead Event = "secret.read"
	// EventSecretDeliver 密钥随下发离开服务端。
	EventSecretDeliver Event = "secret.deliver"
	// EventSync 一次下发。
	EventSync Event = "sync"
	// EventOverride 检测到本地改动被覆盖。
	EventOverride Event = "override"
	// EventEnroll 设备登记尝试（成功与失败都记）。
	EventEnroll Event = "device.enroll"
	// EventRecover 恢复模式下的取回。
	EventRecover Event = "device.recover"
	// EventRevoke 设备吊销。
	EventRevoke Event = "device.revoke"
	// EventDeviceKind 设备类型变更。
	EventDeviceKind Event = "device.kind"
	// EventProject 项目登记与订阅变更。
	EventProject Event = "project.change"
	// EventRootKeyLoad 根密钥加载。
	EventRootKeyLoad Event = "rootkey.load"
	// EventRootKeyRotate 根密钥轮换。
	EventRootKeyRotate Event = "rootkey.rotate"
	// EventBootstrap 初始化。
	EventBootstrap Event = "bootstrap"
	// EventDeploy 部署。
	EventDeploy Event = "deploy"
	// EventDownload 客户端下载。
	EventDownload Event = "download"
	// EventBackup 冷备与审计推送结果。
	EventBackup Event = "backup"
	// EventRollback 回滚。
	EventRollback Event = "rollback"
)

// Entry 是一条审计记录。
type Entry struct {
	// At 是事件时间（UTC）。
	At time.Time `json:"at"`
	// Event 是事件类型。
	Event Event `json:"event"`
	// DeviceID 是发起方设备；无设备身份时为空。
	DeviceID string `json:"device_id,omitempty"`
	// Source 是来源入口（agent、mcp、admin、hostcli、enroll）。
	//
	// 需求 18.11 要求写明设备 ID 与来源入口，这两项是审计的基本定位信息。
	Source string `json:"source,omitempty"`
	// Subject 是操作对象，例如 bundle 名、密钥名、设备 ID。
	Subject string `json:"subject,omitempty"`
	// Detail 是补充说明。不放密钥明文、不放密码。
	Detail string `json:"detail,omitempty"`
	// OK 表示操作是否成功。
	OK bool `json:"ok"`
	// RemoteIP 是请求来源地址，便于事后判断是不是同一处发起。
	RemoteIP string `json:"remote_ip,omitempty"`
}

// Log 是审计日志。
//
// 写入分两步：先追加到本地工作树文件，再提交到审计仓库。
// 两步都在同一把锁里，保证 commit 里的内容与追加顺序一致。
type Log struct {
	dir  string
	repo *git.Repository

	mu sync.Mutex
	// pending 累计尚未提交的条数，用于合并提交。
	pending int
	// lastCommit 是上一次提交的时间，用于按时间窗合并。
	lastCommit time.Time
}

// CommitWindow 是审计提交的合并窗口。
//
// 来历（2026-10-23 生产故障）：最初每条记录都单独 commit，理由是"即使下一秒
// 机器被砸了也已经记下来了"。这个理由本身没错，但它把 commit 当成了持久化手段，
// 而持久化其实在图前一步——记录先 fsync 到工作树文件才提交。真正的代价是仓库
// 体积：一次 MCP 会话就能产生几万条记录，76,350 个 commit 把 .git 撑到 5.7G，
// 占满 40G 系统盘后机器卡死、SSH 与 HTTPS 全部无响应。
//
// 所以改成按时间窗合并提交：记录仍然逐条 fsync（进程崩溃不丢数据），
// 提交攒够一个窗口或攒够 CommitBatch 条再做一次。审计的可读性不受影响——
// 记录本身带秒级时间戳，不依赖 commit 分隔。
const CommitWindow = 5 * time.Second

// CommitBatch 是触发提交的条数阈值。
//
// 高流量时不必等满一个窗口：条数到了就提交，避免积压太多未提交内容
// 让"最近发生了什么"在 git 历史里看不到。
const CommitBatch = 200

// Open 打开或初始化审计仓库。
func Open(dir string) (*Log, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("创建审计目录失败: %w", err)
	}
	repo, err := git.PlainOpen(dir)
	if err != nil {
		// 与 SSOT 共用同一个初始化函数：两边分支名必须一致，
		// 否则审计仓库会推到一个远端不存在的分支上（见 ssot.InitRepo 的注释）。
		repo, err = ssot.InitRepo(dir)
		if err != nil {
			return nil, fmt.Errorf("初始化审计仓库失败: %w", err)
		}
	}
	l := &Log{dir: dir, repo: repo}
	// 启动时先清掉上次进程留下的未提交改动：它们已经在工作树里，
	// 补一次提交即可，不必等新记录来触发。
	if err := l.commitPending(); err != nil {
		return nil, err
	}
	return l, nil
}

// Dir 返回审计仓库路径。
func (l *Log) Dir() string { return l.dir }

// Repo 返回底层仓库，供冷备推送使用。
func (l *Log) Repo() *git.Repository { return l.repo }

// Append 追加一条记录。
//
// 记录本身逐条 fsync 到工作树文件后就返回，不在这里做 commit：
// 落盘（fsync）才是"机器被砸也不丢"的保证，commit 只是把改动记进历史。
// 提交按 CommitWindow / CommitBatch 合并，避免每条一个 commit 把仓库撑爆
// （见 CommitWindow 的注释里那次生产故障）。
func (l *Log) Append(e Entry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	e.At = e.At.UTC().Truncate(time.Second)
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("序列化审计记录失败: %w", err)
	}
	line = append(line, '\n')

	rel := filepath.Join("events", e.At.Format("2006-01"), e.At.Format("2006-01-02")+".jsonl")
	full := filepath.Join(l.dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return fmt.Errorf("创建审计子目录失败: %w", err)
	}
	f, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("打开审计文件失败: %w", err)
	}
	if _, err := f.Write(line); err != nil {
		f.Close()
		return fmt.Errorf("写入审计记录失败: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("同步审计文件失败: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}

	l.pending++
	// 窗口未到、条数也没攒够，就先不提交。
	if l.pending < CommitBatch && time.Since(l.lastCommit) < CommitWindow {
		return nil
	}
	return l.commitPending()
}

// Flush 立即提交待提交的记录。
//
// 冷备推送前必须调用：推送只看到已提交的内容，窗口期内的记录还在工作树里，
// 直接推会漏。平时不必显式调用，Append 会按窗口自己提交。
func (l *Log) Flush() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.commitPending()
}

// commitPending 把工作树里累积的改动提交一次。
//
// 调用方需持有 l.mu（Open 里是单线程，无需加锁）。
func (l *Log) commitPending() error {
	wt, err := l.repo.Worktree()
	if err != nil {
		return fmt.Errorf("读取审计工作树失败: %w", err)
	}
	status, err := wt.Status()
	if err != nil {
		return fmt.Errorf("读取审计状态失败: %w", err)
	}
	if status.IsClean() {
		// 没有可提交的改动（例如刚启动、上次已经提交过）。
		l.pending = 0
		l.lastCommit = time.Now()
		return nil
	}
	// 按状态里实际变化的文件逐个加入：合并提交时无法预先知道这期间动过哪几个
	// 按日期分文件，交给状态自己算。AddGlob 在目录尚不存在时会报
	// "glob pattern did not match any files"，不适合这里。
	set := map[string]struct{}{}
	for path := range status {
		set[path] = struct{}{}
	}
	for path := range set {
		if _, err := wt.Add(path); err != nil {
			return fmt.Errorf("暂存审计文件 %s 失败: %w", path, err)
		}
	}
	n := l.pending
	if n < 1 {
		n = 1
	}
	_, err = wt.Commit(fmt.Sprintf("审计 %d 条", n), &git.CommitOptions{
		Author: &object.Signature{
			Name:  "dec-cloud",
			Email: "cloud@dec.local",
			When:  time.Now().UTC(),
		},
	})
	if err != nil {
		// 内容相同（例如同一秒同一事件）时没有可提交的变更，不算失败。
		if strings.Contains(err.Error(), "clean") {
			l.pending = 0
			l.lastCommit = time.Now()
			return nil
		}
		return fmt.Errorf("提交审计记录失败: %w", err)
	}
	l.pending = 0
	l.lastCommit = time.Now()
	return nil
}

// AppendCommit 把审计提交与业务 commit 绑定。
//
// 审计记录里带业务 revision，才能在事后把“谁改了什么”对应到具体内容。
func (l *Log) AppendCommit(e Entry, revision string) error {
	e.Detail = joinDetail(e.Detail, "revision="+revision)
	return l.Append(e)
}

func joinDetail(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + " " + b
}

// Read 读取某一天的审计记录。
//
// 只用于测试与排查，不对外暴露：审计是给人看的档案，不是查询接口。
func (l *Log) Read(day time.Time) ([]Entry, error) {
	rel := filepath.Join("events", day.Format("2006-01"), day.Format("2006-01-02")+".jsonl")
	data, err := os.ReadFile(filepath.Join(l.dir, rel))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Entry
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("解析审计记录失败: %w", err)
		}
		out = append(out, e)
	}
	return out, nil
}

// Files 返回审计仓库里的全部文件，供冷备推送前做完整性检查。
func (l *Log) Files() ([]string, error) {
	head, err := l.repo.Head()
	if err != nil {
		return nil, err
	}
	commit, err := l.repo.CommitObject(head.Hash())
	if err != nil {
		return nil, err
	}
	tree, err := commit.Tree()
	if err != nil {
		return nil, err
	}
	var out []string
	err = tree.Files().ForEach(func(f *object.File) error {
		out = append(out, f.Name)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// Head 返回审计仓库当前 revision。
func (l *Log) Head() string {
	ref, err := l.repo.Head()
	if err != nil {
		return ""
	}
	return ref.Hash().String()
}

// Author 构造审计用的作者信息。
func Author(source string) ssot.Author {
	if source == "" {
		source = "cloud"
	}
	return ssot.Author{Name: "dec-cloud", Email: "cloud@dec.local", Source: source}
}
