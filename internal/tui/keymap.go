package tui

// 底栏空闲快捷键的单一来源。覆盖态（新建向导、远端、推断确认）优先于页签默认。
func (m model) statusBarLeftHints() string {
	if busy := m.ioBusyLabel(); busy != "" {
		return busy
	}
	if m.isHomePage() && m.hasVaultInferencePrompt() {
		return "y/Enter apply · n skip | q quit | r refresh"
	}
	if m.isRemotePage() {
		if m.deleteStage == "summary" {
			return "y/Enter continue · n/Esc cancel"
		}
		if m.deleteStage == "typed" {
			return "type folder or DELETE · Enter confirm · Esc back"
		}
		if m.deleteStage == "confirm" {
			return "y confirm delete · n/Esc back"
		}
		if m.deleteFilterInput {
			return "filter · Enter apply · Esc cancel"
		}
		if m.addSecretStage != "" {
			return "register · tab switch · Enter next · Esc cancel"
		}
		return "q quit | j/k | space | a all | A none | n/N add | e edit | d delete | r refresh"
	}
	if m.createStage != "" {
		return "j/k 类型 · Enter 下一步 · Esc 取消 | q quit"
	}
	switch m.currentShellPage() {
	case pageProject:
		return "n 新建 · q quit | Tab 页签 | ^R 远端 | r refresh"
	case pageImport:
		return "space 勾选 · s 保存 · / 筛选 | q quit | r refresh"
	case pageSync:
		return "p 拉取 · P 推送 · u 自更新 | q quit | r refresh"
	case pageSettings:
		return "e 编辑 · s 保存 | q quit | r refresh"
	}
	return "q quit | Tab 页签 | r refresh"
}
