import { describe, expect, it } from 'vitest'
import {
  actionConflict,
  actionRegistryReducer,
  actionStartDecision,
  emptyActionRegistry,
  nextGeneration,
  runningActions,
  type ActionSpec,
} from './action-registry'

const read = (key: string, resources = ['workspace:a']): ActionSpec => ({
  key,
  label: key,
  deviceId: 'device-1',
  resources,
  kind: 'read',
})

describe('action registry', () => {
  it('allows reads together and blocks writes on the same resource', () => {
    let state = actionRegistryReducer(emptyActionRegistry, {
      type: 'start',
      spec: read('load-a'),
      generation: 1,
      at: 1,
    })
    const running = runningActions(state)
    expect(actionConflict(read('load-b'), running)).toBeUndefined()
    expect(actionConflict({ ...read('save-a'), kind: 'write' }, running)?.key).toBe('load-a')
    expect(actionConflict(read('other', ['workspace:b']), running)).toBeUndefined()
  })

  it('makes session actions exclusive only within their device', () => {
    const session: ActionSpec = {
      key: 'connect',
      label: 'connect',
      deviceId: 'device-1',
      resources: ['session'],
      kind: 'session',
    }
    const state = actionRegistryReducer(emptyActionRegistry, {
      type: 'start',
      spec: session,
      generation: 1,
      at: 1,
    })
    expect(actionConflict(read('load'), runningActions(state))?.key).toBe('connect')
    expect(
      actionConflict({ ...read('other-device'), deviceId: 'device-2' }, runningActions(state)),
    ).toBeUndefined()
  })

  it('ignores stale generations and retains the latest result', () => {
    let state = actionRegistryReducer(emptyActionRegistry, {
      type: 'start',
      spec: read('load'),
      generation: 1,
      at: 1,
    })
    state = actionRegistryReducer(state, {
      type: 'start',
      spec: read('load'),
      generation: nextGeneration(state, 'load'),
      at: 2,
    })
    state = actionRegistryReducer(state, {
      type: 'succeed',
      key: 'load',
      generation: 1,
      result: 'stale',
      at: 3,
    })
    expect(state.records.load.status).toBe('running')
    state = actionRegistryReducer(state, {
      type: 'succeed',
      key: 'load',
      generation: 2,
      result: 'fresh',
      at: 4,
    })
    expect(state.records.load.result).toBe('fresh')
  })

  it('deduplicates repeated actions but starts a new forced read generation', () => {
    const spec = read('load')
    const state = actionRegistryReducer(emptyActionRegistry, {
      type: 'start',
      spec,
      generation: 1,
      at: 1,
    })
    expect(actionStartDecision(state, spec).type).toBe('dedupe')
    expect(actionStartDecision(state, spec, true)).toEqual({ type: 'start', generation: 2 })
  })

  it('keeps unrelated browsing available while pull locks its workspace', () => {
    const pull: ActionSpec = {
      key: 'pull:a',
      label: 'pull',
      deviceId: 'device-1',
      resources: ['workspace:a'],
      kind: 'operation',
    }
    const state = actionRegistryReducer(emptyActionRegistry, {
      type: 'start',
      spec: pull,
      generation: 1,
      at: 1,
    })
    expect(actionStartDecision(state, read('browse', ['device:filesystem'])).type).toBe('start')
    expect(actionStartDecision(state, read('assets', ['workspace:a'])).type).toBe('blocked')
  })

  it('keeps progress and completion after the initiating consumer disappears', () => {
    let state = actionRegistryReducer(emptyActionRegistry, {
      type: 'start',
      spec: { ...read('scan'), kind: 'operation' },
      generation: 1,
      at: 1,
    })
    state = actionRegistryReducer(state, {
      type: 'progress',
      key: 'scan',
      generation: 1,
      operationId: 'op-1',
      event: {
        level: 'info',
        scope: 'projects.scan',
        message: 'scanning',
        timeUnixMs: 2,
        progress: { phase: 'scan', current: 2, total: 5 },
      },
    })
    state = actionRegistryReducer(state, {
      type: 'succeed',
      key: 'scan',
      generation: 1,
      result: { Projects: ['a'] },
      at: 3,
    })
    expect(state.records.scan.operationId).toBe('op-1')
    expect(state.records.scan.progress?.current).toBe(2)
    expect(state.records.scan.status).toBe('succeeded')
  })
})
