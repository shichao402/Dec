import * as React from 'react'
import { ChevronDown } from 'lucide-react'
import { cn } from '@/lib/utils'

const control =
  'h-9 w-full rounded-lg border border-line-hi bg-canvas px-3 text-[13px] text-ink transition-colors placeholder:text-faint hover:border-line-hi focus:border-accent focus-visible:outline-none disabled:opacity-45'

export function Input({ className, ...props }: React.ComponentProps<'input'>) {
  return <input className={cn(control, className)} {...props} />
}

export function Select({ className, children, ...props }: React.ComponentProps<'select'>) {
  return (
    <div className="relative">
      <select className={cn(control, 'appearance-none pr-8', className)} {...props}>
        {children}
      </select>
      <ChevronDown className="pointer-events-none absolute right-2.5 top-1/2 size-4 -translate-y-1/2 text-faint" />
    </div>
  )
}

export function Label({ className, ...props }: React.ComponentProps<'label'>) {
  return <label className={cn('text-[13px] font-medium text-muted', className)} {...props} />
}

export function Field({
  label,
  hint,
  children,
  className,
}: {
  label: string
  hint?: string
  children: React.ReactNode
  className?: string
}) {
  return (
    <div className={cn('min-w-0', className)}>
      <Label className="mb-1.5 block">{label}</Label>
      {children}
      {hint && <p className="mt-1.5 text-xs text-faint">{hint}</p>}
    </div>
  )
}
