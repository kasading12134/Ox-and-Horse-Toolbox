package indicators

import (
	"errors"
	"math"
)

// RSI calculates the Relative Strength Index using Wilder's smoothing.
func RSI(series []float64, period int) ([]float64, error) {
	if period <= 0 {
		return nil, errors.New("period must be positive")
	}
	if len(series) < period+1 {
		return nil, errors.New("series length smaller than period")
	}

	rsi := make([]float64, len(series))

	avgGain := 0.0
	avgLoss := 0.0
	for i := 1; i <= period; i++ {
		change := series[i] - series[i-1]
		if change > 0 {
			avgGain += change
		} else {
			avgLoss -= change
		}
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)

	for i := 0; i < period; i++ {
		rsi[i] = math.NaN()
	}

	rs := 0.0
	if avgLoss == 0 {
		rs = math.Inf(1)
	} else {
		rs = avgGain / avgLoss
	}
	rsi[period] = 100 - (100 / (1 + rs))

	for i := period + 1; i < len(series); i++ {
		change := series[i] - series[i-1]
		gain := 0.0
		loss := 0.0
		if change > 0 {
			gain = change
		} else {
			loss = -change
		}
		avgGain = ((avgGain * float64(period-1)) + gain) / float64(period)
		avgLoss = ((avgLoss * float64(period-1)) + loss) / float64(period)

		if avgLoss == 0 {
			rsi[i] = 100
		} else {
			rs = avgGain / avgLoss
			rsi[i] = 100 - (100 / (1 + rs))
		}
	}

	return rsi, nil
}
