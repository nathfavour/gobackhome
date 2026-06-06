package kernel

import (
	"context"
	"errors"
	"testing"

	"github.com/nathfavour/gobackhome/core/ports"
)

type mockStorage struct {
	initialized   bool
	decommissioned bool
	migrated      bool
	inserted      int
	data          []ports.Record
}

func (m *mockStorage) InitializeTenant(ctx context.Context, tenantID string) error {
	m.initialized = true
	return nil
}

func (m *mockStorage) DecommissionTenant(ctx context.Context, tenantID string) error {
	m.decommissioned = true
	return nil
}

func (m *mockStorage) ExecuteMigration(ctx context.Context, tenantID string, blueprint ports.SchemaBlueprint) error {
	m.migrated = true
	return nil
}

func (m *mockStorage) Insert(ctx context.Context, tenantID string, collection string, record ports.Record) (string, error) {
	m.inserted++
	return "mock-id", nil
}

func (m *mockStorage) Update(ctx context.Context, tenantID string, collection string, id string, record ports.Record) error {
	return nil
}

func (m *mockStorage) Delete(ctx context.Context, tenantID string, collection string, id string) error {
	return nil
}

func (m *mockStorage) FindByID(ctx context.Context, tenantID string, collection string, id string) (ports.Record, error) {
	return nil, errors.New("not implemented")
}

func (m *mockStorage) Select(ctx context.Context, tenantID string, query ports.Query) ([]ports.Record, error) {
	return m.data, nil
}

func TestOrchestrator_CoordinateMigration(t *testing.T) {
	orchestrator := NewOrchestrator()
	tenantID := "tenant-x"

	sourceEngine := &mockStorage{
		data: []ports.Record{
			{"id": "1", "name": "foo"},
			{"id": "2", "name": "bar"},
		},
	}
	targetEngine := &mockStorage{}

	orchestrator.RegisterTenant(tenantID, sourceEngine)

	tc, err := orchestrator.GetTenant(tenantID)
	if err != nil {
		t.Fatalf("expected to find tenant, got err: %v", err)
	}

	if tc.GetEngine() != sourceEngine {
		t.Fatalf("expected source engine initially")
	}

	blueprint := ports.SchemaBlueprint{
		Collections: []ports.CollectionBlueprint{
			{Name: "items"},
		},
	}

	ctx := context.Background()
	err = orchestrator.CoordinateMigration(ctx, tenantID, blueprint, targetEngine)
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Verify target engine was correctly initialized and migrated
	if !targetEngine.initialized {
		t.Errorf("target engine was not initialized")
	}
	if !targetEngine.migrated {
		t.Errorf("target engine was not migrated")
	}

	// Verify data stream
	if targetEngine.inserted != 2 {
		t.Errorf("expected 2 records inserted into target engine, got %d", targetEngine.inserted)
	}

	// Verify swap occurred
	if tc.GetEngine() != targetEngine {
		t.Errorf("expected engine to be swapped to target")
	}

	// Verify old engine was decommissioned
	if !sourceEngine.decommissioned {
		t.Errorf("expected source engine to be decommissioned")
	}
	
	// Verify state is back to active
	if tc.GetState() != StateActive {
		t.Errorf("expected tenant state to be active, got %v", tc.GetState())
	}
}
