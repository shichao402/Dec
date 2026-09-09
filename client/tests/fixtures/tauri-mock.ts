import type { Scenario } from './data'

// 这段函数会被序列化进浏览器，在 App 启动前装好 Tauri IPC。
// 放在测试目录而不是 src/ 下：生产构建里不该存在 mock 分支，main.tsx 也不需要 VITE_DEC_MOCK 开关。
export function installTauriMock(scenario: Scenario) {
  const state = {
    connections: scenario.connections.map((conn) => ({ ...conn })),
    settings: { ...scenario.settings },
  }

  const pullResult = {
    ProjectRoot: '',
    RequestedCount: 4,
    PulledCount: 3,
    FailedCount: 1,
    SkippedReason: '',
    MissingBundles: ['relkit'],
    MissingProjects: [],
    ValidationWarnings: ['relkit 的 requires 指向了未启用的 bundle'],
    NonFatalWarnings: [],
    SecretsSkippedReason: '',
    SecretsNoteCount: 6,
    SecretsSSHKeyCount: 1,
    EffectiveIDEs: scenario.settings.EffectiveIDEs,
    SelectedProjects: ['dec'],
    RequiredProjects: [],
    Quadrants: {},
  }

  const methods: Record<string, unknown> = {
    load_device_summary: scenario.device,
    load_global_settings: state.settings,
    load_asset_selection: scenario.assets,
    save_global_settings: {},
    save_enabled_bundles: {},
    browse_directories: scenario.listing,
    register_managed_project: scenario.device.Projects[0] || {},
    remove_managed_project: {},
    bind_managed_project: {},
    create_remote_project: { Name: 'newproject' },
    prepare_project_config_init: {
      AvailableProjects: ['dec', 'relkit', 'investm', 'lyra'],
      HomeProject: 'dec',
    },
  }

  // 元数据接口按设计不返回 Note 正文，mock 也不该出现任何像密钥值的字段。
  methods.list_secrets = {
    bitwarden_configured: true,
    session_active: true,
    remote_checked: true,
    files: [
      {
        secrets_bundle: 'relkit/private/project',
        project_rel_path: '.secrets/relkit/.env/upload.env',
        local_exists: true,
        local_size_bytes: 148,
        local_modified_unix: 1_757_000_000,
        remote_exists: true,
      },
      {
        secrets_bundle: 'relkit/private/project',
        project_rel_path: '.secrets/relkit/.env/agent.env',
        local_exists: false,
        remote_exists: true,
      },
    ],
  }

  // 删除候选覆盖两个分区：Console 必须能分别渲染，并且不允许混选。
  methods.list_delete_candidates = [
    {
      Kind: 'secret',
      Label: '.secrets/relkit/.env/upload.env',
      SecretPath: '.env/upload.env',
      LocalRoot: 'relkit',
      Plane: 'project',
      SecretsBundle: 'relkit/private/project',
      GroupTitle: 'relkit/private/project',
      Partition: 'remote',
      Orphan: false,
      Unmanaged: false,
      ReadOnly: false,
    },
    {
      Kind: 'dec',
      Label: 'skill/release',
      Type: 'skill',
      Name: 'release',
      Vault: 'relkit',
      GroupTitle: 'relkit (bundle)',
      Visibility: 'public',
      AssetPlane: 'project',
      Partition: 'remote',
      Orphan: false,
      Unmanaged: false,
      ReadOnly: false,
    },
    {
      Kind: 'dec',
      Label: 'skill/stale-local',
      Type: 'skill',
      Name: 'stale-local',
      Vault: 'relkit',
      GroupTitle: 'relkit (bundle)',
      Partition: 'local',
      Orphan: true,
      Unmanaged: false,
      ReadOnly: false,
    },
  ]

  // 推送预览按平面分流：Global 推 ~/.dec 下的 user 平面资产，项目推该项目资产。
  // mock 按 workspacePlane 返回不同结果，这样测试能抓到平面参数传错。
  const projectPushPreview = {
    SecretsTargetCount: 1,
    DecCandidateCount: 2,
    DecHasChanges: true,
    DecSkippedReason: '',
    BitwardenConfigured: true,
    HomeProject: 'dec',
    Changes: [
      { Op: '修改', Path: 'p/dec/private/project/secrets/relkit.env', Quadrant: 'private/project' },
      { Op: '修改', Path: 'p/dec/public/project/skills/release/SKILL.md', Quadrant: 'public/project' },
    ],
  }

  const globalPushPreview = {
    SecretsTargetCount: 2,
    DecCandidateCount: 1,
    DecHasChanges: true,
    DecSkippedReason: '',
    BitwardenConfigured: true,
    HomeProject: '',
    Changes: [
      { Op: '修改', Path: 'p/dec/private/user/env/machine.env', Quadrant: 'private/user' },
    ],
  }

  const ok = (value: unknown) => ({ result_json: JSON.stringify(value ?? {}), error: '' })

  const handlers: Record<string, (args: Record<string, unknown>) => unknown> = {
    take_open_intent: () => null,
    list_connections: () => state.connections,
    save_connection: (args) => {
      const conn = args.conn as Scenario['connections'][number]
      const stored = { ...conn, id: conn.id || 'saved' }
      const index = state.connections.findIndex((item) => item.id === stored.id)
      if (index >= 0) state.connections[index] = stored
      else state.connections.push(stored)
      return stored
    },
    delete_connection: (args) => {
      state.connections = state.connections.filter((item) => item.id !== args.id)
      return null
    },
    load_saved_password: () => '',
    connect_target: () => scenario.ping,
    ping_server: () => scenario.ping,
    authenticate: () => ({
      unlocked: scenario.ping.unlocked,
      need_2fa: false,
      control_token: 'token',
      expires_in_ms: 3_600_000,
      error: '',
    }),
    disconnect: () => null,
    stop_service: () => null,
    get_active_operation: () => ({ active: false }),
    watch_operation: () => ok(pullResult),
    run_operation: (args) => {
      const operation = String(args.operation || '')
      const global = String(args.workspacePlane || '') === 'global'
      if (operation === 'preview_push') return ok(global ? globalPushPreview : projectPushPreview)
      if (operation === 'delete') {
        return ok({
          DecDeleted: 0,
          SecretsDeleted: 1,
          SSHKeysDeleted: 0,
          BundlesDeleted: 0,
          VersionCommit: '',
          SkippedReason: '',
          Remnants: [],
          Mode: 'remote',
        })
      }
      if (operation === 'push') {
        return ok({
          DecPushedCount: 2,
          DecSkippedReason: '',
          VersionCommit: 'mock-commit',
          SecretsCreatedCount: 0,
          SecretsUpdatedCount: 1,
          SecretsSkippedReason: '',
        })
      }
      return ok(pullResult)
    },
    invoke_method: (args) => {
      const method = String(args.method || '')
      if (method === 'save_project_tags') {
        const raw = String(args.payloadJson || args.payload_json || '{}')
        const payload = JSON.parse(raw) as { Name?: string; Tags?: string[] }
        const name = String(payload.Name || '')
        const tags = Array.isArray(payload.Tags) ? payload.Tags : []
        const item = scenario.assets.Bundles.find((bundle) => bundle.Name === name)
        if (item) item.Tags = tags
        return ok({ Name: name, Tags: tags, Committed: true })
      }
      if (!(method in methods)) return { result_json: '', error: `未知服务方法 "${method}"` }
      return ok(methods[method])
    },
    'plugin:event|listen': () => 1,
    'plugin:event|unlisten': () => null,
    'plugin:event|emit': () => null,
  }

  const internals = {
    invoke: (cmd: string, args: Record<string, unknown> = {}) => {
      const handler = handlers[cmd]
      if (!handler) return Promise.reject(new Error(`mock 未实现命令 ${cmd}`))
      return Promise.resolve(handler(args || {}))
    },
    transformCallback: (callback?: (payload: unknown) => void) => {
      const id = Math.floor(Math.random() * 1_000_000)
      const store = window as unknown as Record<string, unknown>
      if (callback) store[`_${id}`] = callback
      return id
    },
    unregisterCallback: () => undefined,
    convertFileSrc: (path: string) => path,
  }

  Object.assign(window as unknown as Record<string, unknown>, {
    __TAURI_INTERNALS__: internals,
    // event.js 在 unlisten 路径上直接读这个对象；缺了它 StrictMode 的二次卸载会抛未处理拒绝。
    __TAURI_EVENT_PLUGIN_INTERNALS__: { unregisterListener: () => undefined },
  })
}
