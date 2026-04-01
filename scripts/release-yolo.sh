#!/bin/bash
# Dec 发布准备脚本
# 说明：版本号在本地写入 version.json，推送包含 version.json 变更的 main 提交后，GitHub Actions 会自动校验、打 tag、构建并创建 Release。
# 用法：./scripts/release-yolo.sh [版本号]
#       不传版本号时，仅执行本地测试与构建检查。
#       传入版本号时，会先更新 version.json。

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
MAGENTA='\033[0;35m'
NC='\033[0m'

info()    { echo -e "${BLUE}ℹ${NC}  $1"; }
success() { echo -e "${GREEN}✓${NC}  $1"; }
warn()    { echo -e "${YELLOW}⚠${NC}  $1"; }
error()   { echo -e "${RED}✗${NC}  $1"; }
step()    { echo -e "\n${MAGENTA}━━━ $1 ━━━${NC}\n"; }

check_cmd() {
    if ! command -v "$1" >/dev/null 2>&1; then
        error "缺少命令: $1"
        exit 1
    fi
}

normalize_version() {
    local v="$1"
    if [[ -z "$v" ]]; then
        return 1
    fi
    if [[ "$v" != v* ]]; then
        v="v${v}"
    fi
    echo "$v"
}

validate_version() {
    local v="$1"
    if [[ ! "$v" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        error "无效版本号格式: ${v}（期望: vMAJOR.MINOR.PATCH）"
        exit 1
    fi
}

current_version() {
    python3 -c "import json; print(json.load(open('version.json'))['version'])"
}

main() {
    step "检查环境"
    check_cmd git
    check_cmd go
    check_cmd python3

    if [ ! -f "version.json" ]; then
        error "请在项目根目录运行此脚本"
        exit 1
    fi

    local current
    current=$(current_version)
    local target="$current"

    if [ -n "$1" ]; then
        target=$(normalize_version "$1")
        validate_version "$target"
        python3 - <<'PY' "$target"
import json, sys
path = 'version.json'
version = sys.argv[1]
with open(path, 'r', encoding='utf-8') as f:
    data = json.load(f)
data['version'] = version
with open(path, 'w', encoding='utf-8') as f:
    json.dump(data, f, ensure_ascii=False, indent=2)
    f.write('\n')
PY
        success "version.json 已更新为 ${target}"
    else
        info "沿用当前版本: ${target}"
    fi

    step "运行测试"
    go test ./... -v
    ./scripts/run-tests.sh
    success "本地测试完成"

    step "构建发布产物"
    ./scripts/build.sh --all
    success "构建完成"

    step "后续步骤"
    echo "1. 检查 dist/ 目录中的二进制与 BUILD_INFO 文件"
    echo "2. 更新 CHANGELOG 与 README（如有需要）"
    echo "3. 提交本次变更并推送到 main"
    echo ""
    echo "   说明："
    echo "   - 只有 version.json 发生变更时，GitHub Actions 才会执行发布"
    echo "   - 发布流程会自动校验版本号、创建 tag、构建并创建 GitHub Release"
    echo "   - 无需手动打 tag"
    echo ""
    if [ -n "$1" ]; then
        echo "   推荐命令："
        echo "   git add version.json <其他变更>"
        echo "   git commit -m \"chore: release ${target}\""
        echo "   git push origin main"
    else
        warn "本次未修改 version.json；如果直接 push，GitHub Actions 不会触发发布。"
    fi
}

main "$@"
