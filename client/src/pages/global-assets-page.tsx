import { RefreshCw, UploadCloud } from 'lucide-react'
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
  onSync: () => void
  onWriteback: () => void
}) {
  const syncState = useDecAction(
    actionSpec(`operation:update:${props.deviceId}:global`, '更新 Global 资产', props.deviceId, [resource.global], 'operation'),
  )
  return (
    <Page>
      <PageHeader
        title="Global 资产"
        description="装到这台设备用户环境的 bundle，不属于任何单个项目。"
        meta={props.repoURL ? <Badge tone="quiet" className="font-mono">{props.repoURL}</Badge> : undefined}
        actions={
          <Button onClick={props.onSync} disabled={syncState.blocked}>
            <RefreshCw className="size-4" />
            更新
          </Button>
        }
      />
      <PageFill>
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
          hint="官方项目按 pin 安装，私仓项目跟随 HEAD；装到用户环境（如 ~/.cursor、~/.claude）。"
        />
      </PageFill>
    </Page>
  )
}
