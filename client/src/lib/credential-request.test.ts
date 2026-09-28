import { describe, expect, it } from 'vitest'
import {
  CREDENTIAL_KIND_CONFIRM,
  CREDENTIAL_KIND_KEY_PASSPHRASE,
  CREDENTIAL_KIND_LOGIN_PASSWORD,
  CREDENTIAL_KIND_PRIVATE_KEY_MATERIAL,
  credentialSubmitEnabled,
  credentialSubmitLabel,
  kindHasPublicKeyField,
  kindHasSecretInput,
  kindIsConfirmOnly,
  toCredentialRequestView,
} from '@/lib/credential-request'

describe('credential kind 控件映射', () => {
  it('登录密码 / 口令 / 私钥材料带秘密输入，确认类不带', () => {
    expect(kindHasSecretInput(CREDENTIAL_KIND_LOGIN_PASSWORD)).toBe(true)
    expect(kindHasSecretInput(CREDENTIAL_KIND_KEY_PASSPHRASE)).toBe(true)
    expect(kindHasSecretInput(CREDENTIAL_KIND_PRIVATE_KEY_MATERIAL)).toBe(true)
    expect(kindHasSecretInput(CREDENTIAL_KIND_CONFIRM)).toBe(false)
    expect(kindHasSecretInput(0)).toBe(false)
  })

  it('公钥字段只出现在私钥材料形态', () => {
    expect(kindHasPublicKeyField(CREDENTIAL_KIND_PRIVATE_KEY_MATERIAL)).toBe(true)
    expect(kindHasPublicKeyField(CREDENTIAL_KIND_LOGIN_PASSWORD)).toBe(false)
  })

  it('确认类无输入框且永远可提交', () => {
    expect(kindIsConfirmOnly(CREDENTIAL_KIND_CONFIRM)).toBe(true)
    expect(credentialSubmitEnabled(CREDENTIAL_KIND_CONFIRM, '')).toBe(true)
    expect(credentialSubmitLabel(CREDENTIAL_KIND_CONFIRM)).toBe('批准')
  })

  it('输入类要求秘密非空才能提交，空白被拒绝', () => {
    expect(credentialSubmitEnabled(CREDENTIAL_KIND_LOGIN_PASSWORD, 'pw')).toBe(true)
    expect(credentialSubmitEnabled(CREDENTIAL_KIND_LOGIN_PASSWORD, '   ')).toBe(false)
    expect(credentialSubmitEnabled(CREDENTIAL_KIND_KEY_PASSPHRASE, 'phrase')).toBe(true)
    expect(credentialSubmitEnabled(CREDENTIAL_KIND_PRIVATE_KEY_MATERIAL, '-----BEGIN')).toBe(true)
    expect(credentialSubmitLabel(CREDENTIAL_KIND_LOGIN_PASSWORD)).toBe('提交')
  })
})

describe('toCredentialRequestView 拉取结果分流', () => {
  const base = {
    pending: true,
    request_id: 'cred-abc',
    kind: CREDENTIAL_KIND_LOGIN_PASSWORD,
    prompt: '为设备 dev-box 提供登录密码',
    operation: 'dec_install_ssh_key',
  }

  it('pending 且字段合法时产出视图', () => {
    expect(toCredentialRequestView(base)).toEqual({
      request_id: 'cred-abc',
      kind: CREDENTIAL_KIND_LOGIN_PASSWORD,
      prompt: '为设备 dev-box 提供登录密码',
      operation: 'dec_install_ssh_key',
    })
  })

  it('无挂起 / 有错误 / 缺请求 ID 时返回 null', () => {
    expect(toCredentialRequestView({ ...base, pending: false })).toBeNull()
    expect(toCredentialRequestView({ ...base, error: '已超时' })).toBeNull()
    expect(toCredentialRequestView({ ...base, request_id: '' })).toBeNull()
  })

  it('未知 kind 返回 null，不渲染异常控件', () => {
    expect(toCredentialRequestView({ ...base, kind: 0 })).toBeNull()
    expect(toCredentialRequestView({ ...base, kind: 99 })).toBeNull()
  })
})
