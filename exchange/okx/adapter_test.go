package okx

import "testing"

func TestConvertToOKXSwapInstID(t *testing.T) {
	tests := []struct {
		symbol string
		want   string
	}{
		{symbol: "ETHUSDT", want: "ETH-USDT-SWAP"},
		{symbol: "BTCUSDT", want: "BTC-USDT-SWAP"},
		{symbol: "SOLUSDT", want: "SOL-USDT-SWAP"},
	}

	for _, tt := range tests {
		got := convertToOKXSwapInstID(tt.symbol)
		if got != tt.want {
			t.Fatalf("convertToOKXSwapInstID(%q) = %q, want %q", tt.symbol, got, tt.want)
		}
	}
}

func TestMapOrderIntentForLongShortMode(t *testing.T) {
	tests := []struct {
		name        string
		side        Side
		reduceOnly  bool
		wantSide    string
		wantPosSide string
	}{
		{name: "open long", side: SideBuy, reduceOnly: false, wantSide: "buy", wantPosSide: "long"},
		{name: "close long", side: SideSell, reduceOnly: true, wantSide: "sell", wantPosSide: "long"},
		{name: "open short", side: SideSell, reduceOnly: false, wantSide: "sell", wantPosSide: "short"},
		{name: "close short", side: SideBuy, reduceOnly: true, wantSide: "buy", wantPosSide: "short"},
	}

	for _, tt := range tests {
		gotSide, gotPosSide, err := mapOrderIntent(tt.side, tt.reduceOnly)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.name, err)
		}
		if gotSide != tt.wantSide || gotPosSide != tt.wantPosSide {
			t.Fatalf("%s: got (%s, %s), want (%s, %s)", tt.name, gotSide, gotPosSide, tt.wantSide, tt.wantPosSide)
		}
	}
}

func TestNormalizeOrderState(t *testing.T) {
	tests := []struct {
		state string
		want  OrderStatus
	}{
		{state: "live", want: OrderStatusNew},
		{state: "partially_filled", want: OrderStatusPartiallyFilled},
		{state: "filled", want: OrderStatusFilled},
		{state: "canceled", want: OrderStatusCanceled},
	}

	for _, tt := range tests {
		if got := normalizeOrderState(tt.state); got != tt.want {
			t.Fatalf("normalizeOrderState(%q) = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestContractsFromBaseQuantity(t *testing.T) {
	tests := []struct {
		name       string
		baseQty    float64
		contractSz float64
		step       float64
		want       string
		wantErr    bool
	}{
		{name: "round down to whole contract", baseQty: 0.123, contractSz: 0.01, step: 1, want: "12"},
		{name: "support decimal contract steps", baseQty: 0.125, contractSz: 0.01, step: 0.1, want: "12.5"},
		{name: "reject quantity below one contract", baseQty: 0.009, contractSz: 0.01, step: 1, wantErr: true},
	}

	for _, tt := range tests {
		got, err := contractsFromBaseQuantity(tt.baseQty, tt.contractSz, tt.step)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("%s: expected error, got nil", tt.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tt.name, err)
		}
		if got != tt.want {
			t.Fatalf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}
