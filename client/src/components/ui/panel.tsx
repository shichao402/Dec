import type { ComponentProps, ReactNode } from 'react'
import { cn } from '@/lib/utils'

export function Panel({ className, ...props }: ComponentProps<'div'>) {
  return <div className={cn('rounded-xl border border-line bg-panel', className)} {...props} />
}

export function PanelHeader({
  title,
  description,
  action,
  className,
}: {
  title: ReactNode
  description?: ReactNode
  action?: ReactNode
  className?: string
}) {
  return (
    <div className={cn('flex items-start justify-between gap-3 border-b border-line px-4 py-3', className)}>
      <div className="min-w-0">
        <div className="text-[13px] font-semibold text-ink">{title}</div>
        {description && <div className="mt-0.5 text-xs text-faint">{description}</div>}
      </div>
      {action}
    </div>
  )
}

export function PanelBody({ className, ...props }: ComponentProps<'div'>) {
  return <div className={cn('p-4', className)} {...props} />
}

export function PanelFooter({ className, ...props }: ComponentProps<'div'>) {
  return (
    <div
      className={cn('flex flex-wrap items-center gap-3 border-t border-line px-4 py-3', className)}
      {...props}
    />
  )
}

// 分区标题在左、字段在右：设置类页面用它就不会出现「窄卡片 + 大片空白」。
export function SettingsSection({
  title,
  description,
  children,
}: {
  title: string
  description?: ReactNode
  children: ReactNode
}) {
  return (
    <div className="grid gap-4 px-4 py-5 lg:grid-cols-[minmax(0,15rem)_minmax(0,1fr)] lg:gap-8">
      <div className="min-w-0">
        <div className="text-[13px] font-semibold text-ink">{title}</div>
        {description && <p className="mt-1 text-xs leading-relaxed text-faint">{description}</p>}
      </div>
      <div className="min-w-0 space-y-4">{children}</div>
    </div>
  )
}
