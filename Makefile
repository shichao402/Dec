# Dec Makefile

BINARY_NAMES=dec dec-server dec-mcp dec-exec
DIST_DIR=dist
VERSION=$(shell cat version.json 2>/dev/null | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4 || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

.PHONY: all build build-all test clean install-dev install-dev-test fmt lint help ensure-relkit

all: build

# go.mod replace → ./third_party/relkit；缺失时 sparse-checkout。
ensure-relkit:
	@python3 scripts/ensure_relkit_sparse.py --sdk-only --allow-stale

build: ensure-relkit
	@mkdir -p $(DIST_DIR)
	@echo "🔨 构建 Dec 程序组..."
	@echo "📌 版本: $(VERSION)"
	go build $(LDFLAGS) -o $(DIST_DIR)/dec .
	go build $(LDFLAGS) -o $(DIST_DIR)/dec-server ./cmd/dec-server
	go build $(LDFLAGS) -o $(DIST_DIR)/dec-mcp ./cmd/dec-mcp
	go build $(LDFLAGS) -o $(DIST_DIR)/dec-exec ./cmd/dec-exec
	@echo "✅ 构建完成: $(DIST_DIR)/{dec,dec-server,dec-mcp,dec-exec}"

build-all: ensure-relkit
	@./scripts/build.sh --all

test: ensure-relkit
	@echo "🧪 运行 Go 单元测试..."
	go test ./... -v -cover

clean:
	@echo "🧹 清理构建产物..."
	rm -f $(BINARY_NAMES)
	rm -rf $(DIST_DIR) logs/
	@echo "✅ 清理完成"

install-dev: ensure-relkit
	@./scripts/install-dev.sh

install-dev-test: ensure-relkit
	@./scripts/install-dev.sh --test

fmt:
	@echo "📝 格式化代码..."
	go fmt ./...
	@echo "✅ 格式化完成"

lint:
	@echo "🔍 代码检查..."
	golangci-lint run ./...

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
	@echo "安装目标："
	@echo "  make install-dev     - 安装当前源码到本地"
	@echo "  make install-dev-test - 安装前先运行单元测试"
	@echo ""
	@echo "其他目标："
	@echo "  make ensure-relkit   - sparse-checkout relkit → third_party/relkit"
	@echo "  make clean           - 清理构建产物"
	@echo "  make fmt             - 格式化 Go 代码"
	@echo "  make lint            - 运行 golangci-lint"
