import { useCallback, useEffect, useMemo, useState } from 'react'
import { ExternalLink, Pencil, RefreshCw } from 'lucide-react'
import { ActionFeedback } from '@/components/action-feedback'
import { Page, PageHeader, PageScroll } from '@/components/shell/page'
import { ActionButton } from '@/components/ui/action-button'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { EmptyState, Loading, Notice } from '@/components/ui/feedback'
import { Field, Input, Select } from '@/components/ui/input'
import { Panel, PanelBody, PanelHeader } from '@/components/ui/panel'
import { useActionRegistry } from '@/lib/action-context'
import { invokeTyped } from '@/lib/api'
import { actionSpec, resource } from '@/lib/console'

type AssetFile = { Path: string; Content: string }
type OfficialAssetEdit = {
  Project: string
  Type: string
  Name: string
  BaseVersion?: string
  OverrideID?: string
  OverrideActive?: boolean
  UpstreamURL?: string
  Files?: AssetFile[]
}
type OfficialAssetEditsState = { Assets?: OfficialAssetEdit[] }
type PreviewResult = { Diff: string }
type ApplyResult = { ID: string; Kind: string; URL: string; Diff: string }

export function OfficialOverridesPage(props: {
  deviceId: string
  root: string
  label: string
  onBack: () => void
}) {
  const [assets, setAssets] = useState<OfficialAssetEdit[] | null>(null)
  const [editing, setEditing] = useState<OfficialAssetEdit | null>(null)
  const [files, setFiles] = useState<AssetFile[]>([])
  const [diff, setDiff] = useState('')
  const [originRepo, setOriginRepo] = useState('')
  const [title, setTitle] = useState('')
  const [mode, setMode] = useState('issue')
  const [branch, setBranch] = useState('')
  const [submitted, setSubmitted] = useState<ApplyResult | null>(null)
  const actions = useActionRegistry()
  const runAction = actions.run
  const workspaceResource = resource.workspace(props.root)
  const loadSpec = useMemo(
    () => actionSpec(`overrides:list:${props.deviceId}:${props.root}`, '加载可修改的官方资产', props.deviceId, [workspaceResource], 'read'),
    [props.deviceId, props.root, workspaceResource],
  )
  const editSpec = actionSpec(`overrides:edit:${props.deviceId}:${props.root}`, '读取官方资产', props.deviceId, [workspaceResource], 'read')
  const previewSpec = actionSpec(`overrides:preview:${props.deviceId}:${props.root}`, '预览本地覆写', props.deviceId, [workspaceResource], 'read')
  const submitSpec = actionSpec(`overrides:submit:${props.deviceId}:${props.root}`, '提交上游并启用本地覆写', props.deviceId, [workspaceResource], 'write', '本地覆写已启用')

  const load = useCallback(async () => {
    const outcome = await runAction(
      loadSpec,
      () => invokeTyped<OfficialAssetEditsState>('list_official_asset_edits', props.root, 'local', {}, loadSpec.key),
      { force: true },
    )
    if (outcome.ok) setAssets(outcome.value.Assets || [])
  }, [loadSpec, props.root, runAction])

  // oxlint-disable-next-line react(set-state-in-effect)
  useEffect(() => { void load() }, [load])

  const payload = editing ? { Project: editing.Project, Type: editing.Type, Name: editing.Name, Files: files } : null
  const beginEdit = async (asset: OfficialAssetEdit) => {
    const outcome = await runAction(editSpec, () => invokeTyped<OfficialAssetEdit>(
      'load_official_asset_edit',
      props.root,
      'local',
      { Project: asset.Project, Type: asset.Type, Name: asset.Name },
      editSpec.key,
    ))
    if (!outcome.ok) return
    setEditing(outcome.value)
    setFiles(outcome.value.Files || [])
    setOriginRepo('')
    setTitle(`fix: ${asset.Name}`)
    setDiff('')
    setSubmitted(null)
  }

  return (
    <Page>
      <PageHeader
        title="本地覆写"
        description="临时修改这个项目里已安装的官方资产，并关联上游 Issue 或 PR。官方注册表只由提供方 CI 写。"
        meta={<Badge tone="quiet" className="font-mono" title={props.root}>{props.label}</Badge>}
        actions={
          <>
            <Button variant="outline" onClick={props.onBack}>返回</Button>
            <Button variant="outline" onClick={() => void load()}>
              <RefreshCw className="size-4" />
              刷新
            </Button>
          </>
        }
      />
      <PageScroll className="space-y-4">
        <div className="space-y-2 empty:hidden">
          <ActionFeedback actionKey={loadSpec.key} />
          <ActionFeedback actionKey={editSpec.key} />
          <ActionFeedback actionKey={previewSpec.key} />
          <ActionFeedback actionKey={submitSpec.key} />
        </div>
        <Panel>
          <PanelHeader title="可修改的官方资产" description="只列这个项目已安装的官方 bundle。" />
          {assets === null ? (
            <Loading />
          ) : assets.length === 0 ? (
            <EmptyState text="还没有可修改的官方资产" hint="先到「更新」页安装官方依赖。" />
          ) : (
            <div className="divide-y divide-line">
              {assets.map((asset) => (
                <div key={`${asset.Project}/${asset.Type}/${asset.Name}`} className="flex items-center gap-3 px-4 py-3">
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span className="font-mono text-xs font-medium text-ink">{asset.Project}/{asset.Type}/{asset.Name}</span>
                      <Badge tone="quiet">{asset.BaseVersion || '未知版本'}</Badge>
                      {asset.OverrideActive && <Badge tone="warn">覆写中</Badge>}
                    </div>
                    {asset.UpstreamURL && (
                      <a className="mt-1 inline-flex items-center gap-1 text-xs text-accent-hi hover:underline" href={asset.UpstreamURL} target="_blank" rel="noreferrer">
                        上游票据 <ExternalLink className="size-3" />
                      </a>
                    )}
                  </div>
                  <Button size="sm" variant="outline" onClick={() => void beginEdit(asset)}>
                    <Pencil className="size-3.5" />修改
                  </Button>
                </div>
              ))}
            </div>
          )}
        </Panel>
        {editing && (
          <Panel>
            <PanelHeader
              title={`${editing.Project}/${editing.Type}/${editing.Name}`}
              description="改完先预览 diff，再连带上游票据一起提交。"
              action={<Button size="sm" variant="ghost" onClick={() => setEditing(null)}>关闭</Button>}
            />
            <PanelBody className="space-y-3">
              {files.map((file, index) => (
                <Field key={file.Path} label={file.Path}>
                  <textarea
                    aria-label={file.Path}
                    className="min-h-48 w-full resize-y rounded-lg border border-line bg-canvas px-3 py-2 font-mono text-xs text-ink outline-none focus:border-accent"
                    value={file.Content}
                    onChange={(event) => setFiles((current) => current.map((item, itemIndex) => (
                      itemIndex === index ? { ...item, Content: event.target.value } : item
                    )))}
                  />
                </Field>
              ))}
              <div className="grid gap-3 md:grid-cols-2">
                <Field label="上游仓库">
                  <Input aria-label="上游仓库" value={originRepo} onChange={(event) => setOriginRepo(event.target.value)} placeholder="owner/repo" />
                </Field>
                <Field label="标题">
                  <Input aria-label="标题" value={title} onChange={(event) => setTitle(event.target.value)} />
                </Field>
                <Field label="提交方式">
                  <Select aria-label="提交方式" value={mode} onChange={(event) => setMode(event.target.value)}>
                    <option value="issue">Issue</option>
                    <option value="pr">PR</option>
                  </Select>
                </Field>
                {mode === 'pr' && (
                  <Field label="已推送分支">
                    <Input aria-label="已推送分支" value={branch} onChange={(event) => setBranch(event.target.value)} placeholder="owner:branch" />
                  </Field>
                )}
              </div>
              <div className="flex flex-wrap gap-2">
                <ActionButton
                  spec={previewSpec}
                  variant="outline"
                  disabled={!payload}
                  action={() => invokeTyped<PreviewResult>('preview_official_override', props.root, 'local', payload, previewSpec.key)}
                  onSuccess={(value) => { setDiff(value.Diff); setSubmitted(null) }}
                  runningLabel="预览中…"
                >
                  预览修改
                </ActionButton>
                {diff && (
                  <ActionButton
                    spec={submitSpec}
                    disabled={!originRepo.trim() || !title.trim() || (mode === 'pr' && !branch.trim())}
                    action={() => invokeTyped<ApplyResult>(
                      'apply_official_override',
                      props.root,
                      'local',
                      { ...payload, OriginRepo: originRepo, Title: title, Mode: mode, Branch: branch },
                      submitSpec.key,
                    )}
                    onSuccess={(value) => { setSubmitted(value); void load() }}
                    runningLabel="提交中…"
                  >
                    提交 {mode === 'pr' ? 'PR' : 'Issue'} 并启用覆写
                  </ActionButton>
                )}
              </div>
              {diff && <pre className="max-h-80 overflow-auto whitespace-pre-wrap rounded-lg border border-line bg-canvas p-3 font-mono text-xs text-muted">{diff}</pre>}
              {submitted && (
                <Notice
                  tone="info"
                  text={<a href={submitted.URL} target="_blank" rel="noreferrer" className="underline">{submitted.Kind} 已创建：{submitted.URL}</a>}
                />
              )}
            </PanelBody>
          </Panel>
        )}
      </PageScroll>
    </Page>
  )
}
