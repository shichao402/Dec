import { useState } from 'react'
import type { ReactNode } from 'react'
import { ChevronDown, Link2, RefreshCw } from 'lucide-react'
import { ActionFeedback } from '@/components/action-feedback'
import { Page, PageFill, PageHeader, PageScroll } from '@/components/shell/page'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ActionButton } from '@/components/ui/action-button'
import { Notice } from '@/components/ui/feedback'
import { Field, Input, Select } from '@/components/ui/input'
import { Panel, PanelBody, PanelHeader } from '@/components/ui/panel'
import { useDecAction } from '@/lib/action-context'
import { invokeTyped } from '@/lib/api'
import { actionSpec, resource, suggestProjectName } from '@/lib/console'
import { AssetsPanel } from '@/pages/assets-panel'
import type { ManagedProject } from '@/lib/utils'

type ProjectPreparation = { AvailableProjects: string[]; HomeProject: string }

export function ProjectPage(props: {
  deviceId: string
  project: ManagedProject
  onPull: () => void
  onRemoved: () => void
  onChanged: () => void | Promise<void>
}) {
  const [project, setProject] = useState(props.project)
  const [bindingOpen, setBindingOpen] = useState(false)
  const workspaceResource = resource.workspace(project.Root)
  const removeSpec = actionSpec(`project:remove:${props.deviceId}:${project.Root}`, '移除项目管理', props.deviceId, [workspaceResource, resource.global], 'write', '已移除项目管理')
  const pullSpec = actionSpec(`operation:pull:${props.deviceId}:${project.Root}`, '拉取项目资产', props.deviceId, [workspaceResource], 'operation')
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
  const onBound = async (refreshed: ManagedProject) => {
    setProject(refreshed)
    setBindingOpen(false)
    await props.onChanged()
  }

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
              <ProjectBinding deviceId={props.deviceId} project={project} onBound={onBound} />
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
        description="家项目与本仓库的 requires 决定这里能安装的资产。"
        meta={
          <>
            <Badge tone="quiet" className="font-mono" title={project.Root}>{project.Root}</Badge>
            {project.Error && <Badge tone="bad">{project.Error}</Badge>}
          </>
        }
        actions={
          <>
            {removeButton}
            <Button onClick={props.onPull} disabled={pullState.blocked}>
              <RefreshCw className="size-4" />
              拉取到设备
            </Button>
          </>
        }
      />
      <PageFill>
        <ActionFeedback actionKey={removeSpec.key} />
        <Panel className="mb-4 shrink-0">
          <button
            onClick={() => setBindingOpen((value) => !value)}
            className="flex w-full items-center gap-3 px-4 py-3 text-left transition-colors hover:bg-panel-hi"
          >
            <Link2 className="size-4 shrink-0 text-faint" />
            <span className="min-w-0 flex-1">
              <span className="block text-[13px] font-medium text-ink">家项目绑定</span>
              <span className="block truncate text-xs text-faint">
                {bindingOpen ? '绑定名必须是私仓里已存在的项目' : '需要换绑或在私仓新建项目时展开'}
              </span>
            </span>
            <ChevronDown className={`size-4 shrink-0 text-faint transition-transform ${bindingOpen ? 'rotate-180' : ''}`} />
          </button>
          {bindingOpen && (
            <PanelBody className="border-t border-line">
              <ProjectBinding deviceId={props.deviceId} project={project} onBound={onBound} />
            </PanelBody>
          )}
        </Panel>
        <AssetsPanel
          deviceId={props.deviceId}
          root={project.Root}
          plane="local"
          hint="这里的选择只影响这个目录：家项目自带的资产加上本仓库 requires 声明的引入。"
        />
      </PageFill>
    </Page>
  )
}

// 家项目必须先存在于私仓：bind 与 push 都只接受已存在的项目名，
// 所以新机器上的新项目要能在这里就地新建，否则心流断在「绑定名不存在」。
function ProjectBinding(props: {
  deviceId: string
  project: ManagedProject
  onBound: (project: ManagedProject) => void | Promise<void>
  extraAction?: ReactNode
}) {
  const { deviceId, project } = props
  const [preparation, setPreparation] = useState<ProjectPreparation | null>(null)
  const [homeProject, setHomeProject] = useState('')
  const [newProject, setNewProject] = useState('')
  const workspaceResource = resource.workspace(project.Root)
  const prepareSpec = actionSpec(`project:prepare:${deviceId}:${project.Root}`, '检查项目配置', deviceId, [workspaceResource], 'read')
  const createSpec = actionSpec(`project:create-remote:${deviceId}:${project.Root}`, '在私仓新建项目', deviceId, [resource.global], 'write', '项目已创建并推送到私仓')
  const bindSpec = actionSpec(`project:bind:${deviceId}:${project.Root}`, project.Initialized ? '保存家项目绑定' : '初始化项目', deviceId, [workspaceResource, resource.global], 'write', project.Initialized ? '绑定已保存' : '项目初始化完成')

  const prepare = () => invokeTyped<ProjectPreparation>('prepare_project_config_init', project.Root, 'local', {}, prepareSpec.key)
  const applyPreparation = (value: ProjectPreparation, prefer = '') => {
    setPreparation(value)
    const available = value.AvailableProjects || []
    const wanted = [prefer, value.HomeProject].find((name) => name && available.includes(name))
    setHomeProject(wanted || available[0] || '')
    if (!newProject) setNewProject(suggestProjectName(project.Root))
  }
  const bind = async () => {
    if (homeProject) {
      await invokeTyped('bind_managed_project', project.Root, 'local', { ProjectName: homeProject }, bindSpec.key)
    }
    return invokeTyped<ManagedProject>('register_managed_project', '', 'global', { Root: project.Root, Label: project.Label }, bindSpec.key)
  }

  const available = preparation?.AvailableProjects || []
  const boundName = preparation?.HomeProject?.trim() || ''
  const boundMissing = Boolean(preparation && boundName && !available.includes(boundName))

  return (
    <div className="space-y-4">
      <ActionFeedback actionKey={prepareSpec.key} />
      <ActionFeedback actionKey={createSpec.key} />
      <ActionFeedback actionKey={bindSpec.key} />
      {!preparation ? (
        <div className="flex flex-wrap items-center gap-2">
          <ActionButton spec={prepareSpec} action={prepare} runningLabel="检查中…" onSuccess={(value) => applyPreparation(value)}>
            {project.Initialized ? '读取私仓项目列表' : '检查并初始化'}
          </ActionButton>
          {props.extraAction}
          <span className="text-xs text-faint">读取私仓里可用的项目名，不会修改任何文件。</span>
        </div>
      ) : (
        <>
          {boundMissing && (
            <Notice
              text={`当前绑定的 “${boundName}” 不在私仓里，所以拉不到任何资产。选一个已有项目，或在下面就地新建它。`}
            />
          )}
          <div className="grid gap-4 lg:grid-cols-2">
            {available.length > 0 ? (
              <Field label="绑定为家项目" hint={boundName ? `设备上记录的绑定：${boundName}` : undefined}>
                <Select value={homeProject} onChange={(e) => setHomeProject(e.target.value)}>
                  {available.map((name) => <option key={name} value={name}>{name}</option>)}
                </Select>
              </Field>
            ) : (
              <Notice tone="info" text="仓库中没有项目清单，将保留本地最小配置。" />
            )}
            <Field label="或在私仓新建一个项目" hint="小写字母、数字与连字符，例如 agentshelpme">
              <div className="flex gap-2">
                <Input value={newProject} onChange={(e) => setNewProject(e.target.value)} />
                <ActionButton
                  variant="secondary"
                  spec={createSpec}
                  disabled={!newProject.trim()}
                  action={async () => {
                    const created = await invokeTyped<{ Name: string }>('create_remote_project', '', 'global', { Name: newProject.trim(), Title: project.Label || project.Name }, createSpec.key)
                    return { created, preparation: await prepare() }
                  }}
                  runningLabel="创建中…"
                  onSuccess={({ created, preparation: refreshed }) => applyPreparation(refreshed, created.Name)}
                >
                  新建并选中
                </ActionButton>
              </div>
            </Field>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <ActionButton spec={bindSpec} action={bind} runningLabel={project.Initialized ? '保存中…' : '初始化中…'} onSuccess={props.onBound}>
              {project.Initialized ? '保存绑定' : '确认初始化'}
            </ActionButton>
            {props.extraAction}
          </div>
        </>
      )}
    </div>
  )
}
