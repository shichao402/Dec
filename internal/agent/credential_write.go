package agent

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/internal/proto"
)

// 凭据落点的写入细节（.agent/credential-landing 需求 1.3–1.6、2.3–2.6、3）。

// secretsIgnoreName 是代理在 <项目>/.secrets 下维护的忽略文件。
const secretsIgnoreName = ".gitignore"

// secretsIgnoreBody 让 git 忽略 .secrets 下的全部内容，包括这个文件本身。
const secretsIgnoreBody = "*\n"

// writeCredential 写一个凭据落点，返回内容哈希与需要提示的告警。
//
// projectRoot 只在 secrets 根下有值。
func (w *Writer) writeCredential(full string, kind credentialKind, f proto.File, projectRoot string) (string, []string, error) {
	if !isWithin(w.home, full) {
		return "", nil, fmt.Errorf("%w: 落点 %q 不在用户目录内", ErrNotAllowed, full)
	}
	switch kind {
	case credSSHConfig:
		if f.Format != proto.FormatManagedBlock {
			return "", nil, fmt.Errorf("%w: ~/.ssh/config 只能以 %s 格式写入", ErrNotAllowed, proto.FormatManagedBlock)
		}
		if err := ensureDir(filepath.Dir(full), 0o700); err != nil {
			return "", nil, err
		}
		var notes []string
		if norm, ok := proto.NormalizeBlockBody(f.Content); ok {
			note, err := w.adoptIfNeeded(full, kind, f, hashOf([]byte(norm)))
			if err != nil {
				return "", nil, err
			}
			if note != "" {
				notes = append(notes, note)
			}
		}
		body, err := writeManagedBlock(full, f.Content)
		if err != nil {
			return "", notes, err
		}
		return hashOf([]byte(body)), notes, nil
	}

	if f.Format != proto.FormatWhole {
		return "", nil, fmt.Errorf("%w: 凭据落点只接受 %s 格式，收到 %q", ErrNotAllowed, proto.FormatWhole, f.Format)
	}

	switch kind {
	case credSSHPrivate:
		content := proto.NormalizeSSHKey(f.Content)
		if err := ensureDir(filepath.Dir(full), 0o700); err != nil {
			return "", nil, err
		}
		notes, err := w.adoptNotes(full, kind, f, content)
		if err != nil {
			return "", nil, err
		}
		if err := writeFileAtomic(full, []byte(content), 0o600); err != nil {
			return "", notes, err
		}
		if err := restrictToOwner(full); err != nil {
			return "", notes, fmt.Errorf("收紧私钥 %q 的权限失败: %w", full, err)
		}
		return hashOf([]byte(content)), notes, nil

	case credSSHPublic:
		mode := os.FileMode(0o644)
		if f.Secret {
			mode = 0o600
		}
		// ~/.ssh 与 ~/.ssh/config.d 都按 0700 建：OpenSSH 对目录权限同样挑剔。
		if err := ensureDir(filepath.Join(w.home, sshDir), 0o700); err != nil {
			return "", nil, err
		}
		if err := ensureDir(filepath.Dir(full), 0o700); err != nil {
			return "", nil, err
		}
		notes, err := w.adoptNotes(full, kind, f, f.Content)
		if err != nil {
			return "", nil, err
		}
		if err := writeFileAtomic(full, []byte(f.Content), mode); err != nil {
			return "", notes, err
		}
		if f.Secret {
			if err := restrictToOwner(full); err != nil {
				return "", notes, fmt.Errorf("收紧 %q 的权限失败: %w", full, err)
			}
		}
		return hashOf([]byte(f.Content)), notes, nil

	case credProjectSecret:
		tracked, err := trackedByGit(projectRoot, full)
		if err != nil {
			return "", nil, fmt.Errorf("无法确认 %q 是否被 git 跟踪，拒绝写入明文: %w", full, err)
		}
		if tracked {
			return "", nil, fmt.Errorf("%w: %q 已被 git 跟踪，写入明文会被提交", ErrNotAllowed, full)
		}
		base := filepath.Join(projectRoot, secretsDir)
		warn, err := ensureSecretsIgnore(base)
		if err != nil {
			return "", nil, err
		}
		notes, err := w.adoptNotes(full, kind, f, f.Content)
		warn = append(warn, notes...)
		if err != nil {
			return "", warn, err
		}
		if err := writeFileAtomic(full, []byte(f.Content), 0o600); err != nil {
			return "", warn, err
		}
		if err := restrictToOwner(full); err != nil {
			return "", warn, fmt.Errorf("收紧 %q 的权限失败: %w", full, err)
		}
		return hashOf([]byte(f.Content)), warn, nil
	}
	return "", nil, fmt.Errorf("%w: 未知凭据落点", ErrNotAllowed)
}

// adoptNotes 是整文件落点的接管归档入口，把说明包成切片便于并入告警。
func (w *Writer) adoptNotes(full string, kind credentialKind, f proto.File, content string) ([]string, error) {
	note, err := w.adoptIfNeeded(full, kind, f, hashOf([]byte(content)))
	if err != nil || note == "" {
		return nil, err
	}
	return []string{note}, nil
}

// ensureDir 创建目录。已存在的目录不改权限：~/.ssh 是用户的东西。
func ensureDir(dir string, mode os.FileMode) error {
	if err := os.MkdirAll(dir, mode); err != nil {
		return fmt.Errorf("创建目录 %q 失败: %w", dir, err)
	}
	return nil
}

// ensureSecretsIgnore 确保 <项目>/.secrets/.gitignore 存在（需求 2.4）。
//
// 已存在但内容不同的保留原样，只返回告警：用户可能有意只忽略一部分，
// 被跟踪的文件另有 trackedByGit 拦着，不会因此泄露。
func ensureSecretsIgnore(base string) ([]string, error) {
	if err := ensureDir(base, 0o700); err != nil {
		return nil, err
	}
	p := filepath.Join(base, secretsIgnoreName)
	data, err := os.ReadFile(p)
	switch {
	case err == nil:
		if string(data) == secretsIgnoreBody {
			return nil, nil
		}
		return []string{fmt.Sprintf("%s 已存在且内容不是 %q，保留原样", p, strings.TrimSpace(secretsIgnoreBody))}, nil
	case errors.Is(err, os.ErrNotExist):
		if err := writeFileAtomic(p, []byte(secretsIgnoreBody), 0o644); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return nil, fmt.Errorf("读取 %q 失败: %w", p, err)
}

// 托管块（需求 3）。

// splitManagedBlock 把文件内容拆成「块前、块内、块后」。
//
// 块边界按整行匹配（忽略首尾空白）。只认第一对完整的边界；
// 有 BEGIN 没 END 视为损坏，返回错误而不是猜测范围。
func splitManagedBlock(content string) (before, body, after string, found bool, err error) {
	lines := strings.SplitAfter(content, "\n")
	begin, end := -1, -1
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if begin < 0 && t == proto.BlockBegin {
			begin = i
			continue
		}
		if begin >= 0 && t == proto.BlockEnd {
			end = i
			break
		}
	}
	if begin < 0 {
		return content, "", "", false, nil
	}
	if end < 0 {
		return "", "", "", false, fmt.Errorf("托管块只有开始标记，没有结束标记")
	}
	before = strings.Join(lines[:begin], "")
	body = strings.Join(lines[begin+1:end], "")
	after = strings.Join(lines[end+1:], "")
	return before, body, after, true, nil
}

// writeManagedBlock 把 body 写进文件的托管块，返回规范化后的块内容。
//
// 文件不存在时新建（0600）；块不存在时插在最前面（需求 3.2）；
// 块已存在时原地替换（需求 3.3）。文件是软链接时写链接指向的文件，
// 链接本身保留（~/.ssh/config 常放在 dotfiles 仓库里）。
func writeManagedBlock(full, content string) (string, error) {
	body, ok := proto.NormalizeBlockBody(content)
	if !ok {
		return "", fmt.Errorf("%w: 托管块内容不能包含块边界标记", ErrNotAllowed)
	}
	target, err := resolveLinkTarget(full)
	if err != nil {
		return "", err
	}
	existing, mode, err := readWithMode(target, 0o600)
	if err != nil {
		return "", err
	}
	block := proto.BlockBegin + "\n" + body + proto.BlockEnd + "\n"
	before, _, after, found, err := splitManagedBlock(existing)
	if err != nil {
		return "", fmt.Errorf("%q: %w，拒绝改写", full, err)
	}
	var out string
	if found {
		out = before + block + after
	} else if existing == "" {
		out = block
	} else {
		// 块与原内容之间空一行，便于阅读。
		out = block + "\n" + existing
	}
	if err := writeFileAtomic(target, []byte(out), mode); err != nil {
		return "", err
	}
	return body, nil
}

// removeManagedBlock 只删除托管块（需求 3.5），文件本身始终保留。
func removeManagedBlock(full string) error {
	target, err := resolveLinkTarget(full)
	if err != nil {
		return err
	}
	existing, mode, err := readWithMode(target, 0o600)
	if err != nil {
		return err
	}
	before, _, after, found, err := splitManagedBlock(existing)
	if err != nil {
		return fmt.Errorf("%q: %w，拒绝改写", full, err)
	}
	if !found {
		return nil
	}
	// 写入时在块后补过一个空行，删除时一并收回，避免反复增删后空行越积越多。
	if before == "" {
		after = strings.TrimPrefix(after, "\n")
	}
	return writeFileAtomic(target, []byte(before+after), mode)
}

// hashManagedBlock 按块内内容计算哈希（需求 3.4）。没有块返回空哈希。
func hashManagedBlock(data []byte) (string, error) {
	_, body, _, found, err := splitManagedBlock(string(data))
	if err != nil || !found {
		return "", nil
	}
	norm, ok := proto.NormalizeBlockBody(body)
	if !ok {
		return "", nil
	}
	return hashOf([]byte(norm)), nil
}

// resolveLinkTarget 返回软链接最终指向的路径；不是软链接或不存在时原样返回。
func resolveLinkTarget(full string) (string, error) {
	info, err := os.Lstat(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return full, nil
		}
		return "", err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return full, nil
	}
	target, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", fmt.Errorf("%q 是失效的软链接: %w", full, err)
	}
	return target, nil
}

// readWithMode 读取文件内容与权限；不存在时返回空内容与默认权限。
func readWithMode(p string, def os.FileMode) (string, os.FileMode, error) {
	info, err := os.Stat(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", def, nil
		}
		return "", 0, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", 0, err
	}
	return string(data), info.Mode().Perm(), nil
}

// git 索引（需求 2.5）。
//
// 代理不执行外部命令（host.go 包注释），所以直接读 .git/index。
// 只需要回答「这个路径在不在索引里」，按格式文档解析 v2/v3/v4 的条目路径即可。

// trackedByGit 报告 full 是否在其所在仓库的 git 索引里。
//
// 找不到仓库或仓库还没有索引，视为未跟踪。索引存在但读不懂时返回错误：
// 调用方据此拒绝写入，宁可这一项落不下来，也不把明文写进会被提交的位置。
func trackedByGit(projectRoot, full string) (bool, error) {
	repo, gitDir, ok, err := findGitDir(projectRoot)
	if err != nil || !ok {
		return false, err
	}
	rel, err := filepath.Rel(repo, full)
	if err != nil {
		return false, err
	}
	rel = filepath.ToSlash(rel)
	data, err := os.ReadFile(filepath.Join(gitDir, "index"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	paths, err := parseGitIndex(data)
	if err != nil {
		return false, err
	}
	for _, p := range paths {
		if p == rel {
			return true, nil
		}
	}
	return false, nil
}

// findGitDir 从 start 向上找仓库根，返回仓库根与 git 目录。
//
// .git 可能是目录，也可能是 worktree / submodule 的 `gitdir: <路径>` 文件。
func findGitDir(start string) (repo, gitDir string, ok bool, err error) {
	dir := filepath.Clean(start)
	for {
		p := filepath.Join(dir, ".git")
		info, statErr := os.Stat(p)
		if statErr == nil {
			if info.IsDir() {
				return dir, p, true, nil
			}
			data, readErr := os.ReadFile(p)
			if readErr != nil {
				return "", "", false, readErr
			}
			line := strings.TrimSpace(string(data))
			if !strings.HasPrefix(line, "gitdir:") {
				return "", "", false, fmt.Errorf("%q 不是可识别的 .git 文件", p)
			}
			gd := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
			if !filepath.IsAbs(gd) {
				gd = filepath.Join(dir, gd)
			}
			return dir, gd, true, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", false, nil
		}
		dir = parent
	}
}

// parseGitIndex 解析 git 索引，返回全部条目路径。
//
// 格式见 git 文档 gitformat-index：12 字节头（"DIRC"、版本、条目数），
// 每个条目 62 字节定长部分 + 可选扩展标志 + 路径。v2/v3 路径以 NUL 结尾并
// 按 8 字节对齐；v4 路径做前缀压缩、不对齐。末尾 20 字节是 SHA-1 校验和。
func parseGitIndex(data []byte) ([]string, error) {
	if len(data) < 12+sha1.Size || string(data[:4]) != "DIRC" {
		return nil, fmt.Errorf("git 索引格式无法识别")
	}
	sum := sha1.Sum(data[:len(data)-sha1.Size])
	if !bytes.Equal(sum[:], data[len(data)-sha1.Size:]) {
		// SHA-256 仓库的校验和是 32 字节，这里不支持，按读不懂处理。
		return nil, fmt.Errorf("git 索引校验和不符（可能是 SHA-256 仓库）")
	}
	version := binary.BigEndian.Uint32(data[4:8])
	if version < 2 || version > 4 {
		return nil, fmt.Errorf("不支持的 git 索引版本 %d", version)
	}
	count := int(binary.BigEndian.Uint32(data[8:12]))
	body := data[:len(data)-sha1.Size]
	pos := 12
	out := make([]string, 0, count)
	prev := ""
	for i := 0; i < count; i++ {
		start := pos
		if pos+62 > len(body) {
			return nil, fmt.Errorf("git 索引条目被截断")
		}
		flags := binary.BigEndian.Uint16(body[pos+60 : pos+62])
		pos += 62
		if flags&0x4000 != 0 {
			if version < 3 {
				return nil, fmt.Errorf("git 索引 v2 不应有扩展标志")
			}
			pos += 2
		}
		var name string
		if version == 4 {
			strip, n := readOffsetVarint(body[pos:])
			if n <= 0 || strip > len(prev) {
				return nil, fmt.Errorf("git 索引 v4 路径前缀损坏")
			}
			pos += n
			end := bytes.IndexByte(body[pos:], 0)
			if end < 0 {
				return nil, fmt.Errorf("git 索引路径没有结尾")
			}
			name = prev[:len(prev)-strip] + string(body[pos:pos+end])
			pos += end + 1
		} else {
			end := bytes.IndexByte(body[pos:], 0)
			if end < 0 {
				return nil, fmt.Errorf("git 索引路径没有结尾")
			}
			name = string(body[pos : pos+end])
			pos += end + 1
			// 条目总长按 8 字节对齐，NUL 填充。
			for (pos-start)%8 != 0 {
				pos++
			}
		}
		out = append(out, name)
		prev = name
	}
	return out, nil
}

// readOffsetVarint 读 git 的 offset 变长整数（与 protobuf varint 不同：每多一字节先加 1）。
func readOffsetVarint(b []byte) (int, int) {
	if len(b) == 0 {
		return 0, 0
	}
	c := b[0]
	val := int(c & 0x7f)
	i := 1
	for c&0x80 != 0 {
		if i >= len(b) {
			return 0, 0
		}
		c = b[i]
		i++
		val = ((val + 1) << 7) | int(c&0x7f)
	}
	return val, i
}

// removeEmptySecretsParents 删除 .secrets 下因删除文件而变空的子目录，
// 最多删到 .secrets 为止，.secrets 本身保留（需求 2.6）。
func removeEmptySecretsParents(full string) {
	dir := filepath.Dir(full)
	for i := 0; i < 32; i++ {
		if filepath.Base(dir) == secretsDir {
			return
		}
		if !strings.Contains(filepath.ToSlash(dir), "/"+secretsDir+"/") {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}
