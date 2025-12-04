#!/bin/bash
# CursorToolset 本地构建脚本
# 功能：构建当前平台或所有平台版本，支持日志收集和输出位置配置

set -e

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# 打印带颜色的消息
print_info() {
    echo -e "${BLUE}ℹ${NC}  $1" | tee -a "${LOG_FILE}"
}

print_success() {
    echo -e "${GREEN}✓${NC}  $1" | tee -a "${LOG_FILE}"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC}  $1" | tee -a "${LOG_FILE}"
}

print_error() {
    echo -e "${RED}✗${NC}  $1" | tee -a "${LOG_FILE}"
}

print_header() {
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}" | tee -a "${LOG_FILE}"
    echo -e "${CYAN}$1${NC}" | tee -a "${LOG_FILE}"
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}" | tee -a "${LOG_FILE}"
}

# 默认配置
OUTPUT_DIR="${OUTPUT_DIR:-dist}"
LOG_DIR="${LOG_DIR:-logs}"
LOG_FILE="${LOG_DIR}/build-$(date +%Y%m%d-%H%M%S).log"
BINARY_NAME="cursortoolset"
BUILD_ALL=false
CLEAN_BEFORE=true
CLEAN_AFTER=false

# 解析参数
while [[ $# -gt 0 ]]; do
    case $1 in
        --output-dir|-o)
            OUTPUT_DIR="$2"
            shift 2
            ;;
        --log-dir|-l)
            LOG_DIR="$2"
            shift 2
            ;;
        --all|-a)
            BUILD_ALL=true
            shift
            ;;
        --no-clean)
            CLEAN_BEFORE=false
            shift
            ;;
        --clean-after)
            CLEAN_AFTER=true
            shift
            ;;
        --help|-h)
            cat << EOF
CursorToolset 本地构建脚本

用法: $0 [选项]

选项:
  -o, --output-dir DIR    指定输出目录（默认: dist）
  -l, --log-dir DIR        指定日志目录（默认: logs）
  -a, --all                构建所有平台版本
  --no-clean               构建前不清理输出目录
  --clean-after            构建后清理临时文件
  -h, --help               显示此帮助信息

环境变量:
  OUTPUT_DIR               输出目录（默认: dist）
  LOG_DIR                  日志目录（默认: logs）
  CURSOR_TOOLSET_ROOT      开发根目录（默认: .root）

示例:
  $0                      # 构建当前平台版本
  $0 --all                # 构建所有平台版本
  $0 -o build -l build-logs  # 指定输出和日志目录
EOF
            exit 0
            ;;
        *)
            print_error "未知参数: $1"
            echo "使用 --help 查看帮助信息"
            exit 1
            ;;
    esac
done

# 创建必要的目录
mkdir -p "${OUTPUT_DIR}"
mkdir -p "${LOG_DIR}"

# 记录开始时间
START_TIME=$(date +%s)
print_header "CursorToolset 本地构建"
print_info "开始时间: $(date '+%Y-%m-%d %H:%M:%S')"
print_info "输出目录: $(realpath "${OUTPUT_DIR}")"
print_info "日志文件: $(realpath "${LOG_FILE}")"
echo "" | tee -a "${LOG_FILE}"

# 检查 version.json
if [ ! -f "version.json" ]; then
    print_error "version.json 文件不存在"
    exit 1
fi

# 读取版本信息
VERSION=$(cat version.json | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4 || echo "dev")
if [ -z "${VERSION}" ]; then
    print_warning "无法从 version.json 读取版本号，使用默认值: dev"
    VERSION="dev"
fi

BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")

print_info "版本: ${VERSION}"
print_info "构建时间: ${BUILD_TIME}"
print_info "提交哈希: ${COMMIT}"
print_info "分支: ${BRANCH}"
echo "" | tee -a "${LOG_FILE}"

# 清理旧的构建产物
if [ "${CLEAN_BEFORE}" = true ]; then
    print_header "清理旧的构建产物"
    
    # 清理输出目录
    if [ -d "${OUTPUT_DIR}" ] && [ "$(ls -A "${OUTPUT_DIR}" 2>/dev/null)" ]; then
        print_info "清理输出目录: ${OUTPUT_DIR}"
        rm -rf "${OUTPUT_DIR}"/*
        print_success "输出目录已清理"
    else
        print_info "输出目录为空，无需清理"
    fi
    
    # 清理根目录下的可执行文件
    if [ -f "${BINARY_NAME}" ]; then
        print_info "清理根目录下的可执行文件: ${BINARY_NAME}"
        rm -f "${BINARY_NAME}"
        print_success "可执行文件已清理"
    fi
    
    echo "" | tee -a "${LOG_FILE}"
fi

# 设置开发环境变量
if [ -z "${CURSOR_TOOLSET_ROOT}" ]; then
    export CURSOR_TOOLSET_ROOT="$(pwd)/.root"
    print_info "设置开发环境变量: CURSOR_TOOLSET_ROOT=${CURSOR_TOOLSET_ROOT}"
    mkdir -p "${CURSOR_TOOLSET_ROOT}"
fi

# 构建函数
build_platform() {
    local os=$1
    local arch=$2
    local ext=$3
    local output_name="${BINARY_NAME}-${os}-${arch}${ext}"
    local output_path="${OUTPUT_DIR}/${output_name}"
    
    print_info "构建 ${os}-${arch}..."
    print_info "  输出: ${output_path}"
    
    # 构建命令
    GOOS="${os}" GOARCH="${arch}" go build \
        -ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}" \
        -o "${output_path}" \
        . 2>&1 | tee -a "${LOG_FILE}"
    
    if [ $? -eq 0 ]; then
        local size=$(ls -lh "${output_path}" | awk '{print $5}')
        print_success "${os}-${arch} 构建完成 (${size})"
        echo "${output_path}" >> "${OUTPUT_DIR}/.build-manifest"
        return 0
    else
        print_error "${os}-${arch} 构建失败"
        return 1
    fi
}

# 构建当前平台
build_current() {
    print_header "构建当前平台版本"
    
    local output_path="${OUTPUT_DIR}/${BINARY_NAME}"
    if [ "$(uname -s)" = "Darwin" ] || [ "$(uname -s)" = "Linux" ]; then
        # Unix-like 系统，不需要扩展名
        output_path="${OUTPUT_DIR}/${BINARY_NAME}"
    else
        # Windows
        output_path="${OUTPUT_DIR}/${BINARY_NAME}.exe"
    fi
    
    print_info "构建当前平台: $(go env GOOS)-$(go env GOARCH)"
    print_info "输出: ${output_path}"
    
    go build \
        -ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME}" \
        -o "${output_path}" \
        . 2>&1 | tee -a "${LOG_FILE}"
    
    if [ $? -eq 0 ]; then
        local size=$(ls -lh "${output_path}" | awk '{print $5}')
        print_success "构建完成 (${size})"
        echo "${output_path}" >> "${OUTPUT_DIR}/.build-manifest"
        
        # 同时在根目录创建符号链接（Unix-like）或复制（Windows）
        if [ "$(uname -s)" != "MINGW"* ] && [ "$(uname -s)" != "MSYS"* ]; then
            ln -sf "$(realpath "${output_path}")" "${BINARY_NAME}"
            print_info "已创建符号链接: ${BINARY_NAME} -> ${output_path}"
        fi
    else
        print_error "构建失败"
        exit 1
    fi
}

# 构建所有平台
build_all_platforms() {
    print_header "构建所有平台版本"
    
    local failed=0
    local total=0
    
    # Linux
    build_platform "linux" "amd64" "" && ((total++)) || ((failed++))
    build_platform "linux" "arm64" "" && ((total++)) || ((failed++))
    
    # macOS
    build_platform "darwin" "amd64" "" && ((total++)) || ((failed++))
    build_platform "darwin" "arm64" "" && ((total++)) || ((failed++))
    
    # Windows
    build_platform "windows" "amd64" ".exe" && ((total++)) || ((failed++))
    
    echo "" | tee -a "${LOG_FILE}"
    print_info "构建统计: 成功 ${total} 个"
    if [ ${failed} -gt 0 ]; then
        print_warning "失败 ${failed} 个"
    fi
}

# 生成构建清单
generate_manifest() {
    print_header "生成构建清单"
    
    local manifest_file="${OUTPUT_DIR}/BUILD_INFO.txt"
    cat > "${manifest_file}" << EOF
CursorToolset 构建信息
====================

版本: ${VERSION}
构建时间: ${BUILD_TIME}
提交哈希: ${COMMIT}
分支: ${BRANCH}
构建平台: $(go env GOOS)-$(go env GOARCH)
Go 版本: $(go version)

构建产物:
EOF
    
    if [ -f "${OUTPUT_DIR}/.build-manifest" ]; then
        while IFS= read -r file; do
            if [ -f "${file}" ]; then
                local size=$(ls -lh "${file}" | awk '{print $5}')
                local sha256=$(sha256sum "${file}" 2>/dev/null | cut -d' ' -f1 || shasum -a 256 "${file}" 2>/dev/null | cut -d' ' -f1 || echo "N/A")
                echo "  - $(basename "${file}") (${size}, SHA256: ${sha256})" >> "${manifest_file}"
            fi
        done < "${OUTPUT_DIR}/.build-manifest"
    fi
    
    print_success "构建清单已生成: ${manifest_file}"
}

# 执行构建
if [ "${BUILD_ALL}" = true ]; then
    build_all_platforms
else
    build_current
fi

# 生成构建清单
generate_manifest

# 清理临时文件
if [ "${CLEAN_AFTER}" = true ]; then
    print_header "清理临时文件"
    rm -f "${OUTPUT_DIR}/.build-manifest"
    print_success "临时文件已清理"
fi

# 计算构建时间
END_TIME=$(date +%s)
DURATION=$((END_TIME - START_TIME))

# 输出总结
echo "" | tee -a "${LOG_FILE}"
print_header "构建完成"
print_success "构建成功！"
print_info "总耗时: ${DURATION} 秒"
print_info "输出目录: $(realpath "${OUTPUT_DIR}")"
print_info "日志文件: $(realpath "${LOG_FILE}")"

# 列出构建产物
if [ -d "${OUTPUT_DIR}" ] && [ "$(ls -A "${OUTPUT_DIR}" 2>/dev/null)" ]; then
    echo "" | tee -a "${LOG_FILE}"
    print_info "构建产物:"
    ls -lh "${OUTPUT_DIR}" | grep -v "^total" | grep -v "^d" | awk '{print "  " $9 " (" $5 ")"}' | tee -a "${LOG_FILE}"
fi

echo "" | tee -a "${LOG_FILE}"

