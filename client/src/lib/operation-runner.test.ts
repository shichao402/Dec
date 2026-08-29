import { describe, expect, it, vi } from 'vitest'
import { runOrWatch, type OperationTransport } from './operation-runner'

const response = (value: unknown) => ({
  result_json: JSON.stringify(value),
  error: '',
})

describe('operation runner', () => {
  it('starts a new operation when the workspace is idle', async () => {
    const transport: OperationTransport = {
      getActive: vi.fn().mockResolvedValue({ active: false }),
      watch: vi.fn(),
      start: vi.fn().mockResolvedValue(response({ PulledCount: 2 })),
    }
    const result = await runOrWatch<{ PulledCount: number }>(transport, {
      actionKey: 'pull:a',
      operation: 'pull',
      projectRoot: 'a',
      workspacePlane: 'local',
    })
    expect(result.PulledCount).toBe(2)
    expect(transport.start).toHaveBeenCalledWith('pull', 'a', 'local', undefined, 'pull:a')
    expect(transport.watch).not.toHaveBeenCalled()
  })

  it('reattaches to a matching active operation', async () => {
    const transport: OperationTransport = {
      getActive: vi.fn().mockResolvedValue({
        active: true,
        operationId: 'op-1',
        operation: 'scan_managed_projects',
      }),
      watch: vi.fn().mockResolvedValue(response({ Projects: ['a'] })),
      start: vi.fn(),
    }
    const result = await runOrWatch<{ Projects: string[] }>(transport, {
      actionKey: 'scan:a',
      operation: 'scan_managed_projects',
    })
    expect(result.Projects).toEqual(['a'])
    expect(transport.watch).toHaveBeenCalledWith('', 'op-1', 'scan:a', 'scan_managed_projects')
    expect(transport.start).not.toHaveBeenCalled()
  })

  it('does not parse another operation as the requested result type', async () => {
    const transport: OperationTransport = {
      getActive: vi.fn().mockResolvedValue({
        active: true,
        operationId: 'op-2',
        operation: 'pull',
      }),
      watch: vi.fn(),
      start: vi.fn(),
    }
    await expect(runOrWatch(transport, {
      actionKey: 'scan:a',
      operation: 'scan_managed_projects',
    })).rejects.toThrow('正在执行 pull')
  })
})
