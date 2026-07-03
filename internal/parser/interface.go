package parser

// Parser is the interface that all config parsers must implement
type Parser interface {
	// Parse converts the raw config file content into a structured format (JSON-compatible map/struct)
	Parse(content string) (interface{}, error)

	// Generate converts the structured data back into a config file string
	Generate(data interface{}) (string, error)

	// Validate checks if the content is syntactically correct
	// Ideally, this runs the actual service's validation command (e.g., nginx -t)
	Validate(content string) error
}

// Block represents a parsed block in a config file (e.g., "server { ... }")
type Block struct {
	Name       string            `json:"name"`
	Parameters []string          `json:"parameters"`
	Directives map[string]string `json:"directives"`
	Children   []Block           `json:"children"`
}
