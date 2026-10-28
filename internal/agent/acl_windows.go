//go:build windows

package agent

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// restrictToOwner 把文件的 DACL 改成只允许当前用户完全控制，并切断继承（需求 1.4）。
//
// Windows 上 0600 不起作用：文件继承用户目录的 ACL，Users 组可读，
// OpenSSH 会以 UNPROTECTED PRIVATE KEY FILE 拒绝加载私钥。
func restrictToOwner(path string) error {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return fmt.Errorf("取当前用户失败: %w", err)
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.SET_ACCESS,
		Inheritance:       windows.NO_INHERITANCE,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(user.User.Sid),
		},
	}}, nil)
	if err != nil {
		return fmt.Errorf("构造 ACL 失败: %w", err)
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil)
}
