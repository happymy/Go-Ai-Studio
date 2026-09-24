package api

import (
	"os"
	"strings"
	"testing"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	d, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	// sqlite :memory: 每个连接是独立数据库；强制单连接，避免 AutoMigrate 建的表
	// 被连接池中其他连接查询时出现 "no such table"。
	sqlDB, err := d.DB()
	if err != nil {
		panic(err)
	}
	sqlDB.SetMaxOpenConns(1)
	db.DB = d
	if err := d.AutoMigrate(&models.SystemLog{}, &models.Character{}, &models.SystemSettings{}); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestGetLLMTimeoutMinutes(t *testing.T) {
	cases := []struct {
		name     string
		value    string
		expected float64
	}{
		{"配置 240 分钟", "240", 240},
		{"配置 15 分钟", "15", 15},
		{"空值回落默认 30", "", 30},
		{"非法值回落默认 30", "abc", 30},
		{"配置 5 的合法下限", "5", 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db.DB.Where("key = ?", KeyLLMTimeoutMinutes).Delete(&models.SystemSettings{})
			if tc.value != "" {
				if err := db.DB.Create(&models.SystemSettings{Key: KeyLLMTimeoutMinutes, Value: tc.value}).Error; err != nil {
					t.Fatalf("seed setting: %v", err)
				}
			}
			got := getLLMTimeoutMinutes().Minutes()
			if got != tc.expected {
				t.Fatalf("expected %.0f minutes, got %.0f", tc.expected, got)
			}
		})
	}
}

func newValidatePayload(chars []lightweightStoryCharacter) *lightweightStoryResponse {
	return &lightweightStoryResponse{
		Characters: chars,
		Scenes: []lightweightStoryScene{
			{SceneID: 1, DurationSeconds: 5, ImagePrompt: "img", VideoPrompt: "vid"},
		},
	}
}

func TestValidateLightweightStoryResponseSkipsExistingNames(t *testing.T) {
	existing := []lightweightStoryCharacter{{Name: "李四"}}
	payload := newValidatePayload([]lightweightStoryCharacter{
		{Name: "张三"},
		{Name: "李四"},
	})
	if err := validateLightweightStoryResponse(payload, existing, "high_quality", 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(payload.Characters) != 1 || payload.Characters[0].Name != "张三" {
		t.Fatalf("existing-name character should be filtered out, got %+v", payload.Characters)
	}
}

func TestValidateLightweightStoryResponseRejectsBadInputs(t *testing.T) {
	if err := validateLightweightStoryResponse(newValidatePayload([]lightweightStoryCharacter{{Name: "张三"}, {Name: "张三"}}), nil, "high_quality", 0); err == nil || !strings.Contains(err.Error(), "duplicate character name") {
		t.Fatalf("expected duplicate character name error, got %v", err)
	}
	if err := validateLightweightStoryResponse(newValidatePayload([]lightweightStoryCharacter{{Name: ""}}), nil, "high_quality", 0); err == nil {
		t.Fatal("expected empty-name error")
	}
}

// 跨集锚点断链修复的回归保护：persist 侧写入的扩展 JSON 列，重新加载时必须能回填
// alias/personality/relations，否则跨集 existing_characters 永远只有 appearance。
func TestNormalizeStoryCharacterRecordRoundTrip(t *testing.T) {
	record := models.Character{
		Name:            "沈西风",
		Appearance:      "灰衣剑客",
		AliasJSON:       marshalJSONField([]string{"沈公子", "沈 西 风"}),
		PersonalityJSON: marshalJSONField([]string{"外冷内热", "寡言"}),
		RelationsJSON:   marshalJSONField([]lightweightStoryCharacterRelation{{Name: "李三", Type: "旧识"}}),
	}
	ch := normalizeStoryCharacterRecord(record)
	if len(ch.Alias) != 2 || ch.Alias[0] != "沈公子" {
		t.Errorf("alias round-trip failed: %v", ch.Alias)
	}
	if len(ch.Personality) != 2 || ch.Personality[1] != "寡言" {
		t.Errorf("personality round-trip failed: %v", ch.Personality)
	}
	if len(ch.Relations) != 1 || ch.Relations[0].Name != "李三" || ch.Relations[0].Type != "旧识" {
		t.Errorf("relations round-trip failed: %v", ch.Relations)
	}

	// 无扩展数据（旧库存/手工角色）回退为空，不报错
	plain := normalizeStoryCharacterRecord(models.Character{Name: "李三", Appearance: "矮胖掌柜"})
	if plain.Alias != nil || plain.Personality != nil || plain.Relations != nil {
		t.Errorf("legacy record should fall back to nil fields, got %+v", plain)
	}
}

// 第一梯队：续写 prompt 必须把上一段最后一场的 ending_state 作为续写起点注入，
// 实现跨场状态延续（承接位置/持物/服装/情绪，禁止凭空重置）。
func TestBuildLightweightStoryContinuationUserPromptCarriesEndingState(t *testing.T) {
	base := "base user prompt"
	baseScenes := []lightweightStoryScene{
		{SceneID: 1, DurationSeconds: 5, ImagePrompt: "img1", VideoPrompt: "vid1",
			EndingState: "沈西风握剑立于桌前，油灯将熄"},
	}
	partial := &lightweightStoryPartialContext{
		TotalScenes: 3,
		Scenes:      baseScenes,
		NextSceneID: 2,
	}
	prompt, err := buildLightweightStoryContinuationUserPrompt(base, partial)
	if err != nil {
		t.Fatalf("build continuation prompt failed: %v", err)
	}
	if !strings.Contains(prompt, "状态快照") || !strings.Contains(prompt, "沈西风握剑立于桌前") {
		t.Errorf("continuation prompt should carry last scene ending_state, got: %s", prompt)
	}
	if !strings.Contains(prompt, "禁止跳跃或凭空重置") {
		t.Errorf("continuation prompt should require natural continuation, got: %s", prompt)
	}

	// 上一段末场没有 ending_state → 不注入该指令，prompt 仍可生成
	partialNoState := &lightweightStoryPartialContext{
		TotalScenes: 2,
		Scenes:      []lightweightStoryScene{{SceneID: 1, DurationSeconds: 5, ImagePrompt: "img1", VideoPrompt: "vid1"}},
		NextSceneID: 2,
	}
	promptNoState, err := buildLightweightStoryContinuationUserPrompt(base, partialNoState)
	if err != nil {
		t.Fatalf("build continuation prompt (no ending_state) failed: %v", err)
	}
	if strings.Contains(promptNoState, "状态快照") {
		t.Errorf("continuation prompt should not inject ending_state instruction when absent, got: %s", promptNoState)
	}
}
