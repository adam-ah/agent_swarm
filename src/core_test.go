package main

import "testing"

func TestSplitItem(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantKind string
		wantName string
		wantErr  bool
	}{
		{name: "settings file", path: "settings.json", wantKind: "settings", wantName: "settings"},
		{name: "settings short", path: "settings", wantKind: "settings", wantName: "settings"},
		{name: "chain", path: "chains/fix_bug.json", wantKind: "chains", wantName: "fix_bug"},
		{name: "tool", path: "tools/run-tests.json", wantKind: "tools", wantName: "run-tests"},
		{name: "prompt", path: "chains/fix/triage.md", wantKind: "prompts", wantName: "fix/triage"},
		{name: "prompt outside a chain", path: "prompts/triage.md", wantErr: true},
		{name: "chain with md extension", path: "chains/fix.md", wantErr: true},
		{name: "prompt with json extension", path: "chains/fix/triage.json", wantErr: true},
		{name: "prompt nested deeper", path: "chains/fix/a/triage.md", wantErr: true},
		{name: "uppercase name", path: "chains/Fix.json", wantErr: true},
		{name: "name with dot", path: "chains/a.b.json", wantErr: true},
		{name: "nested folder", path: "tools/a/b.json", wantErr: true},
		{name: "unknown kind", path: "other/x.json", wantErr: true},
		{name: "no name", path: "chains/.json", wantErr: true},
		{name: "no slash", path: "chains", wantErr: true},
		{name: "empty", path: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kind, name, err := splitItem(tt.path)
			if (err != nil) != tt.wantErr {
				t.Fatalf("splitItem(%q) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
			if kind != tt.wantKind || name != tt.wantName {
				t.Errorf("splitItem(%q) = (%q, %q), want (%q, %q)", tt.path, kind, name, tt.wantKind, tt.wantName)
			}
		})
	}
}

func TestStrictJSON(t *testing.T) {
	type item struct {
		Name string `json:"name"`
	}
	tests := []struct {
		name    string
		raw     string
		want    item
		wantErr bool
	}{
		{name: "known field", raw: `{"name":"fix"}`, want: item{Name: "fix"}},
		{name: "unknown field refused", raw: `{"name":"fix","shade":"red"}`, wantErr: true},
		{name: "not json", raw: `name: fix`, wantErr: true},
		{name: "wrong type", raw: `{"name":3}`, wantErr: true},
		{name: "empty", raw: ``, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got item
			err := strictJSON([]byte(tt.raw), &got)
			if (err != nil) != tt.wantErr {
				t.Fatalf("strictJSON(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("strictJSON(%q) = %+v, want %+v", tt.raw, got, tt.want)
			}
		})
	}
}
