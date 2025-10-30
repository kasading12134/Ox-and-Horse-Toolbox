package deepseek

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"autobot/internal/ai"
	"autobot/internal/config"
	loggerpkg "autobot/internal/logger"
	"autobot/internal/mcp"
	"autobot/internal/news"
)

const (
	defaultCompletionPath = "/v1/chat/completions"
	maxRetries           = 3                    // 最大重试次数
	baseRetryDelay       = 2 * time.Second     // 基础重试延迟
	defaultTimeout       = 120 * time.Second   // 120秒超时（分析大量数据）
)

// 网络错误检测函数
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	
	// 检查常见的网络错误类型
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	
	// 检查HTTP连接错误
	if strings.Contains(err.Error(), "connection") ||
		strings.Contains(err.Error(), "network") ||
		strings.Contains(err.Error(), "timeout") ||
		strings.Contains(err.Error(), "reset") ||
		strings.Contains(err.Error(), "refused") {
		return true
	}
	
	return false
}

// Client 封装与 DeepSeek API 的交互。
type Client struct {
	mcpClient *mcp.Client
	cfg       config.DeepseekConfig
	logger    *loggerpkg.ModuleLogger
	mu        sync.RWMutex
	apiKey    string
}

// 确保实现 ai.Provider 接口。
var _ ai.Provider = (*Client)(nil)

// New 创建 DeepSeek 客户端。
func New(cfg config.DeepseekConfig) *Client {
	if !cfg.Enabled {
		return nil
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.deepseek.com/v1"  // DeepSeek官方API v1端点
	}
	if cfg.Model == "" {
		cfg.Model = "deepseek-chat"                // 使用deepseek-chat模型
	}
	if cfg.Temperature == 0 {
		cfg.Temperature = 0.5  // 较低温度提高JSON稳定性
	}
	if cfg.TopP == 0 {
		cfg.TopP = 0.9
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 2000   // 足够返回思维链+JSON
	}
	
	// 使用120秒超时
	timeout := defaultTimeout
	
	moduleLogger := loggerpkg.Get("ai.deepseek")
	moduleLogger.Printf("initialized deepseek client model=%s base=%s timeout=%v", cfg.Model, cfg.BaseURL, timeout)
	
	// 创建MCP客户端时使用配置的超时时间
	mcpClient := mcp.New(cfg.BaseURL, timeout)
	return &Client{mcpClient: mcpClient, cfg: cfg, logger: moduleLogger}
}

type completionMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type completionRequest struct {
	Model       string              `json:"model"`
	Messages    []completionMessage `json:"messages"`
	Temperature float64             `json:"temperature"`
	TopP        float64             `json:"top_p"`
	MaxTokens   int                 `json:"max_tokens"`
}

type completionResponse struct {
	Choices []struct {
		Message completionMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// SetDeepSeekAPIKey 设置DeepSeek API密钥
func (c *Client) SetDeepSeekAPIKey(key string) {
	if c == nil {
		return
	}
	trimmed := strings.TrimSpace(key)
	c.mu.Lock()
	c.apiKey = trimmed
	c.mu.Unlock()
	if c.logger != nil {
		masked := ""
		if len(trimmed) >= 8 {
			masked = trimmed[:4] + "***" + trimmed[len(trimmed)-3:]
		}
		c.logger.Printf("api key updated masked=%s", masked)
	}
}

// AnalyzeNews 使用 DeepSeek 对新闻进行情绪分析。
func (c *Client) AnalyzeNews(ctx context.Context, articles []news.Article) (news.SentimentSummary, error) {
	if c == nil {
		return news.SentimentSummary{}, errors.New("deepseek client is nil")
	}
	if c.apiKeyValue() == "" {
		return news.SentimentSummary{}, errors.New("deepseek api key 未设置")
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

	systemPrompt := "你是一名资深的加密货币市场分析师，善于从新闻中提炼情绪与风险。"
	userPrompt := fmt.Sprintf("请处理以下上下文:\n```json\n%s\n```", string(body))
	
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

	// 使用新的重试机制
	respContent, err := c.CallWithMessages(systemPrompt, userPrompt)
	if err != nil {
		if c.logger != nil {
			c.logger.Printf("news.error: %v", err)
		}
		return news.SentimentSummary{}, err
	}

	content := cleanJSON(respContent)
	summary := news.SentimentSummary{}
	if err := json.Unmarshal([]byte(content), &summary); err != nil {
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

// GenerateDecision 请求 DeepSeek 输出交易决策。
func (c *Client) GenerateDecision(ctx context.Context, req ai.DecisionRequest) (ai.DecisionResponse, error) {
	if c == nil {
		return ai.DecisionResponse{}, errors.New("deepseek client is nil")
	}
	if c.apiKeyValue() == "" {
		return ai.DecisionResponse{}, errors.New("deepseek api key 未设置")
	}

	// 获取绩效数据和持仓信息
	performance := req.Context.Performance
	positions := req.Context.Positions
	
	promptCtx := newPromptContext(req, performance, positions)
	accountEquity := req.AccountBalance
	if req.Context.Account.TotalEquity > 0 {
		accountEquity = req.Context.Account.TotalEquity
	}
	
	// 使用集成了反思模块的系统提示
	systemPrompt := buildSystemPrompt(accountEquity, req.Context.BTCETHLeverage, req.Context.AltcoinLeverage, req.RiskLimits, performance, positions)
	userPrompt := buildUserPrompt(promptCtx)
	if c.logger != nil {
		c.logger.Printf("decision.prompt system=%d chars user=long_prompt", len(systemPrompt))
	}

	// 使用新的重试机制
	respContent, err := c.CallWithMessages(systemPrompt, userPrompt)
	if err != nil {
		if c.logger != nil {
			c.logger.Printf("decision.error: %v", err)
		}
		return ai.DecisionResponse{}, err
	}

	decision, err := parseFullDecisionResponse(respContent)
	if err != nil {
		if c.logger != nil {
			c.logger.Printf("decision.parse.error: %v content=%s", err, respContent)
		}
		return ai.DecisionResponse{}, err
	}
	decision.RawContent = respContent
	if decision.CoTTrace == "" {
		decision.CoTTrace = extractCoTTrace(respContent)
	}
	if err := validateDecisionResponse(decision, req.RiskLimits); err != nil {
		if c.logger != nil {
			c.logger.Printf("decision.validate.error: %v", err)
		}
		return ai.DecisionResponse{}, err
	}
	
	if c.logger != nil {
		if data, err := json.Marshal(decision); err == nil {
			c.logger.Printf("decision.response payload=%s", string(data))
		}
	}

	return decision, nil
}

// CallWithMessages 带重试的AI调用
func (c *Client) CallWithMessages(systemPrompt, userPrompt string) (string, error) {
	if c == nil {
		return "", errors.New("deepseek client is nil")
	}
	if c.apiKeyValue() == "" {
		return "", errors.New("deepseek api key 未设置")
	}

	// 构建 messages 数组
	messages := []completionMessage{}
	// 添加 system message（交易规则）
	messages = append(messages, completionMessage{
		Role:    "system",
		Content: systemPrompt,
	})
	// 添加 user message（市场数据）
	messages = append(messages, completionMessage{
		Role:    "user", 
		Content: userPrompt,
	})

	maxRetries := 3  // 最大重试次数
	var lastErr error
	
	for attempt := 1; attempt <= maxRetries; attempt++ {
		response, err := c.sendCompletion(context.Background(), messages)
		if err == nil {
			return response.Content, nil  // 成功返回
		}
		
		// 如果是网络错误才重试
		if isNetworkError(err) {
			lastErr = err
			if c.logger != nil {
				c.logger.Printf("retry.attempt attempt=%d/%d error=%v", attempt, maxRetries, err)
			}
			time.Sleep(time.Duration(attempt) * baseRetryDelay)  // 指数退避
			continue
		}
		
		return "", err  // 非网络错误直接返回
	}
	
	return "", fmt.Errorf("重试%d次后仍然失败: %w", maxRetries, lastErr)
}

// sendCompletion 单次调用AI API
func (c *Client) sendCompletion(ctx context.Context, messages []completionMessage) (completionMessage, error) {
	if len(messages) == 0 {
		return completionMessage{}, errors.New("messages为空")
	}

	// 构建请求体 - 符合OpenAI标准格式
	requestBody := completionRequest{
		Model:       c.cfg.Model,
		Messages:    messages,
		Temperature: c.cfg.Temperature,
		TopP:        c.cfg.TopP,
		MaxTokens:   c.cfg.MaxTokens,
	}

	if c.logger != nil {
		c.logger.Printf("http.request model=%s messages=%d", c.cfg.Model, len(messages))
	}

	apiKey := c.apiKeyValue()
	if apiKey == "" {
		return completionMessage{}, errors.New("deepseek api key 未设置")
	}
	
	// 标准OpenAI认证头
	headers := map[string]string{
		"Authorization": "Bearer " + apiKey,
		"Content-Type":  "application/json",
	}

	var payload completionResponse
	if err := c.mcpClient.PostJSON(ctx, defaultCompletionPath, headers, requestBody, &payload); err != nil {
		if c.logger != nil {
			c.logger.Printf("http.error request=%v", err)
		}
		return completionMessage{}, fmt.Errorf("deepseek request: %w", err)
	}

	if payload.Error != nil {
		if c.logger != nil {
			c.logger.Printf("http.error payload=%v", payload.Error)
		}
		return completionMessage{}, errors.New(payload.Error.Message)
	}
	if len(payload.Choices) == 0 {
		if c.logger != nil {
			c.logger.Printf("http.error no choices payload=%v", payload)
		}
		return completionMessage{}, errors.New("deepseek无返回结果")
	}

	result := payload.Choices[0].Message
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

func (c *Client) apiKeyValue() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.apiKey
}
