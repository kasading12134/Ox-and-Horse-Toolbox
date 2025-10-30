package pool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	loggerpkg "autobot/internal/logger"
)

var defaultMainstreamCoins = []string{
	"BTCUSDT",
	"ETHUSDT",
	"SOLUSDT",
	"BNBUSDT",
	"XRPUSDT",
	"DOGEUSDT",
	"ADAUSDT",
	"HYPEUSDT",
}

// Config 定义币种池服务的运行参数。
type Config struct {
	UseDefault     bool
	CoinPoolAPIURL string
	CoinPoolAPIKey string
	OITopAPIURL    string
	OITopAPIKey    string
	CacheTTL       time.Duration
	MaxCombined    int
}

// CoinInfo 描述单个币种的来源与评分。
type CoinInfo struct {
	Symbol  string
	Score   float64
	Sources []string
}

// Service 负责聚合多源币种池并提供缓存。
type Service struct {
	cfg     Config
	client  *http.Client
	logger  *loggerpkg.ModuleLogger
	mu      sync.Mutex
	cache   []CoinInfo
	expires time.Time
}

// NewService 创建币种池服务。
func NewService(cfg Config) *Service {
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = 5 * time.Minute
	}
	if cfg.MaxCombined <= 0 {
		cfg.MaxCombined = 32
	}
	return &Service{
		cfg: cfg,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		logger: loggerpkg.Get("pool"),
	}
}

// Select 返回推荐的币种列表，按照score降序排序。
func (s *Service) Select(ctx context.Context, limit int) []CoinInfo {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if len(s.cache) > 0 && now.Before(s.expires) {
		return cloneCoins(s.cache, limit)
	}

	coins := s.refresh(ctx)
	if len(coins) == 0 {
		coins = convertSymbolsToCoins(defaultMainstreamCoins)
	}
	s.cache = coins
	s.expires = now.Add(s.cfg.CacheTTL)
	return cloneCoins(s.cache, limit)
}

func (s *Service) refresh(ctx context.Context) []CoinInfo {
	aggregated := map[string]*CoinInfo{}
	merge := func(infos []CoinInfo) {
		for _, info := range infos {
			symbol := normalizeSymbol(info.Symbol)
			if symbol == "" {
				continue
			}
			if existing, ok := aggregated[symbol]; ok {
				existing.Score = maxFloat(existing.Score, info.Score)
				existing.Sources = mergeSources(existing.Sources, info.Sources)
				continue
			}
			copySources := append([]string(nil), info.Sources...)
			aggregated[symbol] = &CoinInfo{Symbol: symbol, Score: info.Score, Sources: copySources}
		}
	}

	if coins, err := s.fetchCoins(ctx, s.cfg.CoinPoolAPIURL, s.cfg.CoinPoolAPIKey, "ai500", 1.2); err == nil {
		merge(coins)
	} else if err != nil && s.logger != nil {
		s.logger.Printf("coin_pool.fetch.ai500 error=%v", err)
	}

	if coins, err := s.fetchCoins(ctx, s.cfg.OITopAPIURL, s.cfg.OITopAPIKey, "oi-top", 1.0); err == nil {
		merge(coins)
	} else if err != nil && s.logger != nil {
		s.logger.Printf("coin_pool.fetch.oitop error=%v", err)
	}

	if len(aggregated) == 0 || s.cfg.UseDefault {
		merge(convertSymbolsToCoins(defaultMainstreamCoins))
	}

	list := make([]CoinInfo, 0, len(aggregated))
	for _, info := range aggregated {
		sort.Strings(info.Sources)
		list = append(list, *info)
	}
	sort.SliceStable(list, func(i, j int) bool {
		if almostEqual(list[i].Score, list[j].Score) {
			return list[i].Symbol < list[j].Symbol
		}
		return list[i].Score > list[j].Score
	})

	if s.cfg.MaxCombined > 0 && len(list) > s.cfg.MaxCombined {
		list = list[:s.cfg.MaxCombined]
	}
	return list
}

func (s *Service) fetchCoins(ctx context.Context, url, apiKey, tag string, baseScore float64) ([]CoinInfo, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, errors.New("api url empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", apiKey)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var payload interface{}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	symbols := extractSymbols(payload)
	coins := make([]CoinInfo, 0, len(symbols))
	for i, sym := range symbols {
		if sym == "" {
			continue
		}
		score := baseScore - float64(i)*0.01
		if score < 0 {
			score = 0
		}
		coins = append(coins, CoinInfo{Symbol: sym, Score: score, Sources: []string{tag}})
	}
	return coins, nil
}

func convertSymbolsToCoins(symbols []string) []CoinInfo {
	coins := make([]CoinInfo, 0, len(symbols))
	for idx, sym := range symbols {
		norm := normalizeSymbol(sym)
		if norm == "" {
			continue
		}
		score := 0.8 - float64(idx)*0.01
		if score < 0.1 {
			score = 0.1
		}
		coins = append(coins, CoinInfo{Symbol: norm, Score: score, Sources: []string{"default"}})
	}
	return coins
}

func cloneCoins(in []CoinInfo, limit int) []CoinInfo {
	if limit <= 0 || limit > len(in) {
		limit = len(in)
	}
	copySlice := make([]CoinInfo, 0, limit)
	for i := 0; i < limit; i++ {
		sources := append([]string(nil), in[i].Sources...)
		copySlice = append(copySlice, CoinInfo{Symbol: in[i].Symbol, Score: in[i].Score, Sources: sources})
	}
	return copySlice
}

func mergeSources(existing, incoming []string) []string {
	set := make(map[string]struct{}, len(existing)+len(incoming))
	for _, s := range existing {
		set[s] = struct{}{}
	}
	for _, s := range incoming {
		set[s] = struct{}{}
	}
	merged := make([]string, 0, len(set))
	for src := range set {
		merged = append(merged, src)
	}
	return merged
}

func extractSymbols(node interface{}) []string {
	set := make(map[string]struct{})
	walkSymbols(node, set)
	result := make([]string, 0, len(set))
	for sym := range set {
		result = append(result, sym)
	}
	sort.Strings(result)
	return result
}

func walkSymbols(node interface{}, set map[string]struct{}) {
	switch v := node.(type) {
	case map[string]interface{}:
		for key, val := range v {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "symbol") || strings.Contains(lower, "pair") {
				if str, ok := val.(string); ok {
					if sym := normalizeSymbol(str); sym != "" {
						set[sym] = struct{}{}
					}
				}
			}
			walkSymbols(val, set)
		}
	case []interface{}:
		for _, item := range v {
			walkSymbols(item, set)
		}
	case string:
		if sym := normalizeSymbol(v); sym != "" {
			set[sym] = struct{}{}
		}
	}
}

func normalizeSymbol(input string) string {
	s := strings.ToUpper(strings.TrimSpace(input))
	if s == "" {
		return ""
	}
	if strings.ContainsAny(s, " \t\n/\\") {
		return ""
	}
	if len(s) < 6 || len(s) > 20 {
		return ""
	}
	if strings.HasSuffix(s, "USDT") || strings.HasSuffix(s, "USDC") || strings.HasSuffix(s, "USD") {
		return s
	}
	return ""
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func almostEqual(a, b float64) bool {
	if a == b {
		return true
	}
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < 1e-6
}
