package news

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"autobot/internal/config"
	loggerpkg "autobot/internal/logger"
)

// Article描述从新闻API或AI中提取的事件。
type Article struct {
	Title       string    `json:"title"`
	Summary     string    `json:"summary"`
	URL         string    `json:"url"`
	Source      string    `json:"source"`
	PublishedAt time.Time `json:"publishedAt"`
}

// SentimentSummary为新闻情绪分析结果。
type SentimentSummary struct {
	Sentiment   string   `json:"sentiment"`
	Score       float64  `json:"score"`
	Highlights  []string `json:"highlights"`
	RiskFactors []string `json:"riskFactors"`
}

// Fetcher负责从外部接口拉取新闻。
type Fetcher struct {
	httpClient *http.Client
	cfg        config.NewsConfig
	apiKey     string
	cacheTTL   time.Duration

	mu      sync.Mutex
	cached  []Article
	expires time.Time
	logger  *loggerpkg.ModuleLogger
}

// NewFetcher 创建新闻抓取器。
func NewFetcher(apiKey string, cfg config.NewsConfig, cacheTTL time.Duration) *Fetcher {
	if !cfg.Enabled {
		return nil
	}
	module := cfg.Provider
	if module == "" {
		module = "generic"
	}
	moduleLogger := loggerpkg.Get(fmt.Sprintf("news.%s", strings.ToLower(module)))
	if strings.EqualFold(cfg.Provider, "blockbeats") && cfg.BlockbeatsDisabled {
		if moduleLogger != nil {
			moduleLogger.Printf("provider disabled via config")
		}
		return nil
	}
	return &Fetcher{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		cfg:        cfg,
		apiKey:     apiKey,
		cacheTTL:   cacheTTL,
		logger:     moduleLogger,
	}
}

// FetchLatest 按配置抓取最新新闻，并带缓存。
func (f *Fetcher) FetchLatest(ctx context.Context) ([]Article, error) {
	if f == nil {
		return nil, errors.New("news fetcher is nil")
	}

	if articles := f.cachedCopy(); len(articles) > 0 {
		if f.logger != nil {
			f.logger.Printf("cache.hit count=%d", len(articles))
		}
		return articles, nil
	}

	var (
		items []Article
		err   error
	)

	switch strings.ToLower(f.cfg.Provider) {
	case "binance":
		items, err = f.fetchBinance(ctx)
	case "cryptopanic":
		items, err = f.fetchCryptoPanic(ctx)
	case "blockbeats":
		items, err = f.fetchBlockBeats(ctx)
	default:
		items, err = f.fetchGeneric(ctx)
	}
	if err != nil {
		if f.logger != nil {
			f.logger.Printf("fetch.error provider=%s err=%v", f.cfg.Provider, err)
		}
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("新闻源未返回有效内容")
	}

	f.storeCache(items)
	if f.logger != nil {
		f.logger.Printf("fetch.success provider=%s count=%d", f.cfg.Provider, len(items))
	}
	return items, nil
}

func (f *Fetcher) cachedCopy() []Article {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.cached) == 0 {
		return nil
	}
	if time.Now().After(f.expires) {
		if f.logger != nil {
			f.logger.Printf("cache.expired")
		}
		return nil
	}
	clone := make([]Article, len(f.cached))
	copy(clone, f.cached)
	return clone
}

func (f *Fetcher) storeCache(items []Article) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cached = make([]Article, len(items))
	copy(f.cached, items)
	if f.cacheTTL <= 0 {
		f.cacheTTL = 2 * time.Minute
	}
	f.expires = time.Now().Add(f.cacheTTL)
	if f.logger != nil {
		f.logger.Printf("cache.store count=%d ttl=%s", len(items), f.cacheTTL)
	}
}

func (f *Fetcher) fetchGeneric(ctx context.Context) ([]Article, error) {
	if f.cfg.APIURL == "" {
		return nil, errors.New("news apiUrl为空")
	}

	endpoint, err := url.Parse(f.cfg.APIURL)
	if err != nil {
		return nil, fmt.Errorf("parse news api url: %w", err)
	}

	query := endpoint.Query()
	if f.cfg.MaxItems > 0 {
		query.Set("limit", fmt.Sprintf("%d", f.cfg.MaxItems))
	}
	if f.cfg.Lookback != "" {
		query.Set("lookback", f.cfg.Lookback)
	}
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	if f.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+f.apiKey)
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch news: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		if f.logger != nil {
			f.logger.Printf("generic.status provider=%s status=%d", f.cfg.Provider, resp.StatusCode)
		}
		return nil, fmt.Errorf("news api status %d", resp.StatusCode)
	}

	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode news response: %w", err)
	}

	items := extractArticles(raw)
	if len(items) == 0 {
		return nil, errors.New("news api未返回有效文章")
	}
	return items, nil
}

func (f *Fetcher) fetchCryptoPanic(ctx context.Context) ([]Article, error) {
	endpoint := f.cfg.APIURL
	if endpoint == "" {
		endpoint = "https://cryptopanic.com/api/v1/posts/"
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse cryptopanic url: %w", err)
	}

	query := parsed.Query()
	if f.apiKey != "" {
		query.Set("auth_token", f.apiKey)
	}
	if f.cfg.MaxItems > 0 {
		query.Set("limit", fmt.Sprintf("%d", f.cfg.MaxItems))
	}
	query.Set("kind", "news")
	if f.cfg.Lookback != "" {
		query.Set("filter", f.cfg.Lookback)
	}
	parsed.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch cryptopanic: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("cryptopanic status %d", resp.StatusCode)
	}

	var payload struct {
		Results []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			PublishedAt string `json:"published_at"`
			Source      struct {
				Title string `json:"title"`
			} `json:"source"`
			Metadata struct {
				Domain string `json:"domain"`
			} `json:"metadata"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode cryptopanic: %w", err)
	}

	articles := make([]Article, 0, len(payload.Results))
	for _, r := range payload.Results {
		if r.Title == "" {
			continue
		}
		source := r.Source.Title
		if source == "" {
			source = r.Metadata.Domain
		}
		articles = append(articles, Article{
			Title:       r.Title,
			URL:         r.URL,
			Source:      source,
			PublishedAt: parseTime(r.PublishedAt),
		})
	}
	return articles, nil
}

func (f *Fetcher) fetchBinance(ctx context.Context) ([]Article, error) {
	endpoint := f.cfg.APIURL
	if endpoint == "" {
		endpoint = "https://www.binance.com/bapi/composite/v1/public/cms/article/list/query"
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse binance url: %w", err)
	}

	query := parsed.Query()
	query.Set("page", "1")
	if f.cfg.MaxItems > 0 {
		query.Set("pageSize", fmt.Sprintf("%d", f.cfg.MaxItems))
	}
	parsed.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	if f.apiKey != "" {
		req.Header.Set("X-API-KEY", f.apiKey)
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch binance news: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("binance news status %d", resp.StatusCode)
	}

	var payload struct {
		Data struct {
			Articles []struct {
				Title       string `json:"title"`
				Description string `json:"description"`
				URL         string `json:"url"`
				ReleaseDate int64  `json:"releaseDate"`
			} `json:"articles"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode binance news: %w", err)
	}

	articles := make([]Article, 0, len(payload.Data.Articles))
	for _, item := range payload.Data.Articles {
		if item.Title == "" {
			continue
		}
		published := time.UnixMilli(item.ReleaseDate)
		articles = append(articles, Article{
			Title:       item.Title,
			Summary:     item.Description,
			URL:         item.URL,
			Source:      "Binance",
			PublishedAt: published,
		})
	}
	return articles, nil
}

func (f *Fetcher) fetchBlockBeats(ctx context.Context) ([]Article, error) {
	endpoint := f.cfg.APIURL
	if endpoint == "" {
		endpoint = "https://api.blockbeats.cn/v2/newsflash/list?page=1&limit=20&ios=-2&detective=1"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Origin", "https://www.theblockbeats.info")
	req.Header.Set("Referer", "https://www.theblockbeats.info/")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Sec-CH-UA", `"Google Chrome";v="141", "Not?A_Brand";v="8", "Chromium";v="141"`)
	req.Header.Set("Sec-CH-UA-Mobile", "?0")
	req.Header.Set("Sec-CH-UA-Platform", `"macOS"`)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36")
	req.Header.Set("lang", "cn")
	if f.apiKey != "" {
		req.Header.Set("token", f.apiKey)
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch blockbeats: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("blockbeats status %d", resp.StatusCode)
	}

	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode blockbeats: %w", err)
	}

	articles := extractBlockBeats(raw)
	if len(articles) == 0 {
		return nil, errors.New("blockbeats 未返回有效快讯")
	}
	return articles, nil
}

func extractArticles(raw map[string]any) []Article {
	var containers []any
	for _, key := range []string{"data", "results", "articles"} {
		if arr, ok := raw[key]; ok {
			if list, ok := arr.([]any); ok {
				containers = list
				break
			}
		}
	}
	if len(containers) == 0 {
		return nil
	}

	articles := make([]Article, 0, len(containers))
	for _, item := range containers {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		a := Article{}
		if v, ok := obj["title"].(string); ok {
			a.Title = v
		}
		if v, ok := obj["summary"].(string); ok {
			a.Summary = v
		} else if v, ok := obj["description"].(string); ok {
			a.Summary = v
		}
		if v, ok := obj["url"].(string); ok {
			a.URL = v
		} else if v, ok := obj["link"].(string); ok {
			a.URL = v
		}
		if v, ok := obj["source"].(string); ok {
			a.Source = v
		} else if src, ok := obj["source"].(map[string]any); ok {
			if name, ok := src["name"].(string); ok {
				a.Source = name
			}
		}
		if v, ok := obj["published_at"].(string); ok {
			a.PublishedAt = parseTime(v)
		} else if v, ok := obj["publishedAt"].(string); ok {
			a.PublishedAt = parseTime(v)
		}

		if a.Title == "" {
			continue
		}
		if a.Summary == "" {
			a.Summary = ""
		}
		articles = append(articles, a)
	}

	return articles
}

func extractBlockBeats(raw map[string]any) []Article {
	var list []any
	if data, ok := raw["data"]; ok {
		switch typed := data.(type) {
		case map[string]any:
			if l, ok := typed["list"].([]any); ok {
				list = l
			} else if l, ok := typed["items"].([]any); ok {
				list = l
			}
		case []any:
			list = typed
		}
	}
	if list == nil && raw["list"] != nil {
		if l, ok := raw["list"].([]any); ok {
			list = l
		}
	}
	if len(list) == 0 {
		return nil
	}
	articles := make([]Article, 0, len(list))
	for _, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		title := strVal(obj, "title")
		if title == "" {
			title = strVal(obj, "content_title")
		}
		summary := strVal(obj, "content")
		if summary == "" {
			summary = strVal(obj, "summary")
		}
		if summary == "" {
			summary = strVal(obj, "description")
		}
		url := strVal(obj, "url")
		if url == "" {
			url = strVal(obj, "jump_url")
		}
		published := parseBlockBeatsTime(obj)
		if title == "" && summary == "" {
			continue
		}
		if published.IsZero() {
			if logger := loggerpkg.Get("news.blockbeats"); logger != nil {
				logger.Printf("missing publish time keys=%v", objectKeys(obj))
			}
			published = time.Now()
		}
		articles = append(articles, Article{
			Title:       title,
			Summary:     summary,
			URL:         url,
			Source:      "BlockBeats",
			PublishedAt: published,
		})
	}
	return articles
}

func strVal(obj map[string]any, key string) string {
	if v, ok := obj[key]; ok {
		return strings.TrimSpace(fmt.Sprint(v))
	}
	return ""
}

func parseBlockBeatsTime(obj map[string]any) time.Time {
	for _, key := range []string{"publish_time", "publishTime", "publish_time_str", "flash_time", "add_time", "created_at", "createdAt", "updated_at", "updatedAt", "createdTime", "post_time", "release_time"} {
		if v, ok := obj[key]; ok {
			switch t := v.(type) {
			case float64:
				if t > 1e12 {
					return time.UnixMilli(int64(t))
				}
				return time.Unix(int64(t), 0)
			case string:
				if ts := parseTime(t); !ts.IsZero() {
					return ts
				}
				if unix, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64); err == nil {
					if unix > 1e12 {
						return time.UnixMilli(unix)
					}
					return time.Unix(unix, 0)
				}
			}
		}
	}
	return time.Time{}
}

func parseTime(v string) time.Time {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006/01/02 15:04:05",
		"2006/01/02 15:04",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if ts, err := time.ParseInLocation(layout, v, time.Local); err == nil {
			return ts
		}
	}
	if strings.Count(v, ":") == 1 && len(v) == 5 {
		parts := strings.Split(v, ":")
		if len(parts) == 2 {
			hour, herr := strconv.Atoi(parts[0])
			minute, merr := strconv.Atoi(parts[1])
			if herr == nil && merr == nil {
				now := time.Now()
				return time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.Local)
			}
		}
	}
	return time.Time{}
}

func objectKeys(obj map[string]any) []string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
