import { Pencil, Plus, RefreshCw, Rocket, Server, Trash2 } from 'lucide-react'
import { Page, PageFill, PageHeader, ScrollArea } from '@/components/shell/page'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { CheckOption } from '@/components/ui/checkbox'
import { EmptyState, Notice, WarningList } from '@/components/ui/feedback'
import { Field, Input, Select } from '@/components/ui/input'
import { Panel, PanelBody, PanelFooter, PanelHeader } from '@/components/ui/panel'
import { connectionAddress, connectionKindLabel } from '@/lib/console'
import type { RemoteHostProbe, SavedConnection } from '@/lib/utils'

export function ConnectPage(props: {
  saved: SavedConnection[]
  draft: SavedConnection
  setDraft: (value: SavedConnection) => void
  onConnect: (conn: SavedConnection) => void
  onSaveConnect: () => void
  onDelete: (id: string) => void
  onResetDraft: () => void
  remoteProbe: RemoteHostProbe | null
  provisionConfirm: string
  setProvisionConfirm: (value: string) => void
  onProbeRemote: () => void
  onProvisionRemote: () => void
  busy: boolean
}) {
  const { draft, setDraft, remoteProbe } = props
  const editing = Boolean(draft.id)
  const sshTarget = draft.ssh_host.trim()
  const needsProvision = draft.kind === 'ssh' && Boolean(remoteProbe?.Reachable) && !remoteProbe?.ListenReady
  const firstProvision = needsProvision && !remoteProbe?.DecInstalled
  const canProvision = Boolean(
    needsProvision
    && remoteProbe?.Supported
    && remoteProbe.Blockers.length === 0
    && (!firstProvision || props.provisionConfirm.trim() === sshTarget),
  )

  return (
    <Page>
      <PageHeader
        title="选择设备"
        description="本机自动发现服务；远端只需填写 SSH 目标，Dec 会检测、置备并按需拉起服务。"
      />
      <PageFill className="max-w-6xl">
        <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto lg:grid lg:grid-cols-[minmax(0,1fr)_22rem] lg:overflow-hidden">
          <Panel className="flex min-h-[18rem] flex-col overflow-hidden lg:max-h-full lg:min-h-0">
            <PanelHeader
              title="已保存的设备"
              description={props.saved.length ? `${props.saved.length} 台` : '还没有保存的连接'}
            />
            {props.saved.length === 0 ? (
              <PanelBody>
                <EmptyState
                  className="border-none"
                  icon={<Server className="size-5" />}
                  text="还没有保存的设备"
                  hint="用右侧表单添加。本机会自动发现服务；远端填写 SSH Host 别名、主机名或 user@host 即可。"
                />
              </PanelBody>
            ) : (
              <ScrollArea className="divide-y divide-line">
                {props.saved.map((conn) => (
                  <div key={conn.id} className="group flex items-center gap-3 px-4 py-3">
                    <span className="grid size-9 shrink-0 place-items-center rounded-lg bg-panel-hi text-faint">
                      <Server className="size-4" />
                    </span>
                    <div className="min-w-0 flex-1">
                      <div className="flex min-w-0 items-center gap-2">
                        <span className="truncate text-[13px] font-medium text-ink">{conn.label}</span>
                        <Badge tone="quiet">{connectionKindLabel(conn.kind)}</Badge>
                        {conn.password_saved && <Badge tone="accent">已存密码</Badge>}
                      </div>
                      <div className="truncate font-mono text-[11px] text-faint">{connectionAddress(conn)}</div>
                    </div>
                    <div className="flex shrink-0 items-center gap-1">
                      <Button size="sm" onClick={() => props.onConnect(conn)} disabled={props.busy}>
                        {props.busy ? '连接中…' : '连接'}
                      </Button>
                      <Button size="icon" variant="ghost" aria-label="编辑" onClick={() => setDraft(conn)} disabled={props.busy}>
                        <Pencil className="size-4" />
                      </Button>
                      <Button size="icon" variant="ghost" aria-label="删除" onClick={() => props.onDelete(conn.id)} disabled={props.busy}>
                        <Trash2 className="size-4" />
                      </Button>
                    </div>
                  </div>
                ))}
              </ScrollArea>
            )}
          </Panel>

          {/* 堆叠时表单不能内部滚动：外层已经在滚，再套一层会把页脚按钮顶到面板之外。 */}
          <Panel className="flex flex-col lg:max-h-full lg:min-h-0 lg:overflow-hidden">
            <PanelHeader
              title={editing ? '编辑设备' : '添加设备'}
              description={editing ? draft.label : '检测通过后保存并连接'}
              action={editing
                ? <Button size="sm" variant="ghost" onClick={props.onResetDraft} disabled={props.busy}><Plus className="size-3.5" />新建</Button>
                : undefined}
            />
            <PanelBody className="min-h-0 flex-1 space-y-3 lg:overflow-y-auto">
              <Field label="名称">
                <Input value={draft.label} onChange={(e) => setDraft({ ...draft, label: e.target.value })} />
              </Field>
              <Field label="连接方式">
                <Select value={draft.kind} onChange={(e) => setDraft({ ...draft, kind: e.target.value as SavedConnection['kind'] })}>
                  <option value="local">本机</option>
                  <option value="ssh">SSH</option>
                  <option value="remote">TLS gRPC</option>
                </Select>
              </Field>

              {draft.kind === 'ssh' && (
                <>
                  <Field label="SSH 主机" hint="支持 ~/.ssh/config 的 Host 别名、主机名或 user@host；密钥与端口继续由 SSH 配置管理。">
                    <Input
                      className="font-mono text-xs"
                      placeholder="例如 build-box 或 dev@10.0.0.8"
                      value={draft.ssh_host}
                      onChange={(e) => setDraft({ ...draft, ssh_host: e.target.value, ssh_user: '', host: '127.0.0.1', port: 47653 })}
                    />
                  </Field>
                  {remoteProbe && <RemoteProbeStatus probe={remoteProbe} />}
                  {firstProvision && remoteProbe && remoteProbe.Blockers.length === 0 && (
                    <Field
                      label={`键入 ${sshTarget} 以确认首次部署`}
                      hint="首次部署会通过 SSH 向目标机注入并执行安装脚本。"
                    >
                      <Input
                        className="font-mono text-xs"
                        value={props.provisionConfirm}
                        onChange={(e) => props.setProvisionConfirm(e.target.value)}
                      />
                    </Field>
                  )}
                </>
              )}

              {draft.kind === 'remote' && (
                <>
                  <div className="grid grid-cols-[minmax(0,1fr)_6rem] gap-2">
                    <Field label="gRPC Host">
                      <Input className="font-mono text-xs" value={draft.host} onChange={(e) => setDraft({ ...draft, host: e.target.value })} />
                    </Field>
                    <Field label="端口">
                      <Input className="tnum" type="number" value={draft.port} onChange={(e) => setDraft({ ...draft, port: Number(e.target.value) })} />
                    </Field>
                  </div>
                  <CheckOption
                    label="启用 TLS（远程直连必须开启）"
                    checked={draft.tls}
                    onChange={() => setDraft({ ...draft, tls: !draft.tls })}
                  />
                  <Field label="证书服务器名（可选）">
                    <Input className="font-mono text-xs" value={draft.tls_server_name} onChange={(e) => setDraft({ ...draft, tls_server_name: e.target.value })} />
                  </Field>
                </>
              )}

              <Field label="Bitwarden 邮箱（可选）" hint="填了就不用每次解锁时再输一遍。">
                <Input autoComplete="username" value={draft.auth_email} onChange={(e) => setDraft({ ...draft, auth_email: e.target.value })} />
              </Field>
              {draft.password_saved && (
                <p className="text-xs leading-relaxed text-good">
                  这个连接的主密码已加密保存在系统凭据库，可在下次解锁时取消保存。
                </p>
              )}
            </PanelBody>
            <PanelFooter>
              {draft.kind === 'ssh' && !remoteProbe?.ListenReady ? (
                needsProvision && remoteProbe?.Blockers.length === 0 ? (
                  <Button className="w-full" onClick={props.onProvisionRemote} disabled={props.busy || !canProvision}>
                    <Rocket className="size-4" />{props.busy ? '正在部署…' : remoteProbe?.DecInstalled ? '完成配置并连接' : '一键部署并连接'}
                  </Button>
                ) : (
                  <Button className="w-full" onClick={props.onProbeRemote} disabled={props.busy || !sshTarget}>
                    <RefreshCw className="size-4" />{props.busy ? '正在检测…' : remoteProbe ? '重新检测' : '检测设备'}
                  </Button>
                )
              ) : (
                <Button className="w-full" onClick={props.onSaveConnect} disabled={props.busy}>
                  {props.busy ? '保存并连接中…' : '保存并连接'}
                </Button>
              )}
            </PanelFooter>
          </Panel>
        </div>
      </PageFill>
    </Page>
  )
}

function RemoteProbeStatus({ probe }: { probe: RemoteHostProbe }) {
  if (!probe.Reachable) {
    return <Notice tone="bad" text={probe.SSHError || 'SSH 无法连接，请先确认终端中可以免交互登录。'} />
  }
  if (probe.Blockers.length > 0) {
    return <WarningList title="暂时无法部署" items={probe.Blockers} />
  }
  if (probe.ListenReady) {
    return (
      <Notice
        tone="info"
        text={`已就绪：${probe.OS}/${probe.Arch}${probe.DecVersion ? ` · ${probe.DecVersion}` : ''}。连接时会自动拉起远端服务。`}
      />
    )
  }
  return (
    <div className="space-y-2">
      <Notice
        tone="warn"
        text={probe.DecInstalled
          ? 'Dec 已安装，但固定监听端口尚未配置。可以自动完成配置。'
          : '目标机满足条件，但尚未安装 Dec。首次部署会在远端执行安装脚本。'}
      />
      {probe.Warnings.length > 0 && <WarningList title="注意事项" items={probe.Warnings} />}
    </div>
  )
}
