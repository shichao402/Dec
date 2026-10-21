package prune

import (
	"testing"
	"time"
)

func TestOptionsFromEnvDefaults(t *testing.T) {
	opts, err := OptionsFromEnv(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatalf("无环境变量应无错误: %v", err)
	}
	if opts.Retention != 0 || opts.MaxBytes != 0 {
		t.Fatalf("零值应保留, got %+v", opts)
	}
}

func TestOptionsFromEnvParses(t *testing.T) {
	env := map[string]string{
		envRetention: "168h",
		envMaxBytes:  "50M",
	}
	opts, err := OptionsFromEnv(func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if opts.Retention != 7*24*time.Hour {
		t.Fatalf("保留期不符: %v", opts.Retention)
	}
	if opts.MaxBytes != 50<<20 {
		t.Fatalf("容量不符: %d", opts.MaxBytes)
	}
}

func TestOptionsFromEnvRejectsBadRetention(t *testing.T) {
	env := map[string]string{envRetention: "not-a-duration"}
	_, err := OptionsFromEnv(func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	})
	if err == nil {
		t.Fatal("坏保留期应报错")
	}
}

func TestParseBytes(t *testing.T) {
	cases := []struct {
		raw  string
		want int64
	}{
		{"1024", 1024},
		{"1k", 1 << 10},
		{"2K", 2 << 10},
		{"5M", 5 << 20},
		{"1G", 1 << 30},
		{"3MiB", 3 << 20},
		{"8b", 8},
	}
	for _, tc := range cases {
		got, err := parseBytes(tc.raw)
		if err != nil {
			t.Fatalf("parseBytes(%q) 错误: %v", tc.raw, err)
		}
		if got != tc.want {
			t.Fatalf("parseBytes(%q)=%d want %d", tc.raw, got, tc.want)
		}
	}
}

func TestParseBytesRejectsGarbage(t *testing.T) {
	for _, raw := range []string{"", "abc", "1.2.3", "10X"} {
		if _, err := parseBytes(raw); err == nil {
			t.Fatalf("parseBytes(%q) 应报错", raw)
		}
	}
}

func TestParseRetention(t *testing.T) {
	if d, _ := parseRetention("336h"); d != 14*24*time.Hour {
		t.Fatalf("duration 形式不符: %v", d)
	}
	if d, _ := parseRetention("48"); d != 48*time.Hour {
		t.Fatalf("小时数形式不符: %v", d)
	}
	if _, err := parseRetention("-3h"); err == nil {
		t.Fatal("负保留期应报错")
	}
}
