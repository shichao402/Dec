import type { ReactNode } from 'react'
import { useEffect, useMemo, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Card, Input, Label } from '@/components/ui/input'
import { authenticate, connectTarget, deleteConnection, disconnect, invokeMethod, listConnections, runOperation, saveConnection } from '@/lib/api'
import type { PingInfo, SavedConnection } from '@/lib/utils'
import {
  Cable,
  FolderTree,
  Globe,
  KeyRound,
  LayoutDashboard,
  RefreshCw,
  Server,
  Settings,
} from 'lucide-react'

type Screen = 'connect' | 'unlock' | 'console'
type Tab = 'overview' | 'workspaces' | 'global' | 'sync' | 'secrets' | 'settings'

const emptyConn = (): SavedConnection => ({
  id: '',
  label: '本机',
  kind: 'local',
  host: '127.0.0.1',
  port: 0,
  ssh_host: '',
  ssh_user: '',
})

export default function App() {
  const [screen, setScreen] = useState<Screen>('connect')
  const [tab, setTab] = useState<Tab>('overview')
  const [saved, setSaved] = useState<SavedConnection[]>([])
  const [draft, setDraft] = useState<SavedConnection>(emptyConn())
  const [ping, setPing] = useState<PingInfo | null>(null)
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [totp, setTotp] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [need2fa, setNeed2fa] = useState(false)
  const [projectRoot, setProjectRoot] = useState('')
  const [output, setOutput] = useState('')

  useEffect(() => {
    listConnections()
      .then(setSaved)
      .catch(() => setSaved([]))
  }, [])

  const title = useMemo(() => {
    if (screen === 'connect') return '连接 dec-server'
    if (screen === 'unlock') return '解锁实例'
    return '设备控制台'
  }, [screen])

  async function handleConnect(conn = draft) {
    setBusy(true)
    setError('')
    try {
      const info = await connectTarget({
        kind: conn.kind,
        host: conn.host,
        port: conn.port || 0,
        sshHost: conn.ssh_host,
        sshUser: conn.ssh_user,
      })
      setPing(info)
      if (info.unlocked) {
        setScreen('console')
      } else {
        setScreen('unlock')
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function handleUnlock() {
    setBusy(true)
    setError('')
    try {
      const result = await authenticate(email, password, totp, true)
      if (result.error) {
        setError(result.error)
        return
      }
      if (result.need_2fa) {
        setNeed2fa(true)
        return
      }
      if (result.unlocked) {
        setPassword('')
        setTotp('')
        setScreen('console')
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function handleDisconnect() {
    await disconnect().catch(() => undefined)
    setScreen('connect')
    setPing(null)
    setNeed2fa(false)
  }

  async function callInvoke(method: string, plane = 'project', payload: unknown = {}) {
    setBusy(true)
    setError('')
    try {
      const result = await invokeMethod(method, projectRoot, plane, payload)
      setOutput(result.error || result.result_json || '(empty)')
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  async function callRun(operation: string, plane = 'project', payload: unknown = {}) {
    setBusy(true)
    setError('')
    try {
      const result = await runOperation(operation, projectRoot, plane, payload)
      setOutput(result.error || result.result_json || '(empty)')
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex h-full min-h-screen bg-zinc-50 text-zinc-900 dark:bg-zinc-950 dark:text-zinc-50">
      {screen === 'console' && (
        <aside className="flex w-56 flex-col border-r border-zinc-200 bg-white p-3 dark:border-zinc-800 dark:bg-zinc-900">
          <div className="mb-4 px-2 text-sm font-semibold tracking-tight">Dec Console</div>
          {(
            [
              ['overview', '概览', LayoutDashboard],
              ['workspaces', '工作区', FolderTree],
              ['global', '本机平面', Globe],
              ['sync', '同步', RefreshCw],
              ['secrets', 'Secrets', KeyRound],
              ['settings', '设置', Settings],
            ] as const
          ).map(([id, label, Icon]) => (
            <button
              key={id}
              className={`mb-1 flex items-center gap-2 rounded-md px-2 py-2 text-left text-sm ${
                tab === id ? 'bg-zinc-900 text-white dark:bg-zinc-100 dark:text-zinc-900' : 'hover:bg-zinc-100 dark:hover:bg-zinc-800'
              }`}
              onClick={() => setTab(id)}
            >
              <Icon className="h-4 w-4" />
              {label}
            </button>
          ))}
          <div className="mt-auto space-y-2 px-1 text-xs text-zinc-500">
            <div>版本 {ping?.version ?? '—'}</div>
            <Button variant="outline" size="sm" className="w-full" onClick={handleDisconnect}>
              断开
            </Button>
          </div>
        </aside>
      )}
      <main className="flex min-w-0 flex-1 flex-col p-6">
        <header className="mb-6 flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-semibold">{title}</h1>
            <p className="text-sm text-zinc-500">本机与远程共用锁定 + 主密码鉴权</p>
          </div>
          {busy && <span className="text-sm text-zinc-500">处理中…</span>}
        </header>
        {error && (
          <div className="mb-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">{error}</div>
        )}

        {screen === 'connect' && (
          <div className="grid gap-6 lg:grid-cols-2">
            <Card>
              <h2 className="mb-4 flex items-center gap-2 text-lg font-medium">
                <Server className="h-4 w-4" /> 新连接
              </h2>
              <div className="space-y-3">
                <Label>名称</Label>
                <Input value={draft.label} onChange={(e) => setDraft({ ...draft, label: e.target.value })} />
                <Label>类型</Label>
                <select
                  className="h-9 w-full rounded-md border border-zinc-300 bg-white px-2 text-sm dark:border-zinc-700 dark:bg-zinc-950"
                  value={draft.kind}
                  onChange={(e) => setDraft({ ...draft, kind: e.target.value as SavedConnection['kind'] })}
                >
                  <option value="local">本机</option>
                  <option value="remote">远程 gRPC</option>
                  <option value="ssh">SSH 隧道</option>
                </select>
                {draft.kind !== 'local' && (
                  <>
                    <Label>Host</Label>
                    <Input value={draft.host} onChange={(e) => setDraft({ ...draft, host: e.target.value })} />
                    <Label>端口</Label>
                    <Input
                      type="number"
                      value={draft.port || ''}
                      onChange={(e) => setDraft({ ...draft, port: Number(e.target.value) })}
                    />
                  </>
                )}
                {draft.kind === 'ssh' && (
                  <>
                    <Label>SSH 主机</Label>
                    <Input value={draft.ssh_host} onChange={(e) => setDraft({ ...draft, ssh_host: e.target.value })} />
                    <Label>SSH 用户</Label>
                    <Input value={draft.ssh_user} onChange={(e) => setDraft({ ...draft, ssh_user: e.target.value })} />
                  </>
                )}
                <div className="flex gap-2 pt-2">
                  <Button onClick={() => handleConnect(draft)}>连接</Button>
                  <Button
                    variant="secondary"
                    onClick={async () => {
                      const savedConn = await saveConnection(draft)
                      setDraft(savedConn)
                      setSaved(await listConnections())
                    }}
                  >
                    保存
                  </Button>
                </div>
              </div>
            </Card>
            <Card>
              <h2 className="mb-4 flex items-center gap-2 text-lg font-medium">
                <Cable className="h-4 w-4" /> 已保存
              </h2>
              <div className="space-y-2">
                {saved.length === 0 && <p className="text-sm text-zinc-500">还没有保存的连接</p>}
                {saved.map((conn) => (
                  <div key={conn.id} className="flex items-center justify-between rounded-md border border-zinc-200 px-3 py-2 dark:border-zinc-800">
                    <div>
                      <div className="text-sm font-medium">{conn.label}</div>
                      <div className="text-xs text-zinc-500">{conn.kind}</div>
                    </div>
                    <div className="flex gap-2">
                      <Button size="sm" onClick={() => handleConnect(conn)}>
                        连接
                      </Button>
                      <Button
                        size="sm"
                        variant="ghost"
                        onClick={async () => {
                          await deleteConnection(conn.id)
                          setSaved(await listConnections())
                        }}
                      >
                        删除
                      </Button>
                    </div>
                  </div>
                ))}
              </div>
            </Card>
          </div>
        )}

        {screen === 'unlock' && (
          <Card className="max-w-md">
            <p className="mb-4 text-sm text-zinc-500">实例 {ping?.instance_id ?? ''} 已锁定。输入 Bitwarden 主密码解锁这台 dec-server。</p>
            <div className="space-y-3">
              <Label>邮箱</Label>
              <Input value={email} onChange={(e) => setEmail(e.target.value)} autoComplete="username" />
              <Label>主密码</Label>
              <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" />
              {need2fa && (
                <>
                  <Label>TOTP</Label>
                  <Input value={totp} onChange={(e) => setTotp(e.target.value)} />
                </>
              )}
              <Button onClick={handleUnlock}>解锁</Button>
            </div>
          </Card>
        )}

        {screen === 'console' && (
          <div className="space-y-4">
            <Card className="flex flex-wrap items-end gap-3">
              <div className="min-w-64 flex-1">
                <Label>工作区根路径</Label>
                <Input
                  className="mt-1"
                  placeholder="本仓库平面填写项目根；本机平面可留空"
                  value={projectRoot}
                  onChange={(e) => setProjectRoot(e.target.value)}
                />
              </div>
            </Card>
            {tab === 'overview' && (
              <Panel
                title="概览"
                actions={
                  <Button size="sm" onClick={() => callInvoke('load_project_overview', projectRoot ? 'project' : 'user', { IncludeVaultBundles: true })}>
                    刷新概览
                  </Button>
                }
              >
                查看仓库连接、绑定项目与最近状态。结构化结果如下。
              </Panel>
            )}
            {tab === 'workspaces' && (
              <Panel
                title="工作区"
                actions={
                  <>
                    <Button size="sm" onClick={() => callInvoke('load_asset_selection', 'project')}>
                      加载资产
                    </Button>
                    <Button size="sm" variant="secondary" onClick={() => callInvoke('prepare_project_config_init', 'project')}>
                      初始化准备
                    </Button>
                  </>
                }
              >
                绑定项目、四象限与 requires。保存启用列表请在设置里用 save_enabled_bundles。
              </Panel>
            )}
            {tab === 'global' && (
              <Panel
                title="本机平面"
                actions={
                  <Button size="sm" onClick={() => callInvoke('load_asset_selection', 'user')}>
                    加载本机项目
                  </Button>
                }
              >
                对应 dec --global：enabled_projects 与本机 secrets。
              </Panel>
            )}
            {tab === 'sync' && (
              <Panel
                title="同步"
                actions={
                  <>
                    <Button size="sm" onClick={() => callRun('pull', projectRoot ? 'project' : 'user')}>
                      Pull
                    </Button>
                    <Button size="sm" variant="secondary" onClick={() => callRun('preview_push', projectRoot ? 'project' : 'user')}>
                      Preview push
                    </Button>
                    <Button size="sm" variant="outline" onClick={() => callRun('push', projectRoot ? 'project' : 'user')}>
                      Push
                    </Button>
                  </>
                }
              >
                结果卡片展示 SkippedReason、缺失项与告警；过程事件见运行输出。
              </Panel>
            )}
            {tab === 'secrets' && (
              <Panel
                title="Secrets / Remote"
                actions={
                  <Button size="sm" onClick={() => callInvoke('list_delete_candidates', projectRoot ? 'project' : 'user', { IncludeRemote: true })}>
                    加载库存
                  </Button>
                }
              >
                远端库存与删除候选。列表不包含 secret 正文。
              </Panel>
            )}
            {tab === 'settings' && (
              <Panel
                title="设置"
                actions={
                  <Button size="sm" onClick={() => callInvoke('load_global_settings', 'user')}>
                    加载全局设置
                  </Button>
                }
              >
                Git、IDE、idle timeout。重启服务走 Shutdown。
              </Panel>
            )}
            <Card>
              <div className="mb-2 text-sm font-medium">结果</div>
              <pre className="max-h-80 overflow-auto rounded-md bg-zinc-950 p-3 text-xs text-zinc-100">{output || '尚无结果'}</pre>
            </Card>
          </div>
        )}
      </main>
    </div>
  )
}

function Panel({
  title,
  actions,
  children,
}: {
  title: string
  actions?: React.ReactNode
  children: ReactNode
}) {
  return (
    <Card>
      <div className="mb-3 flex items-center justify-between gap-3">
        <h2 className="text-lg font-medium">{title}</h2>
        <div className="flex flex-wrap gap-2">{actions}</div>
      </div>
      <p className="text-sm text-zinc-500">{children}</p>
    </Card>
  )
}
