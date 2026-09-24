package api

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"kt-ai-studio/internal/models"
)

// normalizeTextForMatch 用于台词/角色名模糊匹配的统一规范化：
// 全角字母数字转半角、去掉全部空白与标点符号（保留汉字/字母/数字）。
func normalizeTextForMatch(s string) string {
	s = normalizeFullWidthAndWhitespace(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// checkDialogueCoverage 从 plot 提取引号台词（复用 extractStoryboardQuotedDialogues），
// 逐条核对是否出现在某 scene 的 narration/video_prompt/image_prompt 中（规范化模糊包含），
// 返回未落位的台词清单。没有引号台词时返回 nil。
func checkDialogueCoverage(plot string, scenes []lightweightStoryScene) []string {
	dialogues := extractStoryboardQuotedDialogues(plot)
	if len(dialogues) == 0 {
		return nil
	}

	var sceneText strings.Builder
	for _, scene := range scenes {
		sceneText.WriteString(scene.Narration)
		sceneText.WriteString(" ")
		sceneText.WriteString(scene.VideoPrompt)
		sceneText.WriteString(" ")
		sceneText.WriteString(scene.ImagePrompt)
		sceneText.WriteString(" ")
	}
	haystack := normalizeTextForMatch(sceneText.String())
	if haystack == "" {
		return dialogues
	}

	var missing []string
	for _, dialogue := range dialogues {
		needle := normalizeTextForMatch(dialogue)
		if needle == "" {
			continue
		}
		if strings.Contains(haystack, needle) {
			continue
		}
		// 长台词强召回：LLM 可能在镜内改写过台词的后半句，取前 8 字再查一次。
		runes := []rune(needle)
		if len(runes) > 12 && strings.Contains(haystack, string(runes[:8])) {
			continue
		}
		missing = append(missing, dialogue)
	}
	return missing
}

// checkNarrationRatio 返回旁白类文本占全部镜头正文的字数比例。
// 定义：narration 字符数 / (narration + video_prompt) 字符数，防止除零。
func checkNarrationRatio(scenes []lightweightStoryScene) float64 {
	var narrationChars, totalChars int
	for _, scene := range scenes {
		n := utf8.RuneCountInString(strings.TrimSpace(scene.Narration))
		v := utf8.RuneCountInString(strings.TrimSpace(scene.VideoPrompt))
		narrationChars += n
		totalChars += n + v
	}
	if totalChars == 0 {
		return 0
	}
	return float64(narrationChars) / float64(totalChars)
}

// checkGhostCharacters 检查每个 scene 标注的出场角色是否在人物库（已知名字集合）内，
// 返回出场但未登记的角色清单（去重）。knownNames 由调用方汇总既有角色与新角色及其别名。
func checkGhostCharacters(scenes []lightweightStoryScene, knownNames []string) []string {
	known := make(map[string]struct{}, len(knownNames))
	for _, name := range knownNames {
		if norm := normalizeTextForMatch(name); norm != "" {
			known[norm] = struct{}{}
		}
	}

	seen := make(map[string]struct{})
	var ghosts []string
	for _, scene := range scenes {
		for _, name := range scene.Characters {
			norm := normalizeTextForMatch(name)
			if norm == "" {
				continue
			}
			if _, hit := known[norm]; hit {
				continue
			}
			if _, dup := seen[norm]; dup {
				continue
			}
			seen[norm] = struct{}{}
			ghosts = append(ghosts, name)
		}
	}
	return ghosts
}

// NarrationRatioSoftThreshold / NarrationRatioHardThreshold 旁白占比阈值（0.35 软告警，0.5 重扣）。
const (
	NarrationRatioSoftThreshold = 0.35
	NarrationRatioHardThreshold = 0.50
)

// checkObjectiveTurnArticulation 目标达成验证（第二梯队，R2 HAR 验证场景达成目标 的规则版）：
// 在"每场有目标/转折"基础上，检查目标是否落实为镜头说明、转折是否真的产生了变化：
// 1) objective 非空但该场 narration 为空 → 目标没有任何镜头说明落实；
// 2) turn 与 objective 规范化后相同或互为包含 → 转折只是目标的复述，本场无实际变化；
// 3) 相邻两场 turn 规范化后相同 → 剧情状态停滞未推进。
// 返回问题清单；无问题时返回 nil（纯规则，不依赖分词，可靠但保守）。
func checkObjectiveTurnArticulation(scenes []lightweightStoryScene) []string {
	var issues []string
	for i := range scenes {
		scene := &scenes[i]
		obj := strings.TrimSpace(scene.Objective)
		turn := strings.TrimSpace(scene.Turn)
		if obj == "" && turn == "" {
			continue
		}
		if obj != "" && strings.TrimSpace(scene.Narration) == "" {
			issues = append(issues, fmt.Sprintf("scene %d 目标为 %q，但该场没有任何 narration 说明，目标未落实为镜头内容", scene.SceneID, obj))
		}
		if obj != "" && turn != "" {
			normObj := normalizeTextForMatch(obj)
			normTurn := normalizeTextForMatch(turn)
			if normObj != "" && normTurn != "" && (normObj == normTurn || strings.Contains(normObj, normTurn) || strings.Contains(normTurn, normObj)) {
				issues = append(issues, fmt.Sprintf("scene %d 转折只是目标的复述（objective=%q / turn=%q），本场没有实际状态变化", scene.SceneID, obj, turn))
			}
		}
	}
	for i := 1; i < len(scenes); i++ {
		prev := normalizeTextForMatch(scenes[i-1].Turn)
		cur := normalizeTextForMatch(scenes[i].Turn)
		if prev != "" && cur != "" && prev == cur {
			issues = append(issues, fmt.Sprintf("scene %d 与 scene %d 的 turn 相同，剧情状态未推进", scenes[i-1].SceneID, scenes[i].SceneID))
		}
	}
	return issues
}

// checkCastPresenceInSceneBody 出场角色可见性检查：scene.Characters 标注出场的角色，
// 其名字必须出现在该场 narration/image_prompt/video_prompt 至少一处（规范化模糊包含），
// 否则视为"在场人物无声消失"（h3_short 机制 C 的质量化落地）。
func checkCastPresenceInSceneBody(scenes []lightweightStoryScene) []string {
	var missing []string
	for _, scene := range scenes {
		if len(scene.Characters) == 0 {
			continue
		}
		body := normalizeTextForMatch(strings.Join([]string{scene.Narration, scene.ImagePrompt, scene.VideoPrompt}, " "))
		for _, name := range scene.Characters {
			norm := normalizeTextForMatch(name)
			if norm == "" {
				continue
			}
			if body != "" && strings.Contains(body, norm) {
				continue
			}
			missing = append(missing, fmt.Sprintf("scene %d 出场角色 %s 无可见痕迹", scene.SceneID, name))
		}
	}
	return missing
}

// lightweightStoryQualityReport 生成质量报告（P3）。
// 评分参照 1dashboard：结构 40 / 格式 30 / 内容 30，总分 0-100。
type lightweightStoryQualityReport struct {
	Score           int      `json:"score"`
	Structure       int      `json:"structure"`
	Format          int      `json:"format"`
	Content         int      `json:"content"`
	DialogueLoss    []string `json:"dialogue_loss,omitempty"`
	NarrationRatio  float64  `json:"narration_ratio"`
	GhostCharacters []string `json:"ghost_characters,omitempty"`
	CastMissing     []string `json:"cast_missing,omitempty"`
	ObjectiveTurn   []string `json:"objective_turn_issues,omitempty"`
	Issues          []string `json:"issues"`
}

// buildLightweightStoryQualityReport 生成质量报告。
// plot 用于台词覆盖检查；existingCharacters + payload.Characters 组成人物库。
func buildLightweightStoryQualityReport(payload *lightweightStoryResponse, existingCharacters []lightweightStoryCharacter, plot string) lightweightStoryQualityReport {
	report := lightweightStoryQualityReport{}
	if payload == nil {
		report.Issues = append(report.Issues, "story payload is nil")
		return report
	}

	report.DialogueLoss = checkDialogueCoverage(plot, payload.Scenes)
	report.NarrationRatio = checkNarrationRatio(payload.Scenes)

	knownNames := make([]string, 0, (len(existingCharacters)+len(payload.Characters))*2)
	for _, ch := range existingCharacters {
		knownNames = append(knownNames, ch.Name)
		knownNames = append(knownNames, ch.Alias...)
	}
	for _, ch := range payload.Characters {
		knownNames = append(knownNames, ch.Name)
		knownNames = append(knownNames, ch.Alias...)
	}
	report.GhostCharacters = checkGhostCharacters(payload.Scenes, knownNames)
	objectiveTurnIssues := checkObjectiveTurnArticulation(payload.Scenes)
	report.ObjectiveTurn = objectiveTurnIssues
	report.CastMissing = checkCastPresenceInSceneBody(payload.Scenes)

	// 结构 40：scene_id 覆盖 1..N 完整性 / 时长合法 / 戏剧卡 objective 覆盖率
	// 注：LLM 返回的数组顺序可能乱序（persist 前会按 scene_id 排序），
	// 因此不按数组下标判连续，只检查 id 集合是否覆盖 1..N（缺号才扣分）。
	structure := 40
	if len(payload.Scenes) == 0 {
		structure = 0
		report.Issues = append(report.Issues, "结构：scenes 为空")
	} else {
		seenSceneIDs := make(map[int]struct{}, len(payload.Scenes))
		for _, scene := range payload.Scenes {
			seenSceneIDs[scene.SceneID] = struct{}{}
		}
		for id := 1; id <= len(payload.Scenes); id++ {
			if _, ok := seenSceneIDs[id]; !ok {
				structure -= 10
				report.Issues = append(report.Issues, fmt.Sprintf("结构：scene_id 缺号 %d（应覆盖 1..%d）", id, len(payload.Scenes)))
				break
			}
		}
		for _, scene := range payload.Scenes {
			if scene.DurationSeconds <= 0 {
				structure -= 5
				report.Issues = append(report.Issues, fmt.Sprintf("结构：scene %d duration_seconds 非法", scene.SceneID))
			}
		}
		missingObjective := 0
		for _, scene := range payload.Scenes {
			if strings.TrimSpace(scene.Objective) == "" {
				missingObjective++
			}
		}
		missingRatio := float64(missingObjective) / float64(len(payload.Scenes))
		switch {
		case missingRatio > 0.70:
			structure -= 20
			report.Issues = append(report.Issues, fmt.Sprintf("结构：%.0f%% 场景缺少 objective（戏剧目标）", missingRatio*100))
		case missingRatio > 0.30:
			structure -= 10
			report.Issues = append(report.Issues, fmt.Sprintf("结构：%.0f%% 场景缺少 objective（戏剧目标）", missingRatio*100))
		}
	}
	if structure < 0 {
		structure = 0
	}

	// 格式 30：镜头正文必填完整性 + 台词落位
	format := 30
	for _, scene := range payload.Scenes {
		if strings.TrimSpace(scene.ImagePrompt) == "" {
			format -= 5
			report.Issues = append(report.Issues, fmt.Sprintf("格式：scene %d image_prompt 为空", scene.SceneID))
		}
		if strings.TrimSpace(scene.VideoPrompt) == "" {
			format -= 5
			report.Issues = append(report.Issues, fmt.Sprintf("格式：scene %d video_prompt 为空", scene.SceneID))
		}
		if strings.TrimSpace(scene.Narration) == "" {
			format -= 2
			report.Issues = append(report.Issues, fmt.Sprintf("格式：scene %d narration 为空", scene.SceneID))
		}
	}
	if len(report.DialogueLoss) > 0 {
		format -= 3 * len(report.DialogueLoss)
		report.Issues = append(report.Issues, fmt.Sprintf("格式：%d 条台词未落位", len(report.DialogueLoss)))
	}
	if format < 0 {
		format = 0
	}

	// 内容 30：幽灵角色 / 人物库为空 / 旁白占比 / 目标达成 / 出场角色可见性
	content := 30
	content -= 5 * len(report.GhostCharacters)
	if len(report.GhostCharacters) > 0 {
		report.Issues = append(report.Issues, fmt.Sprintf("内容：%d 个幽灵角色（出场但未登记）", len(report.GhostCharacters)))
	}
	if len(payload.Characters) == 0 && len(existingCharacters) == 0 {
		content -= 5
		report.Issues = append(report.Issues, "内容：人物库为空")
	}
	for _, issue := range objectiveTurnIssues {
		content -= 3
		report.Issues = append(report.Issues, "内容："+issue)
	}
	content -= 2 * len(report.CastMissing)
	if len(report.CastMissing) > 0 {
		report.Issues = append(report.Issues, fmt.Sprintf("内容：%d 个出场角色无可见痕迹（在场人物无声消失）", len(report.CastMissing)))
	}
	switch {
	case report.NarrationRatio > NarrationRatioHardThreshold:
		content -= 15
		report.Issues = append(report.Issues, fmt.Sprintf("内容：旁白占比 %.0f%% 超过硬阈值", report.NarrationRatio*100))
	case report.NarrationRatio > NarrationRatioSoftThreshold:
		content -= 10
		report.Issues = append(report.Issues, fmt.Sprintf("内容：旁白占比 %.0f%% 超过软阈值", report.NarrationRatio*100))
	}
	if content < 0 {
		content = 0
	}

	report.Structure = structure
	report.Format = format
	report.Content = content
	report.Score = structure + format + content
	return report
}

// buildLightweightStoryQualityReportMarkdown 把质量报告渲染为 Markdown，
// 用于写入任务结果字段（task.Result），便于跨次运行对比分数。
func buildLightweightStoryQualityReportMarkdown(report lightweightStoryQualityReport, req models.AutoGenerateRequest) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## 生成质量报告（模式：%s / 第 %d 集）\n\n", strings.TrimSpace(req.GenerationMode), req.Episode)
	fmt.Fprintf(&sb, "| 维度 | 得分 |\n|------|------|\n")
	fmt.Fprintf(&sb, "| 结构 | %d/40 |\n", report.Structure)
	fmt.Fprintf(&sb, "| 格式 | %d/30 |\n", report.Format)
	fmt.Fprintf(&sb, "| 内容 | %d/30 |\n", report.Content)
	fmt.Fprintf(&sb, "| **总分** | **%d/100** |\n\n", report.Score)
	fmt.Fprintf(&sb, "- 旁白占比：%.0f%%\n", report.NarrationRatio*100)
	fmt.Fprintf(&sb, "- 台词缺失：%d 条\n", len(report.DialogueLoss))
	fmt.Fprintf(&sb, "- 目标达成问题：%d 条（objective 无落点/转折复述/状态停滞）\n", len(report.ObjectiveTurn))
	fmt.Fprintf(&sb, "- 幽灵角色：%d 个", len(report.GhostCharacters))
	if len(report.GhostCharacters) > 0 {
		sb.WriteString("（" + strings.Join(report.GhostCharacters, "、") + "）")
	}
	sb.WriteString("\n")
	fmt.Fprintf(&sb, "- 出场角色无可见痕迹：%d 个", len(report.CastMissing))
	if len(report.CastMissing) > 0 {
		sb.WriteString("（" + strings.Join(report.CastMissing, "、") + "）")
	}
	sb.WriteString("\n\n")
	if len(report.Issues) == 0 {
		sb.WriteString("问题清单：无\n")
	} else {
		sb.WriteString("问题清单：\n")
		for _, issue := range report.Issues {
			sb.WriteString("- " + issue + "\n")
		}
	}
	return strings.TrimSpace(sb.String())
}
