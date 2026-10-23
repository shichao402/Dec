package cloudsvc

import "testing"

// 版本对齐判定的测试（需求 19.4、19.2）。
//
// 这块出过一次真实故障：发布 v2.1.5 时只改了服务端，CI 按需求 19.2 沿用
// 上一个要求版本 v2.1.4；代理已经升到 v2.1.5，按「精确相等」判定就成了
// 「版本不对齐」，升级反而让设备不可用。要求版本的语义是「最低可用版本」，
// 所以这里把方向和边界都固定下来。

func TestCompareVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v2.1.5", "v2.1.4", 1},
		{"v2.1.4", "v2.1.4", 0},
		{"v2.1.3", "v2.1.4", -1},
		// 数字比较而非字符串比较：v2.10.0 比 v2.9.9 新。
		{"v2.10.0", "v2.9.9", 1},
		{"v2.9.9", "v2.10.0", -1},
		// 带不带 v 前缀等价。
		{"1.2.3", "v1.2.3", 0},
		// 预发布后缀只取数字部分比较。
		{"v2.1.5-rc1", "v2.1.5", 0},
	}
	for _, c := range cases {
		if got := compareVersion(c.a, c.b); got != c.want {
			t.Errorf("compareVersion(%q, %q) = %d，期望 %d", c.a, c.b, got, c.want)
		}
	}
}

// 格式不对时不能把设备挡在门外：返回 0（无法比较）比返回「更低」安全。
func TestCompareVersionMalformed(t *testing.T) {
	for _, c := range []struct{ a, b string }{
		{"", "v2.1.4"},
		{"2.1", "v2.1.4"},
		{"vx.y.z", "v2.1.4"},
		{"dev", "v2.1.4"},
	} {
		if got := compareVersion(c.a, c.b); got != 0 {
			t.Errorf("compareVersion(%q, %q) = %d，非法输入应返回 0", c.a, c.b, got)
		}
	}
}

// TestAlignedAcceptsNewerAgent 是这次故障的直接回归。
func TestAlignedAcceptsNewerAgent(t *testing.T) {
	h := newHarness(t, "v2.1.4")

	cases := []struct {
		version string
		want    bool
		why     string
	}{
		{"v2.1.4", true, "等于要求版本"},
		{"v2.1.5", true, "高于要求版本（只改服务端的发布，代理已升级）"},
		{"v2.2.0", true, "更高的次版本"},
		{"v2.1.3", false, "低于要求版本，必须拦住"},
		{"v1.9.9", false, "远低于要求版本"},
		{"", true, "未上报版本时放行，由其它检查兜底"},
	}
	for _, c := range cases {
		if got := h.srv.aligned(c.version); got != c.want {
			t.Errorf("aligned(%q) = %v，期望 %v（%s）", c.version, got, c.want, c.why)
		}
	}
}

// TestAlignedFallsBackToServerVersion 没有显式配置要求版本时，
// 构造阶段会用 server version 兜底，因此低于它的设备仍会被拦。
//
// 这条不是为了限制升级，而是为了兜住「老代理连新服务端」：服务端协议可能
// 已经变了，而配置里没写要求版本。所以兜底值必须真的生效。
func TestAlignedFallsBackToServerVersion(t *testing.T) {
	h := newHarness(t, "") // harness 的 ServerVersion 是 1.0.0
	if h.srv.cfg.RequiredVersion != h.srv.cfg.ServerVersion {
		t.Fatalf("未配置要求版本时应回退到 server version，实际 %q", h.srv.cfg.RequiredVersion)
	}
	if !h.srv.aligned(h.srv.cfg.ServerVersion) {
		t.Error("等于 server version 时应判为对齐")
	}
	if h.srv.aligned("v0.0.1") {
		t.Error("低于兜底要求版本时应判为不对齐")
	}
}
