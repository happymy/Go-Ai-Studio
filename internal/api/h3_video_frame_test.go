package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kt-ai-studio/internal/workflow"
)

func TestNormalizeH3VideoFrameSize(t *testing.T) {
	cases := []struct {
		w, h         int
		wantW, wantH int
	}{
		{1024, 1024, 976, 976},
		{768, 1344, 736, 1296},
		{512, 512, 512, 512},
		{0, 0, 736, 1296},
		{100, 100, 96, 96},
		{7, 7, 16, 16},
	}
	for _, c := range cases {
		gotW, gotH := normalizeH3VideoFrameSize(c.w, c.h)
		if gotW != c.wantW || gotH != c.wantH {
			t.Errorf("normalizeH3VideoFrameSize(%d,%d) = (%d,%d), want (%d,%d)", c.w, c.h, gotW, gotH, c.wantW, c.wantH)
		}
		if gotW%h3VideoFrameSizeMultiple != 0 || gotH%h3VideoFrameSizeMultiple != 0 {
			t.Errorf("normalizeH3VideoFrameSize(%d,%d) = (%d,%d) not aligned to %d", c.w, c.h, gotW, gotH, h3VideoFrameSizeMultiple)
		}
		if gotW <= 0 || gotH <= 0 {
			t.Errorf("normalizeH3VideoFrameSize(%d,%d) returned non-positive", c.w, c.h)
		}
	}
}

func TestIsVideoOutputFilename(t *testing.T) {
	for _, name := range []string{"a.mp4", "b.WEBM", "c.gif", "d.mov", "e.mkv"} {
		if !isVideoOutputFilename(name) {
			t.Errorf("isVideoOutputFilename(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"a.png", "b.jpg", "noext", "", "  "} {
		if isVideoOutputFilename(name) {
			t.Errorf("isVideoOutputFilename(%q) = true, want false", name)
		}
	}
}

func TestInjectH3T2VParams(t *testing.T) {
	workflowPath := filepath.Join("..", "..", "workflows", h3T2VWorkflowFileName)
	if _, err := os.Stat(workflowPath); err != nil {
		t.Skipf("H3 t2v workflow not present: %v", err)
	}
	meta, err := workflow.ParseWorkflow(workflowPath)
	if err != nil {
		t.Fatalf("ParseWorkflow: %v", err)
	}
	data, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	var wfJSON map[string]interface{}
	if err := json.Unmarshal(data, &wfJSON); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if meta.PositiveNodeID == "" || meta.SeedNodeID == "" || meta.WidthNodeID == "" || meta.HeightNodeID == "" {
		t.Fatalf("parser missed node ids: %+v", meta)
	}
	t.Logf("meta: pos=%s/%s neg=%s/%s seed=%s/%s width=%s/%s height=%s/%s",
		meta.PositiveNodeID, meta.PositiveInputKey, meta.NegativeNodeID, meta.NegativeInputKey,
		meta.SeedNodeID, meta.SeedInputKey, meta.WidthNodeID, meta.WidthInputKey,
		meta.HeightNodeID, meta.HeightInputKey)

	injectH3T2VParams(wfJSON, meta, "test prompt", 12345, 512, 512)

	inputValue := func(nodeID, key string) interface{} {
		node, _ := wfJSON[nodeID].(map[string]interface{})
		inputs, _ := node["inputs"].(map[string]interface{})
		return inputs[key]
	}
	if got := inputValue(meta.PositiveNodeID, meta.PositiveInputKey); got != "test prompt" {
		t.Errorf("positive prompt = %v, want test prompt", got)
	}
	if got := inputValue(meta.SeedNodeID, meta.SeedInputKey); got != int64(12345) && got != 12345 {
		t.Errorf("seed = %v (%T), want 12345", got, got)
	}
	if got := inputValue(meta.WidthNodeID, meta.WidthInputKey); got != 512 {
		t.Errorf("width = %v, want 512", got)
	}
	if got := inputValue(meta.HeightNodeID, meta.HeightInputKey); got != 512 {
		t.Errorf("height = %v, want 512", got)
	}

	foundDuration := false
	for _, node := range wfJSON {
		nodeMap, ok := node.(map[string]interface{})
		if !ok {
			continue
		}
		if classType, _ := nodeMap["class_type"].(string); classType != "PrimitiveFloat" {
			continue
		}
		metaMap, _ := nodeMap["_meta"].(map[string]interface{})
		title, _ := metaMap["title"].(string)
		if title == "" {
			continue
		}
		foundDuration = true
		inputs, _ := nodeMap["inputs"].(map[string]interface{})
		if inputs["value"] != h3VideoFrameDurationSeconds {
			t.Errorf("duration node %q value = %v, want %v", title, inputs["value"], h3VideoFrameDurationSeconds)
		}
	}
	if !foundDuration {
		t.Error("no PrimitiveFloat node found in H3 t2v workflow")
	}
}

func TestMergeH3StaticPrompt(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		extra  string
		want   string
	}{
		{"空附加词原样返回", "场景描述", "", "场景描述"},
		{"纯空白附加词原样返回", "场景描述", "  \n ", "场景描述"},
		{"正常追加到末尾", "场景描述", "完全静止", "场景描述\n完全静止"},
		{"已包含时去重", "场景描述\n完全静止", "完全静止", "场景描述\n完全静止"},
		{"空提示词只留附加词", "   ", "完全静止", "完全静止"},
		{"附加词首尾空格被清理", "场景描述", "  完全静止  ", "场景描述\n完全静止"},
	}
	for _, c := range cases {
		if got := mergeH3StaticPrompt(c.prompt, c.extra); got != c.want {
			t.Errorf("%s: mergeH3StaticPrompt(%q, %q) = %q, want %q", c.name, c.prompt, c.extra, got, c.want)
		}
	}
}

func TestH3VideoFramePromptPresets(t *testing.T) {
	if len(h3VideoFramePromptPresets) < 3 {
		t.Fatalf("expected at least 3 presets, got %d", len(h3VideoFramePromptPresets))
	}
	seen := map[string]bool{}
	for _, p := range h3VideoFramePromptPresets {
		if p.ID == "" || p.Label == "" || p.Text == "" {
			t.Errorf("preset has empty field: %+v", p)
		}
		if seen[p.ID] {
			t.Errorf("duplicate preset id %q", p.ID)
		}
		seen[p.ID] = true
		if strings.TrimSpace(p.Text) != p.Text {
			t.Errorf("preset %q text has leading/trailing whitespace", p.ID)
		}
		if !strings.Contains(p.Text, "慢动作") {
			t.Errorf("preset %q must disambiguate slow motion", p.ID)
		}
		if n := len([]rune(p.Text)); n < 60 || n > 400 {
			t.Errorf("preset %q rune length = %d, want 60-400", p.ID, n)
		}
	}
	if got := h3VideoFrameDefaultPrompt(); got != h3VideoFramePromptPresets[0].Text {
		t.Error("default prompt must equal the first preset text")
	}
}

func TestH3TargetFrameIndex(t *testing.T) {
	cases := []struct {
		total int
		pick  string
		want  int
	}{
		{5, H3FramePickFirst, 0},
		{5, H3FramePickMiddle, 2},
		{5, H3FramePickLast, 4},
		{5, "", 2},
		{5, "weird", 2},
		{1, H3FramePickMiddle, 0},
		{1, H3FramePickLast, 0},
		{0, H3FramePickMiddle, 0},
		{0, H3FramePickLast, 0},
		{5, "LAST", 4},
	}
	for _, c := range cases {
		if got := h3TargetFrameIndex(c.total, c.pick); got != c.want {
			t.Errorf("h3TargetFrameIndex(%d, %q) = %d, want %d", c.total, c.pick, got, c.want)
		}
	}
}
