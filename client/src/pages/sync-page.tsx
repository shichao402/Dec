import { useEffect, useState } from 'react'
import * as Dialog from '@radix-ui/react-dialog'
import { Activity, ArrowLeft, CheckCircle2, GitCompare, History, RefreshCw, TriangleAlert, UploadCloud } from 'lucide-react'
import { ActionFeedback } from '@/components/action-feedback'
import { Page, PageFill, PageHeader, ScrollArea } from '@/components/shell/page'
import { ActionButton } from '@/components/ui/action-button'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { EmptyState, Notice, WarningList } from '@/components/ui/feedback'
import { Field, Select } from '@/components/ui/input'
import { Panel, PanelBody, PanelHeader } from '@/components/ui/panel'
import { runOrWatchTyped } from '@/lib/api'
import { actionSpec, resource } from '@/lib/console'
import { SecretsPanel, type SecretsMetadata } from '@/pages/secrets-panel'
import { cn, pullResultDiagnosis } from '@/lib/utils'
import type { ManagedProject, OperationEvent, PullResult } from '@/lib/utils'

// 操作列固定在最后一列且不参与滚动：窄窗口宁可折行时间，也不能让「对比」滑出视口。
const previewGrid = 'grid grid-cols-[minmax(0,1fr)_auto_auto] items-center gap-x-3 px-3'
  + ' xl:grid-cols-[minmax(0,1fr)_7rem_8rem_8rem_8rem_auto]'

// 预览只需要「哪个更新」，完整 ISO 串会把三列时间撑到必须横向滚动。
function formatSyncTime(value?: string): string {
  if (!value) return '—'
  const at = new Date(value)
  if (Number.isNaN(at.getTime())) return value
  const pad = (input: number) => String(input).padStart(2, '0')
  return `${pad(at.getMonth() + 1)}-${pad(at.getDate())} ${pad(at.getHours())}:${pad(at.getMinutes())}`
}

export type PullHistoryEntry = { title: string; result: PullResult; at: Date }

export type SyncTarget = { key: string; label: string; root: string; plane: 'local' | 'global' }
type SyncMode = 'auto' | 'pull' | 'push'
type SyncPreviewItem = {
  Source: string
  Target: string
  Status: string
  LocalMtime?: string
  RemoteCommitTime?: string
  LastSyncTime?: string
  SourceModifiedAt?: string
  TargetModifiedAt?: string
  Secret?: boolean
  LocalContent?: string
  RemoteContent?: string
  Diff?: string
  MetadataOnly?: boolean
}
type SyncConflict = { Worktree?: string; Message?: string }
type SyncPreview = {
  Items?: SyncPreviewItem[]
  Entries?: SyncPreviewItem[]
  Conflict?: SyncConflict
  Worktree?: string
  HasConflicts?: boolean
  ConflictedPaths?: string[]
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
}) {
  const projects = props.projects.filter((project) => project.Initialized)
  const targets: PushTarget[] = [
    { key: GLOBAL_TARGET_KEY, label: 'Global（本机）', root: '', plane: 'global' },
    ...projects.map((item) => ({
      key: item.Root,
      label: item.Label || item.Name,
      root: item.Root,
      plane: 'local' as const,
    })),
  ]
  const [targetKey, setTargetKey] = useState(props.initialTarget?.key || GLOBAL_TARGET_KEY)
  const [secrets, setSecrets] = useState<SecretsMetadata | null>(null)
  const target = targets.find((item) => item.key === targetKey) || targets[0]

  useEffect(() => {
    if (props.initialTarget) setTargetKey(props.initialTarget.key)
  }, [props.initialTarget])

  return (
    <Page>
      <PageHeader
        title="同步"
        description="先预览 source 与 target 的差异，再选择自动同步、Pull 或 Push。"
        actions={props.onBack && (
          <Button variant="outline" onClick={props.onBack}>
            <ArrowLeft className="size-4" />返回
          </Button>
        )}
      />
      <PageFill>
        <ScrollArea className="space-y-3 pr-0.5">
          <SyncPreviewPanel
            deviceId={props.deviceId}
            targets={targets}
            target={target}
            onTarget={(value) => {
              setTargetKey(value)
              setSecrets(null)
            }}
          />
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

function SyncPreviewPanel(props: {
  deviceId: string
  targets: SyncTarget[]
  target: SyncTarget
  onTarget: (key: string) => void
}) {
  const [preview, setPreview] = useState<SyncPreview | null>(null)
  const [selected, setSelected] = useState<SyncPreviewItem | null>(null)
  const { target } = props
  const workspaceResource = resource.workspace(target.root)
  const previewSpec = actionSpec(`sync:preview:${props.deviceId}:${target.key}`, '预览同步差异', props.deviceId, [workspaceResource], 'read')
  const syncSpec = (mode: SyncMode) => actionSpec(
    `operation:sync:${props.deviceId}:${target.key}:${mode}`,
    `${mode === 'auto' ? '自动同步' : mode === 'pull' ? 'Pull' : 'Push'} ${target.label}`,
    props.deviceId,
    [workspaceResource],
    'operation',
    `${target.label} 同步完成`,
  )
  const runCompatible = async (actionKey: string, operation: string, legacyOperation: string, payload?: unknown) => {
    const input = {
      actionKey,
      operation,
      projectRoot: target.root,
      workspacePlane: target.plane,
      payload,
    }
    try {
      return normalizeSyncPreview(await runOrWatchTyped<SyncPreview>(input))
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      if (!message.includes('未知操作') && !message.includes('unknown operation')) throw error
      return normalizeSyncPreview(await runOrWatchTyped<SyncPreview>({ ...input, operation: legacyOperation }))
    }
  }
  const runPreview = () => runCompatible(previewSpec.key, 'preview_sync', 'preview_provides_sync')
  const conflict = preview?.Conflict
  // Worktree 是所有 provides 同步都会有的持久工作区，不等于发生冲突。
  // 只有后端明确返回 Conflict 时才展示 Continue / Abort。
  const conflictWorktree = conflict?.Worktree

  const runSync = (mode: SyncMode, conflictAction?: 'continue' | 'abort') =>
    runCompatible(syncSpec(mode).key, 'sync', 'sync_provides', { Mode: mode, ...(conflictAction ? { ConflictAction: conflictAction } : {}) })

  return (
    <Panel>
      <PanelHeader
        title="同步预览"
        description="source 是项目提供源，target 是私仓或本机落点；预览不会修改文件。"
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
                setSelected(null)
              }}
            >
              {props.targets.map((item) => <option key={item.key} value={item.key}>{item.label}</option>)}
            </Select>
          </Field>
          <ActionButton spec={previewSpec} variant="outline" action={runPreview} onSuccess={setPreview} runningLabel="预览中…">
            <GitCompare className="size-4" />刷新预览
          </ActionButton>
        </div>
        <p className="break-all font-mono text-[11px] text-faint">
          {target.root || 'Global 本机平面（projectRoot 为空）'}
        </p>
        <ActionFeedback actionKey={previewSpec.key} />
        {(['auto', 'pull', 'push'] as SyncMode[]).map((mode) => (
          <ActionFeedback key={mode} actionKey={syncSpec(mode).key} />
        ))}
        {!preview && <Notice tone="info" text="刷新预览后会显示方向、状态以及三种时间；执行同步前可逐项检查内容差异。" />}
        {preview && (
          <>
            <div className="min-h-56 rounded-lg border border-line">
              <div className={cn(previewGrid, 'border-b border-line bg-canvas/50 py-2 text-[11px] text-faint')}>
                <span>source → target</span>
                <span>状态</span>
                <span className="hidden xl:block">本地 mtime</span>
                <span className="hidden xl:block">远端 commit</span>
                <span className="hidden xl:block">last sync</span>
                <span />
              </div>
              {(preview.Items || []).map((item, index) => (
                <div key={`${item.Source}-${item.Target}-${index}`} className={cn(previewGrid, 'border-b border-line/70 py-2.5 text-xs last:border-b-0')}>
                  <div className="min-w-0">
                    <p className="truncate font-mono text-muted" title={item.Source}>{item.Source || '—'}</p>
                    <p className="truncate font-mono text-[11px] text-faint" title={item.Target}>→ {item.Target || '—'}</p>
                    {/* 窄窗口装不下三个时间列，但时间是判断同步方向的关键，
                        所以折行显示而不是靠横向滚动藏起来。 */}
                    <p className="mt-0.5 flex flex-wrap gap-x-3 text-[11px] text-faint xl:hidden">
                      <span>本地 {formatSyncTime(item.LocalMtime)}</span>
                      <span>远端 {formatSyncTime(item.RemoteCommitTime)}</span>
                      <span>上次 {formatSyncTime(item.LastSyncTime)}</span>
                    </p>
                  </div>
                  <Badge tone={item.Status === 'conflict' ? 'bad' : item.Status === 'synced' ? 'good' : 'warn'}>{item.Status || 'unknown'}</Badge>
                  <span className="hidden tnum text-[11px] text-faint xl:block">{formatSyncTime(item.LocalMtime)}</span>
                  <span className="hidden tnum text-[11px] text-faint xl:block">{formatSyncTime(item.RemoteCommitTime)}</span>
                  <span className="hidden tnum text-[11px] text-faint xl:block">{formatSyncTime(item.LastSyncTime)}</span>
                  <Button size="sm" variant="outline" onClick={() => setSelected(item)}>
                    {item.Secret || item.MetadataOnly ? '元数据' : '对比'}
                  </Button>
                </div>
              ))}
              {(preview.Items || []).length === 0 && (
                <p className="px-3 py-5 text-center text-xs text-faint">没有待同步或可比较的资产。</p>
              )}
            </div>
            {conflictWorktree && (
              <Notice
                tone="warn"
                text={`检测到冲突${conflict?.Message ? `：${conflict.Message}` : ''}。worktree：${conflictWorktree}`}
              />
            )}
            <div className="flex flex-wrap gap-2">
              {(['auto', 'pull', 'push'] as SyncMode[]).map((mode) => {
                const spec = syncSpec(mode)
                return (
                  <ActionButton
                    key={mode}
                    spec={spec}
                    variant={mode === 'auto' ? 'default' : 'outline'}
                    action={() => runSync(mode)}
                    onSuccess={setPreview}
                    runningLabel="同步中…"
                  >
                    {mode === 'auto' ? <RefreshCw className="size-4" /> : mode === 'push' ? <UploadCloud className="size-4" /> : <ArrowLeft className="size-4" />}
                    {mode === 'auto' ? '自动同步' : mode === 'pull' ? 'Pull' : 'Push'}
                  </ActionButton>
                )
              })}
              {conflictWorktree && (
                <>
                  <ActionButton spec={syncSpec('auto')} variant="secondary" action={() => runSync('auto', 'continue')} onSuccess={setPreview}>Continue</ActionButton>
                  <ActionButton spec={syncSpec('auto')} variant="destructive" action={() => runSync('auto', 'abort')} onSuccess={setPreview}>Abort</ActionButton>
                </>
              )}
            </div>
          </>
        )}
      </PanelBody>
      <DiffDialog item={selected} onClose={() => setSelected(null)} />
    </Panel>
  )
}

function normalizeSyncPreview(value: SyncPreview): SyncPreview {
  const items = value.Items || value.Entries || []
  const hasConflicts = value.HasConflicts || Boolean(value.ConflictedPaths?.length)
  return {
    ...value,
    Items: items.map((item) => ({
      ...item,
      LocalMtime: item.LocalMtime || item.SourceModifiedAt,
      RemoteCommitTime: item.RemoteCommitTime || item.TargetModifiedAt,
      Secret: item.Secret || item.MetadataOnly,
    })),
    Conflict: value.Conflict || (hasConflicts
      ? { Worktree: value.Worktree, Message: value.ConflictedPaths?.join('、') || '存在未解决冲突' }
      : undefined),
  }
}

function DiffDialog({ item, onClose }: { item: SyncPreviewItem | null; onClose: () => void }) {
  return (
    <Dialog.Root open={Boolean(item)} onOpenChange={(open) => { if (!open) onClose() }}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-black/65" />
        <Dialog.Content className="fixed inset-[3vh_2vw] z-50 flex flex-col overflow-hidden rounded-xl border border-line bg-panel shadow-2xl">
          <div className="flex items-start justify-between gap-3 border-b border-line px-4 py-3">
            <div>
              <Dialog.Title className="text-sm font-semibold text-ink">{item?.Secret ? 'Secret 元数据' : '内容对比'}</Dialog.Title>
              <Dialog.Description className="mt-1 text-xs text-faint">
                {item?.Secret ? 'Secret 正文永不传入或显示，只能选择同步方向。' : `${item?.Source || 'source'} → ${item?.Target || 'target'}`}
              </Dialog.Description>
            </div>
            <Dialog.Close asChild><Button size="sm" variant="ghost">关闭</Button></Dialog.Close>
          </div>
          <div className="min-h-0 flex-1 overflow-auto p-4">
            {item?.Secret ? (
              <div className="space-y-3">
                <Notice tone="warn" text="为避免泄露，此处仅显示状态和时间元数据，不展示本地或远端正文。" />
                <div className="grid gap-3 sm:grid-cols-2">
                  <SyncMetric label="本地" value={item.LocalMtime || '不存在'} />
                  <SyncMetric label="远端" value={item.RemoteCommitTime || '不存在'} />
                </div>
                <p className="text-xs text-faint">在预览页使用 Pull 或 Push 选择保留远端或本地版本。</p>
              </div>
            ) : item?.Diff ? (
              <pre className="overflow-auto whitespace-pre-wrap rounded-lg border border-line bg-canvas p-3 font-mono text-xs text-muted">{item.Diff}</pre>
            ) : (
              <div className="grid gap-3 lg:grid-cols-2">
                <DiffColumn title="source / 本地" content={item?.LocalContent} />
                <DiffColumn title="target / 远端" content={item?.RemoteContent} />
              </div>
            )}
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}

function DiffColumn({ title, content }: { title: string; content?: string }) {
  return (
    <div className="min-w-0">
      <div className="mb-1.5 text-xs font-medium text-muted">{title}</div>
      <pre className="h-[calc(94vh-9rem)] min-h-96 overflow-auto whitespace-pre-wrap rounded-lg border border-line bg-canvas p-3 font-mono text-xs text-muted">
        {content || '（无可显示内容）'}
      </pre>
    </div>
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
