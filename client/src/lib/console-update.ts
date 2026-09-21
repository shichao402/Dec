import { fromJson, type JsonValue } from '@bufbuild/protobuf'
import {
  CheckResultSchema,
  StatusSnapshotSchema,
  type CheckResult,
  type StatusSnapshot,
} from '@relkit/updater-bindings/updater/v1'

export type ConsoleUpdateEnvelope = {
  currentVersion: string
  channel: string
  canAutoInstall: boolean
  result: CheckResult
  status: StatusSnapshot
}

export function decodeConsoleUpdateEnvelope(value: {
  currentVersion: string
  channel: string
  canAutoInstall: boolean
  result: JsonValue
  status: JsonValue
}): ConsoleUpdateEnvelope {
  return {
    currentVersion: value.currentVersion,
    channel: value.channel,
    canAutoInstall: value.canAutoInstall,
    result: fromJson(CheckResultSchema, value.result),
    status: fromJson(StatusSnapshotSchema, value.status),
  }
}

// 有更新时给出目标版本，没有就返回空串：侧边栏与卡片共用同一判定。
export function availableVersion(status: ConsoleUpdateEnvelope | null) {
  const kind = status?.result.kind
  return kind?.case === 'updateAvailable' ? kind.value.version : ''
}
