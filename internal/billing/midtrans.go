package billing

import (
	"bytes"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"9router-gateway/internal/config"
)

type MidtransClient struct {
	serverKey    string
	clientKey    string
	isProduction bool
	httpClient   *http.Client
}

func NewMidtransClient(cfg *config.Config) *MidtransClient {
	return &MidtransClient{
		serverKey:    cfg.MidtransServerKey,
		clientKey:    cfg.MidtransClientKey,
		isProduction: cfg.MidtransIsProduction,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (m *MidtransClient) SnapURL() string {
	if m.isProduction {
		return "https://app.midtrans.com/snap/snap.js"
	}
	return "https://app.sandbox.midtrans.com/snap/snap.js"
}

func (m *MidtransClient) snapAPIURL() string {
	if m.isProduction {
		return "https://app.midtrans.com/snap/v1/transactions"
	}
	return "https://app.sandbox.midtrans.com/snap/v1/transactions"
}

type SnapTransactionRequest struct {
	TransactionDetails struct {
		OrderID     string `json:"order_id"`
		GrossAmount int64  `json:"gross_amount"`
	} `json:"transaction_details"`
	ItemDetails []struct {
		ID       string `json:"id"`
		Price    int64  `json:"price"`
		Quantity int    `json:"quantity"`
		Name     string `json:"name"`
	} `json:"item_details"`
	CustomerDetails struct {
		FirstName string `json:"first_name"`
		Email     string `json:"email"`
	} `json:"customer_details"`
}

type SnapTransactionResponse struct {
	Token         string   `json:"token"`
	RedirectURL   string   `json:"redirect_url"`
	ErrorMessages []string `json:"error_messages,omitempty"`
}

func (m *MidtransClient) CreateSnapTransaction(orderID string, amount int64, packageName string, packageID string, userName string) (*SnapTransactionResponse, error) {
	reqBody := SnapTransactionRequest{}
	reqBody.TransactionDetails.OrderID = orderID
	reqBody.TransactionDetails.GrossAmount = amount

	reqBody.ItemDetails = []struct {
		ID       string `json:"id"`
		Price    int64  `json:"price"`
		Quantity int    `json:"quantity"`
		Name     string `json:"name"`
	}{
		{
			ID:       packageID,
			Price:    amount,
			Quantity: 1,
			Name:     packageName,
		},
	}

	reqBody.CustomerDetails.FirstName = userName
	reqBody.CustomerDetails.Email = userName + "@aimanager.local"

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequest(http.MethodPost, m.snapAPIURL(), bytes.NewBuffer(jsonBytes))
	if err != nil {
		return nil, err
	}

	// Basic Auth: base64(serverKey + ":")
	authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(m.serverKey+":"))
	httpReq.Header.Set("Authorization", authHeader)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := m.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to contact Midtrans API: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var snapResp SnapTransactionResponse
	if err := json.Unmarshal(respBytes, &snapResp); err != nil {
		return nil, fmt.Errorf("invalid response from Midtrans: %s", string(respBytes))
	}

	if len(snapResp.ErrorMessages) > 0 {
		return nil, fmt.Errorf("midtrans error: %s", strings.Join(snapResp.ErrorMessages, ", "))
	}

	return &snapResp, nil
}

type MidtransWebhookPayload struct {
	TransactionTime   string `json:"transaction_time"`
	TransactionStatus string `json:"transaction_status"` // "settlement", "capture", "pending", "expire", "cancel"
	TransactionID     string `json:"transaction_id"`
	StatusMessage     string `json:"status_message"`
	StatusCode        string `json:"status_code"`
	SignatureKey      string `json:"signature_key"`
	PaymentType       string `json:"payment_type"`
	OrderID           string `json:"order_id"`
	GrossAmount       string `json:"gross_amount"`
	FraudStatus       string `json:"fraud_status,omitempty"`
}

// VerifySignature validates SHA512(order_id + status_code + gross_amount + server_key)
func (m *MidtransClient) VerifySignature(payload *MidtransWebhookPayload) bool {
	raw := payload.OrderID + payload.StatusCode + payload.GrossAmount + m.serverKey
	hasher := sha512.New()
	hasher.Write([]byte(raw))
	expected := hex.EncodeToString(hasher.Sum(nil))

	return strings.EqualFold(expected, payload.SignatureKey)
}
