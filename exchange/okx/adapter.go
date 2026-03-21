package okx

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"opensqt/logger"
)

const (
	okxInstTypeSwap  = "SWAP"
	okxTDModeCross   = "cross"
	okxPosModeHedge  = "long_short_mode"
	okxPublicWSURL   = "wss://ws.okx.com:8443/ws/v5/public"
	okxPrivateWSURL  = "wss://ws.okx.com:8443/ws/v5/private"
	okxBusinessWSURL = "wss://ws.okx.com:8443/ws/v5/business"
)

type OKXAdapter struct {
	client         *Client
	wsManager      *WebSocketManager
	klineWSManager *KlineWebSocketManager

	symbol           string
	instID           string
	tdMode           string
	posMode          string
	baseAsset        string
	quoteAsset       string
	settleAsset      string
	priceTick        float64
	priceDecimals    int
	contractValue    float64
	contractStep     float64
	minContracts     float64
	quantityStep     float64
	quantityDecimals int
	accountLeverage  int
}

func NewOKXAdapter(cfg map[string]string, symbol string) (*OKXAdapter, error) {
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]
	passphrase := cfg["passphrase"]
	if apiKey == "" || secretKey == "" || passphrase == "" {
		return nil, fmt.Errorf("okx API config incomplete")
	}

	adapter := &OKXAdapter{
		client:         NewClient(apiKey, secretKey, passphrase),
		wsManager:      NewWebSocketManager(apiKey, secretKey, passphrase),
		klineWSManager: NewKlineWebSocketManager(),
		symbol:         symbol,
		instID:         convertToOKXSwapInstID(symbol),
		tdMode:         okxTDModeCross,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := adapter.fetchInstrumentInfo(ctx); err != nil {
		return nil, err
	}
	if err := adapter.fetchAccountConfig(ctx); err != nil {
		return nil, err
	}
	adapter.fetchLeverageInfo(ctx)

	return adapter, nil
}

func (o *OKXAdapter) GetName() string {
	return "OKX"
}

func convertToOKXSwapInstID(symbol string) string {
	upper := strings.ToUpper(strings.TrimSpace(symbol))
	if strings.HasSuffix(upper, "-SWAP") {
		return upper
	}
	for _, quote := range []string{"USDT", "USDC"} {
		if strings.HasSuffix(upper, quote) {
			base := strings.TrimSuffix(upper, quote)
			return fmt.Sprintf("%s-%s-SWAP", base, quote)
		}
	}
	return upper
}

func mapOrderIntent(side Side, reduceOnly bool) (string, string, error) {
	switch side {
	case SideBuy:
		if reduceOnly {
			return "buy", "short", nil
		}
		return "buy", "long", nil
	case SideSell:
		if reduceOnly {
			return "sell", "long", nil
		}
		return "sell", "short", nil
	default:
		return "", "", fmt.Errorf("unsupported side: %s", side)
	}
}

func normalizeOrderState(state string) OrderStatus {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "live":
		return OrderStatusNew
	case "partially_filled":
		return OrderStatusPartiallyFilled
	case "filled":
		return OrderStatusFilled
	case "canceled", "cancelled", "mmp_canceled":
		return OrderStatusCanceled
	case "order_failed":
		return OrderStatusRejected
	default:
		return OrderStatus(state)
	}
}

func contractsFromBaseQuantity(baseQty, contractSz, step float64) (string, error) {
	if baseQty <= 0 || contractSz <= 0 || step <= 0 {
		return "", fmt.Errorf("invalid quantity conversion input")
	}
	rawContracts := baseQty / contractSz
	steps := math.Floor((rawContracts / step) + 1e-9)
	contracts := steps * step
	if contracts < step {
		return "", fmt.Errorf("quantity %.8f is below minimum contract size", baseQty)
	}
	return formatFloatByStep(contracts, step), nil
}

func formatFloatByStep(value, step float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.*f", decimalPlaces(step), value), "0"), ".")
}

func decimalPlaces(value float64) int {
	if value <= 0 {
		return 0
	}
	text := strconv.FormatFloat(value, 'f', -1, 64)
	parts := strings.Split(text, ".")
	if len(parts) != 2 {
		return 0
	}
	return len(parts[1])
}

func parseFloat(value string) float64 {
	f, _ := strconv.ParseFloat(value, 64)
	return f
}

func normalizeKlineBar(interval string) string {
	switch strings.ToLower(strings.TrimSpace(interval)) {
	case "1m":
		return "1m"
	case "3m":
		return "3m"
	case "5m":
		return "5m"
	case "15m":
		return "15m"
	case "30m":
		return "30m"
	case "1h":
		return "1H"
	case "4h":
		return "4H"
	case "1d":
		return "1D"
	default:
		return interval
	}
}

func parseOKXCandle(symbol string, raw []string) (*Candle, error) {
	if len(raw) < 6 {
		return nil, fmt.Errorf("invalid candle payload")
	}
	ts, _ := strconv.ParseInt(raw[0], 10, 64)
	open, _ := strconv.ParseFloat(raw[1], 64)
	high, _ := strconv.ParseFloat(raw[2], 64)
	low, _ := strconv.ParseFloat(raw[3], 64)
	closePrice, _ := strconv.ParseFloat(raw[4], 64)
	volume, _ := strconv.ParseFloat(raw[5], 64)
	isClosed := true
	if len(raw) >= 9 {
		isClosed = raw[8] == "1"
	}
	return &Candle{
		Symbol:    strings.ReplaceAll(strings.TrimSuffix(symbol, "-SWAP"), "-", ""),
		Open:      open,
		High:      high,
		Low:       low,
		Close:     closePrice,
		Volume:    volume,
		Timestamp: ts,
		IsClosed:  isClosed,
	}, nil
}

func (o *OKXAdapter) fetchInstrumentInfo(ctx context.Context) error {
	query := url.Values{}
	query.Set("instType", okxInstTypeSwap)
	query.Set("instId", o.instID)

	data, err := o.client.DoRequest(ctx, "GET", "/api/v5/public/instruments", query, nil)
	if err != nil {
		return fmt.Errorf("fetch OKX instrument info: %w", err)
	}

	var instruments []struct {
		TickSz    string `json:"tickSz"`
		LotSz     string `json:"lotSz"`
		MinSz     string `json:"minSz"`
		CtVal     string `json:"ctVal"`
		BaseCcy   string `json:"baseCcy"`
		QuoteCcy  string `json:"quoteCcy"`
		SettleCcy string `json:"settleCcy"`
	}
	if err := json.Unmarshal(data, &instruments); err != nil {
		return fmt.Errorf("decode OKX instrument info: %w", err)
	}
	if len(instruments) == 0 {
		return fmt.Errorf("OKX instrument not found: %s", o.instID)
	}

	inst := instruments[0]
	o.baseAsset = inst.BaseCcy
	o.quoteAsset = inst.QuoteCcy
	o.settleAsset = inst.SettleCcy
	o.priceTick = parseFloat(inst.TickSz)
	o.contractStep = parseFloat(inst.LotSz)
	o.minContracts = parseFloat(inst.MinSz)
	o.contractValue = parseFloat(inst.CtVal)
	if o.contractStep == 0 {
		o.contractStep = 1
	}
	if o.minContracts == 0 {
		o.minContracts = o.contractStep
	}
	if o.contractValue <= 0 {
		return fmt.Errorf("invalid OKX contract value for %s", o.instID)
	}
	o.priceDecimals = decimalPlaces(o.priceTick)
	o.quantityStep = o.contractValue * o.contractStep
	o.quantityDecimals = decimalPlaces(o.quantityStep)

	logger.Info("INFO [OKX instrument] %s - qty step:%d, price step:%d, contract value:%g, base:%s, quote:%s",
		o.instID, o.quantityDecimals, o.priceDecimals, o.contractValue, o.baseAsset, o.quoteAsset)

	return nil
}

func (o *OKXAdapter) fetchAccountConfig(ctx context.Context) error {
	data, err := o.client.DoRequest(ctx, "GET", "/api/v5/account/config", nil, nil)
	if err != nil {
		return fmt.Errorf("fetch OKX account config: %w", err)
	}

	var configs []struct {
		PosMode string `json:"posMode"`
	}
	if err := json.Unmarshal(data, &configs); err != nil {
		return fmt.Errorf("decode OKX account config: %w", err)
	}
	if len(configs) == 0 {
		return fmt.Errorf("OKX account config missing")
	}
	o.posMode = configs[0].PosMode
	if o.posMode != okxPosModeHedge {
		return fmt.Errorf("OKX account posMode=%s, required=%s for this integration", o.posMode, okxPosModeHedge)
	}
	return nil
}

func (o *OKXAdapter) fetchLeverageInfo(ctx context.Context) {
	query := url.Values{}
	query.Set("instId", o.instID)
	query.Set("mgnMode", o.tdMode)
	data, err := o.client.DoRequest(ctx, "GET", "/api/v5/account/leverage-info", query, nil)
	if err != nil {
		return
	}
	var leverages []struct {
		Lever string `json:"lever"`
	}
	if err := json.Unmarshal(data, &leverages); err != nil || len(leverages) == 0 {
		return
	}
	if lev, err := strconv.Atoi(strings.Split(leverages[0].Lever, ".")[0]); err == nil {
		o.accountLeverage = lev
	}
}

func (o *OKXAdapter) convertContractsToBase(contracts string) float64 {
	return parseFloat(contracts) * o.contractValue
}

func (o *OKXAdapter) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	side, posSide, err := mapOrderIntent(req.Side, req.ReduceOnly)
	if err != nil {
		return nil, err
	}

	contracts, err := contractsFromBaseQuantity(req.Quantity, o.contractValue, o.contractStep)
	if err != nil {
		return nil, err
	}
	if parseFloat(contracts) < o.minContracts {
		return nil, fmt.Errorf("OKX order size below minimum: %s < %s contracts", contracts, formatFloatByStep(o.minContracts, o.contractStep))
	}

	ordType := "limit"
	if req.PostOnly {
		ordType = "post_only"
	}

	body := map[string]string{
		"instId":  o.instID,
		"tdMode":  o.tdMode,
		"side":    side,
		"ordType": ordType,
		"px":      fmt.Sprintf("%.*f", req.PriceDecimals, req.Price),
		"sz":      contracts,
		"posSide": posSide,
	}
	if req.ClientOrderID != "" {
		body["clOrdId"] = req.ClientOrderID
	}

	data, err := o.client.DoRequest(ctx, "POST", "/api/v5/trade/order", nil, body)
	if err != nil {
		return nil, err
	}

	var result []struct {
		OrdID   string `json:"ordId"`
		ClOrdID string `json:"clOrdId"`
		SCode   string `json:"sCode"`
		SMsg    string `json:"sMsg"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode OKX order response: %w", err)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("empty OKX order response")
	}
	if result[0].SCode != "" && result[0].SCode != "0" {
		return nil, fmt.Errorf("okx order rejected: code=%s msg=%s", result[0].SCode, result[0].SMsg)
	}

	orderID, _ := strconv.ParseInt(result[0].OrdID, 10, 64)
	return &Order{
		OrderID:       orderID,
		ClientOrderID: result[0].ClOrdID,
		Symbol:        o.symbol,
		Side:          req.Side,
		Type:          OrderTypeLimit,
		Price:         req.Price,
		Quantity:      req.Quantity,
		Status:        OrderStatusNew,
		CreatedAt:     time.Now(),
	}, nil
}

func (o *OKXAdapter) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	results := make([]*Order, 0, len(orders))
	hasMarginError := false
	for _, req := range orders {
		order, err := o.PlaceOrder(ctx, req)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "insufficient") {
				hasMarginError = true
			}
			logger.Warn("WARN [OKX] place order failed %.2f %s: %v", req.Price, req.Side, err)
			continue
		}
		results = append(results, order)
	}
	return results, hasMarginError
}

func (o *OKXAdapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	body := map[string]string{
		"instId": o.instID,
		"ordId":  strconv.FormatInt(orderID, 10),
	}
	data, err := o.client.DoRequest(ctx, "POST", "/api/v5/trade/cancel-order", nil, body)
	if err != nil {
		return err
	}
	var result []struct {
		SCode string `json:"sCode"`
		SMsg  string `json:"sMsg"`
	}
	if err := json.Unmarshal(data, &result); err == nil && len(result) > 0 && result[0].SCode != "" && result[0].SCode != "0" {
		return fmt.Errorf("okx cancel rejected: code=%s msg=%s", result[0].SCode, result[0].SMsg)
	}
	return nil
}

func (o *OKXAdapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	if len(orderIDs) == 0 {
		return nil
	}
	payload := make([]map[string]string, 0, len(orderIDs))
	for _, orderID := range orderIDs {
		payload = append(payload, map[string]string{
			"instId": o.instID,
			"ordId":  strconv.FormatInt(orderID, 10),
		})
	}
	data, err := o.client.DoRequest(ctx, "POST", "/api/v5/trade/cancel-batch-orders", nil, payload)
	if err != nil {
		return err
	}
	var result []struct {
		SCode string `json:"sCode"`
		SMsg  string `json:"sMsg"`
	}
	if err := json.Unmarshal(data, &result); err == nil {
		for _, item := range result {
			if item.SCode != "" && item.SCode != "0" {
				return fmt.Errorf("okx batch cancel rejected: code=%s msg=%s", item.SCode, item.SMsg)
			}
		}
	}
	return nil
}

func (o *OKXAdapter) CancelAllOrders(ctx context.Context, symbol string) error {
	orders, err := o.GetOpenOrders(ctx, symbol)
	if err != nil {
		return err
	}
	orderIDs := make([]int64, 0, len(orders))
	for _, order := range orders {
		orderIDs = append(orderIDs, order.OrderID)
	}
	return o.BatchCancelOrders(ctx, symbol, orderIDs)
}

func (o *OKXAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	query := url.Values{}
	query.Set("instId", o.instID)
	query.Set("ordId", strconv.FormatInt(orderID, 10))
	data, err := o.client.DoRequest(ctx, "GET", "/api/v5/trade/order", query, nil)
	if err != nil {
		return nil, err
	}
	var orders []okxRestOrder
	if err := json.Unmarshal(data, &orders); err != nil {
		return nil, fmt.Errorf("decode OKX order details: %w", err)
	}
	if len(orders) == 0 {
		return nil, fmt.Errorf("OKX order not found: %d", orderID)
	}
	return o.mapRESTOrder(orders[0]), nil
}

func (o *OKXAdapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	query := url.Values{}
	query.Set("instType", okxInstTypeSwap)
	query.Set("instId", o.instID)
	data, err := o.client.DoRequest(ctx, "GET", "/api/v5/trade/orders-pending", query, nil)
	if err != nil {
		return nil, err
	}
	var orders []okxRestOrder
	if err := json.Unmarshal(data, &orders); err != nil {
		return nil, fmt.Errorf("decode OKX pending orders: %w", err)
	}
	result := make([]*Order, 0, len(orders))
	for _, order := range orders {
		result = append(result, o.mapRESTOrder(order))
	}
	return result, nil
}

type okxRestOrder struct {
	OrdID     string `json:"ordId"`
	ClOrdID   string `json:"clOrdId"`
	Side      string `json:"side"`
	Px        string `json:"px"`
	Sz        string `json:"sz"`
	AccFillSz string `json:"accFillSz"`
	AvgPx     string `json:"avgPx"`
	State     string `json:"state"`
	CTime     string `json:"cTime"`
	UTime     string `json:"uTime"`
}

func (o *OKXAdapter) mapRESTOrder(order okxRestOrder) *Order {
	orderID, _ := strconv.ParseInt(order.OrdID, 10, 64)
	createTime, _ := strconv.ParseInt(order.CTime, 10, 64)
	updateTime, _ := strconv.ParseInt(order.UTime, 10, 64)
	side := SideBuy
	if strings.EqualFold(order.Side, "sell") {
		side = SideSell
	}
	return &Order{
		OrderID:       orderID,
		ClientOrderID: order.ClOrdID,
		Symbol:        o.symbol,
		Side:          side,
		Type:          OrderTypeLimit,
		Price:         parseFloat(order.Px),
		Quantity:      o.convertContractsToBase(order.Sz),
		ExecutedQty:   o.convertContractsToBase(order.AccFillSz),
		AvgPrice:      parseFloat(order.AvgPx),
		Status:        normalizeOrderState(order.State),
		CreatedAt:     time.UnixMilli(createTime),
		UpdateTime:    updateTime,
	}
}

func (o *OKXAdapter) GetAccount(ctx context.Context) (*Account, error) {
	query := url.Values{}
	query.Set("ccy", o.quoteAsset)
	data, err := o.client.DoRequest(ctx, "GET", "/api/v5/account/balance", query, nil)
	if err != nil {
		return nil, err
	}

	var balances []struct {
		Details []struct {
			Ccy      string `json:"ccy"`
			AvailBal string `json:"availBal"`
			Eq       string `json:"eq"`
		} `json:"details"`
	}
	if err := json.Unmarshal(data, &balances); err != nil {
		return nil, fmt.Errorf("decode OKX balance: %w", err)
	}
	if len(balances) == 0 {
		return nil, fmt.Errorf("OKX balance unavailable")
	}

	available := 0.0
	equity := 0.0
	for _, detail := range balances[0].Details {
		if detail.Ccy != o.quoteAsset {
			continue
		}
		available = parseFloat(detail.AvailBal)
		equity = parseFloat(detail.Eq)
		break
	}

	positions, _ := o.GetPositions(ctx, o.symbol)
	return &Account{
		TotalWalletBalance: equity,
		TotalMarginBalance: equity,
		AvailableBalance:   available,
		Positions:          positions,
		AccountLeverage:    o.accountLeverage,
		PosMode:            o.posMode,
	}, nil
}

func (o *OKXAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	query := url.Values{}
	query.Set("instType", okxInstTypeSwap)
	query.Set("instId", o.instID)
	data, err := o.client.DoRequest(ctx, "GET", "/api/v5/account/positions", query, nil)
	if err != nil {
		return nil, err
	}

	var positions []struct {
		Pos     string `json:"pos"`
		PosSide string `json:"posSide"`
		AvgPx   string `json:"avgPx"`
		MarkPx  string `json:"markPx"`
		Upl     string `json:"upl"`
		Lever   string `json:"lever"`
		MgnMode string `json:"mgnMode"`
		IMR     string `json:"imr"`
	}
	if err := json.Unmarshal(data, &positions); err != nil {
		return nil, fmt.Errorf("decode OKX positions: %w", err)
	}

	result := make([]*Position, 0, len(positions))
	for _, pos := range positions {
		sizeContracts := parseFloat(pos.Pos)
		if sizeContracts == 0 {
			continue
		}
		leverage, _ := strconv.Atoi(strings.Split(pos.Lever, ".")[0])
		size := sizeContracts * o.contractValue
		if strings.EqualFold(pos.PosSide, "short") {
			size = -size
		}
		result = append(result, &Position{
			Symbol:         o.symbol,
			Size:           size,
			EntryPrice:     parseFloat(pos.AvgPx),
			MarkPrice:      parseFloat(pos.MarkPx),
			UnrealizedPNL:  parseFloat(pos.Upl),
			Leverage:       leverage,
			MarginType:     pos.MgnMode,
			IsolatedMargin: parseFloat(pos.IMR),
		})
	}
	return result, nil
}

func (o *OKXAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	account, err := o.GetAccount(ctx)
	if err != nil {
		return 0, err
	}
	return account.AvailableBalance, nil
}

func (o *OKXAdapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return o.wsManager.StartOrderStream(ctx, o.instID, o.contractValue, callback)
}

func (o *OKXAdapter) StopOrderStream() error {
	o.wsManager.Stop()
	return nil
}

func (o *OKXAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	if latest := o.wsManager.GetLatestPrice(); latest > 0 {
		return latest, nil
	}
	query := url.Values{}
	query.Set("instId", o.instID)
	data, err := o.client.DoRequest(ctx, "GET", "/api/v5/market/ticker", query, nil)
	if err != nil {
		return 0, err
	}
	var tickers []struct {
		Last string `json:"last"`
	}
	if err := json.Unmarshal(data, &tickers); err != nil || len(tickers) == 0 {
		return 0, fmt.Errorf("decode OKX ticker")
	}
	return parseFloat(tickers[0].Last), nil
}

func (o *OKXAdapter) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return o.wsManager.StartPriceStream(ctx, o.instID, callback)
}

func (o *OKXAdapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(candle interface{})) error {
	instIDs := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		instIDs = append(instIDs, convertToOKXSwapInstID(symbol))
	}
	return o.klineWSManager.Start(ctx, instIDs, interval, callback)
}

func (o *OKXAdapter) StopKlineStream() error {
	o.klineWSManager.Stop()
	return nil
}

func (o *OKXAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	query := url.Values{}
	query.Set("instId", convertToOKXSwapInstID(symbol))
	query.Set("bar", normalizeKlineBar(interval))
	query.Set("limit", strconv.Itoa(limit))
	data, err := o.client.DoRequest(ctx, "GET", "/api/v5/market/history-candles", query, nil)
	if err != nil {
		return nil, err
	}

	var raw [][]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode OKX candles: %w", err)
	}

	candles := make([]*Candle, 0, len(raw))
	for i := len(raw) - 1; i >= 0; i-- {
		candle, err := parseOKXCandle(symbol, raw[i])
		if err != nil {
			continue
		}
		candles = append(candles, candle)
	}
	return candles, nil
}

func (o *OKXAdapter) GetPriceDecimals() int {
	return o.priceDecimals
}

func (o *OKXAdapter) GetQuantityDecimals() int {
	return o.quantityDecimals
}

func (o *OKXAdapter) GetBaseAsset() string {
	return o.baseAsset
}

func (o *OKXAdapter) GetQuoteAsset() string {
	return o.quoteAsset
}
