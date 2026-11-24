package main

import (
	"regexp"
	"strings"
)

// WHAT COUNTS AS WIRING is what runs on a schedule or on a pull request or
// push, never what merely mentions a target (the cold read of 2026-11-24
// found three escapes, each a planted gate the check called reached):
//
//   - a comment: a workflow line, or the rest of a line, after a `#` that
//     starts a YAML or shell comment;
//   - a job that runs only on workflow_dispatch, or a workflow whose only
//     trigger is workflow_dispatch: nothing starts it on any night;
//   - a negative-control manifest's `excluded` list, which names the controls
//     the groups do NOT run. Only a `targets` list is run.

// stripComments drops every comment from YAML or shell text: whole comment
// lines, and a ` #` comment at the end of a line when it is outside quotes.
func stripComments(text string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			b.WriteByte('\n')
			continue
		}
		b.WriteString(cutTrailingComment(line))
		b.WriteByte('\n')
	}
	return b.String()
}

// cutTrailingComment cuts a line at the first `#` that follows whitespace
// outside single or double quotes: `make x # make y` keeps `make x`, while
// `"$#"`, `${#v}` and `a#b` are not comments.
func cutTrailingComment(line string) string {
	var single, double bool
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case c == '\\' && !single:
			i++
		case c == '\'' && !double:
			single = !single
		case c == '"' && !single:
			double = !double
		case c == '#' && !single && !double && i > 0 && (line[i-1] == ' ' || line[i-1] == '\t'):
			return line[:i]
		}
	}
	return line
}

var (
	topKey     = regexp.MustCompile(`^([A-Za-z_"'][^:]*):`)
	jobHeader  = regexp.MustCompile(`^  ([A-Za-z0-9_-]+):\s*$`)
	jobIf      = regexp.MustCompile(`^    if:\s*(.*)$`)
	onInline   = regexp.MustCompile(`^on:\s*(\S.*)$`)
	triggerKey = regexp.MustCompile(`^  ([a-z_]+):`)
)

// dispatchOnly reports whether a job's `if:` admits workflow_dispatch alone:
// it requires `github.event_name == 'workflow_dispatch'` and offers no `||`
// alternative beside it.
func dispatchOnly(cond string) bool {
	return regexp.MustCompile(`github\.event_name\s*==\s*'workflow_dispatch'`).MatchString(cond) && !strings.Contains(cond, "||")
}

// triggers is the event names a workflow's `on:` names.
func triggers(lines []string) []string {
	var out []string
	for i, l := range lines {
		if m := onInline.FindStringSubmatch(l); m != nil {
			for t := range strings.FieldsSeq(strings.Trim(m[1], "[] ")) {
				out = append(out, strings.Trim(t, ", "))
			}
			return out
		}
		if strings.TrimRight(l, " ") != "on:" {
			continue
		}
		for _, l2 := range lines[i+1:] {
			if l2 != "" && !strings.HasPrefix(l2, " ") {
				break
			}
			if m := triggerKey.FindStringSubmatch(l2); m != nil {
				out = append(out, m[1])
			}
		}
		return out
	}
	return out
}

// scheduledText is the part of a workflow (comments already stripped) that
// something other than a manual dispatch runs: "" for a workflow whose only
// trigger is workflow_dispatch, and every job whose `if:` admits only a
// dispatch is cut out. A composite action (no `on:`) is kept whole.
func scheduledText(text string) string {
	lines := strings.Split(text, "\n")
	ts := triggers(lines)
	if len(ts) == 1 && ts[0] == "workflow_dispatch" {
		return ""
	}
	var b strings.Builder
	inJobs := false
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if m := topKey.FindStringSubmatch(l); m != nil && !strings.HasPrefix(l, " ") {
			inJobs = m[1] == "jobs"
		}
		if inJobs && jobHeader.MatchString(l) {
			end := i + 1
			for end < len(lines) && (lines[end] == "" || strings.HasPrefix(lines[end], "   ")) {
				end++
			}
			job := lines[i:end]
			if !jobIsDispatchOnly(job) {
				b.WriteString(strings.Join(job, "\n"))
				b.WriteByte('\n')
			}
			i = end - 1
			continue
		}
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String()
}

// jobIsDispatchOnly reads a job's own `if:`, a folded or literal block
// included.
func jobIsDispatchOnly(job []string) bool {
	for i, l := range job {
		m := jobIf.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		cond := m[1]
		if strings.HasPrefix(cond, ">") || strings.HasPrefix(cond, "|") {
			cond = ""
			for _, l2 := range job[i+1:] {
				if !strings.HasPrefix(l2, "      ") {
					break
				}
				cond += " " + strings.TrimSpace(l2)
			}
		}
		return dispatchOnly(cond)
	}
	return false
}

// controlTargets is every string in a `targets` list of a negative-control
// manifest, anywhere but under `excluded`.
func controlTargets(v any, out *[]string) {
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			controlTargets(e, out)
		}
	case map[string]any:
		for k, e := range x {
			switch k {
			case "excluded":
			case "targets":
				if list, ok := e.([]any); ok {
					for _, t := range list {
						if s, ok := t.(string); ok {
							*out = append(*out, s)
						}
					}
				}
			default:
				controlTargets(e, out)
			}
		}
	}
}
