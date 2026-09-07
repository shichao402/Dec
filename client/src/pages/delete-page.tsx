import { useState } from 'react'
import { Trash2 } from 'lucide-react'
import { ActionFeedback } from '@/components/action-feedback'
import { Page, PageFill, PageHeader, ScrollArea } from '@/components/shell/page'
import { ActionButton } from '@/components/ui/action-button'
import { Badge } from '@/components/ui/badge'
import { Checkbox } from '@/components/ui/checkbox'
import { EmptyState, Notice, WarningList } from '@/components/ui/feedback'
import { Field, Input, Select } from '@/components/ui/input'
import { Panel, PanelBody, PanelHeader } from '@/components/ui/panel'
import { invokeTyped, runOrWatchTyped } from '@/lib/api'
import { actionSpec, resource } from '@/lib/console'
import { cn } from '@/lib/utils'
import type { ManagedProject } from '@/lib/utils'

const CONFIRM_WORD = '删除'
const GLOBAL_TARGET_KEY = 'global'

type Partition = 'remote' | 'local'

// 字段名跟 app.DeleteCandidate 一致：那些结构体没有 json tag，序列化后是 PascalCase。
type DeleteCandidate = {
  Kind: string
  Label: string
  Type: string
  Name: string
  Vault: string
  SecretPath: string
  LocalRoot: string
  Plane: string
  SecretsBundle: string
  SSHKeyName: string
  DecBundleName: string
  BundleName: string
  ProjectName: string
  Members?: unknown[]
  Orphan: boolean
  GroupTitle: string
  Partition: Partition
  ScopeTag: string
  Visibility: string
  AssetPlane: string
  Unmanaged: boolean
  ReadOnly: boolean
}

type DeleteResult = {
  DecDeleted: number
  SecretsDeleted: number
  SSHKeysDeleted: number
  BundlesDeleted: number
  VersionCommit: string
  SkippedReason: string
  Remnants?: string[]
  Mode: string
}

type Target = { key: string; label: string; root: string; plane: 'local' | 'global' }

export function DeletePage(props: { deviceId: string; projects: ManagedProject[] }) {
  const projects = props.projects.filter((project) => project.Initialized)
  const targets: Target[] = [
    { key: GLOBAL_TARGET_KEY, label: 'Global（本机）', root: '', plane: 'global' },
    ...projects.map((item) => ({
      key: item.Root,
      label: item.Label || item.Name,
      root: item.Root,
      plane: 'local' as const,
    })),
  ]
  const [targetKey, setTargetKey] = useState(GLOBAL_TARGET_KEY)
  const [candidates, setCandidates] = useState<DeleteCandidate[] | null>(null)
  const [picked, setPicked] = useState<number[]>([])
  const [confirm, setConfirm] = useState('')
  const [result, setResult] = useState<DeleteResult | null>(null)
  const target = targets.find((item) => item.key === targetKey) || targets[0]

  const listSpec = actionSpec(
    `delete:list:${props.deviceId}:${target.key}`,
    '列出可删除项',
    props.deviceId,
    [resource.workspace(target.root)],
    'read',
  )
  const deleteSpec = actionSpec(
    `operation:delete:${props.deviceId}:${target.key}`,
    '删除选中项',
    props.deviceId,
    [resource.workspace(target.root)],
    'operation',
    '选中项已删除',
  )

  const reset = () => {
    setCandidates(null)
    setPicked([])
    setConfirm('')
    setResult(null)
  }

  const rows = candidates || []
  const selectable = rows
    .map((item, index) => ({ item, index }))
    .filter(({ item }) => !item.ReadOnly)
  // 远端与本机是两套事务：远端只改 Bitwarden / 私仓，本机只清这台设备的落地文件。
  // 服务端拒绝混选，这里提前锁住另一个分区，而不是等报错。
  const activePartition: Partition | null = picked.length > 0
    ? (rows[picked[0]]?.Partition || 'remote')
    : null
  const pickedItems = picked.map((index) => rows[index]).filter(Boolean)
  const ready = pickedItems.length > 0 && confirm.trim() === CONFIRM_WORD

  return (
    <Page>
      <PageHeader
        title="删除"
        description="删远端只改 Bitwarden 与私仓，删本机只清这台设备的落地文件；两者不能混选。"
      />
      <PageFill>
        <Panel className="flex max-h-full min-h-[20rem] flex-1 flex-col overflow-hidden">
          <PanelHeader
            title="可删除项"
            description="先列出库存再选。删除的密钥进 Bitwarden 回收站，删除的私仓资产留在 Git 历史里，都可以恢复。"
          />
          <div className="flex shrink-0 flex-wrap items-end gap-2 border-b border-line px-3 pb-3">
            <Field label="范围" className="min-w-56 flex-1">
              <Select
                aria-label="删除范围"
                value={target.key}
                onChange={(event) => {
                  setTargetKey(event.target.value)
                  reset()
                }}
              >
                {targets.map((item) => (
                  <option key={item.key} value={item.key}>{item.label}</option>
                ))}
              </Select>
            </Field>
            <ActionButton
              variant="outline"
              spec={listSpec}
              action={() => invokeTyped<DeleteCandidate[]>(
                'list_delete_candidates',
                target.root,
                target.plane,
                { IncludeRemote: true },
                listSpec.key,
              )}
              runningLabel="读取中…"
              onSuccess={(value) => {
                setCandidates(value || [])
                setPicked([])
                setConfirm('')
                setResult(null)
              }}
            >
              列出库存
            </ActionButton>
          </div>

          <div className="shrink-0 px-3 pt-3 empty:hidden">
            <ActionFeedback actionKey={listSpec.key} />
            <ActionFeedback actionKey={deleteSpec.key} />
          </div>

          {!candidates ? (
            <PanelBody>
              <p className="text-xs leading-relaxed text-faint">
                列出库存会读取 Bitwarden 与私仓，需要已认证的 session；这一步不改动任何东西。
              </p>
            </PanelBody>
          ) : rows.length === 0 ? (
            <PanelBody>
              <EmptyState
                className="border-none"
                icon={<Trash2 className="size-5" />}
                text="这个范围里没有可删除的东西"
                hint="库存来自私仓资产、Bitwarden 的 Note 与 SSH Key，以及本机已落地的文件。"
              />
            </PanelBody>
          ) : (
            <ScrollArea className="divide-y divide-line">
              {(['remote', 'local'] as Partition[]).map((partition) => {
                const group = selectable.filter(({ item }) => (item.Partition || 'remote') === partition)
                if (group.length === 0) return null
                const locked = activePartition !== null && activePartition !== partition
                return (
                  <div key={partition}>
                    <div className="flex items-center gap-2 border-b border-line bg-canvas/40 px-3.5 py-2">
                      <span className="text-[11px] tracking-wide text-faint uppercase">
                        {partition === 'remote' ? '远端（改 Bitwarden 与私仓）' : '本机（只清这台设备）'}
                      </span>
                      <span className="tnum text-[11px] text-faint">{group.length} 项</span>
                      {locked && <Badge tone="quiet">已锁定，先清空另一个分区</Badge>}
                    </div>
                    {group.map(({ item, index }) => (
                      <CandidateRow
                        key={`${partition}-${index}`}
                        item={item}
                        checked={picked.includes(index)}
                        disabled={locked}
                        onToggle={() => setPicked((prev) => (
                          prev.includes(index) ? prev.filter((value) => value !== index) : [...prev, index]
                        ))}
                      />
                    ))}
                  </div>
                )
              })}
            </ScrollArea>
          )}

          {rows.length > 0 && (
            <div className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-2 border-t border-line px-3.5 py-2.5">
              <span className="tnum text-xs text-muted">
                已选 <span className="font-semibold text-ink">{pickedItems.length}</span> 项
                {activePartition && ` · ${activePartition === 'remote' ? '远端' : '本机'}`}
              </span>
              <Input
                value={confirm}
                onChange={(event) => setConfirm(event.target.value)}
                placeholder={`输入“${CONFIRM_WORD}”确认`}
                className="max-w-44"
                aria-label="删除确认"
              />
              <ActionButton
                spec={deleteSpec}
                variant="destructive"
                disabled={!ready}
                action={() => runOrWatchTyped<DeleteResult>({
                  actionKey: deleteSpec.key,
                  operation: 'delete',
                  projectRoot: target.root,
                  workspacePlane: target.plane,
                  payload: {
                    Items: pickedItems,
                    Confirmed: true,
                    Mode: activePartition === 'local' ? 'local' : 'remote',
                  },
                })}
                runningLabel="删除中…"
                onSuccess={(value) => {
                  setResult(value)
                  setCandidates(null)
                  setPicked([])
                  setConfirm('')
                }}
              >
                <Trash2 className="size-4" />
                删除选中项
              </ActionButton>
              {pickedItems.length === 0 && <span className="text-xs text-faint">先勾选要删除的条目。</span>}
            </div>
          )}
        </Panel>

        {result && (
          <Panel className="mt-3 shrink-0">
            <PanelHeader
              title="删除结果"
              description={result.Mode === 'local' ? '只清了本机，远端未改动。' : '已改远端；密钥在 Bitwarden 回收站里可恢复。'}
            />
            <PanelBody className="space-y-3">
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                <ResultMetric label="Dec 资产" value={result.DecDeleted} />
                <ResultMetric label="Secrets" value={result.SecretsDeleted} />
                <ResultMetric label="SSH Key" value={result.SSHKeysDeleted} />
                <ResultMetric label="Bundle" value={result.BundlesDeleted} />
              </div>
              {result.VersionCommit && (
                <p className="break-all font-mono text-xs text-faint">提交：{result.VersionCommit}</p>
              )}
              {result.SkippedReason && <Notice tone="info" text={result.SkippedReason} />}
              {result.Remnants && result.Remnants.length > 0 && (
                <WarningList title="残留（需要手工处理）" items={result.Remnants} />
              )}
            </PanelBody>
          </Panel>
        )}
      </PageFill>
    </Page>
  )
}

function CandidateRow(props: {
  item: DeleteCandidate
  checked: boolean
  disabled: boolean
  onToggle: () => void
}) {
  const { item } = props
  return (
    <label
      className={cn(
        'flex flex-wrap items-center gap-x-3 gap-y-1 px-3.5 py-2 transition-colors',
        props.disabled ? 'cursor-not-allowed opacity-45' : 'cursor-pointer hover:bg-panel-hi',
        props.checked && 'bg-bad/6',
      )}
    >
      <Checkbox
        aria-label={item.Label}
        checked={props.checked}
        disabled={props.disabled}
        onChange={props.onToggle}
      />
      <span className="min-w-0 flex-1 break-all text-[13px] text-ink">{item.Label}</span>
      <Badge tone="quiet">{kindLabel(item.Kind)}</Badge>
      {item.GroupTitle && <Badge tone="quiet" className="font-mono">{item.GroupTitle}</Badge>}
      {item.Orphan && <Badge tone="warn">孤儿</Badge>}
      {item.Unmanaged && <Badge tone="warn">非 Dec 管理</Badge>}
    </label>
  )
}

function kindLabel(kind: string) {
  if (kind === 'dec') return 'Dec 资产'
  if (kind === 'secret') return 'Secret'
  if (kind === 'ssh') return 'SSH Key'
  if (kind === 'bundle') return 'Bundle'
  return kind
}

function ResultMetric({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-lg border border-line bg-canvas/60 px-3 py-2">
      <div className="text-[11px] text-faint">{label}</div>
      <div className="tnum mt-0.5 text-lg leading-6 font-semibold text-ink">{value}</div>
    </div>
  )
}
