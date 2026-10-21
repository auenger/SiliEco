// Package workflowcontract defines the restricted SOP v1 contract shared by
// local preflight and authoritative server validation. It performs no I/O;
// resource authorization and immutable storage verification belong to callers.
package workflowcontract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"sort"
	"strings"
)

const Version = "silieco.stage/v1"

// Schema is deliberately a subset, not an implementation of JSON Schema.
// Objects are closed: properties absent from Properties are always rejected.
type Schema struct {
	Type       string            `json:"type"`
	Properties map[string]Schema `json:"properties,omitempty"`
	Required   []string          `json:"required,omitempty"`
	Items      *Schema           `json:"items,omitempty"`
	Enum       []json.RawMessage `json:"enum,omitempty"`
	Minimum    *json.Number      `json:"minimum,omitempty"`
	Maximum    *json.Number      `json:"maximum,omitempty"`
	MinItems   *int              `json:"min_items,omitempty"`
	MaxItems   *int              `json:"max_items,omitempty"`
}

type ArtifactRequirement struct {
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	MediaType     string `json:"media_type"`
	SchemaVersion string `json:"schema_version"`
	Required      bool   `json:"required"`
}

type Contract struct {
	SchemaVersion string                `json:"schema_version"`
	Data          Schema                `json:"data"`
	Artifacts     []ArtifactRequirement `json:"artifacts,omitempty"`
}

// Violation paths use JSON Pointer so UI and CLI can identify the same field.
type Violation struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func strictDecode(raw []byte, target any) error {
	if len(raw) > 1<<20 {
		return fmt.Errorf("JSON exceeds 1 MiB limit")
	}
	if err := checkJSON(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected exactly one JSON value")
	}
	return nil
}

func Parse(raw []byte) (Contract, error) {
	var contract Contract
	if err := strictDecode(raw, &contract); err != nil {
		return Contract{}, err
	}
	if err := contract.Check(); err != nil {
		return Contract{}, err
	}
	return contract, nil
}

func (c Contract) Check() error {
	if c.SchemaVersion != Version {
		return fmt.Errorf("unsupported schema_version %q", c.SchemaVersion)
	}
	nodes := 0
	if err := c.Data.check("/data", 0, &nodes); err != nil {
		return err
	}
	if c.Data.Type != "object" {
		return fmt.Errorf("/data must describe an object")
	}
	names := map[string]bool{}
	for i, a := range c.Artifacts {
		if strings.TrimSpace(a.Name) == "" || strings.TrimSpace(a.Kind) == "" || strings.TrimSpace(a.MediaType) == "" || strings.TrimSpace(a.SchemaVersion) == "" {
			return fmt.Errorf("/artifacts/%d requires name, kind, media_type and schema_version", i)
		}
		if names[a.Name] {
			return fmt.Errorf("duplicate artifact name %q", a.Name)
		}
		names[a.Name] = true
	}
	return nil
}

func pointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func (s Schema) check(path string, depth int, nodes *int) error {
	*nodes++
	if depth > 32 || *nodes > 1024 {
		return fmt.Errorf("%s exceeds contract complexity limit", path)
	}
	switch s.Type {
	case "object", "array", "string", "boolean", "number", "integer":
	default:
		return fmt.Errorf("%s/type is unsupported: %q", path, s.Type)
	}
	if s.Type != "object" && (s.Properties != nil || s.Required != nil) {
		return fmt.Errorf("%s: properties and required require object type", path)
	}
	if s.Type != "array" && (s.Items != nil || s.MinItems != nil || s.MaxItems != nil) {
		return fmt.Errorf("%s: items and item limits require array type", path)
	}
	if s.Type != "number" && s.Type != "integer" && (s.Minimum != nil || s.Maximum != nil) {
		return fmt.Errorf("%s: numeric limits require number or integer type", path)
	}
	if s.Minimum != nil && number(*s.Minimum) == nil || s.Maximum != nil && number(*s.Maximum) == nil {
		return fmt.Errorf("%s: invalid numeric limit", path)
	}
	if s.Minimum != nil && s.Maximum != nil && number(*s.Minimum).Cmp(number(*s.Maximum)) > 0 {
		return fmt.Errorf("%s: minimum exceeds maximum", path)
	}
	if s.MinItems != nil && *s.MinItems < 0 || s.MaxItems != nil && *s.MaxItems < 0 {
		return fmt.Errorf("%s: item limits must be nonnegative", path)
	}
	if s.MinItems != nil && s.MaxItems != nil && *s.MinItems > *s.MaxItems {
		return fmt.Errorf("%s: min_items exceeds max_items", path)
	}
	required := map[string]bool{}
	for _, name := range s.Required {
		if _, ok := s.Properties[name]; !ok || required[name] {
			return fmt.Errorf("%s/required contains unknown or duplicate property %q", path, name)
		}
		required[name] = true
	}
	for _, name := range sortedKeys(s.Properties) {
		if err := s.Properties[name].check(path+"/properties/"+pointer(name), depth+1, nodes); err != nil {
			return err
		}
	}
	if s.Type == "array" {
		if s.Items == nil {
			return fmt.Errorf("%s/items is required", path)
		}
		if err := s.Items.check(path+"/items", depth+1, nodes); err != nil {
			return err
		}
	}
	if s.Enum != nil {
		if len(s.Enum) == 0 {
			return fmt.Errorf("%s/enum cannot be empty", path)
		}
		if s.Type == "object" || s.Type == "array" {
			return fmt.Errorf("%s/enum requires a scalar type", path)
		}
		withoutEnum := s
		withoutEnum.Enum = nil
		for _, raw := range s.Enum {
			var value any
			if err := strictDecode(raw, &value); err != nil {
				return fmt.Errorf("%s/enum: %w", path, err)
			}
			if violations := withoutEnum.validate(value, path); len(violations) > 0 {
				return fmt.Errorf("%s/enum: %s", path, violations[0].Message)
			}
		}
	}
	return nil
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Bound numeric exponents before arbitrary precision conversion to avoid
// unbounded allocation from otherwise valid JSON such as 1e999999999.
func number(n json.Number) *big.Rat {
	text := n.String()
	if len(text) > 256 || !json.Valid([]byte(text)) || len(text) == 0 || (text[0] != '-' && (text[0] < '0' || text[0] > '9')) {
		return nil
	}
	if pos := strings.IndexAny(text, "eE"); pos >= 0 {
		exponent, ok := new(big.Int).SetString(text[pos+1:], 10)
		if !ok || !exponent.IsInt64() || exponent.Int64() < -1024 || exponent.Int64() > 1024 {
			return nil
		}
	}
	value, ok := new(big.Rat).SetString(text)
	if !ok {
		return nil
	}
	return value
}

func (c Contract) ValidateData(raw []byte) ([]Violation, error) {
	if err := c.Check(); err != nil {
		return nil, err
	}
	var value any
	if err := strictDecode(raw, &value); err != nil {
		return []Violation{{"/data", "invalid_json", err.Error()}}, nil
	}
	return c.Data.validate(value, "/data"), nil
}

func (s Schema) validate(value any, path string) []Violation {
	invalid := func(code, message string) []Violation { return []Violation{{path, code, message}} }
	var violations []Violation
	switch s.Type {
	case "object":
		object, ok := value.(map[string]any)
		if !ok || object == nil {
			return invalid("type", "expected object")
		}
		for _, name := range s.Required {
			if _, exists := object[name]; !exists {
				violations = append(violations, Violation{path + "/" + pointer(name), "required", "required field is missing"})
			}
		}
		for _, name := range sortedKeys(object) {
			field, exists := s.Properties[name]
			if !exists {
				violations = append(violations, Violation{path + "/" + pointer(name), "unknown_field", "field is not declared in contract"})
				continue
			}
			violations = append(violations, field.validate(object[name], path+"/"+pointer(name))...)
		}
	case "array":
		array, ok := value.([]any)
		if !ok {
			return invalid("type", "expected array")
		}
		if s.MinItems != nil && len(array) < *s.MinItems || s.MaxItems != nil && len(array) > *s.MaxItems {
			violations = append(violations, Violation{path, "item_count", "array length is outside contract limits"})
		}
		for i, item := range array {
			violations = append(violations, s.Items.validate(item, fmt.Sprintf("%s/%d", path, i))...)
		}
	case "string":
		if _, ok := value.(string); !ok {
			return invalid("type", "expected string")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return invalid("type", "expected boolean")
		}
	case "number", "integer":
		n, ok := value.(json.Number)
		if !ok {
			return invalid("type", "expected "+s.Type)
		}
		rational := number(n)
		if rational == nil {
			return invalid("number_range", "number exceeds supported precision or exponent")
		}
		if s.Type == "integer" && !rational.IsInt() {
			return invalid("type", "expected integer")
		}
		if s.Minimum != nil && rational.Cmp(number(*s.Minimum)) < 0 || s.Maximum != nil && rational.Cmp(number(*s.Maximum)) > 0 {
			violations = append(violations, Violation{path, "range", "number is outside contract limits"})
		}
	}
	if s.Enum != nil {
		found := false
		for _, raw := range s.Enum {
			var candidate any
			_ = strictDecode(raw, &candidate)
			if a, ok := value.(json.Number); ok {
				if b, ok := candidate.(json.Number); ok && number(a) != nil && number(a).Cmp(number(b)) == 0 {
					found = true
				}
			} else if value == candidate {
				found = true
			}
		}
		if !found {
			violations = append(violations, Violation{path, "enum", "value is not an allowed choice"})
		}
	}
	return violations
}

// Reject duplicate keys before decoding so a signed or reviewed payload cannot
// have different meanings for two JSON consumers.
func checkJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var visit func(int) error
	visit = func(depth int) error {
		if depth > 64 {
			return fmt.Errorf("JSON nesting exceeds 64 levels")
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			keys := map[string]bool{}
			for d.More() {
				keyToken, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("expected object key")
				}
				if keys[key] {
					return fmt.Errorf("duplicate JSON key %q", key)
				}
				keys[key] = true
				if err := visit(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := visit(depth + 1); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unexpected JSON delimiter")
		}
		_, err = d.Token()
		return err
	}
	if err := visit(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("expected exactly one JSON value")
	}
	return nil
}
