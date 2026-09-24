package api

import (
	"strings"
	"testing"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"
)

// P0 接线：请求校验与 active provider 解析（纯函数可测，handler 层只做绑定与响应，项目无 gin 测试先例）。
func TestValidateNovelToScriptRequest(t *testing.T) {
	if err := ValidateNovelToScriptRequest("标题", "  "); err == nil || !strings.Contains(err.Error(), "novel_text") {
		t.Fatalf("empty novel_text should error with field reference, got %v", err)
	}
	if err := ValidateNovelToScriptRequest("", "正文内容"); err != nil {
		t.Fatalf("valid request should pass, got %v", err)
	}
	// title 允许为空（runNovelToScript 不强制 title）
	if err := ValidateNovelToScriptRequest("", "正文"); err != nil {
		t.Fatalf("empty title should be allowed, got %v", err)
	}
}

func TestResolveActiveLLMProvider(t *testing.T) {
	// 无激活 provider → 报错（handler 映射为 500 "no active LLM provider found"）
	if _, err := ResolveActiveLLMProvider(); err == nil {
		t.Fatal("expected error when no active provider exists")
	}

	// 建表并插入激活 provider → 返回该 provider
	if err := db.DB.AutoMigrate(&models.LLMProvider{}); err != nil {
		t.Fatalf("auto migrate LLMProvider failed: %v", err)
	}
	active := models.LLMProvider{Name: "测试模型", APIAddress: "http://localhost:1/v1", ModelName: "test", IsActive: true}
	if err := db.DB.Create(&active).Error; err != nil {
		t.Fatalf("create active provider failed: %v", err)
	}
	got, err := ResolveActiveLLMProvider()
	if err != nil {
		t.Fatalf("expected active provider, got %v", err)
	}
	if got.ID != active.ID || got.Name != "测试模型" {
		t.Errorf("expected provider id=%d name=%s, got id=%d name=%s", active.ID, active.Name, got.ID, got.Name)
	}

	// 插入第二条激活 provider → 只返回最早激活的一条（行为与现链路一致）
	second := models.LLMProvider{Name: "备用模型", APIAddress: "http://localhost:2/v1", ModelName: "test2", IsActive: true}
	if err := db.DB.Create(&second).Error; err != nil {
		t.Fatalf("create second provider failed: %v", err)
	}
	got2, err := ResolveActiveLLMProvider()
	if err != nil {
		t.Fatalf("expected active provider, got %v", err)
	}
	if got2.ID != active.ID {
		t.Errorf("expected first active provider id=%d, got %d", active.ID, got2.ID)
	}
}
