# third_party

本目录不存放手拷贝的上游源码。

`relkit/` 由 `scripts/relkit_consume.py` sparse-checkout 自
https://github.com/shichao402/relkit（版本由 `scripts/relkit.lock.json` 决定，可用 `RELKIT_REF` / `--ref` 覆盖）。

`scripts/relkit_consume.py` 是上游 `scripts/host/relkit_consume.py` 的逐字节副本，
本仓不改；cone、CNB token 注入、`go build` 全在该 SHA 的 `scripts/consume.py` 里。

`go.mod` 通过 replace 指向本目录：

```
replace go.firoyang.com/relkit => ./third_party/relkit
```

构建 / CI 会自动确保稀疏树存在；日常也可手动：

```bat
python scripts\relkit_consume.py --sdk-only
```

```bash
python3 scripts/relkit_consume.py --sdk-only
```

发布流水线需要 `cmd/relkit` 时去掉 `--sdk-only`，或加 `--build-cli`。
