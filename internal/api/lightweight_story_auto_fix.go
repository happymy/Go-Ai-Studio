package api

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// fixableStoryValidationPatterns 可修复的校验错误特征（validateLightweightStoryResponse 的消息文本）。
// 命中这些特征的错误，会携带错误清单重试一次；其余视为结构级/解析级错乱，保持失败。
var fixableStoryValidationPatterns = []string{
	"image_prompt is required",
	"video_prompt is required",
	"duration_seconds must be greater than 0",
	"scene_id must be greater than 0",
	"duplicate scene_id",
	"duplicate character name",
	"character name is required",
	"scenes array must not be empty",
	"less than required narrative node count",
}

// classifyStoryValidationError 判断校验错误是否可修复（返回错误消息清单，供重试 prompt 使用）。
func classifyStoryValidationError(err error) (bool, []string) {
	if err == nil {
		return false, nil
	}
	message := err.Error()
	for _, pattern := range fixableStoryValidationPatterns {
		if strings.Contains(message, pattern) {
			return true, []string{message}
		}
	}
	return false, nil
}

// buildStoryFixInstruction 构造附在 userPrompt 之后的"修复指令"：
// 给出校验错误清单 + 上次失败返回的完整内容（对标 wswhhhc 迭代修复：让 LLM 基于部分结果修正而非重写）。
func buildStoryFixInstruction(messages []string, partial *lightweightStoryResponse) string {
	if len(messages) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("【上次生成的 JSON 未通过校验，请修复后重新输出完整 JSON】\n")
	sb.WriteString("校验问题清单：\n")
	for _, m := range messages {
		sb.WriteString("- " + m + "\n")
	}
	sb.WriteString("修复要求：\n")
	sb.WriteString("1. 输出必须是完整顶层 JSON（total_scenes、characters、scenes、episode_memory），禁止只输出补丁或局部字段。\n")
	sb.WriteString("2. 逐条修复上面清单里指出的问题（缺字段补齐、非法值改合法、重名合并去重）。\n")
	sb.WriteString("3. 除被点名修复的字段外，其余内容尽量保持与上次一致，不要重写人物外观锚点或场景三件套。\n")
	if partial != nil && len(partial.Scenes) > 0 {
		if b, err := json.MarshalIndent(partial, "", "  "); err == nil {
			sb.WriteString("以下是上次返回内容（仅供定位问题，禁止原样重复其中的错误）：\n")
			sb.WriteString(string(b) + "\n")
		}
	}
	return strings.TrimSpace(sb.String())
}

// splitStorySentences 把原文按中文句读标点与换行切成句子（保留非空段落）。
func splitStorySentences(plot string) []string {
	fields := strings.FieldsFunc(plot, func(r rune) bool {
		switch r {
		case '。', '！', '？', '；', '\n', '\r', '!', '?', ';':
			return true
		}
		return false
	})
	sentences := make([]string, 0, len(fields))
	for _, f := range fields {
		if s := strings.TrimSpace(f); s != "" {
			sentences = append(sentences, s)
		}
	}
	return sentences
}

// buildStoryFixKeywords 从本次成功解析的结果中提取检索关键词：本集新角色名 + 各场景出场角色名。
// 关键词经规范化（去空白/标点/全角转半角）且去重；长度不足 2 字符的丢弃（防单字误命中）。
func buildStoryFixKeywords(payload *lightweightStoryResponse) []string {
	seen := make(map[string]struct{})
	var keywords []string
	add := func(s string) {
		norm := normalizeTextForMatch(s)
		if norm == "" || len([]rune(norm)) < 2 {
			return
		}
		if _, ok := seen[norm]; ok {
			return
		}
		seen[norm] = struct{}{}
		keywords = append(keywords, norm)
	}
	if payload != nil {
		for _, ch := range payload.Characters {
			add(ch.Name)
		}
		for _, scene := range payload.Scenes {
			for _, name := range scene.Characters {
				add(name)
			}
		}
	}
	return keywords
}

// buildStoryFixContext 第三梯队（R2 HAR Context Retrieval 的规则版）：
// 修正重试时，从原文 plot 中检索与本次修正最相关的句子片段，附加到修复指令，
// 让 LLM 基于原文语境修正而非凭空重写。
// 相关度 = 句中命中角色名关键词的数量（规范化包含匹配，无需分词依赖）。
// 返回命中片段按原文顺序拼接，总字数不超过 maxRunes（单句超限跳过、不截断，保持完整语义）；
// 无可命中内容时返回空串。
func buildStoryFixContext(plot string, payload *lightweightStoryResponse, maxRunes int) string {
	if strings.TrimSpace(plot) == "" || payload == nil || maxRunes <= 0 {
		return ""
	}
	keywords := buildStoryFixKeywords(payload)
	if len(keywords) == 0 {
		return ""
	}
	type scoredSentence struct {
		index int
		score int
		text  string
	}
	var scored []scoredSentence
	for i, sentence := range splitStorySentences(plot) {
		norm := normalizeTextForMatch(sentence)
		if norm == "" {
			continue
		}
		score := 0
		for _, kw := range keywords {
			if strings.Contains(norm, kw) {
				score++
			}
		}
		if score > 0 {
			scored = append(scored, scoredSentence{index: i, score: score, text: sentence})
		}
	}
	if len(scored) == 0 {
		return ""
	}
	// 先按命中数降序选取，再按原文顺序还原拼接。
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].score > scored[j].score })
	var selected []scoredSentence
	totalRunes := 0
	for _, s := range scored {
		runes := utf8.RuneCountInString(s.text)
		if totalRunes+runes > maxRunes {
			if len(selected) == 0 {
				continue // 单句超限：跳过而不是截断，保持完整语义
			}
			break
		}
		selected = append(selected, s)
		totalRunes += runes
	}
	if len(selected) == 0 {
		return ""
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].index < selected[j].index })
	var parts []string
	for _, s := range selected {
		parts = append(parts, s.text)
	}
	return strings.Join(parts, "。")
}

// runLightweightStoryGenerationWithRetry 执行"请求→解析→校验→质量后检"循环：
// 校验失败且可修复时，携带错误清单与上次部分结果重试（最多 maxAttempts 次，含首次）。
// postQualityCheck 在校验通过后执行：返回非空问题清单（如台词缺失）时同样触发一次携带清单的重试。
// requestOnce 返回 LLM 原始文本；parseOnce 解析结构（解析失败视为不可修复，直接失败）。
// continuationPartial 只在第一次成功后合并一次（重试为全量重新生成，不再续接）。
// plot 为原文剧本：重试时经 buildStoryFixContext 检索相关段落附进修复指令（第三梯队语境增强）。
// 返回最终 payload 与最后一次使用的 userPrompt。
func runLightweightStoryGenerationWithRetry(
	systemPrompt string,
	userPrompt string,
	continuationPartial *lightweightStoryPartialContext,
	plot string,
	requestOnce func(systemPrompt string, userPrompt string) (string, error),
	parseOnce func(raw string) (*lightweightStoryResponse, error),
	validate func(payload *lightweightStoryResponse) error,
	postQualityCheck func(payload *lightweightStoryResponse) []string,
	maxAttempts int,
) (*lightweightStoryResponse, string, error) {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	currentUserPrompt := userPrompt
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		raw, err := requestOnce(systemPrompt, currentUserPrompt)
		if err != nil {
			return nil, currentUserPrompt, err
		}
		payload, err := parseOnce(raw)
		if err != nil {
			// 解析失败属于结构级错乱，不可修复。
			return nil, currentUserPrompt, err
		}
		if continuationPartial != nil {
			payload = mergeLightweightStoryContinuation(continuationPartial, payload)
			continuationPartial = nil
		}
		var issueMessages []string
		if err := validate(payload); err != nil {
			fixable, messages := classifyStoryValidationError(err)
			if !fixable || attempt >= maxAttempts {
				return nil, currentUserPrompt, err
			}
			issueMessages = messages
		} else if postQualityCheck != nil {
			// 校验通过但质量后检发现问题（如台词缺失）→ 同样进入修复重试。
			if issues := postQualityCheck(payload); len(issues) > 0 {
				if attempt >= maxAttempts {
					// 最后一次尝试：质量后检问题（如台词缺失）属软信号，
					// 由质量报告标注，不升级为整集失败，避免丢弃合法产物。
					Log(
						LogLevelWarn,
						"质量后检未完全修复",
						fmt.Sprintf("第 %d 次尝试后仍存在 %d 项质量提示，接受当前结果：%v", attempt+1, len(issues), issues),
					)
					return payload, currentUserPrompt, nil
				}
				issueMessages = issues
			}
		}
		if len(issueMessages) == 0 {
			return payload, currentUserPrompt, nil
		}
		fixInstruction := buildStoryFixInstruction(issueMessages, payload)
		if fixInstruction == "" {
			return nil, currentUserPrompt, fmt.Errorf("lightweight story generation fix instruction empty")
		}
		if context := buildStoryFixContext(plot, payload, 400); context != "" {
			fixInstruction = fixInstruction + "\n\n【相关原文语境（供修正参考；只能据此修正内容，不得改写原文本身）】\n" + context
		}
		Log(
			LogLevelWarn,
			"自动修复重试",
			fmt.Sprintf("第 %d 次尝试未通过，将进行第 %d 次重试（上限 %d）：%v", attempt+1, attempt+2, maxAttempts, issueMessages),
		)
		currentUserPrompt = currentUserPrompt + "\n\n" + fixInstruction
	}
	return nil, currentUserPrompt, fmt.Errorf("lightweight story generation retry exhausted")
}
