#!/bin/bash
# Dec 发布准备脚本
# 说明：推送 tag 后 GitHub Actions 会自动构建并创建 Release，本脚本用于本地校验与构建准备。
# 用法：./scripts/release-yolo.sh [版本号]

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
    echo "3. 提交并推送到 main，GitHub Actions 会自动自增版本号、打 tag、构建并创建 Release"
    echo ""
    echo "   或手动指定版本发布："
    echo "   git tag ${target} && git push origin ${target}"
    echo ""
    warn "推送到 main 后 GitHub Actions 会自动完成版本自增与发布，通常无需手动打 tag。"
}

main "$@"
