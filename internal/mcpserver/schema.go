package mcpserver

import (
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

// The API takes and returns every timestamp as UTC "YYYY-MM-DD hh:mm:ss".
const (
	timeLayout  = "2006-01-02 15:04:05"
	timePattern = `^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`
	uuidPattern = `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`
)

func property(s *jsonschema.Schema, name string) *jsonschema.Schema {
	p, ok := s.Properties[name]
	if !ok {
		panic(fmt.Sprintf("schema has no property %q", name))
	}
	return p
}

func enum(s *jsonschema.Schema, name string, values ...string) {
	p := property(s, name)
	p.Enum = make([]any, len(values))
	for i, v := range values {
		p.Enum[i] = v
	}
}

func itemsEnum(s *jsonschema.Schema, name string, values ...string) {
	items := property(s, name).Items
	if items == nil {
		panic(fmt.Sprintf("property %q is not an array", name))
	}
	items.Enum = make([]any, len(values))
	for i, v := range values {
		items.Enum[i] = v
	}
}

// defaultPageSize is the page size a list tool asks for when the client names none.
const defaultPageSize = 50

// pageLimit bounds a list tool's "limit" to 1..max and defaults it to defaultPageSize.
func pageLimit(s *jsonschema.Schema, max float64) {
	between(s, "limit", 1, max)
	raw, err := json.Marshal(defaultPageSize)
	if err != nil {
		panic(fmt.Sprintf("default for \"limit\": %v", err))
	}
	property(s, "limit").Default = raw
}

func between(s *jsonschema.Schema, name string, min, max float64) {
	p := property(s, name)
	p.Minimum = &min
	p.Maximum = &max
}

func atLeast(s *jsonschema.Schema, name string, min float64) {
	property(s, name).Minimum = &min
}

func pattern(s *jsonschema.Schema, name, re string) {
	property(s, name).Pattern = re
}

func minLength(s *jsonschema.Schema, name string, n int) {
	property(s, name).MinLength = &n
}

func nonEmpty(q map[string]any, key string, value string) {
	if value != "" {
		q[key] = value
	}
}

func positive(q map[string]any, key string, value int64) {
	if value > 0 {
		q[key] = value
	}
}

func present[T any](q map[string]any, key string, value *T) {
	if value != nil {
		q[key] = *value
	}
}

func listedStrings(q map[string]any, key string, values []string) {
	if len(values) > 0 {
		q[key] = values
	}
}
