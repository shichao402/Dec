import { RefreshCw } from 'lucide-react'
import { Page, PageFill, PageHeader } from '@/components/shell/page'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useDecAction } from '@/lib/action-context'
import { actionSpec, resource } from '@/lib/console'
import { AssetsPanel } from '@/pages/assets-panel'

export function GlobalAssetsPage(props: { deviceId: string; repoURL: string; onPull: () => void }) {
  const pullState = useDecAction(
    actionSpec(`operation:pull:${props.deviceId}:global`, '拉取资产', props.deviceId, [resource.global], 'operation'),
  )
  return (
    <Page>
      <PageHeader
        title="Global 资产"
        description="装到这台设备用户环境的 bundle，不属于任何单个项目。"
        meta={props.repoURL ? <Badge tone="quiet" className="font-mono">{props.repoURL}</Badge> : undefined}
        actions={
          <Button onClick={props.onPull} disabled={pullState.blocked}>
            <RefreshCw className="size-4" />
            拉取到设备
          </Button>
        }
      />
      <PageFill>
        <AssetsPanel
          deviceId={props.deviceId}
          root=""
          plane="global"
          hint="Global 平面的资产装到用户环境（如 ~/.cursor、~/.claude）；secrets 落到 .secrets 同步根。"
        />
      </PageFill>
    </Page>
  )
}
