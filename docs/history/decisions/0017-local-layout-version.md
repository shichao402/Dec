# 0017 — 本机配置 kind/version 与 layout_version

- **状态**：已接受
- **日期**：2026-08-28
- **关联**：[0015](0015-project-config-boundary.md)、[0016](0016-p-four-quadrant-model.md)
- **影响范围**：`~/.dec/config.yaml`、工作区 `.dec/config.yaml`、启动清理、Git/Bitwarden 象限名 `global`/`local`

## 决策

1. 全局配置与项目配置都带 `kind` + `version` + `layout_version`。`kind` 防止互相误读；`version` 管 yaml 字段；`layout_version` 管 cache/secrets 树。两种文件的 version 数字不必互相兼容。
2. [0015](0015-project-config-boundary.md) 否掉的是「只用 version 当防误认」。全局配置**应当**带 version；防误认靠 `kind` 与空 root 守卫。读到错误 `kind` 直接拒绝。缺 `kind` 的旧文件按路径补写，不走「当成 v1 项目配置升级」。
3. 配置 `version` 无 migrator：失败退出，不删 `repo_url` / `project_name` / `enabled_projects`。
4. `layout_version` 无 migrator：删除派生数据（cache、可再 pull 的 secrets 树），写当前版本，提示到同步页 Pull。不删 `device.json`、vars、集成凭据。
5. 安装平面目录从 `user`/`project` 改为 `global`/`local`。远端一次性改写；本地不写搬家，旧树当 layout 0 直接清。
6. `dec --global` 为本机平面入口；`--user` 为隐藏别名。
7. `.dec/cache/` 是绑定项目（及本机 `enabled_projects`）的可写副本；push 目标集 = vault ∪ cache。`requires` 引入的副本只读。
8. 四象限不当 TUI 导航；页签按动词：项目 / 引入 / 同步 / 设置。

## 被否方案

**在 TUI 做迁移向导。** 远端改名是一次性维护操作；本地缺 layout migrator 时清 cache 再 Pull 即可。

**配置 version 当唯一防误认。** 见 0015：空 root 仍会撞 `~/.dec/config.yaml`。
