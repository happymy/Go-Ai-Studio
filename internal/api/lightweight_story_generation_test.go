package api

import (
	"encoding/json"
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

// 一键转剧本新角色必须携带 face_fingerprint / fingerprint（体态/服装/装备锚点），
// 否则生图与跨集复用拿不到稳定体态服装锚点（回归保护：曾全部为空）。
func TestLightweightStoryCharacterUnmarshalFingerprint(t *testing.T) {
	input := []byte(`{
		"name": "苏橙",
		"gender": "女性",
		"age": "24岁",
		"height": "约一米七二",
		"era": "现代",
		"country": "中国",
		"appearance": "黑色长直发偏中分，鹅蛋脸，眉眼清冷，鼻梁高挺",
		"face_fingerprint": "鹅蛋脸，眉眼清冷，鼻梁高挺，黑色长直发偏中分",
		"fingerprint": "体态挺拔的年轻女性，穿白色护士制服，腰间佩工作卡套，白色护士鞋"
	}`)
	var ch lightweightStoryCharacter
	if err := json.Unmarshal(input, &ch); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if ch.FaceFingerprint != "鹅蛋脸，眉眼清冷，鼻梁高挺，黑色长直发偏中分" {
		t.Errorf("face_fingerprint not parsed: %q", ch.FaceFingerprint)
	}
	if ch.Fingerprint != "体态挺拔的年轻女性，穿白色护士制服，腰间佩工作卡套，白色护士鞋" {
		t.Errorf("fingerprint not parsed: %q", ch.Fingerprint)
	}

	// 旧输出不含新字段时保持向后兼容
	var legacy lightweightStoryCharacter
	if err := json.Unmarshal([]byte(`{"name":"李三","appearance":"矮胖掌柜"}`), &legacy); err != nil {
		t.Fatalf("legacy unmarshal failed: %v", err)
	}
	if legacy.FaceFingerprint != "" || legacy.Fingerprint != "" {
		t.Errorf("legacy output should keep empty anchors, got face=%q fp=%q", legacy.FaceFingerprint, legacy.Fingerprint)
	}
}

// 落库时 LLM 未返回锚点时，从 appearance 兜底拆解脸部与非脸部锚点，保证非空。
func TestDeriveLightweightCharacterAnchors(t *testing.T) {
	cases := []struct {
		name         string
		appearance   string
		wantFaceSub  string
		wantBodySub  string
		wantBodySome bool
	}{
		{
			name:         "混合脸部与体态服装句",
			appearance:   "黑色长直发偏中分，鹅蛋脸，眉眼清冷，身材高挑，穿白色护士制服，白色护士鞋",
			wantFaceSub:  "鹅蛋脸",
			wantBodySub:  "白色护士制服",
			wantBodySome: true,
		},
		{
			name:         "身高体态句归体态",
			appearance:   "短发，脸型偏方，约一米八五，肩背挺直",
			wantFaceSub:  "脸型偏方",
			wantBodySub:  "约一米八五",
			wantBodySome: true,
		},
		{
			name:         "纯脸部描述时体态可为空但脸部非空",
			appearance:   "短发刚修剪过，脸型偏方，眉骨清晰，鼻梁挺直，下巴线条平直",
			wantFaceSub:  "眉骨清晰",
			wantBodySome: false,
		},
		{
			name:         "空串返回双空",
			appearance:   "",
			wantFaceSub:  "",
			wantBodySub:  "",
			wantBodySome: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			faceFP, bodyFP := deriveLightweightCharacterAnchors(tc.appearance)
			if tc.wantFaceSub != "" && !strings.Contains(faceFP, tc.wantFaceSub) {
				t.Errorf("face anchor missing %q, got %q", tc.wantFaceSub, faceFP)
			}
			if tc.wantBodySub != "" && !strings.Contains(bodyFP, tc.wantBodySub) {
				t.Errorf("body anchor missing %q, got %q", tc.wantBodySub, bodyFP)
			}
			hasBody := bodyFP != ""
			if tc.wantBodySome && !hasBody {
				t.Errorf("expected non-empty body anchor, got %q", bodyFP)
			}
			if !tc.wantBodySome && hasBody {
				t.Errorf("expected empty body anchor, got %q", bodyFP)
			}
			// 脸部兜底不得为空（有 appearance 时）
			if tc.appearance != "" && faceFP == "" {
				t.Errorf("face anchor should not be empty for non-empty appearance, got %q", faceFP)
			}
		})
	}
}

// normalizeStoryCharacterRecord 续写注入旧角色时必须带上已锁定的锚点，
// 否则跨集续写会再次丢失体态/服装/装备锚点。
func TestNormalizeStoryCharacterRecordFingerprintRoundTrip(t *testing.T) {
	record := models.Character{
		Name:            "苏橙",
		Appearance:      "黑色长直发偏中分，鹅蛋脸",
		FaceFingerprint: "鹅蛋脸，眉眼清冷",
		Fingerprint:     "穿白色护士制服，白色护士鞋",
	}
	ch := normalizeStoryCharacterRecord(record)
	if ch.FaceFingerprint != "鹅蛋脸，眉眼清冷" {
		t.Errorf("face_fingerprint round-trip failed: %q", ch.FaceFingerprint)
	}
	if ch.Fingerprint != "穿白色护士制服，白色护士鞋" {
		t.Errorf("fingerprint round-trip failed: %q", ch.Fingerprint)
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
