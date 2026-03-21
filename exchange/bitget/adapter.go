package bitget

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"opensqt/logger"
)

// BitgetAdapter Bitget 交易所适配器
type BitgetAdapter struct {
	client         *Client
	wsManager      *WebSocketManager
	klineWSManager *KlineWebSocketManager
	symbol         string // 交易对（如 ETHUSDT，V2 API 不带 _UMCBL 后缀）
	useWebSocket   bool   // 是否使用 WebSocket 下单

	// 🔥 新增：订单ID到价格的映射注册回调
	// 用于在下单成功后立即建立映射，避免 WebSocket 更新先到导致找不到槽位
	orderMappingCallback func(orderID int64, price float64)

	posMode      string // 持仓模式：hedge_mode 或 one_way_mode
	productType  string // 合约类型：usdt-futures（U本位）或 coin-futures（币本位）
	marginCoin   string // 保证金币种：自动从合约信息获取
	volumePlace  int    // 数量小数位（从合约信息获取）
	pricePlace   int    // 价格小数位（从合约信息获取）
	minTradeNum  string // 最小下单数量
	minTradeUSDT string // 最小下单金额（USDT）
	baseAsset    string // 基础资产（交易币种），如 BTC
	quoteAsset   string // 计价资产（结算币种），如 USDT、USD
}

// NewBitgetAdapter 创建 Bitget 适配器
func NewBitgetAdapter(cfg map[string]string, symbol string) (*BitgetAdapter, error) {
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]
	passphrase := cfg["passphrase"]

	if apiKey == "" || secretKey == "" || passphrase == "" {
		return nil, fmt.Errorf("bitget API 配置不完整")
	}

	// Bitget V2 合约符号格式：直接使用 ETHUSDT（不带 _UMCBL 后缀）
	bitgetSymbol := convertToBitgetSymbol(symbol)

	client := NewClient(apiKey, secretKey, passphrase)
	wsManager := NewWebSocketManager(apiKey, secretKey, passphrase)

	adapter := &BitgetAdapter{
		client:       client,
		wsManager:    wsManager,
		symbol:       bitgetSymbol,
		useWebSocket: false, // 使用 REST API 下单（混合模式）
	}

	// 初始化获取合约信息和持仓模式
	ctxInit, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. 先获取合约信息（必须先获取，因为需要设置productType和marginCoin）
	if err := adapter.fetchContractInfo(ctxInit); err != nil {
		logger.Warn("⚠️ [Bitget] 获取合约信息失败: %v", err)
		// 使用默认值
		adapter.volumePlace = 4
		adapter.pricePlace = 2
		adapter.productType = "usdt-futures"
		adapter.marginCoin = "USDT"
	}

	// 2. 获取持仓模式和账户信息
	acc, err := adapter.GetAccount(ctxInit)
	if err != nil {
		logger.Warn("⚠️ [Bitget] 初始化获取账户信息失败: %v", err)
		adapter.posMode = "hedge_mode" // 默认双向持仓
	} else {
		adapter.posMode = acc.PosMode
		// 显示持仓模式（双向/单向）
		posModeDesc := "双向持仓"
		if acc.PosMode == "one_way_mode" {
			posModeDesc = "单向持仓"
		}
		logger.Info("ℹ️ [Bitget] 持仓模式: %s (%s)", posModeDesc, acc.PosMode)
	}

	// 移除这里的自动连接，统一由 StartPriceStream 或 StartOrderStream 触发
	// 这样可以避免重复连接和日志重复
	/*
		ctx := context.Background()
		go func() {
			logger.Info("🔗 [Bitget] 正在连接 WebSocket...")
			if err := wsManager.ConnectAndLogin(ctx, bitgetSymbol); err != nil {
				logger.Warn("⚠️ [Bitget] WebSocket 连接失败: %v（不影响交易）", err)
			} else {
				logger.Info("✅ [Bitget] WebSocket 已连接并登录")
			}
		}()
	*/

	return adapter, nil
}

// GetName 获取交易所名称
func (b *BitgetAdapter) GetName() string {
	return "Bitget"
}

// fetchContractInfo 获取合约信息（数量精度、价格精度等）
func (b *BitgetAdapter) fetchContractInfo(ctx context.Context) error {
	// 尝试从多个合约类型中查找（先U本位，再币本位）
	productTypes := []string{"usdt-futures", "coin-futures", "usdc-futures"}
	var lastErr error

	for _, pt := range productTypes {
		path := fmt.Sprintf("/api/v2/mix/market/contracts?productType=%s&symbol=%s", pt, b.symbol)
		resp, err := b.client.DoRequest(ctx, "GET", path, nil)
		if err != nil {
			lastErr = err
			continue
		}

		// 解析合约信息
		var dataList []struct {
			Symbol             string   `json:"symbol"`
			VolumePlace        string   `json:"volumePlace"`        // 数量小数位
			PricePlace         string   `json:"pricePlace"`         // 价格小数位
			MinTradeNum        string   `json:"minTradeNum"`        // 最小下单数量
			MinTradeUSDT       string   `json:"minTradeUSDT"`       // 最小下单金额
			BaseCoin           string   `json:"baseCoin"`           // 基础币种
			QuoteCoin          string   `json:"quoteCoin"`          // 计价币种
			SupportMarginCoins []string `json:"supportMarginCoins"` // 支持的保证金币种
		}

		if err := json.Unmarshal(resp.Data, &dataList); err != nil {
			lastErr = fmt.Errorf("解析合约信息失败: %w", err)
			continue
		}

		if len(dataList) == 0 {
			continue // 尝试下一个productType
		}

		// 找到合约信息
		contract := dataList[0]
		b.productType = pt
		b.volumePlace, _ = strconv.Atoi(contract.VolumePlace)
		b.pricePlace, _ = strconv.Atoi(contract.PricePlace)
		b.minTradeNum = contract.MinTradeNum
		b.minTradeUSDT = contract.MinTradeUSDT
		b.baseAsset = contract.BaseCoin
		b.quoteAsset = contract.QuoteCoin

		// 设置保证金币种（优先使用supportMarginCoins的第一个，否则使用quoteCoin）
		if len(contract.SupportMarginCoins) > 0 {
			b.marginCoin = contract.SupportMarginCoins[0]
		} else {
			b.marginCoin = contract.QuoteCoin
		}

		// 判断合约类型描述
		contractTypeDesc := "U本位合约"
		if pt == "coin-futures" {
			contractTypeDesc = "币本位合约"
		} else if pt == "usdc-futures" {
			contractTypeDesc = "USDC合约"
		}

		logger.Info("ℹ️ [Bitget 合约信息] %s - %s, 数量精度:%d, 价格精度:%d, 基础币种:%s, 计价币种:%s, 保证金:%s",
			b.symbol, contractTypeDesc, b.volumePlace, b.pricePlace, b.baseAsset, b.quoteAsset, b.marginCoin)

		return nil
	}

	if lastErr != nil {
		return fmt.Errorf("未找到合约信息 %s: %w", b.symbol, lastErr)
	}
	return fmt.Errorf("未找到合约信息: %s", b.symbol)
}

// PlaceOrder 下单（使用 REST API）
func (b *BitgetAdapter) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	// 混合模式：使用 REST API 下单，更稳定可靠
	return b.placeOrderViaREST(ctx, req)
}

// placeOrderViaREST 通过 REST API 下单
func (b *BitgetAdapter) placeOrderViaREST(ctx context.Context, req *OrderRequest) (*Order, error) {
	// 确定 side 和 tradeSide
	side := strings.ToLower(string(req.Side))
	var tradeSide string

	// 🔥 Bitget 双向持仓的特殊逻辑：
	// 开多：side=buy, tradeSide=open
	// 平多：side=buy, tradeSide=close （注意！平多也是 buy）
	// 开空：side=sell, tradeSide=open
	// 平空：side=sell, tradeSide=close
	if b.posMode == "hedge_mode" {
		if req.ReduceOnly {
			// 平仓：保持 side 方向不变，只改 tradeSide
			// 如果是 SELL（卖出），实际上是要平多仓，需要改为 buy
			if req.Side == SideSell {
				side = "buy" // 平多仓必须用 buy
			} else {
				side = "sell" // 平空仓必须用 sell
			}
			tradeSide = "close"
		} else {
			tradeSide = "open"
		}
	}

	// 🔥 使用合约信息中的精度格式化数量和价格
	quantityStr := fmt.Sprintf("%.*f", b.volumePlace, req.Quantity)
	priceStr := fmt.Sprintf("%.*f", b.pricePlace, req.Price)

	// 根据 PostOnly 参数选择 force 类型
	forceType := "gtc" // 默认使用 GTC (Good Till Cancel)
	if req.PostOnly {
		forceType = "post_only" // Post Only - 只做 Maker
	}

	// Bitget V2 下单参数
	body := map[string]interface{}{
		"symbol":      req.Symbol,
		"productType": b.productType,
		"marginMode":  "crossed",
		"marginCoin":  "USDT",
		"side":        side,
		"orderType":   "limit",
		"price":       priceStr,
		"size":        quantityStr,
		"force":       forceType,
	}

	// 设置自定义订单ID
	if req.ClientOrderID != "" {
		body["clientOid"] = req.ClientOrderID
	}

	// 双向持仓模式下添加 tradeSide（必须）
	// 🔥 关键：双向持仓模式下，不能使用 reduceOnly 参数，只能用 tradeSide=close
	if tradeSide != "" {
		body["tradeSide"] = tradeSide
	}

	// 🔥 单向持仓模式下，如果是只减仓，必须使用 reduceOnly 参数
	// 注意：单向持仓时 tradeSide 参数必须省略，否则会报错
	if b.posMode != "hedge_mode" && req.ReduceOnly {
		body["reduceOnly"] = "YES"
	}

	// 只请求1次，不重试
	resp, err := b.client.DoRequest(ctx, "POST", "/api/v2/mix/order/place-order", body)
	if err != nil {
		// 检查错误类型
		if strings.Contains(err.Error(), "insufficient balance") || strings.Contains(err.Error(), "40007") {
			return nil, fmt.Errorf("保证金不足: %w", err)
		}
		return nil, err
	}

	// 解析响应
	var data struct {
		OrderID       string `json:"orderId"`
		ClientOrderID string `json:"clientOid"`
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("解析下单响应失败: %w", err)
	}

	// 🔍 添加调试：打印完整响应
	logger.Debug("🔍 [Bitget REST] 下单响应: %s", string(resp.Data))

	orderID, _ := strconv.ParseInt(data.OrderID, 10, 64)
	if orderID == 0 {
		return nil, fmt.Errorf("下单响应中orderId为空或无效: %s", string(resp.Data))
	}

	order := &Order{
		OrderID:       orderID,
		ClientOrderID: data.ClientOrderID,
		Symbol:        req.Symbol,
		Side:          req.Side,
		Type:          req.Type,
		Price:         req.Price,
		Quantity:      req.Quantity,
		Status:        OrderStatusNew,
		CreatedAt:     time.Now(),
	}

	// 🔥 诊断：获取当前市场价格，检查订单价格是否合理
	ctxPrice, cancelPrice := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelPrice()
	currentPrice, err := b.GetLatestPrice(ctxPrice, b.symbol)
	if err == nil {
		priceDiff := req.Price - currentPrice
		priceDiffPercent := (priceDiff / currentPrice) * 100
		logger.Debug("🔍 [Bitget下单诊断] 订单价格: %.2f, 当前价格: %.2f, 价差: %.2f (%.3f%%)",
			req.Price, currentPrice, priceDiff, priceDiffPercent)
	}

	// 注意：不在这里打印日志，由executor统一打印避免重复
	return order, nil
}

// BatchPlaceOrders 批量下单
func (b *BitgetAdapter) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	placedOrders := make([]*Order, 0, len(orders))
	hasMarginError := false

	for _, orderReq := range orders {
		order, err := b.PlaceOrder(ctx, orderReq)
		if err != nil {
			logger.Warn("⚠️ [Bitget] 下单失败 %.2f %s: %v",
				orderReq.Price, orderReq.Side, err)

			if strings.Contains(err.Error(), "保证金不足") {
				hasMarginError = true
			}
			continue
		}

		// 🔥 关键：确保 order.Price 包含请求的价格
		// 这样调用者就能正确建立 orderID -> price 的映射
		order.Price = orderReq.Price

		// 🔥 新增：立即注册订单ID到价格的映射
		// 这样可以防止 WebSocket 更新先到导致找不到槽位
		if b.orderMappingCallback != nil && order.OrderID > 0 {
			b.orderMappingCallback(order.OrderID, orderReq.Price)
			logger.Debug("🔍 [Bitget映射] 注册 订单ID=%d -> 价格=%.2f", order.OrderID, orderReq.Price)
		}

		placedOrders = append(placedOrders, order)
	}

	return placedOrders, hasMarginError
}

// CancelOrder 取消订单
func (b *BitgetAdapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	body := map[string]interface{}{
		"symbol":      b.symbol,
		"productType": b.productType,
		"marginCoin":  b.marginCoin,
		"orderId":     fmt.Sprintf("%d", orderID),
	}

	_, err := b.client.DoRequest(ctx, "POST", "/api/v2/mix/order/cancel-order", body)
	if err != nil {
		// 订单不存在不算错误
		if strings.Contains(err.Error(), "order does not exist") || strings.Contains(err.Error(), "40029") {
			logger.Info("ℹ️ [Bitget] 订单 %d 已不存在，跳过取消", orderID)
			return nil
		}
		return fmt.Errorf("取消订单失败: %w", err)
	}

	logger.Info("✅ [Bitget] 取消订单成功: %d", orderID)
	return nil
}

// BatchCancelOrders 批量取消订单
func (b *BitgetAdapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	if len(orderIDs) == 0 {
		return nil
	}

	// 🔥 Bitget 批量撤单限制：最多20个，必须传symbol、productType、marginCoin
	batchSize := 20
	for i := 0; i < len(orderIDs); i += batchSize {
		end := i + batchSize
		if end > len(orderIDs) {
			end = len(orderIDs)
		}

		batch := orderIDs[i:end]

		// 🔥 如果只有1个订单，直接用单个撤单接口
		if len(batch) == 1 {
			if err := b.CancelOrder(ctx, symbol, batch[0]); err != nil {
				logger.Warn("⚠️ [Bitget] 取消订单失败 %d: %v", batch[0], err)
			}
			continue
		}

		// 构造订单ID字符串列表
		orderIDStrs := make([]string, len(batch))
		for j, id := range batch {
			orderIDStrs[j] = fmt.Sprintf("%d", id)
		}

		// 🔥 确保所有必需参数都存在
		body := map[string]interface{}{
			"symbol":      b.symbol,      // 必需
			"productType": b.productType, // 必需：USDT-FUTURES
			"marginCoin":  b.marginCoin,  // 必需：USDT
			"orderIdList": orderIDStrs,   // 必需：订单ID列表
		}

		_, err := b.client.DoRequest(ctx, "POST", "/api/v2/mix/order/batch-cancel-orders", body)
		if err != nil {
			logger.Warn("⚠️ [Bitget] 批量撤单失败 (共%d个): %v", len(batch), err)
			// 失败时尝试单个撤单
			logger.Info("🔄 [Bitget] 改为逐个撤单...")
			for _, orderID := range batch {
				_ = b.CancelOrder(ctx, symbol, orderID)
				time.Sleep(100 * time.Millisecond) // 避免限频
			}
		} else {
			logger.Info("✅ [Bitget] 批量撤单成功: %d 个订单", len(batch))
		}

		// 避免限频
		if i+batchSize < len(orderIDs) {
			time.Sleep(100 * time.Millisecond)
		}
	}

	return nil
}

// CancelAllOrders 一键全撤所有订单（Bitget特有功能）
func (b *BitgetAdapter) CancelAllOrders(ctx context.Context) error {
	body := map[string]interface{}{
		"productType": b.productType, // 必需：USDT-FUTURES
		"marginCoin":  b.marginCoin,  // 必需：USDT
	}

	resp, err := b.client.DoRequest(ctx, "POST", "/api/v2/mix/order/cancel-all-orders", body)
	if err != nil {
		return fmt.Errorf("一键全撤失败: %w", err)
	}

	// 解析响应
	var data struct {
		SuccessList []struct {
			OrderID   string `json:"orderId"`
			ClientOid string `json:"clientOid"`
		} `json:"successList"`
		FailureList []struct {
			OrderID   string `json:"orderId"`
			ClientOid string `json:"clientOid"`
			ErrorMsg  string `json:"errorMsg"`
		} `json:"failureList"`
	}

	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return fmt.Errorf("解析一键全撤响应失败: %w", err)
	}

	logger.Info("✅ [Bitget 一键全撤] 成功: %d 个, 失败: %d 个",
		len(data.SuccessList), len(data.FailureList))

	if len(data.FailureList) > 0 {
		for _, fail := range data.FailureList {
			logger.Warn("⚠️ [Bitget 一键全撤失败] 订单ID: %s, 原因: %s", fail.OrderID, fail.ErrorMsg)
		}
	}

	return nil
}

// GetOrder 查询订单
func (b *BitgetAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	path := fmt.Sprintf("/api/v2/mix/order/detail?symbol=%s&productType=%s&orderId=%d", b.symbol, b.productType, orderID)
	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	// 解析订单详情
	var data struct {
		Symbol    string `json:"symbol"`
		Size      string `json:"size"`
		OrderId   string `json:"orderId"`
		ClientOid string `json:"clientOid"`
		FilledQty string `json:"filledQty"`
		Price     string `json:"price"`
		Side      string `json:"side"`
		Status    string `json:"status"`
		PriceAvg  string `json:"priceAvg"`
		CTime     string `json:"cTime"`
		UTime     string `json:"uTime"`
	}

	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("解析订单详情失败: %w", err)
	}

	// 转换为通用格式
	ordID, _ := strconv.ParseInt(data.OrderId, 10, 64)
	price, _ := strconv.ParseFloat(data.Price, 64)
	quantity, _ := strconv.ParseFloat(data.Size, 64)
	executedQty, _ := strconv.ParseFloat(data.FilledQty, 64)
	avgPrice, _ := strconv.ParseFloat(data.PriceAvg, 64)
	updateTime, _ := strconv.ParseInt(data.UTime, 10, 64)

	side := SideBuy
	if data.Side == "sell" {
		side = SideSell
	}

	var status OrderStatus = "NEW"
	switch data.Status {
	case "new":
		status = "NEW"
	case "partial-fill":
		status = "PARTIALLY_FILLED"
	case "full-fill":
		status = "FILLED"
	case "cancelled":
		status = "CANCELED"
	}

	return &Order{
		OrderID:       ordID,
		ClientOrderID: data.ClientOid,
		Symbol:        data.Symbol,
		Side:          side,
		Type:          OrderTypeLimit,
		Price:         price,
		Quantity:      quantity,
		ExecutedQty:   executedQty,
		AvgPrice:      avgPrice,
		Status:        status,
		UpdateTime:    updateTime,
	}, nil
}

// GetOpenOrders 查询未完成订单
func (b *BitgetAdapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	path := fmt.Sprintf("/api/v2/mix/order/orders-pending?symbol=%s&productType=%s", b.symbol, b.productType)
	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	// 解析订单列表（V2 API 返回对象格式）
	var wrapper struct {
		EntrustedList []struct {
			Symbol        string `json:"symbol"`
			Size          string `json:"size"`
			OrderId       string `json:"orderId"`
			ClientOid     string `json:"clientOid"`
			FilledQty     string `json:"filledQty"`
			Fee           string `json:"fee"`
			Price         string `json:"price"`
			Side          string `json:"side"` // "buy" or "sell"
			Status        string `json:"status"`
			PriceAvg      string `json:"priceAvg"`
			BaseVolume    string `json:"baseVolume"`
			QuoteVolume   string `json:"quoteVolume"`
			EntrustVolume string `json:"entrustVolume"`
			TradeAmount   string `json:"tradeAmount"`
			CTime         string `json:"cTime"`
			UTime         string `json:"uTime"`
		} `json:"entrustedList"`
	}

	if err := json.Unmarshal(resp.Data, &wrapper); err != nil {
		return nil, fmt.Errorf("解析订单列表失败: %w", err)
	}

	dataList := wrapper.EntrustedList

	orders := make([]*Order, 0, len(dataList))
	for _, item := range dataList {
		orderID, _ := strconv.ParseInt(item.OrderId, 10, 64)
		price, _ := strconv.ParseFloat(item.Price, 64)
		quantity, _ := strconv.ParseFloat(item.Size, 64)
		executedQty, _ := strconv.ParseFloat(item.FilledQty, 64)
		avgPrice, _ := strconv.ParseFloat(item.PriceAvg, 64)
		updateTime, _ := strconv.ParseInt(item.UTime, 10, 64)

		// 转换方向
		side := SideBuy
		if item.Side == "sell" {
			side = SideSell
		}

		// 转换状态
		var status OrderStatus = "NEW"
		switch item.Status {
		case "new":
			status = "NEW"
		case "partial-fill":
			status = "PARTIALLY_FILLED"
		case "full-fill":
			status = "FILLED"
		case "cancelled":
			status = "CANCELED"
		}

		orders = append(orders, &Order{
			OrderID:       orderID,
			ClientOrderID: item.ClientOid,
			Symbol:        item.Symbol,
			Side:          side,
			Type:          OrderTypeLimit,
			Price:         price,
			Quantity:      quantity,
			ExecutedQty:   executedQty,
			AvgPrice:      avgPrice,
			Status:        status,
			UpdateTime:    updateTime,
		})
	}

	return orders, nil
}

// GetAccount 获取账户信息
func (b *BitgetAdapter) GetAccount(ctx context.Context) (*Account, error) {
	path := fmt.Sprintf("/api/v2/mix/account/account?symbol=%s&productType=%s&marginCoin=%s", b.symbol, b.productType, b.marginCoin)
	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	// 解析账户信息
	var data struct {
		MarginCoin            string `json:"marginCoin"`
		Locked                string `json:"locked"`
		Available             string `json:"available"`
		CrossMaxAvailable     string `json:"crossedMaxAvailable"`  // 注意：API文档是crossedMaxAvailable
		FixedMaxAvailable     string `json:"isolatedMaxAvailable"` // 注意：API文档是isolatedMaxAvailable
		MaxTransferOut        string `json:"maxTransferOut"`
		Equity                string `json:"accountEquity"` // 注意：API文档是accountEquity
		USDTEquity            string `json:"usdtEquity"`
		BTCEquity             string `json:"btcEquity"`
		PosMode               string `json:"posMode"`
		MarginMode            string `json:"marginMode"`            // 保证金模式：crossed全仓/isolated逐仓
		CrossedMarginLeverage int    `json:"crossedMarginLeverage"` // 全仓杠杆倍数（数字类型）
		IsolatedLongLever     int    `json:"isolatedLongLever"`     // 逐仓多头杠杆（数字类型）
		IsolatedShortLever    int    `json:"isolatedShortLever"`    // 逐仓空头杠杆（数字类型）
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return nil, fmt.Errorf("解析账户信息失败: %w", err)
	}

	// 转换为通用格式
	available, _ := strconv.ParseFloat(data.Available, 64)
	equity, _ := strconv.ParseFloat(data.Equity, 64)

	// 🔥 强制检查保证金模式：必须是全仓模式
	if data.MarginMode != "crossed" {
		return nil, fmt.Errorf("⚠️ 当前保证金模式为【%s】，本程序仅支持全仓模式(crossed)。\n"+
			"请登录 Bitget 交易所，将保证金模式切换为【全仓】后再运行程序。\n"+
			"切换路径：合约交易 -> 持仓设置 -> 保证金模式 -> 选择全仓模式", data.MarginMode)
	}

	// 解析杠杆倍数（全仓模式）
	accountLeverage := data.CrossedMarginLeverage
	if accountLeverage <= 0 {
		accountLeverage = 1 // 默认1倍
	}

	// 显示持仓模式（双向/单向）
	posModeDesc := "双向持仓"
	if data.PosMode == "one_way_mode" {
		posModeDesc = "单向持仓"
	}

	logger.Info("ℹ️ [Bitget 账户] 保证金模式: crossed(全仓), 持仓模式: %s, 杠杆倍数: %dx, 可用余额: %.2f %s",
		posModeDesc, accountLeverage, available, data.MarginCoin)

	return &Account{
		TotalWalletBalance: equity,
		TotalMarginBalance: equity,
		AvailableBalance:   available,
		Positions:          []*Position{}, // 持仓信息需要单独查询
		PosMode:            data.PosMode,
		AccountLeverage:    accountLeverage, // 添加账户级别的杠杆倍数
	}, nil
}

// GetPositions 获取持仓信息
func (b *BitgetAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	path := fmt.Sprintf("/api/v2/mix/position/single-position?symbol=%s&productType=%s&marginCoin=%s", b.symbol, b.productType, b.marginCoin)
	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	// 解析持仓信息（Bitget 返回数组）
	var dataList []struct {
		MarginCoin        string `json:"marginCoin"`
		Symbol            string `json:"symbol"`
		HoldSide          string `json:"holdSide"` // "long" or "short"
		OpenDelegateCount string `json:"openDelegateCount"`
		Margin            string `json:"margin"`
		Available         string `json:"available"`
		Locked            string `json:"locked"`
		Total             string `json:"total"`
		Leverage          string `json:"leverage"`
		AchievedProfits   string `json:"achievedProfits"`
		AverageOpenPrice  string `json:"averageOpenPrice"`
		MarginMode        string `json:"marginMode"`
		PositionSide      string `json:"positionSide"`
		UnrealizedPL      string `json:"unrealizedPL"`
		LiquidationPrice  string `json:"liquidationPrice"`
		KeepMarginRate    string `json:"keepMarginRate"`
		MarkPrice         string `json:"markPrice"`
	}

	if err := json.Unmarshal(resp.Data, &dataList); err != nil {
		return nil, fmt.Errorf("解析持仓信息失败: %w", err)
	}

	// 转换为通用格式
	positions := make([]*Position, 0, len(dataList))
	for _, item := range dataList {
		total, _ := strconv.ParseFloat(item.Total, 64)
		if total == 0 {
			continue // 跳过空持仓
		}

		entryPrice, _ := strconv.ParseFloat(item.AverageOpenPrice, 64)
		markPrice, _ := strconv.ParseFloat(item.MarkPrice, 64)
		unrealizedPNL, _ := strconv.ParseFloat(item.UnrealizedPL, 64)
		leverage, _ := strconv.Atoi(item.Leverage)
		margin, _ := strconv.ParseFloat(item.Margin, 64)

		// Bitget 使用 holdSide 表示方向，需要转换为正负数
		size := total
		if item.HoldSide == "short" {
			size = -total
		}

		positions = append(positions, &Position{
			Symbol:         item.Symbol,
			Size:           size,
			EntryPrice:     entryPrice,
			MarkPrice:      markPrice,
			UnrealizedPNL:  unrealizedPNL,
			Leverage:       leverage,
			MarginType:     item.MarginMode,
			IsolatedMargin: margin,
		})
	}

	return positions, nil
}

// GetBalance 获取余额
func (b *BitgetAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	account, err := b.GetAccount(ctx)
	if err != nil {
		return 0, err
	}
	return account.AvailableBalance, nil
}

// SetOrderMappingCallback 设置订单映射回调
// 用于在下单成功后立即建立 orderID -> price 的映射
func (b *BitgetAdapter) SetOrderMappingCallback(callback func(orderID int64, price float64)) {
	b.orderMappingCallback = callback
}

// StartOrderStream 启动订单流（WebSocket）
// 架构说明：
// - 订单流通过 main.go 中的 ex.StartOrderStream() 启动
// - 如果价格流已经启动，这里会复用同一个 WebSocket 连接
// - 订单流需要订阅私有频道（orders），需要登录认证
func (b *BitgetAdapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	logger.Debug("🔗 [Bitget] 启动订单流 WebSocket（私有频道）")

	// 转换回调函数
	wrappedCallback := func(update interface{}) {
		// 如果是 *OrderUpdate 指针类型，转换为通用结构体
		if localUpdate, ok := update.(*OrderUpdate); ok {
			logger.Debug("🔍 [Bitget Adapter] 订单更新回调触发: ID=%d, ClientOID=%s, Status=%s",
				localUpdate.OrderID, localUpdate.ClientOrderID, string(localUpdate.Status))
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
				OrderID:       localUpdate.OrderID,
				ClientOrderID: localUpdate.ClientOrderID, // 🔥 关键：传递 ClientOrderID
				Symbol:        localUpdate.Symbol,
				Side:          string(localUpdate.Side),
				Type:          string(localUpdate.Type),
				Status:        string(localUpdate.Status),
				Price:         localUpdate.Price,
				Quantity:      localUpdate.Quantity,
				ExecutedQty:   localUpdate.ExecutedQty,
				AvgPrice:      localUpdate.AvgPrice,
				UpdateTime:    localUpdate.UpdateTime,
			}
			callback(genericUpdate)
		} else {
			logger.Warn("⚠️ [Bitget Adapter] 订单更新类型断言失败: %T", update)
		}
	}

	return b.wsManager.Start(ctx, b.symbol, wrappedCallback)
}

// StopOrderStream 停止订单流
func (b *BitgetAdapter) StopOrderStream() error {
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
func (b *BitgetAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
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
// 架构说明：
// - 价格流通过 PriceMonitor 在 main.go 中启动（唯一入口）
// - 价格流和订单流共用同一个 WebSocketManager
// - 如果只需要价格流，传入 callback=nil 给 wsManager.Start()
func (b *BitgetAdapter) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	// 注册价格回调
	b.wsManager.SetPriceCallback(func(s string, p float64) {
		// 过滤交易对
		if s == b.symbol {
			callback(p)
		}
	})

	// 如果 WebSocket 还没启动，启动公共频道（ticker）
	// 注意：传入 nil 作为订单回调，表示只订阅价格，不订阅订单
	if !b.wsManager.IsRunning() {
		logger.Debug("🔗 [Bitget] 启动价格流 WebSocket（公共频道）")
		return b.wsManager.Start(ctx, b.symbol, nil)
	}

	logger.Debug("✅ [Bitget] 价格流回调已注册（WebSocket已在运行）")
	return nil
}

// StartKlineStream 启动K线流（WebSocket）
func (b *BitgetAdapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(candle interface{})) error {
	if b.klineWSManager == nil {
		b.klineWSManager = NewKlineWebSocketManager()
	}
	return b.klineWSManager.Start(ctx, symbols, interval, callback)
}

// StopKlineStream 停止K线流
func (b *BitgetAdapter) StopKlineStream() error {
	if b.klineWSManager != nil {
		b.klineWSManager.Stop()
	}
	return nil
}

// GetHistoricalKlines 获取历史K线数据
func (b *BitgetAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	// Bitget 支持的K线周期映射
	// 1m, 3m, 5m, 15m, 30m, 1H, 4H, 6H, 12H, 1D, 3D, 1W, 1M
	bitgetInterval := convertToBitgetInterval(interval)

	// 构建请求路径
	// limit: Bitget 最多支持 1000 根K线
	if limit > 1000 {
		limit = 1000
	}

	// 计算结束时间（当前时间）和开始时间
	endTime := time.Now().UnixMilli()

	path := fmt.Sprintf("/api/v2/mix/market/candles?symbol=%s&productType=%s&granularity=%s&limit=%d&endTime=%d",
		b.symbol, b.productType, bitgetInterval, limit, endTime)

	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("获取历史K线失败: %w", err)
	}

	// 解析K线数据
	// Bitget 返回格式: [[timestamp, open, high, low, close, volume, ...], ...]
	var dataList [][]string
	if err := json.Unmarshal(resp.Data, &dataList); err != nil {
		return nil, fmt.Errorf("解析K线数据失败: %w", err)
	}

	candles := make([]*Candle, 0, len(dataList))
	for _, item := range dataList {
		if len(item) < 6 {
			continue // 跳过无效数据
		}

		timestamp, _ := strconv.ParseInt(item[0], 10, 64)
		open, _ := strconv.ParseFloat(item[1], 64)
		high, _ := strconv.ParseFloat(item[2], 64)
		low, _ := strconv.ParseFloat(item[3], 64)
		close, _ := strconv.ParseFloat(item[4], 64)
		volume, _ := strconv.ParseFloat(item[5], 64)

		candles = append(candles, &Candle{
			Symbol:    symbol,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
			Timestamp: timestamp,
			IsClosed:  true, // 历史K线都是已完结的
		})
	}

	// Bitget 返回的K线是倒序的（最新的在前），需要反转
	for i, j := 0, len(candles)-1; i < j; i, j = i+1, j-1 {
		candles[i], candles[j] = candles[j], candles[i]
	}

	return candles, nil
}

// convertToBitgetInterval 将标准K线周期转换为 Bitget 格式
// 输入: 1m, 3m, 5m, 15m, 30m, 1h, 4h, 6h, 12h, 1d, 3d, 1w, 1M
// 输出: 1m, 3m, 5m, 15m, 30m, 1H, 4H, 6H, 12H, 1D, 3D, 1W, 1M
func convertToBitgetInterval(interval string) string {
	switch interval {
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
	case "6h":
		return "6H"
	case "12h":
		return "12H"
	case "1d":
		return "1D"
	case "3d":
		return "3D"
	case "1w":
		return "1W"
	case "1M":
		return "1M"
	default:
		return interval // 如果已经是 Bitget 格式，直接返回
	}
}

// convertToBitgetSymbol 将标准符号转换为 Bitget 合约符号
// Bitget V2 API 使用不带后缀的符号格式（如 ETHUSDT）
func convertToBitgetSymbol(symbol string) string {
	// 去掉可能存在的 _UMCBL 后缀（兼容旧配置）
	if strings.Contains(symbol, "_UMCBL") {
		return strings.TrimSuffix(symbol, "_UMCBL")
	}
	// V2 API 直接使用原始符号
	return symbol
}

// getHoldSide 根据持仓数量判断持仓方向
func getHoldSide(size float64) string {
	if size > 0 {
		return "long"
	} else if size < 0 {
		return "short"
	}
	return "none"
}

// GetPriceDecimals 获取价格精度（小数位数）
func (b *BitgetAdapter) GetPriceDecimals() int {
	return b.pricePlace
}

// GetQuantityDecimals 获取数量精度（小数位数）
func (b *BitgetAdapter) GetQuantityDecimals() int {
	return b.volumePlace
}

// GetBaseAsset 获取基础资产（交易币种）
func (b *BitgetAdapter) GetBaseAsset() string {
	return b.baseAsset
}

// GetQuoteAsset 获取计价资产（结算币种）
func (b *BitgetAdapter) GetQuoteAsset() string {
	return b.quoteAsset
}
