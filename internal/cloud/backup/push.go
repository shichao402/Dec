// Package backup 实现冷备与审计仓库的推送（需求 15.1、15.2、15.3）。
//
// 三个硬约束：
//
//  1. 只推不改：远端必须是本地历史的祖先。远端被改写过就停下并告警，
//     绝不用本地历史覆盖远端，那样会把“有人动过”这个信号彻底抹掉。
//  2. 不持有长期凭据：deploy key 由部署写入文件（0600），进程只读。
//  3. 失败不阻塞业务：推送在后台做，业务写入不等它。
package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	xcrypto "golang.org/x/crypto/ssh"
)

// 推送结果分类，用于决定是否告警。
var (
	// ErrRemoteRewritten 表示远端历史不再是本地历史的祖先。
	//
	// 这是最危险的一种：可能是有人手动改写了远端，也可能是本地仓库被重建过。
	// 两种情况都不能自动“修好”，必须由人判断（需求 15.2）。
	ErrRemoteRewritten = errors.New("远端历史已被改写")
	// ErrNotConfigured 表示没有配置推送目标，属正常状态（例如本机调试）。
	ErrNotConfigured = errors.New("未配置推送目标")
)

// Target 描述一个推送目标仓库。
type Target struct {
	// Name 用于日志与告警文案，例如“冷备”“审计”。
	Name string
	// Dir 是本地仓库目录。
	Dir string
	// URL 是远端地址，形如 git@github.com:owner/repo.git。
	URL string
	// KeyPath 是 deploy key 的路径。只读，进程不改它。
	KeyPath string
	// Branch 是推送分支，默认 main。
	Branch string
	// Passphrase 是 deploy key 的口令，通常为空。
	Passphrase string
	// KnownHosts 是用于校验推送目标主机密钥的文件路径。
	//
	// 显式给出而不是依赖 SSH_KNOWN_HOSTS 环境变量：服务由 systemd 拉起，
	// 环境变量要靠 EnvironmentFile 传导，多一层不透明的耦合；而且 go-git 在
	// 环境变量指向的文件不存在时会直接失败，错误信息与「密钥不对」难区分。
	// 留空则退回 go-git 的默认查找（~/.ssh/known_hosts 等）。
	KnownHosts string
}

// Pusher 把一个本地仓库推到远端。
type Pusher struct {
	Target Target

	mu sync.Mutex
	// user 是 SSH 认证，惰性构建并复用，避免每次推送都解析私钥。
	user transport.AuthMethod
}

// Outcome 是一次推送的结果。
type Outcome struct {
	// Pushed 表示确实推了内容（远端已是最新时为 false）。
	Pushed bool
	// Head 是推送后本地 head，也是远端现在应当指向的位置。
	Head string
	// CheckedAt 是本次校验成功的时间。
	CheckedAt time.Time
}

// Check 只做祖先关系校验，不推送。
//
// 单独抽出来是因为“部署前确认远端没被改写”和“推送”是两件事，
// 前者不该顺带产生一次推送。
func (p *Pusher) Check(repo *git.Repository) (remoteHead string, err error) {
	branch := p.branchOf(repo)
	remoteRef, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", branch), false)
	if err != nil {
		// 还没有远端跟踪引用：要么远端为空，要么还没 fetch 过。
		// 两种情况都说明这里无从判断“远端被改写”，交给 fetch 后的下一次校验。
		return "", nil
	}
	remoteHead = remoteRef.Hash().String()

	if !hasCommit(repo, remoteRef.Hash()) {
		return remoteHead, fmt.Errorf("%w: 远端 %s 的 %s 不在本地历史里", ErrRemoteRewritten, p.Target.Name, remoteHead)
	}
	head, err := repo.Head()
	if err != nil {
		return remoteHead, fmt.Errorf("读取本地 head 失败: %w", err)
	}
	if remoteRef.Hash() == head.Hash() {
		return remoteHead, nil
	}
	remoteCommit, err := repo.CommitObject(remoteRef.Hash())
	if err != nil {
		return remoteHead, fmt.Errorf("读取远端 commit 失败: %w", err)
	}
	localCommit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return remoteHead, fmt.Errorf("读取本地 commit 失败: %w", err)
	}
	ancestor, err := remoteCommit.IsAncestor(localCommit)
	if err != nil {
		return remoteHead, fmt.Errorf("判断祖先关系失败: %w", err)
	}
	if !ancestor {
		return remoteHead, fmt.Errorf("%w: 远端 %s 不是本地 %s 的祖先", ErrRemoteRewritten, remoteHead, head.Hash())
	}
	return remoteHead, nil
}

// Push 先 fetch 再校验祖先关系，最后非 force 推送当前分支。
//
// 顺序不能变：不先 fetch，本地看到的“远端”可能是几天前的快照，
// 祖先判断就失去意义。
func (p *Pusher) Push(ctx context.Context, repo *git.Repository) (*Outcome, error) {
	if p.Target.URL == "" {
		return nil, ErrNotConfigured
	}
	auth, err := p.auth()
	if err != nil {
		return nil, err
	}
	branch := p.branchOf(repo)

	// Push 自带远端对齐：调用方只需要给 Target，不必记得先 EnsureRemote。
	if err := p.EnsureRemote(repo); err != nil {
		return nil, err
	}

	fetchErr := repo.FetchContext(ctx, &git.FetchOptions{
		Auth:       auth,
		RemoteName: "origin",
		RefSpecs: []config.RefSpec{
			config.RefSpec(fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch)),
		},
	})
	if fetchErr != nil &&
		!errors.Is(fetchErr, git.NoErrAlreadyUpToDate) &&
		!errors.Is(fetchErr, transport.ErrEmptyRemoteRepository) {
		return nil, fmt.Errorf("fetch 失败: %w", fetchErr)
	}

	if _, err := p.Check(repo); err != nil {
		return nil, err
	}

	head, err := repo.Head()
	if err != nil {
		return nil, fmt.Errorf("读取本地 head 失败: %w", err)
	}
	if ref, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", branch), false); err == nil {
		if ref.Hash() == head.Hash() {
			return &Outcome{Pushed: false, Head: head.Hash().String(), CheckedAt: time.Now().UTC()}, nil
		}
	}

	pushErr := repo.PushContext(ctx, &git.PushOptions{
		Auth: auth,
		RefSpecs: []config.RefSpec{
			config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch)),
		},
		// 明确不开 force：需求 15.2 要求绝不覆盖远端历史。
		Force: false,
	})
	if pushErr != nil && !errors.Is(pushErr, git.NoErrAlreadyUpToDate) {
		return nil, fmt.Errorf("推送失败: %w", pushErr)
	}
	return &Outcome{Pushed: true, Head: head.Hash().String(), CheckedAt: time.Now().UTC()}, nil
}

// EnsureRemote 配置或更新 origin。
//
// 远端地址属于部署配置，允许变化（例如换仓库），所以每次都对齐一次。
func (p *Pusher) EnsureRemote(repo *git.Repository) error {
	if p.Target.URL == "" {
		return ErrNotConfigured
	}
	if existing, err := repo.Remote("origin"); err == nil {
		cfg := existing.Config()
		if len(cfg.URLs) == 1 && cfg.URLs[0] == p.Target.URL {
			return nil
		}
		if err := repo.DeleteRemote("origin"); err != nil {
			return fmt.Errorf("删除旧远端失败: %w", err)
		}
	}
	if _, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{p.Target.URL},
	}); err != nil {
		return fmt.Errorf("配置远端失败: %w", err)
	}
	return nil
}

func (p *Pusher) branch() string {
	if p.Target.Branch != "" {
		return p.Target.Branch
	}
	return "main"
}

// branchOf 返回仓库当前 HEAD 的分支名，取不到时回退到 branch()。
//
// 为什么要跟随本地 HEAD 而不是固定 "main"：
// SSOT 与审计仓库都由 go-git 的 PlainInit 初始化，它的默认分支是 master
// （go-git 不读 init.defaultBranch）。固定的 "main" 在这些仓库里根本不存在，
// fetch 的 refspec 指向一个空分支，随后 push 会以
// "src refspec main does not match any" 失败——而日志里只会看到一句
// "推送失败"，很难联想到分支名。
//
// 远端沿用同一个名字：冷备与审计是这两个本地仓库的镜像，分支名不必另立规矩。
func (p *Pusher) branchOf(repo *git.Repository) string {
	head, err := repo.Head()
	if err != nil {
		return p.branch()
	}
	if head.Name().IsBranch() {
		return head.Name().Short()
	}
	return p.branch()
}

// auth 惰性构建认证。
//
// 远端是本地路径（测试、或同机镜像）时不需要任何认证，返回 nil 交给传输层。
// 远端是 SSH 地址时才读 deploy key。
//
// 主机密钥一律校验，不跳过：部署密钥泄露时这是唯一的防线（需求 15.2）。
// 优先用 Target.KnownHosts 指定的文件；没指定时保持 go-git 的默认行为
// （读 ~/.ssh/known_hosts 与 /etc/ssh/ssh_known_hosts）。
func (p *Pusher) auth() (transport.AuthMethod, error) {
	if !needsAuth(p.Target.URL) {
		return nil, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.user != nil {
		return p.user, nil
	}
	if p.Target.KeyPath == "" {
		return nil, errors.New("缺少部署密钥路径")
	}
	key, err := os.ReadFile(p.Target.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("读取部署密钥失败: %w", err)
	}
	user, err := ssh.NewPublicKeys("git", key, p.Target.Passphrase)
	if err != nil {
		return nil, fmt.Errorf("解析部署密钥失败: %w", err)
	}
	if p.Target.KnownHosts != "" {
		cb, err := ssh.NewKnownHostsCallback(p.Target.KnownHosts)
		if err != nil {
			return nil, fmt.Errorf("读取 known_hosts %s 失败: %w", p.Target.KnownHosts, err)
		}
		user.HostKeyCallback = cb
		// 只接受 ed25519 主机密钥。
		//
		// 这不是画蛇添足：Go 的 ssh 客户端默认把 rsa-sha2-* 排在 ed25519 前面，
		// GitHub 同时提供多种类型，协商结果常常是 RSA，而 known_hosts 里只写了
		// ed25519（GitHub 公布的密钥、以及我们写死的那一条），于是校验直接报
		// "knownhosts: key mismatch"——看着像被中间人劫持，实际只是算法挑错了。
		//
		// 用 openssh 的 ssh -T 测同一个地址是通的，因为它按 known_hosts 里已有
		// 的类型挑选算法；Go 不会，必须显式限定。
		user.HostKeyAlgorithms = []string{xcrypto.KeyAlgoED25519}
	}
	p.user = user
	return p.user, nil
}

// needsAuth 报告远端地址是否需要认证。
//
// 只有 scp 风格（git@host:path）与 ssh:// 需要 key；
// http(s) 走凭据、本地路径根本不需要，硬塞一把 SSH key 只会让错误信息更难懂。
func needsAuth(url string) bool {
	if strings.HasPrefix(url, "ssh://") {
		return true
	}
	if strings.Contains(url, "://") {
		return false
	}
	// scp 风格：git@github.com:owner/repo.git
	at := strings.Index(url, "@")
	colon := strings.Index(url, ":")
	return at >= 0 && colon > at
}

// hasCommit 报告仓库里是否存在该 commit。
func hasCommit(repo *git.Repository, hash plumbing.Hash) bool {
	_, err := repo.CommitObject(hash)
	return err == nil
}
