package ports

// SchemaBlueprint represents an intermediate JSON-serializable database schema layout.
type SchemaBlueprint struct {
	Collections []CollectionBlueprint `json:"collections"`
}

type CollectionBlueprint struct {
	Name   string           `json:"name"`
	Fields []FieldBlueprint `json:"fields"`
}

type FieldBlueprint struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // e.g., string, number, boolean, json
	Required bool   `json:"required"`
}
