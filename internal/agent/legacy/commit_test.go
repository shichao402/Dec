package legacy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/sysproc"
)

// 这些测试用真实 git：提交语义（路径限定、钩子回滚暂存区）正是要验证的东西，打桩会把它绕过去。

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("没有 git")
	}
	// 隔离用户与系统的 git 配置（全局钩子、签名、默认分支等）。
	empty := filepath.Join(t.TempDir(), "gitconfig")
	_ = os.WriteFile(empty, nil, 0o600)
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := sysproc.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v：%v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func setIdentity(t *testing.T, dir string) {
	git(t, dir, "config", "user.name", "Tester")
	git(t, dir, "config", "user.email", "t@example.com")
	git(t, dir, "config", "commit.gpgsign", "false")
}

// gitProject 建一个带裸远端的项目仓库，已提交旧版 .dec 文件与一个普通文件，并推送到上游。
func gitProject(t *testing.T) (state, proj, remote string) {
	requireGit(t)
	state, root := archiveFixture(t)
	remote = filepath.Join(root, "remote.git")
	proj = filepath.Join(root, "proj")
	git(t, root, "init", "--quiet", "--bare", "-b", "main", remote)
	git(t, root, "init", "--quiet", "-b", "main", proj)
	setIdentity(t, proj)
	put(t, filepath.Join(proj, ".dec/config.yaml"), "old: 1\n", 0o644)
	put(t, filepath.Join(proj, ".dec/cache/a"), "cache\n", 0o644)
	put(t, filepath.Join(proj, "app.go"), "package app\n", 0o644)
	put(t, filepath.Join(proj, ".mcp.json"), "{\n  \"mcpServers\": {\n    \"dec-old\": {\"command\": \"x\"},\n    \"keep\": {\"command\": \"y\"}\n  }\n}\n", 0o644)
	git(t, proj, "add", "-A")
	git(t, proj, "commit", "--quiet", "-m", "init")
	git(t, proj, "remote", "add", "origin", remote)
	git(t, proj, "push", "--quiet", "-u", "origin", "main")
	return state, proj, remote
}

func projItems(proj string) []Item {
	return []Item{
		{Rule: RuleProjectDec, Action: ActionDelete, Path: filepath.Join(proj, ".dec/config.yaml"), Project: proj},
		{Rule: RuleProjectDec, Action: ActionDelete, Path: filepath.Join(proj, ".dec/cache"), Project: proj, IsDir: true},
		{Rule: RuleProjectDec, Action: ActionDelete, Path: filepath.Join(proj, ".dec/untracked.yaml"), Project: proj},
		{Rule: RuleProjectMCP, Action: ActionStrip, Path: filepath.Join(proj, ".mcp.json"), Project: proj, Entries: []string{"dec-old"}},
	}
}

func execGit(t *testing.T, state string, items []Item) *Result {
	t.Helper()
	opts := execOpts(&fakeProcs{})
	opts.Git = GitOptions{NetTimeout: 20 * time.Second}
	res, err := Execute(state, &Report{Items: items}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed() {
		t.Fatalf("执行有失败项：%+v", res.Items)
	}
	return res
}

func onlyRepo(t *testing.T, res *Result) RepoResult {
	t.Helper()
	if len(res.Repos) != 1 {
		t.Fatalf("应有一个仓库结果：%+v", res.Repos)
	}
	return res.Repos[0]
}

// 一个提交，只含本次删除/改写的被跟踪路径；用户的暂存与未暂存改动原样保留；推送到上游（需求 8.1–8.3）。
func TestCommitPushesOnlyRemovedPaths(t *testing.T) {
	state, proj, remote := gitProject(t)
	put(t, filepath.Join(proj, ".dec/untracked.yaml"), "u", 0o644)
	// 用户正在做的事：一个已暂存的新文件、一处未暂存修改。
	put(t, filepath.Join(proj, "staged.go"), "package app\n", 0o644)
	git(t, proj, "add", "staged.go")
	put(t, filepath.Join(proj, "app.go"), "package app // wip\n", 0o644)

	res := execGit(t, state, projItems(proj))
	rr := onlyRepo(t, res)
	if rr.Status != RepoPushed {
		t.Fatalf("应已提交并推送：%+v", rr)
	}
	if strings.Join(rr.Paths, ",") != ".dec/cache,.dec/config.yaml,.mcp.json" {
		t.Fatalf("提交路径不对：%v", rr.Paths)
	}
	files := git(t, proj, "show", "--name-status", "--format=", "HEAD")
	want := "D\t.dec/cache/a\nD\t.dec/config.yaml\nM\t.mcp.json"
	if files != want {
		t.Fatalf("提交内容应只有本次删除与改写：\n%s", files)
	}
	msg := git(t, proj, "log", "-1", "--format=%s%n%b")
	if !strings.HasPrefix(msg, CommitSubject) || !strings.Contains(msg, res.Archive.ID) {
		t.Fatalf("提交说明应固定且带归档 id：%q", msg)
	}
	if a := git(t, proj, "log", "-1", "--format=%an <%ae>"); a != "Tester <t@example.com>" {
		t.Fatalf("应使用仓库自身身份：%s", a)
	}
	if git(t, proj, "diff", "--cached", "--name-only") != "staged.go" {
		t.Fatal("用户已暂存的改动应仍在暂存区")
	}
	if got, _ := os.ReadFile(filepath.Join(proj, "app.go")); !strings.Contains(string(got), "wip") {
		t.Fatal("用户未暂存的改动应原样保留")
	}
	if git(t, remote, "rev-parse", "main") != rr.Commit {
		t.Fatal("远端应已是这次提交")
	}
}

// 落后上游不推送、不 rebase；本地还有自己未推的提交时也不推（需求 8.3）。
func TestCommitDoesNotPushWhenBehindOrAhead(t *testing.T) {
	t.Run("behind", func(t *testing.T) {
		state, proj, remote := gitProject(t)
		other := filepath.Join(filepath.Dir(proj), "other")
		git(t, filepath.Dir(proj), "clone", "--quiet", remote, other)
		setIdentity(t, other)
		put(t, filepath.Join(other, "new.go"), "package app\n", 0o644)
		git(t, other, "add", "new.go")
		git(t, other, "commit", "--quiet", "-m", "other")
		git(t, other, "push", "--quiet")
		remoteHead := git(t, remote, "rev-parse", "main")

		rr := onlyRepo(t, execGit(t, state, projItems(proj)[:1]))
		if rr.Status != RepoCommitted || !strings.Contains(rr.Reason, "落后") || len(rr.Manual) == 0 {
			t.Fatalf("落后时应只提交不推送并给出命令：%+v", rr)
		}
		if git(t, remote, "rev-parse", "main") != remoteHead {
			t.Fatal("远端不应被改动")
		}
		if git(t, proj, "rev-parse", "HEAD") != rr.Commit {
			t.Fatal("本地不应被 rebase")
		}
	})
	t.Run("own unpushed commits", func(t *testing.T) {
		state, proj, remote := gitProject(t)
		put(t, filepath.Join(proj, "mine.go"), "package app\n", 0o644)
		git(t, proj, "add", "mine.go")
		git(t, proj, "commit", "--quiet", "-m", "mine")
		before := git(t, remote, "rev-parse", "main")
		rr := onlyRepo(t, execGit(t, state, projItems(proj)[:1]))
		if rr.Status != RepoCommitted || !strings.Contains(rr.Reason, "未推送的提交") {
			t.Fatalf("不应替用户推送其自己的提交：%+v", rr)
		}
		if git(t, remote, "rev-parse", "main") != before {
			t.Fatal("远端不应被改动")
		}
	})
}

// 无上游只提交；推送失败不影响结果中的提交（需求 8.3、8.5）。
func TestCommitNoUpstream(t *testing.T) {
	state, proj, _ := gitProject(t)
	git(t, proj, "branch", "--unset-upstream")
	rr := onlyRepo(t, execGit(t, state, projItems(proj)[:1]))
	if rr.Status != RepoCommitted || rr.Commit == "" || !strings.Contains(rr.Reason, "上游") {
		t.Fatalf("无上游时应只提交：%+v", rr)
	}
}

// 钩子失败、detached HEAD、merge 进行中、没有身份：都不提交，文件照样已删，给出手工命令（需求 8.4、8.6）。
func TestCommitSkipsAbnormalRepos(t *testing.T) {
	cases := map[string]func(t *testing.T, proj string){
		"hook": func(t *testing.T, proj string) {
			put(t, filepath.Join(proj, ".git/hooks/pre-commit"), "#!/bin/sh\nexit 1\n", 0o755)
		},
		"detached": func(t *testing.T, proj string) {
			git(t, proj, "checkout", "--quiet", "--detach")
		},
		"merge": func(t *testing.T, proj string) {
			put(t, filepath.Join(proj, ".git/MERGE_HEAD"), git(t, proj, "rev-parse", "HEAD")+"\n", 0o644)
		},
		"identity": func(t *testing.T, proj string) {
			git(t, proj, "config", "--unset", "user.email")
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			state, proj, _ := gitProject(t)
			setup(t, proj)
			head := git(t, proj, "rev-parse", "HEAD")
			res := execGit(t, state, projItems(proj)[:1])
			rr := onlyRepo(t, res)
			if rr.Status != RepoSkipped || rr.Reason == "" || len(rr.Manual) == 0 || rr.Commit != "" {
				t.Fatalf("应跳过提交并给出命令：%+v", rr)
			}
			if !strings.Contains(rr.Manual[0], CommitSubject) || strings.Contains(rr.Manual[0], "--no-verify") {
				t.Fatalf("手工命令不对：%s", rr.Manual[0])
			}
			if git(t, proj, "rev-parse", "HEAD") != head {
				t.Fatal("不应产生提交")
			}
			if _, err := os.Stat(filepath.Join(proj, ".dec/config.yaml")); err == nil {
				t.Fatal("文件仍应已删除")
			}
			if name == "hook" && git(t, proj, "diff", "--cached", "--name-only") != "" {
				t.Fatal("钩子失败后暂存区应恢复原状")
			}
			if name == "merge" && !strings.Contains(rr.Reason, "正在进行 merge") {
				t.Fatalf("应在提交前识别出 merge 进行中：%s", rr.Reason)
			}
		})
	}
}

// 改写的共享文件里有用户未提交的改动时，不放进提交（否则会把用户的改动一起提交）。
func TestCommitExcludesStripWithUserEdits(t *testing.T) {
	state, proj, _ := gitProject(t)
	mcp := filepath.Join(proj, ".mcp.json")
	data, _ := os.ReadFile(mcp)
	put(t, mcp, strings.Replace(string(data), `"y"`, `"y2"`, 1), 0o644)

	res := execGit(t, state, []Item{projItems(proj)[3]})
	rr := onlyRepo(t, res)
	if rr.Status != RepoSkipped || len(rr.Excluded) != 1 || !strings.Contains(rr.Excluded[0], "未提交的改动") {
		t.Fatalf("有用户改动的共享文件不应提交：%+v", rr)
	}
	if git(t, proj, "status", "--porcelain", "--", ".mcp.json") == "" {
		t.Fatal("文件应留在工作区待用户自己提交")
	}
}

// 不是 git 仓库或删的都不被跟踪时，没有仓库结果。
func TestCommitIgnoresNonRepoAndUntracked(t *testing.T) {
	requireGit(t)
	state, root := archiveFixture(t)
	plain := filepath.Join(root, "plain")
	put(t, filepath.Join(plain, ".dec/config.yaml"), "x", 0o644)
	res := execGit(t, state, []Item{{Rule: RuleProjectDec, Action: ActionDelete, Path: filepath.Join(plain, ".dec/config.yaml"), Project: plain}})
	if len(res.Repos) != 0 {
		t.Fatalf("非仓库不应有提交结果：%+v", res.Repos)
	}

	state2, proj, _ := gitProject(t)
	put(t, filepath.Join(proj, ".dec/untracked.yaml"), "u", 0o644)
	res = execGit(t, state2, []Item{projItems(proj)[2]})
	if len(res.Repos) != 0 {
		t.Fatalf("只删了未跟踪文件不应有提交结果：%+v", res.Repos)
	}
}
