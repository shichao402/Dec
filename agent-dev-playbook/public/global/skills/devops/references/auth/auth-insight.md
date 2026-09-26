# 洞察命令

这些命令用于分析成员权限、资源授权面和项目授权风险。默认是只读命令。

## `analyze-member`

生成成员权限分析报告。

```bash
python scripts/auth_client.py analyze-member \
  --project-id demo \
  --member-id alice
```

适合先看成员总体权限轮廓，再决定是否进入续期、移除或交接流程。

## `permissions-matrix`

查看某个资源的权限矩阵。

```bash
python scripts/auth_client.py permissions-matrix \
  --project-id demo \
  --resource-type pipeline \
  --resource-code p-123
```

适合回答：
- 谁拥有这个资源的权限
- 是哪些用户组承载了这些权限

## `diagnose`

诊断某成员为什么没有某个权限。

```bash
python scripts/auth_client.py diagnose \
  --project-id demo \
  --member-id alice \
  --resource-type pipeline \
  --resource-code p-123 \
  --action pipeline_execute
```

常见用途：
- 明确缺的是哪个用户组
- 判断是未授权、已过期还是资源范围不匹配

## `compare`

比较两个用户的权限差异。

```bash
python scripts/auth_client.py compare \
  --project-id demo \
  --user-id-a alice \
  --user-id-b bob \
  --resource-type pipeline
```

适合做“以老带新”式授权复制前的差异确认。

## `health-check`

做项目授权健康检查。

```bash
python scripts/auth_client.py health-check \
  --project-id demo
```

适合定期扫描：
- 孤立管理员
- 即将过期的授权
- 风险授权分布
