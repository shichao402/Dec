import { useState } from 'react'
import { GitCompare, UploadCloud } from 'lucide-react'
import { ActionFeedback } from '@/components/action-feedback'
import { Page, PageHeader, PageScroll } from '@/components/shell/page'
import { ActionButton } from '@/components/ui/action-button'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Notice } from '@/components/ui/feedback'
import { Panel, PanelBody, PanelHeader } from '@/components/ui/panel'
import { runOrWatchTyped } from '@/lib/api'
import { actionSpec, resource } from '@/lib/console'
import { cn } from '@/lib/utils'
import { SecretsPanel, type SecretsMetadata } from '@/pages/secrets-panel'

type PushChange = { Op: string; Path: string; Quadrant?: string }
type PushPreview = {
  SecretsTargetCount?: number
  DecCandidateCount?: number
  DecSkippedReason?: string
  BitwardenConfigured?: boolean
  Changes?: PushChange[]
}
type PushResult = {
  DecPushedCount?: number
  DecSkippedReason?: string
  SecretsCreatedCount?: number
  SecretsUpdatedCount?: number
  SecretsSkippedReason?: string
}
type SecretsPushResult = {
  CreatedCount?: number
  UpdatedCount?: number
  SkippedReason?: string
}

const previewGrid = 'grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 px-3'
  + ' xl:grid-cols-[minmax(0,1fr)_6rem_10rem]'

// 写回与密钥清单都是偶发操作：写回要先预览再确认，密钥清单只读元数据。
// 它们留在 Global / 项目主区时，天天要看的订阅表被挤到屏幕下半截，所以收进下级页。
export function WritebackPage(props: {
  deviceId: string
  root: string
  plane: 'local' | 'global'
  label: string
  onBack: () => void
}) {
  const [preview, setPreview] = useState<PushPreview | null>(null)
  const [result, setResult] = useState<PushResult | null>(null)
  const [secretsResult, setSecretsResult] = useState<SecretsPushResult | null>(null)
  const [secrets, setSecrets] = useState<SecretsMetadata | null>(null)
  const scope = props.root || 'global'
  const workspaceResource = resource.workspace(props.root)
  const previewSpec = actionSpec(`write:preview:${props.deviceId}:${scope}`, '预览写回内容', props.deviceId, [workspaceResource], 'read')
  const pushSpec = actionSpec(`operation:push-personal:${props.deviceId}:${scope}`, `写回 ${props.label} 个人资产`, props.deviceId, [workspaceResource], 'operation', '个人资产已写回')
  const secretsPushSpec = actionSpec(`operation:push-secrets:${props.deviceId}:${scope}`, `提交 ${props.label} 密钥`, props.deviceId, [workspaceResource], 'operation', '密钥已提交')
  const run = <T,>(actionKey: string, operation: string) => runOrWatchTyped<T>({
    actionKey,
    operation,
    projectRoot: props.root,
    workspacePlane: props.plane,
  })

  return (
    <Page>
      <PageHeader
        title="写回与密钥"
        description="把这台设备上的改动写回私仓与 Bitwarden；密钥只显示落地路径与状态。"
        meta={<Badge tone="quiet" className="font-mono" title={props.root}>{props.label}</Badge>}
        actions={<Button variant="outline" onClick={props.onBack}>返回</Button>}
      />
      <PageScroll className="space-y-4">
        <Panel>
          <PanelHeader title="写回" description="个人资产写入私仓，密钥写入 Bitwarden。" />
          <PanelBody className="space-y-3">
            <div className="flex flex-wrap gap-2">
              <ActionButton
                spec={previewSpec}
                variant="outline"
                action={() => run<PushPreview>(previewSpec.key, 'preview_push')}
                onSuccess={(value) => { setPreview(value); setResult(null) }}
                runningLabel="预览中…"
              >
                <GitCompare className="size-4" />预览写回
              </ActionButton>
              {preview && (
                <>
                  <ActionButton
                    spec={pushSpec}
                    action={() => run<PushResult>(pushSpec.key, 'push_personal')}
                    onSuccess={(value) => setResult(value)}
                    runningLabel="写回中…"
                  >
                    <UploadCloud className="size-4" />写回个人资产
                  </ActionButton>
                  <ActionButton
                    spec={secretsPushSpec}
                    variant="outline"
                    action={() => run<SecretsPushResult>(secretsPushSpec.key, 'push_secrets')}
                    onSuccess={setSecretsResult}
                    runningLabel="提交中…"
                  >
                    提交密钥
                  </ActionButton>
                </>
              )}
            </div>
            <ActionFeedback actionKey={previewSpec.key} />
            <ActionFeedback actionKey={pushSpec.key} />
            <ActionFeedback actionKey={secretsPushSpec.key} />
            {preview?.DecSkippedReason && <Notice tone="warn" text={preview.DecSkippedReason} />}
            {preview && (
              <div className="rounded-lg border border-line">
                <div className={cn(previewGrid, 'border-b border-line bg-canvas/50 py-2 text-[11px] text-faint')}>
                  <span>路径</span><span>操作</span><span className="hidden xl:block">象限</span>
                </div>
                {(preview.Changes || []).map((item) => (
                  <div key={item.Path} className={cn(previewGrid, 'border-b border-line/70 py-2.5 text-xs last:border-b-0')}>
                    <span className="truncate font-mono text-muted" title={item.Path}>{item.Path}</span>
                    <Badge tone={item.Op === '删除' ? 'bad' : item.Op === '新建' ? 'good' : 'warn'}>{item.Op}</Badge>
                    <span className="hidden text-[11px] text-faint xl:block">{item.Quadrant || '—'}</span>
                  </div>
                ))}
                {(preview.Changes || []).length === 0 && <p className="px-3 py-4 text-xs text-faint">没有个人资产改动。</p>}
              </div>
            )}
            {result && (
              <Notice
                tone="info"
                text={`个人资产已写回 ${result.DecPushedCount || 0} 项`}
              />
            )}
            {result?.DecSkippedReason && <Notice tone="warn" text={result.DecSkippedReason} />}
            {result?.SecretsSkippedReason && <Notice tone="warn" text={result.SecretsSkippedReason} />}
            {secretsResult && (
              <Notice tone="info" text={`密钥新增 ${secretsResult.CreatedCount || 0}、更新 ${secretsResult.UpdatedCount || 0}`} />
            )}
            {secretsResult?.SkippedReason && <Notice tone="warn" text={secretsResult.SkippedReason} />}
          </PanelBody>
        </Panel>
        <SecretsPanel
          deviceId={props.deviceId}
          root={props.root}
          plane={props.plane}
          scopeKey={scope}
          data={secrets}
          onLoaded={setSecrets}
        />
      </PageScroll>
    </Page>
  )
}
