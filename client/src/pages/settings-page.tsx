import { useState } from 'react'
import { RotateCw } from 'lucide-react'
import { ActionFeedback } from '@/components/action-feedback'
import { Page, PageHeader, PageScroll } from '@/components/shell/page'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ActionButton } from '@/components/ui/action-button'
import { CheckOption } from '@/components/ui/checkbox'
import { KeyValue, Notice } from '@/components/ui/feedback'
import { Input } from '@/components/ui/input'
import { Panel, PanelBody, PanelFooter, PanelHeader, SettingsSection } from '@/components/ui/panel'
import { useDecAction } from '@/lib/action-context'
import { invokeTyped } from '@/lib/api'
import { actionSpec, resource, toggle } from '@/lib/console'
import type { DeviceSummary, GlobalSettings, PingInfo } from '@/lib/utils'

export function SettingsPage(props: {
  deviceId: string
  settings: GlobalSettings
  summary: DeviceSummary
  ping: PingInfo | null
  setSettings: (value: GlobalSettings) => void
  onSaved: () => void
  onRestart: () => void
}) {
  const [repoURL, setRepoURL] = useState(props.settings.RepoURL)
  const [idle, setIdle] = useState(props.settings.ServerIdleTimeout)
  const [ides, setIDEs] = useState(props.settings.SelectedIDEs)
  const saveSpec = actionSpec(`settings:save:${props.deviceId}`, '保存设备设置', props.deviceId, [resource.global], 'write', '设备设置已保存')
  const restartSpec = actionSpec(`session:restart:${props.deviceId}`, '重启服务并重连', props.deviceId, [resource.session], 'session')
  const restartState = useDecAction(restartSpec)
  const dirty =
    repoURL !== props.settings.RepoURL ||
    idle !== props.settings.ServerIdleTimeout ||
    ides.join(',') !== props.settings.SelectedIDEs.join(',')

  const save = async () => {
    const result = await invokeTyped<{ RepoAuthRequired?: boolean; RepoHost?: string; ConnectError?: string }>(
      'save_global_settings',
      '',
      'global',
      { RepoURL: repoURL, IDEs: ides, ServerIdleTimeout: idle },
      saveSpec.key,
    )
    if (result.RepoAuthRequired || result.ConnectError) {
      throw new Error(`私仓认证失败${result.RepoHost ? `（${result.RepoHost}）` : ''}：${result.ConnectError || '需要配置 Git 凭据'}`)
    }
    return invokeTyped<GlobalSettings>('load_global_settings', '', 'global', {}, saveSpec.key)
  }

  return (
    <Page>
      <PageHeader title="设备设置" description="这些设置属于目标设备上的 dec-server，不是本地客户端偏好。" />
      <PageScroll className="max-w-4xl space-y-4">
        <ActionFeedback actionKey={saveSpec.key} />
        <Panel>
          <PanelHeader title="私仓与运行环境" description="保存时会校验私仓是否可达。" />
          <div className="divide-y divide-line">
            <SettingsSection
              title="Dec 私仓"
              description="资产与项目清单的来源。保存前需要在这台设备上配置好 Git 凭据。"
            >
              <Input className="font-mono text-xs" value={repoURL} onChange={(e) => setRepoURL(e.target.value)} />
              {props.settings.RepoConnected ? (
                <div className="flex flex-wrap items-center gap-2 text-xs text-faint">
                  <Badge tone="good">已连接</Badge>
                  <span className="min-w-0 font-mono break-all">{props.settings.ConnectedRepoURL || props.settings.RepoURL}</span>
                </div>
              ) : (
                <Notice text="私仓尚未连接，资产列表会是空的。检查地址与 Git 凭据后重新保存。" />
              )}
            </SettingsSection>

            <SettingsSection
              title="服务空闲退出"
              description="dec-server 在无请求后自动退出的时长，例如 30m、2h。"
            >
              <Input className="max-w-40" value={idle} onChange={(e) => setIdle(e.target.value)} />
            </SettingsSection>

            <SettingsSection
              title="目标 IDE"
              description="决定 rules / skills / MCP 配置写到哪些 IDE 目录。"
            >
              <div className="flex flex-wrap gap-2">
                {props.settings.AvailableIDEs.map((ide) => (
                  <CheckOption key={ide} label={ide} checked={ides.includes(ide)} onChange={() => setIDEs(toggle(ides, ide))} />
                ))}
              </div>
              <p className="text-xs text-faint">
                当前生效：{props.settings.EffectiveIDEs.join(' · ') || '—'}
                {props.settings.ConfiguredEditor ? ` · 配置的编辑器 ${props.settings.ConfiguredEditor}` : ''}
              </p>
            </SettingsSection>
          </div>
          <PanelFooter>
            <ActionButton spec={saveSpec} action={save} runningLabel="保存中…" disabled={!dirty} onSuccess={(next) => {
              props.setSettings(next)
              setRepoURL(next.RepoURL)
              setIdle(next.ServerIdleTimeout)
              setIDEs(next.SelectedIDEs)
              props.onSaved()
            }}>
              保存设置
            </ActionButton>
            <span className="text-xs text-faint">{dirty ? '有未保存的改动' : '与设备上的配置一致'}</span>
          </PanelFooter>
        </Panel>

        <Panel>
          <PanelHeader title="服务实例" description="一机单例：换过 dec-server 二进制后，旧实例不会加载新能力。" />
          <PanelBody className="space-y-3">
            <div className="flex flex-wrap items-center gap-2 text-xs text-faint">
              <Badge tone="quiet" className="font-mono">{props.ping?.version || '版本未知'}</Badge>
              <span>重启会断开当前 session，需要重新用主密码解锁。</span>
            </div>
            <Button variant="outline" onClick={props.onRestart} disabled={restartState.blocked}>
              <RotateCw className="size-4" />
              重启服务并重连
            </Button>
          </PanelBody>
        </Panel>

        <Panel>
          <PanelHeader title="设备信息" />
          <PanelBody className="space-y-2.5">
            <KeyValue label="实例 ID" value={props.ping?.instance_id || '—'} mono />
            <KeyValue label="平台" value={props.summary.Platform || '—'} />
            <KeyValue label="Home 目录" value={props.summary.HomeDir || '—'} mono />
            <KeyValue label="受管项目" value={`${props.summary.Projects.length} 个`} />
          </PanelBody>
        </Panel>
      </PageScroll>
    </Page>
  )
}
