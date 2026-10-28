package assets

import (
	"fmt"

	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// validateFileItem 校验 file 条目（.agent/credential-landing 需求 4.1、4.2）。
//
// 规则与代理侧白名单（internal/agent/credential.go）同源：都调 proto 里的
// 路径函数。在写入时就拒绝，而不是等代理逐台设备报写入失败。
func validateFileItem(item ssot.ManifestItem) error {
	if len(item.Planes) != 1 {
		return fmt.Errorf("%w: file 条目 %q 只能声明一个平面", ErrSchema, item.Path)
	}
	plane := item.Planes[0]
	format := item.Format
	if format == "" {
		format = ssot.FileFormatWhole
	}
	if format != ssot.FileFormatWhole && format != ssot.FileFormatManagedBlock {
		return fmt.Errorf("%w: file 条目 %q 的格式 %q 不受支持", ErrSchema, item.Path, item.Format)
	}
	if item.Mode&^0o777 != 0 {
		return fmt.Errorf("%w: file 条目 %q 的权限 %o 不合法", ErrSchema, item.Path, item.Mode)
	}

	switch item.Root {
	case ssot.FileRootSSH:
		if plane != ssot.PlaneUser {
			return fmt.Errorf("%w: file 条目 %q 的 ssh 根只能用于用户平面", ErrSchema, item.Path)
		}
		if !proto.ValidSSHPath(item.Target) {
			return fmt.Errorf("%w: file 条目 %q 的目标 %q 不在 ssh 根白名单内（dec_*、config.d/dec.conf、config）",
				ErrSchema, item.Path, item.Target)
		}
		isConfig := item.Target == proto.SSHConfigPath
		if isConfig != (format == ssot.FileFormatManagedBlock) {
			return fmt.Errorf("%w: file 条目 %q：~/.ssh/config 必须且只有它能用 managed_block 格式",
				ErrSchema, item.Path)
		}
	case ssot.FileRootSecrets:
		if plane != ssot.PlaneProject {
			return fmt.Errorf("%w: file 条目 %q 的 secrets 根只能用于项目平面", ErrSchema, item.Path)
		}
		if !proto.ValidSecretsPath(item.Target) {
			return fmt.Errorf("%w: file 条目 %q 的目标 %q 不是合法的 .secrets 相对路径", ErrSchema, item.Path, item.Target)
		}
		if format != ssot.FileFormatWhole {
			return fmt.Errorf("%w: file 条目 %q：secrets 根只支持 whole 格式", ErrSchema, item.Path)
		}
	default:
		return fmt.Errorf("%w: file 条目 %q 的落点根 %q 不受支持（只能是 ssh 或 secrets）", ErrSchema, item.Path, item.Root)
	}
	return nil
}
