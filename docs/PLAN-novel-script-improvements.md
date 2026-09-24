# 计划：小说转剧本 · 剧本拆分人物形象 · 场景描述 改进实验

> 分支：`exp/novel-script-improvements`
> 基线：`fix/auto-series-episode-increment`（领先 main 42 个提交）
> 日期：2026-09-24
> 状态：草案（分阶段实施，每阶段独立可验收、可回退）

---

## 1. 目标

对 Go-Ai-Studio 的轻量故事生成链路（`internal/api/lightweight_story_*`）做三项可验证的改进实验：

1. **小说转剧本**：新增「小说原文 → 结构化剧本初稿」环节，让系统能直接消费小说，而不是只接受现成剧本/分镜文本。
2. **剧本拆分人物形象**：人物从「单次 LLM 顺带输出」升级为「结构化人物库」——补性格/关系/首次出场等字段，做跨集别名去重合并，防人物形象漂移。
3. **场景描述**：场景从裸 `image_prompt/video_prompt` 升级为带结构化场景资产（地点 / 时间 / 出场角色 / 镜头功能 / 情绪弧），保证跨镜场景一致。

全程约束：**小步、可回退、不破坏现有四种生成模式（standard / high_quality / h3_short / storyboard）**。

---

## 2. 现状分析（已核实，2026-09-24）

### 2.1 现有链路

```
AutoGenerateRequest.Plot（剧本/分镜文本）
        │
        ├─ standard / high_quality ── 单次 LLM ──► lightweightStoryResponse
        ├─ h3_short ──► runLightweightStoryBreakdown（叙事节点）
        │                    └─► 单次 LLM ──► lightweightStoryResponse
        └─ storyboard ──► 分镜脚本翻译为 image/video prompt（结构同上）
```

入口：`runLightweightStoryGeneration`（lightweight_story_generation.go:1991）
任务注册：`projects.go`（auto_generate_project / continue_auto_generate_project）

### 2.2 关键数据结构（现状）

| 结构 | 字段 | 缺陷 |
|------|------|------|
| `lightweightStoryCharacter` | name / gender / age / height / era / country / appearance | 无性格、无关系、无首次出场；跨集仅**精确同名**跳过，无别名合并 |
| `lightweightStoryScene` | scene_id / duration_seconds / narration / image_prompt / video_prompt | 无地点/时间/出场角色/镜头功能/情绪弧；场景一致性全靠 prompt 约束，无结构化资产可复用 |
| `lightweightStoryEpisodeMemory` | story_summary / ending_state / character_status / open_threads | 已有每集记忆，可复用 |
| `lightweightStoryBreakdownResponse` | narrative_nodes（含 characters / location_hint） | H3 短剧前置节点，人物/地点信息**产生后即丢弃**，未沉淀成资产 |

### 2.3 已确认的基线

- `go test ./internal/api -run "LightweightStory|Storyboard|H3" -count=1` → **ok**
- 校验入口 `validateLightweightStoryResponse`（lightweight_story_generation.go:1656）：已做场景数/时长/image_prompt/video_prompt/角色重名检查，**失败即硬失败**，无自动修复
- 角色入库受指纹锁定保护（characters.go:1408 附近），字段扩展需兼容旧数据

### 2.4 与用户目标的对齐

| 用户目标 | 现状差距 |
|----------|----------|
| 小说转剧本 | **不存在独立环节**。`Plot` 要求直接是剧本/分镜；小说原文只能被 qwen_tts 等旁路消费 |
| 剧本拆分人物形象 | 有基本人物表，但缺性格/关系/首次出场、缺别名合并、appearance 无跨镜一致性校验 |
| 场景描述 | 场景只有成对 prompt，无结构化场景资产；同一地点跨镜靠模型自觉 |

---

## 3. 对标开源项目的方法取舍（只吸收适配本项目的部分）

参考：Toonflow-app、baoerger/novel-to-script、JohnvenTom/Novel2Script-AI、wswhhhc/novel2script、slow2342/n2s、AIScript、R2 论文。

| 开源方法 | 是否吸收 | 理由 / 适配方式 |
|----------|----------|-----------------|
| 多阶段流水线（章分析→人物库→场景规划→生成） | **部分吸收** | 本项目追求轻量（单机 LLM 任务），不全盘重写；先加「小说→剧本」环节，后续阶段复用其输出 |
| 人物缓存锚定 + 跨章/跨集去重合并 | **吸收**（阶段 2） | 成本低、收益高：对既有 `existingCharacters` 增加别名归并，先精确后模糊 |
| 场景结构增强（地点/时间/出场角色/功能/情绪弧） | **吸收**（阶段 3） | 扩展 `lightweightStoryScene`，字段缺省不报错，向后兼容 |
| Schema 强约束 + 自动修复（最多 N 次迭代） | **吸收**（阶段 4） | 把硬失败改为「可修复错误→带错误说明重试 1–2 次→仍失败才报错」 |
| RAG/事件图谱/实体记忆 | **暂不吸收** | 引入向量库/图库超出本轮实验范围；用现有 `episode_memory` + 人物库先覆盖 80% 场景 |
| 心理占比巡检（>8% 自动精简） | **暂不吸收** | 本项目 scene 无心理单元概念，收益不明显 |

---

## 4. 分阶段实施计划

> 每个阶段：独立 commit → 跑测试 → 验收通过才进入下一阶段。任何阶段可单独回退（分支上单阶段 commit revert 即可，不影响其他阶段）。

### 阶段 0：基线固化（半天）

- **内容**：记录当前基线（已完成：测试 ok、分支已建）；补一次全量 `go build ./...`
- **产出**：基线构建+测试通过记录
- **验收**：`go build ./...` 与现有全部测试通过

### 阶段 1：小说转剧本（新增能力，不动现有链路）

**目标**：新增「小说原文 → 结构化剧本初稿」的一级能力，输出可被现有 scene 生成继续消费。

**改动文件**：
- 新增 `internal/api/lightweight_story_novel_to_script.go`：
  - `lightweightNovelScriptScene`：scene_id / location（地点+时间）/ cast（出场角色）/ 单元（action / dialogue / narration 混合列表）
  - `buildNovelToScriptPrompts()`：两段式——先提取情节主线+人物预表，再逐场景展开剧本（对标 R2 的 outline→scene 两段式，但保持单文件内完成）
  - `parseNovelToScript()` + 结构校验（对标 schema 校验：场景头必填、dialogue 必须带角色）
- 新入口函数 `runNovelToScript()`，任务进度复用 `task.GlobalTaskManager`

**关键决策**：
- 新接口、新文件，**不改** `AutoGenerateRequest` 语义，不动 `runLightweightStoryGeneration` 主链路
- 输出 JSON 与现有 `lightweightStoryScene` 分离（剧本初稿 ≠ 镜头提示词），避免污染既有产物

**风险**：接口新增无破坏性；小说过长超上下文 → 沿用现有 token 预估/报错逻辑（lightweight_story_generation.go:892 已有先例）

**验收**：
- 单测：给定一篇 2–3 段短小说 → 输出合法剧本 JSON（场景头/单元/角色完整）
- 手工样例：解析结果 json.Unmarshal 通过、必填字段齐全
- 现有 4 模式测试不回归

### 阶段 2：剧本拆分人物形象（人物库加固）

**目标**：人物字段结构化 + 跨集去重合并，防漂移。

**改动文件**：
- `lightweight_story_generation.go`：
  - `lightweightStoryCharacter` 新增可选字段：`personality []string`、`demeanor`（口头禅/习惯动作，可空）、`relations []{name,type}`、`first_seen`（首现章节/场景，可空）
  - 新增 `mergeLightweightStoryCharacters()`：先精确名合并，再「去掉空白/全半角/常见称谓（爷/娘/大人/小姐等）」的规范化归并；**只在新角色入库前做归并，不改已锁定的既有角色**
  - prompt 侧：`buildReadableNarrationRule` 之外新增一条"人物库字段填写规范"（性格 ≤3 条、relations 只写明确关系）
- 测试文件：`lightweight_story_generation_test.go` 增加别名合并用例

**关键决策**：
- 兼容：新字段全部可选，旧 JSON 缺字段不报错（沿用 `coerceJSONScalarToString` 的风格）
- 归并仅影响「本集新产出角色」，不动已锁定角色指纹（characters.go:1408 的锁定语义不变）
- 场景内出现但人物库没有的角色（幽灵角色）**只告警不失败**（阶段 3 再升级为硬校验）

**风险**：模糊归并误合并 → 只归并规范化后完全相等的名字（保守策略），不做语义相似匹配

**验收**：
- 单测：同名字不同写法（含空格/全半角/称谓）合并成功；不同角色不误并
- 生成样例含 personality / relations 字段且可解析

### 阶段 3：场景描述增强（场景资产结构化）

**目标**：scene 增加结构化场景资产，跨镜一致可复用。

**改动文件**：
- `lightweight_story_generation.go`：
  - `lightweightStoryScene` 新增可选字段：`location`（地点描述）、`time_of_day`（日/夜/晨/昏/黎明）、`scene_function`（建立/关系/行为/反应/信息揭露/收束）、`cast []string`（出场角色名）、`mood_arc`（情绪弧，如"平静→紧张"，可空）
  - prompt：4 种模式 systemPrompt 统一插入"场景资产填写规范"（复用现有 `buildVisibleSceneContinuityRule` 等辅助函数拼装）
  - 校验：`validateLightweightStoryResponse` 增加——若 `cast` 非空，成员必须存在于人物库（既有 + 本集新建），否则**告警**；阶段 4 再决定是否硬失败
- `scenes.go` 的场景概念图提示词构造处：若 scene 有 location/cast，注入到 Keyframe prompt 的场景描述段（增强场景一致性）

**关键决策**：
- 全部可选字段，旧产物零影响；h3_short 模式先强制要求 location/time_of_day/cast（它是新链路，可严格一点）
- `cast` 与人物库联动，为阶段 4 的幽灵角色硬校验铺路

**风险**：字段多了会挤占 token → 约束每个字段 ≤ 一句中文（prompt 里写明）

**验收**：
- h3_short 样例：每个 scene 含 location / time_of_day / cast / scene_function
- 幽灵角色告警日志出现且任务不失败
- 4 模式测试不回归

### 阶段 4：Schema 校验 + 自动修复（软降级）

**目标**：校验从硬失败升级为「分类 → 自动修复重试」。（对标 wswhhhc 的 3 次迭代）

**改动文件**：
- `lightweight_story_generation.go`：新增 `classifyStoryValidationError()` 把 `validateLightweightStoryResponse` 的错误分两类：
  - **可修复**（缺字段/数组空/时长越界）→ 携带错误说明重试 LLM 一次（最多 2 次），重试 prompt 附上错误清单与"必须修正"指令
  - **不可修复**（JSON 解析失败/结构级错误）→ 直接失败（保持现状）
- 重试走现有 `requestLightweightStoryOnce`，复用超时/重试骨架

**关键决策**：默认只重试 1 次（成本可控），可修复类再失败即报错；全部通过配置/常量开关

**验收**：
- 单测：构造缺字段响应 → 自动修复成功（用注入的 mock 验证重试路径）
- 手测：人为在返回 JSON 删掉某 scene 的 image_prompt → 系统自动补全一次后成功

### 阶段 5：端到端验证与文档

- **内容**：
  1. 整链路手测：长度 1–2 万字小说 → 阶段 1 转剧本 → h3_short 风格生成（含人物库/场景资产）→ 校验通过
  2. 更新本文档为「实施记录 + 已知限制」
  3. 相关前端字段若已支持展示（frontend 侧愿力不强则只标注，不改前端）
- **验收**：链路走通、文档更新、分支总结提交

---

## 5. 全程约束与工作方式

1. **测试先行**：每阶段先写/补测试，再改实现；改动后跑 `go test ./internal/api -count=1`
2. **小步提交**：每阶段一个 commit，message 遵循 conventional commits（`feat:` / `fix:` / `test:`），可独立 revert
3. **不破坏现有链路**：所有新字段可选；新能力走新文件/新接口；4 种既有模式行为不变量（有变化必须在本计划里注明）
4. **不引入新依赖**：继续用 LLM JSON 结构化输出 + 标准库 JSON 校验，不引向量库/图库（本轮边界）
5. **每次验收跑基线回归**：`go build ./...` + 现有测试 + 手工样例

---

## 6. 验收总览（最终完成标准）

| 编号 | 验收项 |
|------|--------|
| A | `go build ./...` 与 `go test ./internal/api -count=1` 全绿 |
| B | 小说原文可转换为合法结构化剧本初稿（阶段 1） |
| C | 人物库含性格/关系字段，跨别名合并生效且不误并（阶段 2） |
| D | scene 含地点/时间/出场角色/镜头功能，幽灵角色告警（阶段 3） |
| E | 可修复校验错误自动重试成功（阶段 4） |
| F | 端到端手测走通，文档更新（阶段 5） |

## 7. 明确的"不做"清单（防范围蔓延）

- ❌ 不做小说分章解析器（txt/epub 多格式）——输入端只收纯文本，分章交给 LLM
- ❌ 不做向量库/图数据库/RAG 长期记忆
- ❌ 不做多智能体（LangGraph 等）编排
- ❌ 不改前端（除非前端字段天然兼容才顺带标注）
- ❌ 不改已锁定角色指纹的入库语义（characters.go）
- ❌ 不做心理占比巡检、对白改写成稿、多格式导出