# Dec 管理客户端

独立 Tauri 2 桌面客户端，管理本机或远程 `dec-server`。不替代 `dec` TUI。

## 要求

- Node.js、Rust（stable）
- Windows 需 WebView2
- 本机连接会发现或拉起已安装的 `dec-server`

## 开发

```bash
cd client
npm install
npm run tauri dev
```

启动后先选连接（本机 / 远程 gRPC / SSH 隧道），再用 Bitwarden 主密码解锁。`dec-server` 启动后全局锁定，解锁成功后控制权与 BW session 同为 1 小时内存态。

连接会保存 Bitwarden 邮箱。主密码默认不保存；用户明确勾选后才通过统一的系统凭据接口写入 Windows Credential Manager、macOS Keychain 或 Linux Secret Service，不会进入 `connections.json`。取消勾选或删除连接时会同时删除对应凭据。Linux 构建静态携带 D-Bus 客户端依赖，桌面会话仍需提供 Secret Service（如 GNOME Keyring 或 KWallet）。

远程直连必须使用由系统信任根校验的 TLS；若服务只监听 loopback，使用 SSH 隧道。连接成功后的完整流程是：

1. 初始化设备私仓、IDE 与 Global 资产；
2. 显式选择服务器目录接管项目，或手动选择一个范围扫描已有 Dec 项目；
3. 初始化项目并选择家项目 / requires 资产；
4. Pull 后在结果卡查看落地数量、跳过原因、缺失项与警告。

受管项目列表保存在目标设备，移除管理不会删除项目文件。Global 请求始终使用空项目路径；项目请求始终携带目标服务器上的绝对路径。

`dec-server` 是一机单例：换过二进制后仍在运行的旧实例不会加载新方法，调用会返回「未知服务方法」。控制台把这类错误翻译成重启提示，可在错误条或「设备设置 → 服务实例」重启服务并重连；本机连接会拉起新二进制，远端由该设备的服务管理器负责重启。

## 异步交互

所有连接、读取、保存和长任务统一进入 Shell 级 action registry。动作按设备和工作区资源分域互斥：冲突按钮自动禁用，但侧栏导航与无冲突读取保持可用。任务切页不取消，运行进度在全局状态区持续可见；回到原页面后可继续读取结果。

`RunOperation` 会自动检查并旁观相同工作区的在飞任务。连接成功和受管项目变化后，Console 也会轮询 Global 与项目根，恢复本 Console 或 MCP 已发起的任务。页面组件不应自行增加 `busy` / `loading` / `saving` 状态；新增异步入口必须声明 action key、资源锁和读写类型。

## 构建

```bash
npm run tauri build
```
