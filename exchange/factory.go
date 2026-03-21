package exchange

import (
	"fmt"
	"opensqt/config"
	"opensqt/exchange/binance"
	"opensqt/exchange/bitget"
	"opensqt/exchange/gate"
	"opensqt/exchange/okx"
)

// NewExchange 创建交易所实例
func NewExchange(cfg *config.Config) (IExchange, error) {
	exchangeName := cfg.App.CurrentExchange

	switch exchangeName {
	case "bitget":
		exchangeCfg, exists := cfg.Exchanges["bitget"]
		if !exists {
			return nil, fmt.Errorf("bitget 配置不存在")
		}
		// 将 ExchangeConfig 转换为 map[string]string
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"passphrase": exchangeCfg.Passphrase,
		}
		adapter, err := bitget.NewBitgetAdapter(cfgMap, cfg.Trading.Symbol)
		if err != nil {
			return nil, err
		}
		return &bitgetWrapper{adapter: adapter}, nil

	case "binance":
		exchangeCfg, exists := cfg.Exchanges["binance"]
		if !exists {
			return nil, fmt.Errorf("binance 配置不存在")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
		}
		adapter, err := binance.NewBinanceAdapter(cfgMap, cfg.Trading.Symbol)
		if err != nil {
			return nil, err
		}
		return &binanceWrapper{adapter: adapter}, nil

	case "gate":
		exchangeCfg, exists := cfg.Exchanges["gate"]
		if !exists {
			return nil, fmt.Errorf("gate 配置不存在")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"settle":     "usdt", // 默认 USDT 永续合约
		}
		adapter, err := gate.NewGateAdapter(cfgMap, cfg.Trading.Symbol)
		if err != nil {
			return nil, err
		}
		return &gateWrapper{adapter: adapter}, nil

	case "okx":
		exchangeCfg, exists := cfg.Exchanges["okx"]
		if !exists {
			return nil, fmt.Errorf("okx configuration not found")
		}
		cfgMap := map[string]string{
			"api_key":    exchangeCfg.APIKey,
			"secret_key": exchangeCfg.SecretKey,
			"passphrase": exchangeCfg.Passphrase,
		}
		adapter, err := okx.NewOKXAdapter(cfgMap, cfg.Trading.Symbol)
		if err != nil {
			return nil, err
		}
		return &okxWrapper{adapter: adapter}, nil

	case "bybit":
		return nil, fmt.Errorf("bybit 尚未实现")

	case "edgex":
		return nil, fmt.Errorf("edgeX 尚未实现")

	default:
		return nil, fmt.Errorf("不支持的交易所: %s", exchangeName)
	}
}
