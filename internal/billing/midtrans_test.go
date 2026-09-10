package billing

import (
	"crypto/sha512"
	"encoding/hex"
	"testing"

	"9router-gateway/internal/config"
)

func TestVerifySignature(t *testing.T) {
	cfg := &config.Config{
		MidtransServerKey: "real-secret-server-key",
	}
	client := NewMidtransClient(cfg)

	orderID := "ORDER-12345"
	statusCode := "200"
	grossAmount := "100000.00"

	// Valid signature
	raw := orderID + statusCode + grossAmount + cfg.MidtransServerKey
	hasher := sha512.New()
	hasher.Write([]byte(raw))
	validSig := hex.EncodeToString(hasher.Sum(nil))

	payload := &MidtransWebhookPayload{
		OrderID:           orderID,
		StatusCode:        statusCode,
		GrossAmount:       grossAmount,
		SignatureKey:      validSig,
		TransactionStatus: "settlement",
	}

	if !client.VerifySignature(payload) {
		t.Errorf("expected signature to be valid")
	}

	// Invalid signature
	payloadBad := &MidtransWebhookPayload{
		OrderID:           orderID,
		StatusCode:        statusCode,
		GrossAmount:       grossAmount,
		SignatureKey:      "invalid-signature-hex",
		TransactionStatus: "settlement",
	}

	if client.VerifySignature(payloadBad) {
		t.Errorf("expected invalid signature to be rejected")
	}

	// Empty server key
	clientEmpty := NewMidtransClient(&config.Config{MidtransServerKey: ""})
	if clientEmpty.VerifySignature(payload) {
		t.Errorf("expected verification to fail when server key is empty")
	}

	// Nil payload
	if client.VerifySignature(nil) {
		t.Errorf("expected verification to fail for nil payload")
	}
}
