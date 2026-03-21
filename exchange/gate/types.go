package gate

import (
	shared "opensqt/internal/exchangetypes"
)

type Side = shared.Side

const (
	SideBuy  = shared.SideBuy
	SideSell = shared.SideSell
)

type OrderType = shared.OrderType

const (
	OrderTypeLimit = shared.OrderTypeLimit
)

type OrderStatus = shared.OrderStatus

const (
	OrderStatusNew             = shared.OrderStatusNew
	OrderStatusPartiallyFilled = shared.OrderStatusPartiallyFilled
	OrderStatusFilled          = shared.OrderStatusFilled
	OrderStatusCanceled        = shared.OrderStatusCanceled
	OrderStatusRejected        = shared.OrderStatusRejected
	OrderStatusExpired         = shared.OrderStatusExpired
)

type TimeInForce = shared.TimeInForce

const (
	TimeInForceGTC = shared.TimeInForceGTC
)

type OrderRequest = shared.OrderRequest
type Order = shared.Order
type Position = shared.Position
type OrderUpdate = shared.OrderUpdate
type Candle = shared.Candle

type Account struct {
	TotalWalletBalance float64
	TotalMarginBalance float64
	AvailableBalance   float64
	Positions          []*Position
	AccountLeverage    int
	PosMode            string
}

// ============ Gate.io API 娑撴挾鏁ょ紒鎾寸€担?============

type GateResponse struct {
	Label   string `json:"label,omitempty"`
	Message string `json:"message,omitempty"`
}

type ContractInfo struct {
	Name              string  `json:"name"`
	Type              string  `json:"type"`
	QuantoMultiplier  string  `json:"quanto_multiplier"`
	OrderPriceRound   string  `json:"order_price_round"`
	OrderSizeMin      float64 `json:"order_size_min"`
	OrderSizeMax      float64 `json:"order_size_max"`
	OrderSizeRound    string  `json:"order_size_round"`
	OrderPriceDeviate string  `json:"order_price_deviate"`
	RefDiscountRate   string  `json:"ref_discount_rate"`
	OrderbookID       int64   `json:"orderbook_id"`
	TradeSize         float64 `json:"trade_size"`
	MarkPriceRound    string  `json:"mark_price_round"`
}

type FuturesAccount struct {
	User                  int64  `json:"user"`
	Currency              string `json:"currency"`
	Total                 string `json:"total"`
	UnrealisedPnl         string `json:"unrealised_pnl"`
	PositionMargin        string `json:"position_margin"`
	OrderMargin           string `json:"order_margin"`
	Available             string `json:"available"`
	Point                 string `json:"point"`
	Bonus                 string `json:"bonus"`
	InDualMode            bool   `json:"in_dual_mode"`
	EnableCredit          bool   `json:"enable_credit"`
	PositionInitialMargin string `json:"position_initial_margin"`
	MaintenanceMargin     string `json:"maintenance_margin"`
}

type FuturesPosition struct {
	User            int64  `json:"user"`
	Contract        string `json:"contract"`
	Size            int64  `json:"size"`
	Leverage        string `json:"leverage"`
	RiskLimit       string `json:"risk_limit"`
	LeverageMax     string `json:"leverage_max"`
	MaintenanceRate string `json:"maintenance_rate"`
	Value           string `json:"value"`
	Margin          string `json:"margin"`
	EntryPrice      string `json:"entry_price"`
	LiqPrice        string `json:"liq_price"`
	MarkPrice       string `json:"mark_price"`
	UnrealisedPnl   string `json:"unrealised_pnl"`
	RealisedPnl     string `json:"realised_pnl"`
	HistoryPnl      string `json:"history_pnl"`
	LastClosePnl    string `json:"last_close_pnl"`
	RealisedPoint   string `json:"realised_point"`
	HistoryPoint    string `json:"history_point"`
	AdlRanking      int    `json:"adl_ranking"`
	PendingOrders   int    `json:"pending_orders"`
	CloseOrder      *struct {
		ID    int64  `json:"id"`
		Price string `json:"price"`
		IsLiq bool   `json:"is_liq"`
	} `json:"close_order"`
	Mode               string `json:"mode"`
	CrossLeverageLimit string `json:"cross_leverage_limit"`
}

type FuturesOrder struct {
	ID            int64   `json:"id"`
	User          int64   `json:"user"`
	Contract      string  `json:"contract"`
	CreateTime    float64 `json:"create_time"`
	FinishTime    float64 `json:"finish_time"`
	FinishAs      string  `json:"finish_as"`
	Status        string  `json:"status"`
	Size          int64   `json:"size"`
	Price         string  `json:"price"`
	FillPrice     string  `json:"fill_price"`
	Left          int64   `json:"left"`
	Text          string  `json:"text"`
	Tif           string  `json:"tif"`
	IsLiq         bool    `json:"is_liq"`
	IsClose       bool    `json:"is_close"`
	IsReduceOnly  bool    `json:"is_reduce_only"`
	IsPostOnly    bool    `json:"is_post_only"`
	Iceberg       int64   `json:"iceberg"`
	AutoSize      string  `json:"auto_size"`
	RefundedFee   string  `json:"refunded_fee"`
	Fee           string  `json:"fee"`
	FillSize      int64   `json:"fill_size"`
	RealisedPnl   string  `json:"realised_pnl"`
	RealisedPoint string  `json:"realised_point"`
}

type WSRequest struct {
	Time    int64                  `json:"time"`
	Channel string                 `json:"channel"`
	Event   string                 `json:"event"`
	Payload map[string]interface{} `json:"payload,omitempty"`
}

type WSOrderPayload struct {
	ReqHeader map[string]string      `json:"req_header"`
	ReqID     string                 `json:"req_id"`
	ReqParam  map[string]interface{} `json:"req_param"`
}

type WSResponse struct {
	Time    int64       `json:"time"`
	TimeMs  int64       `json:"time_ms"`
	Channel string      `json:"channel"`
	Event   string      `json:"event"`
	Result  interface{} `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}
