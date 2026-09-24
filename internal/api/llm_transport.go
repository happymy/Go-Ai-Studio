package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"kt-ai-studio/internal/errfmt"
	"kt-ai-studio/internal/models"

	"github.com/sashabaranov/go-openai"
)

type directLLMChatCompletionResponse struct {
	Choices []directLLMChatCompletionChoice `json:"choices"`
}

type directLLMChatCompletionChoice struct {
	Message directLLMChatCompletionMessage `json:"message"`
	Delta   directLLMChatCompletionMessage `json:"delta"`
}

type directLLMChatCompletionMessage struct {
	Content string `json:"content"`
}

func buildLLMHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	return &http.Client{Timeout: timeout}
}

func buildLLMStreamingHTTPClient() *http.Client {
	return &http.Client{}
}

func buildLLMOpenAIClient(provider models.LLMProvider, timeout time.Duration, streaming bool) *openai.Client {
	config := openai.DefaultConfig(provider.APIKey)
	config.BaseURL = provider.APIAddress
	httpClient := buildLLMStreamingHTTPClient()
	if !streaming {
		httpClient = buildLLMHTTPClient(timeout)
	}
	// 兼容 LM Studio 且用户关闭思考时，在出站请求体注入 think 参数（本地推理服务通用思考开关，
	// 如 Ollama / LM Studio 的 Qwen3 系模型）。仅对 CompatLMStudio 注入，避免 OpenAI 官方端点
	// 因未知参数返回 400。
	if provider.CompatLMStudio && !provider.EnableThinking {
		base := httpClient.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		httpClient.Transport = &thinkInjectionRoundTripper{base: base, think: false}
	}
	config.HTTPClient = httpClient
	return openai.NewClientWithConfig(config)
}

// thinkInjectionRoundTripper 在请求出站前给 /chat/completions 请求体注入 think 参数。
type thinkInjectionRoundTripper struct {
	base  http.RoundTripper
	think bool
}

func (t *thinkInjectionRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body == nil || !strings.Contains(req.URL.Path, "/chat/completions") {
		return t.base.RoundTrip(req)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	if err := req.Body.Close(); err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		// 非 JSON 请求体原样透传
		req.Body = io.NopCloser(bytes.NewReader(body))
		return t.base.RoundTrip(req)
	}
	payload["think"] = t.think
	newBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(newBody))
	req.ContentLength = int64(len(newBody))
	req.Header.Set("Content-Type", "application/json")
	return t.base.RoundTrip(req)
}

type llmStreamIdleController struct {
	ctx      context.Context
	cancel   context.CancelFunc
	activity chan struct{}
	stopCh   chan struct{}
	stopOnce sync.Once
	expired  atomic.Bool
}

func newLLMStreamIdleController(idleTimeout time.Duration) *llmStreamIdleController {
	if idleTimeout <= 0 {
		idleTimeout = 30 * time.Minute
	}
	ctx, cancel := context.WithCancel(context.Background())
	controller := &llmStreamIdleController{
		ctx:      ctx,
		cancel:   cancel,
		activity: make(chan struct{}, 1),
		stopCh:   make(chan struct{}),
	}
	go controller.watch(idleTimeout)
	return controller
}

func (c *llmStreamIdleController) watch(idleTimeout time.Duration) {
	timer := time.NewTimer(idleTimeout)
	defer timer.Stop()
	for {
		select {
		case <-c.activity:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(idleTimeout)
		case <-timer.C:
			c.expired.Store(true)
			c.cancel()
			return
		case <-c.stopCh:
			return
		}
	}
}

func (c *llmStreamIdleController) Context() context.Context {
	return c.ctx
}

func (c *llmStreamIdleController) Touch() {
	select {
	case c.activity <- struct{}{}:
	default:
	}
}

func (c *llmStreamIdleController) Stop() {
	c.stopOnce.Do(func() {
		close(c.stopCh)
		c.cancel()
	})
}

func (c *llmStreamIdleController) WrapError(err error) error {
	if err == nil {
		return nil
	}
	if c.expired.Load() || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("llm stream idle timeout exceeded")
	}
	return err
}

func isCustomLLMProvider(provider string) bool {
	trimmed := strings.TrimSpace(provider)
	return strings.EqualFold(trimmed, "custom") || trimmed == "自定义"
}

func shouldUseDirectLLMEndpoint(provider models.LLMProvider) bool {
	if isCustomLLMProvider(provider.Provider) {
		return true
	}

	parsed, err := url.Parse(strings.TrimSpace(provider.APIAddress))
	if err != nil {
		return false
	}

	path := strings.ToLower(strings.TrimSpace(parsed.Path))
	return strings.Contains(path, "/chat/completions")
}

// applyLMStudioReasoningBudget reserves enough output room for Qwen3-style
// thinking models served by LM Studio. Without an explicit max_tokens, LM Studio
// defaults to ~2048 output tokens, which a long "reasoning_content" preamble can
// consume entirely, leaving the actual content empty (empty llm response).
func applyLMStudioReasoningBudget(provider models.LLMProvider, req openai.ChatCompletionRequest) openai.ChatCompletionRequest {
	if req.MaxTokens == 0 {
		req.MaxTokens = provider.LMStudioMaxTokens
	}
	if req.MaxTokens == 0 {
		req.MaxTokens = 8192
	}
	return req
}

// requestLLMContentStreaming strips the OpenAI-style json_object response_format
// for providers explicitly configured as LM Studio compatible, which reject it
// and only accept json_schema or text.
func requestLLMContentStreaming(provider models.LLMProvider, req openai.ChatCompletionRequest, timeout time.Duration, taskID string, streamLogLabel string) (string, error) {
	req = applyProviderAdvancedRequestParams(provider, req)
	if provider.CompatLMStudio {
		req.ResponseFormat = nil
		req = applyLMStudioReasoningBudget(provider, req)
	}
	if shouldUseDirectLLMEndpoint(provider) {
		return requestLLMContentStreamingDirect(provider, req, timeout, taskID, streamLogLabel)
	}
	return requestLLMContentStreamingOpenAI(buildLLMOpenAIClient(provider, timeout, true), req, provider, timeout, taskID, streamLogLabel)
}

func requestLLMContentNonStreaming(provider models.LLMProvider, req openai.ChatCompletionRequest, timeout time.Duration, taskID string, streamLogLabel string) (string, error) {
	req = applyProviderAdvancedRequestParams(provider, req)
	if provider.CompatLMStudio {
		req.ResponseFormat = nil
		req = applyLMStudioReasoningBudget(provider, req)
	}
	if shouldUseDirectLLMEndpoint(provider) {
		return requestLLMContentNonStreamingDirect(provider, req, timeout, taskID, streamLogLabel)
	}
	return requestLLMContentNonStreamingOpenAI(buildLLMOpenAIClient(provider, timeout, false), req, provider, taskID, streamLogLabel)
}

func requestLLMContentStreamingDirect(provider models.LLMProvider, req openai.ChatCompletionRequest, timeout time.Duration, taskID string, streamLogLabel string) (string, error) {
	streamReq := req
	streamReq.Stream = true
	idleController := newLLMStreamIdleController(timeout)
	defer idleController.Stop()

	messageParts := make([]string, 0, len(streamReq.Messages))
	for _, message := range streamReq.Messages {
		messageParts = append(messageParts, message.Content)
	}
	RecordLLMUsageInput(provider, messageParts...)

	httpReq, err := newDirectLLMRequest(idleController.Context(), provider, streamReq)
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := buildLLMStreamingHTTPClient().Do(httpReq)
	if err != nil {
		return "", idleController.WrapError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("API 请求失败: http %d: %s", resp.StatusCode, trimHTTPErrorBody(body))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	var builder strings.Builder
	var rawBody strings.Builder
	var streamStateID uint
	sawStreamChunk := false
	if streamLogLabel != "" {
		streamStateID = upsertLLMStreamState(0, taskID, provider, streamLogLabel, "", "running")
	}

	for scanner.Scan() {
		idleController.Touch()
		line := scanner.Text()
		rawBody.WriteString(line)
		rawBody.WriteString("\n")

		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "data:") {
			continue
		}

		data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}

		var chunk directLLMChatCompletionResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			content := strings.TrimSpace(builder.String())
			if streamLogLabel != "" {
				finalizeLLMStreamState(streamStateID, taskID, provider, streamLogLabel, content, "failed")
			}
			return content, fmt.Errorf("failed to parse direct stream chunk: %w", err)
		}

		delta := pickDirectLLMContent(chunk)
		if delta == "" {
			continue
		}

		sawStreamChunk = true
		builder.WriteString(delta)
		currentContent := strings.TrimSpace(builder.String())
		if streamLogLabel != "" {
			streamStateID = upsertLLMStreamState(streamStateID, taskID, provider, streamLogLabel, currentContent, "running")
		}
	}

	if err := scanner.Err(); err != nil {
		content := strings.TrimSpace(builder.String())
		if streamLogLabel != "" {
			finalizeLLMStreamState(streamStateID, taskID, provider, streamLogLabel, content, "failed")
		}
		return content, idleController.WrapError(err)
	}

	content := strings.TrimSpace(builder.String())
	if !sawStreamChunk {
		content, err = parseDirectLLMResponseBody([]byte(rawBody.String()))
		if err != nil {
			return content, err
		}
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return requestLLMContentNonStreamingDirect(provider, req, timeout, taskID, streamLogLabel)
	}
	if streamLogLabel != "" {
		finalizeLLMStreamState(streamStateID, taskID, provider, streamLogLabel, content, "completed")
	}
	RecordLLMUsageOutput(provider, content)
	return content, nil
}

func requestLLMContentNonStreamingDirect(provider models.LLMProvider, req openai.ChatCompletionRequest, timeout time.Duration, taskID string, streamLogLabel string) (string, error) {
	nonStreamReq := req
	nonStreamReq.Stream = false

	messageParts := make([]string, 0, len(nonStreamReq.Messages))
	for _, message := range nonStreamReq.Messages {
		messageParts = append(messageParts, message.Content)
	}
	RecordLLMUsageInput(provider, messageParts...)

	httpReq, err := newDirectLLMRequest(context.Background(), provider, nonStreamReq)
	if err != nil {
		return "", err
	}

	resp, err := buildLLMHTTPClient(timeout).Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("API 请求失败: http %d: %s", resp.StatusCode, trimHTTPErrorBody(body))
	}

	content, err := parseDirectLLMResponseBody(body)
	if err != nil {
		return "", err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return "", fmt.Errorf("empty completion response")
	}
	if streamLogLabel != "" {
		finalizeLLMStreamState(0, taskID, provider, streamLogLabel, content, "completed")
	}
	RecordLLMUsageOutput(provider, content)
	return content, nil
}

func newDirectLLMRequest(ctx context.Context, provider models.LLMProvider, req openai.ChatCompletionRequest) (*http.Request, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	// 兼容 LM Studio 且用户关闭思考时，注入 think 参数（本地推理服务通用思考开关）
	if provider.CompatLMStudio && !provider.EnableThinking {
		var m map[string]any
		if uerr := json.Unmarshal(payload, &m); uerr == nil {
			m["think"] = false
			if payload, err = json.Marshal(m); err != nil {
				return nil, err
			}
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSpace(provider.APIAddress), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(provider.APIKey) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(provider.APIKey))
	}
	return httpReq, nil
}

func parseDirectLLMResponseBody(body []byte) (string, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return "", nil
	}

	if strings.HasPrefix(trimmed, "data:") || strings.Contains(trimmed, "\ndata:") || strings.Contains(trimmed, "\r\ndata:") {
		return parseDirectLLMStreamBody(trimmed)
	}

	var resp directLLMChatCompletionResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("failed to parse direct completion response: %w", err)
	}
	return pickDirectLLMContent(resp), nil
}

func parseDirectLLMStreamBody(payload string) (string, error) {
	var builder strings.Builder
	lines := strings.Split(strings.ReplaceAll(payload, "\r\n", "\n"), "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}

		var resp directLLMChatCompletionResponse
		if err := json.Unmarshal([]byte(data), &resp); err != nil {
			return "", fmt.Errorf("failed to parse direct stream chunk: %w", err)
		}
		delta := pickDirectLLMContent(resp)
		if delta == "" {
			continue
		}
		builder.WriteString(delta)
	}

	return strings.TrimSpace(builder.String()), nil
}

func pickDirectLLMContent(resp directLLMChatCompletionResponse) string {
	for _, choice := range resp.Choices {
		if content := strings.TrimSpace(choice.Delta.Content); content != "" {
			return content
		}
		if content := strings.TrimSpace(choice.Message.Content); content != "" {
			return content
		}
	}
	return ""
}

func trimHTTPErrorBody(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return http.StatusText(http.StatusBadGateway)
	}
	normalized := errfmt.NormalizeUserFacingError(trimmed, 400)
	if normalized != "" && normalized != trimmed {
		return normalized
	}
	if utf8.RuneCountInString(trimmed) <= 400 {
		return trimmed
	}
	runes := []rune(trimmed)
	return string(runes[:400])
}
