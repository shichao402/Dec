package cloudsvc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/shichao402/Dec/internal/cloud/alert"
	"github.com/shichao402/Dec/internal/cloud/audit"
)

// 部署期协作文件（需求 21.2、21.3、21.8）。
//
// 部署脚本与服务端是两个进程，中间还夹着一次停服，没法靠内存里的状态对话。
// 所以约定用三个文件，位置固定在数据目录的 deploy/ 下：
//
//	maintenance   存在即为维护状态（需求 21.4）
//	pause-backup  存在即暂停冷备推送（需求 15.1）
//	pending.json  部署脚本写出结果，服务端读后删除并记成审计（需求 21.8）
//
// 为什么不给部署开一个协议方法：部署期间服务本来就是停的，让脚本先连上来
// 置位没有意义；而服务重新起来之后，让它自己看文件是最不容易出错的做法。
// 万一脚本在置位之后崩了，重启服务端仍然看得到维护标记，不会带着半成品
// 状态对外提供服务。
//
// 这三个文件都不在快照里：回滚不应该把“正在部署”这个状态一起回滚回去。

const (
	// maintenanceFile 是维护标记。
	maintenanceFile = "maintenance"
	// backupPauseFile 是冷备暂停标记。
	backupPauseFile = "pause-backup"
	// deployRecordFile 是待处理的部署记录。
	deployRecordFile = "pending.json"
)

// deployWatchInterval 是轮询协作文件的间隔。
//
// 取 2 秒：部署窗口本身以秒计，这个延迟可以忽略；而比起为它引入一个
// 文件系统事件依赖，定时读三个小文件便宜得多，也不会因为事件丢失而卡住。
const deployWatchInterval = 2 * time.Second

// deployDir 是部署协作目录。
func deployDir(dataDir string) string { return filepath.Join(dataDir, "deploy") }

// maintenancePath 是维护标记路径。
func maintenancePath(dataDir string) string {
	return filepath.Join(deployDir(dataDir), maintenanceFile)
}

// backupPausePath 是冷备暂停标记路径。
func backupPausePath(dataDir string) string {
	return filepath.Join(deployDir(dataDir), backupPauseFile)
}

// deployRecordPath 是部署记录路径。
func deployRecordPath(dataDir string) string {
	return filepath.Join(deployDir(dataDir), deployRecordFile)
}

// deployRecord 是一份部署结果，由部署脚本写出（需求 21.8）。
//
// 字段刻意做成扁平且全是字符串：写它的是 shell 脚本，读它的是这里，
// 中间没有 schema 校验的机会，越简单越不会解析失败。
type deployRecord struct {
	// Version 是这次部署的服务端版本。
	Version string `json:"version"`
	// Tag 是发布 tag。
	Tag string `json:"tag"`
	// Commit 是 tag 指向的 commit。
	Commit string `json:"commit"`
	// Result 是结果，取值 "ok" 或 "rolled_back"。
	Result string `json:"result"`
	// RolledBack 表示这次部署执行了回滚。
	RolledBack bool `json:"rolled_back"`
	// Detail 是补充说明，例如失败在哪一步。
	Detail string `json:"detail,omitempty"`
}

// OK 报告这次部署是否成功。
func (r deployRecord) OK() bool { return r.Result == "ok" && !r.RolledBack }

// summary 拼出写进审计与告警的一行摘要。
func (r deployRecord) summary() string {
	out := fmt.Sprintf("版本 %s，tag %s，commit %s，结果 %s",
		fallbackText(r.Version, "未知"), fallbackText(r.Tag, "未知"),
		shortCommit(r.Commit), fallbackText(r.Result, "未知"))
	if r.RolledBack {
		out += "，已回滚"
	}
	if r.Detail != "" {
		out += "；" + r.Detail
	}
	return out
}

// prepareDeployDir 建立部署协作目录。
//
// 目录权限 0750：部署脚本以部署账户身份写入，服务端自己也读，
// 不需要给其他用户任何权限。
func prepareDeployDir(dataDir string) error {
	if err := os.MkdirAll(deployDir(dataDir), 0o750); err != nil {
		return fmt.Errorf("创建部署目录失败: %w", err)
	}
	return nil
}

// fileExists 报告文件是否存在。
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// initMaintenanceState 在启动时读取维护标记（需求 21.4）。
//
// 部署脚本在停服前写下标记，下次启动就应该带着维护状态起来：
// 此时新版本还没通过健康检查，不能让代理以为服务已经就绪。
func (s *Server) initMaintenanceState() {
	on := fileExists(maintenancePath(s.cfg.DataDir))
	s.maintenance.Store(on)
	if on {
		s.logf("检测到维护标记，服务以维护状态启动")
	}
}

// SetMaintenance 切换维护状态（需求 21.4）。
//
// 内存标记与标记文件同时改：文件是跨进程、跨重启的唯一事实来源，
// 只改内存会出现“管理面和部署脚本看到的状态不一致”。
func (s *Server) SetMaintenance(on bool) {
	s.maintenance.Store(on)
	if on {
		if err := os.MkdirAll(deployDir(s.cfg.DataDir), 0o750); err != nil {
			s.logf("创建部署目录失败: %v", err)
			return
		}
		stamp := time.Now().UTC().Format(time.RFC3339) + "\n"
		if err := os.WriteFile(maintenancePath(s.cfg.DataDir), []byte(stamp), 0o640); err != nil {
			s.logf("写维护标记失败: %v", err)
		}
		return
	}
	if err := os.Remove(maintenancePath(s.cfg.DataDir)); err != nil && !os.IsNotExist(err) {
		s.logf("清除维护标记失败: %v", err)
	}
}

// Maintenance 报告当前是否处于维护状态。
func (s *Server) Maintenance() bool { return s.maintenance.Load() }

// watchDeploy 持续跟进部署协作文件。
//
// 只做三件事，全部是“文件说了算”：维护状态、冷备暂停、部署记录入审计。
// 每一项都容忍文件不存在——那是常态。
func (s *Server) watchDeploy(ctx context.Context) {
	ticker := time.NewTicker(deployWatchInterval)
	defer ticker.Stop()
	for {
		s.syncDeployFiles()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// syncDeployFiles 按文件现状对齐内存状态，并消费部署记录。
func (s *Server) syncDeployFiles() {
	dataDir := s.cfg.DataDir

	// 维护状态：文件在即维护。只看文件，不反向写文件，
	// 否则脚本与内存之间会互相覆盖。
	want := fileExists(maintenancePath(dataDir))
	if want != s.maintenance.Load() {
		s.maintenance.Store(want)
		if want {
			s.logf("进入维护状态")
		} else {
			s.logf("退出维护状态")
		}
	}

	// 冷备暂停：部署脚本置位，健康检查通过后清除。
	if s.backup != nil {
		paused := fileExists(backupPausePath(dataDir))
		if paused && !s.backup.Paused() {
			s.backup.Pause()
			s.logf("冷备推送已暂停（部署中）")
		}
		if !paused && s.backup.Paused() {
			s.backup.Resume()
			s.logf("冷备推送已恢复")
		}
	}

	s.consumeDeployRecord()
}

// consumeDeployRecord 读取并清除部署记录，写审计、必要时告警（需求 21.8）。
//
// 先读再删：记不进审计就把文件留着，下一轮还会再试。
// 反过来先删后读，一次写审计失败就会让这次部署彻底没有记录。
//
// 两个并发点必须挡住，否则会出现「读到半截 JSON，当成坏文件删掉」：
//   - watchDeploy 每秒轮询一次，测试与运维工具也可能主动调用 syncDeployFiles；
//   - 部署脚本在写 pending.json 的同时，轮询可能正好读到写了一半的内容。
//
// 因此：进程内用互斥串行化；跨进程用「先 rename 到私有名再读」，
// rename 在同一文件系统上是原子的——读到的要么完整，要么文件根本不在。
func (s *Server) consumeDeployRecord() {
	s.deployMu.Lock()
	defer s.deployMu.Unlock()

	path := deployRecordPath(s.cfg.DataDir)
	own := path + ".consuming"
	if err := os.Rename(path, own); err != nil {
		// 文件不在是常态：本轮没有待处理的部署。
		if !os.IsNotExist(err) {
			s.logf("接管部署记录失败: %v", err)
		}
		return
	}
	// 无论后续成败，私有副本都不该留下；审计写失败时把它放回原处等下一轮。
	restore := func() { _ = os.Rename(own, path) }

	raw, err := os.ReadFile(own)
	if err != nil {
		restore()
		return
	}
	var rec deployRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		// 内容坏了也要清掉，否则每一轮都会重试同一条坏数据。
		s.logf("部署记录解析失败: %v", err)
		_ = os.Remove(own)
		return
	}
	if s.audit != nil {
		detail := rec.summary()
		if err := s.audit.Append(audit.Entry{
			Event:   audit.EventDeploy,
			Source:  "deploy",
			Subject: rec.Tag,
			Detail:  detail,
			OK:      rec.OK(),
		}); err != nil {
			// 审计写不进去就保留文件，下轮重试。
			s.logf("写部署审计失败: %v", err)
			restore()
			return
		}
	}
	if !rec.OK() && s.alerts != nil {
		s.alerts.Raise(alert.KindDeploy, "部署失败", rec.summary())
	}
	if err := os.Remove(own); err != nil && !os.IsNotExist(err) {
		s.logf("清除部署记录失败: %v", err)
		return
	}
	s.logf("部署记录已记账: %s", rec.summary())
}

// shortCommit 取 commit 前 8 位，便于在日志与审计里一眼看完。
func shortCommit(v string) string {
	if len(v) <= 8 {
		return fallbackText(v, "未知")
	}
	return v[:8]
}

// fallbackText 返回第一个非空字符串。
func fallbackText(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
