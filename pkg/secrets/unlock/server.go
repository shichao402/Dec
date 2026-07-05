package unlock

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

type server struct {
	auth     Authenticator
	onUnlock func(session string)
	done     chan error

	mu          sync.Mutex
	awaiting2FA bool
	closed      bool
	httpServer  *http.Server
}

func newServer(auth Authenticator, onUnlock func(session string)) *server {
	return &server{
		auth:     auth,
		onUnlock: onUnlock,
		done:     make(chan error, 1),
	}
}

func (s *server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /unlock", s.handleIndex)
	mux.HandleFunc("POST /unlock", s.handleUnlock)
	mux.HandleFunc("GET /unlock/2fa", s.handle2FA)
	mux.HandleFunc("POST /unlock/2fa", s.handle2FASubmit)
	return mux
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	awaiting := s.awaiting2FA
	s.mu.Unlock()
	if awaiting {
		http.Redirect(w, r, "/unlock/2fa", http.StatusSeeOther)
		return
	}
	s.render(w, "unlock", pageData{})
}

func (s *server) handleUnlock(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.render(w, "unlock", pageData{Error: "请求无效"})
		return
	}
	password := r.FormValue("password")
	session, need2FA, err := s.auth.Unlock(r.Context(), password)
	if err != nil {
		s.render(w, "unlock", pageData{Error: err.Error()})
		return
	}
	if need2FA {
		s.mu.Lock()
		s.awaiting2FA = true
		s.mu.Unlock()
		http.Redirect(w, r, "/unlock/2fa", http.StatusSeeOther)
		return
	}
	s.complete(session)
	s.render(w, "success", pageData{})
}

func (s *server) handle2FA(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	awaiting := s.awaiting2FA
	s.mu.Unlock()
	if !awaiting {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, "2fa", pageData{})
}

func (s *server) handle2FASubmit(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	awaiting := s.awaiting2FA
	s.mu.Unlock()
	if !awaiting {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.render(w, "2fa", pageData{Error: "请求无效"})
		return
	}
	session, err := s.auth.Verify2FA(r.Context(), r.FormValue("code"))
	if err != nil {
		s.render(w, "2fa", pageData{Error: err.Error()})
		return
	}
	s.complete(session)
	s.render(w, "success", pageData{})
}

func (s *server) complete(session string) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()

	if s.onUnlock != nil {
		s.onUnlock(session)
	}
	select {
	case s.done <- nil:
	default:
	}
	go s.shutdown()
}

func (s *server) fail(err error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	select {
	case s.done <- err:
	default:
	}
	go s.shutdown()
}

func (s *server) shutdown() {
	if s.httpServer != nil {
		_ = s.httpServer.Shutdown(context.Background())
	}
}

func (s *server) render(w http.ResponseWriter, name string, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTemplates.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "页面渲染失败", http.StatusInternalServerError)
	}
}

func (s *server) waitReady(ctx context.Context, baseURL string) error {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	delay := 20 * time.Millisecond
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusSeeOther {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-s.done:
			if err != nil {
				return err
			}
			return fmt.Errorf("解锁服务已关闭")
		case <-time.After(delay):
			if delay < 200*time.Millisecond {
				delay += 20 * time.Millisecond
			}
		}
	}
}

func (s *server) listenAndServe(ctx context.Context, listenAddr string) (baseURL string, err error) {
	if listenAddr == "" {
		listenAddr = "127.0.0.1:0"
	}
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return "", fmt.Errorf("启动本地 HTTP 服务失败: %w", err)
	}
	s.httpServer = &http.Server{Handler: s.routes()}
	go func() {
		if serveErr := s.httpServer.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			s.fail(fmt.Errorf("HTTP 服务异常: %w", serveErr))
		}
	}()
	go func() {
		<-ctx.Done()
		s.fail(ctx.Err())
	}()
	host, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		return "", err
	}
	if host == "" || host == "0.0.0.0" || host == "[::]" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%s/unlock", host, port), nil
}
