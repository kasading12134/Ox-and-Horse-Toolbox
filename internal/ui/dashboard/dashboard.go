package dashboard

import (
	"context"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"autobot/internal/news"
)

const (
	leftWidth      = 98
	rightWidth     = 98
	topRows        = 10
	compactRows    = topRows - 2
	aiRows         = 12
	aiHistoryLimit = 36
	renderInterval = time.Second
)

// Color defines supported ANSI color intents for dashboard cells.
type Color int

const (
	ColorNone Color = iota
	ColorPositive
	ColorNegative
	ColorBuy
	ColorSell
)

type Line struct {
	Text  string
	Color Color
}

type traderSection struct {
	Symbol   string
	Exchange string
	Events   []Line
}

type orderSnapshot struct {
	Side  string
	Lines []Line
}

type PnLSnapshot struct {
	Realized    float64
	Unrealized  float64
	Equity      float64
	MarginUsage float64
	Available   float64
	RiskStatus  string
	MaxDrawdown float64
}

// ContextSnapshot 保存交易上下文概要信息。
type ContextSnapshot struct {
	Timestamp      time.Time
	RuntimeMinutes int
	CallCount      int
	Equity         float64
	Available      float64
	Unrealized     float64
	DailyRealized  float64
	MarginUsage    float64
	RiskStatus     string
	Sharpe         float64
	WinRate        float64
	TotalTrades    int
	ProfitFactor   float64
	Positions      []ContextPosition
	InitialEquity  float64
	PnLPercent     float64
}

// ContextPosition 表示单个持仓快照。
type ContextPosition struct {
	Symbol         string
	Side           string
	Quantity       float64
	EntryPrice     float64
	Leverage       float64
	Unrealized     float64
	HoldingMinutes int
	MarkPrice      float64
	UnrealizedPct  float64
	MarginUsed     float64
	Liquidation    float64
}

type DecisionLogEntry struct {
	Timestamp  time.Time
	Symbol     string
	Action     string
	Confidence float64
	Reason     string
	Thought    string
	RiskNotes  []string
	Result     string
	Error      string
}

type EquityPoint struct {
	Timestamp time.Time
	Equity    float64
}

// Dashboard maintains aggregated runtime information for terminal rendering.
type Dashboard struct {
	mu            sync.Mutex
	writer        io.Writer
	news          []Line
	newsSource    string
	traders       map[string]*traderSection
	orders        map[string]orderSnapshot
	pnls          map[string]PnLSnapshot
	aiThoughts    map[string][]Line
	aiPlans       map[string][]Line
	primary       string
	trigger       chan struct{}
	contexts      map[string]ContextSnapshot
	decisionLogs  map[string][]DecisionLogEntry
	equityHistory map[string][]EquityPoint
}

// New creates a dashboard using the provided writer for output.
func New(writer io.Writer) *Dashboard {
	return &Dashboard{
		writer:        writer,
		newsSource:    "news.blockbeats",
		traders:       make(map[string]*traderSection),
		orders:        make(map[string]orderSnapshot),
		pnls:          make(map[string]PnLSnapshot),
		aiThoughts:    make(map[string][]Line),
		aiPlans:       make(map[string][]Line),
		trigger:       make(chan struct{}, 1),
		contexts:      make(map[string]ContextSnapshot),
		decisionLogs:  make(map[string][]DecisionLogEntry),
		equityHistory: make(map[string][]EquityPoint),
	}
}

// RegisterTrader ensures dashboard allocates sections for the trader.
func (d *Dashboard) RegisterTrader(name, symbol, exchange string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.traders[name]; !ok {
		d.traders[name] = &traderSection{Symbol: symbol, Exchange: strings.ToLower(exchange)}
	}
	if d.primary == "" {
		d.primary = name
	}
}

// UpdateNews publishes the latest news articles for the ticker panel.
func (d *Dashboard) UpdateNews(sourceHint string, articles []news.Article) {
	d.mu.Lock()
	defer d.mu.Unlock()

	lines := make([]Line, 0, len(articles))
	for _, article := range articles {
		ts := article.PublishedAt
		if ts.IsZero() {
			ts = time.Now()
		}
		title := strings.TrimSpace(article.Title)
		if title == "" {
			continue
		}
		line := fmt.Sprintf("%s %s", ts.Local().Format("2006-01-02 15:04:05"), title)
		lines = append(lines, Line{Text: line})
		if len(lines) >= topRows {
			break
		}
	}
	d.news = lines
	if len(articles) > 0 {
		source := strings.TrimSpace(articles[0].Source)
		if source != "" {
			d.newsSource = fmt.Sprintf("news.%s", strings.ToLower(source))
		} else if sourceHint != "" {
			d.newsSource = fmt.Sprintf("news.%s", strings.ToLower(sourceHint))
		}
	} else if sourceHint != "" {
		d.newsSource = fmt.Sprintf("news.%s", strings.ToLower(sourceHint))
	}
	d.requestRender()
}

// AppendTraderEvent records the latest trading event for a trader.
func (d *Dashboard) AppendTraderEvent(trader string, message string) {
	if message == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	section, ok := d.traders[trader]
	if !ok {
		section = &traderSection{}
		d.traders[trader] = section
		if d.primary == "" {
			d.primary = trader
		}
	}
	line := Line{Text: message}
	section.Events = append([]Line{line}, section.Events...)
	if len(section.Events) > topRows {
		section.Events = section.Events[:topRows]
	}
	d.requestRender()
}

// UpdateOrder replaces the current order snapshot for the trader.
func (d *Dashboard) UpdateOrder(trader string, side string, lines []Line) {
	d.mu.Lock()
	defer d.mu.Unlock()

	copyLines := make([]Line, len(lines))
	copy(copyLines, lines)
	d.orders[trader] = orderSnapshot{Side: strings.ToUpper(side), Lines: copyLines}
	d.requestRender()
}

// UpdatePnL refreshes realized/unrealized PnL metrics for the trader.
func (d *Dashboard) UpdatePnL(trader string, snapshot PnLSnapshot) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pnls[trader] = snapshot
	d.requestRender()
}

// UpdateAI records latest AI reasoning summary.
func (d *Dashboard) UpdateAI(trader string, lines []Line) {
	d.mu.Lock()
	defer d.mu.Unlock()
	copyLines := make([]Line, len(lines))
	copy(copyLines, lines)
	d.aiThoughts[trader] = copyLines
	d.requestRender()
}

// UpdateAIPlan records structured action plan info.
func (d *Dashboard) UpdateAIPlan(trader string, lines []Line) {
	d.mu.Lock()
	defer d.mu.Unlock()
	copyLines := make([]Line, len(lines))
	copy(copyLines, lines)
	d.aiPlans[trader] = copyLines
	d.requestRender()
}

// UpdateContext 更新账户上下文信息。
func (d *Dashboard) UpdateContext(trader string, snapshot ContextSnapshot) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.contexts[trader] = snapshot
	d.requestRender()
}

// AppendDecisionLog 记录一次最新的 AI 决策。
func (d *Dashboard) AppendDecisionLog(trader string, entry DecisionLogEntry) {
	d.mu.Lock()
	defer d.mu.Unlock()
	logs := append([]DecisionLogEntry{entry}, d.decisionLogs[trader]...)
	if len(logs) > 5 {
		logs = logs[:5]
	}
	d.decisionLogs[trader] = logs
	d.requestRender()
}

// AppendEquityPoint 添加净值时间序列点。
func (d *Dashboard) AppendEquityPoint(trader string, timestamp time.Time, equity float64) {
	if math.IsNaN(equity) || math.IsInf(equity, 0) {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	history := append(d.equityHistory[trader], EquityPoint{Timestamp: timestamp, Equity: equity})
	if len(history) > 120 {
		history = history[len(history)-120:]
	}
	d.equityHistory[trader] = history
	d.requestRender()
}

// AppendAIPlanLine appends a single line to existing plan output.
func (d *Dashboard) AppendAIPlanLine(trader string, line Line) {
	d.mu.Lock()
	defer d.mu.Unlock()
	existing := append([]Line{}, d.aiPlans[trader]...)
	existing = append(existing, line)
	if len(existing) > aiHistoryLimit {
		existing = existing[len(existing)-aiHistoryLimit:]
	}
	d.aiPlans[trader] = existing
	d.requestRender()
}

// Start begins the rendering loop controlled by the provided context.
func (d *Dashboard) Start(ctx context.Context) {
	ticker := time.NewTicker(renderInterval)
	go func() {
		defer ticker.Stop()
		d.renderOnce()
		for {
			select {
			case <-ctx.Done():
				d.renderOnce()
				return
			case <-ticker.C:
				d.renderOnce()
			case <-d.trigger:
				d.renderOnce()
			}
		}
	}()
}

func (d *Dashboard) requestRender() {
	select {
	case d.trigger <- struct{}{}:
	default:
	}
}

func (d *Dashboard) renderOnce() {
	output := d.render()
	if output == "" {
		return
	}
	fmt.Fprintf(d.writer, "\033[H\033[2J%s", output)
}

func (d *Dashboard) render() string {
	d.mu.Lock()
	defer d.mu.Unlock()

	ctxSnapshot := d.contexts[d.primary]
	pnlSnapshot := d.pnls[d.primary]

	summaryLines := buildSummaryLines(ctxSnapshot, pnlSnapshot)
	if len(summaryLines) == 0 {
		summaryLines = []Line{{Text: "等待账户数据..."}}
	}
	summaryTitle := fmt.Sprintf("账户概览 (%s)", d.primary)

	equityLines := buildEquityLines(d.equityHistory[d.primary])
	if len(equityLines) > 0 {
		summaryLines = append(summaryLines, Line{Text: "收益率趋势"})
		summaryLines = append(summaryLines, equityLines...)
	} else {
		summaryLines = append(summaryLines, Line{Text: "收益率趋势: 等待净值数据..."})
	}

	positionsLines := buildPositionLines(ctxSnapshot)
	if len(positionsLines) == 0 {
		positionsLines = []Line{{Text: "暂无持仓"}}
	}

	decisionLines := buildDecisionLogLines(d.decisionLogs[d.primary])
	if len(decisionLines) == 0 {
		decisionLines = []Line{{Text: "暂无决策"}}
	}

	var eventLines []Line
	tradeTitle := "交易日志"
	if section, ok := d.traders[d.primary]; ok && section != nil {
		tradeTitle = fmt.Sprintf("交易日志 (%s.%s)", d.primary, section.Symbol)
		eventLines = append(eventLines, section.Events...)
	}
	if len(eventLines) == 0 {
		eventLines = []Line{{Text: "等待交易事件..."}}
	}

	orderTitle := "下单详情"
	orderLines := []Line{}
	if snapshot, ok := d.orders[d.primary]; ok {
		orderLines = append(orderLines, snapshot.Lines...)
		if section, ok := d.traders[d.primary]; ok && section != nil {
			base := fmt.Sprintf("下单详情 (%s)", section.Exchange)
			if snapshot.Side != "" {
				orderTitle = fmt.Sprintf("%s | %s", base, snapshot.Side)
			} else {
				orderTitle = base
			}
		}
	}
	if len(orderLines) == 0 {
		orderLines = []Line{{Text: "等待下单..."}}
	}

	pnlTitle := "收益统计"
	pnlLines := buildPnLLines(pnlSnapshot)

	newsLines := append([]Line(nil), d.news...)
	newsTitle := fmt.Sprintf("新闻快讯 (%s)", d.newsSource)
	if d.newsSource == "" {
		newsTitle = "新闻快讯"
	}

	aiLines := d.aiThoughts[d.primary]
	if len(aiLines) == 0 {
		aiLines = []Line{{Text: "等待 AI 推理..."}}
	}
	aiTitle := fmt.Sprintf("AI 推理 (%s)", d.primary)

	planLines := d.aiPlans[d.primary]
	if len(planLines) == 0 {
		planLines = []Line{{Text: "等待操作计划..."}}
	}
	aiPlanTitle := fmt.Sprintf("AI 操作计划 (%s)", d.primary)

	learningLines := buildLearningLines(ctxSnapshot)
	if len(learningLines) == 0 {
		learningLines = []Line{{Text: "等待交易统计..."}}
	}

	output := renderFullWidth(summaryTitle, summaryLines)
	output += renderTwoPanel("持仓列表", positionsLines, "决策日志", decisionLines)
	output += renderTwoPanelWithRows(tradeTitle, eventLines, pnlTitle, pnlLines, compactRows)
	output += renderTwoPanel(orderTitle, orderLines, newsTitle, newsLines)
	output += renderFullWidth(aiTitle, aiLines)
	output += renderFullWidth("AI 学习分析", learningLines)
	output += renderFullWidth(aiPlanTitle, planLines)
	return output
}

func renderTwoPanel(leftTitle string, left []Line, rightTitle string, right []Line) string {
	return renderTwoPanelWithRows(leftTitle, left, rightTitle, right, topRows)
}

func renderTwoPanelWithRows(leftTitle string, left []Line, rightTitle string, right []Line, rows int) string {
	if rows <= 0 {
		rows = 1
	}
	leftExpanded := expandLines(left, leftWidth)
	rightExpanded := expandLines(right, rightWidth)

	maxLen := len(leftExpanded)
	if len(rightExpanded) > maxLen {
		maxLen = len(rightExpanded)
	}
	if maxLen == 0 {
		maxLen = 1
	}
	if rows > maxLen {
		rows = maxLen
	}
	leftExpanded = trimLines(leftExpanded, rows)
	rightExpanded = trimLines(rightExpanded, rows)
	leftPrepared := padLines(leftExpanded, rows)
	rightPrepared := padLines(rightExpanded, rows)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("┌%s┬%s┐\n", strings.Repeat("─", leftWidth+2), strings.Repeat("─", rightWidth+2)))
	b.WriteString(formatRow(leftTitle, rightTitle))
	b.WriteString(fmt.Sprintf("├%s┼%s┤\n", strings.Repeat("─", leftWidth+2), strings.Repeat("─", rightWidth+2)))
	for i := 0; i < rows; i++ {
		b.WriteString(formatLineRow(leftPrepared[i], rightPrepared[i]))
	}
	b.WriteString(fmt.Sprintf("└%s┴%s┘\n", strings.Repeat("─", leftWidth+2), strings.Repeat("─", rightWidth+2)))
	return b.String()
}

func expandLines(lines []Line, width int) []Line {
	if width <= 0 {
		width = 1
	}
	var expanded []Line
	for _, line := range lines {
		segments := wrapText(line.Text, width)
		if len(segments) == 0 {
			segments = []string{""}
		}
		for _, segment := range segments {
			text := segment
			if strings.TrimSpace(text) == "" {
				text = " "
			}
			expanded = append(expanded, Line{Text: text, Color: line.Color})
		}
	}
	if len(expanded) == 0 {
		return []Line{{Text: " "}}
	}
	return expanded
}

func trimLines(lines []Line, rows int) []Line {
	if rows <= 0 {
		return lines
	}
	if len(lines) <= rows {
		return lines
	}
	return append([]Line(nil), lines[:rows]...)
}

func padLines(lines []Line, rows int) []Line {
	if rows <= 0 {
		rows = 1
	}
	out := make([]Line, rows)
	for i := 0; i < rows; i++ {
		if i < len(lines) {
			out[i] = lines[i]
		} else {
			out[i] = Line{Text: " "}
		}
		if strings.TrimSpace(out[i].Text) == "" {
			out[i].Text = " "
		}
	}
	return out
}

func renderFullWidth(title string, lines []Line) string {
	inner := leftWidth + rightWidth + 3
	var b strings.Builder
	b.WriteString(fmt.Sprintf("┌%s┐\n", strings.Repeat("─", inner+2)))
	b.WriteString(fmt.Sprintf("│ %-*s │\n", inner, truncate(strings.TrimSpace(title), inner)))
	b.WriteString(fmt.Sprintf("├%s┤\n", strings.Repeat("─", inner+2)))
	for _, line := range expandLines(lines, inner) {
		text := padWithColor(line, inner)
		b.WriteString(fmt.Sprintf("│ %s │\n", text))
	}
	b.WriteString(fmt.Sprintf("└%s┘\n", strings.Repeat("─", inner+2)))
	return b.String()
}

func buildSummaryLines(ctx ContextSnapshot, pnl PnLSnapshot) []Line {
	timestamp := ctx.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	runtime := ctx.RuntimeMinutes
	callCount := ctx.CallCount
	trades := ctx.TotalTrades

	equity := pickNonZero(ctx.Equity, pnl.Equity)
	available := pickNonZero(ctx.Available, pnl.Available)
	unrealized := pickNonZero(ctx.Unrealized, pnl.Unrealized)
	realized := pickNonZero(ctx.DailyRealized, pnl.Realized)
	margin := pickNonZero(ctx.MarginUsage, pnl.MarginUsage)
	initial := pickNonZero(ctx.InitialEquity, equity)
	totalPnLPct := 0.0
	if initial > 0 {
		totalPnLPct = (equity/initial - 1) * 100
	}
	if ctx.PnLPercent != 0 {
		totalPnLPct = ctx.PnLPercent
	}
	risk := ctx.RiskStatus
	if risk == "" {
		risk = pnl.RiskStatus
	}
	sharpe := ctx.Sharpe
	winRate := ctx.WinRate * 100
	profitFactor := ctx.ProfitFactor

	lines := []Line{
		{Text: fmt.Sprintf("当前时间: %s | 上次刷新: %s | 运行: %dm | 周期: #%d | 交易: %d", time.Now().Format("15:04:05"), timestamp.Format("15:04:05"), runtime, callCount, trades)},
		{Text: fmt.Sprintf("净值: %.2f | 可用: %.2f | 保证金: %.2f%%", equity, available, margin)},
		{Text: fmt.Sprintf("已实现: %s | 未实现: %s | 风控: %s", formatSigned(realized), formatSigned(unrealized), risk), Color: colorByValue(realized + unrealized)},
		{Text: fmt.Sprintf("总收益: %+.2f%% | 夏普: %.2f | 胜率: %.2f%% | ProfitFactor: %s", totalPnLPct, sharpe, winRate, formatProfitFactor(profitFactor)), Color: colorByValue(totalPnLPct)},
	}

	if margin > 75 {
		lines[1].Color = ColorNegative
	}
	if strings.Contains(risk, "暂停") {
		lines[1].Color = ColorNegative
	}

	return lines
}

func buildPositionLines(ctx ContextSnapshot) []Line {
	positions := make([]ContextPosition, len(ctx.Positions))
	copy(positions, ctx.Positions)
	if len(positions) == 0 {
		return nil
	}
	sort.Slice(positions, func(i, j int) bool {
		return math.Abs(positions[i].Unrealized) > math.Abs(positions[j].Unrealized)
	})

	lines := make([]Line, 0, len(positions))
	for _, pos := range positions {
		pnlText := fmt.Sprintf("%+.2f%% (%s)", pos.UnrealizedPct, formatSigned(pos.Unrealized))
		liqText := "--"
		if pos.Liquidation > 0 {
			liqText = fmt.Sprintf("%.2f", pos.Liquidation)
		}
		durText := ""
		if pos.HoldingMinutes > 0 {
			durText = fmt.Sprintf(" | 持仓%dm", pos.HoldingMinutes)
		}
		text := fmt.Sprintf("%s %-5s %.4f @ %.2f → %.2f | 盈亏 %s | 保证金 %.2f | 强平价 %s%s",
			pos.Symbol,
			pos.Side,
			pos.Quantity,
			pos.EntryPrice,
			pos.MarkPrice,
			pnlText,
			pos.MarginUsed,
			liqText,
			durText,
		)
		color := colorByValue(pos.Unrealized)
		if color == ColorNone {
			if strings.EqualFold(pos.Side, "LONG") {
				color = ColorBuy
			} else if strings.EqualFold(pos.Side, "SHORT") {
				color = ColorSell
			}
		}
		lines = append(lines, Line{Text: text, Color: color})
	}
	return lines
}

func buildDecisionLogLines(logs []DecisionLogEntry) []Line {
	if len(logs) == 0 {
		return nil
	}
	lines := make([]Line, 0, topRows)
	for i, log := range logs {
		if i >= 3 {
			break
		}
		header := fmt.Sprintf("%s %s %s", log.Timestamp.Format("15:04:05"), log.Symbol, log.Action)
		if log.Confidence > 0 {
			header += fmt.Sprintf(" (信心%.1f)", log.Confidence)
		}
		if log.Result != "" {
			header += " -> " + log.Result
		}
		color := ColorNone
		if strings.Contains(log.Result, "成功") {
			color = ColorPositive
		} else if strings.Contains(log.Result, "失败") {
			color = ColorNegative
		}
		lines = append(lines, Line{Text: header, Color: color})
		if log.Thought != "" {
			for _, segment := range wrapText("思维: "+strings.TrimSpace(log.Thought), rightWidth) {
				lines = append(lines, Line{Text: segment})
			}
		}
		if log.Reason != "" {
			for _, segment := range wrapText("理由: "+log.Reason, rightWidth) {
				lines = append(lines, Line{Text: segment})
			}
		}
		for _, note := range log.RiskNotes {
			note = strings.TrimSpace(note)
			if note == "" {
				continue
			}
			for _, segment := range wrapText("- "+note, rightWidth) {
				lines = append(lines, Line{Text: segment, Color: ColorNegative})
			}
		}
		if log.Error != "" {
			for _, segment := range wrapText("错误: "+log.Error, rightWidth) {
				lines = append(lines, Line{Text: segment, Color: ColorNegative})
			}
		}
	}
	return lines
}

func buildEquityLines(history []EquityPoint) []Line {
	if len(history) == 0 {
		return nil
	}
	sample := history
	if len(sample) > 16 {
		sample = sample[len(sample)-16:]
	}
	first := sample[0]
	last := sample[len(sample)-1]
	delta := last.Equity - first.Equity
	percent := 0.0
	if first.Equity > 0 {
		percent = (last.Equity/first.Equity - 1) * 100
	}
	minVal := sample[0].Equity
	maxVal := sample[0].Equity
	for _, point := range sample {
		if point.Equity < minVal {
			minVal = point.Equity
		}
		if point.Equity > maxVal {
			maxVal = point.Equity
		}
	}
	spark := generateSpark(sample, minVal, maxVal)
	lines := []Line{
		{Text: fmt.Sprintf("区间: %s %.2f → %s %.2f", first.Timestamp.Format("15:04"), first.Equity, last.Timestamp.Format("15:04"), last.Equity)},
		{Text: fmt.Sprintf("变化: %+.2f (%.2f%%)", delta, percent), Color: colorByValue(delta)},
		{Text: spark},
	}
	return lines
}

func buildLearningLines(ctx ContextSnapshot) []Line {
	lines := []Line{}
	if ctx.TotalTrades > 0 {
		lines = append(lines, Line{Text: fmt.Sprintf("总交易: %d | 胜率: %.2f%%", ctx.TotalTrades, ctx.WinRate*100)})
	}
	lines = append(lines, Line{Text: fmt.Sprintf("夏普: %.2f | ProfitFactor: %s", ctx.Sharpe, formatProfitFactor(ctx.ProfitFactor)), Color: colorByValue(ctx.Sharpe)})
	if ctx.RuntimeMinutes > 0 && ctx.TotalTrades > 0 {
		hr := float64(ctx.RuntimeMinutes) / 60.0
		if hr > 0 {
			perHour := float64(ctx.TotalTrades) / hr
			perDay := perHour * 24
			lines = append(lines, Line{Text: fmt.Sprintf("频率: %.2f 笔/小时 ≈ %.2f 笔/日", perHour, perDay)})
		}
	}
	if ctx.RiskStatus != "" {
		lines = append(lines, Line{Text: fmt.Sprintf("风险状态: %s", ctx.RiskStatus)})
	}
	return lines
}

func generateSpark(points []EquityPoint, minVal, maxVal float64) string {
	sparks := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	if maxVal-minVal < 1e-9 {
		return strings.Repeat(string(sparks[len(sparks)/2]), len(points))
	}
	var b strings.Builder
	for _, point := range points {
		ratio := (point.Equity - minVal) / (maxVal - minVal)
		idx := int(math.Round(ratio * float64(len(sparks)-1)))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sparks) {
			idx = len(sparks) - 1
		}
		b.WriteRune(sparks[idx])
	}
	return b.String()
}

func wrapText(text string, width int) []string {
	if width <= 0 {
		width = 1
	}
	text = strings.ReplaceAll(text, "\r", "")
	var lines []string
	var builder strings.Builder
	currentWidth := 0
	flush := func() {
		if builder.Len() == 0 {
			return
		}
		lines = append(lines, builder.String())
		builder.Reset()
		currentWidth = 0
	}
	for _, r := range text {
		if r == '\n' {
			flush()
			continue
		}
		rw := runeWidth(r)
		if currentWidth+rw > width && builder.Len() > 0 {
			flush()
		}
		builder.WriteRune(r)
		currentWidth += rw
	}
	flush()
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func pickNonZero(primary, fallback float64) float64 {
	if primary != 0 {
		return primary
	}
	return fallback
}

func formatSigned(value float64) string {
	return fmt.Sprintf("%+.2f", value)
}

func formatProfitFactor(value float64) string {
	if math.IsInf(value, 1) || value > 1e6 {
		return "∞"
	}
	if value == 0 {
		return "0.00"
	}
	return fmt.Sprintf("%.2f", value)
}

func colorByValue(value float64) Color {
	if value > 0.0001 {
		return ColorPositive
	}
	if value < -0.0001 {
		return ColorNegative
	}
	return ColorNone
}

func formatRow(leftTitle, rightTitle string) string {
	return fmt.Sprintf("│ %-*s │ %-*s │\n", leftWidth, truncate(strings.TrimSpace(leftTitle), leftWidth), rightWidth, truncate(strings.TrimSpace(rightTitle), rightWidth))
}

func formatLineRow(left Line, right Line) string {
	leftText := padWithColor(left, leftWidth)
	rightText := padWithColor(right, rightWidth)
	return fmt.Sprintf("│ %s │ %s │\n", leftText, rightText)
}

func padWithColor(line Line, width int) string {
	truncated := truncate(line.Text, width)
	pad := width - displayWidth(truncated)
	if pad < 0 {
		pad = 0
	}
	padded := truncated + strings.Repeat(" ", pad)
	return applyColor(padded, line.Color)
}

func truncate(s string, width int) string {
	var builder strings.Builder
	current := 0
	for _, r := range s {
		rw := runeWidth(r)
		if current+rw > width {
			break
		}
		builder.WriteRune(r)
		current += rw
	}
	return builder.String()
}

func displayWidth(s string) int {
	width := 0
	for _, r := range s {
		width += runeWidth(r)
	}
	return width
}

func runeWidth(r rune) int {
	if r == 0 {
		return 0
	}
	if r <= 0x1F || (r >= 0x7F && r <= 0x9F) {
		return 0
	}
	if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
		return 2
	}
	// Fullwidth and wide forms range
	if (r >= 0x1100 && r <= 0x115F) || (r >= 0x2E80 && r <= 0xA4CF) || (r >= 0xAC00 && r <= 0xD7A3) || (r >= 0xF900 && r <= 0xFAFF) || (r >= 0xFE10 && r <= 0xFE19) || (r >= 0xFE30 && r <= 0xFE6F) || (r >= 0xFF00 && r <= 0xFF60) || (r >= 0xFFE0 && r <= 0xFFE6) {
		return 2
	}
	return 1
}

func applyColor(text string, color Color) string {
	switch color {
	case ColorPositive, ColorBuy:
		return "\033[32m" + text + "\033[0m"
	case ColorNegative, ColorSell:
		return "\033[31m" + text + "\033[0m"
	default:
		return text
	}
}

func buildPnLLines(snapshot PnLSnapshot) []Line {
	if snapshot == (PnLSnapshot{}) {
		return []Line{
			{Text: "当日已实现盈亏： --"},
			{Text: "当前未实现盈亏： --"},
			{Text: "账户净值： --"},
			{Text: "保 证 金 使用率： --"},
			{Text: "可用余额： --"},
			{Text: "风控状态： --"},
		}
	}

	realizedColor := chooseSignColor(snapshot.Realized)
	unrealizedColor := chooseSignColor(snapshot.Unrealized)

	lines := []Line{
		{Text: fmt.Sprintf("当日已实现盈亏： %s", formatCurrency(snapshot.Realized)), Color: realizedColor},
		{Text: fmt.Sprintf("当前未实现盈亏： %s", formatCurrency(snapshot.Unrealized)), Color: unrealizedColor},
		{Text: fmt.Sprintf("账户净值： %.2f USDT", snapshot.Equity)},
		{Text: fmt.Sprintf("保证金使用率： %.1f%%", snapshot.MarginUsage)},
		{Text: fmt.Sprintf("可用余额： %.2f USDT", snapshot.Available)},
		{Text: fmt.Sprintf("风控状态： %s", snapshot.RiskStatus)},
	}
	return lines
}

func chooseSignColor(value float64) Color {
	if value > 0.0001 {
		return ColorPositive
	}
	if value < -0.0001 {
		return ColorNegative
	}
	return ColorNone
}

func formatCurrency(value float64) string {
	if math.Abs(value) < 0.005 {
		return "0.00 USDT"
	}
	return fmt.Sprintf("%+.2f USDT", value)
}
