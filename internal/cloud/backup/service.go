package backup

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
)

// Reporter 接收推送结果，用于告警与审计。
//
// 用回调而不是直接依赖 alert 包：backup 是底层能力，
// 它不该知道告警是怎么发出去的，否则会形成 backup → alert → ... 的耦合。
type Reporter interface {
	// BackupFailed 在推送失败时调用。kind 为 "backup" 或 "audit"。
	BackupFailed(kind string, err error)
	// RemoteRewritten 在检测到远端历史被改写时调用。
	RemoteRewritten(kind string, err error)
	// BackupChecked 在一次校验成功后调用，用于管理面展示时间。
	BackupChecked(kind string, at time.Time)
}

// Service 负责把两个本地仓库推到远端（需求 15.1）。
//
// 触发时机：
//
//  1. 每次 commit 后 —— 由业务层调用 Trigger，内部合并成一次推送。
//  2. 每天一次 —— 兜底，防止某次触发因为进程重启而漏掉。
//  3. 部署期间暂停 —— 部署中的 commit 可能随后被回滚，推上去只会污染冷备。
type Service struct {
	backup Pusher
	audit  Pusher
	report Reporter

	// dailyTick 是兜底推送周期，测试里可以调小。
	dailyTick time.Duration
	// debounce 是提交后合并推送的延迟：连续写入只推一次。
	debounce time.Duration

	mu      sync.Mutex
	paused  bool
	running bool
	pending bool
	// lastErr 保留最近一次失败，便于排查“为什么一直没推上去”。
	lastErr error
	// stopped 关闭后不再接受 Trigger。
	closed bool

	// flushAudit 在推送审计仓库前把待提交记录落成 commit。
	flushAudit func() error

	wake chan struct{}
	done chan struct{}
}

// Options 是 Service 的构造参数。
type Options struct {
	BackupTarget Target
	AuditTarget  Target
	Reporter     Reporter
	// DailyTick 是兜底周期，默认 24 小时。
	DailyTick time.Duration
	// Debounce 是提交后合并窗口，默认 5 秒。
	Debounce time.Duration
	// FlushAudit 在推送审计仓库前调用，把待提交的记录落成 commit。
	//
	// 审计按时间窗合并提交（见 audit.CommitWindow），窗口内的记录还在工作树里；
	// 推送只看已提交内容，不先冲一次就会漏掉最近几秒的审计。
	// 用回调而不是直接依赖 audit 包：backup 是底层能力，不该反向依赖上层。
	FlushAudit func() error
}

// NewService 构造冷备服务。
func NewService(opts Options) *Service {
	if opts.DailyTick <= 0 {
		opts.DailyTick = 24 * time.Hour
	}
	if opts.Debounce <= 0 {
		opts.Debounce = 5 * time.Second
	}
	return &Service{
		backup:     Pusher{Target: opts.BackupTarget},
		audit:      Pusher{Target: opts.AuditTarget},
		report:     opts.Reporter,
		dailyTick:  opts.DailyTick,
		debounce:   opts.Debounce,
		flushAudit: opts.FlushAudit,
		wake:       make(chan struct{}, 1),
		done:       make(chan struct{}),
	}
}

// Start 启动后台推送循环。ctx 结束或 Stop 后退出。
func (s *Service) Start(ctx context.Context) {
	go s.loop(ctx)
}

// Stop 停止接收新的触发，并唤醒循环让它尽快退出。
func (s *Service) Stop() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Trigger 通知有新 commit 需要推送。
//
// 非阻塞：写入路径不能被冷备拖慢。连续多次触发会被 debounce 合并。
func (s *Service) Trigger() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.pending = true
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Pause 暂停推送（部署开始）。
//
// 暂停期间产生的 commit 不推送；Resume 后立刻补推一次。
// 这正是需求 21.2 要的行为：部署失败回滚的数据不该留在冷备里。
func (s *Service) Pause() {
	s.mu.Lock()
	s.paused = true
	s.mu.Unlock()
}

// Resume 恢复推送并立即补推一次。
func (s *Service) Resume() {
	s.mu.Lock()
	s.paused = false
	s.mu.Unlock()
	s.Trigger()
}

// Paused 报告是否处于暂停状态。
func (s *Service) Paused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paused
}

// LastError 返回最近一次推送错误。
func (s *Service) LastError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

// loop 是推送主循环。
func (s *Service) loop(ctx context.Context) {
	defer close(s.done)
	ticker := time.NewTicker(s.dailyTick)
	defer ticker.Stop()

	// debounce 用一次性定时器，避免连续写入时反复重置一个长 ticker。
	var timer *time.Timer
	var timerCh <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
			s.mu.Lock()
			closed, paused, pending := s.closed, s.paused, s.pending
			s.mu.Unlock()
			if closed {
				return
			}
			if paused || !pending {
				continue
			}
			if timer == nil {
				timer = time.NewTimer(s.debounce)
				timerCh = timer.C
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(s.debounce)
			}
		case <-timerCh:
			timerCh = nil
			s.mu.Lock()
			paused := s.paused
			s.pending = false
			s.mu.Unlock()
			if !paused {
				s.flush(ctx)
			}
		case <-ticker.C:
			s.mu.Lock()
			paused := s.paused
			s.mu.Unlock()
			if !paused {
				s.flush(ctx)
			}
		}
	}
}

// flush 依次推送冷备与审计。
//
// 两个仓库独立失败：审计推不上去不应该阻止冷备继续工作，
// 反过来也一样。每次失败都单独告警。
func (s *Service) flush(ctx context.Context) {
	s.mu.Lock()
	if s.running || s.closed {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	var firstErr error
	s.pushOne(ctx, "冷备", "backup", &s.backup, &firstErr)
	// 审计仓库按时间窗合并提交，窗口内的记录还在工作树里；
	// 推送前先冲一次，否则推上去的会缺最近几秒。
	if s.flushAudit != nil {
		if err := s.flushAudit(); err != nil {
			s.fail("audit", fmt.Errorf("审计冲写失败: %w", err), &firstErr)
		}
	}
	s.pushOne(ctx, "审计", "audit", &s.audit, &firstErr)

	s.mu.Lock()
	s.lastErr = firstErr
	s.mu.Unlock()
}

// pushOne 推送一个仓库并上报结果。
func (s *Service) pushOne(ctx context.Context, label, kind string, p *Pusher, firstErr *error) {
	if p.Target.URL == "" {
		// 未配置目标：本机调试与首次部署前的正常状态，不告警。
		return
	}
	repo, err := p.open()
	if err != nil {
		s.fail(kind, fmt.Errorf("%s: %w", label, err), firstErr)
		return
	}
	out, err := p.Push(ctx, repo)
	if err != nil {
		if errors.Is(err, ErrRemoteRewritten) && s.report != nil {
			s.report.RemoteRewritten(kind, fmt.Errorf("%s: %w", label, err))
		}
		s.fail(kind, fmt.Errorf("%s: %w", label, err), firstErr)
		return
	}
	if s.report != nil {
		s.report.BackupChecked(kind, out.CheckedAt)
	}
}

func (s *Service) fail(kind string, err error, firstErr *error) {
	if *firstErr == nil {
		*firstErr = err
	}
	if s.report != nil {
		s.report.BackupFailed(kind, err)
	}
}

// open 打开目标仓库，并保证 origin 指向配置的地址。
//
// 冷备仓库与审计仓库各自是独立的 git 仓库，由部署脚本创建在数据目录下；
// 这里只负责打开与对齐 origin，不负责初始化内容。
func (p *Pusher) open() (*git.Repository, error) {
	repo, err := git.PlainOpen(p.Target.Dir)
	if err != nil {
		return nil, fmt.Errorf("打开仓库 %s 失败: %w", p.Target.Dir, err)
	}
	if err := p.EnsureRemote(repo); err != nil {
		return nil, err
	}
	return repo, nil
}
