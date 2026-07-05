package unlock

import (
	"context"
	"fmt"
)

// Options 配置 web unlock 流程。
type Options struct {
	Authenticator Authenticator
	OpenBrowser   BrowserOpener
	ListenAddr    string
	OnSession     func(session string)
	// OnReady 在 HTTP 服务就绪后、尝试打开浏览器前回调（可用于展示手动打开链接）。
	OnReady func(url string)
	// OnBrowserError 在自动打开浏览器失败时回调；流程仍继续等待用户手动访问。
	OnBrowserError func(err error)
}

// Run 启动本地 HTTP 解锁服务并阻塞至成功、失败或 ctx 取消。
func Run(ctx context.Context, opts Options) error {
	if opts.Authenticator == nil {
		return fmt.Errorf("unlock: 缺少 Authenticator")
	}
	opener := opts.OpenBrowser
	if opener == nil {
		opener = defaultBrowserOpener
	}

	onSession := opts.OnSession
	srv := newServer(opts.Authenticator, func(session string) {
		if onSession != nil {
			onSession(session)
		}
	})

	baseURL, err := srv.listenAndServe(ctx, opts.ListenAddr)
	if err != nil {
		return err
	}
	if err := srv.waitReady(ctx, baseURL); err != nil {
		srv.shutdown()
		return err
	}
	if opts.OnReady != nil {
		opts.OnReady(baseURL)
	}
	if err := opener(baseURL); err != nil {
		if opts.OnBrowserError != nil {
			opts.OnBrowserError(err)
		}
	}

	select {
	case <-ctx.Done():
		srv.shutdown()
		return ctx.Err()
	case err := <-srv.done:
		if err != nil {
			return err
		}
		return nil
	}
}
