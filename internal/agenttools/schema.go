package agenttools

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

func schemaFor(sample any) (json.RawMessage, error) {
	t := reflect.TypeOf(sample)
	if t == nil {
		return json.RawMessage(`{"type":"object","properties":{}}`), nil
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	s, err := jsonschema.ForType(t, nil)
	if err != nil {
		return nil, fmt.Errorf("推导 schema: %w", err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	return raw, nil
}
