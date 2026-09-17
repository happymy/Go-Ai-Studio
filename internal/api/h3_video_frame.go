package api

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kt-ai-studio/internal/models"
)

const (
	h3T2VWorkflowFileName       = "minimax_h3_t2v-gguf-api.json"
	h3VideoFrameDurationSeconds = 0.1
	h3VideoFrameMaxPixels       = 980000
	h3VideoFrameSizeMultiple    = 16
)

// useH3VideoFrameMode 判断当前图片生成是否走 MiniMax H3 短视频抽帧。
func useH3VideoFrameMode() bool {
	return getConfiguredImageGenMode() == ImageGenModeH3VideoFrame
}

// findH3T2VWorkflowFile 按文件名定位内置 H3 t2v 工作流。
// 该工作流的 SaveVideo prefix 与 i2v 相同，不能依赖 WorkflowName 匹配。
func findH3T2VWorkflowFile() (string, error) {
	files, _ := filepath.Glob(filepath.Join("workflows", "*.json"))
	for _, file := range files {
		if strings.EqualFold(filepath.Base(file), h3T2VWorkflowFileName) {
			return file, nil
		}
	}
	return "", fmt.Errorf("H3 t2v workflow '%s' not found", h3T2VWorkflowFileName)
}

// normalizeH3VideoFrameSize 保持宽高比等比缩小至 H3 分辨率上限内，并对齐到 16 的倍数。
func normalizeH3VideoFrameSize(width, height int) (int, int) {
	if width <= 0 || height <= 0 {
		width, height = 768, 1344
	}
	if width*height > h3VideoFrameMaxPixels {
		scale := math.Sqrt(float64(h3VideoFrameMaxPixels) / float64(width*height))
		width = int(math.Floor(float64(width) * scale))
		height = int(math.Floor(float64(height) * scale))
	}
	return alignDownH3Size(width), alignDownH3Size(height)
}

func alignDownH3Size(value int) int {
	aligned := value / h3VideoFrameSizeMultiple * h3VideoFrameSizeMultiple
	if aligned < h3VideoFrameSizeMultiple {
		return h3VideoFrameSizeMultiple
	}
	return aligned
}

// injectH3T2VParams 注入提示词/种子/尺寸/时长。
func injectH3T2VParams(wfJSON map[string]interface{}, meta *models.WorkflowMetadata, positivePrompt string, seed int64, width, height int) {
	setInput := func(nodeID string, key string, value interface{}) {
		if nodeID == "" {
			return
		}
		if node, ok := wfJSON[nodeID].(map[string]interface{}); ok {
			if inputs, ok := node["inputs"].(map[string]interface{}); ok {
				inputs[key] = value
			}
		}
	}
	setInput(meta.PositiveNodeID, meta.PositiveInputKey, positivePrompt)
	if meta.NegativeNodeID != "" {
		setInput(meta.NegativeNodeID, meta.NegativeInputKey, "")
	}
	setInput(meta.SeedNodeID, meta.SeedInputKey, seed)
	setInput(meta.WidthNodeID, meta.WidthInputKey, width)
	setInput(meta.HeightNodeID, meta.HeightInputKey, height)
	injectH3Duration(wfJSON, h3VideoFrameDurationSeconds)
}

// injectH3Duration 找到 title 含 duration 的 PrimitiveFloat（ComfyMathExpression 的时长输入）并设为指定秒数。
func injectH3Duration(wfJSON map[string]interface{}, seconds float64) {
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
		if !strings.Contains(strings.ToLower(title), "duration") {
			continue
		}
		if inputs, ok := nodeMap["inputs"].(map[string]interface{}); ok {
			inputs["value"] = seconds
		}
	}
}

// isVideoOutputFilename 通过扩展名判断 ComfyUI 输出是视频而非图片。
func isVideoOutputFilename(name string) bool {
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(name))) {
	case ".mp4", ".webm", ".mov", ".mkv", ".gif":
		return true
	default:
		return false
	}
}

// resolveImageOrVideoOutput 从 ComfyUI 单节点输出中取出首个媒体引用，并判断其是否为视频。
func resolveImageOrVideoOutput(nodeOutput map[string]interface{}) (map[string]interface{}, bool, bool) {
	if videoData, ok := firstComfyOutputItem(nodeOutput, "gifs"); ok {
		return videoData, true, true
	}
	imgData, ok := firstComfyOutputItem(nodeOutput, "images")
	if !ok {
		return nil, false, false
	}
	filename, _ := imgData["filename"].(string)
	if filename == "" {
		return nil, false, false
	}
	return imgData, isVideoOutputFilename(filename), true
}

func firstComfyOutputItem(nodeOutput map[string]interface{}, key string) (map[string]interface{}, bool) {
	items, ok := nodeOutput[key].([]interface{})
	if !ok || len(items) == 0 {
		return nil, false
	}
	item, ok := items[0].(map[string]interface{})
	if !ok {
		return nil, false
	}
	return item, true
}

// downloadHistoryVideoAndExtractFrame 下载视频输出并抽取中间帧为 PNG，返回 web 路径。
func downloadHistoryVideoAndExtractFrame(fileData map[string]interface{}, saveDir string, saveFilename string) (string, error) {
	filename, _ := fileData["filename"].(string)
	subfolder, _ := fileData["subfolder"].(string)
	typeStr, _ := fileData["type"].(string)
	if filename == "" {
		return "", fmt.Errorf("empty video filename in comfyui output")
	}
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		return "", err
	}

	ext := filepath.Ext(filename)
	if ext == "" {
		ext = ".mp4"
	}
	tempVideoPath := filepath.Join(saveDir, fmt.Sprintf(".h3_tmp_%d%s", time.Now().UnixNano(), ext))
	if err := DownloadComfyImage(filename, subfolder, typeStr, tempVideoPath); err != nil {
		return "", err
	}
	defer os.Remove(tempVideoPath)

	savePath := filepath.Join(saveDir, saveFilename)
	if err := extractMiddleFrameFromVideo(tempVideoPath, savePath); err != nil {
		return "", err
	}
	return "/" + filepath.ToSlash(savePath), nil
}

// extractMiddleFrameFromVideo 用 ffprobe 取总帧数后，用 ffmpeg 抽取正中一帧。
func extractMiddleFrameFromVideo(videoAbsPath string, pngAbsPath string) error {
	totalFrames, _, err := ffprobeVideoFramesAndFPS(videoAbsPath)
	if err != nil {
		return err
	}
	middleFrame := totalFrames / 2
	if middleFrame < 0 {
		middleFrame = 0
	}
	filter := fmt.Sprintf("select=eq(n\\,%d)", middleFrame)
	return runFFmpeg("-i", videoAbsPath, "-vf", filter, "-frames:v", "1", pngAbsPath, "-y")
}
