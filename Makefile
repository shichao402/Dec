# CursorToolset Makefile

# 变量定义
BINARY_NAME=cursortoolset
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# 默认目标
.PHONY: all
all: build

# 构建当前平台版本
.PHONY: build
build:
	@echo "🔨 构建 $(BINARY_NAME)..."
	go build $(LDFLAGS) -o $(BINARY_NAME) .
	@echo "✅ 构建完成: $(BINARY_NAME)"

# 构建所有平台版本
.PHONY: build-all
build-all: clean
	@echo "🏗️  开始跨平台构建..."
	@mkdir -p dist
	
	@echo "📦 构建 Linux AMD64..."
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY_NAME)-linux-amd64 .
	
	@echo "📦 构建 Linux ARM64..."
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o dist/$(BINARY_NAME)-linux-arm64 .
	
	@echo "📦 构建 macOS AMD64..."
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY_NAME)-darwin-amd64 .
	
	@echo "📦 构建 macOS ARM64..."
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o dist/$(BINARY_NAME)-darwin-arm64 .
	
	@echo "📦 构建 Windows AMD64..."
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o dist/$(BINARY_NAME)-windows-amd64.exe .
	
	@echo ""
	@echo "✅ 所有平台构建完成！"
	@echo ""
	@echo "📊 构建产物:"
	@ls -lh dist/
	@echo ""

# 运行测试
.PHONY: test
test:
	@echo "🧪 运行测试..."
	go test ./... -v -cover

# 运行所有测试（包括集成测试）
.PHONY: test-all
test-all: test
	@echo ""
	@echo "🧪 运行集成测试..."
	@./test-install.sh
	@echo ""
	@./test-clean.sh
	@echo ""
	@./test-update.sh

# 清理构建产物
.PHONY: clean
clean:
	@echo "🧹 清理构建产物..."
	rm -f $(BINARY_NAME)
	rm -rf dist/
	@echo "✅ 清理完成"

# 安装到本地
.PHONY: install
install: build
	@echo "📦 安装到 ~/.cursor/toolsets/CursorToolset/..."
	@mkdir -p ~/.cursor/toolsets/CursorToolset/bin
	@cp $(BINARY_NAME) ~/.cursor/toolsets/CursorToolset/bin/
	@cp available-toolsets.json ~/.cursor/toolsets/CursorToolset/
	@echo "✅ 安装完成"
	@echo ""
	@echo "💡 请确保 ~/.cursor/toolsets/CursorToolset/bin 在您的 PATH 中"

# 格式化代码
.PHONY: fmt
fmt:
	@echo "📝 格式化代码..."
	go fmt ./...
	@echo "✅ 格式化完成"

# 代码检查
.PHONY: lint
lint:
	@echo "🔍 代码检查..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "⚠️  golangci-lint 未安装，跳过"; \
	fi

# 显示帮助
.PHONY: help
help:
	@echo "CursorToolset Makefile"
	@echo ""
	@echo "可用目标："
	@echo "  make build      - 构建当前平台版本"
	@echo "  make build-all  - 构建所有平台版本"
	@echo "  make test       - 运行单元测试"
	@echo "  make test-all   - 运行所有测试"
	@echo "  make clean      - 清理构建产物"
	@echo "  make install    - 安装到本地"
	@echo "  make fmt        - 格式化代码"
	@echo "  make lint       - 代码检查"
	@echo ""

