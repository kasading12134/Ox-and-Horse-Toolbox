package deepseek

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"autobot/internal/ai"
)

// promptContext 汇总生成提示词所需的运行时信息。
type promptContext struct {
	Request ai.DecisionRequest
	Performance ai.PerformanceStats
	Positions []ai.PositionContext
}

func newPromptContext(req ai.DecisionRequest, performance ai.PerformanceStats, positions []ai.PositionContext) promptContext {
	return promptContext{
		Request: req,
		Performance: performance,
		Positions: positions,
	}
}

func formatNewsSummary(sentiment string, score float64) string {
	if sentiment == "" {
		return "无"
	}
	if score == 0 {
		return sentiment
	}
	return fmt.Sprintf("%s(%.2f)", sentiment, score)
}

// buildSystemPrompt 定义交易系统的硬性约束与目标。
func buildSystemPrompt(accountEquity float64, btcEthLeverage, altcoinLeverage int, limits ai.RiskLimits, performance ai.PerformanceStats, positions []ai.PositionContext) string {
	if btcEthLeverage <= 0 {
		btcEthLeverage = int(limits.MaxLeverage)
	}
	if btcEthLeverage <= 0 {
		btcEthLeverage = 5
	}
	if altcoinLeverage <= 0 {
		altcoinLeverage = btcEthLeverage
	}

	var sb strings.Builder
	sb.WriteString("你是专业的加密货币交易AI，在币安合约市场进行自主交易。\n\n")
	sb.WriteString("# 🎯 核心目标\n\n")
	sb.WriteString("**最大化夏普比率（Sharpe Ratio）**，并保持资本曲线稳定。\n\n")

	// 添加反思模块
	reflectionPrompt := buildReflectionPrompt(performance.SharpeRatio, performance, positions)
	sb.WriteString(reflectionPrompt)

	sb.WriteString("# 📏 硬性约束\n\n")
	sb.WriteString("- 所有决策必须以 JSON 形式输出，且字段完整。\n")
	sb.WriteString("- 必须严格遵守风险参数：\n")
	sb.WriteString(fmt.Sprintf("  * BTC/ETH 最大杠杆 %dx，名义仓位上限 %.1f × 账户净值。\n", btcEthLeverage, limits.BtcEthNotionalMultiple))
	sb.WriteString(fmt.Sprintf("  * 山寨币最大杠杆 %dx，名义仓位上限 %.1f × 账户净值。\n", altcoinLeverage, limits.AltNotionalMultiple))
	if limits.MaxPositionNotionalUSD > 0 {
		sb.WriteString(fmt.Sprintf("  * 单笔名义价值不得超过 %.2f USDT。\n", limits.MaxPositionNotionalUSD))
	}
	if limits.MaxConcurrentPositions > 0 {
		sb.WriteString(fmt.Sprintf("  * 最大同时持仓数：%d。\n", limits.MaxConcurrentPositions))
	}
	if limits.MinRiskRewardRatio > 0 {
		sb.WriteString(fmt.Sprintf("  * 止盈/止损需满足风险回报 ≥ %.1f。\n", limits.MinRiskRewardRatio))
	}
	if accountEquity > 0 {
		sb.WriteString(fmt.Sprintf("- 当前账户净值≈%.2f USDT，请优先考虑 8~10 USDT 保证金的开仓规模。\n", accountEquity))
	}

	sb.WriteString("\n# ✅ 决策必备字段\n\n")
	sb.WriteString("返回 JSON 时必须包含 action、confidence、reason、adjustments{sizeMultiplier,targetLeverage,stopLossPercent,takeProfitPercent,trailingStopPercent} 以及 riskNotes。\n")
	sb.WriteString("若无信号，请返回 action=\"wait\" 并说明理由。\n")

	return sb.String()
}

// buildUserPrompt 根据实时上下文构建用户提示。
func buildUserPrompt(ctx promptContext) string {
	now := time.Now().Format("2006-01-02 15:04:05")
	request := ctx.Request
	context := request.Context
	newsSummary := formatNewsSummary(request.NewsSentiment.Sentiment, request.NewsSentiment.Score)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**时间**: %s | **运行**: %d分钟 | **周期**: #%d\n\n", now, context.RuntimeMinutes, context.CallCount))
	sb.WriteString(fmt.Sprintf("**账户**: 净值%.2f | 可用%.2f | 未实现PnL %+.2f | 持仓%d个\n\n",
		context.Account.TotalEquity, context.Account.Available, context.Account.UnrealizedPNL, len(context.Positions)))
	sb.WriteString(fmt.Sprintf("**交易对**: %s (%s) | 当前价格 %.2f | 策略信号 %s\n\n",
		request.Symbol, strings.ToUpper(request.Exchange), request.CurrentPrice, request.StrategySignal))

	if len(context.Positions) > 0 {
		sb.WriteString("## 持仓明细\n")
		for idx, pos := range context.Positions {
			holdingText := ""
			if pos.HoldingMinutes > 0 {
				holdingText = fmt.Sprintf(" | 持仓%d分钟", pos.HoldingMinutes)
			}
			liquidation := "--"
			if pos.Liquidation > 0 {
				liquidation = fmt.Sprintf("%.4f", pos.Liquidation)
			}
			sb.WriteString(fmt.Sprintf("%d. %s %s | 入场价%.4f 当前价%.4f | 盈亏%+.2f%% | 杠杆%.1fx | 保证金%.2f | 强平价%s%s\n",
				idx+1,
				pos.Symbol,
				strings.ToUpper(pos.Side),
				pos.EntryPrice,
				pos.MarkPrice,
				pos.UnrealizedPct,
				pos.Leverage,
				pos.MarginUsed,
				liquidation,
				holdingText,
			))
			sb.WriteString(fmt.Sprintf("- 数量%.4f | 未实现PnL %+.2f\n\n", pos.Quantity, pos.UnrealizedPNL))
		}
	} else {
		sb.WriteString("## 持仓明细\n- 当前空仓\n\n")
	}

	if newsSummary != "" && newsSummary != "无" {
		sb.WriteString(fmt.Sprintf("## 新闻情绪\n- 总结: %s\n", newsSummary))
		for _, highlight := range request.NewsSentiment.Highlights {
			sb.WriteString(fmt.Sprintf("- %s\n", highlight))
		}
		sb.WriteString("\n")
	}

	if len(request.LearningSnippets) > 0 {
		sb.WriteString("## 历史学习片段\n")
		for _, snippet := range request.LearningSnippets {
			sb.WriteString(fmt.Sprintf("- %s\n", snippet))
		}
		sb.WriteString("\n")
	}

	if len(context.CandidateCoins) > 0 {
		sb.WriteString("## 候选币种\n")
		for _, coin := range context.CandidateCoins {
			sb.WriteString(fmt.Sprintf("- %s 权重%.2f 理由:%s\n", coin.Symbol, coin.Weight, coin.Reason))
		}
		sb.WriteString("\n")
	}

	if len(context.MarketData) > 0 {
		symbols := make([]string, 0, len(context.MarketData))
		for symbol := range context.MarketData {
			symbols = append(symbols, symbol)
		}
		sort.Strings(symbols)
		sb.WriteString("## 市场数据快照\n")
		for _, symbol := range symbols {
			snapshot := context.MarketData[symbol]
			sb.WriteString(fmt.Sprintf("- %s 现价%.4f 1h:%+.2f%% 4h:%+.2f%% EMA20=%.2f MACD=%.4f RSI7=%.2f RSI14=%.2f Funding=%.5f OI=%.2f\n",
				symbol, snapshot.CurrentPrice, snapshot.PriceChange1h, snapshot.PriceChange4h, snapshot.EMA20, snapshot.MACD, snapshot.RSI7, snapshot.RSI14, snapshot.FundingRate, snapshot.OpenInterest))
		}
		sb.WriteString("\n")
	}

	if context.Performance.TotalTrades > 0 {
		sb.WriteString("## 历史绩效\n")
		sb.WriteString(fmt.Sprintf("- 交易次数: %d\n- 胜率: %.2f%%\n- 夏普比: %.2f\n- Profit Factor: %.2f\n\n",
			context.Performance.TotalTrades, context.Performance.WinRate*100, context.Performance.SharpeRatio, context.Performance.ProfitFactor))
	}

	limitsJSON, _ := json.Marshal(request.RiskLimits)
	sb.WriteString("## 系统约束\n")
	sb.WriteString(fmt.Sprintf("```json\n%s\n```\n", string(limitsJSON)))

	return sb.String()
}

// buildReflectionPrompt 构建基于夏普比率的反思提示
func buildReflectionPrompt(sharpeRatio float64, performance ai.PerformanceStats, positions []ai.PositionContext) string {
	var sb strings.Builder
	
	// 夏普比率自我进化框架
	sb.WriteString("## 📊 夏普比率驱动的反思框架\n\n")
	
	// 绩效阈值触发机制
	if sharpeRatio < -0.5 {
		sb.WriteString("**夏普比率 < -0.5** (持续亏损):\n")
		sb.WriteString("  → 🛑 停止交易，连续观望至少6个周期（18分钟）\n")
		sb.WriteString("  → 🔍 深度反思：\n")
		sb.WriteString("     • 交易频率过高？（每小时>2次就是过度）\n")
		sb.WriteString("     • 持仓时间过短？（<30分钟就是过早平仓）\n")
		sb.WriteString("     • 信号强度不足？（信心度<75）\n")
		sb.WriteString("     • 是否在做空？（单边做多是错误的）\n\n")
	} else if sharpeRatio < 0 {
		sb.WriteString("**夏普比率 -0.5 ~ 0** (轻微亏损):\n")
		sb.WriteString("  → ⚠️ 严格风控：信心度>80，每小时≤1笔\n")
		sb.WriteString("  → 📉 降低风险：减少仓位规模，提高止损比例\n\n")
	} else if sharpeRatio < 0.7 {
		sb.WriteString("**夏普比率 0 ~ 0.7** (稳定盈利):\n")
		sb.WriteString("  → ✅ 维持策略：继续当前交易模式\n")
		sb.WriteString("  → 📈 小幅优化：寻找改进机会\n\n")
	} else {
		sb.WriteString("**夏普比率 > 0.7** (优秀表现):\n")
		sb.WriteString("  → 🚀 优化扩张：适度扩大仓位，复制成功模式\n\n")
	}
	
	// 多维度反思指标
	sb.WriteString("## 📏 多维度反思指标\n\n")
	
	// 交易频率反思
	sb.WriteString("**量化标准**:\n")
	sb.WriteString("- 优秀交易员：每天2-4笔 = 每小时0.1-0.2笔\n")
	sb.WriteString("- 过度交易：每小时>2笔 = 严重问题\n")
	sb.WriteString("- 最佳节奏：开仓后持有至少30-60分钟\n\n")
	sb.WriteString("**自查**:\n")
	sb.WriteString("如果你发现自己每个周期都在交易 → 说明标准太低\n\n")
	
	// 信号质量反思
	sb.WriteString("**开仓标准（严格）**:\n")
	sb.WriteString("- 信心度 ≥ 75（100为极度自信）\n")
	sb.WriteString("- 多维度确认（价格+指标+量+OI+趋势）\n")
	sb.WriteString("- 风险回报比 ≥ 1:3（硬性要求）\n")
	sb.WriteString("- 避免单一指标决策\n\n")
	sb.WriteString("**避免低质量信号**:\n")
	sb.WriteString("- 单一维度（只看一个指标）\n")
	sb.WriteString("- 相互矛盾（涨但量萎缩）\n")
	sb.WriteString("- 横盘震荡\n\n")
	
	// 持仓时长分析
	if len(positions) > 0 {
		sb.WriteString("## ⏰ 当前持仓分析\n")
		for _, pos := range positions {
			if pos.HoldingMinutes > 0 {
				holdingText := ""
				if pos.HoldingMinutes < 60 {
					holdingText = fmt.Sprintf("持仓%d分钟", pos.HoldingMinutes)
				} else {
					durationHour := pos.HoldingMinutes / 60
					durationMinRemainder := pos.HoldingMinutes % 60
					holdingText = fmt.Sprintf("持仓%d小时%d分钟", durationHour, durationMinRemainder)
				}
				sb.WriteString(fmt.Sprintf("- %s %s: %s, 盈亏%+.2f%%\n", 
					pos.Symbol, strings.ToUpper(pos.Side), holdingText, pos.UnrealizedPct))
			}
		}
		sb.WriteString("\n")
	}
	
	// 反思执行流程
	sb.WriteString("## 🔄 反思执行流程\n")
	sb.WriteString("1. **分析夏普比率**: 当前策略是否有效？需要调整吗？\n")
	sb.WriteString("2. **评估持仓**: 趋势是否改变？是否该止盈/止损？\n")
	sb.WriteString("3. **寻找新机会**: 有强信号吗？多空机会？\n")
	sb.WriteString("4. **输出决策**: 思维链分析 + JSON\n\n")
	
	return sb.String()
}

// parseFullDecisionResponse 负责从模型输出中提取结构化 JSON。
func parseFullDecisionResponse(raw string) (ai.DecisionResponse, error) {
	content := trimJSONFences(raw)
	if content == "" {
		return ai.DecisionResponse{}, fmt.Errorf("模型未返回内容")
	}

	if resp, err := decodeDecision(content); err == nil {
		resp.RawContent = content
		resp.CoTTrace = extractCoTTrace(raw)
		return resp, nil
	}

	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		snippet := content[start : end+1]
		if resp, err := decodeDecision(snippet); err == nil {
			resp.RawContent = content
			resp.CoTTrace = extractCoTTrace(raw)
			return resp, nil
		}
	}

	return ai.DecisionResponse{}, fmt.Errorf("无法解析模型输出: %s", content)
}

type rawDecision struct {
	Action      string            `json:"action"`
	Confidence  float64           `json:"confidence"`
	Reason      string            `json:"reason"`
	Adjustments ai.AdjustmentPlan `json:"adjustments"`
	RiskNotes   json.RawMessage   `json:"riskNotes"`
}

func decodeDecision(content string) (ai.DecisionResponse, error) {
	var raw rawDecision
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return ai.DecisionResponse{}, err
	}

	resp := ai.DecisionResponse{
		Action:      raw.Action,
		Confidence:  raw.Confidence,
		Reason:      raw.Reason,
		Adjustments: raw.Adjustments,
	}

	notes, err := parseRiskNotes(raw.RiskNotes)
	if err != nil {
		return ai.DecisionResponse{}, err
	}
	resp.RiskNotes = notes

	return resp, nil
}

func extractCoTTrace(raw string) string {
	trimmed := strings.TrimSpace(raw)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "```json") {
		idx := strings.Index(trimmed, "\n")
		if idx != -1 {
			trimmed = strings.TrimSpace(trimmed[idx+1:])
		}
	}
	idx := strings.Index(trimmed, "{")
	if idx <= 0 {
		return ""
	}
	trace := strings.TrimSpace(trimmed[:idx])
	trace = strings.Trim(trace, "`")
	return strings.TrimSpace(trace)
}

func parseRiskNotes(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var notes []string
	if err := json.Unmarshal(raw, &notes); err == nil {
		return notes, nil
	}

	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		if strings.TrimSpace(single) == "" {
			return nil, nil
		}
		return []string{single}, nil
	}

	var arr []interface{}
	if err := json.Unmarshal(raw, &arr); err == nil {
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			result = append(result, fmt.Sprint(item))
		}
		return result, nil
	}

	return nil, fmt.Errorf("riskNotes 无法解析: %s", string(raw))
}

// validateDecisionResponse 对模型返回的决策进行初步校验。
func validateDecisionResponse(decision ai.DecisionResponse, limits ai.RiskLimits) error {
	action := strings.ToLower(strings.TrimSpace(decision.Action))
	validActions := map[string]struct{}{
		"open_long":      {},
		"open_short":     {},
		"increase_long":  {},
		"increase_short": {},
		"close":          {},
		"exit":           {},
		"reduce":         {},
		"hold":           {},
		"wait":           {},
	}
	if _, ok := validActions[action]; !ok && action != "" {
		return fmt.Errorf("未知 action: %s", decision.Action)
	}

	targetLev := decision.Adjustments.TargetLeverage
	if targetLev < 0 {
		return fmt.Errorf("targetLeverage 不得为负数")
	}
	if limits.MaxLeverage > 0 && targetLev > limits.MaxLeverage {
		return fmt.Errorf("targetLeverage %.2f 超过上限 %.2f", targetLev, limits.MaxLeverage)
	}

	if decision.Adjustments.StopLossPercent > 0 && decision.Adjustments.TakeProfitPercent > 0 {
		if limits.MinRiskRewardRatio > 0 {
			rr := decision.Adjustments.TakeProfitPercent / decision.Adjustments.StopLossPercent
			if rr+1e-9 < limits.MinRiskRewardRatio {
				return fmt.Errorf("风险回报 %.2f 低于要求 %.2f", rr, limits.MinRiskRewardRatio)
			}
		}
	}

	return nil
}

func trimJSONFences(s string) string {
	trimmed := strings.TrimSpace(s)
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```")
		trimmed = strings.TrimSpace(trimmed)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "json") {
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
