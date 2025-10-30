package strategy

import (
	"fmt"
	"math"

	"autobot/internal/indicators"
)

// CompositeStrategy combines EMA crossover with RSI and MACD confirmation.
type CompositeStrategy struct {
	FastEMAPeriod    int
	SlowEMAPeriod    int
	RSIPeriod        int
	RSIUpper         float64
	RSILower         float64
	MACDFastPeriod   int
	MACDSlowPeriod   int
	MACDSignalPeriod int
}

func (c CompositeStrategy) Name() string {
	return "ema_rsi_macd"
}

func (c CompositeStrategy) withDefaults() CompositeStrategy {
	cfg := c
	if cfg.FastEMAPeriod == 0 {
		cfg.FastEMAPeriod = 12
	}
	if cfg.SlowEMAPeriod == 0 {
		cfg.SlowEMAPeriod = 26
	}
	if cfg.RSIPeriod == 0 {
		cfg.RSIPeriod = 14
	}
	if cfg.RSIUpper == 0 {
		cfg.RSIUpper = 55
	}
	if cfg.RSILower == 0 {
		cfg.RSILower = 45
	}
	if cfg.MACDFastPeriod == 0 {
		cfg.MACDFastPeriod = 12
	}
	if cfg.MACDSlowPeriod == 0 {
		cfg.MACDSlowPeriod = 26
	}
	if cfg.MACDSignalPeriod == 0 {
		cfg.MACDSignalPeriod = 9
	}
	return cfg
}

// Evaluate checks EMA crossover and requires RSI/MACD confirmation.
func (c CompositeStrategy) Evaluate(candles []Candle) (Signal, error) {
	cfg := c.withDefaults()

	if len(candles) == 0 {
		return SignalHold, fmt.Errorf("no candles provided")
	}
	if cfg.FastEMAPeriod <= 0 || cfg.SlowEMAPeriod <= 0 || cfg.MACDFastPeriod <= 0 || cfg.MACDSlowPeriod <= 0 || cfg.MACDSignalPeriod <= 0 || cfg.RSIPeriod <= 0 {
		return SignalHold, fmt.Errorf("periods must be positive")
	}
	if cfg.FastEMAPeriod >= cfg.SlowEMAPeriod {
		return SignalHold, fmt.Errorf("fast EMA period must be smaller than slow EMA period")
	}
	if cfg.MACDFastPeriod >= cfg.MACDSlowPeriod {
		return SignalHold, fmt.Errorf("MACD fast period must be smaller than slow period")
	}

	minLen := maxInt(cfg.SlowEMAPeriod+1, cfg.MACDSlowPeriod+cfg.MACDSignalPeriod, cfg.RSIPeriod+1)
	if len(candles) < minLen {
		return SignalHold, fmt.Errorf("need at least %d candles", minLen)
	}

	closes := make([]float64, len(candles))
	for i, cndl := range candles {
		closes[i] = cndl.Close
	}

	fast, err := indicators.EMA(closes, cfg.FastEMAPeriod)
	if err != nil {
		return SignalHold, err
	}
	slow, err := indicators.EMA(closes, cfg.SlowEMAPeriod)
	if err != nil {
		return SignalHold, err
	}

	macdLine, signalLine, histLine, err := indicators.MACD(closes, cfg.MACDFastPeriod, cfg.MACDSlowPeriod, cfg.MACDSignalPeriod)
	if err != nil {
		return SignalHold, err
	}
	rsiValues, err := indicators.RSI(closes, cfg.RSIPeriod)
	if err != nil {
		return SignalHold, err
	}

	last := len(closes) - 1
	prev := last - 1

	if math.IsNaN(fast[last]) || math.IsNaN(fast[prev]) || math.IsNaN(slow[last]) || math.IsNaN(slow[prev]) {
		return SignalHold, fmt.Errorf("ema not ready")
	}

	candidate := SignalHold
	fastPrev := fast[prev]
	fastLast := fast[last]
	slowPrev := slow[prev]
	slowLast := slow[last]

	if fastPrev <= slowPrev && fastLast > slowLast {
		candidate = SignalLong
	} else if fastPrev >= slowPrev && fastLast < slowLast {
		candidate = SignalShort
	}

	if candidate == SignalHold {
		return SignalHold, nil
	}

	rsi := rsiValues[last]
	macdHist := histLine[last]
	macdLineLast := macdLine[last]
	macdSignalLast := signalLine[last]

	if math.IsNaN(rsi) || math.IsNaN(macdHist) || math.IsNaN(macdLineLast) || math.IsNaN(macdSignalLast) {
		return SignalHold, fmt.Errorf("indicators not ready")
	}

	switch candidate {
	case SignalLong:
		if rsi < cfg.RSIUpper {
			return SignalHold, nil
		}
		if macdHist <= 0 || macdLineLast <= macdSignalLast {
			return SignalHold, nil
		}
		return SignalLong, nil
	case SignalShort:
		if rsi > cfg.RSILower {
			return SignalHold, nil
		}
		if macdHist >= 0 || macdLineLast >= macdSignalLast {
			return SignalHold, nil
		}
		return SignalShort, nil
	default:
		return SignalHold, nil
	}
}

func maxInt(values ...int) int {
	max := values[0]
	for _, v := range values[1:] {
		if v > max {
			max = v
		}
	}
	return max
}
