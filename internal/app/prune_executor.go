package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shichao402/Dec/internal/prune"
	"github.com/shichao402/Dec/internal/secrets"
)

// PruneExecutor 把剪枝判定落到文件系统：统一走隔离区，任何情况下都不直接
// os.Remove 本机无备份的条目。
//
// 这是执行路径的唯一入口。判定与执行的分界是「决策器只算不动，执行器只动
// 不判」，两边共用同一份 PruneItem，避免出现预览与执行结论不一致。
type PruneExecutor struct {
	store *prune.Store
}

// NewPruneExecutor 构造执行器。store 不可为 nil；隔离区初始化失败必须由
// 调用方在更上层处理。
func NewPruneExecutor(store *prune.Store) *PruneExecutor {
	return &PruneExecutor{store: store}
}

// DefaultPruneExecutor 按当前环境配置返回执行器。隔离区不可用时返回错误，
// 调用剪枝的流程必须中止，绝不退化为永久删除。
func DefaultPruneExecutor() (*PruneExecutor, error) {
	store, err := mustPruneQuarantineStore()
	if err != nil {
		return nil, err
	}
	return NewPruneExecutor(store), nil
}

// PruneResult 汇总一次执行的结果，供事件与报告使用。
type PruneResult struct {
	RemovedSecretPaths []string
	RemovedSSHKeys     []string
	Warnings           []string
}

// Execute 把一批判定为可重建的条目移入隔离区。
//
// 单条失败不影响其余条目。返回值里的路径都只是 display 形式（给人看），
// 不暴露绝对路径。
func (e *PruneExecutor) Execute(ctx context.Context, items []PruneItem, reporter Reporter) PruneResult {
	reporter = defaultReporter(reporter)
	var res PruneResult
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			res.Warnings = append(res.Warnings, "剪枝被中止："+err.Error())
			break
		}
		switch item.Kind {
		case "ssh_key":
			res.merge(e.executeSSHKey(ctx, item, reporter))
		default:
			res.merge(e.executeNote(ctx, item, reporter))
		}
	}
	return res
}

func (r *PruneResult) merge(o PruneResult) {
	r.RemovedSecretPaths = append(r.RemovedSecretPaths, o.RemovedSecretPaths...)
	r.RemovedSSHKeys = append(r.RemovedSSHKeys, o.RemovedSSHKeys...)
	r.Warnings = append(r.Warnings, o.Warnings...)
}

// QuarantineBase 返回隔离区根目录，供测试与诊断。
func (e *PruneExecutor) QuarantineBase() string {
	if e.store == nil {
		return ""
	}
	return e.store.Root()
}

func (e *PruneExecutor) executeNote(ctx context.Context, item PruneItem, reporter Reporter) PruneResult {
	var res PruneResult
	if item.AbsPath == "" {
		res.Warnings = append(res.Warnings, fmt.Sprintf("跳过 %s：缺少绝对路径", item.DisplayPath))
		return res
	}
	info, err := os.Lstat(item.AbsPath)
	if err != nil {
		if os.IsNotExist(err) {
			res.Warnings = append(res.Warnings, fmt.Sprintf("跳过 %s：已不存在", item.DisplayPath))
			return res
		}
		res.Warnings = append(res.Warnings, fmt.Sprintf("跳过 %s：读取失败 %v", item.DisplayPath, err))
		return res
	}

	entry := prune.Entry{
		OriginalPath: item.AbsPath,
		DisplayPath:  item.DisplayPath,
		RelPath:      item.RelPath,
		Kind:         item.Kind,
		Reason:       item.Reason,
		Detail:       item.Detail,
		Bundle:       item.Bundle,
		Plane:        item.Plane,
		ProjectRoot:  item.ProjectRoot,
		IsDir:        info.IsDir(),
	}
	if _, moveErr := e.store.Move(item.AbsPath, entry); moveErr != nil {
		// 隔离区失败必须显式暴露：宁可剪枝失败，也不允许数据静默丢失。
		res.Warnings = append(res.Warnings, fmt.Sprintf("移入隔离区失败 %s：%v", item.DisplayPath, moveErr))
		emit(reporter, EventError, "pull.prune",
			fmt.Sprintf("⚠️ 未能把 %s 移入隔离区，已保留原文件 : %v", item.DisplayPath, moveErr), nil)
		return res
	}
	res.RemovedSecretPaths = append(res.RemovedSecretPaths, item.DisplayPath)
	emit(reporter, EventInfo, "pull.prune",
		fmt.Sprintf("已将孤儿 %s 移入隔离区（可恢复，远端已无）", item.DisplayPath), nil)
	return res
}

func (e *PruneExecutor) executeSSHKey(ctx context.Context, item PruneItem, reporter Reporter) PruneResult {
	var res PruneResult
	if item.AbsPath == "" || item.RelPath == "" {
		res.Warnings = append(res.Warnings, fmt.Sprintf("跳过 SSH Key %s：缺少路径信息", item.DisplayPath))
		return res
	}
	// 只搬 Dec 托管落点的私钥；公钥与配置条目跟随清理。
	privPath := item.AbsPath
	pubPath := privPath + ".pub"
	if _, err := os.Stat(privPath); err != nil {
		if os.IsNotExist(err) {
			res.Warnings = append(res.Warnings, fmt.Sprintf("跳过 SSH Key %s：已不存在", item.DisplayPath))
			return res
		}
		res.Warnings = append(res.Warnings, fmt.Sprintf("跳过 SSH Key %s：读取失败 %v", item.DisplayPath, err))
		return res
	}

	entry := prune.Entry{
		OriginalPath: privPath,
		DisplayPath:  item.DisplayPath,
		RelPath:      item.RelPath,
		Kind:         "ssh_key",
		Reason:       item.Reason,
		Detail:       item.Detail,
		Bundle:       item.Bundle,
		Plane:        item.Plane,
		ProjectRoot:  item.ProjectRoot,
	}
	moved, moveErr := e.store.Move(privPath, entry)
	if moveErr != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("移入隔离区失败 %s：%v", item.DisplayPath, moveErr))
		emit(reporter, EventError, "pull.prune",
			fmt.Sprintf("⚠️ 未能把 %s 移入隔离区，已保留原文件 : %v", item.DisplayPath, moveErr), nil)
		return res
	}

	// 密钥对完整性：私钥搬走后，公钥也一并复制进同一隔离项目录，保证可整体恢复。
	if pubInfo, statErr := os.Stat(pubPath); statErr == nil && !pubInfo.IsDir() {
		if copyErr := copyFileTo(pubPath, filepath.Join(e.store.ItemDir(moved.ID), moved.StoredRel+".pub")); copyErr != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("公钥备份失败 %s.pub：%v", item.DisplayPath, copyErr))
		} else if rmErr := os.Remove(pubPath); rmErr != nil && !os.IsNotExist(rmErr) {
			res.Warnings = append(res.Warnings, fmt.Sprintf("清理公钥原落点失败 %s.pub：%v", item.DisplayPath, rmErr))
		}
	}
	if item.Plane == string(secrets.SyncPlaneProject) {
		_ = secrets.RemoveProjectSSHKeyLanding(item.ProjectRoot, item.Bundle, item.RelPath)
	} else {
		_ = secrets.RemoveSSHKeyLanding(item.Bundle, item.RelPath)
	}

	res.RemovedSSHKeys = append(res.RemovedSSHKeys, item.DisplayPath)
	emit(reporter, EventInfo, "pull.prune",
		fmt.Sprintf("已将孤儿 SSH Key %s 移入隔离区（可恢复，远端已无）", item.DisplayPath), nil)
	return res
}

// CopyFileTo 复制单个文件到目标。供公钥备份复用。
func copyFileTo(src, dest string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, data, info.Mode().Perm())
}
