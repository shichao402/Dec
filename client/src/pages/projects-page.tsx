import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ChevronRight, CornerLeftUp, Folder, FolderSearch, RefreshCw, Search } from 'lucide-react'
import { ActionFeedback } from '@/components/action-feedback'
import { Page, PageFill, PageHeader, ScrollArea, Toolbar } from '@/components/shell/page'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ActionButton } from '@/components/ui/action-button'
import { EmptyState } from '@/components/ui/feedback'
import { Input } from '@/components/ui/input'
import { Panel, PanelBody, PanelFooter, PanelHeader } from '@/components/ui/panel'
import { useActionRegistry, useDecAction } from '@/lib/action-context'
import { invokeTyped, runOrWatchTyped } from '@/lib/api'
import { actionSpec, resource } from '@/lib/console'
import { cn } from '@/lib/utils'
import type { DirectoryListing, ManagedProject } from '@/lib/utils'

// auto-fit：项目少时卡片自己铺开占满整行，不会在宽屏右侧留一整列空白。
const cardGrid = 'grid gap-3 [grid-template-columns:repeat(auto-fit,minmax(18rem,1fr))]'

export function ProjectsPage(props: {
  deviceId: string
  projects: ManagedProject[]
  onRefresh: () => Promise<void>
  onOpen: (project: ManagedProject) => void
}) {
  const [picker, setPicker] = useState(false)
  const [browserPath, setBrowserPath] = useState('')
  const [query, setQuery] = useState('')
  const actions = useActionRegistry()
  const refreshState = useDecAction(
    actionSpec(`device:refresh:${props.deviceId}`, '刷新设备状态', props.deviceId, [resource.global], 'read'),
  )
  const scanPrefix = `projects:scan:${props.deviceId}:`
  const latestScan = Object.values(actions.state.records)
    .filter((record) => record.key.startsWith(scanPrefix) && record.status === 'succeeded')
    .sort((a, b) => (b.finishedAt || 0) - (a.finishedAt || 0))[0]
  const scan = ((latestScan?.result as { Projects?: ManagedProject[] } | undefined)?.Projects || [])
    .filter((project) => !props.projects.some((item) => item.Root === project.Root))

  async function register(root: string) {
    const spec = actionSpec(`projects:register:${props.deviceId}:${root}`, `导入 ${root}`, props.deviceId, [resource.global], 'write', '项目已导入')
    const outcome = await actions.run(spec, () => invokeTyped<ManagedProject>('register_managed_project', '', 'global', { Root: root }, spec.key))
    if (outcome.ok) await props.onRefresh()
  }

  async function scanRoot(root: string) {
    const spec = actionSpec(`${scanPrefix}${root}`, `扫描 ${root}`, props.deviceId, [resource.global], 'operation', '项目扫描完成')
    await actions.run(spec, () => runOrWatchTyped<{ Projects: ManagedProject[] }>({
      actionKey: spec.key,
      operation: 'scan_managed_projects',
      projectRoot: '',
      workspacePlane: 'global',
      payload: { ScanRoot: root, MaxDepth: 6 },
    }))
  }

  const currentScanKey = browserPath ? `${scanPrefix}${browserPath}` : latestScan?.key
  const keyword = query.trim().toLowerCase()
  const filtered = keyword
    ? props.projects.filter((project) =>
        `${project.Label} ${project.Name} ${project.Root}`.toLowerCase().includes(keyword))
    : props.projects
  const initialized = props.projects.filter((project) => project.Initialized).length

  return (
    <Page>
      <PageHeader
        title="项目"
        description="以显式登记的目录为主；只有你选定范围后才会扫描已有 Dec 项目。"
        actions={
          <>
            <Button variant="outline" onClick={() => void props.onRefresh()} disabled={refreshState.blocked}>
              <RefreshCw className={refreshState.running ? 'size-4 animate-spin' : 'size-4'} />
              刷新
            </Button>
            <Button onClick={() => setPicker((value) => !value)}>
              <FolderSearch className="size-4" />
              {picker ? '收起目录选择' : '接管目录'}
            </Button>
          </>
        }
      />
      <PageFill>
        {currentScanKey && <ActionFeedback actionKey={currentScanKey} />}
        {picker && (
          <DirectoryBrowser
            deviceId={props.deviceId}
            initialPath={browserPath}
            onPathChange={setBrowserPath}
            onSelect={register}
            onScan={scanRoot}
          />
        )}
        {scan.length > 0 && (
          <Panel className="mb-4 shrink-0">
            <PanelHeader title="扫描发现" description={`${scan.length} 个尚未接管的 Dec 项目`} />
            <div className="max-h-56 divide-y divide-line overflow-y-auto">
              {scan.map((project) => {
                const spec = actionSpec(`projects:register:${props.deviceId}:${project.Root}`, `导入 ${project.Root}`, props.deviceId, [resource.global], 'write', '项目已导入')
                return (
                  <div key={project.Root} className="flex items-center gap-3 px-4 py-2.5">
                    <Folder className="size-4 shrink-0 text-faint" />
                    <span className="min-w-0 flex-1 truncate font-mono text-xs text-muted">{project.Root}</span>
                    <ActionButton
                      size="sm"
                      variant="secondary"
                      spec={spec}
                      action={() => invokeTyped<ManagedProject>('register_managed_project', '', 'global', { Root: project.Root }, spec.key)}
                      runningLabel="导入中…"
                      onSuccess={props.onRefresh}
                    >
                      导入
                    </ActionButton>
                  </div>
                )
              })}
            </div>
          </Panel>
        )}

        <Toolbar>
          <div className="relative w-72">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-faint" />
            <Input className="pl-8" placeholder="按名称或路径过滤" value={query} onChange={(e) => setQuery(e.target.value)} />
          </div>
          <span className="tnum text-xs text-faint">
            {keyword ? `${filtered.length} / ${props.projects.length} 个匹配` : `共 ${props.projects.length} 个 · 已初始化 ${initialized}`}
          </span>
        </Toolbar>

        <ScrollArea className="-mx-1 px-1 pb-1">
          {filtered.length === 0 ? (
            <EmptyState
              icon={<FolderSearch className="size-5" />}
              text={props.projects.length === 0 ? '这台设备还没有受管项目' : '没有匹配的项目'}
              hint={props.projects.length === 0
                ? '用「接管目录」选择设备上的项目路径，或先扫描一个范围找出已有 Dec 项目。'
                : '换个关键词，或清空过滤条件。'}
              action={props.projects.length === 0
                ? <Button size="sm" onClick={() => setPicker(true)}>接管目录</Button>
                : <Button size="sm" variant="ghost" onClick={() => setQuery('')}>清空过滤</Button>}
            />
          ) : (
            <div className={cardGrid}>
              {filtered.map((project) => (
                <ProjectCard key={project.Root} project={project} onOpen={() => props.onOpen(project)} />
              ))}
            </div>
          )}
        </ScrollArea>
      </PageFill>
    </Page>
  )
}

function ProjectCard({ project, onOpen }: { project: ManagedProject; onOpen: () => void }) {
  const broken = Boolean(project.Error) || !project.Exists
  return (
    <button
      onClick={onOpen}
      className="group flex min-h-[6.5rem] flex-col gap-1.5 rounded-xl border border-line bg-panel p-3.5 text-left transition-colors hover:border-line-hi hover:bg-panel-hi"
    >
      <div className="flex min-w-0 items-center gap-2.5">
        <span
          className={cn(
            'grid size-8 shrink-0 place-items-center rounded-lg',
            broken ? 'bg-bad/12 text-bad' : 'bg-panel-hi text-faint group-hover:text-ink',
          )}
        >
          <Folder className="size-4" />
        </span>
        <span
          className="min-w-0 flex-1 truncate text-[13px] font-medium text-ink"
          title={project.Label || project.Name}
        >
          {project.Label || project.Name}
        </span>
        {!project.Exists ? (
          <Badge tone="bad">目录缺失</Badge>
        ) : project.Error ? (
          <Badge tone="bad">异常</Badge>
        ) : project.Initialized ? (
          <Badge tone="good">已初始化</Badge>
        ) : (
          <Badge tone="warn">待初始化</Badge>
        )}
      </div>
      <div className="truncate font-mono text-[11px] text-faint" title={project.Root}>{project.Root}</div>
      {project.Error && <div className="text-[11px] leading-relaxed text-bad">{project.Error}</div>}
      <div className="mt-auto flex items-center gap-1 text-[11px] text-faint transition-colors group-hover:text-accent-hi">
        {project.Initialized ? '进入项目' : '初始化项目'}
        <ChevronRight className="size-3.5" />
      </div>
    </button>
  )
}

function DirectoryBrowser(props: {
  deviceId: string
  initialPath: string
  onPathChange: (path: string) => void
  onSelect: (root: string) => void | Promise<void>
  onScan: (root: string) => void | Promise<void>
}) {
  const { initialPath: savedPath, onPathChange } = props
  const initialPath = useRef(savedPath)
  const [listing, setListing] = useState<DirectoryListing | null>(null)
  const [path, setPath] = useState('')
  const actions = useActionRegistry()
  const runAction = actions.run
  const browseSpec = useMemo(
    () => actionSpec(`directories:browse:${props.deviceId}`, '读取目标设备目录', props.deviceId, [resource.filesystem], 'read'),
    [props.deviceId],
  )
  const browseState = useDecAction<DirectoryListing>(browseSpec)
  const registerSpec = actionSpec(`projects:register:${props.deviceId}:${path}`, `导入 ${path}`, props.deviceId, [resource.global], 'write')
  const scanSpec = actionSpec(`projects:scan:${props.deviceId}:${path}`, `扫描 ${path}`, props.deviceId, [resource.global], 'operation')
  const mutationBlocked = Boolean(actions.blockedBy(registerSpec) || actions.blockedBy(scanSpec))
  const open = useCallback(async (target = '') => {
    const outcome = await runAction(browseSpec, () => invokeTyped<DirectoryListing>('browse_directories', '', 'global', { Path: target }, browseSpec.key), { force: true })
    if (outcome.ok) {
      const value = outcome.value
      setListing(value)
      setPath(value.Current)
      onPathChange(value.Current)
    }
  }, [browseSpec, onPathChange, runAction])
  // 首次打开从服务器 Home 开始；收起再打开时恢复用户上次浏览的位置。
  // oxlint-disable-next-line react/set-state-in-effect
  useEffect(() => { void open(initialPath.current) }, [open])

  return (
    <Panel className="mb-4 shrink-0">
      <PanelHeader title="选择设备上的目录" description="双击进入下一级，选中后可直接接管，或只扫描这个范围。" />
      <PanelBody className="space-y-3">
        <ActionFeedback actionKey={browseSpec.key} />
        <div className="flex flex-wrap items-center gap-2">
          <div className="flex min-w-[18rem] flex-1 gap-2">
            <Input
              className="font-mono text-xs"
              value={path}
              onChange={(e) => setPath(e.target.value)}
              onKeyDown={(e) => { if (e.key === 'Enter') void open(path) }}
            />
            <Button variant="secondary" onClick={() => open(path)}>
              {browseState.running ? '打开中…' : '打开'}
            </Button>
          </div>
          <div className="flex flex-wrap items-center gap-1">
            {listing?.Parent && (
              <Button size="sm" variant="ghost" onClick={() => open(listing.Parent)}>
                <CornerLeftUp className="size-3.5" />
                上一级
              </Button>
            )}
            {listing?.Home && <Button size="sm" variant="ghost" onClick={() => open(listing.Home)}>Home</Button>}
            {listing?.Roots.map((root) => (
              <Button key={root} size="sm" variant="ghost" className="font-mono" onClick={() => open(root)}>{root}</Button>
            ))}
          </div>
        </div>
        <div className="h-56 overflow-y-auto rounded-lg border border-line bg-canvas/60">
          {listing?.Entries.length === 0 && (
            <div className="px-3 py-6 text-center text-xs text-faint">这个目录下没有子目录</div>
          )}
          {listing?.Entries.map((entry) => (
            <button
              key={entry.Path}
              onDoubleClick={() => open(entry.Path)}
              onClick={() => setPath(entry.Path)}
              className={cn(
                'flex w-full items-center gap-2 px-3 py-1.5 text-left text-[13px] transition-colors',
                path === entry.Path ? 'bg-accent/12 text-ink' : 'text-muted hover:bg-panel-hi hover:text-ink',
              )}
            >
              <Folder className="size-3.5 shrink-0 text-faint" />
              <span className="min-w-0 truncate">{entry.Name}</span>
            </button>
          ))}
        </div>
      </PanelBody>
      <PanelFooter>
        <Button disabled={!path || mutationBlocked} onClick={() => props.onSelect(path)}>接管此目录</Button>
        <Button variant="outline" disabled={!path || mutationBlocked} onClick={() => props.onScan(path)}>扫描此范围</Button>
        <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-faint">{path || '未选择目录'}</span>
      </PanelFooter>
    </Panel>
  )
}
