# third_party

本目录不存放手拷贝的上游源码。

`relkit/` 由 `python scripts/host/relkit_host.py install` 从
checksum-pinned 的 `relkit-sdk-go.zip` 安装。版本由 `scripts/relkit.lock.json`
（schema `relkit.consume/2`）决定。

CLI 与 updater 不进这个目录，它们装到 `tools/bin/`。

Go SDK 已改为直接依赖 `github.com/shichao402/relkit`（见 go.mod），
`third_party/relkit/` 下的 Go SDK 目录仅作为 lock 安装产物保留（Rust facade、
TypeScript 绑定仍从本目录消费），Go 构建不再 replace 到这里。日常安装附件：

```bat
python scripts\host\relkit_host.py install
```

```bash
python3 scripts/host/relkit_host.py install
```
