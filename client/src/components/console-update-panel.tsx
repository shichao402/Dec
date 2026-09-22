import { useEffect, useRef, useState } from 'react'
import { Download, RefreshCw } from 'lucide-react'
import { ActionFeedback } from '@/components/action-feedback'
import { Badge } from '@/components/ui/badge'
import { ActionButton } from '@/components/ui/action-button'
import { Notice } from '@/components/ui/feedback'
import { Select } from '@/components/ui/input'
import { Panel, PanelBody, PanelHeader } from '@/components/ui/panel'
import { actionSpec, resource } from '@/lib/console'
import type { ConsoleUpdateEnvelope } from '@/lib/console-update'
import { cn } from '@/lib/utils'

const checkUpdateSpec = actionSpec(
  'console:update:check',
  '检查 Console 更新',
  'console',
  [resource.consoleUpdate],
  'read',
)
const installUpdateSpec = actionSpec(
  'console:update:install',
  '下载并安装 Console 更新',
  'console',
  [resource.consoleUpdate],
  'session',
)

export function ConsoleUpdatePanel(props: {
  status: ConsoleUpdateEnvelope | null
  error: string
  onCheck: () => Promise<ConsoleUpdateEnvelope>
  onInstall: () => Promise<ConsoleUpdateEnvelope>
  onChannelChange: (channel: string) => Promise<ConsoleUpdateEnvelope>
  // 侧边栏跳进来时递增，卡片滚到可视区并闪一下边框。
  focusSignal?: number
}) {
  const kind = props.status?.result.kind
  const available = kind?.case === 'updateAvailable' ? kind.value : null
  const fallback = kind?.case === 'fallbackRequired' ? kind.value : null
  const failed = kind?.case === 'failed' ? kind.value : null
  const channels = props.status?.allowedChannels?.length
    ? props.status.allowedChannels
    : ['stable', 'dev']
  const channel = props.status?.channel || channels[0]
  const panelRef = useRef<HTMLDivElement>(null)
  const [focused, setFocused] = useState(false)
  const focusSignal = props.focusSignal

  useEffect(() => {
    if (!focusSignal) return
    panelRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
    setFocused(true)
    const timer = setTimeout(() => setFocused(false), 1600)
    return () => clearTimeout(timer)
  }, [focusSignal])

  return (
    <Panel ref={panelRef} className={cn(focused && 'ring-2 ring-accent/60')}>
      <PanelHeader
        title="Console 更新"
        description="属于本机程序壳，不经过当前连接的 dec-server，也不会更新远端设备。"
      />
      <PanelBody className="space-y-3">
        <ActionFeedback actionKey={checkUpdateSpec.key} />
        <ActionFeedback actionKey={installUpdateSpec.key} />
        <div className="flex flex-wrap items-center gap-2 text-xs text-faint">
          <Badge tone="quiet" className="font-mono">
            当前 {props.status?.currentVersion || '版本未知'}
          </Badge>
          {available ? (
            <Badge tone="warn">可更新至 {available.version}</Badge>
          ) : kind?.case === 'upToDate' ? (
            <Badge tone="good">已是最新版本</Badge>
          ) : kind?.case === 'throttled' ? (
            <Badge tone="quiet">检查已节流</Badge>
          ) : fallback ? (
            <Badge tone="warn">需要手动更新</Badge>
          ) : failed ? (
            <Badge tone="warn">检查失败</Badge>
          ) : (
            <Badge tone="quiet">尚未完成检查</Badge>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs text-faint">更新渠道</span>
          <Select
            className="max-w-40"
            value={channel}
            disabled={!props.status}
            onChange={(event) => {
              const next = event.target.value
              if (next === channel) return
              void props.onChannelChange(next)
            }}
          >
            {channels.map((value) => (
              <option key={value} value={value}>
                {value}
              </option>
            ))}
          </Select>
        </div>
        <p className="text-xs leading-relaxed text-faint">
          Console 启动时由更新引擎检查；成功后 24 小时内由引擎节流。只检查，不会自动安装。
          默认渠道是 stable；可改并保存，检查与安装都跟所选渠道。
        </p>
        {props.error && <Notice tone="warn" text={`自动检查失败：${props.error}`} />}
        {failed?.error?.message && <Notice tone="warn" text={failed.error.message} />}
        {fallback?.message && <Notice tone="warn" text={fallback.message} />}
        {available?.releaseNotesMarkdown && (
          <div className="max-h-40 overflow-auto whitespace-pre-wrap rounded-lg border border-line bg-canvas/60 px-3 py-2 text-xs leading-relaxed text-muted">
            {available.releaseNotesMarkdown}
          </div>
        )}
        <div className="flex flex-wrap gap-2">
          <ActionButton spec={checkUpdateSpec} variant="outline" action={props.onCheck}>
            <RefreshCw className="size-4" />
            检查更新
          </ActionButton>
          {available && (
            <ActionButton spec={installUpdateSpec} action={props.onInstall} runningLabel="下载更新中…">
              <Download className="size-4" />
              {props.status?.canAutoInstall ? '下载并安装' : '下载安装包'}
            </ActionButton>
          )}
        </div>
      </PanelBody>
    </Panel>
  )
}
