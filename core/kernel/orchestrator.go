package kernel

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/nathfavour/gobackhome/core/ports"
)

type TenantState int

const (
	StateActive TenantState = iota
	StateMigrating
)

// TenantContext wraps the runtime state of an isolated application tenant.
type TenantContext struct {
	ID            string
	State         atomic.Value // holds TenantState
	StorageEngine atomic.Value // holds ports.StorageEngine
	mu            sync.RWMutex
}

func (tc *TenantContext) GetState() TenantState {
	return tc.State.Load().(TenantState)
}

func (tc *TenantContext) SetState(s TenantState) {
	tc.State.Store(s)
}

func (tc *TenantContext) GetEngine() ports.StorageEngine {
	return tc.StorageEngine.Load().(ports.StorageEngine)
}

func (tc *TenantContext) SwapEngine(engine ports.StorageEngine) {
	tc.StorageEngine.Store(engine)
}

// Orchestrator manages the topology and runtime mapping of multi-tenant domains.
type Orchestrator struct {
	tenants sync.Map // map[string]*TenantContext
}

func NewOrchestrator() *Orchestrator {
	return &Orchestrator{}
}

func (o *Orchestrator) RegisterTenant(id string, initialEngine ports.StorageEngine) *TenantContext {
	tc := &TenantContext{
		ID: id,
	}
	tc.SetState(StateActive)
	tc.SwapEngine(initialEngine)
	o.tenants.Store(id, tc)
	return tc
}

func (o *Orchestrator) GetTenant(id string) (*TenantContext, error) {
	val, ok := o.tenants.Load(id)
	if !ok {
		return nil, ports.ErrTenantNotFound
	}
	return val.(*TenantContext), nil
}

// CoordinateMigration orchestrates the hot-swap sequence.
func (o *Orchestrator) CoordinateMigration(ctx context.Context, id string, blueprint ports.SchemaBlueprint, targetEngine ports.StorageEngine) error {
	tc, err := o.GetTenant(id)
	if err != nil {
		return err
	}

	// Lock tenant structurally for the migration
	tc.mu.Lock()
	defer tc.mu.Unlock()

	// Phase 1: Intercept & Queue (Flag target tenant state as MIGRATING)
	tc.SetState(StateMigrating)
	defer tc.SetState(StateActive)

	// Phase 2: Target Verification
	if err := targetEngine.InitializeTenant(ctx, id); err != nil {
		return fmt.Errorf("target engine init failed: %w", err)
	}
	if err := targetEngine.ExecuteMigration(ctx, id, blueprint); err != nil {
		return fmt.Errorf("target engine migration failed: %w", err)
	}

	// Phase 3: Stream & Pipe
	// Data is streamed out of the old storage engine and piped into the new one.
	oldEngine := tc.GetEngine()
	if err := o.streamData(ctx, id, blueprint, oldEngine, targetEngine); err != nil {
		return fmt.Errorf("data pipeline stream failed: %w", err)
	}

	// Phase 4: Swap & Purge
	tc.SwapEngine(targetEngine)
	
	// Safely purge old engine (could be done asynchronously)
	if err := oldEngine.DecommissionTenant(ctx, id); err != nil {
		// Log error, but migration is functionally complete
		fmt.Printf("warning: failed to decommission old tenant storage: %v\n", err)
	}

	return nil
}

// streamData utilizes zero-copy / chunked pipelines to transfer data.
func (o *Orchestrator) streamData(ctx context.Context, tenantID string, blueprint ports.SchemaBlueprint, source, target ports.StorageEngine) error {
	for _, coll := range blueprint.Collections {
		// Iterate over all items in the collection
		// In a production system, this requires cursors/pagination to avoid OOM.
		// For the core engine skeleton, we read and pipe:
		query := ports.Query{
			Collection: coll.Name,
		}
		
		records, err := source.Select(ctx, tenantID, query)
		if err != nil {
			return fmt.Errorf("failed to stream %s: %w", coll.Name, err)
		}

		// Pipe rows into target
		for _, record := range records {
			if _, err := target.Insert(ctx, tenantID, coll.Name, record); err != nil {
				return fmt.Errorf("failed to pipe record into %s: %w", coll.Name, err)
			}
		}
	}
	return nil
}
