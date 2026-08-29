package servicehost

import (
	"fmt"
	"net"
	"strings"

	"github.com/shichao402/Dec/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const defaultListenAddr = "127.0.0.1:0"

type listenSettings struct {
	Addr string
	Opts []grpc.ServerOption
}

func loadListenSettings() (listenSettings, error) {
	cfg, err := config.LoadGlobalConfig()
	if err != nil || cfg == nil {
		return listenSettings{Addr: defaultListenAddr}, nil
	}
	addr := strings.TrimSpace(cfg.ManagementListen)
	if addr == "" {
		addr = defaultListenAddr
	}
	host, _, err := splitHostPortAllowEmptyPort(addr)
	if err != nil {
		return listenSettings{}, fmt.Errorf("management_listen 无效: %w", err)
	}
	cert := strings.TrimSpace(cfg.ManagementTLSCert)
	key := strings.TrimSpace(cfg.ManagementTLSKey)
	loopback := isLoopbackHost(host)
	if !loopback && (cert == "" || key == "") {
		return listenSettings{}, fmt.Errorf("非 loopback 的 management_listen %q 必须同时配置 management_tls_cert 与 management_tls_key", addr)
	}
	settings := listenSettings{Addr: addr}
	if cert != "" || key != "" {
		if cert == "" || key == "" {
			return listenSettings{}, fmt.Errorf("management_tls_cert 与 management_tls_key 必须成对配置")
		}
		creds, err := credentials.NewServerTLSFromFile(cert, key)
		if err != nil {
			return listenSettings{}, fmt.Errorf("加载管理 TLS 证书失败: %w", err)
		}
		settings.Opts = []grpc.ServerOption{grpc.Creds(creds)}
	}
	return settings, nil
}

func splitHostPortAllowEmptyPort(addr string) (host, port string, err error) {
	if !strings.Contains(addr, ":") {
		return addr, "", nil
	}
	return net.SplitHostPort(addr)
}

func isLoopbackHost(host string) bool {
	h := strings.TrimSpace(host)
	if h == "" {
		return false
	}
	if h == "localhost" || h == "127.0.0.1" || h == "::1" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
