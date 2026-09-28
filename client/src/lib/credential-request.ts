// ADR 0036 凭据请求通道的前端纯逻辑：kind 与控件形态的映射。
// 数值与 schema/service/v1/service.proto 的 CredentialRequestKind 对齐。

export type CredentialRequestView = {
  request_id: string
  kind: number
  prompt: string
  operation: string
}

export const CREDENTIAL_KIND_LOGIN_PASSWORD = 1
export const CREDENTIAL_KIND_KEY_PASSPHRASE = 2
export const CREDENTIAL_KIND_PRIVATE_KEY_MATERIAL = 3
export const CREDENTIAL_KIND_CONFIRM = 4

export const credentialKindValues = [
  CREDENTIAL_KIND_LOGIN_PASSWORD,
  CREDENTIAL_KIND_KEY_PASSPHRASE,
  CREDENTIAL_KIND_PRIVATE_KEY_MATERIAL,
  CREDENTIAL_KIND_CONFIRM,
] as const

export type CredentialKindValue = (typeof credentialKindValues)[number]

// 是否有秘密输入控件（密码框 / 口令框 / 私钥粘贴）。
export function kindHasSecretInput(kind: number): boolean {
  return kind === CREDENTIAL_KIND_LOGIN_PASSWORD
    || kind === CREDENTIAL_KIND_KEY_PASSPHRASE
    || kind === CREDENTIAL_KIND_PRIVATE_KEY_MATERIAL
}

// 是否带可选公钥字段（仅私钥材料粘贴）。
export function kindHasPublicKeyField(kind: number): boolean {
  return kind === CREDENTIAL_KIND_PRIVATE_KEY_MATERIAL
}

// 仅确认按钮，无输入框。
export function kindIsConfirmOnly(kind: number): boolean {
  return kind === CREDENTIAL_KIND_CONFIRM
}

// 提交可用条件：确认类永远可提交；输入类要求秘密非空。
export function credentialSubmitEnabled(kind: number, secret: string): boolean {
  if (kindIsConfirmOnly(kind)) return true
  return kindHasSecretInput(kind) && secret.trim() !== ''
}

// 提交按钮文案：确认类显示批准，其余显示提交。
export function credentialSubmitLabel(kind: number): string {
  return kindIsConfirmOnly(kind) ? '批准' : '提交'
}

// 来源操作 ID → 用户可读标签。prompt 本身已含完整说明，这里是兜底；
// 未知 ID 原样展示（未来新增工具不致于显示成空）。
const OPERATION_LABELS: Record<string, string> = {
  dec_install_ssh_key: '安装 SSH 登录密钥（dec_install_ssh_key）',
  dec_import_sshkey: '导入私钥到密码库（dec_import_sshkey）',
  dec_rotate_ssh_key: '轮换 SSH 密钥（dec_rotate_ssh_key）',
}

export function operationLabel(operation: string): string {
  return OPERATION_LABELS[operation] || operation
}

// 拉取结果转凭据页视图：pending 且请求 ID 非空才可渲染。
export function toCredentialRequestView(info: {
  pending: boolean
  request_id: string
  kind: number
  prompt: string
  operation: string
  error?: string
}): { request_id: string; kind: number; prompt: string; operation: string } | null {
  if (!info.pending || info.error || !info.request_id) return null
  if (!credentialKindValues.includes(info.kind as CredentialKindValue)) return null
  return {
    request_id: info.request_id,
    kind: info.kind,
    prompt: info.prompt,
    operation: info.operation,
  }
}
