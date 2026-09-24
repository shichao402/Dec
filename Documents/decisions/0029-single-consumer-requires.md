# 0029 — 消费声明唯一化与提供方组成改名

- **状态**：已接受；`vault` pin 已被 [0033](0033-requires-registry-only.md) 废除。控制面边界见 [0031](0031-control-plane-and-upstream-contribution.md)
- **日期**：2026-09-17
- **关联**：[0015](0015-project-config-boundary.md)、[0016](0016-p-four-quadrant-model.md)（`requires` 语义段被本决策取代）、[0023](0023-facade-capability-parity.md)、[0026](0026-project-provides-and-sync-worktree.md)、[0028](0028-official-registry-and-install.md)（消费声明段被本决策细化）
- **影响范围**：`internal/types`、`internal/config`、`internal/install`、`internal/app` 解析与写入、`set_requires` RPC 与 `dec_set_requires`、Console 订阅面板与「更新」页

## 问题

消费声明（「这个工作区用哪些项目」）此前摊成三处两种形状：

| 声明 | 位置 | 形状 |
|----|----|----|
| 个人私仓 · 项目平面 | 私仓内 `<home>/dec.yaml` 的 `requires` | `[]string`，无版本 |
| 个人私仓 · Global 平面 | `~/.dec/config.yaml` 的 `enabled_projects` | `[]string`，无版本 |
| 官方注册表 · 两个平面 | `.dec/config.yaml` 的 `requires` | `map[项目]版本` |

第一行是病根：**消费者的选择被写进了提供方的数据模型**。于是代码里必须过滤 `name != home`，
Global 平面因为没有 本仓项目只能另开 `enabled_projects`，而 0028 新增官方 `requires` 时
无法复用上述任何一处，只能再开第四份语义——它至今没有任何写入入口，提供方 CI 发布成功后
Console 里看不到、也无法订阅。四份声明同名不同义，等于没有 SSOT。

## 决策

**一、消费声明只有一处一形状。** `.dec/config.yaml`（项目平面）与 `~/.dec/config.yaml`
（Global 平面）的 `requires` map 是唯一消费声明：

```yaml
requires:
  relkit: latest        # 官方注册表，跟随最新已发布 tag
  tencent-cloud: v0.2.1 # 官方注册表，钉死
  my-notes: vault       # 个人私仓，跟随私仓 HEAD
```

来源由 pin 决定，不由机制决定：`latest` / `v*` 解析注册表 tag，`vault` 解析个人私仓。
私仓是单分支可变仓，没有版本，因此 pin 只能是 `vault`；不给私仓编版本号。
保存订阅**只写消费方配置**，绝不写私仓。

**二、提供方组成改名 `depends_on`。** `<项目>/dec.yaml` 的 `requires` 改为 `depends_on`，
语义收窄为「项目 A 的资产依赖项目 B」。安装规则：被 `requires` 直接声明的项目装本平面
全部资产，其 `depends_on` 闭包只装 `public`。读取旧 `requires` 字段时按 `depends_on` 处理，
写入一律 `depends_on`；这样项目平面的有效安装集合与改名前完全相同，无需数据迁移。

**三、`project_name` 是本仓项目。** 它表示「这个仓正在写哪个项目」，从工作树安装，
不出现在 `requires` 里，也不再兼任订阅列表的锚点。

**四、删除 `enabled_projects` / `enabled_bundles` 与 legacy `bundles/` 解析。** 写入路径直接移除；
读取旧配置时一次性折叠为 `requires{<名>: vault}` 且不写回。私仓早已是项目模型，
`bundles/` 分支对真实数据是死代码，一并删除。

**五、写入入口只有一个。** `set_requires` RPC（MCP `dec_set_requires`）替代 `save_enabled_bundles`
与 `dec_set_assets`。Console 在项目页 / Global 资产页提供**订阅**面板：列出注册表已发布项目
∪ 私仓项目，每行标来源与 pin（官方默认 `latest`，私仓固定 `vault`），勾选保存即写 `requires`。

同名项目只给一行，且**行的身份跟着订阅版本走**：某项目在注册表与私仓同时存在时，若它的订阅版本是 `latest` 或 `v*`，
这一行就是注册表行，带已装 / 可用版本与「有更新」；否则保持私仓身份，只附带远端可用版本
作为参考。反过来（按私仓渲染官方订阅）会同时藏掉版本和更新提示——提供方 CI 发了新版，
消费者在 Console 里依然看不到该升级，正是本决策要修的那个缺口。

订阅态只看 pin。「已安装」比「已订阅」宽：`depends_on` 闭包带进来的项目也已落地，
但它不在 `requires` 里，面板必须显式区分，否则未订阅的行会显示成已订阅，保存时凭空多出声明。

**六、「更新」页只做远端到本地。** 对同一份 `requires` 做只读差异：官方行比 tag，私仓行比 commit；
多选预览后安装。个人资产与密钥的写回留在资产页，不与更新混为双向同步。

## 被否方案

- 在「更新」页另加订阅区：那是第四处声明入口，问题不在缺入口。
- 给个人私仓编版本 / 打 tag：私仓是个人单分支仓，版本号没有生产者。
- 保留 `enabled_*` 双写兼容层：同名不同义正是本决策要消灭的东西，读侧折叠一次即可。
- 让官方 `requires` 也写进私仓清单：消费者选择再次污染提供方数据模型。
