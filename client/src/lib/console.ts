import type { ActionSpec } from '@/lib/action-registry'
import type { SavedConnection } from '@/lib/utils'

export type View = 'overview' | 'global' | 'projects' | 'project' | 'sync' | 'settings'

export const resource = {
  connections: 'console:connections',
  session: 'session',
  global: 'workspace:global',
  workspace: (root: string) => (root ? `workspace:${root}` : 'workspace:global'),
  filesystem: 'device:filesystem',
}

export function actionSpec(
  key: string,
  label: string,
  deviceId: string,
  resources: string[],
  kind: ActionSpec['kind'],
  successMessage?: string,
): ActionSpec {
  return { key, label, deviceId: deviceId || 'console', resources, kind, successMessage }
}

export function connectionAddress(conn: SavedConnection) {
  if (conn.kind === 'local') return '本机 dec-server'
  if (conn.kind === 'ssh') return `${conn.ssh_user ? `${conn.ssh_user}@` : ''}${conn.ssh_host} → 127.0.0.1:${conn.port}`
  return `${conn.tls ? 'https' : 'http'}://${conn.host}:${conn.port}`
}

export function connectionKindLabel(kind: SavedConnection['kind']) {
  if (kind === 'local') return '本机'
  if (kind === 'ssh') return 'SSH 隧道'
  return 'TLS gRPC'
}

// 实例 id 是纳秒时间戳，整串放进顶栏只是噪声；这里留尾部特征位，完整值走 title。
export function shortInstanceId(id: string) {
  const text = id.trim()
  if (text.length <= 8) return text
  return `…${text.slice(-6)}`
}

export function pathTail(root: string) {
  return root.split(/[\\/]/).filter(Boolean).at(-1) || root
}

// 私仓项目名是小写 kebab-case，目录名通常是驼峰或含下划线，这里给一个可直接用的建议值。
export function suggestProjectName(root: string) {
  return pathTail(root).toLowerCase().replace(/[^a-z0-9]+/g, '')
}

export function toggle(items: string[], value: string) {
  return items.includes(value) ? items.filter((item) => item !== value) : [...items, value]
}
