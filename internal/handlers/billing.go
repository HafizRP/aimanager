package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"9router-gateway/internal/billing"
	"9router-gateway/internal/models"
)

func (h *Handler) BillingPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	packages, err := h.repo.GetActivePackages(ctx)
	if err != nil {
		packages = []models.TokenPackage{}
	}

	var txs []models.Transaction
	var totalTxCount int

	if currentUser != nil && currentUser.IsAdmin() {
		txs, totalTxCount, _ = h.repo.GetAllTransactions(ctx, 50, 0)
	} else if currentUser != nil {
		txs, totalTxCount, _ = h.repo.GetTransactionsByUserID(ctx, currentUser.ID, 30, 0)
	}

	// Calculate total revenue for admin
	var totalRevenue int64
	if currentUser != nil && currentUser.IsAdmin() {
		for _, t := range txs {
			if t.Status == "settlement" || t.Status == "paid" || t.Status == "capture" {
				totalRevenue += t.AmountIDR
			}
		}
	}

	allUsers, _ := h.repo.GetAllUsers(ctx)

	midtransClient := billing.NewMidtransClient(h.cfg)

	h.render(w, r, "billing.html", "base.html", map[string]interface{}{
		"ActivePage":        "billing",
		"Packages":          packages,
		"Transactions":      txs,
		"TotalTxCount":      totalTxCount,
		"TotalRevenue":      totalRevenue,
		"AllUsers":          allUsers,
		"MidtransClientKey": h.cfg.MidtransClientKey,
		"MidtransSnapURL":   midtransClient.SnapURL(),
		"IsProduction":      h.cfg.MidtransIsProduction,
		"SuccessMsg":        r.URL.Query().Get("msg"),
		"ErrorMsg":          r.URL.Query().Get("error"),
	})
}

func (h *Handler) CheckoutSnap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)
	if currentUser == nil {
		http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
		return
	}

	_ = r.ParseForm()
	packageID := strings.TrimSpace(r.FormValue("package_id"))
	if packageID == "" {
		http.Error(w, `{"error":"package_id is required"}`, http.StatusBadRequest)
		return
	}

	pkg, err := h.repo.GetPackageByID(ctx, packageID)
	if err != nil || pkg == nil {
		http.Error(w, `{"error":"Package not found"}`, http.StatusNotFound)
		return
	}

	orderID := fmt.Sprintf("AIM-%s-%s", time.Now().Format("20060102-150405"), uuid.New().String()[:5])

	midtransClient := billing.NewMidtransClient(h.cfg)
	snapResp, err := midtransClient.CreateSnapTransaction(orderID, pkg.PriceIDR, pkg.Name, pkg.ID, currentUser.Name)
	if err != nil {
		slog.Error("Midtrans Snap transaction failed", "err", err)
		http.Error(w, fmt.Sprintf(`{"error":"Midtrans error: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Create Pending Transaction in Database
	tx := &models.Transaction{
		ID:          orderID,
		UserID:      currentUser.ID,
		PackageID:   pkg.ID,
		Tokens:      pkg.Tokens,
		AmountIDR:   pkg.PriceIDR,
		Status:      "pending",
		SnapToken:   snapResp.Token,
		SnapURL:     snapResp.RedirectURL,
	}
	_ = h.repo.CreateTransaction(ctx, tx)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"snap_token":   snapResp.Token,
		"redirect_url": snapResp.RedirectURL,
		"order_id":     orderID,
	})
}

func (h *Handler) MidtransWebhook(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	var payload billing.MidtransWebhookPayload
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	slog.Info("Received Midtrans Webhook",
		"order_id", payload.OrderID,
		"status", payload.TransactionStatus,
		"amount", payload.GrossAmount,
	)

	midtransClient := billing.NewMidtransClient(h.cfg)
	// If server key is configured with real key, verify signature
	if !strings.Contains(h.cfg.MidtransServerKey, "demo") && !midtransClient.VerifySignature(&payload) {
		slog.Warn("Midtrans webhook signature invalid", "order_id", payload.OrderID)
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	tx, err := h.repo.GetTransactionByID(ctx, payload.OrderID)
	if err != nil || tx == nil {
		slog.Warn("Midtrans webhook: transaction not found", "order_id", payload.OrderID)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ignored_unknown_order"}`))
		return
	}

	status := strings.ToLower(payload.TransactionStatus)
	isPaid := false

	if status == "settlement" {
		isPaid = true
	} else if status == "capture" {
		if payload.FraudStatus == "accept" || payload.FraudStatus == "" {
			isPaid = true
		}
	}

	if isPaid {
		// Only credit if not already processed as settlement
		if tx.Status != "settlement" && tx.Status != "paid" {
			_ = h.repo.CreditUserTokens(ctx, tx.UserID, tx.Tokens)
			_ = h.repo.UpdateTransactionStatus(ctx, tx.ID, "settlement", payload.PaymentType, payload.TransactionID)
			slog.Info("Successfully credited tokens from Midtrans payment!",
				"user_id", tx.UserID,
				"tokens", tx.Tokens,
				"order_id", tx.ID,
			)
		}
	} else if status == "expire" || status == "cancel" || status == "deny" {
		_ = h.repo.UpdateTransactionStatus(ctx, tx.ID, status, payload.PaymentType, payload.TransactionID)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func (h *Handler) ManualCreditTokens(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	ctx := r.Context()

	userID := strings.TrimSpace(r.FormValue("user_id"))
	tokensStr := strings.TrimSpace(r.FormValue("tokens"))
	tokens, _ := strconv.ParseInt(tokensStr, 10, 64)

	if userID == "" || tokens <= 0 {
		http.Redirect(w, r, "/billing?error=Invalid+user+or+token+amount", http.StatusSeeOther)
		return
	}

	user, err := h.repo.GetUserByID(ctx, userID)
	if err != nil || user == nil {
		http.Redirect(w, r, "/billing?error=User+not+found", http.StatusSeeOther)
		return
	}

	// Credit tokens
	_ = h.repo.CreditUserTokens(ctx, userID, tokens)

	// Record manual settlement transaction
	orderID := fmt.Sprintf("MANUAL-%s-%s", time.Now().Format("20060102-150405"), uuid.New().String()[:5])
	manualTx := &models.Transaction{
		ID:          orderID,
		UserID:      userID,
		PackageID:   "manual",
		Tokens:      tokens,
		AmountIDR:   0,
		Status:      "settlement",
		PaymentType: "manual_credit",
	}
	_ = h.repo.CreateTransaction(ctx, manualTx)

	msg := fmt.Sprintf("Successfully credited %d tokens to %s!", tokens, user.Name)
	http.Redirect(w, r, "/billing?msg="+msg, http.StatusSeeOther)
}
