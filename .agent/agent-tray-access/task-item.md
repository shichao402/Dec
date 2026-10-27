# 实施计划：代理可见性与本机访问入口

> 2026-10-27 变更：`local.dec.firoyang.com` 域名、本机证书与本机 HTTPS 已整体删除，改为托盘内唯一的「Dec 管理面」窗口（wry WebView，复用原打开面板的 admin_url）；回环默认端口由 8787 改为 43679。下文涉及域名、证书、443 与 8787 的条目均已作废，仅供追溯。

- [ ] 1. 日志合并与轮转
   - 在 `internal/agent` 新增日志写入器：同一文件 `logs/agent.log`，超过阈值轮转并保留最近若干份；目录不可写时退回 stderr 继续运行
   - `cmd/dec-agent/main.go` 的 `logf` 改走该写入器；`service.go` 中 launchd plist 的 stdout 与 stderr 指向同一文件，systemd、Windows 计划任务同步调整
   - 同步失败日志补齐时间、原因、当前 revision
   - 单元测试：轮转、目录不可写降级
   - _需求：5.1、5.2、5.4、5.5_

- [ ] 2. 扩展回环 Host 校验与状态接口
- [ ] 2.1 Host 白名单接受 `*.localhost` 与 `local.dec.firoyang.com`
   - 修改 `loopback.go` 的 `hostOK` 与 `originOK`，端口校验规则不变，其余域名继续拒绝
   - 单元测试覆盖 `127.0.0.1`、`localhost`、`*.localhost`、`local.dec.firoyang.com`、非法域名五类输入
   - _需求：4.1、4.2、7.7_

- [ ] 2.2 `/api/status` 补充托盘所需字段
   - 增加实际监听地址列表（HTTP 端口、HTTPS 地址及是否可用）、管理面首选地址、日志文件路径、本机证书到期时间与状态
   - 管理面概览页展示全部可用地址
   - _需求：3.2、3.3、4.9、2.3_

- [ ] 3. 云端签发并下发 `local.dec.firoyang.com` 证书
- [ ] 3.1 云端 DNS-01 签发与续期
   - 在 `internal/cloud` 新增证书管理：读取存为云端密钥的 DNSPod API 凭据，DNS-01 签发，到期前 30 天自动续期，设备吊销后下次续期换新私钥
   - 证书与私钥作为一类受控资产存入 SSOT，凭据不进入下发内容
   - 单元测试：续期时机判定、吊销触发换钥（DNS 提供方用接口替身）
   - _需求：4.4、4.6_

- [ ] 3.2 经设备通道下发证书
   - 在同步协议中加入本机证书条目，代理落到 `~/.dec-agent/tls/`（权限 0600），写入白名单同步更新
   - 测试：下发后落盘、下发失败不影响其他落点
   - _需求：4.5_

- [ ] 4. 代理本机 HTTPS 监听
   - 持有有效证书时在 `127.0.0.1:443` 额外提供 HTTPS，被占用时在原端口旁提供 HTTPS 并记录实际地址
   - 证书文件变化时热加载（`tls.Config.GetCertificate`），无需重启
   - 证书距到期不足 14 天或缺失时状态标记告警并发系统通知，HTTP 回环入口始终可用
   - 测试：证书热替换、443 占用回退、无证书时只开 HTTP
   - _需求：4.7、4.8、4.5_

- [ ] 5. 托盘共享逻辑（纯 Go）
   - 新建 `internal/tray`：轮询 `/api/status` 并映射为五种图标状态（正常、异常、维护、重启中、未运行）；生成悬停文本与菜单状态文本；计算管理面打开地址（优先 local 子域，否则实际回环地址）
   - 平台无关的动作接口：打开浏览器、打开日志、重启代理、启动代理、退出
   - 单元测试：状态映射、地址选择、版本未对齐时的提示文本
   - _需求：2.1、2.2、2.4、2.5、3.1、3.2、3.3、1.6_

- [ ] 6. Windows 托盘 `dec-tray.exe`
   - `cmd/dec-tray` 的 Windows 实现：用 `golang.org/x/sys/windows` 调 `Shell_NotifyIconW`，`CGO_ENABLED=0` 交叉编译
   - 双击打开管理面，右键弹出菜单（打开管理面、查看状态、查看日志、重启代理、退出）
   - 重启代理通过计划任务结束再启动；30 秒内未恢复则置异常并通知；浏览器启动失败时弹出含地址的提示
   - _需求：1.1、1.2、1.3、1.5、1.8、1.9、1.10、3.4、6.1、6.2_

- [ ] 7. macOS 托盘 `dec-tray`
   - 新建 `tray/macos` Swift 工程，`NSStatusBar` 自管点击：双击打开管理面，单击等系统双击间隔后弹菜单，右键或 Control 单击立即弹菜单
   - 通过 HTTP 读取 `/api/status`，菜单项与 Windows 一致
   - 重启代理用 `launchctl kickstart -k gui/<uid>/com.dec.agent`；退出时 `launchctl bootout` 代理后自身退出
   - 打包为 `Dec Tray.app`（`LSUIElement`，无 Dock 图标），附图标资源
   - _需求：1.1、1.2、1.3、1.4、1.5、1.8、1.9、1.10、6.1、6.2_

- [ ] 8. 安装、自启动与卸载接入托盘
   - `dec-agent install` 在 macOS、Windows 上同时安装 `dec-tray` 并注册用户级自启动（LaunchAgent `com.dec.tray`、计划任务 `DecTray`），托盘异常退出由自启动机制拉起
   - `dec-agent uninstall` 清理托盘程序与自启动项
   - `dec-tray` 缺失或启动失败时 `dec-agent` 正常运行，只记录告警
   - Linux 检测托盘可用性，不可用则跳过
   - 测试：install、uninstall 生成与清理的服务定义
   - _需求：1.1、1.7、6.3、6.4、7.2、7.4、7.5、7.6_

- [ ] 9. CI 构建与下载通道
   - `release.yml` 增加 macOS runner 构建 Swift 托盘，Windows 托盘在现有 Linux 交叉编译中产出；两者与 `dec-agent` 同一 tag
   - 发布清单与服务端下载通道增加 `dec-tray` 产物
   - 校验主程序仍为 `CGO_ENABLED=0`
   - _需求：7.1、7.3_

- [ ] 10. 部署 DNS 记录与端到端验收
   - 新增部署脚本步骤：经 DNSPod API 确保 `local.dec.firoyang.com` A 记录指向 `127.0.0.1`
   - 扩展 `dec-agent acceptance`：检查证书已下发、`https://local.dec.firoyang.com/api/status` 可访问、`http://dec.localhost:<端口>` 可访问、日志只写单文件
   - 更新 `docs/USAGE.md` 的访问方式与托盘说明
   - _需求：4.3、4.7、4.9、5.1_
