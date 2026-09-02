#!/bin/bash
# Dec 一键安装脚本 (Linux/macOS)
# 主路径: curl -fsSL https://cnb.cool/shichao402/Dec/-/git/raw/main/scripts/install.sh | bash
# 镜像备份: curl -fsSL https://raw.githubusercontent.com/shichao402/Dec/main/scripts/install.sh | bash

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

# 从 version.json 的 checksums 段取出指定产物的 sha256。
# 结构：{"version":"vX.Y.Z","checksums":{"dec-linux-amd64":"<sha256>",...}}
extract_checksum() {
    local json="$1"
    local name="$2"
    echo "${json}" \
        | tr -d '\n' \
        | grep -o "\"${name}\"[[:space:]]*:[[:space:]]*\"[a-fA-F0-9]\{64\}\"" \
        | head -1 \
        | cut -d'"' -f4
}

# 计算文件 sha256；返回空表示本机没有可用的摘要工具。
compute_sha256() {
    local file="$1"
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "${file}" | awk '{print $1}'
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "${file}" | awk '{print $1}'
    else
        echo ""
    fi
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
    local binaries=("dec" "dec-server" "dec-mcp" "dec-exec")
    local update_branch="${DEC_BRANCH:-main}"
    local requested_version="${DEC_VERSION:-}"

    print_info "检测到平台: ${platform}"
    print_info "安装目录: ${install_dir}"
    print_info "更新分支: ${update_branch}"

    # Console 会钉死自身版本，避免主干 version.json 在构建/传播窗口里与面板错位。
    local version_sources=()
    if [ -n "${requested_version}" ]; then
        if ! echo "${requested_version}" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'; then
            print_error "DEC_VERSION 无效: ${requested_version}"
            exit 1
        fi
        local requested_nover="${requested_version#v}"
        version_sources=(
            "https://updates.firoyang.com/rup/artifact/dec/${requested_nover}/dec-runtime-manifest.json"
            "https://github.com/shichao402/Dec/releases/download/${requested_version}/version.json"
        )
    else
        version_sources=(
            "https://cnb.cool/shichao402/Dec/-/git/raw/${update_branch}/version.json"
            "https://raw.githubusercontent.com/shichao402/Dec/${update_branch}/version.json"
        )
    fi
    local version_json=""
    local i
    for i in "${!version_sources[@]}"; do
        version_json=$(curl -fsSL --connect-timeout 8 --max-time 20 "${version_sources[$i]}" 2>/dev/null || true)
        [ -n "${version_json}" ] && break
        if [ $((i + 1)) -lt ${#version_sources[@]} ]; then
            print_warning "从 ${version_sources[$i]} 获取版本信息失败，尝试下一个来源"
        fi
    done
    if [ -z "${version_json}" ]; then
        print_error "无法从 ${update_branch} 分支获取版本信息"
        print_error "若使用代理客户端，请先 export HTTPS_PROXY=http://127.0.0.1:<端口> 后重试"
        exit 1
    fi

    local latest_version
    latest_version=$(echo "${version_json}" | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4)
    if [ -z "${latest_version}" ]; then
        print_error "无法解析版本号"
        exit 1
    fi
    if [ -n "${requested_version}" ] && [ "${latest_version}" != "${requested_version}" ]; then
        print_error "版本清单不匹配: 请求 ${requested_version}，得到 ${latest_version}"
        exit 1
    fi
    print_info "最新版本: ${latest_version}"

    if [ -x "${binary_path}" ]; then
        # macOS: 先清除可能存在的扩展属性，避免 --version 挂起
        if [ "$(uname -s)" = "Darwin" ]; then
            xattr -cr "${binary_path}" 2>/dev/null || true
        fi
        local current_version
        # 限时读取版本，避免 stuck exec（UE 态）导致安装脚本本身挂起。
        current_version=$(perl -e 'alarm 3; exec @ARGV' "${binary_path}" --version 2>&1 | grep -o 'v[0-9]\+\.[0-9]\+\.[0-9]\+' | head -1 || true)
        if [ -n "${current_version}" ]; then
            print_info "当前已安装版本: ${current_version}"
            local compare_result
            compare_result=$(compare_versions "${current_version}" "${latest_version}")
            if [ "${compare_result}" -ge 0 ]; then
                local suite_complete=true
                for binary in "${binaries[@]}"; do
                    [ -x "${bin_dir}/${binary}" ] || suite_complete=false
                done
                if [ "${suite_complete}" = true ]; then
                    print_success "已是最新版本，且四个程序完整"
                    exit 0
                fi
                print_warning "主程序已是最新版本，但服务/门面程序不完整，将修复安装"
            fi
            # 版本较旧，提示用户选择
            if [ -t 0 ] && [ "${DEC_NONINTERACTIVE:-}" != "1" ]; then
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
            if [ -t 0 ] && [ "${DEC_NONINTERACTIVE:-}" != "1" ]; then
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

    local ver_nover="${latest_version#v}"
    local checksum_missing=false
    local checksum_verified=0
    print_info "下载 Dec 程序组..."
    for binary in "${binaries[@]}"; do
        local binary_name="${binary}-${platform}"
        # COS/RUP 产物（与自更新同源）；未齐时回退 GitHub Release（仅首次安装）
        local download_urls=(
            "https://updates.firoyang.com/rup/artifact/dec/${ver_nover}/${binary_name}"
            "https://github.com/shichao402/Dec/releases/download/${download_tag}/${binary_name}"
        )
        local target="${bin_dir}/${binary}"
        # 先删除旧二进制再下载，避免 macOS 上 stuck exec 占用同一 inode 导致新进程卡在 dyld。
        rm -f "${target}"
        local downloaded=false
        local download_url=""
        for download_url in "${download_urls[@]}"; do
            if curl -fsSL --connect-timeout 8 --max-time 120 -o "${target}" "${download_url}"; then
                downloaded=true
                break
            fi
            print_warning "从 ${download_url} 下载失败，尝试下一个来源"
            rm -f "${target}"
        done
        if [ "${downloaded}" != true ]; then
            print_error "下载失败: ${binary_name}"
            exit 1
        fi

        # 产物完整性校验：注入式远程安装下，静默接受未校验产物是不可接受的风险。
        local expected_sha
        expected_sha=$(extract_checksum "${version_json}" "${binary_name}")
        if [ -n "${expected_sha}" ]; then
            local actual_sha
            actual_sha=$(compute_sha256 "${target}")
            if [ -z "${actual_sha}" ]; then
                checksum_missing=true
                print_warning "本机无 sha256sum / shasum，跳过 ${binary_name} 的完整性校验"
            elif [ "${actual_sha}" != "${expected_sha}" ]; then
                rm -f "${target}"
                print_error "产物校验失败: ${binary_name}"
                print_error "  期望 sha256: ${expected_sha}"
                print_error "  实际 sha256: ${actual_sha}"
                print_error "已删除该产物。请勿使用来源不明的二进制；如确认发布端异常请联系维护者。"
                exit 1
            else
                checksum_verified=$((checksum_verified + 1))
            fi
        else
            checksum_missing=true
        fi

        chmod +x "${target}"
    done
    # macOS: 清除下载的扩展属性（com.apple.provenance 等），否则二进制可能被系统阻止执行
    if [ "$(uname -s)" = "Darwin" ]; then
        for binary in "${binaries[@]}"; do
            xattr -cr "${bin_dir}/${binary}" 2>/dev/null || true
        done
    fi
    print_success "四个程序下载完成"
    if [ "${checksum_missing}" = true ]; then
        print_warning "未提供产物摘要或本机无摘要工具，本次安装未完整校验产物完整性"
    elif [ "${checksum_verified}" -gt 0 ]; then
        print_success "产物校验通过（sha256 × ${checksum_verified}）"
    fi

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
    echo "  # 人机入口是 Dec Console（桌面客户端），不是终端 TUI"
    echo ""
}

main
