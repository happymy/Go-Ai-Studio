# 计划：借助开源方法论提升 小说转剧本 · 人物形象 · 场景描述 的生成物质量

> 分支：`exp/novel-script-improvements`
> 基线：`fix/auto-series-episode-increment`（领先 main 42 个提交）
> 日期：2026-09-24（v2 重写：主轴从"结构改造"改为"生成物质量"）
> 状态：P0 已完成；P1–P5 分阶段实施，每阶段独立验收、可回退

---

## 1. 目标（北极星：生成物更好）

对 Go-Ai-Studio 轻量故事生成链路（`internal/api/lightweight_story_*`）的**产物质量**做可感知的提升。所谓"更好"，翻译成可验收的四条：

1. **人物不漂移**：同一角色跨集/跨镜，外观锚点、性格、关系保持一致，人物表无重复。
2. **场景有戏**：每场有明确"目标 → 冲突 → 转折"，不是平铺直叙；同地点跨镜场景描述一致。
3. **台词不丢、心理不堆**：原文引号台词逐条落位；narration/心理占比可控。
4. **输出结构稳**：JSON 结构漂移自动修复；质量有报告、分数可对比。

全程约束：**小步、可回退、不破坏现有四种生成模式（standard / high_quality / h3_short / storyboard）**，不引入向量库/图库/多智能体等重依赖。

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

### 2.2 关键数据结构（现状）

| 结构 | 字段 | 与"生成物质量"相关的短板 |
|------|------|--------------------------|
| `lightweightStoryCharacter` | name/gender/age/height/era/country/appearance | 无性格、无关系、无首现记录；跨集仅**精确同名**跳过，无别名归并；personality 靠 LLM 自觉 |
| `lightweightStoryScene` | scene_id/duration_seconds/narration/image_prompt/video_prompt | 无目标/冲突/转折/镜头功能/情绪弧；地点描述散在 image_prompt 里，无资产可复用 → 跨镜场景易不一致 |
| `episode_memory` | story_summary/ending_state/character_status/open_threads | 已有，可承载人物/情节记忆 |
| `validateLightweightStoryResponse` | 场景数/时长/必填/重名检查 | 硬失败无自动修复；**无台词覆盖、心理占比、场景三件套等质量检查** |

### 2.3 已确认基线

- 阶段 1 已完成（commit `7fbcba5`）：`runNovelToScript` 两段式流水线（大纲→逐场展开），产出含人物表（含 alias）+ 场景（location/time_of_day/cast/summary）+ 单元（action/dialogue/narration），17 个测试用例绿。
- `go test ./internal/api -count=1` 全绿；`go build ./...` 通过。
- 已知历史 vet 告警 `scenes.go:219`（自我赋值），非本轮范围。

---

## 3. 开源方法论 → 生成物质量提升映射（本计划的核心依据）

参考项目：JohnvenTom/Novel2Script-AI、baoerger/novel-to-script、1dashboard/ai-novel-screenplay、wswhhhc/novel2script、Axelxrd/ai-novel2script、slow2342/n2s、Toonflow-app、R2 论文。

| # | 开源方法（出处） | 它解决的质量问题 | 落地到本项目（阶段） |
|---|------------------|------------------|----------------------|
| 1 | **人物缓存锚定**（JohnvenTom：分片时注入人物缓存+上文摘要） | 人物形象漂移、人物名不一致 | P1：生成时把既有角色完整资产（appearance+personality）作为硬锚点注入 prompt；别名归并 |
| 2 | **跨章角色去重/别名合并**（baoerger：跨章角色别名合并） | 人物表重复、同一角色两个形象 | P1：`mergeLightweightStoryCharacters` 规范化归并（保守，只并完全等价写法） |
| 3 | **每场戏剧目标/冲突/转折**（Axelxrd：每场输出目标/冲突/转折；R2：scene 写作计划=storyline+goal；n2s：情绪弧） | 场景平铺直叙、镜头无功能 | P2：`lightweightStoryScene` 扩展 objective/conflict/turn/mood_arc |
| 4 | **story_bible 场景/地点资产**（Axelxrd：人物连续性+地点+道具/线索；n2s：locations 注册表） | 同地点跨镜描述不一致 | P2：场景资产（地点描述/时间/出场角色）结构化，image_prompt 的"场景"标签引用它 |
| 5 | **台词覆盖率检查**（Go-Ai-Studio storyboard 已有引号台词 checklist；baoerger：对白缺失统计） | 台词被吞、被改写 | P3：后置检查——引号台词逐条核对落位，缺失 → 告警（P4 自动修复） |
| 6 | **心理占比巡检**（JohnvenTom：>8% 触发二次精简） | 心理描写过多挤占画面/台词 | P3：narration 占比检测，超阈值 → 告警（先告警不自动改） |
| 7 | **幽灵角色检测**（baoerger：场景出现但不在角色表） | 出场角色未登记 → 后续无法生成 | P3：h3_short 链路的 scene 出场角色必须 ∈ 人物库，缺失 → 告警 |
| 8 | **Schema 校验 + 自动修复**（wswhhhc：校验失败最多 3 次迭代修复；JohnvenTom：YAML 三层防护） | JSON 结构漂移、字段缺失导致白跑 | P4：错误分类（可修复/不可修复），可修复类附错误清单重试 ≤2 次 |
| 9 | **质量评估报告**（1dashboard：三维度量化评分；Axelxrd：coverage_report） | 无法感知"是否变好" | P5：生成后输出 Markdown 质量报告（结构/格式/内容三维度），分数可对比 |

**明确不吸收**（防范围蔓延）：向量库/图库 RAG 长期记忆（Toonflow 事件图谱、aristotle GraphRAG）、多智能体编排（cdba1517）、多格式分章解析器、多格式导出、心理占比自动精简（只告警）、豆瓣/IMDb 等外部数据。

---

## 4. 分阶段实施（主轴：生成物质量）

> 每阶段：独立 commit → 测试 → 验收清单全部通过才进下一阶段。单阶段可独立 revert。

### ✅ P0：小说转剧本资产化（已完成，commit `7fbcba5` + `ab20e67`）

- 产出：小说原文 → 剧本初稿（人物表+场景大纲+场景单元），强校验保证结构可信。
- 质量收益：**输入资产可信**——后续所有质量改进都建立在结构化人物表/场景上。

### P1：人物形象稳定性（防漂移）——解决"同一角色两副脸"

**目标**：人物表可去重、可锚定、可继承。

改 `lightweight_story_generation.go`：
- `lightweightStoryCharacter` 扩展可选字段：`personality []string`（≤3 条）、`demeanor`（习惯动作/口头禅，可空）、`relations []{name,type}`（只写明确关系）、`first_seen`（首现场景，可空）
- 新增 `mergeLightweightStoryCharacters()`：先精确名合并，再"去空白/全半角/常见称谓"规范化归并；只作用于**本集新产出角色**，不动已锁定指纹（characters.go 锁定语义不变）
- prompt 注入增强：`existing_characters` 注入时带完整锚点（appearance+personality+relations），并要求"同名角色必须沿用锚点，禁止改写外观"

测试：`lightweight_story_generation_test.go` 增补别名归并用例（含不误并用例）。

**验收清单（可感知）**：
- [ ] 同角色"沈西风 / 沈 西 风 / 沈公子 / 沈西风大人"归并后人物表唯一
- [ ] 不同角色（沈西风 vs 沈西楼）不误并
- [ ] 生成样例 characters 含 personality/relations 且可解析
- [ ] 既有 4 模式测试不回归

### P2：场景戏剧化 + 场景资产（解决"平铺直叙、同地点两张脸"）

**目标**：每场有戏、场景描述可复用。

改 `lightweight_story_generation.go` + 4 个 prompt 文件：
- `lightweightStoryScene` 扩展可选字段：
  - `objective`：本场主角要什么（一句）
  - `conflict`：遇到的障碍/对抗（一句，可空——允许纯过渡场）
  - `turn`：本场结束时的状态变化/情绪转折（一句，可空）
  - `scene_function`：建立/关系/行为/反应/信息揭露/收束
  - `mood_arc`：情绪弧（"平静→紧张"）
  - `location`：场景资产（地点描述一句，独立于 image_prompt）
- 4 模式 systemPrompt 统一追加"场景写作卡"：每场必须给出 目标→冲突→转折 三件套（纯过渡场允许 conflict 为空但必须写 turn）；禁止在 image_prompt 的"场景"标签里写与 location 冲突的内容
- h3_short 模式强制要求 location/objective/turn（新链路可严格）；standard 等旧模式字段可选

测试：构造含三件套的样例解析成功；缺 objective 的 h3_short 样例报告警不失败。

**验收清单（可感知）**：
- [ ] 抽查 10 场，每场能写出"谁要什么 / 遇到什么 / 结果怎么变"
- [ ] 同地点跨镜（如"客栈大堂 内 夜"出现 3 次），image_prompt 场景标签关键元素（陈设/光线）一致
- [ ] h3_short 样例每场含 objective/conflict/turn

### P3：台词不丢 · 心理不堆 · 人不幽灵（后置质量检查层）

**目标**：生成后主动暴露质量问题，而非等人工发现。

新增 `internal/api/lightweight_story_quality.go`（纯函数，可单测）：
- `checkDialogueCoverage(plot string, scenes []lightweightStoryScene) []string`：从 plot 提取引号台词（复用 `extractStoryboardQuotedDialogues`），逐条核对是否出现在某 scene 的 video_prompt/narration/dialogue 内容中（模糊包含匹配），返回缺失清单
- `checkNarrationRatio(scenes) float64`：narration/video_prompt 中旁白类文本占全部镜头正文的字数比例，超阈值（默认 35%）进报告
- `checkGhostCharacters(scene 出场角色, 人物库) []string`：出场但未登记角色清单
- 报告结构 `lightweightStoryQualityReport`（structure/format/content 三个维度 + 评分 0-100 + 问题清单），评分规则参照 1dashboard：结构 40 / 格式 30 / 内容 30

接入：`runLightweightStoryGeneration` 生成成功后调用，产出报告写任务进度日志（P5 再做落地展示）。

测试：构造"缺台词/高旁白/幽灵角色"样例 → 报告正确识别。

**验收清单（可感知）**：
- [ ] 输入 5 句引号台词，报告能指出哪句没落位
- [ ] narration 占比超阈值被报告标注
- [ ] 幽灵角色被报告列出

### P4：自动修复（把"报错"变成"救回来"）

**目标**：可修复的质量/结构问题自动重试，减少人工返工。

改 `lightweight_story_generation.go`：
- `classifyStoryValidationError()`：把 `validateLightweightStoryResponse` 错误分两类
  - 可修复（缺字段/空数组/时长越界/台词缺失类）→ 携带错误清单重试一次（最多 2 次），重试 prompt 附"必须修正"
  - 不可修复（JSON 解析失败/结构级错乱）→ 保持失败
- 台词缺失自动修复：把 P3 的缺失清单注入重试 prompt（对标 wswhhhc 迭代修复）

测试：mock 注入"缺 image_prompt 的响应" → 断言走了重试路径；重试仍失败 → 报错。

**验收清单（可感知）**：
- [ ] 人为删掉某 scene 的 image_prompt → 自动补全一次成功
- [ ] 台词缺失 → 重试后补齐（mock 验证路径）

### P5：质量报告落地展示（让"更好"看得见）

**目标**：每次生成的报告可被用户看到、分数可对比。

- 在任务进度节点输出报告摘要（结构/格式/内容分 + 问题数）
- 报告 Markdown 存到任务结果字段（`task.Task.Result` 附上），不另开接口
- 人工对比：同一输入改 prompt 前后跑两次，看报告分数变化

**验收清单（可感知）**：
- [ ] 一次 h3_short 生成结束后，进度日志出现三维度分数
- [ ] 修复前/后两次运行的分数可对比且修复后更高

### P6：端到端手测 + 文档收尾

- 手测：万字小说 → P0 转剧本 → h3_short 链路（P1–P5 全开）→ 质量报告
- 更新本文档为实施记录 + 已知限制、验收复核

---

## 5. 全程约束与工作方式

1. **测试先行**：每阶段先补测试再改实现；每次跑 `go test ./internal/api -count=1`
2. **小步提交**：每阶段一个 commit（conventional commits），可独立 revert
3. **不破坏现有链路**：新字段全部可选；新能力走新文件/新接口；4 种既有模式行为不变（有变必在计划注明）
4. **不引入新依赖**：继续 LLM JSON 结构化输出 + 标准库校验
5. **质量导向验收**：每阶段验收都是"生成物可感知的变好"清单，不搞纯代码指标

---

## 6. 完成标准（最终）

| # | 完成项 |
|---|--------|
| A | `go build ./...` + `go test ./internal/api -count=1` 全绿 |
| B | 小说原文 → 结构化剧本初稿（P0 ✅） |
| C | 人物表可归并可锚定，跨集同一形象（P1） |
| D | 每场有目标/冲突/转折，同地点跨镜一致（P2） |
| E | 台词/心理/幽灵角色三类质量检查出报告（P3） |
| F | 可修复问题自动重试成功（P4） |
| G | 质量报告落地可对比（P5） |

## 7. 明确"不做"清单

- ❌ 分章解析器（txt/docx/epub 多格式）——输入端只收纯文本
- ❌ 向量库 / 图数据库 / RAG 长期记忆 / 多智能体编排
- ❌ 心理占比自动精简（只告警）、台词自动改写成稿、多格式导出
- ❌ 前端改动（报告先走任务日志/结果字段）
- ❌ 改已锁定角色指纹的入库语义（characters.go）
- ❌ 豆瓣/外部数据库、模型训练、微调