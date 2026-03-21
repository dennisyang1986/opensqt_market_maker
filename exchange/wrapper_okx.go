package exchange

import (
	"context"
	"opensqt/exchange/okx"
)

type okxWrapper struct {
	adapter *okx.OKXAdapter
}

func (w *okxWrapper) GetName() string { return w.adapter.GetName() }

func (w *okxWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	okxReq := &okx.OrderRequest{
		Symbol:        req.Symbol,
		Side:          okx.Side(req.Side),
		Type:          okx.OrderType(req.Type),
		TimeInForce:   okx.TimeInForce(req.TimeInForce),
		Quantity:      req.Quantity,
		Price:         req.Price,
		ReduceOnly:    req.ReduceOnly,
		PostOnly:      req.PostOnly,
		PriceDecimals: req.PriceDecimals,
		ClientOrderID: req.ClientOrderID,
	}
	okxOrder, err := w.adapter.PlaceOrder(ctx, okxReq)
	if err != nil {
		return nil, err
	}
	return toExchangeOrder(okxOrder), nil
}

func (w *okxWrapper) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	okxOrders := make([]*okx.OrderRequest, len(orders))
	for i, req := range orders {
		okxOrders[i] = &okx.OrderRequest{
			Symbol:        req.Symbol,
			Side:          okx.Side(req.Side),
			Type:          okx.OrderType(req.Type),
			TimeInForce:   okx.TimeInForce(req.TimeInForce),
			Quantity:      req.Quantity,
			Price:         req.Price,
			ReduceOnly:    req.ReduceOnly,
			PostOnly:      req.PostOnly,
			PriceDecimals: req.PriceDecimals,
			ClientOrderID: req.ClientOrderID,
		}
	}
	okxResult, hasMarginError := w.adapter.BatchPlaceOrders(ctx, okxOrders)
	result := make([]*Order, len(okxResult))
	for i, ord := range okxResult {
		result[i] = toExchangeOrder(ord)
	}
	return result, hasMarginError
}

func (w *okxWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return w.adapter.CancelOrder(ctx, symbol, orderID)
}

func (w *okxWrapper) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	return w.adapter.BatchCancelOrders(ctx, symbol, orderIDs)
}

func (w *okxWrapper) CancelAllOrders(ctx context.Context, symbol string) error {
	return w.adapter.CancelAllOrders(ctx, symbol)
}

func (w *okxWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	okxOrder, err := w.adapter.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}
	return toExchangeOrder(okxOrder), nil
}

func (w *okxWrapper) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	okxOrders, err := w.adapter.GetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, err
	}
	orders := make([]*Order, len(okxOrders))
	for i, ord := range okxOrders {
		orders[i] = toExchangeOrder(ord)
	}
	return orders, nil
}

func (w *okxWrapper) GetAccount(ctx context.Context) (*Account, error) {
	okxAccount, err := w.adapter.GetAccount(ctx)
	if err != nil {
		return nil, err
	}
	positions := make([]*Position, len(okxAccount.Positions))
	for i, pos := range okxAccount.Positions {
		positions[i] = &Position{
			Symbol:         pos.Symbol,
			Size:           pos.Size,
			EntryPrice:     pos.EntryPrice,
			MarkPrice:      pos.MarkPrice,
			UnrealizedPNL:  pos.UnrealizedPNL,
			Leverage:       pos.Leverage,
			MarginType:     pos.MarginType,
			IsolatedMargin: pos.IsolatedMargin,
		}
	}
	return &Account{
		TotalWalletBalance: okxAccount.TotalWalletBalance,
		TotalMarginBalance: okxAccount.TotalMarginBalance,
		AvailableBalance:   okxAccount.AvailableBalance,
		Positions:          positions,
		AccountLeverage:    okxAccount.AccountLeverage,
	}, nil
}

func (w *okxWrapper) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	okxPositions, err := w.adapter.GetPositions(ctx, symbol)
	if err != nil {
		return nil, err
	}
	positions := make([]*Position, len(okxPositions))
	for i, pos := range okxPositions {
		positions[i] = &Position{
			Symbol:         pos.Symbol,
			Size:           pos.Size,
			EntryPrice:     pos.EntryPrice,
			MarkPrice:      pos.MarkPrice,
			UnrealizedPNL:  pos.UnrealizedPNL,
			Leverage:       pos.Leverage,
			MarginType:     pos.MarginType,
			IsolatedMargin: pos.IsolatedMargin,
		}
	}
	return positions, nil
}

func (w *okxWrapper) GetBalance(ctx context.Context, asset string) (float64, error) {
	return w.adapter.GetBalance(ctx, asset)
}

func (w *okxWrapper) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return w.adapter.StartOrderStream(ctx, callback)
}

func (w *okxWrapper) StopOrderStream() error { return w.adapter.StopOrderStream() }

func (w *okxWrapper) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetLatestPrice(ctx, symbol)
}

func (w *okxWrapper) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return w.adapter.StartPriceStream(ctx, symbol, callback)
}

func (w *okxWrapper) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	return w.adapter.StartKlineStream(ctx, symbols, interval, func(candle interface{}) {
		if c, ok := candle.(*okx.Candle); ok {
			callback(&Candle{
				Symbol:    c.Symbol,
				Open:      c.Open,
				High:      c.High,
				Low:       c.Low,
				Close:     c.Close,
				Volume:    c.Volume,
				Timestamp: c.Timestamp,
				IsClosed:  c.IsClosed,
			})
		}
	})
}

func (w *okxWrapper) StopKlineStream() error { return w.adapter.StopKlineStream() }

func (w *okxWrapper) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	candles, err := w.adapter.GetHistoricalKlines(ctx, symbol, interval, limit)
	if err != nil {
		return nil, err
	}
	result := make([]*Candle, len(candles))
	for i, c := range candles {
		result[i] = &Candle{
			Symbol:    c.Symbol,
			Open:      c.Open,
			High:      c.High,
			Low:       c.Low,
			Close:     c.Close,
			Volume:    c.Volume,
			Timestamp: c.Timestamp,
			IsClosed:  c.IsClosed,
		}
	}
	return result, nil
}

func (w *okxWrapper) GetPriceDecimals() int    { return w.adapter.GetPriceDecimals() }
func (w *okxWrapper) GetQuantityDecimals() int { return w.adapter.GetQuantityDecimals() }
func (w *okxWrapper) GetBaseAsset() string     { return w.adapter.GetBaseAsset() }
func (w *okxWrapper) GetQuoteAsset() string    { return w.adapter.GetQuoteAsset() }

func toExchangeOrder(ord *okx.Order) *Order {
	return &Order{
		OrderID:       ord.OrderID,
		ClientOrderID: ord.ClientOrderID,
		Symbol:        ord.Symbol,
		Side:          Side(ord.Side),
		Type:          OrderType(ord.Type),
		Price:         ord.Price,
		Quantity:      ord.Quantity,
		ExecutedQty:   ord.ExecutedQty,
		AvgPrice:      ord.AvgPrice,
		Status:        OrderStatus(ord.Status),
		CreatedAt:     ord.CreatedAt,
		UpdateTime:    ord.UpdateTime,
	}
}
