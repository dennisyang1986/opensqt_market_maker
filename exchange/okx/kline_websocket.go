package okx

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"opensqt/logger"

	"github.com/gorilla/websocket"
)

type KlineWebSocketManager struct {
	conn   *websocket.Conn
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.RWMutex
}

func NewKlineWebSocketManager() *KlineWebSocketManager {
	return &KlineWebSocketManager{}
}

func (k *KlineWebSocketManager) Start(ctx context.Context, instIDs []string, interval string, callback func(candle interface{})) error {
	if len(instIDs) == 0 {
		return fmt.Errorf("no OKX instruments provided for kline stream")
	}
	if k.cancel != nil {
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	k.cancel = cancel
	k.wg.Add(1)
	go func() {
		defer k.wg.Done()
		k.runLoop(runCtx, instIDs, interval, callback)
	}()
	return nil
}

func (k *KlineWebSocketManager) runLoop(ctx context.Context, instIDs []string, interval string, callback func(candle interface{})) {
	channel := "candle" + normalizeKlineBar(interval)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(okxBusinessWSURL, nil)
		if err != nil {
			logger.Warn("WARN [OKX Kline WS] connect failed: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		k.mu.Lock()
		k.conn = conn
		k.mu.Unlock()

		args := make([]map[string]string, 0, len(instIDs))
		for _, instID := range instIDs {
			args = append(args, map[string]string{"channel": channel, "instId": instID})
		}
		subscribe := map[string]interface{}{"op": "subscribe", "args": args}
		if err := conn.WriteJSON(subscribe); err != nil {
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		k.readLoop(ctx, conn, callback)

		k.mu.Lock()
		if k.conn == conn {
			k.conn = nil
		}
		k.mu.Unlock()
		conn.Close()
		time.Sleep(5 * time.Second)
	}
}

func (k *KlineWebSocketManager) readLoop(ctx context.Context, conn *websocket.Conn, callback func(candle interface{})) {
	conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		return nil
	})

	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}()

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
				InstID  string `json:"instId"`
			} `json:"arg"`
			Data [][]string `json:"data"`
		}
		if err := json.Unmarshal(message, &payload); err != nil {
			continue
		}
		if payload.Arg.Channel == "" || len(payload.Data) == 0 {
			continue
		}

		for _, raw := range payload.Data {
			candle, err := parseOKXCandle(payload.Arg.InstID, raw)
			if err != nil {
				continue
			}
			if callback != nil {
				callback(candle)
			}
		}
	}
}

func (k *KlineWebSocketManager) Stop() {
	if k.cancel != nil {
		k.cancel()
		k.cancel = nil
	}
	k.mu.Lock()
	if k.conn != nil {
		k.conn.Close()
		k.conn = nil
	}
	k.mu.Unlock()
	k.wg.Wait()
}
