package main

import (
	"reflect"
	"testing"
)

func TestParseCondition(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantKey string
		wantOp  string
		wantVal string
		wantErr bool
	}{
		{name: "equal", in: "verdict == bug", wantKey: "verdict", wantOp: "==", wantVal: "bug"},
		{name: "not equal", in: "verdict != bug", wantKey: "verdict", wantOp: "!=", wantVal: "bug"},
		{name: "no spaces", in: "a==b", wantKey: "a", wantOp: "==", wantVal: "b"},
		{name: "surrounding spaces", in: "   a == b   ", wantKey: "a", wantOp: "==", wantVal: "b"},
		{name: "double quoted value", in: `a == "not bug"`, wantKey: "a", wantOp: "==", wantVal: "not bug"},
		{name: "single quoted value", in: "a != 'x'", wantKey: "a", wantOp: "!=", wantVal: "x"},
		{name: "dotted output name", in: "triage.verdict == bug", wantKey: "triage.verdict", wantOp: "==", wantVal: "bug"},
		{name: "empty value", in: "a ==", wantKey: "a", wantOp: "==", wantVal: ""},
		{name: "uppercase name", in: "A == b", wantErr: true},
		{name: "single equals", in: "a = b", wantErr: true},
		{name: "name starts with digit", in: "1a == b", wantErr: true},
		{name: "no operator", in: "verdict", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, op, value, err := parseCondition(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseCondition(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if name != tt.wantKey || op != tt.wantOp || value != tt.wantVal {
				t.Errorf("parseCondition(%q) = (%q, %q, %q), want (%q, %q, %q)", tt.in, name, op, value, tt.wantKey, tt.wantOp, tt.wantVal)
			}
		})
	}
}

func TestFieldSchema(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    map[string]any
		wantErr bool
	}{
		{name: "text", in: "text", want: map[string]any{"type": "string"}},
		{name: "number with spaces", in: "  number ", want: map[string]any{"type": "number"}},
		{name: "bool", in: "bool", want: map[string]any{"type": "boolean"}},
		{name: "list", in: "list", want: map[string]any{"type": "array", "items": map[string]any{"type": "string"}}},
		{name: "enum", in: "bug | not_bug | unsure", want: map[string]any{"type": "string", "enum": []string{"bug", "not_bug", "unsure"}}},
		{name: "two option enum", in: "yes|no", want: map[string]any{"type": "string", "enum": []string{"yes", "no"}}},
		{name: "unknown word", in: "color", wantErr: true},
		{name: "capitalised keyword", in: "Text", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fieldSchema(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("fieldSchema(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("fieldSchema(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestReturnsSchema(t *testing.T) {
	got := returnsSchema(Fields{{Name: "count", Value: "number"}, {Name: "ok", Value: "bool"}})
	want := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"count": map[string]any{"type": "number"},
			"ok":    map[string]any{"type": "boolean"},
		},
		"required":             []string{"count", "ok"},
		"additionalProperties": false,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("returnsSchema() = %v, want %v", got, want)
	}
}

func TestReturnsSchemaWithoutFields(t *testing.T) {
	got := returnsSchema(nil)
	if got["type"] != "object" || got["additionalProperties"] != false {
		t.Errorf("returnsSchema(nil) = %v, want a closed object", got)
	}
	if required, _ := got["required"].([]string); len(required) != 0 {
		t.Errorf("returnsSchema(nil) required = %v, want none", required)
	}
}
