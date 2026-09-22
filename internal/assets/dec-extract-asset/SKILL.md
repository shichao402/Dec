---
name: dec-extract-asset
description: >
  把当前项目里已经验证过的本地能力提炼成可复用的 Dec 官方资产，并经覆写提上游。
  用户说「沉淀」「抽资产」「提到 playbook / 提供方」「dec-extract-asset」，
  或要把本地 Skill/Rule/MCP 交给别的项目复用时，必须用本 Skill。
  硬禁令：未确认目标提供方前，禁止写入 ~/.cursor/skills、~/.claude/skills
  或任何「个人 Skill 副本」；官方沉淀只有 .dec/overrides/ + dec_propose_upstream。
---

# Dec 资产沉淀

当用户已经在当前项目里做出了一个好用的本地能力，并且明确表示要“抽出来放到 Dec 里，给别的项目复用”时，使用这个 Skill。

## 硬门禁（先过门再落盘）

下列任一条未满足时：**只提问，不创建、不覆盖、不移动任何资产文件。**

1. **来源已确认**：要沉淀的目录 / 文件路径清楚（或用户授权代选且已写明假设）。
2. **目标提供方已确认**：例如 `agent-dev-playbook` / `relkit`；消费 pin 用 `latest`，不是 `vault`。
3. **资产名与类型已确认**：跨项目可复用的名字；`skill` / `rule` / `mcp`。
4. **落盘路径唯一**：官方资产只写当前消费项目的 `.dec/overrides/`，再 `dec_propose_upstream`。

### 明确禁止（常见错意）

| 禁止行为 | 正确替代 |
|----------|----------|
| 先写 `~/.cursor/skills/<name>/`「方便下次用」 | 等上游合入后由消费方 `dec_pull` |
| 把「沉淀」理解成 create-skill / 个人家目录副本 | 读本 Skill，走覆写 + 提上游 |
| 直接改提供方源仓、手工开 PR 排版 | `.dec/overrides/` + `dec_propose_upstream`，整理权在提供方 |
| `dec_push` 官方路径进私仓，或只改 `.dec/cache/` | 官方走 propose；仅真正个人笔记才私仓 / cache |
| 未确认提供方就写 SKILL.md / Rule / MCP 片段 | 先问提供方；信息不足则停 |

若已误写入家目录或其他歧路：先删掉误产物，再按门禁重走；不要把误产物「顺手留着」。

## 适用场景

1. 用户说“这个 skill 很好用，帮我沉淀到 dec 里”
2. 用户说“把这个项目里的 agent workflow 抽象成通用资产”
3. 你刚刚在当前项目里产出了可复用的 skill，用户希望以后别的项目直接复用
4. 用户纠正「不要直接落个人副本，要提交到某提供方 / 走 dec-extract-asset」

## 当前范围

- 当前优先处理 Skill 资产
- Rule / MCP 也遵循同一套沉淀思路，但如果用户给的是 Rule / MCP，先沿用这里的抽象流程，再按对应资产格式落盘
- 目标不是机械复制当前项目内容，而是提炼成跨项目可复用的资产

## 工作原则

1. 先确认来源
   - 优先从当前项目已经存在的本地 Skill 目录读取，例如 `.cursor/skills/<name>/`、`.claude/skills/<name>/`、`.codex/skills/<name>/`
   - 如果来源不明确，再向用户确认应该沉淀哪个目录或文件

2. 信息不足时先问清楚，不要替用户拍板
   - 如果缺少来源目录、目标提供方、资产名、目标类型、是否需要变量化等关键信息，先向用户提问
   - 提问尽量使用 agent 可直接继续执行的问答形式，一次给出最少但必要的问题
   - 可以直接给用户可选答案模板，例如：`provider=agent-dev-playbook`、`name=code-review-workflow`、`type=skill`
   - 只在用户已经明确授权你代为决定，或者仓库里存在明显单一约定时，才做最小假设
   - 如果做了任何假设，执行前或结果里都要明确写出

3. 先做抽象，再做搬运
   - 删除当前项目特有的仓库名、路径、业务实体、临时约束
   - 对必须保留、且确实会跨项目变化的值，改为 `{{VAR_NAME}}` 占位符
   - 稳定的流程 bucket 名、label 名、状态名、固定约定不要为了抽象而强行变量化
   - 保留真正可复用的流程、约束、检查清单和输出格式

4. 沉淀到提供方只有一条路：`.dec/overrides/` + `dec_propose_upstream` 提 Issue / PR。
   - 上游仓从订阅面板或 registry 快照的 `origin_repo` 取
   - 提的时候连同脱敏后的来源经验一起给：这条经验在哪验证过、失败模式是什么、边界在哪
   - 合不合、怎么整理由提供方仓决定；不要替它排版或直接改它的源仓
   - 真正的个人笔记才写入私仓 / cache
   不要把官方路径 `dec_push` 进私仓，也不要只改 `.dec/cache/`。

5. 同步维护项目配置
   - 消费仓把官方依赖写进 `.dec/config.yaml` 的 `requires` map
   - 个人启用列表与 `provides` 分开

6. 有变量就补变量说明
   - 如果引入了 `{{VAR_NAME}}`，同步更新 `.dec/vars.yaml`
   - 机器级敏感信息放 `~/.dec/local/vars.yaml`

7. 完成后
   - 提供方：票据合入源仓后由 CI `publish-provides` 写入 registry，消费方再 `dec_pull`
   - 个人：Console **同步** 页 push 私仓，或 Agent `dec_push`（`plane=local`）
   - 入口：Console 项目下级页「本地覆写」+ `dec_propose_upstream`
   - 回报：覆写路径 + Issue/PR URL；不要回报「已写入 ~/.cursor/skills」当作成功

## 信息不全时的推荐提问模板

当你无法安全继续时，优先一次性确认这些问题中的必要子集：

1. 来源是什么？
   - `请告诉我要沉淀的来源路径，例如 .cursor/skills/foo 或某个具体文件。`

2. 要放到哪个提供方？
   - `目标提供方项目名是什么？例如 agent-dev-playbook / relkit。消费 pin 应是 latest，不是 vault。`

3. 新资产叫什么？
   - `沉淀后的资产名是什么？请给一个跨项目可复用的名字。`

4. 资产类型是什么？
   - `这是要沉淀成 skill、rule，还是 mcp？如果不确定，我可以先按 skill 处理，但需要你确认。`

5. 是否允许抽象和变量化？
   - `哪些内容需要替换成通用描述，哪些值又确实值得提炼成 {{VAR_NAME}} 占位符？`

如果只是缺 1 到 2 个信息，不要长篇解释，直接问最关键的问题。**缺提供方时绝不能用「先写个人 Skill」顶替。**

## 推荐执行顺序

1. 过硬门禁（来源 / 提供方 / 名 / 类型）；不过则只问不写
2. 识别要沉淀的本地资产来源并抽象成通用版本
3. 从订阅面板或 `origin_repo` 确认上游仓
4. 写入 `.dec/overrides/` + `dec_propose_upstream`，附脱敏来源经验
5. 仅用户明确说「个人资产 / 私仓」时，才写 cache 再 `dec_push`

## 输出标准

- 产物必须是可直接 propose 的 Dec 覆写资产，而不是停留在分析或建议
- 如果你对提供方名、资产名或变量名做了假设，要明确写出来
- 如果信息不足且用户尚未确认，不要静默创建最终资产
- 如果当前项目还没初始化 Dec，先走 Console **引导 / 项目** 或 `dec_init_project` 再继续
- 不要直接修改 `~/.dec/repo.git` 或手工维护的 IDE 托管副本
- 不要使用已下线的用户面 CLI（`dec pull` / `dec config init` / `dec list` 等）
- 成功标准是「覆写已写 + 上游票据已开」，不是「本机已有一份 Skill」

## 资产格式提醒

- Skill: 目录，且必须包含 `SKILL.md`
- Rule: 单个 `.mdc` 文件
- MCP: 单个 server JSON 片段，至少包含 `command`
