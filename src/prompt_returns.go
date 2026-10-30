package main

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	returnHeading = regexp.MustCompile(`(?i)^#{1,6}\s+returns?\s*$`)
	anyHeading    = regexp.MustCompile(`^#{1,6}\s`)
	returnItem    = regexp.MustCompile("^[-*]\\s+`([^`]*)`\\s*(?:\\(([^)]*)\\))?\\s*(?::\\s*(.*))?$")
	choiceList    = regexp.MustCompile("^`[^`]+`(?:\\s*(?:,|,?\\s*or|\\|)\\s*`[^`]+`)+\\.?$")
	backticked    = regexp.MustCompile("`([^`]+)`")
)

func newPrompt(text, source string) Prompt {
	returns, problems := promptReturns(text)
	return Prompt{Text: text, Source: source, Returns: returns, Problems: problems}
}

func promptReturns(text string) (Fields, []string) {
	var fields Fields
	var problems []string
	seen := map[string]bool{}
	inside, fenced := false, false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " \t\r")
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		if anyHeading.MatchString(line) {
			inside = returnHeading.MatchString(line)
			continue
		}
		if !inside || !strings.HasPrefix(line, "- ") && !strings.HasPrefix(line, "* ") {
			continue
		}
		field, err := returnField(line)
		switch {
		case err != nil:
			problems = append(problems, "## Return: "+err.Error())
		case seen[field.Name]:
			problems = append(problems, fmt.Sprintf("## Return: %s is listed twice", field.Name))
		default:
			seen[field.Name] = true
			fields = append(fields, field)
		}
	}
	return fields, append(problems, questionProblems(fields)...)
}

func returnField(line string) (Field, error) {
	m := returnItem.FindStringSubmatch(line)
	if m == nil || !stepID.MatchString(m[1]) {
		return Field{}, fmt.Errorf("cannot read %q; write each field as - `name`: what it is, with a lowercase name", line)
	}
	kind := strings.TrimSpace(m[2])
	if kind == "" {
		kind = "text"
		if description := strings.TrimSpace(m[3]); choiceList.MatchString(description) {
			var options []string
			for _, option := range backticked.FindAllStringSubmatch(description, -1) {
				options = append(options, option[1])
			}
			kind = strings.Join(options, " | ")
		}
	}
	if _, err := fieldSchema(kind); err != nil {
		return Field{}, fmt.Errorf("%s: %w", m[1], err)
	}
	return Field{Name: m[1], Value: kind}, nil
}
