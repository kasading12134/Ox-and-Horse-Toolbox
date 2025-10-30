package qwen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"autobot/internal/ai"
	"autobot/internal/config"
	loggerpkg "autobot/internal/logger"
	"autobot/internal/news"
)

const defaultEndpoint = "/api/v1/chat/completions"

// Client 封装通义千问 REST 接口。
type Client struct {
	httpClient *http.Client
	apiKey     string
	cfg        config.QwenConfig
	logger     *loggerpkg.ModuleLogger
}

var _ ai.Provider = (*Client)(nil)

// New 创建 Qwen 客户端。
func New(apiKey string, cfg config.QwenConfig) *Client {
	if !cfg.Enabled {
		return nil
	}
	client := &http.Client{Timeout: 20 * time.Second}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://dashscope.aliyuncs.com"
	}
	if cfg.Model == "" {
		cfg.Model = "qwen-turbo"
	}
	if cfg.Temperature == 0 {
		cfg.Temperature = 0.4
	}
	if cfg.TopP == 0 {
		cfg.TopP = 0.8
	}
	moduleLogger := loggerpkg.Get("ai.qwen")
	moduleLogger.Printf("initialized qwen client model=%s base=%s", cfg.Model, cfg.BaseURL)
	return &Client{httpClient: client, apiKey: apiKey, cfg: cfg, logger: moduleLogger}
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type requestBody struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	Temperature float64   `json:"temperature"`
	TopP        float64   `json:"top_p"`
}

type responseBody struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
	OutputText string `json:"output_text"`
	Error      *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *Client) AnalyzeNews(ctx context.Context, articles []news.Article) (news.SentimentSummary, error) {
	if c == nil {
		return news.SentimentSummary{}, errors.New("qwen client is nil")
	}
	if len(articles) == 0 {
		return news.SentimentSummary{Sentiment: "neutral"}, nil
	}

	payload := map[string]any{
		"task":         "crypto_news_sentiment",
		"instructions": "请分析以下加密货币新闻，输出JSON {\"sentiment\":string, \"score\":number(0-1), \"highlights\":[], \"riskFactors\":[]}。",
		"articles":     articles,
	}
	body, _ := json.Marshal(payload)

	titles := make([]string, 0, len(articles))
	for _, article := range articles {
		if article.Title != "" {
			titles = append(titles, article.Title)
		}
	}

	msgs := []message{
		{Role: "system", Content: "你是一名资深的加密货币市场分析师。"},
		{Role: "user", Content: fmt.Sprintf("请处理以下上下文:\n```json\n%s\n```", string(body))},
	}
	if c.logger != nil {
		previewCount := len(titles)
		const maxPreview = 5
		if previewCount > maxPreview {
			previewCount = maxPreview
		}
		titlePreview := strings.Join(titles[:previewCount], " | ")
		if len(titles) > maxPreview {
			titlePreview += " ..."
		}
		c.logger.Printf("news.request count=%d titles=%s", len(titles), titlePreview)
	}

	resp, err := c.send(ctx, msgs)
	if err != nil {
		if c.logger != nil {
			c.logger.Printf("news.error: %v", err)
		}
		return news.SentimentSummary{}, err
	}

	content := cleanJSON(resp.Content)
	summary := news.SentimentSummary{}
	if err := json.Unmarshal([]byte(content), &summary); err != nil {
		if resp.Content == "" && resp.Output != "" {
			cleaned := cleanJSON(resp.Output)
			if err := json.Unmarshal([]byte(cleaned), &summary); err == nil {
				if c.logger != nil {
					c.logger.Printf("news.response payload=%s", cleaned)
				}
				return summary, nil
			}
		}
		if c.logger != nil {
			c.logger.Printf("news.parse.error: %v content=%s", err, content)
		}
		return news.SentimentSummary{}, fmt.Errorf("parse news sentiment: %w", err)
	}
	if summary.Sentiment == "" {
		summary.Sentiment = "neutral"
	}
	if c.logger != nil {
		if data, err := json.Marshal(summary); err == nil {
			c.logger.Printf("news.response payload=%s", string(data))
		}
	}
	return summary, nil
}

func (c *Client) GenerateDecision(ctx context.Context, req ai.DecisionRequest) (ai.DecisionResponse, error) {
	if c == nil {
		return ai.DecisionResponse{}, errors.New("qwen client is nil")
	}

	payload, _ := json.Marshal(req)
	msgs := []message{
		{Role: "system", Content: "你是一名自动加密货币交易顾问，请严格遵守风控并输出JSON"},
		{Role: "user", Content: fmt.Sprintf("交易上下文如下:\n```json\n%s\n```\n请输出JSON {\"action\":string, \"confidence\":number(0-1), \"reason\":string, \"adjustments\":{\"sizeMultiplier\":number, \"targetLeverage\":number, \"stopLossPercent\":number, \"takeProfitPercent\":number, \"trailingStopPercent\":number}, \"riskNotes\":[string]}。", string(payload))},
	}
	if c.logger != nil {
		c.logger.Printf("decision.request payload=%s", string(payload))
	}

	resp, err := c.send(ctx, msgs)
	if err != nil {
		if c.logger != nil {
			c.logger.Printf("decision.error: %v", err)
		}
		return ai.DecisionResponse{}, err
	}

	content := cleanJSON(resp.Content)
	decision := ai.DecisionResponse{}
	if err := json.Unmarshal([]byte(content), &decision); err != nil {
		if resp.Content == "" && resp.Output != "" {
			cleaned := cleanJSON(resp.Output)
			if err := json.Unmarshal([]byte(cleaned), &decision); err == nil {
				if c.logger != nil {
					c.logger.Printf("decision.response payload=%s", cleaned)
				}
				return decision, nil
			}
		}
		if c.logger != nil {
			c.logger.Printf("decision.parse.error: %v content=%s", err, content)
		}
		return ai.DecisionResponse{}, fmt.Errorf("parse decision: %w", err)
	}
	if c.logger != nil {
		if data, err := json.Marshal(decision); err == nil {
			c.logger.Printf("decision.response payload=%s", string(data))
		}
	}

	return decision, nil
}

type completion struct {
	Content string
	Output  string
}

func (c *Client) send(ctx context.Context, messages []message) (completion, error) {
	body := requestBody{
		Model:       c.cfg.Model,
		Messages:    messages,
		Temperature: c.cfg.Temperature,
		TopP:        c.cfg.TopP,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return completion{}, err
	}
	if c.logger != nil {
		c.logger.Printf("http.request model=%s messages=%d", c.cfg.Model, len(messages))
	}

	endpoint := c.cfg.BaseURL + defaultEndpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return completion{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return completion{}, fmt.Errorf("qwen request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		if c.logger != nil {
			c.logger.Printf("http.error status=%d", resp.StatusCode)
		}
		return completion{}, fmt.Errorf("qwen status %d", resp.StatusCode)
	}

	var payload responseBody
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return completion{}, fmt.Errorf("decode completion: %w", err)
	}
	if payload.Error != nil {
		if c.logger != nil {
			c.logger.Printf("http.error payload=%v", payload.Error)
		}
		return completion{}, errors.New(payload.Error.Message)
	}

	if len(payload.Choices) == 0 {
		if c.logger != nil {
			c.logger.Printf("http.error no choices response=%v", payload)
		}
		return completion{}, errors.New("qwen无返回结果")
	}

	result := completion{Content: payload.Choices[0].Message.Content, Output: payload.OutputText}
	if c.logger != nil {
		c.logger.Printf("http.response choices=%d", len(payload.Choices))
	}
	return result, nil
}

func cleanJSON(s string) string {
	trimmed := strings.TrimSpace(s)
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimSpace(trimmed)
		if strings.HasPrefix(strings.ToLower(trimmed), "json") {
			if idx := strings.Index(trimmed, "\n"); idx != -1 {
				trimmed = trimmed[idx+1:]
			} else {
				trimmed = ""
			}
		}
		trimmed = strings.TrimSpace(trimmed)
		if strings.HasSuffix(trimmed, "```") {
			trimmed = strings.TrimSuffix(trimmed, "```")
		}
		trimmed = strings.TrimSpace(trimmed)
	}
	return trimmed
}
