import { fromJson, type JsonValue } from '@bufbuild/protobuf'
import {
  CheckResultSchema,
  StatusSnapshotSchema,
  type CheckResult,
  type StatusSnapshot,
} from '@relkit/updater-bindings/updater/v1'

export type ConsoleUpdateEnvelope = {
  currentVersion: string
  canAutoInstall: boolean
  result: CheckResult
  status: StatusSnapshot
}

export function decodeConsoleUpdateEnvelope(value: {
  currentVersion: string
  canAutoInstall: boolean
  result: JsonValue
  status: JsonValue
}): ConsoleUpdateEnvelope {
  return {
    currentVersion: value.currentVersion,
    canAutoInstall: value.canAutoInstall,
    result: fromJson(CheckResultSchema, value.result),
    status: fromJson(StatusSnapshotSchema, value.status),
  }
}
