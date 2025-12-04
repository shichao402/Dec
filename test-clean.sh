#!/bin/bash
# CursorToolset 清理功能测试脚本

set -e

echo "🧪 CursorToolset 清理功能测试"
echo "================================"
echo ""

# 1. 构建项目
echo "📦 1. 构建项目..."
go build -o cursortoolset
echo "   ✅ 构建完成"
echo ""

# 2. 清理旧文件
echo "🧹 2. 清理旧文件..."
./cursortoolset clean --force || true
echo ""

# 3. 安装工具集
echo "📥 3. 安装工具集..."
./cursortoolset install
echo ""

# 4. 验证安装
echo "✅ 4. 验证安装结果..."
echo ""
echo "检查规则文件："
ls -lh .cursor/rules/github-actions/ 2>&1 || echo "   ❌ 规则文件目录不存在"
echo ""
echo "检查 .cursor/toolsets 目录："
ls -lh .cursor/toolsets/ 2>&1 || echo "   ❌ .cursor/toolsets 目录不存在"
echo ""

# 5. 测试 clean --keep-toolsets
echo "🧹 5. 测试 clean --keep-toolsets..."
./cursortoolset clean --keep-toolsets --force
echo ""

# 6. 验证 .cursor/toolsets 目录保留
echo "✅ 6. 验证 .cursor/toolsets 目录保留..."
if [ -d ".cursor/toolsets/github-action-toolset" ]; then
    echo "   ✅ .cursor/toolsets 目录已保留"
else
    echo "   ❌ .cursor/toolsets 目录被删除了！"
    exit 1
fi

if [ -d ".cursor/rules/github-actions" ]; then
    echo "   ❌ 规则目录应该被删除但还存在！"
    exit 1
else
    echo "   ✅ 规则目录已清理"
fi
echo ""

# 7. 测试完全清理
echo "🧹 7. 测试完全清理（包括 toolsets）..."
./cursortoolset clean --force
echo ""

# 8. 验证完全清理
echo "✅ 8. 验证完全清理结果..."
if [ -d ".cursor/toolsets" ]; then
    echo "   ❌ .cursor/toolsets 目录应该被删除但还存在！"
    exit 1
else
    echo "   ✅ .cursor/toolsets 目录已清理"
fi

if [ -d ".cursor/rules" ]; then
    echo "   ❌ 规则目录应该被删除但还存在！"
    exit 1
else
    echo "   ✅ 规则目录已清理"
fi
echo ""

# 9. 验证状态
echo "📋 9. 验证工具集状态..."
./cursortoolset list
echo ""

echo "================================"
echo "🎉 所有测试通过！"

