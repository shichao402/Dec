# Dec Makefile

BINARY_NAME=dec
DIST_DIR=dist
VERSION=$(shell cat version.json 2>/dev/null | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4 || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

.PHONY: all build build-all test test-self-host clean install-dev install-dev-test fmt lint help

all: build

build:
	@mkdir -p $(DIST_DIR)
	@echo "🔨 构建 $(BINARY_NAME)..."
	@echo "📌 版本: $(VERSION)"
	go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME) .
	@echo "✅ 构建完成: $(DIST_DIR)/$(BINARY_NAME)"

build-all:
	@./scripts/build.sh --all

test:
	@echo "🧪 运行 Go 单元测试..."
	go test ./... -v -cover

test-self-host:
	@echo "🧪 运行自托管流程测试..."
	@./scripts/run-tests.sh

clean:
	@echo "🧹 清理构建产物..."
	rm -f $(BINARY_NAME)
	rm -rf $(DIST_DIR) logs/
	@echo "✅ 清理完成"

install-dev:
	@./scripts/install-dev.sh

install-dev-test:
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
	@echo "  make test-self-host  - 运行自托管流程测试"
	@echo ""
	@echo "安装目标："
	@echo "  make install-dev     - 安装当前源码到本地"
	@echo "  make install-dev-test - 安装前先运行单元测试"
	@echo ""
	@echo "其他目标："
	@echo "  make clean           - 清理构建产物"
	@echo "  make fmt             - 格式化 Go 代码"
	@echo "  make lint            - 运行 golangci-lint"
