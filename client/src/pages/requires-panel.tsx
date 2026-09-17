import { useCallback, useEffect, useMemo, useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { ActionFeedback } from '@/components/action-feedback'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { EmptyState, Loading, Notice } from '@/components/ui/feedback'
import { Panel, PanelBody, PanelHeader } from '@/components/ui/panel'
import { useActionRegistry } from '@/lib/action-context'
import { invokeTyped } from '@/lib/api'
import { actionSpec, resource } from '@/lib/console'

type OfficialRequire = {
  Project: string
  Want: string
  Installed?: string
  Available?: string
  Tag?: string
  UpdateAvailable?: boolean
  Error?: string
  OverrideActive?: boolean
  OverrideReadyToDrop?: boolean
}

type OfficialRequiresState = { Items?: OfficialRequire[] }

export function RequiresPanel(props: {
  deviceId: string
  root: string
  plane: 'local' | 'global'
}) {
  const [items, setItems] = useState<OfficialRequire[] | null>(null)
  const actions = useActionRegistry()
  const runAction = actions.run
  const workspaceResource = resource.workspace(props.root)
  const scope = props.root || 'global'
  const loadSpec = useMemo(
    () => actionSpec(`requires:list:${props.deviceId}:${scope}`, '检查官方依赖', props.deviceId, [workspaceResource], 'read'),
    [props.deviceId, scope, workspaceResource],
  )

  const load = useCallback(async () => {
    const outcome = await runAction(
      loadSpec,
      () => invokeTyped<OfficialRequiresState>('list_official_requires', props.root, props.plane, {}, loadSpec.key),
      { force: true },
    )
    if (outcome.ok) setItems(outcome.value.Items || [])
  }, [loadSpec, props.plane, props.root, runAction])

  // oxlint-disable-next-line react/set-state-in-effect
  useEffect(() => { void load() }, [load])

  const pending = (items || []).filter((item) => item.UpdateAvailable || item.Error).length

  return (
    <Panel className="shrink-0">
      <PanelHeader
        title="官方依赖"
        description={pending > 0 ? `${pending} 个待更新` : '按 requires 安装的官方资产'}
        action={
          <Button size="sm" variant="ghost" onClick={() => void load()}>
            <RefreshCw className="size-3.5" />检查更新
          </Button>
        }
      />
      <div className="px-4 pt-3 empty:hidden">
        <ActionFeedback actionKey={loadSpec.key} />
      </div>
      {items === null ? (
        <Loading />
      ) : items.length === 0 ? (
        <EmptyState text="未声明官方依赖" />
      ) : (
        <PanelBody className="space-y-2">
          {items.map((item) => (
            <div key={item.Project} className="flex flex-wrap items-center gap-3 rounded-lg border border-line px-3 py-2.5">
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-1.5">
                  <span className="font-mono text-sm font-medium text-ink">{item.Project}</span>
                  <Badge tone="quiet">{item.Want}</Badge>
                  {item.UpdateAvailable && <Badge tone="warn">有更新</Badge>}
                  {item.OverrideActive && <Badge tone="warn">本地覆写</Badge>}
                  {item.OverrideReadyToDrop && <Badge tone="good">上游已解决</Badge>}
                  {item.Error && <Badge tone="bad">失败</Badge>}
                  {!item.Error && !item.UpdateAvailable && item.Installed && <Badge tone="good">已最新</Badge>}
                </div>
                <p className="mt-1 font-mono text-[11px] text-faint">
                  已装 {item.Installed || '—'}
                  <span className="mx-2 text-line">·</span>
                  可用 {item.Available || '—'}
                </p>
                {item.Error && <Notice className="mt-2" tone="warn" text={item.Error} />}
              </div>
              <span className="text-xs text-faint">到「更新」页安装</span>
            </div>
          ))}
        </PanelBody>
      )}
    </Panel>
  )
}
