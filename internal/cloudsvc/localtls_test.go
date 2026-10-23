package cloudsvc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/cloud/localcert"
	"github.com/shichao402/Dec/internal/proto"
)

// 本机证书下发（agent-tray-access 需求 4.5、4.6）。

// selfSignIssuer 每次签发一张新私钥的自签证书。
func selfSignIssuer(calls *atomic.Int32) localcert.IssueFunc {
	return func(ctx context.Context, domain string) (*localcert.Issued, error) {
		n := calls.Add(1)
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		tpl := &x509.Certificate{
			SerialNumber: big.NewInt(int64(n) + 5000),
			Subject:      pkix.Name{CommonName: domain},
			DNSNames:     []string{domain},
			NotBefore:    time.Now().Add(-time.Hour),
			NotAfter:     time.Now().Add(90 * 24 * time.Hour),
		}
		der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
		if err != nil {
			return nil, err
		}
		kder, _ := x509.MarshalPKCS8PrivateKey(key)
		return &localcert.Issued{
			CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
			KeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: kder}),
		}, nil
	}
}

func withLocalTLS(t *testing.T, h *harness) *atomic.Int32 {
	t.Helper()
	calls := &atomic.Int32{}
	h.srv.localTLS = localcert.New(localcert.Options{
		Store:    h.srv.store,
		RootKey:  h.srv.rootKeyOf,
		Issue:    selfSignIssuer(calls),
		OnIssued: h.srv.NotifyChanged,
		Logf:     t.Logf,
	})
	if did, err := h.srv.localTLS.RenewIfNeeded(context.Background()); err != nil || !did {
		t.Fatalf("预签发失败: did=%v err=%v", did, err)
	}
	return calls
}

func (h *harness) syncWith(cert *tls.Certificate, in proto.SyncRequest) proto.SyncResponse {
	h.t.Helper()
	req := &proto.Request{ID: "1", Protocol: proto.Version, Method: proto.MethodSync}
	if err := req.EncodePayload(&in); err != nil {
		h.t.Fatal(err)
	}
	resp := h.postTLS(req, cert)
	if !resp.OK {
		h.t.Fatalf("sync 失败: %s", resp.Error)
	}
	var out proto.SyncResponse
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		h.t.Fatal(err)
	}
	return out
}

func TestSyncDeliversLocalTLSBySerial(t *testing.T) {
	h := newHarnessWithRootKey(t, "1.0.0")
	withLocalTLS(t, h)
	cert := h.registerDevice("dev-a", "MacBook")

	// 设备没有证书：下发，且带长轮询预算时也必须立即返回。
	start := time.Now()
	out := h.syncWith(cert, proto.SyncRequest{WaitSeconds: 20})
	if time.Since(start) > 5*time.Second {
		t.Fatal("有证书待下发时不应挂起长轮询")
	}
	b := out.LocalTLS
	if b == nil || b.Serial == "" || b.Domain != localcert.DefaultDomain {
		t.Fatalf("应下发本机证书: %+v", b)
	}
	if _, err := tls.X509KeyPair([]byte(b.CertPEM), []byte(b.KeyPEM)); err != nil {
		t.Fatalf("下发的证书与私钥不匹配: %v", err)
	}

	// 已持有同一序列号：不再下发。
	out = h.syncWith(cert, proto.SyncRequest{LocalTLSSerial: b.Serial})
	if out.LocalTLS != nil {
		t.Fatal("序列号一致时不应重复下发")
	}
}

func TestSyncWithoutLocalTLSConfigured(t *testing.T) {
	h := newHarnessWithRootKey(t, "1.0.0")
	cert := h.registerDevice("dev-a", "MacBook")
	out := h.syncWith(cert, proto.SyncRequest{})
	if out.LocalTLS != nil {
		t.Fatal("没有签发证书时不应下发")
	}
}

func TestRevokeRotatesLocalTLSKey(t *testing.T) {
	h := newHarnessWithRootKey(t, "1.0.0")
	withLocalTLS(t, h)
	adminCert := h.registerDevice("dev-admin", "Admin")
	cert := h.registerDevice("dev-a", "MacBook")
	first := h.syncWith(cert, proto.SyncRequest{}).LocalTLS
	if first == nil {
		t.Fatal("首次应下发")
	}

	req := &proto.Request{ID: "2", Protocol: proto.Version, Method: proto.MethodRevokeDevice, Source: proto.SourceAdmin}
	if err := req.EncodePayload(&proto.RevokeRequest{DeviceID: "dev-a", Reason: "丢失"}); err != nil {
		t.Fatal(err)
	}
	if resp := h.postTLS(req, adminCert); !resp.OK {
		t.Fatalf("吊销失败: %s", resp.Error)
	}

	// 吊销后后台立即换钥重签。
	//
	// 不能只等 calls>=2：那个计数在签发函数内部就自增了，早于证书写进 SSOT。
	// 于是这里可能在"已经签好"和"还没落盘"之间读到旧证书，偶发失败。
	// 要等的是能观察到的最终状态——序列号真的变了。
	deadline := time.Now().Add(5 * time.Second)
	var cur *localcert.Cert
	for time.Now().Before(deadline) {
		c, err := h.srv.localTLS.Current()
		if err == nil && c != nil && c.Serial != first.Serial {
			cur = c
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if cur == nil {
		c, err := h.srv.localTLS.Current()
		t.Fatalf("吊销后应换钥重签并在 5 秒内落盘，当前: %+v err=%v", c, err)
	}
	if string(cur.KeyPEM) == first.KeyPEM {
		t.Fatal("换钥后私钥应改变")
	}

	// 其余设备下一轮拿到新证书。
	out := h.syncWith(adminCert, proto.SyncRequest{LocalTLSSerial: first.Serial})
	if out.LocalTLS == nil || out.LocalTLS.Serial != cur.Serial {
		t.Fatalf("其余设备应收到新证书: %+v", out.LocalTLS)
	}
}
