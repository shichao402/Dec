# 构建指南

## 快速开始

### 基本构建

```bash
# 构建当前平台版本
./build.sh
```

这会：
1. 自动清理旧的构建产物
2. 从 `version.json` 读取版本信息
3. 构建当前平台版本到 `dist/` 目录
4. 生成构建日志到 `logs/` 目录
5. 创建构建信息文件 `BUILD_INFO.txt`

### 构建所有平台

```bash
# 构建所有平台版本（Linux、macOS、Windows）
./build.sh --all
```

## 输出位置

### 默认输出

- **构建产物**: `dist/` 目录
- **日志文件**: `logs/` 目录（带时间戳）
- **构建信息**: `dist/BUILD_INFO.txt`

### 自定义输出位置

```bash
# 指定输出目录
./build.sh -o my-build

# 指定日志目录
./build.sh -l my-logs

# 同时指定
./build.sh -o my-build -l my-logs
```

### 环境变量

也可以通过环境变量设置：

```bash
export OUTPUT_DIR=my-build
export LOG_DIR=my-logs
./build.sh
```

## 日志收集

### 日志文件位置

所有构建日志保存在 `logs/` 目录，文件名格式：
```
logs/build-YYYYMMDD-HHMMSS.log
```

### 日志内容

日志文件包含：
- 构建开始时间
- 版本信息
- 构建过程输出
- 错误信息（如有）
- 构建统计
- 构建完成时间

### 查看日志

```bash
# 查看最新日志
ls -t logs/*.log | head -1 | xargs cat

# 查看所有日志
ls -lh logs/
```

## 清理选项

### 自动清理（默认）

构建脚本默认会在构建前清理：
- 输出目录中的所有文件
- 根目录下的可执行文件

### 禁用自动清理

```bash
# 构建前不清理
./build.sh --no-clean
```

### 构建后清理

```bash
# 构建后清理临时文件
./build.sh --clean-after
```

## 构建信息

### BUILD_INFO.txt

每次构建都会生成 `dist/BUILD_INFO.txt`，包含：

```
CursorToolset 构建信息
====================

版本: v1.0.1
构建时间: 2025-12-04_14:15:55
提交哈希: 0bf48b6
分支: main
构建平台: darwin-amd64
Go 版本: go version go1.25.4 darwin/amd64

构建产物:
  - cursortoolset (6.1M, SHA256: ...)
```

### 使用构建信息

```bash
# 查看构建信息
cat dist/BUILD_INFO.txt

# 获取版本号
grep "版本:" dist/BUILD_INFO.txt

# 获取 SHA256
grep "SHA256:" dist/BUILD_INFO.txt
```

## 完整示例

### 示例 1: 开发构建

```bash
# 快速构建当前平台
./build.sh

# 构建产物在 dist/cursortoolset
# 日志在 logs/build-*.log
```

### 示例 2: 发布构建

```bash
# 构建所有平台版本
./build.sh --all

# 结果：
# dist/cursortoolset-linux-amd64
# dist/cursortoolset-linux-arm64
# dist/cursortoolset-darwin-amd64
# dist/cursortoolset-darwin-arm64
# dist/cursortoolset-windows-amd64.exe
```

### 示例 3: 自定义构建

```bash
# 自定义输出和日志目录
./build.sh -o release -l release-logs --all

# 结果：
# release/ 目录包含所有构建产物
# release-logs/ 目录包含构建日志
```

## 故障排查

### 构建失败

1. **查看日志文件**：
   ```bash
   ls -t logs/*.log | head -1 | xargs cat
   ```

2. **检查版本文件**：
   ```bash
   cat version.json
   ```

3. **检查 Go 环境**：
   ```bash
   go version
   ```

### 清理问题

如果遇到遗留文件问题：

```bash
# 手动清理
rm -rf dist/ logs/ cursortoolset

# 重新构建
./build.sh
```

## 与 Makefile 的区别

| 特性 | build.sh | Makefile |
|------|----------|----------|
| 日志收集 | ✅ 自动保存到文件 | ❌ 仅控制台输出 |
| 输出位置 | ✅ 可配置（默认 dist/） | ❌ 固定位置 |
| 自动清理 | ✅ 构建前清理 | ❌ 需手动清理 |
| 构建信息 | ✅ 生成 BUILD_INFO.txt | ❌ 无 |
| 多平台构建 | ✅ 支持 | ✅ 支持 |
| 开发环境 | ✅ 自动设置 | ✅ 自动设置 |

## 最佳实践

1. **开发时使用 build.sh**：日志可追溯，输出位置明确
2. **发布前使用 --all**：确保所有平台版本都构建成功
3. **定期清理日志**：避免日志文件过多
   ```bash
   # 保留最近 10 个日志
   ls -t logs/*.log | tail -n +11 | xargs rm
   ```
4. **检查构建信息**：发布前确认版本号和 SHA256

