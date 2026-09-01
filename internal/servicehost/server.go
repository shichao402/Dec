package servicehost

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/shichao402/Dec/internal/app"
	"github.com/shichao402/Dec/internal/compat"
	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/diag"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/service"
	servicev1 "github.com/shichao402/Dec/schema/gen/go/service/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const defaultIdleTimeout = 30 * time.Minute

type Server struct {
	servicev1.UnimplementedDecServiceServer

	version          string
	instanceID       string
	listenToken      string
	controlMu        sync.Mutex
	controlTokens    map[string]time.Time
	broker           *operationBroker
	presence         *presenceTracker
	requestStop      func()
	machineMu        sync.Mutex
	repairedProjects sync.Map
}

func (s *Server) ensureProjectRepaired(projectRoot string, reporter app.Reporter) {
	if strings.TrimSpace(projectRoot) == "" {
		return
	}
	// 设备级置备的 project_root 是合成键（device:<alias>），不是项目路径，
	// 不能拿它去跑项目兼容修复。
	if app.IsDeviceOperationKey(projectRoot) {
		return
	}
	key := projectKey(projectRoot)
	if _, loaded := s.repairedProjects.LoadOrStore(key, struct{}{}); loaded {
		return
	}
	for _, note := range compat.RepairOnStartup(projectRoot) {
		if reporter != nil {
			reporter.Emit(app.OperationEvent{
				Time:    time.Now(),
				Level:   app.EventInfo,
				Scope:   "compat",
				Message: note,
			})
		}
	}
}

func Run(ctx context.Context, version string) error {
	lock, err := service.AcquireServerLock()
	if err != nil {
		return err
	}
	defer lock.Unlock()

	token, err := service.NewToken()
	if err != nil {
		return err
	}
	listen, err := loadListenSettings()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", listen.Addr)
	if err != nil {
		return fmt.Errorf("监听本机服务端口失败: %w", err)
	}
	defer listener.Close()

	idleTimeout := loadIdleTimeout()
	stopRequested := make(chan struct{}, 1)
	host := &Server{
		version:       version,
		instanceID:    fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().UnixMilli()),
		broker:        newOperationBroker(),
		listenToken:   token,
		controlTokens: map[string]time.Time{},
	}
	host.requestStop = func() {
		select {
		case stopRequested <- struct{}{}:
		default:
		}
	}
	host.presence = newPresenceTracker(idleTimeout, func() {
		host.requestStop()
	})

	serverOpts := []grpc.ServerOption{
		grpc.UnaryInterceptor(host.unaryAuth()),
		grpc.StreamInterceptor(host.streamAuth()),
	}
	serverOpts = append(serverOpts, listen.Opts...)
	grpcServer := grpc.NewServer(serverOpts...)
	servicev1.RegisterDecServiceServer(grpcServer, host)
	metadataPath, err := service.WriteMetadata(listener.Addr().String(), token)
	if err != nil {
		return err
	}
	defer service.RemoveMetadataAt(metadataPath)

	go func() {
		for _, note := range compat.RepairOnStartup("") {
			diag.StartupLog("compat: %s", note)
		}
		pruneOrphanWorktreesAtStartup()
	}()

	errCh := make(chan error, 1)
	go func() { errCh <- grpcServer.Serve(listener) }()
	select {
	case <-ctx.Done():
		grpcServer.GracefulStop()
		return ctx.Err()
	case <-stopRequested:
		// KeepAlive 等长连接会拖住 GracefulStop；更新/手动重启需尽快退出。
		done := make(chan struct{})
		go func() {
			grpcServer.GracefulStop()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			grpcServer.Stop()
		}
		return nil
	case err := <-errCh:
		return err
	}
}

// pruneOrphanWorktreesAtStartup 在服务启动时后台回收上次异常退出残留的事务工作树。
// 单例锁保证此刻无其他实例、无活跃事务，清理是安全的。
func pruneOrphanWorktreesAtStartup() {
	removed, err := repo.PruneOrphanWorktrees()
	if err != nil {
		diag.StartupLog("pruneOrphanWorktrees error: %v", err)
		return
	}
	if removed > 0 {
		diag.StartupLog("pruneOrphanWorktrees removed=%d", removed)
	}
}

func loadIdleTimeout() time.Duration {
	cfg, err := config.LoadGlobalConfig()
	if err != nil || strings.TrimSpace(cfg.ServerIdleTimeout) == "" {
		return defaultIdleTimeout
	}
	d, err := time.ParseDuration(cfg.ServerIdleTimeout)
	if err != nil || d <= 0 {
		return defaultIdleTimeout
	}
	return d
}

func (s *Server) Ping(context.Context, *servicev1.PingRequest) (*servicev1.PingResponse, error) {
	return &servicev1.PingResponse{
		Version:    s.version,
		InstanceId: s.instanceID,
		Unlocked:   secrets.InstanceUnlocked(),
	}, nil
}

func (s *Server) Authenticate(ctx context.Context, req *servicev1.AuthenticateRequest) (*servicev1.AuthenticateResponse, error) {
	result, err := secrets.UnlockWithPassword(ctx, req.GetEmail(), req.GetPassword(), req.GetTotp(), req.GetRememberDevice())
	if err != nil {
		return &servicev1.AuthenticateResponse{Error: err.Error()}, nil
	}
	if result != nil && result.Need2FA {
		return &servicev1.AuthenticateResponse{Need_2Fa: true}, nil
	}
	token, expires := s.issueControlToken()
	return &servicev1.AuthenticateResponse{
		Unlocked:     true,
		ControlToken: token,
		ExpiresInMs:  expires,
	}, nil
}

func (s *Server) Shutdown(context.Context, *servicev1.ShutdownRequest) (*servicev1.ShutdownResponse, error) {
	if op := s.broker.firstActive(); op != nil && op.Active {
		return nil, status.Errorf(codes.FailedPrecondition,
			"dec-server 正执行 %s（%s），请等待完成后再重启", op.Operation, op.Facade)
	}
	if s.requestStop != nil {
		s.requestStop()
	}
	return &servicev1.ShutdownResponse{Accepted: true}, nil
}

// KeepAlive 是 TUI 门面持有的长连流：其存活期由 streamAuth 拦截器计入 presence，
// 因此服务在 TUI 打开期间不会空闲退出。MCP 门面不持有此流，只在具体 RPC 执行期间占用 presence。
func (s *Server) KeepAlive(stream grpc.BidiStreamingServer[servicev1.KeepAliveRequest, servicev1.KeepAliveResponse]) error {
	for {
		if _, err := stream.Recv(); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if err := stream.Send(&servicev1.KeepAliveResponse{ServerTimeUnixMs: time.Now().UnixMilli()}); err != nil {
			return err
		}
	}
}

func (s *Server) GetActiveOperation(_ context.Context, req *servicev1.GetActiveOperationRequest) (*servicev1.GetActiveOperationResponse, error) {
	return &servicev1.GetActiveOperationResponse{Operation: s.broker.active(req.ProjectRoot)}, nil
}

func (s *Server) WatchOperation(req *servicev1.WatchOperationRequest, stream grpc.ServerStreamingServer[servicev1.WatchOperationResponse]) error {
	history, live, cancel, err := s.broker.subscribe(req.ProjectRoot, req.OperationId)
	if err != nil {
		return status.Error(codes.NotFound, err.Error())
	}
	defer cancel()
	for _, message := range history {
		if err := stream.Send(message); err != nil {
			return err
		}
	}
	if live == nil {
		return nil
	}
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case message, ok := <-live:
			if !ok {
				return nil
			}
			if err := stream.Send(message); err != nil {
				return err
			}
		}
	}
}

const lockedRPCMessage = "dec-server 已锁定，请先通过 Authenticate 使用主密码解锁"

func methodAllowedWhenLocked(fullMethod string) bool {
	switch fullMethod {
	case servicev1.DecService_Ping_FullMethodName, servicev1.DecService_Authenticate_FullMethodName:
		return true
	default:
		return false
	}
}

func (s *Server) unaryAuth() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := s.authorizeRPC(ctx, info.FullMethod); err != nil {
			return nil, err
		}
		s.presence.connected()
		defer s.presence.disconnected()
		return handler(ctx, req)
	}
}

func (s *Server) streamAuth() grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := s.authorizeRPC(stream.Context(), info.FullMethod); err != nil {
			return err
		}
		s.presence.connected()
		defer s.presence.disconnected()
		return handler(srv, stream)
	}
}

func (s *Server) authorizeRPC(ctx context.Context, fullMethod string) error {
	if methodAllowedWhenLocked(fullMethod) {
		return nil
	}
	if !s.validTransportToken(ctx) {
		return status.Error(codes.Unauthenticated, "invalid dec-server token")
	}
	if !secrets.InstanceUnlocked() {
		// Invoke / RunOperation 是通用调度 RPC，是否允许在锁定态执行必须继续由
		// handler 根据具体 method / operation 精确判断，不能在这里整条放开。
		switch fullMethod {
		case servicev1.DecService_Invoke_FullMethodName, servicev1.DecService_RunOperation_FullMethodName:
			return nil
		default:
			return status.Error(codes.FailedPrecondition, lockedRPCMessage)
		}
	}
	return nil
}

func (s *Server) validTransportToken(ctx context.Context) bool {
	values := metadata.ValueFromIncomingContext(ctx, service.TokenHeader)
	if len(values) != 1 {
		return false
	}
	got := values[0]
	if got != "" && got == s.listenToken {
		return true
	}
	now := time.Now()
	s.controlMu.Lock()
	defer s.controlMu.Unlock()
	exp, ok := s.controlTokens[got]
	if !ok {
		return false
	}
	if !now.Before(exp) {
		delete(s.controlTokens, got)
		return false
	}
	return true
}

func (s *Server) issueControlToken() (string, int64) {
	token, err := service.NewToken()
	if err != nil {
		token = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	ttl := secrets.DefaultSessionTTL
	s.controlMu.Lock()
	if s.controlTokens == nil {
		s.controlTokens = map[string]time.Time{}
	}
	s.controlTokens[token] = time.Now().Add(ttl)
	s.controlMu.Unlock()
	return token, ttl.Milliseconds()
}

type presenceTracker struct {
	mu      sync.Mutex
	count   int
	timeout time.Duration
	timer   *time.Timer
	stop    func()
}

func newPresenceTracker(timeout time.Duration, stop func()) *presenceTracker {
	p := &presenceTracker{timeout: timeout, stop: stop}
	p.timer = time.AfterFunc(timeout, stop)
	return p
}

func (p *presenceTracker) connected() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.count++
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
}

func (p *presenceTracker) disconnected() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.count > 0 {
		p.count--
	}
	if p.count == 0 && p.timer == nil {
		p.timer = time.AfterFunc(p.timeout, p.stop)
	}
}

func (p *presenceTracker) setTimeout(timeout time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.timeout = timeout
	if p.count == 0 {
		if p.timer != nil {
			p.timer.Stop()
		}
		p.timer = time.AfterFunc(p.timeout, p.stop)
	}
}
