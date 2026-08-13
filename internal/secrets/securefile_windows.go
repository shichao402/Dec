//go:build windows

package secrets

import (
	"fmt"
	"os"
	"runtime"

	"golang.org/x/sys/windows"
)

// restrictFilePermissions 在 Windows 上去掉继承，仅保留当前用户权限。
// mode 的写位决定是否授予写权限（私钥 0600 → 读写；公钥 0644 → 只读）。
func restrictFilePermissions(path string, mode os.FileMode) error {
	return applyUserOnlyACL(path, mode&0200 != 0)
}

func restrictDirPermissions(dir string) error {
	// Windows 上对目录施加 PROTECTED DACL 容易导致随后无法读取目录内已有文件
	// （测试 TempDir / 预置 config）。SSH 安全关键是私钥文件 ACL，目录仅确保存在即可。
	_ = dir
	return nil
}

func applyUserOnlyACL(path string, writable bool) error {
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return fmt.Errorf("打开进程 token 失败: %w", err)
	}
	defer token.Close()

	user, err := token.GetTokenUser()
	if err != nil {
		return fmt.Errorf("获取当前用户 SID 失败: %w", err)
	}
	sid := user.User.Sid

	accessMask := windows.ACCESS_MASK(windows.GENERIC_READ | windows.GENERIC_EXECUTE | windows.DELETE | windows.WRITE_DAC | windows.READ_CONTROL | windows.SYNCHRONIZE)
	if writable {
		accessMask = windows.GENERIC_ALL
	}

	entries := []windows.EXPLICIT_ACCESS{{
		AccessPermissions: accessMask,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}}

	acl, err := windows.ACLFromEntries(entries, nil)
	if err != nil {
		return fmt.Errorf("构建 ACL 失败: %w", err)
	}

	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	); err != nil {
		return fmt.Errorf("设置文件 ACL 失败: %w", err)
	}
	runtime.KeepAlive(acl)
	return nil
}
