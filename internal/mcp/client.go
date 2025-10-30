package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Provider AI提供商类型
type Provider string

const (
	ProviderDeepSeek Provider = "deepseek"  // DeepSeek AI
	ProviderQwen     Provider = "qwen"      // 通义千问
)

// Config AI API配置
type Config struct {
	Provider  Provider        // 提供商类型
	APIKey    string          // API密钥
	SecretKey string          // 阿里云需要密钥
	BaseURL   string          // API基础地址
	Model     string          // 模型名称
	Timeout   time.Duration   // 请求超时
}

// 默认配置
var defaultConfig = Config{
	Provider: ProviderDeepSeek,
	BaseURL:  "https://api.deepseek.com/v1",
	Model:    "deepseek-chat",
	Timeout:  120 * time.Second,
}

var configMutex sync.RWMutex

// SetDeepSeekAPIKey 设置DeepSeek API密钥
func SetDeepSeekAPIKey(apiKey string) {
	configMutex.Lock()
	defer configMutex.Unlock()
	
	defaultConfig.Provider = ProviderDeepSeek
	defaultConfig.APIKey = apiKey
	defaultConfig.SecretKey = ""
	defaultConfig.BaseURL = "https://api.deepseek.com/v1"
	defaultConfig.Model = "deepseek-chat"
}

// SetQwenAPIKey 设置通义千问API密钥
func SetQwenAPIKey(apiKey, secretKey string) {
	configMutex.Lock()
	defer configMutex.Unlock()
	
	defaultConfig.Provider = ProviderQwen
	defaultConfig.APIKey = apiKey
	defaultConfig.SecretKey = secretKey
	defaultConfig.BaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	defaultConfig.Model = "qwen-turbo"
}

// GetConfig 获取当前配置
func GetConfig() Config {
	configMutex.RLock()
	defer configMutex.RUnlock()
	return defaultConfig
}

// Client 封装与模型控制平面的HTTP交互。
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New 返回一个具备基础超时配置的 MCP 客户端。
func New(baseURL string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	if baseURL == "" {
		baseURL = "https://api.deepseek.com"
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: timeout},
	}
}

// PostJSON 以 JSON 格式发送请求并解析响应。
func (c *Client) PostJSON(ctx context.Context, path string, headers map[string]string, reqPayload any, respPayload any) error {
	if c == nil {
		return fmt.Errorf("mcp client is nil")
	}
	data, err := json.Marshal(reqPayload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	endpoint := c.baseURL + path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("http status %d", resp.StatusCode)
	}
	if respPayload == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(respPayload); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

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

// CallWithMessages 带重试的AI调用
func CallWithMessages(systemPrompt, userPrompt string) (string, error) {
	config := GetConfig()
	
	// 构建 messages 数组
	messages := []map[string]string{}
	// 添加 system message（交易规则）
	messages = append(messages, map[string]string{
		"role":    "system",
		"content": systemPrompt,
	})
	// 添加 user message（市场数据）
	messages = append(messages, map[string]string{
		"role":    "user", 
		"content": userPrompt,
	})

	maxRetries := 3  // 最大重试3次
	var lastErr error
	
	for attempt := 1; attempt <= maxRetries; attempt++ {
		response, err := callOnce(config, messages)
		if err == nil {
			return response, nil  // 成功返回
		}
		
		// 网络错误时智能重试
		if isNetworkError(err) {
			lastErr = err
			time.Sleep(time.Duration(attempt) * 2 * time.Second)  // 指数退避
			continue
		}
		
		return "", err  // 非网络错误直接返回
	}
	
	return "", fmt.Errorf("重试%d次后仍然失败: %w", maxRetries, lastErr)
}

// callOnce 单次调用AI API
func callOnce(config Config, messages []map[string]string) (string, error) {
	// 构建请求体
	requestBody := map[string]interface{}{
		"model":       config.Model,
		"messages":    messages,
		"temperature": 0.5,  // 较低温度提高JSON稳定性
		"max_tokens":  2000, // 足够返回思维链+JSON
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("序列化请求失败: %w", err)
	}

	// 创建HTTP请求
	req, err := http.NewRequest("POST", config.BaseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	
	// 设置认证头
	if config.Provider == ProviderDeepSeek {
		req.Header.Set("Authorization", "Bearer "+config.APIKey)
	} else if config.Provider == ProviderQwen {
		req.Header.Set("Authorization", "Bearer "+config.APIKey)
		// 阿里云可能需要额外的认证头
	}

	// 发送请求
	client := &http.Client{Timeout: config.Timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("HTTP错误状态码: %d", resp.StatusCode)
	}

	// 解析响应
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	// 提取AI回复内容
	if choices, ok := result["choices"].([]interface{}); ok && len(choices) > 0 {
		if choice, ok := choices[0].(map[string]interface{}); ok {
			if message, ok := choice["message"].(map[string]interface{}); ok {
				if content, ok := message["content"].(string); ok {
					return content, nil  // 返回AI的完整回复
				}
			}
		}
	}

	return "", errors.New("无法解析AI响应")
}
