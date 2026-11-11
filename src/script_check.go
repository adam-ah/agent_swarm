package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"mvdan.cc/sh/v3/syntax"
)

type script struct {
	interpreter string
	code        string
	line        uint
}

const compilePython = `import sys
try:
    compile(sys.stdin.read(), "script", "exec")
except SyntaxError as e:
    sys.stderr.write("line %s: %s\n" % (e.lineno, e.msg))
    sys.exit(3)
`

const syntaxTimeout = 10 * time.Second

var (
	pythonName  = regexp.MustCompile(`^python[0-9.]*$`)
	pythonFlags = regexp.MustCompile(`^-[A-Za-z]+$`)
	envAssign   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	errorLine   = regexp.MustCompile(`(?:^|[: ])(?:line )?(\d+): (.*)`)
	shells      = map[string]bool{"bash": true, "sh": true, "dash": true, "zsh": true}
	wrappers    = map[string]bool{"env": true, "exec": true, "command": true, "nohup": true}
	scriptKinds = map[string]string{"py": "python3", "python": "python3", "python3": "python3", "sh": "bash", "bash": "bash", "zsh": "zsh"}
	checked     sync.Map
)

func scriptProblems(run string) []string {
	if cached, ok := checked.Load(run); ok {
		problems, _ := cached.([]string)
		return problems
	}
	filled := placeholder.ReplaceAllString(run, "'x'")
	var problems []string
	if problem := syntaxProblem(script{"bash", filled, 1}); problem != "" {
		problems = []string{problem}
	} else {
		for _, s := range embeddedScripts(filled) {
			if problem := syntaxProblem(s); problem != "" {
				problems = append(problems, problem)
			}
		}
	}
	checked.Store(run, problems)
	return problems
}

func embeddedScripts(code string) []script {
	file, err := syntax.NewParser().Parse(strings.NewReader(code), "")
	if err != nil {
		return nil
	}
	var found []script
	syntax.Walk(file, func(node syntax.Node) bool {
		if stmt, ok := node.(*syntax.Stmt); ok {
			found = append(found, stmtScripts(stmt)...)
		}
		return true
	})
	return found
}

func stmtScripts(stmt *syntax.Stmt) []script {
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok {
		return nil
	}
	name, args := command(call.Args)
	var code *syntax.Word
	var stdin bool
	switch base := filepath.Base(name); {
	case name == "":
		return nil
	case base == "cat" || base == "tee":
		return writtenScripts(stmt, args)
	case pythonName.MatchString(base):
		code, stdin = pythonCode(args)
	case shells[base]:
		code, stdin = shellCode(args)
	default:
		return nil
	}
	if code != nil {
		if text, ok := literal(code); ok {
			return []script{{name, text, code.Pos().Line()}}
		}
		return nil
	}
	if !stdin {
		return nil
	}
	var found []script
	for _, redir := range stmt.Redirs {
		if text, line, ok := stdinScript(redir); ok {
			found = append(found, script{name, text, line})
		}
	}
	return found
}

func writtenScripts(stmt *syntax.Stmt, args []*syntax.Word) []script {
	targets := slices.Clone(args)
	for _, redir := range stmt.Redirs {
		if redir.Op == syntax.RdrOut || redir.Op == syntax.AppOut || redir.Op == syntax.ClbOut {
			targets = append(targets, redir.Word)
		}
	}
	var found []script
	for _, redir := range stmt.Redirs {
		text, line, ok := stdinScript(redir)
		if !ok || redir.Op == syntax.WordHdoc {
			continue
		}
		delimiter, _ := literal(redir.Word)
		interpreter := scriptKinds[strings.ToLower(delimiter)]
		for _, target := range targets {
			if file, named := literal(target); named && interpreter == "" {
				interpreter = scriptKinds[strings.TrimPrefix(filepath.Ext(file), ".")]
			}
		}
		if interpreter != "" {
			found = append(found, script{interpreter, text, line})
		}
	}
	return found
}

func command(args []*syntax.Word) (string, []*syntax.Word) {
	for i, arg := range args {
		word, ok := literal(arg)
		if !ok {
			return "", nil
		}
		if !wrappers[word] && !envAssign.MatchString(word) {
			return word, args[i+1:]
		}
	}
	return "", nil
}

func pythonCode(args []*syntax.Word) (*syntax.Word, bool) {
	for i := 0; i < len(args); i++ {
		word, ok := literal(args[i])
		switch {
		case !ok:
			return nil, false
		case word == "-":
			return nil, true
		case word == "--":
			return nil, i+1 == len(args)
		case strings.HasPrefix(word, "--"):
		case !pythonFlags.MatchString(word), strings.HasSuffix(word, "m"):
			return nil, false
		case strings.HasSuffix(word, "c"):
			if i+1 < len(args) {
				return args[i+1], false
			}
			return nil, false
		case strings.HasSuffix(word, "W"), strings.HasSuffix(word, "X"):
			i++
		}
	}
	return nil, true
}

type shellFlags struct{ fromArg, fromStdin bool }

func shellCode(args []*syntax.Word) (*syntax.Word, bool) {
	var flags shellFlags
	for i := 0; i < len(args); i++ {
		word, ok := literal(args[i])
		switch {
		case ok && (word == "--" || word == "-"):
			return flags.positional(args[i+1:])
		case ok && strings.HasPrefix(word, "--"):
		case ok && len(word) > 1 && (word[0] == '-' || word[0] == '+'):
			if flags.read(word) {
				i++
			}
		default:
			return flags.positional(args[i:])
		}
	}
	return flags.positional(nil)
}

func (f *shellFlags) read(word string) bool {
	if word[0] == '-' {
		f.fromArg = f.fromArg || strings.Contains(word, "c")
		f.fromStdin = f.fromStdin || strings.Contains(word, "s")
	}
	return strings.ContainsAny(word, "oO")
}

func (f *shellFlags) positional(rest []*syntax.Word) (*syntax.Word, bool) {
	if f.fromArg && len(rest) > 0 {
		return rest[0], false
	}
	return nil, !f.fromArg && (f.fromStdin || len(rest) == 0)
}

func stdinScript(redir *syntax.Redirect) (string, uint, bool) {
	if redir.N != nil && redir.N.Value != "0" {
		return "", 0, false
	}
	if redir.Op == syntax.WordHdoc {
		text, ok := literal(redir.Word)
		return text + "\n", redir.Word.Pos().Line(), ok
	}
	if redir.Op != syntax.Hdoc && redir.Op != syntax.DashHdoc || redir.Hdoc == nil {
		return "", 0, false
	}
	body := redir.Hdoc.Lit()
	delimiter := redir.Word.Lit()
	if body == "" || delimiter != "" && !strings.Contains(delimiter, `\`) && strings.Contains(body, `\`) {
		return "", 0, false
	}
	if redir.Op == syntax.DashHdoc {
		lines := strings.Split(body, "\n")
		for i, line := range lines {
			lines[i] = strings.TrimLeft(line, "\t")
		}
		body = strings.Join(lines, "\n")
	}
	return body, redir.Hdoc.Pos().Line(), true
}

func literal(word *syntax.Word) (string, bool) {
	var b strings.Builder
	for _, part := range word.Parts {
		switch p := part.(type) {
		case *syntax.SglQuoted:
			if p.Dollar {
				return "", false
			}
			b.WriteString(p.Value)
		case *syntax.Lit:
			if strings.Contains(p.Value, `\`) {
				return "", false
			}
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, inner := range p.Parts {
				lit, ok := inner.(*syntax.Lit)
				if !ok || p.Dollar || strings.Contains(lit.Value, `\`) {
					return "", false
				}
				b.WriteString(lit.Value)
			}
		default:
			return "", false
		}
	}
	return b.String(), true
}

func syntaxProblem(s script) string {
	line, message := checkSyntax(s.interpreter, s.code)
	if message == "" {
		return ""
	}
	if line > 0 {
		s.line += line - 1
	}
	return fmt.Sprintf("line %d: %s: %s", s.line, filepath.Base(s.interpreter), message)
}

func checkSyntax(interpreter, code string) (uint, string) {
	path, err := exec.LookPath(interpreter)
	if err != nil {
		return 0, ""
	}
	python := pythonName.MatchString(filepath.Base(interpreter))
	var cmd *exec.Cmd
	switch {
	case python:
		cmd = exec.Command(path, "-E", "-c", compilePython)
		cmd.Stdin = strings.NewReader(code)
	case filepath.Base(interpreter) == "bash":
		cmd = exec.Command(path, "-O", "extglob", "-n", "-c", code)
	case filepath.Base(interpreter) == "zsh":
		cmd = exec.Command(path, "-f", "-n", "-c", code)
	default:
		cmd = exec.Command(path, "-n", "-c", code)
	}
	cmd.Env = append(os.Environ(), "BASH_ENV=", "ENV=")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if cmd.Start() != nil {
		return 0, ""
	}
	timer := time.AfterFunc(syntaxTimeout, func() { _ = cmd.Process.Kill() })
	err = cmd.Wait()
	timer.Stop()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || python && exit.ExitCode() != 3 {
		return 0, ""
	}
	first, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n")
	if m := errorLine.FindStringSubmatch(first); m != nil {
		n, _ := strconv.ParseUint(m[1], 10, 32)
		return uint(n), m[2]
	}
	return 0, first
}
