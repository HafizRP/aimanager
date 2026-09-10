package handlers

import (
	"strings"
	"testing"

	"9router-gateway/internal/models"
)

func TestGenerateSecureAPIKey(t *testing.T) {
	keyDefault := GenerateSecureAPIKey("")
	if !strings.HasPrefix(keyDefault, "sk-gw-") {
		t.Errorf("expected prefix 'sk-gw-', got '%s'", keyDefault)
	}
	if len(keyDefault) < 20 {
		t.Errorf("expected key length at least 20, got %d", len(keyDefault))
	}

	keyAdmin := GenerateSecureAPIKey("sk-gw-admin-")
	if !strings.HasPrefix(keyAdmin, "sk-gw-admin-") {
		t.Errorf("expected prefix 'sk-gw-admin-', got '%s'", keyAdmin)
	}

	// Verify uniqueness
	key2 := GenerateSecureAPIKey("")
	if keyDefault == key2 {
		t.Errorf("two independently generated keys should not collide")
	}
}

func TestPasswordHashing(t *testing.T) {
	password := "SecretP@ssword123"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if hash == password {
		t.Fatal("password hash should not equal plain text password")
	}

	// Verify correct password matches
	if !CheckPasswordHash(password, hash) {
		t.Fatal("expected CheckPasswordHash to succeed for correct password")
	}

	// Verify incorrect password fails
	if CheckPasswordHash("WrongPassword", hash) {
		t.Fatal("expected CheckPasswordHash to fail for wrong password")
	}

	// Empty hash check
	if CheckPasswordHash(password, "") {
		t.Fatal("expected CheckPasswordHash to fail for empty hash")
	}
}

func TestUserVirtualMethods(t *testing.T) {
	adminUser := &models.User{Role: "admin"}
	if !adminUser.IsAdmin() {
		t.Error("expected IsAdmin() true for role 'admin'")
	}

	superUser := &models.User{Role: "superadmin"}
	if !superUser.IsAdmin() {
		t.Error("expected IsAdmin() true for role 'superadmin'")
	}

	regularUser := &models.User{Role: "user"}
	if regularUser.IsAdmin() {
		t.Error("expected IsAdmin() false for role 'user'")
	}

	// Quota calculations
	unlimitedUser := &models.User{TokenQuota: 0, TokensUsed: 500}
	if pct := unlimitedUser.QuotaPercent(); pct != 0 {
		t.Errorf("expected 0 for unlimited user, got %f", pct)
	}

	quotaUser := &models.User{TokenQuota: 1000, TokensUsed: 250}
	if pct := quotaUser.QuotaPercent(); pct != 25.0 {
		t.Errorf("expected 25.0%%, got %f", pct)
	}

	overQuotaUser := &models.User{TokenQuota: 1000, TokensUsed: 1500}
	if pct := overQuotaUser.QuotaPercent(); pct != 100.0 {
		t.Errorf("expected capped 100.0%%, got %f", pct)
	}
}
