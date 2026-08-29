import { invoke } from '@tauri-apps/api/core'
import type { InvokeResult, PingInfo, SavedConnection } from '@/lib/utils'

export async function listConnections() {
  return invoke<SavedConnection[]>('list_connections')
}

export async function saveConnection(conn: SavedConnection) {
  return invoke<SavedConnection>('save_connection', { conn })
}

export async function deleteConnection(id: string) {
  return invoke('delete_connection', { id })
}

export async function connectTarget(input: {
  kind: string
  host: string
  port: number
  sshHost: string
  sshUser: string
}) {
  return invoke<PingInfo>('connect_target', {
    kind: input.kind,
    host: input.host,
    port: input.port,
    sshHost: input.sshHost,
    sshUser: input.sshUser,
  })
}

export async function disconnect() {
  return invoke('disconnect')
}

export async function pingServer() {
  return invoke<PingInfo>('ping_server')
}

export async function authenticate(email: string, password: string, totp: string, rememberDevice: boolean) {
  return invoke<{
    unlocked: boolean
    need_2fa: boolean
    control_token: string
    expires_in_ms: number
    error: string
  }>('authenticate', { email, password, totp, rememberDevice })
}

export async function invokeMethod(
  method: string,
  projectRoot: string,
  workspacePlane: string,
  payload: unknown = {},
) {
  return invoke<InvokeResult>('invoke_method', {
    method,
    projectRoot,
    workspacePlane,
    payloadJson: JSON.stringify(payload ?? {}),
  })
}

export async function runOperation(
  operation: string,
  projectRoot: string,
  workspacePlane: string,
  payload: unknown = {},
) {
  return invoke<InvokeResult>('run_operation', {
    operation,
    projectRoot,
    workspacePlane,
    payloadJson: JSON.stringify(payload ?? {}),
  })
}
