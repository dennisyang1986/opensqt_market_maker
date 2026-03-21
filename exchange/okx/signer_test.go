package okx

import "testing"

func TestSignerSign(t *testing.T) {
	signer := NewSigner("key", "secret", "passphrase")
	got := signer.Sign("2024-01-01T00:00:00.000Z", "GET", "/api/v5/account/config", "")

	const want = "ZmVSP6HC6jqy0qqoYH8XTAw+ptYzrFqa5cFW5HOapZM="
	if got != want {
		t.Fatalf("Sign() = %q, want %q", got, want)
	}
}
