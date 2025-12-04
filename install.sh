#!/bin/bash
# CursorToolset 一键安装脚本 (Linux/macOS)
# 使用方法: curl -fsSL https://raw.githubusercontent.com/firoyang/CursorToolset/main/install.sh | bash

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 打印带颜色的消息
print_info() {
    echo -e "${BLUE}ℹ${NC}  $1"
}

print_success() {
    echo -e "${GREEN}✓${NC}  $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC}  $1"
}

print_error() {
    echo -e "${RED}✗${NC}  $1"
}

# 检测操作系统和架构
detect_platform() {
    local os=""
    local arch=""
    
    case "$(uname -s)" in
        Darwin*)
            os="darwin"
            ;;
        Linux*)
            os="linux"
            ;;
        *)
            print_error "不支持的操作系统: $(uname -s)"
            exit 1
            ;;
    esac
    
    case "$(uname -m)" in
        x86_64)
            arch="amd64"
            ;;
        arm64|aarch64)
            arch="arm64"
            ;;
        *)
            print_error "不支持的架构: $(uname -m)"
            exit 1
            ;;
    esac
    
    echo "${os}-${arch}"
}

# 检查依赖
check_dependencies() {
    print_info "检查依赖..."
    
    if ! command -v git &> /dev/null; then
        print_error "Git 未安装。请先安装 Git。"
        exit 1
    fi
    
    if ! command -v curl &> /dev/null; then
        print_error "curl 未安装。请先安装 curl。"
        exit 1
    fi
    
    print_success "依赖检查通过"
}

# 主安装函数
main() {
    echo ""
    echo "╔═══════════════════════════════════════╗"
    echo "║   CursorToolset 一键安装脚本          ║"
    echo "╚═══════════════════════════════════════╝"
    echo ""
    
    # 检查依赖
    check_dependencies
    
    # 检测平台
    PLATFORM=$(detect_platform)
    print_info "检测到平台: ${PLATFORM}"
    
    # 定义安装路径
    INSTALL_DIR="${HOME}/.cursor/toolsets/CursorToolset"
    BIN_DIR="${INSTALL_DIR}/bin"
    BINARY_PATH="${BIN_DIR}/cursortoolset"
    
    print_info "安装目录: ${INSTALL_DIR}"
    
    # 创建安装目录
    print_info "创建安装目录..."
    mkdir -p "${BIN_DIR}"
    
    # 克隆仓库到临时目录
    TEMP_DIR=$(mktemp -d)
    print_info "克隆仓库到临时目录: ${TEMP_DIR}"
    
    if ! git clone --depth 1 https://github.com/firoyang/CursorToolset.git "${TEMP_DIR}"; then
        print_error "克隆仓库失败"
        rm -rf "${TEMP_DIR}"
        exit 1
    fi
    
    # 检查是否安装了 Go
    if command -v go &> /dev/null; then
        print_info "使用 Go 构建..."
        cd "${TEMP_DIR}"
        
        if ! go build -o "${BINARY_PATH}" .; then
            print_error "构建失败"
            rm -rf "${TEMP_DIR}"
            exit 1
        fi
        
        print_success "构建成功"
    else
        print_warning "Go 未安装，尝试下载预编译版本..."
        
        # 获取最新版本号
        LATEST_VERSION=$(curl -fsSL https://api.github.com/repos/firoyang/CursorToolset/releases/latest | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/' 2>/dev/null || echo "")
        
        if [ -z "${LATEST_VERSION}" ]; then
            print_warning "无法获取最新版本，使用 main 分支构建"
            print_error "请先安装 Go："
            print_error "  macOS: brew install go"
            print_error "  Linux: https://go.dev/doc/install"
            rm -rf "${TEMP_DIR}"
            exit 1
        fi
        
        # 下载预编译版本
        BINARY_NAME="cursortoolset-${PLATFORM}"
        DOWNLOAD_URL="https://github.com/firoyang/CursorToolset/releases/download/${LATEST_VERSION}/${BINARY_NAME}"
        
        print_info "下载 ${LATEST_VERSION} 版本..."
        if curl -fsSL -o "${BINARY_PATH}" "${DOWNLOAD_URL}"; then
            chmod +x "${BINARY_PATH}"
            print_success "下载成功"
        else
            print_error "下载失败，请先安装 Go 并重试"
            rm -rf "${TEMP_DIR}"
            exit 1
        fi
    fi
    
    # 复制配置文件
    print_info "复制配置文件..."
    cp "${TEMP_DIR}/available-toolsets.json" "${INSTALL_DIR}/"
    
    # 清理临时目录
    rm -rf "${TEMP_DIR}"
    
    # 添加到 PATH
    print_info "配置环境变量..."
    
    SHELL_RC=""
    case "${SHELL}" in
        */zsh)
            SHELL_RC="${HOME}/.zshrc"
            ;;
        */bash)
            if [[ "$(uname -s)" == "Darwin" ]]; then
                SHELL_RC="${HOME}/.bash_profile"
            else
                SHELL_RC="${HOME}/.bashrc"
            fi
            ;;
        *)
            print_warning "未识别的 Shell: ${SHELL}"
            ;;
    esac
    
    if [[ -n "${SHELL_RC}" ]]; then
        # 检查是否已经添加
        if ! grep -q "cursortoolset" "${SHELL_RC}" 2>/dev/null; then
            echo "" >> "${SHELL_RC}"
            echo "# CursorToolset" >> "${SHELL_RC}"
            echo "export PATH=\"${BIN_DIR}:\$PATH\"" >> "${SHELL_RC}"
            print_success "已添加到 ${SHELL_RC}"
        else
            print_info "环境变量已存在，跳过"
        fi
    fi
    
    # 验证安装
    print_info "验证安装..."
    if [[ -x "${BINARY_PATH}" ]]; then
        VERSION=$("${BINARY_PATH}" --version 2>&1 || echo "unknown")
        print_success "安装成功！版本: ${VERSION}"
    else
        print_error "安装失败：可执行文件不存在或无执行权限"
        exit 1
    fi
    
    echo ""
    echo "╔═══════════════════════════════════════╗"
    echo "║         安装完成！                    ║"
    echo "╚═══════════════════════════════════════╝"
    echo ""
    print_info "安装位置: ${BINARY_PATH}"
    echo ""
    print_info "请运行以下命令使环境变量生效："
    if [[ -n "${SHELL_RC}" ]]; then
        echo "  source ${SHELL_RC}"
    fi
    echo ""
    print_info "或者直接使用完整路径："
    echo "  ${BINARY_PATH} --help"
    echo ""
    print_info "之后可以在任何位置运行："
    echo "  cursortoolset install"
    echo "  cursortoolset list"
    echo "  cursortoolset update"
    echo ""
}

# 运行主函数
main

