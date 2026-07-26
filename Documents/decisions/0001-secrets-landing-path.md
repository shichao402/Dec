# 0001 — Secrets 落地路径：消费者路径即落地路径

- **状态**：已实施
- **日期**：2026-07-27
- **影响范围**：`pkg/secrets/`（`paths.go` 已删、新增 `landing.go` / `remote.go`）、`pkg/app/secrets_pull.go`、`pkg/app/secrets_push.go`、`pkg/app/delete.go`、`internal/tui/`、`Documents/BUNDLE-SECRETS-MODEL.md`、`schema/secrets/v1/`（`state.proto` 已删）

## 问题

Bitwarden Secure Note 拉到本地后应该落在哪个路径？这件事在仓库里长期存在三种并存说法，
任何一种都"能跑"，因此谁也排除不掉谁，隔一段时间就没人记得哪个才是对的。

## 现状盘点（决策前的事实）

### 三层分歧

| 层面 | 实际取值 | 出处 |
|------|----------|------|
| Bitwarden 匹配键 | `mise/conf.d/vikunja.toml`（裸相对路径，无前缀） | `CanonicalNoteName`（`pkg/secrets/paths.go`） |
| 本地落地（代码现状） | `.secrets/<secrets_bundle>/mise/conf.d/vikunja.toml` | `LandingPathForNote`、`SecretsRootDir` 常量 |
| 本地落地（规范文档） | `.config/mise/conf.d/vikunja.toml` | 见下方三处 |

规范文档的三处独立来源，说法一致且**均未出现 `.secrets/`**：

- `.cursor/rules/bundle-secrets-mirror.mdc`：「Secure Note **名称** = 敏感文件在项目根的**目标相对路径**」
- `Documents/ARCHITECTURE.md`：`└── .config/mise/conf.d/  # Bitwarden secrets 落地（不进 .dec/）`
- `schema/secrets/v1/state.proto`：「项目根相对落地路径（Secure Note 名），如 `.config/mise/conf.d/vikunja.toml`」

### `.secrets/` 的来源

`9d31e89`（v1.13.13）引入 `SecretsRootDir = ".secrets"`。该提交只改了两份文档：
`BUNDLE-SECRETS-MODEL.md`（+74 行新说法，但未删除原有的「项目根直接落地」描述）与
`ARCHITECTURE.md`（1 行）。规则文件与 schema 未同步。因此 `BUNDLE-SECRETS-MODEL.md`
自相矛盾：第 7/15/19/209/230/248 行描述旧模型，第 110-115/342/351 行描述新模型。

### 匹配层实测

`CanonicalNoteName` 会剥掉 `.secrets/<bundle>/` 与 `.config/` 前缀后再比较，四种历史写法归一到同一个键：

```
Bitwarden note 名（输入）                                   →  匹配键
mise/conf.d/vikunja.toml                                   →  mise/conf.d/vikunja.toml
.config/mise/conf.d/vikunja.toml                           →  mise/conf.d/vikunja.toml
.secrets/vikunja_workflow/mise/conf.d/vikunja.toml         →  mise/conf.d/vikunja.toml
.secrets/vikunja_workflow/.config/mise/conf.d/vikunja.toml →  mise/conf.d/vikunja.toml
```

**推论**：本地落地路径的改动**不影响** Bitwarden 侧的增删判定（`findExistingCipher`
与 `noteStillPresent` 都在规范化之后比较）。迁移的远端风险因此很低。

附带发现：`updateSecureNote` 会把 name 写成规范化后的裸路径，即 update 时**会就地重命名**
存量 note。这与 `BUNDLE-SECRETS-MODEL.md:342`「不重命名」的表述不符，属于文档与代码分歧。

## 决策

**Bitwarden Secure Note 名 = 该文件在项目根的目标相对路径；pull 原样落地到该路径，不加任何前缀、不插入 bundle 名。**

```
Bitwarden folder: vikunja_workflow
  note ".config/mise/conf.d/vikunja.toml"  →  <project>/.config/mise/conf.d/vikunja.toml
  note "config/server.yaml"                →  <project>/config/server.yaml
```

SSH Key 维持既有例外：落地机器级 `~/.ssh/`（OpenSSH 不认项目内路径）。

## 理由

**判定原则：每个密文件都有一个消费者，消费者的读取路径是硬性的，dec 的职责是把文件送到消费者手上。**

- mise 只读 `.config/mise/conf.d/*.toml`
- 应用只读它自己的 `config/server.yaml`
- OpenSSH 只读 `~/.ssh/`

按此原则，落地路径必须等于消费者路径，答案唯一。

**SSH 已是先例。** dec 对 SSH Key 就是这么做的。`.secrets/` 是整套设计里唯一没有消费者的
落地路径，因此必然要配一个手工复制步骤才能用——`skills/tencent-cloud/SKILL.md` 里
「复制 `.config/mise/conf.d/tencent-cloud.toml.example` 并填写密钥」这一步就是这么来的。
它是例外，不是另外三个是例外。

**自解释。** note 名即目标路径，在 Bitwarden 里一眼就知道文件该在哪，不需要记住任何映射规则。
这正是本文档要解决的"隔段时间就忘"的问题。

## 被否方案

**A. 保留 `.secrets/` 作暂存，另加部署步骤送到消费者路径。**
否决：引入第二次写入与两份副本，需要额外处理漂移（用户改了消费者路径上的文件怎么办），
复杂度高于直接落地，且没有换来任何好处。

**B. 只把 `.secrets` 根目录做成可配置项（默认不变）。**
否决：落地路径形态是 `<根>/<bundle>/<相对路径>`，改根之后中间的 `<bundle>/` 段仍在，
得到 `.config/tencent-cloud/mise/conf.d/tencent-cloud.toml`，mise 依然读不到。只是改名，不解决问题。

**C. 维持现状，把 `.secrets/` 扶正为正式模型，改文档去迁就代码。**
否决：等于承认密文件落地后还需要人工搬运才能被消费，与「dec 自动下发私密配置」的产品目标冲突。

## `.secrets/` 当初解决的问题及其去向

`.secrets/` 解决的是「push 时怎么知道哪些文件是密文件」——一个目录扫下去最省事
（`ScanSecretsBundleFiles`）。

本决策起草时打算把这件事交给 `SyncStateFile`（`~/.dec/secrets/state.json`）。**实施时否决了该方案**：
本地索引会与远端漂移，漂移后 push 依据的是一份过期清单，而这个清单的错误后果是删错密钥。
改为**每次操作实时枚举远端 folder 的 note 列表**——权威索引只有一份，在 Bitwarden 侧，没有可漂移的副本。
`state.proto` 随之删除。

代价是 push 不再自动发现本地新文件：新增 secret 需显式登记（TUI Project 页 `A`）。
这个代价是可接受的——自动发现在「落地路径散在项目根」的模型下本就无法安全实现。

## 实施结果

远端未做任何改动：旧代码的匹配键与前缀无关，且经核对存量 note 名本就是消费者路径。
落地路径的变化因此完全在本地一侧。

1. `paths.go`（`CanonicalNoteName` / `LandingPathForNote` / `SecretsRootDir` / `ScanSecretsBundleFiles`）**整个文件删除**
2. pull 改两阶段：先取回全部 folder 的 note，汇总后一次校验，再写盘。跨 folder 撞车只有在汇总视图下才看得见
3. 落地前校验集中在 `pkg/secrets/landing.go`：非法路径（绝对路径 / `~` / `..` / 盘符）、跨 folder 撞车、`.dec/` 重叠、符号链接逃逸、git 跟踪
4. push 改为按远端 note 列表读本地文件；**删掉了删远端孤儿的逻辑**，本地缺文件只报告
5. `updateSecureNote` 原样回传远端 name 密文，不再用本地推导的名字重新加密：
   note 名就是落地路径，push 无权把一条 secret 改指到另一个文件上（修掉了盘点中发现的「update 会就地重命名」）
6. `BUNDLE-SECRETS-MODEL.md` 已改平；`state.proto` 已删

一次性迁移曾用 hidden 命令 `__migrate-secrets-paths` 实现（`--plan` 产出映射表供人工确认——
Bitwarden 里已丢失原始目标路径信息，无法自动推断；apply 时先移本地文件再改远端 note 名）。
经确认存量 note 名已经是消费者路径，无需改写，该命令连同 `RenameSecureNote` / `ListFolderNames`
一并删除。**不要为「以后可能还需要」而重新引入**：迁移工具只在有存量要迁时才该存在。

**保留兼容层是这套东西越来越乱的直接原因**：现在 note 名只有一种合法形态，
`findExistingCipher` 精确匹配，没有任何归一化或别名层。

## 参考

- `Documents/BUNDLE-SECRETS-MODEL.md` — 详细设计
- `.cursor/rules/bundle-secrets-mirror.mdc` — 存储根分离与零重叠约束
- `pkg/secrets/landing.go` — 落地前校验
