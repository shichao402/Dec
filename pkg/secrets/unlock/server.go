package unlock

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"
)

type server struct {
	auth         Authenticator
	onUnlock     func(session string)
	onEmailSaved func(email string) error
	initialEmail string
	pendingEmail string
	done         chan error

	mu          sync.Mutex
	awaiting2FA bool
	closed      bool
	httpServer  *http.Server
}

func newServer(auth Authenticator, initialEmail string, onUnlock func(session string), onEmailSaved func(email string) error) *server {
	return &server{
		auth:         auth,
		initialEmail: strings.TrimSpace(initialEmail),
		onUnlock:     onUnlock,
		onEmailSaved: onEmailSaved,
		done:         make(chan error, 1),
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
	s.render(w, "unlock", pageData{Email: s.initialEmail})
}

func (s *server) handleUnlock(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.render(w, "unlock", pageData{Error: "请求无效", Email: s.initialEmail})
		return
	}
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	if email == "" {
		s.render(w, "unlock", pageData{Error: "邮箱不能为空"})
		return
	}
	session, need2FA, err := s.auth.Unlock(r.Context(), email, password)
	if err != nil {
		s.render(w, "unlock", pageData{Error: err.Error(), Email: email})
		return
	}
	if need2FA {
		s.mu.Lock()
		s.awaiting2FA = true
		s.pendingEmail = email
		s.mu.Unlock()
		http.Redirect(w, r, "/unlock/2fa", http.StatusSeeOther)
		return
	}
	if err := s.persistEmail(email); err != nil {
		s.render(w, "unlock", pageData{Error: err.Error(), Email: email})
		return
	}
	s.complete(session)
	s.render(w, "success", pageData{})
}

func (s *server) persistEmail(email string) error {
	if s.onEmailSaved == nil {
		return nil
	}
	return s.onEmailSaved(email)
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
	rememberDevice := r.FormValue("remember") == "1" || r.FormValue("remember") == "on"
	session, err := s.auth.Verify2FA(r.Context(), r.FormValue("code"), rememberDevice)
	if err != nil {
		s.render(w, "2fa", pageData{Error: err.Error()})
		return
	}
	if err := s.persistEmail(s.pendingEmail); err != nil {
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

func isAddrInUse(err error) bool {
	var opErr *net.OpError
	if !errors.As(err, &opErr) {
		return false
	}
	var errno syscall.Errno
	return errors.As(opErr.Err, &errno) && errno == syscall.EADDRINUSE
}

func resolveListenAddrs(listenAddr string) []string {
	if listenAddr != "" {
		return []string{listenAddr}
	}
	return []string{
		fmt.Sprintf("127.0.0.1:%d", DefaultUnlockPort),
		"127.0.0.1:0",
	}
}

func listenTCP(addrs ...string) (net.Listener, error) {
	var lastErr error
	for _, addr := range addrs {
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			return ln, nil
		}
		lastErr = err
		if len(addrs) > 1 && isAddrInUse(err) {
			continue
		}
		return nil, err
	}
	return nil, lastErr
}

func (s *server) listenAndServe(ctx context.Context, listenAddr string) (baseURL string, err error) {
	ln, err := listenTCP(resolveListenAddrs(listenAddr)...)
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
