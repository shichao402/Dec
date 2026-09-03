import { useCallback, useEffect, useRef, useState } from 'react'
import { LoaderCircle } from 'lucide-react'
import {
  authenticate,
  connectTarget,
  deleteConnection,
  disconnect,
  invokeTyped,
  listConnections,
  loadSavedPassword,
  probeRemoteHost,
  provisionRemoteHost,
  runOrWatchTyped,
  saveConnection,
  stopService,
} from '@/lib/api'
import { ActionCenter } from '@/components/action-feedback'
import { Sidebar } from '@/components/shell/sidebar'
import { TopBar } from '@/components/shell/top-bar'
import { Badge } from '@/components/ui/badge'
import { useActionRegistry, useOperationObserver } from '@/lib/action-context'
import { runningActions } from '@/lib/action-registry'
import { actionSpec, resource, shortInstanceId, type View } from '@/lib/console'
import {
  onOpenIntent,
  selectLocalConnection,
  takeOpenIntent,
} from '@/lib/open-intent'
import { ConnectPage } from '@/pages/connect-page'
import { GlobalAssetsPage } from '@/pages/global-assets-page'
import { OnboardingPage } from '@/pages/onboarding-page'
import { OverviewPage } from '@/pages/overview-page'
import { ProjectPage } from '@/pages/project-page'
import { ProjectsPage } from '@/pages/projects-page'
import { SettingsPage } from '@/pages/settings-page'
import { SyncPage, type PullHistoryEntry } from '@/pages/sync-page'
import { UnlockPage } from '@/pages/unlock-page'
import type {
  DeviceSummary,
  GlobalSettings,
  ManagedProject,
  PingInfo,
  PullResult,
  RemoteHostProbe,
  ProvisionRemoteResult,
  SavedConnection,
} from '@/lib/utils'

type Screen = 'connect' | 'unlock' | 'console'

const viewTitles: Record<View, string> = {
  overview: '概览',
  global: 'Global 资产',
  projects: '项目',
  project: '项目',
  sync: '同步记录',
  settings: '设备设置',
}

const emptyConn = (): SavedConnection => ({
  id: '',
  label: '新设备',
  kind: 'ssh',
  host: '127.0.0.1',
  port: 47653,
  ssh_host: '',
  ssh_user: '',
  tls: false,
  tls_server_name: '',
  auth_email: '',
  password_saved: false,
})

export default function App() {
  const [screen, setScreen] = useState<Screen>('connect')
  const [view, setView] = useState<View>('overview')
  const [saved, setSaved] = useState<SavedConnection[]>([])
  const [draft, setDraft] = useState<SavedConnection>(emptyConn())
  const [remoteProbe, setRemoteProbe] = useState<RemoteHostProbe | null>(null)
  const [provisionConfirm, setProvisionConfirm] = useState('')
  const [current, setCurrent] = useState<SavedConnection | null>(null)
  const [ping, setPing] = useState<PingInfo | null>(null)
  const [summary, setSummary] = useState<DeviceSummary | null>(null)
  const [settings, setSettings] = useState<GlobalSettings | null>(null)
  const [selectedProject, setSelectedProject] = useState<ManagedProject | null>(null)
  const [history, setHistory] = useState<PullHistoryEntry[]>([])
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [rememberPassword, setRememberPassword] = useState(false)
  const [totp, setTotp] = useState('')
  const [need2fa, setNeed2fa] = useState(false)
  const openIntentBusy = useRef(false)
  const openIntentRerun = useRef(false)
  const handledUnlockFailure = useRef('')
  const handleConnectRef = useRef<(conn: SavedConnection, forceUnlock?: boolean) => Promise<void>>(
    () => Promise.resolve(),
  )
  const actions = useActionRegistry()
  const runAction = actions.run
  const activeActions = runningActions(actions.state)
  const sessionAction = activeActions.find((record) => record.kind === 'session')
  const connectionBusy = activeActions.some((record) => record.resources.includes(resource.connections))
  const operationAction = Object.values(actions.state.records)
    .filter((record) => record.kind === 'operation')
    .sort((a, b) => (b.finishedAt || b.startedAt) - (a.finishedAt || a.startedAt))[0]
  const busy = Boolean(sessionAction)
  const busyMessage = sessionAction?.label || ''
  // 顶栏只提示会话与长任务：后台读取也进来会让状态区一直在闪。
  const foregroundAction = sessionAction
    || activeActions.find((record) => record.kind === 'operation' || record.kind === 'write')
  const events = operationAction?.events || []
  const deviceId = current?.id || 'console'
  const observedRoots = ['', ...(summary?.Projects.map((project) => project.Root) || [])]

  useOperationObserver(deviceId, observedRoots, screen === 'console' && Boolean(current))

  useEffect(() => {
    if (!current) return
    const failure = Object.values(actions.state.records)
      .filter((record) => record.deviceId === current.id
        && record.status === 'failed'
        && isUnlockRequiredError(record.error || ''))
      .sort((a, b) => (b.finishedAt || 0) - (a.finishedAt || 0))[0]
    if (!failure) return
    const id = `${failure.key}:${failure.generation}`
    if (handledUnlockFailure.current === id) return
    handledUnlockFailure.current = id
    setNeed2fa(false)
    setTotp('')
    setScreen('unlock')
  }, [actions.state.records, current])

  useEffect(() => {
    const spec = actionSpec('connections:list', '加载设备连接', 'console', [resource.connections], 'read')
    void runAction(spec, listConnections).then((outcome) => {
      if (outcome.ok) setSaved(outcome.value)
    })
  }, [runAction])

  const fetchDevice = useCallback(async (actionKey: string) => {
    const [device, globalSettings] = await Promise.all([
      invokeTyped<DeviceSummary>('load_device_summary', '', 'global', {}, actionKey),
      invokeTyped<GlobalSettings>('load_global_settings', '', 'global', {}, actionKey),
    ])
    return { device, globalSettings }
  }, [])

  async function handleConnect(conn: SavedConnection, forceUnlock = false) {
    setEmail(conn.auth_email || '')
    setPassword('')
    setRememberPassword(conn.password_saved)
    setNeed2fa(false)
    setTotp('')
    const spec = actionSpec(`session:connect:${conn.id}`, `正在连接 ${conn.label}`, conn.id, [resource.session], 'session', `已连接 ${conn.label}`)
    const outcome = await actions.run(spec, async () => {
      const savedPassword = conn.password_saved && conn.id ? await loadSavedPassword(conn.id) : ''
      const info = await connectTarget({
        kind: conn.kind,
        host: conn.host,
        port: conn.port,
        sshHost: conn.ssh_host,
        sshUser: conn.ssh_user,
        tls: conn.tls,
        tlsServerName: conn.tls_server_name,
      })
      const loaded = info.unlocked ? await fetchDevice(spec.key) : null
      return { info, loaded, savedPassword }
    })
    if (!outcome.ok) return
    const { info, loaded, savedPassword } = outcome.value
    if (savedPassword) setPassword(savedPassword)
    setCurrent(conn)
    setPing(info)
    setSummary(loaded?.device || null)
    setSettings(loaded?.globalSettings || null)
    setSelectedProject(null)
    setView('overview')
    setScreen(forceUnlock ? 'unlock' : info.unlocked ? 'console' : 'unlock')
  }
  handleConnectRef.current = handleConnect

  useEffect(() => {
    const drainOpenIntents = async () => {
      if (openIntentBusy.current) {
        openIntentRerun.current = true
        return
      }
      openIntentBusy.current = true
      try {
        do {
          openIntentRerun.current = false
          let intent = await takeOpenIntent()
          while (intent) {
            if (intent === 'unlock-local') {
              const connections = await listConnections()
              setSaved(connections)
              await handleConnectRef.current(selectLocalConnection(connections), true)
            }
            intent = await takeOpenIntent()
          }
        } while (openIntentRerun.current)
      } finally {
        openIntentBusy.current = false
        if (openIntentRerun.current) void drainOpenIntents()
      }
    }

    const unlisten = onOpenIntent(() => void drainOpenIntents())
    void drainOpenIntents()
    return () => {
      void unlisten.then((dispose) => dispose())
    }
  }, [])

  function updateDraft(next: SavedConnection) {
    const targetChanged = next.kind !== draft.kind || next.ssh_host !== draft.ssh_host
    setDraft(next)
    if (targetChanged) {
      setRemoteProbe(null)
      setProvisionConfirm('')
    }
  }

  async function handleProbeRemote() {
    const target = draft.ssh_host.trim()
    if (!target) return
    const spec = actionSpec(
      `device:probe:${target}`,
      `正在检测 ${target}`,
      'console',
      [resource.device(target), resource.connections],
      'read',
      '远端检测完成',
    )
    const outcome = await actions.run(spec, () => probeRemoteHost<RemoteHostProbe>(target), { force: true })
    if (outcome.ok) setRemoteProbe(outcome.value)
  }

  async function handleProvisionRemote() {
    const target = draft.ssh_host.trim()
    if (!target) return
    const spec = actionSpec(
      `operation:provision:${target}`,
      `正在置备 ${target}`,
      'console',
      [resource.device(target), resource.connections],
      'operation',
      '远端置备完成',
    )
    const outcome = await actions.run(spec, () => provisionRemoteHost<ProvisionRemoteResult>({
      alias: draft.label.trim() || target,
      sshTarget: target,
      confirm: provisionConfirm,
      actionKey: spec.key,
    }))
    if (!outcome.ok) return
    setRemoteProbe(outcome.value.Verify)
    setProvisionConfirm('')
    await handleSaveAndConnect(true)
  }

  async function handleSaveAndConnect(remoteReady = false) {
    if (draft.kind === 'ssh' && !remoteReady && !remoteProbe?.ListenReady) {
      await handleProbeRemote()
      return
    }
    const spec = actionSpec(`connections:save:${draft.id || 'new'}`, '正在保存连接', 'console', [resource.connections], 'write', '连接已保存')
    const outcome = await actions.run(spec, async () => {
      const stored = await saveConnection(draft)
      return { stored, all: await listConnections() }
    })
    if (!outcome.ok) return
    setDraft(outcome.value.stored)
    setSaved(outcome.value.all)
    await handleConnect(outcome.value.stored)
  }

  async function handleUnlock() {
    if (!current) return
    const spec = actionSpec(`session:unlock:${current.id}`, '正在解锁设备', current.id, [resource.session], 'session', '设备已解锁')
    const outcome = await actions.run(spec, async () => {
      let stored = current
      if (current.auth_email !== email.trim()) {
        stored = await saveConnection({ ...current, auth_email: email.trim() })
      }
      const result = await authenticate(email, password, totp, true)
      if (result.error) throw new Error(result.error)
      if (!result.unlocked) return { result, stored, loaded: null, all: await listConnections() }
      stored = await saveConnection(
        { ...stored, auth_email: email.trim(), password_saved: rememberPassword },
        rememberPassword ? password : undefined,
      )
      return {
        result,
        stored,
        loaded: await fetchDevice(spec.key),
        all: await listConnections(),
      }
    })
    if (!outcome.ok) return
    const { result, stored, loaded, all } = outcome.value
    setCurrent(stored)
    setSaved(all)
    if (result.need_2fa) {
      setNeed2fa(true)
      return
    }
    if (result.unlocked && loaded) {
      setPassword('')
      setTotp('')
      setSummary(loaded.device)
      setSettings(loaded.globalSettings)
      setScreen('console')
    }
  }

  async function handleDisconnect() {
    const disconnecting = current
    if (!disconnecting) return
    const spec = actionSpec(`session:disconnect:${disconnecting.id}`, '正在断开设备', disconnecting.id, [resource.session], 'session', '设备已断开')
    const outcome = await actions.run(spec, async () => {
      await disconnect().catch(() => undefined)
    })
    if (!outcome.ok) return
    setScreen('connect')
    setCurrent(null)
    setPing(null)
    setSummary(null)
    setSettings(null)
  }

  async function refreshDevice() {
    if (!current) return
    const spec = actionSpec(`device:refresh:${current.id}`, '正在刷新设备状态', current.id, [resource.global], 'read', '设备状态已刷新')
    const outcome = await actions.run(spec, () => fetchDevice(spec.key), { force: true })
    if (!outcome.ok) return
    setSummary(outcome.value.device)
    setSettings(outcome.value.globalSettings)
  }

  async function restartService() {
    if (!current) return
    const conn = current
    const spec = actionSpec(`session:restart:${conn.id}`, '正在重启服务并重连', conn.id, [resource.session], 'session', '服务已重启')
    const outcome = await actions.run(spec, async () => {
      await stopService()
      await new Promise((resolve) => setTimeout(resolve, 1200))
      const info = await connectTarget({
        kind: conn.kind,
        host: conn.host,
        port: conn.port,
        sshHost: conn.ssh_host,
        sshUser: conn.ssh_user,
        tls: conn.tls,
        tlsServerName: conn.tls_server_name,
      })
      return { info, loaded: info.unlocked ? await fetchDevice(spec.key) : null }
    })
    if (!outcome.ok) return
    setPing(outcome.value.info)
    setSummary(outcome.value.loaded?.device || null)
    setSettings(outcome.value.loaded?.globalSettings || null)
    setScreen(outcome.value.info.unlocked ? 'console' : 'unlock')
  }

  async function pull(title: string, root: string, plane: 'local' | 'global') {
    if (!current) return
    const spec = actionSpec(`operation:pull:${current.id}:${root || 'global'}`, `正在拉取 ${title} 资产`, current.id, [resource.workspace(root)], 'operation', `${title} 资产拉取完成`)
    const outcome = await actions.run(spec, () => runOrWatchTyped<PullResult>({
      actionKey: spec.key,
      operation: 'pull',
      projectRoot: root,
      workspacePlane: plane,
    }))
    if (!outcome.ok) {
      if (isUnlockRequiredError(outcome.error)) setScreen('unlock')
      return
    }
    setHistory((items) => [{ title, result: outcome.value, at: new Date() }, ...items].slice(0, 20))
    setView('sync')
  }

  async function handleDeleteConnection(id: string) {
    const spec = actionSpec(`connections:delete:${id}`, '正在删除连接', 'console', [resource.connections], 'write', '连接已删除')
    const outcome = await actions.run(spec, async () => {
      await deleteConnection(id)
      return listConnections()
    })
    if (outcome.ok) setSaved(outcome.value)
  }

  const consoleReady = screen === 'console' && summary && settings
  const onboarding = Boolean(consoleReady && summary && !summary.Initialized)
  const crumbs = buildCrumbs({ screen, view, onboarding, deviceLabel: current?.label, project: selectedProject })

  return (
    <div className="flex h-screen min-h-0 overflow-hidden bg-canvas text-ink">
      <ActionCenter onRestart={current ? restartService : undefined} />
      {busy && (
        <div
          className="fixed inset-0 z-50 flex items-start justify-center bg-canvas/70 pt-24 backdrop-blur-sm"
          role="status"
          aria-live="polite"
        >
          <div className="flex items-center gap-3 rounded-xl border border-line bg-panel px-4 py-3 text-[13px] shadow-xl shadow-black/50">
            <LoaderCircle className="size-4 animate-spin text-accent-hi" />
            <span>{busyMessage || '操作正在执行'}，完成前已锁定其他操作</span>
          </div>
        </div>
      )}
      {screen === 'console' && (
        <Sidebar
          saved={saved}
          current={current}
          ping={ping}
          view={view}
          projectCount={summary?.Projects.length || 0}
          project={selectedProject}
          onboarding={onboarding}
          onView={(next) => {
            if (next !== 'project') setSelectedProject(null)
            setView(next)
          }}
          onProject={(project) => {
            setSelectedProject(project)
            setView('project')
          }}
          onConnect={handleConnect}
          onDisconnect={handleDisconnect}
          busy={busy}
        />
      )}
      <main className="flex min-w-0 flex-1 flex-col">
        <TopBar
          crumbs={crumbs}
          busyLabel={foregroundAction?.label}
          right={
            consoleReady && summary && ping ? (
              <>
                <Badge tone={summary.RepoConnected ? 'good' : 'warn'}>
                  {summary.RepoConnected ? '私仓已连接' : '私仓未连接'}
                </Badge>
                <span className="text-[11px] text-faint">{summary.Platform}</span>
                <span className="font-mono text-[11px] text-faint" title={ping.instance_id}>
                  实例 {shortInstanceId(ping.instance_id)}
                </span>
              </>
            ) : undefined
          }
        />
        <div className="min-h-0 flex-1">
          {screen === 'connect' && (
            <ConnectPage
              saved={saved}
              draft={draft}
              setDraft={updateDraft}
              onConnect={handleConnect}
              onSaveConnect={handleSaveAndConnect}
              remoteProbe={remoteProbe}
              provisionConfirm={provisionConfirm}
              setProvisionConfirm={setProvisionConfirm}
              onProbeRemote={handleProbeRemote}
              onProvisionRemote={handleProvisionRemote}
              onResetDraft={() => {
                setDraft(emptyConn())
                setRemoteProbe(null)
                setProvisionConfirm('')
              }}
              busy={busy || connectionBusy}
              onDelete={handleDeleteConnection}
            />
          )}
          {screen === 'unlock' && (
            <UnlockPage
              deviceLabel={current?.label || '设备'}
              ping={ping}
              email={email}
              password={password}
              totp={totp}
              need2fa={need2fa}
              rememberPassword={rememberPassword}
              setEmail={setEmail}
              setPassword={setPassword}
              setTotp={setTotp}
              setRememberPassword={setRememberPassword}
              onUnlock={handleUnlock}
              onBack={handleDisconnect}
              busy={busy}
            />
          )}
          {consoleReady && summary && settings && !summary.Initialized && (
            <OnboardingPage
              deviceId={deviceId}
              settings={settings}
              setSettings={setSettings}
              onComplete={refreshDevice}
              onPull={async () => {
                await refreshDevice()
                await pull('Global', '', 'global')
              }}
            />
          )}
          {consoleReady && summary && settings && summary.Initialized && (
            <>
              {view === 'overview' && (
                <OverviewPage
                  deviceId={deviceId}
                  summary={summary}
                  settings={settings}
                  lastPull={history[0]}
                  onRefresh={refreshDevice}
                  onNavigate={setView}
                  onOpenProject={(project) => {
                    setSelectedProject(project)
                    setView('project')
                  }}
                  onPullGlobal={() => pull('Global', '', 'global')}
                />
              )}
              {view === 'global' && (
                <GlobalAssetsPage
                  deviceId={deviceId}
                  repoURL={summary.RepoURL}
                  onPull={() => pull('Global', '', 'global')}
                />
              )}
              {view === 'projects' && (
                <ProjectsPage
                  deviceId={deviceId}
                  projects={summary.Projects}
                  onRefresh={refreshDevice}
                  onOpen={(project) => {
                    setSelectedProject(project)
                    setView('project')
                  }}
                />
              )}
              {view === 'project' && selectedProject && (
                <ProjectPage
                  deviceId={deviceId}
                  project={selectedProject}
                  onPull={() => pull(selectedProject.Name, selectedProject.Root, 'local')}
                  onChanged={refreshDevice}
                  onRemoved={async () => {
                    setSelectedProject(null)
                    setView('projects')
                    await refreshDevice()
                  }}
                />
              )}
              {view === 'sync' && <SyncPage events={events} history={history} />}
              {view === 'settings' && (
                <SettingsPage
                  deviceId={deviceId}
                  local={current?.kind === 'local'}
                  settings={settings}
                  summary={summary}
                  ping={ping}
                  setSettings={setSettings}
                  onSaved={refreshDevice}
                  onRestart={restartService}
                />
              )}
            </>
          )}
        </div>
      </main>
    </div>
  )
}

function isUnlockRequiredError(error: string) {
  return error.includes('CONSOLE_UNLOCK_') || error.includes('Unauthenticated') || error.includes('锁定')
}

function buildCrumbs(input: {
  screen: Screen
  view: View
  onboarding: boolean
  deviceLabel?: string
  project: ManagedProject | null
}) {
  if (input.screen === 'connect') return ['Dec Console', '设备']
  const device = input.deviceLabel || 'Dec Console'
  if (input.screen === 'unlock') return [device, '解锁']
  if (input.onboarding) return [device, '初始化设备']
  if (input.view === 'project') {
    return [device, viewTitles.projects, input.project?.Label || input.project?.Name || '项目']
  }
  return [device, viewTitles[input.view]]
}
