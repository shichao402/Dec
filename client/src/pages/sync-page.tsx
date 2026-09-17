import { useCallback, useEffect, useMemo, useState } from 'react'
import { CheckCircle2, History, RefreshCw, TriangleAlert } from 'lucide-react'
import { ActionFeedback } from '@/components/action-feedback'
import { ActionButton } from '@/components/ui/action-button'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { EmptyState, Notice, WarningList } from '@/components/ui/feedback'
import { Page, PageFill, PageHeader, ScrollArea } from '@/components/shell/page'
import { Panel, PanelBody, PanelHeader } from '@/components/ui/panel'
import { useActionRegistry } from '@/lib/action-context'
import { invokeTyped, runOrWatchTyped } from '@/lib/api'
import { actionSpec, resource } from '@/lib/console'
import { pullResultDiagnosis } from '@/lib/utils'
import type { ManagedProject, OperationEvent, PullResult } from '@/lib/utils'

export type PullHistoryEntry = { title: string; result: PullResult; at: Date }
export type SyncTarget = { key: string; label: string; root: string; plane: 'local' | 'global'; projectName?: string }

type OfficialRequire = {
  Project: string
  Want: string
  Installed?: string
  Available?: string
  UpdateAvailable?: boolean
  Error?: string
  OverrideActive?: boolean
  OverrideReadyToDrop?: boolean
}
type OfficialRequiresState = { Items?: OfficialRequire[] }
type UpdateRow = OfficialRequire & { key: string; target: SyncTarget }
type UpdateOutcome = { target: SyncTarget; result?: PullResult; error?: string }

const GLOBAL_TARGET_KEY = 'global'

export function SyncPage(props: {
  deviceId: string
  projects: ManagedProject[]
  events: OperationEvent[]
  history: PullHistoryEntry[]
  initialTarget?: SyncTarget | null
  onBack?: () => void
  onPullResult?: (title: string, result: PullResult) => void
}) {
  const targets = useMemo<SyncTarget[]>(() => [
    { key: GLOBAL_TARGET_KEY, label: 'Global（本机）', root: '', plane: 'global' },
    ...props.projects.filter((project) => project.Initialized).map((project) => ({
      key: project.Root,
      label: project.Label || project.Name,
      root: project.Root,
      plane: 'local' as const,
      projectName: project.Name,
    })),
  ], [props.projects])
  const [rows, setRows] = useState<UpdateRow[] | null>(null)
  const [selected, setSelected] = useState<string[]>([])
  const [preview, setPreview] = useState<UpdateRow[] | null>(null)
  const [outcomes, setOutcomes] = useState<UpdateOutcome[]>([])
  const actions = useActionRegistry()
  const runAction = actions.run
  const loadSpec = useMemo(
    () => actionSpec(
      `updates:list:${props.deviceId}`,
      '检查官方更新',
      props.deviceId,
      targets.map((target) => resource.workspace(target.root)),
      'read',
    ),
    [props.deviceId, targets],
  )

  const load = useCallback(async () => {
    const outcome = await runAction(loadSpec, async () => {
      const states = await Promise.all(targets.map(async (target) => ({
        target,
        state: await invokeTyped<OfficialRequiresState>(
          'list_official_requires',
          target.root,
          target.plane,
          {},
          `${loadSpec.key}:${target.key}`,
        ),
      })))
      return states.flatMap(({ target, state }) => (state.Items || []).map((item) => ({
        ...item,
        target,
        key: `${target.key}:${item.Project}`,
      })))
    }, { force: true })
    if (!outcome.ok) return
    setRows(outcome.value)
    const preferredTarget = props.initialTarget?.key
    setSelected(outcome.value
      .filter((row) => row.UpdateAvailable && (!preferredTarget || row.target.key === preferredTarget))
      .map((row) => row.key))
    setPreview(null)
  }, [loadSpec, props.initialTarget?.key, runAction, targets])

  // oxlint-disable-next-line react(set-state-in-effect)
  useEffect(() => { void load() }, [load])

  const picked = (rows || []).filter((row) => selected.includes(row.key))
  const updateSpec = actionSpec(
    `updates:apply:${props.deviceId}`,
    `更新 ${picked.length} 项官方依赖`,
    props.deviceId,
    [...new Map(picked.map((row) => [row.target.key, row.target])).values()]
      .map((target) => resource.workspace(target.root)),
    'operation',
    '官方依赖更新完成',
  )

  const toggle = (key: string) => {
    setSelected((current) => current.includes(key) ? current.filter((item) => item !== key) : [...current, key])
    setPreview(null)
    setOutcomes([])
  }
  const applyUpdates = async (): Promise<UpdateOutcome[]> => {
    const groups = new Map<string, { target: SyncTarget; projects: string[] }>()
    for (const row of picked) {
      const group = groups.get(row.target.key) || { target: row.target, projects: [] }
      group.projects.push(row.Project)
      groups.set(row.target.key, group)
    }
    const results: UpdateOutcome[] = []
    for (const [index, group] of [...groups.values()].entries()) {
      try {
        const result = await runOrWatchTyped<PullResult>({
          actionKey: `${updateSpec.key}:${index}`,
          operation: 'update_official',
          projectRoot: group.target.root,
          workspacePlane: group.target.plane,
          payload: { Projects: group.projects },
        })
        results.push({ target: group.target, result })
      } catch (error) {
        results.push({ target: group.target, error: error instanceof Error ? error.message : String(error) })
      }
    }
    return results
  }

  return (
    <Page>
      <PageHeader
        title="更新"
        description="选择官方依赖，预览后安装到对应工作区。"
        actions={props.onBack && <Button variant="outline" onClick={props.onBack}>返回</Button>}
      />
      <PageFill>
        <ScrollArea className="space-y-3 pr-0.5">
          <Panel>
            <PanelHeader
              title="官方依赖"
              description={rows ? `${rows.filter((row) => row.UpdateAvailable).length} 项可更新` : '正在检查'}
              action={<Button size="sm" variant="ghost" onClick={() => void load()}><RefreshCw className="size-3.5" />检查更新</Button>}
            />
            <div className="px-4 pt-3"><ActionFeedback actionKey={loadSpec.key} /></div>
            {rows === null ? null : rows.length === 0 ? (
              <EmptyState text="没有工作区声明官方依赖" />
            ) : (
              <PanelBody className="space-y-3">
                <div className="divide-y divide-line overflow-hidden rounded-lg border border-line">
                  {rows.map((row) => (
                    <label key={row.key} className="flex cursor-pointer items-start gap-3 px-3 py-2.5">
                      <Checkbox checked={selected.includes(row.key)} onChange={() => toggle(row.key)} />
                      <span className="min-w-0 flex-1">
                        <span className="flex flex-wrap items-center gap-1.5">
                          <span className="text-sm font-medium text-ink">{row.target.label}</span>
                          <span className="font-mono text-xs text-muted">{row.Project}</span>
                          <Badge tone="quiet">{row.Want}</Badge>
                          {row.UpdateAvailable && <Badge tone="warn">有更新</Badge>}
                          {row.OverrideActive && <Badge tone="warn">本地覆写</Badge>}
                          {!row.Error && !row.UpdateAvailable && row.Installed && <Badge tone="good">已最新</Badge>}
                        </span>
                        <span className="mt-1 block font-mono text-[11px] text-faint">
                          已装 {row.Installed || '—'} · 可用 {row.Available || '—'}
                        </span>
                        {row.Error && <span className="mt-1 block text-xs text-bad">{row.Error}</span>}
                      </span>
                    </label>
                  ))}
                </div>
                <div className="flex flex-wrap gap-2">
                  <Button
                    variant="outline"
                    disabled={picked.length === 0}
                    onClick={() => { setPreview(picked); setOutcomes([]) }}
                  >
                    预览更新
                  </Button>
                  {preview && (
                    <ActionButton
                      spec={updateSpec}
                      action={applyUpdates}
                      onSuccess={(value) => {
                        setOutcomes(value)
                        for (const item of value) {
                          if (item.result) props.onPullResult?.(`${item.target.label} 更新`, item.result)
                        }
                        void load()
                      }}
                      runningLabel="更新中…"
                    >
                      确认更新 {preview.length} 项
                    </ActionButton>
                  )}
                </div>
                <ActionFeedback actionKey={updateSpec.key} />
                {preview && (
                  <div className="rounded-lg border border-line bg-canvas/50 px-3 py-2.5 text-xs text-muted">
                    {preview.map((row) => (
                      <div key={row.key} className="py-0.5">
                        {row.target.label} · {row.Project}: {row.Installed || '未安装'} → {row.Available || '不可用'}
                        {row.OverrideReadyToDrop ? '（将移除已解决的本地覆写）' : ''}
                      </div>
                    ))}
                  </div>
                )}
                {outcomes.map((outcome) => (
                  outcome.error
                    ? <Notice key={outcome.target.key} tone="bad" text={`${outcome.target.label}：${outcome.error}`} />
                    : <Notice key={outcome.target.key} tone="info" text={`${outcome.target.label}：更新完成`} />
                ))}
              </PanelBody>
            )}
          </Panel>
          {props.history.length === 0 ? (
            <EmptyState icon={<History className="size-5" />} text="还没有更新记录" />
          ) : (
            <Panel>
              <PanelHeader title="更新记录" description={`最近 ${props.history.length} 条`} />
              <div className="divide-y divide-line">
                {props.history.map((entry, index) => (
                  <PullResultLog key={`${entry.at.toISOString()}-${index}`} entry={entry} />
                ))}
              </div>
            </Panel>
          )}
        </ScrollArea>
      </PageFill>
    </Page>
  )
}

function PullResultLog({ entry }: { entry: PullHistoryEntry }) {
  const { headline, warnings, missing, skipped } = pullResultDiagnosis(entry.result)
  const result = entry.result
  const failed = Boolean(result.FailedCount)
  const hasNotes = Boolean(headline || skipped) || missing.length > 0 || warnings.length > 0
  return (
    <div className="px-4 py-2.5">
      <div className="flex items-start gap-2">
        {failed
          ? <TriangleAlert className="mt-0.5 size-3.5 shrink-0 text-bad" />
          : <CheckCircle2 className="mt-0.5 size-3.5 shrink-0 text-good" />}
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="text-[13px] text-ink">{entry.title}</span>
            {failed ? <Badge tone="bad">有失败</Badge> : hasNotes ? <Badge tone="warn">需要处理</Badge> : <Badge tone="good">完成</Badge>}
            <span className="text-[11px] text-faint">{entry.at.toLocaleString()}</span>
          </div>
          <div className="tnum mt-0.5 text-[11px] text-faint">
            已拉取 {result.PulledCount || 0} · Secrets {(result.SecretsNoteCount || 0) + (result.SecretsSSHKeyCount || 0)} · 失败 {result.FailedCount || 0}
          </div>
          {hasNotes && (
            <div className="mt-2 space-y-1.5">
              {headline && <Notice text={headline} />}
              {skipped && <Notice text={`已跳过：${skipped}`} />}
              {missing.length > 0 && <WarningList title="缺失项目 / 资产" items={missing} />}
              {warnings.length > 0 && <WarningList title="警告" items={warnings} />}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
