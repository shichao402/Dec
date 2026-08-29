import { useCallback, useEffect, useMemo, useState } from 'react'
import { Boxes, RefreshCw, Search } from 'lucide-react'
import { ActionFeedback } from '@/components/action-feedback'
import { ScrollArea } from '@/components/shell/page'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { EmptyState, Loading, Notice } from '@/components/ui/feedback'
import { Input } from '@/components/ui/input'
import { Panel } from '@/components/ui/panel'
import { ActionButton } from '@/components/ui/action-button'
import { useActionRegistry, useDecAction } from '@/lib/action-context'
import { invokeTyped } from '@/lib/api'
import { actionSpec, resource, toggle } from '@/lib/console'
import { cn } from '@/lib/utils'
import type { AssetOption, AssetSelection } from '@/lib/utils'

type Filter = 'all' | 'enabled' | 'required'

// 行按列对齐：名称、说明、成员各占固定语义列，宽屏不会只在左侧堆一小块。
const row = 'grid grid-cols-[auto_minmax(9rem,16rem)_minmax(0,1fr)_auto] items-center gap-x-3'
const compactRow = 'grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-x-3'

export function AssetsPanel(props: {
  deviceId: string
  root: string
  plane: 'local' | 'global'
  hint: string
}) {
  const [data, setData] = useState<AssetSelection | null>(null)
  const [selected, setSelected] = useState<string[]>([])
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<Filter>('all')
  const actions = useActionRegistry()
  const runAction = actions.run
  const workspaceResource = resource.workspace(props.root)
  const scope = props.root || 'global'
  const loadSpec = useMemo(
    () => actionSpec(`assets:load:${props.deviceId}:${scope}`, '加载资产选择', props.deviceId, [workspaceResource], 'read'),
    [props.deviceId, scope, workspaceResource],
  )
  const saveSpec = actionSpec(`assets:save:${props.deviceId}:${scope}`, '保存资产选择', props.deviceId, [workspaceResource], 'write', '资产选择已保存')
  const loadState = useDecAction<AssetSelection>(loadSpec)
  const saveState = useDecAction<{ RejectedBundles?: string[]; RejectedProjects?: string[] }>(saveSpec)

  const applySelection = useCallback((result: AssetSelection) => {
    setData(result)
    setSelected(result.Bundles.filter((item) => item.Enabled).map((item) => item.Name))
  }, [])

  const load = useCallback(async () => {
    const outcome = await runAction(
      loadSpec,
      () => invokeTyped<AssetSelection>('load_asset_selection', props.root, props.plane, {}, loadSpec.key),
      { force: true },
    )
    if (outcome.ok) applySelection(outcome.value)
  }, [applySelection, loadSpec, props.plane, props.root, runAction])

  // 远端工作区变化后需要重新同步服务端选择状态。
  // oxlint-disable-next-line react/set-state-in-effect
  useEffect(() => { void load() }, [load])

  const bundles = data?.Bundles || []
  const enabledNames = bundles.filter((item) => item.Enabled).map((item) => item.Name)
  const added = selected.filter((name) => !enabledNames.includes(name))
  const removed = enabledNames.filter((name) => !selected.includes(name))
  const dirty = added.length + removed.length > 0

  const visible = bundles.filter((item) => {
    if (filter === 'enabled' && !selected.includes(item.Name)) return false
    if (filter === 'required' && !(item.Home || item.Required)) return false
    if (!query.trim()) return true
    const haystack = `${item.Name} ${item.Description} ${(item.Members || []).map((m) => `${m.Type}/${m.Name}`).join(' ')}`
    return haystack.toLowerCase().includes(query.trim().toLowerCase())
  })
  const selectable = visible.filter((item) => !item.OtherPlane)
  const rejected = saveState.record?.result
    ? [...(saveState.record.result.RejectedProjects || []), ...(saveState.record.result.RejectedBundles || [])]
    : []

  // 表格页与卡片页不同：底部操作条要贴在视口下沿，行的位置也不该随条数跳动，所以这里撑满高度。
  return (
    <Panel className="flex max-h-full min-h-[20rem] flex-1 flex-col overflow-hidden">
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-line p-3">
        <div className="relative min-w-[14rem] flex-1">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-faint" />
          <Input className="pl-8" placeholder="搜索资产、描述或成员" value={query} onChange={(e) => setQuery(e.target.value)} />
        </div>
        <SegmentedFilter value={filter} onChange={setFilter} />
        <div className="ml-auto flex items-center gap-1">
          <Button
            size="sm"
            variant="ghost"
            disabled={selectable.length === 0}
            onClick={() => setSelected([...new Set([...selected, ...selectable.map((item) => item.Name)])])}
          >
            全选当前
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={selectable.length === 0}
            onClick={() => setSelected(selected.filter((name) => !selectable.some((item) => item.Name === name)))}
          >
            清除当前
          </Button>
          <Button size="icon" variant="ghost" aria-label="重新加载" onClick={() => void load()} disabled={loadState.running}>
            <RefreshCw className={loadState.running ? 'size-3.5 animate-spin' : 'size-3.5'} />
          </Button>
        </div>
      </div>

      <div className="shrink-0 px-3 pt-3 empty:hidden">
        <ActionFeedback actionKey={loadSpec.key} />
        <ActionFeedback actionKey={saveSpec.key} />
        {rejected.length > 0 && <Notice text={`已保存，但设备未接受：${rejected.join('、')}`} />}
      </div>

      {!data ? (
        <Loading />
      ) : (
        <>
          <div className={cn(row, 'shrink-0 border-b border-line bg-canvas/40 px-3.5 py-2 text-[11px] tracking-wide text-faint uppercase')}>
            <span className="w-4" />
            <span>资产</span>
            <span className="hidden lg:block">说明</span>
            <span className="hidden text-right xl:block">成员</span>
          </div>
          <ScrollArea className="divide-y divide-line">
            {visible.map((item) => (
              <AssetRow
                key={`${item.Vault}-${item.Name}`}
                item={item}
                checked={selected.includes(item.Name)}
                changed={added.includes(item.Name) || removed.includes(item.Name)}
                onToggle={() => setSelected(toggle(selected, item.Name))}
              />
            ))}
            {visible.length === 0 && (
              <EmptyState
                className="m-4 border-none"
                icon={<Boxes className="size-5" />}
                text={query || filter !== 'all' ? '没有匹配的资产' : '这个范围里还没有可选资产'}
                hint={query || filter !== 'all'
                  ? '换个关键词，或把筛选切回「全部」。'
                  : '先确认私仓已连接，并且家项目绑定的是私仓里已存在的项目。'}
              />
            )}
          </ScrollArea>
        </>
      )}

      <div className="flex shrink-0 flex-wrap items-center gap-x-4 gap-y-2 border-t border-line px-3.5 py-2.5">
        <span className="tnum text-xs text-muted">
          已选 <span className="font-semibold text-ink">{selected.length}</span> / {bundles.length}
          {visible.length !== bundles.length && ` · 当前视图 ${visible.length}`}
        </span>
        {dirty ? (
          <span className="tnum flex min-w-0 items-center gap-2 text-xs">
            {added.length > 0 && <span className="text-good">+{added.length}</span>}
            {removed.length > 0 && <span className="text-bad">−{removed.length}</span>}
            <span className="min-w-0 truncate text-faint">
              {[...added, ...removed].slice(0, 3).join('、')}
              {added.length + removed.length > 3 && ` 等 ${added.length + removed.length} 项待保存`}
            </span>
          </span>
        ) : (
          <span className="text-xs text-faint">与设备上的记录一致</span>
        )}
        <span className="hidden min-w-0 flex-1 truncate text-xs text-faint xl:block">{props.hint}</span>
        <div className="ml-auto flex items-center gap-2">
          <ActionButton
            spec={saveSpec}
            disabled={!dirty}
            action={() => invokeTyped('save_enabled_bundles', props.root, props.plane, { EnabledProjects: selected }, saveSpec.key)}
            runningLabel="保存中…"
            onSuccess={load}
          >
            保存选择
          </ActionButton>
        </div>
      </div>
    </Panel>
  )
}

export function AssetRow({
  item,
  checked,
  changed,
  compact,
  onToggle,
}: {
  item: AssetOption
  checked: boolean
  changed?: boolean
  compact?: boolean
  onToggle: () => void
}) {
  const members = item.Members || []
  const memberTypes = [...new Set(members.map((member) => member.Type))]
  return (
    <label
      className={cn(
        compact ? compactRow : row,
        'px-3.5 py-2 transition-colors',
        item.OtherPlane ? 'cursor-not-allowed opacity-45' : 'cursor-pointer hover:bg-panel-hi',
        changed && 'bg-accent/6',
      )}
    >
      <Checkbox aria-label={item.Name} checked={checked} disabled={item.OtherPlane} onChange={onToggle} />
      <div className="flex min-w-0 flex-col">
        <span className="flex min-w-0 items-center gap-1.5">
          <span className="truncate text-[13px] font-medium text-ink" title={item.Name}>{item.Name}</span>
          {item.Home && <Badge tone="accent">home</Badge>}
          {item.Required && <Badge>requires</Badge>}
          {item.SecretsOnly && <Badge tone="quiet">secrets</Badge>}
          {item.OtherPlane && <Badge tone="warn">另一平面</Badge>}
          {item.RemoteMissing && <Badge tone="bad">私仓缺失</Badge>}
          {item.RemoteUnverified && <Badge tone="warn">未校验</Badge>}
        </span>
        <span className={cn('line-clamp-2 text-[11px] leading-4 text-faint', !compact && 'lg:hidden')}>
          {item.Description || (memberTypes.length ? memberTypes.join(' · ') : `${members.length} 个成员`)}
        </span>
      </div>
      {compact ? (
        <span className="tnum text-[11px] text-faint">{members.length} 项</span>
      ) : (
        <>
          {/* 说明列允许两行：一刀切成单行时长描述只能读到不足一半，等于没写。 */}
          <span className="hidden min-w-0 text-xs leading-4 text-faint lg:line-clamp-2">
            {item.Description || (memberTypes.length ? memberTypes.join(' · ') : '无描述')}
          </span>
          <span className="hidden items-center justify-end gap-1 xl:flex">
            {members.slice(0, 2).map((member) => (
              <Badge key={`${member.Type}-${member.Name}`} tone="quiet" className="font-mono">
                {member.Type}/{member.Name}
              </Badge>
            ))}
            <span className="tnum w-12 text-right text-[11px] text-faint">{members.length} 项</span>
          </span>
        </>
      )}
    </label>
  )
}

function SegmentedFilter({ value, onChange }: { value: Filter; onChange: (next: Filter) => void }) {
  const options: [Filter, string][] = [['all', '全部'], ['enabled', '已选'], ['required', '必需']]
  return (
    <div className="flex rounded-lg border border-line p-0.5">
      {options.map(([id, label]) => (
        <button
          key={id}
          onClick={() => onChange(id)}
          className={cn(
            'h-7 rounded-md px-2.5 text-xs transition-colors',
            value === id ? 'bg-panel-hi font-medium text-ink' : 'text-faint hover:text-ink',
          )}
        >
          {label}
        </button>
      ))}
    </div>
  )
}
