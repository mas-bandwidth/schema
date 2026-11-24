package fixedvector

// EVERY TEST IS A SHARED CORPUS (docs/rulings.json every-test-is-a-shared-corpus,
// 2026-11-24): the fixed form's cases are the C++ reference's corpus, defined
// once in build/fixedform-corpus/manifest.txt, and a leg answers every root
// that manifest names or the harness refuses it. A leg that walks a
// hand-picked list of files answers the files it picked, and a root the
// reference grows tomorrow is never checked on that leg.
//
// EveryRoot is the harness half, and it judges each answer itself:
//
//   - a root whose line carries refuse=<reason> (the reference's own reader
//     refused the file whole) must be refused by that name;
//   - every other root must land the line's records= count and every value
//     the reference wrote (values=, with the forged cell and the reader's
//     expect= laid over it), compared path by path; a value the leg did not
//     land is red by its path.
//
// When the leg's test ends, the answered set must equal the manifest's set,
// by name, both ways, and a complete walk leaves a RECORD of the run (Main
// reads it). internal/ci's TestEveryLegAnswersEveryFixedCorpusRoot holds each
// leg to running through Main.

import (
	"bufio"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// Value is one landed value of a corpus file: the manifest's path (`r0.v`,
// `r1.items[2].a`) and its text in the manifest's spelling.
type Value struct {
	Path, Text string
}

// ManifestRow is one `file=` line of the corpus manifest: the file, the
// versioning row and side the dump wrote it for, its fixed root, its record
// count, the refusal the reference's reader named (Refuse, "" for a file it
// reads), and what a read of it lands (Landed).
type ManifestRow struct {
	File, Row, Side, Root string
	Records               int
	Refuse                string
	// Landed is the values a conforming read lands, in the manifest's order:
	// `values=` (what the writer stored), with `expect=` (what the
	// reference's reader landed, where the line says so) laid over it, or,
	// on a forged line with no expect=, the `forged=` cell laid over the
	// stored value it reached.
	Landed []Value
}

// Manifest reads every `file=` line of corpus/manifest.txt, in the manifest's
// order. A corpus with no manifest, or a manifest naming no file, is an error
// by name: a leg reading it would otherwise answer nothing and pass.
func Manifest(corpus string) ([]ManifestRow, error) {
	path := filepath.Join(corpus, "manifest.txt")
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("fixedvector: the reference's corpus manifest %s is not readable (make tables-fixedform-corpus builds it): %w", path, err)
	}
	defer func() { _ = f.Close() }() // read-only: Scanner reports read errors
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	var out []ManifestRow
	seen := map[string]bool{}
	for sc.Scan() {
		head, values, hasValues := strings.Cut(sc.Text(), " values=")
		var r ManifestRow
		records, forged, expect := "", "", ""
		for field := range strings.FieldsSeq(head) {
			k, v, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch k {
			case "file":
				r.File = v
			case "row":
				r.Row = v
			case "side":
				r.Side = v
			case "root":
				r.Root = v
			case "records":
				records = v
			case "refuse":
				r.Refuse = v
			case "forged":
				forged = v
			case "expect":
				expect = v
			}
		}
		if r.File == "" {
			continue
		}
		n, err := strconv.Atoi(records)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("fixedvector: the manifest line for %s carries no records= count (%q)", r.File, records)
		}
		r.Records = n
		if !hasValues {
			return nil, fmt.Errorf("fixedvector: the manifest line for %s carries no values=", r.File)
		}
		if r.Landed, err = landed(r.File, values, forged, expect); err != nil {
			return nil, err
		}
		if seen[r.File] {
			return nil, fmt.Errorf("fixedvector: the manifest names %s twice", r.File)
		}
		seen[r.File] = true
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("fixedvector: the corpus manifest %s names no file", path)
	}
	return out, nil
}

// landed lays forged= and expect= over values=.
func landed(file, values, forged, expect string) ([]Value, error) {
	var out []Value
	at := map[string]int{}
	if values != "" {
		pairs, err := splitPairs(values)
		if err != nil {
			return nil, fmt.Errorf("fixedvector: %s's values= %w", file, err)
		}
		for _, pair := range pairs {
			k, v, ok := strings.Cut(pair, "=")
			if !ok || k == "" {
				return nil, fmt.Errorf("fixedvector: %s's values= has a pair with no value: %q", file, pair)
			}
			if _, dup := at[k]; dup {
				return nil, fmt.Errorf("fixedvector: %s's values= names %s twice", file, k)
			}
			at[k] = len(out)
			out = append(out, Value{Path: k, Text: v})
		}
	}
	if forged != "" && expect == "" {
		path, rest, _ := strings.Cut(forged, "@")
		_, text, ok := strings.Cut(rest, "=")
		if !ok {
			return nil, fmt.Errorf("fixedvector: %s's forged= carries no value: %q", file, forged)
		}
		// a forged file whose line states no expect= lands the forged cell as
		// the value it holds (hostile_cfloat_range_widen: aim forged to 1.5).
		// Where the line states expect=, that is what the reader landed and
		// the forge is not laid over: a dead-byte control forges one BYTE of
		// a span (r0.text@620=0), which is no value of the field.
		if i, stored := at[path]; stored {
			out[i].Text = text
		}
	}
	if expect != "" {
		pairs, err := splitPairs(expect)
		if err != nil {
			return nil, fmt.Errorf("fixedvector: %s's expect= %w", file, err)
		}
		for _, pair := range pairs {
			k, v, ok := strings.Cut(pair, "=")
			if !ok {
				return nil, fmt.Errorf("fixedvector: %s's expect= has a pair with no value: %q", file, pair)
			}
			if k == "clamped" {
				continue
			}
			i, stored := at[k]
			if !stored {
				at[k] = len(out)
				out = append(out, Value{Path: k, Text: v})
				continue
			}
			// expect= spells a landed float by its integer value (negative_zero's
			// `r0.value=0`): keep the stored spelling's width, digits and bits
			if bits := floatBits(out[i].Text); bits != "" && !strings.Contains(v, "|") {
				n, err := strconv.ParseInt(v, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("fixedvector: %s's expect= %s is not an integer: %w", file, k, err)
				}
				if len(bits) == 8 {
					v = fmt.Sprintf("%d|0x%08X", n, math.Float32bits(float32(n)))
				} else {
					v = fmt.Sprintf("%d|0x%016X", n, math.Float64bits(float64(n)))
				}
			}
			out[i].Text = v
		}
	}
	return out, nil
}

// floatBits is the hex digits of a manifest float (`0.5|0x3F000000`), "" for
// any other spelling.
func floatBits(text string) string {
	_, bits, ok := strings.Cut(text, "|0x")
	if !ok || (len(bits) != 8 && len(bits) != 16) {
		return ""
	}
	if _, err := strconv.ParseUint(bits, 16, 64); err != nil {
		return ""
	}
	return bits
}

// Text spells raw text the way the manifest quotes it (the dump's man_quote):
// `"`, `\` and `,` escaped, a byte outside printable ASCII as \uXXXX. A leg
// hands a landed text value through it.
func Text(raw string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c == '"' || c == '\\' || c == ',':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c >= 0x20 && c < 0x7F:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "\\u%04X", c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// Same reports whether a leg's landed text is the manifest's value. The leg
// hands each value in the manifest's spelling, and three spellings the
// manifest writes twice are matched on the part that is the value:
//
//   - a float, `<digits>|0x<bits>`, by its BITS: the leg's `0x<bits>` at the
//     same width, or a float32's bits widened exactly to float64's
//     (docs/SPEC-TABLES.md §3.3, §4);
//   - a fixed(I,F), `<real>|raw=<int>`, by its RAW scaled integer;
//   - text, by the bytes it quotes (Text spells the leg's side).
//
// Anything else is the same text or it is not the same value.
func Same(want, got string) bool {
	if want == got {
		return true
	}
	if bits := floatBits(want); bits != "" {
		g, ok := strings.CutPrefix(got, "0x")
		if !ok {
			return false
		}
		gv, err := strconv.ParseUint(g, 16, 64)
		if err != nil {
			return false
		}
		wv, _ := strconv.ParseUint(bits, 16, 64)
		switch {
		case len(g) == len(bits):
			return gv == wv
		case len(bits) == 8 && len(g) == 16:
			return gv == math.Float64bits(float64(math.Float32frombits(uint32(wv))))
		}
		return false
	}
	if _, raw, ok := strings.Cut(want, "|raw="); ok {
		return got == raw || got == "raw="+raw
	}
	if strings.HasPrefix(want, `"`) && strings.HasPrefix(got, `"`) {
		w, err1 := unquote(want)
		g, err2 := unquote(got)
		return err1 == nil && err2 == nil && w == g
	}
	return false
}

// Answer is what a leg's read of one corpus root came to: Refused is the
// refusal's name ("" when the load landed); otherwise Records is how many
// records landed and Values the landed values by the manifest's path, each in
// the manifest's spelling (Same).
type Answer struct {
	Refused string
	Records int
	Values  map[string]string
}

// Answers is the set of corpus roots one leg's test answered, and the tally
// of what the harness compared. It is safe for parallel subtests.
type Answers struct {
	mu       sync.Mutex
	got      map[string]bool
	reads    int
	refusals int
	compared int
}

// Check judges one answer against the root's manifest line and records the
// root as answered. It returns what is wrong, one line each, and nil when the
// answer is the reference's: the refusal it named, or the record count and
// every value it wrote.
func (a *Answers) Check(row ManifestRow, got Answer) []string {
	var wrong []string
	compared := 0
	switch {
	case row.Refuse != "" && got.Refused != row.Refuse:
		if got.Refused == "" {
			wrong = append(wrong, fmt.Sprintf("landed %d records; the reference refuses the whole load %s", got.Records, row.Refuse))
		} else {
			wrong = append(wrong, fmt.Sprintf("refused %s; the reference refuses the whole load %s", got.Refused, row.Refuse))
		}
	case row.Refuse != "":
	case got.Refused != "":
		wrong = append(wrong, fmt.Sprintf("refused %s; the reference reads it, %d records", got.Refused, row.Records))
	default:
		if got.Records != row.Records {
			wrong = append(wrong, fmt.Sprintf("landed %d records; the reference wrote %d", got.Records, row.Records))
		}
		for _, v := range row.Landed {
			text, ok := got.Values[v.Path]
			switch {
			case !ok:
				wrong = append(wrong, fmt.Sprintf("landed nothing at %s; the reference wrote %s", v.Path, v.Text))
			case !Same(v.Text, text):
				wrong = append(wrong, fmt.Sprintf("landed %s=%s; the reference wrote %s", v.Path, text, v.Text))
			}
			compared++
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.got == nil {
		a.got = map[string]bool{}
	}
	a.got[row.File] = true
	if row.Refuse != "" {
		a.refusals++
	} else {
		a.reads++
	}
	a.compared += compared
	return wrong
}

// Count is how many roots were answered.
func (a *Answers) Count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.got)
}

// Gap compares the answered set to the manifest's: the roots the manifest
// names that were never answered, and the answers naming a file the manifest
// does not carry, each sorted.
func (a *Answers) Gap(rows []ManifestRow) (unanswered, unknown []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	want := map[string]bool{}
	for _, r := range rows {
		want[r.File] = true
		if !a.got[r.File] {
			unanswered = append(unanswered, r.File)
		}
	}
	for f := range a.got {
		if !want[f] {
			unknown = append(unknown, f)
		}
	}
	slices.Sort(unanswered)
	slices.Sort(unknown)
	return unanswered, unknown
}

// TB is the part of testing.TB EveryRoot needs (fixedvector is not a test
// package, so it does not import testing).
type TB interface {
	Helper()
	Name() string
	Cleanup(func())
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Logf(format string, args ...any)
}

// Record is what a complete every-root walk leaves behind: the leg, the test
// that walked, how many roots it answered of the manifest's, and what the
// harness compared.
type Record struct {
	Leg, Test                 string
	Answered, Manifest        int
	Reads, Refusals, Compared int
}

var (
	recordsMu sync.Mutex
	records   = map[string][]Record{}
)

// recorded returns the records this process holds for leg.
func recorded(leg string) []Record {
	recordsMu.Lock()
	defer recordsMu.Unlock()
	return slices.Clone(records[leg])
}

// EveryRoot reads the corpus's manifest and returns the leg's answer set and
// the rows to walk. When t ends (after its parallel subtests) the answered
// set must equal the manifest's, by name, both ways; a complete walk leaves
// its Record for Main. The rows are the manifest's and never a list the leg
// hands in, so a leg cannot answer every root of a list it picked.
func EveryRoot(t TB, leg, corpus string) (*Answers, []ManifestRow) {
	t.Helper()
	rows, err := Manifest(corpus)
	if err != nil {
		t.Fatalf("%s: %v", leg, err)
	}
	a := &Answers{}
	test := t.Name()
	t.Cleanup(func() {
		unanswered, unknown := a.Gap(rows)
		for _, f := range unanswered {
			t.Errorf("%s answered no verdict for the corpus root %s: every root the manifest names is answered (EVERY TEST IS A SHARED CORPUS)", leg, f)
		}
		for _, f := range unknown {
			t.Errorf("%s answered %s, which the corpus manifest does not carry", leg, f)
		}
		if len(unanswered) > 0 || len(unknown) > 0 {
			t.Errorf("%s answered %d of the manifest's %d fixed corpus roots", leg, a.Count()-len(unknown), len(rows))
			return
		}
		a.mu.Lock()
		rec := Record{Leg: leg, Test: test, Answered: len(a.got), Manifest: len(rows), Reads: a.reads, Refusals: a.refusals, Compared: a.compared}
		a.mu.Unlock()
		if rec.Reads > 0 && rec.Compared == 0 {
			t.Errorf("%s read %d corpus roots and the harness compared no landed value: a walk that checks no value is not an answer", leg, rec.Reads)
			return
		}
		t.Logf("%s answered %d of the manifest's %d fixed corpus roots: %d read with %d landed values compared against the reference's, %d refused by the reference's name",
			leg, rec.Answered, rec.Manifest, rec.Reads, rec.Compared, rec.Refusals)
		recordsMu.Lock()
		records[leg] = append(records[leg], rec)
		recordsMu.Unlock()
	})
	return a, rows
}

// Main judges a leg's run once its tests have run: call it from TestMain as
// os.Exit(fixedvector.Main(m.Run(), "lua", "TestLuaFixedCorpusEveryRoot")),
// code being m.Run's. It requires the RECORD of a complete every-root walk
// from this very run: test, by that name, reached EveryRoot and answered
// every root. A test that exists but never calls EveryRoot, or calls it and
// returns early, leaves no record and the run is red by name. A run that does
// not select test (-run, -skip, -list) or that the slow gate holds back says
// so on stderr and is judged on its own tests.
func Main(code int, leg, test string) int {
	verdict, fail := judge(leg, test, selected(test), slowtest.Enabled(), recorded(leg))
	fmt.Fprintln(os.Stderr, verdict)
	if fail && code == 0 {
		code = 1
	}
	return code
}

// judge is Main's decision, apart from the process: the line it prints, and
// whether the run fails.
func judge(leg, test string, selected, slow bool, recs []Record) (string, bool) {
	if !selected {
		return fmt.Sprintf("fixedvector: %s: %s is not selected in this run (-run/-skip/-list); its every-root record is required where it is selected", leg, test), false
	}
	if !slow {
		return fmt.Sprintf("fixedvector: %s: SCHEMA_SLOW is not set, so %s did not walk the corpus in this run; the slow half (ci-full.yml, make test-%s) requires its record", leg, test, leg), false
	}
	for _, r := range recs {
		if r.Test == test && r.Answered == r.Manifest && r.Answered > 0 {
			return fmt.Sprintf("fixedvector: %s: every-root record: %s answered %d of the manifest's %d roots, %d landed values compared, %d refusals by name",
				leg, test, r.Answered, r.Manifest, r.Compared, r.Refusals), false
		}
	}
	return fmt.Sprintf("FAIL fixedvector: %s: %s ran and left no every-root record: it never reached fixedvector.EveryRoot, or its walk did not answer every root of the manifest (EVERY TEST IS A SHARED CORPUS)", leg, test), true
}

// selected reports whether this run's -run and -skip select the top-level
// test name, as go test matches it (a pattern's first '/'-separated element
// against the top-level name).
func selected(test string) bool {
	get := func(name string) string {
		if f := flag.Lookup(name); f != nil {
			return f.Value.String()
		}
		return ""
	}
	if get("test.list") != "" {
		return false
	}
	return selects(get("test.run"), get("test.skip"), test)
}

// selects is selected's matching, on the two patterns.
func selects(run, skip, test string) bool {
	top := func(p string) string {
		if i := strings.Index(p, "/"); i >= 0 {
			return p[:i]
		}
		return p
	}
	if run != "" {
		re, err := regexp.Compile(top(run))
		if err != nil || !re.MatchString(test) {
			return false
		}
	}
	// a -skip with a '/' skips subtests and still runs the top-level test
	if skip != "" && !strings.Contains(skip, "/") {
		re, err := regexp.Compile(skip)
		if err == nil && re.MatchString(test) {
			return false
		}
	}
	return true
}
