package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

func clock(t time.Time) string {
	return t.Format("15:04:05")
}

func elapsed(from time.Time, to *time.Time) time.Duration {
	end := time.Now()
	if to != nil {
		end = *to
	}
	return max(0, end.Sub(from))
}

func span(d time.Duration) string {
	s := int(d.Round(time.Second) / time.Second)
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	m := s / 60
	if m < 60 {
		return fmt.Sprintf("%dm %02ds", m, s%60)
	}
	return fmt.Sprintf("%dh %02dm", m/60, m%60)
}

func took(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return span(d)
}

func money(cost float64) string {
	if cost == 0 {
		return ""
	}
	return fmt.Sprintf("$%.2f", cost)
}

func tokens(n int) string {
	if n < 10000 {
		return fmt.Sprintf("%.1fk tokens", float64(n)/1000)
	}
	return fmt.Sprintf("%.0fk tokens", float64(n)/1000)
}

func sizeOf(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

func printJSON(v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(raw))
	return nil
}

func indented(text, indent string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func block(label, text, indent string) string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return ""
	}
	if !strings.Contains(text, "\n") && len(label)+len(text) < 120 {
		return indent + label + ": " + text + "\n"
	}
	return indent + label + ":\n" + indented(text, indent+"  ")
}

func sized(label, text string) string {
	return fmt.Sprintf("%s (%s)", label, sizeOf(len(text)))
}

func valueText(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case float64, bool:
		return fmt.Sprint(v)
	case []any:
		lines := make([]string, len(v))
		for i, item := range v {
			lines[i] = "- " + strings.ReplaceAll(valueText(item), "\n", "\n  ")
		}
		return strings.Join(lines, "\n")
	}
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(raw)
}

func fieldsText(fields map[string]any, indent string) string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		text := valueText(fields[name])
		if _, list := fields[name].([]any); list && text != "" {
			b.WriteString(indent + name + ":\n" + indented(text, indent+"  "))
			continue
		}
		b.WriteString(block(name, text, indent))
	}
	return b.String()
}

func toAny(values map[string]string) map[string]any {
	out := make(map[string]any, len(values))
	for k, v := range values {
		out[k] = v
	}
	return out
}

var plainWord = regexp.MustCompile(`^[\w@%+=:,./-]+$`)

func shellWord(s string) string {
	if plainWord.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
