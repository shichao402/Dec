# 常见构建错误模式与修复建议

基于日志内容进行模式匹配，快速定位根因。

## 依赖安装失败

| 日志特征 | 根因 | 修复建议 |
|----------|------|----------|
| `npm ERR! network` / `ETIMEDOUT` / `ECONNREFUSED` | 网络/registry 不可达 | 检查构建机网络、npm registry 配置 |
| `npm ERR! 404 Not Found` | 包不存在、私有 registry 配错，或镜像缺少**当前平台**的 optional 包 | 确认包名与 `.npmrc`；Windows 上再查是否缺 `*-windows-*` 平台包，需要则显式安装/验证或换可回源的镜像 |
| `npm ERR! ERESOLVE` | 依赖版本冲突 | `npm install --legacy-peer-deps` 或修复版本 |
| `yarn error An unexpected error occurred` | yarn.lock 损坏或缓存异常 | 清除缓存重试 |
| `pnpm ERR! LOCKFILE_MISSING` | lockfile 缺失 | 提交 pnpm-lock.yaml |

## 编译/构建错误

| 日志特征 | 根因 | 修复建议 |
|----------|------|----------|
| `error TS\d+:` | TypeScript 编译错误 | 根据错误码定位文件修复类型 |
| `ESLint.*error` / `✖ \d+ problems` | ESLint 检查不通过 | 修复 lint 错误或调整规则 |
| `Module not found` / `Cannot resolve` | 模块路径错误 | 检查 import 路径、alias 配置 |
| `JavaScript heap out of memory` | Node 内存溢出 | 增加 `--max-old-space-size` |
| `Segmentation fault` | Node 二进制兼容问题 | 检查 Node 版本、重建 node_modules |

## 测试失败

| 日志特征 | 根因 | 修复建议 |
|----------|------|----------|
| `FAIL.*\.test\.(ts|js)` | Jest/Vitest 用例失败 | 查看 expect 与 received 差异 |
| `Timeout.*exceeded` | 测试超时 | 增大 timeout 或排查异步逻辑 |
| `Cannot find module` (test context) | 测试环境配置缺失 | 检查 jest.config / vitest.config |

## 部署/发布失败

| 日志特征 | 根因 | 修复建议 |
|----------|------|----------|
| `Permission denied` / `403 Forbidden` | 权限不足 | 检查部署账号权限 |
| `Connection refused` / `No route to host` | 目标机器不可达 | 检查网络、目标服务是否存活 |
| `disk space` / `No space left` | 磁盘空间不足 | 清理构建机磁盘 |
| `docker.*pull.*failed` | 镜像拉取失败 | 检查镜像仓库地址和凭证 |

## 子流水线失败

| 日志特征 | 根因 | 修复建议 |
|----------|------|----------|
| `subPipeline.*FAILED` | 子流水线内部失败 | 提取子流水线 buildId 递归分析 |
| `Waiting for sub-pipeline timeout` | 子流水线超时 | 排查子流水线卡住环节 |

## 环境/配置问题

| 日志特征 | 根因 | 修复建议 |
|----------|------|----------|
| `env.*not set` / `undefined variable` | 环境变量缺失 | 在流水线变量中配置 |
| `certificate.*expired` / `SSL` | 证书过期 | 更新证书或跳过验证（临时） |
| `command not found` | 构建工具缺失 | 检查构建镜像是否包含所需工具 |

## 蓝盾平台级错误

| 错误码 | 错误信息 | 根因 | 修复建议 |
|--------|----------|------|----------|
| 2103003 | 第三方构建机状态异常/Bad build agent status | Agent 节点离线或繁忙 | 重试构建；持续失败联系 DevOps 团队检查 Agent 状态 |
| 2103004 | 构建机启动超时 | Agent 启动慢或资源不足 | 重试；检查 Agent 机器负载 |
| 2199002 | 子流水线运行失败 | 子流水线内部异常 | 递归分析子流水线（`--recursive`） |
| 2199001 | 子流水线启动失败 | 子流水线配置错误或无权限 | 检查子流水线 ID 和权限配置 |
| 2101001 | 流水线已被锁定 | 其他构建正在运行或人工锁定 | 等待或联系锁定者解除 |
| 2101002 | 流水线并发数已达上限 | 并发限制 | 等待队列排空或增加并发配额 |
| 2101003 | 流水线已被禁用 | 流水线处于停用状态 | 联系管理员启用流水线 |
| 2104001 | 构建超时 | 执行时间超过 Job/Task 设定的最大时长 | 优化构建速度或增大超时配置 |
| 2104002 | 排队超时 | 等待 Agent 资源时间过长 | 检查 Agent 池是否充足 |
| 2128001 | 人工审核超时 | 质量红线/人工卡点未在规定时间审批 | 联系审批人操作 |
| 2128002 | 人工审核驳回 | 质量红线审核不通过 | 查看驳回原因，修复后重试 |
| N/A | `Script command execution failed with exit code(N)` | 脚本插件返回非零退出码 | 查看对应 task 日志定位脚本错误 |
| N/A | `Market atom execution exit with StackTrace` | 商店插件内部异常 | 查看 StackTrace 定位具体报错 |
| 800006 | 用户配置错误 | **杂物箱**，同一码可覆盖缓存竞态、外部 SCM、编码、脚本非零退出等 | 不要停在这句；拉该 step 完整日志，按时间线找首次异常 |

## 状态误判

| 现象 | 含义 | 做法 |
|------|------|------|
| 流水线绿，外部没更新 | 假绿：嵌套 pwsh 吞退出码、发布脚本未校验远端 | 查 `$LASTEXITCODE` / 外部 ID；不要只信 SUCCEED |
| 流水线红，外部已提交/已上传 | 假红：后置校验或 stdout 文案解析失败 | 先核对外部系统，禁止直接再发一次 |
| 只盯日志最后一行 | 最后异常常是下游派生（如缺某个 exe） | 从首次 `Access is denied` / 404 / 非零退出往下追 |
| 归档步骤找不到根目录文件 | glob `**/` 不匹配工作区根文件 | 改精确路径；见 [ci-runtime-pitfalls.md](../generate/ci-runtime-pitfalls.md) |
