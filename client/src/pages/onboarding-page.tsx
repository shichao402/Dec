import { useState } from 'react'
import { Check, CheckCircle2, Search } from 'lucide-react'
import { ActionFeedback } from '@/components/action-feedback'
import { Page, PageHeader, PageScroll } from '@/components/shell/page'
import { Button } from '@/components/ui/button'
import { ActionButton } from '@/components/ui/action-button'
import { CheckOption } from '@/components/ui/checkbox'
import { EmptyState } from '@/components/ui/feedback'
import { Field, Input } from '@/components/ui/input'
import { Panel, PanelBody, PanelFooter, PanelHeader } from '@/components/ui/panel'
import { invokeTyped } from '@/lib/api'
import { actionSpec, resource, toggle } from '@/lib/console'
import { AssetRow } from '@/pages/assets-panel'
import { cn } from '@/lib/utils'
import type { AssetSelection, GlobalSettings } from '@/lib/utils'

const steps = [
  { title: '连上私仓', hint: '私仓地址、IDE 与服务空闲时间' },
  { title: '选 Global 资产', hint: '决定装到这台设备的 bundle' },
  { title: '拉取落地', hint: '把选择写进用户环境' },
]

export function OnboardingPage(props: {
  deviceId: string
  settings: GlobalSettings
  setSettings: (value: GlobalSettings) => void
  onComplete: () => void | Promise<void>
  onPull: () => void | Promise<void>
}) {
  const [step, setStep] = useState(1)
  const [repoURL, setRepoURL] = useState(props.settings.RepoURL)
  const [ides, setIDEs] = useState<string[]>(props.settings.SelectedIDEs.length ? props.settings.SelectedIDEs : ['cursor'])
  const [idle, setIdle] = useState(props.settings.ServerIdleTimeout || '30m')
  const [assets, setAssets] = useState<AssetSelection | null>(null)
  const [selected, setSelected] = useState<string[]>([])
  const [query, setQuery] = useState('')
  const saveDeviceSpec = actionSpec('onboarding:settings', '验证并保存设备设置', props.deviceId, [resource.global], 'write', '设备设置已保存')
  const saveAssetsSpec = actionSpec('onboarding:assets', '保存 Global 资产选择', props.deviceId, [resource.global], 'write', 'Global 资产选择已保存')

  const saveDevice = async () => {
    const saved = await invokeTyped<{ RepoAuthRequired?: boolean; RepoHost?: string; ConnectError?: string }>(
      'save_global_settings',
      '',
      'global',
      { RepoURL: repoURL, IDEs: ides, ServerIdleTimeout: idle },
      saveDeviceSpec.key,
    )
    if (saved.RepoAuthRequired || saved.ConnectError) {
      throw new Error(`私仓认证失败${saved.RepoHost ? `（${saved.RepoHost}）` : ''}：${saved.ConnectError || '需要先配置 Git 凭据'}`)
    }
    const next = await invokeTyped<GlobalSettings>('load_global_settings', '', 'global', {}, saveDeviceSpec.key)
    props.setSettings(next)
    return invokeTyped<AssetSelection>('load_asset_selection', '', 'global', {}, saveDeviceSpec.key)
  }

  const saveAssets = () => invokeTyped('save_enabled_bundles', '', 'global', { EnabledProjects: selected }, saveAssetsSpec.key)
  const bundles = assets?.Bundles || []
  const keyword = query.trim().toLowerCase()
  const visible = keyword
    ? bundles.filter((item) => `${item.Name} ${item.Description}`.toLowerCase().includes(keyword))
    : bundles

  return (
    <Page>
      <PageHeader
        title="准备这台设备"
        description="三步完成初始化：连上私仓、选好 Global 资产、拉取落地。之后都能在设备设置里改。"
      />
      <PageScroll className="max-w-5xl">
        <div className="grid gap-6 lg:grid-cols-[minmax(0,14rem)_minmax(0,1fr)]">
          <ol className="space-y-1">
            {steps.map((item, index) => {
              const number = index + 1
              const done = step > number
              const active = step === number
              return (
                <li
                  key={item.title}
                  className={cn(
                    'flex gap-3 rounded-lg px-3 py-2.5',
                    active ? 'bg-panel' : 'opacity-70',
                  )}
                >
                  <span
                    className={cn(
                      'tnum mt-0.5 grid size-5 shrink-0 place-items-center rounded-full text-[11px] font-semibold',
                      done ? 'bg-good/15 text-good' : active ? 'bg-accent text-white' : 'bg-panel-hi text-faint',
                    )}
                  >
                    {done ? <Check className="size-3" strokeWidth={3} /> : number}
                  </span>
                  <span className="min-w-0">
                    <span className={cn('block text-[13px]', active ? 'font-medium text-ink' : 'text-muted')}>{item.title}</span>
                    <span className="block text-[11px] leading-relaxed text-faint">{item.hint}</span>
                  </span>
                </li>
              )
            })}
          </ol>

          <div className="min-w-0 space-y-4">
            <ActionFeedback actionKey={saveDeviceSpec.key} />
            <ActionFeedback actionKey={saveAssetsSpec.key} />

            {step === 1 && (
              <Panel>
                <PanelHeader title="私仓与运行环境" description="保存时会校验私仓是否可达。" />
                <PanelBody className="space-y-4">
                  <Field label="Dec 私仓地址" hint="需要这台设备已配置好 Git 凭据。">
                    <Input className="font-mono text-xs" placeholder="https://..." value={repoURL} onChange={(e) => setRepoURL(e.target.value)} />
                  </Field>
                  <Field label="服务空闲退出" hint="无请求后自动退出的时长，例如 30m。">
                    <Input className="max-w-40" value={idle} onChange={(e) => setIdle(e.target.value)} />
                  </Field>
                  <Field label="目标 IDE">
                    <div className="flex flex-wrap gap-2">
                      {props.settings.AvailableIDEs.map((ide) => (
                        <CheckOption key={ide} label={ide} checked={ides.includes(ide)} onChange={() => setIDEs(toggle(ides, ide))} />
                      ))}
                    </div>
                  </Field>
                </PanelBody>
                <PanelFooter>
                  <ActionButton
                    spec={saveDeviceSpec}
                    action={saveDevice}
                    runningLabel="验证并保存中…"
                    onSuccess={(selection) => {
                      setAssets(selection)
                      setSelected(selection.Bundles.filter((item) => item.Enabled).map((item) => item.Name))
                      setStep(2)
                    }}
                  >
                    验证并继续
                  </ActionButton>
                  <span className="text-xs text-faint">私仓不可达时会停在这一步并说明原因。</span>
                </PanelFooter>
              </Panel>
            )}

            {step === 2 && assets && (
              <Panel className="overflow-hidden">
                <PanelHeader
                  title="选择 Global 资产"
                  description={`已选 ${selected.length} / ${bundles.length}，之后可在 Global 资产页调整。`}
                />
                <div className="border-b border-line p-3">
                  <div className="relative">
                    <Search className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-faint" />
                    <Input className="pl-8" placeholder="搜索资产或描述" value={query} onChange={(e) => setQuery(e.target.value)} />
                  </div>
                </div>
                <div className="max-h-[26rem] divide-y divide-line overflow-y-auto">
                  {visible.map((item) => (
                    <AssetRow
                      key={`${item.Vault}-${item.Name}`}
                      item={item}
                      compact
                      checked={selected.includes(item.Name)}
                      onToggle={() => setSelected(toggle(selected, item.Name))}
                    />
                  ))}
                  {visible.length === 0 && (
                    <EmptyState className="m-4 border-none" text="没有匹配的资产" hint="换个关键词，或先确认私仓连接正常。" />
                  )}
                </div>
                <PanelFooter>
                  <ActionButton spec={saveAssetsSpec} action={saveAssets} runningLabel="保存中…" onSuccess={() => setStep(3)}>
                    保存选择
                  </ActionButton>
                  <Button variant="ghost" onClick={() => setStep(1)}>返回上一步</Button>
                </PanelFooter>
              </Panel>
            )}

            {step === 3 && (
              <Panel>
                <PanelBody className="space-y-3 p-6 text-center">
                  <CheckCircle2 className="mx-auto size-9 text-good" />
                  <h2 className="text-base font-semibold text-ink">设备初始化完成</h2>
                  <p className="mx-auto max-w-md text-[13px] leading-relaxed text-faint">
                    现在可以直接拉取 Global 资产落地到用户环境，也可以先进入控制台接管项目目录。
                  </p>
                  <div className="flex justify-center gap-2 pt-1">
                    <Button onClick={props.onPull}>拉取 Global 资产</Button>
                    <Button variant="outline" onClick={props.onComplete}>进入控制台</Button>
                  </div>
                </PanelBody>
              </Panel>
            )}
          </div>
        </div>
      </PageScroll>
    </Page>
  )
}
