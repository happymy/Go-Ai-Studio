package api

import (
	"math"
	"strings"
	"testing"

	"kt-ai-studio/internal/models"
)

func TestNormalizeTextForMatch(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"今晚打烊", "今晚打烊"},
		{"今晚 打烊", "今晚打烊"},
		{"aＢc", "aBc"},          // 全角 B 转半角
		{"“今晚打烊。”", "今晚打烊"},     // 引号/句号剔除
		{"沈西风，你来了！？", "沈西风你来了"}, // 标点剔除
		{"", ""},
		{"   ", ""},
	}
	for _, tc := range cases {
		if got := normalizeTextForMatch(tc.in); got != tc.want {
			t.Errorf("normalizeTextForMatch(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCheckDialogueCoverage(t *testing.T) {
	plot := "王五说：“今晚打烊。” 沈西风追问：“李三在哪？”"

	// 覆盖一句，另一句缺失
	scenes := []lightweightStoryScene{
		{SceneID: 1, Narration: "王五关上门板", VideoPrompt: "王五说今晚打烊，油灯晃动"},
		{SceneID: 2, VideoPrompt: "沈西风拍桌质问"},
	}
	missing := checkDialogueCoverage(plot, scenes)
	if len(missing) != 1 || missing[0] != "李三在哪？" {
		t.Fatalf("expected 1 missing dialogue, got %v", missing)
	}

	// 全部覆盖
	scenesAll := []lightweightStoryScene{
		{SceneID: 1, VideoPrompt: "王五说今晚打烊也就关门了"},
		{SceneID: 2, VideoPrompt: "沈西风追问李三在哪，王五低头"},
	}
	if missing := checkDialogueCoverage(plot, scenesAll); len(missing) != 0 {
		t.Fatalf("expected no missing dialogues, got %v", missing)
	}

	// 无引号台词
	if missing := checkDialogueCoverage("纯叙述，没有对白。", scenes); len(missing) != 0 {
		t.Fatalf("expected nil when no quoted dialogues, got %v", missing)
	}

	// 空 scene 列表 → 全部缺失
	if missing := checkDialogueCoverage(plot, nil); len(missing) != 2 {
		t.Fatalf("expected all dialogues missing with empty scenes, got %v", missing)
	}
}

func TestCheckNarrationRatio(t *testing.T) {
	scenesAll := []lightweightStoryScene{
		{Narration: "夜幕降临", VideoPrompt: "商贩们收摊，油灯一盏盏熄灭", DurationSeconds: 5},
	}
	ratio := checkNarrationRatio(scenesAll) // 4 / (4+13) ≈ 0.235
	if math.Abs(ratio-0.235) > 0.03 {
		t.Fatalf("expected ratio ~0.235, got %.2f", ratio)
	}

	pure := []lightweightStoryScene{{Narration: "旁白旁白旁白", VideoPrompt: ""}}
	if ratio := checkNarrationRatio(pure); ratio != 1.0 {
		t.Fatalf("expected ratio 1.0 for pure narration, got %.2f", ratio)
	}

	if ratio := checkNarrationRatio(nil); ratio != 0 {
		t.Fatalf("expected ratio 0 for empty scenes, got %.2f", ratio)
	}
}

func TestCheckGhostCharacters(t *testing.T) {
	known := []string{"沈西风", "王五", "李三"}
	scenes := []lightweightStoryScene{
		{SceneID: 1, Characters: []string{"沈西风", "王五"}},
		{SceneID: 2, Characters: []string{"沈西风", "赵六"}},
		{SceneID: 3, Characters: []string{"赵六"}}, // 重复幽灵角色去重
	}
	ghosts := checkGhostCharacters(scenes, known)
	if len(ghosts) != 1 || ghosts[0] != "赵六" {
		t.Fatalf("expected ghost [赵六], got %v", ghosts)
	}

	clean := []lightweightStoryScene{{SceneID: 1, Characters: []string{"沈西风", "王五"}}}
	if ghosts := checkGhostCharacters(clean, known); len(ghosts) != 0 {
		t.Fatalf("expected no ghosts, got %v", ghosts)
	}
}

func TestBuildLightweightStoryQualityReportPerfect(t *testing.T) {
	payload := &lightweightStoryResponse{
		TotalScenes: 2,
		Characters: []lightweightStoryCharacter{
			{Name: "沈西风", Alias: []string{"沈公子"}},
			{Name: "王五"},
		},
		Scenes: []lightweightStoryScene{
			{SceneID: 1, DurationSeconds: 5, Narration: "王五说今晚打烊", ImagePrompt: "客栈夜", VideoPrompt: "王五说今晚打烊，随后将门板一块块合上，油灯在风中晃动", Objective: "王五想收摊", Turn: "沈西风进来", Location: "客栈内夜", Characters: []string{"王五"}},
			{SceneID: 2, DurationSeconds: 5, Narration: "沈西风追问", ImagePrompt: "客栈夜", VideoPrompt: "沈西风追问李三在哪，王五低头避开目光，手按在账本上", Objective: "查明李三下落", Turn: "发现破绽", Location: "客栈内夜", Characters: []string{"沈西风"}},
		},
	}
	plot := "王五说：“今晚打烊。” 沈西风追问：“李三在哪？”"
	report := buildLightweightStoryQualityReport(payload, nil, plot)
	if report.Score != 100 {
		t.Fatalf("expected score 100, got %d (issues: %v)", report.Score, report.Issues)
	}
	if len(report.Issues) != 0 {
		t.Fatalf("expected no issues, got %v", report.Issues)
	}
}

func TestBuildLightweightStoryQualityReportCatchesProblems(t *testing.T) {
	payload := &lightweightStoryResponse{
		TotalScenes: 2,
		Characters:  []lightweightStoryCharacter{{Name: "沈西风"}, {Name: "王五"}},
		Scenes: []lightweightStoryScene{
			{SceneID: 1, DurationSeconds: 5, Narration: "王五说今晚打烊", ImagePrompt: "", VideoPrompt: "王五说今晚打烊", Objective: "收摊", Characters: []string{"王五", "赵六"}}, // 空 image_prompt + 幽灵角色赵六
			{SceneID: 2, DurationSeconds: 0, Narration: "沈西风追问", ImagePrompt: "客栈", VideoPrompt: "沈西风看窗外", Characters: []string{"沈西风"}},                        // 时长非法 + objective 缺失 + 台词未落位
		},
	}
	plot := "王五说：“今晚打烊。” 沈西风追问：“李三在哪？”" // "李三在哪" 未出现在任何 scene → 台词缺失
	report := buildLightweightStoryQualityReport(payload, nil, plot)
	if report.Score >= 100 {
		t.Fatalf("expected score < 100, got %d", report.Score)
	}
	if report.Structure >= 40 || report.Format >= 30 || report.Content >= 30 {
		t.Fatalf("each dimension should be penalized: %+v", report)
	}
	if len(report.DialogueLoss) != 1 {
		t.Errorf("expected 1 dialogue loss, got %v", report.DialogueLoss)
	}
	if len(report.GhostCharacters) != 1 || report.GhostCharacters[0] != "赵六" {
		t.Errorf("expected ghost 赵六, got %v", report.GhostCharacters)
	}
	if len(report.Issues) == 0 {
		t.Errorf("expected issues, got none")
	}
}

// scene_id 覆盖 1..N 完整性：LLM 数组乱序（如 [2,1]）不扣分（persist 前会按 id 排序），缺号才扣分。
func TestBuildLightweightStoryQualityReportSceneIDCompleteness(t *testing.T) {
	baseScenes := func(ids ...int) []lightweightStoryScene {
		scenes := make([]lightweightStoryScene, 0, len(ids))
		for _, id := range ids {
			scenes = append(scenes, lightweightStoryScene{
				SceneID:         id,
				DurationSeconds: 5,
				Narration:       "王五说今晚打烊",
				ImagePrompt:     "客栈夜",
				VideoPrompt:     "王五说今晚打烊，随后将门板一块块合上",
				Objective:       "收摊",
				Turn:            "沈西风进来",
				Location:        "客栈内夜",
				Characters:      []string{"王五"},
			})
		}
		return scenes
	}

	// 乱序但完整（1,3,2）→ 不因顺序扣结构分
	outOfOrder := &lightweightStoryResponse{
		TotalScenes: 3,
		Characters:  []lightweightStoryCharacter{{Name: "王五"}},
		Scenes:      baseScenes(1, 3, 2),
	}
	report := buildLightweightStoryQualityReport(outOfOrder, nil, "王五说：“今晚打烊。”")
	for _, issue := range report.Issues {
		if strings.Contains(issue, "scene_id") {
			t.Errorf("out-of-order but complete scene ids should not be penalized: %v", report.Issues)
		}
	}

	// 缺号（1,2,4，覆盖 1..3 缺 3）→ 结构扣 10 分并给出缺号提示
	gap := &lightweightStoryResponse{
		TotalScenes: 3,
		Characters:  []lightweightStoryCharacter{{Name: "王五"}},
		Scenes:      baseScenes(1, 2, 4),
	}
	report2 := buildLightweightStoryQualityReport(gap, nil, "王五说：“今晚打烊。”")
	if report2.Structure != 30 {
		t.Errorf("missing scene_id should cost 10 structure points, got %d (issues: %v)", report2.Structure, report2.Issues)
	}
	found := false
	for _, issue := range report2.Issues {
		if strings.Contains(issue, "缺号") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected missing scene_id issue, got %v", report2.Issues)
	}
}

func TestBuildLightweightStoryQualityReportNil(t *testing.T) {
	report := buildLightweightStoryQualityReport(nil, nil, "")
	if report.Score != 0 || len(report.Issues) == 0 {
		t.Fatalf("nil payload should produce 0 score with issues, got %+v", report)
	}
	if !strings.Contains(report.Issues[0], "nil") {
		t.Errorf("expected nil issue message, got %v", report.Issues[0])
	}
}

func TestBuildLightweightStoryQualityReportMarkdown(t *testing.T) {
	report := lightweightStoryQualityReport{
		Score:           85,
		Structure:       40,
		Format:          25,
		Content:         20,
		DialogueLoss:    []string{"李三在哪？"},
		NarrationRatio:  0.42,
		GhostCharacters: []string{"赵六"},
		Issues:          []string{"格式：scene 1 video_prompt 为空", "内容：旁白占比 42% 超过软阈值"},
	}
	markdown := buildLightweightStoryQualityReportMarkdown(report, models.AutoGenerateRequest{GenerationMode: "h3_short", Episode: 3})
	for _, keyword := range []string{"结构", "40/40", "格式", "25/30", "内容", "20/30", "85/100", "旁白占比：42%", "台词缺失：1 条", "幽灵角色：1 个", "h3_short", "第 3 集", "赵六"} {
		if !strings.Contains(markdown, keyword) {
			t.Errorf("markdown missing keyword %q\n%s", keyword, markdown)
		}
	}
	if !strings.Contains(markdown, "问题清单：\n- ") {
		t.Errorf("expected issues list in markdown\n%s", markdown)
	}

	clean := lightweightStoryQualityReport{Score: 100, Structure: 40, Format: 30, Content: 30}
	cleanMarkdown := buildLightweightStoryQualityReportMarkdown(clean, models.AutoGenerateRequest{GenerationMode: "standard", Episode: 1})
	if !strings.Contains(cleanMarkdown, "问题清单：无") {
		t.Errorf("expected 无 issues for clean report\n%s", cleanMarkdown)
	}
}

func TestCheckObjectiveTurnArticulation(t *testing.T) {
	// 正常：目标/转折各自成立且有变化
	ok := []lightweightStoryScene{
		{SceneID: 1, Objective: "王五想收摊", Turn: "沈西风推门进来", Narration: "王五开始搬门板"},
		{SceneID: 2, Objective: "查明李三下落", Turn: "发现账本破绽", Narration: "沈西风翻开账本"},
	}
	if issues := checkObjectiveTurnArticulation(ok); len(issues) != 0 {
		t.Fatalf("expected no issues for healthy scenes, got %v", issues)
	}

	// 转折是目标的复述 → 告警
	repetitive := []lightweightStoryScene{
		{SceneID: 1, Objective: "沈西风要找到李三", Turn: "沈西风要找到李三", Narration: "沈西风四处打听"},
	}
	issues := checkObjectiveTurnArticulation(repetitive)
	if len(issues) != 1 || !strings.Contains(issues[0], "复述") {
		t.Fatalf("expected turn-repeats-objective issue, got %v", issues)
	}

	// objective 存在但无 narration 落实 → 告警
	noFallout := []lightweightStoryScene{
		{SceneID: 1, Objective: "拿下城门", Turn: "守军溃散", Narration: "   "},
	}
	issues = checkObjectiveTurnArticulation(noFallout)
	if len(issues) != 1 || !strings.Contains(issues[0], "narration") {
		t.Fatalf("expected objective-without-narration issue, got %v", issues)
	}

	// 相邻两场 turn 相同 → 状态停滞告警
	stalled := []lightweightStoryScene{
		{SceneID: 1, Objective: "a", Turn: "僵持不下", Narration: "x"},
		{SceneID: 2, Objective: "b", Turn: "僵持不下", Narration: "y"},
	}
	issues = checkObjectiveTurnArticulation(stalled)
	if len(issues) != 1 || !strings.Contains(issues[0], "未推进") {
		t.Fatalf("expected stalled-turn issue, got %v", issues)
	}

	// 字段全空 → 不告警
	if issues := checkObjectiveTurnArticulation([]lightweightStoryScene{{SceneID: 1}}); len(issues) != 0 {
		t.Fatalf("expected no issues for empty fields, got %v", issues)
	}
}

func TestCheckCastPresenceInSceneBody(t *testing.T) {
	// 出场角色在正文有可见痕迹 → 通过
	visible := []lightweightStoryScene{
		{SceneID: 1, Characters: []string{"沈西风", "王五"}, Narration: "沈西风推门，王五起身", ImagePrompt: "客栈夜", VideoPrompt: "沈西风走到柜台前，王五低头"},
	}
	if missing := checkCastPresenceInSceneBody(visible); len(missing) != 0 {
		t.Fatalf("expected no missing cast, got %v", missing)
	}

	// 出场角色在正文无任何痕迹 → 告警
	invisible := []lightweightStoryScene{
		{SceneID: 1, Characters: []string{"沈西风", "赵六"}, Narration: "沈西风推门", VideoPrompt: "沈西风打量四周", ImagePrompt: "客栈空无一人"},
	}
	missing := checkCastPresenceInSceneBody(invisible)
	if len(missing) != 1 || !strings.Contains(missing[0], "赵六") {
		t.Fatalf("expected 赵六 missing, got %v", missing)
	}

	// 无出场角色标注 → 跳过
	if missing := checkCastPresenceInSceneBody([]lightweightStoryScene{{SceneID: 1, Narration: "空镜"}}); len(missing) != 0 {
		t.Fatalf("expected no issue without cast list, got %v", missing)
	}
}

func TestBuildLightweightStoryQualityReportObjectiveTurnPenalty(t *testing.T) {
	payload := &lightweightStoryResponse{
		TotalScenes: 1,
		Characters:  []lightweightStoryCharacter{{Name: "沈西风"}},
		Scenes: []lightweightStoryScene{
			{SceneID: 1, DurationSeconds: 5, Narration: "沈西风四处打听", ImagePrompt: "客栈夜", VideoPrompt: "沈西风四处打听李三下落",
				Objective: "沈西风要找到李三", Turn: "沈西风要找到李三", Location: "客栈内夜", Characters: []string{"沈西风"}}, // 转折=目标复述
		},
	}
	report := buildLightweightStoryQualityReport(payload, nil, "")
	if len(report.ObjectiveTurn) != 1 || !strings.Contains(report.ObjectiveTurn[0], "复述") {
		t.Fatalf("expected turn-repeats-objective in report, got %v", report.ObjectiveTurn)
	}
	if report.Score >= 100 {
		t.Fatalf("objective/turn articulation problem should lower score, got %d", report.Score)
	}
	if !strings.Contains(report.Issues[0], "内容") {
		t.Errorf("objective/turn issues should surface in issues list, got %v", report.Issues)
	}
	markdown := buildLightweightStoryQualityReportMarkdown(report, models.AutoGenerateRequest{GenerationMode: "h3_short", Episode: 1})
	if !strings.Contains(markdown, "目标达成问题：1 条") {
		t.Errorf("markdown should count objective/turn issues\n%s", markdown)
	}
}
