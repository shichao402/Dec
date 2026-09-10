import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { CheckCircle2, ChevronRight, CornerLeftUp, Folder, FolderSearch, LoaderCircle, RefreshCw, Search } from 'lucide-react'
import { ActionFeedback } from '@/components/action-feedback'
import { Page, PageFill, PageHeader, ScrollArea, Toolbar } from '@/components/shell/page'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { EmptyState } from '@/components/ui/feedback'
import { Input } from '@/components/ui/input'
import { Panel, PanelBody, PanelFooter, PanelHeader } from '@/components/ui/panel'
import { useActionRegistry, useDecAction } from '@/lib/action-context'
import type { ActionRecord } from '@/lib/action-registry'
import { invokeTyped, runOrWatchTyped } from '@/lib/api'
import { actionSpec, resource } from '@/lib/console'
import { cn } from '@/lib/utils'
import type { DirectoryListing, ManagedProject } from '@/lib/utils'

// auto-fit：项目少时卡片自己铺开占满整行，不会在宽屏右侧留一整列空白。
const cardGrid = 'grid gap-3 [grid-template-columns:repeat(auto-fit,minmax(18rem,1fr))]'

// 接管流水线的两块面板：分栏时各自吃满整列并在内部滚动；堆叠时不设高度上限，
// 由整页滚动兜底。堆叠时强行 max-h-full，面板内容会溢出并画到自己的 footer 上。
const flowPanel = 'flex min-h-0 shrink-0 flex-col xl:max-h-full xl:shrink xl:overflow-hidden'
// 列表同理：窄屏固定高度，分栏时才吃满剩余高度。
const flowList = 'h-56 min-h-0 shrink-0 overflow-y-auto xl:h-auto xl:flex-1 xl:shrink'

export function ProjectsPage(props: {
  deviceId: string
  projects: ManagedProject[]
  onRefresh: () => Promise<void>
  onOpen: (project: ManagedProject) => void
}) {
  const [picker, setPicker] = useState(false)
  const [browserPath, setBrowserPath] = useState('')
  const [selected, setSelected] = useState<string[]>([])
  const [query, setQuery] = useState('')
  const actions = useActionRegistry()
  const refreshState = useDecAction(
    actionSpec(`device:refresh:${props.deviceId}`, '刷新设备状态', props.deviceId, [resource.global], 'read'),
  )
  const scanPrefix = `projects:scan:${props.deviceId}:`
  const importPrefix = `projects:register:${props.deviceId}:`
  const records = Object.values(actions.state.records)
  // 扫描反馈跟着最近一次扫描走（可能还在跑），结果只认最近一次成功的。
  const scanRecord = records
    .filter((record) => record.key.startsWith(scanPrefix))
    .sort((a, b) => (b.finishedAt || b.startedAt) - (a.finishedAt || a.startedAt))[0]
  const latestScan = records
    .filter((record) => record.key.startsWith(scanPrefix) && record.status === 'succeeded')
    .sort((a, b) => (b.finishedAt || 0) - (a.finishedAt || 0))[0]
  const scan = ((latestScan?.result as { Projects?: ManagedProject[] } | undefined)?.Projects || [])
    .filter((project) => !props.projects.some((item) => item.Root === project.Root))

  const importSpec = (root: string) => actionSpec(
    `${importPrefix}${root}`,
    `导入 ${root}`,
    props.deviceId,
    [resource.global],
    'write',
    '项目已导入',
  )
  // 只用来问「现在能不能改本机受管列表」，这个 key 本身永远不会执行。
  const importBlocked = Boolean(actions.blockedBy(importSpec('#probe')))
  const importing = records.some((record) => record.key.startsWith(importPrefix) && record.status === 'running')
  const picked = selected.filter((root) => scan.some((project) => project.Root === root))

  // 队列串行跑：受管列表是同一份全局配置，并发导入只会互相阻塞。
  // 跑完才刷新一次设备——每导入一个就全量巡检，等待时间会随选中数线性叠加。
  async function importRoots(roots: string[]) {
    const failed: string[] = []
    for (const root of roots) {
      const spec = importSpec(root)
      const outcome = await actions.run(spec, () => invokeTyped<ManagedProject>(
        'register_managed_project',
        '',
        'global',
        { Root: root },
        spec.key,
      ))
      if (!outcome.ok) failed.push(root)
    }
    await props.onRefresh()
    // 只清掉这一批里成功的；期间新勾的行保留，失败的留在选中态里可以直接重试。
    setSelected((prev) => prev.filter((root) => failed.includes(root) || !roots.includes(root)))
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
        {picker ? (
          // 选目录 → 扫描 → 勾选导入是一条流水线：两步并排铺满，别把结果挤成四行。
          <div className="flex min-h-0 flex-1 flex-col gap-4 xl:grid xl:grid-cols-2 xl:overflow-hidden">
            <DirectoryBrowser
              deviceId={props.deviceId}
              initialPath={browserPath}
              onPathChange={setBrowserPath}
              onSelect={(root) => void importRoots([root])}
              onScan={scanRoot}
            />
            <ScanResults
              projects={scan}
              picked={picked}
              scanRecord={scanRecord}
              importing={importing}
              blocked={importBlocked}
              recordOf={(root) => actions.state.records[`${importPrefix}${root}`]}
              onToggle={(root) => setSelected((prev) => (
                prev.includes(root) ? prev.filter((item) => item !== root) : [...prev, root]
              ))}
              onToggleAll={() => setSelected(
                picked.length >= scan.length ? [] : scan.map((project) => project.Root),
              )}
              onImport={(roots) => void importRoots(roots)}
            />
          </div>
        ) : (
          <>
            {scan.length > 0 && (
              <div className="mb-3 flex shrink-0 flex-wrap items-center gap-3 rounded-xl border border-accent/30 bg-accent/8 px-3.5 py-2.5">
                <FolderSearch className="size-4 shrink-0 text-accent-hi" />
                <span className="min-w-0 flex-1 text-xs text-muted">
                  上次扫描还有 {scan.length} 个尚未接管的项目
                </span>
                <Button size="sm" variant="secondary" onClick={() => setPicker(true)}>继续导入</Button>
              </div>
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
          </>
        )}
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

function ScanResults(props: {
  projects: ManagedProject[]
  picked: string[]
  scanRecord?: ActionRecord
  importing: boolean
  blocked: boolean
  recordOf: (root: string) => ActionRecord | undefined
  onToggle: (root: string) => void
  onToggleAll: () => void
  onImport: (roots: string[]) => void
}) {
  const total = props.projects.length
  const allPicked = total > 0 && props.picked.length >= total
  const done = props.picked.filter((root) => props.recordOf(root)?.status === 'succeeded').length
  const scanning = props.scanRecord?.status === 'running'
  const scanned = Boolean(props.scanRecord) && !scanning

  return (
    <Panel className={cn(flowPanel, 'min-h-[16rem]')}>
      <PanelHeader
        title="第 2 步 · 勾选要导入的项目"
        description={total > 0
          ? `${total} 个尚未接管 · 已选 ${props.picked.length}`
          : '导入只登记路径，不改动项目里的任何文件。'}
        action={total > 0 && (
          <Button size="sm" variant="ghost" onClick={props.onToggleAll}>{allPicked ? '清空选择' : '全选'}</Button>
        )}
      />
      {props.scanRecord && (
        <div className="shrink-0 px-4 pt-3 empty:hidden">
          <ActionFeedback actionKey={props.scanRecord.key} />
        </div>
      )}
      {total === 0 ? (
        <PanelBody className="flex min-h-0 flex-1 items-center justify-center">
          <EmptyState
            className="w-full border-none"
            icon={<FolderSearch className="size-5" />}
            text={scanning ? '正在扫描这个范围' : scanned ? '这个范围里的项目都已接管' : '还没有扫描结果'}
            hint={scanned
              ? '换一个目录再扫，或用「接管此目录」直接登记左边选中的路径。'
              : '在左边选一个目录，点「扫描此范围」。带 .dec 或 .git 的目录都会被找出来。'}
          />
        </PanelBody>
      ) : (
        <div className={cn(flowList, 'divide-y divide-line')}>
          {props.projects.map((project) => (
            <ScanRow
              key={project.Root}
              project={project}
              checked={props.picked.includes(project.Root)}
              record={props.recordOf(project.Root)}
              importing={props.importing}
              blocked={props.blocked}
              onToggle={() => props.onToggle(project.Root)}
              onImport={() => props.onImport([project.Root])}
            />
          ))}
        </div>
      )}
      <PanelFooter>
        <Button
          disabled={props.picked.length === 0 || props.blocked}
          onClick={() => props.onImport(props.picked)}
        >
          {props.importing && <LoaderCircle className="size-4 animate-spin" />}
          {props.importing
            ? `导入中 ${done}/${props.picked.length}`
            : props.picked.length > 0
              ? `导入选中的 ${props.picked.length} 个`
              : '导入选中项'}
        </Button>
        <span className="min-w-0 flex-1 text-[11px] leading-relaxed text-faint">
          勾完一次性导入：队列串行执行，全部登记完才刷新一次设备，不用守着一个个点。
        </span>
      </PanelFooter>
    </Panel>
  )
}

function ScanRow(props: {
  project: ManagedProject
  checked: boolean
  record?: ActionRecord
  importing: boolean
  blocked: boolean
  onToggle: () => void
  onImport: () => void
}) {
  const status = props.record?.status
  const running = status === 'running'
  const imported = status === 'succeeded'
  const queued = props.checked && props.importing && !running && !imported

  return (
    <div className={cn('flex items-center gap-3 px-4 py-2', props.checked && 'bg-accent/8')}>
      <label className="flex min-w-0 flex-1 cursor-pointer items-center gap-2.5 py-1">
        <Checkbox
          aria-label={props.project.Root}
          checked={props.checked}
          disabled={running || imported}
          onChange={props.onToggle}
        />
        <Folder className="size-4 shrink-0 text-faint" />
        <span className="min-w-0 flex-1 truncate font-mono text-xs text-muted" title={props.project.Root}>
          {props.project.Root}
        </span>
      </label>
      {status === 'failed' && props.record?.error && (
        <span className="max-w-[12rem] truncate text-[11px] text-bad" title={props.record.error}>
          {props.record.error}
        </span>
      )}
      <Badge tone={props.project.Initialized ? 'good' : 'warn'}>
        {props.project.Initialized ? '已初始化' : '待初始化'}
      </Badge>
      {/* 固定宽度的状态位：导入过程中行不会因为字数变化左右跳动。 */}
      <div className="flex w-20 shrink-0 items-center justify-end">
        {running ? (
          <span className="flex items-center gap-1.5 text-[11px] text-accent-hi">
            <LoaderCircle className="size-3.5 animate-spin" />
            导入中
          </span>
        ) : imported ? (
          <span className="flex items-center gap-1.5 text-[11px] text-good">
            <CheckCircle2 className="size-3.5" />
            已导入
          </span>
        ) : queued ? (
          <span className="text-[11px] text-faint">排队中</span>
        ) : (
          <Button size="sm" variant="ghost" disabled={props.blocked} onClick={props.onImport}>导入</Button>
        )}
      </div>
    </div>
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
    <Panel className={cn(flowPanel, 'min-h-[18rem]')}>
      <PanelHeader title="第 1 步 · 选择设备上的目录" description="双击进入下一级；选中后扫描这个范围，或直接接管它。" />
      <PanelBody className="flex min-h-0 flex-1 flex-col gap-3">
        <ActionFeedback actionKey={browseSpec.key} />
        <div className="flex shrink-0 flex-wrap items-center gap-2">
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
        <div className={cn(flowList, 'rounded-lg border border-line bg-canvas/60')}>
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
        <Button disabled={!path || mutationBlocked} onClick={() => props.onScan(path)}>扫描此范围</Button>
        <Button variant="outline" disabled={!path || mutationBlocked} onClick={() => props.onSelect(path)}>接管此目录</Button>
        <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-faint">{path || '未选择目录'}</span>
      </PanelFooter>
    </Panel>
  )
}
