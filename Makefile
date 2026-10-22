# Dec Makefile

# 所有 Go 状态（构建缓存、模块缓存、临时目录）都留在仓库内 .cache/go，
# 不写 ~/Library/Caches 或 ~/go。每条 go 命令前面都带 GOENV 前缀。
# 详见 scripts/goenv.sh。
GOENV=sh scripts/goenv.sh

BINARY_NAMES=dec-server dec-mcp dec-exec dec-host-setup
DIST_DIR=dist
VERSION=$(shell cat version.json 2>/dev/null | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4 || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

.PHONY: all build build-all test clean fmt lint help ensure-relkit

all: build

# go.mod replace → ./third_party/relkit；缺失时 sparse-checkout。
ensure-relkit:
	@python3 scripts/ensure_relkit_sparse.py --sdk-only --allow-stale

build: ensure-relkit
	@mkdir -p $(DIST_DIR)
	@echo "🔨 构建 Dec 程序组..."
	@echo "📌 版本: $(VERSION)"
	$(GOENV) go build $(LDFLAGS) -o $(DIST_DIR)/dec-server ./cmd/dec-server
	$(GOENV) go build $(LDFLAGS) -o $(DIST_DIR)/dec-mcp ./cmd/dec-mcp
	$(GOENV) go build $(LDFLAGS) -o $(DIST_DIR)/dec-exec ./cmd/dec-exec
	$(GOENV) go build $(LDFLAGS) -o $(DIST_DIR)/dec-host-setup ./cmd/dec-host-setup
	@echo "✅ 构建完成: $(DIST_DIR)/{dec-server,dec-mcp,dec-exec,dec-host-setup}"

build-all: ensure-relkit
	@./scripts/build.sh --all

test: ensure-relkit
	@echo "🧪 运行 Go 单元测试..."
	$(GOENV) go test ./... -v -cover

clean:
	@echo "🧹 清理构建产物..."
	rm -f $(BINARY_NAMES)
	rm -rf $(DIST_DIR) logs/
	@echo "✅ 清理完成"

fmt:
	@echo "📝 格式化代码..."
	$(GOENV) go fmt ./...
	@echo "✅ 格式化完成"

lint:
	@echo "🔍 代码检查..."
	$(GOENV) go vet ./...

help:
	@echo "Dec Makefile"
	@echo ""
	@echo "构建目标："
	@echo "  make build           - 构建当前平台版本"
	@echo "  make build-all       - 构建全部平台版本"
	@echo ""
	@echo "测试目标："
	@echo "  make test            - 运行 Go 单元测试"
	@echo ""
	@echo "本机部署（唯一入口，不经 make）："
	@echo "  python scripts/build-console.py --skip-deps   # 打 Console 安装包并覆盖安装"
	@echo ""
	@echo "其他目标："
	@echo "  make ensure-relkit   - sparse-checkout relkit → third_party/relkit"
	@echo "  make clean           - 清理构建产物"
	@echo "  make fmt             - 格式化 Go 代码"
	@echo "  make lint            - 运行 golangci-lint"
