package binance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"autobot/internal/strategy"
)

const defaultBaseURL = "https://fapi.binance.com"

// Client implements the minimal http bindings we need for Binance futures.
type Client struct {
	apiKey     string
	apiSecret  string
	baseURL    string
	httpClient *http.Client
}

// New returns a ready-to-use client.
func New(apiKey, apiSecret, baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// OrderSide represents BUY or SELL.
type OrderSide string

// PositionSide indicates hedge mode orientation.
type PositionSide string

// OrderType identifies execution type.
type OrderType string

// TimeInForce instructs how a limit order lives in the book.
type TimeInForce string

const (
	OrderSideBuy  OrderSide = "BUY"
	OrderSideSell OrderSide = "SELL"

	PositionSideBoth  PositionSide = "BOTH"
	PositionSideLong  PositionSide = "LONG"
	PositionSideShort PositionSide = "SHORT"

	OrderTypeMarket           OrderType = "MARKET"
	OrderTypeLimit            OrderType = "LIMIT"
	OrderTypeStopMarket       OrderType = "STOP_MARKET"
	OrderTypeTakeProfitMarket OrderType = "TAKE_PROFIT_MARKET"

	TimeInForceGTC TimeInForce = "GTC"
	TimeInForceIOC TimeInForce = "IOC"
	TimeInForceFOK TimeInForce = "FOK"
)

// OrderRequest contains the minimum parameters for a futures order.
type OrderRequest struct {
	Symbol       string
	Side         OrderSide
	PositionSide PositionSide
	Type         OrderType
	Quantity     float64
	ReduceOnly   bool
	Price        float64
	TimeInForce  TimeInForce
	StopPrice    float64
	WorkingType  string
}

// OrderResponse maps the subset of response fields we care about.
type OrderResponse struct {
	Symbol        string    `json:"symbol"`
	OrderID       int64     `json:"orderId"`
	ClientOrderID string    `json:"clientOrderId"`
	TransactTime  int64     `json:"transactTime"`
	AvgPrice      string    `json:"avgPrice"`
	ExecutedQty   string    `json:"executedQty"`
	Status        string    `json:"status"`
	UpdateTime    time.Time `json:"-"`
}

// AccountInfo carries wallet data relevant to risk controls.
type AccountInfo struct {
	TotalWalletBalance float64
	AvailableBalance   float64
	CrossUnrealizedPNL float64
	LastUpdate         time.Time
}

// PositionRisk captures futures position status.
type PositionRisk struct {
	Symbol        string
	PositionSide  PositionSide
	Quantity      float64
	EntryPrice    float64
	MarkPrice     float64
	Leverage      float64
	UnrealizedPNL float64
	UpdateTime    time.Time
}

// GetKlines retrieves recent OHLCV data for the strategy evaluation.
func (c *Client) GetKlines(ctx context.Context, symbol, interval string, limit int) ([]strategy.Candle, error) {
	endpoint := fmt.Sprintf("%s/fapi/v1/klines", c.baseURL)
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("interval", interval)
	params.Set("limit", strconv.Itoa(limit))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get klines: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("klines status %d: %s", resp.StatusCode, string(data))
	}

	var raw [][]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode klines: %w", err)
	}

	candles := make([]strategy.Candle, 0, len(raw))
	for _, entry := range raw {
		if len(entry) < 6 {
			continue
		}
		openTime := time.UnixMilli(int64(entry[0].(float64)))
		open, _ := strconv.ParseFloat(fmt.Sprint(entry[1]), 64)
		high, _ := strconv.ParseFloat(fmt.Sprint(entry[2]), 64)
		low, _ := strconv.ParseFloat(fmt.Sprint(entry[3]), 64)
		closePrice, _ := strconv.ParseFloat(fmt.Sprint(entry[4]), 64)
		volume, _ := strconv.ParseFloat(fmt.Sprint(entry[5]), 64)

		candles = append(candles, strategy.Candle{
			OpenTime: openTime,
			Open:     open,
			High:     high,
			Low:      low,
			Close:    closePrice,
			Volume:   volume,
		})
	}

	if len(candles) == 0 {
		return nil, errors.New("no candles returned")
	}

	return candles, nil
}

// GetPositions retrieves current futures positions.
func (c *Client) GetPositions(ctx context.Context, symbol string) ([]PositionRisk, error) {
	if c.apiKey == "" || c.apiSecret == "" {
		return nil, errors.New("api key/secret required for position endpoints")
	}

	endpoint := fmt.Sprintf("%s/fapi/v2/positionRisk", c.baseURL)
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	params.Set("recvWindow", "5000")
	signature := sign(c.apiSecret, params.Encode())
	params.Set("signature", signature)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-MBX-APIKEY", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get positions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("positions status %d: %s", resp.StatusCode, string(data))
	}

	var payload []struct {
		Symbol       string `json:"symbol"`
		PositionAmt  string `json:"positionAmt"`
		EntryPrice   string `json:"entryPrice"`
		MarkPrice    string `json:"markPrice"`
		UnRealizedPn string `json:"unRealizedProfit"`
		Leverage     string `json:"leverage"`
		PositionSide string `json:"positionSide"`
		UpdateTime   int64  `json:"updateTime"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode positions: %w", err)
	}

	positions := make([]PositionRisk, 0, len(payload))
	for _, item := range payload {
		qty, _ := strconv.ParseFloat(item.PositionAmt, 64)
		if qty == 0 {
			continue
		}
		entry, _ := strconv.ParseFloat(item.EntryPrice, 64)
		mark, _ := strconv.ParseFloat(item.MarkPrice, 64)
		pnl, _ := strconv.ParseFloat(item.UnRealizedPn, 64)
		lev, _ := strconv.ParseFloat(item.Leverage, 64)

		positions = append(positions, PositionRisk{
			Symbol:        item.Symbol,
			PositionSide:  PositionSide(item.PositionSide),
			Quantity:      qty,
			EntryPrice:    entry,
			MarkPrice:     mark,
			UnrealizedPNL: pnl,
			Leverage:      lev,
			UpdateTime:    time.UnixMilli(item.UpdateTime),
		})
	}

	return positions, nil
}

// GetAccountInfo pulls wallet info for risk sizing.
func (c *Client) GetAccountInfo(ctx context.Context) (AccountInfo, error) {
	if c.apiKey == "" || c.apiSecret == "" {
		return AccountInfo{}, errors.New("api key/secret required for private endpoints")
	}

	endpoint := fmt.Sprintf("%s/fapi/v2/account", c.baseURL)
	params := url.Values{}
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	params.Set("recvWindow", "5000")
	signature := sign(c.apiSecret, params.Encode())
	params.Set("signature", signature)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return AccountInfo{}, err
	}
	req.Header.Set("X-MBX-APIKEY", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return AccountInfo{}, fmt.Errorf("get account info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return AccountInfo{}, fmt.Errorf("account info status %d: %s", resp.StatusCode, string(data))
	}

	var payload struct {
		TotalWalletBalance string `json:"totalWalletBalance"`
		AvailableBalance   string `json:"availableBalance"`
		CrossUnPNL         string `json:"totalCrossUnPnl"`
		UpdateTime         int64  `json:"updateTime"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return AccountInfo{}, fmt.Errorf("decode account info: %w", err)
	}

	wallet, _ := strconv.ParseFloat(payload.TotalWalletBalance, 64)
	avail, _ := strconv.ParseFloat(payload.AvailableBalance, 64)
	pnl, _ := strconv.ParseFloat(payload.CrossUnPNL, 64)

	return AccountInfo{
		TotalWalletBalance: wallet,
		AvailableBalance:   avail,
		CrossUnrealizedPNL: pnl,
		LastUpdate:         time.UnixMilli(payload.UpdateTime),
	}, nil
}

// PlaceOrder submits an order to Binance futures.
func (c *Client) PlaceOrder(ctx context.Context, reqPayload OrderRequest) (OrderResponse, error) {
	if c.apiKey == "" || c.apiSecret == "" {
		return OrderResponse{}, errors.New("api key/secret required for trading")
	}

	endpoint := fmt.Sprintf("%s/fapi/v1/order", c.baseURL)
	params := url.Values{}
	params.Set("symbol", reqPayload.Symbol)
	params.Set("side", string(reqPayload.Side))
	if reqPayload.PositionSide != "" {
		params.Set("positionSide", string(reqPayload.PositionSide))
	}
	params.Set("type", string(reqPayload.Type))
	params.Set("quantity", formatQuantity(reqPayload.Quantity))
	if reqPayload.Type == OrderTypeLimit {
		params.Set("price", formatPrice(reqPayload.Price))
		if reqPayload.TimeInForce == "" {
			params.Set("timeInForce", string(TimeInForceGTC))
		} else {
			params.Set("timeInForce", string(reqPayload.TimeInForce))
		}
	}
	if reqPayload.StopPrice > 0 {
		params.Set("stopPrice", formatPrice(reqPayload.StopPrice))
	}
	if reqPayload.WorkingType != "" {
		params.Set("workingType", reqPayload.WorkingType)
	}
	if reqPayload.ReduceOnly {
		params.Set("reduceOnly", "true")
	}
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	params.Set("recvWindow", "5000")

	signature := sign(c.apiSecret, params.Encode())
	params.Set("signature", signature)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, io.NopCloser(strings.NewReader(params.Encode())))
	if err != nil {
		return OrderResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("X-MBX-APIKEY", c.apiKey)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return OrderResponse{}, fmt.Errorf("place order: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return OrderResponse{}, fmt.Errorf("order status %d: %s", resp.StatusCode, string(data))
	}

	var payload OrderResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return OrderResponse{}, fmt.Errorf("decode order response: %w", err)
	}

	payload.UpdateTime = time.UnixMilli(payload.TransactTime)
	return payload, nil
}

func formatQuantity(q float64) string {
	return strconv.FormatFloat(q, 'f', -1, 64)
}

func formatPrice(p float64) string {
	return strconv.FormatFloat(p, 'f', -1, 64)
}

// GetFundingRate fetches the current funding rate (last funding) for the symbol.
func (c *Client) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	endpoint := fmt.Sprintf("%s/fapi/v1/premiumIndex", c.baseURL)
	params := url.Values{}
	params.Set("symbol", symbol)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return 0, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("get funding rate: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("funding rate status %d: %s", resp.StatusCode, string(data))
	}

	var payload struct {
		LastFundingRate string `json:"lastFundingRate"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, fmt.Errorf("decode funding rate: %w", err)
	}

	rate, err := strconv.ParseFloat(payload.LastFundingRate, 64)
	if err != nil {
		return 0, fmt.Errorf("parse funding rate: %w", err)
	}

	return rate, nil
}

// GetOpenInterest fetches the current open interest for the symbol.
func (c *Client) GetOpenInterest(ctx context.Context, symbol string) (float64, error) {
	endpoint := fmt.Sprintf("%s/fapi/v1/openInterest", c.baseURL)
	params := url.Values{}
	params.Set("symbol", symbol)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return 0, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("get open interest: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("open interest status %d: %s", resp.StatusCode, string(data))
	}

	var payload struct {
		OpenInterest string `json:"openInterest"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, fmt.Errorf("decode open interest: %w", err)
	}

	oi, err := strconv.ParseFloat(payload.OpenInterest, 64)
	if err != nil {
		return 0, fmt.Errorf("parse open interest: %w", err)
	}

	return oi, nil
}
