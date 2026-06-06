package storage_sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"

	"github.com/nathfavour/gobackhome/core/ports"
)

// buildDDL translates a SchemaBlueprint into SQLite DDL statements.
func buildDDL(blueprint ports.SchemaBlueprint) (string, error) {
	var buf bytes.Buffer

	for _, coll := range blueprint.Collections {
		buf.WriteString(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n", coll.Name))
		buf.WriteString("    id TEXT PRIMARY KEY,\n")

		for i, field := range coll.Fields {
			sqliteType := mapFieldType(field.Type)
			if sqliteType == "" {
				return "", fmt.Errorf("unsupported field type: %s", field.Type)
			}

			notNull := ""
			if field.Required {
				notNull = " NOT NULL"
			}

			comma := ","
			if i == len(coll.Fields)-1 {
				comma = ""
			}

			buf.WriteString(fmt.Sprintf("    %s %s%s%s\n", field.Name, sqliteType, notNull, comma))
		}

		// Use STRICT mode for better type safety in SQLite
		buf.WriteString(") STRICT;\n")
	}

	return buf.String(), nil
}

func mapFieldType(t string) string {
	switch t {
	case "string":
		return "TEXT"
	case "number":
		return "REAL" // Or INTEGER depending on precision, REAL covers both generally in SQLite STRICT mode
	case "integer":
		return "INTEGER"
	case "boolean":
		return "INTEGER" // SQLite booleans are typically integers (0/1)
	case "json":
		return "TEXT" // JSON is stored as text
	default:
		return ""
	}
}

// migrate executes the DDL sequentially.
func (w *tenantWorker) migrate(ctx context.Context, blueprint ports.SchemaBlueprint) error {
	ddl, err := buildDDL(blueprint)
	if err != nil {
		return err
	}

	_, err = w.executeWrite(ctx, func(tx *sql.Tx) (any, error) {
		_, err := tx.ExecContext(ctx, ddl)
		return nil, err
	})

	return err
}
