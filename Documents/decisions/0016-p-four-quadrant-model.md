# 0016 — 顶层 P 与公开/私有 × 用户/项目四象限

- **状态**：已接受（阶段 1–7 已实施）
- **日期**：2026-08-27
- **取代**：[0009](0009-bundle-binary-scope.md)、[0013](0013-secrets-belong-to-declared-target.md)、[0014](0014-bundle-sole-writable-aggregate.md)
- **保留边界**：[0015](0015-project-config-boundary.md)；用户平面仍没有项目配置

## 问题

旧模型同时存在 `projects/`、`bundles/`、bundle `scope` 和两份启用清单。project 与
bundle 都在表达“一组共同演进的能力”，但拥有不同写入口和引用方式，导致配置漂移、
跨平面推断以及 Git/BW folder 命名不一致。

## 决策

### 1. 顶层只有 P

Git 仓库每个合法顶层目录都是一个 P；P 可以对应代码项目，也可以只是通用能力包。
名称必须匹配 `^[a-z0-9]+(?:-[a-z0-9]+)*$`，展示名写入 `<p>/dec.yaml`。

```text
<p>/
├─ dec.yaml
├─ public/
│  ├─ user/
│  └─ project/
└─ private/
   ├─ user/
   └─ project/
```

四个 Git 象限都只保存非敏感资产。`public/private` 表示能否被其他 P 引用，不表示
明文/密文；`user/project` 表示安装与运行平面。

### 2. 引用是直接且单向的

`<p>/dec.yaml` 的 `requires` 只允许引用其他 P 的 `public/project`：

- 不递归展开被引用 P 自己的 `requires`；
- 不引用 `public/user`；
- 任何 `private/*` 都不得通过引用进入其他 P；
- 缺失引用产生结构化告警，不猜测或回退到旧 bundle。

本机启用列表控制 P 的 user 两支；项目工作区绑定家 P，安装家 P 的 project 两支及
直接 requires 的 `public/project`。

### 3. Git 与 Bitwarden 分工

Git 的四象限均为非敏感资产。Bitwarden 只保存 `private/user`、`private/project`
两个平面的敏感正文，不建立多余的 public folder。同一 P/plane/相对路径不得同时由
Git 与 BW 持有。

Bitwarden 的 folder 只有一层，名字里的斜杠不表示层级。因此 folder 名就是 P 名，
平面与同步根相对路径一起编码进条目名（`private/<plane>/<rel>`，SSH Key 条目也不
例外）。`<p>/private/<plane>` 只是逻辑地址写法；这套切分只在 BW 实现内部定义，
`internal/app` 与 `internal/tui` 只传 (P, 平面, 相对路径)。

`private/user` 中的 `.gcm` / `.sshkey` 保持机器级安装语义。`private/project`
允许相同类型，但副作用必须定向到家工作区：

- Git/GCM 通过精确的 `includeIf.gitdir` 引入项目 fragment，启用
  `credential.useHttpPath`，按 origin 的仓库 path Apply/Inspect/Revoke；
- SSH 通过该项目 fragment 的 `core.sshCommand` 选择项目专属 SSH config
  fragment；原始 Host 声明只写入该 fragment，不写入全局 `config.d/dec.conf`；
- OpenSSH 不存在 `includeIf`，不得虚构；项目 fragment 可在自身 Host 规则之后
  Include 用户主配置，以继承 User/ProxyJump 等非密钥设置；
- project 与 user 的 SSH 密钥文件名分域，同一 P/实例不得相互覆盖或撤销。

阶段 1–3 只切换 Git 资产路径；在新的 BW 协议完成前，P 仓库不得误用旧
`bundle/<name>` 协议同步 secrets。

### 4. 写入与冲突

项目平面只能回推家 P，不能把 requires 引入的副本写回依赖 P。用户平面可回推本机
显式启用的 P。多个 P 在同一平面竞争同一 IDE 目标路径时硬失败；不得按扫描顺序覆盖。

## 被否方案

**继续保留 Project + Bundle。** 两类聚合对象边界重叠，仍需要 scope 推断和双清单同步。

**把 private 理解为敏感正文。** 会诱导敏感信息进入 Git；private 的稳定语义应是
“不可被其他 P 引用”，敏感正文的存储介质由 BW 决定。

**递归 requires。** AI 资产不是链接期依赖；递归会扩大安装污染，还引入环和菱形冲突。

**requires 同时拉 private。** 直接破坏不可见边界，并会在多个工作区产生同一私密资产
的可写副本。

**长期新旧双读。** 同一资产可能从两棵树读写，无法定义权威来源。远端一次性迁完后只读 P 模型。

## 一次性远端迁移

- 由维护者对 Git vault 与 Bitwarden 做一次性改写：先写入并校验新树，再删除旧 `projects/`、`bundles/` 与旧 folder。
- 客户端不提供迁移 UI。新版本启动清理本机旧 cache / `.secrets` 遗留并清空启用列表，由用户重新选择后 Pull。
- 检测到 `projects/` 或 `bundles/` 时普通 Push 拒绝。
- P 名按小写 kebab-case 规范化；仅大小写不同的旧名称合并为同一 P。缺失 requires 与 user-scope 引用降为警告并丢弃。
