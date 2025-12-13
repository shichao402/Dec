# Dec Makefile

# 变量定义
BINARY_NAME=dec

# 从 version.json 读取版本号
VERSION=$(shell cat version.json 2>/dev/null | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4 || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# 开发环境变量设置
# 本项目开发过程中，任何命令的执行都需要export环境变量根路径到本项目的.root目录
ROOT_DIR=$(shell pwd)/.root
export DEC_ROOT=$(ROOT_DIR)

# 确保 .root 目录存在
.root:
	@mkdir -p .root
	@echo "📁 创建开发根目录: $(ROOT_DIR)"
	@echo "🔧 设置环境变量 DEC_ROOT=$(ROOT_DIR)"

# 默认目标
.PHONY: all
all: .root build

# 构建当前平台版本
.PHONY: build
build: .root
	@if [ ! -f "version.json" ]; then \
		echo "❌ 错误: version.json 文件不存在"; \
		exit 1; \
	fi
	@mkdir -p dist
	@echo "🔨 构建 $(BINARY_NAME)..."
	@echo "📌 版本: $(VERSION)"
	@echo "🔧 使用开发根目录: $(DEC_ROOT)"
	go build $(LDFLAGS) -o dist/$(BINARY_NAME) .
	@echo "✅ 构建完成: dist/$(BINARY_NAME)"

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
test: .root
	@echo "🧪 运行测试..."
	@echo "🔧 使用开发根目录: $(DEC_ROOT)"
	go test ./... -v -cover

# 运行所有测试（包括集成测试）
.PHONY: test-all
test-all: .root test
	@echo ""
	@echo "🧪 运行集成测试..."
	@echo "🔧 使用开发根目录: $(DEC_ROOT)"
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

# 清理开发根目录
.PHONY: clean-root
clean-root:
	@echo "🧹 清理开发根目录..."
	@if [ -d ".root" ]; then \
		rm -rf .root; \
		echo "✅ 开发根目录已清理"; \
	else \
		echo "ℹ️  开发根目录不存在，无需清理"; \
	fi

# 安装到本地（使用环境变量指定的路径或默认路径）
.PHONY: install
install: build
	@if [ -n "$$DEC_ROOT" ]; then \
		INSTALL_DIR="$$DEC_ROOT"; \
		echo "📦 安装到环境变量指定的目录: $$INSTALL_DIR"; \
	else \
		INSTALL_DIR="$$HOME/.cursor/toolsets/Dec"; \
		echo "📦 安装到默认目录: $$INSTALL_DIR"; \
	fi; \
	mkdir -p "$$INSTALL_DIR/bin"; \
	cp $(BINARY_NAME) "$$INSTALL_DIR/bin/"; \
	cp available-toolsets.json "$$INSTALL_DIR/"; \
	echo "✅ 安装完成"; \
	echo ""; \
	echo "💡 请确保 $$INSTALL_DIR/bin 在您的 PATH 中"

# 本地开发安装（覆盖系统安装的 dec）
.PHONY: install-local
install-local: build
	@echo "📦 本地开发安装..."
	@./scripts/install-dev.sh
	@echo "✅ 本地开发安装完成"

# 快速本地安装（跳过测试）
.PHONY: install-dev
install-dev:
	@echo "📦 快速开发安装..."
	@./scripts/install-dev.sh
	@echo "✅ 快速开发安装完成"

# 本地安装并运行测试
.PHONY: install-dev-test
install-dev-test:
	@echo "📦 开发安装（含测试）..."
	@./scripts/install-dev.sh --test
	@echo "✅ 开发安装完成"

# 格式化代码
.PHONY: fmt
fmt: .root
	@echo "📝 格式化代码..."
	@echo "🔧 使用开发根目录: $(DEC_ROOT)"
	go fmt ./...
	@echo "✅ 格式化完成"

# 代码检查
.PHONY: lint
lint: .root
	@echo "🔍 代码检查..."
	@echo "🔧 使用开发根目录: $(DEC_ROOT)"
	golangci-lint run ./...

# 显示帮助
.PHONY: help
help:
	@echo "Dec Makefile"
	@echo ""
	@echo "构建目标："
	@echo "  make build          - 构建当前平台版本"
	@echo "  make build-all      - 构建所有平台版本"
	@echo ""
	@echo "测试目标："
	@echo "  make test           - 运行单元测试"
	@echo "  make test-all       - 运行所有测试"
	@echo ""
	@echo "安装目标："
	@echo "  make install        - 安装到开发目录"
	@echo "  make install-dev    - 快速开发安装（覆盖系统安装）"
	@echo "  make install-dev-test - 开发安装（含测试）"
	@echo ""
	@echo "其他目标："
	@echo "  make clean          - 清理构建产物"
	@echo "  make clean-root     - 清理开发根目录"
	@echo "  make fmt            - 格式化代码"
	@echo "  make lint           - 代码检查"
	@echo ""
	@echo "开发流程："
	@echo "  1. make build       - 本地构建检查"
	@echo "  2. make test        - 运行测试"
	@echo "  3. make install-dev - 本地安装验证"
	@echo "  4. 提交代码到 main 分支"
	@echo "  5. 推送 test tag 触发 CI 构建"
	@echo "  6. 测试通过后推送正式 tag 发布"
	@echo ""

