package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config 做市商系统配置
type Config struct {
	// 应用配置
	App struct {
		CurrentExchange string `yaml:"current_exchange"` // 当前使用的交易所
	} `yaml:"app"`

	// 多交易所配置
	Exchanges map[string]ExchangeConfig `yaml:"exchanges"`

	Trading struct {
		Mode                  string  `yaml:"mode"` // 交易模式: long / short
		Symbol                string  `yaml:"symbol"`
		PriceInterval         float64 `yaml:"price_interval"`
		OrderQuantity         float64 `yaml:"order_quantity"`  // 每单购买金额（USDT/USDC）
		MinOrderValue         float64 `yaml:"min_order_value"` // 最小订单价值（USDT），默认6U，小于此值不挂单
		BuyWindowSize         int     `yaml:"buy_window_size"`
		SellWindowSize        int     `yaml:"sell_window_size"` // 卖单窗口大小
		ReconcileInterval     int     `yaml:"reconcile_interval"`
		OrderCleanupThreshold int     `yaml:"order_cleanup_threshold"`      // 订单清理上限（默认100）
		CleanupBatchSize      int     `yaml:"cleanup_batch_size"`           // 清理批次大小（默认10）
		MarginLockDurationSec int     `yaml:"margin_lock_duration_seconds"` // 保证金锁定时间（秒，默认10）
		PositionSafetyCheck   int     `yaml:"position_safety_check"`        // 持仓安全性检查（默认100，最少能向下持有多少仓）
		// 注意：price_decimals 和 quantity_decimals 已废弃，现在从交易所自动获取
	} `yaml:"trading"`

	System struct {
		LogLevel     string `yaml:"log_level"`
		CancelOnExit bool   `yaml:"cancel_on_exit"`
	} `yaml:"system"`

	// 主动安全风控配置
	RiskControl struct {
		Enabled           bool     `yaml:"enabled"`            // 是否启用风控，默认true
		MonitorSymbols    []string `yaml:"monitor_symbols"`    // 监控币种，如 ["BTCUSDT", "ETHUSDT"]
		Interval          string   `yaml:"interval"`           // K线周期，如 "1m", "3m", "5m"
		VolumeMultiplier  float64  `yaml:"volume_multiplier"`  // 成交量倍数阈值，默认3.0
		AverageWindow     int      `yaml:"average_window"`     // 移动平均窗口大小，默认20
		RecoveryThreshold int      `yaml:"recovery_threshold"` // 恢复交易所需的正常币种数量，默认3
	} `yaml:"risk_control"`

	// 时间间隔配置（单位：秒，除非特别说明）
	Timing struct {
		// WebSocket相关
		WebSocketReconnectDelay    int `yaml:"websocket_reconnect_delay"`     // WebSocket断线重连等待时间（秒，默认5）
		WebSocketWriteWait         int `yaml:"websocket_write_wait"`          // WebSocket写入等待时间（秒，默认10）
		WebSocketPongWait          int `yaml:"websocket_pong_wait"`           // WebSocket PONG等待时间（秒，默认60）
		WebSocketPingInterval      int `yaml:"websocket_ping_interval"`       // WebSocket PING间隔（秒，默认20）
		ListenKeyKeepAliveInterval int `yaml:"listen_key_keepalive_interval"` // listenKey保活间隔（分钟，默认30）

		// 价格监控相关
		PriceSendInterval int `yaml:"price_send_interval"` // 定期发送价格的间隔（毫秒，默认50）

		// 订单执行相关
		RateLimitRetryDelay  int `yaml:"rate_limit_retry_delay"` // 速率限制重试等待时间（秒，默认1）
		OrderRetryDelay      int `yaml:"order_retry_delay"`      // 其他错误重试等待时间（毫秒，默认500）
		PricePollInterval    int `yaml:"price_poll_interval"`    // 等待获取价格的轮询间隔（毫秒，默认500）
		StatusPrintInterval  int `yaml:"status_print_interval"`  // 定期打印状态的间隔（分钟，默认1）
		OrderCleanupInterval int `yaml:"order_cleanup_interval"` // 订单清理检查间隔（秒，默认60）
	} `yaml:"timing"`
}

// ExchangeConfig 交易所配置
type ExchangeConfig struct {
	APIKey     string  `yaml:"api_key"`
	SecretKey  string  `yaml:"secret_key"`
	Passphrase string  `yaml:"passphrase"` // Bitget 需要
	FeeRate    float64 `yaml:"fee_rate"`   // 手续费率（例如 0.0002 表示 0.02%）
}

// LoadConfig 加载配置文件
func LoadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %v", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("配置验证失败: %v", err)
	}

	return &cfg, nil
}

// Validate 验证配置
func (c *Config) Validate() error {
	// 验证交易所配置
	if c.App.CurrentExchange == "" {
		return fmt.Errorf("必须指定当前使用的交易所 (app.current_exchange)")
	}

	// 验证多交易所配置
	if len(c.Exchanges) == 0 {
		return fmt.Errorf("未配置任何交易所，请在 exchanges 中添加配置")
	}

	exchangeCfg, exists := c.Exchanges[c.App.CurrentExchange]
	if !exists {
		return fmt.Errorf("交易所 %s 的配置不存在", c.App.CurrentExchange)
	}

	if exchangeCfg.APIKey == "" || exchangeCfg.SecretKey == "" {
		return fmt.Errorf("交易所 %s 的 API 配置不完整", c.App.CurrentExchange)
	}

	// 验证手续费率配置
	if exchangeCfg.FeeRate < 0 {
		return fmt.Errorf("交易所 %s 的手续费率不能为负数", c.App.CurrentExchange)
	}

	if c.Trading.Symbol == "" {
		return fmt.Errorf("交易对不能为空")
	}
	if strings.TrimSpace(c.Trading.Mode) == "" {
		c.Trading.Mode = "long"
	} else {
		mode := strings.ToLower(strings.TrimSpace(c.Trading.Mode))
		if mode != "long" && mode != "short" {
			return fmt.Errorf("trading.mode 仅支持 long 或 short")
		}
		c.Trading.Mode = mode
	}
	if c.Trading.OrderQuantity <= 0 {
		return fmt.Errorf("订单金额必须大于0")
	}
	if c.Trading.BuyWindowSize <= 0 {
		return fmt.Errorf("买单窗口大小必须大于0")
	}
	if c.Trading.SellWindowSize <= 0 {
		c.Trading.SellWindowSize = c.Trading.BuyWindowSize // 默认与买单窗口相同
	}
	if c.Trading.CleanupBatchSize <= 0 {
		c.Trading.CleanupBatchSize = 10 // 默认10
	}
	// 注意：price_decimals 和 quantity_decimals 已从配置中移除，现在从交易所自动获取
	if c.Trading.MinOrderValue <= 0 {
		c.Trading.MinOrderValue = 20.0 // 默认6U (币安通常最小5U)
	}

	// 设置默认时间间隔
	if c.Timing.WebSocketReconnectDelay <= 0 {
		c.Timing.WebSocketReconnectDelay = 5 // 默认5秒
	}
	if c.Timing.WebSocketWriteWait <= 0 {
		c.Timing.WebSocketWriteWait = 10 // 默认10秒
	}
	if c.Timing.WebSocketPongWait <= 0 {
		c.Timing.WebSocketPongWait = 60 // 默认60秒
	}
	if c.Timing.WebSocketPingInterval <= 0 {
		c.Timing.WebSocketPingInterval = 20 // 默认20秒
	}
	if c.Timing.ListenKeyKeepAliveInterval <= 0 {
		c.Timing.ListenKeyKeepAliveInterval = 30 // 默认30分钟
	}
	if c.Timing.PriceSendInterval <= 0 {
		c.Timing.PriceSendInterval = 50 // 默认50毫秒
	}
	if c.Timing.RateLimitRetryDelay <= 0 {
		c.Timing.RateLimitRetryDelay = 1 // 默认1秒
	}
	if c.Timing.OrderRetryDelay <= 0 {
		c.Timing.OrderRetryDelay = 500 // 默认500毫秒
	}
	if c.Timing.PricePollInterval <= 0 {
		c.Timing.PricePollInterval = 500 // 默认500毫秒
	}
	if c.Timing.StatusPrintInterval <= 0 {
		c.Timing.StatusPrintInterval = 1 // 默认1分钟
	}
	if c.Timing.OrderCleanupInterval <= 0 {
		c.Timing.OrderCleanupInterval = 60 // 默认60秒
	}

	// 验证风控配置并设置默认值
	if c.RiskControl.Interval == "" {
		c.RiskControl.Interval = "1m" // 默认1分钟
	}
	if c.RiskControl.VolumeMultiplier <= 0 {
		c.RiskControl.VolumeMultiplier = 3.0 // 默认3倍
	}
	if c.RiskControl.AverageWindow <= 0 {
		c.RiskControl.AverageWindow = 20 // 默认20根K线
	}
	if len(c.RiskControl.MonitorSymbols) == 0 {
		c.RiskControl.MonitorSymbols = []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "XRPUSDT", "DOGEUSDT"}
	}

	// 验证恢复阈值配置
	monitorCount := len(c.RiskControl.MonitorSymbols)
	if c.RiskControl.RecoveryThreshold <= 0 {
		c.RiskControl.RecoveryThreshold = 3 // 默认3个币种
	} else if c.RiskControl.RecoveryThreshold < 1 {
		c.RiskControl.RecoveryThreshold = 1 // 最小1个
	} else if c.RiskControl.RecoveryThreshold > monitorCount {
		c.RiskControl.RecoveryThreshold = monitorCount // 最大为监控币种数量
	}

	return nil
}
