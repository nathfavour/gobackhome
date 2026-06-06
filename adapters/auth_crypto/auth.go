package auth_crypto

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nathfavour/gobackhome/core/ports"
	"golang.org/x/crypto/argon2"
)

var (
	ErrInvalidCredentials  = errors.New("invalid email or password")
	ErrUserAlreadyExists   = errors.New("user already exists")
	ErrSessionInvalid      = errors.New("invalid or expired session")
	ErrSignatureInvalid    = errors.New("invalid web3 signature")
)

type cryptoEngine struct {
	storage ports.StorageEngine
}

func New(storage ports.StorageEngine) ports.AuthEngine {
	return &cryptoEngine{
		storage: storage,
	}
}

// GenerateOpaqueToken creates a cryptographically secure random session token.
func generateOpaqueToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

// hashToken creates a SHA-256 hash of the session token for database storage.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// hashPassword computes an Argon2id hash.
// Using memory-hard settings (m=65536, t=3, p=4) as per specification.
func hashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)
	// Format: $argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, 64*1024, 3, 4,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash)), nil
}

// verifyPassword checks an Argon2id hash against a plaintext password.
func verifyPassword(password, encodedHash string) (bool, error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return false, errors.New("invalid hash format")
	}
	// Simplified parsing for brevity; in a full implementation, parse all params.
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	decodedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 4, 32)
	
	if subtle.ConstantTimeCompare(decodedHash, hash) == 1 {
		return true, nil
	}
	return false, nil
}

func (e *cryptoEngine) RegisterPasswordUser(ctx context.Context, tenantID string, email, password string) (string, error) {
	// Check if user exists
	query := ports.Query{
		Collection: "_sys_users",
		Filters: []ports.Filter{
			{Field: "email", Operator: "=", Value: email},
		},
		Limit: 1,
	}
	existing, err := e.storage.Select(ctx, tenantID, query)
	if err != nil {
		return "", err
	}
	if len(existing) > 0 {
		return "", ErrUserAlreadyExists
	}

	hashedPassword, err := hashPassword(password)
	if err != nil {
		return "", err
	}

	userID := uuid.New().String()
	record := ports.Record{
		"id":             userID,
		"email":          email,
		"password_hash":  hashedPassword,
		"wallet_address": nil,
		"created_at":     time.Now().Format(time.RFC3339),
	}

	return e.storage.Insert(ctx, tenantID, "_sys_users", record)
}

func (e *cryptoEngine) VerifyPasswordUser(ctx context.Context, tenantID string, email, password string) (*ports.AuthSession, error) {
	query := ports.Query{
		Collection: "_sys_users",
		Filters: []ports.Filter{
			{Field: "email", Operator: "=", Value: email},
		},
		Limit: 1,
	}
	users, err := e.storage.Select(ctx, tenantID, query)
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, ErrInvalidCredentials
	}

	user := users[0]
	hashVal, ok := user["password_hash"].(string)
	if !ok || hashVal == "" {
		return nil, ErrInvalidCredentials
	}

	valid, err := verifyPassword(password, hashVal)
	if err != nil || !valid {
		return nil, ErrInvalidCredentials
	}

	// Password valid, create session
	userID := fmt.Sprintf("%v", user["id"])
	return e.createSession(ctx, tenantID, userID)
}

func (e *cryptoEngine) VerifyWeb3Signature(ctx context.Context, tenantID string, address, message, signature string) (*ports.AuthSession, error) {
	// This would integrate github.com/spruceid/siwe-go in a full implementation.
	// For now, we mock the domain-enforced nonce contextualization check.
	// 4.4 Cross-Tenant Web3 Signature Replay Vectors mitigation: Domain Enforced Nonce Contextualization
	if !strings.Contains(message, tenantID) { // Simplistic domain mapping check
		return nil, ErrSignatureInvalid
	}

	// Assuming signature is valid for this example
	
	// Find or create user by wallet address
	query := ports.Query{
		Collection: "_sys_users",
		Filters: []ports.Filter{
			{Field: "wallet_address", Operator: "=", Value: address},
		},
		Limit: 1,
	}
	users, err := e.storage.Select(ctx, tenantID, query)
	if err != nil {
		return nil, err
	}

	var userID string
	if len(users) == 0 {
		// Auto-register Web3 user
		userID = uuid.New().String()
		record := ports.Record{
			"id":             userID,
			"email":          nil,
			"password_hash":  nil,
			"wallet_address": address,
			"created_at":     time.Now().Format(time.RFC3339),
		}
		if _, err := e.storage.Insert(ctx, tenantID, "_sys_users", record); err != nil {
			return nil, err
		}
	} else {
		userID = fmt.Sprintf("%v", users[0]["id"])
	}

	return e.createSession(ctx, tenantID, userID)
}

func (e *cryptoEngine) createSession(ctx context.Context, tenantID string, userID string) (*ports.AuthSession, error) {
	token := generateOpaqueToken()
	tokenHash := hashToken(token)
	expiresAt := time.Now().Add(24 * time.Hour) // Default 24h session

	record := ports.Record{
		"token_hash": tokenHash,
		"user_id":    userID,
		"expires_at": expiresAt.Format(time.RFC3339),
	}

	// Ensure system tables exist via some bootstrap hook. Assuming they exist here.
	if _, err := e.storage.Insert(ctx, tenantID, "_sys_sessions", record); err != nil {
		return nil, err
	}

	return &ports.AuthSession{
		Token:     token, // Return plain token to user ONCE
		UserID:    userID,
		TenantID:  tenantID,
		ExpiresAt: expiresAt,
	}, nil
}

func (e *cryptoEngine) ValidateSessionToken(ctx context.Context, tenantID string, plainToken string) (*ports.AuthSession, error) {
	tokenHash := hashToken(plainToken)

	record, err := e.storage.FindByID(ctx, tenantID, "_sys_sessions", tokenHash)
	if err != nil {
		return nil, ErrSessionInvalid // Normalizes DB errors like not found
	}

	expStr, ok := record["expires_at"].(string)
	if !ok {
		return nil, ErrSessionInvalid
	}

	expiresAt, err := time.Parse(time.RFC3339, expStr)
	if err != nil || time.Now().After(expiresAt) {
		// Session expired, lazy pruning takes care of deletion later or we can delete now
		_ = e.storage.Delete(ctx, tenantID, "_sys_sessions", tokenHash)
		return nil, ErrSessionInvalid
	}

	userID := fmt.Sprintf("%v", record["user_id"])

	return &ports.AuthSession{
		Token:     plainToken,
		UserID:    userID,
		TenantID:  tenantID,
		ExpiresAt: expiresAt,
	}, nil
}

func (e *cryptoEngine) RevokeSessionToken(ctx context.Context, tenantID string, plainToken string) error {
	tokenHash := hashToken(plainToken)
	return e.storage.Delete(ctx, tenantID, "_sys_sessions", tokenHash)
}
