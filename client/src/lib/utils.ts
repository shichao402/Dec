import type { ClassValue } from 'clsx'
import { clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export type SavedConnection = {
  id: string
  label: string
  kind: 'local' | 'remote' | 'ssh'
  host: string
  port: number
  ssh_host: string
  ssh_user: string
  tls: boolean
  tls_server_name: string
  auth_email: string
  password_saved: boolean
}

export type PingInfo = {
  version: string
  instance_id: string
  unlocked: boolean
}

export type RemoteHostProbe = {
  Target: string
  Reachable: boolean
  SSHError: string
  OS: string
  Arch: string
  Supported: boolean
  DecInstalled: boolean
  DecVersion: string
  MissingBinaries: string[]
  SpawnCapable: boolean
  ManagementListen: string
  ListenReady: boolean
  ServerRunning: boolean
  Blockers: string[]
  Warnings: string[]
  NextAction: string
}

export type ManagedDevice = {
  Alias: string
  SSHTarget: string
  ManagementListen: string
  Tags: string[]
  ProvisionedVersion: string
}

export type ProvisionRemoteResult = {
  Target: string
  Probe: RemoteHostProbe
  Verify: RemoteHostProbe
  Skipped: boolean
  InstalledVersion: string
  ChecksumVerified: boolean
  Warnings: string[]
  NextAction: string
  Device?: ManagedDevice
}

export type InvokeResult = {
  result_json: string
  error: string
}

export type ManagedProject = {
  Root: string
  Label: string
  Name: string
  Exists: boolean
  Initialized: boolean
  ConfigPath: string
  Error: string
}

export type DeviceSummary = {
  Initialized: boolean
  RepoConnected: boolean
  RepoURL: string
  HomeDir: string
  Platform: string
  Projects: ManagedProject[]
}

export type LocalCleanupItem = {
  Action: 'delete' | 'modify' | 'revoke'
  Category: string
  Path: string
  Detail: string
}

export type LocalCleanupPreview = {
  Items: LocalCleanupItem[]
  Preserved: string[]
}

export type LocalCleanupResult = {
  Deleted: string[]
  Modified: string[]
  Revoked: string[]
  Warnings: string[]
}

export type DirectoryListing = {
  Current: string
  Parent: string
  Home: string
  Roots: string[]
  Entries: { Name: string; Path: string }[]
}

export type AssetMember = {
  Name: string
  Type: string
  Vault: string
  Visibility: string
  Plane: string
}

export type AssetOption = {
  Name: string
  Description: string
  Vault: string
  Members: AssetMember[]
  Enabled: boolean
  SecretsOnly: boolean
  OtherPlane: boolean
  RemoteMissing: boolean
  RemoteUnverified: boolean
  Model: string
  Home: boolean
  Required: boolean
  Quadrants: Record<string, number>
}

export type AssetSelection = {
  ProjectRoot: string
  ConfigPath: string
  ExistingConfig: boolean
  Bundles: AssetOption[]
  Model: string
  Plane: string
}

export type GlobalSettings = {
  RepoConnected: boolean
  RepoURL: string
  ConnectedRepoURL: string
  AvailableIDEs: string[]
  SelectedIDEs: string[]
  EffectiveIDEs: string[]
  ConfiguredEditor: string
  ServerIdleTimeout: string
}

export type OperationEvent = {
  level: string
  scope: string
  message: string
  timeUnixMs: number
  actionKey?: string
  operationId?: string
  projectRoot?: string
  operation?: string
  progress?: { phase: string; current: number; total: number }
}

export type PullResult = {
  ProjectRoot: string
  RequestedCount: number
  PulledCount: number
  FailedCount: number
  SkippedReason: string
  MissingBundles: string[]
  MissingProjects: string[]
  ValidationWarnings: string[]
  NonFatalWarnings: string[]
  SecretsSkippedReason: string
  SecretsNoteCount: number
  SecretsSSHKeyCount: number
  EffectiveIDEs: string[]
  SelectedProjects: string[]
  RequiredProjects: string[]
  Quadrants: Record<string, number>
}

function uniqueTexts(items: (string | undefined)[]) {
  const seen = new Set<string>()
  for (const item of items) {
    const text = item?.trim()
    if (text) seen.add(text)
  }
  return [...seen]
}

export function pullResultIssues(result: PullResult) {
  return {
    missing: uniqueTexts([...(result.MissingProjects || []), ...(result.MissingBundles || [])]),
    warnings: uniqueTexts([...(result.ValidationWarnings || []), ...(result.NonFatalWarnings || [])]),
  }
}

// 一个「绑定的项目不在私仓里」的根因，后端会从 requires、项目选择和 secrets target
// 三个角度各报一次，加上跳过原因和缺失列表，同一件事在卡片上出现四五处，读的人
// 反而不知道到底哪里出了问题、下一步该做什么。这里收敛成一句带动作的结论。
export function pullResultDiagnosis(result: PullResult) {
  const { missing, warnings } = pullResultIssues(result)
  if (missing.length === 0) {
    return { headline: '', missing, warnings, skipped: result.SkippedReason || '' }
  }
  const restates = (warning: string) =>
    warning.includes('不存在') && missing.some((name) => warning.includes(name))
  const names = missing.map((name) => `“${name}”`).join('、')
  return {
    headline: `${names} 在私仓中不存在，所以这次没有资产可拉，secrets 也同步不了。到「项目资产」页确认绑定的项目名，或先在私仓里创建它。`,
    missing: [],
    warnings: warnings.filter((warning) => !restates(warning)),
    skipped: '',
  }
}

// 服务端文案经 gRPC status 转义后带反斜杠与引号，例如：未知服务方法 \"load_device_summary\"
const staleServicePattern = /(?:未知服务方法|未知操作)[\s\\"]*([\w.-]+)/

// 目标设备上跑着旧版 dec-server 时，服务端只会回一句「未知服务方法」。
// 这里换成能指向下一步动作的说明，并保留原始文本便于排查。
export function describeServiceError(reason: unknown) {
  const text = reason instanceof Error ? reason.message : String(reason)
  const matched = staleServicePattern.exec(text)
  if (!matched) return text
  const target = matched[1] ? `「${matched[1]}」` : '该能力'
  return `目标 dec-server 版本较旧，不认识${target}。在设备设置里重启服务后重连即可加载新版本。原始错误：${text}`
}

export function isStaleServiceError(reason: unknown) {
  const text = reason instanceof Error ? reason.message : String(reason)
  return staleServicePattern.test(text)
}

export function parseResult<T>(raw: InvokeResult): T {
  if (raw.error) {
    throw new Error(raw.error)
  }
  if (!raw.result_json) {
    return {} as T
  }
  return JSON.parse(raw.result_json) as T
}
