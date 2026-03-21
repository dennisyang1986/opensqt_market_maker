package okx

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"opensqt/logger"

	"github.com/gorilla/websocket"
)

type WebSocketManager struct {
	apiKey     string
	passphrase string
	signer     *Signer

	publicConn  *websocket.Conn
	privateConn *websocket.Conn
	publicMu    sync.RWMutex
	privateMu   sync.RWMutex

	priceCancel context.CancelFunc
	orderCancel context.CancelFunc
	priceWG     sync.WaitGroup
	orderWG     sync.WaitGroup

	latestPrice float64
	priceMu     sync.RWMutex
}

func NewWebSocketManager(apiKey, secretKey, passphrase string) *WebSocketManager {
	return &WebSocketManager{
		apiKey:     apiKey,
		passphrase: passphrase,
		signer:     NewSigner(apiKey, secretKey, passphrase),
	}
}

func (w *WebSocketManager) StartPriceStream(ctx context.Context, instID string, callback func(price float64)) error {
	if w.priceCancel != nil {
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	w.priceCancel = cancel
	w.priceWG.Add(1)
	go func() {
		defer w.priceWG.Done()
		w.runPublicLoop(runCtx, instID, callback)
	}()
	return nil
}

func (w *WebSocketManager) runPublicLoop(ctx context.Context, instID string, callback func(price float64)) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(okxPublicWSURL, nil)
		if err != nil {
			logger.Warn("WARN [OKX WS] public connect failed: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		w.publicMu.Lock()
		w.publicConn = conn
		w.publicMu.Unlock()

		subscribe := map[string]interface{}{
			"op": "subscribe",
			"args": []map[string]string{
				{"channel": "tickers", "instId": instID},
			},
		}
		if err := conn.WriteJSON(subscribe); err != nil {
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		w.readPublicMessages(ctx, conn, callback)

		w.publicMu.Lock()
		if w.publicConn == conn {
			w.publicConn = nil
		}
		w.publicMu.Unlock()
		conn.Close()
		time.Sleep(5 * time.Second)
	}
}

func (w *WebSocketManager) readPublicMessages(ctx context.Context, conn *websocket.Conn, callback func(price float64)) {
	conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		return nil
	})
	go w.keepAlive(ctx, conn, "public")

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}
		conn.SetReadDeadline(time.Now().Add(90 * time.Second))

		var payload struct {
			Arg struct {
				Channel string `json:"channel"`
			} `json:"arg"`
			Data []struct {
				Last string `json:"last"`
			} `json:"data"`
		}
		if err := json.Unmarshal(message, &payload); err != nil {
			continue
		}
		if payload.Arg.Channel != "tickers" || len(payload.Data) == 0 {
			continue
		}
		price := parseFloat(payload.Data[0].Last)
		if price <= 0 {
			continue
		}
		w.priceMu.Lock()
		w.latestPrice = price
		w.priceMu.Unlock()
		if callback != nil {
			callback(price)
		}
	}
}

func (w *WebSocketManager) StartOrderStream(ctx context.Context, instID string, contractValue float64, callback func(interface{})) error {
	if w.orderCancel != nil {
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	w.orderCancel = cancel
	w.orderWG.Add(1)
	go func() {
		defer w.orderWG.Done()
		w.runPrivateLoop(runCtx, instID, contractValue, callback)
	}()
	return nil
}

func (w *WebSocketManager) runPrivateLoop(ctx context.Context, instID string, contractValue float64, callback func(interface{})) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(okxPrivateWSURL, nil)
		if err != nil {
			logger.Warn("WARN [OKX WS] private connect failed: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		if err := w.loginPrivate(conn); err != nil {
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		w.privateMu.Lock()
		w.privateConn = conn
		w.privateMu.Unlock()

		subscribe := map[string]interface{}{
			"op": "subscribe",
			"args": []map[string]string{
				{"channel": "orders", "instType": okxInstTypeSwap},
			},
		}
		if err := conn.WriteJSON(subscribe); err != nil {
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		w.readPrivateMessages(ctx, conn, contractValue, callback)

		w.privateMu.Lock()
		if w.privateConn == conn {
			w.privateConn = nil
		}
		w.privateMu.Unlock()
		conn.Close()
		time.Sleep(5 * time.Second)
	}
}

func (w *WebSocketManager) loginPrivate(conn *websocket.Conn) error {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	login := map[string]interface{}{
		"op": "login",
		"args": []map[string]string{
			{
				"apiKey":     w.apiKey,
				"passphrase": w.passphrase,
				"timestamp":  timestamp,
				"sign":       w.signer.Sign(timestamp, "GET", "/users/self/verify", ""),
			},
		},
	}
	if err := conn.WriteJSON(login); err != nil {
		return err
	}

	_, message, err := conn.ReadMessage()
	if err != nil {
		return err
	}
	var payload struct {
		Event string `json:"event"`
		Code  string `json:"code"`
		Msg   string `json:"msg"`
	}
	if err := json.Unmarshal(message, &payload); err != nil {
		return err
	}
	if payload.Event == "login" || payload.Code == "0" {
		return nil
	}
	return fmt.Errorf("OKX websocket login failed: %s", payload.Msg)
}

func (w *WebSocketManager) readPrivateMessages(ctx context.Context, conn *websocket.Conn, contractValue float64, callback func(interface{})) {
	conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		return nil
	})
	go w.keepAlive(ctx, conn, "private")

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}
		conn.SetReadDeadline(time.Now().Add(90 * time.Second))

		var payload struct {
			Arg struct {
				Channel string `json:"channel"`
			} `json:"arg"`
			Data []struct {
				OrdID     string `json:"ordId"`
				ClOrdID   string `json:"clOrdId"`
				InstID    string `json:"instId"`
				Side      string `json:"side"`
				State     string `json:"state"`
				Px        string `json:"px"`
				Sz        string `json:"sz"`
				AccFillSz string `json:"accFillSz"`
				AvgPx     string `json:"avgPx"`
				UTime     string `json:"uTime"`
			} `json:"data"`
		}
		if err := json.Unmarshal(message, &payload); err != nil {
			continue
		}
		if payload.Arg.Channel != "orders" {
			continue
		}

		for _, item := range payload.Data {
			orderID, _ := strconv.ParseInt(item.OrdID, 10, 64)
			updateTime, _ := strconv.ParseInt(item.UTime, 10, 64)
			side := "BUY"
			if strings.EqualFold(item.Side, "sell") {
				side = "SELL"
			}
			generic := struct {
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
				OrderID:       orderID,
				ClientOrderID: item.ClOrdID,
				Symbol:        strings.ReplaceAll(strings.TrimSuffix(item.InstID, "-SWAP"), "-", ""),
				Side:          side,
				Type:          string(OrderTypeLimit),
				Status:        string(normalizeOrderState(item.State)),
				Price:         parseFloat(item.Px),
				Quantity:      parseFloat(item.Sz) * contractValue,
				ExecutedQty:   parseFloat(item.AccFillSz) * contractValue,
				AvgPrice:      parseFloat(item.AvgPx),
				UpdateTime:    updateTime,
			}
			if callback != nil {
				callback(generic)
			}
		}
	}
}

func (w *WebSocketManager) keepAlive(ctx context.Context, conn *websocket.Conn, connType string) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				logger.Warn("WARN [OKX WS %s] ping failed: %v", connType, err)
				return
			}
		}
	}
}

func (w *WebSocketManager) GetLatestPrice() float64 {
	w.priceMu.RLock()
	defer w.priceMu.RUnlock()
	return w.latestPrice
}

func (w *WebSocketManager) Stop() {
	if w.priceCancel != nil {
		w.priceCancel()
		w.priceCancel = nil
	}
	if w.orderCancel != nil {
		w.orderCancel()
		w.orderCancel = nil
	}

	w.publicMu.Lock()
	if w.publicConn != nil {
		w.publicConn.Close()
		w.publicConn = nil
	}
	w.publicMu.Unlock()

	w.privateMu.Lock()
	if w.privateConn != nil {
		w.privateConn.Close()
		w.privateConn = nil
	}
	w.privateMu.Unlock()

	w.priceWG.Wait()
	w.orderWG.Wait()
}
