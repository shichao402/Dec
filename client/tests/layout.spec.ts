import { expect, test } from '@playwright/test'
import { cases, prepare, viewports } from './cases'
import { probeLayout, type Finding, type ProbeOptions } from './layout/probe'

const options: ProbeOptions = {
  deadSpaceRatio: 0.15,
  widthUsageRatio: 0.8,
  containerUsageRatio: 0.6,
  nestedScrollShare: 0.6,
  minTargetSize: 20,
}

function format(findings: Finding[]) {
  return findings
    .map((finding) => `[${finding.severity}] ${finding.rule}: ${finding.detail}\n    ${finding.target}`)
    .join('\n')
}

for (const viewport of viewports) {
  test.describe(viewport.name, () => {
    for (const item of cases) {
      test(item.name, async ({ page }, testInfo) => {
        await prepare(page, item, viewport)

        const report = await page.evaluate(probeLayout, options)
        const ignore = new Set(item.ignore || [])
        const kept = report.findings.filter((finding) => !ignore.has(finding.rule))
        const errors = kept.filter((finding) => finding.severity === 'error')

        await testInfo.attach(`${item.name}-${viewport.name}.json`, {
          body: JSON.stringify({ metrics: report.metrics, findings: kept }, null, 2),
          contentType: 'application/json',
        })

        expect(errors, `${item.name} @ ${viewport.name}\n${format(kept)}`).toEqual([])
      })
    }
  })
}
