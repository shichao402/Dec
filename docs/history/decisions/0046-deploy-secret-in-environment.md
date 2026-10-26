# ADR 0046：部署凭据收进 production environment，作用域由平台裁决

- 状态：已采纳
- 日期：2026-10-26
- 相关：需求 21.6、21.7、22.7，ADR 0045

## 背景

需求 21.7 要求部署凭据存放在 Actions environment secret 中，不得存为仓库级 secret，并要启用 environment 保护规则把引用范围限定在 `refs/tags/v*` 上。

在此之前，四个 `DEPLOY_*`（`DEPLOY_SSH_KEY`、`DEPLOY_KNOWN_HOSTS`、`DEPLOY_HOST`、`DEPLOY_USER`）全部是仓库级 secret，`release.yml` 的 deploy job 也没有声明 environment。当时的范围限制只有一条写在 workflow 里的 `if: startsWith(github.ref, 'refs/tags/v')`。

仓库级 secret 的语义是「本仓库任何 workflow、任何分支都能引用」。而部署私钥能登录生产机，部署动作会替换服务端二进制，它等价于服务器的完全控制权。这意味着仓库里任意一个 workflow 都可以取到这把私钥，限制只靠人不去写那行引用。

## 决策

### 一、凭据迁到 production environment

新建 `production` environment，四个 secret 全部迁为 environment 级，并删除仓库级同名项。删除这一步不能省：只迁不删等于两处并存，job 不挂 environment 时仍会从仓库级取到，environment 那份形同虚设。

deploy job 增加一行 `environment: production`。workflow 里原有的 `if` 保留，但它降级为第二道闸门。

### 二、为什么要两道，而不是留一道

`if` 写在仓库的 YAML 里，属于代码。谁能推代码，谁就能删掉这一行；它防的是手误，防不了「有人改了这个文件」。

environment 的部署分支策略存在 GitHub 设置里，在仓库之外，由平台在 job 启动之前判定。workflow 文件里写什么也绕不过它。这是「纪律」与「机制」的差别，也是这条需求真正的价值所在。

### 三、免费套餐的适用范围要按仓库可见性区分

这里有一个容易传错的结论，值得写清楚：

- 公开仓库：GitHub Free 下可以使用分支保护与 environment 部署分支策略。
- 私有仓库：两者都要 Pro 及以上，API 会返回 `403 Upgrade to GitHub Pro or make this repository public`。

`shichao402/Dec` 是公开仓库，所以这些能力都拿得到。此前 workflow 注释里写的「免费套餐没有 environment 保护规则」是错的——当时的依据是一个 `404`，而 `404` 在分支保护语境下表示「还没设置」，不表示「套餐不支持」。

## 验证

不用「配置存在」作为通过标准，而是逐步实测：

1. 部署真实跑通：`v2.1.20` 经 CI 部署成功，部署记录 `env=production`、`ref=v2.1.20`，服务端日志 `健康检查通过：server_version=v2.1.20`，快照写入 `/opt/dec-cloud/snapshots/20261026T041917Z`。
2. 凭据确实只从 environment 取：删掉仓库级 secret 后同一次部署仍然成功，说明取值路径已经切换。
3. 策略确实会拦截，而不只是存在：用一个临时 job 在 `main` 上引用 `production`，job 在 1 秒内失败且没有任何 step 执行，annotation 明确写着 `Branch "main" is not allowed to deploy to production due to environment protection rules.`。拦截发生在 job 启动前，与代码无关。

第 3 条暴露了一个容易误判的点：直接调 `POST /repos/{o}/{r}/deployments` 指定 `environment=production`、`ref=main` 是能成功的，API 层级不校验分支策略。策略只在 Actions job 请求 environment 时生效。所以用 deployments API 做探测会得出「策略没生效」的错误结论。

## 影响

- 部署凭据的可用范围从「整仓库任意 workflow、任意分支」收窄到「`production` environment 且 ref 匹配 `v*` 标签」。
- 打 tag 发布流程不变，发布耗时不变，多出的只是 environment secret 的一次性配置。
- 仓库级只保留三个与部署无关的 secret（`CNB_TOKEN`、`RELKIT_AGENT_URL`、`RELKIT_UPLOAD_TOKEN`），它们服务于其他 workflow。
- 部署失败或回滚的告警路径未受影响，仍在部署入口脚本内。

## 未采纳的方案

**只删仓库级 secret，不建 environment**：workflow 会直接取不到值而失败，等于把部署打断，没有解决作用域问题。

**只迁 secret，不加部署分支策略**：凭据虽然收进了 environment，但任何 ref 都能请求这个 environment，范围限制退回到只有 `if` 一道，等于把「机制」又换回「纪律」。

**用 deployments API 的拒绝结果作为验证依据**：该接口不校验分支策略，会给出误导性的通过结论。验证必须走 Actions job 请求 environment 这条真实路径。
