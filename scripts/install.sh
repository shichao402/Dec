#!/bin/bash
# Dec 一键安装脚本 (Linux/macOS)
# 使用方法: curl -fsSL https://raw.githubusercontent.com/shichao402/Dec/ReleaseLatest/scripts/install.sh | bash

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

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

detect_platform() {
    local os=""
    local arch=""

    case "$(uname -s)" in
        Darwin*) os="darwin" ;;
        Linux*) os="linux" ;;
        *)
            print_error "不支持的操作系统: $(uname -s)"
            exit 1
            ;;
    esac

    case "$(uname -m)" in
        x86_64) arch="amd64" ;;
        arm64|aarch64) arch="arm64" ;;
        *)
            print_error "不支持的架构: $(uname -m)"
            exit 1
            ;;
    esac

    echo "${os}-${arch}"
}

compare_versions() {
    local v1="${1#v}"
    local v2="${2#v}"

    IFS='.' read -ra v1_parts <<< "$v1"
    IFS='.' read -ra v2_parts <<< "$v2"

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

main() {
    echo ""
    echo "╔═══════════════════════════════════════╗"
    echo "║        Dec 一键安装脚本              ║"
    echo "╚═══════════════════════════════════════╝"
    echo ""

    if ! command -v curl >/dev/null 2>&1; then
        print_error "curl 未安装。请先安装 curl。"
        exit 1
    fi

    local platform
    platform=$(detect_platform)
    local install_dir="${DEC_HOME:-${HOME}/.dec}"
    local bin_dir="${install_dir}/bin"
    local binary_path="${bin_dir}/dec"
    local update_branch="${DEC_BRANCH:-ReleaseLatest}"

    print_info "检测到平台: ${platform}"
    print_info "安装目录: ${install_dir}"
    print_info "更新分支: ${update_branch}"

    local version_json
    version_json=$(curl -fsSL "https://raw.githubusercontent.com/shichao402/Dec/${update_branch}/version.json" 2>/dev/null || true)
    if [ -z "${version_json}" ]; then
        print_error "无法从 ${update_branch} 分支获取版本信息"
        exit 1
    fi

    local latest_version
    latest_version=$(echo "${version_json}" | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4)
    if [ -z "${latest_version}" ]; then
        print_error "无法解析版本号"
        exit 1
    fi
    print_info "最新版本: ${latest_version}"

    if [ -x "${binary_path}" ]; then
        # macOS: 先清除可能存在的扩展属性，避免 --version 挂起
        if [ "$(uname -s)" = "Darwin" ]; then
            xattr -cr "${binary_path}" 2>/dev/null || true
        fi
        local current_version
        current_version=$("${binary_path}" --version 2>&1 | grep -o 'v[0-9]\+\.[0-9]\+\.[0-9]\+' | head -1 || true)
        if [ -n "${current_version}" ]; then
            print_info "当前已安装版本: ${current_version}"
            local compare_result
            compare_result=$(compare_versions "${current_version}" "${latest_version}")
            if [ "${compare_result}" -ge 0 ]; then
                print_success "已是最新版本，无需更新"
                exit 0
            fi
            # 版本较旧，提示用户选择
            if [ -t 0 ]; then
                # 终端模式，交互式提示
                printf "${YELLOW}?${NC}  检测到旧版本 ${current_version}，最新版本为 ${latest_version}，是否覆盖安装？[Y/n] "
                read -r answer
                if [ "${answer}" = "n" ] || [ "${answer}" = "N" ]; then
                    print_info "已跳过安装"
                    exit 0
                fi
            else
                # 管道模式（如 curl | bash），默认覆盖安装
                print_info "检测到旧版本 ${current_version}，将自动覆盖安装为 ${latest_version}"
            fi
        else
            # 版本解析失败，提示用户选择
            print_warning "检测到已安装的 Dec，但无法获取版本号"
            if [ -t 0 ]; then
                printf "${YELLOW}?${NC}  是否覆盖安装？[Y/n] "
                read -r answer
                if [ "${answer}" = "n" ] || [ "${answer}" = "N" ]; then
                    print_info "已跳过安装"
                    exit 0
                fi
            else
                print_info "将自动覆盖安装为 ${latest_version}"
            fi
        fi
    fi

    mkdir -p "${bin_dir}"

    local download_tag="${latest_version}"
    if [ "${update_branch}" = "ReleaseTest" ]; then
        download_tag="test-${latest_version}"
    fi

    local binary_name="dec-${platform}"
    local download_url="https://github.com/shichao402/Dec/releases/download/${download_tag}/${binary_name}"

    print_info "下载预编译版本..."
    if ! curl -fsSL -o "${binary_path}" "${download_url}"; then
        print_error "下载失败: ${download_url}"
        exit 1
    fi
    chmod +x "${binary_path}"
    # macOS: 清除下载的扩展属性（com.apple.provenance 等），否则二进制可能被系统阻止执行
    if [ "$(uname -s)" = "Darwin" ]; then
        xattr -cr "${binary_path}" 2>/dev/null || true
    fi
    print_success "二进制下载完成"

    local shell_rc=""
    case "${SHELL}" in
        */zsh) shell_rc="${HOME}/.zshrc" ;;
        */bash)
            if [ "$(uname -s)" = "Darwin" ]; then
                shell_rc="${HOME}/.bash_profile"
            else
                shell_rc="${HOME}/.bashrc"
            fi
            ;;
    esac

    if [ -n "${shell_rc}" ]; then
        if ! grep -Fq "${bin_dir}" "${shell_rc}" 2>/dev/null; then
            {
                echo ""
                echo "# Dec"
                echo "export PATH=\"${bin_dir}:\$PATH\""
            } >> "${shell_rc}"
            print_success "已将 ${bin_dir} 添加到 ${shell_rc}"
        else
            print_info "PATH 中已存在 ${bin_dir}，跳过写入"
        fi
    else
        print_warning "未识别当前 Shell，请手动把 ${bin_dir} 加入 PATH"
    fi

    print_info "验证安装..."
    local installed_version
    installed_version=$("${binary_path}" --version 2>&1) || true
    if [ -z "${installed_version}" ] || ! echo "${installed_version}" | grep -q 'v[0-9]\+\.[0-9]\+\.[0-9]\+'; then
        print_error "安装失败：无法验证已安装的二进制文件"
        exit 1
    fi
    print_success "安装成功，版本: ${installed_version}"

    echo ""
    print_info "之后可以运行："
    echo "  dec --help"
    echo "  dec config repo <your-vault-repo-url>"
    echo "  dec list"
    echo ""
}

main
