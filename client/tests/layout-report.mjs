// 把 playwright 的 JSON 结果压成「规则 × 用例」的清单，便于一眼看出问题聚在哪。
// 用法：npx playwright test --reporter=json > .layout-report.json; node tests/layout-report.mjs .layout-report.json
import { readFileSync } from 'node:fs'

const path = process.argv[2] || '.layout-report.json'
const report = JSON.parse(readFileSync(path, 'utf8'))
const rows = []

const walk = (suites, trail = []) => {
  for (const suite of suites || []) {
    const next = suite.title ? [...trail, suite.title] : trail
    for (const spec of suite.specs || []) {
      for (const test of spec.tests || []) {
        for (const result of test.results || []) {
          const attachment = (result.attachments || []).find((item) => item.contentType === 'application/json')
          if (!attachment?.body) continue
          const payload = JSON.parse(Buffer.from(attachment.body, 'base64').toString('utf8'))
          for (const finding of payload.findings || []) {
            rows.push({ case: [...next, spec.title].join(' / '), ...finding })
          }
        }
      }
    }
    walk(suite.suites, next)
  }
}

walk(report.suites)

const byRule = new Map()
for (const row of rows) {
  const list = byRule.get(row.rule) || []
  list.push(row)
  byRule.set(row.rule, list)
}

for (const [rule, list] of [...byRule.entries()].sort((a, b) => b[1].length - a[1].length)) {
  const errors = list.filter((row) => row.severity === 'error').length
  console.log(`\n## ${rule}  共 ${list.length}（error ${errors}）`)
  const seen = new Set()
  for (const row of list) {
    const key = `${row.case}|${row.detail}`
    if (seen.has(key)) continue
    seen.add(key)
    console.log(`  ${row.case}\n    ${row.detail}\n    ${row.target}`)
  }
}
