package release

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"testing"
)

func testKey(t *testing.T, b byte) (seed, pub string) {
	t.Helper()
	s := make([]byte, ed25519.SeedSize)
	for i := range s {
		s[i] = b
	}
	k := ed25519.NewKeyFromSeed(s)
	return base64.StdEncoding.EncodeToString(s),
		base64.StdEncoding.EncodeToString(k.Public().(ed25519.PublicKey))
}

func TestSignVerifyRoundTrip(t *testing.T) {
	seed, pub := testKey(t, 1)
	data := []byte(`{"version":"v1.2.3","required_client":"v1.2.0","files":[{"name":"a","size":1}]}`)
	sig, err := Sign(seed, data)
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseVerified([]string{pub}, data, sig)
	if err != nil {
		t.Fatalf("应验签通过: %v", err)
	}
	if m.Version != "v1.2.3" || m.RequiredClient != "v1.2.0" {
		t.Fatalf("解析结果不对: %+v", m)
	}
	if _, ok := m.Find("a"); !ok {
		t.Fatal("应找到产物 a")
	}
}

func TestVerifyRejectsTamperAndWrongKey(t *testing.T) {
	seed, pub := testKey(t, 1)
	_, other := testKey(t, 2)
	data := []byte(`{"version":"v1.2.3"}`)
	sig, _ := Sign(seed, data)

	if err := Verify([]string{pub}, []byte(`{"version":"v9.9.9"}`), sig); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("篡改后应失败，实际 %v", err)
	}
	if err := Verify([]string{other}, data, sig); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("错误公钥应失败，实际 %v", err)
	}
	if err := Verify([]string{"不是base64", other, pub}, data, sig); err != nil {
		t.Fatalf("列表中任一把对上即应通过（轮换），实际 %v", err)
	}
	if err := Verify([]string{pub}, data, "garbage"); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("签名格式错误应失败，实际 %v", err)
	}
}

func TestSignRejectsBadSeed(t *testing.T) {
	if _, err := Sign("c2hvcnQ=", []byte("x")); err == nil {
		t.Fatal("长度不对的种子应报错")
	}
}

func TestTrustedKeysValid(t *testing.T) {
	if len(TrustedKeys) == 0 {
		t.Fatal("TrustedKeys 不能为空：否则所有设备都会拒绝更新")
	}
	for _, k := range TrustedKeys {
		b, err := base64.StdEncoding.DecodeString(k)
		if err != nil || len(b) != ed25519.PublicKeySize {
			t.Fatalf("公钥 %q 不是 32 字节 base64", k)
		}
	}
}

func TestParseAndCompareVersion(t *testing.T) {
	for _, v := range []string{"v1.2.3", "1.2.3", "v10.0.0"} {
		if _, ok := ParseVersion(v); !ok {
			t.Fatalf("%s 应能解析", v)
		}
	}
	for _, v := range []string{"dev", "v1.2", "v1.2.3-rc1", "v1..3", "v1.2.3.4", ""} {
		if _, ok := ParseVersion(v); ok {
			t.Fatalf("%q 不应能解析", v)
		}
	}
	cases := []struct {
		a, b string
		want int
	}{
		{"v2.1.47", "v2.1.48", -1},
		{"v2.1.48", "v2.1.48", 0},
		{"v2.10.0", "v2.9.9", 1},
	}
	for _, c := range cases {
		if got, ok := CompareVersion(c.a, c.b); !ok || got != c.want {
			t.Fatalf("Compare(%s,%s)=%d,%v 想要 %d", c.a, c.b, got, ok, c.want)
		}
	}
	if _, ok := CompareVersion("dev", "v1.0.0"); ok {
		t.Fatal("dev 不应可比较")
	}
}
