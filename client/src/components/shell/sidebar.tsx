import { Boxes, ChevronRight, Folder, Globe, LayoutDashboard, LogOut, RefreshCw, Settings } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { StatusDot } from '@/components/ui/badge'
import { connectionAddress, type View } from '@/lib/console'
import type { ManagedProject, PingInfo, SavedConnection } from '@/lib/utils'
import { cn } from '@/lib/utils'

type NavItem = { id: View; label: string; icon: LucideIcon; count?: number }

export function Sidebar(props: {
  saved: SavedConnection[]
  current: SavedConnection | null
  ping: PingInfo | null
  view: View
  projectCount: number
  project: ManagedProject | null
  onboarding: boolean
  onView: (view: View) => void
  onProject: (project: ManagedProject) => void
  onConnect: (conn: SavedConnection) => void
  onDisconnect: () => void
  busy: boolean
}) {
  const groups: { label: string; items: NavItem[] }[] = [
    {
      label: '工作区',
      items: [
        { id: 'overview', label: '概览', icon: LayoutDashboard },
        { id: 'global', label: 'Global 资产', icon: Globe },
        { id: 'projects', label: '项目', icon: Folder, count: props.projectCount },
      ],
    },
    { label: '运行', items: [{ id: 'sync', label: '同步记录', icon: RefreshCw }] },
    { label: '设备', items: [{ id: 'settings', label: '设备设置', icon: Settings }] },
  ]

  return (
    <aside className="flex w-[264px] shrink-0 flex-col border-r border-line bg-surface">
      <div className="flex h-14 shrink-0 items-center gap-2.5 border-b border-line px-4">
        <span className="grid size-7 place-items-center rounded-lg bg-accent/15 text-accent-hi">
          <Boxes className="size-4" />
        </span>
        <span className="min-w-0 truncate text-[13px] font-semibold tracking-tight">Dec Console</span>
      </div>

      <div className="shrink-0 border-b border-line p-2.5">
        <div className="mb-1.5 px-1.5 text-[11px] font-medium tracking-wide text-faint uppercase">设备</div>
        <div className="space-y-0.5">
          {props.saved.map((conn) => {
            const active = props.current?.id === conn.id
            return (
              <button
                key={conn.id}
                onClick={() => props.onConnect(conn)}
                disabled={props.busy}
                className={cn(
                  'flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left transition-colors disabled:opacity-50',
                  active ? 'bg-panel-hi text-ink' : 'text-muted hover:bg-panel-hi hover:text-ink',
                )}
              >
                <StatusDot tone={active ? 'good' : 'neutral'} />
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-[13px] font-medium">{conn.label}</span>
                  <span className="block truncate text-[11px] text-faint">{connectionAddress(conn)}</span>
                </span>
              </button>
            )
          })}
        </div>
      </div>

      <nav className="min-h-0 flex-1 space-y-4 overflow-y-auto p-2.5">
        {props.onboarding && (
          <p className="px-1.5 text-xs leading-relaxed text-faint">
            这台设备还没初始化，先完成右侧三步，控制台导航随后开放。
          </p>
        )}
        {!props.onboarding && groups.map((group) => (
          <div key={group.label}>
            <div className="mb-1 px-1.5 text-[11px] font-medium tracking-wide text-faint uppercase">{group.label}</div>
            <div className="space-y-0.5">
              {group.items.map((item) => (
                <div key={item.id}>
                  <NavButton
                    item={item}
                    active={props.view === item.id}
                    disabled={props.busy}
                    onClick={() => props.onView(item.id)}
                  />
                  {item.id === 'projects' && props.project && (
                    <button
                      onClick={() => props.onProject(props.project!)}
                      disabled={props.busy}
                      className={cn(
                        'mt-0.5 ml-3 flex w-[calc(100%-0.75rem)] items-center gap-1.5 rounded-lg border-l border-line pl-2.5 pr-2 py-1.5 text-left transition-colors disabled:opacity-50',
                        props.view === 'project' ? 'bg-panel-hi text-ink' : 'text-muted hover:bg-panel-hi hover:text-ink',
                      )}
                    >
                              <ChevronRight className="size-3.5 shrink-0 text-faint" />
                              <span
                                className="min-w-0 flex-1 truncate text-[13px]"
                                title={props.project.Label || props.project.Name}
                              >
                                {props.project.Label || props.project.Name}
                              </span>
                    </button>
                  )}
                </div>
              ))}
            </div>
          </div>
        ))}
      </nav>

      <div className="shrink-0 border-t border-line p-2.5">
        <div className="mb-2 flex items-center justify-between px-1.5 text-[11px] text-faint">
          <span>dec-server</span>
          <span className="font-mono">{props.ping?.version || '—'}</span>
        </div>
        <Button variant="outline" size="sm" className="w-full" onClick={props.onDisconnect} disabled={props.busy}>
          <LogOut className="size-3.5" /> 断开连接
        </Button>
      </div>
    </aside>
  )
}

function NavButton({
  item,
  active,
  disabled,
  onClick,
}: {
  item: NavItem
  active: boolean
  disabled: boolean
  onClick: () => void
}) {
  const Icon = item.icon
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className={cn(
        'flex h-9 w-full items-center gap-2.5 rounded-lg px-2 text-[13px] transition-colors disabled:opacity-50',
        active ? 'bg-panel-hi font-medium text-ink' : 'text-muted hover:bg-panel-hi hover:text-ink',
      )}
    >
      <Icon className={cn('size-4 shrink-0', active ? 'text-accent-hi' : 'text-faint')} />
      <span className="min-w-0 flex-1 truncate text-left">{item.label}</span>
      {item.count ? <span className="tnum text-[11px] text-faint">{item.count}</span> : null}
    </button>
  )
}
