import type { ActionSpec } from '@/lib/action-registry'
import type { SavedConnection } from '@/lib/utils'

// binding / overrides / provides 是 project 的下级页面，writeback 是 project 与 global 共用的下级页面：
// 都不进侧栏导航，只能从上级页的入口卡进入。
export type View =
  | 'overview'
  | 'global'
  | 'projects'
  | 'project'
  | 'binding'
  | 'overrides'
  | 'provides'
  | 'writeback'
  | 'sync'
  | 'delete'
  | 'settings'

// 下级页没有自己的导航项，高亮留在它所属的上级导航上。
export function navView(view: View, writebackPlane?: 'local' | 'global'): View {
  if (view === 'binding' || view === 'overrides' || view === 'provides') return 'project'
  if (view === 'writeback') return writebackPlane === 'global' ? 'global' : 'project'
  return view
}

export const resource = {
  connections: 'console:connections',
  consoleUpdate: 'console:update',
  session: 'session',
  global: 'workspace:global',
  workspace: (root: string) => (root ? `workspace:${root}` : 'workspace:global'),
  filesystem: 'device:filesystem',
  device: (target: string) => `device:${target.trim().toLowerCase()}`,
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
  if (conn.kind === 'ssh') return `${conn.ssh_host} → 127.0.0.1:47653`
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
// 驼峰边界要留成连字符：抹平后的 cppsvnauthoranalysis 人根本读不出来。
export function suggestProjectName(root: string) {
  return pathTail(root)
    .replace(/([a-z0-9])([A-Z])/g, '$1-$2')
    .replace(/([A-Z]+)([A-Z][a-z])/g, '$1-$2')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
}

export function toggle(items: string[], value: string) {
  return items.includes(value) ? items.filter((item) => item !== value) : [...items, value]
}

export const PROJECT_TAG_GLOBAL = 'global'

export function hasTag(tags: string[] | undefined, tag: string) {
  return (tags || []).includes(tag)
}

export function toggleTag(tags: string[] | undefined, tag: string) {
  return toggle(tags || [], tag)
}
