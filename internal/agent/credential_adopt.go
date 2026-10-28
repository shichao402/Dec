package agent

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/shichao402/Dec/internal/agent/legacy"
	"github.com/shichao402/Dec/internal/proto"
)

// 首次接管（.agent/credential-landing 需求 5）。
//
// 云端第一次往一个凭据落点下发时（它还没有这个条目的落地哈希）会把
// proto.File.Adopt 置真。代理不保存任何状态，判断完全在现场做：
// 落点已有内容且与要写的不同，就先用旧版清理的归档格式把现场打包，
// 归档校验通过之后才覆盖。归档失败则整条写入失败，原文件不动。
//
// 归档进同一个 legacy-archive 目录，工具页的归档列表、还原、365 天保留
// 都直接复用，不再造一套。

// RuleAdopt 是接管归档在归档清单里的规则名，与旧版清理规则（H/P 编号）区分开。
const RuleAdopt = "adopt"

// SetArchiveDir 设置首次接管归档使用的状态目录。为空时遇到需要归档的接管直接拒绝写入。
func (w *Writer) SetArchiveDir(stateDir string) { w.stateDir = stateDir }

// adoptIfNeeded 在首次接管且现场内容不同的情况下归档现场，返回回执说明。
//
// expected 是写入后落点应有的哈希（与 HashFile 同口径）。
func (w *Writer) adoptIfNeeded(full string, kind credentialKind, f proto.File, expected string) (string, error) {
	if !f.Adopt {
		return "", nil
	}
	target := full
	if kind == credSSHConfig {
		t, err := resolveLinkTarget(full)
		if err != nil {
			return "", err
		}
		target = t
	}
	if _, err := os.Lstat(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	format := proto.FormatWhole
	if kind == credSSHConfig {
		format = proto.FormatManagedBlock
	}
	current, err := HashFile(target, format, "")
	if err != nil {
		return "", err
	}
	if kind == credSSHConfig && current == "" {
		// 还没有托管块：写入只是在顶部插一块，用户原有内容一行不动，无需归档。
		return "", nil
	}
	if current == expected {
		return "", nil
	}
	if w.stateDir == "" {
		return "", fmt.Errorf("%w: %q 已有不同内容，且没有可用的归档目录，拒绝覆盖", ErrNotAllowed, full)
	}
	item := legacy.Item{
		Rule:   RuleAdopt,
		Group:  legacy.GroupCredential,
		Action: legacy.ActionDelete,
		Path:   target,
	}
	if kind == credSSHConfig {
		// 共享文件整份归档，执行侧只替换托管块。
		item.Action = legacy.ActionStrip
		item.Entries = []string{"托管块 " + proto.BlockBegin}
	}
	if info, err := os.Stat(target); err == nil {
		item.Size = info.Size()
	}
	a, err := legacy.WriteArchive(w.stateDir, []legacy.Item{item}, time.Now())
	if err != nil {
		return "", fmt.Errorf("首次接管 %q 归档失败，拒绝覆盖: %w", full, err)
	}
	return fmt.Sprintf("已归档后覆盖 %s（归档 %s）", full, a.ID), nil
}
