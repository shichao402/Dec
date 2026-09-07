import { KeyRound } from 'lucide-react'
import { ActionFeedback } from '@/components/action-feedback'
import { Badge } from '@/components/ui/badge'
import { ActionButton } from '@/components/ui/action-button'
import { EmptyState, Notice } from '@/components/ui/feedback'
import { Panel, PanelBody, PanelHeader } from '@/components/ui/panel'
import { invokeTyped } from '@/lib/api'
import { actionSpec, resource } from '@/lib/console'

// SecretFile 只有元数据。list_secrets 按设计不返回 Note 正文，
// Console 这一侧也不该有任何能显示密钥内容的分支。
type SecretFile = {
  secrets_bundle: string
  project_rel_path: string
  local_exists: boolean
  local_size_bytes?: number
  local_modified_unix?: number
  remote_exists?: boolean
}

export type SecretsMetadata = {
  bitwarden_configured: boolean
  session_active: boolean
  remote_checked: boolean
  files: SecretFile[]
  skipped_reason?: string
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
        description="只显示落地路径与状态，不显示也不记录任何正文。清单以 Bitwarden 的 Note 列表为准。"
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
              <div className="divide-y divide-line overflow-hidden rounded-lg border border-line">
                {data.files.map((file) => (
                  <SecretRow key={`${file.secrets_bundle}-${file.project_rel_path}`} file={file} />
                ))}
              </div>
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

function SecretRow({ file }: { file: SecretFile }) {
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 bg-canvas/40 px-3 py-2">
      <span className="min-w-0 flex-1 break-all font-mono text-xs text-ink">{file.project_rel_path}</span>
      <Badge tone="quiet" className="font-mono">{file.secrets_bundle}</Badge>
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
