package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gofrs/flock"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/update"
)

var runtimeSuiteNames = update.SuiteComponents

type ResolvedSuite struct {
	Version string
	OS      string
	Arch    string
	Dir     string
	Source  string // cache | download
}

type runtimeSuiteManifest struct {
	Version string            `json:"version"`
	OS      string            `json:"os"`
	Arch    string            `json:"arch"`
	Files   map[string]string `json:"files"`
}

func suiteBinaryName(name, goos string) string {
	if goos == "windows" {
		return name + ".exe"
	}
	return name
}

func suiteCacheDir(releaseVersion, goos, goarch string) (string, error) {
	root, err := repo.GetRootDir()
	if err != nil {
		return "", err
	}
	releaseVersion = strings.TrimPrefix(strings.TrimSpace(releaseVersion), "v")
	return filepath.Join(root, "runtime-cache", releaseVersion, goos+"-"+goarch), nil
}

func suiteFilesComplete(dir, goos string) bool {
	for _, name := range runtimeSuiteNames {
		info, err := os.Stat(filepath.Join(dir, suiteBinaryName(name, goos)))
		if err != nil || info.IsDir() || info.Size() == 0 {
			return false
		}
	}
	return true
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func verifyCachedSuite(dir, releaseVersion, goos, goarch string) bool {
	if !suiteFilesComplete(dir, goos) {
		return false
	}
	data, err := os.ReadFile(filepath.Join(dir, "runtime-manifest.json"))
	if err != nil {
		return false
	}
	var manifest runtimeSuiteManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return false
	}
	if normalizeReleaseVersion(manifest.Version) != normalizeReleaseVersion(releaseVersion) ||
		manifest.OS != goos || manifest.Arch != goarch {
		return false
	}
	for _, component := range runtimeSuiteNames {
		name := suiteBinaryName(component, goos)
		expected := strings.ToLower(strings.TrimSpace(manifest.Files[name]))
		if len(expected) != 64 {
			return false
		}
		actual, err := sha256File(filepath.Join(dir, name))
		if err != nil || actual != expected {
			return false
		}
	}
	return true
}

func writeSuiteManifest(dir, releaseVersion, goos, goarch string) error {
	manifest := runtimeSuiteManifest{
		Version: normalizeReleaseVersion(releaseVersion),
		OS:      goos,
		Arch:    goarch,
		Files:   make(map[string]string, len(runtimeSuiteNames)),
	}
	for _, component := range runtimeSuiteNames {
		name := suiteBinaryName(component, goos)
		digest, err := sha256File(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		manifest.Files[name] = digest
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(dir, "runtime-manifest.json"), data, 0o644)
}

func normalizeReleaseVersion(value string) string {
	value = strings.TrimSpace(value)
	if value != "" && !strings.HasPrefix(value, "v") {
		return "v" + value
	}
	return value
}

// ResolveRuntimeSuite resolves the Console-pinned os/arch suite from verified
// cache, or from RUP only while the channel head still equals that version.
func ResolveRuntimeSuite(ctx context.Context, releaseVersion, goos, goarch string, reporter Reporter) (*ResolvedSuite, error) {
	reporter = defaultReporter(reporter)
	releaseVersion = normalizeReleaseVersion(releaseVersion)
	if !validReleaseVersion(releaseVersion) {
		return nil, fmt.Errorf("置备版本无效: %q", releaseVersion)
	}
	goos = strings.TrimSpace(goos)
	goarch = strings.TrimSpace(goarch)
	if goos == "" || goarch == "" {
		return nil, fmt.Errorf("目标 os/arch 不能为空")
	}

	cacheDir, err := suiteCacheDir(releaseVersion, goos, goarch)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(cacheDir), 0o755); err != nil {
		return nil, err
	}
	lock := flock.New(cacheDir + ".lock")
	if err := lock.Lock(); err != nil {
		return nil, fmt.Errorf("锁定运行时缓存失败: %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	if verifyCachedSuite(cacheDir, releaseVersion, goos, goarch) {
		emit(reporter, EventInfo, "provision.suite",
			fmt.Sprintf("复用缓存四件套 %s（%s/%s）", releaseVersion, goos, goarch), nil)
		return &ResolvedSuite{Version: releaseVersion, OS: goos, Arch: goarch, Dir: cacheDir, Source: "cache"}, nil
	}

	emit(reporter, EventInfo, "provision.suite",
		fmt.Sprintf("通过 RUP 下载四件套 %s（%s/%s）到发起端缓存", releaseVersion, goos, goarch), nil)
	tempDir, err := os.MkdirTemp(filepath.Dir(cacheDir), "."+filepath.Base(cacheDir)+".tmp-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir)
	if err := update.DownloadSuite(ctx, releaseVersion, goos, goarch, tempDir); err != nil {
		return nil, fmt.Errorf("RUP 下载目标运行时失败: %w", err)
	}
	if err := writeSuiteManifest(tempDir, releaseVersion, goos, goarch); err != nil {
		return nil, fmt.Errorf("写入运行时缓存清单失败: %w", err)
	}
	if !verifyCachedSuite(tempDir, releaseVersion, goos, goarch) {
		return nil, fmt.Errorf("下载后的运行时缓存完整性校验失败")
	}
	if err := os.RemoveAll(cacheDir); err != nil {
		return nil, err
	}
	if err := os.Rename(tempDir, cacheDir); err != nil {
		return nil, fmt.Errorf("原子发布运行时缓存失败: %w", err)
	}
	return &ResolvedSuite{Version: releaseVersion, OS: goos, Arch: goarch, Dir: cacheDir, Source: "download"}, nil
}

// PushRuntimeSuite streams four verified local files through system ssh. The
// target only needs POSIX sh/core utilities and never downloads from network.
func PushRuntimeSuite(ctx context.Context, target RemoteTarget, suite *ResolvedSuite, reporter Reporter) error {
	reporter = defaultReporter(reporter)
	if suite == nil {
		return fmt.Errorf("runtime suite is nil")
	}
	if err := target.validate(); err != nil {
		return err
	}
	versionDir := strings.TrimPrefix(suite.Version, "v")
	prepare := fmt.Sprintf(`set -eu
dec_home="${DEC_HOME:-$HOME/.dec}"
stage="$dec_home/tmp/suite-%s"
rm -rf "$stage"
mkdir -p "$stage" "$dec_home/bin"
`, versionDir)
	if out, err := runSSHCommand(ctx, target, "sh -s", prepare); err != nil {
		return fmt.Errorf("准备远端目录失败: %s", summarizeSSHError(out, err))
	}

	for _, component := range runtimeSuiteNames {
		localPath := filepath.Join(suite.Dir, suiteBinaryName(component, suite.OS))
		emit(reporter, EventInfo, "provision.suite", "推送 "+component, nil)
		if err := pushRuntimeFile(ctx, target, localPath, versionDir, component); err != nil {
			return fmt.Errorf("推送 %s 失败: %w", component, err)
		}
	}

	hashes := make(map[string]string, len(runtimeSuiteNames))
	for _, component := range runtimeSuiteNames {
		digest, err := sha256File(filepath.Join(suite.Dir, suiteBinaryName(component, suite.OS)))
		if err != nil {
			return fmt.Errorf("计算 %s 摘要失败: %w", component, err)
		}
		hashes[component] = digest
	}
	activate, err := runtimeActivationScript(suite.Version, versionDir, hashes)
	if err != nil {
		return err
	}
	out, err := runSSHCommand(ctx, target, "sh -s", activate)
	if err != nil {
		return fmt.Errorf("激活远端四件套失败: %s", summarizeSSHError(out, err))
	}
	if strings.Contains(out, "降级为四组件 --version 校验") {
		emit(reporter, EventWarn, "provision.suite",
			"目标缺少 sha256sum/shasum，传输后完整性降级为四组件版本校验", nil)
	}
	emit(reporter, EventInfo, "provision.suite", "远端四件套已就位 "+suite.Version, nil)
	return nil
}

func runtimeActivationScript(releaseVersion, versionDir string, hashes map[string]string) (string, error) {
	releaseVersion = normalizeReleaseVersion(releaseVersion)
	if !validReleaseVersion(releaseVersion) || !validReleaseVersion(versionDir) {
		return "", fmt.Errorf("无效运行时版本 %q", releaseVersion)
	}
	var hashCases strings.Builder
	for _, component := range runtimeSuiteNames {
		digest := strings.ToLower(strings.TrimSpace(hashes[component]))
		if len(digest) != 64 {
			return "", fmt.Errorf("%s sha256 无效", component)
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return "", fmt.Errorf("%s sha256 无效: %w", component, err)
		}
		fmt.Fprintf(&hashCases, "    %s) expected=%s ;;\n", component, digest)
	}
	return fmt.Sprintf(`set -eu
dec_home="${DEC_HOME:-$HOME/.dec}"
stage="$dec_home/tmp/suite-%s"
bin="$dec_home/bin"
backup="$dec_home/tmp/suite-backup-%s-$$"

if command -v sha256sum >/dev/null 2>&1; then
  hash_tool=sha256sum
elif command -v shasum >/dev/null 2>&1; then
  hash_tool=shasum
else
  hash_tool=
fi

if [ -n "$hash_tool" ]; then
  for b in %s; do
    case "$b" in
%s    esac
    if [ "$hash_tool" = sha256sum ]; then
      actual=$(sha256sum "$stage/$b")
    else
      actual=$(shasum -a 256 "$stage/$b")
    fi
    actual=${actual%% *}
    if [ "$actual" != "$expected" ]; then
      echo "$b 传输后 sha256 校验失败" >&2
      exit 1
    fi
  done
else
  echo "warning: 目标缺少 sha256sum/shasum，降级为四组件 --version 校验" >&2
  for b in %s; do
    chmod 755 "$stage/$b"
    output=$("$stage/$b" --version 2>&1) || {
      echo "$b --version 校验失败" >&2
      exit 1
    }
    case "$output" in
      *"%s"*) ;;
      *) echo "$b 版本不匹配，期望 %s" >&2; exit 1 ;;
    esac
  done
fi

for b in %s; do
  chmod 755 "$stage/$b"
done

mkdir -p "$backup" "$bin"
backed=
installed=
committed=0
rollback_suite() {
  [ "$committed" -eq 1 ] && return 0
  for b in $installed; do rm -f "$bin/$b"; done
  for b in $backed; do
    [ ! -e "$backup/$b" ] || mv -f "$backup/$b" "$bin/$b"
  done
}
trap rollback_suite 0 1 2 3 15

for b in %s; do
  if [ -e "$bin/$b" ]; then
    mv "$bin/$b" "$backup/$b"
    backed="$backed $b"
  fi
done
for b in %s; do
  mv "$stage/$b" "$bin/$b"
  installed="$installed $b"
done
for b in %s; do
  output=$("$bin/$b" --version 2>&1) || exit 1
  case "$output" in
    *"%s"*) ;;
    *) echo "$b 激活后版本不匹配，期望 %s" >&2; exit 1 ;;
  esac
done

if command -v xattr >/dev/null 2>&1; then
  xattr -cr "$bin" 2>/dev/null || true
fi
committed=1
rm -rf "$backup" "$stage"
echo "verified=$hash_tool version=%s"
`, versionDir, versionDir, strings.Join(runtimeSuiteNames, " "), hashCases.String(),
		strings.Join(runtimeSuiteNames, " "), releaseVersion, releaseVersion,
		strings.Join(runtimeSuiteNames, " "), strings.Join(runtimeSuiteNames, " "),
		strings.Join(runtimeSuiteNames, " "), strings.Join(runtimeSuiteNames, " "),
		releaseVersion, releaseVersion, releaseVersion), nil
}

func pushRuntimeFile(ctx context.Context, target RemoteTarget, localPath, versionDir, component string) error {
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()
	command := fmt.Sprintf(
		`dec_home="${DEC_HOME:-$HOME/.dec}"; cat > "$dec_home/tmp/suite-%s/%s"`,
		versionDir, component,
	)
	cmd := exec.CommandContext(ctx, "ssh", sshArgs(target, command)...)
	cmd.Stdin = file
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", summarizeSSHError(string(out), err))
	}
	return nil
}

func lastNonEmptyLine(out string) string {
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}
