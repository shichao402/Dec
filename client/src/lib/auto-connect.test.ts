import { describe, expect, it } from 'vitest'
import { selectAutoConnectConnection } from '@/lib/auto-connect'
import type { SavedConnection } from '@/lib/utils'

function connection(id: string, passwordSaved: boolean): SavedConnection {
  return {
    id,
    label: id,
    kind: 'local',
    host: '127.0.0.1',
    port: 47653,
    ssh_host: '',
    ssh_user: '',
    tls: false,
    tls_server_name: '',
    auth_email: 'alice@example.com',
    password_saved: passwordSaved,
  }
}

describe('selectAutoConnectConnection', () => {
  it('优先选择上一次连接且已保存密码的服务', () => {
    const first = connection('first', true)
    const last = connection('last', true)

    expect(selectAutoConnectConnection([first, last], 'last')).toBe(last)
  })

  it('首次启动时仅有一个已保存密码的服务就自动选择', () => {
    const saved = connection('saved', true)

    expect(selectAutoConnectConnection([connection('plain', false), saved], '')).toBe(saved)
  })

  it('多个保存密码的服务但没有上次连接记录时不猜', () => {
    expect(selectAutoConnectConnection([
      connection('first', true),
      connection('second', true),
    ], '')).toBeNull()
  })

  it('上一次连接没有保存密码时不自动连接它', () => {
    expect(selectAutoConnectConnection([
      connection('last', false),
      connection('other', false),
    ], 'last')).toBeNull()
  })
})
