package ai

import (
	"context"

	"autobot/internal/news"
)

// DecisionRequest 提供给AI的交易上下文。
type DecisionRequest struct {
	TraderName       string                `json:"traderName"`
	Exchange         string                `json:"exchange"`
	Symbol           string                `json:"symbol"`
	CurrentPrice     float64               `json:"currentPrice"`
	StrategySignal   string                `json:"strategySignal"`
	AccountBalance   float64               `json:"accountBalance"`
	AvailableBalance float64               `json:"availableBalance"`
	UnrealizedPNL    float64               `json:"unrealizedPnl"`
	Positions        []PositionSnapshot    `json:"positions"`
	LearningSnippets []string              `json:"learningSnippets"`
	NewsSentiment    news.SentimentSummary `json:"newsSentiment"`
	RiskLimits       RiskLimits            `json:"riskLimits"`
	Context          DecisionContext       `json:"context"`
}

// PositionSnapshot 为AI压缩后的持仓信息。
type PositionSnapshot struct {
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	Quantity      float64 `json:"quantity"`
	EntryPrice    float64 `json:"entryPrice"`
	Leverage      float64 `json:"leverage"`
	UnrealizedPNL float64 `json:"unrealizedPnl"`
}

// RiskLimits 提供风控边界。
type RiskLimits struct {
	MaxDailyLossPercent    float64 `json:"maxDailyLossPercent"`
	MaxPositionNotionalUSD float64 `json:"maxPositionNotionalUsd"`
	MaxConcurrentPositions int     `json:"maxConcurrentPositions"`
	MaxLeverage            float64 `json:"maxLeverage"`
	BtcEthNotionalMultiple float64 `json:"btcEthNotionalMultiple"`
	AltNotionalMultiple    float64 `json:"altNotionalMultiple"`
	MinRiskRewardRatio     float64 `json:"minRiskRewardRatio"`
}

// DecisionContext 提供更丰富的状态背景。
type DecisionContext struct {
	CurrentTime     string                        `json:"currentTime"`
	RuntimeMinutes  int                           `json:"runtimeMinutes"`
	CallCount       int                           `json:"callCount"`
	Account         AccountContext                `json:"account"`
	Positions       []PositionContext             `json:"positions"`
	CandidateCoins  []CandidateContext            `json:"candidateCoins"`
	MarketData      map[string]MarketDataSnapshot `json:"marketData"`
	OITopData       map[string]OITopSnapshot      `json:"oiTopData"`
	Performance     PerformanceStats              `json:"performance"`
	BTCETHLeverage  int                           `json:"btcEthLeverage"`
	AltcoinLeverage int                           `json:"altcoinLeverage"`
	MarginUsage     float64                       `json:"marginUsage"`
	InitialEquity   float64                       `json:"initialEquity"`
	PnLPercent      float64                       `json:"pnlPercent"`
}

type AccountContext struct {
	TotalEquity   float64 `json:"totalEquity"`
	Available     float64 `json:"available"`
	UnrealizedPNL float64 `json:"unrealizedPnl"`
	DailyRealized float64 `json:"dailyRealized"`
	MaxDrawdown   float64 `json:"maxDrawdown"`
	MarginUsage   float64 `json:"marginUsage"`
}

type PositionContext struct {
	Symbol         string  `json:"symbol"`
	Side           string  `json:"side"`
	Quantity       float64 `json:"quantity"`
	EntryPrice     float64 `json:"entryPrice"`
	Leverage       float64 `json:"leverage"`
	UnrealizedPNL  float64 `json:"unrealizedPnl"`
	HoldingMinutes int     `json:"holdingMinutes"`
	MarkPrice      float64 `json:"markPrice"`
	UnrealizedPct  float64 `json:"unrealizedPct"`
	MarginUsed     float64 `json:"marginUsed"`
	Liquidation    float64 `json:"liquidationPrice"`
}

type CandidateContext struct {
	Symbol string  `json:"symbol"`
	Weight float64 `json:"weight"`
	Reason string  `json:"reason"`
}

type MarketDataSnapshot struct {
	Symbol        string  `json:"symbol"`
	CurrentPrice  float64 `json:"currentPrice"`
	PriceChange1h float64 `json:"priceChange1h"`
	PriceChange4h float64 `json:"priceChange4h"`
	EMA20         float64 `json:"ema20"`
	MACD          float64 `json:"macd"`
	MACDSignal    float64 `json:"macdSignal"`
	RSI7          float64 `json:"rsi7"`
	RSI14         float64 `json:"rsi14"`
	FundingRate   float64 `json:"fundingRate"`
	OpenInterest  float64 `json:"openInterest"`
	Volume24h     float64 `json:"volume24h"`
	DataInterval  string  `json:"dataInterval"`
}

type OITopSnapshot struct {
	Symbol       string  `json:"symbol"`
	Rank         int     `json:"rank"`
	OpenInterest float64 `json:"openInterest"`
	Notional     float64 `json:"notional"`
}

type PerformanceStats struct {
	SharpeRatio  float64 `json:"sharpeRatio"`
	WinRate      float64 `json:"winRate"`
	TotalTrades  int     `json:"totalTrades"`
	ProfitFactor float64 `json:"profitFactor"`
}

// DecisionResponse 为AI返回的结构化交易建议。
type DecisionResponse struct {
	Action      string         `json:"action"`
	Confidence  float64        `json:"confidence"`
	Reason      string         `json:"reason"`
	Adjustments AdjustmentPlan `json:"adjustments"`
	RiskNotes   []string       `json:"riskNotes"`
	RawContent  string         `json:"-"`
	CoTTrace    string         `json:"-"`
}

// AdjustmentPlan 用于AI微调仓位与风控参数。
type AdjustmentPlan struct {
	SizeMultiplier      float64 `json:"sizeMultiplier"`
	TargetLeverage      float64 `json:"targetLeverage"`
	StopLossPercent     float64 `json:"stopLossPercent"`
	TakeProfitPercent   float64 `json:"takeProfitPercent"`
	TrailingStopPercent float64 `json:"trailingStopPercent"`
}

// Provider 为AI决策引擎统一接口。
type Provider interface {
	AnalyzeNews(ctx context.Context, articles []news.Article) (news.SentimentSummary, error)
	GenerateDecision(ctx context.Context, req DecisionRequest) (DecisionResponse, error)
}
