package api

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeLightweightCharacterName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"沈西风", "沈西风"},
		{"沈 西 风", "沈西风"},
		{"沈　西风", "沈西风"},    // 全角空格
		{"沈西风\t大人", "沈西风"}, // 称谓后缀
		{"沈西风大人", "沈西风"},
		{"", ""},
	}

	// "沈公子" 剥离"公子"后剩"沈"（1 字），按保守策略不剥离，保持"沈公子"。
	// 上面用例是按剥离逻辑写的，这里单独验证保守策略。
	got := normalizeLightweightCharacterName("沈公子")
	if got != "沈公子" {
		t.Errorf("normalizeLightweightCharacterName(沈公子) = %q, want 沈公子 (保守：剩余1字不剥离)", got)
	}

	for _, tc := range cases {
		got := normalizeLightweightCharacterName(tc.in)
		if got != tc.want {
			t.Errorf("normalizeLightweightCharacterName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCoerceJSONStringSlice(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{"array", `["外冷内热","寡言"]`, []string{"外冷内热", "寡言"}},
		{"single string", `"冷峻"`, []string{"冷峻"}},
		{"null", `null`, []string{}},
		{"empty array", `[]`, []string{}},
		{"with blanks", `[" 甲 ", " 乙"]`, []string{"甲", "乙"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := coerceJSONStringSlice(json.RawMessage(tc.raw))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}

	if _, err := coerceJSONStringSlice(json.RawMessage(`123`)); err == nil {
		t.Errorf("expected error for number input")
	}
}

func TestLightweightStoryCharacterUnmarshalNewFields(t *testing.T) {
	raw := `{
		"name": "沈西风",
		"gender": "男性",
		"age": "30岁",
		"appearance": "灰衣剑客",
		"personality": ["重诺", "寡言"],
		"demeanor": "习惯先按下剑再说话",
		"relations": [{"name": "李三", "type": "旧识"}],
		"first_seen": "scene 1"
	}`
	var ch lightweightStoryCharacter
	if err := json.Unmarshal([]byte(raw), &ch); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(ch.Personality) != 2 || ch.Personality[0] != "重诺" {
		t.Errorf("personality parse failed: %v", ch.Personality)
	}
	if ch.Demeanor == "" {
		t.Errorf("demeanor should not be empty")
	}
	if len(ch.Relations) != 1 || ch.Relations[0].Name != "李三" {
		t.Errorf("relations parse failed: %v", ch.Relations)
	}
	if ch.FirstSeen == "" {
		t.Errorf("first_seen should not be empty")
	}

	// personality 单字符串容错
	rawSingle := `{"name":"张三","personality":"冷峻"}`
	var ch2 lightweightStoryCharacter
	if err := json.Unmarshal([]byte(rawSingle), &ch2); err != nil {
		t.Fatalf("unmarshal single personality failed: %v", err)
	}
	if len(ch2.Personality) != 1 || ch2.Personality[0] != "冷峻" {
		t.Errorf("single personality parse failed: %v", ch2.Personality)
	}

	// relations 非法（数字）应报错
	rawBad := `{"name":"张三","relations":123}`
	var ch3 lightweightStoryCharacter
	if err := json.Unmarshal([]byte(rawBad), &ch3); err == nil {
		t.Errorf("expected error for invalid relations")
	}
}

func TestMergeLightweightStoryCharacters(t *testing.T) {
	existing := []lightweightStoryCharacter{
		{Name: "沈西风", Appearance: "灰衣剑客，身形清瘦", Alias: []string{"沈公子", "沈 西 风"}},
	}

	cases := []struct {
		name         string
		input        []lightweightStoryCharacter
		wantLen      int
		wantRemoved  int
		wantMerged   int
		wantContains string
		wantAlias    string
	}{
		{
			name: "规范化同名命中既有角色则移除",
			input: []lightweightStoryCharacter{
				{Name: "沈 西 风", Appearance: "灰衣剑客"},
				{Name: "李三", Appearance: "矮胖掌柜"},
			},
			wantLen:      1,
			wantRemoved:  1,
			wantMerged:   0,
			wantContains: "李三",
		},
		{
			name: "新角色名字命中既有角色alias也移除",
			input: []lightweightStoryCharacter{
				{Name: "沈公子", Appearance: "灰衣"},
			},
			wantLen:     0,
			wantRemoved: 1,
		},
		{
			name: "新角色内部规范化相同则合并",
			input: []lightweightStoryCharacter{
				{Name: "张三", Alias: []string{"三哥"}, Personality: []string{"莽撞"}},
				{Name: "张 三", Alias: []string{"张三爷"}, Personality: []string{"仗义"}},
			},
			wantLen:      1,
			wantMerged:   1,
			wantContains: "张三",
			wantAlias:    "三哥",
		},
		{
			name: "不同角色不误并",
			input: []lightweightStoryCharacter{
				{Name: "沈西楼", Appearance: "白衣"},
				{Name: "沈青萍", Appearance: "青衣"},
			},
			wantLen:     2,
			wantRemoved: 0,
			wantMerged:  0,
		},
		{
			name: "称谓差异命中既有角色",
			input: []lightweightStoryCharacter{
				{Name: "沈西风大人", Appearance: "灰衣剑客"},
			},
			wantLen:     0,
			wantRemoved: 1,
		},
		{
			name: "单字保守不误并",
			input: []lightweightStoryCharacter{
				{Name: "刘爷", Appearance: "富商"},
			},
			wantLen:      1,
			wantRemoved:  0,
			wantContains: "刘爷",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := &lightweightStoryResponse{Characters: tc.input}
			removed, merged := mergeLightweightStoryCharacters(payload, existing)
			if removed != tc.wantRemoved {
				t.Errorf("removed = %d, want %d", removed, tc.wantRemoved)
			}
			if merged != tc.wantMerged {
				t.Errorf("merged = %d, want %d", merged, tc.wantMerged)
			}
			if len(payload.Characters) != tc.wantLen {
				t.Fatalf("characters len = %d, want %d (got %+v)", len(payload.Characters), tc.wantLen, payload.Characters)
			}
			if tc.wantContains != "" {
				found := false
				for _, ch := range payload.Characters {
					if ch.Name == tc.wantContains {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected character %q in result", tc.wantContains)
				}
			}
			if tc.wantAlias != "" {
				found := false
				for _, ch := range payload.Characters {
					for _, alias := range ch.Alias {
						if alias == tc.wantAlias {
							found = true
						}
					}
				}
				if !found {
					t.Errorf("expected alias %q in result", tc.wantAlias)
				}
			}
			// 合并用例补充：并集去重断言
			if tc.wantMerged == 1 && tc.name == "新角色内部规范化相同则合并" {
				ch := payload.Characters[0]
				if len(ch.Personality) != 2 {
					t.Errorf("merged personality should be deduped union, got %v", ch.Personality)
				}
				if len(ch.Alias) != 2 {
					t.Errorf("merged alias should be deduped union, got %v", ch.Alias)
				}
			}
		})
	}
}

func TestMergeLightweightStoryCharactersNil(t *testing.T) {
	if removed, merged := mergeLightweightStoryCharacters(nil, nil); removed != 0 || merged != 0 {
		t.Errorf("nil payload should be a no-op, got %d/%d", removed, merged)
	}
}

func TestBuildCharacterAssetRules(t *testing.T) {
	rules := buildCharacterAssetRules()
	if rules == "" {
		t.Fatal("rules should not be empty")
	}
	for _, keyword := range []string{"personality", "demeanor", "relations", "first_seen", "existing_characters"} {
		if !strings.Contains(rules, keyword) {
			t.Errorf("rules missing keyword %q", keyword)
		}
	}
}
