#!/bin/sh
# goenv.sh — 让 Go 的所有状态都留在本仓库内，不写到 ~/Library、~/go 等全局位置。
#
# 用法（在仓库根）：
#   . scripts/goenv.sh && go build ./...
#   sh scripts/goenv.sh go build ./...
#   make test            # Makefile 已内置
#
# 为什么需要它：
#   Go 默认把构建缓存放在 ~/Library/Caches/go-build（macOS）或 ~/.cache/go-build，
#   把模块缓存放在 ~/go/pkg/mod，把工具链放在 ~/go/pkg/mod/golang.org/toolchain。
#   在别人机器上跑构建时，这些目录会被悄悄创建和写满，既不可见也不好清理。
#   这里全部重定向到 .cache/go/（已被 .gitignore 忽略）。
#
# GOWORK=off：仓库根的 go.work 指向 ../relkit（本地开发用）。不关闭的话，
#   构建会依赖仓库外的目录，CI 和别人的机器上会直接失败。本项目以 go.mod 为准。
#
# 注意：设了 GOMODCACHE 后，模块会重新下载一份到项目内（首次会慢一些）。

DEC_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

DEC_GO_CACHE_DIR="$DEC_ROOT/.cache/go"
GOCACHE="$DEC_GO_CACHE_DIR/build"
GOMODCACHE="$DEC_GO_CACHE_DIR/mod"
GOPATH="$DEC_GO_CACHE_DIR/path"
GOTMPDIR="$DEC_GO_CACHE_DIR/tmp"
GOWORK=off
GOFLAGS="${GOFLAGS:--mod=mod}"

mkdir -p "$GOCACHE" "$GOMODCACHE" "$GOPATH" "$GOTMPDIR"

export GOCACHE GOMODCACHE GOPATH GOTMPDIR GOWORK GOFLAGS DEC_GO_CACHE_DIR

# 带参数调用：sh scripts/goenv.sh <command...>，执行完即退出。
if [ "$#" -gt 0 ]; then
	exec "$@"
fi
