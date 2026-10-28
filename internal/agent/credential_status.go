package agent

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/proto"
)

// 本机凭据落点的现场状态（.agent/credential-landing 需求 8.2）。
//
// 代理不保存下发清单，所以这里只能回答「现场有什么、形状对不对」：
// 是否与云端一致、被拒原因由云端状态回答（管理面设备页）。
// 只看文件名、权限与行尾，从不输出内容。

// LocalSlot 是一个凭据落点的现场状态。
type LocalSlot struct {
	Root   string
	Path   string
	Mode   os.FileMode
	Issues []string
}

// LocalCredentialStatus 扫描 ~/.ssh 白名单与已登记项目的 .secrets 目录。
func LocalCredentialStatus(home string, projects []Project) ([]LocalSlot, error) {
	var out []LocalSlot
	sshBase := filepath.Join(home, sshDir)
	entries, err := os.ReadDir(sshBase)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	var rels []string
	for _, e := range entries {
		if !e.IsDir() && proto.ValidSSHPath(e.Name()) && e.Name() != proto.SSHConfigPath {
			rels = append(rels, e.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(sshBase, filepath.FromSlash(proto.SSHFragmentPath))); err == nil {
		rels = append(rels, proto.SSHFragmentPath)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		full := filepath.Join(sshBase, filepath.FromSlash(rel))
		slot, err := inspectSlot(proto.RootSSH, full, proto.IsSSHPrivateKey(rel))
		if err != nil {
			return nil, err
		}
		out = append(out, slot)
	}
	cfg := filepath.Join(sshBase, proto.SSHConfigPath)
	if data, err := os.ReadFile(cfg); err == nil {
		if _, _, _, found, perr := splitManagedBlock(string(data)); perr != nil {
			out = append(out, LocalSlot{Root: proto.RootSSH, Path: cfg, Issues: []string{"托管块边界损坏：" + perr.Error()}})
		} else if found {
			out = append(out, LocalSlot{Root: proto.RootSSH, Path: cfg + "（托管块）"})
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	for _, p := range projects {
		base := filepath.Join(p.Path, secretsDir)
		if _, err := os.Stat(base); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		var files []string
		werr := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && path != filepath.Join(base, secretsIgnoreName) {
				files = append(files, path)
			}
			return nil
		})
		if werr != nil {
			return nil, werr
		}
		ignore, err := os.ReadFile(filepath.Join(base, secretsIgnoreName))
		if err != nil || string(ignore) != secretsIgnoreBody {
			out = append(out, LocalSlot{Root: proto.RootSecrets, Path: filepath.Join(base, secretsIgnoreName),
				Issues: []string{".gitignore 缺失或内容不是 *"}})
		}
		for _, f := range files {
			slot, err := inspectSlot(proto.RootSecrets, f, true)
			if err != nil {
				return nil, err
			}
			if tracked, terr := trackedByGit(p.Path, f); terr == nil && tracked {
				slot.Issues = append(slot.Issues, "被 git 跟踪")
			}
			out = append(out, slot)
		}
	}
	return out, nil
}

func inspectSlot(root, full string, private bool) (LocalSlot, error) {
	info, err := os.Stat(full)
	if err != nil {
		return LocalSlot{}, err
	}
	slot := LocalSlot{Root: root, Path: full, Mode: info.Mode().Perm()}
	if private && runtime.GOOS != "windows" && slot.Mode&0o077 != 0 {
		slot.Issues = append(slot.Issues, fmt.Sprintf("权限 %o，应为 600", slot.Mode))
	}
	if root == proto.RootSSH && private {
		data, err := os.ReadFile(full)
		if err != nil {
			return LocalSlot{}, err
		}
		if strings.Contains(string(data), "\r") {
			slot.Issues = append(slot.Issues, "含 CRLF 行尾，OpenSSH 会读不了")
		}
	}
	return slot, nil
}
