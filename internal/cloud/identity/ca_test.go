package identity

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"
	"time"
)

func newTestCA(t *testing.T) *CA {
	t.Helper()
	ca, err := NewCA()
	if err != nil {
		t.Fatalf("NewCA: %v", err)
	}
	return ca
}

func newDeviceKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成设备密钥: %v", err)
	}
	return key
}

func TestSignAndVerify(t *testing.T) {
	ca := newTestCA(t)
	devKey := newDeviceKey(t)

	issued, err := ca.SignDevice("dev-abc123", &devKey.PublicKey, time.Time{})
	if err != nil {
		t.Fatalf("SignDevice: %v", err)
	}
	if len(issued.CertPEM) == 0 || issued.Serial == "" || issued.Fingerprint == "" {
		t.Fatalf("签发结果不完整: %+v", issued)
	}
	if !issued.NotAfter.After(time.Now()) {
		t.Fatal("证书应在未来过期")
	}

	deviceID, serial, notAfter, err := ca.VerifyDevice(issued.CertPEM, time.Now())
	if err != nil {
		t.Fatalf("VerifyDevice: %v", err)
	}
	if deviceID != "dev-abc123" {
		t.Fatalf("设备 ID 错误: %s", deviceID)
	}
	if serial != issued.Serial {
		t.Fatalf("序列号不一致: %s != %s", serial, issued.Serial)
	}
	if !notAfter.Equal(issued.NotAfter) {
		t.Fatalf("过期时间不一致: %v != %v", notAfter, issued.NotAfter)
	}
}

func TestVerifyRejectsForeignCert(t *testing.T) {
	ca := newTestCA(t)
	other := newTestCA(t)
	devKey := newDeviceKey(t)
	issued, err := other.SignDevice("dev-x", &devKey.PublicKey, time.Time{})
	if err != nil {
		t.Fatalf("SignDevice: %v", err)
	}
	if _, _, _, err := ca.VerifyDevice(issued.CertPEM, time.Now()); !errors.Is(err, ErrCertInvalid) {
		t.Fatalf("别的 CA 签发的证书应被拒绝，实际 %v", err)
	}
}

func TestVerifyRejectsExpired(t *testing.T) {
	ca := newTestCA(t)
	devKey := newDeviceKey(t)
	issued, err := ca.SignDevice("dev-x", &devKey.PublicKey, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("SignDevice: %v", err)
	}
	if _, _, _, err := ca.VerifyDevice(issued.CertPEM, time.Now()); !errors.Is(err, ErrCertExpired) {
		t.Fatalf("过期证书应报 ErrCertExpired，实际 %v", err)
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	ca := newTestCA(t)
	for _, data := range [][]byte{nil, []byte("not pem"), []byte("-----BEGIN CERTIFICATE-----\nxx\n-----END CERTIFICATE-----\n")} {
		if _, _, _, err := ca.VerifyDevice(data, time.Now()); !errors.Is(err, ErrCertInvalid) {
			t.Fatalf("非法证书应报 ErrCertInvalid，实际 %v", err)
		}
	}
}

func TestLoadCARoundTrip(t *testing.T) {
	ca := newTestCA(t)
	keyPEM, err := ca.KeyPEM()
	if err != nil {
		t.Fatalf("KeyPEM: %v", err)
	}
	restored, err := LoadCA(keyPEM, ca.CertPEM())
	if err != nil {
		t.Fatalf("LoadCA: %v", err)
	}
	devKey := newDeviceKey(t)
	issued, err := restored.SignDevice("dev-x", &devKey.PublicKey, time.Time{})
	if err != nil {
		t.Fatalf("SignDevice: %v", err)
	}
	// 原始 CA 必须能验证重建后签发的证书。
	if _, _, _, err := ca.VerifyDevice(issued.CertPEM, time.Now()); err != nil {
		t.Fatalf("重建的 CA 签发证书无法被原 CA 验证: %v", err)
	}
}

func TestLoadCARejectsBadInput(t *testing.T) {
	ca := newTestCA(t)
	if _, err := LoadCA([]byte("bad"), ca.CertPEM()); !errors.Is(err, ErrCertInvalid) {
		t.Fatalf("坏私钥应报 ErrCertInvalid，实际 %v", err)
	}
	keyPEM, _ := ca.KeyPEM()
	if _, err := LoadCA(keyPEM, []byte("bad")); !errors.Is(err, ErrCertInvalid) {
		t.Fatalf("坏证书应报 ErrCertInvalid，实际 %v", err)
	}
}

func TestParsePublicKey(t *testing.T) {
	devKey := newDeviceKey(t)
	der, err := x509.MarshalPKIXPublicKey(&devKey.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})

	pub, err := ParsePublicKey(pubPEM)
	if err != nil {
		t.Fatalf("ParsePublicKey: %v", err)
	}
	if !pub.Equal(&devKey.PublicKey) {
		t.Fatal("公钥不一致")
	}
	if _, err := ParsePublicKey([]byte("bad")); !errors.Is(err, ErrCertInvalid) {
		t.Fatalf("坏公钥应报 ErrCertInvalid，实际 %v", err)
	}
}

func TestPublicKeyFingerprint(t *testing.T) {
	a := newDeviceKey(t)
	b := newDeviceKey(t)
	fa, err := PublicKeyFingerprint(&a.PublicKey)
	if err != nil {
		t.Fatalf("指纹: %v", err)
	}
	fb, err := PublicKeyFingerprint(&b.PublicKey)
	if err != nil {
		t.Fatalf("指纹: %v", err)
	}
	if fa == fb {
		t.Fatal("不同公钥指纹相同")
	}
	if len(fa) != 64 {
		t.Fatalf("指纹长度错误: %d", len(fa))
	}
	if _, err := PublicKeyFingerprint(nil); err == nil {
		t.Fatal("空公钥应报错")
	}
}

func TestSignDeviceRejectsEmpty(t *testing.T) {
	ca := newTestCA(t)
	devKey := newDeviceKey(t)
	if _, err := ca.SignDevice("", &devKey.PublicKey, time.Time{}); err == nil {
		t.Fatal("空设备 ID 应报错")
	}
	if _, err := ca.SignDevice("dev-x", nil, time.Time{}); err == nil {
		t.Fatal("空公钥应报错")
	}
	var nilCA *CA
	if _, err := nilCA.SignDevice("dev-x", &devKey.PublicKey, time.Time{}); err == nil {
		t.Fatal("空 CA 应报错")
	}
}

func TestSerialsAreUnique(t *testing.T) {
	ca := newTestCA(t)
	devKey := newDeviceKey(t)
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		issued, err := ca.SignDevice("dev-x", &devKey.PublicKey, time.Time{})
		if err != nil {
			t.Fatalf("SignDevice: %v", err)
		}
		if seen[issued.Serial] {
			t.Fatalf("序列号重复: %s", issued.Serial)
		}
		seen[issued.Serial] = true
	}
}

func TestDeviceCertHasClientAuthUsage(t *testing.T) {
	ca := newTestCA(t)
	devKey := newDeviceKey(t)
	issued, err := ca.SignDevice("dev-x", &devKey.PublicKey, time.Time{})
	if err != nil {
		t.Fatalf("SignDevice: %v", err)
	}
	block, _ := pem.Decode(issued.CertPEM)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	hasClientAuth := false
	for _, u := range cert.ExtKeyUsage {
		if u == x509.ExtKeyUsageClientAuth {
			hasClientAuth = true
		}
	}
	if !hasClientAuth {
		t.Fatal("设备证书必须带 ClientAuth 用途")
	}
	if cert.IsCA {
		t.Fatal("设备证书不应是 CA")
	}
}
