import { RefreshCw, UploadCloud } from 'lucide-react'
import { ActionFeedback } from '@/components/action-feedback'
import { Page, PageFill, PageHeader } from '@/components/shell/page'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { NavCard, NavCardGrid } from '@/components/ui/nav-card'
import { useDecAction } from '@/lib/action-context'
import { actionSpec, resource } from '@/lib/console'
import { SubscriptionPanel } from '@/pages/subscription-panel'

export function GlobalAssetsPage(props: {
  deviceId: string
  repoURL: string
  onPull: () => void
  onSync: () => void
  onWriteback: () => void
}) {
  const pullSpec = actionSpec(
    `operation:pull:${props.deviceId}:global`,
    '正在拉取 Global 资产',
    props.deviceId,
    [resource.global],
    'operation',
    'Global 资产拉取完成',
  )
  const pullState = useDecAction(pullSpec)
  return (
    <Page>
      <PageHeader
        title="Global 资产"
        description="装到这台设备用户环境的 bundle，不属于任何单个项目。"
        meta={props.repoURL ? <Badge tone="quiet" className="font-mono">{props.repoURL}</Badge> : undefined}
        actions={
          <>
            <Button variant="outline" onClick={props.onSync}>
              官方更新
            </Button>
            <Button onClick={props.onPull} disabled={pullState.blocked}>
              <RefreshCw className="size-4" />
              {pullState.running ? '拉取中…' : '拉取'}
            </Button>
          </>
        }
      />
      <PageFill>
        <ActionFeedback actionKey={pullSpec.key} />
        {/* 写回与密钥清单是偶发操作，和项目页用同一种入口卡，主区留给订阅。 */}
        <NavCardGrid className="mb-4">
          <NavCard
            icon={UploadCloud}
            title="写回与密钥"
            description="个人资产写入私仓，密钥写入 Bitwarden"
            onClick={props.onWriteback}
          />
        </NavCardGrid>
        <SubscriptionPanel
          deviceId={props.deviceId}
          root=""
          plane="global"
          hint="保存订阅后点「拉取」落地到用户环境（如 ~/.cursor、~/.claude）；官方新版本用「官方更新」。"
        />
      </PageFill>
    </Page>
  )
}
