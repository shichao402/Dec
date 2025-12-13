#!/bin/bash
# Dec 开发版安装脚本
# 用于从源码构建并安装到本地，覆盖现有安装
# 使用方法: ./scripts/install-dev.sh

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

print_info() { echo -e "${BLUE}ℹ${NC}  $1"; }
print_success() { echo -e "${GREEN}✓${NC}  $1"; }
print_warning() { echo -e "${YELLOW}⚠${NC}  $1"; }
print_error() { echo -e "${RED}✗${NC}  $1"; }
print_step() { echo -e "${CYAN}▶${NC}  $1"; }

# 获取脚本所在目录的父目录（项目根目录）
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

# 安装目录（与正式安装脚本保持一致）
if [ -n "${DEC_HOME}" ]; then
    INSTALL_DIR="${DEC_HOME}"
else
    INSTALL_DIR="${HOME}/.decs"
fi
BIN_DIR="${INSTALL_DIR}/bin"
BINARY_PATH="${BIN_DIR}/dec"

echo ""
echo "╔═══════════════════════════════════════╗"
echo "║   Dec 开发版安装脚本        ║"
echo "╚═══════════════════════════════════════╝"
echo ""

# 检查是否在项目目录
if [ ! -f "${PROJECT_DIR}/go.mod" ] || [ ! -f "${PROJECT_DIR}/version.json" ]; then
    print_error "请在 Dec 项目目录中运行此脚本"
    exit 1
fi

cd "${PROJECT_DIR}"

# 显示当前版本信息
print_info "项目目录: ${PROJECT_DIR}"
print_info "安装目录: ${INSTALL_DIR}"

# 读取版本号
VERSION=$(cat version.json 2>/dev/null | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4 || echo "dev")
print_info "源码版本: ${VERSION}"

# 检查当前安装的版本
if [ -x "${BINARY_PATH}" ]; then
    CURRENT_VERSION=$("${BINARY_PATH}" --version 2>&1 | grep -o 'v[0-9]\+\.[0-9]\+\.[0-9]\+' | head -1 || echo "unknown")
    print_info "当前安装版本: ${CURRENT_VERSION}"
fi

echo ""

# Step 1: 检查 Go 环境
print_step "检查 Go 环境..."
if ! command -v go &> /dev/null; then
    print_error "Go 未安装。请先安装 Go: https://golang.org/dl/"
    exit 1
fi
GO_VERSION=$(go version | awk '{print $3}')
print_success "Go 版本: ${GO_VERSION}"

# Step 2: 运行测试（可选）
if [ "$1" = "--test" ] || [ "$1" = "-t" ]; then
    print_step "运行单元测试..."
    if go test ./... -v; then
        print_success "测试通过"
    else
        print_error "测试失败，中止安装"
        exit 1
    fi
    echo ""
fi

# Step 3: 构建
print_step "构建 dec..."
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS="-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}"

# 检测当前平台
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "${ARCH}" in
    x86_64) ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
esac
PLATFORM="${OS}-${ARCH}"
print_info "目标平台: ${PLATFORM}"

mkdir -p dist
if go build -ldflags "${LDFLAGS}" -o dist/dec .; then
    print_success "构建成功"
else
    print_error "构建失败"
    exit 1
fi

# Step 4: 安装
print_step "安装到 ${BIN_DIR}..."
mkdir -p "${BIN_DIR}"

# 备份旧版本（如果存在）
if [ -f "${BINARY_PATH}" ]; then
    BACKUP_PATH="${BINARY_PATH}.backup"
    cp "${BINARY_PATH}" "${BACKUP_PATH}"
    print_info "已备份旧版本到: ${BACKUP_PATH}"
fi

# 复制新版本
cp dist/dec "${BINARY_PATH}"
chmod +x "${BINARY_PATH}"
print_success "安装完成"

# Step 5: 复制系统配置文件
print_step "安装系统配置..."
CONFIG_DIR="${INSTALL_DIR}/config"
mkdir -p "${CONFIG_DIR}"

if [ -f "${PROJECT_DIR}/config/system.json" ]; then
    cp "${PROJECT_DIR}/config/system.json" "${CONFIG_DIR}/system.json"
    print_success "系统配置已安装"
else
    print_warning "未找到 config/system.json，将使用内置默认值"
fi

# Step 6: 包开发指南说明
print_step "包开发指南..."
print_info "包开发文档现已通过 CursorColdStart 的 dec pack 提供"
print_info "在包项目中运行: coldstart enable dec && coldstart init ."

# Step 7: 清理构建产物
rm -f dist/dec
print_info "已清理本地构建产物"

# Step 8: 验证安装
print_step "验证安装..."
INSTALLED_VERSION=$("${BINARY_PATH}" --version 2>&1 || echo "unknown")
print_success "安装版本: ${INSTALLED_VERSION}"

echo ""
echo "╔═══════════════════════════════════════╗"
echo "║         开发版安装完成！              ║"
echo "╚═══════════════════════════════════════╝"
echo ""
print_info "安装位置: ${BINARY_PATH}"
echo ""
print_info "使用方法:"
echo "  dec --help"
echo "  dec list"
echo ""
print_warning "注意: 这是开发版本，仅用于本地测试"
echo ""

# 提示下一步
print_info "下一步操作:"
echo "  1. 验证各项功能"
echo "  2. 提交代码到 main 分支"
echo "  3. 推送 test tag: git tag test-vX.X.X && git push origin test-vX.X.X"
echo "  4. 测试通过后推送正式 tag: git tag vX.X.X && git push origin vX.X.X"
echo ""
