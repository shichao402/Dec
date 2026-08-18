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

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("监听本机服务端口失败: %w", err)
	}
	defer listener.Close()

	token, err := service.NewToken()
	if err != nil {
		return err
	}
	idleTimeout := loadIdleTimeout()
	stopRequested := make(chan struct{}, 1)
	host := &Server{
		version:    version,
		instanceID: fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().UnixMilli()),
		broker:     newOperationBroker(),
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

	grpcServer := grpc.NewServer(
		grpc.UnaryInterceptor(unaryAuth(token, host.presence)),
		grpc.StreamInterceptor(streamAuth(token, host.presence)),
	)
	servicev1.RegisterDecServiceServer(grpcServer, host)
	if err := service.WriteMetadata(listener.Addr().String(), token); err != nil {
		return err
	}
	defer service.RemoveMetadata()

	go pruneOrphanWorktreesAtStartup()

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
	return &servicev1.PingResponse{Version: s.version, InstanceId: s.instanceID}, nil
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

// unaryAuth 校验 token，并在 RPC 执行期间把它计入 presence：
// 任何在飞调用都视为活跃，避免无 TUI、仅 MCP 时长操作被空闲计时器误杀。
func unaryAuth(token string, presence *presenceTracker) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if !validToken(ctx, token) {
			return nil, status.Error(codes.Unauthenticated, "invalid dec-server token")
		}
		presence.connected()
		defer presence.disconnected()
		return handler(ctx, req)
	}
}

// streamAuth 校验 token，并在流存活期间计入 presence。
// TUI 的 KeepAlive 长连流、以及各门面的 RunOperation / WatchOperation 流都由此占用 presence。
func streamAuth(token string, presence *presenceTracker) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if !validToken(stream.Context(), token) {
			return status.Error(codes.Unauthenticated, "invalid dec-server token")
		}
		presence.connected()
		defer presence.disconnected()
		return handler(srv, stream)
	}
}

func validToken(ctx context.Context, want string) bool {
	values := metadata.ValueFromIncomingContext(ctx, service.TokenHeader)
	return len(values) == 1 && values[0] == want
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
