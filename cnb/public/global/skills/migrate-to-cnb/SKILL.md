---
name: migrate-to-cnb
description: >
  把当前 Git 仓库迁到 CNB 当主力远程：建仓或复用已有仓、推分支与标签、把 origin 改到 cnb.cool，
  并在存在 GitHub↔CNB 桥接时把该仓标成 CNB 主力。用户说「迁移到 CNB」「迁到 cnb」「migrate to cnb」时使用。
---

# 把仓库迁到 CNB

把当前工作区的 Git 仓库改成以 [CNB](https://cnb.cool) 为主力。步骤必须按下面顺序做完；认证、组织名、仓库名都从**当前环境与官方接口**读取，禁止依赖某台机器上「刚好已经有」的凭据、目录或仓库。

## 目标状态

1. CNB 上有同名仓库（默认私有），`main`（或当前默认分支）和标签与本地一致。
2. 本地 `origin` 指向 `https://cnb.cool/<org>/<repo>.git`（或团队约定的 SSH 等价地址）。
3. 日常 `git push` / `git pull` 走 CNB。
4. 若该组织使用 GitHub↔CNB 定时镜像：该仓必须进入「CNB 主力」名单，否则下一次 GitHub→CNB 会覆盖刚推上去的提交。

## 不要做的事

- 不要把访问令牌写进仓库、remote URL 或聊天回复。
- 不要假设本机已登录、已装 CLI、已有某个本地目录或某个固定盘符路径。
- 不要把 `cnb login` 的 OAuth 当成一定能建仓。
- 不要在未确认桥接仓的情况下跳过「CNB 主力名单」（有桥接却漏登记，等于迁完会被镜像打回 GitHub 旧状态）。

## 0. 读当前仓库

在仓库根执行，记下分支名、HEAD、已有 remote：

```text
git remote -v
git branch --show-current
git log -1 --format=%H
git tag
```

仓库名默认用当前目录名，或 GitHub remote 的 `owner/name` 里的 `name`。若用户指定了 CNB 仓库名，用用户的。

组织 slug 不要写死。用第 2 步的 `cnb users get-user-info`（个人空间常见即用户名），或用户明确给出的组织路径。

## 1. 工具与认证（每台设备都要能独立完成）

### CLI

需要 `cnb`（npm 包 `@cnbcool/cnb-cli`）。没有则安装：

```text
npm install -g @cnbcool/cnb-cli
cnb --version
cnb status
```

### 访问令牌

建仓需要 **`group-resource:rw`**；推代码需要 **`repo-code:rw`**。

便携获取方式（任选其一，按环境检查，缺了就当场补，不要「沿用本机旧凭据」）：

1. **环境变量 `CNB_TOKEN`**：已设置且 `cnb status` 可用即可。
2. **Dec 用户平面 cnb bundle**：先确认用户平面已启用 `cnb` 并完成过 `dec pull`。令牌在用户主目录下 Dec secrets 的标准位置：
   - `~/.dec/secrets/bundles/cnb/.env/app.env`（其中应有 `CNB_TOKEN=...`）
   - 加载进当前 shell 的 `CNB_TOKEN` 后再调 CLI。文件不存在就停：让用户在 CNB 网页创建访问令牌，写入该文件或环境变量，再 `dec pull` / `dec push` 把 secrets 同步到其他设备。不要发明别的本机路径。
3. **CNB 网页新建访问令牌**：打开 CNB → 个人设置 → 访问令牌，勾选上面两个 scope，把值赋给 `CNB_TOKEN`。

`cnb login`（设备码 OAuth）经常**没有** `group-resource:rw`，建仓会 403。只把它当「已有仓库的只读/有限写」备用，建仓失败时改用访问令牌。

自检（不要打印令牌正文）：

```text
cnb status
cnb users get-user-info
cnb repositories create-repo --help
```

`create-repo --help` 应显示权限含 `group-resource:rw`。若 `get-user-info` 失败，先修认证再往下。

Git 推送同一条令牌即可：HTTPS + Git Credential Helper，或把公钥登记到 CNB 后改用 SSH。推送前用 `git ls-remote https://cnb.cool/<org>/<repo>.git` 验证能读；失败则补凭据，不要换一台「已经配好」的机器假设。

## 2. 建仓或复用已有仓

```text
cnb repositories get-repos --search <repo> --page-size 20
```

- **没有同名仓**：

```text
cnb repositories create-repo --slug <org> --name <repo> --visibility private --description "<一句话描述>"
```

默认私有。用户要求公开再改 `--visibility public`。

- **已存在（409 / 搜索命中）**：不要删仓。多半是 GitHub→CNB 镜像先建过。继续推代码、改 origin、登记主力名单。

CNB 网页地址：`https://cnb.cool/<org>/<repo>`。

## 3. 对齐提交后再推

```text
git ls-remote --heads --tags origin
git ls-remote --heads --tags https://cnb.cool/<org>/<repo>.git
```

把本地当前分支和标签推到 CNB（分支名按实际替换）：

```text
git push https://cnb.cool/<org>/<repo>.git HEAD:refs/heads/<branch>
git push https://cnb.cool/<org>/<repo>.git --tags
```

CNB 已与本地同一 SHA 时，推送会显示 up-to-date，仍要继续改 origin。

## 4. 改本地 remote

把原 `origin` 改成 CNB。若原来的 origin 是 GitHub（或其他备份），改名前保留，便于手工补推：

```text
git remote rename origin github
git remote add origin https://cnb.cool/<org>/<repo>.git
git fetch origin
git branch --set-upstream-to=origin/<branch> <branch>
```

若当前没有名为 `origin` 的 remote，直接 `git remote add origin https://cnb.cool/<org>/<repo>.git`。

不要用 `git config --global`。不要 `--force` 推 CNB，除非用户明确要求覆盖。

## 5. 有 GitHub↔CNB 桥接时：确认主仓策略

许多团队用单独的桥接仓做定时镜像：

- `default_primary: github`：未点名仓 GitHub → CNB
- `default_primary: cnb`：未点名仓 CNB → GitHub
- `primary_overrides`：只列与默认侧不同的例外

查找方式（不要假设本机已 clone）：

```text
cnb repositories get-repos --search cnb-bridge --page-size 20
```

若存在该仓：用 `git clone https://cnb.cool/<org>/cnb-bridge.git`（或组织里实际的桥接仓路径）拿到 `config.yaml`。读取 `sync.default_primary` 与 `sync.primary_overrides`：若该仓未点名且默认已是 `cnb`，无需加名单；若默认是 `github`，在例外中写 `owner/repo: cnb`。例外只写与默认侧不同的仓，禁止维护全量 CNB 名单。提交并推送到**桥接仓自己的 origin（CNB）**。

没有桥接仓、用户也确认没有定时 GitHub→CNB：跳过本步并在结果里写明。

## 6. 验收

- 浏览器或 CLI 能打开 `https://cnb.cool/<org>/<repo>`。
- `git remote -v` 中 `origin` 是 CNB。
- `git status` 跟踪 `origin/<branch>`。
- 桥接若存在：策略判定本仓为 CNB 主仓，且桥接仓已推送。

## 完成后告诉用户

用三行说清即可：CNB URL、本地 `origin` 已指向 CNB、以后推这个仓走 CNB。有桥接则说明主仓策略已确认、定时同步方向为 CNB→GitHub。
