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
import { actionSpec, PROJECT_TAG_GLOBAL, hasTag, resource } from '@/lib/console'
import { cn, repoLabel, versionStatusLine } from '@/lib/utils'
import type { AssetOption, AssetSelection } from '@/lib/utils'

type Filter = 'all' | 'enabled' | 'official' | 'vault' | 'tagged'

const PIN_LATEST = 'latest'
const PIN_VAULT = 'vault'

type Source = 'official' | 'vault'

// 草稿里的 pin 优先于服务端下发的 Source：还没保存时，徽标与版本列必须立刻跟上。
function sourceOf(item: AssetOption, pin?: string): Source {
  if (pin) return pin === PIN_VAULT ? 'vault' : 'official'
  return item.Source === 'official' ? 'official' : 'vault'
}

// 已发布项目跟随最新 tag。注册表里没有的个人项目只能跟私仓 HEAD（ADR 0031）。
function defaultPin(item: AssetOption) {
  if (item.Pin) return item.Pin
  if (item.Source === 'official') return PIN_LATEST
  return PIN_VAULT
}

export function SubscriptionPanel(props: {
  deviceId: string
  root: string
  plane: 'local' | 'global'
  hint: string
}) {
  const [data, setData] = useState<AssetSelection | null>(null)
  const [pins, setPins] = useState<Record<string, string>>({})
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<Filter>('all')
  const actions = useActionRegistry()
  const runAction = actions.run
  const workspaceResource = resource.workspace(props.root)
  const scope = props.root || 'global'
  const loadSpec = useMemo(
    () => actionSpec(`requires:load:${props.deviceId}:${scope}`, '加载可订阅项目', props.deviceId, [workspaceResource], 'read'),
    [props.deviceId, scope, workspaceResource],
  )
  const saveSpec = actionSpec(`requires:save:${props.deviceId}:${scope}`, '保存订阅', props.deviceId, [workspaceResource], 'write', '订阅已保存')
  const loadState = useDecAction<AssetSelection>(loadSpec)
  const saveState = useDecAction<{ Rejected?: string[] }>(saveSpec)

  const applySelection = useCallback((result: AssetSelection) => {
    setData(result)
    setPins(subscribedPins(result.Bundles))
  }, [])

  const load = useCallback(async () => {
    const outcome = await runAction(
      loadSpec,
      () => invokeTyped<AssetSelection>('list_subscription_candidates', props.root, props.plane, {}, loadSpec.key),
      { force: true },
    )
    if (outcome.ok) applySelection(outcome.value)
  }, [applySelection, loadSpec, props.plane, props.root, runAction])

  // 远端工作区变化后需要重新同步服务端订阅状态。
  // oxlint-disable-next-line react/set-state-in-effect
  useEffect(() => { void load() }, [load])

  const bundles = data?.Bundles || []
  const saved = subscribedPins(bundles)
  const added = Object.keys(pins).filter((name) => !saved[name])
  const removed = Object.keys(saved).filter((name) => !pins[name])
  const repinned = Object.keys(pins).filter((name) => saved[name] && saved[name] !== pins[name])
  const dirty = added.length + removed.length + repinned.length > 0

  const togglePin = (item: AssetOption) => {
    setPins((current) => {
      const next = { ...current }
      if (next[item.Name]) delete next[item.Name]
      else next[item.Name] = defaultPin(item)
      return next
    })
  }

  const setPin = (name: string, pin: string) => {
    setPins((current) => ({ ...current, [name]: pin }))
  }

  const visible = bundles.filter((item) => {
    const source = sourceOf(item, pins[item.Name])
    if (filter === 'enabled' && !pins[item.Name]) return false
    if (filter === 'official' && source !== 'official') return false
    if (filter === 'vault' && source !== 'vault') return false
    if (filter === 'tagged' && !hasTag(item.Tags, PROJECT_TAG_GLOBAL)) return false
    if (!query.trim()) return true
    const haystack = `${item.Name} ${item.Description} ${item.OriginRepo || ''} ${(item.Tags || []).join(' ')} ${(item.Members || []).map((m) => `${m.Type}/${m.Name}`).join(' ')}`
    return haystack.toLowerCase().includes(query.trim().toLowerCase())
  }).sort((a, b) => {
    const ag = hasTag(a.Tags, PROJECT_TAG_GLOBAL) ? 0 : 1
    const bg = hasTag(b.Tags, PROJECT_TAG_GLOBAL) ? 0 : 1
    if (ag !== bg) return ag - bg
    return a.Name.localeCompare(b.Name)
  })
  const selectable = visible.filter((item) => !item.OtherPlane && !(props.plane === 'local' && item.Home))
  const rejected = saveState.record?.result?.Rejected || []
  const updatable = bundles.filter((item) => pins[item.Name] && item.UpdateAvailable).length

  // 表格页与卡片页不同：底部操作条要贴在视口下沿，行的位置也不该随条数跳动，所以这里撑满高度。
  return (
    <Panel className="flex max-h-full min-h-[20rem] flex-1 flex-col overflow-hidden">
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-line p-3">
        <div className="relative min-w-[14rem] flex-1">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-faint" />
          <Input className="pl-8" placeholder="搜索项目、描述或成员" value={query} onChange={(e) => setQuery(e.target.value)} />
        </div>
        <SegmentedFilter value={filter} onChange={setFilter} />
        <div className="ml-auto flex items-center gap-1">
          <Button
            size="sm"
            variant="ghost"
            disabled={selectable.length === 0}
            onClick={() => setPins((current) => {
              const next = { ...current }
              for (const item of selectable) if (!next[item.Name]) next[item.Name] = defaultPin(item)
              return next
            })}
          >
            全选当前
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={selectable.length === 0}
            onClick={() => setPins((current) => {
              const next = { ...current }
              for (const item of selectable) delete next[item.Name]
              return next
            })}
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
        {rejected.length > 0 && <Notice text={`已保存，但未订阅：${rejected.join('、')}`} />}
        {updatable > 0 && <Notice tone="warn" text={`${updatable} 个已订阅项目有新版本，到「更新」页预览后安装。`} />}
      </div>

      {!data ? (
        <Loading />
      ) : (
        <>
          <ScrollArea className="divide-y divide-line">
            {visible.map((item) => (
              <AssetRow
                key={item.Name}
                item={item}
                // 家项目不是订阅（它从工作树创作），但资产无条件安装：勾上且不可改，别让它看起来像没装。
                checked={(props.plane === 'local' && item.Home) || Boolean(pins[item.Name])}
                pin={pins[item.Name]}
                changed={added.includes(item.Name) || removed.includes(item.Name) || repinned.includes(item.Name)}
                locked={props.plane === 'local' && item.Home}
                onToggle={() => togglePin(item)}
                onPin={(pin) => setPin(item.Name, pin)}
              />
            ))}
            {visible.length === 0 && (
              <EmptyState
                className="m-4 border-none"
                icon={<Boxes className="size-5" />}
                text={query || filter !== 'all' ? '没有匹配的项目' : '这个范围里还没有可订阅的项目'}
                hint={query || filter !== 'all'
                  ? filter === 'tagged'
                    ? '当前没有带 global 标签的项目。'
                    : '换个关键词，或把筛选切回「全部」。'
                  : '官方项目来自注册表，个人项目来自已连接的私仓。'}
              />
            )}
          </ScrollArea>
        </>
      )}

      <div className="flex shrink-0 flex-wrap items-center gap-x-4 gap-y-2 border-t border-line px-3.5 py-2.5">
        <span className="tnum text-xs text-muted">
          已订阅 <span className="font-semibold text-ink">{Object.keys(pins).length}</span> / {bundles.length}
          {visible.length !== bundles.length && ` · 当前视图 ${visible.length}`}
        </span>
        {dirty ? (
          <span className="tnum flex min-w-0 items-center gap-2 text-xs">
            {added.length > 0 && <span className="text-good">+{added.length}</span>}
            {removed.length > 0 && <span className="text-bad">−{removed.length}</span>}
            {repinned.length > 0 && <span className="text-warn">~{repinned.length}</span>}
            <span className="min-w-0 truncate text-faint">
              {[...added, ...removed, ...repinned].slice(0, 3).join('、')}
              {added.length + removed.length + repinned.length > 3 && ` 等 ${added.length + removed.length + repinned.length} 项待保存`}
            </span>
          </span>
        ) : (
          <span className="text-xs text-faint">与设备上的记录一致</span>
        )}
        <span className="hidden min-w-0 flex-1 truncate text-xs text-faint 2xl:block" title={props.hint}>{props.hint}</span>
        <div className="ml-auto flex items-center gap-2">
          <ActionButton
            spec={saveSpec}
            disabled={!dirty}
            action={() => invokeTyped('set_requires', props.root, props.plane, { Requires: pins }, saveSpec.key)}
            runningLabel="保存中…"
            onSuccess={load}
          >
            保存订阅
          </ActionButton>
        </div>
      </div>
    </Panel>
  )
}

// subscribedPins 读出服务端已保存的订阅表：只认 Pin。
// Enabled 还包含「被别人的 depends_on 带进来」的项目，拿它兜底会把没订阅的行显示成已订阅。
function subscribedPins(items: AssetOption[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const item of items) {
    if (!item.Pin || item.Home) continue
    out[item.Name] = item.Pin
  }
  return out
}

export function AssetRow({
  item,
  checked,
  pin,
  changed,
  compact,
  locked,
  onToggle,
  onPin,
}: {
  item: AssetOption
  checked: boolean
  pin?: string
  changed?: boolean
  compact?: boolean
  locked?: boolean
  onToggle: () => void
  onPin?: (pin: string) => void
}) {
  const members = item.Members || []
  const shownMembers = members.slice(0, compact ? 3 : 6)
  const hiddenMembers = members.length - shownMembers.length
  const origin = repoLabel(item.OriginRepo)
  const subtitle = [origin, (item.Description || '').trim()].filter(Boolean).join(' · ')
  const tags = item.Tags || []
  const source = sourceOf(item, pin)
  const version = source === 'official' ? versionStatusLine(item.Installed, item.Available) : ''
  return (
    <label
      className={cn(
        'grid grid-cols-[1.25rem_minmax(0,1fr)] items-start gap-x-3 px-3.5 transition-colors',
        compact ? 'py-2.5' : 'py-3',
        item.OtherPlane || locked ? 'cursor-not-allowed opacity-60' : 'cursor-pointer hover:bg-panel-hi',
        changed && 'bg-accent/6',
      )}
    >
      <Checkbox className="mt-0.5" aria-label={item.Name} checked={checked} disabled={item.OtherPlane || locked} onChange={onToggle} />
      <div className="flex min-w-0 items-start justify-between gap-6">
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <span className="truncate text-sm font-medium text-ink" title={item.Name}>{item.Name}</span>
            {item.Home && <Badge tone="accent">{locked ? 'home · 必选' : 'home'}</Badge>}
            {item.Enabled && !item.Pin && !item.Home && (
              <Badge tone="quiet" title="随其他已订阅项目的 depends_on 一并安装，不需要自己订阅">依赖引入</Badge>
            )}
            {item.UpdateAvailable && <Badge tone="warn">有更新</Badge>}
            {item.SecretsOnly && members.every((member) => member.Type === 'secret') && <Badge tone="quiet">secrets</Badge>}
            {item.OtherPlane && <Badge tone="warn">另一平面</Badge>}
            {item.RemoteMissing && <Badge tone="bad">私仓缺失</Badge>}
            {item.RemoteUnverified && <Badge tone="warn">未校验</Badge>}
            {tags.map((tag) => (
              <Badge key={tag} tone="quiet" className="font-mono">{tag}</Badge>
            ))}
          </div>
          {subtitle && (
            <p className="mt-0.5 truncate text-xs text-faint" title={item.OriginRepo || item.Description}>
              {subtitle}
            </p>
          )}
          {shownMembers.length > 0 && (
            <div className="mt-2 flex flex-wrap gap-1">
              {shownMembers.map((member) => (
                <Badge key={`${member.Type}-${member.Name}`} tone="quiet" className="font-mono">
                  {member.Type}/{member.Name}
                </Badge>
              ))}
              {hiddenMembers > 0 && <span className="tnum self-center text-[11px] text-faint">另外 {hiddenMembers} 项</span>}
            </div>
          )}
        </div>
        <div className="flex shrink-0 flex-col items-end gap-1.5">
          <div className="flex items-center gap-2">
            <span className={cn('text-xs font-medium', source === 'official' ? 'text-accent-hi' : 'text-faint')}>
              {source === 'official' ? '官方' : '私仓'}
            </span>
            {version && version !== '—' && (
              <span className="tnum font-mono text-[11px] text-muted">{version}</span>
            )}
          </div>
          {onPin && (
            <div className="flex flex-wrap justify-end gap-1.5">
              {source === 'official' && (
                <PinControl item={item} checked={checked} pin={pin} onPin={onPin} />
              )}
            </div>
          )}
        </div>
      </div>
    </label>
  )
}

// PinControl 只暴露两种 pin：跟随最新，或钉死当前可用版本。
// 更细的历史版本回退是「更新」页的事，订阅这里不该变成版本选择器。
function PinControl({
  item,
  checked,
  pin,
  onPin,
}: {
  item: AssetOption
  checked: boolean
  pin?: string
  onPin?: (pin: string) => void
}) {
  const available = item.Available || ''
  const pinned = Boolean(pin && pin !== PIN_LATEST && pin !== PIN_VAULT)
  if (!checked || !onPin || !available) return null
  return (
    <button
      type="button"
      title={pinned ? '已固定在这个版本，点此改回跟随官方最新版' : `现在跟随官方最新版。点此固定在 ${available}，之后不再自动升级`}
      onClick={(event) => {
        event.preventDefault()
        event.stopPropagation()
        onPin(pinned ? PIN_LATEST : available)
      }}
      className={cn(
        'rounded-md border px-1.5 py-0.5 font-mono text-[11px] leading-4',
        pinned ? 'border-accent/40 bg-accent/12 text-accent-hi' : 'border-dashed border-line text-faint hover:border-line-hi hover:text-ink',
      )}
    >
      {pinned ? `固定在 ${pin}` : '跟随最新'}
    </button>
  )
}

function SegmentedFilter({ value, onChange }: { value: Filter; onChange: (next: Filter) => void }) {
  const options: [Filter, string][] = [
    ['all', '全部'],
    ['enabled', '已订阅'],
    ['official', '官方'],
    ['vault', '私仓'],
    ['tagged', 'global'],
  ]
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
