package main

import (
	"reflect"
	"testing"
)

func TestPromptReturns(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		want     Fields
		problems []string
	}{
		{name: "no return section", text: "Fix it.\n\n- `verdict`: not a field here\n"},
		{name: "text fields", text: "Fix it.\n\n## Return\n\n- `summary`: one sentence\n- `commit_message`: one line\n",
			want: Fields{{Name: "summary", Value: "text"}, {Name: "commit_message", Value: "text"}}},
		{name: "choices read from the description", text: "## Return\n\n- `verdict`: `APPROVE` or `CHANGES`\n- `size`: `small`, `medium` or `large`.\n",
			want: Fields{{Name: "verdict", Value: "APPROVE | CHANGES"}, {Name: "size", Value: "small | medium | large"}}},
		{name: "backticks inside prose stay text", text: "## Return\n\n- `findings`: what is wrong, each with `path:line` and why, or `none`\n",
			want: Fields{{Name: "findings", Value: "text"}}},
		{name: "kind in brackets", text: "## Returns\n\n- `notes` (list): one line each\n- `count` (number)\n- `ok` (bool): did it pass\n- `level` (low | high): how bad\n",
			want: Fields{{Name: "notes", Value: "list"}, {Name: "count", Value: "number"}, {Name: "ok", Value: "bool"}, {Name: "level", Value: "low | high"}}},
		{name: "section ends at the next heading", text: "## Return\n\n- `summary`: first\n\n## Notes\n\n- `other`: not a field\n",
			want: Fields{{Name: "summary", Value: "text"}}},
		{name: "prose and nested items are skipped", text: "## Return\n\nThese fields:\n\n- `summary`: first\n  - more about it\n",
			want: Fields{{Name: "summary", Value: "text"}}},
		{name: "code blocks are skipped", text: "```\n## Return\n- `x`: no\n```\n## Return\n- `y`: yes\n",
			want: Fields{{Name: "y", Value: "text"}}},
		{name: "item without backticks", text: "## Return\n\n- verdict: APPROVE or CHANGES\n",
			problems: []string{"## Return: cannot read \"- verdict: APPROVE or CHANGES\"; write each field as - `name`: what it is, with a lowercase name"}},
		{name: "name that cannot be a placeholder", text: "## Return\n\n- `filesChanged`: the files\n",
			problems: []string{"## Return: cannot read \"- `filesChanged`: the files\"; write each field as - `name`: what it is, with a lowercase name"}},
		{name: "unknown kind", text: "## Return\n\n- `color` (red): its shade\n",
			problems: []string{`## Return: color: "red" is not text, number, bool, list or A | B | C`}},
		{name: "field listed twice", text: "## Return\n\n- `summary`: one\n- `summary`: two\n",
			want: Fields{{Name: "summary", Value: "text"}}, problems: []string{"## Return: summary is listed twice"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, problems := promptReturns(tt.text)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("promptReturns() fields = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(problems, tt.problems) {
				t.Errorf("promptReturns() problems = %q, want %q", problems, tt.problems)
			}
		})
	}
}

func TestSamplePromptReturns(t *testing.T) {
	tests := []struct {
		prompt string
		want   Fields
	}{
		{prompt: "make-change/change", want: Fields{{"summary", "text"}, {"files_changed", "list"}, {"test_command", "text"}, {"commit_message", "text"}}},
		{prompt: "make-change/review-change", want: Fields{{"verdict", "APPROVE | CHANGES"}, {"summary", "text"}, {"findings", "text"}}},
		{prompt: "make-change/learn", want: Fields{{"summary", "text"}, {"notes", "list"}}},
		{prompt: "edit-chain/edit-chain", want: Fields{{"summary", "text"}, {"files", "list"}}},
	}
	for _, tt := range tests {
		t.Run(tt.prompt, func(t *testing.T) {
			raw, err := sampleFiles.ReadFile("samples/chains/" + tt.prompt + ".md")
			if err != nil {
				t.Fatal(err)
			}
			got, problems := promptReturns(string(raw))
			if !reflect.DeepEqual(got, tt.want) || problems != nil {
				t.Errorf("promptReturns() = %v, %q, want %v and no problems", got, problems, tt.want)
			}
		})
	}
}
