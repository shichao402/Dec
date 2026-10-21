package agent

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 管理面的边界（需求 14.1、14.2、14.3）。
//
// 页面本身是静态资源，能测的是三件事：能被取到、自包含、以及
// “写操作都要二次确认、明文都不进列表”这些靠脚本结构保证的约束。

// getPath 用本机 Host 取一个 GET 路径。
func getPath(t *testing.T, l *Loopback, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "127.0.0.1:" + l.port()
	rec := httptest.NewRecorder()
	l.Handler().ServeHTTP(rec, req)
	return rec
}

// TestAdminPageServed 验证管理面页面与脚本都能取到。
func TestAdminPageServed(t *testing.T) {
	l := newTestLoopback(t, LoopbackOptions{})

	rec := getPath(t, l, "/admin")
	if rec.Code != http.StatusOK {
		t.Fatalf("管理面应可读取，得到 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Dec 管理面") {
		t.Fatalf("页面内容异常：%s", body)
	}
	// 六个页签是需求 14.1 列出的全部对象，缺一个就等于少一块功能。
	for _, page := range []string{"overview", "devices", "assets", "secrets", "history", "projects"} {
		if !strings.Contains(body, `data-page="`+page+`"`) {
			t.Fatalf("缺少页签 %s", page)
		}
	}

	js := getPath(t, l, "/admin.js")
	if js.Code != http.StatusOK {
		t.Fatalf("脚本应可读取，得到 %d", js.Code)
	}
	if ct := js.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("脚本 Content-Type 为 %q", ct)
	}
	if !strings.Contains(js.Body.String(), "function api(") {
		t.Fatal("脚本内容异常")
	}
}

// TestAdminPageIsSelfContained 验证页面不依赖任何外部资源。
//
// 代理是下载即跑的单文件程序，页面必须在断网、无 CDN 的环境里也能打开。
// 唯一允许的引用是脚本自己 /admin.js。
func TestAdminPageIsSelfContained(t *testing.T) {
	l := newTestLoopback(t, LoopbackOptions{})
	body := getPath(t, l, "/admin").Body.String()

	for _, bad := range []string{"http://", "https://", "//cdn", "<link rel=\"stylesheet\" href="} {
		if strings.Contains(body, bad) {
			t.Fatalf("页面不应引用外部资源：%s", bad)
		}
	}
	if !strings.Contains(body, `<script src="/admin.js"></script>`) {
		t.Fatal("页面应引用自己的脚本")
	}
}

// TestAdminJSConfirmsEveryWrite 验证脚本里每个写操作都经过二次确认。
//
// 这条约束靠代码结构保证：所有写方法都必须经由 confirmDo 触发。
// 用例的做法是检查每个高危方法体内都出现了 confirmDo，
// 因为这是唯一能在此层发现“新加了一个写按钮但忘了确认”的方式。
func TestAdminJSConfirmsEveryWrite(t *testing.T) {
	writes := map[string]string{
		"setKind":            "set_device_kind",
		"revoke":             "revoke_device",
		"openNewBundle":      "write_asset",
		"addAssetFile":       "newFilePath",
		"saveAssetFile":      "write_asset",
		"revealSecret":       "read_secret",
		"editSecret":         "put_secret",
		"openSecretForm":     "put_secret",
		"removeSecret":       "delete_secret",
		"rollback":           "rollback",
		"addLocalProject":    "/api/projects",
		"removeLocalProject": "/api/projects",
		"addCloudProject":    "put_project",
		"removeCloudProject": "delete_project",
		"editSubscriptions":  "set_subscriptions",
	}
	for fn, method := range writes {
		body := funcBody(t, adminJS, fn)
		if body == "" {
			t.Fatalf("找不到函数 %s", fn)
		}
		if !strings.Contains(body, "confirmDo(") {
			t.Fatalf("%s 没有走二次确认（它调用了 %s）", fn, method)
		}
		if !strings.Contains(body, method) {
			t.Fatalf("%s 里没有调用 %s", fn, method)
		}
	}
}

// TestAdminJSSecretNeverListed 验证明文不会出现在列表里。
//
// 列表会被截图、会被投屏，所以明文只能出现在单独的一块里，
// 并且必须由显式操作带出来。
func TestAdminJSSecretNeverListed(t *testing.T) {
	body := funcBody(t, adminJS, "loadSecrets")
	if strings.Contains(body, "read_secret") {
		t.Fatal("密钥列表不应读取明文")
	}
	if strings.Contains(body, ".value") {
		t.Fatal("密钥列表不应出现明文值")
	}
	// 明文只允许经由 showPlaintext 呈现，理由必须随请求带上去。
	reveal := funcBody(t, adminJS, "revealSecret")
	if !strings.Contains(reveal, "read_secret") {
		t.Fatal("查看明文应调用 read_secret")
	}
	if !strings.Contains(reveal, "reason") {
		t.Fatal("查看明文应带上理由")
	}
}

// TestAdminJSEscapesRenderedContent 验证渲染进页面的内容都过了转义。
func TestAdminJSEscapesRenderedContent(t *testing.T) {
	// 资产名、密钥名、提交说明都来自用户自己写的内容，
	// 直接拼进 innerHTML 会被一个引号搞坏页面结构。
	for _, fn := range []string{"loadBundles", "loadSecrets", "loadDevices", "loadHistory", "loadProjects"} {
		body := funcBody(t, adminJS, fn)
		if body == "" {
			t.Fatalf("找不到函数 %s", fn)
		}
		if !strings.Contains(body, "esc(") {
			t.Fatalf("%s 没有转义渲染内容", fn)
		}
	}
}

// TestAdminRoutesShareGuard 验证管理面同样受 Host 校验保护。
//
// 管理面是最敏感的一块，如果它能被 DNS rebinding 打进来，
// 前面所有“不暴露公网页面”的设计都白做了。
func TestAdminRoutesShareGuard(t *testing.T) {
	l := newTestLoopback(t, LoopbackOptions{})
	for _, path := range []string{"/admin", "/admin.js"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "evil.example.com:" + l.port()
		rec := httptest.NewRecorder()
		l.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s 在外部 Host 下应被拒绝，得到 %d", path, rec.Code)
		}
	}
}

// TestAdminRejectsNonGet 验证管理面不接受写方法。
func TestAdminRejectsNonGet(t *testing.T) {
	l := newTestLoopback(t, LoopbackOptions{})
	for _, path := range []string{"/admin", "/admin.js"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		req.Host = "127.0.0.1:" + l.port()
		rec := httptest.NewRecorder()
		l.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s 的 POST 应被拒绝，得到 %d", path, rec.Code)
		}
	}
}

// funcBody 粗略截取一个 JS 函数的函数体。
//
// 不引 JS 解析器：这里只需要“这个方法里有没有出现某个调用”，
// 用括号配对截取到函数结束就够了。
func funcBody(t *testing.T, src, name string) string {
	t.Helper()
	head := "function " + name + "("
	i := strings.Index(src, head)
	if i < 0 {
		return ""
	}
	start := strings.Index(src[i:], "{")
	if start < 0 {
		return ""
	}
	start += i
	depth := 0
	for j := start; j < len(src); j++ {
		switch src[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start : j+1]
			}
		}
	}
	return ""
}
