#!/bin/bash
# CursorToolset 一键安装脚本 (Linux/macOS)
# 使用方法: curl -fsSL https://raw.githubusercontent.com/shichao402/CursorToolset/ReleaseLatest/install.sh | bash

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
    
    if ! command -v curl &> /dev/null; then
        print_error "curl 未安装。请先安装 curl。"
        exit 1
    fi
    
    print_success "依赖检查通过"
}

# 比较版本号（简单比较，v1.0.1 > v1.0.0）
compare_versions() {
    local v1="$1"
    local v2="$2"
    
    # 移除 v 前缀
    v1="${v1#v}"
    v2="${v2#v}"
    
    # 分割版本号
    IFS='.' read -ra v1_parts <<< "$v1"
    IFS='.' read -ra v2_parts <<< "$v2"
    
    # 比较每个部分
    for i in 0 1 2; do
        local v1_part="${v1_parts[$i]:-0}"
        local v2_part="${v2_parts[$i]:-0}"
        
        if [ "$v1_part" -gt "$v2_part" ]; then
            echo "1"
            return
        elif [ "$v1_part" -lt "$v2_part" ]; then
            echo "-1"
            return
        fi
    done
    
    echo "0"
}

# 主安装函数
main() {
    echo ""
    echo "╔═══════════════════════════════════════╗"
    echo "║   CursorToolset 一键安装脚本          ║"
    echo "╚═══════════════════════════════════════╗"
    echo ""
    
    # 检查依赖
    check_dependencies
    
    # 检测平台
    PLATFORM=$(detect_platform)
    print_info "检测到平台: ${PLATFORM}"
    
    # 定义安装路径（新设计：统一使用环境目录）
    # 优先使用环境变量 CURSOR_TOOLSET_HOME，如果未设置则使用默认路径
    # 新路径设计（类似 pip/brew）：
    # ~/.cursortoolsets/             <- 根目录 (独立于 .cursor 系统目录)
    # ├── bin/                       <- CursorToolset 可执行文件
    # ├── repos/                     <- 工具集仓库源码
    # └── config/                    <- 配置文件
    if [ -n "${CURSOR_TOOLSET_HOME}" ]; then
        INSTALL_DIR="${CURSOR_TOOLSET_HOME}"
    else
        INSTALL_DIR="${HOME}/.cursortoolsets"
    fi
    BIN_DIR="${INSTALL_DIR}/bin"
    CONFIG_DIR="${INSTALL_DIR}/config"
    REPOS_DIR="${INSTALL_DIR}/repos"
    BINARY_PATH="${BIN_DIR}/cursortoolset"
    CONFIG_PATH="${CONFIG_DIR}/available-toolsets.json"
    
    print_info "安装目录: ${INSTALL_DIR}"
    if [ -n "${CURSOR_TOOLSET_HOME}" ]; then
        print_info "使用环境变量 CURSOR_TOOLSET_HOME: ${CURSOR_TOOLSET_HOME}"
    fi
    
    # 从 ReleaseLatest 分支获取版本号（唯一来源）
    print_info "获取最新版本号..."
    VERSION_JSON=$(curl -fsSL "https://raw.githubusercontent.com/shichao402/CursorToolset/ReleaseLatest/version.json" 2>/dev/null)
    
    if [ -z "${VERSION_JSON}" ]; then
        print_error "无法从 ReleaseLatest 分支获取版本信息"
        print_error "请检查网络连接或稍后重试"
        exit 1
    fi
    
    LATEST_VERSION=$(echo "${VERSION_JSON}" | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4)
    
    if [ -z "${LATEST_VERSION}" ]; then
        print_error "无法解析版本号"
        exit 1
    fi
    
    print_info "最新版本: ${LATEST_VERSION}"
    
    # 检查是否已安装
    CURRENT_VERSION=""
    if [ -x "${BINARY_PATH}" ]; then
        CURRENT_VERSION=$("${BINARY_PATH}" --version 2>&1 | grep -o 'v[0-9]\+\.[0-9]\+\.[0-9]\+' | head -1 || echo "")
        if [ -n "${CURRENT_VERSION}" ]; then
            print_info "当前已安装版本: ${CURRENT_VERSION}"
            
            # 比较版本
            COMPARE_RESULT=$(compare_versions "${CURRENT_VERSION}" "${LATEST_VERSION}")
            if [ "${COMPARE_RESULT}" -ge "0" ]; then
                print_success "已是最新版本，无需更新"
                exit 0
            else
                print_info "发现新版本，准备更新..."
            fi
        fi
    fi
    
    # 创建安装目录
    print_info "创建安装目录..."
    mkdir -p "${BIN_DIR}"
    mkdir -p "${CONFIG_DIR}"
    mkdir -p "${REPOS_DIR}"
    
    # 构建下载 URL（使用版本号）
    BINARY_NAME="cursortoolset-${PLATFORM}"
    DOWNLOAD_URL="https://github.com/shichao402/CursorToolset/releases/download/${LATEST_VERSION}/${BINARY_NAME}"
    
    # 下载预编译版本
    print_info "下载预编译版本..."
    if ! curl -fsSL -o "${BINARY_PATH}" "${DOWNLOAD_URL}"; then
        print_error "下载预编译版本失败"
        print_error "下载 URL: ${DOWNLOAD_URL}"
        print_error "请确保该版本已发布到 GitHub Releases"
        exit 1
    fi
    
    chmod +x "${BINARY_PATH}"
    print_success "预编译版本下载成功"
    
    # 下载配置文件到新位置
    print_info "下载配置文件..."
    if ! curl -fsSL -o "${CONFIG_PATH}" \
        "https://raw.githubusercontent.com/shichao402/CursorToolset/ReleaseLatest/available-toolsets.json"; then
        print_warning "配置文件下载失败，将使用默认配置"
    else
        print_success "配置文件已保存到 ${CONFIG_PATH}"
    fi
    
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
        INSTALLED_VERSION=$("${BINARY_PATH}" --version 2>&1 || echo "unknown")
        print_success "安装成功！版本: ${INSTALLED_VERSION}"
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
