package update

import "testing"

func TestManualInstallCommand(t *testing.T) {
	linuxCmd := manualInstallCommand("linux")
	if linuxCmd != "curl -fsSL https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/scripts/install.sh | bash" {
		t.Fatalf("linux 安装命令错误: %s", linuxCmd)
	}

	windowsCmd := manualInstallCommand("windows")
	if windowsCmd != "iwr -useb https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/scripts/install.ps1 | iex" {
		t.Fatalf("windows 安装命令错误: %s", windowsCmd)
	}
}
