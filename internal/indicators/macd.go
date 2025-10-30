package indicators

import (
	"errors"
	"math"
)

// MACD calculates the MACD line, signal line, and histogram.
func MACD(series []float64, fastPeriod, slowPeriod, signalPeriod int) ([]float64, []float64, []float64, error) {
	if fastPeriod <= 0 || slowPeriod <= 0 || signalPeriod <= 0 {
		return nil, nil, nil, errors.New("periods must be positive")
	}
	if fastPeriod >= slowPeriod {
		return nil, nil, nil, errors.New("fast period must be smaller than slow period")
	}
	if len(series) < slowPeriod+signalPeriod {
		return nil, nil, nil, errors.New("series length smaller than required periods")
	}

	fast, err := EMA(series, fastPeriod)
	if err != nil {
		return nil, nil, nil, err
	}
	slow, err := EMA(series, slowPeriod)
	if err != nil {
		return nil, nil, nil, err
	}

	macdLine := make([]float64, len(series))
	for i := range series {
		if math.IsNaN(fast[i]) || math.IsNaN(slow[i]) {
			macdLine[i] = math.NaN()
			continue
		}
		macdLine[i] = fast[i] - slow[i]
	}

	// Build valid slice for signal EMA starting from first non-NaN.
	firstValid := -1
	for i, v := range macdLine {
		if !math.IsNaN(v) {
			firstValid = i
			break
		}
	}
	if firstValid == -1 || len(macdLine)-firstValid < signalPeriod {
		return nil, nil, nil, errors.New("insufficient data for signal line")
	}

	signalInput := macdLine[firstValid:]
	signalRaw, err := EMA(signalInput, signalPeriod)
	if err != nil {
		return nil, nil, nil, err
	}

	signalLine := make([]float64, len(series))
	histLine := make([]float64, len(series))
	for i := 0; i < firstValid+signalPeriod-1; i++ {
		signalLine[i] = math.NaN()
		histLine[i] = math.NaN()
	}
	for idx, val := range signalRaw {
		signalIdx := firstValid + idx
		signalLine[signalIdx] = val
		if math.IsNaN(macdLine[signalIdx]) || math.IsNaN(val) {
			histLine[signalIdx] = math.NaN()
		} else {
			histLine[signalIdx] = macdLine[signalIdx] - val
		}
	}

	return macdLine, signalLine, histLine, nil
}
