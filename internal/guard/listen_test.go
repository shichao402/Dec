package guard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCloudListensBehindNginx 锁定「公网 443 归 nginx，dec-cloud 只监听回环高位端口」。
//
// nginx 以 stream + ssl_preread 按 SNI 透传 TLS 到 dec-cloud，所以：
//   - dec-cloud 不能再绑 :443，否则与 nginx 抢端口，二者必有一个起不来；
//   - systemd 单元不该再授予 CAP_NET_BIND_SERVICE（高位端口用不到，多给就是多余权限）；
//   - 部署健康检查要直连 dec-cloud 的回环端口，而不是 443（那是 nginx）。
func TestCloudListensBehindNginx(t *testing.T) {
	root := repoRoot(t)
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", rel, err)
		}
		return string(b)
	}

	main := read("cmd/dec-cloud/main.go")
	if !strings.Contains(main, `flag.String("listen", "127.0.0.1:8444"`) {
		t.Error("dec-cloud 的 -listen 默认值必须是 127.0.0.1:8444（公网 443 归 nginx）")
	}

	prov := read("deploy/provision.sh")
	if strings.Contains(prov, "CAP_NET_BIND_SERVICE") {
		t.Error("provision.sh 不应再授予 CAP_NET_BIND_SERVICE：dec-cloud 不再绑低位端口")
	}
	if strings.Contains(prov, "-listen :443") {
		t.Error("provision.sh 的 ExecStart 仍在监听 :443，会与 nginx 冲突")
	}
	if !strings.Contains(prov, `LISTEN_ADDR="${DEC_LISTEN_ADDR:-127.0.0.1:8444}"`) {
		t.Error("provision.sh 的默认监听地址必须是 127.0.0.1:8444")
	}

	deploy := read("deploy/dec-cloud-deploy.sh")
	if strings.Contains(deploy, `:443:127.0.0.1"`) {
		t.Error("健康检查仍打到本机 443，那是 nginx 不是 dec-cloud")
	}
	if !strings.Contains(deploy, `--connect-to "${host}:443:${LISTEN_ADDR}"`) {
		t.Error("健康检查必须用 --connect-to 直连 dec-cloud 的回环监听地址")
	}

	// 测试实例不能和生产撞端口。
	if strings.Contains(read("deploy/test-e2e.sh"), `SERVER_PORT="8444"`) {
		t.Error("test-e2e.sh 的测试实例不能使用生产端口 8444")
	}
}
