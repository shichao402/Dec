# third_party

本目录不存放手拷贝的上游源码。

`relkit/` 由 `python scripts/host/relkit_host.py install` 从
checksum-pinned 的 `relkit-sdk-go.zip` 安装。版本由 `scripts/relkit.lock.json`
（schema `relkit.consume/2`）决定。

CLI 与 updater 不进这个目录，它们装到 `tools/bin/`。

`go.mod` 通过 replace 指向本目录：

```
replace go.firoyang.com/relkit => ./third_party/relkit
```

构建 / CI 会自动确保附件已安装；日常也可手动：

```bat
python scripts\host\relkit_host.py install
```

```bash
python3 scripts/host/relkit_host.py install
```
