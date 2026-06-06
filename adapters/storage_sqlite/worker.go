package storage_sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

type writeTask struct {
	ctx    context.Context
	action func(tx *sql.Tx) (any, error)
	result chan writeResult
}

type writeResult struct {
	data any
	err  error
}

// tenantWorker manages the isolated SQLite database for a single tenant.
type tenantWorker struct {
	id       string
	db       *sql.DB
	writeCh  chan writeTask
	shutdown chan struct{}
	wg       sync.WaitGroup
}

func newTenantWorker(tenantID string, dbPath string) (*tenantWorker, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create tenant directory: %w", err)
	}

	// URI format for SQLite
	dsn := fmt.Sprintf("%s?_busy_timeout=5000&_journal_mode=WAL&_synchronous=NORMAL", dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}

	// SQLite concurrent reads are fine in WAL mode, but we restrict to 1 open conn
	// if we're using a single worker thread for writes. However, reads can happen
	// concurrently with a single write in WAL mode.
	// We will let *sql.DB handle read connections, but writes go through writeCh.
	db.SetMaxOpenConns(10) // allow concurrent reads
	db.SetMaxIdleConns(5)

	worker := &tenantWorker{
		id:       tenantID,
		db:       db,
		writeCh:  make(chan writeTask, 100),
		shutdown: make(chan struct{}),
	}

	worker.wg.Add(1)
	go worker.runWriteLoop()

	return worker, nil
}

// runWriteLoop enforces write serialization.
func (w *tenantWorker) runWriteLoop() {
	defer w.wg.Done()
	// To strictly serialize writes, we use a single dedicated goroutine
	// that acquires a connection and executes transactions.
	for {
		select {
		case <-w.shutdown:
			return
		case task := <-w.writeCh:
			// Execute write task
			res, err := w.executeTx(task.ctx, task.action)
			task.result <- writeResult{data: res, err: err}
		}
	}
}

func (w *tenantWorker) executeTx(ctx context.Context, action func(tx *sql.Tx) (any, error)) (any, error) {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() // ignored if already committed

	res, err := action(tx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return res, nil
}

// executeWrite dispatches a write function to the serialization loop.
func (w *tenantWorker) executeWrite(ctx context.Context, action func(tx *sql.Tx) (any, error)) (any, error) {
	resCh := make(chan writeResult, 1)
	task := writeTask{
		ctx:    ctx,
		action: action,
		result: resCh,
	}

	select {
	case w.writeCh <- task:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	select {
	case res := <-resCh:
		return res.data, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (w *tenantWorker) close() error {
	close(w.shutdown)
	w.wg.Wait()
	return w.db.Close()
}
