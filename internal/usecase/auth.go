package usecase

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"9router-gateway/internal/entity"
)

var (
	// ErrNotFound is returned when a requested entity does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict is returned when a uniqueness constraint is violated.
	ErrConflict = errors.New("conflict")
	// ErrPermission is returned when the actor lacks the required role/ownership.
	ErrPermission = errors.New("permission denied")
)

// AuthInput carries credentials submitted via the login form.
type AuthInput struct {
	Username string
	Password string
}

// LoginResult describes a successful login.
type LoginResult struct {
	User  *entity.User
	Token string
	// ExpiresAt is the session cookie expiry.
	ExpiresAt time.Time
}

// AuthService implements the authentication and session use cases:
// login, session lifecycle, CSRF signing, and password verification.
type AuthService struct {
	store         Store
	sessionSecret string
	adminUsername string
	adminPassword string
	sessionTTL    time.Duration
}

// NewAuthService builds an AuthService.
func NewAuthService(store Store, sessionSecret, adminUsername, adminPassword string) *AuthService {
	return &AuthService{
		store:         store,
		sessionSecret: sessionSecret,
		adminUsername: adminUsername,
		adminPassword: adminPassword,
		sessionTTL:    7 * 24 * time.Hour,
	}
}

// Authenticate validates username/password, records failed attempts, clears them
// on success, and returns the user (session token is created by the caller).
func (s *AuthService) Authenticate(ctx context.Context, in AuthInput, clientIP string) (*entity.User, error) {
	recentFails, _ := s.store.GetRecentLoginAttempts(ctx, clientIP, 15)
	if recentFails >= 5 {
		return nil, fmt.Errorf("too many failed login attempts. Please wait 15 minutes")
	}

	if strings.TrimSpace(in.Username) == "" || strings.TrimSpace(in.Password) == "" {
		return nil, fmt.Errorf("username and password are required")
	}

	user, err := s.store.GetUserByUsername(ctx, strings.TrimSpace(in.Username))
	if err != nil || user == nil {
		_ = s.store.RecordLoginAttempt(ctx, clientIP)
		return nil, fmt.Errorf("invalid username or password")
	}
	if !user.IsActive {
		return nil, fmt.Errorf("account is suspended. Please contact administrator")
	}
	if !CheckPasswordHash(in.Password, user.PasswordHash) {
		_ = s.store.RecordLoginAttempt(ctx, clientIP)
		return nil, fmt.Errorf("invalid username or password")
	}

	_ = s.store.ClearLoginAttempts(ctx, clientIP)
	_ = s.store.UpdateUserLastLogin(ctx, user.ID)
	return user, nil
}

// StartSession creates a server-side session row and returns its token.
func (s *AuthService) StartSession(ctx context.Context, userID string) (string, time.Time, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", time.Time{}, err
	}
	token := hex.EncodeToString(tokenBytes)
	expiresAt := time.Now().Add(s.sessionTTL)
	if err := s.store.CreateSession(ctx, token, userID, expiresAt); err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

// GetSessionUser resolves a session token to a user.
// Remove the unused variable warning on GetSessionUser (user is used below).
func (s *AuthService) GetSessionUser(ctx context.Context, token string) (*entity.User, error) {
	user, err := s.store.GetSessionUser(ctx, token)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.IsActive {
		return nil, ErrNotFound
	}
	return user, nil
}

// DeleteSession removes the session row for a token.
func (s *AuthService) DeleteSession(ctx context.Context, token string) error {
	return s.store.DeleteSession(ctx, token)
}

// SignSession computes an HMAC signature of data using the session secret (used for CSRF tokens).
func (s *AuthService) SignSession(data string) string {
	return SignSession(s.sessionSecret, data)
}

// SignSession computes hex(hmac-sha256(secret, data)) using the raw session secret.
func SignSession(secret, data string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifySessionPassword allows the legacy admin password to act as the current password.
func (s *AuthService) VerifySessionPassword(user *entity.User, candidate string) bool {
	if CheckPasswordHash(candidate, user.PasswordHash) {
		return true
	}
	if user.Username == "admin" || user.Username == s.adminUsername {
		return candidate == s.adminPassword
	}
	return false
}

// ---- Key generation & password hashing helpers ----

// GenerateSecureAPIKey creates a random prefixed API key.
func GenerateSecureAPIKey(prefix string) string {
	if prefix == "" {
		prefix = "sk-gw-"
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// GenerateRandomPassword creates a random alphanumeric hex-like password of the given length.
func GenerateRandomPassword(length int) string {
	b := make([]byte, length/2+1)
	_, _ = rand.Read(b)
	str := hex.EncodeToString(b)
	if len(str) > length {
		return str[:length]
	}
	return str
}

// HashPassword hashes a plaintext password with bcrypt cost 12.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(bytes), err
}

// CheckPasswordHash compares a plaintext password against a bcrypt hash.
func CheckPasswordHash(password, hash string) bool {
	if hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// ---- User use cases ----

// UserService implements user management use cases: create, update, delete,
// password resets, status toggling and usage reset.
type UserService struct {
	store Store
	sync  KeySyncer
}

// KeySyncer pushes key state changes to 9router Core.
type KeySyncer interface {
	SyncKey(key *entity.APIKey, userName string) error
	ToggleKey(keyID string, isActive bool) error
	DeleteKey(keyID string) error
	BackfillAll(keys []entity.APIKey) error
	UpdateDBPath(path string)
}

// NewUserService builds a UserService.
func NewUserService(store Store, sync KeySyncer) *UserService {
	return &UserService{store: store, sync: sync}
}

// CreateUserInput is the parsed payload for creating a user.
type CreateUserInput struct {
	Name          string
	Username      string
	Password      string
	Role          string
	TokenQuota    int64
	AllowedModels string
	CreateKey     bool
}

// CreateUser creates the user, optionally generating an initial key synced to 9router.
// It returns the user and (if generated) the plaintext password/key for display.
func (s *UserService) CreateUser(ctx context.Context, in CreateUserInput) (*entity.User, string, string, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, "", "", fmt.Errorf("user name cannot be empty")
	}
	if len(name) > 100 {
		name = name[:100]
	}

	username := strings.ToLower(strings.TrimSpace(in.Username))
	if username == "" {
		username = nonAlphaNumericRegex.ReplaceAllString(strings.ToLower(name), "")
		if username == "" {
			username = "user" + GenerateRandomPassword(4)
		}
	}
	if !validUsernameRegex.MatchString(username) {
		return nil, "", "", fmt.Errorf("username must be 3-32 characters and contain only letters, numbers, hyphens, or underscores")
	}
	if existing, _ := s.store.GetUserByUsername(ctx, username); existing != nil {
		return nil, "", "", fmt.Errorf("username '%s' is already taken", username)
	}

	password := strings.TrimSpace(in.Password)
	if password != "" && len(password) < 6 {
		return nil, "", "", fmt.Errorf("password must be at least 6 characters")
	}
	if password == "" {
		password = GenerateRandomPassword(8)
	}

	passwordHash, err := HashPassword(password)
	if err != nil {
		return nil, "", "", fmt.Errorf("failed to hash password")
	}

	role := strings.TrimSpace(in.Role)
	if role != "admin" {
		role = "user"
	}

	quota := in.TokenQuota
	if quota < 0 {
		quota = 0
	}

	allowedModelsJSON := strings.TrimSpace(in.AllowedModels)
	if allowedModelsJSON == "" {
		allowedModelsJSON = `["*"]`
	}

	user := &entity.User{
		ID:            uuid.New().String(),
		Username:      username,
		Name:          name,
		PasswordHash:  passwordHash,
		Role:          role,
		TokenQuota:    quota,
		TokensUsed:    0,
		AllowedModels: allowedModelsJSON,
		IsActive:      true,
	}

	if err := s.store.CreateUser(ctx, user); err != nil {
		return nil, "", "", err
	}

	var generatedKey string
	if in.CreateKey {
		generatedKey = GenerateSecureAPIKey("sk-gw-")
		apiKey := &entity.APIKey{
			ID:       uuid.New().String(),
			UserID:   user.ID,
			Key:      generatedKey,
			Name:     "Initial Key",
			IsActive: true,
		}
		if err := s.store.CreateAPIKey(ctx, apiKey); err != nil {
			return nil, "", "", err
		}
		if s.sync != nil {
			_ = s.sync.SyncKey(apiKey, user.Name)
		}
	}

	return user, password, generatedKey, nil
}

// UpdateUserInput is the parsed payload for updating a user.
type UpdateUserInput struct {
	ID            string
	Name          string
	Username      string
	Role          string
	TokenQuota    int64
	AllowedModels string
	IsActive      bool
}

// UpdateUser applies allowed profile updates.
func (s *UserService) UpdateUser(ctx context.Context, in UpdateUserInput) (*entity.User, error) {
	user, err := s.store.GetUserByID(ctx, in.ID)
	if err != nil {
		return nil, ErrNotFound
	}

	if name := strings.TrimSpace(in.Name); name != "" {
		if len(name) > 100 {
			name = name[:100]
		}
		user.Name = name
	}

	if username := strings.ToLower(strings.TrimSpace(in.Username)); username != "" && username != user.Username {
		if !validUsernameRegex.MatchString(username) {
			return nil, fmt.Errorf("username must be 3-32 characters and contain only letters, numbers, hyphens, or underscores")
		}
		if existing, _ := s.store.GetUserByUsername(ctx, username); existing != nil && existing.ID != user.ID {
			return nil, fmt.Errorf("username '%s' is already taken", username)
		}
		user.Username = username
	}

	if in.Role == "admin" || in.Role == "user" {
		user.Role = in.Role
	}

	if in.TokenQuota < 0 {
		in.TokenQuota = 0
	}
	user.TokenQuota = in.TokenQuota

	if strings.TrimSpace(in.AllowedModels) != "" {
		user.AllowedModels = in.AllowedModels
	}

	user.IsActive = in.IsActive

	if err := s.store.UpdateUser(ctx, user); err != nil {
		return nil, err
	}
	return user, nil
}

// ResetPassword resets a user's password, returning the new plaintext password.
func (s *UserService) ResetPassword(ctx context.Context, userID, newPassword string) (string, error) {
	if _, err := s.store.GetUserByID(ctx, userID); err != nil {
		return "", ErrNotFound
	}

	newPassword = strings.TrimSpace(newPassword)
	if newPassword != "" && len(newPassword) < 6 {
		return "", fmt.Errorf("new password must be at least 6 characters")
	}
	if newPassword == "" {
		newPassword = GenerateRandomPassword(8)
	}

	hash, err := HashPassword(newPassword)
	if err != nil {
		return "", fmt.Errorf("failed to hash password")
	}
	if err := s.store.UpdateUserPassword(ctx, userID, hash); err != nil {
		return "", err
	}
	return newPassword, nil
}

// ToggleUserStatus flips a user's active flag and mirrors the change to 9router keys.
func (s *UserService) ToggleUserStatus(ctx context.Context, userID string) (bool, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return false, ErrNotFound
	}
	newStatus := !user.IsActive
	if err := s.store.ToggleUserStatus(ctx, userID, newStatus); err != nil {
		return false, err
	}
	if s.sync != nil {
		keys, _ := s.store.GetAPIKeysByUserID(ctx, userID)
		for _, k := range keys {
			_ = s.sync.ToggleKey(k.ID, newStatus)
		}
	}
	return newStatus, nil
}

// DeleteUser removes a user after cleaning up 9router keys.
func (s *UserService) DeleteUser(ctx context.Context, userID string) error {
	keys, _ := s.store.GetAPIKeysByUserID(ctx, userID)
	if err := s.store.DeleteUser(ctx, userID); err != nil {
		return err
	}
	if s.sync != nil {
		for _, k := range keys {
			_ = s.sync.DeleteKey(k.ID)
		}
	}
	return nil
}

// ResetUsage zeroes the tokens_used counter.
func (s *UserService) ResetUsage(ctx context.Context, userID string) error {
	return s.store.ResetUserUsage(ctx, userID)
}

// ---- API key use cases ----

// KeyService implements API key use cases: creation with validation, toggling,
// deletion, rotation and expiry cleanup.
type KeyService struct {
	store Store
	sync  KeySyncer
}

// NewKeyService builds a KeyService.
func NewKeyService(store Store, sync KeySyncer) *KeyService {
	return &KeyService{store: store, sync: sync}
}

// CreateKeyInput is the parsed payload for creating an API key.
type CreateKeyInput struct {
	UserID        string
	Name          string
	CustomKey     string
	AllowedModels string
	RateLimitRPM  int
	ExpiresAt     *time.Time
}

// CreateKey creates an API key and syncs it to 9router Core.
func (s *KeyService) CreateKey(ctx context.Context, in CreateKeyInput) (*entity.APIKey, error) {
	finalKey := strings.TrimSpace(in.CustomKey)
	if finalKey == "" {
		finalKey = GenerateSecureAPIKey("sk-gw-")
	} else {
		if len(finalKey) < 8 || len(finalKey) > 128 {
			return nil, fmt.Errorf("custom key must be between 8 and 128 characters")
		}
		if existing, _ := s.store.GetAPIKeyByKey(ctx, finalKey); existing != nil {
			return nil, fmt.Errorf("API key already exists")
		}
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "API Key"
	}
	if len(name) > 64 {
		name = name[:64]
	}

	rateLimitRPM := in.RateLimitRPM
	if rateLimitRPM < 0 {
		rateLimitRPM = 0
	}

	allowedModels := normalizeAllowedModels(in.AllowedModels)

	key := &entity.APIKey{
		ID:            uuid.New().String(),
		UserID:        in.UserID,
		Key:           finalKey,
		Name:          name,
		AllowedModels: allowedModels,
		RateLimitRPM:  rateLimitRPM,
		IsActive:      true,
		ExpiresAt:     in.ExpiresAt,
	}

	if err := s.store.CreateAPIKey(ctx, key); err != nil {
		return nil, err
	}

	if s.sync != nil {
		u, _ := s.store.GetUserByID(ctx, in.UserID)
		uName := ""
		if u != nil {
			uName = u.Name
		}
		_ = s.sync.SyncKey(key, uName)
	}
	return key, nil
}

// ToggleKeyStatus flips a key's active flag (ownership enforced by caller).
func (s *KeyService) ToggleKeyStatus(ctx context.Context, keyID string) (bool, error) {
	keys, err := s.store.GetAllAPIKeys(ctx)
	if err != nil {
		return false, ErrNotFound
	}
	var target *entity.APIKey
	for i := range keys {
		if keys[i].ID == keyID {
			target = &keys[i]
			break
		}
	}
	if target == nil {
		return false, ErrNotFound
	}

	newStatus := !target.IsActive
	if err := s.store.ToggleAPIKeyStatus(ctx, keyID, newStatus); err != nil {
		return false, err
	}
	if s.sync != nil {
		_ = s.sync.ToggleKey(keyID, newStatus)
	}
	return newStatus, nil
}

// DeleteKey removes a key and syncs the deletion to 9router.
func (s *KeyService) DeleteKey(ctx context.Context, keyID string) error {
	if _, err := s.getKeyByID(ctx, keyID); err != nil {
		return err
	}
	if err := s.store.DeleteAPIKey(ctx, keyID); err != nil {
		return err
	}
	if s.sync != nil {
		_ = s.sync.DeleteKey(keyID)
	}
	return nil
}

func (s *KeyService) getKeyByID(ctx context.Context, keyID string) (*entity.APIKey, error) {
	keys, err := s.store.GetAllAPIKeys(ctx)
	if err != nil {
		return nil, err
	}
	for i := range keys {
		if keys[i].ID == keyID {
			return &keys[i], nil
		}
	}
	return nil, ErrNotFound
}

// ---- Shared helpers ----

var nonAlphaNumericRegex = mustCompile(`[^a-z0-9]+`)
var validUsernameRegex = mustCompile(`^[a-z0-9_.-]{3,32}$`)

func mustCompile(expr string) *regexp.Regexp {
	return regexp.MustCompile(expr)
}

func normalizeAllowedModels(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "*" {
		return ""
	}
	if strings.HasPrefix(raw, "[") {
		return raw
	}
	parts := strings.Split(raw, ",")
	var clean []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			clean = append(clean, p)
		}
	}
	b, _ := json.Marshal(clean)
	return string(b)
}