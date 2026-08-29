import { useEffect } from 'react'
import { CheckCircle2, LoaderCircle, TriangleAlert, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useActionRegistry } from '@/lib/action-context'
import type { ActionRecord } from '@/lib/action-registry'
import { isStaleServiceError } from '@/lib/utils'

export function ActionCenter({ onRestart }: { onRestart?: () => void }) {
  const registry = useActionRegistry()
  const visible = Object.values(registry.state.records)
    .filter((record) => !record.dismissed)
    .sort((a, b) => (b.finishedAt || b.startedAt) - (a.finishedAt || a.startedAt))
  const running = visible.filter((record) => record.status === 'running')
  const feedback = visible.filter((record) => record.status !== 'running').slice(0, 2)

  return (
    <div className="pointer-events-none fixed right-4 top-4 z-50 flex w-[min(28rem,calc(100vw-2rem))] flex-col gap-2">
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
  return (
    <div className="mb-4">
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
      className={`pointer-events-auto rounded-md border px-3 py-2 text-sm shadow-xl ${
        failed
          ? 'border-red-900 bg-red-950 text-red-200'
          : succeeded
            ? 'border-emerald-900 bg-emerald-950 text-emerald-200'
            : 'border-sky-900 bg-sky-950 text-sky-200'
      }`}
      role="status"
      aria-live="polite"
    >
      <div className="flex items-start gap-2">
        {record.status === 'running'
          ? <LoaderCircle className="mt-0.5 h-4 w-4 shrink-0 animate-spin" />
          : failed
            ? <TriangleAlert className="mt-0.5 h-4 w-4 shrink-0" />
            : <CheckCircle2 className="mt-0.5 h-4 w-4 shrink-0" />}
        <div className="min-w-0 flex-1">
          <div className="font-medium">{record.label}</div>
          {detail && <div className="mt-0.5 break-words text-xs opacity-80">{detail}</div>}
          {record.progress && record.progress.total > 0 && (
            <div className="mt-2 h-1.5 overflow-hidden rounded bg-black/20">
              <div
                className="h-full rounded bg-current transition-[width]"
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
          <button aria-label="关闭" onClick={onDismiss}>
            <X className="h-4 w-4" />
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
