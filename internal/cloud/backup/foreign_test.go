package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/sysproc"
)

// 端到端验证「本地 main 仓库能推到远端 main」这条路径，
// 覆盖此前两个真实生产故障的组合场景：
//
//  1. 初始化用的是 PlainInit → 本地是 master，远端默认 main；
//  2. 远端有一个与本地无关的初始化提交（GitHub 建仓时自动生成的 README）。
//
// 用本地路径当远端，不碰网络；走的是真实的 Push 代码路径，
// 因此能复现 Check 的祖先判断与非 force 推送的行为。
func TestPushToRemoteWithForeignInitialCommit(t *testing.T) {
	root := t.TempDir()
	srcDir := filepath.Join(root, "src")
	remoteDir := filepath.Join(root, "remote")

	// 远端：先有一个与本地毫无关系的提交，模拟 GitHub 建仓时的占位 README。
	if _, err := sysproc.Command("git", "--version").Output(); err != nil {
		t.Skip("需要 git 命令行构造远端占位提交")
	}
	if err := os.MkdirAll(remoteDir, 0o700); err != nil {
		t.Fatal(err)
	}
	runGit(t, remoteDir, "init", "-q", "-b", "main")
	runGit(t, remoteDir, "config", "user.email", "test@localhost")
	runGit(t, remoteDir, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(remoteDir, "README.md"), []byte("初始化\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, remoteDir, "add", ".")
	runGit(t, remoteDir, "commit", "-q", "-m", "初始化")

	// 本地：用生产同款初始化路径，先生成一个提交。
	repo, err := ssot.InitRepo(srcDir)
	if err != nil {
		t.Fatalf("InitRepo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "data.txt"), []byte("payload\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, srcDir, "config", "user.email", "test@localhost")
	runGit(t, srcDir, "config", "user.name", "test")
	runGit(t, srcDir, "add", ".")
	runGit(t, srcDir, "commit", "-q", "-m", "本地数据")

	// 远端有陌生历史时，Push 必须拒绝而不是覆盖。
	p := &Pusher{Target: Target{
		Name: "冷备",
		Dir:  srcDir,
		URL:  remoteDir,
	}}
	_, err = p.Push(t.Context(), repo)
	if err == nil {
		t.Fatal("远端存在与本地无关的历史时，Push 应拒绝而非覆盖")
	}
	if !strings.Contains(err.Error(), "远端历史已被改写") {
		t.Fatalf("错误应指明远端历史被改写，实际: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := sysproc.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@localhost",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@localhost",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
