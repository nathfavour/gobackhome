package storage_postgres

import (
	"bytes"
	"context"
	"fmt"

	"github.com/nathfavour/gobackhome/core/ports"
)

// buildDDL translates a SchemaBlueprint into PostgreSQL DDL statements within a target schema.
func buildDDL(tenantID string, blueprint ports.SchemaBlueprint) (string, error) {
	var buf bytes.Buffer
	schema := sanitizeIdentifier(tenantID)

	for _, coll := range blueprint.Collections {
		table := sanitizeIdentifier(coll.Name)
		buf.WriteString(fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s.%s (\n", schema, table))
		buf.WriteString("    id TEXT PRIMARY KEY,\n")

		for i, field := range coll.Fields {
			pgType := mapFieldType(field.Type)
			if pgType == "" {
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

			buf.WriteString(fmt.Sprintf("    %s %s%s%s\n", sanitizeIdentifier(field.Name), pgType, notNull, comma))
		}

		buf.WriteString(");\n")
	}

	return buf.String(), nil
}

func mapFieldType(t string) string {
	switch t {
	case "string":
		return "TEXT"
	case "number":
		return "DOUBLE PRECISION"
	case "integer":
		return "BIGINT"
	case "boolean":
		return "BOOLEAN"
	case "json":
		return "JSONB" // Utilize PostgreSQL's native high-performance binary JSON
	default:
		return ""
	}
}

func (e *pgEngine) ExecuteMigration(ctx context.Context, tenantID string, blueprint ports.SchemaBlueprint) error {
	ddl, err := buildDDL(tenantID, blueprint)
	if err != nil {
		return err
	}

	_, err = e.db.ExecContext(ctx, ddl)
	return err
}
