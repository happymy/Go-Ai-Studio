# 一站式生成物质量论证：对照开源一键短剧头部项目 + MiniMax H3 适配核验

> 结论先行：
> 1. **生成物对比**：我们的剧本质量闭环（P3 评分 + P4 修复）与跨镜连续性（位置台账/ending_state）**领先头部项目**；真正的缺陷集中在**资产体系（道具/服装非一等资产）、多 Agent 分工、事件图谱、镜内关键帧、长期记忆、prompt 可配置性** 六项。
> 2. **H3 适配**：prompt 内容层 base 模式（T2VA/I2VA/首尾帧）已按官方规范对齐 ≈95%；**R2V 参考生成未切换官方六段式模板（唯一实质缺口）**；执行形态为 ComfyUI 本地 GGUF（自定义节点语义，非官方 API）；**端到端未实测**。因此"完全适配"严格不成立。
> 3. 差距 C（H3-Context-IR）详见第五章。

---

## 1. 对比基准与范围

- 时间：2026-09-24。以下项目的代码/文档均为调研当日公开状态，未做逐行代码审计，仅以官方 README/文档及生成物字段描述为准。
- 头部开源一键短剧项目（按 Star/影响力）：
  - **Jellyfish**（Forget-C，FastAPI+React+MySQL+Redis，~5.5k★）：一致性资产管理 + 镜头准备/候选确认工作流
  - **Toonflow**（HBAI-Ltd，Node 桌面端，10k+★）：三层 Agent + 事件图谱 + ONNX 向量记忆 + Skill 文件化
  - **智剧通 ZJT**（jiyongdian/ZJT）：8 专家 Agent + ask_user 交互 + 多宫格锁脸 + 红果上线验证
  - **idea15/Jellyfish 系**（分镜级精细控制）：首/尾/关键帧独立提示词 + 多版本 + ControlNet + 智能对口型
- 对比维度：**生成物本身**（剧本、角色、分镜的字段粒度、完整性、一致性与下游可用性），不含 UI/部署/计费等工程面（另有说明的除外）。

---

## 2. 我们已具备且不输头部的生成物能力

| 能力 | 我们的实现（文件） | 头部项目对照 |
|---|---|---|
| 剧本质量闭环 | P3 量化评分（结构/格式/内容 40/30/30，`lightweight_story_quality.go`）+ P4 带原文语境的修复重试（`lightweight_story_auto_fix.go` `buildStoryFixContext`） | 强于 Toonflow 监督 Agent（他们有定性审阅、无确定性格点）；智剧通无评分体系 |
| 台词保真 | 台词零删改铁律 + 单句 ≤20 字 + 对白不吞并合并（机制 C） | 头部项目普遍无此硬约束 |
| 跨镜连续性 | 全局位置台账（机制 E）+ scene `ending_state` 承接 + 出场角色可见性检查（`checkCastPresenceInSceneBody`） | **领先**，Jellyfish/Toonflow 均无台账级连续性 |
| H3 prompt 原生适配 | 官方三段式结构、对白进主段、首尾帧接力、参考图注入（`lightweight_story_prompts_h3_short.go` + `h3_video_frame.go`） | 头部项目无一做到 H3 原生 prompt 级适配（详见第四章） |
| 异步任务/进度 | `task.GlobalTaskManager`（进度/取消/持久化） | 对标 Jellyfish 统一异步任务中心，不输 |
| 配音链路 | qwen_tts 音色克隆+合成 | 存在即达标（头部亦多走 TTS 供应商） |

---

## 3. 生成物缺陷清单（按严重度排序）

> 均为"生成物质量/下游可用性"视角；标注文件路径与落地成本，供后续计划引用。

### 缺陷 1：参考图没有"道具/服装"资产——画面一致性的下半身是空的（对照 Jellyfish）
- **头部做法**：Jellyfish 维护四类实体资产：角色 / 场景 / **道具** / **服装**，全部可跨镜复用、查重、镜头级链接；Toonflow 同样有"资产一致性保持"。
- **我们的现状**：只有 `Character`（+ `RefImage`）+ 场景图（`ref_image_0` 固定槽）。**服装与道具不是一等资产**：服装只能作为"临时状态"写死在 `image_prompt`，跨镜无资产台账；道具同理。
- **后果**：短剧里反复出现的信物、武器、标志性服装每镜重新描述 → **跨镜漂移**。这是可见一致性的最大缺口。
- **落地成本**：大（新表 + 抽取 + 跨镜注入 + 与场景图管线接线）。

### 缺陷 2：单 LLM 直出，无多 Agent 分工——剧本上限受一次输出制约（对照 Toonflow/智剧通）
- **头部做法**：Toonflow 决策层→执行层→监督层三层 Agent；智剧通 8 个专家 Agent 分工（编剧/角色/场景/情节分析/合规/分集/双设计），且 `ask_user` 与用户交互。
- **我们的现状**：单 LLM 一轮输出 + 规则校验 + 修复重试；**导演/摄影/美术/编剧职责全部压在一次生成里**。
- **后果**：万字小说转剧本时，单一上下文同时承担结构、台词、镜头、美术，长文本信息丢失概率高。
- **备注**：多 Agent 属架构级改造，且可能触碰"不引入新依赖"约束；`ask_user` 人机确认流项目已刻意不落地（不受影响）。

### 缺陷 3：无事件图谱——长文本改编的信息锚点缺失（对照 Toonflow）
- **头部做法**：Toonflow 自动提取原著章节事件→结构化存储→按事件图谱精准调用上下文，减少长文本信息丢失。这是其长篇小说赛道的核心卖点。
- **我们的现状**：P0 两段式大纲（情节主线 + 人物预表 + 场景大纲）+ breakdown 叙事节点清单（`lightweight_story_breakdown.go`）——已是轻量锚点，但**事件级因果/时间/人物关系未结构化**，续写与修复都检索不到"第 3 章的伏笔"。
- **后果**：长文本改编的"信息回滚"只能靠每轮重新读原文（`buildStoryFixContext` 切句注入），没有图谱级记忆。
- **落地成本**：大。

### 缺陷 4：分镜缺"镜内关键帧锚定 + 多版本"——长镜中间段漂移风险（对照 idea15/Jellyfish 系）
- **头部做法**：分镜有首/尾/**关键帧**三组独立提示词 + 多版本管理（同一镜出多版挑选，无需人审也得益）。
- **我们的现状**：一镜只有首帧图（`(input:image) First Frame`）+ 尾帧（=下一镜首帧，`resolveH3R2VLastFrameImage`）+ 三段式 video_prompt。**无镜内 mid-frame 锚点**：8~15 秒长镜中间画面全靠文本约束。
- **后果**：长镜中段漂移风险高于有关键帧的方案。
- **落地成本**：中（可把官方 FL2VA/L2VA 关键帧模式接入，作为镜内锚点）。

### 缺陷 5：无长期记忆检索——跨集连续性只靠"上一集"（对照 Toonflow ONNX 向量记忆）
- **头部做法**：Toonflow 本地 ONNX 向量检索跨会话记忆（短期消息/长期摘要/语义召回）。
- **我们的现状**：`PreviousEpisodeContextJSON` 只注入上一集 + `ending_state` 承接。50 集剧本第 30 集伏笔在第 45 集无法检索。
- **后果**：长篇剧集的跨集一致性停留在"单层最近记忆"。
- **落地成本**：大，且向量检索明确需要引入依赖（可能触碰约束，列为 P4 待议）。

### 缺陷 6：prompt 全部硬编码在 Go 里——调优成本高（对照 Toonflow Skill 文件化）
- **头部做法**：Toonflow 把 ScriptAgent/ProductionAgent 提示词外置为 Markdown Skill 文件，在线编辑即时生效。
- **我们的现状**：52 条硬约束 + 全部机制提示词在 `.go` 常量里；**改一句提示词=改代码+编译+重启**。
- **后果**：迭代成本高一个量级，直接拖慢生成物质量的持续优化速度。
- **落地成本**：中（提示词外置 + 热加载，不引入依赖）。

---

## 4. MiniMax H3 适配核验表

> 官方规范来源：MiniMax 开放平台（`platform.minimaxi.com/docs`）＋ MiniMax-AI/MiniMax-H3 官方 skill（`h3-prompt-writing`）。
> 我方实现：`lightweight_story_prompts_h3_short.go`（prompt 约束）、`h3_video_frame.go`、`video_segments.go`（执行层）、`workflows/minimax_h3_*-gguf-api.json`（ComfyUI 工作流）。

### 4.1 已对标项（≈95% 覆盖，证据硬）

| 官方规范 | 我方实现 | 判定 |
|---|---|---|
| 三段式主文：`integrated_multimodal_description`（时间轴连续叙事，`At 00:00.000` 起点）→ `overall_soundscape`（环境/动作声，不含台词）→ `non_diegetic_music`（配乐，禁抽象情绪词） | h3_short 硬约束强制三段式、段序、时间码起点，与官方 base-en.txt 逐条一致 | ✅ |
| 台词/对白/画内音乐归主段，不进 soundscape | 约束："台词直接写进 integrated_multimodal_description 对应动作时刻…不要拆到 soundscape" | ✅ |
| 参考图标签 `<Picture N>` | 机制 A `@图N` → 桥接 `<Picture N+1>`（`planH3CharacterRefInjections`），槽位与官方参考图语义一致 | ✅ |
| 首尾帧：官方 I2VA/FL2VA first_frame+last_frame | `resolveH3R2VLastFrameImage`：下一镜首帧图 = 本镜尾帧，实现 H3 多镜自动接力 | ✅ |
| 参考图 ≤9 张 | `ref_images.ref_image_0` 场景 + `ref_image_1..8` 角色 = 9 槽（`h3RefImageSlotLimit=9`） | ✅ |
| 时长 4~15s（H3） | prompt 硬约束下限 4/上限 15；`injectH3Duration` 注入 `(length-1)/fps` 秒 | ✅ |
| 分辨率 768P/2K | `normalizeH3VideoFrameSize` 980000px 上限 + 16 对齐 | ✅ |
| H3 原生音频输出（VAE Audio） | R2V 工作流含 `VAEDecodeAudio`/`LoadAudio`/`ref_videos`/`ref_audios` 通道 | ✅ 通道存在 |
| 禁 ASCII 双引号（防 prompt 结构破坏） | 约束 9.5：正文仅允许中文直角引号「」 | ✅ |

### 4.2 未完全适配项（诚实清单）

| # | 差距 | 说明 | 严重度 |
|---|---|---|---|
| **A** | **R2V 官方六段式未用** | 官方 full-reference 模式有专用模板：`subject_definitions / summary / retention_analysis / detailed_description / overall_soundscape / non_diegetic_music`；其中 `retention_analysis`（保留性分析）控制参考图信息留存度。我们 R2V 场景图生成仍走 base 三段式 → **提示词模板与官方 ref 模式错配**，可能是"角色参考图跟随不稳"的潜在根因 | 高（唯一实质缺口） |
| **B** | 执行形态非官方 API | 官方 API：`POST /v2/video_generation`（鉴权/计费/轮询）；约束：**first/last_frame 与 reference_\* 互斥、text ≤7000 字符**。我们走 ComfyUI 本地 GGUF（`UnetLoaderGGUF`+`MiniMaxH3ReferenceToVideo` 社区节点）：R2V 同时接入 First/Last Frame **和** ref_images——与官方互斥约束不一致（节点语义为社区自定义）；7000 字符预算未做 | 中（架构选择，非错误；但语义需实测确认） |
| **C** | 未用 H3-Context-IR | 官方"生成视频前先增强提示词"专用任务；我们用业务 LLM 直出 prompt 替代。**详见第五章** | 中（可选优化） |
| **D** | P6 端到端未实测 | 以上"已对标"均为静态代码比对；参考图跟随率、长镜漂移、口型同步、首尾帧接力质量**从未在真实 LLM+H3 上验证** | 高（证据缺位） |

### 4.3 总判定

> **prompt 内容层：base 模式已按官方规范对齐 ≈95%；R2V 参考生成未切换官方六段式模板（唯一实质缺口）；执行形态为 ComfyUI 本地 GGUF（自定义节点语义），非官方 API；端到端未实测。因此"已完全适配"严格不成立，但核心链路已对齐官方规范，补齐项明确、工程量小。**

---

## 5. 差距 C 详细解释：H3-Context-IR（官方提示词增强前置任务）

### 5.1 它是什么（官方语义）

H3-Context-IR 是 MiniMax 视频生成服务提供的**独立异步任务**，接口 `POST /v2/h3_context_ir`，官方原文：

> "深度理解多模态上下文，并生成结构化、语义更丰富的视频提示词。"
> "对文本、图像、音频和视频等多模态上下文进行深度理解，分析素材之间以及素材与目标生成结果之间的关系，并进行复杂逻辑推理。系统会将理解结果转换为结构化表达，在尽量保持用户原始意图的前提下丰富语义细节。"
> "⚠️ 本接口只返回增强后的视频提示词，不会创建视频生成任务。"

关键官方备注（直接关系到本项目定位）：

> **"H3-Context-IR 是一个复杂系统，暂不提供开源实现；本 API 既可用于验证 Full 2K-Workflow 的官方效果，也可集成到生产工作流中。"**

即：这是**专为「把旁白/需求 → 生产级 H3 prompt」训练的封闭系统，官方不给开源**。它充当两个角色：
1. **基准答案生成器**（用于验证官方 Full 2K 工作流效果）；
2. **生产流水线的一环**（先增强 prompt，再喂视频生成）。

### 5.2 调用契约（官方）

```json
POST /v2/h3_context_ir
{
  "model": "MiniMax-H3",
  "content": [
    { "type": "text", "text": "史诗级太空歌剧院线预告：女舰长独自站在巨大观景窗前……" },
    // 可加：{ "type": "image_url", "image_url": {"url": "…"}, "role": "first_frame" }
    // 可加：{ "type": "video_url", …role: "reference_video" } / { "type": "audio_url", …role: "reference_audio" }
  ],
  "duration": 5,
  "ratio": "16:9"
}
// → 返回 task_id；轮询查询接口，成功后 content.prompt 即增强后的三段式 H3 prompt
```

响应示例（官方文档原文，节选结构）：

```
integrated_multimodal_description: [Shot 1] Cinematic, wide shot with a slow push in
  on a female captain standing center frame with her back to the camera…
  [Shot 2] At 00:02.800, the camera cuts to a medium close-up… (镜头#/#时间码/动作链)
overall_soundscape: Deep, resonant low-frequency thrumming of ship engines…
non_diegetic_music: Symphonic orchestral score… (配器/速度/动态，无抽象情绪词)
```

注意：**Context-IR 的输出结构，恰好就是我们 h3_short 硬约束强制的那套三段式**——说明我们的体系方向与官方一致，差距在"产出者"与"输入模态"。

### 5.3 我们当前怎么做（替代方案）

我们的链路（`lightweight_story_generation.go` → `requestLightweightStoryOnce` → 业务 LLM）：

```
分镜需求（剧情/角色锚点/镜头语言卡）
  → 业务 LLM（用户配置的任意 provider：Kimi/Qwen/DeepSeek/Claude…）
  → 直接产出三段式 video_prompt（image_prompt 另出，供文生图/首帧用）
```

即：**我们让"通用语言模型"承担了 Context-IR 的职责**，用 52 条硬约束 + 机制 A~E 把它"按 H3 官方规范写 prompt"这件事显式化。

### 5.4 差距的本质（5 点）

| # | 官方 Context-IR | 我们（业务 LLM 直出） | 影响 |
|---|---|---|---|
| 1 | **专用模型**：为 H3 prompt 调优的封闭系统，"复杂系统，暂不提供开源实现" | 通用 LLM 的泛化能力，H3 prompt 只是其能力子集 | prompt 的镜头语言/语义丰富度上限不同 |
| 2 | **真·多模态输入**：可输入首帧图/参考图/参考视频/参考音频，并"分析素材之间及与目标结果的关系" | 业务 LLM **纯文本输入**：`ReferenceCharactersJSON` 只是角色外貌文字索引，LLM **看不到参考图本身** | 机制 A 的"参考图@图N"对 LLM 只是文本占位符，无法按真实画面构图/呼应 |
| 3 | **素材关系推理**：上下文多素材联合推理（尾帧↔首帧↔参考角色↔音频） | 每镜 prompt 由 LLM 独占生成，不具备跨素材关系分析能力 | 跨镜/跨素材的一致性推理弱一环 |
| 4 | 官方定位含"**验证官方效果的基准**" | 我们的 prompt 从未与官方基准对照过 | 缺一条「质量标尺」，P3 评分是对格式/结构评分，不评"与官方基准的差距" |
| 5 | 独立按量计费的官方 API（ComfyUI 本地链路无此依赖） | 零额外依赖、无忧免 | 若接入需新增官方 API 通路 + API Key + 计费 |

### 5.5 影响评估（诚实结论）

- **不是错误，是"替代方案换模型"**：我们让通用 LLM 在强约束下产出官方结构，避免了为每镜调用官方 Context-IR 的 API 成本与依赖。方向（三段式）与官方一致。
- **实打实的损失**：
  1. 机制 A 的参考图**对生成 prompt 的 LLM 是不可见的**——LLM 只能按文字锚点描述角色，写不出"参考图里角色的真实姿态/光线"级别的细节；官方 Context-IR 能直接看图。
  2. 没有"素材间关系推理"：官方能把「尾帧图+首帧图+参考角色+音频」联合推理，我们不行。
  3. 缺官方基准对照：无法回答"我们的 prompt 跟官方 Full 2K 工作流差多少"。
- **不建议立即接入的原因**：
  1. 架构冲突——我们刻意走 ComfyUI 本地 GGUF（无 API 依赖、无计费），Context-IR 只能云端调用，接入即新增一条官方付费通路；
  2. 约束——"不引入新依赖"（新增官方 API 客户端属新集成面）；
  3. 收益不确定——差距 D（未实测）尚未证明"业务 LLM 直出"在真实 H3 上确实劣于官方增强，应先跑 P6 手测获得基线，再决定是否值得接。

### 5.6 若未来接入（方案备查，不落地）

1. **模式一（增强，推荐）**：保留分镜体系，把每个 scene 的 video_prompt 作为 `text` 输入 Context-IR（可选附加首帧图/参考图），取回增强 prompt 替换 video_prompt。硬约束（台词零删改/位置台账）需在增强后做校验回滚。
2. **模式二（基准）**：仅用于 P6 手测对照——同一输入跑"业务 LLM 直出"与"Context-IR 增强"两版，量化差距后再决定是否投入。
3. **模式三（全托管）**：分镜 JSON → 每镜 Context-IR → 官方 API 生成视频，完全弃用 ComfyUI GGUF 链路（与项目本地化定位相悖，不推荐）。

---

## 6. 可落地改进项总表（供决策，均不破坏现有 4 模式）

| 优先级 | 项 | 对应 | 工作量 | 依赖 |
|---|---|---|---|---|
| **P1** | R2V 切换官方 Ref2VA 六段式模板（subject_definitions…retention_analysis…） | 差距 A | 中 | 无（改 prompt 模板 + 解析容错） |
| **P2** | 道具/服装资产化（新表 + 抽取 + 跨镜注入） | 缺陷 1 | 大 | 无（新表走 AutoMigrate） |
| **P2** | 长镜关键帧锚定（官方 FL2VA/L2VA 接入） | 缺陷 4 | 中 | 无 |
| **P3** | 事件图谱（章节事件结构化抽取） | 缺陷 3 | 大 | 无 |
| **P3** | prompt 外置 Markdown（Skill 化热编辑） | 缺陷 6 | 中 | 无 |
| **P4** | 跨集向量记忆 | 缺陷 5 | 大 | **需引入依赖（ONNX 类）——待议** |
| **P4** | 多 Agent 分工 | 缺陷 2 | 大 | 架构级，待议 |
| 待定 | H3-Context-IR 接入（先做基准对照） | 差距 C | 中 | **官方 API Key + 计费，待 P6 基线后议** |
| **P0** | P6 端到端手测（真实 LLM，双跑对照） | 差距 D | — | 真实环境，本会话不可执行 |

---

## 7. 证据来源

- MiniMax 开放平台文档索引：https://platform.minimaxi.com/docs/llms.txt
- 创建视频生成任务 V2：https://platform.minimaxi.com/docs/api-reference/video-generation-v2-create
- 创建 H3-Context-IR 任务：https://platform.minimaxi.com/docs/api-reference/video-generation-v2-h3-context-ir
- 视频生成指南（含规格表/Context-IR 说明）：https://platform.minimaxi.com/docs/guides/video-generation
- MiniMax-AI/MiniMax-H3 官方 skill（h3-prompt-writing，base/ref 模板）：https://github.com/MiniMax-AI/MiniMax-H3/blob/main/skills/h3-prompt-writing/SKILL.md
- Forget-C/Jellyfish README：https://github.com/Forget-C/Jellyfish
- HBAI-Ltd/Toonflow-app README：https://github.com/HBAI-Ltd/Toonflow-app
- jiyongdian/ZJT README：https://github.com/jiyongdian/ZJT