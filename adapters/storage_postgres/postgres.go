package storage_postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/nathfavour/gobackhome/core/ports"
	_ "github.com/lib/pq"
)

// pgEngine implements ports.StorageEngine using PostgreSQL.
// It manages multi-tenancy by mapping each tenant to an isolated database schema.
type pgEngine struct {
	db *sql.DB
}

// New initializes a new PostgreSQL storage adapter.
// The DSN should be a valid PostgreSQL connection string.
func New(dsn string) (ports.StorageEngine, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open postgres connection: %w", err)
	}

	// Optimize connection pool for high concurrency
	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(10)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	return &pgEngine{db: db}, nil
}

// sanitizeIdentifier prevents basic SQL injection for schema/table names.
// In a production system, a more robust identifier quoter should be used.
func sanitizeIdentifier(name string) string {
	return `"` + name + `"`
}

func (e *pgEngine) InitializeTenant(ctx context.Context, tenantID string) error {
	// Create an isolated schema for the tenant
	query := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s;", sanitizeIdentifier(tenantID))
	_, err := e.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create tenant schema: %w", err)
	}
	return nil
}

func (e *pgEngine) DecommissionTenant(ctx context.Context, tenantID string) error {
	// Drop the schema and all its objects
	query := fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE;", sanitizeIdentifier(tenantID))
	_, err := e.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to decommission tenant: %w", err)
	}
	return nil
}
