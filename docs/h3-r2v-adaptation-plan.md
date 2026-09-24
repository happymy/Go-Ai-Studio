# MiniMax H3 R2V 适配与参考图改造计划

目标：将轻量故事生成（wc-lightweight）从单帧 z-image + LTX2.3 全镜子视频（3–15s）改造为 MiniMax H3 R2V 的 5 秒镜头段 + first_frame/last_frame 无缝拼接，并支持多参考图驱动角色一致性。范围 = P0 + P1 + P2 全链路。

## 决策（用户已确认）

1. 实施范围：P0 + P1 + P2 全链路。
2. 参考图存储：采用项目现有方法 —— gorm model 字段 + 文件路径字符串（`models.Character.RefImage`/`UseRefImage` 已存在，models.go:228-229），参考图路径写入 DB。字符图片文件按项目现有约定存放。
3. 先落盘本计划到 `docs/`，再动代码。

## 现状事实（已核查）

- 三 prompt 模板硬编码视频模型 LTX2.3（`high_quality.go:102-113`）、首帧生图模型 z-image（85-91）/ krea2（`storyboard.go:30-40`）、24fps、单镜 3–15s（"严禁超过 15 秒"）。
- Phase 时间轴模板从 0.0s 起步，只描述全镜子时长（`high_quality.go:132-147`、`standard.go:158-161`）。
- 轻量生成 dispatch：`lightweight_story_generation.go:728-744`，三个 mode 走三个 prompt 文件，default = standard。
- 结构 `lightweightStoryCharacter`（generation.go:19）：Name/Gender/Age/Height/Era/Country/Appearance 纯文本，无图片字段。
- 上下文：`SceneImageWidth/Height` = `getConfiguredSceneImageSize()`（settings.go:202，默认 768×1344）；`SceneImageFrameType` = `describeFrameType(w,h)`（generation.go:221）；`FixedVideoFPS` = `defaultSegmentFPS`=24（video_segments.go:23）。
- `models.Character`：已有 `FaceFingerprint`(219)/`Fingerprint`(221)/`RefImage`(228)/`UseRefImage`(229)。
- `models.Shot`（Shot/Scene/Video 共享 `shots` 表，models.go:238/297/336）都有 `ImagePrompt`/`VideoPrompt`/`GeneratedImage`/`GeneratedVideo`/`VideoGeneratedWorkflow` 字段。
- 项目已有上片段尾帧→下片段首帧的视频节点先例：`WanFirstLastFrameToVideo`（store_visit_dish_generation.go:520）。
- ComfyUI 已内置 H3 R2V 工作流：`workflows/minimax_h3_i2v-gguf-api.json`（未跟踪），节点 133 `MiniMaxH3ImageToVideo` 同时接收 first_frame(114) 与 last_frame(141)。
- 目标视频归一化由 `video_segments.go` 承担（`defaultSegmentFPS`=24、`maxSingleSceneVideoLengthFrames`）。

## 实施步骤

### P0 — R2V 镜头规划提示词适配

- 三 prompt 模板：
  - 视频规格改为 H3 R2V 可生成时长（~5s/镜），移除 LTX2.3 / "严禁超过 15 秒" 表述，统一为"单个镜头约 5 秒"。
  - Phase 时间轴改为多镜段语义：每镜 0.0→5.0s，强调本镜首帧画面 = 上一镜尾帧画面（无缝衔接）。
  - 首帧生图规格表述保留（z-image/krea2），供 P1 用参考图 + 尾帧约束。
- `lightweight_story_generation.go`：新增 `buildHighQualityShotPlanningInstruction` 的 R2V 变体（如 `buildH3R2VShotPlanningInstruction`），注入：场景尺寸、帧率 24、镜头数上限（按 `maxSingleSceneVideoLengthFrames` 折算）、首尾帧衔接约束。
- `auto_generate_modes.go`：three-mode 判定中为 R2V 增加模式映射（hi 模式走 R2V 指令）。
- Phase JSON 输出结构不变（scene_id/duration_seconds/narration/dialogue/image_prompt/video_prompt，generation.go:584-594）——duration_seconds 落值约 5。

### P1 — 参考图资产与角色一致性注入

- 采用项目现有存储方法（gorm + 文件路径）：
  - 已在数据库中：`Character.RefImage`/`UseRefImage`（个人参考图）；场景参考图落 `Shot.GeneratedImage`（首帧产物，天然成为下一镜的 last_frame 输入）。
  - 渲染时从 DB 读参考图路径，注入 ComfyUI 工作流节点。
- 分支 B（已实现且更优）参考：`character_sheet`/`clue_sheet` 多参考图 + i2v 起始帧；`is_continuation=true` 跳过生图、取前驱尾帧。R2V 用 last_frame 原生承担该语义。
- 轻量角色图：`wc-lightweight` 角色表增加参考图维度（复用 `RefImage` 语义，DB 存路径文件）；`lightweightStoryCharacter` 增加图片标识字段（路径或 hash），prompt 中要求"保持该角色参考图外观"。

### P2 — R2V 渲染链路（CF + 拼接）

- 首帧图生成沿用现有场景图链路（`Shot.GeneratedImage`），后续镜首帧取前一镜 last_frame 输出。
- 视频节点：t2i 分镜图 → i2v first_frame=分镜图（默认，无 last_frame）；续镜 first_frame=前一镜 last_frame 输出、last_frame=本镜分镜图。
- 输出端点：`SaveVideo` → `video/MiniMax_H3`，落 `Shot.GeneratedVideo`。
- 拼接：`video_segments.go` 现有归一化（24fps、时长上限）基础上按 R2V 段序拼接（WanFirstLastFrameToVideo 已有先例可仿）。
- 无图参考（无 RefImage）时回退现有行为，不破坏旧流程。

## 风险与回退

- R2V 未跟踪 workflow 是唯一事实来源；如果 H3 i2v 节点不接受 last_frame（某些版本只 first_frame），则退回 branch（B）：`is_continuation=true` 跳过生图取前驱尾帧，或仅首帧 + 全拼接。
- 不改 Combinators 主仓库（PR 已关闭）与 videos/ 结构，避免回归。
- mock 层未覆盖的链路由既有高优故事链路兜底。

## 完成定义

- 三 prompt 模板无 LTX2.3 / "严禁超过 15 秒"，R2V 5 秒镜段表述生效。
- 轻量故事输出 duration_seconds ≈ 5，首尾帧衔接语义写入 prompt。
- 角色/场景参考图路径可经 DB 读取并注入工作流。
- 渲染按 R2V 段拼接出连续视频（可有 mock 短期顶替，标注 ponytail）。