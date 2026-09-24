# 一键剧集 / 角色管理功能补全计划

> 背景：第二集创建死锁已修复（`e21daf7`）。本计划针对用户的三个痛点：①跨集重名角色无法跳过（硬失败）；②剧集管理 UI 缺失（插入/删除/重新生成）；③角色管理缺失（锁定/解锁/修改/合并）。目标：先出方案确认，再最小 diff 实施。

---

## 一、现状盘点（已存在的接口与 UI）

### 后端已具备
| 能力 | 接口 | 说明 |
|---|---|---|
| 一键生成故事 | `POST /projects/:id/auto-generate` | 小说/剧本 → characters + scenes（按集） |
| 继续失败草稿 | `POST .../auto-generate-draft/continue` | 续传失败任务 |
| 草稿管理 | GET/PUT/DELETE `/projects/:id/auto-generate-draft` | 保存/更新/删除草稿 |
| 剧情 JSON 导入/导出 | `POST .../import-story-json` / `export-story-json` | 外部严格 JSON 入库/导出 |
| 单集重置资产 | `POST /projects/:id/episodes/reset-assets` | 清该集角色图/场景图/视频 + 清 anchor memory |
| 单集删除 | `POST /projects/:id/episodes/delete` | 删该集全部 shots + 孤儿角色 + 资产文件（第 1 集禁删） |
| 场景管理 | GET/POST/PUT/DELETE `/scenes` 系列 + generate-image / reset-image / repair-prompts / batch-generate | shots 表即场景 |
| 角色管理 | GET `projects/:id/characters`；POST/PUT/DELETE `/characters/:charId`；generate-image / reset-image / auto-generate-prompt / batch-generate | `IsLocked` 字段已存在 |
| 集数聚合视图 | `GET /projects/:id/scenes?summary=episodes` | 前端 ProjectDetail 已按集分页浏览场景 |

### 前端已具备
- **AutoSeries.tsx**（一键剧集页）：项目选择、生成模式、标签增强、剧情输入、集数指定、生成/继续、任务流、JSON 导入导出。**没有任何剧集列表与管理操作**。
- **ProjectDetail.tsx**（剧情管理页）：按集分组的场景浏览 + 场景级生成/重置/删除 + 角色浏览（随场景联查）。

### 关键文件坐标
- `internal/api/lightweight_story_generation.go`：生成主链路
- `internal/api/episode_management.go`：`collectEpisodeScopedData`(L31) / `EpisodeResetAssets`(L80) / `DeleteEpisode`(L198)
- `internal/api/characters.go`：`UpdateCharacter`(L95) / `DeleteCharacter`(L141)
- `internal/models/models.go`：`Character`(L211) / `EpisodeMemory`(L174) / `EpisodeEditorialGuide`(L186) / `ProjectAnchorMemory`(L197) / `Shot`(L241)

---

## 二、问题清单与差距分析

### 问题 1：跨集重名角色会导致整集生成硬失败（用户说的"重名角色无法跳过"）

**根因**（同名字符 = 同一角色，但当前代码一律当作错误）：
1. `loadExistingStoryCharacters`（`lightweight_story_generation.go:442`）：只要 DB 里出现两个同名角色记录 → `duplicate existing character name` → 读取 `existing_characters` 直接失败 → 生成中断。
2. `validateLightweightStoryResponse`（同上 L1681）：LLM 在 `characters` 里返回了已存在的角色名 → `existing character %s must not be returned in characters` → 硬失败。
3. 与 `persistLightweightStoryPayload`（L1841 `existingByName` 同名跳过）的语义**自相矛盾**：入库时已能容忍同名跳过，校验却先一步把整个生成杀掉。

**期望行为**：模型把已有锁定角色放进 `characters` 时，应**静默丢弃该条**（或降级为 warning 记录），继续正常入库；DB 存在重名记录时按名字合并/取最旧，而不是中断。

**修改方向**：
- 校验层：命中 `existingByName` 的角色条目从 `payload.Characters` 过滤掉（不报错），其余校验照旧。
- 读取层：`loadExistingStoryCharacters` 重名时保留首个、跳过后续并记 warning（DB 里历史脏数据不至于卡死生成）。
- 配套：提供「角色重命名/合并」能力（见问题 3），让用户能主动消除脏重名。

### 问题 2：剧集管理 UI 缺失（插入 / 删除 / 重新生成 / 重置）

**现状**：后端已有 `reset-assets`、`delete`；前端无任何入口。剧集列表仅能靠 ProjectDetail 的 summary=episodes 间接浏览。无「插入新集并重排后续集号」「单集重新生成」能力。

**目标（最小闭环）**：
1. **剧集列表**：AutoSeries 或 ProjectDetail 顶部展示该项目的剧集清单（编号、各集场景数/进度、锁定角色数），一键定位。
2. **删除整集**：调用已有 `episodes/delete`（前端补确认框 + 进度；第 1 集禁删）。
3. **重置资产**：调用已有 `episodes/reset-assets`（清该集图/视频，剧本文本保留）。
4. **重新生成单集**：组合 = 重置第 N 集资产 → 用该集剧情单独跑 `auto-generate`（后端只需在 `AutoGenerateProject` 上支持"指定集数覆盖"，现已支持指定 episode；重置保证不撞冲突检查）。
5. **插入剧集**（新增后端）：在第 N 集后插入新集，后续所有 `shots.episode` 自 N+1 起 +1；同步 `episode_memory` / `episode_editorial_guide`（如有） / `project_anchor_memory`（episode_refs 重写） / `auto_generate_draft.episode`（若存在）。**需先确认是否需要**——若用户主要是"补拍/续写"，删除+重生成已够，插入（重编号）工作量大、风险高（涉及锚点记忆重写），按 YAGNI 先不做，仅列入可选。

### 问题 3：角色管理缺失（锁定 / 解锁 / 修改 / 合并）

**现状**：`Character.IsLocked`、`UpdateCharacter`（可改 Name/Appearance/IsLocked 等，**无锁定门卫、无重名冲突处理**）都已有，但前端 ProjectDetail 只能随场景看到角色，**没有角色独立管理入口**。

**目标**：
1. **角色列表管理面板**：按项目列出全部角色（名称、性别/年龄/外形摘要、锁定状态、生成状态、所属集中出现次数）。
2. **锁定/解锁**：一键切换 `IsLocked`。**语义约定**：解锁后允许编辑（改名/改外形/prompt），保存重新锁定。
3. **编辑角色**：复用 `UpdateCharacter`；改名需保证项目内唯一（后端校验），且要处理——改名后，历史场景 `image_prompt` 文本里的旧名字不会自动跟随（提示用户旧镜头需重新生成或手动修 prompt；或提供"批量替换该角色名于该集 prompt 文本"的高级能力，可选）。
4. **删除/合并**：删除已有接口；"合并重名角色"（把几条同名记录合并成一条并迁移 `shot_characters` 关联 + 保留资产）作为可选增强，用于清理问题 1 产生的历史脏数据。

### 问题 4：小说转剧本及其管理的其余缺口（补充项）

- **每集状态视图**：一键剧集产出后，缺乏"哪些集已完成/草稿/进行中、各集首尾镜进度"的总览（ProjectDetail 有按集分页，但无状态徽标与一键跳转 AutoSeries 续写该集）。
- **导入 JSON 与既有锁定角色的冲突处理**：目前仅靠提示"characters 只能放新角色"（AutoSeries 固定要求第 4 条），导入侧无同名去重——导入包含既有角色名时应**自动跳过而非报错**（与问题 1 同源），避免导入即失败。
- **重新生成入口聚合**：把"重置该集资产 → 填充文本 → 重新生成"收拢成一个按钮，避免用户手动分步。

---

## 三、分阶段实施计划

### 阶段 1：跨集重名容忍（后端最小修复）
- `validateLightweightStoryResponse`：命中既有角色名 → 过滤该条，不报错（warning 日志）。
- `loadExistingStoryCharacters`：重名记录去重容忍（保留首条）。
- **验证**：构造两集生成，第二集剧情含与第一集同名角色，确认能正常落库且不创建重复角色记录。

### 阶段 2：角色管理面板（前端 + 少量后端）
- 后端：`UpdateCharacter` 加项目内重名校验（改名时）；其余复用。
- 前端：ProjectDetail 增加「角色管理」面板（列表 / 锁定切换 / 编辑 / 删除），复用已有角色接口。

### 阶段 3：剧集管理 UI（前端为主）
- AutoSeries 或 ProjectDetail 增加：剧集列表（编号/状态/场景数）→ 删除 / 重置资产 / 重新生成。
- "重新生成"按钮 = 后端确认的 `reset-assets` + 指定 episode 重新 `auto-generate` 组合流。
- 合并重复角色的可选项（迁移 shot_characters），清理问题 1 脏数据。

### 阶段 4（可选，需确认再做）
- 剧集插入 + 全链重编号（`shots` / `episode_memory` / `episode_editorial_guide` / `project_anchor_memory`）。
- 角色改名后的历史 prompt 批量替换。
- 每集状态徽标与"续写该集"快捷入口。

---

## 四、待确认问题（影响实施范围）

1. 阶段 3 的剧集管理入口放哪个页面？建议 **AutoSeries 页**（与生成/继续同处，方便"删除→重新生成"闭环），ProjectDetail 保持场景浏览。
2. 阶段 4 的"插入剧集"是否本期需要？（重编号影响面大，plan 倾向先不做。）
3. 角色改名后，历史镜头提示词里的旧名是否需要批量替换，还是仅提示手动处理？
4. 角色管理面板是否需要支持"合并同名角色"（迁移关联），还是先只做删除/编辑？