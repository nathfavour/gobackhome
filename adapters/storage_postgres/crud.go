package storage_postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/nathfavour/gobackhome/core/ports"
)

func (e *pgEngine) Insert(ctx context.Context, tenantID string, collection string, record ports.Record) (string, error) {
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

	i := 1
	for k, v := range record {
		keys = append(keys, sanitizeIdentifier(k))
		if isComplex(v) {
			b, _ := json.Marshal(v)
			args = append(args, string(b))
		} else {
			args = append(args, v)
		}
		placeholders = append(placeholders, fmt.Sprintf("$%d", i))
		i++
	}

	schema := sanitizeIdentifier(tenantID)
	table := sanitizeIdentifier(collection)

	query := fmt.Sprintf("INSERT INTO %s.%s (%s) VALUES (%s)", schema, table, strings.Join(keys, ","), strings.Join(placeholders, ","))

	_, err := e.db.ExecContext(ctx, query, args...)
	return id, err
}

func (e *pgEngine) Update(ctx context.Context, tenantID string, collection string, id string, record ports.Record) error {
	if len(record) == 0 {
		return nil
	}

	setClauses := make([]string, 0, len(record))
	args := make([]any, 0, len(record)+1)

	i := 1
	for k, v := range record {
		if k == "id" {
			continue // Do not update primary key
		}
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", sanitizeIdentifier(k), i))
		if isComplex(v) {
			b, _ := json.Marshal(v)
			args = append(args, string(b))
		} else {
			args = append(args, v)
		}
		i++
	}
	args = append(args, id)

	schema := sanitizeIdentifier(tenantID)
	table := sanitizeIdentifier(collection)

	query := fmt.Sprintf("UPDATE %s.%s SET %s WHERE id = $%d", schema, table, strings.Join(setClauses, ","), i)

	_, err := e.db.ExecContext(ctx, query, args...)
	return err
}

func (e *pgEngine) Delete(ctx context.Context, tenantID string, collection string, id string) error {
	schema := sanitizeIdentifier(tenantID)
	table := sanitizeIdentifier(collection)

	query := fmt.Sprintf("DELETE FROM %s.%s WHERE id = $1", schema, table)

	_, err := e.db.ExecContext(ctx, query, id)
	return err
}

func (e *pgEngine) FindByID(ctx context.Context, tenantID string, collection string, id string) (ports.Record, error) {
	schema := sanitizeIdentifier(tenantID)
	table := sanitizeIdentifier(collection)

	query := fmt.Sprintf("SELECT * FROM %s.%s WHERE id = $1", schema, table)
	
	rows, err := e.db.QueryContext(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, sql.ErrNoRows
	}

	return scanRow(rows)
}

func (e *pgEngine) Select(ctx context.Context, tenantID string, query ports.Query) ([]ports.Record, error) {
	schema := sanitizeIdentifier(tenantID)
	table := sanitizeIdentifier(query.Collection)

	q := fmt.Sprintf("SELECT * FROM %s.%s", schema, table)
	var args []any
	
	argIdx := 1
	if len(query.Filters) > 0 {
		q += " WHERE "
		var clauses []string
		for _, f := range query.Filters {
			// Basic operator mapping.
			clauses = append(clauses, fmt.Sprintf("%s %s $%d", sanitizeIdentifier(f.Field), f.Operator, argIdx))
			args = append(args, f.Value)
			argIdx++
		}
		q += strings.Join(clauses, " AND ")
	}

	if query.SortBy != "" {
		q += fmt.Sprintf(" ORDER BY %s", sanitizeIdentifier(query.SortBy))
	}

	if query.Limit > 0 {
		q += fmt.Sprintf(" LIMIT $%d", argIdx)
		args = append(args, query.Limit)
		argIdx++
	}
	if query.Offset > 0 {
		q += fmt.Sprintf(" OFFSET $%d", argIdx)
		args = append(args, query.Offset)
		argIdx++
	}

	rows, err := e.db.QueryContext(ctx, q, args...)
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
