package audit

import (
	"fmt"
	"sync"
	"time"
)

// 传输与访问类事件的聚合与限额（ADR 0043）。
//
// 为什么要聚合而不是简单丢弃：把一万条"同一台设备又收到同一份内容"合并成一条
// count=10000，既保住了"这件事发生了 1 万次"这个量级信号，又不让文件无限增长。
// 直接丢弃会把失控能力和常态混为一谈，事后无法区分"正常重连"和"对账 bug"。
//
// 聚合键刻意不选内容指纹：内容指纹每次变化都不同，用它做键等于没有降噪；
// 而按小时分桶，天然与审计文件的分片对齐，一条聚合记录只落在一个小时文件里。

// MaxAccessRecordsPerHour 是每小时每个聚合键允许产生的原始记录数上限。
//
// 取证价值与体积的平衡点：正常情况下同一 (设备, 来源, 对象) 一小时内下发次数
// 不会超过个位数（长轮询超时、代理重启各算一次）。给到 20 已经很宽松，
// 同时把最坏情况从"每秒一条"压到"每小时 20 条"。
const MaxAccessRecordsPerHour = 20

// MaxAccessRecordsPerHourTotal 是每小时所有聚合键合计的原始记录上限。
//
// 为什么单键限额不够：下载端点是公网匿名接口，聚合键里带 IP，
// 而公网 IP 可以随意轮换。只要不断换 IP，单键限额就永远碰不到，
// 总量仍然无界。所以必须再压一层全局上限，把"任何情况下"都封死。
//
// 400 这个数字取自量级判断：正常使用（一台设备一天几十次下发、
// 偶尔从新机器下载一次客户端）远低于它；而它对应的小时文件只有几十 KB，
// 一年也就几百兆，单用户场景不可能占到需要担心的比例。
const MaxAccessRecordsPerHourTotal = 400

// MaxAccessBuckets 是聚合键数量上限。
//
// 防止"请求方用海量不同 IP 撑爆内存里的桶表"——桶表本身也是一份状态，
// 无界增长同样是设计缺陷。超出后新键统一折进溢出桶，
// 只保留总量信号，不再逐键区分。
const MaxAccessBuckets = 1024

// overflowKey 是超出桶上限后所有新键折进来的桶。
const overflowKey = "__overflow__"

// AccessAggregator 按聚合键统计 access 级事件，决定这一次该不该落盘。
//
// 同一聚合键在同一小时内最多放行 MaxAccessRecordsPerHour 条，超出的部分累计成
// count，在小时切换或关停时作为一条汇总记录补上。
//
// 这样做的结果是：无论上游多失控，单小时单键的记录数都是常数级；而一旦超过阈值，
// 汇总记录里的 count 会明显偏大，正好成为"这里有问题"的信号。
type AccessAggregator struct {
	mu sync.Mutex
	// buckets 的键是聚合键，值是该键在当前小时的累计状态。
	buckets map[string]*accessBucket
	// total 是本小时全部放行数，用于全局上限。
	total accessTotal
	// now 用于测试注入时间。
	now func() time.Time
}

type accessBucket struct {
	// hour 是这个桶所属的小时（UTC，截断到小时）。
	hour time.Time
	// emitted 是本小时已经放行的条数。
	emitted int
	// suppressed 是本小时被聚合掉的条数。
	suppressed int
	// lastAt 是本小时最后一条原始记录的时间，用作汇总记录的时间戳。
	lastAt time.Time
	// sample 保留第一条记录的模板，汇总记录沿用它补齐必要字段。
	sample Entry
}

// accessTotal 记录某个小时的全部放行数，用于全局上限。
type accessTotal struct {
	hour    time.Time
	emitted int
}

// NewAccessAggregator 构造聚合器。
func NewAccessAggregator() *AccessAggregator {
	return &AccessAggregator{buckets: map[string]*accessBucket{}, now: time.Now}
}

// AccessDecision 是一次聚合判定的结果。
type AccessDecision struct {
	// Emit 表示这一条应当原样落盘。
	Emit bool
	// Summaries 是到这一步为止需要补写的汇总记录。
	//
	// 只在小时切换时才有值：上一小时被聚合掉的条数要在新小时开头补一条，
	// 否则那些计数会随桶被清掉而永久丢失。
	Summaries []Entry
}

// accessKey 计算聚合键。
//
// 三个维度缺一不可：
//   - DeviceID：同一份内容送到两台设备是两条独立事实（取证要能回答"送到过哪些机器"）。
//   - RemoteIP：下载记录没有设备 ID，不带 IP 会把所有来源的下载混成一个桶。
//   - Event + Subject：区分"下发"与"下载"，并在下载里区分具体文件。
//
// sync 刻意不含 Subject：它的 Subject 是 revision，每次业务提交都会变，
// 拿来当键会让桶无限增殖、降噪失效。download 的 Subject 是文件名，有限且稳定。
func accessKey(e Entry) string {
	subject := e.Subject
	if e.Event == EventSync {
		subject = ""
	}
	return fmt.Sprintf("%s|%s|%s|%s|%s", e.Event, e.DeviceID, e.RemoteIP, e.Source, subject)
}

// Offer 判定一条 access 级记录该不该落盘。
func (a *AccessAggregator) Offer(e Entry) AccessDecision {
	a.mu.Lock()
	defer a.mu.Unlock()

	// 按记录自身的时间分桶，而不是按当前真实时间：
	// 汇总记录要落在它所属的那个小时文件里，否则跨小时的计数会被写到错误的分片。
	at := e.At
	if at.IsZero() {
		at = a.now().UTC()
	}
	hour := at.UTC().Truncate(time.Hour)

	var out AccessDecision
	// 先结算所有已经过期的桶：它们的 suppressed 计数必须补一条汇总，
	// 否则"被聚合掉多少条"这个唯一线索就丢了。
	for k, b := range a.buckets {
		if b.hour.Equal(hour) {
			continue
		}
		if b.suppressed > 0 {
			out.Summaries = append(out.Summaries, summarize(b))
		}
		delete(a.buckets, k)
	}
	// 全局计数跨小时也要归零，否则上限会一直卡住不放行。
	if !a.total.hour.Equal(hour) {
		a.total = accessTotal{hour: hour}
	}

	key := accessKey(e)
	b := a.buckets[key]
	if b == nil {
		// 桶数封顶：公网请求可以造出无限多的键（换 IP 即可）。
		// 超出后新键折进溢出桶，只保留总量信号，不让桶表本身成为内存风险。
		//
		// 判断时留出一个名额给溢出桶自己，否则"1024 个普通桶 + 1 个溢出桶"
		// 会变成 1025 个，上限名不副实。
		if len(a.buckets) >= MaxAccessBuckets-1 {
			key = overflowKey
			b = a.buckets[key]
			if b == nil {
				b = &accessBucket{hour: hour, sample: e}
				a.buckets[key] = b
			}
		} else {
			b = &accessBucket{hour: hour, sample: e}
			a.buckets[key] = b
		}
	}
	b.lastAt = e.At
	// 两个上限必须同时满足：单键限制局部刷屏，全局限制"换键刷屏"。
	if b.emitted < MaxAccessRecordsPerHour && a.total.emitted < MaxAccessRecordsPerHourTotal {
		b.emitted++
		a.total.emitted++
		out.Emit = true
		return out
	}
	b.suppressed++
	return out
}

// summarize 把桶里的计数变成一条汇总记录。
func summarize(b *accessBucket) Entry {
	sum := b.sample
	sum.At = b.lastAt
	sum.Detail = fmt.Sprintf("%s（本小时另有 %d 条同类记录被聚合）",
		b.sample.Detail, b.suppressed)
	sum.Count = b.suppressed
	// 汇总代表多条记录，可能来自不同来源地址。沿用第一条的 IP 会让读日志的人
	// 误以为这些访问都出自同一处，所以这里清掉，宁可少一个字段也不要错的信息。
	sum.RemoteIP = ""
	return sum
}

// Take 取走待补的汇总记录并清空状态。
//
// 只在进程退出时调用（见 Log.Close）：把"本小时已被聚合掉多少条"落盘，
// 否则最后一次汇总会随进程一起消失。取走即清，避免重复写入。
func (a *AccessAggregator) Take() []Entry {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []Entry
	for k, b := range a.buckets {
		if b.suppressed > 0 {
			out = append(out, summarize(b))
		}
		delete(a.buckets, k)
	}
	return out
}
