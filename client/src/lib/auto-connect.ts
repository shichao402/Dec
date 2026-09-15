import type { SavedConnection } from '@/lib/utils'

const LAST_CONNECTION_KEY = 'dec:last-connection-id'

export function selectAutoConnectConnection(
  connections: SavedConnection[],
  lastConnectionId: string,
): SavedConnection | null {
  const last = connections.find((connection) => connection.id === lastConnectionId)
  if (last?.password_saved) return last

  const withSavedPassword = connections.filter((connection) => connection.password_saved)
  return withSavedPassword.length === 1 ? withSavedPassword[0] : null
}

export function loadLastConnectionId(): string {
  try {
    return window.localStorage.getItem(LAST_CONNECTION_KEY) || ''
  } catch {
    return ''
  }
}

export function rememberLastConnection(id: string): void {
  try {
    window.localStorage.setItem(LAST_CONNECTION_KEY, id)
  } catch {
    // 系统禁用 WebView 存储时仍可手动连接，不让偏好记录阻断主流程。
  }
}
