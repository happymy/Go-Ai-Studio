package api

import (
	"strings"
	"testing"
)

const testNovelText = `客栈夜话

入夜，客栈大堂烛火摇曳。剑客沈西风推门而入，一身灰衣沾着泥。
掌柜李三抬头，堆笑迎上："客官，打尖还是住店？"
沈西风将剑搁在桌上，沉声道："一壶酒，一间房，顺便问问——最近可有人打听我？"
李三脸色微变，正要答话，楼上传来一声冷笑。蒙面人倚在栏杆边，缓缓开口："沈西风，三年了，你欠我的命，该还了。"
沈西风握剑起身，大厅气氛骤然凝固。李三悄悄退到柜台后。两人隔着大堂对峙，烛火猛地一暗。`

const testNovelOutlineJSON = `{
  "plot_line": "剑客沈西风夜宿客栈，被三年宿敌蒙面人寻仇，掌柜李三卷入对峙，气氛剑拔弩张。",
  "characters": [
    {"name": "沈西风", "gender": "男性", "age": "30岁", "appearance": "灰衣剑客，身形清瘦", "role": "主角", "alias": ["沈公子"]},
    {"name": "李三", "gender": "男性", "age": "50多岁", "appearance": "矮胖掌柜", "role": "配角", "alias": []},
    {"name": "蒙面人", "gender": "未知", "age": "未知", "appearance": "黑色斗篷蒙面", "role": "反派", "alias": []}
  ],
  "scene_plan": [
    {"scene_id": 1, "location": "客栈大堂 内 夜", "time_of_day": "夜", "cast": ["沈西风", "李三"], "core_event": "沈西风入住客栈并打听消息"},
    {"scene_id": 2, "location": "客栈大堂 内 夜", "time_of_day": "夜", "cast": ["沈西风", "李三", "蒙面人"], "core_event": "蒙面人现身寻仇，双方对峙"}
  ]
}`

const testNovelScriptJSON = `{
  "novel_title": "客栈夜话",
  "total_scenes": 2,
  "characters": [
    {"name": "沈西风", "gender": "男性", "age": "30岁", "appearance": "灰衣剑客，身形清瘦", "role": "主角", "alias": ["沈公子"]},
    {"name": "李三", "gender": "男性", "age": "50多岁", "appearance": "矮胖掌柜", "role": "配角", "alias": []},
    {"name": "蒙面人", "gender": "未知", "age": "未知", "appearance": "黑色斗篷蒙面", "role": "反派", "alias": []}
  ],
  "scenes": [
    {
      "scene_id": 1,
      "location": "客栈大堂 内 夜",
      "time_of_day": "夜",
      "cast": ["沈西风", "李三"],
      "summary": "沈西风入住客栈并打听消息，掌柜李三接待。",
      "units": [
        {"unit_type": "action", "character": "沈西风", "content": "推门而入，一身灰衣沾着泥，环顾大堂"},
        {"unit_type": "dialogue", "character": "李三", "content": "客官，打尖还是住店？"},
        {"unit_type": "dialogue", "character": "沈西风", "content": "一壶酒，一间房，顺便问问——最近可有人打听我？"}
      ]
    },
    {
      "scene_id": 2,
      "location": "客栈大堂 内 夜",
      "time_of_day": "夜",
      "cast": ["沈西风", "李三", "蒙面人"],
      "summary": "蒙面人现身寻仇，双方在大堂对峙。",
      "units": [
        {"unit_type": "action", "character": "蒙面人", "content": "倚在二楼栏杆边冷笑"},
        {"unit_type": "dialogue", "character": "蒙面人", "content": "沈西风，三年了，你欠我的命，该还了。"},
        {"unit_type": "action", "character": "沈西风", "content": "握剑起身，与蒙面人隔堂对峙"},
        {"unit_type": "narration", "content": "烛火猛地一暗，气氛凝固到极点。"}
      ]
    }
  ]
}`

func TestBuildNovelToScriptPrompts(t *testing.T) {
	sysOutline, userOutline, err := buildNovelToScriptOutlinePrompts("客栈夜话", testNovelText)
	if err != nil {
		t.Fatalf("buildNovelToScriptOutlinePrompts returned error: %v", err)
	}
	if !strings.Contains(sysOutline, "改编大纲") || !strings.Contains(sysOutline, "scene_plan") {
		t.Errorf("outline system prompt missing key sections")
	}
	if !strings.Contains(userOutline, testNovelText) {
		t.Errorf("outline user prompt missing novel text")
	}

	sysScenes, userScenes, err := buildNovelToScriptScenePrompts("客栈夜话", testNovelText, testNovelOutlineJSON)
	if err != nil {
		t.Fatalf("buildNovelToScriptScenePrompts returned error: %v", err)
	}
	if !strings.Contains(sysScenes, "dialogue") || !strings.Contains(sysScenes, "narration") {
		t.Errorf("scene system prompt missing unit type rules")
	}
	if !strings.Contains(userScenes, testNovelOutlineJSON) {
		t.Errorf("scene user prompt missing outline")
	}

	if _, _, err := buildNovelToScriptOutlinePrompts("", ""); err == nil {
		t.Errorf("expected error for empty novel text")
	}
	if _, _, err := buildNovelToScriptScenePrompts("", testNovelText, ""); err == nil {
		t.Errorf("expected error for empty outline")
	}
}

func TestParseNovelToScriptOutlineValid(t *testing.T) {
	outline, err := parseNovelToScriptOutline(testNovelOutlineJSON)
	if err != nil {
		t.Fatalf("parseNovelToScriptOutline returned error: %v", err)
	}
	if outline.PlotLine == "" {
		t.Errorf("plot_line should not be empty")
	}
	if len(outline.Characters) != 3 {
		t.Errorf("expected 3 characters, got %d", len(outline.Characters))
	}
	if len(outline.ScenePlan) != 2 {
		t.Errorf("expected 2 scene plans, got %d", len(outline.ScenePlan))
	}
	if len(outline.Characters[0].Alias) == 0 {
		t.Errorf("alias should be preserved")
	}
}

func TestParseNovelToScriptValid(t *testing.T) {
	payload, err := parseNovelToScript(testNovelScriptJSON)
	if err != nil {
		t.Fatalf("parseNovelToScript returned error: %v", err)
	}
	if payload.TotalScenes != 2 || len(payload.Scenes) != 2 {
		t.Errorf("expected total_scenes=2, got %d/%d", payload.TotalScenes, len(payload.Scenes))
	}
	if payload.NovelTitle != "客栈夜话" {
		t.Errorf("unexpected novel title: %q", payload.NovelTitle)
	}
	if len(payload.Scenes[0].Units) != 3 {
		t.Errorf("scene 1 expected 3 units, got %d", len(payload.Scenes[0].Units))
	}
	if payload.Scenes[1].Units[3].UnitType != "narration" {
		t.Errorf("scene 2 unit 4 should be narration")
	}
}

func TestParseNovelToScriptErrors(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string
	}{
		{
			name: "not json object",
			json: "这根本不是 JSON 内容，也没有任何大括号。",
			want: "JSON object only",
		},
		{
			name: "empty response",
			json: "   ",
			want: "empty llm response",
		},
		{
			name: "empty scenes",
			json: `{"total_scenes":0,"character":[],"scenes":[]}`,
			want: "scenes must not be empty",
		},
		{
			name: "total scenes mismatch",
			json: strings.Replace(testNovelScriptJSON, `"total_scenes": 2`, `"total_scenes": 3`, 1),
			want: "does not match",
		},
		{
			name: "scene id not contiguous",
			json: strings.Replace(testNovelScriptJSON, `"scene_id": 2`, `"scene_id": 3`, 1),
			want: "not contiguous",
		},
		{
			name: "dialogue without character",
			json: strings.Replace(testNovelScriptJSON,
				`{"unit_type": "dialogue", "character": "李三", "content": "客官，打尖还是住店？"}`,
				`{"unit_type": "dialogue", "content": "客官，打尖还是住店？"}`, 1),
			want: "character is required",
		},
		{
			name: "dialogue character not in cast",
			json: strings.Replace(testNovelScriptJSON,
				`{"unit_type": "dialogue", "character": "李三", "content": "客官，打尖还是住店？"}`,
				`{"unit_type": "dialogue", "character": "蒙面人", "content": "客官，打尖还是住店？"}`, 1),
			want: "not in cast",
		},
		{
			name: "narration carries character",
			json: `{"total_scenes":1,"characters":[{"name":"张三"}],"scenes":[{"scene_id":1,"location":"某处 内 日","summary":"s","cast":["张三"],"units":[{"unit_type":"narration","character":"张三","content":"旁白"}]}]}`,
			want: "must not carry character",
		},
		{
			name: "invalid unit type",
			json: `{"total_scenes":1,"characters":[{"name":"张三"}],"scenes":[{"scene_id":1,"location":"某处 内 日","summary":"s","cast":["张三"],"units":[{"unit_type":"psy","character":"张三","content":"心理"}]}]}`,
			want: "invalid unit_type",
		},
		{
			name: "empty content",
			json: `{"total_scenes":1,"characters":[{"name":"张三"}],"scenes":[{"scene_id":1,"location":"某处 内 日","summary":"s","cast":["张三"],"units":[{"unit_type":"action","character":"张三","content":"  "}]}]}`,
			want: "content is required",
		},
		{
			name: "empty location",
			json: `{"total_scenes":1,"characters":[{"name":"张三"}],"scenes":[{"scene_id":1,"location":"  ","summary":"s","cast":["张三"],"units":[{"unit_type":"action","character":"张三","content":"动作"}]}]}`,
			want: "location is required",
		},
		{
			name: "cast member not in characters",
			json: `{"total_scenes":1,"characters":[{"name":"张三"}],"scenes":[{"scene_id":1,"location":"某处 内 日","summary":"s","cast":["李四"],"units":[{"unit_type":"action","character":"李四","content":"动作"}]}]}`,
			want: "not found in characters",
		},
		{
			name: "duplicate character name",
			json: `{"total_scenes":1,"characters":[{"name":"张三"},{"name":"张三"}],"scenes":[{"scene_id":1,"location":"某处 内 日","summary":"s","cast":["张三"],"units":[{"unit_type":"action","character":"张三","content":"动作"}]}]}`,
			want: "duplicate character name",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseNovelToScript(tc.json)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("expected error containing %q, got %q", tc.want, err.Error())
			}
		})
	}
}

func TestParseNovelToScriptOutlineErrors(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string
	}{
		{
			name: "empty plot line",
			json: `{"plot_line":" ","characters":[{"name":"张三"}],"scene_plan":[{"scene_id":1,"location":"某处","cast":["张三"],"core_event":"e"}]}`,
			want: "plot_line is required",
		},
		{
			name: "empty characters",
			json: `{"plot_line":"主线","characters":[],"scene_plan":[{"scene_id":1,"location":"某处","cast":[],"core_event":"e"}]}`,
			want: "characters must not be empty",
		},
		{
			name: "empty scene plan",
			json: `{"plot_line":"主线","characters":[{"name":"张三"}],"scene_plan":[]}`,
			want: "scene_plan must not be empty",
		},
		{
			name: "cast member not in characters",
			json: `{"plot_line":"主线","characters":[{"name":"张三"}],"scene_plan":[{"scene_id":1,"location":"某处","cast":["李四"],"core_event":"e"}]}`,
			want: "not found in characters",
		},
		{
			name: "duplicate scene id",
			json: `{"plot_line":"主线","characters":[{"name":"张三"}],"scene_plan":[{"scene_id":1,"location":"某处","cast":[],"core_event":"a"},{"scene_id":1,"location":"别处","cast":[],"core_event":"b"}]}`,
			want: "is duplicated",
		},
		{
			name: "scene id not contiguous",
			json: `{"plot_line":"主线","characters":[{"name":"张三"}],"scene_plan":[{"scene_id":1,"location":"某处","cast":[],"core_event":"a"},{"scene_id":3,"location":"别处","cast":[],"core_event":"b"}]}`,
			want: "not contiguous",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseNovelToScriptOutline(tc.json)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("expected error containing %q, got %q", tc.want, err.Error())
			}
		})
	}
}

func TestVerifyNovelScriptAgainstOutline(t *testing.T) {
	outline := lightweightNovelOutline{
		ScenePlan: []lightweightNovelOutlineScenePlan{
			{SceneID: 1, Location: "客栈 内 夜", Cast: []string{"沈西风"}, CoreEvent: "a"},
			{SceneID: 2, Location: "客栈 内 夜", Cast: []string{"沈西风"}, CoreEvent: "b"},
		},
	}

	// 一致：2 场剧本对应 2 场大纲 → 通过
	ok := lightweightNovelScriptResult{
		TotalScenes: 2,
		Scenes: []lightweightNovelScriptScene{
			{SceneID: 1, Location: "客栈 内 夜", Cast: []string{"沈西风"}, Summary: "s"},
			{SceneID: 2, Location: "客栈 内 夜", Cast: []string{"沈西风"}, Summary: "s"},
		},
	}
	if err := verifyNovelScriptAgainstOutline(outline, &ok); err != nil {
		t.Errorf("matching outline should pass: %v", err)
	}

	// 少一场 → 报错（LLM 漏场）
	tooFew := lightweightNovelScriptResult{
		TotalScenes: 1,
		Scenes: []lightweightNovelScriptScene{
			{SceneID: 1, Location: "客栈 内 夜", Cast: []string{"沈西风"}, Summary: "s"},
		},
	}
	if err := verifyNovelScriptAgainstOutline(outline, &tooFew); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Errorf("scene count mismatch should be rejected, got %v", err)
	}

	// 数量相等但 scene_id 不在大纲（纯函数防御，绕过 parse 直接构造）
	unknown := lightweightNovelScriptResult{
		TotalScenes: 2,
		Scenes: []lightweightNovelScriptScene{
			{SceneID: 1, Location: "客栈 内 夜", Cast: []string{"沈西风"}, Summary: "s"},
			{SceneID: 3, Location: "客栈 内 夜", Cast: []string{"沈西风"}, Summary: "s"},
		},
	}
	if err := verifyNovelScriptAgainstOutline(outline, &unknown); err == nil || !strings.Contains(err.Error(), "not found in outline") {
		t.Errorf("unknown scene_id should be rejected, got %v", err)
	}
}
