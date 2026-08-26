# Roadmap

尚未排期的意向，不等于已接受的 ADR。落地前再写决策。

## 本机认证：Bitwarden Login with Device

- **状态**：意向
- **背景**：缺 session 时 `dec-server` 走本地 HTTP web unlock，浏览器收集主密码。Bitwarden Password Manager 已有 [Log in with device](https://bitwarden.com/help/log-in-with-device/)（auth request）：发起端提交临时公钥，已登录的手机/桌面/网页核对指纹短语后批准，user key 加密回传。主密码不经过 Dec。
- **目标**：把这条通道接到现有 `EnsureSession` / `internal/secrets/unlock/`，作为本机人类认证的主路径（或与 web unlock 并列，可回退）。
- **不改**：session 仍只在 `dec-server` 进程内存；不落盘；测试仍禁止真实弹窗（注入桩或 `DEC_NO_WEB_UNLOCK`）。
- **不做**：用 auth request 做自动续期 / 机器 token 轮换；不把整库 user key 交给 CI runner。
- **体验注意**：批准端需开启「使用此设备批准来自其他设备的登录请求」；Android 推送依赖 Play 服务，失败时可在 App「待处理的登录请求」或网页 Vault 手动批。TUI 应展示指纹短语，并轮询请求结果（不依赖推送送达）。
- **落点（规划）**：`internal/secrets/unlock/` 增加 auth-request 发起与轮询；TUI/进度流回传指纹短语（类比 0011 的 unlock URL 实时回传）。

## CI 无人值守

- **状态**：调研后维持现状（非正式 ADR）
- **记录**：[research/secrets-ci-centralization.md](./research/secrets-ci-centralization.md)
- **约束**：Password Manager 无免主密码的机器身份；auth request 每次要人在场，不适合流水线。平台 Secrets 继续放业务变量；不把 BW session 当 CI 票。
