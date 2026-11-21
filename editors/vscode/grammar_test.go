// Package vscode holds the VS Code extension for .schema files. Its one Go
// file is this test: it runs the TextMate grammar in syntaxes/ over every
// .schema file in examples/, examples128/ and tables/, and checks the scope
// each token gets against the compiler's own scanner (internal/scanner).
//
// The grammar is run by a small TextMate engine written here: match rules,
// begin/end rules, captures, includes, earliest match wins, ties go to the end
// pattern and then to rule order. That is the part of TextMate the grammar
// uses. Its regular expressions stay inside the subset RE2 and Oniguruma
// share, so Go's regexp runs them as VS Code's Oniguruma does; a rule that
// leaves the subset fails TestGrammarCompiles.
package vscode

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/scanner"
)

const grammarPath = "syntaxes/schema.tmLanguage.json"

type rawCapture struct {
	Name string `json:"name"`
}

type rawRule struct {
	Name          string                `json:"name"`
	Match         string                `json:"match"`
	Begin         string                `json:"begin"`
	End           string                `json:"end"`
	Include       string                `json:"include"`
	Captures      map[string]rawCapture `json:"captures"`
	BeginCaptures map[string]rawCapture `json:"beginCaptures"`
	EndCaptures   map[string]rawCapture `json:"endCaptures"`
	Patterns      []*rawRule            `json:"patterns"`
}

type rawGrammar struct {
	ScopeName  string              `json:"scopeName"`
	Patterns   []*rawRule          `json:"patterns"`
	Repository map[string]*rawRule `json:"repository"`
}

// rule is a compiled grammar rule. A rule with neither match nor begin is a
// container: its patterns stand in its place.
type rule struct {
	name          string
	match         *regexp.Regexp
	begin         *regexp.Regexp
	end           *regexp.Regexp
	captures      map[int]string
	beginCaptures map[int]string
	endCaptures   map[int]string
	patterns      []*rule // flattened: no containers, no includes
}

type grammar struct {
	scope string
	top   []*rule
}

func compileRegexp(src string) (*regexp.Regexp, error) {
	re, err := regexp.Compile(src)
	if err != nil {
		return nil, fmt.Errorf("%q is outside the RE2 subset this test runs: %w", src, err)
	}
	return re, nil
}

func captureMap(m map[string]rawCapture) (map[int]string, error) {
	out := map[int]string{}
	for k, v := range m {
		var n int
		if _, err := fmt.Sscanf(k, "%d", &n); err != nil {
			return nil, fmt.Errorf("capture key %q is not a group number", k)
		}
		if strings.Contains(v.Name, "$") {
			return nil, fmt.Errorf("capture name %q uses a $ substitution this test does not run", v.Name)
		}
		out[n] = v.Name
	}
	return out, nil
}

func loadGrammar(path string) (*grammar, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw rawGrammar
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	compiled := map[*rawRule]*rule{}
	var compile func(r *rawRule) (*rule, error)
	var flatten func(list []*rawRule, seen map[string]bool) ([]*rule, error)
	resolve := func(inc string) (*rawRule, error) {
		if !strings.HasPrefix(inc, "#") {
			return nil, fmt.Errorf("include %q: only #name includes are run", inc)
		}
		r, ok := raw.Repository[inc[1:]]
		if !ok {
			return nil, fmt.Errorf("include %q names no repository entry", inc)
		}
		return r, nil
	}
	flatten = func(list []*rawRule, seen map[string]bool) ([]*rule, error) {
		var out []*rule
		for _, r := range list {
			if r.Include != "" {
				if seen[r.Include] {
					return nil, fmt.Errorf("include cycle at %q", r.Include)
				}
				target, err := resolve(r.Include)
				if err != nil {
					return nil, err
				}
				seen[r.Include] = true
				sub, err := flatten([]*rawRule{target}, seen)
				delete(seen, r.Include)
				if err != nil {
					return nil, err
				}
				out = append(out, sub...)
				continue
			}
			if r.Match == "" && r.Begin == "" {
				sub, err := flatten(r.Patterns, seen)
				if err != nil {
					return nil, err
				}
				out = append(out, sub...)
				continue
			}
			c, err := compile(r)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
		}
		return out, nil
	}
	compile = func(r *rawRule) (*rule, error) {
		if c, ok := compiled[r]; ok {
			return c, nil
		}
		c := &rule{name: r.Name}
		compiled[r] = c
		var err error
		if c.captures, err = captureMap(r.Captures); err != nil {
			return nil, err
		}
		if c.beginCaptures, err = captureMap(r.BeginCaptures); err != nil {
			return nil, err
		}
		if c.endCaptures, err = captureMap(r.EndCaptures); err != nil {
			return nil, err
		}
		if strings.Contains(r.Name, "$") {
			return nil, fmt.Errorf("rule name %q uses a $ substitution this test does not run", r.Name)
		}
		if r.Match != "" {
			if c.match, err = compileRegexp(r.Match); err != nil {
				return nil, err
			}
			return c, nil
		}
		if r.End == "" {
			return nil, fmt.Errorf("begin %q has no end", r.Begin)
		}
		if c.begin, err = compileRegexp(r.Begin); err != nil {
			return nil, err
		}
		if c.end, err = compileRegexp(r.End); err != nil {
			return nil, err
		}
		if c.patterns, err = flatten(r.Patterns, map[string]bool{}); err != nil {
			return nil, err
		}
		return c, nil
	}
	top, err := flatten(raw.Patterns, map[string]bool{})
	if err != nil {
		return nil, err
	}
	return &grammar{scope: raw.ScopeName, top: top}, nil
}

// find is the leftmost match of re in line at or after pos. A pattern anchored
// with ^ matches only at the start of the line, as in Oniguruma.
func find(re *regexp.Regexp, line string, pos int) []int {
	if strings.HasPrefix(re.String(), "^") && pos > 0 {
		return nil
	}
	loc := re.FindStringSubmatchIndex(line[pos:])
	if loc == nil {
		return nil
	}
	for i := range loc {
		if loc[i] >= 0 {
			loc[i] += pos
		}
	}
	return loc
}

// tokenize returns, for each byte of src, the scope stack the grammar gives
// it, outermost first. Newlines carry the stack of the line they end.
func (g *grammar) tokenize(src string) [][]string {
	type frame struct {
		r      *rule
		scopes []string
	}
	stack := []frame{{scopes: []string{g.scope}}}
	out := make([][]string, len(src))
	paint := func(from, to int, scopes []string) {
		for i := from; i < to; i++ {
			out[i] = scopes
		}
	}
	with := func(base []string, names ...string) []string {
		s := append([]string(nil), base...)
		for _, n := range names {
			if n != "" {
				s = append(s, n)
			}
		}
		return s
	}
	paintCaptures := func(loc []int, caps map[int]string, base []string) {
		groups := make([]int, 0, len(caps))
		for n := range caps {
			groups = append(groups, n)
		}
		sort.Ints(groups)
		for _, n := range groups {
			if 2*n+1 >= len(loc) || loc[2*n] < 0 {
				continue
			}
			paint(loc[2*n], loc[2*n+1], with(base, caps[n]))
		}
	}
	lineStart := 0
	for lineStart <= len(src) {
		lineEnd := strings.IndexByte(src[lineStart:], '\n')
		if lineEnd < 0 {
			lineEnd = len(src)
		} else {
			lineEnd += lineStart
		}
		line := src[lineStart:lineEnd]
		pos := 0
		for pos <= len(line) {
			top := stack[len(stack)-1]
			patterns := g.top
			if top.r != nil {
				patterns = top.r.patterns
			}
			var best []int
			var bestRule *rule
			isEnd := false
			if top.r != nil {
				if loc := find(top.r.end, line, pos); loc != nil {
					best, isEnd = loc, true
				}
			}
			for _, r := range patterns {
				re := r.match
				if re == nil {
					re = r.begin
				}
				loc := find(re, line, pos)
				if loc == nil || (best != nil && loc[0] >= best[0]) {
					continue
				}
				if loc[0] == loc[1] {
					continue // an empty match outside an end pattern consumes nothing
				}
				best, bestRule, isEnd = loc, r, false
			}
			if best == nil {
				paint(lineStart+pos, lineStart+len(line), top.scopes)
				break
			}
			paint(lineStart+pos, lineStart+best[0], top.scopes)
			abs := make([]int, len(best))
			for i, v := range best {
				abs[i] = v
				if v >= 0 {
					abs[i] = v + lineStart
				}
			}
			switch {
			case isEnd:
				paint(abs[0], abs[1], top.scopes)
				paintCaptures(abs, top.r.endCaptures, top.scopes)
				stack = stack[:len(stack)-1]
			case bestRule.match != nil:
				s := with(top.scopes, bestRule.name)
				paint(abs[0], abs[1], s)
				paintCaptures(abs, bestRule.captures, s)
			default:
				s := with(top.scopes, bestRule.name)
				paint(abs[0], abs[1], s)
				paintCaptures(abs, bestRule.beginCaptures, s)
				stack = append(stack, frame{r: bestRule, scopes: s})
			}
			if best[1] == best[0] && best[1] == len(line) {
				break
			}
			pos = best[1]
		}
		if lineEnd < len(src) {
			out[lineEnd] = stack[len(stack)-1].scopes
		}
		lineStart = lineEnd + 1
	}
	return out
}

func hasScope(stack []string, prefix string) bool {
	for _, s := range stack {
		if s == prefix || strings.HasPrefix(s, prefix+".") {
			return true
		}
	}
	return false
}

// claimed are the scopes a plain identifier must not carry: an identifier
// that picks one up has been taken for something it is not.
var claimed = []string{
	"comment", "string", "constant.numeric", "keyword", "storage", "support.type",
	"invalid", "entity", "variable.other.enummember",
}

var primitive = map[scanner.Kind]bool{
	scanner.KwBool: true, scanner.KwFloat32: true, scanner.KwFloat64: true,
	scanner.KwString: true, scanner.KwBytes: true,
	scanner.KwBits: true, scanner.KwUfixed: true,
	scanner.KwInt8: true, scanner.KwInt16: true, scanner.KwInt32: true, scanner.KwInt64: true,
	scanner.KwInt128: true, scanner.KwUint8: true, scanner.KwUint16: true,
	scanner.KwUint32: true, scanner.KwUint64: true, scanner.KwUint128: true,
}

// expectations returns, for each token the scanner reads, the scope its every
// byte must carry, or "" for an identifier that must carry none of claimed.
// Punctuation and operators are not checked.
func expectations(toks []scanner.Token) []string {
	want := make([]string, len(toks))
	depth := 0
	firstOnLine := true
	inQual := false
	qualName := false // the next identifier names a qualification
	enumDepth := -1   // brace depth of an enum or flags body, or -1
	enumPending := false
	var prev scanner.Token    // the last token that is not a comment or newline
	prevWasFieldName := false // prev is an identifier that began its line inside a body
	next := func(i int) scanner.Kind {
		for j := i + 1; j < len(toks); j++ {
			if toks[j].Kind != scanner.Comment && toks[j].Kind != scanner.Newline {
				return toks[j].Kind
			}
		}
		return scanner.EOF
	}
	for i, t := range toks {
		switch t.Kind {
		case scanner.Newline:
			firstOnLine = true
			inQual, qualName = false, false
			continue
		case scanner.Comment:
			switch {
			case strings.HasPrefix(t.Text, "///"):
				want[i] = "comment.line.documentation"
			case strings.HasPrefix(t.Text, "//"):
				want[i] = "comment.line.double-slash"
			default:
				want[i] = "comment.block"
			}
			continue
		}
		atLineStart := firstOnLine
		firstOnLine = false
		switch t.Kind {
		case scanner.String:
			want[i] = "string.quoted.double"
		case scanner.Int, scanner.Float:
			want[i] = "constant.numeric"
		case scanner.KwPackage:
			want[i] = "keyword.other.package"
		case scanner.KwType, scanner.KwTable, scanner.KwConst:
			want[i] = "storage.type"
		case scanner.KwEnum:
			want[i] = "storage.type"
			enumPending = true
		case scanner.KwFixed:
			if next(i) == scanner.KwTable {
				want[i] = "storage.modifier"
			} else {
				want[i] = "support.type.primitive"
			}
		case scanner.KwIf, scanner.KwElse:
			want[i] = "keyword.control"
		case scanner.KwAlign, scanner.KwReserved:
			want[i] = "keyword.other"
		case scanner.KwMap:
			want[i] = "storage.type.map"
		case scanner.KwMessage, scanner.KwObject, scanner.KwSwitch, scanner.KwCase,
			scanner.KwInt, scanner.KwUint, scanner.KwWString:
			want[i] = "invalid.illegal"
		case scanner.Pipe:
			inQual, qualName = true, true
		case scanner.Comma:
			if inQual {
				qualName = true
			}
		case scanner.LBrace:
			depth++
			if enumPending {
				enumDepth, enumPending = depth, false
			}
		case scanner.RBrace:
			if depth == enumDepth {
				enumDepth = -1
			}
			depth--
		case scanner.Ident:
			switch {
			case (t.Text == "flags" || t.Text == "union") && atLineStart && depth == 0:
				want[i] = "storage.type"
				if t.Text == "flags" {
					enumPending = true
				}
			case prev.Kind == scanner.KwPackage:
				want[i] = "entity.name.namespace"
			case prev.Kind == scanner.KwConst:
				want[i] = "entity.name.constant"
			case prev.Kind == scanner.KwType || prev.Kind == scanner.KwTable || prev.Kind == scanner.KwEnum ||
				(prev.Kind == scanner.Ident && (prev.Text == "flags" || prev.Text == "union") && want[prevIndex(toks, i)] == "storage.type"):
				want[i] = "entity.name.type"
			case t.Text == "timestamp" && prevWasFieldName && !inQual:
				// the contextual type word of SPEC §4.13, in a field's type
				// position; a field NAMED timestamp falls through to plain
				want[i] = "support.type.primitive"
			case inQual && qualName:
				want[i] = "entity.other.attribute-name"
			case enumDepth >= 0 && depth == enumDepth && !inQual:
				want[i] = "variable.other.enummember"
			}
		default:
			if primitive[t.Kind] {
				want[i] = "support.type.primitive"
			}
		}
		if t.Kind != scanner.Pipe && t.Kind != scanner.Comma {
			qualName = false
		}
		prevWasFieldName = t.Kind == scanner.Ident && atLineStart && depth > 0
		prev = t
	}
	return want
}

func prevIndex(toks []scanner.Token, i int) int {
	for j := i - 1; j >= 0; j-- {
		if toks[j].Kind != scanner.Comment && toks[j].Kind != scanner.Newline {
			return j
		}
	}
	return 0
}

func corpus(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, dir := range []string{"examples", "examples128", "tables"} {
		root := filepath.Join("..", "..", dir)
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".schema") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// timestamp in a field's type position is in no corpus above; these
	// witnesses carry it
	files = append(files,
		filepath.Join("..", "..", "testdata", "conformance", "feature-witnesses", "timestamp", "Probe.schema"),
		filepath.Join("..", "..", "testdata", "conformance", "feature-witnesses", "full", "Features.schema"))
	if len(files) < 50 {
		t.Fatalf("found %d .schema files in the corpus; expected the whole of examples/, examples128/ and tables/", len(files))
	}
	return files
}

func TestGrammarCompiles(t *testing.T) {
	g, err := loadGrammar(grammarPath)
	if err != nil {
		t.Fatal(err)
	}
	if g.scope != "source.schema" {
		t.Errorf("scopeName is %q, want source.schema", g.scope)
	}
}

// TestGrammarColoursCorpus is the corpus test: every keyword, type, comment,
// string, number, declaration name, qualification name and enum variant in
// the corpus gets its scope, and no other identifier gets a scope it should
// not have.
func TestGrammarColoursCorpus(t *testing.T) {
	g, err := loadGrammar(grammarPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range corpus(t) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		toks, errs := scanner.ScanRaw(path, src)
		if len(errs) > 0 {
			t.Fatalf("%s: the corpus does not scan: %v", path, errs[0])
		}
		lineStarts := []int{0}
		for i, c := range src {
			if c == '\n' {
				lineStarts = append(lineStarts, i+1)
			}
		}
		scopes := g.tokenize(string(src))
		want := expectations(toks)
		bad := 0
		for i, tok := range toks {
			if tok.Kind == scanner.EOF || tok.Kind == scanner.Newline {
				continue
			}
			check := want[i] != "" || tok.Kind == scanner.Ident
			if !check {
				continue
			}
			start := lineStarts[tok.Pos.Line-1] + tok.Pos.Col - 1
			for off := start; off < start+len(tok.Text); off++ {
				if src[off] == '\n' || src[off] == '\r' {
					continue
				}
				got := scopes[off]
				var fail string
				if want[i] != "" && !hasScope(got, want[i]) {
					fail = "want " + want[i]
				}
				if want[i] == "" {
					for _, c := range claimed {
						if hasScope(got, c) {
							fail = "a plain identifier, want none of " + strings.Join(claimed, ", ")
							break
						}
					}
				}
				if fail != "" {
					t.Errorf("%s: %q got %v, %s", tok.Pos, tok.Text, got, fail)
					bad++
					break
				}
			}
			if bad >= 20 {
				t.Fatalf("%s: stopping after 20 failures", path)
			}
		}
	}
}

// TestGrammarSamples pins scopes on short lines of every construct, so a
// regression names the construct and not only a corpus position.
func TestGrammarSamples(t *testing.T) {
	g, err := loadGrammar(grammarPath)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		src, text, want string
	}{
		{"package example", "example", "entity.name.namespace"},
		{"const MaxObjects = 10000", "MaxObjects", "entity.name.constant"},
		{"const MaxObjects = 0x1F", "0x1F", "constant.numeric.hex"},
		{"enum Team { Red, Blue }", "Blue", "variable.other.enummember"},
		{"flags ShipFlags { FiringLaser, Boosting }", "ShipFlags", "entity.name.type"},
		{"union Message\n{\n    a A\n}", "Message", "entity.name.type"},
		{"fixed table Physics\n{\n}", "fixed", "storage.modifier"},
		{"type Vec3 | cpp_native = VecMath, cpp_include = \"vec_math.h\"", "cpp_include", "entity.other.attribute-name"},
		{"type Vec3 | cpp_native = VecMath", "cpp_native", "entity.other.attribute-name"},
		{"    x int32 | min = -MaxPositionUnits, max = MaxPositionUnits", "max", "entity.other.attribute-name"},
		{"    v fixed(4, 12)", "fixed", "support.type.primitive"},
		{"    xs [..8]uint8", "..", "keyword.operator.range"},
		{"    g ?GunnerConfig", "?", "keyword.operator.optional"},
		{"    m map[string]Item", "map", "storage.type.map"},
		{"    if !at_rest", "if", "keyword.control"},
		{"    reserved(3)", "reserved", "keyword.other"},
		{"/// a doc comment", "a doc comment", "comment.line.documentation"},
		{"/* a\nblock */", "block", "comment.block"},
		{"message M {}", "message", "invalid.illegal"},
		{"    name wstring(16)", "wstring", "invalid.illegal"},
		{"    reaction float32 = 0.2", "0.2", "constant.numeric.float"},
	}
	for _, c := range cases {
		at := strings.Index(c.src, c.text)
		if at < 0 {
			t.Fatalf("%q is not in %q", c.text, c.src)
		}
		scopes := g.tokenize(c.src)
		for off := at; off < at+len(c.text); off++ {
			if c.src[off] == '\n' {
				continue
			}
			if !hasScope(scopes[off], c.want) {
				t.Errorf("%q in %q: got %v, want %s", c.text, c.src, scopes[off], c.want)
				break
			}
		}
	}
	// a field named like a contextual keyword is a field, not a declaration
	src := "type T\n{\n    flags ShipFlags\n    union_id uint8\n}"
	scopes := g.tokenize(src)
	at := strings.Index(src, "flags")
	if hasScope(scopes[at], "storage") {
		t.Errorf("an indented field named flags got %v", scopes[at])
	}
}

// TestGrammarTimestampTypeVersusName: timestamp is a type only in a field's
// type position (SPEC §4.13). As a field's name it is a plain identifier.
func TestGrammarTimestampTypeVersusName(t *testing.T) {
	g, err := loadGrammar(grammarPath)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		src    string
		nth    int // which occurrence of timestamp, from 0
		isType bool
	}{
		{"table T\n{\n    at timestamp\n}", 0, true},
		{"table T\n{\n    at   timestamp // when\n}", 0, true},
		{"table T\n{\n    timestamp int64\n}", 0, false},
		{"table T\n{\n    timestamp   int64 | min = 0\n}", 0, false},
		{"table T\n{\n    timestamp timestamp\n}", 0, false},
		{"table T\n{\n    timestamp timestamp\n}", 1, true},
		{"table T\n{\n    if timestamp\n    {\n    }\n}", 0, false},
		{"// a timestamp in a comment\n", 0, false},
	}
	for _, c := range cases {
		at := -1
		for k, from := 0, 0; k <= c.nth; k++ {
			i := strings.Index(c.src[from:], "timestamp")
			if i < 0 {
				t.Fatalf("occurrence %d of timestamp is not in %q", c.nth, c.src)
			}
			at = from + i
			from = at + 1
		}
		scopes := g.tokenize(c.src)
		for off := at; off < at+len("timestamp"); off++ {
			got := hasScope(scopes[off], "support.type.primitive")
			if got != c.isType {
				t.Errorf("timestamp #%d in %q: got %v, want type=%v", c.nth, c.src, scopes[off], c.isType)
				break
			}
		}
	}
	// the keyword before it keeps its own scope
	src := "table T\n{\n    if timestamp\n    {\n    }\n}"
	scopes := g.tokenize(src)
	if at := strings.Index(src, "if"); !hasScope(scopes[at], "keyword.control") {
		t.Errorf("if before timestamp got %v, want keyword.control", scopes[at])
	}
}
