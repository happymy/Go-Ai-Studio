package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kt-ai-studio/internal/db"
	"kt-ai-studio/internal/models"

	"github.com/sashabaranov/go-openai"
)

// —— thinkInjectionRoundTripper ——

type recordingRoundTripper struct {
	body       []byte
	sawRequest *http.Request
}

func (r *recordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	b, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	r.body = b
	r.sawRequest = req
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{}`)),
		Header:     http.Header{},
	}, nil
}

const testCompletionPath = "http://example.com/v1/chat/completions"

func readBodyMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("请求体不是合法 JSON: %v", err)
	}
	return m
}

func TestThinkInjectionRoundTripper(t *testing.T) {
	t.Run("chat/completions 注入 think:false", func(t *testing.T) {
		rec := &recordingRoundTripper{}
		rt := &thinkInjectionRoundTripper{base: rec, think: false}
		req := httptest.NewRequest(http.MethodPost, testCompletionPath, bytes.NewBufferString(`{"model":"qwen3","messages":[]}`))
		if _, err := rt.RoundTrip(req); err != nil {
			t.Fatalf("RoundTrip 失败: %v", err)
		}
		m := readBodyMap(t, rec.body)
		v, ok := m["think"]
		if !ok || v != false {
			t.Fatalf("期望 body 含 think:false，实际 %#v", rec.body)
		}
		if rec.sawRequest.ContentLength != int64(len(rec.body)) {
			t.Fatalf("ContentLength 未同步: got %d want %d", rec.sawRequest.ContentLength, len(rec.body))
		}
	})

	t.Run("非 chat/completions 路径不注入", func(t *testing.T) {
		rec := &recordingRoundTripper{}
		rt := &thinkInjectionRoundTripper{base: rec, think: false}
		req := httptest.NewRequest(http.MethodPost, "http://example.com/v1/embeddings", bytes.NewBufferString(`{"input":"x"}`))
		if _, err := rt.RoundTrip(req); err != nil {
			t.Fatalf("RoundTrip 失败: %v", err)
		}
		m := readBodyMap(t, rec.body)
		if _, ok := m["think"]; ok {
			t.Fatalf("非 chat/completions 路径不应注入 think，实际 %#v", rec.body)
		}
	})

	t.Run("非 JSON 请求体原样透传", func(t *testing.T) {
		rec := &recordingRoundTripper{}
		rt := &thinkInjectionRoundTripper{base: rec, think: false}
		req := httptest.NewRequest(http.MethodPost, testCompletionPath, bytes.NewBufferString("not-json"))
		if _, err := rt.RoundTrip(req); err != nil {
			t.Fatalf("RoundTrip 失败: %v", err)
		}
		if string(rec.body) != "not-json" {
			t.Fatalf("期望原样透传，实际 %q", rec.body)
		}
	})
}

// —— newDirectLLMRequest ——

func TestNewDirectLLMRequestThinkInjection(t *testing.T) {
	base := models.LLMProvider{
		APIAddress: "http://example.com/v1/chat/completions",
	}
	req := openai.ChatCompletionRequest{
		Model:    "qwen3",
		Messages: []openai.ChatCompletionMessage{{Role: "user", Content: "1+1=?"}},
	}

	cases := []struct {
		name           string
		compat         bool
		enableThinking bool
		wantInjected   bool
	}{
		{"兼容 LM Studio 且开启思考 → 不注入", true, true, false},
		{"兼容 LM Studio 且关闭思考 → 注入 think:false", true, false, true},
		{"非兼容模式 → 不注入", false, false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			p.CompatLMStudio = tc.compat
			p.EnableThinking = tc.enableThinking
			httpReq, err := newDirectLLMRequest(context.Background(), p, req)
			if err != nil {
				t.Fatalf("newDirectLLMRequest 失败: %v", err)
			}
			body, err := io.ReadAll(httpReq.Body)
			if err != nil {
				t.Fatalf("读取请求体失败: %v", err)
			}
			m := readBodyMap(t, body)
			v, ok := m["think"]
			if tc.wantInjected {
				if !ok || v != false {
					t.Fatalf("期望注入 think:false，实际 %#v", body)
				}
			} else if ok {
				t.Fatalf("不应注入 think，实际 %#v", body)
			}
		})
	}
}

// —— 模型默认值 ——

func TestLLMProviderEnableThinkingDefault(t *testing.T) {
	if err := db.DB.AutoMigrate(&models.LLMProvider{}); err != nil {
		t.Fatalf("AutoMigrate LLMProvider 失败: %v", err)
	}
	p := models.LLMProvider{
		Name:       "think-default-test",
		Provider:   "LM Studio",
		APIAddress: "http://example.com/v1",
	}
	if err := db.DB.Create(&p).Error; err != nil {
		t.Fatalf("创建 provider 失败: %v", err)
	}
	if !p.EnableThinking {
		t.Fatalf("默认 enable_thinking 应为 true，实际 %v", p.EnableThinking)
	}
}
