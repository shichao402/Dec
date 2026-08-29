import { test } from '@playwright/test'
import { mkdir } from 'node:fs/promises'
import { cases, prepare, shotViewports } from './cases'

// 人工过目用：把「页面 × 数据形态 × 视口」渲染成 PNG，一次扫完。
// 断言能判的部分交给 layout.spec.ts，这里只负责让审美判断有素材。
//   SHOTS=1 npx playwright test tests/shots.spec.ts
const enabled = process.env.SHOTS === '1'
const dir = process.env.SHOTS_DIR || '.shots'

test.describe('screenshots', () => {
  test.skip(!enabled, '设置 SHOTS=1 才输出截图')

  for (const viewport of shotViewports) {
    for (const item of cases) {
      test(`${item.name}-${viewport.name}`, async ({ page }) => {
        await mkdir(dir, { recursive: true })
        await prepare(page, item, viewport)
        await page.screenshot({ path: `${dir}/${item.name}-${viewport.name}.png` })
      })
    }
  }
})
