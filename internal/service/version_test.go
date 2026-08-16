package service

import "testing"

func TestVersionsMismatch(t *testing.T) {
	cases := []struct {
		client, server string
		want           bool
	}{
		{"v1.2.0", "v1.2.0", false},
		{"1.2.0", "v1.2.0", false},
		{"v1.2.0", "v1.3.0", true},
		{"v1.2.0", "dev", true},
		{"dev", "v1.2.0", true},
		{"dev", "dev", false},
		{"", "v1.0.0", false},
		{"v1.0.0", "", false},
	}
	for _, tc := range cases {
		if got := VersionsMismatch(tc.client, tc.server); got != tc.want {
			t.Fatalf("VersionsMismatch(%q, %q) = %v, want %v", tc.client, tc.server, got, tc.want)
		}
	}
}
