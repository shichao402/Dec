import { expect, type Page } from '@playwright/test'
import { scenarios, type ScenarioName } from './fixtures/data'
import { installTauriMock } from './fixtures/tauri-mock'

// 视口基线：最窄取 tauri.conf.json 里声明的窗口下限，其余覆盖默认尺寸与宽屏。
// 与 internal/tui 的 snapshotWidths 同一思路：先声明支持范围，再守住边界。
export type Viewport = { name: string; width: number; height: number }

export const viewports: Viewport[] = [
  { name: '960x600', width: 960, height: 600 },
  { name: '1280x840', width: 1280, height: 840 },
  { name: '1728x1080', width: 1728, height: 1080 },
]

// 人工过目只需要两端：最窄和最宽，中间尺寸交给断言。
export const shotViewports: Viewport[] = [viewports[0], viewports[2]]

export type Case = {
  name: string
  scenario: ScenarioName
  open: (page: Page) => Promise<void>
  // 有意为之的布局（居中解锁卡）在这里显式豁免，而不是放宽全局阈值。
  ignore?: string[]
}

async function boot(page: Page, scenario: ScenarioName) {
  await page.addInitScript(installTauriMock, scenarios[scenario]())
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '选择设备' })).toBeVisible()
  await expect(page.getByText('Console 更新', { exact: true })).toBeVisible()
}

async function connect(page: Page) {
  await page.getByRole('button', { name: '连接', exact: true }).first().click()
}

// 导航项的无障碍名里带计数（「项目 5」），所以按前缀匹配而不是全等。
async function nav(page: Page, label: string) {
  await page.locator('aside').getByRole('button', { name: new RegExp(`^${label}`) }).first().click()
}

// 成功提示 4 秒后自撤，会话浮层盖住整屏：留着它们测到的是过渡态。
async function settle(page: Page) {
  const closers = page.getByRole('button', { name: '关闭' })
  for (let remaining = await closers.count(); remaining > 0; remaining -= 1) {
    await closers.first().click({ timeout: 2000 }).catch(() => undefined)
  }
  await expect(page.locator('[role="status"]')).toHaveCount(0, { timeout: 8000 })
  await page.evaluate(() => document.fonts.ready.then(() => undefined))
  await page.waitForTimeout(120)
}

// 把一个用例开到可测量状态：设视口、注入 IPC、走到目标页、等浮层散尽。
export async function prepare(page: Page, item: Case, viewport: Viewport) {
  await page.setViewportSize({ width: viewport.width, height: viewport.height })
  await boot(page, item.scenario)
  await item.open(page)
  await settle(page)
}

export const cases: Case[] = [
  {
    name: 'connect',
    scenario: 'typical',
    open: async () => undefined,
  },
  {
    name: 'connect-empty',
    scenario: 'empty',
    open: async () => undefined,
  },
  {
    name: 'unlock',
    scenario: 'locked',
    open: async (page) => {
      await connect(page)
      await expect(page.getByRole('heading', { name: /解锁/ })).toBeVisible()
      await expect(page.getByText('Console 更新', { exact: true })).toBeVisible()
    },
    // 居中的解锁卡片是业界通行做法，不该被横向利用率规则判死。
    ignore: ['width-usage', 'container-usage'],
  },
  {
    name: 'onboarding',
    scenario: 'fresh',
    open: async (page) => {
      await connect(page)
      await expect(page.getByRole('heading', { name: '准备这台设备' })).toBeVisible()
    },
  },
  {
    name: 'onboarding-assets',
    scenario: 'fresh',
    open: async (page) => {
      await connect(page)
      await page.getByRole('button', { name: '验证并继续' }).click()
      await expect(page.getByText('选择 Global 资产')).toBeVisible()
    },
  },
  {
    name: 'overview',
    scenario: 'typical',
    open: async (page) => {
      await connect(page)
      await expect(page.getByRole('heading', { name: '设备概览' })).toBeVisible()
    },
  },
  {
    name: 'overview-empty',
    scenario: 'empty',
    open: async (page) => {
      await connect(page)
      await expect(page.getByRole('heading', { name: '设备概览' })).toBeVisible()
    },
  },
  {
    name: 'overview-heavy',
    scenario: 'heavy',
    open: async (page) => {
      await connect(page)
      await expect(page.getByRole('heading', { name: '设备概览' })).toBeVisible()
    },
  },
  {
    name: 'global-assets',
    scenario: 'typical',
    open: async (page) => {
      await connect(page)
      await nav(page, 'Global 资产')
      await expect(page.getByRole('heading', { name: 'Global 资产' })).toBeVisible()
      await expect(page.getByText(/提供的 Global/)).toHaveCount(0)
    },
  },
  {
    name: 'global-assets-empty',
    scenario: 'empty',
    open: async (page) => {
      await connect(page)
      await nav(page, 'Global 资产')
      await expect(page.getByText('这个范围里还没有可订阅的项目')).toBeVisible()
    },
  },
  {
    name: 'global-assets-heavy',
    scenario: 'heavy',
    open: async (page) => {
      await connect(page)
      await nav(page, 'Global 资产')
      await expect(page.getByRole('heading', { name: 'Global 资产' })).toBeVisible()
    },
  },
  {
    name: 'global-assets-extreme',
    scenario: 'extreme',
    open: async (page) => {
      await connect(page)
      await nav(page, 'Global 资产')
      await expect(page.getByRole('heading', { name: 'Global 资产' })).toBeVisible()
    },
  },
  {
    name: 'projects',
    scenario: 'typical',
    open: async (page) => {
      await connect(page)
      await nav(page, '项目')
      await expect(page.getByRole('heading', { name: '项目' })).toBeVisible()
    },
  },
  {
    name: 'projects-empty',
    scenario: 'empty',
    open: async (page) => {
      await connect(page)
      await nav(page, '项目')
      await expect(page.getByText('这台设备还没有受管项目')).toBeVisible()
    },
  },
  {
    name: 'projects-heavy',
    scenario: 'heavy',
    open: async (page) => {
      await connect(page)
      await nav(page, '项目')
      await expect(page.getByRole('heading', { name: '项目' })).toBeVisible()
    },
  },
  {
    name: 'projects-extreme',
    scenario: 'extreme',
    open: async (page) => {
      await connect(page)
      await nav(page, '项目')
      await expect(page.getByRole('heading', { name: '项目' })).toBeVisible()
    },
  },
  {
    name: 'projects-failing',
    scenario: 'failing',
    open: async (page) => {
      await connect(page)
      await nav(page, '项目')
      await expect(page.getByRole('heading', { name: '项目' })).toBeVisible()
    },
  },
  {
    name: 'projects-picker',
    scenario: 'typical',
    open: async (page) => {
      await connect(page)
      await nav(page, '项目')
      await page.getByRole('button', { name: '导入目录' }).click()
      await expect(page.getByText('选择设备上的目录')).toBeVisible()
    },
  },
  {
    name: 'projects-scan',
    scenario: 'typical',
    open: async (page) => {
      await connect(page)
      await nav(page, '项目')
      await page.getByRole('button', { name: '导入目录' }).click()
      await page.getByRole('button', { name: '扫描此范围' }).click()
      await page.getByRole('button', { name: '全选' }).click()
      // 勾完一次导入：批量按钮要带上选中数，行内不再需要逐个点。
      await expect(page.getByRole('button', { name: '导入选中的 14 个' })).toBeEnabled()
    },
  },
  {
    name: 'projects-scan-extreme',
    scenario: 'extreme',
    open: async (page) => {
      await connect(page)
      await nav(page, '项目')
      await page.getByRole('button', { name: '导入目录' }).click()
      await page.getByRole('button', { name: '扫描此范围' }).click()
      await page.getByRole('button', { name: '全选' }).click()
      await expect(page.getByRole('button', { name: '导入选中的 2 个' })).toBeEnabled()
    },
  },
  {
    name: 'project',
    scenario: 'typical',
    open: async (page) => {
      await connect(page)
      await nav(page, '项目')
      await page.getByRole('button', { name: /^Dec/ }).first().click()
      await expect(page.getByRole('main').getByRole('button', { name: '更新', exact: true })).toBeVisible()
      // 四个下级页都只在主区留一张入口卡，表单与列表本身不在项目页加载。
      await expect(page.getByRole('button', { name: /^家项目绑定/ })).toBeVisible()
      await expect(page.getByRole('button', { name: /^本地覆写/ })).toBeVisible()
      await expect(page.getByRole('button', { name: '修改', exact: true })).toHaveCount(0)
      await expect(page.getByRole('button', { name: /^我提供的资产/ })).toBeVisible()
      await expect(page.getByLabel('作者目录基准点')).toHaveCount(0)
      await expect(page.getByRole('button', { name: /^写回与密钥/ })).toBeVisible()
      await expect(page.getByRole('button', { name: '预览写回' })).toHaveCount(0)
      await expect(page.getByRole('button', { name: '列出密钥' })).toHaveCount(0)
      // 订阅面板一张表同时列私仓与官方注册表项目，同名项目只给一行（ADR 0029）。
      await expect(page.getByText('订阅', { exact: true })).toBeVisible()
      await expect(page.getByRole('checkbox', { name: 'relkit' })).toHaveCount(1)
      await expect(page.getByText('私仓', { exact: true }).first()).toBeVisible()
      // 官方 pin 的行必须按官方身份渲染，并在任何宽度都留下「有更新」与去哪更新的提示：
      // 版本列窄屏会收起，唯一不该收起的是这两条，否则 CI 发了新版用户仍然不知道。
      await expect(page.getByText('官方', { exact: true }).first()).toBeVisible()
      await expect(page.getByText('有更新', { exact: true }).first()).toBeVisible()
      await expect(page.getByText(/有新版本，到「更新」页预览后安装/)).toBeVisible()
      const home = page.getByRole('checkbox', { name: 'dec' })
      await expect(home).toBeChecked()
      await expect(home).toBeDisabled()
      await expect(page.getByText('home · 必选')).toBeVisible()
    },
  },
  {
    name: 'project-extreme',
    scenario: 'extreme',
    open: async (page) => {
      await connect(page)
      await nav(page, '项目')
      await page.locator('main button').filter({ hasText: '腾讯云基础设施' }).first().click()
      await expect(page.getByRole('main').getByRole('button', { name: '更新', exact: true })).toBeVisible()
    },
  },
  {
    name: 'project-overrides',
    scenario: 'typical',
    open: async (page) => {
      await connect(page)
      await nav(page, '项目')
      await page.getByRole('button', { name: /^Dec/ }).first().click()
      await page.getByRole('button', { name: /^本地覆写/ }).click()
      await expect(page.getByRole('heading', { name: '本地覆写' })).toBeVisible()
      await page.getByRole('button', { name: '修改', exact: true }).click()
      await page.getByLabel('SKILL.md').fill('# changed')
      await page.getByLabel('上游仓库').fill('example/relkit')
      await page.getByRole('button', { name: '预览修改' }).click()
      await expect(page.getByText('+# changed', { exact: false })).toBeVisible()
      await expect(page.getByRole('button', { name: '提交 Issue 并启用覆写' })).toBeVisible()
      // 返回回到项目页，而不是回到项目列表。
      await expect(page.getByRole('button', { name: '返回' })).toBeVisible()
    },
  },
  {
    name: 'project-provides',
    scenario: 'typical',
    open: async (page) => {
      await connect(page)
      await nav(page, '项目')
      await page.getByRole('button', { name: /^Dec/ }).first().click()
      await page.getByRole('button', { name: /^我提供的资产/ }).click()
      await expect(page.getByRole('heading', { name: '我提供的资产' })).toBeVisible()
      // 新项目默认 DecAssets；改根后已登记来源要跟着平移。
      const authorRoot = page.getByLabel('作者目录基准点')
      await expect(authorRoot).toHaveValue('DecAssets')
      await authorRoot.fill('ProductAssets')
      await authorRoot.blur()
      await expect(page.getByText('ProductAssets/skills', { exact: false }).first()).toBeVisible()
      await expect(page.getByRole('button', { name: '返回' })).toBeVisible()
    },
  },
  {
    name: 'project-binding',
    scenario: 'typical',
    open: async (page) => {
      await connect(page)
      await nav(page, '项目')
      await page.getByRole('button', { name: /^Dec/ }).first().click()
      await page.getByRole('button', { name: /^家项目绑定/ }).click()
      await expect(page.getByRole('heading', { name: '家项目绑定' })).toBeVisible()
      await page.getByRole('button', { name: '读取私仓项目列表' }).click()
      await expect(page.getByText('绑定为家项目')).toBeVisible()
      await expect(page.getByRole('button', { name: '返回' })).toBeVisible()
    },
  },
  {
    name: 'project-writeback',
    scenario: 'typical',
    open: async (page) => {
      await connect(page)
      await nav(page, '项目')
      await page.getByRole('button', { name: /^Dec/ }).first().click()
      await page.getByRole('button', { name: /^写回与密钥/ }).click()
      await expect(page.getByRole('heading', { name: '写回与密钥' })).toBeVisible()
      await page.getByRole('button', { name: '预览写回' }).click()
      await expect(page.getByText('p/dec/public/project/skills/release/SKILL.md')).toBeVisible()
      await page.getByRole('button', { name: '列出密钥' }).click()
      await expect(page.getByText('.secrets/relkit/.env/upload.env')).toBeVisible()
      await expect(page.getByRole('button', { name: '返回' })).toBeVisible()
    },
  },
  {
    name: 'global-writeback',
    scenario: 'typical',
    open: async (page) => {
      await connect(page)
      await nav(page, 'Global 资产')
      await page.getByRole('button', { name: /^写回与密钥/ }).click()
      await expect(page.getByRole('heading', { name: '写回与密钥' })).toBeVisible()
      await page.getByRole('button', { name: '预览写回' }).click()
      // 平面参数传错就会拿到项目那份预览，这里按路径认平面。
      await expect(page.getByText('p/dec/private/user/env/machine.env')).toBeVisible()
    },
  },
  {
    name: 'sync',
    scenario: 'typical',
    open: async (page) => {
      await connect(page)
      await nav(page, '项目')
      await page.getByRole('button', { name: /^Dec/ }).first().click()
      await page.getByRole('main').getByRole('button', { name: '更新', exact: true }).click()
      await expect(page.getByRole('heading', { name: '更新' })).toBeVisible()
      // 更新页跨工作区列出依赖；从项目页进入时默认勾选当前项目的可用更新。
      await expect(page.getByText('Dec', { exact: true }).last()).toBeVisible()
      await expect(page.getByText('relkit', { exact: true }).last()).toBeVisible()
      await page.getByRole('button', { name: '预览更新' }).click()
      await expect(page.getByText(/未安装 → v0\.4\.2/)).toBeVisible()
      await expect(page.getByRole('button', { name: /确认更新 1 项/ })).toBeVisible()
      await expect(page.getByRole('button', { name: 'Push' })).toHaveCount(0)
    },
  },
  {
    name: 'sync-empty',
    scenario: 'typical',
    open: async (page) => {
      await connect(page)
      await nav(page, '更新')
      await expect(page.getByRole('heading', { name: '更新' })).toBeVisible()
    },
  },
  {
    name: 'delete',
    scenario: 'typical',
    open: async (page) => {
      await connect(page)
      await nav(page, '删除')
      await page.getByRole('button', { name: '列出库存' }).click()
      // 两个分区都要渲染出来，别把本机项混进远端区。
      await expect(page.getByText('远端（改 Bitwarden 与私仓）')).toBeVisible()
      await expect(page.getByText('本机（只清这台设备）')).toBeVisible()
      // 勾一个远端项后，本机分区必须锁住：服务端拒绝混选，UI 不该等它报错。
      await page.getByRole('checkbox', { name: '.secrets/relkit/.env/upload.env' }).check()
      await expect(page.getByText('已锁定，先清空另一个分区')).toBeVisible()
      // 没输确认词之前删除按钮保持禁用。
      await expect(page.getByRole('button', { name: /删除选中项/ })).toBeDisabled()
      await page.getByLabel('删除确认').fill('删除')
      await expect(page.getByRole('button', { name: /删除选中项/ })).toBeEnabled()
    },
  },
  {
    name: 'delete-empty',
    scenario: 'typical',
    open: async (page) => {
      await connect(page)
      await nav(page, '删除')
      await expect(page.getByRole('heading', { name: '删除' })).toBeVisible()
    },
  },
  {
    name: 'settings',
    scenario: 'typical',
    open: async (page) => {
      await connect(page)
      await nav(page, '设置')
      await expect(page.getByRole('heading', { name: '设置' })).toBeVisible()
    },
  },
  {
    name: 'settings-extreme',
    scenario: 'extreme',
    open: async (page) => {
      await connect(page)
      await nav(page, '设置')
      await expect(page.getByRole('heading', { name: '设置' })).toBeVisible()
    },
  },
]
