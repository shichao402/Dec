import { useEffect } from 'react'
import { CheckCircle2, LoaderCircle, TriangleAlert, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useActionRegistry } from '@/lib/action-context'
import type { ActionRecord } from '@/lib/action-registry'
import { isStaleServiceError } from '@/lib/utils'

export function ActionCenter({ onRestart }: { onRestart?: () => void }) {
  const registry = useActionRegistry()
  // 全局浮层只承担「跨页也要知道」的部分：失败、会话与长任务。
  // 读取成功没有信息量，保存成功由所在页面的结果区就地反馈，弹出来只会盖住正文。
  const visible = Object.values(registry.state.records)
    .filter((record) => {
      if (record.dismissed) return false
      if (record.status === 'failed') return true
      if (record.kind === 'read') return false
      return record.status === 'running' || record.kind === 'session' || record.kind === 'operation'
    })
    .sort((a, b) => (b.finishedAt || b.startedAt) - (a.finishedAt || a.startedAt))
  const running = visible.filter((record) => record.status === 'running')
  const feedback = visible.filter((record) => record.status !== 'running').slice(0, 2)

  return (
    <div className="pointer-events-none fixed right-6 bottom-6 z-50 flex w-[min(26rem,calc(100vw-3rem))] flex-col gap-2">
      {running.map((record) => (
        <ActionNotice key={record.key} record={record} />
      ))}
      {feedback.map((record) => (
        <ActionNotice
          key={record.key}
          record={record}
          onDismiss={() => registry.dismiss(record.key)}
          onRestart={onRestart}
        />
      ))}
    </div>
  )
}

export function ActionFeedback({
  actionKey,
  onRestart,
}: {
  actionKey: string
  onRestart?: () => void
}) {
  const registry = useActionRegistry()
  const record = registry.state.records[actionKey]
  if (!record || record.dismissed) return null
  if (record.kind === 'read' && record.status === 'succeeded') return null
  return (
    <div className="mb-3">
      <ActionNotice
        record={record}
        onDismiss={record.status === 'running' ? undefined : () => registry.dismiss(actionKey)}
        onRestart={onRestart}
      />
    </div>
  )
}

function ActionNotice({
  record,
  onDismiss,
  onRestart,
}: {
  record: ActionRecord
  onDismiss?: () => void
  onRestart?: () => void
}) {
  const succeeded = record.status === 'succeeded'
  const failed = record.status === 'failed'

  useEffect(() => {
    if (!succeeded || !onDismiss) return
    const timer = window.setTimeout(onDismiss, 4000)
    return () => window.clearTimeout(timer)
  }, [onDismiss, succeeded])

  const detail = record.status === 'running'
    ? record.events.at(-1)?.message || progressText(record) || '正在执行'
    : failed
      ? record.error
      : record.successMessage || '操作完成'

  return (
    <div
      className={`pointer-events-auto rounded-xl border bg-panel px-3 py-2.5 shadow-lg shadow-black/40 ${
        failed ? 'border-bad/45' : succeeded ? 'border-good/40' : 'border-accent/40'
      }`}
      role="status"
      aria-live="polite"
    >
      <div className="flex items-start gap-2.5">
        {record.status === 'running'
          ? <LoaderCircle className="mt-0.5 size-4 shrink-0 animate-spin text-accent-hi" />
          : failed
            ? <TriangleAlert className="mt-0.5 size-4 shrink-0 text-bad" />
            : <CheckCircle2 className="mt-0.5 size-4 shrink-0 text-good" />}
        <div className="min-w-0 flex-1">
          <div className="text-[13px] font-medium text-ink">{record.label}</div>
          {detail && <div className="mt-0.5 text-xs leading-relaxed break-words text-muted">{detail}</div>}
          {record.progress && record.progress.total > 0 && (
            <div className="mt-2 h-1 overflow-hidden rounded-full bg-line">
              <div
                className="h-full rounded-full bg-accent transition-[width]"
                style={{ width: `${Math.min(100, Math.round(record.progress.current / record.progress.total * 100))}%` }}
              />
            </div>
          )}
          {failed && onRestart && isStaleServiceError(record.error || '') && (
            <Button size="sm" variant="outline" className="mt-2" onClick={onRestart}>
              重启服务并重连
            </Button>
          )}
        </div>
        {onDismiss && (
          <button aria-label="关闭" className="text-faint transition-colors hover:text-ink" onClick={onDismiss}>
            <X className="size-4" />
          </button>
        )}
      </div>
    </div>
  )
}

function progressText(record: ActionRecord) {
  if (!record.progress) return ''
  const { phase, current, total } = record.progress
  return `${phase || '进度'} ${current}/${total}`
}
