package api

import (
	"encoding/json"
	"os"
	"path/filepath"
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
