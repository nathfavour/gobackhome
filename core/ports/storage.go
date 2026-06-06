package ports

import (
	"context"
	"errors"
)

var (
	ErrTenantNotFound = errors.New("tenant database not provisioned")
	ErrSchemaMismatch = errors.New("provided schema cannot be applied to current target")
)

type Record map[string]any

type Query struct {
	Collection string
	Filters    []Filter
	Limit      int
	Offset     int
	SortBy     string
}

type Filter struct {
	Field    string
	Operator string // "=", "!=", ">", "<", "CONTAINS", "JSON_MATCH"
	Value    any
}

// StorageEngine is the driven port governing the multi-tenant data layer
type StorageEngine interface {
	InitializeTenant(ctx context.Context, tenantID string) error
	DecommissionTenant(ctx context.Context, tenantID string) error

	ExecuteMigration(ctx context.Context, tenantID string, blueprint SchemaBlueprint) error

	Insert(ctx context.Context, tenantID string, collection string, record Record) (string, error)
	Update(ctx context.Context, tenantID string, collection string, id string, record Record) error
	Delete(ctx context.Context, tenantID string, collection string, id string) error
	FindByID(ctx context.Context, tenantID string, collection string, id string) (Record, error)
	Select(ctx context.Context, tenantID string, query Query) ([]Record, error)
}
