# 调研：密钥集中存储与 CI 下放（2026-08-26）

- **状态**：调研记录（非正式 ADR）
- **结论（当时）**：**接受现状**——人 / 本机继续 Bitwarden Password Manager + `dec-server` 内存 session；CI 继续把用到的变量放 GitHub / CNB 平台 Secrets。不为个人 CI 上 OpenBao、自研保险柜、或把 BW session 填进流水线。
- **认证边界更新**：人工认证统一由 Console Authenticate 承载；Login with Device 若实现也只能作为 Console 内的认证方式，见 [0022](../decisions/0022-console-bitwarden-unlock.md) 与 [ROADMAP.md](../ROADMAP.md)。

本文给以后的自己看：问题怎么拆、哪些路走过、为什么停在现状。

## 1. 问题

Bitwarden 被设计成私密正文的唯一源。对人、对本机（Console / MCP / `dec-exec` 读落地文件）自洽。

GitHub Actions、CNB 等流水线：

- 无桌面交互、无常驻 `dec-server`，不能走 Console Authenticate
- 每次 job 是新机器、新 IP，身份不能绑设备指纹
- 密钥不能进 Git 仓库

若把业务密钥同时写在 BW 和各仓平台 Secrets，改一处漏一处，就不是源唯一。

## 2. 源唯一实际指什么

源唯一约束的是 **业务密钥正文只有一个创作/轮换点**，不是「消费端零秘密」。

任何无人值守系统都有一个不能再往上问的 **bootstrap**。CI 的地板是：平台密钥库里有一张东西。设计成败看那张东西是：

- **N 份业务密钥副本**（现状），还是
- **1 张可轮换、可吊销、窄权限的机器票**（委托读库，不是第二份正文）

测试：换一条数据库口令要改几个地方？只改 BW、下次 job 自动拿到 → 源唯一对 CI 也成立。还要改每个仓 Secrets → 多源。

**N→1 的前提：这一份必须是窄票，不能是 BW session / 主密码。** Session 等于已解锁的整库；短过期；禁止落盘。填进 GitHub 是把爆炸半径扩成全 vault，且过期后只能再塞主密码才能续——比现状更差。

票必须放在 **GitHub Secrets / CNB 变量**（平台注入 env），不能放仓库。三次全新 clone 仍可热跑：注入的是同一张票。身份跟 **流水线** 走，不跟 runner。

人仍要在「这条流水线第一次出现 / 票被吊销」时动一次（粘贴或 Telegram 批准后 API 写入）。收益是 **以后改业务密钥不必再改各仓**。不是「CI 上永远不用人填」。

## 3. Bitwarden：两个产品，不是 Dec 的两种模式

Dec 今天打的是 `/api/folders`、`/api/ciphers`，解密靠 `UserKey()`。这是 **Password Manager**（个人 vault：folder、Secure Note、SSH Item）。

**Secrets Manager** 是另一套订阅、另一套数据（project + 键值 secret）、另一套 API。PM 的 Note **不会**出现在 SM。SM 的 machine account / access token **不能**解 PM vault。

| | Password Manager（现状） | Secrets Manager |
|--|-------------------------|-----------------|
| 模型 | 路径 = Note 名，任意正文；SSH Item | 名字 + 一段字符串 |
| 机器身份 | 无免主密码读库；API key 仍要 `unlock` | access token，为 CI 设计 |
| 免费约束 | 个人 vault 可用 | 组织；免费档 project / machine account 很少 |

**私密半边继续 PM** 才对得上 bundle 文件树、Remote、`.env` / `.gcm` / `.sshkey`。不要为了 CI 整库迁 SM。SM 若用，只适合「流水线那几条 env」，且一条密钥只住一边。

无人值守还要 **直拉 PM Note/SSH**：在「不交主密码、不落明文拷贝」下 **做不到**。这是加密模型，不是 Dec 没写接口。

## 4. 认证分层（人 vs 机器）

| | 人 / 本机 | CI |
|--|-----------|-----|
| 证明 | Console 中提交主密码或批准 Login with Device | 窄票（若做）或平台里的业务变量（现状） |
| 得到 | 进程内存 session / user key | 不得拿到 user key |
| 轮换 | session 短过期，续期要人 | 票与业务密钥解耦 |

**不必做 OIDC。** OIDC 是「平台替作业证明身份」。CNB 没有这条；GitHub 有但已决定不走联邦。只验自己发的票时，不需要 IdP。

Login with Device（auth request）：手机/已登录客户端核对指纹短语后，用临时公钥回传 user key。适合 **本机少输主密码**。每次开门要人在场，**不是**旧票换新票。请求约 15 分钟过期。Android 须打开「使用此设备批准来自其他设备的登录请求」；无 Play 推送时可在「待处理的登录请求」或网页 Vault 手批。官方 `bw` CLI 不支持；Dec 自实现 API 可以接。**禁止**把这条回传的 user key 交给 CI runner。

`DEC_BW_PASSWORD` 只服务本机 Agent/开发，永远不进流水线。

## 5. 在「正文只在 PM、不交主密码」下，CI 只有三条

1. **人在环里**：低频发布时批准；不适合每个 PR。  
2. **窄 SM**：仅 CI 用的键值；machine token 在平台 Secrets。  
3. **现状**：业务变量继续手填平台 Secrets。

否掉的「PM 热路径」变体：

- 公网门面持 BW 短 session 现拉 Note：过期就要主密码在门面上，或失败等人。  
- session 窗口做明文投影给 CI：第二份正文，吊销滞后，门面被打穿即整包泄露。  
- Dec 自签 token 当 BW session 用：见 §2。

## 6. 探过的「自建 / OpenBao」方向（未采纳）

动机：文件树 + 手机批准 + CI 短票，BW 拆在 PM/SM 两边。

约束后来收成：

- 公开资产 **继续 Git vault**，不要把 skill/rule 和密钥服务绑成单点。  
- 笔记本 **不要求 Tailscale**；家/公司/酒店漫游走公网 HTTPS。Tailscale 对笔记本换网其实简单，用户明确不接受「电脑必须进虚拟网」。托管 CI 进 Tailscale 更脏（一次性 runner + `TS_AUTHKEY`）。  
- 因此若自建：OpenBao **仅内网**；对外只有 **Dec 在线**；桌面与 CI **同一套客户端**（refresh 在凭据库或平台 Secrets，access 内存，热路径打 Dec 在线，内网再读 OpenBao）。这是 BFF，**不是** OpenBao 教科书（教科书是客户端持 OpenBao token 直打 API）。Dec 在线与 OpenBao 同级敏感。  
- 短 TTL access 不够无人值守，必须另有 **refresh**（可吊销）。  
- 本机仍值得常驻 `dec-server`：协调票与内存明文、给 TUI/MCP 用；**不是**为了长连 OpenBao。CI **不要**常驻。

OpenBao 对个人过重：概念面（policy、AppRole、Raft）+ **seal/unseal**。重启后盘上密文、主密钥不在内存，必须再提交 init 时的 unseal key。这和「没密码解不开」同类，发生在服务器开机。个人可用 1/1 分片；钥匙不能明文放在同一块盘。auto-unseal（云 KMS）对个人往往更烦。解封与 userpass/AppRole 是两套仪式。

若只要「Telegram 人闸 + 加密文件树 + 公网 BFF」，后面不一定非要 OpenBao（age/SQLite 进程内主密钥），**重启解封仍然存在**。

Duo：OpenBao Login MFA **官方支持**（自托管 OpenBao 出网调 Duo 云，官方 App 推送）。用户不要多装 App。Telegram Bot 可作带外确认（绑定 `chat_id`、一次性短链、不传密钥正文）；强度弱于 WebAuthn/Duo（盗 Telegram ≈ 能批准）。CI 日常不要每 job 推 Bot。

自研完整 PW：加密/备份钥匙/设备批准/token 抢换都砍不掉；备份 age 私钥不要和在线服务同一把。

## 7. 拓扑（仅作理解，非实施基线）

曾收敛过的「干净」三层（在「无 Tailscale、OpenBao 不对外」前提下）：

```text
人闸：Telegram ↔ Dec 在线
库：OpenBao（内网） | Git vault（公开资产）
使用方：本机 Dec、CI  ——热路径只打 Dec 在线
```

冷启动 / 解封 / 吊销走入闸；热路径不经过 Telegram。

**未实施。** 与「接受现状」并存时，以 § 结论为准。

## 8. 个人场景的复杂度从哪来

砍不掉的：密文要钥匙；无人值守的机器要窄钥匙或人在场； ephemeral CI 只能把窄钥匙放平台 Secrets。

加戏（个人可不做）：OpenBao 全家桶、自研保险柜、Duo、强制组网、第二套 IAM。

本机「打开保险柜」并不难；难的是同时要无人值守、平台零权柄、无 VPN、推送、文件树、与 PM 同一套锁。

## 9. 相关

- [BUNDLE-SECRETS-MODEL.md](../BUNDLE-SECRETS-MODEL.md)  
- [bitwarden-auth.mdc](../../.cursor/rules/bitwarden-auth.mdc)  
- [ROADMAP.md](../ROADMAP.md)（本机 Login with Device）  
- OpenBao：[seal](https://openbao.org/docs/concepts/seal/)、[AppRole](https://openbao.org/docs/auth/approle/)、[Duo MFA](https://openbao.org/docs/api/secret/identity/mfa/duo/)  
- Bitwarden：[Log in with device](https://bitwarden.com/help/log-in-with-device/)、[SM access tokens](https://bitwarden.com/help/access-tokens/)  
