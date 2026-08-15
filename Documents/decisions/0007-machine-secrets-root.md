# 0007 — 机器级 bundle secrets 根 + 项目覆盖层

- **状态**：已接受（实施中）；**项目覆盖层与三层 env 合并已被 [0009](0009-bundle-binary-scope.md) 取代**
- **日期**：2026-08-13
- **修订**：2026-08-14 — [0009](0009-bundle-binary-scope.md) 删除 user∩project overlay；机器根仅服务 `scope: user` 的 bundle；env 按平面单层加载
- **关联**：[0002](0002-secrets-synctarget-root.md)、[0003](0003-user-enabled-secret-bundles.md)、[0005](0005-secrets-machine-handlers.md)、[0009](0009-bundle-binary-scope.md)
- **影响范围**：`internal/secrets/`（SyncTarget / env / pull / push / landing）、`internal/app/secrets_*.go`、`Documents/BUNDLE-SECRETS-MODEL.md`

## 问题

`user_enabled_bundles` 是机器平面，但 secrets 仍落到「当前项目」的 `.secrets/bundles/<name>/`，导致：

1. 误落到无关仓库（如游戏 trunk）
2. 多项目副本漂移
3. 像 `cnb_gitgcm` 这类机器副作用没有项目语义

同时存在真实需求：

- **混合包**（公开资产 + secrets）：公开资产进项目 `.dec/cache/`，secrets 默认可机器共享
- **同 bundle 按项目不同密钥**：机器默认 + 项目覆盖
- **`dec exec`**：有项目级落地时优先用项目值

## 决策

### 本地同步根

| 条件 | 本地根 | Bitwarden |
|------|--------|-----------|
| 仅 user 启用 | `~/.dec/secrets/bundles/<name>/` | `bundle/<name>` |
| 仅 project 启用 | `<project>/.secrets/bundles/<name>/` | `bundle/<name>` |
| user ∩ project | 机器默认同上；项目覆盖见下 | 默认：`bundle/<name>`；覆盖：项目 folder |

项目覆盖层：

| | |
|--|--|
| 本地 | `<project>/.secrets/bundles/<name>/` |
| Bitwarden | 项目 folder（如 `OSG_Trunk1`）中 Note 名 = `bundles/<name>/` + 相对路径 |
| 例 | 本地 `env/x.env` ↔ 远端 `bundles/tencent-cloud/env/x.env` |

项目级 secrets（非 bundle）仍为 `<project>/.secrets/project/` ↔ 项目 folder；pull 时**跳过**已由覆盖层认领的 `bundles/` 前缀 Note，避免双写。

### `dec exec` 合并顺序

```text
1. ~/.dec/secrets/bundles/<name>/env/*.env     # 机器默认（若存在）
2. <project>/.secrets/bundles/<name>/env/*.env # 项目覆盖（同 key 覆盖）
3. <project>/.secrets/project/env/*.env        # 项目级（再覆盖）
```

### SyncTarget 扩展

- `Plane`：`project`（默认）| `machine`
- `NoteNamePrefix`：覆盖层推/拉时加/剥前缀
- `NoteNameExcludePrefixes`：项目 SyncTarget 排除 `bundles/<name>/`（对每个已建覆盖层）

机器平面 `LocalRoot` 相对 `~/.dec/secrets/`（例如 `bundles/cnb`），与现有 `config.yaml` 同树。

### 不改动

- Bitwarden 仍无 `user/` 命名空间（0003）
- SSH → `~/.ssh/`（0002/0005）
- Handler Note（`*_gitgcm.yaml`）仍先落同步根再 Apply（0005）

## 理由

- 机器级默认同源同根，消除错误项目副本
- 项目覆盖满足混合包与 per-project 密钥，且有 BW 落点
- exec 合并规则与「项目优先」一致

## 被否方案

**A. 全部 bundle secrets 永远只落项目 `.secrets/`。**  
否决：机器级语义不成立；污染无关仓。

**B. 全部 bundle secrets 只落 `~/.dec/secrets/`，禁止项目覆盖。**  
否决：混合包与 per-project 密钥是事实需求。

**C. per-project 密钥另建 `bundle/<name>@<project>` folder。**  
否决：folder 爆炸；覆盖层用项目 folder + 路径前缀即可。

**D. exec 有项目目录就整树替换机器默认。**  
否决：应按 key 合并，项目只覆盖声明过的变量。

## 参考

- `Documents/BUNDLE-SECRETS-MODEL.md`
- `Documents/examples/cnb_gitgcm.yaml.example`
