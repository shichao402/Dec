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

  const ok = (value: unknown) => ({ result_json: JSON.stringify(value ?? {}), error: '' })

  const handlers: Record<string, (args: Record<string, unknown>) => unknown> = {
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
    run_operation: () => ok(pullResult),
    invoke_method: (args) => {
      const method = String(args.method || '')
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
