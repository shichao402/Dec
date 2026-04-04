package repo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetBareRepoDir_UsesDecHome(t *testing.T) {
	decHome := t.TempDir()
	setEnvForTest(t, "DEC_HOME", decHome)

	got, err := GetBareRepoDir()
	if err != nil {
		t.Fatalf("GetBareRepoDir 返回错误: %v", err)
	}

	want := filepath.Join(decHome, "repo.git")
	if got != want {
		t.Fatalf("GetBareRepoDir = %q, want %q", got, want)
	}
}

func TestIsBareConnected_FalseWhenMissing(t *testing.T) {
	decHome := t.TempDir()
	setEnvForTest(t, "DEC_HOME", decHome)

	ok, err := IsBareConnected()
	if err != nil {
		t.Fatalf("IsBareConnected 返回错误: %v", err)
	}
	if ok {
		t.Fatalf("未连接时应返回 false")
	}
}

func TestConnectBare_ClonesRemoteAsBare(t *testing.T) {
	decHome := t.TempDir()
	setEnvForTest(t, "DEC_HOME", decHome)
	remoteBareDir := setupRemoteBareRepo(t)

	if err := ConnectBare(remoteBareDir); err != nil {
		t.Fatalf("ConnectBare 失败: %v", err)
	}

	bareDir, err := GetBareRepoDir()
	if err != nil {
		t.Fatalf("GetBareRepoDir 失败: %v", err)
	}
	if ok, err := isBareRepo(bareDir); err != nil {
		t.Fatalf("检查 bare repo 失败: %v", err)
	} else if !ok {
		t.Fatalf("ConnectBare 后应得到 bare repo")
	}

	ok, err := IsBareConnected()
	if err != nil {
		t.Fatalf("IsBareConnected 返回错误: %v", err)
	}
	if !ok {
		t.Fatalf("ConnectBare 后应返回已连接")
	}
}

func TestConnectBare_FailsWhenAlreadyConnected(t *testing.T) {
	decHome := t.TempDir()
	setEnvForTest(t, "DEC_HOME", decHome)
	remoteBareDir := setupRemoteBareRepo(t)

	if err := ConnectBare(remoteBareDir); err != nil {
		t.Fatalf("首次 ConnectBare 失败: %v", err)
	}
	if err := ConnectBare(remoteBareDir); err == nil {
		t.Fatalf("重复 ConnectBare 应失败")
	} else if !strings.Contains(err.Error(), "仓库已连接") {
		t.Fatalf("错误信息应提示已连接, got: %v", err)
	}
}

func TestFetchBare_UpdatesLocalHeads(t *testing.T) {
	_, localBareDir, remoteBareDir, remoteWorkDir := setupBareRepoFixture(t)

	before := runGitNoDir(t, "--git-dir", localBareDir, "rev-parse", "refs/heads/main")

	writeFile(t, filepath.Join(remoteWorkDir, "remote.txt"), "from remote\n")
	runGit(t, remoteWorkDir, "add", ".")
	runGit(t, remoteWorkDir, "commit", "-m", "remote update")
	runGit(t, remoteWorkDir, "push", "origin", "main")

	if err := FetchBare(); err != nil {
		t.Fatalf("FetchBare 失败: %v", err)
	}

	after := runGitNoDir(t, "--git-dir", localBareDir, "rev-parse", "refs/heads/main")
	remote := runGitNoDir(t, "--git-dir", remoteBareDir, "rev-parse", "refs/heads/main")
	if after == before {
		t.Fatalf("FetchBare 后本地 refs/heads/main 应更新")
	}
	if after != remote {
		t.Fatalf("FetchBare 后本地分支应与远端一致, got %s want %s", after, remote)
	}
}

func TestGetDefaultBranch_ReturnsHeadBranch(t *testing.T) {
	setupBareRepoFixture(t)

	branch, err := GetDefaultBranch()
	if err != nil {
		t.Fatalf("GetDefaultBranch 失败: %v", err)
	}
	if branch != "main" {
		t.Fatalf("GetDefaultBranch = %q, want main", branch)
	}
}

func TestGetDefaultBranch_FallsBackToMasterRef(t *testing.T) {
	decHome := t.TempDir()
	setEnvForTest(t, "DEC_HOME", decHome)

	bareDir := filepath.Join(decHome, "repo.git")
	if err := os.MkdirAll(filepath.Join(bareDir, "refs", "heads"), 0755); err != nil {
		t.Fatalf("创建 refs 目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bareDir, "HEAD"), []byte("0123456789abcdef\n"), 0644); err != nil {
		t.Fatalf("写入 HEAD 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bareDir, "refs", "heads", "master"), []byte("0123456789abcdef\n"), 0644); err != nil {
		t.Fatalf("写入 master ref 失败: %v", err)
	}

	branch, err := GetDefaultBranch()
	if err != nil {
		t.Fatalf("GetDefaultBranch fallback 失败: %v", err)
	}
	if branch != "master" {
		t.Fatalf("GetDefaultBranch fallback = %q, want master", branch)
	}
}

func TestMigrateToBare_MigratesLegacyRepo(t *testing.T) {
	decHome, legacyDir, remoteBareDir := setupLegacyRepoFixture(t)
	setEnvForTest(t, "DEC_HOME", decHome)

	legacyHead := runGit(t, legacyDir, "rev-parse", "HEAD")
	if err := MigrateToBare(); err != nil {
		t.Fatalf("MigrateToBare 失败: %v", err)
	}

	if _, err := os.Stat(filepath.Join(decHome, "repo")); !os.IsNotExist(err) {
		t.Fatalf("迁移后旧仓库目录应被删除")
	}

	bareDir := filepath.Join(decHome, "repo.git")
	if ok, err := isBareRepo(bareDir); err != nil {
		t.Fatalf("检查迁移后的 bare repo 失败: %v", err)
	} else if !ok {
		t.Fatalf("迁移后应生成 bare repo")
	}

	migratedHead := runGitNoDir(t, "--git-dir", bareDir, "rev-parse", "HEAD")
	remoteHead := runGitNoDir(t, "--git-dir", remoteBareDir, "rev-parse", "HEAD")
	if migratedHead != legacyHead || migratedHead != remoteHead {
		t.Fatalf("迁移后 HEAD 不一致, migrated=%s legacy=%s remote=%s", migratedHead, legacyHead, remoteHead)
	}

	// 验证迁移后 origin URL 指向真正的远程仓库，而非旧的本地路径
	originURL := runGitNoDir(t, "--git-dir", bareDir, "config", "--get", "remote.origin.url")
	if originURL != remoteBareDir {
		t.Fatalf("迁移后 origin URL 应指向远程仓库 %q, got %q", remoteBareDir, originURL)
	}
}

func TestMigrateToBare_FailsWhenLegacyRepoDirty(t *testing.T) {
	decHome, legacyDir, _ := setupLegacyRepoFixture(t)
	setEnvForTest(t, "DEC_HOME", decHome)

	writeFile(t, filepath.Join(legacyDir, "dirty.txt"), "dirty\n")

	err := MigrateToBare()
	if err == nil {
		t.Fatalf("dirty legacy repo 迁移应失败")
	}
	if !strings.Contains(err.Error(), "未提交修改") {
		t.Fatalf("错误信息应提示未提交修改, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(decHome, "repo", ".git")); err != nil {
		t.Fatalf("失败时旧仓库应保留: %v", err)
	}
	if _, err := os.Stat(filepath.Join(decHome, "repo.git", "HEAD")); !os.IsNotExist(err) {
		t.Fatalf("失败时不应生成 bare repo")
	}
}

func setupBareRepoFixture(t *testing.T) (string, string, string, string) {
	t.Helper()

	decHome := t.TempDir()
	setEnvForTest(t, "DEC_HOME", decHome)
	root := t.TempDir()
	remoteBareDir := filepath.Join(root, "remote.git")
	seedDir := filepath.Join(root, "seed")
	remoteWorkDir := filepath.Join(root, "remote-work")
	localBareDir := filepath.Join(decHome, "repo.git")

	runGitNoDir(t, "init", "--bare", remoteBareDir)
	runGitNoDir(t, "clone", remoteBareDir, seedDir)
	configureGitUser(t, seedDir)
	writeFile(t, filepath.Join(seedDir, "README.md"), "init\n")
	runGit(t, seedDir, "add", ".")
	runGit(t, seedDir, "commit", "-m", "initial commit")
	runGit(t, seedDir, "branch", "-M", "main")
	runGit(t, seedDir, "push", "-u", "origin", "main")
	runGitNoDir(t, "--git-dir", remoteBareDir, "symbolic-ref", "HEAD", "refs/heads/main")

	runGitNoDir(t, "clone", "--bare", remoteBareDir, localBareDir)
	runGitNoDir(t, "clone", remoteBareDir, remoteWorkDir)
	configureGitUser(t, remoteWorkDir)

	return decHome, localBareDir, remoteBareDir, remoteWorkDir
}

func setupRemoteBareRepo(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	remoteBareDir := filepath.Join(root, "remote.git")
	seedDir := filepath.Join(root, "seed")

	runGitNoDir(t, "init", "--bare", remoteBareDir)
	runGitNoDir(t, "clone", remoteBareDir, seedDir)
	configureGitUser(t, seedDir)
	writeFile(t, filepath.Join(seedDir, "README.md"), "init\n")
	runGit(t, seedDir, "add", ".")
	runGit(t, seedDir, "commit", "-m", "initial commit")
	runGit(t, seedDir, "branch", "-M", "main")
	runGit(t, seedDir, "push", "-u", "origin", "main")
	runGitNoDir(t, "--git-dir", remoteBareDir, "symbolic-ref", "HEAD", "refs/heads/main")

	return remoteBareDir
}

func setupLegacyRepoFixture(t *testing.T) (string, string, string) {
	t.Helper()

	decHome := t.TempDir()
	root := t.TempDir()
	remoteBareDir := filepath.Join(root, "remote.git")
	seedDir := filepath.Join(root, "seed")
	legacyDir := filepath.Join(decHome, "repo")

	runGitNoDir(t, "init", "--bare", remoteBareDir)
	runGitNoDir(t, "clone", remoteBareDir, seedDir)
	configureGitUser(t, seedDir)
	writeFile(t, filepath.Join(seedDir, "README.md"), "init\n")
	runGit(t, seedDir, "add", ".")
	runGit(t, seedDir, "commit", "-m", "initial commit")
	runGit(t, seedDir, "branch", "-M", "main")
	runGit(t, seedDir, "push", "-u", "origin", "main")
	runGitNoDir(t, "--git-dir", remoteBareDir, "symbolic-ref", "HEAD", "refs/heads/main")

	runGitNoDir(t, "clone", remoteBareDir, legacyDir)
	configureGitUser(t, legacyDir)
	return decHome, legacyDir, remoteBareDir
}
