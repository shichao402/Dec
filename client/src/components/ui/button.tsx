import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import * as React from 'react'
import { cn } from '@/lib/utils'

const buttonVariants = cva(
  'inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-lg font-medium transition-[background-color,border-color,color] outline-none disabled:pointer-events-none disabled:opacity-45',
  {
    variants: {
      variant: {
        default: 'bg-ink text-canvas hover:bg-white',
        accent: 'bg-accent text-white hover:bg-accent-hi',
        secondary: 'bg-panel-hi text-ink hover:bg-line',
        outline: 'border border-line-hi bg-transparent text-ink hover:border-line-hi hover:bg-panel-hi',
        ghost: 'text-muted hover:bg-panel-hi hover:text-ink',
        destructive: 'bg-bad/15 text-bad hover:bg-bad/25',
      },
      size: {
        default: 'h-9 px-3.5 text-[13px]',
        sm: 'h-8 px-3 text-xs',
        lg: 'h-10 px-5 text-sm',
        icon: 'size-8',
      },
    },
    defaultVariants: { variant: 'default', size: 'default' },
  },
)

export function Button({
  className,
  variant,
  size,
  asChild = false,
  ...props
}: React.ComponentProps<'button'> & VariantProps<typeof buttonVariants> & { asChild?: boolean }) {
  const Comp = asChild ? Slot : 'button'
  return <Comp className={cn(buttonVariants({ variant, size }), className)} {...props} />
}
