package api

import (
	"encoding/json"
	"strings"
	"testing"

	"kt-ai-studio/internal/models"
)

func TestLightweightStorySceneUnmarshalDramatizationFields(t *testing.T) {
	raw := `{
		"scene_id": 1,
		"duration_seconds": 6,
		"narration": "沈西风推门而入",
		"image_prompt": "客栈大堂 内 夜，木桌油灯",
		"video_prompt": "沈西风推门，油灯晃动",
		"objective": "沈西风要打听到李三下落",
		"conflict": "掌柜王五含糊其辞",
		"turn": "沈西风从王五眼神中看出破绽",
		"scene_function": "关系",
		"mood_arc": "平静→紧张",
		"location": "客栈大堂 内 夜，木桌油灯，桌面摆着酒碗"
	}`
	var scene lightweightStoryScene
	if err := json.Unmarshal([]byte(raw), &scene); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	for field, got := range map[string]string{
		"objective":      scene.Objective,
		"conflict":       scene.Conflict,
		"turn":           scene.Turn,
		"scene_function": scene.SceneFunction,
		"mood_arc":       scene.MoodArc,
		"location":       scene.Location,
	} {
		if got == "" {
			t.Errorf("%s should not be empty", field)
		}
	}
	if scene.SceneID != 1 || scene.DurationSeconds != 6 {
		t.Errorf("legacy fields broken: %+v", scene)
	}
}

func TestLightweightStorySceneUnmarshalLegacyCompatible(t *testing.T) {
	raw := `{"scene_id": 2, "duration_seconds": 5, "narration": "n", "image_prompt": "img", "video_prompt": "vid"}`
	var scene lightweightStoryScene
	if err := json.Unmarshal([]byte(raw), &scene); err != nil {
		t.Fatalf("legacy scene should parse without new fields: %v", err)
	}
	if scene.Objective != "" || scene.Location != "" || scene.Turn != "" {
		t.Errorf("legacy scene should have empty optional fields, got %+v", scene)
	}
	// 旧输出 JSON 序列化时不得出现新字段（omitempty 保证向后兼容）
	out, err := json.Marshal(scene)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if strings.Contains(string(out), "objective") || strings.Contains(string(out), "location") {
		t.Errorf("legacy marshal should omit new fields, got %s", out)
	}
	if strings.Contains(string(out), "shot_size") || strings.Contains(string(out), "ending_state") {
		t.Errorf("legacy marshal should omit cinematic fields, got %s", out)
	}
}

func TestLightweightStorySceneUnmarshalCinematicFields(t *testing.T) {
	raw := `{
		"scene_id": 1,
		"duration_seconds": 6,
		"narration": "n",
		"image_prompt": "img",
		"video_prompt": "vid",
		"shot_size": "中景",
		"camera_angle": "平视",
		"camera_movement": "固定",
		"blocking": "沈西风居左前景，李三居右后景",
		"ending_state": "沈西风握剑立于桌前，油灯将熄"
	}`
	var scene lightweightStoryScene
	if err := json.Unmarshal([]byte(raw), &scene); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	for field, got := range map[string]string{
		"shot_size":       scene.ShotSize,
		"camera_angle":    scene.CameraAngle,
		"camera_movement": scene.CameraMovement,
		"blocking":        scene.Blocking,
		"ending_state":    scene.EndingState,
	} {
		if got == "" {
			t.Errorf("%s should not be empty", field)
		}
	}
}

func TestLightweightStorySceneUnmarshalCinematicDirtyScalars(t *testing.T) {
	// LLM 把字符串字段输出成数字/布尔时容错为字符串（与 coerceJSONScalarToString 一致）
	raw := `{
		"scene_id": 1,
		"duration_seconds": 5,
		"narration": "n",
		"image_prompt": "img",
		"video_prompt": "vid",
		"shot_size": 123,
		"camera_angle": true,
		"camera_movement": "固定",
		"blocking": "ok",
		"ending_state": "ok"
	}`
	var scene lightweightStoryScene
	if err := json.Unmarshal([]byte(raw), &scene); err != nil {
		t.Fatalf("dirty scalar cinematic fields should coerce not fail: %v", err)
	}
	if scene.ShotSize != "123" || scene.CameraAngle != "true" || scene.CameraMovement != "固定" {
		t.Errorf("dirty scalar coercion mismatch: %+v", scene)
	}
	// 对象/数组不能被转字符串：与 P2 字段行为一致，解析失败并指向具体字段
	rawObj := `{"scene_id":1,"duration_seconds":5,"narration":"n","image_prompt":"img","video_prompt":"vid","shot_size":{"a":1}}`
	var sceneObj lightweightStoryScene
	if err := json.Unmarshal([]byte(rawObj), &sceneObj); err == nil || !strings.Contains(err.Error(), "shot_size") {
		t.Errorf("object shot_size should fail with field reference, got %v", err)
	}
}

func TestValidateH3ShortMissingDramatizationWarnsNotFails(t *testing.T) {
	payload := newValidatePayload(nil)
	payload.Scenes[0] = lightweightStoryScene{
		SceneID: 1, DurationSeconds: 5, ImagePrompt: "img", VideoPrompt: "vid",
		// objective/turn/location 全缺
	}
	if err := validateLightweightStoryResponse(payload, nil, AutoGenerateModeH3Short, 0); err != nil {
		t.Fatalf("h3_short missing dramatization fields should warn not fail, got %v", err)
	}
}

func TestValidateH3ShortCompleteDramatizationPasses(t *testing.T) {
	payload := newValidatePayload(nil)
	payload.Scenes[0] = lightweightStoryScene{
		SceneID: 1, DurationSeconds: 5, ImagePrompt: "img", VideoPrompt: "vid",
		Objective: "找李三", Turn: "发现破绽", Location: "客栈大堂 内 夜",
	}
	if err := validateLightweightStoryResponse(payload, nil, AutoGenerateModeH3Short, 0); err != nil {
		t.Fatalf("h3_short complete dramatization should pass, got %v", err)
	}
}

func TestValidateStandardMissingDramatizationPasses(t *testing.T) {
	payload := newValidatePayload(nil)
	if err := validateLightweightStoryResponse(payload, nil, AutoGenerateModeHighQuality, 0); err != nil {
		t.Fatalf("non-h3_short mode should not enforce dramatization fields, got %v", err)
	}
}

func TestBuildSceneWritingCard(t *testing.T) {
	normal := buildSceneWritingCard(false)
	strict := buildSceneWritingCard(true)
	for _, keyword := range []string{"objective", "conflict", "turn", "scene_function", "mood_arc", "location"} {
		if !strings.Contains(normal, keyword) {
			t.Errorf("normal card missing keyword %q", keyword)
		}
	}
	for _, keyword := range []string{"shot_size", "camera_angle", "camera_movement", "blocking", "ending_state"} {
		if !strings.Contains(normal, keyword) {
			t.Errorf("normal card missing cinematic keyword %q", keyword)
		}
	}
	if !strings.Contains(normal, "镜头语言卡与状态衔接") {
		t.Errorf("normal card should contain cinematic card section")
	}
	if strings.Contains(normal, "强制项") {
		t.Errorf("normal card should not contain strict-only rule")
	}
	if !strings.Contains(strict, "必填") || !strings.Contains(strict, "强制项") {
		t.Errorf("strict card should enforce required fields")
	}
	if !strings.Contains(strict, "shot_size") || !strings.Contains(strict, "ending_state") {
		t.Errorf("strict card should enforce cinematic fields")
	}
}

func TestValidateH3ShortMissingCinematicWarnsNotFails(t *testing.T) {
	payload := newValidatePayload(nil)
	payload.Scenes[0] = lightweightStoryScene{
		SceneID: 1, DurationSeconds: 5, ImagePrompt: "img", VideoPrompt: "vid",
		Objective: "找李三", Turn: "发现破绽", Location: "客栈大堂 内 夜",
		// 镜头语言卡/状态衔接全缺
	}
	if err := validateLightweightStoryResponse(payload, nil, AutoGenerateModeH3Short, 0); err != nil {
		t.Fatalf("h3_short missing cinematic fields should warn not fail, got %v", err)
	}
}

func TestValidateH3ShortCompleteCinematicPasses(t *testing.T) {
	payload := newValidatePayload(nil)
	payload.Scenes[0] = lightweightStoryScene{
		SceneID: 1, DurationSeconds: 5, ImagePrompt: "img", VideoPrompt: "vid",
		Objective: "找李三", Turn: "发现破绽", Location: "客栈大堂 内 夜",
		ShotSize: "中景", CameraAngle: "平视", CameraMovement: "固定",
		Blocking: "沈西风居左前景", EndingState: "沈西风握剑立于桌前",
	}
	if err := validateLightweightStoryResponse(payload, nil, AutoGenerateModeH3Short, 0); err != nil {
		t.Fatalf("h3_short complete cinematic fields should pass, got %v", err)
	}
}

func TestAllModesGetSceneWritingCard(t *testing.T) {
	reqBase := models.AutoGenerateRequest{Plot: "test plot", Episode: 1}
	existing := []lightweightStoryCharacter{}
	prev := emptyEpisodeMemory()
	modes := []struct {
		mode   string
		strict bool
	}{
		{"", false}, // default → standard prompt
		{AutoGenerateModeHighQuality, false},
		{AutoGenerateModeStoryboard, false},
		{AutoGenerateModeH3Short, true},
	}
	for _, m := range modes {
		req := reqBase
		req.GenerationMode = m.mode
		systemPrompt, _, err := buildLightweightStoryPrompts(models.Project{}, req, existing, prev, "", 0)
		if err != nil {
			t.Fatalf("mode %s: buildLightweightStoryPrompts failed: %v", m.mode, err)
		}
		if !strings.Contains(systemPrompt, "场景写作卡") {
			t.Errorf("mode %s: systemPrompt missing scene writing card", m.mode)
		}
		if m.strict && !strings.Contains(systemPrompt, "强制项") {
			t.Errorf("mode %s: systemPrompt should contain strict rule", m.mode)
		}
		if !m.strict && strings.Contains(systemPrompt, "强制项") {
			t.Errorf("mode %s: systemPrompt should not contain strict rule", m.mode)
		}
	}
}
