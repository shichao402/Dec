---
name: relkit-deploy
description: >
  relkit 仓装机、交叉编译、升级已有 relkit-agent / relkit-serve 二进制。
  工作区有 scripts/deploy/relkit.py 时使用；产品开箱、token、lock 走 relkit-ops。
---

# relkit 服务部署

## 何时使用

当前仓库是 **relkit 本仓**（存在 `scripts/deploy/relkit.py`），且任务是：

- 空机首装 systemd `relkit-agent` / `relkit-serve`
- 已有箱子换二进制（`upgrade --plan` / `--apply`）
- 为本仓 Release 交叉编译（`build`）

产品仓开箱、升 lock、注册/轮换/吊销 token **不要用本 skill**，用 [`relkit-ops`](../relkit-ops/SKILL.md) 与 `python scripts/host/relkit_host.py`。

## 入口

只跑 `python scripts/deploy/relkit.py`（仓库根；Linux 可用 `python3`）。细节以 [`scripts/deploy/README.md`](../../../scripts/deploy/README.md) 和该脚本 `--help` 为准。

不要手拼 SSH 写配置，不要用腾讯云 TAT / MCP 代跑命令，不要编造命令输出。Go 的 `relkit` / `relkit-agent` / `relkit-serve` 不是人用的第二套装机 CLI。

## 升级闸门

1. 确认能 `ssh <Host>`（Host 来自 `~/.ssh/config`，由 `dec pull` 落地）。
2. 先 `--plan`，把脱敏探测、本机 HEAD、协议窗口给用户看。
3. 用户确认后再 `--apply`。不要把 `--stage-only` 说成升级完成。
4. 工作区脏则停止，除非用户明确 `--allow-dirty`。默认从当前干净 HEAD 构建，stamp 读 `VERSION.json`；只有用户明确要求才 `--unsafe-from-dist`。不要传 `--version`。
5. **agent + serve 是固定配套**：现网箱必须两个进程都在。缺哪个先 `install` 那个，再 `upgrade`（不要 `--agent-only` / `--serve-only`）。`--agent-only` / `--serve-only` 只留给临时抢救，不是现网拓扑。
6. 公网站点配置属于 `/etc/relkit-agent/relkit-agent.json` 顶层 `site.sinks[]`（`makers`/`backend`/`directory`，ADR 0015；旧 `site.makers` 加载时等价展开，已 deprecated），不能下沉到产品 profile。升级后检查 `curl -fsS http://127.0.0.1:8787/-/site` 只返回脱敏 readiness；若配置或模板有变化，执行 `sudo relkit-agent site-rebuild -config /etc/relkit-agent/relkit-agent.json`。这不发布产品版本。

现网别名：

| Host | 跑什么 | upgrade |
|---|---|---|
| `cvm-gz`（`publish.firoyang.com`） | agent 控制面 + serve 操作面（数据面仍在 COS） | 两个都升；不要 `--agent-only`；不要改 profile URL |
| `update.devcloud.woa.com` | agent + serve 数据面 | 两个都升；不要传 `--public-base-url`（会改掉 loom 的 `https://…`） |

## 禁止

- 用 example JSON 覆盖现网配置
- 打印 token 或带 `sig=` 的 URL
- upgrade 默默 `--rotate-token`（token 只经产品仓 `relkit_host.py`）
- 重跑 `install` 来升级已有实例
- 把 `scripts/deploy/` 打进产品 lock 或拷进产品仓

升完 agent 后，发版必须用同一 release 的 publisher（协议窗口，ADR 0009）。
