// Provider 与配套 hooks 必须共享同一个私有 Context，保持在一处可避免循环依赖。
// oxlint-disable react/only-export-components
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useReducer,
  useRef,
  type ReactNode,
} from 'react'
import {
  actionConflict,
  actionRegistryReducer,
  actionStartDecision,
  emptyActionRegistry,
  runningActions,
  type ActionRecord,
  type ActionRegistryEvent,
  type ActionRegistryState,
  type ActionSpec,
} from '@/lib/action-registry'
import { getActiveOperation, onOperationEvent, watchOperation } from '@/lib/api'
import { describeServiceError } from '@/lib/utils'

export type ActionOutcome<T> =
  | { ok: true; value: T; generation: number }
  | { ok: false; error: string; generation?: number; blocked?: boolean }

type RunOptions = {
  force?: boolean
}

type ActionContextValue = {
  state: ActionRegistryState
  run: <T>(
    spec: ActionSpec,
    task: () => Promise<T>,
    options?: RunOptions,
  ) => Promise<ActionOutcome<T>>
  dismiss: (key: string) => void
  clearDevice: (deviceId: string) => void
  blockedBy: (spec: ActionSpec) => ActionRecord | undefined
}

const ActionContext = createContext<ActionContextValue | null>(null)

export function ActionProvider({ children }: { children: ReactNode }) {
  const [state, reactDispatch] = useReducer(actionRegistryReducer, emptyActionRegistry)
  const stateRef = useRef(state)
  const promises = useRef(new Map<string, Promise<ActionOutcome<unknown>>>())

  const dispatch = useCallback((event: ActionRegistryEvent) => {
    stateRef.current = actionRegistryReducer(stateRef.current, event)
    reactDispatch(event)
  }, [])

  useEffect(() => {
    const pending = onOperationEvent((event) => {
      if (!event.actionKey) return
      const record = stateRef.current.records[event.actionKey]
      if (!record || record.status !== 'running') return
      dispatch({
        type: 'progress',
        key: event.actionKey,
        generation: record.generation,
        event,
        operationId: event.operationId,
      })
    })
    return () => {
      void pending.then((unlisten) => unlisten())
    }
  }, [dispatch])

  const run = useCallback(
    <T,>(
      spec: ActionSpec,
      task: () => Promise<T>,
      options: RunOptions = {},
    ): Promise<ActionOutcome<T>> => {
      const decision = actionStartDecision(stateRef.current, spec, options.force)
      if (decision.type === 'dedupe') {
        const existing = promises.current.get(spec.key)
        if (existing) return existing as Promise<ActionOutcome<T>>
        return Promise.resolve({
          ok: false,
          blocked: true,
          error: `${decision.record.label}正在执行`,
        })
      }
      if (decision.type === 'blocked') {
        return Promise.resolve({
          ok: false,
          blocked: true,
          error: `需等待“${decision.record.label}”完成`,
        })
      }

      const generation = decision.generation
      dispatch({ type: 'start', spec, generation, at: Date.now() })
      const promise = (async (): Promise<ActionOutcome<T>> => {
        try {
          const value = await task()
          dispatch({
            type: 'succeed',
            key: spec.key,
            generation,
            result: value,
            at: Date.now(),
          })
          return { ok: true, value, generation }
        } catch (reason) {
          const error = describeServiceError(reason)
          dispatch({
            type: 'fail',
            key: spec.key,
            generation,
            error,
            at: Date.now(),
          })
          return { ok: false, error, generation }
        } finally {
          if (stateRef.current.records[spec.key]?.generation === generation) {
            promises.current.delete(spec.key)
          }
        }
      })()
      promises.current.set(spec.key, promise as Promise<ActionOutcome<unknown>>)
      return promise
    },
    [dispatch],
  )

  const value = useMemo<ActionContextValue>(
    () => ({
      state,
      run,
      dismiss: (key) => dispatch({ type: 'dismiss', key }),
      clearDevice: (deviceId) => dispatch({ type: 'clear-device', deviceId }),
      blockedBy: (spec) => actionConflict(spec, runningActions(state)),
    }),
    [dispatch, run, state],
  )

  return <ActionContext.Provider value={value}>{children}</ActionContext.Provider>
}

export function useActionRegistry() {
  const context = useContext(ActionContext)
  if (!context) throw new Error('useActionRegistry 必须在 ActionProvider 内使用')
  return context
}

export function useDecAction<T>(spec: ActionSpec) {
  const registry = useActionRegistry()
  const record = registry.state.records[spec.key] as ActionRecord<T> | undefined
  const conflict = registry.blockedBy(spec)
  return {
    record,
    running: record?.status === 'running',
    blocked: Boolean(conflict),
    blockedBy: conflict,
    run: (task: () => Promise<T>, options?: RunOptions) => registry.run(spec, task, options),
    dismiss: () => registry.dismiss(spec.key),
  }
}

export function useOperationObserver(
  deviceId: string,
  roots: string[],
  enabled: boolean,
) {
  const { run } = useActionRegistry()
  const polling = useRef(false)
  const rootsKey = JSON.stringify([...new Set(roots)])

  useEffect(() => {
    if (!enabled || !deviceId) return
    let disposed = false
    const observedRoots = JSON.parse(rootsKey) as string[]

    const poll = async () => {
      if (disposed || polling.current) return
      polling.current = true
      try {
        for (const root of observedRoots) {
          if (disposed) return
          const active = await getActiveOperation(root)
          if (!active.active || !active.operationId || !active.operation) continue
          const resource = root ? `workspace:${root}` : 'workspace:global'
          const spec: ActionSpec = {
            key: `observe:${deviceId}:${root || 'global'}`,
            label: `旁观 ${active.operation}`,
            deviceId,
            resources: [resource],
            kind: 'operation',
            successMessage: `${active.operation} 已完成`,
          }
          void run(spec, async () => {
            const result = await watchOperation(
              root,
              active.operationId!,
              spec.key,
              active.operation,
            )
            if (result.error) throw new Error(result.error)
            return result.result_json ? JSON.parse(result.result_json) : {}
          })
        }
      } finally {
        polling.current = false
      }
    }

    void poll()
    const timer = window.setInterval(() => void poll(), 1500)
    return () => {
      disposed = true
      window.clearInterval(timer)
    }
  }, [deviceId, enabled, rootsKey, run])
}
