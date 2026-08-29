import type { LucideIcon } from 'lucide-react'
import {
  ChevronRight,
  CircleAlert,
  Folder,
  FolderSearch,
  Globe,
  RefreshCw,
  Settings,
} from 'lucide-react'
import { fitBlock, Page, PageFill, PageHeader, ScrollArea, SplitPane } from '@/components/shell/page'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { EmptyState, Stat } from '@/components/ui/feedback'
import { Panel, PanelBody, PanelHeader } from '@/components/ui/panel'
import { useDecAction } from '@/lib/action-context'
import { actionSpec, resource, type View } from '@/lib/console'
import type { DeviceSummary, GlobalSettings, ManagedProject, PullResult } from '@/lib/utils'
import { pullResultDiagnosis } from '@/lib/utils'

export function OverviewPage(props: {
  deviceId: string
  summary: DeviceSummary
  settings: GlobalSettings
  lastPull?: { title: string; result: PullResult; at: Date }
  onRefresh: () => void
  onNavigate: (view: View) => void
  onOpenProject: (project: ManagedProject) => void
  onPullGlobal: () => void
}) {
  const refreshState = useDecAction(
    actionSpec(`device:refresh:${props.deviceId}`, '刷新设备状态', props.deviceId, [resource.global], 'read'),
  )
  const projects = props.summary.Projects
  const initialized = projects.filter((project) => project.Initialized).length
  const broken = projects.filter((project) => project.Error).length

  return (
    <Page>
      <PageHeader
        title="设备概览"
        description="这台设备由 Dec 管理的私仓连接、项目与最近一次同步结果。"
        actions={
          <Button variant="outline" onClick={props.onRefresh} disabled={refreshState.blocked}>
            <RefreshCw className={refreshState.running ? 'size-4 animate-spin' : 'size-4'} />
            刷新
          </Button>
        }
      />
      <PageFill>
        <div className="mb-4 grid shrink-0 gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <Stat
            label="私仓"
            value={props.summary.RepoConnected ? '已连接' : '未连接'}
            detail={props.summary.RepoURL || '未配置私仓地址'}
            tone={props.summary.RepoConnected ? 'good' : 'warn'}
          />
          <Stat label="受管项目" value={String(projects.length)} detail={`${initialized} 个已初始化`} />
          <Stat
            label="异常项目"
            value={String(broken)}
            detail={broken ? '打开项目查看具体原因' : '没有报错的项目'}
            tone={broken ? 'bad' : undefined}
          />
          <Stat
            label="Global IDE"
            value={props.settings.EffectiveIDEs.join(' · ') || '—'}
            detail={`服务空闲退出 ${props.settings.ServerIdleTimeout || '—'}`}
          />
        </div>

        <SplitPane
          rail={
            <>
              <Panel className="shrink-0">
                <PanelHeader title="常用操作" />
                <div className="divide-y divide-line">
                  <QuickAction icon={RefreshCw} label="拉取 Global 资产" hint="按当前选择落地到用户环境" onClick={props.onPullGlobal} />
                  <QuickAction icon={Globe} label="调整 Global 资产" hint="勾选装到这台设备的 bundle" onClick={() => props.onNavigate('global')} />
                  <QuickAction icon={FolderSearch} label="接管项目目录" hint="登记目录或扫描已有 Dec 项目" onClick={() => props.onNavigate('projects')} />
                  <QuickAction icon={Settings} label="设备设置" hint="私仓、IDE、服务实例" onClick={() => props.onNavigate('settings')} />
                </div>
              </Panel>
              <Panel className="shrink-0">
                <PanelHeader
                  title="最近同步"
                  action={
                    <Button size="sm" variant="ghost" onClick={() => props.onNavigate('sync')}>
                      全部记录
                      <ChevronRight className="size-3.5" />
                    </Button>
                  }
                />
                <PanelBody>
                  {props.lastPull ? <LastPull entry={props.lastPull} /> : (
                    <p className="text-xs leading-relaxed text-faint">这次连接还没有执行同步。拉取完成后，结论会显示在这里和同步记录页。</p>
                  )}
                </PanelBody>
              </Panel>
            </>
          }
        >
          <Panel className={fitBlock}>
            <PanelHeader
              title="项目"
              description={projects.length ? `共 ${projects.length} 个受管目录` : undefined}
              action={
                <Button size="sm" variant="ghost" onClick={() => props.onNavigate('projects')}>
                  管理项目
                  <ChevronRight className="size-3.5" />
                </Button>
              }
            />
            {projects.length === 0 ? (
              <PanelBody>
                <EmptyState
                  className="border-none"
                  icon={<FolderSearch className="size-5" />}
                  text="尚未接管任何项目"
                  hint="选择这台设备上的项目目录，Dec 就能把家项目和 requires 资产装进去。"
                  action={<Button size="sm" onClick={() => props.onNavigate('projects')}>选择项目目录</Button>}
                />
              </PanelBody>
            ) : (
              <ScrollArea splitOnly className="divide-y divide-line">
                {projects.map((project) => (
                  <button
                    key={project.Root}
                    onClick={() => props.onOpenProject(project)}
                    className="flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-panel-hi"
                  >
                    <Folder className="size-4 shrink-0 text-faint" />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-[13px] text-ink">{project.Label || project.Name}</span>
                      <span className="block truncate font-mono text-[11px] text-faint">{project.Root}</span>
                    </span>
                    {project.Error ? (
                      <Badge tone="bad">异常</Badge>
                    ) : project.Initialized ? (
                      <Badge tone="good">已初始化</Badge>
                    ) : (
                      <Badge tone="warn">待初始化</Badge>
                    )}
                    <ChevronRight className="size-4 shrink-0 text-line-hi" />
                  </button>
                ))}
              </ScrollArea>
            )}
          </Panel>
        </SplitPane>
      </PageFill>
    </Page>
  )
}

function QuickAction({
  icon: Icon,
  label,
  hint,
  onClick,
}: {
  icon: LucideIcon
  label: string
  hint: string
  onClick: () => void
}) {
  return (
    <button onClick={onClick} className="flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-panel-hi">
      <Icon className="size-4 shrink-0 text-faint" />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-[13px] text-ink">{label}</span>
        <span className="block truncate text-[11px] text-faint">{hint}</span>
      </span>
      <ChevronRight className="size-4 shrink-0 text-line-hi" />
    </button>
  )
}

function LastPull({ entry }: { entry: { title: string; result: PullResult; at: Date } }) {
  const { headline, skipped } = pullResultDiagnosis(entry.result)
  const failed = Boolean(entry.result.FailedCount)
  const problem = headline || skipped || entry.result.SecretsSkippedReason
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <span className="truncate text-[13px] font-medium text-ink">{entry.title}</span>
        {failed ? <Badge tone="bad">有失败</Badge> : problem ? <Badge tone="warn">需要处理</Badge> : <Badge tone="good">完成</Badge>}
      </div>
      <div className="text-[11px] text-faint">{entry.at.toLocaleString()}</div>
      <div className="tnum flex gap-4 text-xs text-muted">
        <span>已拉取 {entry.result.PulledCount || 0}</span>
        <span>Secrets {(entry.result.SecretsNoteCount || 0) + (entry.result.SecretsSSHKeyCount || 0)}</span>
        <span>失败 {entry.result.FailedCount || 0}</span>
      </div>
      {problem && (
        <p className="flex gap-1.5 text-xs leading-relaxed text-warn">
          <CircleAlert className="mt-0.5 size-3.5 shrink-0" />
          <span className="min-w-0">{problem}</span>
        </p>
      )}
    </div>
  )
}
