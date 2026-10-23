package agent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/proto"
)

// 本机证书落盘（agent-tray-access 需求 4.5）。

func testBundle(t *testing.T, serial int64) *proto.LocalTLSBundle {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	notAfter := time.Now().Add(90 * 24 * time.Hour).UTC().Truncate(time.Second)
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: LocalDomain},
		DNSNames:     []string{LocalDomain},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kder, _ := x509.MarshalPKCS8PrivateKey(key)
	return &proto.LocalTLSBundle{
		Domain:   LocalDomain,
		Serial:   big.NewInt(serial).Text(16),
		NotAfter: notAfter,
		CertPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		KeyPEM:   string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kder})),
	}
}

func TestLocalTLSStoreSaveLoad(t *testing.T) {
	s := NewLocalTLSStore(t.TempDir())
	if s.Serial() != "" {
		t.Fatal("初始应无证书")
	}
	if c, m, err := s.Load(); c != nil || m != nil || err != nil {
		t.Fatalf("无证书时 Load 应全空: %v %v %v", c, m, err)
	}
	b := testBundle(t, 42)
	if err := s.Save(b); err != nil {
		t.Fatal(err)
	}
	if s.Serial() != b.Serial {
		t.Fatalf("序列号 %q，期望 %q", s.Serial(), b.Serial)
	}
	cert, meta, err := s.Load()
	if err != nil || cert == nil || meta.Domain != LocalDomain || !meta.NotAfter.Equal(b.NotAfter) {
		t.Fatalf("Load: %v %+v %v", cert, meta, err)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(filepath.Join(s.Dir(), tlsKeyFile))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("私钥权限应为 0600: %v %v", fi.Mode(), err)
		}
	}

	// 覆盖为新证书。
	b2 := testBundle(t, 43)
	if err := s.Save(b2); err != nil {
		t.Fatal(err)
	}
	if s.Serial() != b2.Serial {
		t.Fatal("应更新为新序列号")
	}
}

func TestLocalTLSStoreRejectsMismatch(t *testing.T) {
	s := NewLocalTLSStore(t.TempDir())
	good := testBundle(t, 1)
	if err := s.Save(good); err != nil {
		t.Fatal(err)
	}
	bad := testBundle(t, 2)
	bad.KeyPEM = testBundle(t, 3).KeyPEM
	if err := s.Save(bad); err == nil {
		t.Fatal("证书与私钥不匹配时应拒绝")
	}
	if s.Serial() != good.Serial {
		t.Fatal("拒绝写入后应保留旧证书")
	}
	if err := s.Save(&proto.LocalTLSBundle{CertPEM: good.CertPEM, KeyPEM: good.KeyPEM}); err == nil {
		t.Fatal("缺序列号应拒绝")
	}
}

func TestSyncerApplyLocalTLS(t *testing.T) {
	store := NewLocalTLSStore(t.TempDir())
	called := 0
	s := &Syncer{opts: Options{
		LocalTLS:   store,
		OnLocalTLS: func() { called++ },
		Logf:       t.Logf,
	}}
	s.applyLocalTLS(nil)
	if called != 0 {
		t.Fatal("nil 不应触发回调")
	}
	b := testBundle(t, 7)
	s.applyLocalTLS(b)
	if called != 1 || store.Serial() != b.Serial {
		t.Fatalf("应落盘并回调: called=%d serial=%q", called, store.Serial())
	}
	bad := testBundle(t, 8)
	bad.KeyPEM = "garbage"
	s.applyLocalTLS(bad)
	if called != 1 || store.Serial() != b.Serial {
		t.Fatal("落盘失败时不应回调，也不应破坏旧证书")
	}
}
