package okx

import "time"

// Side 表示订单方向（买入或卖出）
type Side string

// OrderType 表示订单类型（限价单或市价单）
type OrderType string

// OrderStatus 表示订单状态
type OrderStatus string

// TimeInForce 表示订单有效期策略
type TimeInForce string

const (
	// SideBuy 买入方向
	SideBuy Side = "BUY"
	// SideSell 卖出方向
	SideSell Side = "SELL"
)

const (
	// OrderTypeLimit 限价订单，指定价格执行
	OrderTypeLimit OrderType = "LIMIT"
	// OrderTypeMarket 市价订单，按市场最优价格立即执行
	OrderTypeMarket OrderType = "MARKET"
)

const (
	// OrderStatusNew 新订单，已提交但尚未成交
	OrderStatusNew OrderStatus = "NEW"
	// OrderStatusPartiallyFilled 部分成交，订单已部分执行
	OrderStatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	// OrderStatusFilled 完全成交，订单已全部执行
	OrderStatusFilled OrderStatus = "FILLED"
	// OrderStatusCanceled 已取消，订单被用户主动取消
	OrderStatusCanceled OrderStatus = "CANCELED"
	// OrderStatusRejected 已拒绝，订单被交易所拒绝
	OrderStatusRejected OrderStatus = "REJECTED"
	// OrderStatusExpired 已过期，订单超过有效期自动失效
	OrderStatusExpired OrderStatus = "EXPIRED"
)

const (
	// TimeInForceGTC Good Till Cancel，订单一直有效直到被取消或完全成交
	TimeInForceGTC TimeInForce = "GTC"
	// TimeInForceGTX Good Till Cross，订单作为 Maker 挂单，若会立即成交则取消
	TimeInForceGTX TimeInForce = "GTX"
)

// OrderRequest 下单请求参数
type OrderRequest struct {
	// Symbol 交易对符号，如 "BTC-USDT"
	Symbol string
	// Side 订单方向（买入/卖出）
	Side Side
	// Type 订单类型（限价/市价）
	Type OrderType
	// TimeInForce 订单有效期策略
	TimeInForce TimeInForce
	// Quantity 下单数量（基础货币）
	Quantity float64
	// Price 订单价格（报价货币），市价单时可为 0
	Price float64
	// ReduceOnly 仅减仓模式，订单只能减少现有持仓而不能开新仓
	ReduceOnly bool
	// PostOnly 被动委托，订单只能作为 Maker 挂入深度，若会立即成交则取消
	PostOnly bool
	// PriceDecimals 价格精度，用于四舍五入到交易所允许的小数位数
	PriceDecimals int
	// ClientOrderID 客户端自定义订单 ID，用于幂等性控制和订单追踪
	ClientOrderID string
}

// Order 订单结构，表示一个完整的订单信息
type Order struct {
	// OrderID 交易所返回的订单 ID
	OrderID int64
	// ClientOrderID 客户端自定义订单 ID
	ClientOrderID string
	// Symbol 交易对符号
	Symbol string
	// Side 订单方向
	Side Side
	// Type 订单类型
	Type OrderType
	// Price 订单价格
	Price float64
	// Quantity 订单数量
	Quantity float64
	// ExecutedQty 已成交数量
	ExecutedQty float64
	// AvgPrice 平均成交价格
	AvgPrice float64
	// Status 订单状态
	Status OrderStatus
	// CreatedAt 订单创建时间
	CreatedAt time.Time
	// UpdateTime 订单最后更新时间戳（毫秒）
	UpdateTime int64
}

// Position 持仓信息结构
type Position struct {
	// Symbol 交易对符号
	Symbol string
	// Size 持仓数量，正数表示多头，负数表示空头
	Size float64
	// EntryPrice 开仓均价
	EntryPrice float64
	// MarkPrice 标记价格，用于计算未实现盈亏
	MarkPrice float64
	// UnrealizedPNL 未实现盈亏
	UnrealizedPNL float64
	// Leverage 杠杆倍数
	Leverage int
	// MarginType 保证金模式，如 "cross"（全仓）或 "isolated"（逐仓）
	MarginType string
	// IsolatedMargin 逐仓保证金余额（逐仓模式下使用）
	IsolatedMargin float64
}

// Account 账户信息结构
type Account struct {
	// TotalWalletBalance 账户总权益（未扣除持仓占用保证金）
	TotalWalletBalance float64
	// TotalMarginBalance 总保证金余额（可用于开仓的总资金）
	TotalMarginBalance float64
	// AvailableBalance 可用余额（扣除持仓占用后可用于下单的资金）
	AvailableBalance float64
	// Positions 当前所有持仓
	Positions []*Position
	// AccountLeverage 账户默认杠杆倍数
	AccountLeverage int
	// PosMode 持仓模式，如 "net_mode"（单向持仓）或 "hedge_mode"（双向持仓）
	PosMode string
}

// OrderUpdate 订单更新推送结构，用于 WebSocket 订单状态变更通知
type OrderUpdate struct {
	// OrderID 交易所订单 ID
	OrderID int64
	// ClientOrderID 客户端订单 ID
	ClientOrderID string
	// Symbol 交易对符号
	Symbol string
	// Side 订单方向
	Side Side
	// Type 订单类型
	Type OrderType
	// Status 订单状态
	Status OrderStatus
	// Price 订单价格
	Price float64
	// Quantity 订单数量
	Quantity float64
	// ExecutedQty 已成交数量
	ExecutedQty float64
	// AvgPrice 平均成交价格
	AvgPrice float64
	// UpdateTime 更新时间戳（毫秒）
	UpdateTime int64
}

// Candle K 线数据结构
type Candle struct {
	// Symbol 交易对符号
	Symbol string
	// Open 开盘价
	Open float64
	// High 最高价
	High float64
	// Low 最低价
	Low float64
	// Close 收盘价
	Close float64
	// Volume 成交量（基础货币）
	Volume float64
	// Timestamp K 线开始时间戳（毫秒）
	Timestamp int64
	// IsClosed K 线是否已闭合（闭合后数据不再变化）
	IsClosed bool
}
