package okx

import shared "opensqt/internal/exchangetypes"

type Side = shared.Side

const (
	SideBuy  = shared.SideBuy
	SideSell = shared.SideSell
)

type OrderType = shared.OrderType

const (
	OrderTypeLimit  = shared.OrderTypeLimit
	OrderTypeMarket = shared.OrderTypeMarket
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
	TimeInForceGTX = shared.TimeInForceGTX
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
