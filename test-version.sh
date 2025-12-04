#!/bin/bash
# CursorToolset 版本控制测试脚本

set -e

echo "🧪 CursorToolset 版本控制测试"
echo "=============================="
echo ""

# 1. 构建带版本号的二进制
echo "📦 1. 构建带版本号的版本..."
go build -ldflags "-X main.Version=v1.0.0 -X main.BuildTime=2024-12-04_11:00:00" -o cursortoolset-v1
echo "   ✅ 构建完成"
echo ""

# 2. 查看版本信息
echo "📌 2. 版本信息:"
./cursortoolset-v1 --version
echo ""

# 3. 测试版本比较功能
echo "🔍 3. 测试版本比较..."
go test ./pkg/version/... -v
echo ""

# 4. 模拟更新检查（当前版本 vs 最新版本）
echo "🆕 4. 模拟更新检查..."
echo "   当前版本: v1.0.0"
echo "   检查 GitHub 最新版本..."

# 使用 Go 代码测试版本检查
cat > /tmp/test_version_check.go << 'EOF'
package main

import (
	"fmt"
	"github.com/firoyang/CursorToolset/pkg/version"
)

func main() {
	currentVersion := "v1.0.0"
	
	fmt.Printf("  📌 当前版本: %s\n", currentVersion)
	
	// 获取最新版本
	release, err := version.GetLatestRelease("firoyang", "CursorToolset")
	if err != nil {
		fmt.Printf("  ⚠️  无法检查版本: %v\n", err)
		return
	}
	
	latestVersion := release.TagName
	fmt.Printf("  📌 最新版本: %s\n", latestVersion)
	
	// 比较版本
	needUpdate := version.NeedUpdate(currentVersion, latestVersion)
	if needUpdate {
		fmt.Printf("  🆕 发现新版本，建议更新！\n")
	} else {
		fmt.Printf("  ✅ 已是最新版本\n")
	}
	
	// 测试不同版本比较
	fmt.Println("\n  📊 版本比较测试:")
	testCases := []struct{
		v1, v2 string
		desc string
	}{
		{"v1.0.0", "v1.0.1", "小版本更新"},
		{"v1.0.0", "v1.1.0", "中版本更新"},
		{"v1.0.0", "v2.0.0", "大版本更新"},
		{"v1.0.0", "v1.0.0", "相同版本"},
		{"v1.0.1", "v1.0.0", "当前版本更新"},
	}
	
	for _, tc := range testCases {
		cmp := version.Compare(tc.v1, tc.v2)
		var result string
		if cmp > 0 {
			result = fmt.Sprintf("%s > %s", tc.v1, tc.v2)
		} else if cmp < 0 {
			result = fmt.Sprintf("%s < %s", tc.v1, tc.v2)
		} else {
			result = fmt.Sprintf("%s = %s", tc.v1, tc.v2)
		}
		fmt.Printf("    %s: %s\n", tc.desc, result)
	}
}
EOF

cd /tmp && go mod init test 2>/dev/null || true
go mod edit -replace github.com/firoyang/CursorToolset=/Users/firoyang/workspace/CursorToolset
go mod tidy 2>&1 | grep -v "go: finding" || true
go run test_version_check.go 2>&1 || echo "   ⚠️  版本检查需要网络连接"
cd - > /dev/null

echo ""

# 5. 清理
echo "🧹 5. 清理临时文件..."
rm -f cursortoolset-v1
rm -rf /tmp/test /tmp/test_version_check.go
echo "   ✅ 清理完成"
echo ""

echo "=============================="
echo "🎉 版本控制测试完成！"

