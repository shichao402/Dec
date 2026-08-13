# dec/v1 Schema

Protobuf 是本目录的 **schema 真相源**；运行时 wire format 仍是 YAML / JSON，不引入 `.pb` 二进制配置文件。

相关文档：[ARCHITECTURE.md](../../Documents/ARCHITECTURE.md) · [BUNDLE-SECRETS-MODEL.md](../../Documents/BUNDLE-SECRETS-MODEL.md) · [README.md](../../README.md)

## 声明层 vs 文件正文

| 适用 proto | 不适用 proto（保持原格式） |
|---|---|
| `Project`、`ProjectConfig`、`GlobalConfig`、`Bundle`、`VarsConfig` | `SKILL.md`、`.mdc` 规则正文 |
| 资产引用（`AssetRef`、bundle `members`） | MCP JSON / TOML 片段正文 |
| `Project.bundles`、`project_name`、`enabled_bundles`、`BundleBinding`（见 secrets/v1） | Bitwarden secrets bundle 内文件明文 |

## Project > Bundle

- **Project**（`projects.proto`）：vault `projects/<name>.yaml`，声明 `bundles`、`ides` 等
- **Bundle**（`assets.proto`）：Git Vault `bundles/<name>/` 目录内的资产组织单位（`skills/`、`rules/`、`mcp/`、`commands/`、`bundle.yaml`）
- **ProjectConfig**（`config.proto`）：本地 `.dec/config.yaml`，以 `project_name` 引用 vault project，同步 `enabled_bundles`；`enabled_bundles` 是唯一的资产启用入口，字段 5/6 上曾经的单资产清单已 reserved
- 私密平面在 Bitwarden **secrets bundle** 中与 Dec bundle **同构绑定**；pull 时按 project bundle 列表成对拉取
- 零路径重叠 invariant 与 pull 流程见 [Documents/BUNDLE-SECRETS-MODEL.md](../../Documents/BUNDLE-SECRETS-MODEL.md)

## 生成 Go 类型（可选）

需安装 [buf](https://buf.build/docs/installation)：

```bash
cd schema
buf lint
buf generate
```

生成物默认输出到 `schema/gen/go/`（见 `buf.gen.yaml`）。当前 Dec 运行时仍使用 `internal/types/` 手写 struct 与 adapter，生成类型供后续接入。
