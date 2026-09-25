package api

import (
	"strings"
	"testing"

	"kt-ai-studio/internal/models"
)

// TestBuildCharacterPreviewPositivePrompt 验证人物图正片 prompt 构造：
// 新规范三锚点分工下，appearance 只含脸部/身材，固定服装与体态锚点在 fingerprint，
// 人物图必须并入 fingerprint，否则新角色人物图丢失服装；旧数据 fingerprint 为空时
// 回退时代/国别通用展示服装。
func TestBuildCharacterPreviewPositivePrompt(t *testing.T) {
	tests := []struct {
		name    string
		char    models.Character
		wantErr bool
		checks  []string // 必须出现的子串
		absent  []string // 禁止出现的子串
	}{
		{
			name:    "appearance 为空报错",
			char:    models.Character{Name: "A"},
			wantErr: true,
		},
		{
			name: "fingerprint 服装锚点并入",
			char: models.Character{
				Appearance:  "圆脸带柔和轮廓，眉眼清秀，鼻梁直挺",
				Fingerprint: "体态匀称、肩背比例协调、常穿护士服配厚白色丝袜，佩戴白色护士帽",
				Gender:      "女",
			},
			checks: []string{
				"圆脸带柔和轮廓",
				"常穿护士服配厚白色丝袜，佩戴白色护士帽",
				"体态匀称",
				"禁止字幕",
			},
		},
		{
			name: "fingerprint 为空回退时代/国别服装",
			char: models.Character{
				Appearance: "剑眉星目，肤色古铜",
				Era:        "明朝",
				Country:    "中国",
			},
			checks: []string{
				"剑眉星目",
				"服装采用符合中国明朝的简洁基础展示服装",
			},
			absent: []string{"锚点出镜"},
		},
		{
			name: "fingerprint 与时代国别均空回退通用服装",
			char: models.Character{
				Appearance: "黑发，肤色白皙",
			},
			checks: []string{
				"黑发",
				"服装采用简洁基础展示服装",
			},
			absent: []string{"锚点出镜"},
		},
		{
			name: "身份片段与 fingerprint 同时并入",
			char: models.Character{
				Appearance:  "长发及肩，圆脸",
				Age:         "14岁",
				BodyHeight:  "160厘米",
				Country:     "中国",
				Gender:      "女",
				Era:         "现代",
				Fingerprint: "常穿校服配白袜",
			},
			checks: []string{
				"14岁", "160厘米", "中国", "女", "现代",
				"常穿校服配白袜",
				"单人全身像",
				"鞋子完整可见",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildCharacterPreviewPositivePrompt(tc.char)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际得到 prompt: %s", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("意外错误: %v", err)
			}
			for _, c := range tc.checks {
				if !strings.Contains(got, c) {
					t.Errorf("prompt 缺少 %q, 实际: %s", c, got)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(got, a) {
					t.Errorf("prompt 不应包含 %q, 实际: %s", a, got)
				}
			}
		})
	}
}