package tui

// shellPage 是顶栏四页签的真枚举；字符串只用于展示。
type shellPage int

const (
	pageProject shellPage = iota
	pageImport
	pageSync
	pageSettings
)

func defaultPages() []string {
	return []string{"项目", "引入", "同步", "设置"}
}

func (m model) currentShellPage() shellPage {
	switch m.currentPage() {
	case "引入":
		return pageImport
	case "同步":
		return pageSync
	case "设置":
		return pageSettings
	default:
		return pageProject
	}
}

func (m model) isProjectPage() bool {
	return m.currentShellPage() == pageProject
}

func (m model) isImportPage() bool {
	return m.currentShellPage() == pageImport
}

func (m model) isSyncPage() bool {
	return m.currentShellPage() == pageSync
}

func (m model) isSettingsPage() bool {
	return m.currentShellPage() == pageSettings
}

// 旧名别名，避免散落的 Home/Bundles/Run 字符串判断。
func (m model) isHomePage() bool    { return m.isProjectPage() }
func (m model) isBundlesPage() bool { return m.isImportPage() }
func (m model) isRunPage() bool     { return m.isSyncPage() }
