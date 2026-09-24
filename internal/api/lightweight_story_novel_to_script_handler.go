package api

import (
	"errors"
	"net/http"
	"strings"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"

	"github.com/gin-gonic/gin"
)

// novelToScriptHandlerRequest P0 小说转剧本接口的请求体。
type novelToScriptHandlerRequest struct {
	Title     string `json:"title"`
	NovelText string `json:"novel_text"`
}

// ResolveActiveLLMProvider 返回当前激活的 LLM Provider（同步 handler 复用）。
func ResolveActiveLLMProvider() (models.LLMProvider, error) {
	var provider models.LLMProvider
	if err := db.DB.Where("is_active = ?", true).First(&provider).Error; err != nil {
		return provider, err
	}
	return provider, nil
}

// ValidateNovelToScriptRequest 校验请求体：novel_text 必填。
// 拆成纯函数便于复用与单测（handler 层只做绑定与响应）。
func ValidateNovelToScriptRequest(title, novelText string) error {
	if strings.TrimSpace(novelText) == "" {
		return errors.New("novel_text is required")
	}
	return nil
}

// HandleNovelToScript P0 小说转剧本：同步执行两段式（大纲 → 逐场展开），
// 返回结构化剧本初稿。未接入后台任务队列，调用方需接受较长的同步等待。
func HandleNovelToScript(c *gin.Context) {
	var req novelToScriptHandlerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := ValidateNovelToScriptRequest(req.Title, req.NovelText); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	provider, err := ResolveActiveLLMProvider()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "no active LLM provider found"})
		return
	}

	result, err := runNovelToScript(strings.TrimSpace(req.Title), strings.TrimSpace(req.NovelText), provider, "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}
