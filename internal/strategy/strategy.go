package strategy

import "time"

// Signal represents the action recommended by a strategy.
type Signal int

const (
    SignalHold Signal = iota
    SignalLong
    SignalShort
    SignalExit
)

func (s Signal) String() string {
    switch s {
    case SignalLong:
        return "long"
    case SignalShort:
        return "short"
    case SignalExit:
        return "exit"
    default:
        return "hold"
    }
}

// Candle represents OHLCV data.
type Candle struct {
    OpenTime time.Time
    Open     float64
    High     float64
    Low      float64
    Close    float64
    Volume   float64
}

// Strategy decides what to do based on recent candles.
type Strategy interface {
    Evaluate(candles []Candle) (Signal, error)
    Name() string
}
