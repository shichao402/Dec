#!/bin/bash
# Dec 一键发布脚本（鲁莽懒人版）
# 功能：从当前状态一路冲到正式版本发布
# 用法：./scripts/release-yolo.sh [版本号]
#
# 流程：
#   1. 检查环境 & 工作区状态
#   2. 更新版本号（可选）
#   3. 本地构建 & 测试
#   4. 提交代码
#   5. 推送 test tag → 触发 CI 构建
#   6. 等待 CI 完成
#   7. 推送正式 tag → 触发正式发布
#   8. 等待正式发布完成
#   9. 验证发布结果

set -e

# 颜色
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
NC='\033[0m'

# 配置
REPO="shichao402/Dec"
POLL_INTERVAL=15  # CI 轮询间隔（秒）
MAX_WAIT=600      # 最大等待时间（秒）

# 打印函数
info()    { echo -e "${BLUE}ℹ${NC}  $1"; }
success() { echo -e "${GREEN}✓${NC}  $1"; }
warn()    { echo -e "${YELLOW}⚠${NC}  $1"; }
error()   { echo -e "${RED}✗${NC}  $1"; }
step()    { echo -e "\n${MAGENTA}━━━ $1 ━━━${NC}\n"; }

# 横幅
banner() {
    echo -e "${CYAN}"
    echo "╔═══════════════════════════════════════════════════════╗"
    echo "║     🚀 Dec 一键发布（YOLO 模式）🚀          ║"
    echo "║                                                       ║"
    echo "║  ⚠️  此脚本会自动推送 tag 并发布新版本                 ║"
    echo "║  💡 适合懒人，不适合胆小鬼                            ║"
    echo "╚═══════════════════════════════════════════════════════╝"
    echo -e "${NC}"
}

# 检查命令是否存在
check_cmd() {
    if ! command -v "$1" &> /dev/null; then
        error "缺少命令: $1"
        exit 1
    fi
}

# 获取当前版本
get_current_version() {
    cat version.json | grep -o '"version"[[:space:]]*:[[:space:]]*"[^"]*"' | cut -d'"' -f4
}

# 递增版本号
bump_version() {
    local version=$1
    local part=${2:-patch}  # major, minor, patch
    
    # 去掉 v 前缀
    version=${version#v}
    
    IFS='.' read -r major minor patch <<< "$version"
    
    case $part in
        major) ((major++)); minor=0; patch=0 ;;
        minor) ((minor++)); patch=0 ;;
        patch) ((patch++)) ;;
    esac
    
    echo "v${major}.${minor}.${patch}"
}

# 等待 CI 工作流完成
wait_for_workflow() {
    local tag=$1
    local workflow_name=$2
    local start_time=$(date +%s)
    
    info "等待 CI 工作流 '${workflow_name}' 完成..."
    info "Tag: ${tag}"
    
    # 等待工作流启动
    sleep 5
    
    while true; do
        local elapsed=$(($(date +%s) - start_time))
        
        if [ $elapsed -gt $MAX_WAIT ]; then
            error "等待超时（${MAX_WAIT}秒）"
            return 1
        fi
        
        # 获取工作流运行状态
        local run_info=$(gh run list --repo "$REPO" --limit 5 --json headBranch,status,conclusion,name,databaseId 2>/dev/null || echo "[]")
        
        # 查找匹配的运行
        local status=$(echo "$run_info" | jq -r ".[] | select(.headBranch == \"$tag\" or .name == \"$workflow_name\") | .status" | head -1)
        local conclusion=$(echo "$run_info" | jq -r ".[] | select(.headBranch == \"$tag\" or .name == \"$workflow_name\") | .conclusion" | head -1)
        
        if [ "$status" = "completed" ]; then
            if [ "$conclusion" = "success" ]; then
                success "工作流完成！(${elapsed}秒)"
                return 0
            else
                error "工作流失败: $conclusion"
                return 1
            fi
        fi
        
        printf "\r${BLUE}⏳${NC} 等待中... (${elapsed}s / ${MAX_WAIT}s) [状态: ${status:-pending}]"
        sleep $POLL_INTERVAL
    done
}

# 简化版等待 - 直接检查 release 是否存在
wait_for_release() {
    local tag=$1
    local start_time=$(date +%s)
    
    info "等待 Release ${tag} 创建..."
    
    while true; do
        local elapsed=$(($(date +%s) - start_time))
        
        if [ $elapsed -gt $MAX_WAIT ]; then
            error "等待超时（${MAX_WAIT}秒）"
            return 1
        fi
        
        # 检查 release 是否存在
        if gh release view "$tag" --repo "$REPO" &>/dev/null; then
            success "Release ${tag} 已创建！(${elapsed}秒)"
            return 0
        fi
        
        printf "\r${BLUE}⏳${NC} 等待 Release... (${elapsed}s / ${MAX_WAIT}s)"
        sleep $POLL_INTERVAL
    done
}

# 主流程
main() {
    banner
    
    # 检查依赖
    step "Step 1/9: 检查环境"
    check_cmd git
    check_cmd go
    check_cmd gh
    check_cmd jq
    success "所有依赖已就绪"
    
    # 检查 gh 登录状态
    if ! gh auth status &>/dev/null; then
        error "请先登录 GitHub CLI: gh auth login"
        exit 1
    fi
    success "GitHub CLI 已认证"
    
    # 检查工作目录
    if [ ! -f "version.json" ]; then
        error "请在项目根目录运行此脚本"
        exit 1
    fi
    
    # 获取版本
    local current_version=$(get_current_version)
    local new_version="${1:-}"
    
    if [ -z "$new_version" ]; then
        new_version=$(bump_version "$current_version" patch)
        info "当前版本: ${current_version}"
        info "新版本（自动递增）: ${new_version}"
        echo ""
        read -p "按 Enter 继续，或输入自定义版本号: " custom_version
        if [ -n "$custom_version" ]; then
            new_version="$custom_version"
            # 确保有 v 前缀
            [[ "$new_version" != v* ]] && new_version="v$new_version"
        fi
    else
        [[ "$new_version" != v* ]] && new_version="v$new_version"
    fi
    
    local test_tag="test-${new_version}"
    
    info "📌 目标版本: ${new_version}"
    info "📌 测试 Tag: ${test_tag}"
    echo ""
    
    # 最后确认
    warn "即将执行以下操作："
    echo "  1. 更新 version.json 到 ${new_version}"
    echo "  2. 提交并推送代码"
    echo "  3. 推送 ${test_tag} 触发 CI"
    echo "  4. 等待 CI 通过"
    echo "  5. 推送 ${new_version} 发布正式版"
    echo ""
    read -p "确认继续？(y/N) " confirm
    if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
        info "已取消"
        exit 0
    fi
    
    # 更新版本号
    step "Step 2/9: 更新版本号"
    echo "{
  \"version\": \"${new_version}\"
}" > version.json
    success "version.json 已更新为 ${new_version}"
    
    # 本地构建测试
    step "Step 3/9: 本地构建 & 测试"
    info "运行 go build..."
    go build -o dist/dec .
    success "构建成功"
    
    info "运行单元测试..."
    go test ./... -v
    success "测试通过"
    
    # 提交代码
    step "Step 4/9: 提交代码"
    git add version.json
    if git diff --cached --quiet; then
        info "没有需要提交的更改"
    else
        git commit -m "chore: bump version to ${new_version}"
        success "代码已提交"
    fi
    
    git push origin main
    success "代码已推送到 main"
    
    # 推送 test tag
    step "Step 5/9: 推送测试 Tag"
    
    # 删除旧的 test tag（如果存在）
    if git tag -l "$test_tag" | grep -q "$test_tag"; then
        warn "删除旧的本地 tag: ${test_tag}"
        git tag -d "$test_tag"
    fi
    if gh release view "$test_tag" --repo "$REPO" &>/dev/null; then
        warn "删除旧的远程 release: ${test_tag}"
        gh release delete "$test_tag" --repo "$REPO" --yes || true
    fi
    git push origin ":refs/tags/${test_tag}" 2>/dev/null || true
    
    git tag "$test_tag"
    git push origin "$test_tag"
    success "已推送 ${test_tag}"
    
    # 等待 CI
    step "Step 6/9: 等待 CI 构建"
    sleep 3  # 给 GitHub 一点时间触发工作流
    wait_for_release "$test_tag"
    
    # 推送正式 tag
    step "Step 7/9: 推送正式 Tag"
    
    # 删除旧的正式 tag（如果存在）
    if git tag -l "$new_version" | grep -q "$new_version"; then
        warn "删除旧的本地 tag: ${new_version}"
        git tag -d "$new_version"
    fi
    if gh release view "$new_version" --repo "$REPO" &>/dev/null; then
        warn "删除旧的远程 release: ${new_version}"
        gh release delete "$new_version" --repo "$REPO" --yes || true
    fi
    git push origin ":refs/tags/${new_version}" 2>/dev/null || true
    
    git tag "$new_version"
    git push origin "$new_version"
    success "已推送 ${new_version}"
    
    # 等待正式发布
    step "Step 8/9: 等待正式发布"
    wait_for_release "$new_version"
    
    # 验证
    step "Step 9/9: 验证发布"
    info "检查 Release 资产..."
    gh release view "$new_version" --repo "$REPO"
    
    echo ""
    success "🎉 发布完成！"
    echo ""
    info "Release URL: https://github.com/${REPO}/releases/tag/${new_version}"
    info "安装命令:"
    echo "  curl -fsSL https://raw.githubusercontent.com/${REPO}/ReleaseLatest/scripts/install.sh | bash"
    echo ""
    info "更新命令:"
    echo "  dec update --self"
    echo ""
}

# 运行
main "$@"
