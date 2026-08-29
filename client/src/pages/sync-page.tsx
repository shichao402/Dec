import { Activity, CheckCircle2, History, TriangleAlert } from 'lucide-react'
import { fitBlock, Page, PageFill, PageHeader, ScrollArea, SplitPane } from '@/components/shell/page'
import { Badge } from '@/components/ui/badge'
import { EmptyState, Notice, WarningList } from '@/components/ui/feedback'
import { Panel, PanelBody, PanelHeader } from '@/components/ui/panel'
import { pullResultDiagnosis } from '@/lib/utils'
import type { OperationEvent, PullResult } from '@/lib/utils'

export type PullHistoryEntry = { title: string; result: PullResult; at: Date }

export function SyncPage(props: { events: OperationEvent[]; history: PullHistoryEntry[] }) {
  return (
    <Page>
      <PageHeader
        title="同步记录"
        description="结论留在结果卡里，事件区只用来看进行中的过程。"
      />
      <PageFill>
        <SplitPane
          className="xl:grid-cols-[minmax(0,1fr)_24rem]"
          rail={<EventsPanel events={props.events} />}
        >
          <ScrollArea splitOnly className="space-y-3 pr-0.5">
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
