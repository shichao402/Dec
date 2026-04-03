#!/bin/bash
# Dec 卸载脚本 (Linux/macOS)
# 使用方法: curl -fsSL https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/scripts/uninstall.sh | bash
# 跳过确认: curl -fsSL ... | bash -s -- --yes

set -e

SKIP_CONFIRM=false
while [[ $# -gt 0 ]]; do
    case $1 in
        --yes|-y)
            SKIP_CONFIRM=true
            shift
            ;;
        *)
            shift
            ;;
    esac
done

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_info() { echo -e "${BLUE}ℹ${NC}  $1"; }
print_success() { echo -e "${GREEN}✓${NC}  $1"; }
print_warning() { echo -e "${YELLOW}⚠${NC}  $1"; }
print_error() { echo -e "${RED}✗${NC}  $1"; }

main() {
    echo ""
    echo "╔═══════════════════════════════════════╗"
    echo "║           Dec 卸载脚本               ║"
    echo "╚═══════════════════════════════════════╝"
    echo ""

    INSTALL_DIR="${DEC_HOME:-${HOME}/.dec}"
    BIN_DIR="${INSTALL_DIR}/bin"
    BINARY_PATH="${BIN_DIR}/dec"
    GLOBAL_CONFIG_PATH="${INSTALL_DIR}/config.yaml"
    BARE_REPO_PATH="${INSTALL_DIR}/repo.git"

    if [[ ! -d "${INSTALL_DIR}" ]] && [[ ! -f "${BINARY_PATH}" ]]; then
        print_warning "未找到安装目录: ${INSTALL_DIR}"
        exit 0
    fi

    print_warning "这将删除整个 Dec 根目录，包括："
    echo "  - 可执行文件: ${BINARY_PATH}"
    echo "  - 全局配置: ${GLOBAL_CONFIG_PATH}"
    echo "  - 本地 bare repo: ${BARE_REPO_PATH}"
    echo ""

    if [[ "${SKIP_CONFIRM}" != "true" ]]; then
        read -p "确定要卸载 Dec 吗？(y/N): " -n 1 -r
        echo ""
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            print_info "已取消卸载"
            exit 0
        fi
    else
        print_info "--yes 模式，跳过确认"
    fi

    local shell_rc=""
    case "${SHELL}" in
        */zsh) shell_rc="${HOME}/.zshrc" ;;
        */bash)
            if [[ "$(uname -s)" == "Darwin" ]]; then
                shell_rc="${HOME}/.bash_profile"
            else
                shell_rc="${HOME}/.bashrc"
            fi
            ;;
    esac

    if [[ -n "${shell_rc}" ]] && [[ -f "${shell_rc}" ]]; then
        cp "${shell_rc}" "${shell_rc}.bak.dec-uninstall"
        sed -i.bak "\\|# Dec|d; \\|${BIN_DIR}|d" "${shell_rc}" 2>/dev/null || true
        rm -f "${shell_rc}.bak"
        print_success "已从 ${shell_rc} 清理 PATH 配置"
    fi

    rm -rf "${INSTALL_DIR}"
    print_success "已删除 ${INSTALL_DIR}"
    echo ""
    print_info "请重新打开终端，或重新加载 shell 配置文件"
}

main
