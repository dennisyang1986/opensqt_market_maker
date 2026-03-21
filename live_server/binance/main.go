package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2"
	"github.com/adshao/go-binance/v2/futures"
	"github.com/gorilla/websocket"
	"gopkg.in/yaml.v3"
)

// Config holds the configuration from config.yaml
type Config struct {
	Binance struct {
		APIKey    string `yaml:"api_key"`
		SecretKey string `yaml:"secret_key"`
	} `yaml:"binance"`
	Trading struct {
		Symbol string `yaml:"symbol"`
	} `yaml:"trading"`
	Server struct {
		Port int `yaml:"live_port"`
	} `yaml:"server"`
}

// WebSocket Message Types
const (
	MsgTypeKline      = "kline"
	MsgTypeAccount    = "account"
	MsgTypeOrder      = "orders"
	MsgTypeTradeEvent = "trade_event"
)

// WSMessage is the standard message format sent to frontend
type WSMessage struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// Client wrapper with write mutex
type Client struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
}

func (c *Client) WriteJSON(v interface{}) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteJSON(v)
}

// Global Hub to manage clients
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan WSMessage
	register   chan *Client
	unregister chan *Client
	mu         sync.Mutex
}

var hub = Hub{
	broadcast:  make(chan WSMessage),
	register:   make(chan *Client),
	unregister: make(chan *Client),
	clients:    make(map[*Client]bool),
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Println("New client connected")
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.conn.Close()
			}
			h.mu.Unlock()
			log.Println("Client disconnected")
		case message := <-h.broadcast:
			h.mu.Lock()
			clientsList := make([]*Client, 0, len(h.clients))
			for client := range h.clients {
				clientsList = append(clientsList, client)
			}
			h.mu.Unlock()

			// Write to clients outside the lock
			for _, client := range clientsList {
				err := client.WriteJSON(message)
				if err != nil {
					log.Printf("Error writing to client: %v", err)
					h.mu.Lock()
					delete(h.clients, client)
					h.mu.Unlock()
					client.conn.Close()
				}
			}
		}
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all CORS for demo purposes
	},
}

func main() {
	// Load Config
	configFile := flag.String("config", "config.yaml", "path to config file")
	flag.Parse()

	data, err := os.ReadFile(*configFile)
	if err != nil {
		log.Fatalf("Error reading config file: %v", err)
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		log.Fatalf("Error parsing config file: %v", err)
	}

	if config.Trading.Symbol == "" {
		config.Trading.Symbol = "ETHUSDC"
	}
	// Default port if not in config
	if config.Server.Port == 0 {
		config.Server.Port = 9798
	}

	log.Printf("Starting Live Server for %s on port %d...", config.Trading.Symbol, config.Server.Port)

	// Start Hub
	go hub.run()

	// Initialize Binance Futures Client (U本位合约)
	futures.UseTestnet = false // 设置为 true 如果使用测试网
	client := binance.NewFuturesClient(config.Binance.APIKey, config.Binance.SecretKey)

	// Function to fetch and send initial account info and open orders (Futures)
	sendInitialData := func(wsClient *Client) {
		// Get futures account information
		account, err := client.NewGetAccountService().Do(context.Background())
		if err != nil {
			log.Printf("Error fetching futures account info: %v", err)
		} else {
			// Send balance info (Futures uses different structure)
			for _, asset := range account.Assets {
				if (asset.Asset == "USDT" || asset.Asset == "USDC") &&
					(asset.WalletBalance != "0" || asset.MarginBalance != "0") {
					wsClient.WriteJSON(WSMessage{
						Type: MsgTypeAccount,
						Data: map[string]interface{}{
							"asset":         asset.Asset,
							"free":          asset.AvailableBalance,
							"balance":       asset.WalletBalance,
							"marginBalance": asset.MarginBalance,
							"symbol":        strings.ToUpper(config.Trading.Symbol),
						},
					})
				}
			}

			// Send positions info
			for _, position := range account.Positions {
				if position.PositionAmt != "0" {
					wsClient.WriteJSON(WSMessage{
						Type: "position",
						Data: map[string]interface{}{
							"symbol":        position.Symbol,
							"amount":        position.PositionAmt,
							"entryPrice":    position.EntryPrice,
							"unrealizedPnL": position.UnrealizedProfit,
						},
					})
				}
			}
		}

		// Get open orders (Futures)
		orders, err := client.NewListOpenOrdersService().Symbol(config.Trading.Symbol).Do(context.Background())
		if err != nil {
			log.Printf("Error fetching futures open orders: %v", err)
		} else if len(orders) > 0 {
			var orderList []map[string]interface{}
			for _, order := range orders {
				orderList = append(orderList, map[string]interface{}{
					"id":     order.OrderID,
					"price":  order.Price,
					"side":   order.Side,
					"status": order.Status,
					"type":   order.Type,
					"symbol": strings.ToUpper(config.Trading.Symbol),
				})
			}
			wsClient.WriteJSON(WSMessage{
				Type: MsgTypeOrder,
				Data: orderList,
			})
		}
	}

	// Function to fetch and broadcast history (Futures)
	sendHistory := func(wsClient *Client) {
		klines, err := client.NewKlinesService().Symbol(config.Trading.Symbol).Interval("1m").Limit(100).Do(context.Background())
		if err != nil {
			log.Printf("Error fetching history: %v", err)
			return
		}

		var historyData []map[string]interface{}
		for _, k := range klines {
			historyData = append(historyData, map[string]interface{}{
				"time":   k.OpenTime / 1000,
				"open":   k.Open,
				"high":   k.High,
				"low":    k.Low,
				"close":  k.Close,
				"volume": k.Volume,
			})
		}

		// Send all history in one message
		wsClient.WriteJSON(WSMessage{
			Type: "history", // New message type
			Data: historyData,
		})
	}

	// 1. Monitor Kline (1m) - Futures
	go func() {
		wsKlineHandler := func(event *futures.WsKlineEvent) {
			kline := event.Kline
			// Broadcast Kline
			hub.broadcast <- WSMessage{
				Type: MsgTypeKline,
				Data: map[string]interface{}{
					"time":   kline.StartTime / 1000, // Seconds
					"open":   kline.Open,
					"high":   kline.High,
					"low":    kline.Low,
					"close":  kline.Close,
					"volume": kline.Volume,
				},
			}
		}
		errHandler := func(err error) {
			log.Println("Kline Stream Error:", err)
		}

		doneC, stopC, err := futures.WsKlineServe(config.Trading.Symbol, "1m", wsKlineHandler, errHandler)
		if err != nil {
			log.Println("Error connecting to Futures Kline stream:", err)
			return
		}
		defer func() { stopC <- struct{}{} }()
		<-doneC
	}()

	// 2. Monitor User Data (Orders/Trades) - Futures
	go func() {
		// Start Futures User Stream
		listenKey, err := client.NewStartUserStreamService().Do(context.Background())
		if err != nil {
			log.Printf("Error starting futures user stream (Check API Key permissions): %v", err)
			return
		}

		// KeepAlive User Stream every 30 mins
		go func() {
			ticker := time.NewTicker(30 * time.Minute)
			for range ticker.C {
				client.NewKeepaliveUserStreamService().ListenKey(listenKey).Do(context.Background())
			}
		}()

		wsHandler := func(event *futures.WsUserDataEvent) {
			switch event.Event {
			case futures.UserDataEventTypeOrderTradeUpdate:
				order := event.OrderTradeUpdate

				log.Printf("Futures Order Update: %s %s %s @ %s, Status: %s",
					order.Side, order.Type, order.Symbol, order.OriginalPrice, order.Status)

				// Broadcast ALL order updates (NEW, PARTIALLY_FILLED, FILLED, CANCELED, EXPIRED)
				// This allows frontend to remove price lines when orders are closed
				hub.broadcast <- WSMessage{
					Type: MsgTypeOrder,
					Data: map[string]interface{}{
						"id":     order.ID,
						"price":  order.OriginalPrice,
						"side":   order.Side,
						"status": order.Status,
						"type":   order.Type,
						"symbol": strings.ToUpper(config.Trading.Symbol),
					},
				}

				// Broadcast trade event when order is filled
				if order.ExecutionType == "TRADE" {
					side := "buy"
					if order.Side == "SELL" {
						side = "sell"
					}

					hub.broadcast <- WSMessage{
						Type: MsgTypeTradeEvent,
						Data: map[string]interface{}{
							"side":   side,
							"price":  order.AveragePrice,
							"amount": order.LastFilledQty,
							"symbol": order.Symbol,
							"time":   time.Now().Unix(),
						},
					}

					log.Printf("🔔 Futures Trade Executed: %s %s @ %s", side, order.LastFilledQty, order.AveragePrice)
				}

			case futures.UserDataEventTypeAccountUpdate:
				// Account balance update
				for _, balance := range event.AccountUpdate.Balances {
					if balance.Asset == "USDT" || balance.Asset == "USDC" {
						hub.broadcast <- WSMessage{
							Type: MsgTypeAccount,
							Data: map[string]interface{}{
								"asset":   balance.Asset,
								"free":    balance.Balance,
								"balance": balance.Balance,
								"symbol":  strings.ToUpper(config.Trading.Symbol),
							},
						}
					}
				}

				// Position update
				for _, position := range event.AccountUpdate.Positions {
					if position.Amount != "0" {
						hub.broadcast <- WSMessage{
							Type: "position",
							Data: map[string]interface{}{
								"symbol":        position.Symbol,
								"amount":        position.Amount,
								"entryPrice":    position.EntryPrice,
								"unrealizedPnL": position.UnrealizedPnL,
							},
						}
					}
				}
			}
		}

		errHandler := func(err error) {
			log.Println("Futures User Stream Error:", err)
		}

		doneC, stopC, err := futures.WsUserDataServe(listenKey, wsHandler, errHandler)
		if err != nil {
			log.Println("Error connecting to Futures User stream:", err)
			return
		}
		defer func() { stopC <- struct{}{} }()
		<-doneC
	}()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade error: %v", err)
			http.Error(w, "WebSocket upgrade failed", http.StatusBadRequest)
			return
		}
		client := &Client{conn: ws}

		// Register client
		hub.register <- client

		// Send initial data (account + open orders)
		go sendInitialData(client)

		// Send history asynchronously
		go sendHistory(client)

		// Read loop
		for {
			_, _, err := ws.ReadMessage()
			if err != nil {
				hub.unregister <- client
				break
			}
		}
	})
	log.Fatal(http.ListenAndServe(fmt.Sprintf("0.0.0.0:%d", config.Server.Port), nil))
}
