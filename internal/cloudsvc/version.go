package cloudsvc

import (
	"strconv"
	"strings"
)

// compareVersion 比较两个形如 v1.2.3 的版本号。
//
// 返回 -1 表示 a 比 b 旧，0 表示相同，1 表示 a 比 b 新。
// 非法输入一律当作「无法比较」返回 0，让调用方保持原有的宽松行为，
// 而不是因为一个格式问题把设备挡在门外。
//
// 为什么需要它：要求版本（required_client）的语义是「最低可用版本」，
// 不是「必须精确等于」。发布只改服务端时 CI 会沿用上一个要求版本，
// 此时已经把代理升到新版本的设备如果按精确相等判定，就会被判成
// 「版本不对齐」而被拒绝服务——升级反而让设备不可用。
func compareVersion(a, b string) int {
	pa, oka := parseVersion(a)
	pb, okb := parseVersion(b)
	if !oka || !okb {
		return 0
	}
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

// parseVersion 把 v1.2.3 或 1.2.3 拆成三个数字。
func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if v == "" {
		return out, false
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		// 允许 1.2.3-rc1 这类后缀，只取前面的数字部分。
		if idx := strings.IndexFunc(p, func(r rune) bool {
			return r < '0' || r > '9'
		}); idx >= 0 {
			p = p[:idx]
		}
		if p == "" {
			return out, false
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
