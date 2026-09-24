package api

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// normalizeTextForMatch 用于台词/角色名模糊匹配的统一规范化：
// 全角字母数字转半角、去掉全部空白与标点符号（保留汉字/字母/数字）。
func normalizeTextForMatch(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= '０' && r <= '９':
			return r - '０' + '0'
		case r >= 'Ａ' && r <= 'Ｚ':
			return r - 'Ａ' + 'A'
		case r >= 'ａ' && r <= 'ｚ':
			return r - 'ａ' + 'a'
		case unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r):
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

	// 结构 40：scene 连续性 / 时长合法 / 戏剧卡 objective 覆盖率
	structure := 40
	if len(payload.Scenes) == 0 {
		structure = 0
		report.Issues = append(report.Issues, "结构：scenes 为空")
	} else {
		for i, scene := range payload.Scenes {
			if scene.SceneID != i+1 {
				structure -= 10
				report.Issues = append(report.Issues, fmt.Sprintf("结构：scene_id 不连续（第 %d 个为 %d）", i+1, scene.SceneID))
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

	// 内容 30：幽灵角色 / 人物库为空 / 旁白占比
	content := 30
	content -= 5 * len(report.GhostCharacters)
	if len(report.GhostCharacters) > 0 {
		report.Issues = append(report.Issues, fmt.Sprintf("内容：%d 个幽灵角色（出场但未登记）", len(report.GhostCharacters)))
	}
	if len(payload.Characters) == 0 && len(existingCharacters) == 0 {
		content -= 5
		report.Issues = append(report.Issues, "内容：人物库为空")
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
