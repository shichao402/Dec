#!/bin/bash
# CursorToolset 安装测试脚本

set -e

echo "🧪 CursorToolset 安装测试"
echo "=========================="
echo ""

# 1. 构建项目
echo "📦 1. 构建项目..."
go build -o cursortoolset
echo "   ✅ 构建完成"
echo ""

# 2. 列出可用工具集
echo "📋 2. 列出可用工具集..."
./cursortoolset list
echo ""

# 3. 清理之前的安装
echo "🧹 3. 清理之前的安装..."
rm -rf .cursor/rules/github-actions/
rm -rf toolsets/
echo "   ✅ 清理完成"
echo ""

# 4. 安装所有工具集
echo "📥 4. 安装所有工具集..."
./cursortoolset install
echo ""

# 5. 验证安装结果
echo "✅ 5. 验证安装结果..."
echo ""
echo "检查规则文件："
ls -lh .cursor/rules/github-actions/ || echo "   ❌ 规则文件目录不存在"
echo ""

echo "检查工具集目录："
ls -lh .cursor/toolsets/ || echo "   ❌ 工具集目录不存在"
echo ""

# 6. 再次列出工具集（应显示已安装）
echo "📋 6. 确认工具集状态..."
./cursortoolset list
echo ""

echo "=========================="
echo "🎉 测试完成！"

