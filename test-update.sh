#!/bin/bash
# CursorToolset 更新功能测试脚本

set -e

echo "🧪 CursorToolset 更新功能测试"
echo "=============================="
echo ""

# 1. 构建项目
echo "📦 1. 构建项目..."
go build -o cursortoolset
echo "   ✅ 构建完成"
echo ""

# 2. 确保有工具集已安装
echo "📥 2. 确保有工具集已安装..."
./cursortoolset install github-action-toolset 2>&1 | tail -5
echo ""

# 3. 测试更新工具集
echo "🔄 3. 测试更新已安装的工具集..."
./cursortoolset update --toolsets
echo ""

# 4. 测试查看帮助
echo "📖 4. 查看 update 命令帮助..."
./cursortoolset update --help
echo ""

# 5. 验证安装状态
echo "✅ 5. 验证工具集状态..."
./cursortoolset list
echo ""

echo "=============================="
echo "🎉 更新功能测试完成！"
echo ""
echo "💡 提示："
echo "  - update --self: 更新 CursorToolset 自身"
echo "  - update --available: 更新可用工具集列表"
echo "  - update --toolsets: 更新已安装的工具集"
echo "  - update: 更新所有"

