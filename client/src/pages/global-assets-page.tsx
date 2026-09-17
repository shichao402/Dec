import { RefreshCw } from 'lucide-react'
import { Page, PageFill, PageHeader } from '@/components/shell/page'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useDecAction } from '@/lib/action-context'
import { actionSpec, resource } from '@/lib/console'
import { AssetsPanel } from '@/pages/assets-panel'
import { RequiresPanel } from '@/pages/requires-panel'
import { WorkspaceWritePanel } from '@/pages/workspace-write-panel'

export function GlobalAssetsPage(props: {
  deviceId: string
  repoURL: string
  onSync: () => void
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
        <div className="mb-4">
          <RequiresPanel deviceId={props.deviceId} root="" plane="global" />
        </div>
        <div className="mb-4">
          <WorkspaceWritePanel deviceId={props.deviceId} root="" plane="global" label="Global" />
        </div>
        <AssetsPanel
          deviceId={props.deviceId}
          root=""
          plane="global"
          hint="Global 平面的资产装到用户环境（如 ~/.cursor、~/.claude）；提供项请在各自项目页管理。"
        />
      </PageFill>
    </Page>
  )
}
