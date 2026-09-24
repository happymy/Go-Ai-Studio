package api

import (
	"encoding/json"
	"fmt"
	"strings"
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

// runLightweightStoryGenerationWithRetry 执行"请求→解析→校验→质量后检"循环：
// 校验失败且可修复时，携带错误清单与上次部分结果重试（最多 maxAttempts 次，含首次）。
// postQualityCheck 在校验通过后执行：返回非空问题清单（如台词缺失）时同样触发一次携带清单的重试。
// requestOnce 返回 LLM 原始文本；parseOnce 解析结构（解析失败视为不可修复，直接失败）。
// continuationPartial 只在第一次成功后合并一次（重试为全量重新生成，不再续接）。
// 返回最终 payload 与最后一次使用的 userPrompt。
func runLightweightStoryGenerationWithRetry(
	systemPrompt string,
	userPrompt string,
	continuationPartial *lightweightStoryPartialContext,
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
					break
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
		Log(
			LogLevelWarn,
			"自动修复重试",
			fmt.Sprintf("attempt %d/%d：%v", attempt+1, maxAttempts, issueMessages),
		)
		currentUserPrompt = currentUserPrompt + "\n\n" + fixInstruction
	}
	return nil, currentUserPrompt, fmt.Errorf("lightweight story generation retry exhausted")
}
