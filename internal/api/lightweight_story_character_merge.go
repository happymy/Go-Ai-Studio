package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// coerceJSONStringSlice 把 JSON 字符串数组或单个字符串宽松转成 []string（null/空返回空 slice）。
func coerceJSONStringSlice(raw json.RawMessage) ([]string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return []string{}, nil
	}

	var asSlice []string
	if err := json.Unmarshal(raw, &asSlice); err == nil {
		out := make([]string, 0, len(asSlice))
		for _, s := range asSlice {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out, nil
	}

	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		s := strings.TrimSpace(asString)
		if s == "" {
			return []string{}, nil
		}
		return []string{s}, nil
	}

	return nil, fmt.Errorf("expected string array or string")
}

// characterHonorificSuffixes 常见称谓后缀白名单。剥离后剩余部分必须至少 2 个汉字，
// 防止把单字姓与称谓剥离后误归并（如"刘爷"不会被归并为"刘"）。
var characterHonorificSuffixes = []string{
	"大人", "公子", "姑娘", "小姐", "兄台", "大哥", "大姐", "道长", "法师", "大师",
	"阁下", "将军", "员外", "掌柜", "夫人", "太太", "先生", "少爷", "老爷", "太后",
	"陛下", "殿下", "王爷", "师父", "姑娘家", "好汉", "英雄", "老板", "大娘", "妹子",
}

// normalizeFullWidthAndWhitespace 全角字母数字转半角、去除全部空白（含全角空格）。
// 角色名归一化与文本模糊匹配归一化的公共前置步骤。
func normalizeFullWidthAndWhitespace(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= '０' && r <= '９':
			return r - '０' + '0'
		case r >= 'Ａ' && r <= 'Ｚ':
			return r - 'Ａ' + 'A'
		case r >= 'ａ' && r <= 'ｚ':
			return r - 'ａ' + 'a'
		}
		return r
	}, s)
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// normalizeLightweightCharacterName 规范化角色名，用于跨写法归并：
// 1. 全角字母数字转半角；2. 去掉所有空白（含全角空格）；3. 剥离白名单内的称谓后缀。
// 保守策略：只做格式与称谓层面的归一，不做语义相似匹配。
func normalizeLightweightCharacterName(name string) string {
	s := normalizeFullWidthAndWhitespace(strings.TrimSpace(name))

	for _, suffix := range characterHonorificSuffixes {
		if strings.HasSuffix(s, suffix) {
			rest := strings.TrimSuffix(s, suffix)
			if utf8.RuneCountInString(rest) >= 2 {
				s = rest
			}
			break
		}
	}
	return s
}

// existingCharacterKeySet 构建既有角色的归并命中集合：
// 既有角色的规范化主名 + 既有角色 alias 的规范化写法都算命中（利用 P0 已产出的 alias）。
func existingCharacterKeySet(existingCharacters []lightweightStoryCharacter) map[string]string {
	keys := make(map[string]string, len(existingCharacters)*2)
	for _, ch := range existingCharacters {
		canonical := normalizeLightweightCharacterName(ch.Name)
		if canonical != "" {
			if _, exists := keys[canonical]; !exists {
				keys[canonical] = ch.Name
			}
		}
		for _, alias := range ch.Alias {
			aliasNorm := normalizeLightweightCharacterName(alias)
			if aliasNorm != "" {
				if _, exists := keys[aliasNorm]; !exists {
					keys[aliasNorm] = ch.Name
				}
			}
		}
	}
	return keys
}

func mergeUniqueStrings(a []string, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, s := range append(append([]string{}, a...), b...) {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func mergeUniqueRelations(a []lightweightStoryCharacterRelation, b []lightweightStoryCharacterRelation) []lightweightStoryCharacterRelation {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]lightweightStoryCharacterRelation, 0, len(a)+len(b))
	for _, r := range append(append([]lightweightStoryCharacterRelation{}, a...), b...) {
		r.Name = strings.TrimSpace(r.Name)
		r.Type = strings.TrimSpace(r.Type)
		if r.Name == "" {
			continue
		}
		key := r.Name + "|" + r.Type
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, r)
	}
	return out
}

// mergeLightweightStoryCharacters 对新产出角色做保守归并：
//  1. 与既有角色（含 alias）规范化同名的 → 移除，视为既有角色，避免重复入库；
//  2. 新角色之间规范化同名的 → 合并为一个（保留首个名字，alias/personality/relations 并集去重）。
//
// 只作用于 payload.Characters，不改既有角色与场景内容。返回 (移除数, 合并数)。
func mergeLightweightStoryCharacters(payload *lightweightStoryResponse, existingCharacters []lightweightStoryCharacter) (int, int) {
	if payload == nil {
		return 0, 0
	}
	existingKeys := existingCharacterKeySet(existingCharacters)

	removedExisting := 0
	byCanonical := make(map[string]int, len(payload.Characters))
	merged := make([]lightweightStoryCharacter, 0, len(payload.Characters))

	for _, ch := range payload.Characters {
		canonical := normalizeLightweightCharacterName(ch.Name)
		if canonical == "" {
			merged = append(merged, ch)
			continue
		}
		if existingName, hit := existingKeys[canonical]; hit {
			removedExisting++
			Log(
				LogLevelWarn,
				"人物归并",
				fmt.Sprintf("新角色 %q 与既有角色 %q 规范化同名(%q)，视为既有角色，不再重复入库", ch.Name, existingName, canonical),
			)
			continue
		}
		if idx, dup := byCanonical[canonical]; dup {
			target := &merged[idx]
			Log(
				LogLevelWarn,
				"人物归并",
				fmt.Sprintf("新角色 %q 与本集角色 %q 规范化同名(%q)，合并为一个", ch.Name, target.Name, canonical),
			)
			// 被合并的异写名（去空白/全半角后相同）一并收进 Alias，便于后续集提示词锚定。
			if ch.Name != target.Name {
				target.Alias = mergeUniqueStrings(target.Alias, []string{ch.Name})
			}
			target.Alias = mergeUniqueStrings(target.Alias, ch.Alias)
			target.Personality = mergeUniqueStrings(target.Personality, ch.Personality)
			target.Relations = mergeUniqueRelations(target.Relations, ch.Relations)
			if strings.TrimSpace(target.Demeanor) == "" {
				target.Demeanor = strings.TrimSpace(ch.Demeanor)
			}
			if strings.TrimSpace(target.FirstSeen) == "" {
				target.FirstSeen = strings.TrimSpace(ch.FirstSeen)
			}
			continue
		}
		byCanonical[canonical] = len(merged)
		merged = append(merged, ch)
	}

	mergedCount := len(payload.Characters) - len(merged) - removedExisting
	payload.Characters = merged
	return removedExisting, mergedCount
}

// buildCharacterAssetRules 返回追加到 4 种模式 systemPrompt 末尾的人物资产填写规范。
// 经 buildLightweightStoryPrompts 统一注入，避免逐个模式手工维护。
func buildCharacterAssetRules() string {
	return strings.TrimSpace(`
【人物资产填写规范】
- characters 只返回本集首次登场的新角色；每个新角色必须包含 name、gender、age、height、era、country、appearance，并尽量填写：
  · personality：3 个以内的性格标签数组，如 ["外冷内热","重诺","寡言"]
  · demeanor：1 句以内的习惯动作或口头禅，可空
  · relations：只写与既有角色或同集角色的明确关系，如 [{"name":"李三","type":"旧识"}]，可空
  · first_seen：该角色本集首次出现的 scene_id，可空
- 若新角色与 existing_characters 中某角色是同一人（名字写法、称呼、别名的差异都算），必须把它当既有角色使用：沿用其 name 与 appearance 锚点，禁止另写一份外观，禁止重复放进 characters。
- appearance 只写永久锚点（体态、发色、脸型、标志性特征），不写服装、持物、伤口与临时状态。`)
}

// marshalJSONField 把值序列化为 JSON 字符串（DB 扩展列用）；空值或序列化失败返回空串。
func marshalJSONField(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

// parseJSONStringArrayField 解析 []string JSON 列；空串或脏数据返回空 slice，不报错。
func parseJSONStringArrayField(s string) []string {
	var out []string
	if strings.TrimSpace(s) == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	if out == nil {
		return []string{}
	}
	return out
}

// parseJSONRelationsField 解析人物关系 JSON 列；空串或脏数据返回空 slice，不报错。
func parseJSONRelationsField(s string) []lightweightStoryCharacterRelation {
	var out []lightweightStoryCharacterRelation
	if strings.TrimSpace(s) == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil
	}
	if out == nil {
		return []lightweightStoryCharacterRelation{}
	}
	return out
}
