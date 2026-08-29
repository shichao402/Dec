import { Check } from 'lucide-react'
import { cn } from '@/lib/utils'

export function Checkbox({
  checked,
  onChange,
  disabled,
  className,
  'aria-label': ariaLabel,
}: {
  checked: boolean
  onChange: () => void
  disabled?: boolean
  className?: string
  'aria-label'?: string
}) {
  return (
    <span className={cn('relative inline-flex size-4 shrink-0 items-center justify-center', className)}>
      <input
        type="checkbox"
        aria-label={ariaLabel}
        checked={checked}
        disabled={disabled}
        onChange={onChange}
        className="peer absolute inset-0 m-0 cursor-pointer appearance-none rounded-[5px] border border-line-hi bg-canvas transition-colors checked:border-accent checked:bg-accent disabled:cursor-not-allowed disabled:opacity-45"
      />
      {/* 绝对定位的 input 会盖住静态兄弟节点，勾必须自己也定位才画得出来。 */}
      <Check
        className="pointer-events-none relative z-10 size-3 text-white opacity-0 transition-opacity peer-checked:opacity-100"
        strokeWidth={3}
      />
    </span>
  )
}

export function CheckOption({
  label,
  checked,
  onChange,
  disabled,
}: {
  label: string
  checked: boolean
  onChange: () => void
  disabled?: boolean
}) {
  return (
    <label
      className={cn(
        'inline-flex items-center gap-2 rounded-lg border px-2.5 py-1.5 text-[13px] transition-colors',
        disabled ? 'cursor-not-allowed border-line opacity-45' : 'cursor-pointer',
        checked ? 'border-accent/45 bg-accent/10 text-ink' : 'border-line bg-panel text-muted hover:border-line-hi hover:text-ink',
      )}
    >
      <Checkbox checked={checked} onChange={onChange} disabled={disabled} />
      {label}
    </label>
  )
}
