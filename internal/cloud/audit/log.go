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
	"sync/atomic"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/sysproc"
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
	// Count 表示这条记录代表了多少次同类事件。
	//
	// 为 0 或 1 时是普通记录；大于 1 说明它是聚合后的汇总（见 aggregate.go）。
	// 用 omitempty 保证绝大多数记录不会多出一个字段，
	// 同时在读日志的人眼里一眼能看出"这条是汇总"。
	Count int `json:"count,omitempty"`
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
	// commitWindow 是提交合并窗口，默认 CommitWindow。
	//
	// 做成字段而非直接用常量：这个值直接决定仓库体积——git 存的是文件快照，
	// 提交越密，同一个小时文件被重复存越多份。它需要能按实测校准
	// （见 TestMeasureAnnualGrowth），也需要测试能调小以免等待。
	commitWindow time.Duration
	// curHour 是当前正在追加的小时分片标识（形如 2006-01-02T15）。
	//
	// 它只用于判断"这条记录有没有跨到新的一个小时文件"——跨了就提交一次，
	// 让上一小时定稿进历史，避免同一个文件被反复快照（见 CommitWindow）。
	curHour string
	// access 是传输与访问类事件的聚合器（ADR 0043）。
	//
	// 只有这类事件会被聚合：它们描述"同一份东西又送了一次"，
	// 频率由网络和 bug 决定，不由业务决定。
	access *AccessAggregator
	// dropped 统计因落盘失败而未写入的条数，供管理面观察。
	//
	// 审计写失败不阻断业务（见 Server.record 的取舍），所以这些记录确实丢了；
	// 记下数量至少能让"审计有洞"这件事被看见，而不是静默。
	dropped atomic.Int64
	// gcRunning 表示此刻有 gc 在跑，期间不提交。
	//
	// gc 会删掉旧 pack，而提交要读 HEAD tree 里的对象；两者撞在一起时，
	// 提交会用到一个正在被删除的 pack（go-git 的索引缓存不会自己更新）。
	// 记录本身仍然逐条 fsync 到工作树文件，只是攒着不 commit，
	// 等 gc 结束换新句柄后由下一次提交一起带上，没有数据损失。
	gcRunning bool
}

// CommitWindow 是未提交记录的最大滞留时长，用于兜底。
//
// 主策略不是时间窗，而是"按小时收口"（见 writeEntry）：审计按小时分片，
// 一个小时文件在跨入下一小时时提交一次，此后就不再变化，因此只会被 git
// 快照一次。这才是符合"文件快照"这一存储特性的粒度。
//
// 为什么不能用"每 N 秒提交一次"：记录间隔通常大于任何小窗口，
// 于是每条记录都会各自触发一次提交，同一个小时文件被反复快照。
// 2026-10-24 实测（TestMeasureAnnualGrowth）：
//
//	每小时提交 1440 次 → 体积是数据的 4.1 倍，每年 354 MB
//	每小时提交 2 次   → 1.2 倍，每年 104 MB
//	每小时提交 1 次   → 1.1 倍，每年 96 MB
//
// 线上也能直接看到：events/2026-10/2026-10-23T20.hour.jsonl 被提交了 594 次，
// 而它当前只有 660 KB。2026-10-23 那次 5.7G 的 .git 正是这样堆出来的。
//
// 这个常量留作兜底：万一因为时钟异常或"小时内持续有写入"导致一直没跨小时，
// 也不至于无限期不提交。取 2 小时，比一个分片周期长，正常路径永远碰不到它。
const CommitWindow = 2 * time.Hour

// CommitBatch 是触发提交的条数阈值。
//
// 与"按小时收口"的关系：主策略管"同一小时文件不要反复快照"，
// 这一条管"别让单次提交太大、别让未提交内容堆积太久"。
// 高峰时段（例如一次故障刷出几千条）会按批提交，不必等满一小时。
const CommitBatch = 200

// commitBatchForTest 让实验能单独隔离"按小时收口"这一策略。
//
// 生产代码始终读 CommitBatch；只有 measure_growth_test.go 会改它。
// 用一个包级变量而不是给 Log 加字段：这个值不该成为运行时配置，
// 它只是为了让实验能排除兜底路径的干扰。
var commitBatchForTest = CommitBatch

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
	l := &Log{dir: dir, repo: repo, access: NewAccessAggregator(), commitWindow: CommitWindow}
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
//
// 句柄会在 gc 之后被替换（见 reopenRepo），所以这里必须持锁读取，
// 否则可能拿到正在被替换的指针。
func (l *Log) Repo() *git.Repository {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.repo
}

// Append 追加一条记录。
//
// 是否落盘由事件级别决定（见 tier.go、ADR 0043）：
//
//   - 安全级与事实级：无条件逐条写入。这两级的频率由人的操作和业务提交决定，
//     天然有界，任何限额都不作用于它们。
//   - 访问级：先过聚合器，同一 (事件, 设备, 来源, 对象) 每小时最多写
//     MaxAccessRecordsPerHour 条，超出的累计成一条带 count 的汇总。
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

	if e.Event.Tier() == TierAccess {
		decision := l.access.Offer(e)
		// 先把上一小时积压的汇总补上：它们代表已经发生、还没落盘的事实，
		// 丢掉就无法还原"被聚合了多少条"。
		for _, sum := range decision.Summaries {
			l.writeEntry(sum)
		}
		if !decision.Emit {
			return nil
		}
	}
	return l.writeEntry(e)
}

// writeEntry 把一条记录写进工作树文件并 fsync。调用方需持有 l.mu。
//
// 失败只累计 dropped 计数、不向上传播：审计是旁路，不能因为磁盘满
// 就让用户的写入操作失败（那会造成"数据没存上，但你以为存上了"）。
// 这与 Server.record 的取舍一致，只是把"静默丢"变成"可数的丢"。
func (l *Log) writeEntry(e Entry) error {
	hour := e.At.Format("2006-01-02T15")

	// 按小时收口：先判断这条记录有没有跨进新的一个小时文件。
	// 跨了就把上一小时提交掉——它此刻是完整定稿的，这一份快照之后不会再变。
	//
	// 为什么要"先提交再写"而不是"先写再提交"：如果先写，新小时文件里已经有
	// 一条记录了，这次提交就会把它一并快照进去；等下个小时收口时它又会被
	// 完整快照一次。每份文件都被存两遍，白白放大一倍。
	//
	// 为什么不在"当前小时还在增长时"提交：git 存的是文件快照，
	// 同一个文件每提交一次就多存一份完整副本。实测（见 CommitWindow 注释）：
	// 每小时提交 1440 次时仓库是数据的 4.1 倍、每年 354 MB；
	// 每小时提交一次时是 1.1 倍、每年 96 MB。
	if l.curHour != "" && l.curHour != hour {
		// 提交失败不阻断写入：记录还没写，这里失败只是"上一小时晚点进历史"，
		// 内容仍在工作树里，下一次收口会一起带上。
		_ = l.commitPending()
	}
	l.curHour = hour

	line, err := json.Marshal(e)
	if err != nil {
		l.dropped.Add(1)
		return fmt.Errorf("序列化审计记录失败: %w", err)
	}
	line = append(line, '\n')

	rel := filepath.Join("events", e.At.Format("2006-01"), hour+".hour.jsonl")
	full := filepath.Join(l.dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		l.dropped.Add(1)
		return fmt.Errorf("创建审计子目录失败: %w", err)
	}
	f, err := os.OpenFile(full, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		l.dropped.Add(1)
		return fmt.Errorf("打开审计文件失败: %w", err)
	}
	if _, err := f.Write(line); err != nil {
		f.Close()
		l.dropped.Add(1)
		return fmt.Errorf("写入审计记录失败: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		l.dropped.Add(1)
		return fmt.Errorf("同步审计文件失败: %w", err)
	}
	if err := f.Close(); err != nil {
		l.dropped.Add(1)
		return err
	}

	l.pending++

	// 兜底两点：批量到阈值就提交（避免高峰时段攒太多），
	// 以及长时间没跨小时时也给一个上界（时钟异常、只写同一小时等）。
	if l.pending < commitBatchForTest && time.Since(l.lastCommit) < l.commitWindow {
		return nil
	}
	return l.commitPending()
}

// Dropped 返回因写入失败而丢失的记录数。
//
// 这些记录确实不在审计里了。暴露出来是为了让管理面能显示"审计有洞"，
// 而不是让丢失静默发生。
func (l *Log) Dropped() int64 { return l.dropped.Load() }

// Suppressed 返回当前所有尚未落盘的聚合计数，供关停前落盘。
//
// 服务退出前必须调用 Flush，它会先把这个计数落盘再提交；
// 否则最后一次汇总会随进程一起消失。
func (l *Log) Suppressed() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.access.Take()
}

// Flush 立即提交待提交的记录。
//
// 只做 git 提交，不结算聚合计数。原因是 Flush 会被周期性调用（冷备推送前、
// 部署前），如果它每次都把 suppressed 结算成一条汇总，那么在"每写一条就 Flush"
// 的场景下，每条被抑制的记录都会变成一条 count=1 的汇总记录重新写回文件——
// 聚合等于没做（2026-10-24 的测试实测：200 次下发仍落盘 200 条）。
//
// 聚合计数有两个正确的结算时机：小时切换（Offer 内部自动结算上一小时）
// 和进程退出（Close）。两者之间不需要中间的落盘点。
func (l *Log) Flush() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.commitPending()
}

// Close 是进程退出前的收尾：把本小时的聚合计数结算成汇总，再提交。
//
// 这是唯一需要显式结算聚合的时机：进程一退出，内存里的 suppressed 就没了，
// 不落盘的话"这段时间有多少条被聚合"会永久丢失，量级信号也就断了。
func (l *Log) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, sum := range l.access.Take() {
		l.writeEntry(sum)
	}
	return l.commitPending()
}

// commitPending 把工作树里累积的改动提交一次。
//
// 调用方需持有 l.mu（Open 里是单线程，无需加锁）。
func (l *Log) commitPending() error {
	// gc 正在重写对象库：此刻提交会用到一个正在被删除的 pack。
	// 记录已经在工作树里（已 fsync），攒到 gc 结束后的下一次提交即可。
	if l.gcRunning {
		return nil
	}
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
//
// 按小时分片后，一天的数据散在多个文件里，这里合并返回；
// 同时兼容 2026-10-24 之前的按天文件（events/YYYY-MM/YYYY-MM-DD.jsonl）。
func (l *Log) Read(day time.Time) ([]Entry, error) {
	dir := filepath.Join(l.dir, "events", day.Format("2006-01"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	dayPrefix := day.UTC().Format("2006-01-02")
	// 收集要读的文件：当天的小时分片，以及可能存在的旧按天文件。
	var files []string
	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		if name == dayPrefix+".jsonl" || strings.HasPrefix(name, dayPrefix+"T") {
			files = append(files, filepath.Join(dir, name))
		}
	}
	// 文件名按小时字典序排列，天然就是时间顺序。
	sort.Strings(files)

	var out []Entry
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
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

// gcMinLooseObjects 是触发 gc 的松散对象数阈值。
//
// 低于这个数不值得动：gc 本身要重写整个 packfile，几百个松散对象的回收收益
// 抵不上它的开销。2000 是个折中：按每小时一个提交、每个提交若干对象估算，
// 大约对应几天到几周的积累，既不会让松散对象长期堆着，也不会每天白跑。
const gcMinLooseObjects = 2000

// Gc 回收松散对象，控制仓库体积。
//
// 为什么需要它：审计是只追加的，定期 commit 会产生大量松散对象。
// 松散对象本身很小，但它们不会自动合并——git 的自动 gc 阈值（默认 6700）
// 在高频提交下可能长期达不到，也可能在某个时刻突然被触发并长时间占用磁盘。
// 主动按天做一次，代价可预期。
//
// 为什么用外部 git 命令而不是 go-git：读源码可知，go-git 的 RepackObjects
// 只做"把已有 pack 重打一遍再删旧 pack"，完全不碰松散对象；而它的
// packfile 编码器需要自己往 .git/objects/pack 里写头部与索引，
// 拼错就会写坏仓库。gc 是低频维护动作，调用系统 git 反而更可靠，
// 代价只是部署机需要装 git（实际必然有，CI 与服务器都在用）。
//
// 失败只返回错误、不 panic：gc 是维护动作，不该影响审计写入。
func (l *Log) Gc() error { return l.gcAt(gcMinLooseObjects) }

// gcAt 在松散对象数达到 min 时执行一次回收。
//
// min 抽成参数只为可测：验证「达到阈值就会真的收」不该要求测试造 2000 个对象，
// 那既慢又依赖环境。生产只通过 Gc 调用，阈值仍是 gcMinLooseObjects。
func (l *Log) gcAt(min int) error {
	// 只在锁内做「提交」与「数对象」这两件必须串行的事（都要读工作树与索引）。
	if err := l.Flush(); err != nil {
		return err
	}
	count, err := l.LooseObjects()
	if err != nil {
		return err
	}
	// 太少就不做：打包有固定开销，几百个对象的回收收益抵不上。
	if count < min {
		return nil
	}
	return l.gcUnlocked()
}

// gcUnlocked 执行一次回收。调用方不得持有 l.mu。
//
// 为什么不能在锁内跑：git gc 要把整个对象库重新打包，仓库越大越慢。
// 而 l.mu 是所有审计写入（也就所有业务写入）的必经之路——在 1G 内存的机器上
// 让一次 gc 压着这把锁，等于给整个服务加一次时长不可控的停顿。
//
// 并发安全性：git gc 只动 .git，不碰工作树；它自身用 gc.pid 互斥，
// 与正常 commit 并发是 git 支持的场景。所以放开锁是安全的。
// 另外这里刻意不用 --prune=now：宽限期是 git 对「刚创建、尚未被引用」的
// 对象（正在写入的 tree/blob）的保护，取消它去多回收几个字节不值得冒这个险。
// 审计只追加，本来也没什么不可达对象。
func (l *Log) gcUnlocked() error {
	// 标记 gc 进行中，让并发的 commit 让路（见 Log.gcRunning）。
	// 只置标记、不持锁：持锁就回到了"gc 压着写入"的老问题。
	l.mu.Lock()
	l.gcRunning = true
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		l.gcRunning = false
		l.mu.Unlock()
	}()

	// 经 sysproc 创建子进程：dec-cloud 以脱离控制台的方式常驻，
	// 直接用 os/exec 在 Windows 上会弹出控制台窗口（见 sysproc 包注释）。
	cmd := sysproc.Command("git", "-c", "gc.auto=0", "gc", "--quiet")
	cmd.Dir = l.dir
	// 显式给身份，避免在没有全局配置的环境里因缺 user.name 失败。
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=dec-cloud", "GIT_AUTHOR_EMAIL=cloud@dec.local",
		"GIT_COMMITTER_NAME=dec-cloud", "GIT_COMMITTER_EMAIL=cloud@dec.local",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("回收审计仓库对象失败: %w（输出: %s）", err, strings.TrimSpace(string(out)))
	}
	// gc 删掉了旧 pack，必须换掉手里的仓库句柄（原因见 reopenRepo）。
	return l.reopenRepo()
}

// reopenRepo 重新打开仓库，丢弃旧的 pack 索引缓存。
//
// 这是个真实踩到的坑，不是保险措施：go-git 的 ObjectStorage 在首次访问时把
// pack 列表与索引缓存进内存（requireIndex 只在 index 为 nil 时才重建），
// 而 git gc 会把旧 pack 整个删掉、写成新 pack。Log 持有的句柄是进程级长寿命的，
// gc 之后它缓存里指向的 pack 文件已经不存在，下一次 commit 读 HEAD tree 就会
// 报 "object not found"。
//
// 更糟的是这条失败会被静默吞掉：commitPending 的错误经 writeEntry 归入
// dropped 计数，业务照常返回成功，而审计从此再也不会前进。
// TestGcKeepsRepositoryUsable 就是对着这个场景写的回归测试。
//
// go-git 有针对这种情况的 Reindex()，但它只清 index、不清 packList 与
// packfile 句柄（见 ObjectStorage 的字段），仍然可能读到已删除的 pack。
// 重新 PlainOpen 一次最彻底，代价只在每天一次 gc 之后发生一次。
func (l *Log) reopenRepo() error {
	repo, err := git.PlainOpen(l.dir)
	if err != nil {
		return fmt.Errorf("gc 后重新打开审计仓库失败: %w", err)
	}
	l.mu.Lock()
	l.repo = repo
	// 换好句柄、清掉 gc 标记之后补一次提交：gc 期间攒下的记录
	// 一直在工作树里（已 fsync），此时才第一次有机会进历史。
	l.gcRunning = false
	err = l.commitPending()
	l.mu.Unlock()
	return err
}

// looseObjectCount 统计 objects 目录下的松散对象文件数量。
//
// 松散对象存放在 objects/<两位十六进制>/<38 位十六进制> 里，
// info 与 pack 是特殊目录，不算对象。
func looseObjectCount(objectsDir string) (int, error) {
	entries, err := os.ReadDir(objectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("读取对象目录失败: %w", err)
	}
	total := 0
	for _, de := range entries {
		if !de.IsDir() {
			continue
		}
		name := de.Name()
		if name == "info" || name == "pack" || len(name) != 2 {
			continue
		}
		files, err := os.ReadDir(filepath.Join(objectsDir, name))
		if err != nil {
			continue
		}
		total += len(files)
	}
	return total, nil
}

// LooseObjects 返回当前松散对象数量，供管理面与测试观察仓库健康度。
func (l *Log) LooseObjects() (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return looseObjectCount(filepath.Join(l.dir, ".git", "objects"))
}

// Author 构造审计用的作者信息。
func Author(source string) ssot.Author {
	if source == "" {
		source = "cloud"
	}
	return ssot.Author{Name: "dec-cloud", Email: "cloud@dec.local", Source: source}
}
