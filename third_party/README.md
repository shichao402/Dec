# third_party

本目录不存放手拷贝的上游源码。

`relkit/` 由 `scripts/ensure_relkit_sparse` sparse-checkout 自
https://cnb.cool/shichao402/relkit（默认钉住已验证 commit `6c78d29`，可用 `RELKIT_REF` 覆盖）。

`go.mod` 通过 replace 指向本目录：

```
replace cnb.cool/shichao402/relkit => ./third_party/relkit
```

构建 / CI 会自动确保稀疏树存在；日常也可手动：

```bat
scripts\ensure_relkit_sparse.bat --sdk-only
```

```bash
./scripts/ensure_relkit_sparse.sh --sdk-only
```

发布流水线需要 `cmd/relkit` 时去掉 `--sdk-only`，或加 `--build-cli`。
