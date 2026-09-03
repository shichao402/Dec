import { listen } from '@tauri-apps/api/event'
import { invoke } from '@tauri-apps/api/core'
import type { SavedConnection } from '@/lib/utils'

export type OpenIntent = 'unlock-local'

export const defaultLocalConnection = (): SavedConnection => ({
  id: 'local',
  label: '本机',
  kind: 'local',
  host: '127.0.0.1',
  port: 47653,
  ssh_host: '',
  ssh_user: '',
  tls: false,
  tls_server_name: '',
  auth_email: '',
  password_saved: false,
})

export function selectLocalConnection(connections: SavedConnection[]): SavedConnection {
  return connections.find((connection) => connection.kind === 'local') || defaultLocalConnection()
}

export function takeOpenIntent() {
  return invoke<OpenIntent | null>('take_open_intent')
}

export function onOpenIntent(handler: () => void) {
  return listen('open-intent', handler)
}
