package consoleopen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const consoleName = "dec-console"

func findConsoleExecutable() (string, error) {
	if path := firstExistingFile(consoleInstallCandidates()); path != "" {
		return path, nil
	}
	if path, err := exec.LookPath(consoleName); err == nil {
		return path, nil
	}
	if runtime.GOOS == "windows" {
		if path, err := exec.LookPath(consoleName + ".exe"); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("未找到已安装的 Dec Console；请先安装桌面客户端后再认证")
}

func firstExistingFile(paths []string) string {
	for _, path := range paths {
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func consoleInstallCandidates() []string {
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "windows":
		var paths []string
		for _, dir := range windowsInstallDirs() {
			paths = append(paths, windowsConsoleBinaries(dir)...)
		}
		return paths
	case "darwin":
		return []string{
			"/Applications/dec-console.app/Contents/MacOS/dec-console",
			"/Applications/dec-console.app/Contents/MacOS/app",
			filepath.Join(home, "Applications/dec-console.app/Contents/MacOS/dec-console"),
			filepath.Join(home, "Applications/dec-console.app/Contents/MacOS/app"),
		}
	default:
		return []string{
			filepath.Join(home, ".local/bin/dec-console"),
			"/usr/local/bin/dec-console",
		}
	}
}

func windowsInstallDirs() []string {
	var dirs []string
	if v := os.Getenv("LOCALAPPDATA"); v != "" {
		dirs = append(dirs, filepath.Join(v, consoleName), filepath.Join(v, "Programs", consoleName))
	}
	if v := os.Getenv("ProgramFiles"); v != "" {
		dirs = append(dirs, filepath.Join(v, consoleName))
	}
	if v := os.Getenv("ProgramFiles(x86)"); v != "" {
		dirs = append(dirs, filepath.Join(v, consoleName))
	}
	return dirs
}

func windowsConsoleBinaries(dir string) []string {
	return []string{
		filepath.Join(dir, consoleName+".exe"),
		filepath.Join(dir, "app.exe"),
	}
}
