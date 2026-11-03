package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func shape(steps []*StepRun) []string {
	out := make([]string, 0, len(steps))
	for _, sr := range steps {
		out = append(out, fmt.Sprintf("%s %s %q %v rounds=%q earlier=%d", sr.Path, sr.Status, sr.Headline, sr.Output, sr.Rounds, len(sr.Earlier)))
		out = append(out, shape(sr.Children)...)
	}
	return out
}

func cardLibrary(t *testing.T) *Library {
	t.Helper()
	lib := sequenceLibrary(t)
	lib.Tools["cards"] = Tool{Run: `echo '{"items": ["one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"], "word": "board"}'`}
	lib.Tools["work"] = Tool{Run: `echo '{"v": "{{value}}"}'`}
	lib.Tools["review"] = Tool{Run: `value={{value}}; n=$(( $(cat {{run_folder}}/n 2>/dev/null || echo 0) + 1 )); echo $n > {{run_folder}}/n
v=CHANGES; [ $n -lt $(( ${#value} - 1 )) ] || v=APPROVE; echo "{\"verdict\": \"$v\", \"round\": \"$n\"}"`}
	lib.Tools["land"] = Tool{Run: `[ {{value}} != five ] || { echo "five never lands" >&2; exit 1; }; echo landed`}
	lib.Tools["tidy"] = Tool{Run: `echo '{"saw": "{{work.v}} {{review.verdict}} {{list.word}}"}'`}
	lib.Chains["card"] = &Chain{Name: "card", Inputs: []Input{{Name: "value"}}, Steps: []Step{
		{ID: "work", Title: "Work", Tool: "work"},
		{ID: "review", Title: "Review", Tool: "review"},
		{ID: "fix", Title: "Fix", Tool: "say", With: map[string]string{"value": "fixed {{review.round}}"}, When: "review.verdict == CHANGES"},
		{ID: "again", Title: "Review again", Repeat: "review", Rounds: 4, When: "review.verdict == CHANGES"},
		{ID: "land", Title: "Land", Tool: "land", When: "review.verdict == APPROVE"},
		{ID: "tidy", Title: "Tidy", Tool: "tidy", Always: true},
	}}
	return lib
}

func TestParallelItemsEndLikeSequentialOnes(t *testing.T) {
	shapes := make([][]string, 0, 2)
	for _, parallel := range []bool{false, true} {
		lib := cardLibrary(t)
		run, _ := execute(t, lib, &Chain{Name: "main", Steps: []Step{
			{ID: "list", Title: "List", Tool: "cards"},
			{ID: "each", Title: "Each", Chain: "card", Parallel: parallel, MaxParallel: 4, KeepGoing: true, With: map[string]string{"value": "{{list.items}}"}},
			{ID: "after", Title: "After", Tool: "say", With: map[string]string{"value": "{{each.summary}}"}},
		}})
		if run.Status != "done" {
			t.Fatalf("parallel %v: run %s (%s)", parallel, run.Status, run.Reason)
		}
		shapes = append(shapes, shape(run.Steps))
	}
	if !slices.Equal(shapes[0], shapes[1]) {
		for i := range min(len(shapes[0]), len(shapes[1])) {
			if shapes[0][i] != shapes[1][i] {
				t.Fatalf("sequential: %s\nparallel:   %s", shapes[0][i], shapes[1][i])
			}
		}
		t.Fatalf("sequential has %d steps, parallel %d", len(shapes[0]), len(shapes[1]))
	}
	if got := shapes[1][len(shapes[1])-1]; !strings.HasPrefix(got, `after done "card ran 10 times: 9 done, 1 failed" map[output:card ran 10 times: 9 done, 1 failed]`) {
		t.Errorf("after = %s", got)
	}
}
