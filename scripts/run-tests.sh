#!/bin/bash
#
# CursorToolset 完整功能测试脚本
# 使用方法: ./scripts/run-tests.sh
#
# 测试原则：
# 1. 每个测试必须有明确的验证条件
# 2. 命令成功 ≠ 测试通过，必须验证结果
# 3. 关键文件必须存在且内容正确
#

set -e

# 清除开发环境变量，使用生产环境
unset CURSOR_TOOLSET_ROOT
unset CURSOR_TOOLSET_HOME

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 计数器
PASSED=0
FAILED=0
SKIPPED=0

# 安装目录（生产环境）
INSTALL_DIR="$HOME/.cursortoolsets"

cd "$(dirname "$0")/.."
PROJECT_DIR=$(pwd)

echo "=========================================="
echo "CursorToolset 完整功能测试"
echo "=========================================="
echo "项目目录: $PROJECT_DIR"
echo "安装目录: $INSTALL_DIR"
echo ""

# 构建（不使用 make，避免开发环境变量）
echo ">>> 构建管理器..."
mkdir -p dist
go build -o dist/cursortoolset .
echo "✅ 构建完成: dist/cursortoolset"
echo ""

# 测试函数：运行命令并验证
# 用法: run_test "编号" "名称" "命令" "验证函数"
run_test() {
    local num=$1
    local name=$2
    local cmd=$3
    local verify_func=$4
    
    echo "=== 测试 $num: $name ==="
    
    # 执行命令
    local output_file="/tmp/test_output_$num.txt"
    local exit_code=0
    eval "$cmd" > "$output_file" 2>&1 || exit_code=$?
    
    # 显示输出
    cat "$output_file"
    
    # 验证结果
    if [ $exit_code -ne 0 ]; then
        echo -e "${RED}❌ 失败 - 命令执行失败 (exit code: $exit_code)${NC}"
        ((FAILED++))
    elif [ -n "$verify_func" ]; then
        # 有验证函数，执行验证
        if $verify_func; then
            echo -e "${GREEN}✅ 通过${NC}"
            ((PASSED++))
        else
            echo -e "${RED}❌ 失败 - 验证未通过${NC}"
            ((FAILED++))
        fi
    else
        # 无验证函数，命令成功即通过
        echo -e "${GREEN}✅ 通过${NC}"
        ((PASSED++))
    fi
    echo ""
}

# 简单测试函数：只检查命令是否成功
run_simple_test() {
    local num=$1
    local name=$2
    local cmd=$3
    run_test "$num" "$name" "$cmd" ""
}

#==========================================
# 验证函数定义
#==========================================

# 验证：缓存目录已清空
verify_clean_all() {
    # clean --all 清理的是：
    # 1. cache/packages/ 下载的 tarball
    # 2. repos/ 已安装的包
    # 注意：cache/manifests/ 是包信息缓存，由 registry update 管理，不被清理
    
    # 检查 packages 目录（排除 .DS_Store）
    local pkg_files=$(find "$INSTALL_DIR/cache/packages" -type f ! -name ".DS_Store" 2>/dev/null | wc -l)
    if [ "$pkg_files" -gt 0 ]; then
        echo "  ⚠️  cache/packages 目录未清空 (有 $pkg_files 个文件)"
        return 1
    fi
    
    # repos 目录应该不存在或为空（排除 .DS_Store）
    local repos_dirs=$(find "$INSTALL_DIR/repos" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l)
    if [ "$repos_dirs" -gt 0 ]; then
        echo "  ⚠️  repos 目录未清空 (有 $repos_dirs 个包)"
        return 1
    fi
    return 0
}

# 验证：索引文件存在
verify_registry_update() {
    # registry.json 在 config 目录下
    if [ ! -f "$INSTALL_DIR/config/registry.json" ]; then
        echo "  ⚠️  config/registry.json 不存在"
        return 1
    fi
    # 检查文件不为空且是有效 JSON
    if ! jq empty "$INSTALL_DIR/config/registry.json" 2>/dev/null; then
        echo "  ⚠️  config/registry.json 不是有效的 JSON"
        return 1
    fi
    return 0
}

# 验证：list 输出包含 test-package
verify_list() {
    if ! grep -q "test-package" /tmp/test_output_03.txt; then
        echo "  ⚠️  输出中未找到 test-package"
        return 1
    fi
    return 0
}

# 验证：search 找到 test-package
verify_search() {
    if ! grep -q "test-package" /tmp/test_output_04.txt; then
        echo "  ⚠️  搜索结果中未找到 test-package"
        return 1
    fi
    return 0
}

# 验证：info 显示包详情
verify_info() {
    local output="/tmp/test_output_05.txt"
    if ! grep -q "test-package" "$output"; then
        echo "  ⚠️  未显示包名"
        return 1
    fi
    if ! grep -q "版本" "$output" && ! grep -q "version" "$output"; then
        echo "  ⚠️  未显示版本信息"
        return 1
    fi
    return 0
}

# 验证：test-package 安装完整
verify_install_test_package() {
    local pkg_dir="$INSTALL_DIR/repos/test-package"
    
    # 1. 目录存在
    if [ ! -d "$pkg_dir" ]; then
        echo "  ⚠️  包目录不存在: $pkg_dir"
        return 1
    fi
    
    # 2. package.json 存在
    if [ ! -f "$pkg_dir/package.json" ]; then
        echo "  ⚠️  package.json 不存在"
        return 1
    fi
    
    # 3. 二进制文件存在
    if [ ! -f "$pkg_dir/test-package" ]; then
        echo "  ⚠️  二进制文件 test-package 不存在"
        return 1
    fi
    
    # 4. .cursortoolset 目录存在
    if [ ! -d "$pkg_dir/.cursortoolset" ]; then
        echo "  ⚠️  .cursortoolset 目录不存在"
        return 1
    fi
    
    echo "  ✓ 包目录完整"
    echo "  ✓ package.json 存在"
    echo "  ✓ 二进制文件存在"
    echo "  ✓ .cursortoolset 目录存在"
    return 0
}

# 验证：已安装包列表包含 test-package
verify_list_installed() {
    if ! grep -q "test-package" /tmp/test_output_07.txt; then
        echo "  ⚠️  已安装列表中未找到 test-package"
        return 1
    fi
    return 0
}

# 验证：二进制文件中嵌入了编译时间
verify_build_time() {
    local binary="$INSTALL_DIR/repos/test-package/test-package"
    
    if [ ! -f "$binary" ]; then
        echo "  ⚠️  二进制文件不存在"
        return 1
    fi
    
    # 使用 strings 提取编译时间
    local build_time=$(strings "$binary" 2>/dev/null | grep -E '^20[0-9]{2}-[0-9]{2}-[0-9]{2}_[0-9]{2}:[0-9]{2}:[0-9]{2}$' | head -1)
    
    if [ -z "$build_time" ]; then
        echo "  ⚠️  未找到嵌入的编译时间"
        return 1
    fi
    
    echo "  ✓ 编译时间: $build_time"
    return 0
}

# 验证：卸载后包不存在
verify_uninstall() {
    if [ -d "$INSTALL_DIR/repos/test-package" ]; then
        echo "  ⚠️  包目录仍然存在"
        return 1
    fi
    return 0
}

# 验证：已安装列表为空
verify_list_installed_empty() {
    if grep -q "test-package\|github-action" /tmp/test_output_11.txt; then
        echo "  ⚠️  已安装列表不为空"
        return 1
    fi
    return 0
}

# 验证：init 创建完整结构
verify_init() {
    local pkg_dir="/tmp/test-init-pkg"
    
    # 1. 目录存在
    if [ ! -d "$pkg_dir" ]; then
        echo "  ⚠️  包目录不存在"
        return 1
    fi
    
    # 2. package.json 存在且有效
    if [ ! -f "$pkg_dir/package.json" ]; then
        echo "  ⚠️  package.json 不存在"
        return 1
    fi
    if ! jq empty "$pkg_dir/package.json" 2>/dev/null; then
        echo "  ⚠️  package.json 不是有效的 JSON"
        return 1
    fi
    
    # 3. README.md 存在
    if [ ! -f "$pkg_dir/README.md" ]; then
        echo "  ⚠️  README.md 不存在"
        return 1
    fi
    
    # 4. .cursortoolset 目录存在
    if [ ! -d "$pkg_dir/.cursortoolset" ]; then
        echo "  ⚠️  .cursortoolset 目录不存在"
        return 1
    fi
    
    # 5. .cursortoolset 目录存在
    if [ ! -d "$pkg_dir/.cursortoolset" ]; then
        echo "  ⚠️  .cursortoolset 目录不存在"
        return 1
    fi
    
    # 6. .github/workflows/release.yml 存在
    if [ ! -f "$pkg_dir/.github/workflows/release.yml" ]; then
        echo "  ⚠️  release.yml 不存在"
        return 1
    fi
    
    # 7. .gitignore 存在
    if [ ! -f "$pkg_dir/.gitignore" ]; then
        echo "  ⚠️  .gitignore 不存在"
        return 1
    fi
    
    echo "  ✓ package.json 存在且有效"
    echo "  ✓ README.md 存在"
    echo "  ✓ .cursortoolset/ 目录存在"
    echo "  ✓ .github/workflows/release.yml 存在"
    echo "  ✓ .gitignore 存在"
    return 0
}

# 验证：缓存已清空
verify_clean_cache() {
    # clean --cache 只清理 cache/packages/ 目录
    local pkg_files=$(find "$INSTALL_DIR/cache/packages" -type f ! -name ".DS_Store" 2>/dev/null | wc -l)
    if [ "$pkg_files" -gt 0 ]; then
        echo "  ⚠️  cache/packages 目录未清空 (有 $pkg_files 个文件)"
        return 1
    fi
    return 0
}

# 验证：pack 生成了 tar.gz 文件
verify_pack() {
    local pkg_dir="/tmp/test-init-pkg"
    
    # 检查是否生成了 tar.gz 文件
    local tarball=$(ls "$pkg_dir"/*.tar.gz 2>/dev/null | head -1)
    if [ -z "$tarball" ]; then
        echo "  ⚠️  未生成 tar.gz 文件"
        return 1
    fi
    
    # 检查文件大小 > 0
    local size=$(stat -f%z "$tarball" 2>/dev/null || stat -c%s "$tarball" 2>/dev/null)
    if [ "$size" -eq 0 ]; then
        echo "  ⚠️  tar.gz 文件为空"
        return 1
    fi
    
    echo "  ✓ 生成 tar.gz: $(basename "$tarball")"
    echo "  ✓ 文件大小: $size bytes"
    return 0
}

# 验证：release --dry-run 输出正确
verify_release_dry_run() {
    local output="/tmp/test_output_20.txt"
    
    # 检查输出包含预览信息
    if ! grep -q "预览" "$output" && ! grep -q "dry" "$output" && ! grep -q "版本" "$output"; then
        echo "  ⚠️  输出不包含预览信息"
        return 1
    fi
    
    echo "  ✓ dry-run 模式正常"
    return 0
}

#==========================================
# 执行测试
#==========================================

echo -e "${BLUE}>>> 阶段 1: 清理环境${NC}"
run_test "01" "clean --all" "./dist/cursortoolset clean --all --force" verify_clean_all

echo -e "${BLUE}>>> 阶段 2: 索引管理${NC}"
run_test "02" "registry update" "./dist/cursortoolset registry update" verify_registry_update

echo -e "${BLUE}>>> 阶段 3: 查询功能${NC}"
run_test "03" "list" "./dist/cursortoolset list" verify_list
run_test "04" "search test" "./dist/cursortoolset search test" verify_search
run_test "05" "info test-package" "./dist/cursortoolset info test-package" verify_info

echo -e "${BLUE}>>> 阶段 4: 安装功能${NC}"
run_test "06" "install test-package" "./dist/cursortoolset install test-package" verify_install_test_package
run_test "07" "list --installed" "./dist/cursortoolset list --installed" verify_list_installed

echo -e "${BLUE}>>> 阶段 5: 关键验证 - 二进制编译时间${NC}"
run_test "08" "验证编译时间嵌入" "echo '检查二进制文件...'" verify_build_time

echo -e "${BLUE}>>> 阶段 6: 更新功能${NC}"
run_simple_test "09" "update --packages" "./dist/cursortoolset update --packages"

echo -e "${BLUE}>>> 阶段 7: 卸载功能${NC}"
run_test "10" "uninstall test-package" "./dist/cursortoolset uninstall test-package --force" verify_uninstall
run_test "11" "list --installed (确认卸载)" "./dist/cursortoolset list --installed" verify_list_installed_empty

echo -e "${BLUE}>>> 阶段 8: 批量安装${NC}"
run_simple_test "12" "install (所有包)" "./dist/cursortoolset install"

echo -e "${BLUE}>>> 阶段 9: 缓存管理${NC}"
run_test "13" "clean --cache" "./dist/cursortoolset clean --cache --force" verify_clean_cache

echo -e "${BLUE}>>> 阶段 10: 初始化功能${NC}"
# init 测试需要切换目录
cd /tmp
rm -rf test-init-pkg
run_test "14" "init test-init-pkg" "$PROJECT_DIR/dist/cursortoolset init test-init-pkg" verify_init

run_simple_test "15" "init --force (重新初始化)" "$PROJECT_DIR/dist/cursortoolset init test-init-pkg --force"

cd "$PROJECT_DIR"

echo -e "${BLUE}>>> 阶段 11: 版本管理${NC}"
# version 命令测试（使用 init 创建的目录）
cd /tmp/test-init-pkg
run_simple_test "16" "version (显示版本)" "$PROJECT_DIR/dist/cursortoolset version"

echo -e "${BLUE}>>> 阶段 12: 配置管理${NC}"
cd "$PROJECT_DIR"
run_simple_test "17" "config list" "./dist/cursortoolset config list"
run_simple_test "18" "config get registry_url" "./dist/cursortoolset config get registry_url"

echo -e "${BLUE}>>> 阶段 13: 打包功能${NC}"
cd /tmp/test-init-pkg
# 初始化 git 仓库（pack 需要）
git init -q 2>/dev/null || true
git add -A 2>/dev/null || true
git commit -m "init" -q 2>/dev/null || true
run_test "19" "pack" "$PROJECT_DIR/dist/cursortoolset pack" verify_pack
cd "$PROJECT_DIR"

echo -e "${BLUE}>>> 阶段 14: 发布功能（dry-run）${NC}"
cd /tmp/test-init-pkg
run_test "20" "release --dry-run" "$PROJECT_DIR/dist/cursortoolset release --dry-run" verify_release_dry_run
cd "$PROJECT_DIR"

echo -e "${BLUE}>>> 阶段 15: 同步功能${NC}"
cd /tmp/test-init-pkg
run_simple_test "21" "sync" "$PROJECT_DIR/dist/cursortoolset sync"
cd "$PROJECT_DIR"

# 清理临时文件
rm -f /tmp/test_output_*.txt
rm -rf /tmp/test-init-pkg

#==========================================
# 统计结果
#==========================================
echo "=========================================="
echo "测试结果统计"
echo "=========================================="
echo -e "通过: ${GREEN}$PASSED${NC}"
echo -e "失败: ${RED}$FAILED${NC}"
if [ $SKIPPED -gt 0 ]; then
    echo -e "跳过: ${YELLOW}$SKIPPED${NC}"
fi
echo ""

if [ $FAILED -eq 0 ]; then
    echo -e "${GREEN}🎉 所有测试通过！${NC}"
    exit 0
else
    echo -e "${RED}⚠️  有 $FAILED 个测试失败${NC}"
    echo ""
    echo "请检查失败的测试并修复问题。"
    exit 1
fi
