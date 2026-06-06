package auth_crypto

import (
	"context"
	"errors"
	"testing"

	"github.com/nathfavour/gobackhome/core/ports"
)

// mockStorage implements ports.StorageEngine for testing auth
type mockStorage struct {
	records map[string]map[string]ports.Record
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		records: make(map[string]map[string]ports.Record),
	}
}

func (m *mockStorage) InitializeTenant(ctx context.Context, tenantID string) error { return nil }
func (m *mockStorage) DecommissionTenant(ctx context.Context, tenantID string) error { return nil }
func (m *mockStorage) ExecuteMigration(ctx context.Context, tenantID string, blueprint ports.SchemaBlueprint) error { return nil }

func (m *mockStorage) Insert(ctx context.Context, tenantID string, collection string, record ports.Record) (string, error) {
	if m.records[collection] == nil {
		m.records[collection] = make(map[string]ports.Record)
	}
	id, ok := record["id"].(string)
	if !ok {
		// Session uses token_hash as primary key equivalent
		if th, ok := record["token_hash"].(string); ok {
			id = th
		} else {
			return "", errors.New("missing ID")
		}
	}
	m.records[collection][id] = record
	return id, nil
}

func (m *mockStorage) Update(ctx context.Context, tenantID string, collection string, id string, record ports.Record) error {
	return nil
}

func (m *mockStorage) Delete(ctx context.Context, tenantID string, collection string, id string) error {
	if m.records[collection] != nil {
		delete(m.records[collection], id)
	}
	return nil
}

func (m *mockStorage) FindByID(ctx context.Context, tenantID string, collection string, id string) (ports.Record, error) {
	if coll, ok := m.records[collection]; ok {
		if rec, ok := coll[id]; ok {
			return rec, nil
		}
	}
	return nil, errors.New("not found")
}

func (m *mockStorage) Select(ctx context.Context, tenantID string, query ports.Query) ([]ports.Record, error) {
	var results []ports.Record
	coll := m.records[query.Collection]
	if coll == nil {
		return results, nil
	}

	for _, rec := range coll {
		match := true
		for _, f := range query.Filters {
			val, exists := rec[f.Field]
			if !exists || val != f.Value {
				match = false
				break
			}
		}
		if match {
			results = append(results, rec)
		}
	}
	return results, nil
}

func TestAuthEngine_PasswordFlow(t *testing.T) {
	store := newMockStorage()
	engine := New(store)
	ctx := context.Background()
	tenantID := "test-tenant"

	email := "test@example.com"
	password := "supersecure123"

	t.Run("Register", func(t *testing.T) {
		userID, err := engine.RegisterPasswordUser(ctx, tenantID, email, password)
		if err != nil {
			t.Fatalf("failed to register: %v", err)
		}
		if userID == "" {
			t.Fatalf("expected non-empty user ID")
		}
		
		// Attempt duplicate
		_, err = engine.RegisterPasswordUser(ctx, tenantID, email, password)
		if !errors.Is(err, ErrUserAlreadyExists) {
			t.Fatalf("expected ErrUserAlreadyExists, got %v", err)
		}
	})

	var sessionToken string

	t.Run("Verify_Success", func(t *testing.T) {
		session, err := engine.VerifyPasswordUser(ctx, tenantID, email, password)
		if err != nil {
			t.Fatalf("failed to verify password: %v", err)
		}
		if session.Token == "" {
			t.Fatalf("expected non-empty session token")
		}
		sessionToken = session.Token
	})

	t.Run("Verify_Failure", func(t *testing.T) {
		_, err := engine.VerifyPasswordUser(ctx, tenantID, email, "wrongpassword")
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("expected ErrInvalidCredentials, got %v", err)
		}
	})

	t.Run("ValidateSession", func(t *testing.T) {
		session, err := engine.ValidateSessionToken(ctx, tenantID, sessionToken)
		if err != nil {
			t.Fatalf("failed to validate session: %v", err)
		}
		if session.Token != sessionToken {
			t.Fatalf("token mismatch")
		}
	})

	t.Run("RevokeSession", func(t *testing.T) {
		err := engine.RevokeSessionToken(ctx, tenantID, sessionToken)
		if err != nil {
			t.Fatalf("failed to revoke session: %v", err)
		}

		_, err = engine.ValidateSessionToken(ctx, tenantID, sessionToken)
		if !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("expected ErrSessionInvalid after revoke, got %v", err)
		}
	})
}

func TestAuthEngine_Web3Flow(t *testing.T) {
	store := newMockStorage()
	engine := New(store)
	ctx := context.Background()
	tenantID := "tenant-xyz"
	address := "0x123456789"
	
	// Valid message containing tenantID as domain nonce context
	validMsg := "Sign into " + tenantID + " with nonce 999"
	invalidMsg := "Sign into other-tenant with nonce 999"

	t.Run("InvalidDomainContext", func(t *testing.T) {
		_, err := engine.VerifyWeb3Signature(ctx, tenantID, address, invalidMsg, "sig")
		if !errors.Is(err, ErrSignatureInvalid) {
			t.Fatalf("expected ErrSignatureInvalid for cross-tenant replay, got %v", err)
		}
	})

	t.Run("ValidLogin_AutoRegister", func(t *testing.T) {
		session, err := engine.VerifyWeb3Signature(ctx, tenantID, address, validMsg, "sig")
		if err != nil {
			t.Fatalf("failed to verify web3 signature: %v", err)
		}
		if session.Token == "" {
			t.Fatalf("expected session token")
		}
	})
}
