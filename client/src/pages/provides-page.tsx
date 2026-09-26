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
import { actionSpec, hasTag, resource, toggleTag, PROJECT_TAG_GLOBAL } from '@/lib/console'

type ProvideMapping = {
  Source: string
  Visibility: string
  Plane: string
  Type: string
  Name: string
  Target?: string
}

type ProductState = {
  Name: string
  Root: string
  Tags?: string[] | null
  SecretsPlane?: string
  Provides?: Record<string, Omit<ProvideMapping, 'Target'>> | null
  Targets?: Record<string, string>
  AuthorDirs?: string[] | null
  IdentityOnly?: boolean
}

type ProvidesResponse = {
  ProjectName?: string
  ProvidesRoot?: string
  AuthorDirs?: string[] | null
  Mappings?: ProvideMapping[]
  Provides?: ProvideMapping[] | Record<string, Omit<ProvideMapping, 'Target'>>
  Targets?: Record<string, string>
  Products?: ProductState[] | null
}

type Candidate = ProvideMapping & { Origin?: string; Declared?: boolean; Product?: string }
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

type ProductDraft = {
  Name: string
  Root: string
  Tags: string[]
  SecretsPlane: string
  Items: ProvideMapping[]
  IdentityOnly: boolean
}

type LoadedState = {
  mappings: ProvideMapping[]
  root: string
  authorDirs: string[]
  products: ProductDraft[]
  isMulti: boolean
  projectName: string
}

function toDraft(product: ProductState): ProductDraft {
  const items = Object.entries(product.Provides || {}).map(([key, mapping]) => ({
    ...mapping,
    Name: mapping.Name || key,
    Target: product.Targets?.[key],
  }))
  return {
    Name: product.Name,
    Root: product.Root,
    Tags: product.Tags || [],
    SecretsPlane: product.SecretsPlane || '',
    Items: items,
    IdentityOnly: product.IdentityOnly ?? items.length === 0,
  }
}

function fromResponse(value: ProvideMapping[] | ProvidesResponse): LoadedState {
  if (Array.isArray(value)) {
    return { mappings: value, root: '', authorDirs: [], products: [], isMulti: false, projectName: '' }
  }
  const products = (value.Products || []).map(toDraft)
  return {
    mappings: mappingsOf(value),
    root: value.ProvidesRoot || '',
    authorDirs: value.AuthorDirs || [],
    products,
    isMulti: products.length > 0,
    projectName: value.ProjectName || '',
  }
}

// 只有资产作者会碰提供项，所以它是项目的下级页面，不占项目页主区。
export function ProvidesPage(props: {
  deviceId: string
  root: string
  label: string
  onBack: () => void
}) {
  const [saved, setSaved] = useState<LoadedState | null>(null)
  const [draft, setDraft] = useState<LoadedState | null>(null)
  const [editing, setEditing] = useState<string | null>(null)
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
    const next = fromResponse(outcome.value)
    setSaved(next)
    setDraft(next)
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

  const dirty = saved === null || draft === null
    ? false
    : JSON.stringify(saved) !== JSON.stringify(draft)
  const multi = draft?.isMulti ?? false
  const products = draft?.products || []
  const singleCount = draft ? draft.mappings.length : 0
  const totalCount = multi ? products.reduce((sum, item) => sum + item.Items.length, 0) : singleCount

  const updateProduct = (name: string, value: ProductDraft) => {
    setDraft((current) => current && ({
      ...current,
      products: current.products.map((item) => item.Name === name ? value : item),
    }))
  }
  const updateProductItems = (name: string, items: ProvideMapping[]) => {
    setDraft((current) => current && ({
      ...current,
      products: current.products.map((item) => item.Name === name
        ? { ...item, Items: items, IdentityOnly: items.length === 0 && item.IdentityOnly }
        : item),
    }))
  }
  const updateMappings = (value: ProvideMapping[]) => {
    setDraft((current) => current && { ...current, mappings: value })
  }

  // 多产品仓保存：整体替换 products 声明，tags/secrets_plane/身份标记一并落盘。
  const save = () => invokeTyped(
    'save_project_provides',
    props.root,
    'local',
    multi
      ? {
        Products: products.map((product) => ({
          Name: product.Name,
          Root: product.Root,
          Tags: product.Tags,
          SecretsPlane: product.SecretsPlane,
          IdentityOnly: product.IdentityOnly,
          Provides: Object.fromEntries(product.Items.map(({ Target: _target, ...mapping }) => [mapping.Name, mapping])),
        })),
      }
      : { ProvidesRoot: draft?.root || '', Provides: providesPayload(draft?.mappings || []) },
    saveSpec.key,
  )

  const invalid = multi
    ? products.some((product) => !product.Name.trim() || product.Items.some((item) => !item.Source.trim() || !item.Name.trim()))
    : (draft?.mappings || []).some((item) => !item.Source.trim() || !item.Name.trim())

  return (
    <Page>
      <PageHeader
        title="我提供的资产"
        description={multi
          ? `多产品仓：${products.length} 个产品，${totalCount} 项提供。身份型产品只发身份，密钥留在 Bitwarden。`
          : '登记本项目提供的 Git 资产；secrets 由 .secrets 同步规则决定，不在这里声明。'}
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
        {draft === null ? (
          <Loading />
        ) : multi ? (
          <>
            {products.map((product) => (
              <ProductPanel
                key={product.Name}
                product={product}
                editing={editing}
                setEditing={setEditing}
                candidates={(candidates || []).filter((item) => (item.Product || '') === product.Name)}
                onUpdate={(value) => updateProduct(product.Name, value)}
                onUpdateItems={(items) => updateProductItems(product.Name, items)}
              />
            ))}
            {products.length === 0 && (
              <Panel>
                <EmptyState
                  text="当前项目还没有产品声明"
                  hint="写入 .dec/config.yaml 的 products 字段后，这里会按产品分组展示。"
                />
              </Panel>
            )}
          </>
        ) : (
          <Panel>
            <AuthorRootField
              value={draft.root}
              savedDirs={draft.authorDirs}
              pending={draft.root !== (saved?.root ?? draft.root)}
              onCommit={(value) => {
                const normalized = normalizeRoot(value)
                setDraft((current) => current && {
                  ...current,
                  root: normalized,
                  mappings: current.mappings.map((item) => ({
                    ...item,
                    Source: withRoot(stripRoot(item.Source, current.root), normalized),
                  })),
                })
              }}
            />
            <CandidatePicker
              candidates={(candidates || []).filter((item) => !item.Product)}
              onAdd={(items) => updateMappings([...draft.mappings, ...items])}
            />
            {draft.mappings.length === 0 ? (
              <EmptyState
                text="当前项目还没有登记提供项"
                hint={((candidates || []).filter((item) => !item.Product)).length > 0
                  ? '上面是从作者目录扫描到的资产，勾选即可登记。'
                  : `请先在上面列出的作者目录中创建资产（${draft.authorDirs.join('、') || 'skills、commands、rules、mcp'}），再重新扫描。`}
              />
            ) : (
              <div className="divide-y divide-line">
                {draft.mappings.map((item, index) => (
                  <ProvideRow
                    key={`${index}-${item.Source}-${item.Name}`}
                    item={item}
                    editing={editing === `single-${index}`}
                    onEdit={() => setEditing(`single-${index}`)}
                    onDone={() => setEditing(null)}
                    onChange={(value) => updateMappings(draft.mappings.map((current, currentIndex) => currentIndex === index ? value : current))}
                    onRemove={() => {
                      updateMappings(draft.mappings.filter((_, currentIndex) => currentIndex !== index))
                      setEditing(null)
                    }}
                  />
                ))}
              </div>
            )}
            <PanelFooter>
              <ActionButton
                spec={saveSpec}
                disabled={!dirty || invalid}
                action={save}
                runningLabel="保存中…"
                onSuccess={() => { void load(); void scan() }}
              >
                保存提供项
              </ActionButton>
              <span className="text-xs text-faint">{dirty ? '有未保存的改动' : `已配置 ${draft.mappings.length} 项`}</span>
            </PanelFooter>
          </Panel>
        )}
        {multi && (
          <Panel>
            <PanelFooter>
              <ActionButton
                spec={saveSpec}
                disabled={!dirty || invalid}
                action={save}
                runningLabel="保存中…"
                onSuccess={() => { void load(); void scan() }}
              >
                保存提供项
              </ActionButton>
              <span className="text-xs text-faint">{dirty ? '有未保存的改动' : `已配置 ${totalCount} 项`}</span>
            </PanelFooter>
          </Panel>
        )}
      </PageScroll>
    </Page>
  )
}

// 单个产品的作者声明卡：root、tags、secrets_plane 与该产品的提供项列表。
function ProductPanel(props: {
  product: ProductDraft
  editing: string | null
  setEditing: (value: string | null) => void
  candidates: Candidate[]
  onUpdate: (value: ProductDraft) => void
  onUpdateItems: (items: ProvideMapping[]) => void
}) {
  const { product } = props
  const items = product.Items
  const declared = new Set(items.map((item) => item.Source.toLowerCase()))
  const available = props.candidates.filter((item) => !declared.has(item.Source.toLowerCase()))
  return (
    <Panel>
      <div className="border-b border-line px-4 py-3">
        <div className="flex flex-wrap items-center gap-2">
          <span className="font-mono text-sm font-medium text-ink">{product.Name || '未命名产品'}</span>
          {product.IdentityOnly && <Badge tone="accent">身份型</Badge>}
          <label className="ml-auto flex cursor-pointer items-center gap-1.5 text-xs text-muted">
            <Checkbox
              aria-label={`${product.Name} 身份型产品`}
              checked={product.IdentityOnly}
              onChange={() => props.onUpdate({ ...product, IdentityOnly: !product.IdentityOnly })}
            />
            身份型（只发身份）
          </label>
        </div>
        <div className="mt-2 grid gap-3 lg:grid-cols-3">
          <Field label="作者目录（root）" hint="产品资产在仓库中的落点，相对仓根。">
            <Input
              value={product.Root}
              aria-label={`${product.Name} 作者目录`}
              onChange={(event) => props.onUpdate({ ...product, Root: normalizeRoot(event.target.value) })}
            />
          </Field>
          <Field label="密钥平面" hint="global = 机器根 ~/.dec/secrets/<p>/；local = 项目 .secrets/<p>/。">
            <Select
              value={product.SecretsPlane || ''}
              aria-label={`${product.Name} 密钥平面`}
              onChange={(event) => props.onUpdate({ ...product, SecretsPlane: event.target.value })}
            >
              <option value="">未声明（迁移期）</option>
              <option value="global">global（机器根）</option>
              <option value="local">local（项目）</option>
            </Select>
          </Field>
          <Field label="标签" hint="global = 新机器初始化时建议默认勾选。">
            <div className="flex h-9 items-center gap-2">
              <button
                type="button"
                aria-pressed={hasTag(product.Tags, PROJECT_TAG_GLOBAL)}
                className={`rounded border px-2 py-0.5 text-xs ${hasTag(product.Tags, PROJECT_TAG_GLOBAL) ? 'border-accent bg-accent/10 text-accent' : 'border-line text-faint'}`}
                onClick={() => props.onUpdate({ ...product, Tags: toggleTag(product.Tags, PROJECT_TAG_GLOBAL) })}
              >
                global
              </button>
            </div>
          </Field>
        </div>
        <p className="mt-1.5 font-mono text-[11px] text-faint">
          {(product.Root ? `${product.Root}/` : '') + 'skills/  commands/  rules/  mcp/'}
        </p>
      </div>
      <CandidatePicker
        candidates={available}
        onAdd={(chosen) => {
          props.onUpdateItems([...items, ...chosen.map(({ Origin: _o, Declared: _d, Product: _p, ...mapping }) => mapping)])
          props.setEditing(null)
        }}
      />
      {items.length === 0 ? (
        <EmptyState
          text={product.IdentityOnly ? '身份型产品：没有 Git 正文，密钥留在 Bitwarden 的同名项目下。' : '该产品还没有提供项'}
          hint={available.length > 0
            ? '上面是从产品作者目录扫描到的资产，勾选即可登记。'
            : `请先在 ${product.Root || '仓库根'} 下的作者目录（skills、commands、rules、mcp）创建资产，再重新扫描。`}
        />
      ) : (
        <div className="divide-y divide-line">
          {items.map((item, index) => (
            <ProvideRow
              key={`${index}-${item.Source}-${item.Name}`}
              item={{ ...item, Source: product.Root ? `${product.Root}/${item.Source}` : item.Source }}
              editing={props.editing === `${product.Name}-${index}`}
              onEdit={() => props.setEditing(`${product.Name}-${index}`)}
              onDone={() => props.setEditing(null)}
              onChange={(value) => props.onUpdateItems(items.map((current, currentIndex) => currentIndex === index
                ? { ...value, Source: stripRoot(value.Source, product.Root) }
                : current))}
              onRemove={() => {
                props.onUpdateItems(items.filter((_, currentIndex) => currentIndex !== index))
                props.setEditing(null)
              }}
            />
          ))}
        </div>
      )}
      <PanelFooter>
        <span className="text-xs text-faint">
          {product.IdentityOnly ? '身份型，无提供项' : `已配置 ${items.length} 项`}
        </span>
      </PanelFooter>
    </Panel>
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
        <span className="text-xs font-medium text-ink">发现 {props.candidates.length} 项可提供资产</span>
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

function ProvideRow(props: {
  item: ProvideMapping
  editing: boolean
  onEdit: () => void
  onDone: () => void
  onChange: (value: ProvideMapping) => void
  onRemove: () => void
}) {
  if (props.editing) {
    return (
      <div className="px-4 py-3">
        <ProvideEditor
          value={props.item}
          onChange={props.onChange}
          onDone={props.onDone}
        />
      </div>
    )
  }
  return (
    <div className="flex items-center gap-3 px-4 py-3">
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="font-mono text-xs font-medium text-ink">{props.item.Type}/{props.item.Name || '未命名'}</span>
          <Badge tone="quiet">{props.item.Visibility}</Badge>
          <Badge tone="quiet">{props.item.Plane}</Badge>
        </div>
        <p className="mt-1 truncate font-mono text-[11px] text-faint" title={props.item.Source}>
          {props.item.Source || '尚未填写来源'}{props.item.Target ? ` → ${props.item.Target}` : ''}
        </p>
      </div>
      <Button size="icon" variant="ghost" aria-label={`编辑 ${props.item.Name}`} onClick={props.onEdit}>
        <Pencil className="size-3.5" />
      </Button>
      <Button
        size="icon"
        variant="ghost"
        aria-label={`删除 ${props.item.Name}`}
        onClick={props.onRemove}
      >
        <Trash2 className="size-3.5 text-bad" />
      </Button>
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
