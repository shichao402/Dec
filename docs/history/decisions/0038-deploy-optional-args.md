# ADR 0038：部署单元的可选参数走 EnvironmentFile

日期：2026-10-23
状态：已接受（已实现）

## 背景

`deploy/provision.sh` 生成的 systemd 单元把启动参数写死在一行 `ExecStart` 里：

```
ExecStart=/opt/dec-cloud/dec-cloud -data-dir ... -listen ... -domain ... -root-key ...
```

这套写法在「启动参数固定」的年代是成立的。现在多了三类可选能力——冷备仓库、审计仓库、本机证书签发——它们的共同特点是「地址和密钥名随环境而变，但不影响服务能否启动」。若照旧硬编码进 `ExecStart`，会撞上两个问题：

一是改一个仓库地址就得重跑 `provision.sh`。那是重建机器用的脚本，会重装软件包、重写 sshd 配置、重配 fail2ban，副作用远大于「改一个字符串」，在已经跑着生产数据的机器上尤其不该随手执行。

二是 `deploy/dec-cloud-deploy.sh` 会从 `ExecStart` 里用 `sed` 提取 `-listen` 来推导回环地址做健康检查。参数越多、越长，贪婪匹配越容易命中错误位置；某天某个仓库地址里恰好含有 `-listen` 字样，健康检查就会去连一个不存在的地址，接着判定部署失败并回滚。

## 决策

1. **可选参数集中到 `/etc/dec-cloud/service.env` 的单个变量 `DEC_EXTRA_ARGS`**，单元用 `EnvironmentFile=-/etc/dec-cloud/service.env` 引入（行首 `-` 表示文件缺失不报错），`ExecStart` 末尾以 `\$DEC_EXTRA_ARGS` 承接展开。

2. **模板只在文件不存在时写入**。`if [ ! -f "$ENV_FILE" ]` 保护：重跑 `provision.sh` 不得覆盖线上已填好的值，只校正权限（`root:root` + `0640` + 属组为服务账号）。

3. **文件里只放非机密内容**：仓库地址、deploy key 的路径、密钥名。真正的机密（DNSPod 凭据、企业微信 webhook）留在 Dec 密钥库里由根密钥加密，不落到这个文件。

4. **主机密钥校验文件由单元显式传参**，即 `-known-hosts $KNOWN_HOSTS_FILE`，不走 `SSH_KNOWN_HOSTS` 环境变量。GitHub 的 ed25519 公钥写死在 `provision.sh` 里，不在运行时 `ssh-keyscan`。

5. **`-listen` 的提取锚定空白边界**：`sed` 表达式由 `-listen[ =]` 收紧为 `[[:space:]]-listen[ =]`，并保持 `tail -n 1` 只取一条。

以上五条都用 `internal/guard/guard_test.go` 里的 `TestProductionDeployOnlyThroughCI` 锁住：缺 `EnvironmentFile`、缺 `\$DEC_EXTRA_ARGS`、缺 `if [ ! -f "$ENV_FILE" ]`、缺写死的公钥、缺 `-known-hosts $KNOWN_HOSTS_FILE`、出现 `ssh-keyscan`（注释行除外）、`ExecStart` 硬编码可选参数、`sed` 未锚定空白边界，任意一条都会让测试失败。

## 为什么不用 ssh-keyscan

`ssh-keyscan` 解决的是「不知道对方公钥」，代价是第一次连接不做校验。攻击者只要在那一次抢先应答，就能冒充 GitHub，此后冷备与审计的全部历史的明文会推到对手机器上。把公钥写死进脚本的成本是 GitHub 换钥时要改代码，而这是极低频、且换钥时会全网通告的可观测事件。两害相权，写死。

## 为什么固定走 ssh.github.com:443

云服务器的出方向常被出口策略拦掉 22 端口。GitHub 在 443 上提供同一套 SSH 服务，主机公钥与 22 端口完全相同（同一把 ed25519 key），因此 `known_hosts` 里两条都写上，用哪个端口都能校验通过。

## 为什么显式传 -known-hosts 而不用环境变量

go-git 在 `SSH_KNOWN_HOSTS` 指向的文件不存在时不是降级，而是直接失败。而服务由 systemd 拉起，环境变量的传导多一层不透明耦合：排查「为什么推送一直失败」时，很难确认到底是变量没传进来、还是文件没读到。改成命令行参数后，`systemctl cat dec-cloud` 就能看到全部输入，且 `Target.KnownHosts` 为空时退回 go-git 默认查找，本机开发无需额外配置。

## 被否方案

**A. 全部参数仍写死在 ExecStart，改配置就重跑 provision.sh。** 否决：在跑着生产数据的机器上重跑重建脚本，风险与收益完全不成比例。

**B. 用多个环境变量（每个参数一个），单元里逐个展开。** 否决：单元里会出现七八个 `$DEC_BACKUP_URL` 之类的占位符，一旦某个变量未定义，systemd 展开成空串会把下一个参数顶到它的位置，造成难以诊断的错位。单个 `DEC_EXTRA_ARGS` 天然规避这个问题。

**C. 用 `EnvironmentFile` 加 `-backup-url` 等参数写在文件里但不用引号包住整体。** 否决：systemd 的环境文件按 `KEY=VALUE` 逐行解析，VALUE 里含空格时必须有引号，而 `DEC_EXTRA_ARGS` 恰恰是要展开成多个 argv 的。必须靠 shell 的词分割语义，即 `\$DEC_EXTRA_ARGS` 不加引号。

**D. 让 `dec-cloud` 自己读配置文件（如 TOML）。** 否决：当前 `dec-cloud` 是「全部参数走 flag」的形态，为几个可选功能引入配置文件的解析、优先级、校验和文档，收益远小于成本。等参数规模真的到需要分组的程度再谈。

## 运维影响

部署后机器的 `/etc/dec-cloud/service.env` 默认是空的 `DEC_EXTRA_ARGS=""`，服务行为与改造前完全一致（冷备不推送、审计不推送、本机证书不签发）。要启用某项能力，编辑该文件填参数、`sudo systemctl restart dec-cloud` 即可，不需要碰 `provision.sh`。

`provision.sh` 收尾的提示里已补上这条路径说明，避免运维按老习惯去改脚本。
