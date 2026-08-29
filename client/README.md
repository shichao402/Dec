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

## 构建

```bash
npm run tauri build
```
