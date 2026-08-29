import { describe, expect, it } from 'vitest'
import {
  cn,
  describeServiceError,
  isStaleServiceError,
  pullResultDiagnosis,
  pullResultIssues,
} from './utils'
import type { PullResult } from './utils'

describe('cn', () => {
  it('merges tailwind classes', () => {
    expect(cn('px-2', 'px-4')).toBe('px-4')
  })
})

describe('describeServiceError', () => {
  const stale =
    'status: Internal, message: "未知服务方法 \\"load_device_summary\\"", details: [], metadata: MetadataMap { headers: {"content-type": "application/grpc"} }'

  it('turns an unknown method status into a restart hint', () => {
    const text = describeServiceError(new Error(stale))
    expect(text).toContain('load_device_summary')
    expect(text).toContain('重启服务')
    expect(text).toContain(stale)
    expect(isStaleServiceError(stale)).toBe(true)
  })

  it('also covers unknown operations', () => {
    expect(isStaleServiceError('未知操作 "scan_managed_projects"')).toBe(true)
  })

  it('leaves unrelated errors untouched', () => {
    expect(describeServiceError('status: Unauthenticated')).toBe('status: Unauthenticated')
    expect(isStaleServiceError('status: Unauthenticated')).toBe(false)
  })
})

describe('pullResultIssues', () => {
  it('keeps missing items and warnings in the structured result', () => {
    const result = {
      MissingProjects: ['project-a'],
      MissingBundles: ['legacy-bundle'],
      ValidationWarnings: ['invalid path'],
      NonFatalWarnings: ['offline cache'],
    } as PullResult
    expect(pullResultIssues(result)).toEqual({
      missing: ['project-a', 'legacy-bundle'],
      warnings: ['invalid path', 'offline cache'],
    })
  })

  it('reports a name listed as both a missing project and a missing bundle only once', () => {
    const result = {
      MissingProjects: ['AgentsHelpMe'],
      MissingBundles: ['AgentsHelpMe'],
    } as PullResult
    expect(pullResultIssues(result).missing).toEqual(['AgentsHelpMe'])
  })
})

describe('pullResultDiagnosis', () => {
  const missingProject = {
    SkippedReason: '没有有效的已启用 Git 资产可拉取（仍尝试同步 secrets）',
    MissingProjects: ['AgentsHelpMe'],
    MissingBundles: ['AgentsHelpMe'],
    NonFatalWarnings: [
      '直接 requires 中有 1 个项目不存在：AgentsHelpMe',
      '项目选择里有 1 个项目在仓库中已不存在：AgentsHelpMe（本次忽略；到 Bundles 页重新保存即可清掉）',
      '项目 "AgentsHelpMe" 不存在，不能声明 secrets target',
      '离线缓存已过期',
    ],
  } as PullResult

  it('collapses the repeated missing-project statements into one actionable headline', () => {
    const diagnosis = pullResultDiagnosis(missingProject)
    expect(diagnosis.headline).toContain('AgentsHelpMe')
    expect(diagnosis.headline).toContain('项目资产')
    expect(diagnosis.missing).toEqual([])
    expect(diagnosis.skipped).toBe('')
    expect(diagnosis.warnings).toEqual(['离线缓存已过期'])
  })

  it('leaves an unrelated skip reason and its warnings alone', () => {
    const diagnosis = pullResultDiagnosis({
      SkippedReason: '未启用 bundle',
      NonFatalWarnings: ['离线缓存已过期'],
    } as PullResult)
    expect(diagnosis.headline).toBe('')
    expect(diagnosis.skipped).toBe('未启用 bundle')
    expect(diagnosis.warnings).toEqual(['离线缓存已过期'])
  })
})
