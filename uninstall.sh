#!/bin/bash
# CursorToolset 卸载脚本 (Linux/macOS)
# 使用方法: curl -fsSL https://raw.githubusercontent.com/shichao402/CursorToolset/ReleaseLatest/uninstall.sh | bash

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

# 主卸载函数
main() {
    echo ""
    echo "╔═══════════════════════════════════════╗"
    echo "║   CursorToolset 卸载脚本              ║"
    echo "╚═══════════════════════════════════════╝"
    echo ""
    
    # 定义安装路径（使用新的目录结构）
    if [ -n "${CURSOR_TOOLSET_HOME}" ]; then
        INSTALL_DIR="${CURSOR_TOOLSET_HOME}"
    else
        INSTALL_DIR="${HOME}/.cursortoolsets"
    fi
    BIN_DIR="${INSTALL_DIR}/bin"
    CONFIG_DIR="${INSTALL_DIR}/config"
    REPOS_DIR="${INSTALL_DIR}/repos"
    BINARY_PATH="${BIN_DIR}/cursortoolset"
    
    # 检查是否已安装
    if [[ ! -d "${INSTALL_DIR}" ]] && [[ ! -f "${BINARY_PATH}" ]]; then
        print_warning "未找到安装目录: ${INSTALL_DIR}"
        print_info "CursorToolset 可能未安装或已卸载"
        exit 0
    fi
    
    print_info "找到安装目录: ${INSTALL_DIR}"
    
    # 确认卸载
    echo ""
    print_warning "这将删除以下内容："
    echo "  - 安装目录: ${INSTALL_DIR}"
    echo "  - 可执行文件: ${BINARY_PATH}"
    echo "  - 配置文件: ${CONFIG_DIR}/available-toolsets.json"
    echo "  - 工具集仓库: ${REPOS_DIR}"
    echo ""
    read -p "确定要卸载 CursorToolset 吗？(y/N): " -n 1 -r
    echo ""
    
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        print_info "取消卸载"
        exit 0
    fi
    
    # 从 PATH 中移除
    print_info "清理环境变量配置..."
    
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
    
    if [[ -n "${SHELL_RC}" ]] && [[ -f "${SHELL_RC}" ]]; then
        # 移除 CursorToolset 相关的配置行
        if grep -q "cursortoolset\|CursorToolset" "${SHELL_RC}" 2>/dev/null; then
            # 创建备份
            cp "${SHELL_RC}" "${SHELL_RC}.backup.$(date +%Y%m%d_%H%M%S)"
            
            # 移除相关行（包括空行）
            sed -i.tmp '/# CursorToolset/,+2d' "${SHELL_RC}" 2>/dev/null || \
            sed -i '/# CursorToolset/d; /cursortoolset/d' "${SHELL_RC}" 2>/dev/null || \
            grep -v "cursortoolset\|CursorToolset" "${SHELL_RC}" > "${SHELL_RC}.tmp" && mv "${SHELL_RC}.tmp" "${SHELL_RC}"
            
            # 清理临时文件
            rm -f "${SHELL_RC}.tmp" 2>/dev/null || true
            
            print_success "已从 ${SHELL_RC} 移除配置"
            print_info "备份文件: ${SHELL_RC}.backup.*"
        else
            print_info "未在 ${SHELL_RC} 中找到相关配置"
        fi
    fi
    
    # 删除安装目录
    print_info "删除安装目录..."
    if [[ -d "${INSTALL_DIR}" ]]; then
        rm -rf "${INSTALL_DIR}"
        print_success "已删除: ${INSTALL_DIR}"
    fi
    
    # 检查 .cursor/toolsets 目录是否为空，如果为空则删除
    CURSOR_TOOLSETS_DIR="${HOME}/.cursor/toolsets"
    if [[ -d "${CURSOR_TOOLSETS_DIR}" ]]; then
        if [[ -z "$(ls -A "${CURSOR_TOOLSETS_DIR}" 2>/dev/null)" ]]; then
            print_info "清理空目录..."
            rmdir "${CURSOR_TOOLSETS_DIR}" 2>/dev/null || true
            rmdir "${HOME}/.cursor" 2>/dev/null || true
        fi
    fi
    
    echo ""
    echo "╔═══════════════════════════════════════╗"
    echo "║         卸载完成！                    ║"
    echo "╚═══════════════════════════════════════╝"
    echo ""
    print_success "CursorToolset 已成功卸载"
    echo ""
    print_info "请运行以下命令使环境变量更改生效："
    if [[ -n "${SHELL_RC}" ]]; then
        echo "  source ${SHELL_RC}"
    fi
    echo ""
    print_info "或者重新打开终端窗口"
    echo ""
}

# 运行主函数
main



