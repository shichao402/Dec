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
    },
  },
  {
    name: 'global-assets-empty',
    scenario: 'empty',
    open: async (page) => {
      await connect(page)
      await nav(page, 'Global 资产')
      await expect(page.getByText('这个范围里还没有可选资产')).toBeVisible()
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
      await page.getByRole('button', { name: '接管目录' }).click()
      await expect(page.getByText('选择设备上的目录')).toBeVisible()
    },
  },
  {
    name: 'project',
    scenario: 'typical',
    open: async (page) => {
      await connect(page)
      await nav(page, '项目')
      await page.getByRole('button', { name: /^Dec/ }).first().click()
      await expect(page.getByRole('button', { name: '拉取到设备' })).toBeVisible()
    },
  },
  {
    name: 'project-extreme',
    scenario: 'extreme',
    open: async (page) => {
      await connect(page)
      await nav(page, '项目')
      await page.locator('main button').filter({ hasText: '腾讯云基础设施' }).first().click()
      await expect(page.getByRole('button', { name: '拉取到设备' })).toBeVisible()
    },
  },
  {
    name: 'project-binding',
    scenario: 'typical',
    open: async (page) => {
      await connect(page)
      await nav(page, '项目')
      await page.getByRole('button', { name: /^Dec/ }).first().click()
      await page.getByRole('button', { name: /家项目绑定/ }).click()
      await page.getByRole('button', { name: '读取私仓项目列表' }).click()
      await expect(page.getByText('绑定为家项目')).toBeVisible()
    },
  },
  {
    name: 'sync',
    scenario: 'typical',
    open: async (page) => {
      await connect(page)
      await page.getByRole('button', { name: /拉取 Global 资产/ }).click()
      await expect(page.getByRole('heading', { name: '同步记录' })).toBeVisible()
    },
  },
  {
    name: 'sync-empty',
    scenario: 'typical',
    open: async (page) => {
      await connect(page)
      await nav(page, '同步记录')
      await expect(page.getByRole('heading', { name: '同步记录' })).toBeVisible()
    },
  },
  {
    name: 'settings',
    scenario: 'typical',
    open: async (page) => {
      await connect(page)
      await nav(page, '设备设置')
      await expect(page.getByRole('heading', { name: '设备设置' })).toBeVisible()
    },
  },
  {
    name: 'settings-extreme',
    scenario: 'extreme',
    open: async (page) => {
      await connect(page)
      await nav(page, '设备设置')
      await expect(page.getByRole('heading', { name: '设备设置' })).toBeVisible()
    },
  },
]
