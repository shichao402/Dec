import type { ReactNode } from 'react'
import { ChevronRight, LoaderCircle } from 'lucide-react'

export function TopBar({
  crumbs,
  busyLabel,
  right,
}: {
  crumbs: string[]
  busyLabel?: string
  right?: ReactNode
}) {
  return (
    <header className="flex h-14 shrink-0 items-center gap-4 border-b border-line bg-canvas/85 px-8 backdrop-blur">
      <nav aria-label="位置" className="flex min-w-0 items-center gap-1">
        {crumbs.map((crumb, index) => (
          <span key={`${crumb}-${index}`} className="flex min-w-0 items-center gap-1">
            {index > 0 && <ChevronRight className="size-3.5 shrink-0 text-line-hi" />}
            <span
              className={
                index === crumbs.length - 1
                  ? 'truncate text-[13px] font-medium text-ink'
                  : 'truncate text-[13px] text-faint'
              }
            >
              {crumb}
            </span>
          </span>
        ))}
      </nav>
      <div className="ml-auto flex shrink-0 items-center gap-2">
        {busyLabel && (
          <span className="flex items-center gap-1.5 rounded-full border border-accent/30 bg-accent/10 px-2.5 py-1 text-xs text-accent-hi">
            <LoaderCircle className="size-3.5 animate-spin" />
            {busyLabel}
          </span>
        )}
        {right}
      </div>
    </header>
  )
}
