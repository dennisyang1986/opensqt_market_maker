package main

import (
	"context"
	"os"
	"os/signal"
	"reflect"
	"syscall"
	"time"

	"opensqt/config"
	"opensqt/exchange"
	"opensqt/logger"
	"opensqt/monitor"
	"opensqt/order"
	"opensqt/position"
	"opensqt/safety"
)

// Version 版本号
var Version = "v3.3.1"

func main() {
	logger.Info("🚀 www.OpenSQT.com 做市商系统启动...")
	logger.Info("📦 版本号: %s", Version)

	// 1. 加载配置
	configPath := "config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		logger.Fatalf("❌ 加载配置失败: %v", err)
	}

	// 初始化日志级别
	logLevel := logger.ParseLogLevel(cfg.System.LogLevel)
	logger.SetLevel(logLevel)
	logger.Info("日志级别设置为: %s", logLevel.String())

	logger.Info("✅ 配置加载成功: 交易对=%s, 窗口大小=%d, 当前交易所=%s",
		cfg.Trading.Symbol, cfg.Trading.BuyWindowSize, cfg.App.CurrentExchange)

	// 2. 创建交易所实例（使用工厂模式）
	ex, err := exchange.NewExchange(cfg)
	if err != nil {
		logger.Fatalf("❌ 创建交易所实例失败: %v", err)
	}
	logger.Info("✅ 使用交易所: %s", ex.GetName())

	// 3. 创建价格监控组件（全局唯一的价格来源）
	// 架构说明：
	// - 这是整个系统中唯一的价格流启动点
	// - WebSocket 是唯一的价格来源，不使用 REST API 轮询
	// - 所有组件需要价格时，都应该通过 priceMonitor.GetLastPrice() 获取
	// - 必须在其他组件初始化前启动，确保价格数据就绪
	priceMonitor := monitor.NewPriceMonitor(
		ex,
		cfg.Trading.Symbol,
		cfg.Timing.PriceSendInterval,
	)

	// 4. 启动价格监控（WebSocket 必须成功）
	logger.Info("🔗 启动 WebSocket 价格流...")
	if err := priceMonitor.Start(); err != nil {
		logger.Fatalf("❌ 启动价格流失败（WebSocket 是唯一价格来源）: %v", err)
	}

	// 5. 等待从 WebSocket 获取初始价格
	logger.Debugln("⏳ 等待 WebSocket 推送初始价格...")
	var currentPrice float64
	var currentPriceStr string
	pollInterval := time.Duration(cfg.Timing.PricePollInterval) * time.Millisecond
	for i := 0; i < 10; i++ {
		currentPrice = priceMonitor.GetLastPrice()
		currentPriceStr = priceMonitor.GetLastPriceString()
		if currentPrice > 0 {
			break
		}
		time.Sleep(pollInterval)
	}

	if currentPrice <= 0 {
		logger.Fatalf("❌ 无法从 WebSocket 获取价格（超时），系统无法启动")
	}

	// 从交易所获取精度信息
	priceDecimals := ex.GetPriceDecimals()
	quantityDecimals := ex.GetQuantityDecimals()
	logger.Info("ℹ️ 交易精度 - 价格精度:%d, 数量精度:%d", priceDecimals, quantityDecimals)
	logger.Debug("📊 当前价格: %.*f", priceDecimals, currentPrice)

	// 6. 持仓安全性检查（必须在开始交易之前执行）
	requiredPositions := cfg.Trading.PositionSafetyCheck
	if requiredPositions <= 0 {
		requiredPositions = 100 // 默认100
	}

	// 获取当前交易所的手续费率
	exchangeCfg := cfg.Exchanges[cfg.App.CurrentExchange]
	feeRate := exchangeCfg.FeeRate
	// 注意：支持0费率，不需要特殊处理

	// 执行持仓安全性检查（使用独立的 safety 包）
	if err := safety.CheckAccountSafety(
		ex,
		cfg.Trading.Symbol,
		currentPrice,
		cfg.Trading.OrderQuantity,
		cfg.Trading.PriceInterval,
		feeRate,
		requiredPositions,
		priceDecimals,
	); err != nil {
		logger.Fatalf("❌ %v", err)
	}
	logger.Info("✅ 持仓安全性检查通过，开始初始化交易组件...")

	// 8. 创建核心组件
	exchangeExecutor := order.NewExchangeOrderExecutor(
		ex,
		cfg.Trading.Symbol,
		cfg.Timing.RateLimitRetryDelay,
		cfg.Timing.OrderRetryDelay,
	)
	executorAdapter := &exchangeExecutorAdapter{executor: exchangeExecutor}

	// 创建交易所适配器（匹配 position.IExchange 接口）
	exchangeAdapter := &positionExchangeAdapter{exchange: ex}
	superPositionManager := position.NewSuperPositionManager(cfg, executorAdapter, exchangeAdapter, priceDecimals, quantityDecimals)

	// === 新增：初始化风控监视器 ===
	riskMonitor := safety.NewRiskMonitor(cfg, ex)

	// === 创建对账器（从仓位管理器剖离） ===
	reconciler := safety.NewReconciler(cfg, exchangeAdapter, superPositionManager)
	// 将风控状态注入到对账器，用于暂停对账日志
	reconciler.SetPauseChecker(func() bool {
		return riskMonitor.IsTriggered()
	})

	// 9. 启动组件
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 🔥 关键修复：先启动订单流，再下单（避免错过成交推送）
	// 启动订单流（通过交易所接口）
	// 架构说明：
	// - 订单流与价格流共用同一个 WebSocket 连接（对于支持的交易所）
	// - 订单更新通过回调函数实时推送给 SuperPositionManager
	//logger.Info("🔗 启动 WebSocket 订单流...")
	if err := ex.StartOrderStream(ctx, func(updateInterface interface{}) {
		// 使用反射提取字段（兼容匿名结构体）
		v := reflect.ValueOf(updateInterface)
		if v.Kind() != reflect.Struct {
			logger.Warn("⚠️ [main.go] 订单更新不是结构体类型: %T", updateInterface)
			return
		}

		// 提取字段值的辅助函数
		getInt64Field := func(name string) int64 {
			field := v.FieldByName(name)
			if field.IsValid() && field.CanInt() {
				return field.Int()
			}
			return 0
		}

		getStringField := func(name string) string {
			field := v.FieldByName(name)
			if field.IsValid() && field.Kind() == reflect.String {
				return field.String()
			}
			return ""
		}

		getFloat64Field := func(name string) float64 {
			field := v.FieldByName(name)
			if field.IsValid() && field.CanFloat() {
				return field.Float()
			}
			return 0.0
		}

		// 提取所有字段
		posUpdate := position.OrderUpdate{
			OrderID:       getInt64Field("OrderID"),
			ClientOrderID: getStringField("ClientOrderID"), // 🔥 关键：传递 ClientOrderID
			Symbol:        getStringField("Symbol"),
			Status:        getStringField("Status"),
			ExecutedQty:   getFloat64Field("ExecutedQty"),
			Price:         getFloat64Field("Price"),
			AvgPrice:      getFloat64Field("AvgPrice"),
			Side:          getStringField("Side"),
			Type:          getStringField("Type"),
			UpdateTime:    getInt64Field("UpdateTime"),
		}

		logger.Debug("🔍 [main.go] 收到订单更新回调: ID=%d, ClientOID=%s, Price=%.2f, Status=%s",
			posUpdate.OrderID, posUpdate.ClientOrderID, posUpdate.Price, posUpdate.Status)
		superPositionManager.OnOrderUpdate(posUpdate)
	}); err != nil {
		logger.Warn("⚠️ 启动订单流失败: %v (将继续运行，但订单状态更新可能延迟)", err)
	} else {
		logger.Info("✅ [%s] 订单流已启动", ex.GetName())
	}

	// 初始化超级仓位管理器（设置价格锚点并创建初始槽位）
	// 注意：必须在订单流启动后再初始化，避免错过入场单成交推送
	if err := superPositionManager.Initialize(currentPrice, currentPriceStr); err != nil {
		logger.Fatalf("❌ 初始化超级仓位管理器失败: %v", err)
	}

	// 启动持仓对账（使用独立的 Reconciler）
	reconciler.Start(ctx)

	// === 创建订单清理器（从仓位管理器剥离） ===
	orderCleaner := safety.NewOrderCleaner(cfg, exchangeExecutor, superPositionManager)
	// 启动订单清理协程
	orderCleaner.Start(ctx)

	// 启动价格监控（WebSocket 是唯一的价格来源）
	// 注意：毫秒级量化系统不支持 REST API 轮询，WebSocket 失败时系统将停止
	go func() {
		// 检查是否已经在运行
		if err := priceMonitor.Start(); err != nil {
			// 忽略"已在运行"的错误
			if err.Error() != "价格监控已在运行" {
				logger.Fatalf("❌ 启动价格监控失败（WebSocket 必须可用）: %v", err)
			}
		}
	}()

	// 启动风控监控
	go riskMonitor.Start(ctx)

	// 10. 监听价格变化,调整订单窗口（实时调整，不打印价格变化日志）
	go func() {
		priceCh := priceMonitor.Subscribe()
		var lastTriggered bool // 记录上一次的风控状态，用于检测状态切换

		for priceChange := range priceCh {
			// === 风控检查：触发时撤销所有入场单并暂停交易 ===
			isTriggered := riskMonitor.IsTriggered()

			if isTriggered {
				// 检测状态切换：从未触发 -> 触发（首次触发）
				if !lastTriggered {
					logger.Warn("🚨 [风控触发] 市场异常，正在撤销所有入场单并暂停交易...")
					superPositionManager.CancelAllBuyOrders() // 🔥 只撤销入场单，保留卖单
					lastTriggered = true
				}
				// 风控触发期间跳过后续下单逻辑
				continue
			}

			// 检测状态切换：从触发 -> 未触发（风控解除）
			if lastTriggered {
				logger.Info("✅ [风控解除] 市场恢复正常，恢复自动交易")
				lastTriggered = false
			}

			// 实时调整订单，不打印价格变化日志（避免日志过多）
			if err := superPositionManager.AdjustOrders(priceChange.NewPrice); err != nil {
				logger.Error("❌ 调整订单失败: %v", err)
			}
		}
	}()

	// 13. 定期打印持仓和订单状态
	go func() {
		statusInterval := time.Duration(cfg.Timing.StatusPrintInterval) * time.Minute
		ticker := time.NewTicker(statusInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// 风控触发时不打印状态
				if !riskMonitor.IsTriggered() {
					superPositionManager.PrintPositions()
				}
			}
		}
	}()

	// 14. 等待退出信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("🛑 收到退出信号，开始优雅关闭...")

	// 🔥 第一优先级：立即撤销所有订单（最重要！）
	// 使用独立的超时 context，确保撤单请求能发送成功
	if cfg.System.CancelOnExit {
		logger.Info("🔄 正在撤销所有订单（最高优先级）...")
		cancelCtx, cancelTimeout := context.WithTimeout(context.Background(), 10*time.Second)
		if err := ex.CancelAllOrders(cancelCtx, cfg.Trading.Symbol); err != nil {
			logger.Error("❌ 撤销订单失败: %v", err)
		} else {
			logger.Info("✅ 所有订单已成功撤销")
		}
		cancelTimeout()
	}

	// 🔥 第二优先级：停止所有协程（取消 context）
	// 这会通知所有使用 ctx 的协程停止工作
	cancel()

	// 🔥 第三优先级：优雅停止各个组件
	// 注意：这些组件的 Stop() 方法内部会处理 WebSocket 关闭等清理工作
	logger.Info("⏹️ 正在停止价格监控...")
	priceMonitor.Stop()

	logger.Info("⏹️ 正在停止订单流...")
	ex.StopOrderStream()

	logger.Info("⏹️ 正在停止风控监视器...")
	riskMonitor.Stop()

	// 等待一小段时间，让协程完成清理（避免强制退出导致日志丢失）
	time.Sleep(500 * time.Millisecond)

	// 打印最终状态
	superPositionManager.PrintPositions()

	// 关闭文件日志
	logger.Close()

	logger.Info("✅ 系统已安全退出 www.OpenSQT.com")
}

// positionExchangeAdapter 适配器，将 exchange.IExchange 转换为 position.IExchange
type positionExchangeAdapter struct {
	exchange exchange.IExchange
}

func (a *positionExchangeAdapter) GetPositions(ctx context.Context, symbol string) (interface{}, error) {
	positions, err := a.exchange.GetPositions(ctx, symbol)
	if err != nil {
		return nil, err
	}

	// 转换为 position.PositionInfo 切片
	result := make([]*position.PositionInfo, len(positions))
	for i, pos := range positions {
		result[i] = &position.PositionInfo{
			Symbol: pos.Symbol,
			Size:   pos.Size,
		}
	}

	return result, nil
}

func (a *positionExchangeAdapter) GetOpenOrders(ctx context.Context, symbol string) (interface{}, error) {
	return a.exchange.GetOpenOrders(ctx, symbol)
}

func (a *positionExchangeAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (interface{}, error) {
	return a.exchange.GetOrder(ctx, symbol, orderID)
}

func (a *positionExchangeAdapter) GetBaseAsset() string {
	return a.exchange.GetBaseAsset()
}

func (a *positionExchangeAdapter) GetName() string {
	return a.exchange.GetName()
}

func (a *positionExchangeAdapter) CancelAllOrders(ctx context.Context, symbol string) error {
	return a.exchange.CancelAllOrders(ctx, symbol)
}

// exchangeExecutorAdapter 适配器，将 order.ExchangeOrderExecutor 转换为 position.OrderExecutorInterface
type exchangeExecutorAdapter struct {
	executor *order.ExchangeOrderExecutor
}

func (a *exchangeExecutorAdapter) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	orderReq := &order.OrderRequest{
		Symbol:        req.Symbol,
		Side:          req.Side,
		Price:         req.Price,
		Quantity:      req.Quantity,
		PriceDecimals: req.PriceDecimals,
		ReduceOnly:    req.ReduceOnly,
		PostOnly:      req.PostOnly,      // 传递 PostOnly 参数
		ClientOrderID: req.ClientOrderID, // 传递 ClientOrderID
	}
	ord, err := a.executor.PlaceOrder(orderReq)
	if err != nil {
		return nil, err
	}
	return &position.Order{
		OrderID:       ord.OrderID,
		ClientOrderID: ord.ClientOrderID, // 返回 ClientOrderID
		Symbol:        ord.Symbol,
		Side:          ord.Side,
		Price:         ord.Price,
		Quantity:      ord.Quantity,
		Status:        ord.Status,
		CreatedAt:     ord.CreatedAt,
	}, nil
}

func (a *exchangeExecutorAdapter) BatchPlaceOrders(orders []*position.OrderRequest) ([]*position.Order, bool) {
	orderReqs := make([]*order.OrderRequest, len(orders))
	for i, req := range orders {
		orderReqs[i] = &order.OrderRequest{
			Symbol:        req.Symbol,
			Side:          req.Side,
			Price:         req.Price,
			Quantity:      req.Quantity,
			PriceDecimals: req.PriceDecimals,
			ReduceOnly:    req.ReduceOnly,
			PostOnly:      req.PostOnly,      // 传递 PostOnly 参数
			ClientOrderID: req.ClientOrderID, // 传递 ClientOrderID
		}
	}
	ords, marginError := a.executor.BatchPlaceOrders(orderReqs)
	result := make([]*position.Order, len(ords))
	for i, ord := range ords {
		result[i] = &position.Order{
			OrderID:       ord.OrderID,
			ClientOrderID: ord.ClientOrderID, // 返回 ClientOrderID
			Symbol:        ord.Symbol,
			Side:          ord.Side,
			Price:         ord.Price,
			Quantity:      ord.Quantity,
			Status:        ord.Status,
			CreatedAt:     ord.CreatedAt,
		}
	}
	return result, marginError
}

func (a *exchangeExecutorAdapter) BatchCancelOrders(orderIDs []int64) error {
	return a.executor.BatchCancelOrders(orderIDs)
}
