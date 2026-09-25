import { KeyRound } from 'lucide-react'
import { ActionFeedback } from '@/components/action-feedback'
import { Badge } from '@/components/ui/badge'
import { ActionButton } from '@/components/ui/action-button'
import { EmptyState, Notice } from '@/components/ui/feedback'
import { Panel, PanelBody, PanelHeader } from '@/components/ui/panel'
import { invokeTyped } from '@/lib/api'
import { actionSpec, resource } from '@/lib/console'
import { repoLabel } from '@/lib/utils'

// SecretFile 只有元数据。list_secrets 按设计不返回 Note 正文，
// Console 这一侧也不该有任何能显示密钥内容的分支。
type SecretFile = {
  secrets_bundle: string
  project_rel_path: string
  local_exists: boolean
  local_size_bytes?: number
  local_modified_unix?: number
  remote_exists?: boolean
  // ADR 0034 归属标注：registry 快照 join 出的只读字段；registry 不可达时全部留空。
  origin_repo?: string
  identity_only?: boolean
  orphan?: boolean
  // ADR 0035 声明平面：global = 机器根，local = 项目 .secrets/；空 = 未声明。
  declared_plane?: string
}

export type SecretsMetadata = {
  bitwarden_configured: boolean
  session_active: boolean
  remote_checked: boolean
  files: SecretFile[]
  skipped_reason?: string
}

// 顶层分组键：孤儿折叠进「疑似孤儿」区，其余按来源仓分组。
type BelongingGroup = {
  key: string
  title: string
  subtitle: string
  suspected: boolean
  files: SecretFile[]
}

export function SecretsPanel(props: {
  deviceId: string
  root: string
  plane: 'local' | 'global'
  scopeKey: string
  data: SecretsMetadata | null
  onLoaded: (value: SecretsMetadata) => void
}) {
  const listSpec = actionSpec(
    `secrets:list:${props.deviceId}:${props.scopeKey}`,
    '列出密钥清单',
    props.deviceId,
    [resource.workspace(props.root)],
    'read',
  )
  const { data } = props

  return (
    <Panel>
      <PanelHeader
        title="密钥清单"
        description="只显示落地路径与状态，不显示也不记录任何正文。清单以 Bitwarden 的 Note 列表为准；归属与密钥平面均来自 registry 快照。"
      />
      <PanelBody className="space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <ActionButton
            variant="outline"
            spec={listSpec}
            action={() => invokeTyped<SecretsMetadata>(
              'list_secrets',
              props.root,
              props.plane,
              { IncludeRemote: true },
              listSpec.key,
            )}
            runningLabel="读取中…"
            onSuccess={props.onLoaded}
          >
            <KeyRound className="size-4" />
            列出密钥
          </ActionButton>
          <span className="text-xs text-faint">需要已认证的 Bitwarden session；未认证时会请求认证。</span>
        </div>
        <ActionFeedback actionKey={listSpec.key} />
        {data && (
          <>
            <div className="flex flex-wrap items-center gap-1.5">
              <Badge tone={data.bitwarden_configured ? 'quiet' : 'warn'}>
                Bitwarden {data.bitwarden_configured ? '已配置' : '未配置'}
              </Badge>
              <Badge tone={data.session_active ? 'good' : 'warn'}>
                session {data.session_active ? '有效' : '缺失'}
              </Badge>
              <Badge tone={data.remote_checked ? 'good' : 'warn'}>
                远端 {data.remote_checked ? '已检查' : '未检查'}
              </Badge>
            </div>
            {data.skipped_reason && <Notice tone="info" text={data.skipped_reason} />}
            {data.files.length > 0 ? (
              <SecretsGroups files={data.files} />
            ) : (
              <EmptyState
                className="border-none"
                icon={<KeyRound className="size-5" />}
                text="这个平面下没有远端登记的密钥文件"
                hint="密钥清单来自 Bitwarden folder 里的 Note；先在项目里创建并推送，才会出现在这里。"
              />
            )}
          </>
        )}
      </PanelBody>
    </Panel>
  )
}

// 按「来源仓 → 产品」两级分组（ADR 0034）：同一产品仓的多个产品 folder 归到一组，
// 组头副标题显示 origin_repo；registry 查无归属的行折叠进「疑似孤儿」区。
function SecretsGroups({ files }: { files: SecretFile[] }) {
  const groups = groupByOrigin(files)
  return (
    <div className="space-y-2">
      {groups.map((group) => (
        <div
          key={group.key}
          className={`overflow-hidden rounded-lg border ${group.suspected ? 'border-warn/35' : 'border-line'}`}
        >
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1 border-b border-line bg-canvas/40 px-3 py-2">
            <span className="text-xs font-medium text-ink">{group.title}</span>
            {group.subtitle && (
              <Badge tone="quiet" className="font-mono" title={group.subtitle}>{group.subtitle}</Badge>
            )}
            {group.suspected && <Badge tone="warn">registry 查无此产品</Badge>}
            <span className="tnum ml-auto text-[11px] text-faint">{group.files.length} 条</span>
          </div>
          <div className="divide-y divide-line">
            {group.files.map((file) => (
              <SecretRow key={`${file.secrets_bundle}-${file.project_rel_path}`} file={file} />
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}

function groupByOrigin(files: SecretFile[]): BelongingGroup[] {
  const buckets = new Map<string, BelongingGroup>()
  for (const file of files) {
    const suspected = file.orphan === true
    const origin = (file.origin_repo || '').trim()
    const key = suspected ? '__orphan__' : origin || '__unassigned__'
    let group = buckets.get(key)
    if (!group) {
      group = {
        key,
        title: suspected ? '疑似孤儿' : origin ? repoLabel(origin) : '未归属',
        subtitle: suspected ? '' : origin,
        suspected,
        files: [],
      }
      buckets.set(key, group)
    }
    group.files.push(file)
  }
  const groups = [...buckets.values()]
  const order = (group: BelongingGroup) =>
    group.suspected ? 2 : group.key === '__unassigned__' ? 1 : 0
  groups.sort((a, b) => order(a) - order(b) || a.title.localeCompare(b.title))
  for (const group of groups) {
    group.files.sort((a, b) =>
      a.secrets_bundle.localeCompare(b.secrets_bundle) || a.project_rel_path.localeCompare(b.project_rel_path)
    )
  }
  return groups
}

function SecretRow({ file }: { file: SecretFile }) {
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 bg-canvas/40 px-3 py-2">
      <span className="min-w-0 flex-1 break-all font-mono text-xs text-ink">{file.project_rel_path}</span>
      <Badge tone="quiet" className="font-mono">{file.secrets_bundle}</Badge>
      {file.declared_plane && (
        <Badge
          tone="quiet"
          title={`产品声明密钥平面为 ${file.declared_plane}：${file.declared_plane === 'global' ? '机器根 ~/.dec/secrets' : '项目 .secrets/'}`}
        >
          {file.declared_plane === 'global' ? '全局平面' : '项目平面'}
        </Badge>
      )}
      {file.orphan
        ? <Badge tone="warn" title="registry 里查无此产品；删除前先确认它真的不再被使用">未归属</Badge>
        : file.identity_only
          ? <Badge tone="accent" title="registry 里有产品身份但无 Git 资产；密钥留在 Bitwarden 同名 folder">仅密钥</Badge>
          : file.origin_repo
            ? <Badge tone="good" title={file.origin_repo}>已归属</Badge>
            : <Badge tone="quiet" title="registry 不可达或产品未发布，暂无归属信息">归属未知</Badge>}
      {file.remote_exists && <Badge tone="good">远端</Badge>}
      {file.local_exists
        ? <span className="tnum text-[11px] text-faint">{describeLocal(file)}</span>
        : <Badge tone="warn">未落地</Badge>}
    </div>
  )
}

function describeLocal(file: SecretFile) {
  const parts = [formatBytes(file.local_size_bytes || 0)]
  if (file.local_modified_unix) {
    parts.push(new Date(file.local_modified_unix * 1000).toLocaleString())
  }
  return parts.join(' · ')
}

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  return `${(bytes / 1024).toFixed(1)} KB`
}
