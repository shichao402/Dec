import { invoke } from '@tauri-apps/api/core'
import { listen } from '@tauri-apps/api/event'
import type { InvokeResult, PingInfo, SavedConnection } from '@/lib/utils'
import type { OperationEvent } from '@/lib/utils'
import { runOrWatch, type ActiveOperation, type OperationInput } from '@/lib/operation-runner'

export async function listConnections() {
  return invoke<SavedConnection[]>('list_connections')
}

export async function saveConnection(conn: SavedConnection, password?: string) {
  return invoke<SavedConnection>('save_connection', { conn, password })
}

export async function deleteConnection(id: string) {
  return invoke('delete_connection', { id })
}

export async function loadSavedPassword(id: string) {
  return invoke<string>('load_saved_password', { id })
}

export async function probeRemoteHost<T>(sshTarget: string) {
  const result = await invoke<InvokeResult>('probe_remote_host', { sshTarget })
  if (result.error) throw new Error(result.error)
  return JSON.parse(result.result_json) as T
}

export async function provisionRemoteHost<T>(input: {
  alias: string
  sshTarget: string
  confirm: string
  actionKey: string
}) {
  const result = await invoke<InvokeResult>('provision_remote_host', {
    alias: input.alias,
    sshTarget: input.sshTarget,
    confirm: input.confirm,
    actionKey: input.actionKey,
  })
  if (result.error) throw new Error(result.error)
  return JSON.parse(result.result_json) as T
}

export async function connectTarget(input: {
  kind: string
  host: string
  port: number
  sshHost: string
  sshUser: string
  tls: boolean
  tlsServerName: string
}) {
  return invoke<PingInfo>('connect_target', {
    kind: input.kind,
    host: input.host,
    port: input.port,
    sshHost: input.sshHost,
    sshUser: input.sshUser,
    tls: input.tls,
    tlsServerName: input.tlsServerName,
  })
}

export async function invokeTyped<T>(
  method: string,
  projectRoot = '',
  workspacePlane: 'local' | 'global' = 'local',
  payload: unknown = {},
  actionKey = '',
) {
  const result = await invokeMethod(method, projectRoot, workspacePlane, payload, actionKey)
  if (result.error) throw new Error(result.error)
  return (result.result_json ? JSON.parse(result.result_json) : {}) as T
}

export async function disconnect() {
  return invoke('disconnect')
}

export async function stopService() {
  return invoke('stop_service')
}

export async function runTyped<T>(
  operation: string,
  projectRoot = '',
  workspacePlane: 'local' | 'global' = 'local',
  payload: unknown = {},
  actionKey = '',
) {
  const result = await runOperation(operation, projectRoot, workspacePlane, payload, actionKey)
  if (result.error) throw new Error(result.error)
  return (result.result_json ? JSON.parse(result.result_json) : {}) as T
}

export async function getActiveOperation(projectRoot: string) {
  return invoke<ActiveOperation>('get_active_operation', { projectRoot })
}

export async function watchOperation(
  projectRoot: string,
  operationId: string,
  actionKey = '',
  operation = '',
) {
  return invoke<InvokeResult>('watch_operation', { projectRoot, operationId, actionKey, operation })
}

export async function runOrWatchTyped<T>(input: OperationInput) {
  return runOrWatch<T>(
    {
      getActive: getActiveOperation,
      watch: watchOperation,
      start: runOperation,
    },
    input,
  )
}

export function onOperationEvent(handler: (event: OperationEvent) => void) {
  return listen<OperationEvent>('operation-event', ({ payload }) => handler(payload))
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
  actionKey = '',
) {
  return invoke<InvokeResult>('invoke_method', {
    method,
    projectRoot,
    workspacePlane,
    payloadJson: JSON.stringify(payload ?? {}),
    actionKey,
  })
}

export async function runOperation(
  operation: string,
  projectRoot: string,
  workspacePlane: string,
  payload: unknown = {},
  actionKey = '',
) {
  return invoke<InvokeResult>('run_operation', {
    operation,
    projectRoot,
    workspacePlane,
    payloadJson: JSON.stringify(payload ?? {}),
    actionKey,
  })
}
