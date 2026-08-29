import type { ComponentProps, ReactNode } from 'react'
import { cn } from '@/lib/utils'

// 内容列宽度统一在这里：太窄会在宽屏留大片空白，太宽会让长列表难以扫读。
const container = 'mx-auto w-full max-w-[1440px]'

// 「可缩不可长」的块：内容短就贴合内容，内容长才吃满剩余高度并由内部滚动。
// 用 flex-1 强行拉满会得到「5 行内容 + 400px 空白」的面板，这是空旷感的主要来源。
export const fitBlock = 'flex min-h-0 max-h-full flex-col overflow-hidden'

export function Page({ className, ...props }: ComponentProps<'div'>) {
  return <div className={cn('flex h-full min-h-0 flex-col', className)} {...props} />
}

export function PageHeader({
  title,
  description,
  actions,
  meta,
}: {
  title: ReactNode
  description?: ReactNode
  actions?: ReactNode
  meta?: ReactNode
}) {
  return (
    <div className="shrink-0 px-8 pt-7 pb-5">
      <div className={cn(container, 'flex items-start justify-between gap-6')}>
        <div className="min-w-0">
          <h1 className="line-clamp-2 text-[22px] leading-7 font-semibold tracking-tight text-ink">{title}</h1>
          {description && <p className="mt-1.5 text-[13px] leading-relaxed text-faint">{description}</p>}
          {meta && <div className="mt-2 flex flex-wrap items-center gap-2">{meta}</div>}
        </div>
        {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
      </div>
    </div>
  )
}

// 表单 / 说明类页面：整页滚动。
// data-page / data-page-container 是布局测试的测量锚点，见 tests/layout/probe.ts。
export function PageScroll({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <div data-page="scroll" className="min-h-0 flex-1 overflow-y-auto px-8 pb-8">
      <div data-page-container className={cn(container, className)}>
        {children}
      </div>
    </div>
  )
}

// 列表类页面：页面本身不滚动，内部列表按需自己滚动。
// 注意子元素用「可缩不可长」：内容短时面板贴合内容高度，内容长时才吃满剩余高度并滚动。
// 强行 flex-1 会得到一个 5 行内容、400px 高的空面板，那正是「看着很空」的来源。
// overflow-y-auto 是兜底：空间够时子元素各自贴合／撑满，页面不滚动；
// 空间不够（窄窗口 + 展开的表单）时整页滚动，而不是把底部操作按钮裁掉。
export function PageFill({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <div data-page="fill" className="flex min-h-0 flex-1 flex-col overflow-y-auto px-8 pb-8">
      <div data-page-container className={cn(container, 'flex min-h-0 flex-1 flex-col', className)}>
        {children}
      </div>
    </div>
  )
}

// 主内容 + 右侧上下文栏。宽屏各列独立滚动；窄屏堆叠成一条整列滚动。
// 堆叠时内部列表必须交出滚动权（见 ScrollArea 的 splitOnly），否则同一条祖先链上
// 出现两个滚动容器，滚轮落在哪一层取决于指针位置。
export function SplitPane({
  children,
  rail,
  className,
}: {
  children: ReactNode
  rail: ReactNode
  className?: string
}) {
  return (
    <div
      className={cn(
        'flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto xl:grid xl:grid-cols-[minmax(0,1fr)_20rem] xl:overflow-hidden',
        className,
      )}
    >
      <div className="flex flex-col xl:min-h-0">{children}</div>
      <div className="flex flex-col gap-4 xl:min-h-0 xl:overflow-y-auto">{rail}</div>
    </div>
  )
}

export function Toolbar({ className, ...props }: ComponentProps<'div'>) {
  return <div className={cn('mb-3 flex shrink-0 flex-wrap items-center gap-2', className)} {...props} />
}

// splitOnly：只在 SplitPane 分栏（xl 以上）时自己滚动；堆叠时把滚动交给外层整列。
export function ScrollArea({
  className,
  splitOnly,
  ...props
}: ComponentProps<'div'> & { splitOnly?: boolean }) {
  return (
    <div
      data-scroll
      className={cn(
        'min-h-0 flex-1',
        splitOnly ? 'overflow-y-visible xl:overflow-y-auto' : 'overflow-y-auto',
        className,
      )}
      {...props}
    />
  )
}
