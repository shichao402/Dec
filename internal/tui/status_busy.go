package tui

// ioBusyLabel 返回当前飞行中 IO 的状态文案；空串表示空闲。
// 顺序按「用户应优先感知」排列；任一命中即短路。
func (m model) ioBusyLabel() string {
	switch {
	case m.runningDelete:
		return "Deleting… Esc cancel"
	case m.runningPull && m.runMode == "push":
		return "Push running… Esc cancel"
	case m.runningPull:
		return "Pull running… Esc cancel"
	case m.runningRemove:
		return "Remove running…"
	case m.updatingBinary:
		return "Updating… Esc cancel"
	case m.restartingServer || m.serverRestartStage == "running":
		return "Restarting dec-server…"
	case m.serverRestartStage == "confirm":
		return "y/Enter restart · n/Esc cancel"
	case m.updateStage == "checking":
		return "Checking updates…"
	case m.pushPreviewLoad.busy() || m.pushStage == "loading":
		return "Loading push preview…"
	case m.vaultApplyLoad.busy():
		return "Applying vault project…"
	case m.localProjectLoad.busy():
		return "Ensuring local project…"
	case m.projectInitLoad.busy():
		return "Initializing project…"
	case m.addSecretStage == addSecretStageRunning:
		return "Adding secret…"
	case m.savingAssets:
		return "Saving bundles…"
	case m.savingSettings:
		return "Saving settings…"
	case m.savingProjectSettings:
		return "Saving project settings…"
	case m.builtinAssetsLoad.busy():
		return "Syncing builtin IDE assets…"
	case m.deleteLoad.busy():
		if len(m.deleteCandidates) > 0 {
			return "Refreshing remote list…"
		}
		return "Loading remote list…"
	case m.projectVarsLoad.busy():
		return "Reloading project vars…"
	case m.globalVarsLoad.busy():
		return "Reloading global vars…"
	case m.shellRefresh.busy():
		return "Refreshing…"
	}
	return ""
}

// statusBarLeftHints 空闲时的左侧快捷键提示；忙碌时改为 IO 状态。
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
		return "q quit | j/k | PgUp/Dn | space | e edit | A add | d delete | r refresh"
	}
	return "q quit | j/k nav | l/h in-out | r refresh"
}
