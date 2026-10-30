package audit

// 审计事件的分级（ADR 0043）。
//
// 分级的出发点不是"事件重不重要"，而是"这条记录的信息量会不会被重复淹没"。
// 2026-10 那次故障里 99.94% 的记录是同一次下发被反复记账（91,328 条 sync 对
// 58 条真实业务事件），真正有用的信息反而没人看得见。
//
// 三级的分界线：
//
//   - A 安全与不可逆：密钥读写、设备登记与吊销、根密钥、初始化、部署、回滚。
//     这类事件发生频率由人的操作决定，天然低频，所以逐条 fsync、永不聚合、
//     永不因限额而丢弃。
//   - B 业务事实：资产与密钥内容的变更、项目订阅。频率上限就是 SSOT 的 commit
//     速率，一次操作一条，不多不少。
//   - C 传输与访问：下发、客户端下载。这类事件描述的是"同一份东西又送了一次"，
//     本身不包含业务变化，频率由网络重连、长轮询超时、乃至 bug 决定。
//     只有它们允许聚合。
type Tier int

const (
	// TierSecurity 是安全与不可逆事件，逐条落盘，任何限额都不作用于它。
	TierSecurity Tier = iota
	// TierFact 是业务事实，逐条记录，不受访问类限额影响。
	TierFact
	// TierAccess 是传输与访问，允许按 (来源, 对象, 小时) 聚合。
	TierAccess
)

// String 便于测试失败时读懂是哪一级。
func (t Tier) String() string {
	switch t {
	case TierSecurity:
		return "security"
	case TierFact:
		return "fact"
	case TierAccess:
		return "access"
	}
	return "unknown"
}

// Tier 返回事件所属级别。
//
// 新增事件类型时必须在这里归类，否则会落到默认值。默认取 TierAccess 是
// 有意的：新事件在证明自己低频之前，先按"可能刷屏"对待，避免再出现一次
// 因为把高频事件当成事实级而写满磁盘的事故。
func (e Event) Tier() Tier {
	switch e {
	// A 类：安全与不可逆。
	case EventSecretWrite, EventSecretDelete, EventSecretRead, EventSecretDeliver,
		EventEnroll, EventRecover, EventRevoke, EventDeviceKind,
		EventRootKeyLoad, EventRootKeyRotate, EventBootstrap, EventDeploy, EventRollback,
		EventOverride:
		return TierSecurity
	// B 类：业务事实。
	case EventAssetWrite, EventAssetDelete, EventProject, EventBackup, EventSettings:
		return TierFact
	// C 类：传输与访问。
	case EventSync, EventDownload:
		return TierAccess
	}
	return TierAccess
}
