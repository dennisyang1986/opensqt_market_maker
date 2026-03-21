package bitget

import shared "opensqt/internal/exchangetypes"

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
type OrderUpdateCallback = shared.OrderUpdateCallback
type Candle = shared.Candle

type Account struct {
	TotalWalletBalance float64
	TotalMarginBalance float64
	AvailableBalance   float64
	Positions          []*Position
	AccountLeverage    int
	PosMode            string
}
