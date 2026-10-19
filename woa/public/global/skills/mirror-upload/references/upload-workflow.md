# 上传实战与踩坑记录

## 端到端案例：flutter 3.38.3 mac 归档补齐

背景：内网 CI 供给盘点 15 项，14 项在位，唯一缺口是
`mirror/flutter/3.38.3/flutter_macos_arm64_3.38.3-stable.zip`（404）。
windows 侧同版本归档已由 CI 懒加载自动回填在位。

### 步骤与结果

1. **先探测，不要盲目下**。HEAD 确认 windows 侧已 200、mac 侧 404，
   于是只需补 mac——windows 那份 1.78GB 根本不用碰。

2. **probe 验证凭据**。`PUT _probe/write-probe.txt` 返回 201，凭据有效。
   这一步避免「下完 2GB 才发现没权限」。

3. **下载官方源**。mac 归档 2,096,231,097 字节，从
   `storage.googleapis.com/flutter_infra_release/releases/stable/macos/` 取。

4. **上传**。PUT 返回 201，本地 sha256
   `76a41524f58c4fabbfd2036d449feb369af7f81711e9752a6371568a55aa8381`，
   与 ADR 0018 裁决中钉定的 darwin 侧值 `76a41524...a8381` 一致——制品正确。

5. **读回校验**。流式 GET 重算远端 sha256，与本地一致，确认无误。

### 实测收益

内网 mirrors 与外网官方源的吞吐对比（同 1.78GB 文件）：

- mirrors：`19.8 MB/s`
- Google 官方源：`4.7 MB/s`

**快约 4.2 倍**。这是预置的价值：不只省一次外网往返，每次构建都省。

## 踩坑清单

### 1. Content-Type 必须 octet-stream

用默认 `application/x-www-form-urlencoded` PUT 二进制，Spring 的
FormContentFilter 会把 body 当表单解析，直接 500。必须显式设置
`Content-Type: application/octet-stream` 且带 `Content-Length`。

### 2. 409 是成功不是失败

制品不可覆盖。PUT 已存在路径返回 409，语义上是「已经有了、没被改坏」，
应视为成功，不要当错误处理。

### 3. 别用文件大小判断一致性

两个文件字节数相同不代表内容相同。必须比 sha256。
真要快速判断，也至少 HEAD 比 `Content-Length` + 抽样比 sha。

### 4. 归档名带平台前缀，且下划线

flutter mac 归档是 `flutter_macos_arm64_3.38.3-stable.zip`——
`macos` 和 `arm64` 之间有下划线。拼错会 404，而 404 与「未预置」表现一致，
极易误判成供给缺口。

### 5. 腾讯源不是万能的

不要因为「腾讯有镜像站」就假定所有工具都有分区。实测结论：
node 有 `nodejs-release` 分区，go 的 goproxy 本身就是腾讯源，
但 **flutter 和 innosetup 没有**。新增前必须 HEAD + GET Range 双验探活。

### 6. 凭据可能不在环境变量

`generic_mirror` 凭据可能只存在于蓝盾 settings 里，本机
cmdkey / _netrc / env / scratch 四处皆无。这种情况下本机无法直接 PUT，
需要先从 DEC 密钥仓库取（`dec_pull`），或让用户提供。

### 7. 大文件用流式，别 read() 整个进内存

2GB 文件 `read_bytes()` 会吃满内存。脚本里 PUT 直接传文件句柄，
校验用分块迭代 `hashlib.update()`。

## 与消费端的配合

消费端（`toolchain.json` + `mirrorfetch` 懒加载）负责 GET 侧，
本 skill 负责 PUT 侧。两侧的**路径分区规则必须一致**：
`mirror/<tool>/<version>/<file>`。改任一侧都要同步另一侧，
否则会出现「传上去了但消费端去别处找」的假缺口。

判断是否为真缺口的顺序：

1. 消费端日志里的目标 URL 是什么
2. 拿那个 URL 直接 `check`
3. 404 才是缺口；200 却还报缺，是路径不一致或缓存问题
