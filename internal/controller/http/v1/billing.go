package v1

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"9router-gateway/internal/billing"
	"9router-gateway/internal/entity"
	"9router-gateway/internal/usecase"
)

// BillingPage renders the token package purchase page.
func (h *Handler) BillingPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)

	packages := h.billing.GetPackages(ctx)

	txs, totalTxCount := h.billing.GetTransactions(ctx, currentUser)

	// Calculate statistics
	var totalRevenue int64
	var settledTxCount, pendingTxCount int
	var totalTokensCredited int64
	for _, t := range txs {
		if t.Status == "settlement" || t.Status == "paid" || t.Status == "capture" {
			if currentUser != nil && currentUser.IsAdmin() {
				totalRevenue += t.AmountIDR
			}
			settledTxCount++
			totalTokensCredited += t.Tokens
		} else if t.Status == "pending" {
			pendingTxCount++
		}
	}

	allUsers, _ := h.repo.GetAllUsers(ctx)

	midtransClient := billing.NewMidtransClient(h.cfg)

	h.render(w, r, "billing.html", "base.html", map[string]interface{}{
		"ActivePage":          "billing",
		"Packages":            packages,
		"Transactions":        txs,
		"TotalTxCount":        totalTxCount,
		"TotalRevenue":        totalRevenue,
		"SettledTxCount":      settledTxCount,
		"PendingTxCount":      pendingTxCount,
		"TotalTokensCredited": totalTokensCredited,
		"AllUsers":            allUsers,
		"MidtransClientKey":   h.cfg.MidtransClientKey,
		"MidtransSnapURL":     midtransClient.SnapURL(),
		"IsProduction":        h.cfg.MidtransIsProduction,
		"SuccessMsg":          r.URL.Query().Get("msg"),
		"ErrorMsg":            r.URL.Query().Get("error"),
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

// ExportTransactions exports billing transactions as CSV or JSON file.
func (h *Handler) ExportTransactions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser := GetUserFromContext(ctx)
	if currentUser == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var txs []entity.Transaction
	var err error

	filterUserID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if currentUser.IsAdmin() {
		if filterUserID != "" {
			txs, _, err = h.repo.GetTransactionsByUserID(ctx, filterUserID, 10000, 0)
		} else {
			txs, _, err = h.repo.GetAllTransactions(ctx, 10000, 0)
		}
	} else {
		txs, _, err = h.repo.GetTransactionsByUserID(ctx, currentUser.ID, 10000, 0)
		for i := range txs {
			if txs[i].UserName == "" || txs[i].UserName == "Unknown" {
				txs[i].UserName = currentUser.Name
			}
		}
	}

	if err != nil {
		http.Error(w, "Failed to retrieve transactions", http.StatusInternalServerError)
		return
	}

	statusFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	var filtered []entity.Transaction
	for _, t := range txs {
		if statusFilter != "" && statusFilter != "all" {
			st := strings.ToLower(t.Status)
			switch statusFilter {
			case "settled", "paid":
				if st != "settlement" && st != "paid" && st != "capture" {
					continue
				}
			case "pending":
				if st != "pending" {
					continue
				}
			case "expired", "failed":
				if st != "expire" && st != "cancel" && st != "deny" {
					continue
				}
			case "manual":
				if st != "manual" && t.PackageID != "pkg_manual" {
					continue
				}
			default:
				if st != statusFilter {
					continue
				}
			}
		}
		filtered = append(filtered, t)
	}

	format := strings.ToLower(r.URL.Query().Get("format"))
	if format != "json" {
		format = "csv"
	}

	timestamp := time.Now().Format("20060102_150405")

	if format == "json" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=transactions_%s.json", timestamp))
		_ = json.NewEncoder(w).Encode(filtered)
		return
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=transactions_%s.csv", timestamp))

	writer := csv.NewWriter(w)
	defer writer.Flush()

	_ = writer.Write([]string{
		"Order_ID", "Created_At_WIB", "User_Name", "User_ID", "Package_ID",
		"Tokens_Added", "Amount_IDR", "Payment_Type", "Status", "Midtrans_Tx_ID",
		"Paid_At_WIB", "Snap_Token",
	})

	for _, t := range filtered {
		createdAtWIB := ""
		if !t.CreatedAt.IsZero() {
			createdAtWIB = t.CreatedAt.Add(7 * time.Hour).Format("2006-01-02 15:04:05")
		}
		paidAtWIB := ""
		if t.PaidAt != nil && !t.PaidAt.IsZero() {
			paidAtWIB = t.PaidAt.Add(7 * time.Hour).Format("2006-01-02 15:04:05")
		}

		_ = writer.Write([]string{
			t.ID,
			createdAtWIB,
			t.UserName,
			t.UserID,
			t.PackageID,
			strconv.FormatInt(t.Tokens, 10),
			strconv.FormatInt(t.AmountIDR, 10),
			t.PaymentType,
			t.Status,
			t.MidtransTxID,
			paidAtWIB,
			t.SnapToken,
		})
	}
}