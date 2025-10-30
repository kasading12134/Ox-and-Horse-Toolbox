package storage

import (
	"context"
	"fmt"

	"autobot/internal/ai"
	"autobot/internal/config"
)

// Store 定义交易记录的持久化接口。
type Store interface {
	RecordDecision(ctx context.Context, record DecisionRecord) error
	RecordTrade(ctx context.Context, record TradeRecord) error
	RecentDecisions(ctx context.Context, limit int) ([]DecisionRecord, error)
	RecentTrades(ctx context.Context, limit int) ([]TradeRecord, error)
	Close() error
}

// DecisionRecord 记录一次AI决策。
type DecisionRecord struct {
	ID           string
	Trader       string
	Provider     string
	Symbol       string
	Action       string
	Confidence   float64
	Reason       string
	Adjust       ai.AdjustmentPlan
	RiskNotes    []string
	Raw          string
	CreatedAt    int64
	
	// 反思模块新增字段
	CycleNumber  int                   // 周期编号
	InputPrompt  string                // 发送给AI的输入prompt
	CoTTrace     string                // AI思维链（输出）
	AccountState AccountSnapshot       // 账户状态快照
	Positions    []PositionSnapshot    // 持仓快照
	ExecutionLog []string              // 执行日志
	Success      bool                  // 是否成功
	ErrorMessage string                // 错误信息
}

// AccountSnapshot 账户状态快照
type AccountSnapshot struct {
	TotalEquity   float64 `json:"totalEquity"`
	Available     float64 `json:"available"`
	UnrealizedPNL float64 `json:"unrealizedPnl"`
	MarginUsage   float64 `json:"marginUsage"`
	Timestamp     int64   `json:"timestamp"`
}

// PositionSnapshot 持仓快照
type PositionSnapshot struct {
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	Quantity      float64 `json:"quantity"`
	EntryPrice    float64 `json:"entryPrice"`
	Leverage      float64 `json:"leverage"`
	UnrealizedPNL float64 `json:"unrealizedPnl"`
	MarkPrice     float64 `json:"markPrice"`
	UpdateTime    int64   `json:"updateTime"`  // 持仓更新时间戳
}

// TradeRecord 记录实际成交或仓位变动。
type TradeRecord struct {
	ID        string
	Trader    string
	Symbol    string
	Side      string
	Quantity  float64
	Price     float64
	Action    string
	PnL       float64
	Notes     string
	CreatedAt int64
}

// New 根据配置创建持久化实现。
func New(cfg config.StorageConfig) (Store, error) {
	switch cfg.Type {
	case "file", "":
		return newFileStore(cfg)
	default:
		return nil, fmt.Errorf("unsupported storage type %s", cfg.Type)
	}
}
