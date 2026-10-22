package cloudsvc

import (
	"crypto/tls"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/cloud/audit"
)

// 部署期协作机制的测试（需求 21.2、21.3、21.4、21.8）。
//
// 这块逻辑只有部署脚本一个调用方，出问题时又恰好是“服务正在换版本”的
// 最坏时刻，所以每条约定都单独固定下来：标记文件在不在、什么时候被清、
// 审计有没有真的落进去。

// TestMaintenanceMarkerSurvivesRestart 验证维护标记能跨进程重启生效（需求 21.4）。
//
// 这是整个部署流程的关键：脚本在停服前写标记，新进程起来时必须先处于
// 维护状态，等健康检查通过后才由脚本清掉标记。如果标记只存在内存里，
// 新版本一起来就直接对外服务，代理会在迁移还没跑完时收到下发。
func TestMaintenanceMarkerSurvivesRestart(t *testing.T) {
	dir := t.TempDir()

	// 模拟部署脚本在停服前写下标记。
	if err := prepareDeployDir(dir); err != nil {
		t.Fatalf("建立部署目录失败: %v", err)
	}
	if err := os.WriteFile(maintenancePath(dir), []byte("2026-10-21T00:00:00Z\n"), 0o640); err != nil {
		t.Fatalf("写维护标记失败: %v", err)
	}

	h := newHarnessAt(t, dir, "1.0.0")
	if !h.srv.Maintenance() {
		t.Fatal("带维护标记启动时应处于维护状态")
	}

	// 健康检查要如实报告不可用，外部探活据此判断。
	body := h.health()
	if body["maintenance"] != true {
		t.Fatalf("健康检查应报告维护中，实际 %v", body["maintenance"])
	}
	if body["ok"] != false {
		t.Fatalf("维护中 ok 应为 false，实际 %v", body["ok"])
	}
}

// TestMaintenanceMarkerRemovedOnExit 验证退出维护会删掉标记文件。
//
// 只改内存不删文件，下次重启又会莫名其妙进入维护状态，
// 而且部署脚本无法判断究竟有没有生效。
func TestMaintenanceMarkerRemovedOnExit(t *testing.T) {
	h := newHarness(t, "1.0.0")

	h.srv.SetMaintenance(true)
	if _, err := os.Stat(maintenancePath(h.dir)); err != nil {
		t.Fatalf("进入维护应写出标记文件: %v", err)
	}

	h.srv.SetMaintenance(false)
	if _, err := os.Stat(maintenancePath(h.dir)); !os.IsNotExist(err) {
		t.Fatalf("退出维护应删除标记文件，实际 err=%v", err)
	}
	if h.srv.Maintenance() {
		t.Fatal("退出维护后内存状态应为 false")
	}
}

// TestMaintenanceMarkerFollowsFile 验证标记文件被外部改动后内存跟随。
//
// 脚本在回滚时会把标记写回去，服务端不能停在旧状态上。
func TestMaintenanceMarkerFollowsFile(t *testing.T) {
	h := newHarness(t, "1.0.0")
	if h.srv.Maintenance() {
		t.Fatal("初始不应处于维护状态")
	}

	path := maintenancePath(h.dir)
	if err := os.WriteFile(path, []byte("now\n"), 0o640); err != nil {
		t.Fatalf("写标记失败: %v", err)
	}
	h.srv.syncDeployFiles()
	if !h.srv.Maintenance() {
		t.Fatal("标记出现后应进入维护状态")
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("删除标记失败: %v", err)
	}
	h.srv.syncDeployFiles()
	if h.srv.Maintenance() {
		t.Fatal("标记消失后应退出维护状态")
	}
}

// TestBackupPauseFollowsFile 验证冷备暂停标记能跨进程生效（需求 15.1）。
//
// 部署期间的 commit 可能随后被回滚，推到冷备只会污染远端历史。
func TestBackupPauseFollowsFile(t *testing.T) {
	h := newHarness(t, "1.0.0")
	h.srv.backup.Resume()
	if h.srv.backup.Paused() {
		t.Fatal("初始不应暂停")
	}

	path := backupPausePath(h.dir)
	if err := os.WriteFile(path, []byte("now\n"), 0o640); err != nil {
		t.Fatalf("写暂停标记失败: %v", err)
	}
	h.srv.syncDeployFiles()
	if !h.srv.backup.Paused() {
		t.Fatal("暂停标记出现后冷备应暂停")
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("删除暂停标记失败: %v", err)
	}
	h.srv.syncDeployFiles()
	if h.srv.backup.Paused() {
		t.Fatal("暂停标记消失后冷备应恢复")
	}
}

// TestDeployRecordAuditedAndCleared 验证部署记录被记账后清除（需求 21.8）。
func TestDeployRecordAuditedAndCleared(t *testing.T) {
	h := newHarness(t, "1.0.0")
	writeDeployRecord(t, h.dir, deployRecord{
		Version: "v1.14.0", Tag: "v1.14.0",
		Commit: "0123456789abcdef0123456789abcdef01234567",
		Result: "ok",
	})

	h.srv.syncDeployFiles()

	if _, err := os.Stat(deployRecordPath(h.dir)); !os.IsNotExist(err) {
		t.Fatalf("部署记录应被清除，实际 err=%v", err)
	}

	found := findDeployAudit(t, h)
	if !found.OK {
		t.Fatal("成功部署的审计 OK 应为 true")
	}
	if found.Subject != "v1.14.0" {
		t.Fatalf("审计对象应为 tag，实际 %q", found.Subject)
	}
	// 摘要里要能看到版本、commit 与结果，回看审计时不用再拼别处信息。
	for _, want := range []string{"v1.14.0", "01234567", "ok"} {
		if !strings.Contains(found.Detail, want) {
			t.Fatalf("审计 Detail 缺少 %q：%s", want, found.Detail)
		}
	}
}

// TestDeployRecordRollbackAudited 验证回滚会记成失败并保留原因（需求 21.3、21.8）。
func TestDeployRecordRollbackAudited(t *testing.T) {
	h := newHarness(t, "1.0.0")
	writeDeployRecord(t, h.dir, deployRecord{
		Version: "v1.14.1", Tag: "v1.14.1",
		Commit: "fedcba9876543210fedcba9876543210fedcba98",
		Result: "rolled_back", RolledBack: true,
		Detail: "健康检查未通过，已回滚到 v1.14.0",
	})

	h.srv.syncDeployFiles()

	found := findDeployAudit(t, h)
	if found.OK {
		t.Fatal("回滚的审计 OK 应为 false")
	}
	if !strings.Contains(found.Detail, "已回滚") {
		t.Fatalf("审计 Detail 应说明回滚：%s", found.Detail)
	}
}

// TestDeployRecordConsumeIsConcurrencySafe 固定「后台轮询 + 主动调用」并发下的行为。
//
// watchDeploy 每秒轮询一次，运维工具与测试也会直接调 syncDeployFiles。
// 没有互斥时两边会同时拿到同一份 pending.json，各自写一条审计；
// 更糟的是读到对方写了一半的内容，被当成坏文件删掉，审计直接丢失。
// CI 上真实出现过：TestDeployRecordRollbackAudited 偶发「应写入一条部署审计」。
func TestDeployRecordConsumeIsConcurrencySafe(t *testing.T) {
	const rounds = 30
	for i := 0; i < rounds; i++ {
		h := newHarness(t, "1.0.0")
		writeDeployRecord(t, h.dir, deployRecord{
			Version: "v1.14.0", Tag: "v1.14.0",
			Commit: "0123456789abcdef0123456789abcdef01234567",
			Result: "ok",
		})

		var wg sync.WaitGroup
		for j := 0; j < 8; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				h.srv.syncDeployFiles()
			}()
		}
		wg.Wait()

		entries, err := h.srv.audit.Read(time.Now().UTC())
		if err != nil {
			t.Fatalf("第 %d 轮读审计失败: %v", i, err)
		}
		n := 0
		for _, e := range entries {
			if e.Event == audit.EventDeploy && e.Subject == "v1.14.0" {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("第 %d 轮部署审计应为 1 条，实际 %d 条", i, n)
		}
		if _, err := os.Stat(deployRecordPath(h.dir)); !os.IsNotExist(err) {
			t.Fatalf("第 %d 轮部署记录应被清除，实际 err=%v", i, err)
		}
	}
}

// TestDeployRecordBadJSONIsCleared 验证坏记录不会每轮重试。
//
// 内容坏了是脚本的 bug，但服务端如果一直重试同一条坏数据，
// 只会把日志刷满，也不会自愈。
func TestDeployRecordBadJSONIsCleared(t *testing.T) {
	h := newHarness(t, "1.0.0")
	if err := prepareDeployDir(h.dir); err != nil {
		t.Fatalf("建立部署目录失败: %v", err)
	}
	if err := os.WriteFile(deployRecordPath(h.dir), []byte("{不是 json"), 0o640); err != nil {
		t.Fatalf("写坏记录失败: %v", err)
	}

	h.srv.syncDeployFiles()

	if _, err := os.Stat(deployRecordPath(h.dir)); !os.IsNotExist(err) {
		t.Fatalf("坏记录应被清除，实际 err=%v", err)
	}
}

// TestDeployDirNotInSnapshot 固定一个约定：协作目录不在快照范围内。
//
// “快照”指部署脚本复制的服务端版本、客户端二进制与数据；
// 协作文件由脚本自己管理。测试只固定目录位置，避免有人把它挪进
// ssot/ 或 state/ 之后被一起回滚。
func TestDeployDirNotInSnapshot(t *testing.T) {
	dir := t.TempDir()
	if got, want := deployDir(dir), filepath.Join(dir, "deploy"); got != want {
		t.Fatalf("部署目录应在数据根下，实际 %q", got)
	}
	for _, sub := range []string{storeDir(dir), stateDir(dir), auditDir(dir)} {
		if deployDir(dir) == sub {
			t.Fatalf("部署目录不能落在 %q 里，否则会被快照与回滚波及", sub)
		}
	}
}

// findDeployAudit 取出今天最后一条部署审计。
func findDeployAudit(t *testing.T, h *harness) *audit.Entry {
	t.Helper()
	entries, err := h.srv.audit.Read(time.Now().UTC())
	if err != nil {
		t.Fatalf("读取审计失败: %v", err)
	}
	var found *audit.Entry
	for i := range entries {
		if entries[i].Event == audit.EventDeploy {
			found = &entries[i]
		}
	}
	if found == nil {
		t.Fatal("应写入一条部署审计")
	}
	return found
}

// health 请求一次健康检查并返回解出来的 JSON。
func (h *harness) health() map[string]any {
	h.t.Helper()
	ts := httptest.NewUnstartedServer(h.handler)
	ts.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	ts.StartTLS()
	defer ts.Close()
	resp, err := ts.Client().Get(ts.URL + "/healthz")
	if err != nil {
		h.t.Fatalf("健康检查失败: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		h.t.Fatalf("健康检查响应不是 JSON: %v", err)
	}
	return out
}

// writeDeployRecord 写出一份部署记录。
//
// 必须原子写：生产环境的部署脚本也是「先写临时文件再 rename」，
// 因为服务端每秒都在轮询这个文件。用 os.WriteFile 直接写会先截断后写，
// 轮询正好落在中间就读到空内容，被当成坏文件删掉，审计就丢了。
func writeDeployRecord(t *testing.T, dataDir string, rec deployRecord) {
	t.Helper()
	if err := prepareDeployDir(dataDir); err != nil {
		t.Fatalf("建立部署目录失败: %v", err)
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("序列化部署记录失败: %v", err)
	}
	path := deployRecordPath(dataDir)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o640); err != nil {
		t.Fatalf("写部署记录临时文件失败: %v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("落盘部署记录失败: %v", err)
	}
}
