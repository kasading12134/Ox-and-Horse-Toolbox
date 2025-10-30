package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// Config 描述全局配置文件结构。
type Config struct {
	Global    GlobalConfig    `json:"global"`
	Traders   []TraderProfile `json:"traders"`
	Deepseek  DeepseekConfig  `json:"deepseek"`
	Qwen      QwenConfig      `json:"qwen"`
	News      NewsConfig      `json:"news"`
	Risk      RiskConfig      `json:"risk"`
	Storage   StorageConfig   `json:"storage"`
	Logging   LoggingConfig   `json:"logging"`
	Exchanges ExchangeConfig  `json:"exchanges"`
	CoinPool  CoinPoolConfig  `json:"coinPool"`
}

// GlobalConfig 定义全局默认值。
type GlobalConfig struct {
	EvaluationInterval  string        `json:"evaluationInterval"`
	ScanIntervalMinutes int           `json:"scanIntervalMinutes"`
	DryRun              bool          `json:"dryRun"`
	Defaults            TradeSettings `json:"defaults"`
}

// TraderProfile 定义单个自动交易者。
type TraderProfile struct {
	Name             string        `json:"name"`
	Exchange         string        `json:"exchange"`
	Symbol           string        `json:"symbol"`
	Interval         string        `json:"interval"`
	DecisionProvider string        `json:"decisionProvider"`
	Settings         TradeSettings `json:"settings"`
}

// TradeSettings 包含交易参数。
type TradeSettings struct {
	ContractType        string   `json:"contractType"`
	Leverage            int      `json:"leverage"`
	OrderQuantity       float64  `json:"orderQuantity"`
	RiskPerTradePercent float64  `json:"riskPerTradePercent"`
	StopLossPercent     float64  `json:"stopLossPercent"`
	TakeProfitPercent   float64  `json:"takeProfitPercent"`
	TrailingStopPercent float64  `json:"trailingStopPercent"`
	MaxExposurePercent  float64  `json:"maxExposurePercent"`
	SlippagePercent     float64  `json:"slippagePercent"`
	LookbackCandles     int      `json:"lookbackCandles"`
	LearningWindow      int      `json:"learningWindow"`
	FastEMAPeriod       int      `json:"fastEmaPeriod"`
	SlowEMAPeriod       int      `json:"slowEmaPeriod"`
	RSIPeriod           int      `json:"rsiPeriod"`
	RSIUpper            float64  `json:"rsiUpper"`
	RSILower            float64  `json:"rsiLower"`
	MACDFastPeriod      int      `json:"macdFastPeriod"`
	MACDSlowPeriod      int      `json:"macdSlowPeriod"`
	MACDSignalPeriod    int      `json:"macdSignalPeriod"`
	CandidateSymbols    []string `json:"candidateSymbols"`
}

// DeepseekConfig 描述 DeepSeek AI 服务参数。
type DeepseekConfig struct {
	Enabled     bool    `json:"enabled"`
	BaseURL     string  `json:"baseUrl"`
	APIKey      string  `json:"apiKey"`
	Model       string  `json:"model"`
	Temperature float64 `json:"temperature"`
	TopP        float64 `json:"topP"`
	MaxTokens   int     `json:"maxTokens"`
}

// QwenConfig 描述通义千问配置。
type QwenConfig struct {
	Enabled     bool    `json:"enabled"`
	BaseURL     string  `json:"baseUrl"`
	Model       string  `json:"model"`
	Temperature float64 `json:"temperature"`
	TopP        float64 `json:"topP"`
}

// NewsConfig 控制新闻源抓取。
type NewsConfig struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"`
	APIURL   string `json:"apiUrl"`
	APIKey   string `json:"apiKey"`
	MaxItems int    `json:"maxItems"`
	Lookback string `json:"lookback"`
	CacheTTL string `json:"cacheTtl"`
	// BlockbeatsDisabled 允许在保持其他新闻源启用的情况下单独关闭律动新闻。
	BlockbeatsDisabled bool `json:"blockbeatsDisabled"`
}

// CoinPoolConfig 控制多源币种池。
type CoinPoolConfig struct {
	UseDefaultCoins bool   `json:"use_default_coins"`
	CoinPoolAPIURL  string `json:"coin_pool_api_url"`
	CoinPoolAPIKey  string `json:"coin_pool_api_key"`
	OITopAPIURL     string `json:"oi_top_api_url"`
	OITopAPIKey     string `json:"oi_top_api_key"`
	CacheTTL        string `json:"cache_ttl"`
	MaxCombined     int    `json:"max_combined"`
}

// RiskConfig 定义附加风控。
type RiskConfig struct {
	MaxDailyLossPercent    float64 `json:"maxDailyLossPercent"`
	MaxPositionNotionalUSD float64 `json:"maxPositionNotionalUsd"`
	MaxConcurrentPositions int     `json:"maxConcurrentPositions"`
	CheckInterval          string  `json:"checkInterval"`
	MaxLeverage            float64 `json:"maxLeverage"`
	BtcEthNotionalMultiple float64 `json:"btcEthNotionalMultiple"`
	AltNotionalMultiple    float64 `json:"altNotionalMultiple"`
	MinRiskRewardRatio     float64 `json:"minRiskRewardRatio"`
}

// StorageConfig 控制持久化。
type StorageConfig struct {
	Type string `json:"type"`
	Path string `json:"path"`
}

// ParsedConfig 为运行时提供解析后的配置。
type ParsedConfig struct {
	Config
	EvaluationDuration time.Duration
	NewsCacheTTL       time.Duration
	RiskCheckDuration  time.Duration
	CoinPoolTTL        time.Duration
	TraderProfiles     []TraderProfileResolved
}

// TraderProfileResolved 合并全局默认值后的配置。
type TraderProfileResolved struct {
	TraderProfile
	Settings TradeSettings
	DryRun   bool
}

// Load 读取配置文件并应用默认值。
func Load(path string) (ParsedConfig, error) {
	if path == "" {
		return ParsedConfig{}, errors.New("config path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return ParsedConfig{}, fmt.Errorf("read config: %w", err)
	}

	cfg := Config{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ParsedConfig{}, fmt.Errorf("parse config json: %w", err)
	}

	applyDefaults(&cfg)
	if err := validate(cfg); err != nil {
		return ParsedConfig{}, err
	}

	var evaluationDuration time.Duration
	eval := cfg.Global.EvaluationInterval
	if cfg.Global.ScanIntervalMinutes > 0 {
		evaluationDuration = time.Duration(cfg.Global.ScanIntervalMinutes) * time.Minute
	} else {
		if eval == "" {
			eval = "30s"
		}
		var err error
		evaluationDuration, err = time.ParseDuration(eval)
		if err != nil {
			return ParsedConfig{}, fmt.Errorf("invalid evaluation interval %q: %w", eval, err)
		}
	}

	cacheTTL := cfg.News.CacheTTL
	if cacheTTL == "" {
		cacheTTL = "2m"
	}
	newsCacheDuration, err := time.ParseDuration(cacheTTL)
	if err != nil {
		return ParsedConfig{}, fmt.Errorf("invalid news cache ttl %q: %w", cacheTTL, err)
	}

	riskCheck := cfg.Risk.CheckInterval
	if riskCheck == "" {
		riskCheck = "5m"
	}
	riskCheckDuration, err := time.ParseDuration(riskCheck)
	if err != nil {
		return ParsedConfig{}, fmt.Errorf("invalid risk check interval %q: %w", riskCheck, err)
	}

	poolTTL := cfg.CoinPool.CacheTTL
	if poolTTL == "" {
		poolTTL = "5m"
	}
	coinPoolTTL, err := time.ParseDuration(poolTTL)
	if err != nil {
		return ParsedConfig{}, fmt.Errorf("invalid coin pool cache ttl %q: %w", poolTTL, err)
	}

	resolved := resolveProfiles(cfg)

	return ParsedConfig{
		Config:             cfg,
		EvaluationDuration: evaluationDuration,
		NewsCacheTTL:       newsCacheDuration,
		RiskCheckDuration:  riskCheckDuration,
		CoinPoolTTL:        coinPoolTTL,
		TraderProfiles:     resolved,
	}, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Global.EvaluationInterval == "" {
		cfg.Global.EvaluationInterval = "30s"
	}
	// 默认交易参数
	defaults := &cfg.Global.Defaults
	if defaults.ContractType == "" {
		defaults.ContractType = "PERPETUAL"
	}
	if defaults.Leverage == 0 {
		defaults.Leverage = 5
	}
	if defaults.OrderQuantity == 0 {
		defaults.OrderQuantity = 0.001
	}
	if defaults.RiskPerTradePercent == 0 {
		defaults.RiskPerTradePercent = 1.0
	}
	if defaults.StopLossPercent == 0 {
		defaults.StopLossPercent = 0.5
	}
	if defaults.TakeProfitPercent == 0 {
		defaults.TakeProfitPercent = 1.0
	}
	if defaults.TrailingStopPercent == 0 {
		defaults.TrailingStopPercent = 0.3
	}
	if defaults.MaxExposurePercent == 0 {
		defaults.MaxExposurePercent = 5.0
	}
	if defaults.SlippagePercent == 0 {
		defaults.SlippagePercent = 0.05
	}
	if defaults.LookbackCandles == 0 {
		defaults.LookbackCandles = 120
	}
	if defaults.LearningWindow == 0 {
		defaults.LearningWindow = 50
	}
	if defaults.FastEMAPeriod == 0 {
		defaults.FastEMAPeriod = 12
	}
	if defaults.SlowEMAPeriod == 0 {
		defaults.SlowEMAPeriod = 26
	}
	if defaults.RSIPeriod == 0 {
		defaults.RSIPeriod = 14
	}
	if defaults.RSIUpper == 0 {
		defaults.RSIUpper = 55
	}
	if defaults.RSILower == 0 {
		defaults.RSILower = 45
	}
	if defaults.MACDFastPeriod == 0 {
		defaults.MACDFastPeriod = 12
	}
	if defaults.MACDSlowPeriod == 0 {
		defaults.MACDSlowPeriod = 26
	}
	if defaults.MACDSignalPeriod == 0 {
		defaults.MACDSignalPeriod = 9
	}

	if cfg.Deepseek.BaseURL == "" {
		cfg.Deepseek.BaseURL = "https://api.deepseek.com"
	}
	if cfg.Deepseek.Model == "" {
		cfg.Deepseek.Model = "deepseek-chat"
	}
	if cfg.Deepseek.Temperature == 0 {
		cfg.Deepseek.Temperature = 0.3
	}
	if cfg.Deepseek.TopP == 0 {
		cfg.Deepseek.TopP = 0.9
	}
	if cfg.Deepseek.MaxTokens == 0 {
		cfg.Deepseek.MaxTokens = 2000
	}

	if cfg.Qwen.BaseURL == "" {
		cfg.Qwen.BaseURL = "https://dashscope.aliyuncs.com"
	}
	if cfg.Qwen.Model == "" {
		cfg.Qwen.Model = "qwen-turbo"
	}
	if cfg.Qwen.Temperature == 0 {
		cfg.Qwen.Temperature = 0.4
	}
	if cfg.Qwen.TopP == 0 {
		cfg.Qwen.TopP = 0.8
	}

	if cfg.News.MaxItems == 0 {
		cfg.News.MaxItems = 20
	}
	if cfg.News.Lookback == "" {
		cfg.News.Lookback = "2h"
	}
	if cfg.News.Provider == "" {
		cfg.News.Provider = "cryptopanic"
	}
	if cfg.News.CacheTTL == "" {
		cfg.News.CacheTTL = "2m"
	}

	if cfg.Risk.MaxDailyLossPercent == 0 {
		cfg.Risk.MaxDailyLossPercent = 5
	}
	if cfg.Risk.MaxConcurrentPositions == 0 {
		cfg.Risk.MaxConcurrentPositions = 1
	}
	if cfg.Risk.CheckInterval == "" {
		cfg.Risk.CheckInterval = "5m"
	}
	if cfg.Risk.MaxLeverage == 0 {
		cfg.Risk.MaxLeverage = float64(cfg.Global.Defaults.Leverage)
	}
	if cfg.Risk.BtcEthNotionalMultiple == 0 {
		cfg.Risk.BtcEthNotionalMultiple = 10
	}
	if cfg.Risk.AltNotionalMultiple == 0 {
		cfg.Risk.AltNotionalMultiple = 1.5
	}
	if cfg.Risk.MinRiskRewardRatio == 0 {
		cfg.Risk.MinRiskRewardRatio = 3
	}

	if cfg.Storage.Type == "" {
		cfg.Storage.Type = "file"
	}
	if cfg.Storage.Path == "" {
		cfg.Storage.Path = "data"
	}

	if cfg.Logging.Directory == "" {
		cfg.Logging.Directory = "logs"
	}

	if cfg.CoinPool.CacheTTL == "" {
		cfg.CoinPool.CacheTTL = "5m"
	}
	if cfg.CoinPool.MaxCombined == 0 {
		cfg.CoinPool.MaxCombined = 24
	}
	if cfg.CoinPool.CoinPoolAPIURL == "" && cfg.CoinPool.OITopAPIURL == "" && !cfg.CoinPool.UseDefaultCoins {
		// 当未配置外部源时，默认启用主流币种作为兜底
		cfg.CoinPool.UseDefaultCoins = true
	}
}

func validate(cfg Config) error {
	if len(cfg.Traders) == 0 {
		return errors.New("traders 配置不能为空")
	}

	for _, trader := range cfg.Traders {
		if trader.Symbol == "" {
			return fmt.Errorf("trader %q 缺少 symbol", trader.Name)
		}
		if trader.Interval == "" {
			return fmt.Errorf("trader %q 缺少 interval", trader.Name)
		}
		settings := mergeSettings(cfg.Global.Defaults, trader.Settings)
		if settings.FastEMAPeriod >= settings.SlowEMAPeriod {
			return fmt.Errorf("trader %s fastEmaPeriod must be smaller than slowEmaPeriod", trader.Name)
		}
		if settings.RiskPerTradePercent <= 0 || settings.RiskPerTradePercent > 5 {
			return fmt.Errorf("trader %s riskPerTradePercent out of bounds", trader.Name)
		}
		if settings.StopLossPercent <= 0 {
			return fmt.Errorf("trader %s stopLossPercent must be positive", trader.Name)
		}
		if settings.TakeProfitPercent <= 0 {
			return fmt.Errorf("trader %s takeProfitPercent must be positive", trader.Name)
		}
		if settings.OrderQuantity <= 0 {
			return fmt.Errorf("trader %s orderQuantity must be positive", trader.Name)
		}
		if settings.RSIPeriod <= 0 {
			return fmt.Errorf("trader %s rsiPeriod must be positive", trader.Name)
		}
		if settings.MACDFastPeriod <= 0 || settings.MACDSlowPeriod <= 0 || settings.MACDSignalPeriod <= 0 {
			return fmt.Errorf("trader %s macd periods must be positive", trader.Name)
		}
		if settings.MACDFastPeriod >= settings.MACDSlowPeriod {
			return fmt.Errorf("trader %s macdFastPeriod must be smaller than macdSlowPeriod", trader.Name)
		}
		if settings.RSIUpper <= settings.RSILower {
			return fmt.Errorf("trader %s rsiUpper must be greater than rsiLower", trader.Name)
		}
	}

	if cfg.Risk.MaxDailyLossPercent <= 0 {
		return errors.New("maxDailyLossPercent必须为正数")
	}
	if cfg.Risk.MaxConcurrentPositions <= 0 {
		return errors.New("maxConcurrentPositions必须为正数")
	}
	if cfg.Risk.MaxLeverage <= 0 {
		return errors.New("maxLeverage必须为正数")
	}
	if cfg.Risk.BtcEthNotionalMultiple <= 0 {
		return errors.New("btcEthNotionalMultiple必须为正数")
	}
	if cfg.Risk.AltNotionalMultiple <= 0 {
		return errors.New("altNotionalMultiple必须为正数")
	}
	if cfg.Risk.MinRiskRewardRatio <= 1 {
		return errors.New("minRiskRewardRatio必须大于1")
	}
	if cfg.CoinPool.MaxCombined <= 0 {
		return errors.New("coinPool.max_combined必须为正数")
	}

	return nil
}

func resolveProfiles(cfg Config) []TraderProfileResolved {
	resolved := make([]TraderProfileResolved, 0, len(cfg.Traders))
	for _, profile := range cfg.Traders {
		settings := mergeSettings(cfg.Global.Defaults, profile.Settings)
		resolved = append(resolved, TraderProfileResolved{
			TraderProfile: profile,
			Settings:      settings,
			DryRun:        cfg.Global.DryRun,
		})
	}
	return resolved
}

func mergeSettings(base TradeSettings, override TradeSettings) TradeSettings {
	result := base
	if override.ContractType != "" {
		result.ContractType = override.ContractType
	}
	if override.Leverage != 0 {
		result.Leverage = override.Leverage
	}
	if override.OrderQuantity != 0 {
		result.OrderQuantity = override.OrderQuantity
	}
	if override.RiskPerTradePercent != 0 {
		result.RiskPerTradePercent = override.RiskPerTradePercent
	}
	if override.StopLossPercent != 0 {
		result.StopLossPercent = override.StopLossPercent
	}
	if override.TakeProfitPercent != 0 {
		result.TakeProfitPercent = override.TakeProfitPercent
	}
	if override.TrailingStopPercent != 0 {
		result.TrailingStopPercent = override.TrailingStopPercent
	}
	if override.MaxExposurePercent != 0 {
		result.MaxExposurePercent = override.MaxExposurePercent
	}
	if override.SlippagePercent != 0 {
		result.SlippagePercent = override.SlippagePercent
	}
	if override.LookbackCandles != 0 {
		result.LookbackCandles = override.LookbackCandles
	}
	if override.LearningWindow != 0 {
		result.LearningWindow = override.LearningWindow
	}
	if override.FastEMAPeriod != 0 {
		result.FastEMAPeriod = override.FastEMAPeriod
	}
	if override.SlowEMAPeriod != 0 {
		result.SlowEMAPeriod = override.SlowEMAPeriod
	}
	if override.RSIPeriod != 0 {
		result.RSIPeriod = override.RSIPeriod
	}
	if override.RSIUpper != 0 {
		result.RSIUpper = override.RSIUpper
	}
	if override.RSILower != 0 {
		result.RSILower = override.RSILower
	}
	if override.MACDFastPeriod != 0 {
		result.MACDFastPeriod = override.MACDFastPeriod
	}
	if override.MACDSlowPeriod != 0 {
		result.MACDSlowPeriod = override.MACDSlowPeriod
	}
	if override.MACDSignalPeriod != 0 {
		result.MACDSignalPeriod = override.MACDSignalPeriod
	}
	if len(override.CandidateSymbols) > 0 {
		result.CandidateSymbols = append([]string{}, override.CandidateSymbols...)
	}
	return result
}

// LoggingConfig 控制日志输出。
type LoggingConfig struct {
	Directory    string `json:"directory"`
	MirrorStdout *bool  `json:"mirrorStdout"`
}

// MirrorToStdout 返回是否同时输出到标准输出。
func (l LoggingConfig) MirrorToStdout() bool {
	if l.MirrorStdout == nil {
		return true
	}
	return *l.MirrorStdout
}

type ExchangeConfig struct {
	Binance BinanceCredentials `json:"binance"`
}

type BinanceCredentials struct {
	APIKey    string `json:"apiKey"`
	APISecret string `json:"apiSecret"`
}
