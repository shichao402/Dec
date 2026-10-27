---
name: relkit-deploy
description: >
  relkit 仓的服务部署：空机首装、换 relkit-agent / relkit-store / relkit-console
  二进制、交叉编译、向服务器发布工具。工作区有 scripts/deploy/relkit.py 时使用；
  产品注册、token、宿主接入走 relkit-ops。
---

# relkit 服务部署

## 何时使用

当前仓库是 relkit 本仓（存在 `scripts/deploy/relkit.py`），且任务是：

- 空机首装 `relkit-store` / `relkit-agent` / `relkit-console` 三个 systemd 服务；
- 已有服务器换二进制（`upgrade --plan` / `--apply`）；
- 为 Release 交叉编译（`build`）；
- 向服务器发布 relkit 工具（`tools publish`；`upgrade` 默认已包含）。

产品注册、token 签发轮换吊销、宿主接入**不用**本 skill，走
[`relkit-ops`](../relkit-ops/SKILL.md)。

## 拓扑

一台服务器三个进程，内网与外网同构，只有地址和根密钥实例不同：

| 进程 | 监听 | 职责 |
| --- | --- | --- |
| `relkit-store` | `127.0.0.1:8080`（外网）/ `:8080` | 数据面 |
| `relkit-agent` | `127.0.0.1:8787` | 发布、签名、产品与 token API、工具分发 |
| `relkit-console` | `127.0.0.1:8081` / `:8081` | 管理面板，经 `tree` 适配器读 store |

| Host | 连接 | 域名 |
| --- | --- | --- |
| 内网 | `ssh -p 36000 root@update.devcloud.woa.com` | `publish` 写面 + 面板、`update-internal` 协议读面、`update` 人页 |
| 外网 | `ssh publish.firoyang.com` | `publish.firoyang.com`、`update-internal.firoyang.com`、`update.firoyang.com` |

拓扑图的唯一来源是 [`docs/design/topology.md`](../../../docs/design/topology.md)。

## 入口

只跑 `python scripts/deploy/relkit.py`（仓库根）。参数以该脚本 `--help` 和
[`scripts/deploy/README.md`](../../../scripts/deploy/README.md) 为准。

首装（空机）：

```text
relkit.py install serve   --binary dist/relkit-store-linux-amd64 ...
relkit.py install agent   --binary dist/relkit-agent-linux-amd64 --product-root <发布树父目录> ...
relkit.py install console --binary dist/relkit-console-linux-amd64 [--agent <agent URL>]
```

`--product-root` 必填，没有默认值（内网 `/data/relkit`，外网 `/srv/relkit`）。console
从配置的 `tree` 适配器读发布树，不再接受 `--dir` / `--state-dir`。

仍在跑 `relkit-serve.service` 的老服务器先 `migrate-serve` 迁到 `relkit-store.service`，
之后才可走 `upgrade`。

不要手拼 SSH 写配置，不要用腾讯云 TAT / MCP 代跑命令，不要编造命令输出。

## 升级闸门

1. 确认能 `ssh <Host>`。Windows 上用 Git 自带的 `ssh.exe`（System32 版连外网会被
   重置），并在仓库外的目录执行命令，避免 SSH 配置里的 `NUL` 在仓库里生成文件。
2. 先 `--plan`，把脱敏探测、本机 HEAD、协议窗口给用户看。
3. 再 `--apply`。不要把 `--stage-only` 说成升级完成。
4. 工作区脏则停止，除非用户明确 `--allow-dirty`。默认从当前干净 HEAD 构建，版本读
   `VERSION.json`；只有用户明确要求才 `--unsafe-from-dist`。
5. 三个进程一起升。console 配置有旧键时，`upgrade` 会一次性迁移成 `tree` 适配器并
   带上 cookie 密钥，新二进制直接拒绝旧配置，所以 console 只能走 `upgrade`。
6. `upgrade` 会回填 agent 配置的 `productRootParent`（取已注册产品的共同父目录），
   有分歧时在输出中说明。
7. 工具 URL 由 agent 配置的 `publicURL` 签发（内网 `http://publish.devcloud.woa.com`，
   外网 `https://publish.firoyang.com`）。
8. 升级后检查三个服务 `active`、`/-/health`，以及各域名主入口。

## 根密钥

每台服务器一把根，私钥只在该服务器与离线备份。部署前后用托管自检确认归属，规范见
[`docs/guide/trust-root-custody.md`](../../../docs/guide/trust-root-custody.md)。私钥找不到时先
跑自检，不要直接生成新根：换根会让已装应用全部失去信任链。

## 禁止

- 用 example JSON 覆盖现网配置；
- 打印 token 或带 `sig=` 的 URL；
- 重跑 `install` 来升级已有实例；
- 把 `scripts/deploy/` 拷进产品仓；
- 用 `robocopy /MIR` 或任何镜像删除命令清理仓库里的杂项文件（会把整个仓库当作镜像
  目标清空）。
