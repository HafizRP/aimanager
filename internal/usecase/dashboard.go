package usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"9router-gateway/internal/models"
)

// DashboardService provides role-scoped statistics for dashboards.
type DashboardService struct {
	store Store
}

// NewDashboardService builds a DashboardService.
func NewDashboardService(store Store) *DashboardService {
	return &DashboardService{store: store}
}

// GetStats returns dashboard stats scoped by user role.
func (s *DashboardService) GetStats(ctx context.Context, user *models.User, timeframe string) (*models.DashboardStats, error) {
	if user == nil {
		return nil, fmt.Errorf("no user in context")
	}
	if strings.TrimSpace(timeframe) == "" {
		timeframe = "1d"
	}
	if user.IsAdmin() {
		return s.store.GetDashboardStats(ctx, timeframe)
	}
	return s.store.GetUserDashboardStats(ctx, user.ID, timeframe)
}

// BillingService wraps token package purchases and Midtrans webhook processing.
type BillingService struct {
	store Store
}

// NewBillingService builds a BillingService.
func NewBillingService(store Store) *BillingService {
	return &BillingService{store: store}
}

// GetPackages returns active token packages.
func (s *BillingService) GetPackages(ctx context.Context) []models.TokenPackage {
	pkgs, err := s.store.GetActivePackages(ctx)
	if err != nil {
		return []models.TokenPackage{}
	}
	return pkgs
}

// GetTransactions returns transactions scoped by role.
func (s *BillingService) GetTransactions(ctx context.Context, user *models.User) ([]models.Transaction, int) {
	if user == nil {
		return []models.Transaction{}, 0
	}
	if user.IsAdmin() {
		txs, total, err := s.store.GetAllTransactions(ctx, 50, 0)
		if err != nil {
			return []models.Transaction{}, 0
		}
		return txs, total
	}
	txs, total, err := s.store.GetTransactionsByUserID(ctx, user.ID, 30, 0)
	if err != nil {
		return []models.Transaction{}, 0
	}
	return txs, total
}

// TotalRevenue sums settled transaction amounts (admin only).
func (s *BillingService) TotalRevenue(txs []models.Transaction) int64 {
	var total int64
	for _, t := range txs {
		if t.Status == "settlement" || t.Status == "paid" || t.Status == "capture" {
			total += t.AmountIDR
		}
	}
	return total
}

// GetPackage returns a package by ID.
func (s *BillingService) GetPackage(ctx context.Context, id string) (*models.TokenPackage, error) {
	pkg, err := s.store.GetPackageByID(ctx, id)
	if err != nil || pkg == nil {
		return nil, fmt.Errorf("package not found")
	}
	return pkg, nil
}

// NewOrderID generates a unique order ID with the given prefix.
func NewOrderID(prefix string) string {
	return fmt.Sprintf("%s-%s-%s", prefix, time.Now().Format("20060102-150405"), uuid.New().String()[:5])
}

// CreateOrder records a pending transaction for a Snap checkout.
func (s *BillingService) CreateOrder(ctx context.Context, user *models.User, pkg *models.TokenPackage, orderID, snapToken, snapURL string) (*models.Transaction, error) {
	if orderID == "" {
		orderID = NewOrderID("AIM")
	}
	tx := &models.Transaction{
		ID:        orderID,
		UserID:    user.ID,
		PackageID: pkg.ID,
		Tokens:    pkg.Tokens,
		AmountIDR: pkg.PriceIDR,
		Status:    "pending",
		SnapToken: snapToken,
		SnapURL:   snapURL,
	}
	if err := s.store.CreateTransaction(ctx, tx); err != nil {
		return nil, err
	}
	return tx, nil
}

// GetTransaction returns a transaction by order ID.
func (s *BillingService) GetTransaction(ctx context.Context, orderID string) (*models.Transaction, error) {
	tx, err := s.store.GetTransactionByID(ctx, orderID)
	if err != nil || tx == nil {
		return nil, fmt.Errorf("transaction not found")
	}
	return tx, nil
}

// MarkPaid credits tokens and marks the transaction settled.
func (s *BillingService) MarkPaid(ctx context.Context, tx *models.Transaction, paymentType, midtransTxID string) error {
	if tx.Status == "settlement" || tx.Status == "paid" {
		return nil
	}
	if err := s.store.CreditUserTokens(ctx, tx.UserID, tx.Tokens); err != nil {
		return err
	}
	return s.store.UpdateTransactionStatus(ctx, tx.ID, "settlement", paymentType, midtransTxID)
}

// MarkFailed records a failed/expired/cancelled transaction.
func (s *BillingService) MarkFailed(ctx context.Context, tx *models.Transaction, status, paymentType, midtransTxID string) error {
	return s.store.UpdateTransactionStatus(ctx, tx.ID, status, paymentType, midtransTxID)
}

// ManualCredit credits tokens and records a manual settlement.
func (s *BillingService) ManualCredit(ctx context.Context, userID string, tokens int64) error {
	if userID == "" || tokens <= 0 || tokens > 1000000000000 {
		return fmt.Errorf("invalid user or token amount")
	}
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil || user == nil {
		return fmt.Errorf("user not found")
	}
	if err := s.store.CreditUserTokens(ctx, userID, tokens); err != nil {
		return err
	}
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
	return s.store.CreateTransaction(ctx, manualTx)
}