package binance

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"opensqt/logger"
	"opensqt/utils"

	"github.com/adshao/go-binance/v2/futures"
)

// BinanceAdapter 币安交易所适配器
type BinanceAdapter struct {
	client           *futures.Client
	symbol           string
	wsManager        *WebSocketManager
	klineWSManager   *KlineWebSocketManager
	priceDecimals    int    // 价格精度（小数位数）
	quantityDecimals int    // 数量精度（小数位数）
	baseAsset        string // 基础资产（交易币种），如 BTC
	quoteAsset       string // 计价资产（结算币种），如 USDT、USD
}

// NewBinanceAdapter 创建币安适配器
func NewBinanceAdapter(cfg map[string]string, symbol string) (*BinanceAdapter, error) {
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("Binance API 配置不完整")
	}

	client := futures.NewClient(apiKey, secretKey)

	// 同步服务器时间
	client.NewSetServerTimeService().Do(context.Background())

	wsManager := NewWebSocketManager(apiKey, secretKey)

	adapter := &BinanceAdapter{
		client:    client,
		symbol:    symbol,
		wsManager: wsManager,
	}

	// 获取合约信息（价格精度、数量精度等）
	ctxInit, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := adapter.fetchExchangeInfo(ctxInit); err != nil {
		logger.Warn("⚠️ [Binance] 获取合约信息失败: %v，使用默认精度", err)
		// 使用默认值
		adapter.priceDecimals = 2
		adapter.quantityDecimals = 3
	}

	return adapter, nil
}

// GetName 获取交易所名称
func (b *BinanceAdapter) GetName() string {
	return "Binance"
}

// fetchExchangeInfo 获取合约信息（价格精度、数量精度等）
func (b *BinanceAdapter) fetchExchangeInfo(ctx context.Context) error {
	exchangeInfo, err := b.client.NewExchangeInfoService().Do(ctx)
	if err != nil {
		return fmt.Errorf("获取交易所信息失败: %w", err)
	}

	// 查找指定交易对的信息
	for _, symbol := range exchangeInfo.Symbols {
		if symbol.Symbol == b.symbol {
			b.priceDecimals = symbol.PricePrecision
			b.quantityDecimals = symbol.QuantityPrecision
			b.baseAsset = symbol.BaseAsset
			b.quoteAsset = symbol.QuoteAsset

			logger.Info("ℹ️ [Binance 合约信息] %s - 数量精度:%d, 价格精度:%d, 基础币种:%s, 计价币种:%s",
				b.symbol, b.quantityDecimals, b.priceDecimals, b.baseAsset, b.quoteAsset)
			return nil
		}
	}

	return fmt.Errorf("未找到合约信息: %s", b.symbol)
}

// PlaceOrder 下单
func (b *BinanceAdapter) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	priceStr := fmt.Sprintf("%.*f", req.PriceDecimals, req.Price)
	quantityStr := fmt.Sprintf("%.4f", req.Quantity)

	// 根据 PostOnly 参数选择 TimeInForce
	timeInForce := futures.TimeInForceTypeGTC
	if req.PostOnly {
		timeInForce = futures.TimeInForceTypeGTX // Post Only - 只做 Maker
	}

	orderService := b.client.NewCreateOrderService().
		Symbol(req.Symbol).
		Side(futures.SideType(req.Side)).
		Type(futures.OrderTypeLimit).
		TimeInForce(timeInForce).
		Quantity(quantityStr).
		Price(priceStr)

	// 设置自定义订单ID（添加返佣标识）
	clientOrderID := req.ClientOrderID
	if clientOrderID != "" {
		// 添加币安返佣前缀 x-zdfVM8vY（合约经纪商ID）
		clientOrderID = utils.AddBrokerPrefix("binance", clientOrderID)
		orderService = orderService.NewClientOrderID(clientOrderID)
	}

	// 币安单向持仓模式：如果是平仓单，需要设置 ReduceOnly
	// 注意：币安的 ReduceOnly 仅在单向持仓模式下有效
	if req.ReduceOnly {
		orderService = orderService.ReduceOnly(true)
	}

	resp, err := orderService.Do(ctx)

	if err != nil {
		return nil, err
	}

	return &Order{
		OrderID:       resp.OrderID,
		ClientOrderID: resp.ClientOrderID,
		Symbol:        req.Symbol,
		Side:          req.Side,
		Type:          req.Type,
		Price:         req.Price,
		Quantity:      req.Quantity,
		Status:        OrderStatus(resp.Status),
		CreatedAt:     time.Now(),
		UpdateTime:    resp.UpdateTime,
	}, nil
}

// BatchPlaceOrders 批量下单
func (b *BinanceAdapter) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	placedOrders := make([]*Order, 0, len(orders))
	hasMarginError := false

	for _, orderReq := range orders {
		order, err := b.PlaceOrder(ctx, orderReq)
		if err != nil {
			logger.Warn("⚠️ [Binance] 下单失败 %.2f %s: %v",
				orderReq.Price, orderReq.Side, err)

			if strings.Contains(err.Error(), "-2019") || strings.Contains(err.Error(), "insufficient") {
				hasMarginError = true
			}
			continue
		}
		placedOrders = append(placedOrders, order)
	}

	return placedOrders, hasMarginError
}

// CancelOrder 取消订单
func (b *BinanceAdapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	_, err := b.client.NewCancelOrderService().
		Symbol(symbol).
		OrderID(orderID).
		Do(ctx)

	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "-2011") || strings.Contains(errStr, "Unknown order") {
			logger.Info("ℹ️ [Binance] 订单 %d 已不存在，跳过取消", orderID)
			return nil
		}
		return err
	}

	logger.Info("✅ [Binance] 取消订单成功: %d", orderID)
	return nil
}

// BatchCancelOrders 批量撤单
func (b *BinanceAdapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	if len(orderIDs) == 0 {
		return nil
	}

	// 🔥 Binance 批量撤单限制：最多10个
	batchSize := 10
	for i := 0; i < len(orderIDs); i += batchSize {
		end := i + batchSize
		if end > len(orderIDs) {
			end = len(orderIDs)
		}

		batch := orderIDs[i:end]

		// 🔥 如果只有1个订单，直接用单个撤单接口
		if len(batch) == 1 {
			if err := b.CancelOrder(ctx, symbol, batch[0]); err != nil {
				logger.Warn("⚠️ [Binance] 取消订单失败 %d: %v", batch[0], err)
			}
			continue
		}

		_, err := b.client.NewCancelMultipleOrdersService().
			Symbol(symbol).
			OrderIDList(batch).
			Do(ctx)

		if err != nil {
			logger.Warn("⚠️ [Binance] 批量撤单失败 (共%d个): %v", len(batch), err)
			// 失败时尝试单个撤单
			logger.Info("🔄 [Binance] 改为逐个撤单...")
			for _, orderID := range batch {
				_ = b.CancelOrder(ctx, symbol, orderID)
				time.Sleep(100 * time.Millisecond) // 避免限频
			}
		} else {
			logger.Info("✅ [Binance] 批量撤单成功: %d 个订单", len(batch))
		}

		// 避免限频
		if i+batchSize < len(orderIDs) {
			time.Sleep(100 * time.Millisecond)
		}
	}

	return nil
}

// GetOrder 查询订单
func (b *BinanceAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	order, err := b.client.NewGetOrderService().
		Symbol(symbol).
		OrderID(orderID).
		Do(ctx)

	if err != nil {
		return nil, err
	}

	price, _ := strconv.ParseFloat(order.Price, 64)
	quantity, _ := strconv.ParseFloat(order.OrigQuantity, 64)
	executedQty, _ := strconv.ParseFloat(order.ExecutedQuantity, 64)
	avgPrice, _ := strconv.ParseFloat(order.AvgPrice, 64)

	return &Order{
		OrderID:       order.OrderID,
		ClientOrderID: order.ClientOrderID,
		Symbol:        order.Symbol,
		Side:          Side(order.Side),
		Type:          OrderType(order.Type),
		Price:         price,
		Quantity:      quantity,
		ExecutedQty:   executedQty,
		AvgPrice:      avgPrice,
		Status:        OrderStatus(order.Status),
		UpdateTime:    order.UpdateTime,
	}, nil
}

// GetOpenOrders 查询未完成订单
func (b *BinanceAdapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	orders, err := b.client.NewListOpenOrdersService().
		Symbol(symbol).
		Do(ctx)

	if err != nil {
		return nil, err
	}

	result := make([]*Order, 0, len(orders))
	for _, order := range orders {
		price, _ := strconv.ParseFloat(order.Price, 64)
		quantity, _ := strconv.ParseFloat(order.OrigQuantity, 64)
		executedQty, _ := strconv.ParseFloat(order.ExecutedQuantity, 64)
		avgPrice, _ := strconv.ParseFloat(order.AvgPrice, 64)

		result = append(result, &Order{
			OrderID:       order.OrderID,
			ClientOrderID: order.ClientOrderID,
			Symbol:        order.Symbol,
			Side:          Side(order.Side),
			Type:          OrderType(order.Type),
			Price:         price,
			Quantity:      quantity,
			ExecutedQty:   executedQty,
			AvgPrice:      avgPrice,
			Status:        OrderStatus(order.Status),
			UpdateTime:    order.UpdateTime,
		})
	}

	return result, nil
}

// GetAccount 获取账户信息（合约账户）
func (b *BinanceAdapter) GetAccount(ctx context.Context) (*Account, error) {
	// 🔥 修复：使用合约账户专用的 API
	account, err := b.client.NewGetAccountService().Do(ctx)
	if err != nil {
		// 将常见的英文错误转换为友好的中文提示
		errStr := err.Error()
		if strings.Contains(errStr, "Service unavailable from a restricted location") {
			return nil, fmt.Errorf("你的网络连接在限制服务区域，请检查网络或使用代理")
		}
		return nil, err
	}

	// 🔥 修复：从合约账户的 Assets 中获取 USDT 余额
	availableBalance := 0.0
	totalWalletBalance := 0.0
	totalMarginBalance := 0.0

	for _, asset := range account.Assets {
		if asset.Asset == "USDT" || asset.Asset == "USDC" || asset.Asset == "BUSD" {
			balance, _ := strconv.ParseFloat(asset.WalletBalance, 64)
			available, _ := strconv.ParseFloat(asset.AvailableBalance, 64)
			marginBalance, _ := strconv.ParseFloat(asset.MarginBalance, 64)

			totalWalletBalance += balance
			availableBalance += available
			totalMarginBalance += marginBalance
		}
	}

	positions := make([]*Position, 0, len(account.Positions))
	for _, pos := range account.Positions {
		posAmt, _ := strconv.ParseFloat(pos.PositionAmt, 64)
		if posAmt == 0 {
			continue
		}

		entryPrice, _ := strconv.ParseFloat(pos.EntryPrice, 64)
		unrealizedPNL, _ := strconv.ParseFloat(pos.UnrealizedProfit, 64)
		leverage, _ := strconv.Atoi(pos.Leverage)

		positions = append(positions, &Position{
			Symbol:         pos.Symbol,
			Size:           posAmt,
			EntryPrice:     entryPrice,
			MarkPrice:      0, // 币安 AccountPosition 没有 MarkPrice
			UnrealizedPNL:  unrealizedPNL,
			Leverage:       leverage,
			MarginType:     "", // 币安 AccountPosition 没有 MarginType
			IsolatedMargin: 0,  // 币安 AccountPosition 没有 IsolatedMargin
		})
	}

	return &Account{
		TotalWalletBalance: totalWalletBalance,
		TotalMarginBalance: totalMarginBalance,
		AvailableBalance:   availableBalance,
		Positions:          positions,
	}, nil
}

// GetPositions 获取持仓信息（使用PositionRisk API获取准确的杠杆倍数）
func (b *BinanceAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	// 🔥 使用 PositionRisk API，可以获取准确的杠杆信息
	positionRisks, err := b.client.NewGetPositionRiskService().Symbol(symbol).Do(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]*Position, 0)
	for _, pos := range positionRisks {
		posAmt, _ := strconv.ParseFloat(pos.PositionAmt, 64)
		entryPrice, _ := strconv.ParseFloat(pos.EntryPrice, 64)
		unrealizedPNL, _ := strconv.ParseFloat(pos.UnRealizedProfit, 64)
		markPrice, _ := strconv.ParseFloat(pos.MarkPrice, 64)
		isolatedMargin, _ := strconv.ParseFloat(pos.IsolatedMargin, 64)
		leverage, _ := strconv.Atoi(pos.Leverage)

		result = append(result, &Position{
			Symbol:         pos.Symbol,
			Size:           posAmt,
			EntryPrice:     entryPrice,
			MarkPrice:      markPrice,
			UnrealizedPNL:  unrealizedPNL,
			Leverage:       leverage,
			MarginType:     pos.MarginType,
			IsolatedMargin: isolatedMargin,
		})
	}

	return result, nil
}

// GetBalance 获取余额
func (b *BinanceAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	account, err := b.GetAccount(ctx)
	if err != nil {
		return 0, err
	}
	return account.AvailableBalance, nil
}

// StartOrderStream 启动订单流（WebSocket）
func (b *BinanceAdapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	// 转换回调函数：将 binance.OrderUpdate 转换为通用格式
	localCallback := func(update OrderUpdate) {
		// 构造通用的 OrderUpdate 结构（避免导入 exchange 包）
		genericUpdate := struct {
			OrderID       int64
			ClientOrderID string
			Symbol        string
			Side          string
			Type          string
			Status        string
			Price         float64
			Quantity      float64
			ExecutedQty   float64
			AvgPrice      float64
			UpdateTime    int64
		}{
			OrderID:       update.OrderID,
			ClientOrderID: update.ClientOrderID, // 🔥 关键：传递 ClientOrderID
			Symbol:        update.Symbol,
			Side:          string(update.Side),
			Type:          string(update.Type),
			Status:        string(update.Status),
			Price:         update.Price,
			Quantity:      update.Quantity,
			ExecutedQty:   update.ExecutedQty,
			AvgPrice:      update.AvgPrice,
			UpdateTime:    update.UpdateTime,
		}
		callback(genericUpdate)
	}
	return b.wsManager.Start(ctx, localCallback)
}

// StopOrderStream 停止订单流
func (b *BinanceAdapter) StopOrderStream() error {
	b.wsManager.Stop()
	return nil
}

// GetLatestPrice 获取最新价格（仅从 WebSocket 缓存读取）
// 架构说明：
// - 各组件不应直接调用此方法获取实时价格
// - 实时价格应该通过 PriceMonitor.GetLastPrice() 获取（订阅模式）
// - 此方法仅用于下单时的价格诊断（检查订单价格与市场价格的偏离）
// - WebSocket 是唯一的价格来源，不使用 REST API
// - 如果 WebSocket 未启动或断开，返回错误
func (b *BinanceAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	// 从 WebSocket 缓存读取价格
	if b.wsManager != nil {
		price := b.wsManager.GetLatestPrice()
		if price > 0 {
			return price, nil
		}
	}

	// WebSocket 未启动或无价格数据
	return 0, fmt.Errorf("WebSocket 价格流未就绪或无价格数据")
}

// StartPriceStream 启动价格流（WebSocket）
func (b *BinanceAdapter) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	// 启动价格流
	return b.wsManager.StartPriceStream(ctx, symbol, callback)
}

// StartKlineStream 启动K线流（WebSocket）
func (b *BinanceAdapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(candle interface{})) error {
	if b.klineWSManager == nil {
		b.klineWSManager = NewKlineWebSocketManager()
	}
	return b.klineWSManager.Start(ctx, symbols, interval, callback)
}

// StopKlineStream 停止K线流
func (b *BinanceAdapter) StopKlineStream() error {
	if b.klineWSManager != nil {
		b.klineWSManager.Stop()
	}
	return nil
}

// GetHistoricalKlines 获取历史K线数据
func (b *BinanceAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	klines, err := b.client.NewKlinesService().
		Symbol(symbol).
		Interval(interval).
		Limit(limit).
		Do(ctx)

	if err != nil {
		return nil, fmt.Errorf("获取历史K线失败: %w", err)
	}

	candles := make([]*Candle, 0, len(klines))
	for _, k := range klines {
		open, _ := strconv.ParseFloat(k.Open, 64)
		high, _ := strconv.ParseFloat(k.High, 64)
		low, _ := strconv.ParseFloat(k.Low, 64)
		close, _ := strconv.ParseFloat(k.Close, 64)
		volume, _ := strconv.ParseFloat(k.Volume, 64)

		candles = append(candles, &Candle{
			Symbol:    symbol,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
			Timestamp: k.OpenTime,
			IsClosed:  true, // 历史K线都是已完结的
		})
	}

	return candles, nil
}

// GetPriceDecimals 获取价格精度（小数位数）
func (b *BinanceAdapter) GetPriceDecimals() int {
	return b.priceDecimals
}

// GetQuantityDecimals 获取数量精度（小数位数）
func (b *BinanceAdapter) GetQuantityDecimals() int {
	return b.quantityDecimals
}

// GetBaseAsset 获取基础资产（交易币种）
func (b *BinanceAdapter) GetBaseAsset() string {
	return b.baseAsset
}

// GetQuoteAsset 获取计价资产（结算币种）
func (b *BinanceAdapter) GetQuoteAsset() string {
	return b.quoteAsset
}
