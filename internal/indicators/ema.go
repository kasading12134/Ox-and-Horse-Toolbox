package indicators

import (
    "errors"
    "math"
)

// EMA computes the exponential moving average for the provided series.
func EMA(series []float64, period int) ([]float64, error) {
    if period <= 0 {
        return nil, errors.New("period must be positive")
    }
    if len(series) < period {
        return nil, errors.New("series length smaller than period")
    }

    ema := make([]float64, len(series))
    multiplier := 2.0 / float64(period+1)

    // Seed the first EMA with a simple average.
    sum := 0.0
    for i := 0; i < period; i++ {
        sum += series[i]
    }
    ema[period-1] = sum / float64(period)

    for i := 0; i < period-1; i++ {
        ema[i] = math.NaN()
    }

    for i := period; i < len(series); i++ {
        ema[i] = ((series[i] - ema[i-1]) * multiplier) + ema[i-1]
    }

    return ema, nil
}
