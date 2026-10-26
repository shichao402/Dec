package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/proto"
)

// 同步主循环的行为（需求 2.4、5.1、5.2、5.4、19.5）。
//
// 这些用例跑在真实的 HTTP 之上：同步循环的价值就在于“线上会发生什么”，
// 把它 mock 掉就测不到长轮询、错误映射和退避这些真正容易错的地方。

// fakeServer 是一个最小的协议服务端。
type fakeServer struct {
	mu sync.Mutex
	// hello 是握手返回。
	hello proto.HelloResponse
	// syncs 是依次返回的对账响应，用完后重复最后一个。
	syncs []*proto.SyncResponse
	// acks 记录收到的回执。
	acks []proto.AckRequest
	// syncReqs 记录收到的对账请求。
	syncReqs []proto.SyncRequest
	// calls 统计各方法被调用次数。
	calls map[proto.Method]int
	// syncErr 非空时让 sync 返回错误码。
	syncErr proto.ErrorCode
}

func newFakeServer(t *testing.T, fs *fakeServer) *httptest.Server {
	t.Helper()
	if fs.calls == nil {
		fs.calls = map[proto.Method]int{}
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req proto.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		fs.mu.Lock()
		fs.calls[req.Method]++
		fs.mu.Unlock()

		resp := &proto.Response{ID: req.ID, OK: true}
		switch req.Method {
		case proto.MethodHello:
			resp.Payload = mustJSON(t, fs.hello)
		case proto.MethodSync:
			var in proto.SyncRequest
			_ = json.Unmarshal(req.Payload, &in)
			fs.mu.Lock()
			fs.syncReqs = append(fs.syncReqs, in)
			errCode := fs.syncErr
			var out proto.SyncResponse
			if len(fs.syncs) > 0 {
				out = *fs.syncs[0]
				if len(fs.syncs) > 1 {
					fs.syncs = fs.syncs[1:]
				}
			}
			fs.mu.Unlock()
			if errCode != "" {
				resp.OK = false
				resp.Code = errCode
				resp.Error = "fake"
				break
			}
			resp.Payload = mustJSON(t, out)
		case proto.MethodAck:
			var in proto.AckRequest
			_ = json.Unmarshal(req.Payload, &in)
			fs.mu.Lock()
			fs.acks = append(fs.acks, in)
			fs.mu.Unlock()
			resp.Payload = mustJSON(t, proto.AckResponse{})
		default:
			resp.OK = false
			resp.Code = proto.ErrInvalid
			resp.Error = "unsupported"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	return data
}

// selfSignedIdentity 造一份可用于 TLS 握手的设备身份。
//
// 客户端在每次需要身份的请求上都会加载证书，所以测试身份必须是
// “证书 + 私钥”真的配对：随手写一个假 PEM 会在握手前就失败，
// 结果测到的是证书加载错误，而不是同步逻辑。
func selfSignedIdentity(t *testing.T, deviceID string) *Identity {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成密钥失败: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: deviceID},
		DNSNames:     []string{deviceID},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("签发证书失败: %v", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("编码私钥失败: %v", err)
	}
	return &Identity{
		DeviceID: deviceID,
		CertPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		KeyPEM:   string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})),
		NotAfter: tmpl.NotAfter,
	}
}

// newTestSyncer 造一个已登记身份的同步器，落点指向临时用户目录。
func newTestSyncer(t *testing.T, srv *httptest.Server, fs *fakeServer) (*Syncer, string) {
	t.Helper()
	if fs != nil && fs.hello.ServerVersion == "" {
		fs.hello = proto.HelloResponse{
			ServerVersion:   "1.0.0",
			RequiredVersion: "1.0.0",
			ProtocolVersion: proto.Version,
			Aligned:         true,
			DeviceID:        "dev-1",
		}
	}
	home := t.TempDir()
	state := t.TempDir()
	store := NewIdentityStore(state)
	if err := store.Save(selfSignedIdentity(t, "dev-1")); err != nil {
		t.Fatalf("保存身份失败: %v", err)
	}
	s, err := NewSyncer(Options{
		ServerURL:  srv.URL,
		Version:    "1.0.0",
		StateDir:   state,
		HomeDir:    home,
		LongPoll:   time.Millisecond,
		Insecure:   true,
		Logf:       func(string, ...any) {},
		DeviceName: "test",
	}, store)
	if err != nil {
		t.Fatalf("构造同步器失败: %v", err)
	}
	return s, home
}

func TestSyncAppliesBatchAndAcks(t *testing.T) {
	fs := &fakeServer{
		syncs: []*proto.SyncResponse{{
			Batch: &proto.Batch{
				Revision: "rev-1",
				Files: []proto.File{{
					ID:      "asset:woa:style",
					Targets: []proto.Target{{Plan: proto.PlanUser, Root: "cursor", Path: "rules/style.md"}},
					Content: "# style\n",
					Format:  proto.FormatWhole,
				}},
			},
		}, {}},
	}
	srv := newFakeServer(t, fs)
	s, home := newTestSyncer(t, srv, fs)

	if err := s.tick(context.Background()); err != nil {
		t.Fatalf("一轮同步失败: %v", err)
	}

	path := filepath.Join(home, ".cursor", "rules", "style.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("内容应已落盘: %v", err)
	}
	if len(fs.acks) != 1 {
		t.Fatalf("应发出 1 次回执，实际 %d", len(fs.acks))
	}
	if fs.acks[0].Revision != "rev-1" {
		t.Fatalf("回执 revision %q，期望 rev-1", fs.acks[0].Revision)
	}
	if len(fs.acks[0].Results) != 1 || !fs.acks[0].Results[0].OK {
		t.Fatalf("回执结果不符：%+v", fs.acks[0].Results)
	}
	if got := s.Status().Revision; got != "rev-1" {
		t.Fatalf("状态 revision %q，期望 rev-1", got)
	}
}

func TestSyncReportsChecksNextRound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.md")
	fs := &fakeServer{
		syncs: []*proto.SyncResponse{
			// 第一轮：下发内容，同时给出核对清单。
			{
				Batch: &proto.Batch{
					Revision: "rev-1",
					Files: []proto.File{{
						ID:      "asset:woa:style",
						Targets: []proto.Target{{Plan: proto.PlanUser, Root: "cursor", Path: "rules/style.md"}},
						Content: "# style\n",
						Format:  proto.FormatWhole,
					}},
				},
			},
			// 第二轮：只要核对结果，没有新内容。
			{},
		},
	}
	srv := newFakeServer(t, fs)
	s, home := newTestSyncer(t, srv, fs)
	_ = path

	if err := s.tick(context.Background()); err != nil {
		t.Fatalf("第一轮失败: %v", err)
	}

	// 模拟云端在下一轮告诉代理“请核对这个落点”。
	realPath := filepath.Join(home, ".cursor", "rules", "style.md")
	s.mu.Lock()
	s.checks = []proto.ManifestEntry{{
		ID:     "asset:woa:style",
		Format: proto.FormatWhole,
		Paths:  []string{realPath},
	}}
	s.mu.Unlock()

	if err := s.tick(context.Background()); err != nil {
		t.Fatalf("第二轮失败: %v", err)
	}

	fs.mu.Lock()
	reqs := append([]proto.SyncRequest(nil), fs.syncReqs...)
	fs.mu.Unlock()
	if len(reqs) < 2 {
		t.Fatalf("应发出至少 2 次对账请求，实际 %d", len(reqs))
	}
	// 第一轮没有核对清单，所以清单应为空；第二轮应当回填哈希。
	if len(reqs[0].Manifest) != 0 {
		t.Fatalf("首轮不应有清单，实际 %+v", reqs[0].Manifest)
	}
	if len(reqs[1].Manifest) != 1 {
		t.Fatalf("次轮应上报 1 条，实际 %+v", reqs[1].Manifest)
	}
	if reqs[1].Manifest[0].Hash == "" {
		t.Fatalf("次轮哈希不应为空：%+v", reqs[1].Manifest[0])
	}
}

func TestSyncReportsMissingPathAsEmptyHash(t *testing.T) {
	// 落点被用户删了：必须上报空哈希，让云端补发，而不是假装一致。
	entries := []proto.ManifestEntry{{
		ID:     "asset:woa:style",
		Format: proto.FormatWhole,
		Paths:  []string{filepath.Join(t.TempDir(), "gone.md")},
	}}
	got := BuildManifest(entries)
	if len(got) != 1 {
		t.Fatalf("条目数 %d", len(got))
	}
	if got[0].Hash != "" {
		t.Fatalf("缺失落点应上报空哈希，实际 %q", got[0].Hash)
	}
}

func TestSyncReportsDivergentCopiesAsEmptyHash(t *testing.T) {
	// 一份资产同时落在多个宿主目录，只要其中任意一份被本地改过，
	// 就必须上报空哈希让云端补发。曾经的实现只保留循环里最后一次读到的
	// 哈希，于是「第一份被改、最后一份没动」会被判成一致，被改的那一份
	// 永远纠正不回来——2026-10-26 在一个多落点资产的 .cursor 副本上实测复现。
	dir := t.TempDir()
	first := filepath.Join(dir, "first.md")
	last := filepath.Join(dir, "last.md")
	if err := os.WriteFile(first, []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(last, []byte("expected\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries := []proto.ManifestEntry{{
		ID:     "asset:woa:style",
		Format: proto.FormatWhole,
		Paths:  []string{first, last},
	}}
	got := BuildManifest(entries)
	if len(got) != 1 {
		t.Fatalf("条目数 %d", len(got))
	}
	if got[0].Hash != "" {
		t.Fatalf("多落点内容不一致时应上报空哈希，实际 %q", got[0].Hash)
	}
}

func TestSyncReportsConsistentCopiesWithHash(t *testing.T) {
	// 一致的多个副本仍然要正常给出哈希，否则每轮都会无谓重发。
	dir := t.TempDir()
	a := filepath.Join(dir, "a.md")
	b := filepath.Join(dir, "b.md")
	body := []byte("same\n")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries := []proto.ManifestEntry{{
		ID:     "asset:woa:style",
		Format: proto.FormatWhole,
		Paths:  []string{a, b},
	}}
	got := BuildManifest(entries)
	if len(got) != 1 {
		t.Fatalf("条目数 %d", len(got))
	}
	if got[0].Hash == "" {
		t.Fatal("内容一致的多个落点不应上报空哈希")
	}
}

func TestSyncNotAlignedSkipsWrite(t *testing.T) {
	fs := &fakeServer{hello: proto.HelloResponse{
		ServerVersion:   "2.0.0",
		RequiredVersion: "2.0.0",
		ProtocolVersion: proto.Version,
		Aligned:         false,
	}}
	srv := newFakeServer(t, fs)
	s, home := newTestSyncer(t, srv, fs)

	err := s.tick(context.Background())
	if err == nil {
		t.Fatal("版本不对齐应返回错误")
	}
	if !isErr(err, ErrNotAligned) {
		t.Fatalf("错误应为 ErrNotAligned，实际 %v", err)
	}
	// 关键：不对齐时不能动任何落点。
	if _, statErr := os.Stat(filepath.Join(home, ".cursor")); !os.IsNotExist(statErr) {
		t.Fatalf("版本不对齐时不应写入任何落点，err=%v", statErr)
	}
	if fs.calls[proto.MethodSync] != 0 {
		t.Fatalf("版本不对齐时不应发起对账，实际 %d 次", fs.calls[proto.MethodSync])
	}
}

func TestSyncMaintenanceSkipsWrite(t *testing.T) {
	fs := &fakeServer{hello: proto.HelloResponse{
		ServerVersion:   "1.0.0",
		RequiredVersion: "1.0.0",
		ProtocolVersion: proto.Version,
		Aligned:         true,
		Maintenance:     true,
	}}
	srv := newFakeServer(t, fs)
	s, home := newTestSyncer(t, srv, fs)

	err := s.tick(context.Background())
	if err == nil || !isErr(err, ErrMaintenance) {
		t.Fatalf("维护中应返回 ErrMaintenance，实际 %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".cursor")); !os.IsNotExist(statErr) {
		t.Fatalf("维护中不应写入任何落点，err=%v", statErr)
	}
	if !s.Status().Maintenance {
		t.Fatal("状态应标记为维护中")
	}
}

func TestSyncOfflineKeepsFiles(t *testing.T) {
	fs := &fakeServer{
		syncs: []*proto.SyncResponse{{
			Batch: &proto.Batch{
				Revision: "rev-1",
				Files: []proto.File{{
					ID:      "asset:woa:style",
					Targets: []proto.Target{{Plan: proto.PlanUser, Root: "cursor", Path: "rules/style.md"}},
					Content: "# style\n",
					Format:  proto.FormatWhole,
				}},
			},
		}},
	}
	srv := newFakeServer(t, fs)
	s, home := newTestSyncer(t, srv, fs)
	if err := s.tick(context.Background()); err != nil {
		t.Fatalf("首轮失败: %v", err)
	}
	path := filepath.Join(home, ".cursor", "rules", "style.md")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}

	// 服务端下线：离线期间已落地文件必须原样保留（需求 5.1）。
	srv.Close()
	if err := s.tick(context.Background()); err == nil {
		t.Fatal("服务端下线后应返回错误")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("离线后文件应仍在: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("离线不应改动已落地内容")
	}
}

func TestSyncRejectedIdentityStops(t *testing.T) {
	fs := &fakeServer{syncErr: proto.ErrUnauthorized}
	srv := newFakeServer(t, fs)
	s, _ := newTestSyncer(t, srv, fs)

	err := s.tick(context.Background())
	if err == nil {
		t.Fatal("身份被拒应返回错误")
	}
	if !isErr(err, ErrIdentityRejected) {
		t.Fatalf("错误应为 ErrIdentityRejected，实际 %v", err)
	}
}

func TestSyncNoIdentityStopsBeforeSync(t *testing.T) {
	fs := &fakeServer{hello: proto.HelloResponse{
		ServerVersion:   "1.0.0",
		RequiredVersion: "1.0.0",
		ProtocolVersion: proto.Version,
		Aligned:         true,
	}}
	srv := newFakeServer(t, fs)
	home := t.TempDir()
	state := t.TempDir()
	// 刻意不写身份：模拟刚装好还没登记。
	s, err := NewSyncer(Options{
		ServerURL: srv.URL,
		Version:   "1.0.0",
		StateDir:  state,
		HomeDir:   home,
		Insecure:  true,
		Logf:      func(string, ...any) {},
	}, NewIdentityStore(state))
	if err != nil {
		t.Fatalf("构造同步器失败: %v", err)
	}

	err = s.tick(context.Background())
	if err == nil || !isErr(err, ErrNoIdentity) {
		t.Fatalf("未登记应返回 ErrNoIdentity，实际 %v", err)
	}
	if fs.calls[proto.MethodSync] != 0 {
		t.Fatalf("未登记时不应发起对账，实际 %d 次", fs.calls[proto.MethodSync])
	}
}

func TestSyncDeleteRemovesFile(t *testing.T) {
	fs := &fakeServer{
		syncs: []*proto.SyncResponse{
			{
				Batch: &proto.Batch{
					Revision: "rev-1",
					Files: []proto.File{{
						ID:      "asset:woa:style",
						Targets: []proto.Target{{Plan: proto.PlanUser, Root: "cursor", Path: "rules/style.md"}},
						Content: "# style\n",
						Format:  proto.FormatWhole,
					}},
				},
			},
			{
				Batch: &proto.Batch{
					Revision: "rev-2",
					Deletes: []proto.File{{
						ID:      "asset:woa:style",
						Targets: []proto.Target{{Plan: proto.PlanUser, Root: "cursor", Path: "rules/style.md"}},
						Format:  proto.FormatWhole,
					}},
				},
			},
		},
	}
	srv := newFakeServer(t, fs)
	s, home := newTestSyncer(t, srv, fs)

	if err := s.tick(context.Background()); err != nil {
		t.Fatalf("首轮失败: %v", err)
	}
	path := filepath.Join(home, ".cursor", "rules", "style.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("文件应存在: %v", err)
	}
	if err := s.tick(context.Background()); err != nil {
		t.Fatalf("次轮失败: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("删除下发后文件应消失，err=%v", err)
	}
}

func TestSyncBadTargetReportsFailureNotCrash(t *testing.T) {
	// 云端给了一个白名单外的落点：这一条要失败，但循环不能崩，
	// 且失败必须如实回执（服务端据此告警）。
	fs := &fakeServer{
		syncs: []*proto.SyncResponse{{
			Batch: &proto.Batch{
				Revision: "rev-1",
				Files: []proto.File{
					{
						ID:      "asset:woa:bad",
						Targets: []proto.Target{{Plan: proto.PlanUser, Root: "not-a-host", Path: "rules/a.md"}},
						Content: "x",
						Format:  proto.FormatWhole,
					},
					{
						ID:      "asset:woa:good",
						Targets: []proto.Target{{Plan: proto.PlanUser, Root: "cursor", Path: "rules/good.md"}},
						Content: "ok",
						Format:  proto.FormatWhole,
					},
				},
			},
		}},
	}
	srv := newFakeServer(t, fs)
	s, home := newTestSyncer(t, srv, fs)

	if err := s.tick(context.Background()); err != nil {
		t.Fatalf("单条失败不应让整轮失败: %v", err)
	}
	if len(fs.acks) != 1 || len(fs.acks[0].Results) != 2 {
		t.Fatalf("应有 2 条回执结果，实际 %+v", fs.acks)
	}
	byID := map[string]proto.AckResult{}
	for _, r := range fs.acks[0].Results {
		byID[r.ID] = r
	}
	if byID["asset:woa:bad"].OK {
		t.Fatal("越界落点应回执失败")
	}
	if !byID["asset:woa:good"].OK {
		t.Fatalf("合法落点应成功，实际 %+v", byID["asset:woa:good"])
	}
	if _, err := os.Stat(filepath.Join(home, ".cursor", "rules", "good.md")); err != nil {
		t.Fatalf("合法条目应已落盘: %v", err)
	}
}

func TestSyncDeliversAlerts(t *testing.T) {
	fs := &fakeServer{
		syncs: []*proto.SyncResponse{{
			Alerts: []proto.Alert{{
				ID:     "a1",
				Kind:   "override",
				Title:  "本地改动被覆盖",
				Detail: "rules/style.md",
				Count:  2,
			}},
		}},
	}
	srv := newFakeServer(t, fs)
	s, _ := newTestSyncer(t, srv, fs)
	n := &recordingNotifier{}
	s.opts.Notifier = n

	if err := s.tick(context.Background()); err != nil {
		t.Fatalf("同步失败: %v", err)
	}
	if len(n.got) != 1 {
		t.Fatalf("应发出 1 条通知，实际 %d", len(n.got))
	}
	if n.got[0].title != "本地改动被覆盖" {
		t.Fatalf("通知标题 %q", n.got[0].title)
	}
	// 合并次数应出现在正文里，否则用户不知道反复被覆盖了多少次。
	if n.got[0].body == "" {
		t.Fatal("通知正文不应为空")
	}
}

// recordingNotifier 记录通知，便于断言。
type recordingNotifier struct {
	mu  sync.Mutex
	got []struct{ title, body string }
}

func (r *recordingNotifier) Notify(title, body string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, struct{ title, body string }{title: title, body: body})
	return nil
}

// isErr 判断错误链里是否包含目标错误。
func isErr(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
