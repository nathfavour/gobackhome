package storage_sqlite

import (
	"context"
	"path/filepath"
	"sync"

	"github.com/nathfavour/gobackhome/core/ports"
)

// sqliteEngine implements ports.StorageEngine
type sqliteEngine struct {
	mu      sync.RWMutex
	dataDir string
	tenants map[string]*tenantWorker
}

func New(dataDir string) ports.StorageEngine {
	return &sqliteEngine{
		dataDir: dataDir,
		tenants: make(map[string]*tenantWorker),
	}
}

func (e *sqliteEngine) InitializeTenant(ctx context.Context, tenantID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.tenants[tenantID]; exists {
		return nil // Already initialized
	}

	dbPath := filepath.Join(e.dataDir, tenantID, "data.db")
	worker, err := newTenantWorker(tenantID, dbPath)
	if err != nil {
		return err
	}

	e.tenants[tenantID] = worker
	return nil
}

func (e *sqliteEngine) DecommissionTenant(ctx context.Context, tenantID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	worker, exists := e.tenants[tenantID]
	if !exists {
		return ports.ErrTenantNotFound
	}

	err := worker.close()
	delete(e.tenants, tenantID)
	return err
}

func (e *sqliteEngine) getWorker(tenantID string) (*tenantWorker, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	worker, exists := e.tenants[tenantID]
	if !exists {
		return nil, ports.ErrTenantNotFound
	}
	return worker, nil
}

func (e *sqliteEngine) ExecuteMigration(ctx context.Context, tenantID string, blueprint ports.SchemaBlueprint) error {
	worker, err := e.getWorker(tenantID)
	if err != nil {
		return err
	}
	return worker.migrate(ctx, blueprint)
}

func (e *sqliteEngine) Insert(ctx context.Context, tenantID string, collection string, record ports.Record) (string, error) {
	worker, err := e.getWorker(tenantID)
	if err != nil {
		return "", err
	}
	return worker.insert(ctx, collection, record)
}

func (e *sqliteEngine) Update(ctx context.Context, tenantID string, collection string, id string, record ports.Record) error {
	worker, err := e.getWorker(tenantID)
	if err != nil {
		return err
	}
	return worker.update(ctx, collection, id, record)
}

func (e *sqliteEngine) Delete(ctx context.Context, tenantID string, collection string, id string) error {
	worker, err := e.getWorker(tenantID)
	if err != nil {
		return err
	}
	return worker.delete(ctx, collection, id)
}

func (e *sqliteEngine) FindByID(ctx context.Context, tenantID string, collection string, id string) (ports.Record, error) {
	worker, err := e.getWorker(tenantID)
	if err != nil {
		return nil, err
	}
	return worker.findByID(ctx, collection, id)
}

func (e *sqliteEngine) Select(ctx context.Context, tenantID string, query ports.Query) ([]ports.Record, error) {
	worker, err := e.getWorker(tenantID)
	if err != nil {
		return nil, err
	}
	return worker.selectRecords(ctx, query)
}
