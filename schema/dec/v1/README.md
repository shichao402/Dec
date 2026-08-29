# dec/v1 Schema

Protobuf 是本目录的 **schema 真相源**；运行时 wire format 仍是 YAML / JSON，不引入 `.pb` 二进制配置文件。

相关文档：[ARCHITECTURE.md](../../Documents/ARCHITECTURE.md) · [BUNDLE-SECRETS-MODEL.md](../../Documents/BUNDLE-SECRETS-MODEL.md) · [README.md](../../README.md)

## 声明层 vs 文件正文

| 适用 proto | 不适用 proto（保持原格式） |
|---|---|
| 顶层 `Project`（项目）、`ProjectConfig`、`GlobalConfig`、`VarsConfig` | `SKILL.md`、`.mdc` 规则正文 |
| 四象限资产视图（`QuadrantAsset`）与直接 `requires` | MCP JSON / TOML 片段正文 |
| `project_name`、`enabled_projects` | Bitwarden 私密正文 |

## 顶层项目与四象限

- **Project**（`projects.proto`）：vault 顶层 `<name>/dec.yaml`；名称强制小写 kebab-case。
- Git 资产位于 `<name>/{public,private}/{global,local}/`，四支均为非敏感内容。
- `requires` 只直接引用其他项目的 `public/local`，不递归，private 永不被引用。
- 本机由 `enabled_projects` 选择项目；本仓库由 `.dec/config.yaml` 的 `project_name` 绑定；`managed_projects` 仅记录 Console 在目标设备上接管的项目目录。
- Bitwarden 只保存 `<name>/private/{global,local}` 的敏感正文。

## 生成 Go 类型（可选）

需安装 [buf](https://buf.build/docs/installation)：

```bash
cd schema
buf lint
buf generate
```

生成物默认输出到 `schema/gen/go/`（见 `buf.gen.yaml`）。当前 Dec 运行时仍使用 `internal/types/` 手写 struct 与 adapter，生成类型供后续接入。
