package api

import (
	"os"
	"strings"
	"testing"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) {
	d, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	// sqlite :memory: 每个连接是独立数据库；强制单连接，避免 AutoMigrate 建的表
	// 被连接池中其他连接查询时出现 "no such table"。
	sqlDB, err := d.DB()
	if err != nil {
		panic(err)
	}
	sqlDB.SetMaxOpenConns(1)
	db.DB = d
	if err := d.AutoMigrate(&models.SystemLog{}, &models.Character{}); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func newValidatePayload(chars []lightweightStoryCharacter) *lightweightStoryResponse {
	return &lightweightStoryResponse{
		Characters: chars,
		Scenes: []lightweightStoryScene{
			{SceneID: 1, DurationSeconds: 5, ImagePrompt: "img", VideoPrompt: "vid"},
		},
	}
}

func TestValidateLightweightStoryResponseSkipsExistingNames(t *testing.T) {
	existing := []lightweightStoryCharacter{{Name: "李四"}}
	payload := newValidatePayload([]lightweightStoryCharacter{
		{Name: "张三"},
		{Name: "李四"},
	})
	if err := validateLightweightStoryResponse(payload, existing, "high_quality", 0); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(payload.Characters) != 1 || payload.Characters[0].Name != "张三" {
		t.Fatalf("existing-name character should be filtered out, got %+v", payload.Characters)
	}
}

func TestValidateLightweightStoryResponseRejectsBadInputs(t *testing.T) {
	if err := validateLightweightStoryResponse(newValidatePayload([]lightweightStoryCharacter{{Name: "张三"}, {Name: "张三"}}), nil, "high_quality", 0); err == nil || !strings.Contains(err.Error(), "duplicate character name") {
		t.Fatalf("expected duplicate character name error, got %v", err)
	}
	if err := validateLightweightStoryResponse(newValidatePayload([]lightweightStoryCharacter{{Name: ""}}), nil, "high_quality", 0); err == nil {
		t.Fatal("expected empty-name error")
	}
}