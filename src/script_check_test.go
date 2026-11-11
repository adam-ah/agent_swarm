package main

import (
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestScriptProblemsChecksBashAndEmbeddedScripts(t *testing.T) {
	tests := []struct {
		name string
		run  string
		want []string
	}{
		{"valid bash", "set -euo pipefail\necho {{who}} > {{run_folder}}/out\n", nil},
		{"broken bash", "if true; then\n  echo hi\nfi fi\n", []string{"line 3: bash: syntax error near unexpected token `fi'"}},
		{"python heredoc", "set -e\npython3 - {{who}} <<'PY'\nimport sys\nx = = 1\nPY\n", []string{"line 4: python3: invalid syntax"}},
		{"python heredoc reading a placeholder", "python3 - <<'PY'\nname = {{name}}\nprint(name)\nPY\n", nil},
		{"python -c", "echo start\npython3 -c 'import sys\nx = = 1'\n", []string{"line 3: python3: invalid syntax"}},
		{"python -c in double quotes", "python3 -B -c \"x = = 1\"\n", []string{"line 1: python3: invalid syntax"}},
		{"python after env", "env FOO=1 python3 -c 'x = = 1'\n", []string{"line 1: python3: invalid syntax"}},
		{"python here-string", "python3 <<< 'x = = 1'\n", []string{"line 1: python3: invalid syntax"}},
		{"python inside a function", "go() {\n  python3 -W ignore - <<'PY'\nx = = 1\nPY\n}\n", []string{"line 3: python3: invalid syntax"}},
		{"tab-stripped heredoc", "python3 - <<-'PY'\n\tif True:\n\t    pass\n\tPY\n", nil},
		{"broken tab-stripped heredoc", "python3 - <<-'PY'\n\tx = 1\n\ty = = 2\n\tPY\n", []string{"line 3: python3: invalid syntax"}},
		{"unquoted heredoc without expansions", "python3 - <<EOF\nx = = 1\nEOF\n", []string{"line 2: python3: invalid syntax"}},
		{"two broken scripts", "python3 -c 'x = = 1'\nbash -euo pipefail -c 'fi'\n", []string{"line 1: python3: invalid syntax", "line 2: bash: syntax error near unexpected token `fi'"}},
		{"bash -s heredoc", "bash -s -- a <<'SH'\nfi\nSH\n", []string{"line 2: bash: syntax error near unexpected token `fi'"}},
		{"bash reading its stdin", "bash <<'SH'\nfi\nSH\n", []string{"line 2: bash: syntax error near unexpected token `fi'"}},
		{"python written to a file, named by its delimiter", "helper={{run_folder}}/seed.py\ncat > \"$helper\" <<'PY'\nimport sys\nx = = 1\nPY\npython3 \"$helper\"\n", []string{"line 4: python3: invalid syntax"}},
		{"python written to a .py file", "cat <<'EOF' > {{run_folder}}/seed.py\nx = = 1\nEOF\n", []string{"line 2: python3: invalid syntax"}},
		{"bash appended with tee", "tee -a run.sh >/dev/null <<'EOF'\nfi\nEOF\n", []string{"line 2: bash: syntax error near unexpected token `fi'"}},
		{"data written with cat", "cat > \"$out\" <<'EOF'\nnot python (\nEOF\n", nil},
		{"heredoc as data for a python file", "python3 tool.py <<'EOF'\nnot python (\nEOF\n", nil},
		{"heredoc as data for python -m", "python3 -m json.tool <<'EOF'\n{ bad\nEOF\n", nil},
		{"heredoc as data for python -c", "python3 -c 'import sys' <<'EOF'\nnot python (\nEOF\n", nil},
		{"heredoc as data for a bash file", "bash build.sh <<'EOF'\nfi\nEOF\n", nil},
		{"heredoc after --", "python3 -- tool.py <<'EOF'\nnot python (\nEOF\n", nil},
		{"unquoted heredoc with an expansion", "python3 - <<EOF\nx = $count\nEOF\n", nil},
		{"unquoted heredoc with an escape", "python3 - <<EOF\nx = \\$count\nEOF\n", nil},
		{"heredoc on another fd", "python3 - 3<<'PY'\nx = = 1\nPY\n", nil},
		{"code from a placeholder", "bash -c {{command}} 2>&1 | tail -40\n", nil},
		{"code from a variable", "python3 -c \"$code\"\n", nil},
		{"missing interpreter", "python9.9 -c 'x = ('\n", nil},
		{"command from a variable", "\"$py\" -c 'x = ('\n", nil},
	}
	for _, tt := range tests {
		if got := scriptProblems(tt.run); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestScriptProblemsChecksZsh(t *testing.T) {
	if _, err := exec.LookPath("zsh"); err != nil {
		t.Skip("zsh is not installed")
	}
	got := scriptProblems("zsh -fc 'if true; then\necho\nfi fi'\n")
	if want := []string{"line 3: zsh: parse error near `fi'"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestScriptProblemsAreCachedByRun(t *testing.T) {
	run := "python3 -c 'x = = 1' # cached\n"
	first := scriptProblems(run)
	t.Setenv("PATH", t.TempDir())
	if again := scriptProblems(run); !reflect.DeepEqual(again, first) || len(first) != 1 {
		t.Errorf("first %q, again %q", first, again)
	}
	if fresh := scriptProblems("fi\n"); fresh != nil {
		t.Errorf("without bash on PATH got %q", fresh)
	}
}

func TestBrokenToolBlocksItsChains(t *testing.T) {
	core, project := surfaceProject(t)
	surfaceFiles(t, project, map[string]string{
		".swarm/tools/broken.json":  `{"title": "Broken", "run": "echo start\npython3 - <<'PY'\nx = = 1\nPY\n"}`,
		".swarm/chains/broken.json": `{"title": "Broken", "steps": [{"id": "go", "title": "Go", "tool": "broken"}]}`,
	})
	_, err := surfaceCLI(t, "check")
	surfaceFails(t, err, `broken (project): step "go": tools/broken.json line 3: python3: invalid syntax`)
	surfaceFails(t, err, "tools/broken.json (project): line 3: python3: invalid syntax")
	if _, refused := core.StartRun(project, "broken", nil); refused == nil || !strings.Contains(refused.Error(), "fix the chain first") {
		t.Errorf("StartRun = %v", refused)
	}
	draft := t.TempDir()
	surfaceFiles(t, draft, map[string]string{"echo.json": `{"title": "Echo", "run": "if true; then echo; fi fi"}`})
	_, err = surfaceCLI(t, "put", "tools/echo.json", "-file", filepath.Join(draft, "echo.json"))
	surfaceFails(t, err, "line 1: bash: syntax error near unexpected token `fi'")
	verdicts, err := core.CheckDraft(project, Draft{Tools: map[string]Tool{"broken": {Title: "Broken", Run: "python3 -c 'x = 1'"}}})
	if err != nil || len(verdicts["chains/broken.json"].Problems) != 0 {
		t.Errorf("fixed draft verdicts = %+v, %v", verdicts["chains/broken.json"], err)
	}
}
