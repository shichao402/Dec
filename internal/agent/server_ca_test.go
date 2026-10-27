package agent

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/proto"
)

// 服务端 CA 恢复与重新登记的代理侧行为（ADR 0052）。

// caFakeCloud 只处理 recover 与 enroll，记下代理发过来的设备公钥。
type caFakeCloud struct {
	mu      sync.Mutex
	gotPubs []string
	gotReq  []proto.RecoverRequest
	// certFor 为收到的公钥签一张证书（测试里用本机身份的证书凑数即可）。
	cert     string
	deviceID string
	// recoverResp 是 recover 的返回；recoverErr 非空时返回协议错误。
	recoverResp proto.RecoverResponse
	recoverErr  proto.ErrorCode
}

func (f *caFakeCloud) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req proto.Request
		_ = json.Unmarshal(raw, &req)
		f.mu.Lock()
		defer f.mu.Unlock()
		var payload any
		switch req.Method {
		case proto.MethodRecover:
			var in proto.RecoverRequest
			_ = json.Unmarshal(req.Payload, &in)
			f.gotReq = append(f.gotReq, in)
			f.gotPubs = append(f.gotPubs, in.DevicePublicKey)
			if f.recoverErr != "" {
				_ = json.NewEncoder(w).Encode(proto.Fail(req.ID, f.recoverErr, "测试错误"))
				return
			}
			payload = f.recoverResp
		case proto.MethodEnroll:
			var in proto.EnrollBeginRequest
			_ = json.Unmarshal(req.Payload, &in)
			f.gotPubs = append(f.gotPubs, in.DevicePublicKey)
			payload = proto.EnrollBeginResponse{
				DeviceID: f.deviceID, Certificate: f.cert, CertNotAfter: time.Now().Add(time.Hour),
			}
		default:
			_ = json.NewEncoder(w).Encode(proto.Fail(req.ID, proto.ErrInvalid, "unexpected"))
			return
		}
		data, _ := json.Marshal(payload)
		_ = json.NewEncoder(w).Encode(&proto.Response{ID: req.ID, OK: true, Payload: data})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func caTestClient(t *testing.T, srv *httptest.Server, store *IdentityStore) *Client {
	t.Helper()
	c, err := NewClient(ClientOptions{BaseURL: srv.URL, Version: "1.0.0", Identity: store.Load2, Insecure: true})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// TestRecoverReusesExistingDeviceKey 本机已有身份时，恢复必须沿用原设备密钥，不能悄悄换掉。
//
// 换了新密钥，服务端就认不出这是同一台设备：CA 重建后会多出一条重复设备、
// 原订阅全丢；CA 从 Bitwarden 取回时更糟——本机原本有效的证书被白白作废。
func TestRecoverReusesExistingDeviceKey(t *testing.T) {
	store := NewIdentityStore(t.TempDir())
	id := selfSignedIdentity(t, "dev-1")
	if err := store.Save(id); err != nil {
		t.Fatalf("Save: %v", err)
	}
	wantPub, err := PublicKeyFromPrivate(id.KeyPEM)
	if err != nil {
		t.Fatalf("PublicKeyFromPrivate: %v", err)
	}
	cloud := &caFakeCloud{recoverResp: proto.RecoverResponse{CARestored: true}}
	srv := cloud.server(t)

	out, err := Recover(context.Background(), caTestClient(t, srv, store), store, RecoverInput{
		EnrollInput: EnrollInput{Email: "me@example.com", Password: "pw"},
	})
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(cloud.gotPubs) != 1 || cloud.gotPubs[0] != wantPub {
		t.Fatal("恢复应发送本机原设备公钥")
	}
	if !out.CARestored || out.Enrolled {
		t.Fatalf("只取回 CA 时不应改动本机身份: %+v", out)
	}
	got, err := store.Load()
	if err != nil || got.CertPEM != id.CertPEM || got.KeyPEM != id.KeyPEM {
		t.Fatal("只取回 CA 时本机身份必须原样保留")
	}
}

// TestRecoverPassesDeclareFlag 宣告丢失的确认必须如实传给服务端。
func TestRecoverPassesDeclareFlag(t *testing.T) {
	store := NewIdentityStore(t.TempDir())
	cloud := &caFakeCloud{recoverResp: proto.RecoverResponse{CARebuilt: true, ReenrollDevices: 2}}
	srv := cloud.server(t)
	out, err := Recover(context.Background(), caTestClient(t, srv, store), store, RecoverInput{
		EnrollInput:   EnrollInput{Email: "me@example.com", Password: "pw"},
		DeclareCALost: true,
	})
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(cloud.gotReq) != 1 || !cloud.gotReq[0].DeclareCALost {
		t.Fatal("DeclareCALost 未传给服务端")
	}
	if !out.CARebuilt || out.ReenrollDevices != 2 {
		t.Fatalf("应带回重建结果: %+v", out)
	}
}

// TestRecoverNotInBackupKeepsIdentity 服务端找不到 CA 条目时，返回可识别的错误码，本机身份不动。
func TestRecoverNotInBackupKeepsIdentity(t *testing.T) {
	store := NewIdentityStore(t.TempDir())
	id := selfSignedIdentity(t, "dev-1")
	_ = store.Save(id)
	cloud := &caFakeCloud{recoverErr: proto.ErrCANotInBackup}
	srv := cloud.server(t)
	_, err := Recover(context.Background(), caTestClient(t, srv, store), store, RecoverInput{
		EnrollInput: EnrollInput{Email: "me@example.com", Password: "pw"},
	})
	if CodeOf(err) != proto.ErrCANotInBackup {
		t.Fatalf("应透出 %s，实际 %v", proto.ErrCANotInBackup, err)
	}
	if got, _ := store.Load(); got == nil || got.CertPEM != id.CertPEM {
		t.Fatal("恢复失败时不得清掉本机身份")
	}
}

// TestReenrollUsesOriginalKey 重新登记用原设备密钥，保存新证书但私钥不变。
func TestReenrollUsesOriginalKey(t *testing.T) {
	store := NewIdentityStore(t.TempDir())
	old := selfSignedIdentity(t, "dev-1")
	_ = store.Save(old)
	wantPub, _ := PublicKeyFromPrivate(old.KeyPEM)

	// 新证书必须和原私钥配对，否则 TLSCertificate 校验会拒收：用同一把私钥再签一张。
	fresh := selfSignedIdentityWithKey(t, "dev-1", old.KeyPEM)
	cloud := &caFakeCloud{cert: fresh, deviceID: "dev-1"}
	srv := cloud.server(t)

	res, err := Reenroll(context.Background(), caTestClient(t, srv, store), store, EnrollInput{
		Email: "me@example.com", Password: "pw",
	})
	if err != nil {
		t.Fatalf("Reenroll: %v", err)
	}
	if res.DeviceID != "dev-1" || len(cloud.gotPubs) != 1 || cloud.gotPubs[0] != wantPub {
		t.Fatal("应用原设备公钥重新登记并沿用原 ID")
	}
	got, _ := store.Load()
	if got.KeyPEM != old.KeyPEM || got.CertPEM != fresh {
		t.Fatal("应保存新证书、保留原私钥")
	}
}

// TestReenrollWithoutIdentityRefuses 没有原身份时不能走重新登记。
func TestReenrollWithoutIdentityRefuses(t *testing.T) {
	store := NewIdentityStore(t.TempDir())
	cloud := &caFakeCloud{}
	srv := cloud.server(t)
	if _, err := Reenroll(context.Background(), caTestClient(t, srv, store), store, EnrollInput{
		Email: "me@example.com", Password: "pw",
	}); err == nil || !strings.Contains(err.Error(), "原设备密钥") {
		t.Fatalf("应拒绝并说明原因，实际 %v", err)
	}
}

// TestCAErrorCodesMapToSpecificErrors 三种服务端状态各有专门的错误与提示，而不是笼统的"身份被拒"。
func TestCAErrorCodesMapToSpecificErrors(t *testing.T) {
	cases := []struct {
		code proto.ErrorCode
		want error
	}{
		{proto.ErrCAPendingRestore, ErrServerCAPending},
		{proto.ErrDeviceNeedsReenroll, ErrServerIdentityReset},
		{proto.ErrRecoveryMode, ErrServerRecovery},
	}
	for _, c := range cases {
		err := errorFromResponse(&proto.Response{Code: c.code, Error: "x"})
		if !errors.Is(err, c.want) {
			t.Errorf("%s 应映射为 %v，实际 %v", c.code, c.want, err)
		}
		if CodeOf(err) != c.code {
			t.Errorf("%s 的错误码应能原样取回，实际 %s", c.code, CodeOf(err))
		}
		if errors.Is(err, ErrIdentityRejected) {
			t.Errorf("%s 不应被当成普通的身份被拒", c.code)
		}
	}
}

// selfSignedIdentityWithKey 用已有私钥再签一张自签证书，返回证书 PEM。
//
// 重新登记的要点是"私钥不变、证书更新"，测试必须让新证书与原私钥真的配对。
func selfSignedIdentityWithKey(t *testing.T, deviceID, keyPEM string) string {
	t.Helper()
	block, _ := pem.Decode([]byte(keyPEM))
	if block == nil {
		t.Fatal("私钥无法解析")
	}
	anyKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("解析私钥: %v", err)
	}
	key := anyKey.(*ecdsa.PrivateKey)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano() + 1),
		Subject:      pkix.Name{CommonName: deviceID},
		DNSNames:     []string{deviceID},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("签发证书: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
