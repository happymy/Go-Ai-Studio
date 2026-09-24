package api

import (
	"errors"
	"strings"
	"testing"
)

func TestClassifyStoryValidationError(t *testing.T) {
	fixable := []error{
		errors.New("scene 1 image_prompt is required"),
		errors.New("scene 2 video_prompt is required"),
		errors.New("scene 1 duration_seconds must be greater than 0"),
		errors.New("duplicate scene_id: 3"),
		errors.New("duplicate character name: 张三"),
		errors.New("character name is required"),
		errors.New("scenes array must not be empty"),
	}
	for _, err := range fixable {
		ok, messages := classifyStoryValidationError(err)
		if !ok || len(messages) != 1 {
			t.Errorf("expected fixable with 1 message for %v, got ok=%v messages=%v", err, ok, messages)
		}
	}
	notFixable := []error{
		errors.New("invalid character 'x' looking for beginning of object key"),
		errors.New("unexpected end of JSON input"),
		errors.New("some unrelated failure"),
		nil,
	}
	for _, err := range notFixable {
		if ok, _ := classifyStoryValidationError(err); ok {
			t.Errorf("expected not fixable for %v", err)
		}
	}
}

func TestBuildStoryFixInstruction(t *testing.T) {
	if got := buildStoryFixInstruction(nil, nil); got != "" {
		t.Fatalf("empty messages should produce empty instruction, got %q", got)
	}
	partial := &lightweightStoryResponse{Scenes: []lightweightStoryScene{{SceneID: 1}}}
	got := buildStoryFixInstruction([]string{"scene 1 image_prompt is required"}, partial)
	for _, keyword := range []string{"未通过校验", "image_prompt is required", "完整顶层 JSON", "scene_id"} {
		if !strings.Contains(got, keyword) {
			t.Errorf("fix instruction missing keyword %q", keyword)
		}
	}
}

const p4ValidPayload = `{
	"total_scenes": 1,
	"characters": [{"name": "沈西风"}],
	"scenes": [{"scene_id": 1, "duration_seconds": 5, "narration": "n", "image_prompt": "img", "video_prompt": "vid"}],
	"episode_memory": {"story_summary": "s"}
}`

const p4MissingImagePromptPayload = `{
	"total_scenes": 1,
	"characters": [{"name": "沈西风"}],
	"scenes": [{"scene_id": 1, "duration_seconds": 5, "narration": "n", "video_prompt": "vid"}],
	"episode_memory": {"story_summary": "s"}
}`

func TestRunLightweightStoryGenerationWithRetryFixesAndSucceeds(t *testing.T) {
	responses := []string{p4MissingImagePromptPayload, p4ValidPayload}
	requestCount := 0
	requestOnce := func(system string, user string) (string, error) {
		requestCount++
		// 第二次请求的 userPrompt 必须携带修复指令
		if requestCount == 2 && !strings.Contains(user, "未通过校验") {
			t.Errorf("retry userPrompt should carry fix instruction")
		}
		if requestCount-1 >= len(responses) {
			return "", errors.New("unexpected extra request")
		}
		return responses[requestCount-1], nil
	}
	parseOnce := func(raw string) (*lightweightStoryResponse, error) {
		return parseStrictLightweightStoryResponse(raw)
	}
	validateCount := 0
	validate := func(p *lightweightStoryResponse) error {
		validateCount++
		return validateLightweightStoryResponse(p, nil, AutoGenerateModeHighQuality, 0)
	}

	payload, _, err := runLightweightStoryGenerationWithRetry("sys", "user", nil, requestOnce, parseOnce, validate, nil, 3)
	if err != nil {
		t.Fatalf("expected success after fix, got %v", err)
	}
	if requestCount != 2 {
		t.Errorf("expected 2 requests, got %d", requestCount)
	}
	if validateCount != 2 {
		t.Errorf("expected 2 validate calls, got %d", validateCount)
	}
	if len(payload.Scenes) != 1 || payload.Scenes[0].ImagePrompt != "img" {
		t.Errorf("payload should be the fixed one, got %+v", payload.Scenes)
	}
}

func TestRunLightweightStoryGenerationWithRetryExhausts(t *testing.T) {
	requestCount := 0
	requestOnce := func(system string, user string) (string, error) {
		requestCount++
		return p4MissingImagePromptPayload, nil
	}
	parseOnce := func(raw string) (*lightweightStoryResponse, error) {
		return parseStrictLightweightStoryResponse(raw)
	}
	validate := func(p *lightweightStoryResponse) error {
		return validateLightweightStoryResponse(p, nil, AutoGenerateModeHighQuality, 0)
	}
	_, _, err := runLightweightStoryGenerationWithRetry("sys", "user", nil, requestOnce, parseOnce, validate, nil, 2)
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	if !strings.Contains(err.Error(), "image_prompt is required") {
		t.Errorf("expected last validation error, got %v", err)
	}
	if requestCount != 2 {
		t.Errorf("expected 2 attempts, got %d", requestCount)
	}
}

func TestRunLightweightStoryGenerationWithRetryParseFailureNoRetry(t *testing.T) {
	requestCount := 0
	requestOnce := func(system string, user string) (string, error) {
		requestCount++
		return "not json at all", nil
	}
	parseOnce := func(raw string) (*lightweightStoryResponse, error) {
		return parseStrictLightweightStoryResponse(raw)
	}
	validate := func(p *lightweightStoryResponse) error {
		return nil
	}
	_, _, err := runLightweightStoryGenerationWithRetry("sys", "user", nil, requestOnce, parseOnce, validate, nil, 3)
	if err == nil {
		t.Fatal("expected parse error to fail immediately")
	}
	if requestCount != 1 {
		t.Errorf("expected no retry on parse failure, got %d requests", requestCount)
	}
}

func TestRunLightweightStoryGenerationWithRetryMaxAttemptsOne(t *testing.T) {
	requestCount := 0
	requestOnce := func(system string, user string) (string, error) {
		requestCount++
		return p4MissingImagePromptPayload, nil
	}
	parseOnce := func(raw string) (*lightweightStoryResponse, error) {
		return parseStrictLightweightStoryResponse(raw)
	}
	validate := func(p *lightweightStoryResponse) error {
		return validateLightweightStoryResponse(p, nil, AutoGenerateModeHighQuality, 0)
	}
	if _, _, err := runLightweightStoryGenerationWithRetry("sys", "user", nil, requestOnce, parseOnce, validate, nil, 1); err == nil {
		t.Fatal("expected error with maxAttempts=1")
	}
	if requestCount != 1 {
		t.Errorf("expected single attempt, got %d", requestCount)
	}
}

func TestRunLightweightStoryGenerationWithRetryDialogueLossTriggersRetry(t *testing.T) {
	// plot 含台词"李三在哪？"，首轮响应未落位 → 触发台词缺失重试；第二轮补齐。
	const validWithDialogue = `{
		"total_scenes": 1,
		"characters": [{"name": "沈西风"}],
		"scenes": [{"scene_id": 1, "duration_seconds": 5, "narration": "沈西风追问李三在哪", "image_prompt": "img", "video_prompt": "沈西风追问李三在哪"}],
		"episode_memory": {"story_summary": "s"}
	}`
	const missingDialogue = `{
		"total_scenes": 1,
		"characters": [{"name": "沈西风"}],
		"scenes": [{"scene_id": 1, "duration_seconds": 5, "narration": "沈西风看着窗外", "image_prompt": "img", "video_prompt": "沈西风沉默"}],
		"episode_memory": {"story_summary": "s"}
	}`
	responses := []string{missingDialogue, validWithDialogue}
	requestCount := 0
	requestOnce := func(system string, user string) (string, error) {
		requestCount++
		if requestCount == 2 && !strings.Contains(user, "未通过校验") {
			t.Errorf("retry userPrompt should carry quality issue list")
		}
		return responses[requestCount-1], nil
	}
	parseOnce := func(raw string) (*lightweightStoryResponse, error) {
		return parseStrictLightweightStoryResponse(raw)
	}
	validate := func(p *lightweightStoryResponse) error {
		return validateLightweightStoryResponse(p, nil, AutoGenerateModeHighQuality, 0)
	}
	postQuality := func(p *lightweightStoryResponse) []string {
		return checkDialogueCoverage("王五说：“李三在哪？”", p.Scenes)
	}
	payload, _, err := runLightweightStoryGenerationWithRetry("sys", "user", nil, requestOnce, parseOnce, validate, postQuality, 3)
	if err != nil {
		t.Fatalf("expected success after dialogue-loss retry, got %v", err)
	}
	if requestCount != 2 {
		t.Errorf("expected 2 requests (dialogue loss trigger), got %d", requestCount)
	}
	if len(payload.Scenes) != 1 || !strings.Contains(payload.Scenes[0].Narration, "李三在哪") {
		t.Errorf("expected fixed dialogue, got %+v", payload.Scenes)
	}
}

// 回归保护：台词缺失属软信号（质量报告标注），末次尝试仍未修复时必须接受结果返回成功，
// 不得升级为整集失败丢弃合法产物（曾出现的回归：retry exhausted → 调用方 return nil, err）。
func TestRunLightweightStoryGenerationWithRetryQualityIssueExhaustedStillSucceeds(t *testing.T) {
	const missingDialogue = `{
		"total_scenes": 1,
		"characters": [{"name": "沈西风"}],
		"scenes": [{"scene_id": 1, "duration_seconds": 5, "narration": "沈西风看着窗外", "image_prompt": "img", "video_prompt": "沈西风沉默"}],
		"episode_memory": {"story_summary": "s"}
	}`
	responses := []string{missingDialogue, missingDialogue, missingDialogue}
	requestCount := 0
	requestOnce := func(system string, user string) (string, error) {
		requestCount++
		return responses[requestCount-1], nil
	}
	parseOnce := func(raw string) (*lightweightStoryResponse, error) {
		return parseStrictLightweightStoryResponse(raw)
	}
	validate := func(p *lightweightStoryResponse) error {
		return validateLightweightStoryResponse(p, nil, AutoGenerateModeHighQuality, 0)
	}
	postQuality := func(p *lightweightStoryResponse) []string {
		return checkDialogueCoverage("王五说：“李三在哪？”", p.Scenes)
	}
	payload, _, err := runLightweightStoryGenerationWithRetry("sys", "user", nil, requestOnce, parseOnce, validate, postQuality, 3)
	if err != nil {
		t.Fatalf("expected success even when quality issue persists at last attempt, got %v", err)
	}
	if requestCount != 3 {
		t.Errorf("expected 3 requests (retries exhausted), got %d", requestCount)
	}
	if len(payload.Scenes) != 1 {
		t.Errorf("expected payload returned, got %+v", payload.Scenes)
	}
}
