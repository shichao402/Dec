//go:build unix

package emergency

import (
	"fmt"
	"os"
	"syscall"
)

// checkOwner 拦住在错误身份下运行的情况。
//
// 应急命令必须以**和数据目录同一个 uid** 运行。最常见的手滑是
// `sudo dec-emergency revoke ...`：它会成功，但这次 commit 写出的对象
// 归 root 所有，随后 dec 服务再提交就会失败，等于用一次应急操作
// 换掉整个服务端的写入能力。
//
// 只比 uid、不比用户名：部署方式（systemd、容器）可能改变账户名，
// 但「谁拥有这个目录」始终是唯一正确的判据。
//
// 非 root 的情况不用查：普通用户能写别人拥有的目录，只可能是目录权限
// 本来就放宽了，那是另一类问题，而这里要守的是 root 静默改归属。
func checkOwner(dir string, info os.FileInfo) error {
	if os.Geteuid() != 0 {
		return nil
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if st.Uid == 0 {
		// 目录本来就属于 root（本地演练、容器内以 root 跑服务），不该被拦。
		return nil
	}
	return fmt.Errorf("数据目录 %s 属于 uid %d，但当前以 root 运行；"+
		"请改用该 uid 执行（sudo -u <服务账户> ...），"+
		"否则本次写入的文件归 root 所有，服务之后将无法提交", dir, st.Uid)
}
