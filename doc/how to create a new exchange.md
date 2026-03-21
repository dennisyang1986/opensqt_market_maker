• 按这个仓库现在的结构，增加一个新交易所，核心是走这条链路：

新交易所 SDK/HTTP+WS 实现 -> exchange/<newex>/adapter.go -> exchange/wrapper_<newex>.go -> exchange.NewExchange(...) -> main.go 启动统一流程。

你要改的地方

1. 实现统一接口
   在 exchange/interface.go 里，IExchange 是硬约束。新交易所至少要实现这些能力：

- 下单/批量下单/撤单/批量撤单/全撤
- 查询订单、持仓、余额、账户
- 订单流、价格流、K 线流
- 精度、基础币、计价币

最省事的做法是直接参考现有目录结构，新建：

- exchange/<newex>/adapter.go
- exchange/<newex>/client.go
- exchange/<newex>/websocket.go
- exchange/<newex>/kline_websocket.go
- 必要时再加 signer.go、types.go

可直接对照 exchange/binance/adapter.go 和 exchange/bitget/adapter.go。

2. 写 wrapper 做类型转换
   根目录 exchange/wrapper_*.go 的职责不是实现交易逻辑，而是把通用 exchange.OrderRequest/Order/... 和交易所本地类型互转。
   直接仿照：

- exchange/wrapper_binance.go
- exchange/wrapper_bitget.go

3. 在工厂注册
   把新交易所接到 exchange/factory.go 的 NewExchange 里。这里负责：

- 读取 cfg.Exchanges["<newex>"]
- 组装 cfgMap
- 调用 New<NewEx>Adapter(...)
- 返回 &<newex>Wrapper{...}

4. 补配置项
   配置结构在 config/config.go。
   如果新交易所需要额外字段，比如 passphrase、uid、broker_id、settle，要扩展 ExchangeConfig，并同步 config.example.yaml。

最容易漏的点

- StartOrderStream 的回调必须吐出一个带这些字段的结构体：OrderID、ClientOrderID、Symbol、Side、Type、Status、Price、ExecutedQty、AvgPrice、UpdateTime。main.go 里是靠反射取字段名的，字段名不对就接不
  上。
- GetPriceDecimals()、GetQuantityDecimals()、GetBaseAsset()、GetQuoteAsset() 不是装饰信息，main.go、safety、position 都会用。
- CancelAllOrders 必须可靠，因为退出时直接调用它。
- 价格流必须能被 main.go 的 PriceMonitor 启动，否则系统起不来。

建议实施顺序

1. 先做 REST：账户、持仓、下单、撤单
2. 再做价格 WebSocket
3. 再做订单 WebSocket
4. 最后做 K 线和历史 K 线
5. 跑 go build ./... 和 go test ./...

如果你要，我可以下一步直接帮你列一个“新增 Bybit 支持”的最小改动清单，精确到文件和函数。