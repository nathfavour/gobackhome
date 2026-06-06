package ports

import (
	"context"
	"time"
)

type AuthSession struct {
	Token     string
	UserID    string
	TenantID  string
	ExpiresAt time.Time
}

type AuthEngine interface {
	RegisterPasswordUser(ctx context.Context, tenantID string, email, password string) (string, error)
	VerifyPasswordUser(ctx context.Context, tenantID string, email, password string) (*AuthSession, error)
	
	VerifyWeb3Signature(ctx context.Context, tenantID string, address, message, signature string) (*AuthSession, error)
	
	ValidateSessionToken(ctx context.Context, tenantID string, plainToken string) (*AuthSession, error)
	RevokeSessionToken(ctx context.Context, tenantID string, plainToken string) error
}
