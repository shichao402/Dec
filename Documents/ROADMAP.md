# Roadmap

尚未排期的意向，不等于已接受的 ADR。落地前再写决策。

## Console 认证体验后续

人工认证入口已由 [ADR 0022](decisions/0022-console-bitwarden-unlock.md) 固定为 Console
Authenticate。后续若接入 Bitwarden
[Log in with device](https://bitwarden.com/help/log-in-with-device/)（auth request），
也只能作为该页面中的认证方式，不得新增服务端页面、浏览器回退或 CLI/MCP 密码输入。

- **状态**：意向
- **目标**：Console 展示指纹短语并轮询请求结果；主密码不经过 Dec。
- **不改**：session、vault/user key、临时密钥与 2FA 中间态只在进程内存；测试不得自动拉起真实 Console。
- **不做**：用 auth request 自动续期、做机器 token 轮换，或把整库 user key 交给 CI runner。
- **体验注意**：批准端需开启「使用此设备批准来自其他设备的登录请求」；推送失败时可在 Bitwarden App 的待处理请求或网页 Vault 手动批准。

## CI 无人值守

- **状态**：调研后维持现状（非正式 ADR）
- **记录**：[research/secrets-ci-centralization.md](./research/secrets-ci-centralization.md)
- **约束**：Password Manager 无免主密码的机器身份；auth request 每次要人在场，不适合流水线。平台 Secrets 继续放业务变量；不把 BW session 当 CI 票。
