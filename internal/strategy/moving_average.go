package strategy

import (
    "fmt"

    "autobot/internal/indicators"
)

// MovingAverageCrossover implements a common trend-following approach.
type MovingAverageCrossover struct {
    FastPeriod int
    SlowPeriod int
}

func (m MovingAverageCrossover) Name() string {
    return "ema_crossover"
}

// Evaluate returns signals when fast EMA crosses slow EMA.
func (m MovingAverageCrossover) Evaluate(candles []Candle) (Signal, error) {
    if len(candles) == 0 {
        return SignalHold, fmt.Errorf("no candles provided")
    }
    if m.FastPeriod <= 0 || m.SlowPeriod <= 0 {
        return SignalHold, fmt.Errorf("ema periods must be positive")
    }
    if m.FastPeriod >= m.SlowPeriod {
        return SignalHold, fmt.Errorf("fast period must be smaller than slow period")
    }
    if len(candles) < m.SlowPeriod+1 {
        return SignalHold, fmt.Errorf("need at least %d candles", m.SlowPeriod+1)
    }

    closes := make([]float64, len(candles))
    for i, c := range candles {
        closes[i] = c.Close
    }

    fast, err := indicators.EMA(closes, m.FastPeriod)
    if err != nil {
        return SignalHold, err
    }
    slow, err := indicators.EMA(closes, m.SlowPeriod)
    if err != nil {
        return SignalHold, err
    }

    last := len(closes) - 1
    prev := last - 1

    fastPrev := fast[prev]
    fastLast := fast[last]
    slowPrev := slow[prev]
    slowLast := slow[last]

    if fastPrev <= slowPrev && fastLast > slowLast {
        return SignalLong, nil
    }
    if fastPrev >= slowPrev && fastLast < slowLast {
        return SignalShort, nil
    }

    return SignalHold, nil
}
