package ci

// The spec-pins guard: every normative sentence of docs/SPEC-TABLES.md carries
// a stable row id, every row is pinned to a test in every port that carries
// its feature (or the pin is owed, by name), and every pin resolves to a test
// that runs. The design is the section "Row ids and pins" at the top of
// docs/SPEC-TABLES.md; this file is its guard, its pin registry and its
// reversed witnesses.
//
// THE PIN REGISTRY. Pins are Go data, one file per spec section group
// (internal/ci/schema_spec_pin_<group>_test.go), each adding its pins with
// registerSpecPins. A group's card owns its file, so the pinning cards land
// independently and never edit each other's pins. This file holds the
// worked example's pins (§2.7).
//
// WHAT IT DOES NOT CHECK, so nobody reads more into a green than is there:
// that a pinned test asserts what its row says (that is the reader's job at
// the pin's card); that a non-Go test is wired into a make target CI runs (a
// non-Go pin is held to: the file exists, the symbol is defined there, it is
// not marked skipped or ignored on or above its definition, and it is called
// from somewhere in its own directory); that a retired id is never removed
// from the retired list (that is a property of history, not of one commit);
// and calls a Go test makes through function values or interfaces (the skip
// search follows plain calls, package-qualified calls into this module, and
// method calls by name).

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

// specPin is one entry of the pin table: a row, a port, and either the test
// that pins the row in that port or the reason the pin is owed.
type specPin struct {
	Row  string // the row id, as its marker in docs/SPEC-TABLES.md spells it
	Port string // a port carrying the row's feature: a roadmap column, or "compiler"
	// Test is "<path>:<symbol>", the path from the repository root. A path
	// ending in _test.go names a Go test function; one ending in .tla names a
	// TLA+ property checked by TLC under Cfg; any other source file names a
	// harness function of a port.
	Test   string
	Cfg    string // .tla only: the TLC configuration that checks the property
	Module string // .tla only: the module defining the property (default: the model's own)
	Owed   string // non-empty: no test yet, and why
	Want   string // with Owed: the name the owed test will carry; it is searched repo-wide
	// Tier is "" for the fast default, "slow" for a test whose only skip is
	// slowtest.Gate (the guard fills it in), or "structure" for a fast
	// source-structure assertion (set by the pin's author). docs/SPEC-TABLES.md,
	// "Row ids and pins".
	Tier string
	// ModuleDir records the nearest nested Go module above a Go pin's test, so
	// the run that executes it is known; "" for the root module. The guard
	// fills it in. docs/SPEC-TABLES.md, "Row ids and pins".
	ModuleDir string
	// Digest is specRowDigest of the row's text when the pin was confirmed
	// against it: a reworded row fails until its pin is re-read and the
	// digest moved. The id survives renumbering; the digest does not
	// survive a reword.
	Digest string
	origin string // the file that registered it
}

// specPinRegistry is every pin the package's files register.
var specPinRegistry []specPin

// registerSpecPins adds one file's pins to the registry. It returns a value
// so a file registers with a package-level `var _ = registerSpecPins(...)`.
func registerSpecPins(origin string, pins ...specPin) bool {
	for _, p := range pins {
		p.origin = origin
		specPinRegistry = append(specPinRegistry, p)
	}
	return true
}

// The worked example: §2.7, "The block form is not declared". Its two rules
// are compiler rules (the compiler is the one place a declaration is
// accepted or refused), so the one port carrying them is the compiler; its
// third normative-worded sentence points at §19 and is marked informative.
//
// T2.7-1: an unmarked `fixed table` gets a block form. The block corpus's
// RenderFrame is declared `fixed table RenderFrame` with no marker, and the
// test requires ir.Blocks to lay it out.
//
// T2.7-2: internal/check refuses a fixed-table field named `magic`,
// `build_version` or `byte_order` (check.go, "collides with the block form's
// generated prologue"). No test in the tree planted that declaration until
// this card's internal/check/blockprologue_test.go, which plants each name on
// a fixed and a plain table and requires the refusal to name the field.
//
// The section is pinned fully: no entry here is Owed.
var _ = registerSpecPins("internal/ci/schema_spec_pin_design_row_ids_test.go",
	specPin{Row: "T2.7-1", Port: "compiler", Test: "ir/blocklayout_test.go:TestBlockLayoutMatchesTheWorkedTable", Digest: "de000392ae1e6bda"},
	specPin{Row: "T2.7-2", Port: "compiler", Test: "internal/check/blockprologue_test.go:TestFixedTableFieldNamedAfterTheBlockPrologueIsRefused", Digest: "bf97ea2fb4572d0b"},
)

// ---------------------------------------------------------------- the spec

// specCompilerPort is the pseudo-port of a compiler rule: a declaration is
// accepted or refused by the one compiler, whatever language it targets.
const specCompilerPort = "compiler"

var (
	specRowIDPattern   = regexp.MustCompile(`^T[0-9A-Za-z.]+-[0-9]+$`)
	specRowMarker      = regexp.MustCompile(`<!--\s*row\s+(\S+)((?:[^-]|-[^-])*)-->`)
	specRetiredMarker  = regexp.MustCompile(`<!--\s*row-retired\s+(\S+)((?:[^-]|-[^-])*)-->`)
	specSectionMarker  = regexp.MustCompile(`^\s*<!--\s*spec-pins\s+(owed|informative)\s+((?:[^-]|-[^-])*)-->\s*$`)
	specAttr           = regexp.MustCompile(`([a-z]+)=("[^"]*"|\S+)`)
	specHeading        = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	specHeadingNumber  = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)*)\.?\s`)
	specFence          = regexp.MustCompile("^\\s{0,3}(`{3,}|~{3,})")
	specListItem       = regexp.MustCompile(`^\s*(?:[-*+]|[0-9]+[.)])\s`)
	specNormativeWords = regexp.MustCompile(`(?i)\b(must|shall|may\s+not|refus(?:e|es|ed|al|als|ing))\b`)
	specOwedCard       = regexp.MustCompile(`^schema-spec-pin-[a-z0-9-]+$`)
	// specOwedCards is the closed list of pinning cards an owed section may
	// name, the cards the sizing table was cut into: a card added here is a
	// reviewed decision in this file, never a name a section can invent to
	// excuse an unmarked rule. A card whose pin file has landed
	// (specLandedCards) owes nothing, whether or not it is still listed.
	specOwedCards = map[string]bool{
		"schema-spec-pin-s0": true, "schema-spec-pin-s2": true, "schema-spec-pin-s3a": true,
		"schema-spec-pin-s34": true, "schema-spec-pin-s45": true, "schema-spec-pin-s6": true,
		"schema-spec-pin-s7": true, "schema-spec-pin-s8": true,
	}
	specPinFile = regexp.MustCompile(`^internal/ci/schema_spec_pin_([a-z0-9_]+)_test\.go$`)
	// specInformativeSections is the closed list of sections that may be
	// marked informative: a section added here is a reviewed decision in this
	// file, never a marker a card can add to escape the guard.
	specInformativeSections = map[string]bool{"Row ids and pins": true}
	specComment             = regexp.MustCompile(`<!--(?:[^-]|-[^-])*-->`)
	specTableRule           = regexp.MustCompile(`^[\s|:\-]*$`)
)

// pinSection is one heading's own body: the text up to the next heading of
// any level.
type pinSection struct {
	Level       int // heading depth, for sizing scope inheritance
	Title       string
	Number      string // "2.7", or "" for an unnumbered heading
	Line        int
	Owed        string // the card that owes this section its row ids
	Informative string // why this section states no rule of the format
	Normative   int    // normative sentences in its body
	Unmarked    int    // of those, without a row id
}

// specSentence is one sentence unit: a sentence of a paragraph or list item,
// or one table row.
type specSentence struct {
	Section   int
	Line      int
	Text      string
	Normative bool
	Rows      []string
}

// specRow is one row id's marker.
type specRow struct {
	ID          string
	Section     int
	Line        int
	Features    []string
	Informative string
	Kind        string // "structure" for a row about source structure; "" means runtime behaviour
	Sentence    string
}

type specDoc struct {
	Sections  []pinSection
	Sentences []specSentence
	Rows      map[string]specRow
	Retired   map[string]bool
	Findings  []string
}

// maskCode replaces the inside of each inline code span with x, so neither
// the sentence splitter nor the normative-word search reads code.
func maskCode(s string) string {
	b := []byte(s)
	in := false
	for i := range b {
		if b[i] == '`' {
			in = !in
			continue
		}
		if in && b[i] != '\n' {
			b[i] = 'x'
		}
	}
	return string(b)
}

// splitSentences returns the start offsets of the sentences of masked text.
func splitSentences(masked string) []int {
	var starts []int
	i := 0
	for i < len(masked) && isSpace(masked[i]) {
		i++
	}
	if i < len(masked) {
		starts = append(starts, i)
	}
	for ; i < len(masked); i++ {
		c := masked[i]
		if c != '.' && c != '!' && c != '?' {
			continue
		}
		j := i + 1
		for j < len(masked) && strings.IndexByte("*_)\"'`]", masked[j]) >= 0 {
			j++
		}
		if j < len(masked) && !isSpace(masked[j]) {
			continue
		}
		k := j
		for k < len(masked) && isSpace(masked[k]) {
			k++
		}
		if k >= len(masked) {
			break
		}
		n := masked[k]
		if (n >= 'A' && n <= 'Z') || (n >= '0' && n <= '9') || strings.IndexByte("*_`(\"[", n) >= 0 || strings.HasPrefix(masked[k:], "§") {
			starts = append(starts, k)
			i = k - 1
		}
	}
	return starts
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// parseSpecDoc reads the spec as the guard sees it.
func parseSpecDoc(text string) specDoc {
	doc := specDoc{Rows: map[string]specRow{}, Retired: map[string]bool{}}
	lines := strings.Split(text, "\n")
	cur := -1
	var unit []string
	unitLine := 0
	table := false

	flush := func() {
		if len(unit) == 0 {
			return
		}
		doc.addUnit(cur, unitLine, strings.Join(unit, "\n"), table)
		unit, table = nil, false
	}
	start := func(n int, l string, isTable bool) {
		flush()
		unit, unitLine, table = []string{l}, n, isTable
	}

	fence := ""
	inComment := false
	for i, l := range lines {
		n := i + 1
		if fence != "" {
			if m := specFence.FindStringSubmatch(l); m != nil && m[1][0] == fence[0] && len(m[1]) >= len(fence) {
				fence = ""
			}
			continue
		}
		if inComment {
			if strings.Contains(l, "-->") {
				inComment = false
			}
			continue
		}
		if m := specFence.FindStringSubmatch(l); m != nil {
			flush()
			fence = m[1]
			continue
		}
		for _, m := range specRetiredMarker.FindAllStringSubmatch(maskCode(l), -1) {
			doc.Retired[m[1]] = true
		}
		if m := specSectionMarker.FindStringSubmatch(l); m != nil {
			flush()
			if cur < 0 {
				doc.Findings = append(doc.Findings, fmt.Sprintf("line %d: a spec-pins marker before the first heading", n))
				continue
			}
			arg := strings.TrimSpace(m[2])
			s := &doc.Sections[cur]
			if s.Owed != "" || s.Informative != "" {
				doc.Findings = append(doc.Findings, fmt.Sprintf("line %d: section %q carries two spec-pins markers", n, s.Title))
			}
			if m[1] == "owed" {
				switch {
				case !specOwedCard.MatchString(arg):
					doc.Findings = append(doc.Findings, fmt.Sprintf("line %d: an owed section names %q, not a schema-spec-pin-* card", n, arg))
				case !specOwedCards[arg]:
					doc.Findings = append(doc.Findings, fmt.Sprintf("line %d: an owed section names %q, which is no card on the guard's specOwedCards list", n, arg))
				}
				s.Owed = arg
			} else {
				if arg == "" {
					doc.Findings = append(doc.Findings, fmt.Sprintf("line %d: an informative section gives no reason", n))
				}
				if !specInformativeSections[s.Title] {
					doc.Findings = append(doc.Findings, fmt.Sprintf("line %d: section %q is marked informative but is not in the guard's specInformativeSections", n, s.Title))
				}
				s.Informative = arg
			}
			continue
		}
		trimmed := strings.TrimSpace(l)
		if strings.HasPrefix(trimmed, "<!--") && !strings.Contains(trimmed, "-->") {
			flush()
			inComment = true
			continue
		}
		if m := specHeading.FindStringSubmatch(l); m != nil {
			flush()
			title := strings.TrimSpace(m[2])
			num := ""
			if nm := specHeadingNumber.FindStringSubmatch(title + " "); nm != nil {
				num = nm[1]
			}
			doc.Sections = append(doc.Sections, pinSection{Level: len(m[1]), Title: title, Number: num, Line: n})
			cur = len(doc.Sections) - 1
			continue
		}
		if trimmed == "" {
			flush()
			continue
		}
		if strings.HasPrefix(trimmed, "|") {
			start(n, l, true)
			flush()
			continue
		}
		if specListItem.MatchString(l) || strings.HasPrefix(trimmed, ">") {
			start(n, l, false)
			continue
		}
		if len(unit) == 0 {
			unitLine = n
		}
		unit = append(unit, l)
	}
	flush()
	return doc
}

// addUnit splits one block into sentences and binds each row marker to the
// last sentence that starts before it.
func (doc *specDoc) addUnit(section, line int, text string, table bool) {
	if section < 0 {
		section = doc.ensurePreamble()
	}
	type marker struct {
		off   int
		id    string
		attrs string
	}
	var markers []marker
	// markers are found outside inline code: a marker spelled in backticks is
	// an example, not a row
	codeMasked := maskCode(text)
	for _, m := range specRowMarker.FindAllStringSubmatchIndex(codeMasked, -1) {
		markers = append(markers, marker{m[0], text[m[2]:m[3]], text[m[4]:m[5]]})
	}
	// blank every comment so neither marker nor note reads as prose
	clean := []byte(text)
	for _, m := range specComment.FindAllStringIndex(codeMasked, -1) {
		for k := m[0]; k < m[1]; k++ {
			clean[k] = ' '
		}
	}
	masked := maskCode(string(clean))
	var starts []int
	if table {
		starts = []int{0}
	} else {
		starts = splitSentences(masked)
	}
	if len(starts) == 0 {
		starts = []int{0}
	}
	if !table {
		// a row marker ends its sentence: whatever follows it is the next one
		for _, m := range markers {
			end := m.off + len(specRowMarker.FindString(codeMasked[m.off:]))
			for end < len(masked) && isSpace(masked[end]) {
				end++
			}
			if end < len(masked) {
				starts = append(starts, end)
			}
		}
		sort.Ints(starts)
		uniq := starts[:0]
		for i, v := range starts {
			if i == 0 || v != starts[i-1] {
				uniq = append(uniq, v)
			}
		}
		starts = uniq
	}
	first := len(doc.Sentences)
	for k, s := range starts {
		e := len(masked)
		if k+1 < len(starts) {
			e = starts[k+1]
		}
		body := masked[s:e]
		if table && specTableRule.MatchString(body) {
			continue
		}
		doc.Sentences = append(doc.Sentences, specSentence{
			Section:   section,
			Line:      line + strings.Count(text[:s], "\n"),
			Text:      strings.Join(strings.Fields(string(clean[s:e])), " "),
			Normative: specNormativeWords.MatchString(body),
		})
	}
	for _, m := range markers {
		at := -1
		for k := first; k < len(doc.Sentences); k++ {
			off := starts[0]
			if !table {
				off = starts[k-first]
			}
			if off <= m.off {
				at = k
			}
		}
		if at < 0 {
			at = first
		}
		if at >= len(doc.Sentences) {
			doc.Findings = append(doc.Findings, fmt.Sprintf("line %d: row %s marks no sentence", line, m.id))
			continue
		}
		mline := line + strings.Count(text[:m.off], "\n")
		doc.addRow(m.id, m.attrs, at, mline)
	}
}

func (doc *specDoc) ensurePreamble() int {
	if len(doc.Sections) > 0 && doc.Sections[0].Line == 0 {
		return 0
	}
	doc.Sections = append([]pinSection{{Title: "(before the first heading)", Line: 0}}, doc.Sections...)
	return 0
}

func (doc *specDoc) addRow(id, attrs string, sentence, line int) {
	s := &doc.Sentences[sentence]
	s.Rows = append(s.Rows, id)
	if !specRowIDPattern.MatchString(id) {
		doc.Findings = append(doc.Findings, fmt.Sprintf("line %d: row id %q is not T<section>-<n>", line, id))
	}
	if prev, dup := doc.Rows[id]; dup {
		doc.Findings = append(doc.Findings, fmt.Sprintf("line %d: row %s is also marked at line %d; an id names one sentence", line, id, prev.Line))
		return
	}
	row := specRow{ID: id, Section: s.Section, Line: line, Sentence: s.Text}
	for _, a := range specAttr.FindAllStringSubmatch(attrs, -1) {
		v := strings.Trim(a[2], `"`)
		switch a[1] {
		case "feature":
			for _, f := range strings.Split(v, ",") {
				if f = strings.TrimSpace(f); f != "" {
					row.Features = append(row.Features, f)
				}
			}
		case "informative":
			row.Informative = v
		case "kind":
			if v != "structure" {
				doc.Findings = append(doc.Findings, fmt.Sprintf("line %d: row %s has unknown kind %q (only kind=structure is defined)", line, id, v))
				continue
			}
			row.Kind = v
		default:
			doc.Findings = append(doc.Findings, fmt.Sprintf("line %d: row %s has an unknown attribute %q", line, id, a[1]))
		}
	}
	switch {
	case row.Informative == "" && len(row.Features) == 0:
		doc.Findings = append(doc.Findings, fmt.Sprintf("line %d: row %s names no feature= and no informative=\"<reason>\"", line, id))
	case row.Informative != "" && len(row.Features) > 0:
		doc.Findings = append(doc.Findings, fmt.Sprintf("line %d: row %s is both informative and pinned to a feature", line, id))
	}
	doc.Rows[id] = row
}

// ------------------------------------------------------------- the roadmap

// sexp is a parsed s-expression: an atom (string or symbol) or a list.
type sexp struct {
	atom string
	str  bool
	list []sexp
	isL  bool
}

func parseSexp(text string) ([]sexp, error) {
	var stack [][]sexp
	var top []sexp
	push := func(e sexp) {
		if len(stack) == 0 {
			top = append(top, e)
		} else {
			stack[len(stack)-1] = append(stack[len(stack)-1], e)
		}
	}
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c == ';':
			for i < len(text) && text[i] != '\n' {
				i++
			}
		case c == '(':
			stack = append(stack, nil)
		case c == ')':
			if len(stack) == 0 {
				return nil, fmt.Errorf("unbalanced ) at byte %d", i)
			}
			l := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			push(sexp{list: l, isL: true})
		case c == '"':
			var b strings.Builder
			j := i + 1
			for ; j < len(text) && text[j] != '"'; j++ {
				if text[j] == '\\' && j+1 < len(text) {
					j++
				}
				b.WriteByte(text[j])
			}
			if j >= len(text) {
				return nil, fmt.Errorf("unterminated string at byte %d", i)
			}
			push(sexp{atom: b.String(), str: true})
			i = j
		case isSpace(c):
		default:
			j := i
			for j < len(text) && !isSpace(text[j]) && text[j] != '(' && text[j] != ')' && text[j] != '"' {
				j++
			}
			push(sexp{atom: text[i:j]})
			i = j - 1
		}
	}
	if len(stack) != 0 {
		return nil, fmt.Errorf("unbalanced (")
	}
	return top, nil
}

func (e sexp) key(k string) (sexp, bool) {
	for i := 0; i+1 < len(e.list); i++ {
		if !e.list[i].isL && !e.list[i].str && e.list[i].atom == k {
			return e.list[i+1], true
		}
	}
	return sexp{}, false
}

// roadmapPorts maps "<roadmap>/<row>" to the ports with a cell in that row.
func roadmapPorts(text string) (map[string][]string, error) {
	top, err := parseSexp(text)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	var walk func(e sexp)
	walk = func(e sexp) {
		if !e.isL {
			return
		}
		id, okID := e.key(":id")
		typ, okT := e.key(":type")
		if okID && okT && typ.atom == ":roadmap" {
			cols := map[string]bool{}
			if c, ok := e.key(":columns"); ok {
				for _, col := range c.list {
					if len(col.list) > 0 {
						cols[col.list[0].atom] = true
					}
				}
			}
			if cells, ok := e.key(":cells"); ok {
				for _, cell := range cells.list {
					if len(cell.list) >= 2 && cols[cell.list[1].atom] {
						k := id.atom + "/" + cell.list[0].atom
						out[k] = append(out[k], cell.list[1].atom)
					}
				}
			}
		}
		for _, c := range e.list {
			walk(c)
		}
	}
	for _, e := range top {
		walk(e)
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out, nil
}

// --------------------------------------------------------------- the guard

// specPinsInput is everything the guard reads; fsys is rooted at the
// repository root.
type specPinsInput struct {
	fsys    fs.FS
	spec    string // docs/SPEC-TABLES.md
	roadmap string // docs/roadmap.sexp
	pins    []specPin
}

// checkSpecPins returns every finding; none is a pass.
func checkSpecPins(in specPinsInput) (specDoc, []string) {
	var out []string
	specText, err := fs.ReadFile(in.fsys, in.spec)
	if err != nil {
		return specDoc{}, []string{"the spec cannot be read: " + err.Error()}
	}
	rmText, err := fs.ReadFile(in.fsys, in.roadmap)
	if err != nil {
		return specDoc{}, []string{"the roadmap cannot be read: " + err.Error()}
	}
	ports, err := roadmapPorts(string(rmText))
	if err != nil {
		return specDoc{}, []string{"the roadmap does not parse: " + err.Error()}
	}
	doc := parseSpecDoc(string(specText))
	out = append(out, doc.Findings...)

	// 1. every normative sentence of a section that owes nothing has an id
	for _, s := range doc.Sentences {
		sec := &doc.Sections[s.Section]
		if !s.Normative {
			continue
		}
		sec.Normative++
		if len(s.Rows) > 0 {
			continue
		}
		sec.Unmarked++
		if sec.Owed == "" && sec.Informative == "" {
			out = append(out, fmt.Sprintf("line %d: normative sentence without a row id (section %q): %s", s.Line, sec.Title, clip(s.Text)))
		}
	}
	// an owed marker on a section with nothing left to mark is stale, and
	// one naming a card that has landed its pins excuses nothing
	landed := specLandedCards(in.pins)
	for _, sec := range doc.Sections {
		if sec.Owed != "" && sec.Unmarked == 0 {
			out = append(out, fmt.Sprintf("line %d: section %q is marked owed by %s but every normative sentence has a row id; remove the marker", sec.Line, sec.Title, sec.Owed))
		}
		if file := landed[sec.Owed]; sec.Owed != "" && file != "" {
			out = append(out, fmt.Sprintf("line %d: section %q is marked owed by %s, whose pins have landed in %s; a landed card owes nothing, so give its sentences row ids and pins", sec.Line, sec.Title, sec.Owed, file))
		}
	}
	for id := range doc.Retired {
		if r, live := doc.Rows[id]; live {
			out = append(out, fmt.Sprintf("line %d: row %s is retired and marked again; a retired id is never reused", r.Line, id))
		}
	}

	// 2. the ports each row owes a pin in
	owes := map[string]map[string]bool{}
	for id, r := range doc.Rows {
		if r.Informative != "" {
			continue
		}
		set := map[string]bool{}
		for _, f := range r.Features {
			if f == specCompilerPort {
				set[specCompilerPort] = true
				continue
			}
			ps, ok := ports[f]
			if !ok {
				out = append(out, fmt.Sprintf("line %d: row %s names feature %q, which is no row of a roadmap in %s", r.Line, id, f, in.roadmap))
				continue
			}
			for _, p := range ps {
				set[p] = true
			}
		}
		owes[id] = set
	}

	// 3. every pin names a live row, a port that row owes, and a test that runs
	have := map[string]map[string]string{} // row -> port -> "pin" | "owed"
	rowTiers := map[string][]string{}      // row -> the tier of each test pin on it
	index := newRepoIndex(in.fsys)
	// resolve over a copy: the guard records each pin's detected tier and
	// nested module on its own entry without mutating the shared registry.
	pins := append([]specPin(nil), in.pins...)
	for i := range pins {
		p := pins[i]
		where := fmt.Sprintf("pin %s/%s (%s)", p.Row, p.Port, p.origin)
		r, live := doc.Rows[p.Row]
		switch {
		case doc.Retired[p.Row]:
			out = append(out, where+": dead pin, the row is retired")
			continue
		case !live:
			out = append(out, where+": dead pin, no such row in "+in.spec)
			continue
		case r.Informative != "":
			out = append(out, where+": the row is informative and takes no pin")
			continue
		case !owes[p.Row][p.Port]:
			out = append(out, fmt.Sprintf("%s: port %q does not carry the row's feature %v", where, p.Port, r.Features))
			continue
		}
		switch d := specRowDigest(r.Sentence); {
		case p.Digest == "":
			out = append(out, fmt.Sprintf("%s: the pin records no Digest of its row's text; read the test against row %s at line %d, then set Digest: %q", where, p.Row, r.Line, d))
		case p.Digest != d:
			out = append(out, fmt.Sprintf("%s: row %s text changed since its pin; re-read the test and update the digest (line %d now digests to %q; test %s)", where, p.Row, r.Line, d, p.Test+p.Want))
		}
		kind := "pin"
		if p.Owed != "" {
			kind = "owed"
		}
		if have[p.Row] == nil {
			have[p.Row] = map[string]string{}
		}
		if prev := have[p.Row][p.Port]; prev != "" && (prev != kind || kind == "owed") {
			out = append(out, fmt.Sprintf("%s: the port is already %s for this row; a row/port is pinned or owed, once", where, prev))
			continue
		}
		have[p.Row][p.Port] = kind
		if kind == "owed" {
			if p.Test != "" {
				out = append(out, where+": an owed entry names a test; pin it or drop the test")
			}
			if p.Want == "" {
				out = append(out, where+": an owed entry names no Want, the test name it owes")
				continue
			}
			if at := index.defines(p.Want); at != "" {
				out = append(out, fmt.Sprintf("%s: owed test %s already exists at %s; pin it", where, p.Want, at))
			}
			continue
		}
		findings, tier, moduleDir := resolvePin(in.fsys, p)
		if tier != "" && p.Tier == "" {
			p.Tier = tier
		}
		if moduleDir != "" {
			p.ModuleDir = moduleDir
		}
		pins[i] = p
		rowTiers[p.Row] = append(rowTiers[p.Row], p.Tier)
		for _, f := range findings {
			out = append(out, where+": "+f)
		}
		if p.Tier == "slow" {
			for _, f := range slowPinRun(in.fsys, p) {
				out = append(out, where+": "+f)
			}
		}
	}
	// 3b. a runtime-behaviour row may not rest on structure pins alone.
	// docs/SPEC-TABLES.md, "Row ids and pins".
	for id, r := range doc.Rows {
		tiers := rowTiers[id]
		if r.Informative != "" || len(tiers) == 0 || r.Kind == "structure" {
			continue
		}
		onlyStructure := true
		for _, t := range tiers {
			if t != "structure" {
				onlyStructure = false
				break
			}
		}
		if onlyStructure {
			out = append(out, fmt.Sprintf("line %d: row %s is runtime-behaviour but every pin on it is tier=structure; a structure pin cannot be its only pin", r.Line, id))
		}
	}
	// 4. every row/port owed a pin has one
	var ids []string
	for id := range owes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		var ps []string
		for p := range owes[id] {
			ps = append(ps, p)
		}
		sort.Strings(ps)
		for _, p := range ps {
			if have[id][p] == "" {
				out = append(out, fmt.Sprintf("line %d: row %s has no pin and no owed entry in port %s", doc.Rows[id].Line, id, p))
			}
		}
	}
	return doc, out
}

// specRowDigest is the digest a pin records of its row's text: the first 16
// hex digits of the SHA-256 of the sentence as the splitter reads it
// (specRow.Sentence: comments blanked, whitespace collapsed to one space,
// inline code kept, since a renamed field in backticks is a changed rule).
func specRowDigest(sentence string) string {
	sum := sha256.Sum256([]byte(sentence))
	return hex.EncodeToString(sum[:])[:16]
}

// specLandedCards maps each pinning card whose pin file is registered to
// that file: internal/ci/schema_spec_pin_<group>_test.go is card
// schema-spec-pin-<group> with underscores as dashes.
func specLandedCards(pins []specPin) map[string]string {
	out := map[string]string{}
	for _, p := range pins {
		if m := specPinFile.FindStringSubmatch(p.origin); m != nil {
			out["schema-spec-pin-"+strings.ReplaceAll(m[1], "_", "-")] = p.origin
		}
	}
	return out
}

func clip(s string) string {
	if len(s) > 120 {
		return s[:120] + "..."
	}
	return s
}

// ------------------------------------------------------- resolving a pin

// resolvePin returns the findings, the detected tier ("slow" for a Go test
// whose only skip is slowtest.Gate, otherwise "") and the nearest nested Go
// module directory above a Go pin ("" for the root module).
func resolvePin(fsys fs.FS, p specPin) ([]string, string, string) {
	file, sym, ok := strings.Cut(p.Test, ":")
	if !ok || file == "" || sym == "" {
		return []string{fmt.Sprintf("test %q is not <path>:<symbol>", p.Test)}, "", ""
	}
	if _, err := fs.Stat(fsys, file); err != nil {
		return []string{fmt.Sprintf("dead pin, %s does not exist", file)}, "", ""
	}
	switch {
	case strings.HasSuffix(file, "_test.go"):
		return resolveGoPin(fsys, file, sym)
	case strings.HasSuffix(file, ".go"):
		return []string{file + " is not a _test.go file"}, "", ""
	case strings.HasSuffix(file, ".tla"):
		return resolveTLAPin(fsys, file, sym, p.Cfg, p.Module), "", ""
	default:
		return resolveHarnessPin(fsys, file, sym), "", ""
	}
}

// specBuildContext is the build CI's go test runs under: linux/amd64, no
// extra tags.
func specBuildContext(fsys fs.FS) build.Context {
	ctx := build.Default
	ctx.GOOS, ctx.GOARCH, ctx.BuildTags, ctx.CgoEnabled = "linux", "amd64", nil, true
	ctx.OpenFile = func(p string) (io.ReadCloser, error) { return fsys.Open(p) }
	ctx.JoinPath = path.Join
	return ctx
}

type goPkg struct {
	name    string                     // the package clause of its non-test files
	funcs   map[string][]*ast.FuncDecl // plain functions, and methods by name
	imports map[*ast.File]map[string]string
	files   map[*ast.FuncDecl]*ast.File
	names   map[*ast.FuncDecl]string // the file each declaration is in
}

func loadGoPkg(fsys fs.FS, dir string, tests bool) (*goPkg, []string) {
	ctx := specBuildContext(fsys)
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, []string{err.Error()}
	}
	pkg := &goPkg{funcs: map[string][]*ast.FuncDecl{}, imports: map[*ast.File]map[string]string{}, files: map[*ast.FuncDecl]*ast.File{}, names: map[*ast.FuncDecl]string{}}
	fset := token.NewFileSet()
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || (!tests && strings.HasSuffix(n, "_test.go")) {
			continue
		}
		if ok, err := ctx.MatchFile(dir, n); err != nil || !ok {
			continue
		}
		src, err := fs.ReadFile(fsys, path.Join(dir, n))
		if err != nil {
			continue
		}
		f, err := parser.ParseFile(fset, n, src, 0)
		if err != nil {
			return nil, []string{fmt.Sprintf("%s does not parse: %v", path.Join(dir, n), err)}
		}
		if !strings.HasSuffix(n, "_test.go") && pkg.name == "" {
			pkg.name = f.Name.Name
		}
		imps := map[string]string{}
		for _, im := range f.Imports {
			p := strings.Trim(im.Path.Value, `"`)
			name := ""
			if im.Name != nil {
				name = im.Name.Name
			}
			imps[p] = name // the explicit name, or "" for the package clause's
		}
		pkg.imports[f] = imps
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				pkg.funcs[fd.Name.Name] = append(pkg.funcs[fd.Name.Name], fd)
				pkg.files[fd] = f
				pkg.names[fd] = n
			}
		}
	}
	return pkg, nil
}

func goModulePath(fsys fs.FS) string {
	return goModulePathAt(fsys, ".")
}

func goModulePathAt(fsys fs.FS, dir string) string {
	data, err := fs.ReadFile(fsys, path.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		if f := strings.Fields(sc.Text()); len(f) == 2 && f[0] == "module" {
			return f[1]
		}
	}
	return ""
}

var specGoTestName = regexp.MustCompile(`^Test([^a-z]|$)`)

// isTestingT reports whether a function takes exactly one *testing.T.
func isTestingT(fd *ast.FuncDecl, imps map[string]string) bool {
	ps := fd.Type.Params.List
	if len(ps) != 1 || len(ps[0].Names) > 1 {
		return false
	}
	star, ok := ps[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "T" {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	name, imported := imps["testing"]
	return imported && (name == x.Name || name == "" && x.Name == "testing")
}

func resolveGoPin(fsys fs.FS, file, sym string) ([]string, string, string) {
	if !specGoTestName.MatchString(sym) {
		return []string{fmt.Sprintf("%s is not a Test function; go test runs only Test functions", sym)}, "", ""
	}
	for _, el := range strings.Split(file, "/") {
		if el == "testdata" || strings.HasPrefix(el, "_") || strings.HasPrefix(el, ".") {
			return []string{fmt.Sprintf("%s sits under %q, which go test ./... never visits", file, el)}, "", ""
		}
	}
	dir, name := path.Split(file)
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" {
		dir = "."
	}
	// A pin may sit in a nested module: find the nearest go.mod above it and
	// run it from that module, whose go.mod names its module path. It is only
	// accepted when a CI step runs `go test` with that module as its working
	// directory. docs/SPEC-TABLES.md, "Row ids and pins".
	moduleDir := ""
	for d := dir; d != "." && d != "/" && d != ""; d = path.Dir(d) {
		if _, err := fs.Stat(fsys, path.Join(d, "go.mod")); err == nil {
			moduleDir = d
			break
		}
	}
	if moduleDir != "" && !nestedModuleRun(fsys, moduleDir) {
		return []string{fmt.Sprintf("%s is in the nested module %s, which no CI step runs", file, moduleDir)}, "", moduleDir
	}
	ctx := specBuildContext(fsys)
	if ok, err := ctx.MatchFile(dir, name); err != nil || !ok {
		return []string{fmt.Sprintf("%s is build-tagged out of a plain linux/amd64 go test", file)}, "", moduleDir
	}
	pkg, errs := loadGoPkg(fsys, dir, true)
	if errs != nil {
		return errs, "", moduleDir
	}
	var fn *ast.FuncDecl
	for _, fd := range pkg.funcs[sym] {
		if fd.Recv == nil && pkg.names[fd] == name {
			fn = fd
		}
	}
	if fn == nil {
		return []string{fmt.Sprintf("dead pin, %s defines no func %s", file, sym)}, "", moduleDir
	}
	if !isTestingT(fn, pkg.imports[pkg.files[fn]]) {
		return []string{fmt.Sprintf("%s does not take one *testing.T, so go test does not run it", sym)}, "", moduleDir
	}
	mod := goModulePath(fsys)
	if moduleDir != "" {
		mod = goModulePathAt(fsys, moduleDir)
	}
	loaded := map[string]*goPkg{}
	var order []*goPkg // every package loaded, for method lookup by name
	load := func(ip string) *goPkg {
		if mod == "" || (ip != mod && !strings.HasPrefix(ip, mod+"/")) {
			return nil
		}
		d := strings.TrimPrefix(strings.TrimPrefix(ip, mod), "/")
		if moduleDir != "" {
			if d == "" {
				d = moduleDir
			} else {
				d = path.Join(moduleDir, d)
			}
		}
		if d == "" {
			d = "."
		}
		if q, ok := loaded[d]; ok {
			return q
		}
		q, _ := loadGoPkg(fsys, d, false)
		loaded[d] = q
		if q != nil {
			order = append(order, q)
		}
		return q
	}
	order = append(order, pkg)
	seen := map[*ast.FuncDecl]bool{}
	var found []string
	slowGate := false
	var walk func(p *goPkg, fd *ast.FuncDecl, via string)
	walk = func(p *goPkg, fd *ast.FuncDecl, via string) {
		if seen[fd] || len(found) > 0 {
			return
		}
		seen[fd] = true
		imps := p.imports[p.files[fd]]
		// the file's in-module imports, by the name the file calls them
		local := map[string]*goPkg{}
		testingName := ""
		slowTestName := ""
		for ip, nm := range imps {
			if ip == "testing" {
				testingName = nm
				if nm == "" {
					testingName = "testing"
				}
				continue
			}
			// internal/slowtest is the one sanctioned gate. Its call is not a
			// plain skip: it is the slow tier. docs/SPEC-TABLES.md, "Row ids
			// and pins".
			if ip == "github.com/mas-bandwidth/schema/v2/internal/slowtest" || strings.HasSuffix(ip, "/internal/slowtest") {
				slowTestName = nm
				if slowTestName == "" {
					slowTestName = "slowtest"
				}
				continue
			}
			if q := load(ip); q != nil {
				if nm == "" {
					nm = q.name
				}
				local[nm] = q
			}
		}
		here := via + fd.Name.Name
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if len(found) > 0 {
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch f := call.Fun.(type) {
			case *ast.Ident:
				for _, g := range p.funcs[f.Name] {
					if g.Recv == nil {
						walk(p, g, here+" -> ")
					}
				}
			case *ast.SelectorExpr:
				name := f.Sel.Name
				if x, ok := f.X.(*ast.Ident); ok {
					if x.Name == testingName && testingName != "" {
						if name == "Short" {
							found = append(found, fmt.Sprintf("%s reads testing.Short, so -short CI skips its body", here))
							return false
						}
						return true
					}
					if x.Name == slowTestName && slowTestName != "" {
						if name == "Gate" {
							slowGate = true
						}
						return true
					}
					if q := local[x.Name]; q != nil {
						for _, g := range q.funcs[name] {
							if g.Recv == nil {
								walk(q, g, here+" -> "+x.Name+".")
							}
						}
						return true
					}
				}
				if name == "Skip" || name == "Skipf" || name == "SkipNow" {
					found = append(found, fmt.Sprintf("%s calls %s; a pin must name a test that runs", here, name))
					return false
				}
				// a method call: every method of that name in a loaded package
				for _, q := range order {
					for _, g := range q.funcs[name] {
						if g.Recv != nil {
							walk(q, g, here+" -> ")
						}
					}
				}
			}
			return true
		})
	}
	walk(pkg, fn, "")
	// a TestMain gates every test of the package
	for _, tm := range pkg.funcs["TestMain"] {
		if tm.Recv != nil || len(found) > 0 {
			continue
		}
		runs := false
		ast.Inspect(tm.Body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if s, ok := c.Fun.(*ast.SelectorExpr); ok && s.Sel.Name == "Run" {
					runs = true
				}
			}
			return true
		})
		if !runs {
			found = append(found, "the package's TestMain never calls m.Run, so no test in it runs")
			continue
		}
		walk(pkg, tm, "")
	}
	tier := ""
	if len(found) == 0 && slowGate {
		tier = "slow"
	}
	return found, tier, moduleDir
}

// ---------------------------------------------------- the CI surface

// runText is one place a CI step may run a module or a slow pin: a workflow,
// the Makefile, or a make include.
type runText struct {
	workflow bool
	text     string
}

func repoRunTexts(fsys fs.FS) []runText {
	var out []runText
	if entries, err := fs.ReadDir(fsys, ".github/workflows"); err == nil {
		for _, e := range entries {
			n := e.Name()
			if e.IsDir() || (!strings.HasSuffix(n, ".yml") && !strings.HasSuffix(n, ".yaml")) {
				continue
			}
			if b, err := fs.ReadFile(fsys, path.Join(".github/workflows", n)); err == nil {
				out = append(out, runText{workflow: true, text: string(b)})
			}
		}
	}
	if b, err := fs.ReadFile(fsys, "Makefile"); err == nil {
		out = append(out, runText{text: string(b)})
	}
	if entries, err := fs.ReadDir(fsys, "make"); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".mk") {
				continue
			}
			if b, err := fs.ReadFile(fsys, path.Join("make", e.Name())); err == nil {
				out = append(out, runText{text: string(b)})
			}
		}
	}
	return out
}

// nestedModuleRun reports whether a CI step runs `go test` with the module
// directory as its working directory: in a workflow, or in the Makefile or an
// include it calls. docs/SPEC-TABLES.md, "Row ids and pins".
func nestedModuleRun(fsys fs.FS, dir string) bool {
	for _, rt := range repoRunTexts(fsys) {
		lines := strings.Split(rt.text, "\n")
		for i, line := range lines {
			t := strings.TrimSpace(line)
			if strings.HasPrefix(t, "#") {
				continue
			}
			if strings.Contains(line, "go test") &&
				(strings.Contains(line, "cd "+dir) || strings.Contains(line, "-C "+dir)) {
				return true
			}
			if strings.HasPrefix(t, "working-directory:") {
				v := strings.Trim(strings.TrimSpace(strings.TrimPrefix(t, "working-directory:")), `"'`)
				if v != dir {
					continue
				}
				for j := i; j < len(lines) && j < i+30; j++ {
					if strings.Contains(lines[j], "go test") {
						return true
					}
				}
			}
		}
	}
	return false
}

type slowCommand struct {
	cwd  string
	line string
}

var (
	wfStepStart  = regexp.MustCompile(`^(\s*)-\s`)
	wfRun        = regexp.MustCompile(`^\s*run:\s*(.*)$`)
	wfWorkDir    = regexp.MustCompile(`^\s*working-directory:\s*(\S+)\s*$`)
	wfSlowEnv    = regexp.MustCompile(`SCHEMA_SLOW:\s*['"]?1['"]?`)
	makeHeader   = regexp.MustCompile(`^([A-Za-z0-9_.-]+):(?:\s|$)`)
	makeCd       = regexp.MustCompile(`(?:^|&&|;)\s*cd\s+([^\s&;]+)`)
	slowInLine   = regexp.MustCompile(`SCHEMA_SLOW\s*=\s*1`)
	makeInvoke   = regexp.MustCompile(`\bmake\s+((?:[A-Za-z0-9_.-]+\s*)+)`)
	goTestInLine = "go test"
)

// workflowSteps splits a workflow into steps and keeps each step's run
// command, working directory and whether it sets SCHEMA_SLOW=1.
func workflowSteps(text string) []struct {
	cwd, run string
	slow     bool
} {
	lines := strings.Split(text, "\n")
	type span struct{ start, end int }
	var spans []span
	start := -1
	for i, l := range lines {
		if m := wfStepStart.FindStringSubmatch(l); m != nil && len(m[1]) <= 10 {
			if start >= 0 {
				spans = append(spans, span{start, i})
			}
			start = i
		}
	}
	if start >= 0 {
		spans = append(spans, span{start, len(lines)})
	}
	var out []struct {
		cwd, run string
		slow     bool
	}
	for _, sp := range spans {
		block := lines[sp.start:sp.end]
		step := struct {
			cwd, run string
			slow     bool
		}{}
		for _, l := range block {
			if wfSlowEnv.MatchString(l) {
				step.slow = true
			}
			if m := wfWorkDir.FindStringSubmatch(l); m != nil {
				step.cwd = strings.Trim(m[1], `"'`)
			}
		}
		for i, l := range block {
			m := wfRun.FindStringSubmatch(l)
			if m == nil {
				continue
			}
			v := strings.TrimSpace(m[1])
			if v == "|" || v == ">" || v == "|-" || v == ">-" || v == "|+" || v == ">+" {
				indent := len(l) - len(strings.TrimLeft(l, " \t"))
				var body []string
				for j := i + 1; j < len(block); j++ {
					ind := len(block[j]) - len(strings.TrimLeft(block[j], " \t"))
					if strings.TrimSpace(block[j]) != "" && ind <= indent {
						break
					}
					body = append(body, strings.TrimSpace(block[j]))
				}
				v = strings.Join(body, " && ")
			}
			step.run = v
			break
		}
		out = append(out, step)
	}
	return out
}

// makeTargets maps each make target to its recipe lines, joining backslash
// continuations first.
func makeTargets(text string) map[string][]string {
	raw := strings.Split(text, "\n")
	var lines []string
	for i := 0; i < len(raw); i++ {
		l := raw[i]
		for strings.HasSuffix(strings.TrimRight(l, " \t"), "\\") && i+1 < len(raw) {
			l = strings.TrimRight(strings.TrimSuffix(strings.TrimRight(l, " \t"), "\\"), " \t") + " " + strings.TrimSpace(raw[i+1])
			i++
		}
		lines = append(lines, l)
	}
	out := map[string][]string{}
	cur := ""
	for _, l := range lines {
		if m := makeHeader.FindStringSubmatch(l); m != nil {
			cur = m[1]
			continue
		}
		if strings.HasPrefix(l, "\t") && cur != "" {
			out[cur] = append(out[cur], strings.TrimSpace(l))
			continue
		}
		if !strings.HasPrefix(l, "\t") && strings.TrimSpace(l) != "" && !strings.HasPrefix(strings.TrimSpace(l), "#") {
			cur = ""
		}
	}
	return out
}

// allSlowCommands is every `go test` command CI runs with SCHEMA_SLOW=1,
// directly in a workflow step or through a make target that step calls.
func allSlowCommands(fsys fs.FS) []slowCommand {
	var out []slowCommand
	texts := repoRunTexts(fsys)
	targets := map[string][]string{}
	for _, rt := range texts {
		for name, body := range makeTargets(rt.text) {
			if _, ok := targets[name]; !ok {
				targets[name] = body
			}
		}
		for _, l := range strings.Split(rt.text, "\n") {
			if strings.Contains(l, goTestInLine) && slowInLine.MatchString(l) {
				out = append(out, slowCommand{cwd: commandCWD("", l), line: l})
			}
		}
		if !rt.workflow {
			continue
		}
		for _, st := range workflowSteps(rt.text) {
			if !strings.Contains(st.run, goTestInLine) && !strings.Contains(st.run, "make ") {
				continue
			}
			if strings.Contains(st.run, goTestInLine) && st.slow {
				out = append(out, slowCommand{cwd: commandCWD(st.cwd, st.run), line: st.run})
			}
			if !st.slow {
				continue
			}
			if m := makeInvoke.FindStringSubmatch(st.run); m != nil {
				for _, name := range strings.Fields(m[1]) {
					for _, line := range targets[name] {
						if strings.Contains(line, goTestInLine) {
							out = append(out, slowCommand{cwd: commandCWD(st.cwd, line), line: line})
						}
					}
				}
			}
		}
	}
	return out
}

func commandCWD(stepCWD, line string) string {
	cwd := stepCWD
	if m := makeCd.FindStringSubmatch(line); m != nil {
		dir := strings.Trim(m[1], `"'`)
		if !strings.HasPrefix(dir, "/") {
			if cwd == "" {
				cwd = path.Clean(dir)
			} else {
				cwd = path.Clean(path.Join(cwd, dir))
			}
		}
	}
	if cwd == "." {
		cwd = ""
	}
	return cwd
}

// slowPinRun returns why a slow pin is not named by a SCHEMA_SLOW=1 step, or
// nil. The package must be run there and no -run filter may exclude the test
// name. docs/SPEC-TABLES.md, "Row ids and pins".
func slowPinRun(fsys fs.FS, p specPin) []string {
	file, sym, _ := strings.Cut(p.Test, ":")
	dir := path.Dir(file)
	if dir == "." {
		dir = ""
	}
	for _, c := range allSlowCommands(fsys) {
		if slowCommandRuns(c, dir, sym) {
			return nil
		}
	}
	return []string{fmt.Sprintf("slow pin %s/%s %s is not named by a SCHEMA_SLOW=1 step in ci-full/nightly: no step runs package %s with %s and without a -run filter excluding it", p.Row, p.Port, p.Test, clip(dirName(dir)), sym)}
}

func dirName(dir string) string {
	if dir == "" {
		return "."
	}
	return dir
}

func slowCommandRuns(c slowCommand, pkgDir, testName string) bool {
	i := strings.Index(c.line, goTestInLine)
	if i < 0 {
		return false
	}
	fields := strings.Fields(c.line[i+len(goTestInLine):])
	filter := ""
	var pkgs []string
	for j := 0; j < len(fields); j++ {
		f := strings.Trim(fields[j], `"'`)
		switch {
		case f == "-run" || f == "--run":
			if j+1 < len(fields) {
				filter = strings.Trim(fields[j+1], `"'`)
				j++
			}
		case strings.HasPrefix(f, "-run="):
			filter = strings.Trim(strings.TrimPrefix(f, "-run="), `"'`)
		case strings.HasPrefix(f, "-"):
			if f == "-timeout" || f == "-overlay" || f == "-soak" || f == "-count" || f == "-tags" || f == "-p" {
				j++
			}
		case f == "" || f == "||" || f == "&&" || f == ";" || f == "then" || f == "do" || f == "else":
		case strings.HasPrefix(f, "2>") || strings.HasPrefix(f, ">"):
		default:
			pkgs = append(pkgs, f)
		}
	}
	covered := false
	for _, tok := range pkgs {
		if packageRuns(c.cwd, tok, pkgDir) {
			covered = true
			break
		}
	}
	if !covered {
		return false
	}
	if filter == "" {
		return true
	}
	re, err := regexp.Compile(filter)
	if err != nil {
		return false
	}
	return re.MatchString(testName)
}

func packageRuns(cwd, tok, pkgDir string) bool {
	if tok == "" || strings.ContainsAny(tok, "$()`") || strings.HasPrefix(tok, "/") {
		return false
	}
	if tok == "..." || tok == "./..." {
		return cwd == "" || pkgDir == cwd || strings.HasPrefix(pkgDir, cwd+"/")
	}
	var p string
	switch {
	case tok == ".":
		p = cwd
	case strings.HasPrefix(tok, "./"):
		p = path.Join(cwd, strings.TrimPrefix(tok, "./"))
	default:
		p = path.Join(cwd, tok)
	}
	p = path.Clean(p)
	if p == "." {
		p = ""
	}
	if strings.HasSuffix(p, "/...") {
		base := strings.TrimSuffix(p, "/...")
		if base == "." {
			base = ""
		}
		if base == "" {
			return pkgDir == "" || !strings.Contains(pkgDir, "/")
		}
		return pkgDir == base || strings.HasPrefix(pkgDir, base+"/")
	}
	return p == pkgDir
}

var (
	tlaExtends   = regexp.MustCompile(`(?m)^\s*EXTENDS\s+([^\n]*)`)
	tlaCfgKeys   = regexp.MustCompile(`^(INVARIANTS?|PROPERTY|PROPERTIES|CONSTANTS?|SPECIFICATION|INIT|NEXT|SYMMETRY|CONSTRAINTS?|ACTION_CONSTRAINTS?|VIEW|CHECK_DEADLOCK|POSTCONDITION|ALIAS)\b`)
	tlaModuleHdr = regexp.MustCompile(`-{4,}\s*MODULE\s+(\w+)\s*-{4,}`)
)

// stripTLAComments blanks every TLA+ comment: a `\*` line comment to the end
// of its line, and a `(* ... *)` block comment, which NESTS, so
// `(* a (* b *) c *)` is one comment; inside a block, a `\*` also hides the
// rest of its line. String literals are kept whole,
// so a `(*` inside one opens nothing. Newlines are kept, so line structure (and
// every ^-anchored match on it) is what TLC sees. The same reading serves a
// TLC .cfg, whose comments are the same two forms.
func stripTLAComments(s string) string {
	b := []byte(s)
	out := make([]byte, 0, len(b))
	depth := 0
	inStr := false
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch {
		case depth == 0 && inStr:
			out = append(out, c)
			if c == '\\' && i+1 < len(b) {
				i++
				out = append(out, b[i])
			} else if c == '"' {
				inStr = false
			}
		case depth == 0 && c == '"':
			inStr = true
			out = append(out, c)
		case c == '(' && i+1 < len(b) && b[i+1] == '*':
			depth++
			out = append(out, ' ', ' ')
			i++
		case depth > 0 && c == '*' && i+1 < len(b) && b[i+1] == ')':
			depth--
			out = append(out, ' ', ' ')
			i++
		case depth > 0 && c == '\\' && i+1 < len(b) && b[i+1] == '*':
			// a `\*` inside a block comment hides the rest of its line, a `*)`
			// on it included. Read this way the guard can only hide more
			// text, never less, so a doubt here is a refused pin, not a
			// commented-out one accepted.
			for i < len(b) && b[i] != '\n' {
				out = append(out, ' ')
				i++
			}
			if i < len(b) {
				out = append(out, '\n')
			}
		case depth > 0:
			if c == '\n' {
				out = append(out, c)
			} else {
				out = append(out, ' ')
			}
		case c == '\\' && i+1 < len(b) && b[i+1] == '*':
			for i < len(b) && b[i] != '\n' {
				out = append(out, ' ')
				i++
			}
			if i < len(b) {
				out = append(out, '\n')
			}
		default:
			out = append(out, c)
		}
	}
	return string(out)
}

func resolveTLAPin(fsys fs.FS, model, prop, cfg, module string) []string {
	var out []string
	raw, _ := fs.ReadFile(fsys, model)
	// every definition, EXTENDS and header is resolved in comment-free text:
	// a property that exists only inside a comment does not exist for TLC
	src := stripTLAComments(string(raw))
	hdr := tlaModuleHdr.FindStringSubmatch(string(src))
	if hdr == nil {
		return []string{model + " has no MODULE header"}
	}
	if module == "" {
		module = hdr[1]
	}
	defText := string(src)
	if module != hdr[1] {
		extends := false
		for _, m := range tlaExtends.FindAllStringSubmatch(string(src), -1) {
			for _, n := range strings.Split(m[1], ",") {
				if strings.TrimSpace(n) == module {
					extends = true
				}
			}
		}
		if !extends {
			out = append(out, fmt.Sprintf("model %s does not EXTENDS %s", model, module))
		}
		ms, err := fs.ReadFile(fsys, path.Join(path.Dir(model), module+".tla"))
		if err != nil {
			out = append(out, fmt.Sprintf("module %s.tla is not beside %s", module, model))
		}
		defText += "\n" + stripTLAComments(string(ms))
	}
	if !regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(prop) + `\s*(\([^)]*\))?\s*==`).MatchString(defText) {
		out = append(out, fmt.Sprintf("dead pin, no %s == in %s or %s", prop, model, module))
	}
	if cfg == "" {
		return append(out, "a .tla pin names no Cfg")
	}
	cs, err := fs.ReadFile(fsys, cfg)
	if err != nil {
		return append(out, fmt.Sprintf("dead pin, cfg %s does not exist", cfg))
	}
	listed, section := false, ""
	cfgText := stripTLAComments(string(cs))
	for _, l := range strings.Split(cfgText, "\n") {
		f := strings.Fields(l)
		if len(f) == 0 {
			continue
		}
		start := 0
		if m := tlaCfgKeys.FindString(f[0]); m != "" {
			section, start = m, 1
		}
		for _, w := range f[start:] {
			if w == prop && strings.HasPrefix(section, "INVARIANT") || w == prop && strings.HasPrefix(section, "PROPERT") {
				listed = true
			}
		}
	}
	if !listed {
		out = append(out, fmt.Sprintf("%s is not under INVARIANT(S) or PROPERTY(IES) in %s, so TLC never checks it", prop, cfg))
	}
	if !referencedByBuild(fsys, path.Base(cfg), hdr[1]) {
		out = append(out, fmt.Sprintf("no line of the Makefile, make/*.mk, a workflow or tla/RUNS.tsv names %s with the model %s, so nothing runs TLC on it", path.Base(cfg), hdr[1]))
	}
	return out
}

// referencedByBuild reports whether one line of the build, or of the TLC
// run ledger tla/RUNS.tsv, names both the cfg and the model.
func referencedByBuild(fsys fs.FS, cfg, model string) bool {
	var files []string
	files = append(files, "Makefile", "tla/RUNS.tsv")
	for _, g := range []string{"make/*.mk", "make/*/*.mk", ".github/workflows/*.yml", "tla/Makefile", "tla/*.mk"} {
		m, _ := fs.Glob(fsys, g)
		files = append(files, m...)
	}
	cfgTok := regexp.MustCompile(`(^|[\s/])` + regexp.QuoteMeta(cfg) + `(\s|$)`)
	modTok := regexp.MustCompile(`(^|[\s/])` + regexp.QuoteMeta(model) + `(\.tla)?(\s|$)`)
	for _, f := range files {
		b, err := fs.ReadFile(fsys, f)
		if err != nil {
			continue
		}
		for _, l := range strings.Split(string(b), "\n") {
			if t := strings.TrimSpace(l); strings.HasPrefix(t, "#") {
				continue
			}
			if cfgTok.MatchString(l) && modTok.MatchString(l) {
				return true
			}
		}
	}
	return false
}

var harnessSkip = regexp.MustCompile(`(?i)(#\[ignore\b|@disabled|@ignore\b|\[ignore\b|skip\s*[:=]|@(module)?tag\s+:?skip|\.skip\s*\()`)

// specComments strips //, /* */ and # line comments before calls are counted.
var specComments = regexp.MustCompile(`(?s)/\*.*?\*/|//[^\n]*|(?m)^\s*#\s[^\n]*`)

// resolveHarnessPin holds a port's harness function to: defined in the
// file, not marked skipped on or above its definition, and called from
// somewhere in its own directory.
func resolveHarnessPin(fsys fs.FS, file, sym string) []string {
	src, _ := fs.ReadFile(fsys, file)
	q := regexp.QuoteMeta(sym)
	def := regexp.MustCompile(`(?m)^.*(\bfn\s+` + q + `\b|\bdefp?\s+` + q + `\b|\bfunction\s+` + q + `\b|\b` + q + `\s*=\s*(async\s*)?(\([^)]*\)|\w+)\s*=>|^[\w<>\[\],*&: \t]+\b` + q + `\s*\([^)]*\)\s*(\{.*|[^;]*)$)`)
	loc := def.FindStringIndex(string(src))
	if loc == nil {
		return []string{fmt.Sprintf("dead pin, %s defines no %s", file, sym)}
	}
	before := string(src[:loc[0]])
	lines := strings.Split(before, "\n")
	ctxLines := lines
	if len(ctxLines) > 3 {
		ctxLines = ctxLines[len(ctxLines)-3:]
	}
	defLine := string(src[loc[0]:loc[1]])
	if harnessSkip.MatchString(strings.Join(ctxLines, "\n") + defLine) {
		return []string{fmt.Sprintf("%s in %s is marked skipped or ignored", sym, file)}
	}
	call := regexp.MustCompile(`\b` + q + `\s*\(`)
	proto := regexp.MustCompile(`^\s*(static\s+|extern\s+|inline\s+)*[A-Za-z_][\w<>:*&\s]*[\s*&]` + q + `\s*\([^)]*\)\s*;\s*$`)
	dir := path.Dir(file)
	entries, _ := fs.ReadDir(fsys, dir)
	calls := 0
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != path.Ext(file) {
			continue
		}
		b, _ := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		for _, l := range strings.Split(specComments.ReplaceAllString(string(b), ""), "\n") {
			if def.MatchString(l) || proto.MatchString(l) {
				continue // the definition, or a declaration, is not a call
			}
			calls += len(call.FindAllStringIndex(l, -1))
		}
	}
	if calls == 0 {
		return []string{fmt.Sprintf("nothing in %s calls %s", dir, sym)}
	}
	return nil
}

// repoIndex finds where a test name is defined, repo-wide.
type repoIndex struct {
	fsys   fs.FS
	files  []string
	words  map[string]bool
	walked bool
}

var specSourceExt = map[string]bool{".go": true, ".c": true, ".h": true, ".cpp": true, ".hpp": true, ".cc": true, ".cs": true, ".rs": true, ".java": true, ".js": true, ".mjs": true, ".dart": true, ".ex": true, ".exs": true, ".tla": true, ".ts": true, ".lua": true, ".luau": true, ".nut": true, ".py": true}

// newRepoIndex walks nothing until the first lookup: only an owed entry
// asks, and a registry with none should not pay for a walk of the tree.
func newRepoIndex(fsys fs.FS) *repoIndex {
	return &repoIndex{fsys: fsys}
}

func (ix *repoIndex) walk() {
	if ix.walked {
		return
	}
	ix.walked = true
	ix.words = map[string]bool{}
	fsys := ix.fsys
	_ = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			n := d.Name()
			if p != "." && (strings.HasPrefix(n, ".") || n == "node_modules" || n == "target" || n == "build" || n == "obj") {
				return fs.SkipDir
			}
			return nil
		}
		if specSourceExt[path.Ext(p)] {
			ix.files = append(ix.files, p)
		}
		return nil
	})
	// A row group with every port owed asks for over a thousand absent test
	// names. Read each source file once and remember its identifier words, so
	// an absent name costs one map lookup instead of a walk of every file.
	for _, f := range ix.files {
		b, err := fs.ReadFile(fsys, f)
		if err != nil {
			continue
		}
		for _, w := range specWord.FindAllString(string(b), -1) {
			ix.words[w] = true
		}
	}
}

var specWord = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

func (ix *repoIndex) defines(name string) string {
	ix.walk()
	if !ix.words[name] {
		return ""
	}
	q := regexp.QuoteMeta(name)
	def := regexp.MustCompile(`\b(func|fn|def|defp|function|void|static|public|private|async|local function)\s+(\([^)]*\)\s*)?` + q + `\b|^\s*` + q + `\s*==|\b(test|it)\(\s*["']` + q + `["']|^\s*(?:[A-Za-z_][\w<>:]*[\s*&]+)+` + q + `\s*\([^;]*$`)
	for _, f := range ix.files {
		b, err := fs.ReadFile(ix.fsys, f)
		if err != nil || !strings.Contains(string(b), name) {
			continue
		}
		for _, l := range strings.Split(string(b), "\n") {
			if def.MatchString(l) {
				return f
			}
		}
	}
	return ""
}

// ------------------------------------------------------------------ tests

// TestSpecPinsHoldOnTheSpec is the guard on the real spec, roadmap and
// registry.
func TestSpecPinsHoldOnTheSpec(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	doc, findings := checkSpecPins(specPinsInput{fsys: os.DirFS(root), spec: "docs/SPEC-TABLES.md", roadmap: "docs/roadmap.sexp", pins: specPinRegistry})
	for _, f := range findings {
		t.Error(f)
	}
	if len(doc.Rows) == 0 || len(specPinRegistry) == 0 {
		t.Fatalf("the guard read %d rows and %d pins; a guard over nothing proves nothing", len(doc.Rows), len(specPinRegistry))
	}
	normative := 0
	for _, s := range doc.Sentences {
		if s.Normative {
			normative++
		}
	}
	if normative < 100 {
		t.Fatalf("only %d normative sentences found; the splitter is reading the wrong text", normative)
	}
}

// overlayFS is the repository with some files replaced, so a witness can
// plant a defect in the real spec or registry without touching the tree.
type overlayFS struct {
	fs.FS
	over map[string][]byte
}

func (o overlayFS) ReadFile(name string) ([]byte, error) {
	if b, ok := o.over[name]; ok {
		return b, nil
	}
	return fs.ReadFile(o.FS, name)
}

// TestSpecPinsRealSpecPlants plants defects in the REAL spec and registry
// (not the fixture) and requires the guard to go red on each: a witness that
// only ever fires on a toy fixture proves the guard on the toy.
func TestSpecPinsRealSpecPlants(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	base := os.DirFS(root)
	spec, err := fs.ReadFile(base, "docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	const marker = "<!-- row T2.7-2 feature=compiler -->"
	if !strings.Contains(string(spec), marker) {
		t.Fatalf("the spec no longer carries %s; the plant needs a live row", marker)
	}
	// T2.7-1 reversed with its marker kept: the r3 read's plant, on which
	// the guard stayed green before pins carried a digest
	const t271 = "**Nothing in a declaration selects it, and nothing is refused for asking.**<!-- row T2.7-1 feature=compiler -->"
	const t271Reversed = "**A declaration must select it with a `block` marker, and an unmarked fixed table is refused.**<!-- row T2.7-1 feature=compiler -->"
	const heading27 = "### 2.7 The block form is not declared\n"
	for _, s := range []string{t271, heading27} {
		if !strings.Contains(string(spec), s) {
			t.Fatalf("the spec no longer carries %q; the plant needs it", s)
		}
	}
	// a new unmarked rule in the fully pinned §2.7, excused by an owed marker
	owedRule := func(card string) string {
		s := strings.Replace(string(spec), heading27, heading27+"<!-- spec-pins owed "+card+" -->\n", 1)
		return strings.Replace(s, t271, t271+"\nA fixed table must not declare a field named `pitch`.", 1)
	}
	cases := []struct {
		name  string
		spec  string
		pins  []specPin
		wants []string
	}{
		{
			// the sentence's id renumbered under a pin that still names the
			// old one: the pin is stale and the new row is unpinned
			name:  "stale row id in the registry",
			spec:  strings.Replace(string(spec), marker, "<!-- row T2.7-9 feature=compiler -->", 1),
			pins:  specPinRegistry,
			wants: []string{"pin T2.7-2/compiler", "dead pin, no such row", "row T2.7-9 has no pin"},
		},
		{
			name:  "a pin naming a row the spec never had",
			spec:  string(spec),
			pins:  append(append([]specPin(nil), specPinRegistry...), specPin{Row: "T2.7-77", Port: "compiler", Test: "internal/check/blockprologue_test.go:TestFixedTableFieldNamedAfterTheBlockPrologueIsRefused", origin: "plant"}),
			wants: []string{"pin T2.7-77/compiler (plant): dead pin, no such row"},
		},
		{
			name:  "the worked example's sentence loses its id",
			spec:  strings.Replace(string(spec), marker, "", 1),
			pins:  specPinRegistry,
			wants: []string{"normative sentence without a row id (section \"2.7 The block form is not declared\")"},
		},
		{
			name:  "the worked example rule reversed with its marker kept",
			spec:  strings.Replace(string(spec), t271, t271Reversed, 1),
			pins:  specPinRegistry,
			wants: []string{"pin T2.7-1/compiler (internal/ci/schema_spec_pin_design_row_ids_test.go): row T2.7-1 text changed since its pin; re-read the test and update the digest"},
		},
		{
			name:  "a new rule in the pinned section excused by an invented card",
			spec:  owedRule("schema-spec-pin-s27"),
			pins:  specPinRegistry,
			wants: []string{`an owed section names "schema-spec-pin-s27", which is no card on the guard's specOwedCards list`},
		},
		{
			name:  "a new rule in the pinned section excused by the landed design card",
			spec:  owedRule("schema-spec-pin-design-row-ids"),
			pins:  specPinRegistry,
			wants: []string{"section \"2.7 The block form is not declared\" is marked owed by schema-spec-pin-design-row-ids, whose pins have landed in internal/ci/schema_spec_pin_design_row_ids_test.go"},
		},
		{
			// a card on the closed list whose pin file has landed: only the
			// landed check can fire, since the name is listed
			name:  "a new rule in the pinned section excused by a listed card that has landed",
			spec:  owedRule("schema-spec-pin-s2"),
			pins:  append(append([]specPin(nil), specPinRegistry...), specPin{Row: "T2.7-1", Port: "compiler", Test: "ir/blocklayout_test.go:TestBlockLayoutMatchesTheWorkedTable", Digest: "de000392ae1e6bda", origin: "internal/ci/schema_spec_pin_s2_test.go"}),
			wants: []string{"section \"2.7 The block form is not declared\" is marked owed by schema-spec-pin-s2, whose pins have landed in internal/ci/schema_spec_pin_s2_test.go"},
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			fsys := overlayFS{FS: base, over: map[string][]byte{"docs/SPEC-TABLES.md": []byte(c.spec)}}
			_, got := checkSpecPins(specPinsInput{fsys: fsys, spec: "docs/SPEC-TABLES.md", roadmap: "docs/roadmap.sexp", pins: c.pins})
			all := strings.Join(got, "\n")
			for _, w := range c.wants {
				if !strings.Contains(all, w) {
					t.Errorf("planted %q; want a finding containing %q, got:\n%s", c.name, w, all)
				}
			}
		})
	}
}

// specSizeScope maps a section's subject to roadmap features for planning.
// It does not assign row ids or pins. A section's port union is an estimate
// until its sentences have individual feature= attributes; Unmarked remains
// visible, so this table cannot be mistaken for completed row/port coverage.
// A trailing /* includes the rows of that roadmap, not a hard-coded port list.
// OnlyPorts narrows a language-specific ruling after validating membership.
type specSizeScope struct {
	Features  []string
	OnlyPorts []string
}

var specSizeScopes = map[string]specSizeScope{
	"schema — tables":                     {Features: []string{"packet-wire/*", "fixed-tables/*", "variable-tables/*"}},
	"The three wires":                     {Features: []string{"compiler", "fixed-tables/*", "variable-tables/*"}},
	"What allocates, and what never does": {Features: []string{"compiler", "fixed-tables/*", "variable-tables/*"}},
	"1":                                   {Features: []string{"compiler", "fixed-tables/*", "variable-tables/*"}},
	"2":                                   {Features: []string{"compiler", "fixed-tables/*", "variable-tables/*"}},
	"2.1":                                 {Features: []string{"compiler", "variable-tables/variable-class", "variable-tables/pointer-arrays"}},
	"2.2":                                 {Features: []string{"compiler", "variable-tables/fixed-class"}},
	"2.3":                                 {Features: []string{"compiler", "variable-tables/optional-fields"}},
	"2.4":                                 {Features: []string{"compiler", "variable-tables/enum-keyed-arrays"}},
	"2.5":                                 {Features: []string{"compiler", "variable-tables/blobs"}},
	"2.6":                                 {Features: []string{"compiler", "variable-tables/union-table-arms", "variable-tables/union-arms-any"}},
	"2.7":                                 {Features: []string{"compiler"}},
	"2.8":                                 {Features: []string{"compiler", "variable-tables/maps"}},
	"2.9":                                 {Features: []string{"compiler", "variable-tables/unbounded-arrays"}},
	"2.10":                                {Features: []string{"compiler", "fixed-tables/field-evolution"}},
	"3":                                   {Features: []string{"compiler", "variable-tables/id-table-wire"}},
	"3.1":                                 {Features: []string{"compiler", "variable-tables/variable-class", "variable-tables/blobs"}},
	"3.2":                                 {Features: []string{"compiler", "variable-tables/enum-keyed-arrays"}},
	"3.3":                                 {Features: []string{"compiler", "variable-tables/message-form"}},
	"3.4":                                 {Features: []string{"compiler", "fixed-tables/*"}},
	"4":                                   {Features: []string{"compiler", "variable-tables/widening", "fixed-tables/numeric-evolution"}},
	"4.2":                                 {Features: []string{"variable-tables/wire-fuzzer", "fixed-tables/hostile-input"}},
	"5":                                   {Features: []string{"compiler", "variable-tables/renames", "variable-tables/defaults-and-table-rename", "variable-tables/id-table-wire"}},
	"6":                                   {Features: []string{"variable-tables/fixed-class", "variable-tables/variable-class", "variable-tables/cook-open"}},
	"6.1":                                 {Features: []string{"variable-tables/fixed-class"}},
	"6.6":                                 {Features: []string{"variable-tables/retain-unknown"}},
	"7":                                   {Features: []string{"compiler", "variable-tables/cook-open", "variable-tables/cook-write"}},
	"8":                                   {Features: []string{"compiler", "variable-tables/reflection", "variable-tables/unit-registry", "variable-tables/doc-tags"}},
	"9":                                   {Features: []string{"compiler", "variable-tables/variable-class", "variable-tables/block-build"}},
	"10":                                  {Features: []string{"compiler", "variable-tables/build-version"}},
	"11":                                  {Features: []string{"compiler", "fixed-tables/*", "variable-tables/*"}},
	"12":                                  {Features: []string{"compiler", "variable-tables/fixed-class"}},
	"12.1":                                {Features: []string{"compiler", "variable-tables/block-read", "variable-tables/block-build"}},
	"13":                                  {Features: []string{"compiler", "fixed-tables/*", "variable-tables/*"}},
	"13.1":                                {Features: []string{"compiler", "variable-tables/variable-class"}},
	"13.3":                                {Features: []string{"compiler", "variable-tables/fixed-class", "variable-tables/optional-fields", "variable-tables/enum-keyed-arrays"}},
	"13.4":                                {Features: []string{"compiler", "variable-tables/cook-open", "variable-tables/cook-write"}},
	"13.6":                                {Features: []string{"compiler", "variable-tables/block-read", "variable-tables/block-build"}},
	"13.7":                                {Features: []string{"compiler", "variable-tables/reflection"}},
	"13.8":                                {Features: []string{"compiler", "variable-tables/build-version"}},
	"13.9":                                {Features: []string{"variable-tables/fixed-class"}, OnlyPorts: []string{"cpp"}},
	"14":                                  {Features: []string{"compiler", "fixed-tables/*", "variable-tables/*"}},
	"15":                                  {Features: []string{"compiler", "fixed-tables/*", "variable-tables/*"}},
	"16":                                  {Features: []string{"compiler", "variable-tables/text-form"}},
	"16.7":                                {Features: []string{"compiler", "variable-tables/text-form-variable"}},
	"17":                                  {Features: []string{"compiler"}},
	"18":                                  {Features: []string{"compiler"}},
	"19":                                  {Features: []string{"compiler", "variable-tables/block-read", "variable-tables/block-build", "variable-tables/reflection"}},
	"20":                                  {Features: []string{"compiler", "variable-tables/build-version"}},
	"21":                                  {Features: []string{"compiler", "fixed-tables/*"}},
}

type specSizeRow struct {
	Section pinSection
	Ports   []string
	Basis   string
}

func sizingScopePorts(scope specSizeScope, roadmap map[string][]string) ([]string, error) {
	set := map[string]bool{}
	for _, feature := range scope.Features {
		if feature == specCompilerPort {
			set[specCompilerPort] = true
			continue
		}
		matched := false
		for name, ports := range roadmap {
			if name == feature || strings.HasSuffix(feature, "/*") && strings.HasPrefix(name, strings.TrimSuffix(feature, "*")) {
				matched = true
				for _, port := range ports {
					set[port] = true
				}
			}
		}
		if !matched {
			return nil, fmt.Errorf("sizing feature %q is not in the roadmap", feature)
		}
	}
	if len(scope.OnlyPorts) != 0 {
		limited := map[string]bool{}
		for _, port := range scope.OnlyPorts {
			if !set[port] {
				return nil, fmt.Errorf("sizing port %q is not carried by %v", port, scope.Features)
			}
			limited[port] = true
		}
		set = limited
	}
	var ports []string
	for port := range set {
		ports = append(ports, port)
	}
	sort.Strings(ports)
	return ports, nil
}

// specSizingRows includes every heading, including zero-count headings and
// the informative design section. Numbered descendants inherit their nearest
// numbered scope; unnumbered subheadings inherit their actual heading parent.
// New top-level sections with unmarked rules need an explicit scope: they
// cannot silently inherit an unrelated previous section or a global default.
func specSizingRows(doc specDoc, roadmap map[string][]string, scopes map[string]specSizeScope) ([]specSizeRow, error) {
	var out []specSizeRow
	type parentScope struct {
		level int
		key   string
	}
	var parents []parentScope
	for i, sec := range doc.Sections {
		for len(parents) > 0 && parents[len(parents)-1].level >= sec.Level {
			parents = parents[:len(parents)-1]
		}
		key := sec.Number
		if key == "" {
			key = sec.Title
			if _, ok := scopes[key]; !ok && sec.Level > 2 && len(parents) > 0 {
				key = parents[len(parents)-1].key
			}
		} else {
			for {
				if _, ok := scopes[key]; ok {
					break
				}
				at := strings.LastIndex(key, ".")
				if at < 0 {
					break
				}
				key = key[:at]
			}
		}
		parents = append(parents, parentScope{sec.Level, key})
		row := specSizeRow{Section: sec, Basis: "row features"}
		if sec.Informative != "" {
			row.Basis = "informative; no port obligation"
			out = append(out, row)
			continue
		}
		set := map[string]bool{}
		for _, rule := range doc.Rows {
			if rule.Section != i || rule.Informative != "" {
				continue
			}
			ps, err := sizingScopePorts(specSizeScope{Features: rule.Features}, roadmap)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", sec.Title, err)
			}
			for _, port := range ps {
				set[port] = true
			}
		}
		if sec.Unmarked > 0 {
			scope, ok := scopes[key]
			if !ok {
				return nil, fmt.Errorf("section %q has %d unmapped sentences and no sizing scope", sec.Title, sec.Unmarked)
			}
			ps, err := sizingScopePorts(scope, roadmap)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", sec.Title, err)
			}
			if len(ps) == 0 {
				return nil, fmt.Errorf("section %q has an empty sizing port set", sec.Title)
			}
			for _, port := range ps {
				set[port] = true
			}
			row.Basis = "section scope " + key + "; row mapping pending"
		} else if sec.Normative == 0 && len(set) == 0 {
			row.Basis = "no normative matches"
		}
		for port := range set {
			row.Ports = append(row.Ports, port)
		}
		sort.Strings(row.Ports)
		out = append(out, row)
	}
	return out, nil
}

func specSizingTable(rows []specSizeRow) string {
	var b strings.Builder
	b.WriteString("| Spec line | Spec section | Normative sentences | Ports carrying the section's features | Mapping basis | Unmapped normative sentences |\n")
	b.WriteString("| ---: | --- | ---: | --- | --- | ---: |\n")
	for _, row := range rows {
		ports := strings.Join(row.Ports, ", ")
		if ports == "" {
			ports = "none"
		}
		unmapped := row.Section.Unmarked
		if row.Section.Informative != "" {
			unmapped = 0
		}
		fmt.Fprintf(&b, "| %d | %s | %d | %s | %s | %d |\n", row.Section.Line, strings.ReplaceAll(row.Section.Title, "|", `\|`), row.Section.Normative, ports, row.Basis, unmapped)
	}
	return b.String()
}

// TestSpecPinsSectionSizes emits the report's complete section/count/ports
// table. Counts use the guard's splitter, and ports use roadmap cells, not
// hard-coded language names or a claim that every cell is implemented.
func TestSpecPinsSectionSizes(t *testing.T) {
	t.Parallel()
	fsys := os.DirFS(repoRoot(t))
	doc, findings := checkSpecPins(specPinsInput{fsys: fsys, spec: "docs/SPEC-TABLES.md", roadmap: "docs/roadmap.sexp", pins: specPinRegistry})
	if len(findings) != 0 {
		t.Fatalf("cannot size an invalid spec/pin ledger: %s", strings.Join(findings, "\n"))
	}
	text, err := fs.ReadFile(fsys, "docs/roadmap.sexp")
	if err != nil {
		t.Fatal(err)
	}
	ports, err := roadmapPorts(string(text))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := specSizingRows(doc, ports, specSizeScopes)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + specSizingTable(rows))
}

func TestSpecPinsSizingUsesRoadmapPorts(t *testing.T) {
	t.Parallel()
	fx := specFixture()
	doc, findings := checkSpecPins(specPinsInput{fsys: fx, spec: "docs/SPEC.md", roadmap: "docs/roadmap.sexp", pins: specFixturePins()})
	if len(findings) != 0 {
		t.Fatal(findings)
	}
	rm, err := roadmapPorts(string(fx["docs/roadmap.sexp"].Data))
	if err != nil {
		t.Fatal(err)
	}
	scopes := map[string]specSizeScope{"2": {Features: []string{"vt/*"}}}
	rows, err := specSizingRows(doc, rm, scopes)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(doc.Sections) {
		t.Fatalf("lost headings: got %d, want %d", len(rows), len(doc.Sections))
	}
	table := specSizingTable(rows)
	for _, want := range []string{
		"| Spec section | Normative sentences | Ports carrying",
		"| 1. Pinned | 4 | compiler, go, rust | row features | 0 |",
		"| 2. Owed | 1 | go, rust | section scope 2; row mapping pending | 1 |",
	} {
		if !strings.Contains(table, want) {
			t.Fatalf("sizing table lacks %q:\n%s", want, table)
		}
	}
	// A new carrying port changes the planning union without editing a
	// language list. This is input data, not an assertion that its pins exist.
	rm["vt/blocks"] = append(rm["vt/blocks"], "newport")
	rows, err = specSizingRows(doc, rm, scopes)
	if err != nil || !strings.Contains(specSizingTable(rows), "| 2. Owed | 1 | go, newport, rust |") {
		t.Fatalf("new roadmap port missing: %v\n%s", err, specSizingTable(rows))
	}
	for _, scope := range []specSizeScope{
		{Features: []string{"vt/missing"}},
		{Features: []string{"missing/*"}},
		{Features: []string{"vt/blocks"}, OnlyPorts: []string{"absent"}},
	} {
		if _, err := sizingScopePorts(scope, rm); err == nil {
			t.Fatalf("invalid sizing scope accepted: %+v", scope)
		}
	}
	delete(scopes, "2")
	if _, err := specSizingRows(doc, rm, scopes); err == nil {
		t.Fatal("an unmarked section without a scope was silently sized")
	}
}

func TestSpecPinsSizingInheritsHeadingScope(t *testing.T) {
	t.Parallel()
	doc := specDoc{Sections: []pinSection{
		{Level: 2, Title: "3. Wire", Number: "3"},
		{Level: 3, Title: "3.4 Fixed", Number: "3.4", Normative: 1, Unmarked: 1},
		{Level: 4, Title: "Framing", Normative: 2, Unmarked: 2},
		{Level: 3, Title: "3.5 Other", Number: "3.5", Normative: 1, Unmarked: 1},
		{Level: 2, Title: "4. Compiler", Number: "4", Normative: 1, Unmarked: 1},
	}}
	scopes := map[string]specSizeScope{
		"3":   {Features: []string{"compiler"}},
		"3.4": {Features: []string{"vt/blocks"}},
		"4":   {Features: []string{"compiler"}},
	}
	rows, err := specSizingRows(doc, map[string][]string{"vt/blocks": {"rust", "go"}}, scopes)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"", "go,rust", "go,rust", "compiler", "compiler"} {
		if got := strings.Join(rows[i].Ports, ","); got != want {
			t.Fatalf("section %q: ports %q, want %q", rows[i].Section.Title, got, want)
		}
	}
	// An unnumbered sibling of a numbered section cannot inherit that
	// section's scope merely because it preceded it in document order.
	doc.Sections = append(doc.Sections, pinSection{Level: 2, Title: "New subject", Normative: 1, Unmarked: 1})
	if _, err := specSizingRows(doc, map[string][]string{"vt/blocks": {"go", "rust"}}, scopes); err == nil {
		t.Fatal("unmapped top-level sibling inherited an unrelated scope")
	}
}

// ------------------------------------------------- the reversed witnesses

// specFixture is a small repository the guard passes on; each witness below
// breaks one thing in a copy and names the finding that must fire.
func specFixture() fstest.MapFS {
	return fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/fx\n\ngo 1.22\n")},
		"docs/roadmap.sexp": {Data: []byte(`; fixture
(:schema 1 :nodes ((:id "vt" :type :roadmap :rows (("blocks" "b")) :columns (("go" "go") ("rust" "rust"))
  :cells (("blocks" "go" "t1") ("blocks" "rust" "t2")))))`)},
		"docs/SPEC.md": {Data: []byte(`# fixture

## 1. Pinned

A reader MUST refuse a short buffer.<!-- row T1-1 feature=vt/blocks --> Some prose that states nothing.
The compiler refuses a reserved name.<!-- row T1-2 feature=compiler -->

| a | table row that must hold <!-- row T1-3 feature=compiler --> |

See §2 for what it refuses.<!-- row T1-4 informative="points elsewhere" -->

<!-- row-retired T1-9 replaced by T1-1 -->

## 2. Owed
<!-- spec-pins owed schema-spec-pin-s2 -->

A writer must zero its slack.

` + "```" + `
code that must not count
` + "```" + `
`)},
		"pkg/a_test.go": {Data: []byte(`package pkg

import (
	"testing"

	"example.com/fx/helper"
)

func TestShort(t *testing.T) { check(t) }

func TestCompiler(t *testing.T) { helper.Run(t) }

func TestRow3(t *testing.T) {}

func TestSkips(t *testing.T) { t.Skip("later") }

func TestHelperSkips(t *testing.T) { helper.Gate(t) }

func TestShortGated(t *testing.T) {
	if testing.Short() {
		return
	}
}

func check(t *testing.T) { t.Helper() }

func Testify(t *testing.T) {}

func TestNoT() {}

func TestMethodSkips(t *testing.T) { helper.G{}.Check(t) }
`)},
		"pkg/tagged_test.go": {Data: []byte("//go:build slow\n\npackage pkg\n\nimport \"testing\"\n\nfunc TestTagged(t *testing.T) {}\n")},
		"helper/helper.go": {Data: []byte(`package helper

import "testing"

func Run(t *testing.T) { t.Helper() }

func Gate(t *testing.T) { inner(t) }

func inner(t *testing.T) { t.SkipNow() }

type G struct{}

func (G) Check(t *testing.T) { t.Skip("method") }
`)},
		"rs/main.rs":             {Data: []byte("fn main() { surface_block(); surface_slow(); }\n\nfn surface_block() {}\n\n#[ignore]\nfn surface_ignored() {}\n\n#[ignore = \"slow\"]\nfn surface_slow() {}\n\nfn never_called() {}\n")},
		"c/h.c":                  {Data: []byte("int proto_only(void);\n// proto_only(); is only a comment\nint proto_only(void) { return 0; }\n")},
		"pkg/testdata/x_test.go": {Data: []byte("package x\n\nimport \"testing\"\n\nfunc TestHidden(t *testing.T) {}\n")},
		"nested/go.mod":          {Data: []byte("module example.com/nested\n")},
		"nested/n_test.go":       {Data: []byte("package nested\n\nimport \"testing\"\n\nfunc TestNested(t *testing.T) {}\n")},
		"tla/Pins.tla":           {Data: []byte("---- MODULE Pins ----\nInv == TRUE\nLive == TRUE\n====\n")},
		"tla/MCPins.tla":         {Data: []byte("---- MODULE MCPins ----\nEXTENDS Pins, Naturals\n====\n")},
		"tla/MCLone.tla":         {Data: []byte("---- MODULE MCLone ----\nEXTENDS Naturals\nInv2 == TRUE\n====\n")},
		"tla/MCPins.cfg":         {Data: []byte("SPECIFICATION Spec\nINVARIANT Inv\nPROPERTIES\n  Live\n")},
		"tla/Bare.cfg":           {Data: []byte("SPECIFICATION Spec\nCONSTANT Inv = 1\n")},
		"Makefile":               {Data: []byte("tlc:\n\ttlc -config tla/MCPins.cfg tla/MCPins.tla\n\ttlc -config tla/Bare.cfg tla/MCPins.tla\n")},
	}
}

// The fixture rows' digests, as specRowDigest reads their sentences.
const (
	specFixtureT11 = "6489d1bb5addd176"
	specFixtureT13 = "2e22501fdc68281a"
)

func specFixturePins() []specPin {
	return []specPin{
		{Row: "T1-1", Port: "go", Test: "pkg/a_test.go:TestShort", Digest: specFixtureT11},
		{Row: "T1-1", Port: "rust", Test: "rs/main.rs:surface_block", Digest: specFixtureT11},
		{Row: "T1-2", Port: "compiler", Test: "pkg/a_test.go:TestCompiler", Digest: "0a00fe170c1b2c5d"},
		{Row: "T1-3", Port: "compiler", Test: "tla/MCPins.tla:Inv", Cfg: "tla/MCPins.cfg", Module: "Pins", Digest: specFixtureT13},
		// a second pin of the same row/port: the multi-line PROPERTIES form
		{Row: "T1-3", Port: "compiler", Test: "tla/MCPins.tla:Live", Cfg: "tla/MCPins.cfg", Module: "Pins", Digest: specFixtureT13},
	}
}

func runSpecFixture(fsys fstest.MapFS, pins []specPin) []string {
	_, f := checkSpecPins(specPinsInput{fsys: fsys, spec: "docs/SPEC.md", roadmap: "docs/roadmap.sexp", pins: pins})
	return f
}

// TestSpecPinsFixturePasses is the positive control: the fixture is clean,
// so every witness below is red for its own plant and nothing else.
func TestSpecPinsFixturePasses(t *testing.T) {
	t.Parallel()
	if f := runSpecFixture(specFixture(), specFixturePins()); len(f) != 0 {
		t.Fatalf("the clean fixture has findings:\n%s", strings.Join(f, "\n"))
	}
}

// TestSpecPinsTLALineCommentHidesABlockOpen: a `(*` behind a `\*` line
// comment opens nothing, so the definition after it is live. A stripper that
// read only block comments would hide Inv and refuse the clean fixture.
func TestSpecPinsTLALineCommentHidesABlockOpen(t *testing.T) {
	t.Parallel()
	fx := specFixture()
	fx["tla/Pins.tla"] = &fstest.MapFile{Data: []byte("---- MODULE Pins ----\n\\* (* not a block\nInv == TRUE\nLive == TRUE\n====\n")}
	if f := runSpecFixture(fx, specFixturePins()); len(f) != 0 {
		t.Fatalf("a `(*` behind a line comment hid a live definition:\n%s", strings.Join(f, "\n"))
	}
}

// TestSpecPinsRefusals plants one defect per case and requires the guard to
// name it.
func TestSpecPinsRefusals(t *testing.T) {
	t.Parallel()
	edit := func(fx fstest.MapFS, file, old, new string) {
		s := string(fx[file].Data)
		if !strings.Contains(s, old) {
			panic("fixture edit misses: " + old)
		}
		fx[file] = &fstest.MapFile{Data: []byte(strings.Replace(s, old, new, 1))}
	}
	setPin := func(pins []specPin, i int, p specPin) []specPin {
		pins[i] = p
		return pins
	}
	cases := []struct {
		name  string
		plant func(fstest.MapFS, []specPin) []specPin
		want  string
	}{
		{"unpinned normative sentence", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "docs/SPEC.md", "Some prose that states nothing.", "Some prose that must hold.")
			return p
		}, "normative sentence without a row id"},
		{"normative table row without an id", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "docs/SPEC.md", "| a | table row that must hold <!-- row T1-3 feature=compiler --> |", "| a | table row that must hold <!-- row T1-3 feature=compiler --> |\n| b | another that must hold |")
			return p
		}, "normative sentence without a row id"},
		{"dead pin: no such row", func(fx fstest.MapFS, p []specPin) []specPin {
			return append(p, specPin{Row: "T1-7", Port: "go", Test: "pkg/a_test.go:TestShort"})
		}, "dead pin, no such row"},
		{"dead pin: retired row", func(fx fstest.MapFS, p []specPin) []specPin {
			return append(p, specPin{Row: "T1-9", Port: "go", Test: "pkg/a_test.go:TestShort"})
		}, "dead pin, the row is retired"},
		{"dead pin: no such test", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 0, specPin{Row: "T1-1", Port: "go", Test: "pkg/a_test.go:TestGone"})
		}, "defines no func TestGone"},
		{"dead pin: no such file", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 0, specPin{Row: "T1-1", Port: "go", Test: "pkg/gone_test.go:TestShort"})
		}, "does not exist"},
		{"row without a port pin", func(fx fstest.MapFS, p []specPin) []specPin {
			return p[:1:1] // drops rust, T1-2 and T1-3
		}, "row T1-1 has no pin and no owed entry in port rust"},
		{"pin on a port the feature does not carry", func(fx fstest.MapFS, p []specPin) []specPin {
			return append(p, specPin{Row: "T1-2", Port: "rust", Test: "rs/main.rs:surface_block"})
		}, `port "rust" does not carry`},
		{"t.Skip in the test", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 0, specPin{Row: "T1-1", Port: "go", Test: "pkg/a_test.go:TestSkips"})
		}, "calls Skip"},
		{"skip inside a helper in another package", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 0, specPin{Row: "T1-1", Port: "go", Test: "pkg/a_test.go:TestHelperSkips"})
		}, "TestHelperSkips -> helper.Gate -> inner calls SkipNow"},
		{"testing.Short gate", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 0, specPin{Row: "T1-1", Port: "go", Test: "pkg/a_test.go:TestShortGated"})
		}, "reads testing.Short"},
		{"build-tagged-out test", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 0, specPin{Row: "T1-1", Port: "go", Test: "pkg/tagged_test.go:TestTagged"})
		}, "build-tagged out"},
		{"skipped harness function", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 1, specPin{Row: "T1-1", Port: "rust", Test: "rs/main.rs:surface_ignored"})
		}, "marked skipped or ignored"},
		{"harness function nothing calls", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 1, specPin{Row: "T1-1", Port: "rust", Test: "rs/main.rs:never_called"})
		}, "nothing in rs calls never_called"},
		{"tla model does not EXTENDS the module", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 3, specPin{Row: "T1-3", Port: "compiler", Test: "tla/MCLone.tla:Inv", Cfg: "tla/MCPins.cfg", Module: "Pins"})
		}, "does not EXTENDS Pins"},
		{"tla property not under INVARIANTS or PROPERTIES", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 3, specPin{Row: "T1-3", Port: "compiler", Test: "tla/MCPins.tla:Inv", Cfg: "tla/Bare.cfg", Module: "Pins"})
		}, "is not under INVARIANT(S) or PROPERTY(IES)"},
		{"tla cfg nothing runs", func(fx fstest.MapFS, p []specPin) []specPin {
			fx["tla/Orphan.cfg"] = &fstest.MapFile{Data: []byte("INVARIANT Inv\n")}
			return setPin(p, 3, specPin{Row: "T1-3", Port: "compiler", Test: "tla/MCPins.tla:Inv", Cfg: "tla/Orphan.cfg", Module: "Pins"})
		}, "nothing runs TLC on it"},
		{"owed test that already exists, found repo-wide", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 1, specPin{Row: "T1-1", Port: "rust", Owed: "rust port lands it", Want: "TestRow3"})
		}, "owed test TestRow3 already exists at pkg/a_test.go"},
		{"owed entry with no Want", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 1, specPin{Row: "T1-1", Port: "rust", Owed: "later"})
		}, "names no Want"},
		{"pinned and owed at once", func(fx fstest.MapFS, p []specPin) []specPin {
			return append(p, specPin{Row: "T1-1", Port: "go", Owed: "dup", Want: "TestNotYet"})
		}, "already pin for this row"},
		{"stale owed section", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "docs/SPEC.md", "A writer must zero its slack.", "A writer zeroes its slack.")
			return p
		}, "is marked owed by schema-spec-pin-s2 but every normative sentence has a row id"},
		{"retired id reused", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "docs/SPEC.md", "<!-- row T1-1 feature=vt/blocks -->", "<!-- row T1-9 feature=vt/blocks -->")
			return p
		}, "is retired and marked again"},
		{"duplicate id", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "docs/SPEC.md", "<!-- row T1-2 feature=compiler -->", "<!-- row T1-1 feature=compiler -->")
			return p
		}, "is also marked at line"},
		{"row with no feature", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "docs/SPEC.md", "<!-- row T1-2 feature=compiler -->", "<!-- row T1-2 -->")
			return p
		}, "names no feature="},
		{"feature not on the roadmap", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "docs/SPEC.md", "<!-- row T1-1 feature=vt/blocks -->", "<!-- row T1-1 feature=vt/nothing -->")
			return p
		}, "which is no row of a roadmap"},
		{"malformed id", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "docs/SPEC.md", "<!-- row T1-2 feature=compiler -->", "<!-- row t12 feature=compiler -->")
			return p
		}, "is not T<section>-<n>"},
		{"normative words wrapped across a line", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "docs/SPEC.md", "Some prose that states nothing.", "Some prose that may\nnot hold.")
			return p
		}, "normative sentence without a row id"},
		{"a row marker ends its sentence", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "docs/SPEC.md", "<!-- row T1-2 feature=compiler -->", "<!-- row T1-2 feature=compiler --> schema lock refuses a stale lock.")
			return p
		}, "schema lock refuses a stale lock"},
		{"tla property with no definition", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 3, specPin{Row: "T1-3", Port: "compiler", Test: "tla/MCPins.tla:Nope", Cfg: "tla/MCPins.cfg", Module: "Pins"})
		}, "dead pin, no Nope =="},
		{"tla cfg that does not exist", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 3, specPin{Row: "T1-3", Port: "compiler", Test: "tla/MCPins.tla:Inv", Cfg: "tla/Gone.cfg", Module: "Pins"})
		}, "cfg tla/Gone.cfg does not exist"},
		{"tla pin with no cfg", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 3, specPin{Row: "T1-3", Port: "compiler", Test: "tla/MCPins.tla:Inv", Module: "Pins"})
		}, "names no Cfg"},
		{"tla property listed only inside a block comment", func(fx fstest.MapFS, p []specPin) []specPin {
			fx["tla/Commented.cfg"] = &fstest.MapFile{Data: []byte("SPECIFICATION Spec\n(* INVARIANT Inv *)\n")}
			edit(fx, "Makefile", "tlc:\n", "tlc:\n\ttlc -config tla/Commented.cfg tla/MCPins.tla\n")
			return setPin(p, 3, specPin{Row: "T1-3", Port: "compiler", Test: "tla/MCPins.tla:Inv", Cfg: "tla/Commented.cfg", Module: "Pins"})
		}, "is not under INVARIANT(S) or PROPERTY(IES)"},
		{"pin on an informative row", func(fx fstest.MapFS, p []specPin) []specPin {
			return append(p, specPin{Row: "T1-4", Port: "compiler", Test: "pkg/a_test.go:TestRow3"})
		}, "informative and takes no pin"},
		{"row both informative and featured", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "docs/SPEC.md", `<!-- row T1-4 informative="points elsewhere" -->`, `<!-- row T1-4 feature=compiler informative="points elsewhere" -->`)
			return p
		}, "both informative and pinned"},
		{"unknown row attribute", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "docs/SPEC.md", "<!-- row T1-2 feature=compiler -->", "<!-- row T1-2 feature=compiler colour=red -->")
			return p
		}, "unknown attribute"},
		{"two section markers", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "docs/SPEC.md", "<!-- spec-pins owed schema-spec-pin-s2 -->", "<!-- spec-pins owed schema-spec-pin-s2 -->\n<!-- spec-pins owed schema-spec-pin-s3 -->")
			return p
		}, "carries two spec-pins markers"},
		{"informative section not on the guard's list", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "docs/SPEC.md", "<!-- spec-pins owed schema-spec-pin-s2 -->", "<!-- spec-pins informative it is only prose -->")
			return p
		}, "not in the guard's specInformativeSections"},
		{"marker before the first heading", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "docs/SPEC.md", "# fixture\n", "<!-- spec-pins owed schema-spec-pin-s0 -->\n# fixture\n")
			return p
		}, "before the first heading"},
		{"not a Test function", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 0, specPin{Row: "T1-1", Port: "go", Test: "pkg/a_test.go:Testify"})
		}, "is not a Test function"},
		{"a Test without *testing.T", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 0, specPin{Row: "T1-1", Port: "go", Test: "pkg/a_test.go:TestNoT"})
		}, "does not take one *testing.T"},
		{"a non-test .go file", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 0, specPin{Row: "T1-1", Port: "go", Test: "helper/helper.go:Run"})
		}, "is not a _test.go file"},
		{"a test under testdata", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 0, specPin{Row: "T1-1", Port: "go", Test: "pkg/testdata/x_test.go:TestHidden"})
		}, "never visits"},
		{"a test in a nested module", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 0, specPin{Row: "T1-1", Port: "go", Test: "nested/n_test.go:TestNested"})
		}, "is in the nested module nested"},
		{"a TestMain that never runs the tests", func(fx fstest.MapFS, p []specPin) []specPin {
			fx["pkg/main_test.go"] = &fstest.MapFile{Data: []byte("package pkg\n\nimport \"testing\"\n\nfunc TestMain(m *testing.M) {}\n")}
			return p
		}, "TestMain never calls m.Run"},
		{"skip in a method of another package's type", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 0, specPin{Row: "T1-1", Port: "go", Test: "pkg/a_test.go:TestMethodSkips"})
		}, "TestMethodSkips -> Check calls Skip"},
		{"rust ignore with a reason", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 1, specPin{Row: "T1-1", Port: "rust", Test: "rs/main.rs:surface_slow"})
		}, "marked skipped or ignored"},
		{"a prototype and a comment are not calls", func(fx fstest.MapFS, p []specPin) []specPin {
			fx["docs/roadmap.sexp"] = &fstest.MapFile{Data: []byte(strings.Replace(string(fx["docs/roadmap.sexp"].Data), `("rust" "rust"))`, `("rust" "rust") ("c" "c"))`, 1))}
			edit(fx, "docs/roadmap.sexp", `("blocks" "rust" "t2")`, `("blocks" "rust" "t2") ("blocks" "c" "t3")`)
			return append(p, specPin{Row: "T1-1", Port: "c", Test: "c/h.c:proto_only"})
		}, "nothing in c calls proto_only"},
		{"owed entry that names a test", func(fx fstest.MapFS, p []specPin) []specPin {
			return setPin(p, 1, specPin{Row: "T1-1", Port: "rust", Test: "rs/main.rs:surface_block", Owed: "x", Want: "surface_later"})
		}, "an owed entry names a test"},
		// a TLA+ property or EXTENDS that exists only inside a comment does
		// not exist for TLC (reader finding, r1: the resolver read raw text)
		{"tla definition only inside a block comment", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "tla/Pins.tla", "Inv == TRUE\n", "(*\nInv == TRUE\n*)\n")
			return p
		}, "dead pin, no Inv =="},
		{"tla definition only inside a nested block comment", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "tla/Pins.tla", "Inv == TRUE\n", "(* outer (* inner *)\nInv == TRUE\n*)\n")
			return p
		}, "dead pin, no Inv =="},
		{"tla definition only after a line comment", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "tla/Pins.tla", "Inv == TRUE\n", "\\* Inv == TRUE\n")
			return p
		}, "dead pin, no Inv =="},
		{"tla definition behind a line comment inside a block comment", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "tla/Pins.tla", "Inv == TRUE\n", "(* a \\* b *)\nInv == TRUE\n*)\n")
			return p
		}, "dead pin, no Inv =="},
		{"tla EXTENDS only inside a comment", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "tla/MCPins.tla", "EXTENDS Pins, Naturals\n", "(*\nEXTENDS Pins, Naturals\n*)\nEXTENDS Naturals\n")
			return p
		}, "does not EXTENDS Pins"},
		{"tla property listed only inside a nested cfg comment", func(fx fstest.MapFS, p []specPin) []specPin {
			fx["tla/Nested.cfg"] = &fstest.MapFile{Data: []byte("SPECIFICATION Spec\n(* a (* b *)\nINVARIANT Inv\n*)\n")}
			edit(fx, "Makefile", "tlc:\n", "tlc:\n\ttlc -config tla/Nested.cfg tla/MCPins.tla\n")
			return setPin(p, 3, specPin{Row: "T1-3", Port: "compiler", Test: "tla/MCPins.tla:Inv", Cfg: "tla/Nested.cfg", Module: "Pins"})
		}, "is not under INVARIANT(S) or PROPERTY(IES)"},
		{"owed marker naming no card", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "docs/SPEC.md", "spec-pins owed schema-spec-pin-s2", "spec-pins owed someday")
			return p
		}, "not a schema-spec-pin-* card"},
		{"owed marker naming a card off the closed list", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "docs/SPEC.md", "spec-pins owed schema-spec-pin-s2", "spec-pins owed schema-spec-pin-s27")
			return p
		}, `names "schema-spec-pin-s27", which is no card on the guard's specOwedCards list`},
		{"owed marker naming a card whose pins have landed", func(fx fstest.MapFS, p []specPin) []specPin {
			for i := range p {
				p[i].origin = "internal/ci/schema_spec_pin_s2_test.go"
			}
			return p
		}, "is marked owed by schema-spec-pin-s2, whose pins have landed in internal/ci/schema_spec_pin_s2_test.go"},
		{"pin with no digest", func(fx fstest.MapFS, p []specPin) []specPin {
			p[2].Digest = ""
			return p
		}, "pin T1-2/compiler (): the pin records no Digest of its row's text"},
		{"pinned row reworded with its marker kept", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "docs/SPEC.md", "The compiler refuses a reserved name.", "The compiler accepts a reserved name and must not refuse it.")
			return p
		}, "row T1-2 text changed since its pin; re-read the test and update the digest"},
		{"pinned row code span renamed", func(fx fstest.MapFS, p []specPin) []specPin {
			edit(fx, "docs/SPEC.md", "A reader MUST refuse a short buffer.", "A reader MUST refuse a short `buffer`.")
			return p
		}, "row T1-1 text changed since its pin"},
		{"owed entry under a reworded row", func(fx fstest.MapFS, p []specPin) []specPin {
			p = setPin(p, 1, specPin{Row: "T1-1", Port: "rust", Owed: "rust lands it", Want: "surface_later", Digest: specFixtureT11})
			edit(fx, "docs/SPEC.md", "A reader MUST refuse a short buffer.", "A reader MAY accept a short buffer and must not refuse it.")
			return p
		}, "pin T1-1/rust (): row T1-1 text changed since its pin"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			fx := specFixture()
			pins := c.plant(fx, specFixturePins())
			got := runSpecFixture(fx, pins)
			for _, f := range got {
				if strings.Contains(f, c.want) {
					return
				}
			}
			t.Fatalf("planted %q; want a finding containing %q, got:\n%s", c.name, c.want, strings.Join(got, "\n"))
		})
	}
}
