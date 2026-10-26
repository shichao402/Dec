# 仓库历史凭据与信息泄露排查

对应实施计划第 1 项。排查对象是 `shichao402/Dec` 全部提交历史（1168 个提交、
4 个分支、514 个 tag），目的是在仓库转为公开（[ADR 0045](../decisions/0045-public-repo-not-private.md)）
之后，确认历史里没有可被外部读取的凭据、真实公网 IP 或服务器登录信息。

## 排查方法

用 `git log --all -p` 展开全历史文本后按模式匹配，覆盖以下类别：

- 私钥块：`BEGIN (RSA|OPENSSH|EC|PGP) PRIVATE KEY`
- 平台令牌：`ghp_`、`github_pat_`、`sk-`、`AKIA`、`AIza` 等前缀
- 凭据赋值：`BW_CLIENTID`、`BW_CLIENTSECRET`、`BW_PASSWORD` 及同义变量名
- 登录信息：`user@host` 形态的 SSH 命令、`-p <端口>` 显式端口
- 网络标识：全部 IPv4 字面量，按出现次数排序后逐个判定归属
- 敏感文件：历史中新增或删除过的 `.env`、密钥、凭据类文件

## 结论

历史中没有真实凭据泄露，无需轮换。逐项依据如下。

**私钥块全部是测试假值。** 一共命中 9 处，内容分别是 `fake`、`PRIV`、
`SECRET`、`abc`、`x` 这类占位串，且都位于 `_test.go` 或测试夹具中，没有一处是
完整可用的私钥。

**令牌命中只有一处，在测试样例里。** 唯一的 `ghp_` 命中是
`"GitHub 令牌": "token=ghp_abcdefghijklmnopqrstuvwxyz0123456789"`，属于疑似明文
扫描功能的测试输入，不是真实令牌。

**Bitwarden 变量全是占位符。** 命中的 `BW_PASSWORD` 有三种形态：
`'<测试账号主密码>'`、`'<主密码>'`、`"$(cat)"`，都是文档或测试中的写法。

**没有任何 SSH 登录命令。** 全历史搜索 `ssh ... user@host` 形态零命中，也没有
`root@<IP>`、`deploy@<IP>` 这类组合。

**`.secrets/` 从未被跟踪。** 历史中出现过 `.secrets/bundles/...`、
`.secrets/Dec/tokens/api.key` 等路径字符串，但都是代码与文档里的路径常量；
`git log --all --name-only` 对 `^\.secrets/` 零命中，说明这个目录从未进入版本库。

**没有真实的 `.env` 文件被提交。** 全历史对 `*.env` 的内容搜索，过滤掉
`example`、`your_`、`xxx`、`<`、`change`、`placeholder`、`dummy` 之后没有剩余。

## 唯一发现：真实公网 IP 在历史与当前分支中明文存在

`43.161.239.208` 是 `dec.firoyang.com`、`publish.firoyang.com`、
`update.firoyang.com` 三个域名的 A 记录指向，即生产服务器本身的地址。

它出现在两个提交里：

- `ab498f9` 新增 `.agent/agent-tray-access/requirements.md`，写明「`dec.firoyang.com`
  A 记录指向 `43.161.239.208`」，用于解释为什么本机不能接管这个域名。
- `196065d` 在 `docs/history/decisions/0044-proxy-protocol-real-ip.md` 的日志示例里
  写入了 `PROXY TCP4 43.161.239.208 10.5.0.9 40812 443`。

这两处内容在当前 `main` 分支上仍然存在，不属于「历史残留」，删除需要改动现有文档。

**判定为可接受，不做处理。** 理由是这个地址本来就公开可查：任何人对
`dec.firoyang.com` 做一次 DNS 解析就能得到它，把它写进文档没有增加任何暴露面。
服务器本身的安全边界应该是 SSH 只允许公钥登录、部署账户 `command=` 限定、
根密钥不进快照这些东西，而不是地址保密。如果要把「不出现 IP 字面量」当作规范，
那是一条独立的口径，需要先写进决策再统一改，不应该在这次排查里零散地改文档。

## 顺带澄清：端口探测在这台服务器上不可信

排查过程中用 `nc -z` 探测 `8443`、`8444`、`8787`，三次重复全部返回「成功」，
一度看起来像安全组放开了多余端口。实测证明这是误判：这台云的网关对所有端口都
回 TCP 握手，连 `9999`、`12345` 这种肯定没有服务的端口也返回成功。

服务器内部的 `ss -tlnp` 才是可信依据，实际结果只有两个公网监听：

- `0.0.0.0:22` sshd
- `0.0.0.0:443` nginx，再按 SNI 分流到 `127.0.0.1:8444`（dec-cloud）与
  `127.0.0.1:8787`（旧更新组件的 agent，2026-10-26 仍在外，计划下线）

`8443` 根本没有监听。所以「安全组只开 22 和 443」这一条实际上已经满足，
不需要调整。这个误判值得记下来：以后核实公网暴露面，必须看服务器内部的
监听表，不能用外部端口探测代替。

## 遗留

`43.161.239.208` 的处理口径待定。若日后决定文档中不出现 IP 字面量，
需要改动 `.agent/agent-tray-access/requirements.md:19`、
`.agent/agent-tray-access/requirements.md:58` 与
`docs/history/decisions/0044-proxy-protocol-real-ip.md:98` 三处。
