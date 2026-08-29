import type { ClassValue } from 'clsx'
import { clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export type SavedConnection = {
  id: string
  label: string
  kind: 'local' | 'remote' | 'ssh'
  host: string
  port: number
  ssh_host: string
  ssh_user: string
}

export type PingInfo = {
  version: string
  instance_id: string
  unlocked: boolean
}

export type InvokeResult = {
  result_json: string
  error: string
}

export function parseResult<T>(raw: InvokeResult): T {
  if (raw.error) {
    throw new Error(raw.error)
  }
  if (!raw.result_json) {
    return {} as T
  }
  return JSON.parse(raw.result_json) as T
}
