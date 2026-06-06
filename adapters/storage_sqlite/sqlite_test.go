package storage_sqlite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nathfavour/gobackhome/core/ports"
)

func TestSQLiteEngine(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sqlite_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	engine := New(tempDir)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tenantID := "test-tenant-1"

	t.Run("InitializeTenant", func(t *testing.T) {
		err := engine.InitializeTenant(ctx, tenantID)
		if err != nil {
			t.Fatalf("failed to initialize tenant: %v", err)
		}
		
		// Verify directory was created
		_, err = os.Stat(filepath.Join(tempDir, tenantID, "data.db"))
		if os.IsNotExist(err) {
			t.Fatalf("database file was not created")
		}
	})

	t.Run("ExecuteMigration", func(t *testing.T) {
		blueprint := ports.SchemaBlueprint{
			Collections: []ports.CollectionBlueprint{
				{
					Name: "users",
					Fields: []ports.FieldBlueprint{
						{Name: "username", Type: "string", Required: true},
						{Name: "age", Type: "integer", Required: false},
						{Name: "is_active", Type: "boolean", Required: false},
					},
				},
			},
		}

		err := engine.ExecuteMigration(ctx, tenantID, blueprint)
		if err != nil {
			t.Fatalf("failed to execute migration: %v", err)
		}
	})

	var insertedID string

	t.Run("Insert", func(t *testing.T) {
		record := ports.Record{
			"username":  "alice",
			"age":       30,
			"is_active": 1, // SQLite boolean
		}
		id, err := engine.Insert(ctx, tenantID, "users", record)
		if err != nil {
			t.Fatalf("failed to insert record: %v", err)
		}
		if id == "" {
			t.Fatalf("expected non-empty ID")
		}
		insertedID = id
	})

	t.Run("FindByID", func(t *testing.T) {
		record, err := engine.FindByID(ctx, tenantID, "users", insertedID)
		if err != nil {
			t.Fatalf("failed to find record: %v", err)
		}
		if record["username"] != "alice" {
			t.Fatalf("expected username 'alice', got %v", record["username"])
		}
	})

	t.Run("Update", func(t *testing.T) {
		updateRecord := ports.Record{
			"age": 31,
		}
		err := engine.Update(ctx, tenantID, "users", insertedID, updateRecord)
		if err != nil {
			t.Fatalf("failed to update record: %v", err)
		}

		record, err := engine.FindByID(ctx, tenantID, "users", insertedID)
		if err != nil {
			t.Fatalf("failed to find record after update: %v", err)
		}
		// SQLite might return int64
		if fmtAge := fmt.Sprint(record["age"]); fmtAge != "31" {
			t.Fatalf("expected age 31, got %v (type %T)", record["age"], record["age"])
		}
	})

	t.Run("ConcurrentWrites", func(t *testing.T) {
		var wg sync.WaitGroup
		errs := make(chan error, 100)
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				record := ports.Record{
					"username":  fmt.Sprintf("user_%d", i),
					"age":       20 + i,
					"is_active": 1,
				}
				_, err := engine.Insert(ctx, tenantID, "users", record)
				if err != nil {
					errs <- err
				}
			}(i)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Errorf("concurrent write failed: %v", err)
		}
	})

	t.Run("SelectRecords", func(t *testing.T) {
		query := ports.Query{
			Collection: "users",
			Filters: []ports.Filter{
				{Field: "username", Operator: "=", Value: "alice"},
			},
			Limit: 1,
		}
		records, err := engine.Select(ctx, tenantID, query)
		if err != nil {
			t.Fatalf("failed to select records: %v", err)
		}
		if len(records) != 1 {
			t.Fatalf("expected 1 record, got %d", len(records))
		}
	})

	t.Run("Delete", func(t *testing.T) {
		err := engine.Delete(ctx, tenantID, "users", insertedID)
		if err != nil {
			t.Fatalf("failed to delete record: %v", err)
		}

		_, err = engine.FindByID(ctx, tenantID, "users", insertedID)
		if err == nil {
			t.Fatalf("expected error when finding deleted record, got nil")
		}
	})

	t.Run("DecommissionTenant", func(t *testing.T) {
		err := engine.DecommissionTenant(ctx, tenantID)
		if err != nil {
			t.Fatalf("failed to decommission tenant: %v", err)
		}
		
		// Worker should be removed
		_, err = engine.(*sqliteEngine).getWorker(tenantID)
		if err != ports.ErrTenantNotFound {
			t.Fatalf("expected ErrTenantNotFound, got %v", err)
		}
	})
}
