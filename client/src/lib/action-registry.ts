import type { OperationEvent } from '@/lib/utils'

export type ActionKind = 'read' | 'write' | 'session' | 'operation'
export type ActionStatus = 'running' | 'succeeded' | 'failed'

export type ActionSpec = {
  key: string
  label: string
  deviceId: string
  resources: string[]
  kind: ActionKind
  successMessage?: string
}

export type ActionRecord<T = unknown> = ActionSpec & {
  generation: number
  status: ActionStatus
  startedAt: number
  finishedAt?: number
  progress?: OperationEvent['progress']
  events: OperationEvent[]
  result?: T
  error?: string
  operationId?: string
  dismissed?: boolean
}

export type ActionRegistryState = {
  records: Record<string, ActionRecord>
  generations: Record<string, number>
}

export type ActionRegistryEvent =
  | { type: 'start'; spec: ActionSpec; generation: number; at: number }
  | { type: 'progress'; key: string; generation: number; event: OperationEvent; operationId?: string }
  | { type: 'succeed'; key: string; generation: number; result: unknown; at: number }
  | { type: 'fail'; key: string; generation: number; error: string; at: number }
  | { type: 'dismiss'; key: string }
  | { type: 'clear-device'; deviceId: string }

export const emptyActionRegistry: ActionRegistryState = {
  records: {},
  generations: {},
}

export function actionRegistryReducer(
  state: ActionRegistryState,
  event: ActionRegistryEvent,
): ActionRegistryState {
  switch (event.type) {
    case 'start':
      return {
        generations: { ...state.generations, [event.spec.key]: event.generation },
        records: {
          ...state.records,
          [event.spec.key]: {
            ...event.spec,
            generation: event.generation,
            status: 'running',
            startedAt: event.at,
            events: [],
          },
        },
      }
    case 'progress': {
      const current = currentGeneration(state, event.key, event.generation)
      if (!current) return state
      return {
        ...state,
        records: {
          ...state.records,
          [event.key]: {
            ...current,
            operationId: event.operationId || current.operationId,
            progress: event.event.progress || current.progress,
            events: [...current.events.slice(-49), event.event],
          },
        },
      }
    }
    case 'succeed': {
      const current = currentGeneration(state, event.key, event.generation)
      if (!current) return state
      return {
        ...state,
        records: {
          ...state.records,
          [event.key]: {
            ...current,
            status: 'succeeded',
            result: event.result,
            error: undefined,
            finishedAt: event.at,
          },
        },
      }
    }
    case 'fail': {
      const current = currentGeneration(state, event.key, event.generation)
      if (!current) return state
      return {
        ...state,
        records: {
          ...state.records,
          [event.key]: {
            ...current,
            status: 'failed',
            error: event.error,
            finishedAt: event.at,
          },
        },
      }
    }
    case 'dismiss': {
      const current = state.records[event.key]
      if (!current) return state
      return {
        ...state,
        records: {
          ...state.records,
          [event.key]: { ...current, dismissed: true },
        },
      }
    }
    case 'clear-device': {
      const records = Object.fromEntries(
        Object.entries(state.records).filter(([, record]) => record.deviceId !== event.deviceId),
      )
      return { ...state, records }
    }
  }
}

export function nextGeneration(state: ActionRegistryState, key: string) {
  return (state.generations[key] || 0) + 1
}

export function runningActions(state: ActionRegistryState) {
  return Object.values(state.records).filter((record) => record.status === 'running')
}

export function actionConflict(spec: ActionSpec, records: ActionRecord[]) {
  return records.find((record) => {
    if (record.status !== 'running' || record.deviceId !== spec.deviceId) return false
    if (record.key === spec.key) return true
    if (record.kind === 'session' || spec.kind === 'session') return true
    const overlaps = spec.resources.some((resource) => record.resources.includes(resource))
    if (!overlaps) return false
    return spec.kind !== 'read' || record.kind !== 'read'
  })
}

export function actionStartDecision(
  state: ActionRegistryState,
  spec: ActionSpec,
  force = false,
):
  | { type: 'start'; generation: number }
  | { type: 'dedupe'; record: ActionRecord }
  | { type: 'blocked'; record: ActionRecord } {
  const current = state.records[spec.key]
  if (current?.status === 'running' && !(force && spec.kind === 'read')) {
    return { type: 'dedupe', record: current }
  }
  const conflict = actionConflict(spec, runningActions(state))
  if (conflict && !(force && spec.kind === 'read' && conflict.key === spec.key)) {
    return { type: 'blocked', record: conflict }
  }
  return { type: 'start', generation: nextGeneration(state, spec.key) }
}

function currentGeneration(
  state: ActionRegistryState,
  key: string,
  generation: number,
) {
  const current = state.records[key]
  return current?.generation === generation ? current : undefined
}
