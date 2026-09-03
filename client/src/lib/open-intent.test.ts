import { describe, expect, it } from 'vitest'
import { defaultLocalConnection, selectLocalConnection } from '@/lib/open-intent'
import type { SavedConnection } from '@/lib/utils'

const remote: SavedConnection = {
  ...defaultLocalConnection(),
  id: 'remote',
  label: '远端',
  kind: 'ssh',
  ssh_host: 'server.example',
}

describe('open intent local connection', () => {
  it('selects an existing local connection with saved unlock settings', () => {
    const local = {
      ...defaultLocalConnection(),
      auth_email: 'dev@example.com',
      password_saved: true,
    }

    expect(selectLocalConnection([remote, local])).toBe(local)
  })

  it('creates a usable local connection when none was saved', () => {
    expect(selectLocalConnection([remote])).toEqual(defaultLocalConnection())
  })
})
