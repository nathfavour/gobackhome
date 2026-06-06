package storage_sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/nathfavour/gobackhome/core/ports"
)

// insert inserts a record and returns its ID.
func (w *tenantWorker) insert(ctx context.Context, collection string, record ports.Record) (string, error) {
	idVal, ok := record["id"]
	var id string
	if ok {
		id = fmt.Sprintf("%v", idVal)
	} else {
		id = uuid.New().String()
		record["id"] = id
	}

	keys := make([]string, 0, len(record))
	args := make([]any, 0, len(record))
	placeholders := make([]string, 0, len(record))

	for k, v := range record {
		keys = append(keys, k)
		// Simplistic handling of complex types (e.g., nested maps or slices to JSON)
		if isComplex(v) {
			b, _ := json.Marshal(v)
			args = append(args, string(b))
		} else {
			args = append(args, v)
		}
		placeholders = append(placeholders, "?")
	}

	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", collection, strings.Join(keys, ","), strings.Join(placeholders, ","))

	_, err := w.executeWrite(ctx, func(tx *sql.Tx) (any, error) {
		_, err := tx.ExecContext(ctx, query, args...)
		return nil, err
	})

	return id, err
}

func (w *tenantWorker) update(ctx context.Context, collection string, id string, record ports.Record) error {
	if len(record) == 0 {
		return nil
	}

	setClauses := make([]string, 0, len(record))
	args := make([]any, 0, len(record)+1)

	for k, v := range record {
		if k == "id" {
			continue // Do not update the primary key
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = ?", k))
		if isComplex(v) {
			b, _ := json.Marshal(v)
			args = append(args, string(b))
		} else {
			args = append(args, v)
		}
	}
	args = append(args, id)

	query := fmt.Sprintf("UPDATE %s SET %s WHERE id = ?", collection, strings.Join(setClauses, ","))

	_, err := w.executeWrite(ctx, func(tx *sql.Tx) (any, error) {
		_, err := tx.ExecContext(ctx, query, args...)
		return nil, err
	})

	return err
}

func (w *tenantWorker) delete(ctx context.Context, collection string, id string) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE id = ?", collection)

	_, err := w.executeWrite(ctx, func(tx *sql.Tx) (any, error) {
		_, err := tx.ExecContext(ctx, query, id)
		return nil, err
	})

	return err
}

func (w *tenantWorker) findByID(ctx context.Context, collection string, id string) (ports.Record, error) {
	query := fmt.Sprintf("SELECT * FROM %s WHERE id = ?", collection)
	
	// Can execute directly on db since reads are concurrent
	rows, err := w.db.QueryContext(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, sql.ErrNoRows
	}

	return scanRow(rows)
}

func (w *tenantWorker) selectRecords(ctx context.Context, query ports.Query) ([]ports.Record, error) {
	// A basic implementation of selection.
	q := fmt.Sprintf("SELECT * FROM %s", query.Collection)
	var args []any
	
	if len(query.Filters) > 0 {
		q += " WHERE "
		var clauses []string
		for _, f := range query.Filters {
			// Basic operator mapping. More complex sanitization needed for prod.
			clauses = append(clauses, fmt.Sprintf("%s %s ?", f.Field, f.Operator))
			args = append(args, f.Value)
		}
		q += strings.Join(clauses, " AND ")
	}

	if query.SortBy != "" {
		q += fmt.Sprintf(" ORDER BY %s", query.SortBy)
	}

	if query.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", query.Limit)
	}
	if query.Offset > 0 {
		q += fmt.Sprintf(" OFFSET %d", query.Offset)
	}

	rows, err := w.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ports.Record
	for rows.Next() {
		record, err := scanRow(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, record)
	}
	return results, nil
}

func scanRow(rows *sql.Rows) (ports.Record, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	values := make([]any, len(columns))
	scanArgs := make([]any, len(columns))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	if err := rows.Scan(scanArgs...); err != nil {
		return nil, err
	}

	record := make(ports.Record)
	for i, col := range columns {
		val := values[i]
		switch v := val.(type) {
		case []byte:
			// SQLite returns string columns as []byte, convert back.
			record[col] = string(v)
		default:
			record[col] = v
		}
	}
	return record, nil
}

func isComplex(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return true
	}
	return false
}
