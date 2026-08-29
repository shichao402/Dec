import type { ComponentProps } from 'react'
import { LoaderCircle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useDecAction } from '@/lib/action-context'
import type { ActionSpec } from '@/lib/action-registry'

type ActionButtonProps<T> = Omit<ComponentProps<typeof Button>, 'onClick'> & {
  spec: ActionSpec
  action: () => Promise<T>
  runningLabel?: string
  onSuccess?: (value: T) => void | Promise<void>
}

export function ActionButton<T>({
  spec,
  action,
  runningLabel,
  onSuccess,
  children,
  disabled,
  ...props
}: ActionButtonProps<T>) {
  const state = useDecAction<T>(spec)

  return (
    <Button
      {...props}
      disabled={disabled || state.blocked}
      onClick={() => {
        void state.run(action).then(async (outcome) => {
          if (outcome.ok) await onSuccess?.(outcome.value)
        })
      }}
    >
      {state.running && <LoaderCircle className="h-4 w-4 animate-spin" />}
      {state.running ? runningLabel || spec.label : children}
    </Button>
  )
}
