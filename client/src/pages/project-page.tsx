import { FileDiff, Link2, RefreshCw, Upload, UploadCloud } from 'lucide-react'
import { ActionFeedback } from '@/components/action-feedback'
import { Page, PageFill, PageHeader, PageScroll } from '@/components/shell/page'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ActionButton } from '@/components/ui/action-button'
import { NavCard, NavCardGrid } from '@/components/ui/nav-card'
import { Panel, PanelBody, PanelHeader } from '@/components/ui/panel'
import { useDecAction } from '@/lib/action-context'
import { invokeTyped } from '@/lib/api'
import { actionSpec, resource } from '@/lib/console'
import { ProjectBinding } from '@/pages/project-binding-page'
import { SubscriptionPanel } from '@/pages/subscription-panel'
import type { ManagedProject } from '@/lib/utils'

export function ProjectPage(props: {
  deviceId: string
  project: ManagedProject
  onPull: () => void
  onSync: () => void
  onBinding: () => void
  onOverrides: () => void
  onProvides: () => void
  onWriteback: () => void
  onRemoved: () => void
  onBound: (project: ManagedProject) => void | Promise<void>
}) {
  const project = props.project
  const workspaceResource = resource.workspace(project.Root)
  const removeSpec = actionSpec(`project:remove:${props.deviceId}:${project.Root}`, '移除项目管理', props.deviceId, [workspaceResource, resource.global], 'write', '已移除项目管理')
  const pullSpec = actionSpec(
    `operation:pull:${props.deviceId}:${project.Root}`,
    '正在拉取项目资产',
    props.deviceId,
    [workspaceResource],
    'operation',
    '项目资产拉取完成',
  )
  const pullState = useDecAction(pullSpec)
  const removeButton = (
    <ActionButton
      variant="outline"
      spec={removeSpec}
      action={() => invokeTyped('remove_managed_project', '', 'global', { Root: project.Root }, removeSpec.key)}
      runningLabel="移除中…"
      onSuccess={props.onRemoved}
    >
      移除管理
    </ActionButton>
  )

  if (!project.Initialized) {
    return (
      <Page>
        <PageHeader
          title={`初始化 ${project.Label || project.Name}`}
          description="将在该目录创建 `.dec/config.yaml` 和变量模板；现有项目文件不会被修改。"
          meta={<Badge tone="quiet" className="font-mono">{project.Root}</Badge>}
          actions={removeButton}
        />
        <PageScroll className="max-w-3xl">
          <Panel>
            <PanelHeader title="家项目绑定" description="家项目决定这个目录能装哪些资产，绑定名必须是私仓里已存在的项目。" />
            <PanelBody>
              <ProjectBinding deviceId={props.deviceId} project={project} onBound={props.onBound} />
            </PanelBody>
          </Panel>
        </PageScroll>
      </Page>
    )
  }

  return (
    <Page>
      <PageHeader
        title={project.Label || project.Name}
        description="主区是订阅；换绑、覆写、提供项与写回都在下级页。"
        meta={
          <>
            <Badge tone="quiet" className="font-mono" title={project.Root}>{project.Root}</Badge>
            {project.Error && <Badge tone="bad">{project.Error}</Badge>}
          </>
        }
        actions={
          <>
            {removeButton}
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
        <ActionFeedback actionKey={removeSpec.key} />
        <ActionFeedback actionKey={pullSpec.key} />
        {/* 换绑、覆写、提供项与写回都是偶发操作，收成一排入口卡，主区留给订阅。 */}
        <NavCardGrid className="mb-4">
          <NavCard
            icon={Link2}
            title="家项目绑定"
            description="换绑家项目，或在私仓里新建一个"
            onClick={props.onBinding}
          />
          <NavCard
            icon={FileDiff}
            title="本地覆写"
            description="临时改官方资产，关联上游 Issue 或 PR"
            onClick={props.onOverrides}
          />
          <NavCard
            icon={Upload}
            title="我提供的资产"
            description="作者视角：登记本项目对外提供的 Git 资产"
            onClick={props.onProvides}
          />
          <NavCard
            icon={UploadCloud}
            title="写回与密钥"
            description="个人资产写入私仓，密钥写入 Bitwarden"
            onClick={props.onWriteback}
          />
        </NavCardGrid>
        <div className="mb-2">
          <h2 className="text-[13px] font-semibold text-ink">订阅</h2>
          <p className="mt-0.5 text-xs text-faint">本项目消费哪些项目：官方注册表或个人私仓。</p>
        </div>
        <SubscriptionPanel
          deviceId={props.deviceId}
          root={project.Root}
          plane="local"
          hint="保存订阅后点「拉取」落地到本项目 IDE 目录；官方新版本用「官方更新」。"
        />
      </PageFill>
    </Page>
  )
}
