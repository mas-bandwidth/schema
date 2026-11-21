package ci

// The workflow reader under TestEveryGoTestIsAssignedToALane.
//
// A step is credited only for a run this reader can prove happens with no
// person starting it. For each workflow it takes the automatic triggers
// (schedule; a push with no path filter), and for each job the matrix rows as
// GitHub builds them (lists multiplied, exclude applied, include merged or
// appended). It then evaluates the job's `if:` under each trigger and each
// step's `if:` under each trigger and row, and writes the step's script once
// per (trigger, row) for which both are certainly true, with every
// `${{ }}` in the script replaced by its value.
//
// Certainly true is the whole rule. An expression is evaluated in three
// values: true, false and unknown. `runner.environment`, `needs.*`, `env.*`,
// `inputs.*`, a matrix that is computed by another job: all unknown, and an
// `if:` that comes out unknown is not a run. A `${{ }}` in a script that has
// no known value stays an opaque word, which the command reader credits with
// nothing. So a mistake here can lose credit, never invent it.

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------- values

// laneVal is a YAML or expression value.
type laneVal struct {
	kind laneKind
	s    string
	b    bool
	list []laneVal
	m    map[string]laneVal
	keys []string // m's keys in source order
	raw  string   // the source text, for a list or a map
}

type laneKind int

const (
	laneUnknown laneKind = iota
	laneStr
	laneBool
	laneNull
	laneList
	laneMap
)

func laneS(s string) laneVal { return laneVal{kind: laneStr, s: s} }
func laneB(b bool) laneVal   { return laneVal{kind: laneBool, b: b} }

// truth reports the value's truthiness and whether it is known.
func (v laneVal) truth() (truthy, known bool) {
	switch v.kind {
	case laneStr:
		return v.s != "", true
	case laneBool:
		return v.b, true
	case laneNull:
		return false, true
	case laneList, laneMap:
		return true, true
	}
	return false, false
}

// text is the value as a script would receive it, when it is a scalar.
func (v laneVal) text() (string, bool) {
	switch v.kind {
	case laneStr:
		return v.s, true
	case laneBool:
		return strconv.FormatBool(v.b), true
	case laneNull:
		return "", true
	}
	return "", false
}

func laneFromJSON(x any) laneVal {
	switch t := x.(type) {
	case string:
		return laneS(t)
	case bool:
		return laneB(t)
	case nil:
		return laneVal{kind: laneNull}
	case float64:
		return laneS(strconv.FormatFloat(t, 'f', -1, 64))
	case []any:
		v := laneVal{kind: laneList}
		for _, e := range t {
			v.list = append(v.list, laneFromJSON(e))
		}
		return v
	case map[string]any:
		v := laneVal{kind: laneMap, m: map[string]laneVal{}}
		for k, e := range t {
			v.m[k] = laneFromJSON(e)
			v.keys = append(v.keys, k)
		}
		sort.Strings(v.keys)
		return v
	}
	return laneVal{}
}

// ---------------------------------------------------------------- YAML

// laneYAMLScalar reads an inline YAML value: a flow list, a flow map, a
// quoted or plain scalar. A value holding `${{` is an expression, kept as
// unknown with its text in raw.
func laneYAMLScalar(s string) laneVal {
	s = strings.TrimSpace(laneStripComment(s))
	if strings.Contains(s, "${{") {
		return laneVal{kind: laneUnknown, raw: s}
	}
	p := &laneFlow{s: s}
	v := p.value()
	p.space()
	if p.i != len(p.s) || p.bad {
		return laneVal{kind: laneUnknown, raw: s}
	}
	return v
}

// laneStripComment removes a trailing ` # comment` that is outside quotes.
func laneStripComment(s string) string {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '#' && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t'):
			return s[:i]
		}
	}
	return s
}

type laneFlow struct {
	s   string
	i   int
	bad bool
}

func (p *laneFlow) space() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t') {
		p.i++
	}
}

func (p *laneFlow) value() laneVal {
	p.space()
	start := p.i
	if p.i >= len(p.s) {
		return laneVal{kind: laneNull}
	}
	switch p.s[p.i] {
	case '[':
		p.i++
		v := laneVal{kind: laneList}
		for {
			p.space()
			if p.i < len(p.s) && p.s[p.i] == ']' {
				p.i++
				break
			}
			v.list = append(v.list, p.value())
			p.space()
			if p.i < len(p.s) && p.s[p.i] == ',' {
				p.i++
				continue
			}
			if p.i < len(p.s) && p.s[p.i] == ']' {
				p.i++
				break
			}
			p.bad = true
			return v
		}
		v.raw = p.s[start:p.i]
		return v
	case '{':
		p.i++
		v := laneVal{kind: laneMap, m: map[string]laneVal{}}
		for {
			p.space()
			if p.i < len(p.s) && p.s[p.i] == '}' {
				p.i++
				break
			}
			k := p.scalar(":")
			p.space()
			if p.i >= len(p.s) || p.s[p.i] != ':' {
				p.bad = true
				return v
			}
			p.i++
			v.m[k.s] = p.value()
			v.keys = append(v.keys, k.s)
			p.space()
			if p.i < len(p.s) && p.s[p.i] == ',' {
				p.i++
				continue
			}
			if p.i < len(p.s) && p.s[p.i] == '}' {
				p.i++
				break
			}
			p.bad = true
			return v
		}
		v.raw = p.s[start:p.i]
		return v
	}
	return p.scalar("")
}

// scalar reads a quoted scalar, or a plain one up to a flow delimiter (inside
// a flow collection) or the end of the text (outside one).
func (p *laneFlow) scalar(stop string) laneVal {
	p.space()
	if p.i < len(p.s) && (p.s[p.i] == '\'' || p.s[p.i] == '"') {
		q := p.s[p.i]
		p.i++
		var b strings.Builder
		for p.i < len(p.s) {
			c := p.s[p.i]
			if c == q {
				if q == '\'' && p.i+1 < len(p.s) && p.s[p.i+1] == '\'' {
					b.WriteByte('\'')
					p.i += 2
					continue
				}
				p.i++
				return laneS(b.String())
			}
			if c == '\\' && q == '"' && p.i+1 < len(p.s) {
				p.i++
				c = p.s[p.i]
			}
			b.WriteByte(c)
			p.i++
		}
		p.bad = true
		return laneVal{}
	}
	start := p.i
	inFlow := start > 0
	for p.i < len(p.s) {
		c := p.s[p.i]
		if inFlow && (c == ',' || c == ']' || c == '}' || strings.IndexByte(stop, c) >= 0) {
			break
		}
		p.i++
	}
	s := strings.TrimSpace(p.s[start:p.i])
	switch s {
	case "true":
		return laneB(true)
	case "false":
		return laneB(false)
	case "", "null", "~":
		return laneVal{kind: laneNull}
	}
	return laneS(s)
}

var laneYAMLKey = regexp.MustCompile(`^([A-Za-z0-9_.-]+):(?:\s+(.*))?$`)

type laneYAMLLine struct {
	indent int
	text   string
}

// laneYAMLBlock reads the block-style subset matrices are written in: maps of
// keys to inline values or nested blocks, and lists of scalars, flow maps or
// block maps. Anything else reads as unknown.
func laneYAMLBlock(lines []laneYAMLLine, pos *int, indent int) laneVal {
	if *pos >= len(lines) || lines[*pos].indent != indent {
		return laneVal{}
	}
	if strings.HasPrefix(lines[*pos].text, "- ") || lines[*pos].text == "-" {
		v := laneVal{kind: laneList}
		for *pos < len(lines) && lines[*pos].indent == indent && strings.HasPrefix(lines[*pos].text, "-") {
			item := strings.TrimPrefix(lines[*pos].text, "-")
			inner := indent + 1 + len(item) - len(strings.TrimLeft(item, " "))
			item = strings.TrimSpace(item)
			if laneYAMLKey.MatchString(item) && !strings.HasPrefix(item, "{") {
				// a block map whose first key sits on the dash line
				lines[*pos] = laneYAMLLine{indent: inner, text: item}
				v.list = append(v.list, laneYAMLBlock(lines, pos, inner))
				continue
			}
			*pos++
			v.list = append(v.list, laneYAMLScalar(item))
		}
		return v
	}
	v := laneVal{kind: laneMap, m: map[string]laneVal{}}
	for *pos < len(lines) && lines[*pos].indent == indent {
		m := laneYAMLKey.FindStringSubmatch(lines[*pos].text)
		if m == nil {
			return laneVal{}
		}
		*pos++
		var val laneVal
		switch {
		case strings.TrimSpace(laneStripComment(m[2])) != "":
			val = laneYAMLScalar(m[2])
		case *pos < len(lines) && lines[*pos].indent > indent:
			val = laneYAMLBlock(lines, pos, lines[*pos].indent)
		default:
			val = laneVal{kind: laneNull}
		}
		v.m[m[1]] = val
		v.keys = append(v.keys, m[1])
		// skip anything deeper this reader did not consume
		for *pos < len(lines) && lines[*pos].indent > indent {
			return laneVal{}
		}
	}
	return v
}

// ---------------------------------------------------------------- expressions

// laneEnv is what an expression is evaluated against: one automatic trigger
// and one matrix row. rowKnown false means the matrix could not be resolved,
// and every matrix.* is unknown.
type laneEnv struct {
	event    string
	row      map[string]laneVal
	rowKnown bool
}

type laneExprParser struct {
	toks []string
	i    int
	env  laneEnv
	bad  bool
}

var laneExprTok = regexp.MustCompile(`'(?:[^']|'')*'|==|!=|<=|>=|&&|\|\||[!(),<>]|[A-Za-z_][A-Za-z0-9_.*\-]*(?:\[[^\]]*\])?|[0-9.]+`)

// laneEval evaluates a GitHub Actions expression (without its `${{ }}`).
func laneEval(expr string, env laneEnv) laneVal {
	p := &laneExprParser{env: env}
	rest := expr
	for {
		rest = strings.TrimLeft(rest, " \t\n")
		if rest == "" {
			break
		}
		loc := laneExprTok.FindStringIndex(rest)
		if loc == nil || loc[0] != 0 {
			return laneVal{}
		}
		p.toks = append(p.toks, rest[:loc[1]])
		rest = rest[loc[1]:]
	}
	v := p.or()
	if p.bad || p.i != len(p.toks) {
		return laneVal{}
	}
	return v
}

func (p *laneExprParser) peek() string {
	if p.i < len(p.toks) {
		return p.toks[p.i]
	}
	return ""
}

func (p *laneExprParser) or() laneVal {
	v := p.and()
	for p.peek() == "||" {
		p.i++
		r := p.and()
		// a || b is a when a is truthy, else b
		lt, lk := v.truth()
		rt, rk := r.truth()
		switch {
		case lk && lt:
		case lk:
			v = r
		case rk && rt:
			v = laneB(true) // truthy either way; which operand is not known
		default:
			v = laneVal{}
		}
	}
	return v
}

func (p *laneExprParser) and() laneVal {
	v := p.cmp()
	for p.peek() == "&&" {
		p.i++
		r := p.cmp()
		// a && b is a when a is falsy, else b
		lt, lk := v.truth()
		rt, rk := r.truth()
		switch {
		case lk && !lt:
		case lk:
			v = r
		case rk && !rt:
			v = laneB(false) // falsy either way
		default:
			v = laneVal{}
		}
	}
	return v
}

func (p *laneExprParser) cmp() laneVal {
	v := p.unary()
	for {
		op := p.peek()
		if op != "==" && op != "!=" {
			if op == "<" || op == ">" || op == "<=" || op == ">=" {
				p.i++
				p.unary()
				v = laneVal{}
				continue
			}
			return v
		}
		p.i++
		r := p.unary()
		eq, known := laneEqual(v, r)
		if !known {
			v = laneVal{}
		} else {
			v = laneB(eq == (op == "=="))
		}
	}
}

// laneEqual is the expression language's ==: strings compare without case,
// and null equals the empty string.
func laneEqual(a, b laneVal) (eq, known bool) {
	as, aok := a.text()
	bs, bok := b.text()
	if !aok || !bok {
		return false, false
	}
	if (a.kind == laneBool) != (b.kind == laneBool) {
		return false, false // a boolean against a string coerces through numbers: not modelled
	}
	return strings.EqualFold(as, bs), true
}

func (p *laneExprParser) unary() laneVal {
	if p.peek() == "!" {
		p.i++
		v := p.unary()
		t, known := v.truth()
		if !known {
			return laneVal{}
		}
		return laneB(!t)
	}
	return p.primary()
}

func (p *laneExprParser) primary() laneVal {
	t := p.peek()
	p.i++
	switch {
	case t == "":
		p.bad = true
		return laneVal{}
	case t == "(":
		v := p.or()
		if p.peek() != ")" {
			p.bad = true
		}
		p.i++
		return v
	case t[0] == '\'':
		return laneS(strings.ReplaceAll(t[1:len(t)-1], "''", "'"))
	case t == "true":
		return laneB(true)
	case t == "false":
		return laneB(false)
	case t == "null":
		return laneVal{kind: laneNull}
	case t[0] >= '0' && t[0] <= '9':
		return laneS(t)
	}
	if p.peek() == "(" {
		p.i++
		var args []laneVal
		for p.peek() != ")" {
			if p.peek() == "" {
				p.bad = true
				return laneVal{}
			}
			args = append(args, p.or())
			if p.peek() == "," {
				p.i++
			}
		}
		p.i++
		return laneCall(strings.ToLower(t), args)
	}
	return p.env.lookup(t)
}

func laneCall(name string, args []laneVal) laneVal {
	str := func(i int) (string, bool) {
		if i >= len(args) {
			return "", false
		}
		return args[i].text()
	}
	switch name {
	case "always":
		return laneB(true)
	case "success":
		// the run this gate asks about is the one where earlier steps passed
		return laneB(true)
	case "cancelled", "failure":
		return laneB(false)
	case "startswith", "endswith", "contains":
		a, aok := str(0)
		b, bok := str(1)
		if !aok || !bok || len(args) != 2 {
			return laneVal{}
		}
		a, b = strings.ToLower(a), strings.ToLower(b)
		switch name {
		case "startswith":
			return laneB(strings.HasPrefix(a, b))
		case "endswith":
			return laneB(strings.HasSuffix(a, b))
		}
		return laneB(strings.Contains(a, b))
	case "fromjson":
		s, ok := str(0)
		if !ok || len(args) != 1 || args[0].kind != laneStr {
			return laneVal{}
		}
		var x any
		if err := json.Unmarshal([]byte(s), &x); err != nil {
			return laneVal{}
		}
		return laneFromJSON(x)
	}
	return laneVal{}
}

func (e laneEnv) lookup(name string) laneVal {
	low := strings.ToLower(name)
	switch {
	case low == "github.event_name":
		return laneS(e.event)
	case low == "github.head_ref" || low == "github.base_ref":
		// set only on a pull request, which is never the trigger here
		return laneS("")
	case strings.HasPrefix(low, "matrix."):
		if !e.rowKnown {
			return laneVal{}
		}
		key := name[len("matrix."):]
		if strings.ContainsAny(key, ".[*") {
			return laneVal{}
		}
		if v, ok := e.row[key]; ok {
			return v
		}
		return laneVal{kind: laneNull} // a key the row does not carry
	}
	return laneVal{}
}

var laneExpr = regexp.MustCompile(`\$\{\{((?:[^}]|\}[^}])*)\}\}`)

// laneCertain reports whether an `if:` is certainly true in env. No `if:` is.
func laneCertain(cond string, env laneEnv) bool {
	cond = strings.TrimSpace(cond)
	if cond == "" {
		return true
	}
	if len(cond) >= 2 && (cond[0] == '"' || cond[0] == '\'') && cond[len(cond)-1] == cond[0] {
		if cond[0] == '\'' {
			cond = strings.ReplaceAll(cond[1:len(cond)-1], "''", "'") // YAML's escape for a quote in a single-quoted scalar
		} else {
			cond = cond[1 : len(cond)-1]
		}
	}
	if m := laneExpr.FindStringSubmatch(cond); m != nil && strings.TrimSpace(cond) == m[0] {
		cond = m[1]
	} else if strings.Contains(cond, "${{") {
		return false
	}
	t, known := laneEval(cond, env).truth()
	return known && t
}

// laneSubst replaces each `${{ }}` in a script by its value in env. One with
// no known scalar value stays as written, and reads as an opaque word.
func laneSubst(s string, env laneEnv) string {
	return laneExpr.ReplaceAllStringFunc(s, func(m string) string {
		if t, ok := laneEval(laneExpr.FindStringSubmatch(m)[1], env).text(); ok {
			return t
		}
		return m
	})
}

// ---------------------------------------------------------------- matrices

// laneMatrix is a job's `strategy.matrix` as written.
type laneMatrix struct {
	present bool
	whole   string    // the matrix is one expression
	keys    []string  // list keys, in source order
	vals    []laneVal // per key: a list, or unknown (an expression in raw)
	include laneVal   // list, null, or unknown (raw)
	exclude laneVal
}

func laneReadMatrix(v laneVal, raw string) laneMatrix {
	mx := laneMatrix{present: true, include: laneVal{kind: laneNull}, exclude: laneVal{kind: laneNull}}
	if v.kind != laneMap {
		mx.whole = raw
		if raw == "" {
			mx.whole = "?"
		}
		return mx
	}
	for _, k := range v.keys {
		switch k {
		case "include":
			mx.include = v.m[k]
		case "exclude":
			mx.exclude = v.m[k]
		default:
			mx.keys = append(mx.keys, k)
			mx.vals = append(mx.vals, v.m[k])
		}
	}
	return mx
}

// resolve evaluates a matrix value that is one expression; a literal passes.
func laneResolve(v laneVal, event string) laneVal {
	if v.kind != laneUnknown || v.raw == "" {
		return v
	}
	m := laneExpr.FindStringSubmatch(v.raw)
	if m == nil || strings.TrimSpace(v.raw) != m[0] {
		return laneVal{}
	}
	return laneEval(m[1], laneEnv{event: event})
}

func laneRowMatches(row, want map[string]laneVal) bool {
	for k, w := range want {
		got, ok := row[k]
		if !ok {
			return false
		}
		eq, known := laneEqual(got, w)
		if !known || !eq {
			return false
		}
	}
	return true
}

// rows builds the matrix's rows for one trigger, as GitHub does: the product
// of the list keys, less each exclude entry, then each include entry merged
// into the rows it does not contradict, or appended as a row of its own.
// known is false when any part cannot be resolved: then nothing about the
// matrix is known.
func (mx laneMatrix) rows(event string) (rows []map[string]laneVal, known bool) {
	if !mx.present {
		return []map[string]laneVal{{}}, true
	}
	if mx.whole != "" {
		return nil, false
	}
	rows = []map[string]laneVal{{}}
	for i, k := range mx.keys {
		vals := laneResolve(mx.vals[i], event)
		if vals.kind != laneList {
			return nil, false
		}
		var next []map[string]laneVal
		for _, r := range rows {
			for _, v := range vals.list {
				n := map[string]laneVal{}
				maps.Copy(n, r)
				n[k] = v
				next = append(next, n)
			}
		}
		rows = next
	}
	original := map[string]bool{}
	for _, k := range mx.keys {
		original[k] = true
	}
	if len(mx.keys) == 0 {
		rows = nil
	}
	exclude := laneResolve(mx.exclude, event)
	switch exclude.kind {
	case laneNull:
	case laneList:
		for _, ex := range exclude.list {
			if ex.kind != laneMap {
				return nil, false
			}
			var kept []map[string]laneVal
			for _, r := range rows {
				if !laneRowMatches(r, ex.m) {
					kept = append(kept, r)
				}
			}
			rows = kept
		}
	default:
		return nil, false
	}
	include := laneResolve(mx.include, event)
	switch include.kind {
	case laneNull:
	case laneList:
		for _, inc := range include.list {
			if inc.kind != laneMap {
				return nil, false
			}
			added := false
			for _, r := range rows {
				fits := true
				for k, v := range inc.m {
					if !original[k] {
						continue
					}
					if eq, ok := laneEqual(r[k], v); !ok || !eq {
						fits = false
					}
				}
				if fits && len(mx.keys) > 0 {
					for k, v := range inc.m {
						if !original[k] {
							r[k] = v
						}
					}
					added = true
				}
			}
			if !added {
				n := map[string]laneVal{}
				maps.Copy(n, inc.m)
				rows = append(rows, n)
			}
		}
	default:
		return nil, false
	}
	return rows, true
}

// ---------------------------------------------------------------- workflows

// laneStep is one step's script as it runs for one trigger and one matrix row.
type laneStep struct {
	src      string
	run      string
	dir      string
	tier     string // "per-commit" (push) or "nightly" (schedule)
	platform string // GOOS/GOARCH, as "darwin/arm64", or "" when not known
	unread   string // why every command in it is credited with nothing
}

type laneRawStep struct {
	src, run, dir, cond, env string
}

type laneJob struct {
	name   string
	cond   string
	runsOn string
	uses   string // a reusable workflow in this repository
	env    string
	matrix laneMatrix
	steps  []laneRawStep
}

type laneWorkflow struct {
	file   string
	events []string // the automatic triggers: "push", "schedule"
	called bool     // has workflow_call
	env    string
	jobs   []laneJob
}

var (
	laneJobLine  = regexp.MustCompile(`^  ([A-Za-z0-9_-]+):\s*(#.*)?$`)
	laneStepLine = regexp.MustCompile(`^(\s+)- `)
	laneKeyLine  = regexp.MustCompile(`^(\s+)(?:- )?([A-Za-z0-9_-]+):\s*(.*)$`)
)

func laneIndent(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

// laneBlockValue reads the value of the key on lines[i]: inline, or the
// block scalar or nested block under it. It returns the text and the index
// after the value.
func laneBlockValue(lines []string, i int, val string) (string, int) {
	val = strings.TrimSpace(val)
	if val != "" && val[0] != '|' && val[0] != '>' {
		return val, i + 1
	}
	base := laneIndent(lines[i])
	if strings.HasPrefix(strings.TrimSpace(lines[i]), "- ") {
		base += 2
	}
	var block []string
	j := i + 1
	for ; j < len(lines); j++ {
		if strings.TrimSpace(lines[j]) != "" && laneIndent(lines[j]) <= base {
			break
		}
		block = append(block, strings.TrimSpace(lines[j]))
	}
	if val != "" && val[0] == '>' {
		return strings.TrimSpace(strings.Join(block, " ")), j
	}
	return strings.Join(block, "\n"), j
}

func laneReadWorkflow(file, body string) laneWorkflow {
	wf := laneWorkflow{file: file}
	lines := strings.Split(body, "\n")
	on := ""
	section := ""
	var job *laneJob
	inSteps := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if len(line) > 0 && line[0] != ' ' && line[0] != '#' {
			section, _, _ = strings.Cut(line, ":")
			section = strings.Trim(section, `"'`)
			if section == "on" {
				on += line + "\n"
			}
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		switch section {
		case "on":
			on += line + "\n"
			continue
		case "env":
			wf.env += line + "\n"
			continue
		case "jobs":
		default:
			continue
		}
		if m := laneJobLine.FindStringSubmatch(line); m != nil {
			wf.jobs = append(wf.jobs, laneJob{name: m[1]})
			job = &wf.jobs[len(wf.jobs)-1]
			inSteps = false
			continue
		}
		if job == nil {
			continue
		}
		if !inSteps {
			if strings.HasPrefix(line, "    steps:") {
				inSteps = true
				continue
			}
			m := laneKeyLine.FindStringSubmatch(line)
			if m == nil || len(m[1]) != 4 {
				continue
			}
			switch m[2] {
			case "if":
				job.cond, _ = laneBlockValue(lines, i, m[3])
			case "runs-on":
				job.runsOn = strings.TrimSpace(m[3])
			case "uses":
				if u, ok := strings.CutPrefix(strings.TrimSpace(laneStripComment(m[3])), "./.github/workflows/"); ok {
					job.uses = u
				}
			case "env":
				for j := i + 1; j < len(lines) && (strings.TrimSpace(lines[j]) == "" || laneIndent(lines[j]) > 4); j++ {
					job.env += lines[j] + "\n"
				}
			case "strategy":
				// the matrix: an expression on its own line, or a block
				for j := i + 1; j < len(lines) && (strings.TrimSpace(lines[j]) == "" || laneIndent(lines[j]) > 4); j++ {
					mm := laneKeyLine.FindStringSubmatch(lines[j])
					if mm == nil || len(mm[1]) != 6 || mm[2] != "matrix" {
						continue
					}
					if inline := strings.TrimSpace(laneStripComment(mm[3])); inline != "" {
						job.matrix = laneReadMatrix(laneYAMLScalar(inline), inline)
						break
					}
					var block []laneYAMLLine
					for k := j + 1; k < len(lines) && (strings.TrimSpace(lines[k]) == "" || laneIndent(lines[k]) > 6 || strings.HasPrefix(strings.TrimSpace(lines[k]), "#")); k++ {
						t := strings.TrimSpace(lines[k])
						if t == "" || strings.HasPrefix(t, "#") {
							continue
						}
						block = append(block, laneYAMLLine{indent: laneIndent(lines[k]), text: t})
					}
					pos := 0
					var v laneVal
					if len(block) > 0 {
						v = laneYAMLBlock(block, &pos, block[0].indent)
					}
					if pos != len(block) {
						v = laneVal{}
					}
					job.matrix = laneReadMatrix(v, "")
					break
				}
			}
			continue
		}
		if !laneStepLine.MatchString(line) {
			continue
		}
		// one step: from this `- ` to the next line at its indent or less
		indent := len(laneStepLine.FindStringSubmatch(line)[1])
		end := i + 1
		for end < len(lines) {
			l := lines[end]
			if strings.TrimSpace(l) != "" && laneIndent(l) <= indent && !strings.HasPrefix(strings.TrimSpace(l), "#") {
				break
			}
			end++
		}
		st := laneRawStep{dir: "."}
		for j := i; j < end; j++ {
			m := laneKeyLine.FindStringSubmatch(lines[j])
			if m == nil || (len(m[1]) != indent+2 && j != i) || strings.HasPrefix(strings.TrimSpace(lines[j]), "#") {
				continue
			}
			switch m[2] {
			case "if":
				st.cond, _ = laneBlockValue(lines[:end], j, m[3])
			case "working-directory":
				st.dir = strings.Trim(strings.TrimSpace(m[3]), `"'`)
				if laneUnreadable(st.dir) || strings.Contains(st.dir, "${{") || strings.HasPrefix(st.dir, "/") {
					st.dir = "\x00"
				}
			case "env":
				for k := j + 1; k < end && (strings.TrimSpace(lines[k]) == "" || laneIndent(lines[k]) > indent+2); k++ {
					st.env += lines[k] + "\n"
				}
			case "run":
				st.src = fmt.Sprintf("%s:%d", file, j+1)
				st.run, _ = laneBlockValue(lines[:end], j, m[3])
			}
		}
		if st.run != "" {
			job.steps = append(job.steps, st)
		}
		i = end - 1
	}
	if strings.Contains(on, "schedule:") {
		wf.events = append(wf.events, "schedule")
	}
	if regexp.MustCompile(`(?m)^  push:`).MatchString(on) && !strings.Contains(on, "paths:") && !strings.Contains(on, "paths-ignore:") {
		wf.events = append(wf.events, "push")
	}
	wf.called = strings.Contains(on, "workflow_call")
	return wf
}

var laneGoFlags = regexp.MustCompile(`(?m)^\s*GOFLAGS:\s*(.*)$`)

// laneEnvChangesGoTest reports an env block that sets GOFLAGS to something
// other than a module mode: it could carry -run, -skip, -tags or -count.
func laneEnvChangesGoTest(envs ...string) bool {
	for _, e := range envs {
		for _, m := range laneGoFlags.FindAllStringSubmatch(e, -1) {
			v := strings.Trim(strings.TrimSpace(laneStripComment(m[1])), `"'`)
			if v != "" && v != "-mod=readonly" && v != "-mod=mod" {
				return true
			}
		}
	}
	return false
}

// laneRunsOn names the GOOS/GOARCH a job's runs-on means for one row, or ""
// when the labels do not say both: the operating system by its label, the
// architecture by an explicit ARM64 or X64 label, else by the GitHub-hosted
// image it names (ubuntu and windows images are x64 unless named -arm; the
// macos images from 14 on are arm64, 13 and the -large and -intel ones x64).
// A runner whose architecture is unknown compiles nothing certain, so its
// commands are credited with nothing (review finding F-026).
func laneRunsOn(runsOn string, env laneEnv) string {
	text := laneExpr.ReplaceAllStringFunc(runsOn, func(m string) string {
		v := laneEval(laneExpr.FindStringSubmatch(m)[1], env)
		if t, ok := v.text(); ok {
			return t
		}
		if v.kind == laneList || v.kind == laneMap {
			return v.raw
		}
		return "\x01"
	})
	low := strings.ToLower(text)
	var got []string
	if strings.Contains(low, "windows") {
		got = append(got, "windows")
	}
	if strings.Contains(low, "macos") {
		got = append(got, "darwin")
	}
	if strings.Contains(low, "ubuntu") || strings.Contains(low, "linux") {
		got = append(got, "linux")
	}
	if len(got) != 1 || strings.Contains(text, "\x01") {
		return ""
	}
	arch := laneRunnerArch(low)
	if arch == "" {
		return ""
	}
	return got[0] + "/" + arch
}

var (
	laneLabelSplit  = regexp.MustCompile(`[\s,\[\]"']+`)
	laneHostedAMD64 = regexp.MustCompile(`^(ubuntu-(latest|[0-9.]+)|windows-(latest|[0-9]+)|macos-13|macos-[0-9a-z]+-(large|intel))$`)
	laneHostedARM64 = regexp.MustCompile(`^(ubuntu-[0-9.]+-arm|windows-[0-9]+-arm|macos-(latest|1[4-9]|2[0-9])(-xlarge)?)$`)
)

// laneRunnerArch is the GOARCH a lower-cased runs-on names, or "".
func laneRunnerArch(low string) string {
	arch := map[string]bool{}
	for _, l := range laneLabelSplit.Split(low, -1) {
		switch {
		case l == "arm64" || l == "aarch64":
			arch["arm64"] = true
		case l == "x64" || l == "amd64" || l == "x86_64":
			arch["amd64"] = true
		case laneHostedARM64.MatchString(l):
			arch["arm64"] = true
		case laneHostedAMD64.MatchString(l):
			arch["amd64"] = true
		}
	}
	if len(arch) != 1 {
		return ""
	}
	for a := range arch {
		return a
	}
	return ""
}

// laneSteps writes every step of every workflow once per automatic trigger
// and matrix row it certainly runs under. A reusable workflow takes the
// triggers of the jobs that certainly call it.
func laneSteps(wfs map[string]laneWorkflow) (steps []laneStep, counted int) {
	events := map[string]map[string]bool{}
	for name, wf := range wfs {
		events[name] = map[string]bool{}
		for _, e := range wf.events {
			events[name][e] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for name, wf := range wfs {
			for _, job := range wf.jobs {
				callee, ok := wfs[job.uses]
				if job.uses == "" || !ok || !callee.called {
					continue
				}
				for e := range events[name] {
					if !events[job.uses][e] && laneCertain(job.cond, laneEnv{event: e}) {
						events[job.uses][e] = true
						changed = true
					}
				}
			}
		}
	}
	names := make([]string, 0, len(wfs))
	for n := range wfs {
		names = append(names, n)
	}
	sort.Strings(names)
	seen := map[string]bool{}
	for _, name := range names {
		wf := wfs[name]
		if len(events[name]) > 0 {
			counted++
		}
		for _, event := range []string{"push", "schedule"} {
			if !events[name][event] {
				continue
			}
			tier := "nightly"
			if event == "push" {
				tier = "per-commit"
			}
			for _, job := range wf.jobs {
				if !laneCertain(job.cond, laneEnv{event: event}) {
					continue
				}
				rows, known := job.matrix.rows(event)
				if !known {
					rows = []map[string]laneVal{nil}
				}
				for _, row := range rows {
					env := laneEnv{event: event, row: row, rowKnown: known}
					platform := laneRunsOn(job.runsOn, env)
					for _, st := range job.steps {
						if !laneCertain(st.cond, env) {
							continue
						}
						out := laneStep{src: st.src, run: laneSubst(st.run, env), dir: st.dir, tier: tier, platform: platform}
						switch {
						case platform == "":
							out.unread = "the job's runs-on does not name one operating system and architecture"
						case st.dir == "\x00":
							out.unread = "the step's working-directory is not a literal"
						case laneEnvChangesGoTest(wf.env, job.env, st.env):
							out.unread = "GOFLAGS is set for this step, which can change what go test runs"
						}
						key := out.src + "\x00" + out.run + "\x00" + tier + "\x00" + platform
						if !seen[key] {
							seen[key] = true
							steps = append(steps, out)
						}
					}
				}
			}
		}
	}
	return steps, counted
}
