import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

type Tone = 'neutral' | 'accent' | 'good' | 'warn' | 'bad' | 'quiet'

const tones: Record<Tone, string> = {
  neutral: 'border-line-hi bg-panel-hi text-muted',
  accent: 'border-accent/40 bg-accent/12 text-accent-hi',
  good: 'border-good/35 bg-good/12 text-good',
  warn: 'border-warn/35 bg-warn/12 text-warn',
  bad: 'border-bad/35 bg-bad/12 text-bad',
  quiet: 'border-transparent bg-panel-hi text-faint',
}

export function Badge({
  children,
  tone = 'neutral',
  className,
  title,
}: {
  children: ReactNode
  tone?: Tone
  className?: string
  title?: string
}) {
  return (
    <span
      title={title}
      className={cn(
        'inline-flex max-w-full items-center gap-1 truncate rounded-md border px-1.5 py-0.5 text-[11px] leading-4 font-medium',
        tones[tone],
        className,
      )}
    >
      {children}
    </span>
  )
}

export function StatusDot({ tone = 'neutral', className }: { tone?: Tone; className?: string }) {
  const color =
    tone === 'good' ? 'bg-good' : tone === 'warn' ? 'bg-warn' : tone === 'bad' ? 'bg-bad' : tone === 'accent' ? 'bg-accent' : 'bg-faint'
  return <span className={cn('size-1.5 shrink-0 rounded-full', color, className)} />
}
