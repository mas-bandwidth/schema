package recipescan

import (
	"regexp"
	"strings"
)

// WORKFLOWS. A GitHub Actions workflow is the third place a failure can be
// lost before anyone sees it (Glenn, 2026-11-22: "Nothing is allowed to fail
// silently"). ScanWorkflow reads one workflow file, parse only, with a small
// line reader of its own (the module has no YAML dependency, and the subset
// the rules need is indentation, keys and block scalars), and reports:
//
//	continue-on-error   a step or job that may fail and leave the run green
//	unchecked-needs     a job that runs under always() / !cancelled() /
//	                    failure() after jobs it needs, and never reads their
//	                    result: it reports success over a red dependency
//	no-strict-shell     a bash shell (a step's, a job's or the workflow's
//	                    default) without -e and pipefail, or a run step whose
//	                    shell is GitHub's own default ("bash -e {0}": no
//	                    pipefail) and that pipes; or a cmd shell, which
//	                    goes on past a failed command (python, pwsh and
//	                    node end nonzero on an uncaught error and are not read)
//
// and every rule of the shell reader (swallow, unchecked, background, ...)
// over the text of each run step, under that step's shell.

// Workflow rules.
const (
	RuleContinueOnError = "continue-on-error"
	RuleUncheckedNeeds  = "unchecked-needs"
)

// yline is one line of a workflow: its indentation (the column of its key,
// past any "- " list markers), its key and its inline value.
type yline struct {
	n      int // 1-based
	indent int
	item   bool // the line starts a list item
	key    string
	value  string
	block  string // a block scalar's text, when value is | or >
	bline  int    // 1-based line of the block's first text line
}

var ykey = regexp.MustCompile(`^([A-Za-z0-9_.${}\-"' ]+?):(\s+(.*))?$`)

func readYAML(src string) []yline {
	raw := strings.Split(src, "\n")
	var out []yline
	for i := 0; i < len(raw); i++ {
		s := strings.TrimRight(raw[i], "\r")
		t := strings.TrimLeft(s, " ")
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		indent := len(s) - len(t)
		item := false
		for strings.HasPrefix(t, "- ") || t == "-" {
			item = true
			t = strings.TrimLeft(strings.TrimPrefix(t, "-"), " ")
			indent = len(s) - len(t)
		}
		m := ykey.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		l := yline{n: i + 1, indent: indent, item: item, key: strings.Trim(m[1], `"'`), value: stripYAMLComment(m[3])}
		if strings.HasPrefix(l.value, "|") || strings.HasPrefix(l.value, ">") {
			var b strings.Builder
			min := -1
			j := i + 1
			for ; j < len(raw); j++ {
				r := strings.TrimRight(raw[j], "\r")
				rt := strings.TrimLeft(r, " ")
				if rt == "" {
					b.WriteByte('\n')
					continue
				}
				ri := len(r) - len(rt)
				if ri <= indent {
					break
				}
				if min < 0 {
					min = ri
				}
				if ri >= min {
					r = r[min:]
				}
				b.WriteString(r)
				b.WriteByte('\n')
			}
			l.block = b.String()
			l.bline = i + 2
			i = j - 1
		}
		out = append(out, l)
	}
	return out
}

func stripYAMLComment(v string) string {
	if v == "" || strings.HasPrefix(v, `"`) || strings.HasPrefix(v, `'`) {
		return strings.TrimSpace(v)
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// children returns the index range of the lines nested under lines[k].
func children(ls []yline, k int) (int, int) {
	j := k + 1
	for j < len(ls) && ls[j].indent > ls[k].indent {
		j++
	}
	// a list under a key may sit at the key's own indentation ("steps:\n- run")
	for j < len(ls) && ls[j].indent == ls[k].indent+2 && ls[j].item && ls[k].block == "" && ls[k].value == "" {
		j++
	}
	return k + 1, j
}

// get returns the line of key directly under lines[from:to] at indentation
// indent, or nil.
func get(ls []yline, from, to, indent int, key string) *yline {
	for i := from; i < to; i++ {
		if ls[i].indent == indent && ls[i].key == key {
			return &ls[i]
		}
	}
	return nil
}

func childIndent(ls []yline, from, to int) int {
	if from < to {
		return ls[from].indent
	}
	return -1
}

// runShell returns the shell under default.run.shell nested in ls[from:to].
func defaultShell(ls []yline, from, to, indent int) *yline {
	d := get(ls, from, to, indent, "defaults")
	if d == nil {
		return nil
	}
	k := indexOf(ls, d)
	f, t := children(ls, k)
	r := get(ls, f, t, childIndent(ls, f, t), "run")
	if r == nil {
		return nil
	}
	k = indexOf(ls, r)
	f, t = children(ls, k)
	return get(ls, f, t, childIndent(ls, f, t), "shell")
}

func indexOf(ls []yline, l *yline) int {
	for i := range ls {
		if &ls[i] == l {
			return i
		}
	}
	return -1
}

var ghExpr = regexp.MustCompile(`\$\{\{[^}]*\}\}`)

// shellStrict reports what a step's shell guarantees. known is false for a
// shell that is not bash or sh (cmd, pwsh, python): its text is not read by
// the shell rules.
func shellStrict(shell string) (pipefail, errexit, known bool) {
	if shell == "bash" {
		// GitHub runs "bash --noprofile --norc -eo pipefail {0}".
		return true, true, true
	}
	if shell == "sh" {
		return false, true, true
	}
	if !strings.Contains(shell, "bash") && !strings.Contains(shell, " sh ") {
		return false, false, false
	}
	f := strings.Fields(shell)
	for i, w := range f {
		if strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "--") {
			if strings.ContainsRune(w, 'e') {
				errexit = true
			}
			if strings.HasSuffix(w, "o") && i+1 < len(f) && f[i+1] == "pipefail" {
				pipefail = true
			}
		}
	}
	return pipefail, errexit, true
}

// ScanWorkflow reads one workflow file.
func ScanWorkflow(file, src string, st *Stats) []Finding {
	var out []Finding
	ls := readYAML(src)
	add := func(line int, rule, msg string) {
		out = append(out, Finding{File: file, Line: line, Rule: rule, Text: msg})
	}
	checkShell := func(l *yline) {
		if l == nil {
			return
		}
		pf, e, known := shellStrict(l.value)
		switch {
		case !known && strings.Fields(l.value + " x")[0] != "cmd":
			// python, pwsh, node: an uncaught error ends the step nonzero
		case !known:
			add(l.n, RuleNoStrictShell, "shell "+l.value+" goes on past a failed command and has no pipefail: use bash with -e -o pipefail")
		case !pf || !e:
			add(l.n, RuleNoStrictShell, "shell "+l.value+" must run bash with -e and -o pipefail")
		}
	}
	wfShell := defaultShell(ls, 0, len(ls), 0)
	checkShell(wfShell)
	jobs := get(ls, 0, len(ls), 0, "jobs")
	if jobs == nil {
		return out
	}
	jf, jt := children(ls, indexOf(ls, jobs))
	ji := childIndent(ls, jf, jt)
	for k := jf; k < jt; k++ {
		if ls[k].indent != ji {
			continue
		}
		job := ls[k]
		f, t := children(ls, k)
		ki := childIndent(ls, f, t)
		jobShell := defaultShell(ls, f, t, ki)
		checkShell(jobShell)
		if c := get(ls, f, t, ki, "continue-on-error"); c != nil && c.value != "false" {
			add(c.n, RuleContinueOnError, "job "+job.key+" may fail and leave the run green")
		}
		if cond := get(ls, f, t, ki, "if"); cond != nil && get(ls, f, t, ki, "needs") != nil {
			v := cond.value + cond.block
			if strings.Contains(v, "always()") || strings.Contains(v, "!cancelled()") || strings.Contains(v, "failure()") {
				// A job that checks the repository out does work of its own,
				// and a failed need stays red in its own row. A job that does
				// not is an aggregator (a verdict, a summary): its green is a
				// claim about its needs, and it must read their results.
				reads, works := false, false
				for i := f; i < t; i++ {
					v := ls[i].value + ls[i].block
					if (strings.Contains(v, "needs.") && strings.Contains(v, ".result")) || strings.Contains(v, "toJSON(needs)") {
						reads = true
					}
					if ls[i].key == "uses" && strings.HasPrefix(v, "actions/checkout@") {
						works = true
					}
				}
				if !reads && !works {
					add(cond.n, RuleUncheckedNeeds, "job "+job.key+" runs after a failed need and never reads needs.<job>.result: it reports success over a red dependency")
				}
			}
		}
		steps := get(ls, f, t, ki, "steps")
		if steps == nil {
			continue
		}
		sf, stt := children(ls, indexOf(ls, steps))
		// each list item is one step: from an item line to the next item line
		// at the same indentation
		var starts []int
		si := -1
		for i := sf; i < stt; i++ {
			if ls[i].item && (si < 0 || ls[i].indent == si) {
				si = ls[i].indent
				starts = append(starts, i)
			}
		}
		for n, a := range starts {
			b := stt
			if n+1 < len(starts) {
				b = starts[n+1]
			}
			if c := get(ls, a, b, si, "continue-on-error"); c != nil && c.value != "false" {
				add(c.n, RuleContinueOnError, "a step of job "+job.key+" may fail and leave the job green")
			}
			run := get(ls, a, b, si, "run")
			if run == nil {
				continue
			}
			shell := "bash -e {0}" // GitHub's default for a step with no shell
			declared := false
			for _, s := range []*yline{get(ls, a, b, si, "shell"), jobShell, wfShell} {
				if s != nil {
					shell = s.value
					declared = true
					break
				}
			}
			if sl := get(ls, a, b, si, "shell"); sl != nil {
				checkShell(sl)
			}
			pf, e, known := shellStrict(shell)
			if !known {
				continue
			}
			text, base := run.block, run.bline
			if text == "" {
				text, base = run.value, run.n
			}
			text = ghExpr.ReplaceAllStringFunc(text, func(s string) string {
				return "GHEXPR" + strings.Repeat("\n", strings.Count(s, "\n"))
			})
			var mine Stats
			ctx := shellContext{file: file, baseLine: base, pipefail: pf, errexit: e}
			out = append(out, scanShell(text, ctx, &mine)...)
			st.Pipes += mine.Pipes
			st.Steps++
			if !declared && mine.Pipes > 0 {
				add(run.n, RuleNoStrictShell, "the step pipes under GitHub's default shell (bash -e {0}, no pipefail): set shell: bash")
			}
		}
	}
	return out
}
