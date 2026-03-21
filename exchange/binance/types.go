package binance

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
type Account = shared.Account
type OrderUpdate = shared.OrderUpdate
type OrderUpdateCallback = shared.OrderUpdateCallback
type Candle = shared.Candle
