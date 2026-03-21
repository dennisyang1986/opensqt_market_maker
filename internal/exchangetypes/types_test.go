package exchangetypes

import "testing"

func TestSharedOrderTypesExposeExpectedConstants(t *testing.T) {
	if SideBuy != "BUY" {
		t.Fatalf("SideBuy = %q, want %q", SideBuy, "BUY")
	}
	if SideSell != "SELL" {
		t.Fatalf("SideSell = %q, want %q", SideSell, "SELL")
	}
	if OrderTypeLimit != "LIMIT" {
		t.Fatalf("OrderTypeLimit = %q, want %q", OrderTypeLimit, "LIMIT")
	}
	if TimeInForceGTX != "GTX" {
		t.Fatalf("TimeInForceGTX = %q, want %q", TimeInForceGTX, "GTX")
	}
}

func TestSharedOrderCarriesCommonLifecycleFields(t *testing.T) {
	order := Order{
		OrderID:       1,
		ClientOrderID: "abc",
		Symbol:        "ETHUSDT",
		Side:          SideBuy,
		Type:          OrderTypeLimit,
		Status:        OrderStatusNew,
	}

	if order.OrderID != 1 || order.ClientOrderID != "abc" || order.Symbol != "ETHUSDT" {
		t.Fatalf("unexpected shared order fields: %+v", order)
	}
}
