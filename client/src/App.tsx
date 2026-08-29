import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import {
  authenticate,
  connectTarget,
  deleteConnection,
  disconnect,
  invokeTyped,
  listConnections,
  loadSavedPassword,
  runOrWatchTyped,
  saveConnection,
  stopService,
} from '@/lib/api'
import { Button } from '@/components/ui/button'
import { ActionButton } from '@/components/ui/action-button'
import { ActionCenter, ActionFeedback } from '@/components/action-feedback'
import { Card, Input, Label } from '@/components/ui/input'
import { useActionRegistry, useDecAction, useOperationObserver } from '@/lib/action-context'
import { runningActions, type ActionSpec } from '@/lib/action-registry'
import type {
  AssetSelection,
  DeviceSummary,
  DirectoryListing,
  GlobalSettings,
  ManagedProject,
  OperationEvent,
  PingInfo,
  PullResult,
  SavedConnection,
} from '@/lib/utils'
import { pullResultDiagnosis } from '@/lib/utils'
import {
  Boxes,
  CheckCircle2,
  ChevronRight,
  Folder,
  FolderSearch,
  Globe,
  LayoutDashboard,
  LoaderCircle,
  LogOut,
  Plus,
  RefreshCw,
  Server,
  Settings,
  Trash2,
  TriangleAlert,
} from 'lucide-react'

type Screen = 'connect' | 'unlock' | 'console'
type View = 'overview' | 'global' | 'projects' | 'project' | 'sync' | 'settings'

const emptyConn = (): SavedConnection => ({
  id: '',
  label: '新设备',
  kind: 'ssh',
  host: '127.0.0.1',
  port: 37820,
  ssh_host: '',
  ssh_user: '',
  tls: false,
  tls_server_name: '',
  auth_email: '',
  password_saved: false,
})

const resource = {
  connections: 'console:connections',
  session: 'session',
  global: 'workspace:global',
  workspace: (root: string) => root ? `workspace:${root}` : 'workspace:global',
  filesystem: 'device:filesystem',
}

function actionSpec(
  key: string,
  label: string,
  deviceId: string,
  resources: string[],
  kind: ActionSpec['kind'],
  successMessage?: string,
): ActionSpec {
  return { key, label, deviceId: deviceId || 'console', resources, kind, successMessage }
}

export default function App() {
  const [screen, setScreen] = useState<Screen>('connect')
  const [view, setView] = useState<View>('overview')
  const [saved, setSaved] = useState<SavedConnection[]>([])
  const [draft, setDraft] = useState<SavedConnection>(emptyConn())
  const [current, setCurrent] = useState<SavedConnection | null>(null)
  const [ping, setPing] = useState<PingInfo | null>(null)
  const [summary, setSummary] = useState<DeviceSummary | null>(null)
  const [settings, setSettings] = useState<GlobalSettings | null>(null)
  const [selectedProject, setSelectedProject] = useState<ManagedProject | null>(null)
  const [history, setHistory] = useState<{ title: string; result: PullResult; at: Date }[]>([])
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [rememberPassword, setRememberPassword] = useState(false)
  const [totp, setTotp] = useState('')
  const [need2fa, setNeed2fa] = useState(false)
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
  const events = operationAction?.events || []
  const deviceId = current?.id || 'console'
  const observedRoots = ['', ...(summary?.Projects.map((project) => project.Root) || [])]

  useOperationObserver(deviceId, observedRoots, screen === 'console' && Boolean(current))

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

  async function handleConnect(conn: SavedConnection) {
    setEmail(conn.auth_email || '')
    setPassword('')
    setRememberPassword(conn.password_saved)
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
    setScreen(info.unlocked ? 'console' : 'unlock')
  }

  async function handleSaveAndConnect() {
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
      if (outcome.error.includes('Unauthenticated') || outcome.error.includes('锁定')) setScreen('unlock')
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

  return (
    <div className="dark flex h-screen min-h-0 bg-zinc-950 text-zinc-100">
      <ActionCenter onRestart={current ? restartService : undefined} />
      {busy && (
        <div className="fixed inset-0 z-50 flex items-start justify-center bg-black/35 pt-20" role="status" aria-live="polite">
          <div className="flex items-center gap-3 rounded-lg border border-zinc-700 bg-zinc-900 px-5 py-3 text-sm shadow-xl">
            <LoaderCircle className="h-4 w-4 animate-spin" />
            {busyMessage || '操作正在执行'}，完成前已锁定其他操作
          </div>
        </div>
      )}
      {screen === 'console' && (
        <Sidebar
          saved={saved}
          current={current}
          ping={ping}
          view={view}
          project={selectedProject}
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
      <main className="min-w-0 flex-1 overflow-auto">
        <header className="sticky top-0 z-10 flex h-16 items-center justify-between border-b border-zinc-800 bg-zinc-950/95 px-6 backdrop-blur">
          <div>
            <div className="font-semibold">{screen === 'connect' ? '设备' : current?.label || 'Dec Console'}</div>
            <div className="text-xs text-zinc-500">
              {screen === 'console' ? `${ping?.instance_id ?? ''} · ${summary?.Platform ?? ''}` : '管理本机与远程 dec-server'}
            </div>
          </div>
          {busy && <div className="flex items-center gap-2 text-xs text-zinc-400"><LoaderCircle className="h-5 w-5 animate-spin" />{busyMessage || '正在处理，请稍候'}</div>}
        </header>
        <div className="mx-auto max-w-6xl p-6">
          {screen === 'connect' && (
            <ConnectionPage
              saved={saved}
              draft={draft}
              setDraft={setDraft}
              onConnect={handleConnect}
              onSaveConnect={handleSaveAndConnect}
              busy={busy || connectionBusy}
              onDelete={handleDeleteConnection}
            />
          )}
          {screen === 'unlock' && (
            <UnlockPage
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
          {consoleReady && !summary.Initialized && (
            <Onboarding
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
          {consoleReady && summary.Initialized && view === 'overview' && (
            <Overview deviceId={deviceId} summary={summary} settings={settings} onRefresh={refreshDevice} onNavigate={setView} />
          )}
          {consoleReady && summary.Initialized && view === 'global' && (
            <AssetsPage
              deviceId={deviceId}
              title="Global 资产"
              description="这些资产安装到当前设备的用户环境，不属于任何单个项目。"
              root=""
              plane="global"
              onPull={() => pull('Global', '', 'global')}
            />
          )}
          {consoleReady && summary.Initialized && view === 'projects' && (
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
          {consoleReady && summary.Initialized && view === 'project' && selectedProject && (
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
          {consoleReady && summary.Initialized && view === 'sync' && <SyncPage events={events} history={history} />}
          {consoleReady && summary.Initialized && view === 'settings' && (
            <DeviceSettings
              deviceId={deviceId}
              settings={settings}
              setSettings={setSettings}
              onSaved={refreshDevice}
              onRestart={restartService}
              version={ping?.version ?? ''}
            />
          )}
        </div>
      </main>
    </div>
  )
}

function Sidebar(props: {
  saved: SavedConnection[]
  current: SavedConnection | null
  ping: PingInfo | null
  view: View
  project: ManagedProject | null
  onView: (view: View) => void
  onProject: (project: ManagedProject) => void
  onConnect: (conn: SavedConnection) => void
  onDisconnect: () => void
  busy: boolean
}) {
  const nav: [View, string, typeof Globe][] = [
    ['overview', '概览', LayoutDashboard],
    ['global', 'Global 资产', Globe],
    ['projects', '项目', Folder],
    ['sync', '同步记录', RefreshCw],
    ['settings', '设备设置', Settings],
  ]
  return (
    <aside className="flex w-64 shrink-0 flex-col border-r border-zinc-800 bg-zinc-900">
      <div className="border-b border-zinc-800 p-3">
        <div className="mb-2 px-2 text-xs font-medium uppercase tracking-wider text-zinc-500">设备</div>
        <div className="space-y-1">
          {props.saved.map((conn) => (
            <button
              key={conn.id}
              onClick={() => props.onConnect(conn)}
              disabled={props.busy}
              className={`flex w-full items-center gap-2 rounded-md px-2 py-2 text-left text-sm ${props.current?.id === conn.id ? 'bg-zinc-700' : 'hover:bg-zinc-800'}`}
            >
              <span className={`h-2 w-2 rounded-full ${props.current?.id === conn.id ? 'bg-emerald-400' : 'bg-zinc-600'}`} />
              <span className="min-w-0 flex-1 truncate">{conn.label}</span>
            </button>
          ))}
        </div>
      </div>
      <nav className="space-y-1 p-3">
        {nav.map(([id, label, Icon]) => (
          <button
            key={id}
            onClick={() => props.onView(id)}
            disabled={props.busy}
            className={`flex w-full items-center gap-2 rounded-md px-2 py-2 text-sm ${props.view === id ? 'bg-zinc-100 text-zinc-950' : 'text-zinc-300 hover:bg-zinc-800'}`}
          >
            <Icon className="h-4 w-4" /> {label}
          </button>
        ))}
        {props.project && (
          <button
            onClick={() => props.onProject(props.project!)}
            disabled={props.busy}
            className={`ml-4 flex w-[calc(100%-1rem)] items-center gap-2 rounded-md px-2 py-2 text-sm ${props.view === 'project' ? 'bg-zinc-700' : 'hover:bg-zinc-800'}`}
          >
            <ChevronRight className="h-4 w-4" />
            <span className="truncate">{props.project.Name}</span>
          </button>
        )}
      </nav>
      <div className="mt-auto border-t border-zinc-800 p-3 text-xs text-zinc-500">
        <div className="mb-2">Dec {props.ping?.version || '—'}</div>
        <Button variant="outline" size="sm" className="w-full" onClick={props.onDisconnect} disabled={props.busy}>
          <LogOut className="mr-2 h-4 w-4" /> 断开
        </Button>
      </div>
    </aside>
  )
}

function ConnectionPage(props: {
  saved: SavedConnection[]
  draft: SavedConnection
  setDraft: (value: SavedConnection) => void
  onConnect: (conn: SavedConnection) => void
  onSaveConnect: () => void
  onDelete: (id: string) => void
  busy: boolean
}) {
  const { draft, setDraft } = props
  return (
    <div className="grid gap-6 lg:grid-cols-[1.1fr_.9fr]">
      <section>
        <h1 className="mb-2 text-3xl font-semibold">选择设备</h1>
        <p className="mb-6 text-zinc-400">连接后可管理该设备的 Global 资产、项目和同步任务。</p>
        <div className="space-y-3">
          {props.saved.map((conn) => (
            <Card key={conn.id} className="flex items-center gap-4">
              <div className="rounded-lg bg-zinc-800 p-3"><Server className="h-5 w-5" /></div>
              <div className="min-w-0 flex-1">
                <div className="font-medium">{conn.label}</div>
                <div className="truncate text-xs text-zinc-500">{connectionAddress(conn)}</div>
              </div>
              <Button size="sm" onClick={() => props.onConnect(conn)} disabled={props.busy}>{props.busy ? '连接中…' : '连接'}</Button>
              <Button size="sm" variant="ghost" onClick={() => props.setDraft(conn)} disabled={props.busy}>编辑</Button>
              <Button size="sm" variant="ghost" onClick={() => props.onDelete(conn.id)} disabled={props.busy}><Trash2 className="h-4 w-4" /></Button>
            </Card>
          ))}
          {props.saved.length === 0 && <EmptyState text="还没有保存的设备，请从右侧添加。" />}
        </div>
      </section>
      <Card>
        <h2 className="mb-4 flex items-center gap-2 text-lg font-medium"><Plus className="h-4 w-4" /> 添加设备</h2>
        <div className="space-y-3">
          <Field label="名称"><Input value={draft.label} onChange={(e) => setDraft({ ...draft, label: e.target.value })} /></Field>
          <Field label="连接方式">
            <select className={selectClass} value={draft.kind} onChange={(e) => setDraft({ ...draft, kind: e.target.value as SavedConnection['kind'] })}>
              <option value="local">本机</option>
              <option value="ssh">SSH 隧道</option>
              <option value="remote">TLS gRPC</option>
            </select>
          </Field>
          {draft.kind !== 'local' && (
            <div className="grid grid-cols-[1fr_8rem] gap-3">
              <Field label="gRPC Host"><Input value={draft.host} onChange={(e) => setDraft({ ...draft, host: e.target.value })} /></Field>
              <Field label="端口"><Input type="number" value={draft.port} onChange={(e) => setDraft({ ...draft, port: Number(e.target.value) })} /></Field>
            </div>
          )}
          {draft.kind === 'ssh' && (
            <div className="grid grid-cols-2 gap-3">
              <Field label="SSH 主机"><Input value={draft.ssh_host} onChange={(e) => setDraft({ ...draft, ssh_host: e.target.value })} /></Field>
              <Field label="SSH 用户"><Input value={draft.ssh_user} onChange={(e) => setDraft({ ...draft, ssh_user: e.target.value })} /></Field>
            </div>
          )}
          {draft.kind === 'remote' && (
            <>
              <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={draft.tls} onChange={(e) => setDraft({ ...draft, tls: e.target.checked })} /> 启用 TLS（远程直连必须开启）</label>
              <Field label="证书服务器名（可选）"><Input value={draft.tls_server_name} onChange={(e) => setDraft({ ...draft, tls_server_name: e.target.value })} /></Field>
            </>
          )}
          <Field label="Bitwarden 邮箱（可选）"><Input autoComplete="username" value={draft.auth_email} onChange={(e) => setDraft({ ...draft, auth_email: e.target.value })} /></Field>
          {draft.password_saved && <p className="text-xs text-emerald-400">此连接的密码已保存在系统凭据库，可在下次解锁时取消保存。</p>}
          <Button className="w-full" onClick={props.onSaveConnect} disabled={props.busy}>{props.busy ? '保存并连接中…' : '保存并连接'}</Button>
        </div>
      </Card>
    </div>
  )
}

function UnlockPage(props: {
  ping: PingInfo | null
  email: string
  password: string
  totp: string
  need2fa: boolean
  rememberPassword: boolean
  setEmail: (value: string) => void
  setPassword: (value: string) => void
  setTotp: (value: string) => void
  setRememberPassword: (value: boolean) => void
  onUnlock: () => void
  onBack: () => void
  busy: boolean
}) {
  return (
    <Card className="mx-auto max-w-md">
      <h1 className="mb-2 text-xl font-semibold">解锁设备</h1>
      <p className="mb-5 text-sm text-zinc-400">实例 {props.ping?.instance_id} 已锁定。凭据只发送给该 dec-server，并保存在其进程内存中。</p>
      <div className="space-y-3">
        <Field label="Bitwarden 邮箱"><Input autoComplete="username" value={props.email} onChange={(e) => props.setEmail(e.target.value)} /></Field>
        <Field label="主密码"><Input type="password" autoComplete="current-password" value={props.password} onChange={(e) => props.setPassword(e.target.value)} /></Field>
        <label className="flex items-center gap-2 text-sm text-zinc-300">
          <input type="checkbox" checked={props.rememberPassword} onChange={(e) => props.setRememberPassword(e.target.checked)} />
          使用系统凭据库加密保存此连接的密码
        </label>
        {props.need2fa && <Field label="TOTP"><Input value={props.totp} onChange={(e) => props.setTotp(e.target.value)} /></Field>}
        <div className="flex gap-2"><Button onClick={props.onUnlock} disabled={props.busy}>{props.busy ? '解锁中…' : '解锁'}</Button><Button variant="ghost" onClick={props.onBack} disabled={props.busy}>返回</Button></div>
      </div>
    </Card>
  )
}

function Onboarding(props: {
  deviceId: string
  settings: GlobalSettings
  setSettings: (value: GlobalSettings) => void
  onComplete: () => void | Promise<void>
  onPull: () => void | Promise<void>
}) {
  const [step, setStep] = useState(1)
  const [repoURL, setRepoURL] = useState(props.settings.RepoURL)
  const [ides, setIDEs] = useState<string[]>(props.settings.SelectedIDEs.length ? props.settings.SelectedIDEs : ['cursor'])
  const [idle, setIdle] = useState(props.settings.ServerIdleTimeout || '30m')
  const [assets, setAssets] = useState<AssetSelection | null>(null)
  const [selected, setSelected] = useState<string[]>([])
  const saveDeviceSpec = actionSpec('onboarding:settings', '验证并保存设备设置', props.deviceId, [resource.global], 'write', '设备设置已保存')
  const saveAssetsSpec = actionSpec('onboarding:assets', '保存 Global 资产选择', props.deviceId, [resource.global], 'write', 'Global 资产选择已保存')

  const saveDevice = async () => {
      const saved = await invokeTyped<{ RepoAuthRequired?: boolean; RepoHost?: string; ConnectError?: string }>('save_global_settings', '', 'global', { RepoURL: repoURL, IDEs: ides, ServerIdleTimeout: idle }, saveDeviceSpec.key)
      if (saved.RepoAuthRequired || saved.ConnectError) {
        throw new Error(`私仓认证失败${saved.RepoHost ? `（${saved.RepoHost}）` : ''}：${saved.ConnectError || '需要先配置 Git 凭据'}`)
      }
      const next = await invokeTyped<GlobalSettings>('load_global_settings', '', 'global', {}, saveDeviceSpec.key)
      props.setSettings(next)
      return invokeTyped<AssetSelection>('load_asset_selection', '', 'global', {}, saveDeviceSpec.key)
  }

  const saveAssets = () => invokeTyped('save_enabled_bundles', '', 'global', { EnabledProjects: selected }, saveAssetsSpec.key)

  return (
    <div className="mx-auto max-w-3xl">
      <div className="mb-6">
        <div className="text-sm text-zinc-500">设备初始化 · 第 {step}/3 步</div>
        <h1 className="mt-1 text-3xl font-semibold">准备这台设备</h1>
      </div>
      <ActionFeedback actionKey={saveDeviceSpec.key} />
      <ActionFeedback actionKey={saveAssetsSpec.key} />
      {step === 1 && (
        <Card>
          <h2 className="mb-4 text-lg font-medium">私仓与个性化配置</h2>
          <div className="space-y-4">
            <Field label="Dec 私仓地址"><Input placeholder="https://..." value={repoURL} onChange={(e) => setRepoURL(e.target.value)} /></Field>
            <Field label="服务空闲时间"><Input value={idle} onChange={(e) => setIdle(e.target.value)} /></Field>
            <Field label="目标 IDE">
              <div className="flex flex-wrap gap-3">{props.settings.AvailableIDEs.map((ide) => <Check key={ide} label={ide} checked={ides.includes(ide)} onChange={() => setIDEs(toggle(ides, ide))} />)}</div>
            </Field>
            <ActionButton
              spec={saveDeviceSpec}
              action={saveDevice}
              runningLabel="验证并保存中…"
              onSuccess={(selection) => {
                setAssets(selection)
                setSelected(selection.Bundles.filter((item) => item.Enabled).map((item) => item.Name))
                setStep(2)
              }}
            >
              验证并继续
            </ActionButton>
          </div>
        </Card>
      )}
      {step === 2 && assets && (
        <Card>
          <h2 className="mb-1 text-lg font-medium">选择 Global 资产</h2>
          <p className="mb-4 text-sm text-zinc-500">可稍后在 Global 资产页修改。</p>
          <AssetChecklist data={assets} selected={selected} setSelected={setSelected} />
          <ActionButton className="mt-5" spec={saveAssetsSpec} action={saveAssets} runningLabel="保存中…" onSuccess={() => setStep(3)}>保存选择</ActionButton>
        </Card>
      )}
      {step === 3 && (
        <Card className="text-center">
          <CheckCircle2 className="mx-auto mb-3 h-10 w-10 text-emerald-400" />
          <h2 className="text-xl font-medium">设备初始化完成</h2>
          <p className="mt-2 text-sm text-zinc-400">可以先拉取 Global 资产，也可以直接进入设备控制台。</p>
          <div className="mt-5 flex justify-center gap-2"><Button onClick={props.onPull}>拉取 Global</Button><Button variant="secondary" onClick={props.onComplete}>进入控制台</Button></div>
        </Card>
      )}
    </div>
  )
}

function Overview(props: { deviceId: string; summary: DeviceSummary; settings: GlobalSettings; onRefresh: () => void; onNavigate: (view: View) => void }) {
  const refreshState = useDecAction(actionSpec(`device:refresh:${props.deviceId}`, '刷新设备状态', props.deviceId, [resource.global], 'read'))
  return (
    <>
      <PageTitle title="设备概览" description="当前设备上由 Dec 管理的资产与项目。" action={<Button size="sm" variant="outline" onClick={props.onRefresh} disabled={refreshState.blocked}><RefreshCw className="mr-2 h-4 w-4" />刷新</Button>} />
      <div className="grid gap-4 md:grid-cols-3">
        <Metric label="私仓" value={props.summary.RepoConnected ? '已连接' : '未连接'} detail={props.summary.RepoURL} />
        <Metric label="受管项目" value={String(props.summary.Projects.length)} detail={`${props.summary.Projects.filter((p) => p.Initialized).length} 个已初始化`} />
        <Metric label="Global IDE" value={props.settings.EffectiveIDEs.join(', ') || '—'} detail={`空闲退出 ${props.settings.ServerIdleTimeout}`} />
      </div>
      <Card className="mt-4">
        <div className="mb-3 flex items-center justify-between"><h2 className="font-medium">项目</h2><Button size="sm" onClick={() => props.onNavigate('projects')}>管理项目</Button></div>
        {props.summary.Projects.length === 0 ? <EmptyState text="尚未接管项目。选择服务器上的项目路径即可开始。" /> : (
          <div className="divide-y divide-zinc-800">{props.summary.Projects.slice(0, 5).map((project) => <ProjectRow key={project.Root} project={project} />)}</div>
        )}
      </Card>
    </>
  )
}

function AssetsPage(props: { deviceId: string; title: string; description: string; root: string; plane: 'local' | 'global'; onPull: () => void }) {
  const [data, setData] = useState<AssetSelection | null>(null)
  const [selected, setSelected] = useState<string[]>([])
  const actions = useActionRegistry()
  const runAction = actions.run
  const workspaceResource = resource.workspace(props.root)
  const loadSpec = useMemo(
    () => actionSpec(`assets:load:${props.deviceId}:${props.root || 'global'}`, '加载资产选择', props.deviceId, [workspaceResource], 'read'),
    [props.deviceId, props.root, workspaceResource],
  )
  const saveSpec = actionSpec(`assets:save:${props.deviceId}:${props.root || 'global'}`, '保存资产选择', props.deviceId, [workspaceResource], 'write', '资产选择已保存')
  const pullSpec = actionSpec(`operation:pull:${props.deviceId}:${props.root || 'global'}`, '拉取资产', props.deviceId, [workspaceResource], 'operation')
  const pullState = useDecAction(pullSpec)
  const saveState = useDecAction<{ RejectedBundles?: string[]; RejectedProjects?: string[] }>(saveSpec)
  const applySelection = useCallback((result: AssetSelection) => {
    setData(result)
    setSelected(result.Bundles.filter((item) => item.Enabled).map((item) => item.Name))
  }, [])

  // 远端工作区变化后需要重新同步服务端选择状态。
  // oxlint-disable-next-line react/set-state-in-effect
  useEffect(() => {
    void runAction(loadSpec, () => invokeTyped<AssetSelection>('load_asset_selection', props.root, props.plane, {}, loadSpec.key), { force: true })
      .then((outcome) => { if (outcome.ok) applySelection(outcome.value) })
  }, [applySelection, loadSpec, props.plane, props.root, runAction])
  const rejected = saveState.record?.result
    ? [...(saveState.record.result.RejectedProjects || []), ...(saveState.record.result.RejectedBundles || [])]
    : []

  return (
    <>
      <PageTitle title={props.title} description={props.description} action={<Button onClick={props.onPull} disabled={pullState.blocked}><RefreshCw className="mr-2 h-4 w-4" />拉取资产</Button>} />
      <ActionFeedback actionKey={loadSpec.key} />
      <ActionFeedback actionKey={saveSpec.key} />
      {rejected.length > 0 && <Notice text={`已保存；未接受：${rejected.join('、')}`} />}
      <Card>{data ? <><AssetChecklist data={data} selected={selected} setSelected={setSelected} /><ActionButton
        className="mt-5"
        spec={saveSpec}
        action={() => invokeTyped('save_enabled_bundles', props.root, props.plane, { EnabledProjects: selected }, saveSpec.key)}
        runningLabel="保存中…"
        onSuccess={async () => {
          const outcome = await runAction(loadSpec, () => invokeTyped<AssetSelection>('load_asset_selection', props.root, props.plane, {}, loadSpec.key), { force: true })
          if (outcome.ok) applySelection(outcome.value)
        }}
      >保存选择</ActionButton></> : <Loading />}</Card>
    </>
  )
}

function ProjectsPage(props: { deviceId: string; projects: ManagedProject[]; onRefresh: () => Promise<void>; onOpen: (project: ManagedProject) => void }) {
  const [picker, setPicker] = useState(false)
  const [browserPath, setBrowserPath] = useState('')
  const actions = useActionRegistry()
  const scanPrefix = `projects:scan:${props.deviceId}:`
  const latestScan = Object.values(actions.state.records)
    .filter((record) => record.key.startsWith(scanPrefix) && record.status === 'succeeded')
    .sort((a, b) => (b.finishedAt || 0) - (a.finishedAt || 0))[0]
  const scan = ((latestScan?.result as { Projects?: ManagedProject[] } | undefined)?.Projects || [])
    .filter((project) => !props.projects.some((item) => item.Root === project.Root))

  async function register(root: string) {
    const spec = actionSpec(`projects:register:${props.deviceId}:${root}`, `导入 ${root}`, props.deviceId, [resource.global], 'write', '项目已导入')
    const outcome = await actions.run(spec, () => invokeTyped<ManagedProject>('register_managed_project', '', 'global', { Root: root }, spec.key))
    if (outcome.ok) await props.onRefresh()
  }

  async function scanRoot(root: string) {
    const spec = actionSpec(`${scanPrefix}${root}`, `扫描 ${root}`, props.deviceId, [resource.global], 'operation', '项目扫描完成')
    await actions.run(spec, () => runOrWatchTyped<{ Projects: ManagedProject[] }>({
      actionKey: spec.key,
      operation: 'scan_managed_projects',
      projectRoot: '',
      workspacePlane: 'global',
      payload: { ScanRoot: root, MaxDepth: 6 },
    }))
  }

  const currentScanKey = browserPath ? `${scanPrefix}${browserPath}` : latestScan?.key
  return (
    <>
      <PageTitle title="项目" description="明确登记的目录为主；仅在你选择范围后扫描已有 Dec 项目。" action={<Button onClick={() => setPicker((value) => !value)}><FolderSearch className="mr-2 h-4 w-4" />{picker ? '收起路径选择器' : '选择路径'}</Button>} />
      {currentScanKey && <ActionFeedback actionKey={currentScanKey} />}
      {picker && <DirectoryBrowser deviceId={props.deviceId} initialPath={browserPath} onPathChange={setBrowserPath} onSelect={register} onScan={scanRoot} />}
      {scan.length > 0 && (
        <Card className="mb-4">
          <h2 className="mb-3 font-medium">扫描发现</h2>
          <div className="space-y-2">{scan.map((project) => {
            const spec = actionSpec(`projects:register:${props.deviceId}:${project.Root}`, `导入 ${project.Root}`, props.deviceId, [resource.global], 'write', '项目已导入')
            return <div key={project.Root} className="flex items-center gap-3 rounded-md border border-zinc-800 p-3"><Folder className="h-4 w-4" /><span className="flex-1 truncate text-sm">{project.Root}</span><ActionButton size="sm" spec={spec} action={() => invokeTyped<ManagedProject>('register_managed_project', '', 'global', { Root: project.Root }, spec.key)} runningLabel="导入中…" onSuccess={props.onRefresh}>导入</ActionButton></div>
          })}</div>
        </Card>
      )}
      <div className="space-y-3">
        {props.projects.map((project) => (
          <Card key={project.Root} className="flex items-center gap-4">
            <div className={`rounded-lg p-3 ${project.Error ? 'bg-red-950 text-red-300' : 'bg-zinc-800'}`}><Folder className="h-5 w-5" /></div>
            <div className="min-w-0 flex-1"><div className="font-medium">{project.Label || project.Name}</div><div className="truncate text-xs text-zinc-500">{project.Root}</div><div className="mt-1 text-xs">{project.Error || (project.Initialized ? '已初始化' : '等待初始化')}</div></div>
            <Button size="sm" onClick={() => props.onOpen(project)}>{project.Initialized ? '进入' : '初始化'}</Button>
          </Card>
        ))}
        {props.projects.length === 0 && <EmptyState text="设备上还没有受管项目。" />}
      </div>
    </>
  )
}

function DirectoryBrowser(props: { deviceId: string; initialPath: string; onPathChange: (path: string) => void; onSelect: (root: string) => void | Promise<void>; onScan: (root: string) => void | Promise<void> }) {
  const { initialPath: savedPath, onPathChange } = props
  const initialPath = useRef(savedPath)
  const [listing, setListing] = useState<DirectoryListing | null>(null)
  const [path, setPath] = useState('')
  const actions = useActionRegistry()
  const runAction = actions.run
  const browseSpec = useMemo(
    () => actionSpec(`directories:browse:${props.deviceId}`, '读取目标设备目录', props.deviceId, [resource.filesystem], 'read'),
    [props.deviceId],
  )
  const browseState = useDecAction<DirectoryListing>(browseSpec)
  const registerSpec = actionSpec(`projects:register:${props.deviceId}:${path}`, `导入 ${path}`, props.deviceId, [resource.global], 'write')
  const scanSpec = actionSpec(`projects:scan:${props.deviceId}:${path}`, `扫描 ${path}`, props.deviceId, [resource.global], 'operation')
  const mutationBlocked = Boolean(actions.blockedBy(registerSpec) || actions.blockedBy(scanSpec))
  const open = useCallback(async (target = '') => {
    const outcome = await runAction(browseSpec, () => invokeTyped<DirectoryListing>('browse_directories', '', 'global', { Path: target }, browseSpec.key), { force: true })
    if (outcome.ok) {
      const value = outcome.value
      setListing(value)
      setPath(value.Current)
      onPathChange(value.Current)
    }
  }, [browseSpec, onPathChange, runAction])
  // 首次打开从服务器 Home 开始；收起再打开时恢复用户上次浏览的位置。
  // oxlint-disable-next-line react/set-state-in-effect
  useEffect(() => { void open(initialPath.current) }, [open])
  return (
    <Card className="mb-4">
      <ActionFeedback actionKey={browseSpec.key} />
      <div className="mb-3 flex gap-2"><Input value={path} onChange={(e) => setPath(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') void open(path) }} /><Button variant="secondary" onClick={() => open(path)}>{browseState.running ? '打开中…' : '打开'}</Button></div>
      <div className="mb-3 flex flex-wrap gap-2">
        {listing?.Parent && <Button size="sm" variant="ghost" onClick={() => open(listing.Parent)}>上一级</Button>}
        {listing?.Roots.map((root) => <Button key={root} size="sm" variant="ghost" onClick={() => open(root)}>{root}</Button>)}
        {listing?.Home && <Button size="sm" variant="ghost" onClick={() => open(listing.Home)}>Home</Button>}
      </div>
      <div className="max-h-64 overflow-auto rounded-md border border-zinc-800">
        {listing?.Entries.map((entry) => <button key={entry.Path} onDoubleClick={() => open(entry.Path)} onClick={() => setPath(entry.Path)} className={`flex w-full items-center gap-2 border-b border-zinc-800 px-3 py-2 text-left text-sm hover:bg-zinc-800 ${path === entry.Path ? 'bg-zinc-800' : ''}`}><Folder className="h-4 w-4" />{entry.Name}</button>)}
      </div>
      <div className="mt-3 flex gap-2"><Button disabled={!path || mutationBlocked} onClick={() => props.onSelect(path)}>接管此目录</Button><Button variant="secondary" disabled={!path || mutationBlocked} onClick={() => props.onScan(path)}>扫描此范围</Button></div>
    </Card>
  )
}

type ProjectPreparation = { AvailableProjects: string[]; HomeProject: string }

// 家项目必须先存在于私仓：bind 与 push 都只接受已存在的项目名，
// 所以新机器上的新项目要能在这里就地新建，否则心流断在「绑定名不存在」。
function ProjectBinding(props: {
  deviceId: string
  project: ManagedProject
  onBound: (project: ManagedProject) => void | Promise<void>
  extraAction?: ReactNode
}) {
  const { deviceId, project } = props
  const [preparation, setPreparation] = useState<ProjectPreparation | null>(null)
  const [homeProject, setHomeProject] = useState('')
  const [newProject, setNewProject] = useState('')
  const workspaceResource = resource.workspace(project.Root)
  const prepareSpec = actionSpec(`project:prepare:${deviceId}:${project.Root}`, '检查项目配置', deviceId, [workspaceResource], 'read')
  const createSpec = actionSpec(`project:create-remote:${deviceId}:${project.Root}`, '在私仓新建项目', deviceId, [resource.global], 'write', '项目已创建并推送到私仓')
  const bindSpec = actionSpec(`project:bind:${deviceId}:${project.Root}`, project.Initialized ? '保存家项目绑定' : '初始化项目', deviceId, [workspaceResource, resource.global], 'write', project.Initialized ? '绑定已保存' : '项目初始化完成')

  const prepare = () => invokeTyped<ProjectPreparation>('prepare_project_config_init', project.Root, 'local', {}, prepareSpec.key)
  const applyPreparation = (value: ProjectPreparation, prefer = '') => {
    setPreparation(value)
    const available = value.AvailableProjects || []
    const wanted = [prefer, value.HomeProject].find((name) => name && available.includes(name))
    setHomeProject(wanted || available[0] || '')
    if (!newProject) setNewProject(suggestProjectName(project.Root))
  }
  const bind = async () => {
    if (homeProject) {
      await invokeTyped('bind_managed_project', project.Root, 'local', { ProjectName: homeProject }, bindSpec.key)
    }
    return invokeTyped<ManagedProject>('register_managed_project', '', 'global', { Root: project.Root, Label: project.Label }, bindSpec.key)
  }

  const available = preparation?.AvailableProjects || []
  const boundName = preparation?.HomeProject?.trim() || ''
  const boundMissing = Boolean(preparation && boundName && !available.includes(boundName))

  return (
    <Card className="mb-4">
      <h2 className="font-medium">家项目绑定</h2>
      <p className="mt-1 mb-4 text-sm text-zinc-400">
        {project.Initialized
          ? '家项目决定这个目录能装哪些资产。绑定名必须是私仓里已存在的项目。'
          : '将在该目录创建 `.dec/config.yaml` 和变量模板；现有项目文件不会被修改。'}
      </p>
      <ActionFeedback actionKey={prepareSpec.key} />
      <ActionFeedback actionKey={createSpec.key} />
      <ActionFeedback actionKey={bindSpec.key} />
      {!preparation ? (
        <div className="flex gap-2">
          <ActionButton spec={prepareSpec} action={prepare} runningLabel="检查中…" onSuccess={(value) => applyPreparation(value)}>
            {project.Initialized ? '检查绑定' : '检查并初始化'}
          </ActionButton>
          {props.extraAction}
        </div>
      ) : (
        <div className="space-y-4">
          {boundMissing && <Notice text={`当前绑定的 “${boundName}” 不在私仓里，所以拉不到任何资产。选一个已有项目，或用下面的输入框在私仓新建它。`} />}
          {available.length > 0 ? (
            <Field label="绑定为家项目">
              <select className={selectClass} value={homeProject} onChange={(e) => setHomeProject(e.target.value)}>
                {available.map((name) => <option key={name} value={name}>{name}</option>)}
              </select>
            </Field>
          ) : <Notice text="仓库中没有项目清单，将保留本地最小配置。" />}
          <Field label="或在私仓新建一个项目">
            <div className="flex gap-2">
              <Input placeholder="小写字母、数字、连字符，例如 agentshelpme" value={newProject} onChange={(e) => setNewProject(e.target.value)} />
              <ActionButton
                variant="secondary"
                spec={createSpec}
                disabled={!newProject.trim()}
                action={async () => {
                  const created = await invokeTyped<{ Name: string }>('create_remote_project', '', 'global', { Name: newProject.trim(), Title: project.Label || project.Name }, createSpec.key)
                  return { created, preparation: await prepare() }
                }}
                runningLabel="创建中…"
                onSuccess={({ created, preparation: refreshed }) => applyPreparation(refreshed, created.Name)}
              >
                新建并选中
              </ActionButton>
            </div>
          </Field>
          <div className="flex gap-2">
            <ActionButton spec={bindSpec} action={bind} runningLabel={project.Initialized ? '保存中…' : '初始化中…'} onSuccess={props.onBound}>
              {project.Initialized ? '保存绑定' : '确认初始化'}
            </ActionButton>
            {props.extraAction}
          </div>
        </div>
      )}
    </Card>
  )
}

// 私仓项目名是小写 kebab-case，目录名通常是驼峰或含下划线，这里给一个可直接用的建议值。
function suggestProjectName(root: string) {
  const base = root.split(/[\\/]/).filter(Boolean).at(-1) || ''
  return base.toLowerCase().replace(/[^a-z0-9]+/g, '')
}

function ProjectPage(props: { deviceId: string; project: ManagedProject; onPull: () => void; onRemoved: () => void; onChanged: () => void | Promise<void> }) {
  const [project, setProject] = useState(props.project)
  const workspaceResource = resource.workspace(project.Root)
  const removeSpec = actionSpec(`project:remove:${props.deviceId}:${project.Root}`, '移除项目管理', props.deviceId, [workspaceResource, resource.global], 'write', '已移除项目管理')
  const pullSpec = actionSpec(`operation:pull:${props.deviceId}:${project.Root}`, '拉取项目资产', props.deviceId, [workspaceResource], 'operation')
  const pullState = useDecAction(pullSpec)
  const removeButton = (
    <ActionButton variant="ghost" spec={removeSpec} action={() => invokeTyped('remove_managed_project', '', 'global', { Root: project.Root }, removeSpec.key)} runningLabel="移除中…" onSuccess={props.onRemoved}>移除管理</ActionButton>
  )
  const onBound = async (refreshed: ManagedProject) => {
    setProject(refreshed)
    await props.onChanged()
  }

  if (!project.Initialized) {
    return (
      <>
        <PageTitle title={`初始化 ${project.Name}`} description={project.Root} />
        <ProjectBinding deviceId={props.deviceId} project={project} onBound={onBound} extraAction={removeButton} />
      </>
    )
  }
  return (
    <>
      <PageTitle title={project.Label || project.Name} description={project.Root} action={<div className="flex gap-2"><Button onClick={props.onPull} disabled={pullState.blocked}><RefreshCw className="mr-2 h-4 w-4" />拉取</Button><ActionButton variant="outline" spec={removeSpec} action={() => invokeTyped('remove_managed_project', '', 'global', { Root: project.Root }, removeSpec.key)} runningLabel="移除中…" onSuccess={props.onRemoved}>移除管理</ActionButton></div>} />
      <ActionFeedback actionKey={removeSpec.key} />
      <ProjectBinding deviceId={props.deviceId} project={project} onBound={onBound} />
      <AssetsPage deviceId={props.deviceId} title="项目资产" description="家项目与直接 requires 决定本次安装内容。" root={project.Root} plane="local" onPull={props.onPull} />
    </>
  )
}

function DeviceSettings(props: { deviceId: string; settings: GlobalSettings; setSettings: (value: GlobalSettings) => void; onSaved: () => void; onRestart: () => void; version: string }) {
  const [repoURL, setRepoURL] = useState(props.settings.RepoURL)
  const [idle, setIdle] = useState(props.settings.ServerIdleTimeout)
  const [ides, setIDEs] = useState(props.settings.SelectedIDEs)
  const saveSpec = actionSpec(`settings:save:${props.deviceId}`, '保存设备设置', props.deviceId, [resource.global], 'write', '设备设置已保存')
  const restartSpec = actionSpec(`session:restart:${props.deviceId}`, '重启服务并重连', props.deviceId, [resource.session], 'session')
  const restartState = useDecAction(restartSpec)
  const save = async () => {
      const result = await invokeTyped<{ RepoAuthRequired?: boolean; RepoHost?: string; ConnectError?: string }>('save_global_settings', '', 'global', { RepoURL: repoURL, IDEs: ides, ServerIdleTimeout: idle }, saveSpec.key)
      if (result.RepoAuthRequired || result.ConnectError) {
        throw new Error(`私仓认证失败${result.RepoHost ? `（${result.RepoHost}）` : ''}：${result.ConnectError || '需要配置 Git 凭据'}`)
      }
      return invokeTyped<GlobalSettings>('load_global_settings', '', 'global', {}, saveSpec.key)
  }
  return (
    <>
      <PageTitle title="设备设置" description="配置当前 dec-server 的私仓与运行环境。" />
      <ActionFeedback actionKey={saveSpec.key} />
      <Card className="max-w-2xl space-y-4">
        <Field label="Dec 私仓"><Input value={repoURL} onChange={(e) => setRepoURL(e.target.value)} /></Field>
        <Field label="服务空闲时间"><Input value={idle} onChange={(e) => setIdle(e.target.value)} /></Field>
        <Field label="IDE"><div className="flex flex-wrap gap-3">{props.settings.AvailableIDEs.map((ide) => <Check key={ide} label={ide} checked={ides.includes(ide)} onChange={() => setIDEs(toggle(ides, ide))} />)}</div></Field>
        <ActionButton spec={saveSpec} action={save} runningLabel="保存中…" onSuccess={(next) => {
          props.setSettings(next)
          props.onSaved()
        }}>保存设置</ActionButton>
      </Card>
      <Card className="mt-4 max-w-2xl">
        <h2 className="font-medium">服务实例</h2>
        <p className="mt-1 text-xs text-zinc-500">
          当前版本 {props.version || '未知'}。设备上换过 dec-server 二进制后需要重启服务，正在运行的旧实例不会自动加载新能力。
        </p>
        <Button className="mt-3" variant="outline" onClick={props.onRestart} disabled={restartState.blocked}>重启服务并重连</Button>
      </Card>
    </>
  )
}

function SyncPage(props: { events: OperationEvent[]; history: { title: string; result: PullResult; at: Date }[] }) {
  return (
    <>
      <PageTitle title="同步记录" description="结构化结果保留结论，事件区只展示最近过程。" />
      <div className="space-y-4">
        {props.history.map((item, index) => <PullResultCard key={`${item.at.toISOString()}-${index}`} title={item.title} result={item.result} at={item.at} />)}
        {props.history.length === 0 && <EmptyState text="尚未执行同步。" />}
        {props.events.length > 0 && <Card><h2 className="mb-3 font-medium">最近事件</h2><div className="max-h-56 space-y-2 overflow-auto font-mono text-xs text-zinc-400">{props.events.slice(-15).map((event, index) => <div key={`${event.timeUnixMs}-${index}`}><span className={event.level === 'warn' ? 'text-amber-400' : ''}>{event.scope}</span> {event.message}{event.progress && ` (${event.progress.current}/${event.progress.total})`}</div>)}</div></Card>}
      </div>
    </>
  )
}

function PullResultCard(props: { title: string; result: PullResult; at: Date }) {
  const { headline, warnings, missing, skipped } = pullResultDiagnosis(props.result)
  return (
    <Card>
      <div className="flex items-start justify-between"><div><h2 className="font-medium">{props.title}</h2><div className="text-xs text-zinc-500">{props.at.toLocaleString()}</div></div>{props.result.FailedCount ? <TriangleAlert className="text-red-400" /> : <CheckCircle2 className="text-emerald-400" />}</div>
      <div className="mt-4 grid grid-cols-3 gap-3"><Metric label="已拉取" value={String(props.result.PulledCount || 0)} /><Metric label="Secrets" value={String((props.result.SecretsNoteCount || 0) + (props.result.SecretsSSHKeyCount || 0))} /><Metric label="失败" value={String(props.result.FailedCount || 0)} /></div>
      {headline && <Notice text={headline} />}
      {skipped && <Notice text={`已跳过：${skipped}`} />}
      {props.result.SecretsSkippedReason && <Notice text={`Secrets：${props.result.SecretsSkippedReason}`} />}
      {missing.length > 0 && <WarningList title="缺失项目/资产" items={missing} />}
      {warnings.length > 0 && <WarningList title="警告" items={warnings} />}
    </Card>
  )
}

function AssetChecklist(props: { data: AssetSelection; selected: string[]; setSelected: (items: string[]) => void }) {
  const [query, setQuery] = useState('')
  const filtered = props.data.Bundles.filter((item) => `${item.Name} ${item.Description}`.toLowerCase().includes(query.toLowerCase()))
  return (
    <div>
      <Input className="mb-3" placeholder="搜索项目或资产" value={query} onChange={(e) => setQuery(e.target.value)} />
      <div className="max-h-[32rem] space-y-2 overflow-auto">
        {filtered.map((item) => (
          <label key={`${item.Vault}-${item.Name}`} className={`block rounded-md border p-3 ${item.OtherPlane ? 'cursor-not-allowed border-zinc-800 opacity-50' : 'border-zinc-700 hover:bg-zinc-800/50'}`}>
            <div className="flex items-start gap-3">
              <input className="mt-1" type="checkbox" disabled={item.OtherPlane} checked={props.selected.includes(item.Name)} onChange={() => props.setSelected(toggle(props.selected, item.Name))} />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 font-medium"><Boxes className="h-4 w-4" />{item.Name}{item.Home && <Badge text="home" />}{item.Required && <Badge text="requires" />}</div>
                <div className="mt-1 text-xs text-zinc-500">{item.Description || `${item.Members?.length || 0} 个成员`}</div>
                {item.Members?.length > 0 && <div className="mt-2 flex flex-wrap gap-1">{item.Members.slice(0, 12).map((member) => <Badge key={`${member.Type}-${member.Name}`} text={`${member.Type}/${member.Name}`} />)}</div>}
              </div>
            </div>
          </label>
        ))}
        {filtered.length === 0 && <EmptyState text="没有匹配的资产。" />}
      </div>
    </div>
  )
}

function PageTitle(props: { title: string; description: string; action?: React.ReactNode }) {
  return <div className="mb-6 flex items-start justify-between gap-4"><div><h1 className="text-2xl font-semibold">{props.title}</h1><p className="mt-1 text-sm text-zinc-500">{props.description}</p></div>{props.action}</div>
}
function Metric(props: { label: string; value: string; detail?: string }) {
  return <div className="rounded-lg border border-zinc-800 bg-zinc-900 p-4"><div className="text-xs text-zinc-500">{props.label}</div><div className="mt-1 text-xl font-semibold">{props.value}</div>{props.detail && <div className="mt-1 truncate text-xs text-zinc-500">{props.detail}</div>}</div>
}
function Field(props: { label: string; children: React.ReactNode }) {
  return <div><Label className="mb-1.5 block">{props.label}</Label>{props.children}</div>
}
function Check(props: { label: string; checked: boolean; onChange: () => void }) {
  return <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={props.checked} onChange={props.onChange} />{props.label}</label>
}
function Badge({ text }: { text: string }) {
  return <span className="rounded bg-zinc-800 px-1.5 py-0.5 text-[10px] font-normal text-zinc-400">{text}</span>
}
function ProjectRow({ project }: { project: ManagedProject }) {
  return <div className="flex items-center gap-3 py-3"><Folder className="h-4 w-4 text-zinc-500" /><div className="min-w-0 flex-1"><div className="text-sm">{project.Label || project.Name}</div><div className="truncate text-xs text-zinc-600">{project.Root}</div></div><Badge text={project.Initialized ? '已初始化' : '待初始化'} /></div>
}
function EmptyState({ text }: { text: string }) {
  return <div className="rounded-md border border-dashed border-zinc-800 p-8 text-center text-sm text-zinc-500">{text}</div>
}
function Loading() {
  return <div className="flex items-center justify-center gap-2 p-10 text-sm text-zinc-500"><LoaderCircle className="h-4 w-4 animate-spin" />加载中</div>
}
function Notice({ text }: { text: string }) {
  return <div className="my-3 rounded-md border border-amber-900/60 bg-amber-950/30 px-3 py-2 text-sm text-amber-200">{text}</div>
}
function WarningList({ title, items }: { title: string; items: string[] }) {
  return <div className="mt-3"><div className="text-xs font-medium text-amber-400">{title}</div><ul className="mt-1 list-inside list-disc text-xs text-zinc-400">{items.map((item) => <li key={item}>{item}</li>)}</ul></div>
}
function toggle(items: string[], value: string) {
  return items.includes(value) ? items.filter((item) => item !== value) : [...items, value]
}
function connectionAddress(conn: SavedConnection) {
  if (conn.kind === 'local') return '本机 dec-server'
  if (conn.kind === 'ssh') return `${conn.ssh_user ? `${conn.ssh_user}@` : ''}${conn.ssh_host} → 127.0.0.1:${conn.port}`
  return `${conn.tls ? 'https' : 'http'}://${conn.host}:${conn.port}`
}
const selectClass = 'h-9 w-full rounded-md border border-zinc-700 bg-zinc-950 px-2 text-sm'
