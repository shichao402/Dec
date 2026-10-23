# Dec Makefile

# 所有 Go 状态（构建缓存、模块缓存、临时目录）都留在仓库内 .cache/go，
# 不写 ~/Library/Caches 或 ~/go。每条 go 命令前面都带 GOENV 前缀。
# 详见 scripts/goenv.sh。
GOENV=sh scripts/goenv.sh

# 断代后只剩两个二进制（需求 3.4）：云端服务端与本机代理。
BINARY_NAMES=dec-cloud dec-agent
DIST_DIR=dist
VERSION=$(shell cat version.json 2>/dev/null | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4 || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

.PHONY: all build test vet clean fmt lint install help

all: build

build:
	@mkdir -p $(DIST_DIR)
	@echo "🔨 构建 Dec 程序组..."
	@echo "📌 版本: $(VERSION)"
	$(GOENV) go build $(LDFLAGS) -o $(DIST_DIR)/dec-cloud ./cmd/dec-cloud
	$(GOENV) go build $(LDFLAGS) -o $(DIST_DIR)/dec-agent ./cmd/dec-agent
	@echo "✅ 构建完成: $(DIST_DIR)/{dec-cloud,dec-agent}"

test:
	@echo "🧪 运行 Go 单元测试..."
	$(GOENV) go test ./...

vet:
	@echo "🔍 代码检查..."
	$(GOENV) go vet ./...

install: build
	@echo "📦 安装 dec-agent..."
	@install -d "$$HOME/.local/bin"
	@install -m 0755 $(DIST_DIR)/dec-agent "$$HOME/.local/bin/dec-agent"
	@echo "✅ 已安装到 ~/.local/bin/dec-agent"

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
	@echo "  make build           - 构建当前平台的 dec-cloud 与 dec-agent"
	@echo "  make install         - 构建并安装 dec-agent 到 ~/.local/bin"
	@echo ""
	@echo "测试目标："
	@echo "  make test            - 运行 Go 单元测试"
	@echo "  make vet             - 运行 go vet"
	@echo ""
	@echo "其他目标："
	@echo "  make clean           - 清理构建产物"
	@echo "  make fmt             - 格式化 Go 代码"
	@echo "  make lint            - 运行 go vet"
