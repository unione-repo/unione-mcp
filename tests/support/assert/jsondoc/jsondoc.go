package jsondoc

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

// Includes checks only the keys named in expected; every expected value goes
// through a JSON round-trip first, so Go ints and typed slices compare equal to
// the float64 and []any a decoded document carries.
func Includes(t *testing.T, got, expected map[string]any) {
	t.Helper()

	keys := make([]string, 0, len(expected))
	for key := range expected {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		want := normalize(t, expected[key])
		actual, present := got[key]
		if !present {
			t.Errorf("json lacks %q; got %v", key, got)
			continue
		}
		if !reflect.DeepEqual(actual, want) {
			t.Errorf("json[%q] = %#v, want %#v", key, actual, want)
		}
	}
}

func normalize(t *testing.T, value any) any {
	t.Helper()

	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("expected value %v is not JSON-encodable: %v", value, err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("expected value %s does not decode back: %v", raw, err)
	}
	return out
}
