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

// h3VideoFramePromptPreset 是 H3 抽帧附加提示词的内置预设，供设置页下拉框直接选用。
type h3VideoFramePromptPreset struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Text  string `json:"text"`
}

// h3VideoFramePromptPresets 是预设文本的唯一真源（设置页下拉与默认值都取自这里），首项即默认值。
// 注意：H3 t2v 工作流没有 negative 输入，所有禁止项只能写进正向提示词并置于末尾。
var h3VideoFramePromptPresets = []h3VideoFramePromptPreset{
	{
		ID:    "compact",
		Label: "精简版（推荐）",
		Text:  "固定机位、固定焦段、固定构图的静态锁定镜头，全片是同一时刻的一张高分辨率照片，完全冻结，不是慢动作也不是升格。全片所有帧完全一致，人物五官、发型、服装、姿势与位置无漂移变形，曝光、色温、色调、锐度保持不变。上文如有动作、运动或镜头调度描述，一律理解为该动作的定格瞬间，不产生实际运动。禁止：相机运动与镜头切换、眨眼、呼吸、口型与表情变化、头发与衣物摆动、风吹叶动、水面波动、烟雾尘埃、人群走动、运动模糊与拖影、噪点闪烁与纹理游移、明暗色彩跳变、字幕文字水印、声音。",
	},
	{
		ID:    "full",
		Label: "完整版（约束最多，最稳定）",
		Text:  "静态锁定镜头、固定机位与焦段、固定构图，全片为同一时刻的一张高分辨率照片，完全冻结，非慢动作、非升格、非定格动画。全片所有帧完全相同：人物五官、发型、发色、服装、配饰、姿势与位置完全一致，无漂移、无变形、无人数增减；曝光、白平衡、色温、对比度、饱和度、锐度、颗粒全片不变；无景深变化、无自动对焦呼吸、无焦段变化、无视差。上文任何动作、运动、镜头调度或情绪变化描述，一律理解为该动作的定格瞬间，不产生实际运动。严格禁止：推、拉、摇、移、跟、升、降、环绕、手持抖动、镜头切换、淡入淡出、黑帧白闪；眨眼、呼吸起伏、口型变化、表情渐变、肢体移动、头发与衣物摆动；风、云移、树叶与窗帘与旗帜飘动、水面波动、烟雾、尘埃、雨雪、火花、影子与光斑移动、人群走动、车辆驶过、物体进出画面；运动模糊、拖影、重影、残影、双重曝光；噪点闪烁、纹理游移、摩尔纹、压缩伪影跳动、亮度与色彩跳变；字幕、文字、水印；声音。",
	},
	{
		ID:    "minimal",
		Label: "极简版（怕稀释场景描述时用）",
		Text:  "静态锁定镜头，固定机位与焦段，全片是同一时刻的高分辨率照片，完全冻结（非慢动作、非升格）；全片所有帧一致，人物与场景无漂移变形；上文动作描述一律理解为定格瞬间。无相机运动与镜头切换，无运动模糊与拖影，无闪烁与明暗跳变，无声音。",
	},
}

func h3VideoFrameDefaultPrompt() string {
	if len(h3VideoFramePromptPresets) == 0 {
		return ""
	}
	return h3VideoFramePromptPresets[0].Text
}

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
	if err := extractFrameFromVideo(tempVideoPath, savePath, getConfiguredH3FramePick()); err != nil {
		return "", err
	}
	return "/" + filepath.ToSlash(savePath), nil
}

// extractFrameFromVideo 用 ffprobe 取总帧数后，按 pick（first/middle/last）抽取一帧。
func extractFrameFromVideo(videoAbsPath string, pngAbsPath string, pick string) error {
	totalFrames, _, err := ffprobeVideoFramesAndFPS(videoAbsPath)
	if err != nil {
		return err
	}
	targetFrame := h3TargetFrameIndex(totalFrames, pick)
	filter := fmt.Sprintf("select=eq(n\\,%d)", targetFrame)
	return runFFmpeg("-i", videoAbsPath, "-vf", filter, "-frames:v", "1", pngAbsPath, "-y")
}

// appendH3VideoFrameStaticPrompt 仅在 H3 抽帧模式下，把用户配置的附加提示词追加到提示词末尾。
// 未启用抽帧模式或配置为空时原样返回。
func appendH3VideoFrameStaticPrompt(prompt string) string {
	if !useH3VideoFrameMode() {
		return prompt
	}
	return mergeH3StaticPrompt(prompt, getConfiguredH3VideoFramePrompt())
}

// mergeH3StaticPrompt 把附加提示词拼到提示词末尾；附加词为空或已包含时原样返回。
func mergeH3StaticPrompt(prompt string, extra string) string {
	trimmedExtra := strings.TrimSpace(extra)
	if trimmedExtra == "" {
		return prompt
	}
	base := strings.TrimSpace(prompt)
	if strings.Contains(base, trimmedExtra) {
		return prompt
	}
	if base == "" {
		return trimmedExtra
	}
	return base + "\n" + trimmedExtra
}

// h3TargetFrameIndex 把抽帧位置映射为帧序号（越界时夹到合法范围）。
func h3TargetFrameIndex(totalFrames int, pick string) int {
	targetFrame := 0
	switch normalizeH3FramePick(pick) {
	case H3FramePickFirst:
		targetFrame = 0
	case H3FramePickLast:
		targetFrame = totalFrames - 1
	default:
		targetFrame = totalFrames / 2
	}
	if targetFrame < 0 {
		return 0
	}
	if totalFrames > 0 && targetFrame > totalFrames-1 {
		return totalFrames - 1
	}
	return targetFrame
}
