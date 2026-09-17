import { useEffect, useState } from 'react'
import { Activity, ArrowLeft, CheckCircle2, GitCompare, History, RefreshCw, TriangleAlert, UploadCloud } from 'lucide-react'
import { ActionFeedback } from '@/components/action-feedback'
import { Page, PageFill, PageHeader, ScrollArea } from '@/components/shell/page'
import { ActionButton } from '@/components/ui/action-button'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { EmptyState, Notice, WarningList } from '@/components/ui/feedback'
import { Field, Select } from '@/components/ui/input'
import { Panel, PanelBody, PanelHeader } from '@/components/ui/panel'
import { invokeTyped, runOrWatchTyped } from '@/lib/api'
import { useDecAction } from '@/lib/action-context'
import { actionSpec, resource } from '@/lib/console'
import { SecretsPanel, type SecretsMetadata } from '@/pages/secrets-panel'
import { cn, pullResultDiagnosis } from '@/lib/utils'
import type { ManagedProject, OperationEvent, PullResult } from '@/lib/utils'

// 路径列吃掉剩余宽度：窄窗口先隐藏象限，也不能让操作徽标被挤出视口。
const previewGrid = 'grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 px-3'
  + ' xl:grid-cols-[minmax(0,1fr)_6rem_10rem]'

export type PullHistoryEntry = { title: string; result: PullResult; at: Date }

export type SyncTarget = { key: string; label: string; root: string; plane: 'local' | 'global'; projectName?: string }
type PushChange = { Op: string; Path: string; Quadrant?: string }
// 预览只覆盖 Push 方向：官方资产是只读安装物，没有「本机比远端新」这回事。
type PushPreview = {
  EnabledBundleCount?: number
  SecretsTargetCount?: number
  ProjectSecretsName?: string
  DecCandidateCount?: number
  DecHasChanges?: boolean
  DecSkippedReason?: string
  BitwardenConfigured?: boolean
  Changes?: PushChange[]
}
type PushResult = {
  DecPushedCount?: number
  DecSkippedReason?: string
  VersionCommit?: string
  SecretsCreatedCount?: number
  SecretsUpdatedCount?: number
  SecretsSkippedReason?: string
}
type ProjectConsumersResult = {
  Provider: string
  Consumers: ManagedProject[]
}
type ConsumerPullOutcome = {
  project: ManagedProject
  result?: PullResult
  error?: string
}

// GLOBAL_TARGET_KEY 标识本机平面。本机平面的 projectRoot 必须为空，
// 否则 .dec/ 会退化成相对服务 cwd 的路径并覆盖 ~/.dec/config.yaml（ADR 0015）。
const GLOBAL_TARGET_KEY = 'global'

type PushTarget = SyncTarget

export function SyncPage(props: {
  deviceId: string
  projects: ManagedProject[]
  events: OperationEvent[]
  history: PullHistoryEntry[]
  initialTarget?: SyncTarget | null
  onBack?: () => void
  onPullResult?: (title: string, result: PullResult) => void
}) {
  const projects = props.projects.filter((project) => project.Initialized)
  const targets: PushTarget[] = [
    { key: GLOBAL_TARGET_KEY, label: 'Global（本机）', root: '', plane: 'global' },
    ...projects.map((item) => ({
      key: item.Root,
      label: item.Label || item.Name,
      root: item.Root,
      plane: 'local' as const,
      projectName: item.Name,
    })),
  ]
  const [targetKey, setTargetKey] = useState(props.initialTarget?.key || GLOBAL_TARGET_KEY)
  const [secrets, setSecrets] = useState<SecretsMetadata | null>(null)
  const [consumers, setConsumers] = useState<ProjectConsumersResult | null>(null)
  const target = targets.find((item) => item.key === targetKey) || targets[0]
  const consumerListSpec = actionSpec(
    `consumers:list:${props.deviceId}`,
    '查找引用方',
    props.deviceId,
    [resource.global],
    'read',
  )
  const consumerListAction = useDecAction<ProjectConsumersResult>(consumerListSpec)

  async function loadConsumers(provider: string) {
    setConsumers(null)
    const outcome = await consumerListAction.run(() => invokeTyped<ProjectConsumersResult>(
      'list_project_consumers',
      '',
      'global',
      { Provider: provider },
      consumerListSpec.key,
    ))
    if (outcome.ok) setConsumers(outcome.value)
  }

  useEffect(() => {
    if (props.initialTarget) setTargetKey(props.initialTarget.key)
  }, [props.initialTarget])

  return (
    <Page>
      <PageHeader
        title="同步"
        description="Pull 按 requires 安装官方资产并取回个人资产与密钥；Push 只回写个人私仓与 Bitwarden。"
        actions={props.onBack && (
          <Button variant="outline" onClick={props.onBack}>
            <ArrowLeft className="size-4" />返回
          </Button>
        )}
      />
      <PageFill>
        <ScrollArea className="space-y-3 pr-0.5">
          <Notice
            tone="info"
            text="官方资产从 Dec 仓 registry 分支按 requires 安装，本机改不动也推不回去：要改请用草稿 + 源仓 PR/Issue（MCP dec_propose_upstream）。TODO(console)：贡献入口尚未做成独立页。"
          />
          <SyncPanel
            deviceId={props.deviceId}
            targets={targets}
            target={target}
            onTarget={(value) => {
              setTargetKey(value)
              setSecrets(null)
              setConsumers(null)
            }}
            onPullResult={props.onPullResult}
            onPushed={(provider) => loadConsumers(provider)}
          />
          <ActionFeedback actionKey={consumerListSpec.key} />
          {consumers && (
            <ConsumerRefreshPanel
              key={consumers.Provider}
              deviceId={props.deviceId}
              data={consumers}
              onPullResult={props.onPullResult}
            />
          )}
          <div className="grid gap-3 xl:grid-cols-2">
            <SecretsPanel
              deviceId={props.deviceId}
              root={target.root}
              plane={target.plane}
              scopeKey={target.key}
              data={secrets}
              onLoaded={setSecrets}
            />
            <EventsPanel events={props.events} />
          </div>
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
      </PageFill>
    </Page>
  )
}

function SyncPanel(props: {
  deviceId: string
  targets: SyncTarget[]
  target: SyncTarget
  onTarget: (key: string) => void
  onPullResult?: (title: string, result: PullResult) => void
  onPushed: (provider: string) => Promise<void>
}) {
  const [preview, setPreview] = useState<PushPreview | null>(null)
  const [pushed, setPushed] = useState<PushResult | null>(null)
  const { target } = props
  const workspaceResource = resource.workspace(target.root)
  const previewSpec = actionSpec(`sync:preview:${props.deviceId}:${target.key}`, '预览待推送内容', props.deviceId, [workspaceResource], 'read')
  const pullSpec = actionSpec(
    `operation:pull:${props.deviceId}:${target.key}`,
    `Pull ${target.label}`,
    props.deviceId,
    [workspaceResource],
    'operation',
    `${target.label} 拉取完成`,
  )
  const pushSpec = actionSpec(
    `operation:push:${props.deviceId}:${target.key}`,
    `Push ${target.label}`,
    props.deviceId,
    [workspaceResource],
    'operation',
    `${target.label} 推送完成`,
  )
  const run = <T,>(actionKey: string, operation: string) => runOrWatchTyped<T>({
    actionKey,
    operation,
    projectRoot: target.root,
    workspacePlane: target.plane,
  })

  return (
    <Panel>
      <PanelHeader
        title="拉取与推送"
        description="Pull：registry 官方资产 + 个人私仓 + 密钥落地。Push：只回写个人私仓与 Bitwarden。"
      />
      <PanelBody className="space-y-3">
        <div className="flex flex-wrap items-end gap-2">
          <Field label="同步目标" className="min-w-64 flex-1">
            <Select
              aria-label="同步目标"
              value={target.key}
              onChange={(event) => {
                props.onTarget(event.target.value)
                setPreview(null)
                setPushed(null)
              }}
            >
              {props.targets.map((item) => <option key={item.key} value={item.key}>{item.label}</option>)}
            </Select>
          </Field>
          <ActionButton
            spec={pullSpec}
            action={() => run<PullResult>(pullSpec.key, 'pull')}
            onSuccess={(result) => props.onPullResult?.(`${target.label} Pull`, result)}
            runningLabel="拉取中…"
          >
            <ArrowLeft className="size-4" />Pull
          </ActionButton>
          <ActionButton
            spec={previewSpec}
            variant="outline"
            action={() => run<PushPreview>(previewSpec.key, 'preview_push')}
            onSuccess={setPreview}
            runningLabel="预览中…"
          >
            <GitCompare className="size-4" />刷新预览
          </ActionButton>
          <ActionButton
            spec={pushSpec}
            variant="outline"
            action={() => run<PushResult>(pushSpec.key, 'push')}
            onSuccess={async (result) => {
              setPushed(result)
              setPreview(null)
              if (target.plane === 'local' && target.projectName) {
                await props.onPushed(target.projectName)
              }
            }}
            runningLabel="推送中…"
          >
            <UploadCloud className="size-4" />Push
          </ActionButton>
        </div>
        <p className="break-all font-mono text-[11px] text-faint">
          {target.root || 'Global 本机平面（projectRoot 为空）'}
        </p>
        <ActionFeedback actionKey={pullSpec.key} />
        <ActionFeedback actionKey={previewSpec.key} />
        <ActionFeedback actionKey={pushSpec.key} />
        {!preview && !pushed && (
          <Notice tone="info" text="Push 前可以先刷新预览，确认这次会把哪些个人资产写回私仓；官方安装物不会出现在列表里。" />
        )}
        {preview && (
          <>
            <div className="grid gap-2 sm:grid-cols-3">
              <SyncMetric label="待推文件" value={preview.DecCandidateCount || 0} />
              <SyncMetric label="密钥 folder" value={preview.SecretsTargetCount || 0} />
              <SyncMetric label="Bitwarden" value={preview.BitwardenConfigured ? '已配置' : '未配置'} />
            </div>
            {preview.DecSkippedReason && <Notice tone="warn" text={`Dec Git 跳过：${preview.DecSkippedReason}`} />}
            <div className="min-h-32 rounded-lg border border-line">
              <div className={cn(previewGrid, 'border-b border-line bg-canvas/50 py-2 text-[11px] text-faint')}>
                <span>路径</span>
                <span>操作</span>
                <span className="hidden xl:block">象限</span>
              </div>
              {(preview.Changes || []).map((item, index) => (
                <div key={`${item.Path}-${index}`} className={cn(previewGrid, 'border-b border-line/70 py-2.5 text-xs last:border-b-0')}>
                  <p className="truncate font-mono text-muted" title={item.Path}>{item.Path}</p>
                  <Badge tone={item.Op === '删除' ? 'bad' : item.Op === '新建' ? 'good' : 'warn'}>{item.Op}</Badge>
                  <span className="hidden text-[11px] text-faint xl:block">{item.Quadrant || '—'}</span>
                </div>
              ))}
              {(preview.Changes || []).length === 0 && (
                <p className="px-3 py-5 text-center text-xs text-faint">没有待推送的个人改动。</p>
              )}
            </div>
          </>
        )}
        {pushed && (
          <>
            <div className="grid gap-2 sm:grid-cols-3">
              <SyncMetric label="已推文件" value={pushed.DecPushedCount || 0} />
              <SyncMetric label="新建密钥" value={pushed.SecretsCreatedCount || 0} />
              <SyncMetric label="更新密钥" value={pushed.SecretsUpdatedCount || 0} />
            </div>
            {pushed.DecSkippedReason && <Notice tone="warn" text={`Dec Git 跳过：${pushed.DecSkippedReason}`} />}
            {pushed.SecretsSkippedReason && <Notice tone="warn" text={`Secrets 跳过：${pushed.SecretsSkippedReason}`} />}
          </>
        )}
      </PanelBody>
    </Panel>
  )
}

function ConsumerRefreshPanel(props: {
  deviceId: string
  data: ProjectConsumersResult
  onPullResult?: (title: string, result: PullResult) => void
}) {
  const [selected, setSelected] = useState(() => props.data.Consumers.map((project) => project.Root))
  const [outcomes, setOutcomes] = useState<ConsumerPullOutcome[]>([])
  const picked = props.data.Consumers.filter((project) => selected.includes(project.Root))
  const batchSpec = actionSpec(
    `consumers:pull:${props.deviceId}:${props.data.Provider}`,
    `刷新 ${props.data.Provider} 的引用方`,
    props.deviceId,
    picked.map((project) => resource.workspace(project.Root)),
    'operation',
    '引用方刷新完成',
  )
  const runPulls = async (): Promise<ConsumerPullOutcome[]> => {
    const results: ConsumerPullOutcome[] = []
    for (const [index, project] of picked.entries()) {
      try {
        const result = await runOrWatchTyped<PullResult>({
          actionKey: `${batchSpec.key}:${index}`,
          operation: 'pull',
          projectRoot: project.Root,
          workspacePlane: 'local',
        })
        results.push({ project, result })
      } catch (error) {
        results.push({ project, error: error instanceof Error ? error.message : String(error) })
      }
    }
    return results
  }

  return (
    <Panel>
      <PanelHeader
        title="刷新引用方"
        description={`刚刚推送了 ${props.data.Provider}；以下受管项目直接 requires 它。`}
      />
      <PanelBody className="space-y-3">
        {props.data.Consumers.length === 0 ? (
          <Notice tone="info" text="这台设备登记的项目中没有直接引用方，无需继续刷新。" />
        ) : (
          <>
            <Notice tone="info" text="选中的工作区会依次 Pull。若仓库跟踪生成后的 IDE 配置，请在各仓库审阅变更后再提交。" />
            <div className="divide-y divide-line overflow-hidden rounded-lg border border-line">
              {props.data.Consumers.map((project) => {
                const outcome = outcomes.find((item) => item.project.Root === project.Root)
                return (
                  <label key={project.Root} className="flex cursor-pointer items-start gap-3 px-3 py-2.5">
                    <Checkbox
                      checked={selected.includes(project.Root)}
                      onChange={() => setSelected((items) => (
                        items.includes(project.Root)
                          ? items.filter((root) => root !== project.Root)
                          : [...items, project.Root]
                      ))}
                    />
                    <span className="min-w-0 flex-1">
                      <span className="block text-sm text-ink">{project.Label || project.Name}</span>
                      <span className="block truncate font-mono text-[11px] text-faint" title={project.Root}>{project.Root}</span>
                      {outcome?.error && <span className="mt-1 block text-xs text-bad">{outcome.error}</span>}
                      {outcome?.result && <span className="mt-1 block text-xs text-good">Pull 完成</span>}
                    </span>
                  </label>
                )
              })}
            </div>
            <ActionButton
              spec={batchSpec}
              action={runPulls}
              disabled={picked.length === 0}
              onSuccess={(results) => {
                setOutcomes(results)
                for (const item of results) {
                  if (item.result) props.onPullResult?.(item.project.Label || item.project.Name, item.result)
                }
              }}
              runningLabel="正在逐项刷新…"
            >
              <RefreshCw className="size-4" />刷新选中的 {picked.length} 个项目
            </ActionButton>
            <ActionFeedback actionKey={batchSpec.key} />
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
    <Panel>
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
