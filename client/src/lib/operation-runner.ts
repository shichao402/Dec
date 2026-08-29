import type { InvokeResult } from '@/lib/utils'

export type ActiveOperation = {
  active: boolean
  operationId?: string
  operation?: string
  facade?: string
  clientId?: string
  startedAtUnixMs?: number
}

export type OperationInput = {
  actionKey: string
  operation: string
  projectRoot?: string
  workspacePlane?: 'local' | 'global'
  payload?: unknown
}

export type OperationTransport = {
  getActive: (projectRoot: string) => Promise<ActiveOperation>
  watch: (
    projectRoot: string,
    operationId: string,
    actionKey: string,
    operation: string,
  ) => Promise<InvokeResult>
  start: (
    operation: string,
    projectRoot: string,
    workspacePlane: 'local' | 'global',
    payload: unknown,
    actionKey: string,
  ) => Promise<InvokeResult>
}

export async function runOrWatch<T>(
  transport: OperationTransport,
  input: OperationInput,
) {
  const projectRoot = input.projectRoot || ''
  const active = await transport.getActive(projectRoot)
  let result: InvokeResult
  if (active.active && active.operationId) {
    if (active.operation && active.operation !== input.operation) {
      throw new Error(`该工作区正在执行 ${active.operation}，不能同时启动 ${input.operation}`)
    }
    result = await transport.watch(
      projectRoot,
      active.operationId,
      input.actionKey,
      input.operation,
    )
  } else {
    result = await transport.start(
      input.operation,
      projectRoot,
      input.workspacePlane || 'local',
      input.payload,
      input.actionKey,
    )
  }
  if (result.error) throw new Error(result.error)
  return (result.result_json ? JSON.parse(result.result_json) : {}) as T
}
