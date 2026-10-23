package localcert

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/cloud/crypt"
	"github.com/shichao402/Dec/internal/cloud/ssot"
)

// fakeIssuer 签发自签证书，记录调用次数与每次的私钥，用于验证换钥。
type fakeIssuer struct {
	calls    atomic.Int32
	validFor time.Duration
	now      func() time.Time
	domain   string // 非空时故意签成别的域名
	err      error
	keys     []string
}

func (f *fakeIssuer) Issue(ctx context.Context, domain string) (*Issued, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	name := domain
	if f.domain != "" {
		name = f.domain
	}
	start := f.now()
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(int64(f.calls.Load()) + 1000),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    start.Add(-time.Hour),
		NotAfter:     start.Add(f.validFor),
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	kder, _ := x509.MarshalPKCS8PrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kder})
	f.keys = append(f.keys, string(keyPEM))
	return &Issued{
		CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		KeyPEM:  keyPEM,
	}, nil
}

type harness struct {
	m      *Manager
	issuer *fakeIssuer
	store  *ssot.Store
	key    *crypt.Key
	now    time.Time
	issued int
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	store, err := ssot.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypt.NewKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{store: store, key: key, now: time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)}
	h.issuer = &fakeIssuer{validFor: 90 * 24 * time.Hour, now: func() time.Time { return h.now }}
	h.m = New(Options{
		Store:    store,
		RootKey:  func() *crypt.Key { return h.key },
		Issue:    h.issuer.Issue,
		Now:      func() time.Time { return h.now },
		OnIssued: func() { h.issued++ },
	})
	return h
}

func TestRenewIssuesWhenMissing(t *testing.T) {
	h := newHarness(t)
	did, err := h.m.RenewIfNeeded(context.Background())
	if err != nil || !did {
		t.Fatalf("首次应签发: did=%v err=%v", did, err)
	}
	if h.issued != 1 {
		t.Fatalf("OnIssued 次数 %d", h.issued)
	}
	cur, err := h.m.Current()
	if err != nil || cur == nil {
		t.Fatalf("Current: %v %v", cur, err)
	}
	if string(cur.KeyPEM) != h.issuer.keys[0] {
		t.Fatal("解密出的私钥与签发结果不一致")
	}
	// 私钥在 SSOT 里必须是密文。
	raw, _ := h.store.ReadFile(File)
	if contains(raw, "PRIVATE KEY") {
		t.Fatal("私钥以明文写进了 SSOT")
	}

	// 有效期充足时不再签发。
	did, err = h.m.RenewIfNeeded(context.Background())
	if err != nil || did {
		t.Fatalf("证书有效时不应重签: did=%v err=%v", did, err)
	}
	if h.issuer.calls.Load() != 1 {
		t.Fatalf("签发次数 %d", h.issuer.calls.Load())
	}
}

func TestRenewBefore30Days(t *testing.T) {
	h := newHarness(t)
	if _, err := h.m.RenewIfNeeded(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(59 * 24 * time.Hour) // 剩 31 天
	if need, _, _ := h.m.NeedsRenewal(); need {
		t.Fatal("剩 31 天时不应续期")
	}
	h.now = h.now.Add(2 * 24 * time.Hour) // 剩 29 天
	need, reason, _ := h.m.NeedsRenewal()
	if !need || reason == "" {
		t.Fatal("剩 29 天时应续期")
	}
	did, err := h.m.RenewIfNeeded(context.Background())
	if err != nil || !did {
		t.Fatalf("续期失败: %v", err)
	}
	cur, _ := h.m.Current()
	if !cur.NotAfter.After(h.now.Add(80 * 24 * time.Hour)) {
		t.Fatalf("续期后到期时间 %v", cur.NotAfter)
	}
}

func TestMarkRekeyForcesNewKey(t *testing.T) {
	h := newHarness(t)
	if _, err := h.m.RenewIfNeeded(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := h.m.MarkRekey("吊销设备 dev-x"); err != nil {
		t.Fatal(err)
	}
	need, reason, _ := h.m.NeedsRenewal()
	if !need || !contains([]byte(reason), "dev-x") {
		t.Fatalf("吊销后应换钥: need=%v reason=%q", need, reason)
	}
	if _, err := h.m.RenewIfNeeded(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.issuer.keys) != 2 || h.issuer.keys[0] == h.issuer.keys[1] {
		t.Fatal("换钥必须生成新私钥")
	}
	cur, _ := h.m.Current()
	if string(cur.KeyPEM) != h.issuer.keys[1] {
		t.Fatal("当前私钥不是新私钥")
	}
	if need, _, _ := h.m.NeedsRenewal(); need {
		t.Fatal("换钥后标记应清除")
	}
}

func TestMarkRekeyWithoutCertIsNoop(t *testing.T) {
	h := newHarness(t)
	if err := h.m.MarkRekey("x"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.ReadFile(File); !errors.Is(err, ssot.ErrNotFound) {
		t.Fatalf("没有证书时不应创建记录: %v", err)
	}
}

func TestRenewRejectsWrongDomain(t *testing.T) {
	h := newHarness(t)
	h.issuer.domain = "evil.example.com"
	if _, err := h.m.RenewIfNeeded(context.Background()); err == nil {
		t.Fatal("签发结果不覆盖目标域名时应报错")
	}
	if _, err := h.store.ReadFile(File); !errors.Is(err, ssot.ErrNotFound) {
		t.Fatal("错误的证书不应写入")
	}
}

func TestNoRootKeySkips(t *testing.T) {
	h := newHarness(t)
	h.key = nil
	did, err := h.m.RenewIfNeeded(context.Background())
	if err != nil || did || h.issuer.calls.Load() != 0 {
		t.Fatalf("恢复模式下不应签发: did=%v err=%v", did, err)
	}
	if _, err := h.m.Current(); err != nil {
		t.Fatalf("没有证书时 Current 应返回 nil, nil: %v", err)
	}
}

func TestRootKeyRotationTriggersReissue(t *testing.T) {
	h := newHarness(t)
	if _, err := h.m.RenewIfNeeded(context.Background()); err != nil {
		t.Fatal(err)
	}
	nk, _ := crypt.NewKey(rand.Reader)
	h.key = nk
	need, _, _ := h.m.NeedsRenewal()
	if !need {
		t.Fatal("根密钥更换后旧密文解不开，应重签")
	}
}

func TestRunReportsFailures(t *testing.T) {
	h := newHarness(t)
	h.issuer.err = errors.New("boom")
	var failures atomic.Int32
	h.m.opts.OnFailure = func(error) { failures.Add(1) }
	h.m.opts.RetryAfter = 5 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	h.m.Run(ctx)
	if failures.Load() < 2 {
		t.Fatalf("失败应按 RetryAfter 重试并告警，告警 %d 次", failures.Load())
	}

	// 未配置不告警。
	h.issuer.err = ErrNotConfigured
	failures.Store(0)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel2()
	h.m.Run(ctx2)
	if failures.Load() != 0 {
		t.Fatal("未配置不应告警")
	}
}

func TestDisabledManager(t *testing.T) {
	h := newHarness(t)
	h.m.opts.Issue = nil
	if h.m.Enabled() {
		t.Fatal("Issue 为空时应视为关闭")
	}
	did, err := h.m.RenewIfNeeded(context.Background())
	if did || err != nil {
		t.Fatal("关闭时不应签发")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.m.Run(ctx) // 应立即返回
}

func contains(b []byte, s string) bool {
	return len(s) > 0 && (string(b) == s || len(b) >= len(s) && indexOf(string(b), s) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
