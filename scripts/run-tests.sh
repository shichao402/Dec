#!/bin/bash
#
# Dec 完整功能测试脚本
# 使用方法: ./scripts/run-tests.sh
#
# 测试原则：
# 1. 每个测试必须有明确的验证条件
# 2. 命令成功 ≠ 测试通过，必须验证结果
# 3. 关键文件必须存在且内容正确
#

set -e

# 清除开发环境变量，使用生产环境
unset DEC_ROOT
unset DEC_HOME

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
INSTALL_DIR="$HOME/.decs"

cd "$(dirname "$0")/.."
PROJECT_DIR=$(pwd)

echo "=========================================="
echo "Dec 完整功能测试"
echo "=========================================="
echo "项目目录: $PROJECT_DIR"
echo "安装目录: $INSTALL_DIR"
echo ""

# 构建（不使用 make，避免开发环境变量）
echo ">>> 构建管理器..."
mkdir -p dist
go build -o dist/dec .
echo "✅ 构建完成: dist/dec"
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
    
    # 4. .dec 目录存在
    if [ ! -d "$pkg_dir/.dec" ]; then
        echo "  ⚠️  .dec 目录不存在"
        return 1
    fi
    
    echo "  ✓ 包目录完整"
    echo "  ✓ package.json 存在"
    echo "  ✓ 二进制文件存在"
    echo "  ✓ .dec 目录存在"
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

# 验证：二进制文件中嵌入了编译时间（通过输出验证）
verify_build_time_output() {
    local output="/tmp/test_output_08.txt"
    
    # 检查输出包含时间戳格式
    if ! grep -qE '^20[0-9]{2}-[0-9]{2}-[0-9]{2}_[0-9]{2}:[0-9]{2}:[0-9]{2}$' "$output"; then
        echo "  ⚠️  未找到嵌入的编译时间"
        return 1
    fi
    
    local build_time=$(cat "$output" | head -1)
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
    
    # 4. .dec 目录存在
    if [ ! -d "$pkg_dir/.dec" ]; then
        echo "  ⚠️  .dec 目录不存在"
        return 1
    fi
    
    # 5. .dec 目录存在
    if [ ! -d "$pkg_dir/.dec" ]; then
        echo "  ⚠️  .dec 目录不存在"
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
    echo "  ✓ .dec/ 目录存在"
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
    
    # 检查输出包含预览信息（必须同时包含关键信息）
    if ! grep -qE "(预览|dry|Dry)" "$output"; then
        echo "  ⚠️  输出不包含预览/dry-run 标识"
        return 1
    fi
    
    echo "  ✓ dry-run 模式正常"
    return 0
}

# 验证：update --packages 输出正确
verify_update_packages() {
    local output="/tmp/test_output_09.txt"
    
    # 检查输出包含更新统计信息
    if ! grep -qE "(更新|update|统计)" "$output"; then
        echo "  ⚠️  输出不包含更新统计信息"
        return 1
    fi
    
    echo "  ✓ 更新命令执行正常"
    return 0
}

# 验证：批量安装后所有包都存在
verify_install_all() {
    local output="/tmp/test_output_12.txt"
    
    # 获取 registry 中的包数量
    local registry_count=$(jq '.packages | length' "$INSTALL_DIR/config/registry.json" 2>/dev/null || echo "0")
    
    # 检查 repos 目录中的包数量
    local installed_count=$(find "$INSTALL_DIR/repos" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l | tr -d ' ')
    
    if [ "$installed_count" -lt "$registry_count" ]; then
        echo "  ⚠️  安装的包数量 ($installed_count) 少于 registry 中的包数量 ($registry_count)"
        return 1
    fi
    
    echo "  ✓ 已安装 $installed_count 个包"
    return 0
}

# 验证：init --force 覆盖了文件
verify_init_force() {
    local pkg_dir="/tmp/test-init-pkg"
    
    # 检查 package.json 存在
    if [ ! -f "$pkg_dir/package.json" ]; then
        echo "  ⚠️  package.json 不存在"
        return 1
    fi
    
    echo "  ✓ init --force 执行成功"
    return 0
}

# 验证：version 在包目录中显示正确
verify_version() {
    local output="/tmp/test_output_16.txt"
    
    # 检查输出包含包名和版本
    if ! grep -qE "(📦|版本|version)" "$output"; then
        echo "  ⚠️  输出不包含版本信息"
        return 1
    fi
    
    echo "  ✓ version 显示正常"
    return 0
}

# 验证：config list 输出配置
verify_config_list() {
    local output="/tmp/test_output_17.txt"
    
    # 检查输出包含 registry_url
    if ! grep -q "registry_url" "$output"; then
        echo "  ⚠️  输出不包含 registry_url"
        return 1
    fi
    
    echo "  ✓ config list 显示正常"
    return 0
}

# 验证：config get 返回有效 URL
verify_config_get() {
    local output="/tmp/test_output_18.txt"
    
    # 检查输出是有效的 URL
    if ! grep -qE "^https?://" "$output"; then
        echo "  ⚠️  输出不是有效的 URL"
        return 1
    fi
    
    echo "  ✓ config get 返回有效 URL"
    return 0
}

# 验证：sync 执行正常
verify_sync() {
    local output="/tmp/test_output_21.txt"
    
    # sync 在包目录中执行，应该有输出
    # 即使没有变化也应该有提示
    if [ ! -s "$output" ]; then
        echo "  ⚠️  sync 无输出"
        return 1
    fi
    
    echo "  ✓ sync 执行正常"
    return 0
}

#==========================================
# 执行测试
#==========================================

echo -e "${BLUE}>>> 阶段 1: 清理环境${NC}"
run_test "01" "clean --all" "./dist/dec clean --all --force" verify_clean_all

echo -e "${BLUE}>>> 阶段 2: 索引管理${NC}"
run_test "02" "registry update" "./dist/dec registry update" verify_registry_update

echo -e "${BLUE}>>> 阶段 3: 查询功能${NC}"
run_test "03" "list" "./dist/dec list" verify_list
run_test "04" "search test" "./dist/dec search test" verify_search
run_test "05" "info test-package" "./dist/dec info test-package" verify_info

echo -e "${BLUE}>>> 阶段 4: 安装功能${NC}"
run_test "06" "install test-package" "./dist/dec install test-package" verify_install_test_package
run_test "07" "list --installed" "./dist/dec list --installed" verify_list_installed

echo -e "${BLUE}>>> 阶段 5: 关键验证 - 二进制编译时间${NC}"
run_test "08" "验证编译时间嵌入" "strings $INSTALL_DIR/repos/test-package/test-package | grep -E '^20[0-9]{2}-[0-9]{2}-[0-9]{2}_[0-9]{2}:[0-9]{2}:[0-9]{2}$' | head -1" verify_build_time_output

echo -e "${BLUE}>>> 阶段 6: 更新功能${NC}"
run_test "09" "update --packages" "./dist/dec update --packages" verify_update_packages

echo -e "${BLUE}>>> 阶段 7: 卸载功能${NC}"
run_test "10" "uninstall test-package" "./dist/dec uninstall test-package --force" verify_uninstall
run_test "11" "list --installed (确认卸载)" "./dist/dec list --installed" verify_list_installed_empty

echo -e "${BLUE}>>> 阶段 8: 批量安装${NC}"
run_test "12" "install (所有包)" "./dist/dec install" verify_install_all

echo -e "${BLUE}>>> 阶段 9: 缓存管理${NC}"
run_test "13" "clean --cache" "./dist/dec clean --cache --force" verify_clean_cache

echo -e "${BLUE}>>> 阶段 10: 初始化功能${NC}"
# init 测试需要切换目录
cd /tmp
rm -rf test-init-pkg
run_test "14" "init test-init-pkg" "$PROJECT_DIR/dist/dec init test-init-pkg" verify_init

run_test "15" "init --force (重新初始化)" "$PROJECT_DIR/dist/dec init test-init-pkg --force" verify_init_force

cd "$PROJECT_DIR"

echo -e "${BLUE}>>> 阶段 11: 版本管理${NC}"
# version 命令测试（使用 init 创建的目录）
cd /tmp/test-init-pkg
run_test "16" "version (显示版本)" "$PROJECT_DIR/dist/dec version" verify_version

echo -e "${BLUE}>>> 阶段 12: 配置管理${NC}"
cd "$PROJECT_DIR"
run_test "17" "config list" "./dist/dec config list" verify_config_list
run_test "18" "config get registry_url" "./dist/dec config get registry_url" verify_config_get

echo -e "${BLUE}>>> 阶段 13: 打包功能${NC}"
cd /tmp/test-init-pkg
# 初始化 git 仓库（pack 需要）
git init -q 2>/dev/null || true
git add -A 2>/dev/null || true
git commit -m "init" -q 2>/dev/null || true
run_test "19" "pack" "$PROJECT_DIR/dist/dec pack" verify_pack
cd "$PROJECT_DIR"

echo -e "${BLUE}>>> 阶段 14: 发布功能（dry-run）${NC}"
cd /tmp/test-init-pkg
run_test "20" "release --dry-run" "$PROJECT_DIR/dist/dec release --dry-run" verify_release_dry_run
cd "$PROJECT_DIR"

echo -e "${BLUE}>>> 阶段 15: 同步功能${NC}"
cd /tmp/test-init-pkg
run_test "21" "sync" "$PROJECT_DIR/dist/dec sync" verify_sync
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
