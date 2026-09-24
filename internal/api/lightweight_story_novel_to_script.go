package api

import (
	"encoding/json"
	"fmt"
	"strings"

	"kt-ai-studio/internal/models"
	"kt-ai-studio/internal/task"
)

// lightweightNovelScriptUnit 剧本初稿中的剧情单元。
// UnitType 取值：action（动作/画面）、dialogue（对白）、narration（旁白/画外音）。
// dialogue 与 action 必须带 Character；narration 的 Character 必须为空。
type lightweightNovelScriptUnit struct {
	UnitType  string `json:"unit_type"`
	Character string `json:"character,omitempty"`
	Content   string `json:"content"`
}

// lightweightNovelScriptScene 剧本初稿中的一场。
// Location 为完整场景头（地点 + 内/外 + 日/夜），TimeOfDay 为独立时间字段，避免 LLM 把
// 时间混在地点里导致后续场景一致性难以校验。
type lightweightNovelScriptScene struct {
	SceneID   int                          `json:"scene_id"`
	Location  string                       `json:"location"`
	TimeOfDay string                       `json:"time_of_day,omitempty"`
	Cast      []string                     `json:"cast"`
	Summary   string                       `json:"summary"`
	Units     []lightweightNovelScriptUnit `json:"units"`
}

// lightweightNovelScriptCharacter 剧本初稿人物表条目。
// Alias 收纳别称/称呼，供阶段 2 的跨集归并使用。
type lightweightNovelScriptCharacter struct {
	Name       string   `json:"name"`
	Gender     string   `json:"gender,omitempty"`
	Age        string   `json:"age,omitempty"`
	Appearance string   `json:"appearance,omitempty"`
	Role       string   `json:"role,omitempty"`
	Alias      []string `json:"alias,omitempty"`
}

// lightweightNovelScriptResult 小说转剧本的最终产物。
type lightweightNovelScriptResult struct {
	NovelTitle  string                            `json:"novel_title,omitempty"`
	TotalScenes int                               `json:"total_scenes"`
	Characters  []lightweightNovelScriptCharacter `json:"characters"`
	Scenes      []lightweightNovelScriptScene     `json:"scenes"`
}

// lightweightNovelOutlineScenePlan 第一段摘要产生的场景大纲条目，作为第二段逐场展开的锚点。
type lightweightNovelOutlineScenePlan struct {
	SceneID   int      `json:"scene_id"`
	Location  string   `json:"location"`
	TimeOfDay string   `json:"time_of_day,omitempty"`
	Cast      []string `json:"cast"`
	CoreEvent string   `json:"core_event"`
}

// lightweightNovelOutline 第一段 LLM 产物：情节主线 + 人物预表 + 场景大纲。
type lightweightNovelOutline struct {
	PlotLine   string                             `json:"plot_line"`
	Characters []lightweightNovelScriptCharacter  `json:"characters"`
	ScenePlan  []lightweightNovelOutlineScenePlan `json:"scene_plan"`
}

// buildNovelToScriptOutlinePrompts 构建第一段（小说 → 大纲）的提示词。
func buildNovelToScriptOutlinePrompts(title string, novelText string) (string, string, error) {
	if strings.TrimSpace(novelText) == "" {
		return "", "", fmt.Errorf("novel text is required")
	}
	systemPrompt := `你是一位专业小说改编编剧。你的第一段任务：通读小说原文，输出一份改编大纲 JSON。
大纲包含三个部分：情节主线（plot_line）、人物预表（characters）、场景大纲（scene_plan）。

要求：
1. plot_line：用 3 到 6 句简体中文概括完整故事主线，包含起因、推进、转折、结局，禁止只写开头。
2. characters：列出全部对剧情有影响的人物（含出场一次但推动情节的角色），不遗漏、不虚构。每个角色的字段：
   - name：角色在小说中的主名，必须是简体中文原文使用的名字
   - gender：男性 / 女性 / 其他，省略号或未知写"未知"
   - age：明确年龄段或年龄，不要写"20岁出头"这类模糊说法，写成如 24、30 多岁、少年
   - appearance：一句话 20 字以内的永久外表锚点（身高体态、发色发型、标志性特征），不写服装道具
   - role：主角 / 重要配角 / 反派 / 配角 / 龙套
   - alias：该角色在小说中的其他称呼/别称数组，没有则返回空数组
3. scene_plan：按事件真实发生顺序列出全部场景（场景 = 同一空间同一连续时间的一段剧情）。每个场景：
   - scene_id：从 1 开始连续递增
   - location：完整场景头，格式"地点 + 内/外 + 日/夜/晨/昏"，例如"客栈大堂 内 夜"
   - time_of_day：日 / 夜 / 晨 / 昏 / 黎明 / 黄昏，与 location 中写的时间保持一致
   - cast：本场实际出场且对剧情有作用的角色名数组，成员必须全部来自 characters 表
   - core_event：一句话概括本场核心事件，供后续逐场展开，禁止写"略""同上"
4. cast 中的每个名字必须能在 characters 表中找到；不要用别名代替主名。
5. 只返回一个 JSON 对象，禁止输出 JSON 之外的任何解释、标题、注释或代码块标记。

返回格式（仅此结构）：
{
  "plot_line": "",
  "characters": [
    {"name": "", "gender": "", "age": "", "appearance": "", "role": "", "alias": []}
  ],
  "scene_plan": [
    {"scene_id": 1, "location": "", "time_of_day": "", "cast": [], "core_event": ""}
  ]
}`

	userPrompt := fmt.Sprintf(`请依据以下小说原文，生成改编大纲 JSON。

小说标题：%s

小说原文：
%s

请输出大纲 JSON。`,
		strings.TrimSpace(title),
		strings.TrimSpace(novelText),
	)

	return systemPrompt, userPrompt, nil
}

// buildNovelToScriptScenePrompts 构建第二段（大纲 + 原文 → 逐场剧本）的提示词。
// outlineJSON 为第一段产物序列化后的 JSON 文本。
func buildNovelToScriptScenePrompts(title string, novelText string, outlineJSON string) (string, string, error) {
	if strings.TrimSpace(novelText) == "" {
		return "", "", fmt.Errorf("novel text is required")
	}
	if strings.TrimSpace(outlineJSON) == "" {
		return "", "", fmt.Errorf("outline is required")
	}
	systemPrompt := `你是一位专业小说改编编剧。你的第二段任务：严格依据给定的改编大纲与小说原文，把全部场景逐场展开为可拍摄的剧本初稿 JSON。

剧本单元（unit）的 type 只允许三种：
- action：画面与动作描写，必须带 character（该动作的发起者）
- dialogue：对白，必须带 character（说话者）与 content（原话，尽量保留小说中的原句，不要改写缩写）
- narration：旁白/画外音，不得带 character

每场 scene 的要求：
1. scene_id、location、time_of_day、cast、summary 必须与大纲对应场景保持一致；cast 是本场真实出场角色，禁止多写无关角色，也禁止漏掉大纲 cast 里的角色。
2. summary：一句话概括本场剧情推进或信息落点，禁止文学化空话。
3. units：按剧情时间顺序排列本场全部单元；动作、对白、旁白混合编排，一个连续动作不要切碎成多个 action，一句对白不要拆成多句。
4. dialogue 的说话者必须在本场 cast 中；action 的发起者也必须在本场 cast 中；narration 不带 character。
5. 小说中的心理描写优先改写为 narration 或通过动作/表情外化，禁止大段内心独白塞进 dialogue。
6. 只返回一个 JSON 对象，禁止输出 JSON 之外的任何解释、标题、注释或代码块标记。

返回格式（仅此结构）：
{
  "novel_title": "",
  "total_scenes": 1,
  "characters": [
    {"name": "", "gender": "", "age": "", "appearance": "", "role": "", "alias": []}
  ],
  "scenes": [
    {
      "scene_id": 1,
      "location": "",
      "time_of_day": "",
      "cast": [],
      "summary": "",
      "units": [
        {"unit_type": "action", "character": "", "content": ""},
        {"unit_type": "dialogue", "character": "", "content": ""},
        {"unit_type": "narration", "content": ""}
      ]
    }
  ]
}`

	userPrompt := fmt.Sprintf(`请依据以下大纲和小说原文，逐场展开生成剧本初稿 JSON。

小说标题：%s

改编大纲（场景数量、场景头的硬约束，必须以它为准）：
%s

小说原文：
%s

请输出剧本初稿 JSON。total_scenes 必须等于大纲 scene_plan 的场景数量。`,
		strings.TrimSpace(title),
		strings.TrimSpace(outlineJSON),
		strings.TrimSpace(novelText),
	)

	return systemPrompt, userPrompt, nil
}

// parseNovelToScriptOutline 解析并校验第一段（大纲）产物。
func parseNovelToScriptOutline(raw string) (*lightweightNovelOutline, error) {
	trimmed := strings.TrimSpace(cleanupLLMJSON(raw))
	if trimmed == "" {
		return nil, fmt.Errorf("empty llm response")
	}
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return nil, fmt.Errorf("llm response must be JSON object only")
	}

	var payload lightweightNovelOutline
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil, fmt.Errorf("invalid json: %v", err)
	}
	payload.PlotLine = strings.TrimSpace(payload.PlotLine)
	if payload.PlotLine == "" {
		return nil, fmt.Errorf("plot_line is required")
	}
	if len(payload.Characters) == 0 {
		return nil, fmt.Errorf("characters must not be empty")
	}

	charNames := make(map[string]struct{}, len(payload.Characters))
	for i := range payload.Characters {
		ch := &payload.Characters[i]
		ch.Name = strings.TrimSpace(ch.Name)
		ch.Gender = strings.TrimSpace(ch.Gender)
		ch.Age = strings.TrimSpace(ch.Age)
		ch.Appearance = strings.TrimSpace(ch.Appearance)
		ch.Role = strings.TrimSpace(ch.Role)
		if ch.Name == "" {
			return nil, fmt.Errorf("character %d name is required", i+1)
		}
		if _, dup := charNames[ch.Name]; dup {
			return nil, fmt.Errorf("duplicate character name: %s", ch.Name)
		}
		charNames[ch.Name] = struct{}{}
		if ch.Alias == nil {
			ch.Alias = []string{}
		}
	}

	if len(payload.ScenePlan) == 0 {
		return nil, fmt.Errorf("scene_plan must not be empty")
	}
	seenSceneIDs := make(map[int]struct{}, len(payload.ScenePlan))
	for i := range payload.ScenePlan {
		sp := &payload.ScenePlan[i]
		sp.Location = strings.TrimSpace(sp.Location)
		sp.TimeOfDay = strings.TrimSpace(sp.TimeOfDay)
		sp.CoreEvent = strings.TrimSpace(sp.CoreEvent)
		if sp.SceneID <= 0 {
			return nil, fmt.Errorf("scene_plan %d scene_id must be greater than 0", i+1)
		}
		if _, dup := seenSceneIDs[sp.SceneID]; dup {
			return nil, fmt.Errorf("scene_plan scene_id %d is duplicated", sp.SceneID)
		}
		seenSceneIDs[sp.SceneID] = struct{}{}
		if sp.Location == "" {
			return nil, fmt.Errorf("scene_plan scene %d location is required", sp.SceneID)
		}
		if sp.CoreEvent == "" {
			return nil, fmt.Errorf("scene_plan scene %d core_event is required", sp.SceneID)
		}
		if sp.Cast == nil {
			sp.Cast = []string{}
		}
		for _, member := range sp.Cast {
			member = strings.TrimSpace(member)
			if _, ok := charNames[member]; !ok {
				return nil, fmt.Errorf("scene_plan scene %d cast member %q not found in characters", sp.SceneID, member)
			}
		}
	}

	return &payload, nil
}

// parseNovelToScript 解析并校验第二段（剧本初稿）产物。
func parseNovelToScript(raw string) (*lightweightNovelScriptResult, error) {
	trimmed := strings.TrimSpace(cleanupLLMJSON(raw))
	if trimmed == "" {
		return nil, fmt.Errorf("empty llm response")
	}
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return nil, fmt.Errorf("llm response must be JSON object only")
	}

	var payload lightweightNovelScriptResult
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil, fmt.Errorf("invalid json: %v", err)
	}
	payload.NovelTitle = strings.TrimSpace(payload.NovelTitle)

	if len(payload.Scenes) == 0 {
		return nil, fmt.Errorf("scenes must not be empty")
	}
	if payload.TotalScenes == 0 {
		payload.TotalScenes = len(payload.Scenes)
	}
	if payload.TotalScenes != len(payload.Scenes) {
		return nil, fmt.Errorf("total_scenes %d does not match scenes count %d", payload.TotalScenes, len(payload.Scenes))
	}

	// 人物表校验：名字唯一。
	charNames := make(map[string]struct{}, len(payload.Characters))
	for i := range payload.Characters {
		ch := &payload.Characters[i]
		ch.Name = strings.TrimSpace(ch.Name)
		if ch.Name == "" {
			return nil, fmt.Errorf("character %d name is required", i+1)
		}
		if _, dup := charNames[ch.Name]; dup {
			return nil, fmt.Errorf("duplicate character name: %s", ch.Name)
		}
		charNames[ch.Name] = struct{}{}
		if ch.Alias == nil {
			ch.Alias = []string{}
		}
	}

	// 场景校验：scene_id 连续唯一、出场角色归属人物表、单元类型与角色约束。
	seenSceneIDs := make(map[int]struct{}, len(payload.Scenes))
	expectedSceneID := 1
	for i := range payload.Scenes {
		sc := &payload.Scenes[i]
		sc.Location = strings.TrimSpace(sc.Location)
		sc.TimeOfDay = strings.TrimSpace(sc.TimeOfDay)
		sc.Summary = strings.TrimSpace(sc.Summary)
		if sc.SceneID != expectedSceneID {
			return nil, fmt.Errorf("scene_id %d is not contiguous (expected %d)", sc.SceneID, expectedSceneID)
		}
		expectedSceneID++
		if _, dup := seenSceneIDs[sc.SceneID]; dup {
			return nil, fmt.Errorf("duplicate scene_id: %d", sc.SceneID)
		}
		seenSceneIDs[sc.SceneID] = struct{}{}
		if sc.Location == "" {
			return nil, fmt.Errorf("scene %d location is required", sc.SceneID)
		}
		if sc.Summary == "" {
			return nil, fmt.Errorf("scene %d summary is required", sc.SceneID)
		}
		if sc.Cast == nil {
			sc.Cast = []string{}
		}
		sceneChars := make(map[string]struct{}, len(sc.Cast))
		for _, member := range sc.Cast {
			member = strings.TrimSpace(member)
			if _, ok := charNames[member]; !ok {
				return nil, fmt.Errorf("scene %d cast member %q not found in characters", sc.SceneID, member)
			}
			sceneChars[member] = struct{}{}
		}

		if len(sc.Units) == 0 {
			return nil, fmt.Errorf("scene %d units must not be empty", sc.SceneID)
		}
		for j := range sc.Units {
			unit := &sc.Units[j]
			unit.UnitType = strings.TrimSpace(unit.UnitType)
			unit.Character = strings.TrimSpace(unit.Character)
			unit.Content = strings.TrimSpace(unit.Content)
			switch unit.UnitType {
			case "action", "dialogue":
				if unit.Character == "" {
					return nil, fmt.Errorf("scene %d %s unit %d character is required", sc.SceneID, unit.UnitType, j+1)
				}
				if _, ok := sceneChars[unit.Character]; !ok {
					return nil, fmt.Errorf("scene %d %s unit character %q not in cast", sc.SceneID, unit.UnitType, unit.Character)
				}
			case "narration":
				if unit.Character != "" {
					return nil, fmt.Errorf("scene %d narration unit must not carry character", sc.SceneID)
				}
			default:
				return nil, fmt.Errorf("scene %d unit %d has invalid unit_type %q", sc.SceneID, j+1, unit.UnitType)
			}
			if unit.Content == "" {
				return nil, fmt.Errorf("scene %d unit %d content is required", sc.SceneID, j+1)
			}
		}
	}

	return &payload, nil
}

// runNovelToScript 小说转剧本编排：第一段产出大纲，第二段依大纲逐场展开。
// 两段产物都会落日志，第二段产物经过 parseNovelToScript 强校验后才返回。
func runNovelToScript(title string, novelText string, provider models.LLMProvider, taskID string) (*lightweightNovelScriptResult, error) {
	if strings.TrimSpace(novelText) == "" {
		return nil, fmt.Errorf("novel text is required")
	}

	task.GlobalTaskManager.UpdateTaskProgress(taskID, 10, "小说转剧本：提取情节主线与人物预表")

	sysOutline, userOutline, err := buildNovelToScriptOutlinePrompts(title, novelText)
	if err != nil {
		return nil, err
	}

	Log(
		LogLevelInfo,
		llmLogMessage("LLM Request", provider),
		"小说转剧本 第一段：大纲生成",
	)

	rawOutline, err := requestLightweightStoryOnce(provider, sysOutline, userOutline, taskID)
	if err != nil {
		Log(
			LogLevelError,
			llmLogMessage("LLM Error", provider),
			fmt.Sprintf("小说转剧本 第一段失败: %v", err),
		)
		return nil, err
	}

	Log(
		LogLevelInfo,
		llmLogMessage("LLM 完整返回(小说转剧本 大纲)", provider),
		rawOutline,
	)

	outline, err := parseNovelToScriptOutline(rawOutline)
	if err != nil {
		Log(
			LogLevelError,
			llmLogMessage("LLM 返回解析失败(小说转剧本 大纲)", provider),
			err.Error(),
		)
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(taskID, 40, fmt.Sprintf("小说转剧本：大纲完成（%d 场），开始逐场展开", len(outline.ScenePlan)))

	outlineJSON, err := json.MarshalIndent(outline, "", "  ")
	if err != nil {
		return nil, err
	}

	sysScenes, userScenes, err := buildNovelToScriptScenePrompts(title, novelText, string(outlineJSON))
	if err != nil {
		return nil, err
	}

	Log(
		LogLevelInfo,
		llmLogMessage("LLM Request", provider),
		"小说转剧本 第二段：逐场展开",
	)

	rawScript, err := requestLightweightStoryOnce(provider, sysScenes, userScenes, taskID)
	if err != nil {
		Log(
			LogLevelError,
			llmLogMessage("LLM Error", provider),
			fmt.Sprintf("小说转剧本 第二段失败: %v", err),
		)
		return nil, err
	}

	Log(
		LogLevelInfo,
		llmLogMessage("LLM 完整返回(小说转剧本 初稿)", provider),
		rawScript,
	)

	result, err := parseNovelToScript(rawScript)
	if err != nil {
		Log(
			LogLevelError,
			llmLogMessage("LLM 返回解析失败(小说转剧本 初稿)", provider),
			err.Error(),
		)
		return nil, err
	}

	task.GlobalTaskManager.UpdateTaskProgress(taskID, 60, fmt.Sprintf("小说转剧本：初稿完成（%d 场 %d 角色）", result.TotalScenes, len(result.Characters)))
	return result, nil
}
