import type { ReactNode } from 'react'
import { Inbox, LoaderCircle, TriangleAlert } from 'lucide-react'
import { cn } from '@/lib/utils'

export function Stat({
  label,
  value,
  detail,
  tone,
}: {
  label: string
  value: string
  detail?: ReactNode
  tone?: 'good' | 'warn' | 'bad'
}) {
  const valueColor = tone === 'good' ? 'text-good' : tone === 'warn' ? 'text-warn' : tone === 'bad' ? 'text-bad' : 'text-ink'
  return (
    <div className="min-w-0 rounded-xl border border-line bg-panel px-4 py-3">
      <div className="text-[11px] font-medium tracking-wide text-faint uppercase">{label}</div>
      <div className={cn('tnum mt-1 truncate text-lg font-semibold', valueColor)}>{value}</div>
      {detail && <div className="mt-0.5 truncate text-xs text-faint">{detail}</div>}
    </div>
  )
}

export function EmptyState({
  text,
  hint,
  icon,
  action,
  className,
}: {
  text: string
  hint?: string
  icon?: ReactNode
  action?: ReactNode
  className?: string
}) {
  return (
    <div
      className={cn(
        'flex min-h-40 flex-col items-center justify-center gap-2 rounded-xl border border-dashed border-line px-6 py-10 text-center',
        className,
      )}
    >
      <div className="text-faint">{icon || <Inbox className="size-5" />}</div>
      <div className="text-[13px] text-muted">{text}</div>
      {hint && <div className="max-w-md text-xs leading-relaxed text-faint">{hint}</div>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  )
}

export function Loading({ className, text = '加载中' }: { className?: string; text?: string }) {
  return (
    <div className={cn('flex items-center justify-center gap-2 py-12 text-[13px] text-faint', className)}>
      <LoaderCircle className="size-4 animate-spin" />
      {text}
    </div>
  )
}

export function Notice({
  text,
  tone = 'warn',
  className,
}: {
  text: ReactNode
  tone?: 'warn' | 'bad' | 'info'
  className?: string
}) {
  const style =
    tone === 'bad'
      ? 'border-bad/40 bg-bad/10 text-bad'
      : tone === 'info'
        ? 'border-accent/35 bg-accent/10 text-accent-hi'
        : 'border-warn/35 bg-warn/10 text-warn'
  return (
    <div className={cn('flex gap-2 rounded-lg border px-3 py-2 text-xs leading-relaxed', style, className)}>
      <TriangleAlert className="mt-0.5 size-3.5 shrink-0" />
      <div className="min-w-0">{text}</div>
    </div>
  )
}

export function WarningList({ title, items }: { title: string; items: string[] }) {
  return (
    <div className="rounded-lg border border-line bg-canvas/60 px-3 py-2.5">
      <div className="text-[11px] font-medium tracking-wide text-warn uppercase">{title}</div>
      <ul className="mt-1.5 space-y-1">
        {items.map((item) => (
          <li key={item} className="flex gap-1.5 text-xs text-muted">
            <span className="text-faint">·</span>
            <span className="min-w-0 break-words">{item}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}

export function KeyValue({ label, value, mono }: { label: string; value: ReactNode; mono?: boolean }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5 sm:flex-row sm:items-baseline sm:gap-3">
      <div className="w-32 shrink-0 text-xs text-faint">{label}</div>
      <div className={cn('min-w-0 flex-1 text-[13px] break-all text-muted', mono && 'font-mono text-xs')}>{value}</div>
    </div>
  )
}
