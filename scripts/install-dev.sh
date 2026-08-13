#!/bin/bash
# Dec 开发版安装脚本
# 使用当前源码构建并安装到本地

set -e

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

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
INSTALL_DIR="${DEC_HOME:-${HOME}/.dec}"
BIN_DIR="${INSTALL_DIR}/bin"
BINARY_PATH="${BIN_DIR}/dec"
BINARY_NAMES=("dec" "dec-server" "dec-mcp" "dec-exec")

if [ ! -f "${PROJECT_DIR}/go.mod" ] || [ ! -f "${PROJECT_DIR}/version.json" ]; then
    print_error "请在 Dec 项目目录中运行此脚本"
    exit 1
fi

cd "${PROJECT_DIR}"
VERSION=$(grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' version.json | cut -d'"' -f4 || echo "dev")
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS="-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}"

echo ""
echo "╔═══════════════════════════════════════╗"
echo "║       Dec 开发版安装脚本             ║"
echo "╚═══════════════════════════════════════╝"
echo ""
print_info "项目目录: ${PROJECT_DIR}"
print_info "安装目录: ${INSTALL_DIR}"
print_info "源码版本: ${VERSION}"

if [ "$1" = "--test" ] || [ "$1" = "-t" ]; then
    print_step "运行 Go 单元测试..."
    go test ./... -v
    print_success "测试通过"
fi

print_step "构建当前源码..."
mkdir -p dist
for binary in "${BINARY_NAMES[@]}"; do
    package="."
    if [ "${binary}" != "dec" ]; then package="./cmd/${binary}"; fi
    go build -ldflags "${LDFLAGS}" -o "dist/${binary}" "${package}" || {
        print_error "${binary} 构建失败"
        exit 1
    }
done
print_success "四个程序构建完成"

print_step "安装到本地目录..."
mkdir -p "${BIN_DIR}"
for binary in "${BINARY_NAMES[@]}"; do
    target="${BIN_DIR}/${binary}"
    # 先删除旧二进制再写入，避免 macOS 上 stuck exec（UE 态）占用同一 inode 导致新进程卡在 dyld。
    rm -f "${target}"
    cp "dist/${binary}" "${target}"
    chmod +x "${target}"
done
# macOS: 清除可能的扩展属性以避免系统阻止执行
if [ "$(uname -s)" = "Darwin" ]; then
    for binary in "${BINARY_NAMES[@]}"; do
        xattr -cr "${BIN_DIR}/${binary}" 2>/dev/null || true
    done
fi
print_success "已写入 ${BIN_DIR}/{dec,dec-server,dec-mcp,dec-exec}"

for binary in "${BINARY_NAMES[@]}"; do rm -f "dist/${binary}"; done
print_step "验证安装..."
INSTALLED_VERSION=$("${BINARY_PATH}" --version 2>&1 || echo "unknown")
print_success "安装版本: ${INSTALLED_VERSION}"

echo ""
print_info "后续可执行："
echo "  dec --help"
echo "  dec            # 打开 TUI；Settings 中连接仓库"
echo ""
print_warning "这是开发安装，适合本地调试，不会自动发布任何版本"
