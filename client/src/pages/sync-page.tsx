import { useState } from 'react'
import { Activity, CheckCircle2, History, TriangleAlert, UploadCloud } from 'lucide-react'
import { ActionFeedback } from '@/components/action-feedback'
import { fitBlock, Page, PageFill, PageHeader, ScrollArea, SplitPane } from '@/components/shell/page'
import { ActionButton } from '@/components/ui/action-button'
import { Badge } from '@/components/ui/badge'
import { EmptyState, Notice, WarningList } from '@/components/ui/feedback'
import { Field, Select } from '@/components/ui/input'
import { Panel, PanelBody, PanelHeader } from '@/components/ui/panel'
import { invokeTyped, runOrWatchTyped } from '@/lib/api'
import { actionSpec, resource } from '@/lib/console'
import { pullResultDiagnosis } from '@/lib/utils'
import type { ManagedProject, OperationEvent, PullResult } from '@/lib/utils'

export type PullHistoryEntry = { title: string; result: PullResult; at: Date }
type PushChange = { Op: string; Path: string; Quadrant: string }
type PushPreview = {
  SecretsTargetCount: number
  DecCandidateCount: number
  DecHasChanges: boolean
  DecSkippedReason: string
  BitwardenConfigured: boolean
  HomeProject: string
  Changes: PushChange[]
}
type PushResult = {
  DecPushedCount: number
  DecSkippedReason: string
  VersionCommit: string
  SecretsCreatedCount: number
  SecretsUpdatedCount: number
  SecretsSkippedReason: string
}

export function SyncPage(props: {
  deviceId: string
  projects: ManagedProject[]
  events: OperationEvent[]
  history: PullHistoryEntry[]
}) {
  const projects = props.projects.filter((project) => project.Initialized)
  const [root, setRoot] = useState(projects[0]?.Root || '')
  const [preview, setPreview] = useState<PushPreview | null>(null)
  const [result, setResult] = useState<PushResult | null>(null)
  const selectedRoot = projects.some((item) => item.Root === root) ? root : projects[0]?.Root || ''
  const project = projects.find((item) => item.Root === selectedRoot)

  return (
    <Page>
      <PageHeader
        title="同步记录"
        description="拉取记录和项目推送都在这里；推送必须先预览影响范围。"
      />
      <PageFill>
        <SplitPane
          className="xl:grid-cols-[minmax(0,1fr)_24rem]"
          rail={<EventsPanel events={props.events} />}
        >
          <ScrollArea splitOnly className="space-y-3 pr-0.5">
            <PushPanel
              deviceId={props.deviceId}
              projects={projects}
              project={project}
              root={selectedRoot}
              preview={preview}
              result={result}
              onRoot={(value) => {
                setRoot(value)
                setPreview(null)
                setResult(null)
              }}
              onPreview={(value) => {
                setPreview(value)
                setResult(null)
              }}
              onPushed={(value) => {
                setResult(value)
                setPreview(null)
              }}
            />
            {props.history.map((item, index) => (
              <PullResultCard key={`${item.at.toISOString()}-${index}`} entry={item} />
            ))}
            {props.history.length === 0 && (
              <EmptyState
                icon={<History className="size-5" />}
                text="这次连接还没有同步记录"
                hint="在 Global 资产或项目页执行拉取后，落地数量、跳过原因和缺失项会显示在这里。"
              />
            )}
          </ScrollArea>
        </SplitPane>
      </PageFill>
    </Page>
  )
}

function PushPanel(props: {
  deviceId: string
  projects: ManagedProject[]
  project?: ManagedProject
  root: string
  preview: PushPreview | null
  result: PushResult | null
  onRoot: (root: string) => void
  onPreview: (value: PushPreview) => void
  onPushed: (value: PushResult) => void
}) {
  const workspaceResource = resource.workspace(props.root)
  const previewSpec = actionSpec(`sync:preview-push:${props.deviceId}:${props.root}`, '预览项目推送', props.deviceId, [workspaceResource], 'read')
  const pushSpec = actionSpec(`operation:push:${props.deviceId}:${props.root}`, '推送项目改动', props.deviceId, [workspaceResource], 'operation', '项目改动已推送')
  const canPush = Boolean(props.preview && (props.preview.DecHasChanges || props.preview.SecretsTargetCount > 0))

  return (
    <Panel>
      <PanelHeader
        title="推送项目改动"
        description="把选中项目的可写 Dec 资产推到 Git，并把本地 secrets 推到 Bitwarden。"
      />
      <PanelBody className="space-y-3">
        {props.projects.length === 0 ? (
          <Notice tone="info" text="当前设备没有已初始化的受管项目。请先在项目页完成初始化。" />
        ) : (
          <>
            <div className="flex flex-wrap items-end gap-2">
              <Field label="项目" className="min-w-64 flex-1">
                <Select value={props.root} onChange={(event) => props.onRoot(event.target.value)}>
                  {props.projects.map((item) => (
                    <option key={item.Root} value={item.Root}>{item.Label || item.Name}</option>
                  ))}
                </Select>
              </Field>
              <ActionButton
                variant="outline"
                spec={previewSpec}
                action={() => invokeTyped<PushPreview>('preview_push', props.root, 'local', {}, previewSpec.key)}
                runningLabel="预览中…"
                onSuccess={props.onPreview}
              >
                预览推送
              </ActionButton>
            </div>
            {props.project && <p className="break-all font-mono text-[11px] text-faint">{props.project.Root}</p>}
            <ActionFeedback actionKey={previewSpec.key} />
            <ActionFeedback actionKey={pushSpec.key} />
            {!props.preview && !props.result && (
              <p className="text-xs text-faint">不会显示或记录 token 明文；Bitwarden session 只保存在 dec-server 内存中。</p>
            )}
            {props.preview && (
              <>
                <div className="grid gap-2 sm:grid-cols-3">
                  <SyncMetric label="Dec 候选" value={props.preview.DecCandidateCount} />
                  <SyncMetric label="Secrets 目标" value={props.preview.SecretsTargetCount} />
                  <SyncMetric label="Bitwarden" value={props.preview.BitwardenConfigured ? '已配置' : '未配置'} />
                </div>
                {props.preview.HomeProject && <p className="text-xs text-faint">可写家项目：{props.preview.HomeProject}</p>}
                {props.preview.DecSkippedReason && <Notice tone="info" text={`Dec：${props.preview.DecSkippedReason}`} />}
                {props.preview.Changes?.length > 0 && (
                  <div className="max-h-40 overflow-auto rounded-lg border border-line bg-canvas/50">
                    {props.preview.Changes.map((change, index) => (
                      <div key={`${change.Path}-${index}`} className="flex gap-3 border-b border-line/70 px-3 py-2 text-xs last:border-b-0">
                        <span className="w-8 shrink-0 text-muted">{change.Op}</span>
                        <span className="min-w-0 flex-1 break-all font-mono text-faint">{change.Path}</span>
                        {change.Quadrant && <Badge tone="quiet">{change.Quadrant}</Badge>}
                      </div>
                    ))}
                  </div>
                )}
                <div className="flex flex-wrap items-center gap-2">
                  <ActionButton
                    spec={pushSpec}
                    disabled={!canPush}
                    action={() => runOrWatchTyped<PushResult>({
                      actionKey: pushSpec.key,
                      operation: 'push',
                      projectRoot: props.root,
                      workspacePlane: 'local',
                    })}
                    runningLabel="推送中…"
                    onSuccess={props.onPushed}
                  >
                    <UploadCloud className="size-4" />
                    确认推送
                  </ActionButton>
                  {!canPush && <span className="text-xs text-faint">当前没有可推送内容。</span>}
                </div>
              </>
            )}
            {props.result && (
              <>
                <div className="grid gap-2 sm:grid-cols-3">
                  <SyncMetric label="Dec 已推" value={props.result.DecPushedCount} />
                  <SyncMetric label="Secrets 新建" value={props.result.SecretsCreatedCount} />
                  <SyncMetric label="Secrets 更新" value={props.result.SecretsUpdatedCount} />
                </div>
                {props.result.VersionCommit && <p className="break-all font-mono text-xs text-faint">提交：{props.result.VersionCommit}</p>}
                {props.result.DecSkippedReason && <Notice tone="info" text={`Dec：${props.result.DecSkippedReason}`} />}
                {props.result.SecretsSkippedReason && <Notice tone="info" text={`Secrets：${props.result.SecretsSkippedReason}`} />}
              </>
            )}
          </>
        )}
      </PanelBody>
    </Panel>
  )
}

function SyncMetric({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="rounded-lg border border-line bg-canvas/60 px-3 py-2">
      <div className="text-[11px] text-faint">{label}</div>
      <div className="mt-0.5 text-sm font-semibold text-ink">{value}</div>
    </div>
  )
}

function PullResultCard({ entry }: { entry: PullHistoryEntry }) {
  const { headline, warnings, missing, skipped } = pullResultDiagnosis(entry.result)
  const result = entry.result
  const failed = Boolean(result.FailedCount)
  const secrets = (result.SecretsNoteCount || 0) + (result.SecretsSSHKeyCount || 0)
  const hasNotes = Boolean(headline || skipped || result.SecretsSkippedReason) || missing.length > 0 || warnings.length > 0

  return (
    <Panel>
      <PanelHeader
        title={
          <span className="flex items-center gap-2">
            {failed ? <TriangleAlert className="size-4 text-bad" /> : <CheckCircle2 className="size-4 text-good" />}
            {entry.title}
            {failed ? <Badge tone="bad">有失败</Badge> : hasNotes ? <Badge tone="warn">需要处理</Badge> : <Badge tone="good">完成</Badge>}
          </span>
        }
        description={entry.at.toLocaleString()}
      />
      <PanelBody className="space-y-3">
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
          <ResultMetric label="请求" value={result.RequestedCount || 0} />
          <ResultMetric label="已拉取" value={result.PulledCount || 0} tone={result.PulledCount ? 'good' : undefined} />
          <ResultMetric label="Secrets" value={secrets} />
          <ResultMetric label="失败" value={result.FailedCount || 0} tone={result.FailedCount ? 'bad' : undefined} />
        </div>
        {headline && <Notice text={headline} />}
        {skipped && <Notice text={`已跳过：${skipped}`} />}
        {result.SecretsSkippedReason && <Notice text={`Secrets：${result.SecretsSkippedReason}`} />}
        {missing.length > 0 && <WarningList title="缺失项目 / 资产" items={missing} />}
        {warnings.length > 0 && <WarningList title="警告" items={warnings} />}
        {result.EffectiveIDEs?.length > 0 && (
          <div className="flex flex-wrap items-center gap-1.5 text-[11px] text-faint">
            <span>目标 IDE</span>
            {result.EffectiveIDEs.map((ide) => <Badge key={ide} tone="quiet">{ide}</Badge>)}
          </div>
        )}
      </PanelBody>
    </Panel>
  )
}

function ResultMetric({ label, value, tone }: { label: string; value: number; tone?: 'good' | 'bad' }) {
  const color = tone === 'good' ? 'text-good' : tone === 'bad' ? 'text-bad' : 'text-ink'
  return (
    <div className="rounded-lg border border-line bg-canvas/60 px-3 py-2">
      <div className="text-[11px] text-faint">{label}</div>
      <div className={`tnum mt-0.5 text-lg leading-6 font-semibold ${color}`}>{value}</div>
    </div>
  )
}

function EventsPanel({ events }: { events: OperationEvent[] }) {
  const recent = events.slice(-60)
  return (
    <Panel className={fitBlock}>
      <PanelHeader
        title="事件区"
        description={recent.length ? `最近 ${recent.length} 条` : '当前没有进行中的任务'}
        action={<Activity className={recent.length ? 'size-4 text-accent-hi' : 'size-4 text-faint'} />}
      />
      {recent.length === 0 ? (
        <PanelBody>
          <p className="text-xs leading-relaxed text-faint">
            拉取或扫描运行时，过程日志会滚动显示在这里。切换页面不会打断任务，结论始终回到左边的结果卡。
          </p>
        </PanelBody>
      ) : (
        <ScrollArea className="max-h-[24rem] space-y-1 px-3 py-2.5 font-mono text-[11px] leading-relaxed xl:max-h-none">
          {recent.map((event, index) => (
            <div key={`${event.timeUnixMs}-${index}`} className="flex gap-2">
              <span className={event.level === 'warn' ? 'shrink-0 text-warn' : 'shrink-0 text-faint'}>{event.scope || '·'}</span>
              <span className={event.level === 'warn' ? 'min-w-0 break-words text-warn' : 'min-w-0 break-words text-muted'}>
                {event.message}
                {event.progress && event.progress.total > 0 && ` (${event.progress.current}/${event.progress.total})`}
              </span>
            </div>
          ))}
        </ScrollArea>
      )}
    </Panel>
  )
}
