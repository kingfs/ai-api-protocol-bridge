package protocolbridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// This file holds the test-only plumbing that loads the vendored official
// protocol schemas under protocols/ and validates JSON values against them.
// Nothing here is part of the library's runtime API.

type schemaBundle struct {
	ID         string                     `json:"id"`
	Protocol   string                     `json:"protocol"`
	Endpoint   string                     `json:"endpoint"`
	Dialect    string                     `json:"dialect"`
	Documents  map[string]json.RawMessage `json:"documents"`
	Source     map[string]any             `json:"source"`
	Components []string                   `json:"components_in_scope"`
	Defs       map[string]any             `json:"$defs"`
}

func loadSchemaBundle(t *testing.T, relativePath string) *schemaBundle {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(relativePath))
	if err != nil {
		t.Fatalf("read schema bundle %s: %v", relativePath, err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var bundle schemaBundle
	if err := decoder.Decode(&bundle); err != nil {
		t.Fatalf("decode schema bundle %s: %v", relativePath, err)
	}
	if len(bundle.Defs) == 0 {
		t.Fatalf("schema bundle %s has no $defs", relativePath)
	}
	return &bundle
}

func (b *schemaBundle) document(t *testing.T, slot string) any {
	t.Helper()
	raw, ok := b.Documents[slot]
	if !ok {
		t.Fatalf("%s: no document %q", b.ID, slot)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("%s: decode document %q: %v", b.ID, slot, err)
	}
	return value
}

// resolve follows a local $ref chain until it reaches a concrete schema.
func (b *schemaBundle) resolve(t *testing.T, schema any) any {
	t.Helper()
	for range 32 {
		object, ok := schema.(map[string]any)
		if !ok {
			return schema
		}
		ref, ok := object["$ref"].(string)
		if !ok {
			return schema
		}
		const prefix = "#/$defs/"
		if !strings.HasPrefix(ref, prefix) {
			t.Fatalf("%s: unsupported ref %q", b.ID, ref)
		}
		name := strings.TrimPrefix(ref, prefix)
		target, ok := b.Defs[name]
		if !ok {
			t.Fatalf("%s: unresolved ref %q", b.ID, ref)
		}
		schema = target
	}
	t.Fatalf("%s: $ref chain too deep", b.ID)
	return nil
}

// properties returns the merged property table of a schema, following $refs and
// allOf composition but not anyOf/oneOf branches.
func (b *schemaBundle) properties(t *testing.T, schema any) map[string]any {
	t.Helper()
	merged := map[string]any{}
	b.collectProperties(t, schema, merged, map[string]bool{})
	return merged
}

func (b *schemaBundle) collectProperties(t *testing.T, schema any, out map[string]any, seen map[string]bool) {
	t.Helper()
	object, ok := schema.(map[string]any)
	if !ok {
		return
	}
	if ref, ok := object["$ref"].(string); ok {
		if seen[ref] {
			return
		}
		seen[ref] = true
	}
	resolved := b.resolve(t, object)
	resolvedObject, ok := resolved.(map[string]any)
	if !ok {
		return
	}
	if properties, ok := resolvedObject["properties"].(map[string]any); ok {
		for name, value := range properties {
			out[name] = value
		}
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		branches, ok := resolvedObject[key].([]any)
		if !ok || key != "allOf" {
			continue
		}
		for _, branch := range branches {
			b.collectProperties(t, branch, out, seen)
		}
	}
}

func (b *schemaBundle) required(t *testing.T, schema any) []string {
	t.Helper()
	names := map[string]bool{}
	b.collectRequired(t, schema, names, map[string]bool{})
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func (b *schemaBundle) collectRequired(t *testing.T, schema any, out map[string]bool, seen map[string]bool) {
	t.Helper()
	object, ok := schema.(map[string]any)
	if !ok {
		return
	}
	if ref, ok := object["$ref"].(string); ok {
		if seen[ref] {
			return
		}
		seen[ref] = true
	}
	resolved := b.resolve(t, object)
	resolvedObject, ok := resolved.(map[string]any)
	if !ok {
		return
	}
	if required, ok := resolvedObject["required"].([]any); ok {
		for _, name := range required {
			if text, ok := name.(string); ok {
				out[text] = true
			}
		}
	}
	if branches, ok := resolvedObject["allOf"].([]any); ok {
		for _, branch := range branches {
			b.collectRequired(t, branch, out, seen)
		}
	}
}

// validate checks a decoded JSON value against a schema and appends every
// violation it finds to problems. It implements the subset of JSON Schema the
// vendored bundles use: $ref, allOf, anyOf, oneOf, type (including OAS 3.0
// nullable), enum, const, required, properties, additionalProperties, items and
// the numeric/string/array bounds. It is deliberately permissive about
// “format“ and about unknown keywords.
func (b *schemaBundle) validate(schema any, value any) []string {
	problems := make([]string, 0)
	b.validateAt(schema, value, "", &problems)
	return problems
}

func (b *schemaBundle) validateAt(schema any, value any, path string, problems *[]string) {
	object, ok := schema.(map[string]any)
	if !ok {
		return
	}
	object = b.resolveNoFail(object)
	if object == nil {
		return
	}

	if nullable, _ := object["nullable"].(bool); nullable && value == nil {
		return
	}
	if branches, ok := object["allOf"].([]any); ok {
		for _, branch := range branches {
			b.validateAt(branch, value, path, problems)
		}
	}
	for _, keyword := range []string{"anyOf", "oneOf"} {
		branches, ok := object[keyword].([]any)
		if !ok {
			continue
		}
		matches := 0
		var closest []string
		for _, branch := range branches {
			branchProblems := b.validate(branch, value)
			if len(branchProblems) == 0 {
				matches++
				continue
			}
			if closest == nil || len(branchProblems) < len(closest) {
				closest = branchProblems
			}
		}
		if matches == 0 {
			detail := ""
			if len(closest) > 0 {
				if len(closest) > 3 {
					closest = closest[:3]
				}
				detail = "; closest branch: " + strings.Join(closest, "; ")
			}
			*problems = append(*problems, fmt.Sprintf("%s: matches no %s branch%s", displayPath(path), keyword, detail))
		}
	}
	if enum, ok := object["enum"].([]any); ok {
		if !containsJSON(enum, value) {
			*problems = append(*problems, fmt.Sprintf("%s: %s not in enum %v", displayPath(path), compactJSON(value), compactJSON(enum)))
		}
	}
	if constant, ok := object["const"]; ok {
		if !jsonEqual(constant, value) {
			*problems = append(*problems, fmt.Sprintf("%s: %s != const %s", displayPath(path), compactJSON(value), compactJSON(constant)))
		}
	}
	if types, ok := object["type"].([]any); ok {
		if !matchesAnyType(types, value) {
			*problems = append(*problems, fmt.Sprintf("%s: %s is not one of %v", displayPath(path), compactJSON(value), types))
		}
	} else if typeName, ok := object["type"].(string); ok {
		if !matchesType(typeName, value) {
			*problems = append(*problems, fmt.Sprintf("%s: %s is not %s", displayPath(path), compactJSON(value), typeName))
		}
	}
	if required, ok := object["required"].([]any); ok {
		asObject, isObject := value.(map[string]any)
		if isObject {
			for _, name := range required {
				text, ok := name.(string)
				if !ok {
					continue
				}
				if _, present := asObject[text]; !present {
					*problems = append(*problems, fmt.Sprintf("%s: missing required property %q", displayPath(path), text))
				}
			}
		}
	}
	if properties, ok := object["properties"].(map[string]any); ok {
		if asObject, isObject := value.(map[string]any); isObject {
			for name, property := range properties {
				child, present := asObject[name]
				if !present {
					continue
				}
				b.validateAt(property, child, path+"/"+name, problems)
			}
		}
	}
	if additional, ok := object["additionalProperties"]; ok {
		if allowed, isBool := additional.(bool); isBool && !allowed {
			if asObject, isObject := value.(map[string]any); isObject {
				for name := range asObject {
					if !b.hasProperty(object, name, map[string]bool{}) {
						*problems = append(*problems, fmt.Sprintf("%s: unexpected property %q", displayPath(path), name))
					}
				}
			}
		}
	}
	if items, ok := object["items"]; ok {
		if asArray, isArray := value.([]any); isArray {
			for index, item := range asArray {
				b.validateAt(items, item, fmt.Sprintf("%s/%d", path, index), problems)
			}
		}
	}
	if minimum, ok := numberValue(object["minimum"]); ok {
		if number, isNumber := numberValue(value); isNumber && number < minimum {
			*problems = append(*problems, fmt.Sprintf("%s: %v < minimum %v", displayPath(path), number, minimum))
		}
	}
	if maximum, ok := numberValue(object["maximum"]); ok {
		if number, isNumber := numberValue(value); isNumber && number > maximum {
			*problems = append(*problems, fmt.Sprintf("%s: %v > maximum %v", displayPath(path), number, maximum))
		}
	}
	if text, isString := value.(string); isString {
		if minimum, ok := numberValue(object["minLength"]); ok && float64(len([]rune(text))) < minimum {
			*problems = append(*problems, fmt.Sprintf("%s: shorter than minLength %v", displayPath(path), minimum))
		}
		if maximum, ok := numberValue(object["maxLength"]); ok && float64(len([]rune(text))) > maximum {
			*problems = append(*problems, fmt.Sprintf("%s: longer than maxLength %v", displayPath(path), maximum))
		}
	}
	if asArray, isArray := value.([]any); isArray {
		if minimum, ok := numberValue(object["minItems"]); ok && float64(len(asArray)) < minimum {
			*problems = append(*problems, fmt.Sprintf("%s: fewer than minItems %v", displayPath(path), minimum))
		}
		if maximum, ok := numberValue(object["maxItems"]); ok && float64(len(asArray)) > maximum {
			*problems = append(*problems, fmt.Sprintf("%s: more than maxItems %v", displayPath(path), maximum))
		}
	}
}

func (b *schemaBundle) resolveNoFail(schema map[string]any) map[string]any {
	for range 32 {
		ref, ok := schema["$ref"].(string)
		if !ok {
			return schema
		}
		const prefix = "#/$defs/"
		if !strings.HasPrefix(ref, prefix) {
			return nil
		}
		target, ok := b.Defs[strings.TrimPrefix(ref, prefix)]
		if !ok {
			return nil
		}
		next, ok := target.(map[string]any)
		if !ok {
			return nil
		}
		schema = next
	}
	return nil
}

// hasProperty reports whether a schema declares a property, following $refs and
// allOf composition. It is used by the additionalProperties check.
func (b *schemaBundle) hasProperty(schema map[string]any, name string, seen map[string]bool) bool {
	if ref, ok := schema["$ref"].(string); ok {
		if seen[ref] {
			return false
		}
		seen[ref] = true
		resolved := b.resolveNoFail(schema)
		if resolved == nil {
			return false
		}
		schema = resolved
	}
	if properties, ok := schema["properties"].(map[string]any); ok {
		if _, ok := properties[name]; ok {
			return true
		}
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		branches, ok := schema[key].([]any)
		if !ok {
			continue
		}
		for _, branch := range branches {
			asObject, ok := branch.(map[string]any)
			if !ok {
				continue
			}
			if b.hasProperty(asObject, name, seen) {
				return true
			}
		}
	}
	return false
}

func displayPath(path string) string {
	if path == "" {
		return "(root)"
	}
	return path
}

func matchesAnyType(types []any, value any) bool {
	for _, entry := range types {
		if name, ok := entry.(string); ok && matchesType(name, value) {
			return true
		}
	}
	return false
}

func matchesType(name string, value any) bool {
	switch name {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	case "number":
		_, ok := numberValue(value)
		return ok
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return false
		}
		return !strings.ContainsAny(number.String(), ".eE")
	default:
		return true
	}
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	default:
		return 0, false
	}
}

func containsJSON(values []any, value any) bool {
	for _, candidate := range values {
		if jsonEqual(candidate, value) {
			return true
		}
	}
	return false
}

func jsonEqual(left, right any) bool {
	return compactJSON(left) == compactJSON(right)
}

func compactJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(raw)
}

// decodeJSONObject decodes raw JSON preserving number fidelity.
func decodeJSONObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	return value
}

func decodeJSONValue(t *testing.T, raw []byte) any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	return value
}
