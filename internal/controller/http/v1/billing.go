package v1

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"

	"9router-gateway/internal/billing"
	"9router-gateway/internal/usecase"
)

// BillingPage renders the token package purchase page.
func (h *Handler) BillingPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	packages := h.billing.GetPackages(ctx)

	txs, totalTxCount := h.billing.GetTransactions(ctx, currentUser)

	// Calculate total revenue for admin
	var totalRevenue int64
	if currentUser != nil && currentUser.IsAdmin() {
		totalRevenue = h.billing.TotalRevenue(txs)
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

// CheckoutSnap starts a Midtrans Snap checkout and records a pending order.
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

	pkg, err := h.billing.GetPackage(ctx, packageID)
	if err != nil {
		http.Error(w, `{"error":"Package not found"}`, http.StatusNotFound)
		return
	}

	orderID := usecase.NewOrderID("AIM")
	midtransClient := billing.NewMidtransClient(h.cfg)
	snapResp, err := midtransClient.CreateSnapTransaction(
		orderID,
		pkg.PriceIDR, pkg.Name, pkg.ID, currentUser.Name,
	)
	if err != nil {
		log.Error().Err(err).Msg("Midtrans Snap transaction failed")
		http.Error(w, fmt.Sprintf(`{"error":"Midtrans error: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// Create Pending Transaction in Database via use case
	tx, err := h.billing.CreateOrder(ctx, currentUser, pkg, orderID, snapResp.Token, snapResp.RedirectURL)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create pending transaction")
		http.Error(w, `{"error":"Failed to create transaction"}`, http.StatusInternalServerError)
		return
	}
	// The order ID placeholder above isn't used; CreateOrder generates the real one.

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"snap_token":   snapResp.Token,
		"redirect_url": snapResp.RedirectURL,
		"order_id":     tx.ID,
	})
}

// MidtransWebhook handles Midtrans payment notifications (idempotent).
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

	log.Info().
		Str("order_id", payload.OrderID).
		Str("status", payload.TransactionStatus).
		Str("amount", payload.GrossAmount).
		Msg("Received Midtrans Webhook")

	midtransClient := billing.NewMidtransClient(h.cfg)
	if h.cfg.MidtransServerKey != "" && !midtransClient.VerifySignature(&payload) {
		log.Warn().Str("order_id", payload.OrderID).Msg("Midtrans webhook signature invalid")
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()
	tx, err := h.billing.GetTransaction(ctx, payload.OrderID)
	if err != nil || tx == nil {
		log.Warn().Str("order_id", payload.OrderID).Msg("Midtrans webhook: transaction not found")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ignored_unknown_order"}`))
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
		if err := h.billing.MarkPaid(ctx, tx, payload.PaymentType, payload.TransactionID); err != nil {
			log.Error().Err(err).Msg("Failed to credit tokens for Midtrans payment")
		} else {
			log.Info().
				Str("user_id", tx.UserID).
				Int64("tokens", tx.Tokens).
				Str("order_id", tx.ID).
				Msg("Successfully credited tokens from Midtrans payment!")
		}
	} else if status == "expire" || status == "cancel" || status == "deny" {
		_ = h.billing.MarkFailed(ctx, tx, status, payload.PaymentType, payload.TransactionID)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// ManualCreditTokens credits tokens to a user manually (admin).
func (h *Handler) ManualCreditTokens(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	ctx := r.Context()

	userID := strings.TrimSpace(r.FormValue("user_id"))
	tokensStr := strings.TrimSpace(r.FormValue("tokens"))
	tokens, _ := strconv.ParseInt(tokensStr, 10, 64)

	err := h.billing.ManualCredit(ctx, userID, tokens)
	if err != nil {
		http.Redirect(w, r, "/billing?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}

	msg := fmt.Sprintf("Successfully credited %d tokens!", tokens)
	http.Redirect(w, r, "/billing?msg="+url.QueryEscape(msg), http.StatusSeeOther)
}