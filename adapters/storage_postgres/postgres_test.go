package storage_postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nathfavour/gobackhome/core/ports"
)

func TestPostgresEngine(t *testing.T) {
	dsn := "postgres://postgres:postgres@localhost:5432/testdb?sslmode=disable"
	
	// Wait a bit for db to be ready in CI environments if needed, but the ping inside New should handle failure.
	var engine ports.StorageEngine
	var err error
	
	// Retry loop for connecting
	for i := 0; i < 5; i++ {
		engine, err = New(dsn)
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}
	
	if err != nil {
		t.Skipf("skipping postgres tests, database not available: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tenantID := "test_tenant_pg"

	t.Run("InitializeTenant", func(t *testing.T) {
		err := engine.InitializeTenant(ctx, tenantID)
		if err != nil {
			t.Fatalf("failed to initialize tenant: %v", err)
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
			"is_active": true,
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
		if fmtAge := fmt.Sprint(record["age"]); fmtAge != "31" {
			t.Fatalf("expected age 31, got %v (type %T)", record["age"], record["age"])
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
	})
}
