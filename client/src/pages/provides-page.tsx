import { useCallback, useEffect, useMemo, useState } from 'react'
import { Pencil, RefreshCw, Trash2 } from 'lucide-react'
import { ActionFeedback } from '@/components/action-feedback'
import { Page, PageHeader, PageScroll } from '@/components/shell/page'
import { ActionButton } from '@/components/ui/action-button'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { EmptyState, Loading } from '@/components/ui/feedback'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, Input, Select } from '@/components/ui/input'
import { Panel, PanelFooter } from '@/components/ui/panel'
import { useActionRegistry } from '@/lib/action-context'
import { invokeTyped } from '@/lib/api'
import { actionSpec, resource } from '@/lib/console'

type ProvideMapping = {
  Source: string
  Visibility: string
  Plane: string
  Type: string
  Name: string
  Target?: string
}

type ProvidesResponse = {
  Mappings?: ProvideMapping[]
  Provides?: ProvideMapping[] | Record<string, Omit<ProvideMapping, 'Target'>>
  Targets?: Record<string, string>
  ProvidesRoot?: string
  AuthorDirs?: string[] | null
}

type Candidate = ProvideMapping & { Origin?: string; Declared?: boolean }
type CandidatesResponse = { ProjectName?: string; Candidates?: Candidate[] | null }

// 作者根只是 source 的前缀：改根时把每条来源整体平移，
// 不用在前端再抄一份「类型 → 目录」的表。
function normalizeRoot(raw: string) {
  return raw.trim().replaceAll('\\', '/').replace(/^\/+|\/+$/g, '')
}

function withRoot(tail: string, root: string) {
  return root ? `${root}/${tail}` : tail
}

function stripRoot(source: string, root: string) {
  const prefix = root ? `${root}/` : ''
  return prefix && source.startsWith(prefix) ? source.slice(prefix.length) : source
}

function mappingsOf(value: ProvideMapping[] | ProvidesResponse) {
  if (Array.isArray(value)) return value
  if (value.Mappings) return value.Mappings
  if (Array.isArray(value.Provides)) return value.Provides
  return Object.entries(value.Provides || {}).map(([key, mapping]) => ({
    ...mapping,
    Name: mapping.Name || key,
    Target: value.Targets?.[key],
  }))
}

function providesPayload(mappings: ProvideMapping[]) {
  return Object.fromEntries(mappings.map(({ Target: _target, ...mapping }) => [mapping.Name, mapping]))
}

// 只有资产作者会碰提供项，所以它是项目的下级页面，不占项目页主区。
export function ProvidesPage(props: {
  deviceId: string
  root: string
  label: string
  onBack: () => void
}) {
  const [saved, setSaved] = useState<ProvideMapping[] | null>(null)
  const [draft, setDraft] = useState<ProvideMapping[]>([])
  const [savedRoot, setSavedRoot] = useState('')
  const [root, setRoot] = useState('')
  const [authorDirs, setAuthorDirs] = useState<string[]>([])
  const [editing, setEditing] = useState<number | null>(null)
  const [candidates, setCandidates] = useState<Candidate[] | null>(null)
  const actions = useActionRegistry()
  const runAction = actions.run
  const workspaceResource = resource.workspace(props.root)
  const loadSpec = useMemo(
    () => actionSpec(`provides:load:${props.deviceId}:${props.root}`, '加载项目提供项', props.deviceId, [workspaceResource], 'read'),
    [props.deviceId, props.root, workspaceResource],
  )
  const saveSpec = actionSpec(`provides:save:${props.deviceId}:${props.root}`, '保存项目提供项', props.deviceId, [workspaceResource], 'write', '项目提供项已保存')
  const suggestSpec = useMemo(
    () => actionSpec(`provides:suggest:${props.deviceId}:${props.root}`, '扫描可提供资产', props.deviceId, [workspaceResource], 'read'),
    [props.deviceId, props.root, workspaceResource],
  )

  const load = useCallback(async () => {
    const outcome = await runAction(
      loadSpec,
      () => invokeTyped<ProvideMapping[] | ProvidesResponse>(
        'load_project_provides',
        props.root,
        'local',
        {},
        loadSpec.key,
      ),
      { force: true },
    )
    if (!outcome.ok) return
    const next = mappingsOf(outcome.value)
    const nextRoot = Array.isArray(outcome.value) ? '' : outcome.value.ProvidesRoot || ''
    setSaved(next)
    setDraft(next)
    setSavedRoot(nextRoot)
    setRoot(nextRoot)
    setAuthorDirs(Array.isArray(outcome.value) ? [] : outcome.value.AuthorDirs || [])
    setEditing(null)
  }, [loadSpec, props.root, runAction])

  const scan = useCallback(async () => {
    const outcome = await runAction(
      suggestSpec,
      () => invokeTyped<CandidatesResponse>('suggest_project_provides', props.root, 'local', {}, suggestSpec.key),
      { force: true },
    )
    if (outcome.ok) setCandidates(outcome.value.Candidates || [])
  }, [props.root, runAction, suggestSpec])

  // oxlint-disable-next-line react/set-state-in-effect
  useEffect(() => { void load(); void scan() }, [load, scan])

  const dirty = JSON.stringify(saved || []) !== JSON.stringify(draft) || savedRoot !== root
  // 改根时已登记的来源必须跟着走，否则保存会被作者目录校验整批拒绝。
  // 落到 blur 而不是每次按键，免得输入路径分隔符时被吃掉。
  const commitRoot = (raw: string) => {
    const value = normalizeRoot(raw)
    if (value === root) return
    setDraft((items) => items.map((item) => ({ ...item, Source: withRoot(stripRoot(item.Source, root), value) })))
    setRoot(value)
  }
  const declaredSources = new Set(draft.map((item) => item.Source.toLowerCase()))
  const available = (candidates || []).filter((item) => !declaredSources.has(item.Source.toLowerCase()))
  const addCandidates = (items: Candidate[]) => {
    setDraft((current) => [
      ...current,
      ...items.map(({ Origin: _origin, Declared: _declared, ...mapping }) => mapping),
    ])
    setEditing(null)
  }
  const update = (index: number, value: ProvideMapping) => {
    setDraft((items) => items.map((item, itemIndex) => itemIndex === index ? value : item))
  }

  return (
    <Page>
      <PageHeader
        title="我提供的资产"
        description="登记本项目提供的 Git 资产；secrets 由 .secrets 同步规则决定，不在这里声明。"
        meta={<Badge tone="quiet" className="font-mono" title={props.root}>{props.label}</Badge>}
        actions={
          <>
            <Button variant="outline" onClick={props.onBack}>返回</Button>
            <Button variant="outline" onClick={() => void scan()}>
              <RefreshCw className="size-4" />
              重新扫描
            </Button>
          </>
        }
      />
      <PageScroll className="space-y-4">
        <div className="space-y-2 empty:hidden">
          <ActionFeedback actionKey={loadSpec.key} />
          <ActionFeedback actionKey={suggestSpec.key} />
          <ActionFeedback actionKey={saveSpec.key} />
        </div>
        <Panel>
          <AuthorRootField
            value={root}
            savedDirs={authorDirs}
            pending={root !== savedRoot}
            onCommit={commitRoot}
          />
          <CandidatePicker candidates={available} onAdd={addCandidates} />
          {saved === null ? (
            <Loading />
          ) : draft.length === 0 ? (
            <EmptyState
              text="当前项目还没有登记提供项"
              hint={available.length > 0
                ? '上面是从作者目录扫描到的资产，勾选即可登记。'
                : `请先在上面列出的作者目录中创建资产（${authorDirs.join('、') || 'skills、commands、rules、mcp'}），再重新扫描。`}
            />
          ) : (
            <div className="divide-y divide-line">
              {draft.map((item, index) => (
                <div key={`${index}-${item.Source}-${item.Name}`} className="px-4 py-3">
                  {editing === index ? (
                    <ProvideEditor
                      value={item}
                      onChange={(value) => update(index, value)}
                      onDone={() => setEditing(null)}
                    />
                  ) : (
                    <div className="flex items-center gap-3">
                      <div className="min-w-0 flex-1">
                        <div className="flex flex-wrap items-center gap-1.5">
                          <span className="font-mono text-xs font-medium text-ink">{item.Type}/{item.Name || '未命名'}</span>
                          <Badge tone="quiet">{item.Visibility}</Badge>
                          <Badge tone="quiet">{item.Plane}</Badge>
                        </div>
                        <p className="mt-1 truncate font-mono text-[11px] text-faint" title={item.Source}>
                          {item.Source || '尚未填写来源'}{item.Target ? ` → ${item.Target}` : ''}
                        </p>
                      </div>
                      <Button size="icon" variant="ghost" aria-label={`编辑 ${item.Name}`} onClick={() => setEditing(index)}>
                        <Pencil className="size-3.5" />
                      </Button>
                      <Button
                        size="icon"
                        variant="ghost"
                        aria-label={`删除 ${item.Name}`}
                        onClick={() => {
                          setDraft((items) => items.filter((_, itemIndex) => itemIndex !== index))
                          setEditing(null)
                        }}
                      >
                        <Trash2 className="size-3.5 text-bad" />
                      </Button>
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
          <PanelFooter>
            <ActionButton
              spec={saveSpec}
              disabled={!dirty || draft.some((item) => !item.Source.trim() || !item.Name.trim())}
              action={() => invokeTyped(
                'save_project_provides',
                props.root,
                'local',
                { ProvidesRoot: root, Provides: providesPayload(draft) },
                saveSpec.key,
              )}
              runningLabel="保存中…"
              onSuccess={() => { void load(); void scan() }}
            >
              保存提供项
            </ActionButton>
            <span className="text-xs text-faint">{dirty ? '有未保存的改动' : `已配置 ${draft.length} 项`}</span>
          </PanelFooter>
        </Panel>
      </PageScroll>
    </Page>
  )
}

// 作者根决定资产在仓库里的落点，默认仓库根。
// 换根不改远端布局，只是让 Dec 不必在每个项目根上占四个目录名。
function AuthorRootField(props: {
  value: string
  savedDirs: string[]
  pending: boolean
  onCommit: (value: string) => void
}) {
  const [raw, setRaw] = useState(props.value)
  useEffect(() => { setRaw(props.value) }, [props.value])
  const dirs = props.pending
    ? ['skills', 'commands', 'rules', 'mcp'].map((dir) => (props.value ? `${props.value}/${dir}` : dir))
    : props.savedDirs
  return (
    <div className="border-b border-line px-4 py-3">
      <Field
        label="作者目录基准点"
        className="max-w-sm"
        hint="新项目默认 DecAssets；旧项目留空仍表示仓库根。不能指向 .dec 或 .cursor 等工具目录。"
      >
        <Input
          value={raw}
          placeholder="DecAssets"
          aria-label="作者目录基准点"
          onChange={(event) => setRaw(event.target.value)}
          onBlur={() => props.onCommit(raw)}
          onKeyDown={(event) => { if (event.key === 'Enter') props.onCommit(raw) }}
        />
      </Field>
      <p className="mt-1.5 font-mono text-[11px] text-faint">
        {dirs.map((dir) => `${dir}/`).join('  ')}
      </p>
      {props.pending && (
        <p className="mt-1 text-[11px] text-warn">已登记来源会随基准点平移；保存后重新扫描才会按新目录取候选。</p>
      )}
    </div>
  )
}

// 扫描结果直接可勾选：源路径、类型、名称和目标都是派生好的，
// 人只需要认领哪些资产由本项目提供。
function CandidatePicker(props: { candidates: Candidate[]; onAdd: (items: Candidate[]) => void }) {
  const [picked, setPicked] = useState<string[]>([])
  if (props.candidates.length === 0) return null
  const chosen = props.candidates.filter((item) => picked.includes(item.Source))
  return (
    <div className="border-b border-line bg-canvas/40 px-4 py-3">
      <div className="mb-2 flex items-center gap-2">
        <span className="text-xs font-medium text-ink">项目里发现 {props.candidates.length} 项可提供资产</span>
        <Button
          size="sm"
          variant="ghost"
          className="ml-auto"
          onClick={() => setPicked(picked.length === props.candidates.length ? [] : props.candidates.map((item) => item.Source))}
        >
          {picked.length === props.candidates.length ? '清空' : '全选'}
        </Button>
        <Button size="sm" disabled={chosen.length === 0} onClick={() => { props.onAdd(chosen); setPicked([]) }}>
          添加选中的 {chosen.length || ''}
        </Button>
      </div>
      <div className="max-h-56 space-y-1 overflow-auto">
        {props.candidates.map((item) => (
          <label key={item.Source} className="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 hover:bg-panel-hi">
            <Checkbox
              aria-label={item.Source}
              checked={picked.includes(item.Source)}
              onChange={() => setPicked((current) => (
                current.includes(item.Source)
                  ? current.filter((source) => source !== item.Source)
                  : [...current, item.Source]
              ))}
            />
            <span className="min-w-0 flex-1">
              <span className="block truncate font-mono text-[11px] text-ink" title={item.Source}>{item.Source}</span>
              <span className="block truncate text-[11px] text-faint" title={item.Target}>
                {item.Origin}{item.Target ? ` → ${item.Target}` : ''}
              </span>
            </span>
            <Badge tone="quiet">{item.Type}</Badge>
            <Badge tone="quiet">{item.Visibility}</Badge>
          </label>
        ))}
      </div>
    </div>
  )
}

function ProvideEditor(props: {
  value: ProvideMapping
  onChange: (value: ProvideMapping) => void
  onDone: () => void
}) {
  const set = (field: keyof ProvideMapping, value: string) => props.onChange({ ...props.value, [field]: value })
  return (
    <div className="grid gap-3 lg:grid-cols-6">
      <Field label="本地来源" className="lg:col-span-3">
        <div className="flex h-9 items-center rounded-lg border border-line bg-canvas/60 px-3 font-mono text-xs text-muted">
          {props.value.Source}
        </div>
      </Field>
      <Field label="类型">
        <div className="flex h-9 items-center rounded-lg border border-line bg-canvas/60 px-3 text-xs text-muted">
          {props.value.Type}
        </div>
      </Field>
      <Field label="名称" className="lg:col-span-2">
        <div className="flex h-9 items-center rounded-lg border border-line bg-canvas/60 px-3 font-mono text-xs text-muted">
          {props.value.Name}
        </div>
      </Field>
      <Field label="可见性">
        <Select value={props.value.Visibility} onChange={(event) => set('Visibility', event.target.value)}>
          <option value="private">private</option>
          <option value="public">public</option>
        </Select>
      </Field>
      <Field label="平面">
        <Select value={props.value.Plane} onChange={(event) => set('Plane', event.target.value)}>
          <option value="local">local</option>
          <option value="global">global</option>
        </Select>
      </Field>
      <p className="self-end text-xs leading-relaxed text-faint lg:col-span-3">
        来源、类型和名称由仓库资产选择派生；这里只调整可见性和平面。
      </p>
      <div className="flex items-end">
        <Button size="sm" variant="secondary" onClick={props.onDone}>完成编辑</Button>
      </div>
    </div>
  )
}
