import type {
  AssetOption,
  AssetSelection,
  DeviceSummary,
  DirectoryListing,
  GlobalSettings,
  ManagedProject,
  PingInfo,
  SavedConnection,
} from '@/lib/utils'

// 布局测试要覆盖的数据形态。之前只对着「中等数量、短名字、无报错」这一种状态改布局，
// 所以空列表留白、长列表溢出、超长中文名撑破一行这些问题都没机会暴露。
export type ScenarioName = 'typical' | 'empty' | 'heavy' | 'extreme' | 'failing' | 'locked' | 'fresh'

export type Scenario = {
  connections: SavedConnection[]
  ping: PingInfo
  device: DeviceSummary
  settings: GlobalSettings
  assets: AssetSelection
  listing: DirectoryListing
}

const longLabel = '腾讯云基础设施与发布流水线共享资产集合（含证书与域名）'
const longRoot = 'D:\\workspace\\GitHub\\very-long-repository-name-for-layout-regression\\nested\\module'

function connection(overrides: Partial<SavedConnection> = {}): SavedConnection {
  return {
    id: 'local',
    label: '本机',
    kind: 'local',
    host: '127.0.0.1',
    port: 37820,
    ssh_host: '',
    ssh_user: '',
    tls: false,
    tls_server_name: '',
    auth_email: 'dev@example.com',
    password_saved: false,
    ...overrides,
  }
}

function project(overrides: Partial<ManagedProject> = {}): ManagedProject {
  return {
    Root: 'D:\\workspace\\GitHub\\Dec',
    Label: 'Dec',
    Name: 'dec',
    Exists: true,
    Initialized: true,
    ConfigPath: 'D:\\workspace\\GitHub\\Dec\\.dec\\config.yaml',
    Error: '',
    ...overrides,
  }
}

function bundle(overrides: Partial<AssetOption> = {}): AssetOption {
  return {
    Name: 'tencent-cloud',
    Description: '腾讯云 MCP + skill（CVM/COS/DNS/Pages/Lighthouse/CDB）',
    Vault: 'tencent-cloud',
    Members: [
      { Name: 'tencent-cloud', Type: 'mcp', Vault: 'tencent-cloud', Visibility: 'public', Plane: 'global' },
      { Name: 'dec-tencent-cloud', Type: 'skill', Vault: 'tencent-cloud', Visibility: 'public', Plane: 'global' },
    ],
    Enabled: true,
    SecretsOnly: false,
    OtherPlane: false,
    RemoteMissing: false,
    RemoteUnverified: false,
    Model: 'p',
    Home: false,
    Required: false,
    Quadrants: {},
    ...overrides,
  }
}

function settings(overrides: Partial<GlobalSettings> = {}): GlobalSettings {
  return {
    RepoConnected: true,
    RepoURL: 'git@github.com:shichao402/Dec.git',
    ConnectedRepoURL: 'git@github.com:shichao402/Dec.git',
    AvailableIDEs: ['claude', 'codex', 'cursor', 'gemini'],
    SelectedIDEs: ['cursor'],
    EffectiveIDEs: ['cursor'],
    ConfiguredEditor: 'code --wait',
    ServerIdleTimeout: '30m',
    ...overrides,
  }
}

const listing: DirectoryListing = {
  Current: 'D:\\workspace\\GitHub',
  Parent: 'D:\\workspace',
  Home: 'C:\\Users\\dev',
  Roots: ['C:\\', 'D:\\'],
  Entries: Array.from({ length: 24 }, (_, index) => ({
    Name: `repo-${String(index + 1).padStart(2, '0')}`,
    Path: `D:\\workspace\\GitHub\\repo-${String(index + 1).padStart(2, '0')}`,
  })),
}

function base(): Scenario {
  const projects = [
    project(),
    project({ Root: 'D:\\workspace\\GitHub\\relkit', Label: 'relkit', Name: 'relkit' }),
    project({ Root: 'D:\\workspace\\GitHub\\InvestM', Label: 'InvestM', Name: 'investm', Initialized: false }),
    project({ Root: 'D:\\workspace\\GitHub\\lyra', Label: 'lyra', Name: 'lyra' }),
    project({ Root: 'D:\\workspace\\GitHub\\MyQuant', Label: 'MyQuant', Name: 'myquant' }),
  ]
  return {
    connections: [connection(), connection({ id: 'nas', label: 'NAS', kind: 'ssh', ssh_host: '192.168.1.20', ssh_user: 'dev' })],
    ping: { version: 'v1.13.35', instance_id: '1788012650175557000', unlocked: true },
    device: {
      Initialized: true,
      RepoConnected: true,
      RepoURL: 'git@github.com:shichao402/Dec.git',
      HomeDir: 'C:\\Users\\dev\\.dec',
      Platform: 'windows',
      Projects: projects,
    },
    settings: settings(),
    assets: {
      ProjectRoot: '',
      ConfigPath: 'C:\\Users\\dev\\.dec\\config.yaml',
      ExistingConfig: true,
      Model: 'p',
      Plane: 'global',
      Bundles: [
        bundle({ Name: 'relkit', Vault: 'relkit', Description: '发布工具链', Enabled: false }),
        bundle(),
        bundle({ Name: 'vsx-publish', Vault: 'vsx-publish', Description: 'secrets-only / machine-enabled placeholder (ADR 0003)', SecretsOnly: true }),
        bundle({ Name: 'woa', Vault: 'woa', Description: 'secrets-only / machine-enabled placeholder (ADR 0003)', SecretsOnly: true }),
      ],
    },
    listing,
  }
}

export const scenarios: Record<ScenarioName, () => Scenario> = {
  typical: base,

  // 全空：概览、项目、资产三页都要靠 EmptyState 撑住，最容易出现「下半屏全空」。
  empty: () => {
    const scenario = base()
    scenario.device.Projects = []
    scenario.device.RepoConnected = false
    scenario.device.RepoURL = ''
    scenario.settings = settings({ RepoConnected: false, RepoURL: '', ConnectedRepoURL: '', SelectedIDEs: [], EffectiveIDEs: [] })
    scenario.assets.Bundles = []
    scenario.assets.ExistingConfig = false
    return scenario
  },

  // 长列表：列表必须在内部滚动，不能把整页顶出视口。
  heavy: () => {
    const scenario = base()
    scenario.device.Projects = Array.from({ length: 120 }, (_, index) =>
      project({
        Root: `D:\\workspace\\GitHub\\project-${String(index + 1).padStart(3, '0')}`,
        Label: `project-${String(index + 1).padStart(3, '0')}`,
        Name: `project${index + 1}`,
        Initialized: index % 3 !== 0,
      }),
    )
    scenario.assets.Bundles = Array.from({ length: 60 }, (_, index) =>
      bundle({ Name: `bundle-${index + 1}`, Vault: `bundle-${index + 1}`, Enabled: index % 2 === 0 }),
    )
    return scenario
  },

  // 超长文本：中文标签、深层路径、大量 IDE，检查截断与换行而不是撑破容器。
  extreme: () => {
    const scenario = base()
    scenario.device.RepoURL = 'ssh://git@internal-git.example.com:2222/very/deep/namespace/path/dec-private-assets.git'
    scenario.device.Projects = [
      project({ Root: longRoot, Label: longLabel, Name: 'verylongprojectnamewithoutseparators' }),
      project({ Root: `${longRoot}\\second`, Label: `${longLabel}（第二个）`, Name: 'another', Initialized: false }),
    ]
    scenario.settings = settings({
      RepoURL: 'ssh://git@internal-git.example.com:2222/very/deep/namespace/path/dec-private-assets.git',
      ConnectedRepoURL: 'ssh://git@internal-git.example.com:2222/very/deep/namespace/path/dec-private-assets.git',
      AvailableIDEs: ['claude', 'codex', 'cursor', 'gemini', 'windsurf', 'zed', 'continue', 'aider'],
      SelectedIDEs: ['cursor', 'codex', 'claude', 'gemini'],
      EffectiveIDEs: ['cursor', 'codex', 'claude', 'gemini'],
      ConfiguredEditor: 'C:\\Program Files\\Microsoft VS Code\\bin\\code.cmd --wait --new-window',
    })
    scenario.assets.Bundles = [
      bundle({ Name: 'tencent-cloud-infrastructure-and-release-pipeline', Description: longLabel }),
      bundle({ Name: 'another-extremely-long-bundle-name-without-separators', Description: `${longLabel}${longLabel}`, Enabled: false }),
    ]
    return scenario
  },

  // 未解锁：连接后停在解锁页。
  locked: () => {
    const scenario = base()
    scenario.ping = { ...scenario.ping, unlocked: false }
    return scenario
  },

  // 新设备：连接后进入全屏引导。
  fresh: () => {
    const scenario = base()
    scenario.device.Initialized = false
    scenario.device.Projects = []
    scenario.device.RepoConnected = false
    scenario.device.RepoURL = ''
    scenario.settings = settings({ RepoConnected: false, RepoURL: '', ConnectedRepoURL: '', SelectedIDEs: [], EffectiveIDEs: [] })
    return scenario
  },

  // 报错态：异常项目、私仓未连接、bundle 远端缺失，检查告警区不挤爆行。
  failing: () => {
    const scenario = base()
    scenario.device.RepoConnected = false
    scenario.device.Projects = [
      project({ Error: '项目目录已不存在，或没有读取权限', Exists: false, Initialized: false }),
      project({ Root: 'D:\\workspace\\GitHub\\relkit', Label: 'relkit', Name: 'relkit', Error: '.dec/config.yaml 解析失败：yaml: line 12: mapping values are not allowed in this context' }),
    ]
    scenario.settings = settings({ RepoConnected: false, ConnectedRepoURL: '' })
    scenario.assets.Bundles = [
      bundle({ RemoteMissing: true, Enabled: true }),
      bundle({ Name: 'relkit', Vault: 'relkit', RemoteUnverified: true, Enabled: true }),
    ]
    return scenario
  },
}
