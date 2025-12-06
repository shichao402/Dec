# CursorToolset 安装指南

## 一键安装

### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/shichao402/CursorToolset/ReleaseLatest/scripts/install.sh | bash
```

### Windows (PowerShell)

```powershell
iwr -useb https://raw.githubusercontent.com/shichao402/CursorToolset/ReleaseLatest/scripts/install.ps1 | iex
```

## 安装位置

```
~/.cursortoolsets/
├── bin/
│   └── cursortoolset      # 可执行文件
├── config/
│   ├── system.json        # 系统配置
│   ├── settings.json      # 用户配置（可选）
│   └── registry.json      # 包索引缓存
├── repos/                  # 已安装的包
└── cache/                  # 下载缓存
```

## 环境变量

安装脚本会自动配置 PATH。如果需要手动配置：

```bash
export PATH="$HOME/.cursortoolsets/bin:$PATH"
```

## 验证安装

```bash
cursortoolset --version
cursortoolset list
```

## 更新

```bash
cursortoolset update
```

## 卸载

```bash
rm -rf ~/.cursortoolsets
# 并从 shell 配置文件中移除 PATH 配置
```

## 开发者安装

如果你要参与开发，请参考 [开发指南](DEVELOPMENT.md)。

```bash
# 克隆仓库
git clone https://github.com/shichao402/CursorToolset.git
cd CursorToolset

# 源码安装
make install-dev
```
