package cloudsvc

import (
	"fmt"
	"net"
	"strconv"
)

// requireLoopbackListen 校验监听地址是本机回环。
//
// 用于给"信任 PROXY protocol"加一道硬约束：只有当转发者就在本机时，
// 它声明的来源地址才值得相信。若监听在 0.0.0.0 或公网地址，
// 任何能连上来的客户端都可以自报 IP，那会让限流形同虚设。
//
// 空地址视为不合法：它意味着"所有网卡"，正是要拒绝的那种情况。
func requireLoopbackListen(listen string) error {
	if listen == "" {
		return fmt.Errorf("开启 PROXY protocol 时必须显式指定回环监听地址")
	}
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("监听地址 %q 无法解析: %w", listen, err)
	}
	if host == "" {
		return fmt.Errorf("监听地址 %q 未指定主机（等于监听所有网卡），"+
			"开启 PROXY protocol 时必须限定在回环地址", listen)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// 主机名：解析后要求全部是回环。
		addrs, err := net.LookupIP(host)
		if err != nil {
			return fmt.Errorf("监听主机 %q 无法解析: %w", host, err)
		}
		for _, a := range addrs {
			if !a.IsLoopback() {
				return fmt.Errorf("监听主机 %q 解析到非回环地址 %s，"+
					"开启 PROXY protocol 时只允许回环监听", host, a)
			}
		}
		return nil
	}
	if !ip.IsLoopback() {
		return fmt.Errorf("监听地址 %q 不是回环地址，"+
			"开启 PROXY protocol 时只允许 127.0.0.0/8 或 ::1", listen)
	}
	return nil
}

// portOf 取监听地址的端口号，失败返回 0。仅用于测试与日志。
func portOf(listen string) int {
	_, portStr, err := net.SplitHostPort(listen)
	if err != nil {
		return 0
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		return 0
	}
	return p
}
