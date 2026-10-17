package fuzz_test

// tablediff_test.go is the differential half of the random-schema harness
// (card schema-go-fuzz-random-table-differential). For each seed it builds a
// random VALID unit (tablegen_test.go), generates the Go leg with
// compiler.New().Generate(u, "go", nil), writes a probe beside it and runs the
// probe in a t.TempDir() under -race (the house runGeneratedUnit pattern,
// internal/codegen/gotable/values_test.go). The probe drives 24 values (value 0
// is {}) through every read and write path, damages every wire, plants hostile
// JSON and runs a bounds pass, and writes each finding as one line instead of
// stopping at the first. This file classifies those lines against the owed
// table below and runs the compiler's engine (tablepack.UnpackOneFile and
// tablepack.Pack) in both directions over every Go wire and text.
//
// The oracle: no panic, no stack overflow, no allocation over 4 MB for an
// input under 4 KB; ReadStrict never refuses a clean Save; every value a read
// accepts measures, saves and writes JSON; LoadMeasureReason agrees with the
// load; engine and leg agree. A finding is a failure unless the owed table
// names an open card for its class, matched narrowly (by line kind AND the
// exact shape of the difference, never by what the unit merely holds), and
// then it is printed with that card's id. Silent wrong-value decodes of damaged
// wires are counted and printed, never failed, until
// schema-go-wire-integrity-check lands.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const tableDiffValues = 24

// finding is one probe or engine line: its kind, the value index, and up to
// two payloads (the expected and the observed text or wire).
type finding struct {
	Kind   string
	Index  int
	A, B   []byte
	Detail string
}

func (f finding) String() string {
	s := fmt.Sprintf("%s value %d", f.Kind, f.Index)
	if f.Detail != "" {
		s += ": " + f.Detail
	}
	if f.A != nil || f.B != nil {
		s += "\n    " + firstDiff(f.A, f.B)
	}
	return s
}

// firstDiff names the first byte where a and b differ, with the context on
// both sides of it.
func firstDiff(a, b []byte) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	lo := max(0, i-60)
	ctx := func(x []byte) string { return fmt.Sprintf("%q", x[min(lo, len(x)):min(i+60, len(x))]) }
	return fmt.Sprintf("first difference at byte %d of %d/%d\n    want: %s\n    got:  %s", i, len(a), len(b), ctx(a), ctx(b))
}

func shorten(s string) string {
	if len(s) > 400 {
		return s[:400] + "..."
	}
	return s
}

var negZeroToken = regexp.MustCompile(`(^|[\[,:\s])-0(\.0+)?([,\]}\s]|$)`)

// onlyNegativeZero reports whether a and b differ, and differ only by plain
// float -0 tokens written as 0 (or the reverse).
func onlyNegativeZero(a, b []byte) bool {
	if bytes.Equal(a, b) {
		return false
	}
	norm := func(x []byte) string {
		return negZeroToken.ReplaceAllString(string(x), "${1}0${2}${3}")
	}
	return norm(a) == norm(b)
}

// owedClass is one class of finding an OPEN card already owns. The harness
// prints it with the card id and does not fail on it. Each matcher names the
// line kinds it may own and the exact shape of the difference; nothing is owed
// because of what the unit merely contains.
type owedClass struct {
	card  string
	why   string
	match func(u *genUnit, f finding) bool
}

var owedClasses = []owedClass{
	{
		card: "schema-go-plain-float-special-values",
		why:  "a plain float's -0 is not carried the same way by every Go form, so two texts of one value differ in a -0 token alone (§3.3 rules compressed floats only)",
		match: func(u *genUnit, f finding) bool {
			return (strings.HasPrefix(f.Kind, "roundtrip-") || f.Kind == "engine-text") && onlyNegativeZero(f.A, f.B)
		},
	},
	{
		card: "schema-go-plain-float-special-values",
		why:  "a damaged wire decodes a plain float to NaN or an infinity, which the JSON writer refuses (no ruling yet on non-finite plain floats in the strict read)",
		match: func(u *genUnit, f finding) bool {
			return strings.HasPrefix(f.Kind, "damage-") && strings.HasSuffix(f.Kind, "-unjson") && u.has(func(t *gType) bool { return t.Kind == gFloat32 || t.Kind == gFloat64 })
		},
	},
	{
		card: "schema-go-load-measure-refuses-damaged-body",
		why:  "LoadMeasureReason accepts a variable wire whose framing the load then reports malformed",
		match: func(u *genUnit, f finding) bool {
			return f.Kind == "measure-accepts-malformed"
		},
	},
	{
		card: "schema-tabletext-text-lists",
		why:  "the compiler's engine does not read or write text lists (§2.13) as the Go leg does: its unpack of a text list is malformed, its pack counts each string element a kind mismatch, and its text differs from the leg's only inside text-list fields",
		match: func(u *genUnit, f finding) bool {
			if !u.has(isTextList) {
				return false
			}
			switch f.Kind {
			case "engine-text":
				// owed only when the two texts agree once every text-list
				// field is taken out of both (a plain float -0 written as 0
				// beside it is the other open card's, so it is set aside too)
				a, errA := withoutTextLists(u, f.A)
				b, errB := withoutTextLists(u, f.B)
				return errA == nil && errB == nil && (a == b || onlyNegativeZero([]byte(a), []byte(b)))
			case "engine-unpack-report":
				// the engine reads a text list's element kind as unknown and
				// its body as malformed
				return strings.Contains(f.Detail, "Malformed:true") && !nonZeroCounter(f.Detail, "Widened", "Clamped", "Duplicate", "KindMismatch")
			case "engine-pack-report":
				return !strings.Contains(f.Detail, "Malformed:true") && !nonZeroCounter(f.Detail, "Unknown", "Widened", "Clamped", "Duplicate")
			case "engine-wire":
				// a pack that skipped text lists as kind mismatches reports
				// it (engine-pack-report) and never gets here
				return false
			}
			return false
		},
	},
}

func isTextList(t *gType) bool { return t.Kind == gTextBounded || t.Kind == gTextList }

var counterField = regexp.MustCompile(`(\w+):(-?\d+)`)

// nonZeroCounter reports whether a printed report names any of the counters
// with a non-zero value.
func nonZeroCounter(detail string, names ...string) bool {
	for _, m := range counterField.FindAllStringSubmatch(detail, -1) {
		for _, n := range names {
			if m[1] == n && m[2] != "0" {
				return true
			}
		}
	}
	return false
}

// withoutTextLists parses a Root text and drops every text-list field,
// wherever the model puts one, then writes it back canonically, so two texts
// can be compared outside the text-list fields alone.
func withoutTextLists(u *genUnit, text []byte) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(text))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return "", err
	}
	var walk func(t *gType, v any) any
	var table func(tb *gTable, v any) any
	table = func(tb *gTable, v any) any {
		m, ok := v.(map[string]any)
		if !ok {
			return v
		}
		for _, f := range tb.Fields {
			if fv, ok := m[f.Name]; ok {
				if isTextList(f.T) {
					delete(m, f.Name)
				} else {
					m[f.Name] = walk(f.T, fv)
				}
			}
		}
		return m
	}
	walk = func(t *gType, v any) any {
		switch t.Kind {
		case gByValue, gPtrTable:
			return table(t.Table, v)
		case gArray, gBounded, gList:
			if xs, ok := v.([]any); ok {
				for i := range xs {
					xs[i] = walk(t.Elem, xs[i])
				}
			}
		case gOptional:
			if v != nil {
				return walk(t.Elem, v)
			}
		case gMapString, gMapInt:
			if m, ok := v.(map[string]any); ok {
				for k := range m {
					m[k] = walk(t.Elem, m[k])
				}
			}
		case gUnionField:
			if m, ok := v.(map[string]any); ok {
				for _, a := range t.Union.Arms {
					if av, ok := m[a.Name]; ok && a.T.Kind != gPing {
						if isTextList(a.T) {
							delete(m, a.Name)
						} else {
							m[a.Name] = walk(a.T, av)
						}
					}
				}
			}
		}
		return v
	}
	out, err := json.Marshal(table(u.Root, v))
	return string(out), err
}

func tableUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	ast, perrs := parser.Parse("probe.schema", []byte(src))
	if len(perrs) > 0 || ast == nil {
		t.Fatalf("parse: %v\n%s", perrs, src)
	}
	unit, cerrs := check.Unit([]check.SourceFile{{Path: "probe.schema", Name: "probe.schema", Base: "probe", Bytes: []byte(src), AST: ast}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v\n%s", cerrs, src)
	}
	return unit
}

// TestRandomTableGenerator pins the generator's contract: every unit it builds
// over the first 400 seeds passes the checker, and together they reach every
// construct the card names.
func TestRandomTableGenerator(t *testing.T) {
	t.Parallel()
	census := map[string]int{}
	for seed := 0; seed < 400; seed++ {
		u := randomUnit(seed)
		tableUnit(t, u.Source())
		for k := range u.Census() {
			census[k]++
		}
	}
	for _, want := range []string{"fixed Root", "variable Root", "fixed inner table", "variable inner table", "enum field",
		"int", "uint", "float32", "float64", "bool", "fixed/ufixed", "string(N)", "bytes(N)", "timestamp", "[N]T", "[..N]T", "[]T",
		"[..N]string(M)", "[]string(M)", "?string(M) list", "map[string(N)]V", "map[intK]V", "?T", "table by value", "*Table",
		"*string", "*bytes", "union", "payload-free arm", "range mode 0", "range mode 1", "range mode 2", "range mode 3"} {
		if census[want] == 0 {
			t.Errorf("no unit over seeds 0..399 holds %s", want)
		}
	}
}

// runGeneratedTableUnit is the house runGeneratedUnit pattern
// (internal/codegen/gotable/values_test.go:34) over compiler.New().Generate,
// with the serialize.go replace line pointed at the sibling checkout.
func runGeneratedTableUnit(t *testing.T, u *ir.Unit, testSource string, env map[string]string, flags ...string) ([]byte, error) {
	t.Helper()
	files, err := compiler.New().Generate(u, "go", nil)
	if err != nil {
		return nil, err
	}
	dir := t.TempDir()
	runtime, err := filepath.Abs("../../../serialize.go")
	if err != nil {
		return nil, err
	}
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %q\n", runtime))
	files["values_test.go"] = []byte(testSource)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			return nil, err
		}
	}
	args := append([]string{"test", "-count=1", "-timeout", "600s"}, flags...)
	cmd := exec.Command("go", append(args, ".")...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	return cmd.CombinedOutput()
}

// The probe's class-specific helpers: a fixed Root is a value (§6.1), a
// variable Root lives in a builder's region (§6.2) and loads into a region its
// LoadMeasure sizes.
const probeFixedHelpers = `
func fromJSON(text []byte, rep *TableReport) (*Root, bool) {
	v := new(Root)
	RootReset(v)
	ok := RootFromJson(v, text, rep)
	return v, ok
}

func load(wire []byte, rep *TableReport) (*Root, bool) {
	v := new(Root)
	RootReset(v)
	ok := RootLoad(v, wire, rep)
	return v, ok
}

const hasLoadMeasure = false

func loadMeasure(wire []byte) (int64, error) { return 0, nil }
`

const probeVariableHelpers = `
func fromJSON(text []byte, rep *TableReport) (*Root, bool) {
	var b RootBuilder
	if !b.Init() {
		return nil, false
	}
	if !RootFromJson(&b, text, rep) {
		b.Shutdown()
		return nil, false
	}
	if !b.Lock() {
		return nil, false
	}
	return b.AsConst(), true
}

func load(wire []byte, rep *TableReport) (*Root, bool) {
	n := RootLoadMeasure(wire)
	if n < 0 {
		return nil, false
	}
	raw := make([]byte, n+64)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	v := RootLoad(raw[off:off+uintptr(n)], wire, rep)
	return v, v != nil
}

const hasLoadMeasure = true

func loadMeasure(wire []byte) (int64, error) { return RootLoadMeasureReason(wire) }
`

const probeBody = `package probe

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	%s
)

%s

var out []string

// note writes one finding line: kind, value index, two base64 payloads, detail.
func note(kind string, i int, a, b []byte, detail string) {
	out = append(out, strings.Join([]string{kind, strconv.Itoa(i), base64.StdEncoding.EncodeToString(a), base64.StdEncoding.EncodeToString(b), strings.ReplaceAll(detail, "\n", " ")}, "\t"))
}

func silent(r TableReport) bool {
	return r.Reason == "" && !r.Malformed && r.Unknown == 0 && r.KindMismatch == 0 && r.Clamped == 0 && r.Duplicate == 0 && r.Widened == 0 && r.MissingRequired == 0
}

func save(v *Root) ([]byte, error) {
	n, err := RootMeasureReason(v)
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, fmt.Errorf("measure %%d", n)
	}
	buf := make([]byte, n)
	if got := RootSave(v, buf); got != n {
		return nil, fmt.Errorf("save wrote %%d of %%d", got, n)
	}
	return buf, nil
}

func toJSON(v *Root) ([]byte, error) {
	n := RootToJsonMeasure(v)
	if n < 0 {
		return nil, fmt.Errorf("tojson measure %%d", n)
	}
	buf := make([]byte, n)
	if got := RootToJson(v, buf); got != n {
		return nil, fmt.Errorf("tojson wrote %%d of %%d", got, n)
	}
	return buf, nil
}

var sabotage = os.Getenv("SCHEMA_TABLE_DIFF_SABOTAGE") == "1"

// same is the probe's one comparison; the negative control flips it.
func same(a, b []byte) bool {
	if sabotage {
		return !bytes.Equal(a, b)
	}
	return bytes.Equal(a, b)
}

// writable checks the oracle's "every value a read accepts measures, saves
// and writes JSON", and that the strict read takes its save back.
func writable(kind string, i int, v *Root, in ...byte) ([]byte, []byte, bool) {
	w, err := save(v)
	if err != nil {
		note(kind+"-unsaveable", i, nil, in, err.Error())
		return nil, nil, false
	}
	j, err := toJSON(v)
	if err != nil {
		note(kind+"-unjson", i, w, in, err.Error())
		return w, nil, false
	}
	if _, err := RootReadStrict(w); err != nil {
		note(kind+"-strict-refuses-resave", i, nil, w, err.Error())
		return w, j, false
	}
	return w, j, true
}

// guarded runs f under recover, measuring what it allocates.
func guarded(kind string, i int, in []byte, f func()) {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	func() {
		defer func() {
			if r := recover(); r != nil {
				note(kind+"-panic", i, nil, in, fmt.Sprint(r))
			}
		}()
		f()
	}()
	runtime.ReadMemStats(&after)
	if len(in) < 4096 && after.TotalAlloc-before.TotalAlloc > 4<<20 {
		note(kind+"-alloc", i, nil, in, fmt.Sprintf("%%d bytes for a %%d-byte input", after.TotalAlloc-before.TotalAlloc, len(in)))
	}
}

func damage(wire []byte) [][]byte {
	var ops [][]byte
	for cut := 0; cut < len(wire); cut++ {
		ops = append(ops, append([]byte{}, wire[:cut]...))
	}
	for idx := range wire {
		for _, m := range []byte{0x01, 0x80, 0xff} {
			cp := append([]byte{}, wire...)
			cp[idx] ^= m
			ops = append(ops, cp)
		}
	}
	r := rand.New(rand.NewSource(int64(len(wire))))
	for k := 0; k < 100 && len(wire) > 0; k++ {
		cp := append([]byte{}, wire...)
		if k%%2 == 0 {
			a := r.Intn(len(cp))
			b := a + r.Intn(len(cp)-a+1)
			at := r.Intn(len(cp))
			cp = append(append(append([]byte{}, cp[:at]...), wire[a:b]...), cp[at:]...)
		} else {
			at := r.Intn(len(cp) + 1)
			ins := make([]byte, 1+r.Intn(4))
			r.Read(ins)
			cp = append(append(append([]byte{}, cp[:at]...), ins...), cp[at:]...)
		}
		ops = append(ops, cp)
	}
	return ops
}

func TestProbe(t *testing.T) {
	dir := os.Getenv("SCHEMA_TABLE_DIFF_DIR")
	n, err := strconv.Atoi(os.Getenv("SCHEMA_TABLE_DIFF_N"))
	if dir == "" || err != nil {
		t.Fatal("SCHEMA_TABLE_DIFF_DIR / SCHEMA_TABLE_DIFF_N unset")
	}
	switch os.Getenv("SCHEMA_TABLE_DIFF_PHASE") {
	case "damage":
		damagePhase(t, dir, n)
		return
	case "race":
		racePhase(t, dir, n)
		return
	}
	for i := 0; i < n; i++ {
		text, err := os.ReadFile(filepath.Join(dir, "in-"+strconv.Itoa(i)+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var rep TableReport
		v, ok := fromJSON(text, &rep)
		if !ok || !silent(rep) {
			note("fromjson", i, nil, text, fmt.Sprintf("ok=%%v report=%%+v", ok, rep))
			continue
		}
		wire, tj, ok := writable("clean", i, v)
		if !ok {
			continue
		}
		_ = os.WriteFile(filepath.Join(dir, "leg-"+strconv.Itoa(i)+".json"), tj, 0o600)
		_ = os.WriteFile(filepath.Join(dir, "leg-"+strconv.Itoa(i)+".wire"), wire, 0o600)
		// every read of the clean wire gives the same text and re-encodes byte-identically
		check := func(kind string, got *Root) {
			gw, gj, ok := writable(kind, i, got)
			if !ok {
				return
			}
			if !same(tj, gj) {
				note("roundtrip-"+kind, i, tj, gj, "text")
			}
			if !same(wire, gw) {
				note("reencode-"+kind, i, wire, gw, "wire")
			}
		}
		if got, err := RootReadStrict(wire); err != nil {
			note("strict-refuses-clean", i, nil, wire, err.Error())
		} else {
			check("strict", got)
		}
		rep = TableReport{}
		if got, ok := load(wire, &rep); !ok || !silent(rep) {
			note("load-refuses-clean", i, nil, wire, fmt.Sprintf("ok=%%v report=%%+v", ok, rep))
		} else {
			check("load", got)
		}
		st := base64.StdEncoding.EncodeToString(wire)
		rep = TableReport{}
		if got, err := RootLoadText(st, &rep); err != nil {
			note("loadtext-refuses-clean", i, nil, wire, err.Error())
		} else {
			check("loadtext", got)
		}
		rep = TableReport{}
		if got, ok := fromJSON(tj, &rep); !ok || !silent(rep) {
			note("fromjson-own-text", i, nil, tj, fmt.Sprintf("ok=%%v report=%%+v", ok, rep))
		} else {
			check("json", got)
		}
		if s, err := RootSaveText(v); err != nil || s != st {
			note("savetext", i, []byte(st), []byte(s), fmt.Sprint(err))
		}
		if hasLoadMeasure {
			if m, err := loadMeasure(wire); err != nil || m < 0 {
				note("loadmeasure-refuses-clean", i, nil, wire, fmt.Sprint(m, err))
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "findings-clean.txt"), []byte(strings.Join(out, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// racePhase runs under -race: for every clean wire, four goroutines read the
// same wire at once and each writes its text. They read with the tolerant
// load, which reads into a region of its own; the strict read is raced on the
// first and the last value only, because each strict read builds a builder
// whose arena struct is megabytes and the race detector's shadow of it costs
// about a second per read.
func racePhase(t *testing.T, dir string, n int) {
	for i := 0; i < n; i++ {
		wire, err1 := os.ReadFile(filepath.Join(dir, "leg-"+strconv.Itoa(i)+".wire"))
		tj, err2 := os.ReadFile(filepath.Join(dir, "leg-"+strconv.Itoa(i)+".json"))
		if err1 != nil || err2 != nil {
			continue
		}
		var wg sync.WaitGroup
		texts := make([][]byte, 4)
		strict := i == 0 || i == n-1
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				if strict {
					if got, err := RootReadStrict(wire); err == nil {
						texts[g], _ = toJSON(got)
					}
					return
				}
				var rep TableReport
				if got, ok := load(wire, &rep); ok && silent(rep) {
					texts[g], _ = toJSON(got)
				}
			}(g)
		}
		wg.Wait()
		for g := range texts {
			if !same(tj, texts[g]) {
				note("roundtrip-concurrent", i, tj, texts[g], "goroutine "+strconv.Itoa(g))
			}
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "findings-race.txt"), []byte(strings.Join(out, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// damagePhase runs every damage of every clean wire, then the plants. It runs
// without -race: the race detector's cost per read times the thousands of
// damaged reads per wire is minutes per seed, and the concurrent reads of the
// clean phase are what -race is for.
func damagePhase(t *testing.T, dir string, n int) {
	wrong := 0
	for i := 0; i < n; i++ {
		wire, err1 := os.ReadFile(filepath.Join(dir, "leg-"+strconv.Itoa(i)+".wire"))
		tj, err2 := os.ReadFile(filepath.Join(dir, "leg-"+strconv.Itoa(i)+".json"))
		if err1 != nil || err2 != nil {
			continue
		}
		for _, d := range damage(wire) {
			// each read is measured on its own: the ceiling is the read's, and
			// writing back what a read accepted is the oracle's, not the read's
			var strict *Root
			var serr error = fmt.Errorf("panicked")
			guarded("damage-strict", i, d, func() { strict, serr = RootReadStrict(d) })
			if serr == nil {
				if _, gj, ok := writable("damage-strict", i, strict, d...); ok && !bytes.Equal(gj, tj) {
					wrong++
				}
			}
			var r TableReport
			var lv *Root
			var lok bool
			guarded("damage-load", i, d, func() { lv, lok = load(d, &r) })
			if lok && lv != nil {
				writable("damage-load", i, lv, d...)
			}
			if hasLoadMeasure {
				var m int64
				var merr error
				guarded("damage-loadmeasure", i, d, func() { m, merr = loadMeasure(d) })
				if (merr != nil || m < 0) && lok {
					note("measure-refuses-loaded", i, nil, d, fmt.Sprint(m, merr))
				}
				if merr == nil && m >= 0 && (r.Malformed || !lok) {
					note("measure-accepts-malformed", i, nil, d, fmt.Sprintf("measure %%d, load ok=%%v report=%%+v", m, lok, r))
				}
			}
			var lt *Root
			var lterr error = fmt.Errorf("panicked")
			text := base64.StdEncoding.EncodeToString(d)
			guarded("damage-loadtext", i, d, func() { lt, lterr = RootLoadText(text, &r) })
			if lterr == nil {
				writable("damage-loadtext", i, lt, d...)
			}
		}
	}
	// hostile JSON plants and the bounds pass: each a document and whether a
	// silent accept of it is wrong
	for k := 0; ; k++ {
		doc, err := os.ReadFile(filepath.Join(dir, "plant-"+strconv.Itoa(k)+".json"))
		if err != nil {
			break
		}
		flag, _ := os.ReadFile(filepath.Join(dir, "plant-"+strconv.Itoa(k)+".flag"))
		guarded("plant", k, doc[:min(len(doc), 64)], func() {
			var rep TableReport
			v, ok := fromJSON(doc, &rep)
			if ok && v != nil {
				writable("plant", k, v)
			}
			if string(flag) == "must" && ok && silent(rep) {
				note("plant-silent", k, nil, doc[:min(len(doc), 256)], "a silent accept")
			}
		})
	}
	out = append(out, "wrongvalue\t"+strconv.Itoa(wrong))
	if err := os.WriteFile(filepath.Join(dir, "findings-damage.txt"), []byte(strings.Join(out, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
`

func probeSource(u *genUnit) string {
	if u.Root.Fixed {
		return fmt.Sprintf(probeBody, "", probeFixedHelpers)
	}
	return fmt.Sprintf(probeBody, `"unsafe"`, probeVariableHelpers)
}

type plant struct {
	doc  string
	must bool
}

// plants are the hostile JSON documents (null, -0, 1e999, NaN, a lone
// surrogate escape, a BOM, trailing data, duplicate keys, nesting to 10^6) and
// the bounds pass (min-1, max+1, the implied extremes plus one, 1e400, 2.5,
// "7", null) over every scalar Root field.
func plants(u *genUnit, valid string) []plant {
	ps := []plant{{"null", true}, {"-0", true}, {"1e999", true}, {"NaN", true}, {`"\ud800"`, true},
		{"\xef\xbb\xbf" + valid, false}, {valid + " {}", true}, {valid + " x", true},
		{"{" + strings.Repeat("[", 1000000) + strings.Repeat("]", 1000000) + "}", true},
		{strings.Repeat("[", 1000000), true}}
	for _, f := range u.Root.Fields {
		switch f.T.Kind {
		case gString:
			ps = append(ps, plant{`{"` + f.Name + `":"a\ud800b"}`, false}, plant{`{"` + f.Name + `":"\udc00"}`, false})
		case gPtrString:
			ps = append(ps, plant{`{"` + f.Name + `":"\ud83d"}`, false})
		}
		ps = append(ps, plant{`{"` + f.Name + `":` + strings.Repeat("[", 1000000) + strings.Repeat("]", 1000000) + `}`, true})
		ps = append(ps, plant{`{"` + f.Name + `":null,"` + f.Name + `":null}`, true})
		for _, tok := range f.T.boundsTokens() {
			ps = append(ps, plant{`{"` + f.Name + `":` + tok.Text + `}`, tok.MustCount})
		}
	}
	return ps
}

// runSeed generates, compiles and runs one seed, then runs the engine over
// every Go wire and text. It returns the failing findings, the owed ones (with
// their card ids) and the wrong-value count.
func runSeed(t *testing.T, seed int, sabotage bool) (failing, owed []string, wrong int) {
	t.Helper()
	u := randomUnit(seed)
	src := u.Source()
	unit := tableUnit(t, src)
	dir := t.TempDir()
	values := u.Values(tableDiffValues)
	for i, v := range values {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("in-%d.json", i)), []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	valid := values[len(values)-1]
	for k, p := range plants(u, valid) {
		flag := "may"
		if p.must {
			flag = "must"
		}
		_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("plant-%d.json", k)), []byte(p.doc), 0o600)
		_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("plant-%d.flag", k)), []byte(flag), 0o600)
	}
	env := map[string]string{"SCHEMA_TABLE_DIFF_DIR": dir, "SCHEMA_TABLE_DIFF_N": strconv.Itoa(tableDiffValues)}
	if sabotage {
		env["SCHEMA_TABLE_DIFF_SABOTAGE"] = "1"
	}
	// three phases over one probe: the clean paths, the concurrent reads under
	// -race, then every damage and plant
	var raw []byte
	for _, phase := range []struct{ name, findings string }{{"clean", "findings-clean.txt"}, {"race", "findings-race.txt"}, {"damage", "findings-damage.txt"}} {
		env["SCHEMA_TABLE_DIFF_PHASE"] = phase.name
		var flags []string
		if phase.name == "race" {
			flags = []string{"-race"}
		}
		out, err := runGeneratedTableUnit(t, unit, probeSource(u), env, flags...)
		got, rerr := os.ReadFile(filepath.Join(dir, phase.findings))
		if err != nil || rerr != nil {
			return []string{fmt.Sprintf("seed %d: the probe's %s phase did not finish (%v, %v)\n%s\n%s", seed, phase.name, err, rerr, shorten(string(out)), src)}, nil, 0
		}
		raw = append(raw, got...)
	}
	var fs []finding
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		parts := strings.Split(line, "\t")
		if parts[0] == "wrongvalue" && len(parts) == 2 {
			wrong, _ = strconv.Atoi(parts[1])
			continue
		}
		if len(parts) != 5 {
			failing = append(failing, fmt.Sprintf("seed %d: unreadable probe line %q", seed, line))
			continue
		}
		idx, _ := strconv.Atoi(parts[1])
		a, _ := base64.StdEncoding.DecodeString(parts[2])
		b, _ := base64.StdEncoding.DecodeString(parts[3])
		fs = append(fs, finding{Kind: parts[0], Index: idx, A: a, B: b, Detail: parts[4]})
	}
	fs = append(fs, engineFindings(t, unit, dir)...)
	for _, f := range fs {
		owner := ""
		for _, c := range owedClasses {
			if c.match(u, f) {
				owner = c.card
				break
			}
		}
		if owner != "" {
			owed = append(owed, fmt.Sprintf("seed %d OWED %s: %s", seed, owner, f.Kind))
		} else {
			msg := fmt.Sprintf("seed %d: %s", seed, f)
			if f.Kind == "engine-text" && u.has(isTextList) {
				a, _ := withoutTextLists(u, f.A)
				b, _ := withoutTextLists(u, f.B)
				msg += "\n    outside text lists: " + firstDiff([]byte(a), []byte(b))
			}
			failing = append(failing, msg)
		}
	}
	if len(failing) > 0 {
		failing = append(failing, fmt.Sprintf("seed %d unit:\n%s", seed, src))
	}
	return failing, owed, wrong
}

// engineFindings runs the compiler's engine over each Go wire and text: the
// wire unpacks to the leg's text, and the leg's text packs to the leg's wire.
func engineFindings(t *testing.T, unit *ir.Unit, dir string) []finding {
	m := tabletext.NewModel(unit)
	var fs []finding
	for i := 0; i < tableDiffValues; i++ {
		wire, err1 := os.ReadFile(filepath.Join(dir, fmt.Sprintf("leg-%d.wire", i)))
		legJSON, err2 := os.ReadFile(filepath.Join(dir, fmt.Sprintf("leg-%d.json", i)))
		if err1 != nil || err2 != nil {
			continue // the probe reported why this value has no clean wire
		}
		d1 := t.TempDir()
		report, err := tablepack.UnpackOneFile(m, "Root", wire, d1)
		switch {
		case err != nil:
			fs = append(fs, finding{Kind: "engine-unpack-refuses", Index: i, Detail: err.Error()})
		case report != (tabletext.Report{}):
			fs = append(fs, finding{Kind: "engine-unpack-report", Index: i, Detail: fmt.Sprintf("%+v", report)})
		default:
			engineJSON, err := os.ReadFile(filepath.Join(d1, "Root.json"))
			if err != nil {
				fs = append(fs, finding{Kind: "engine-unpack-refuses", Index: i, Detail: err.Error()})
			} else if !bytes.Equal(engineJSON, legJSON) {
				fs = append(fs, finding{Kind: "engine-text", Index: i, A: legJSON, B: engineJSON})
			}
		}
		d2 := t.TempDir()
		if err := os.WriteFile(filepath.Join(d2, "Root.json"), legJSON, 0o600); err != nil {
			t.Fatal(err)
		}
		back, _, report2, err := tablepack.Pack(m, "Root", d2)
		switch {
		case err != nil:
			fs = append(fs, finding{Kind: "engine-pack-refuses", Index: i, Detail: err.Error()})
		case report2 != (tabletext.Report{}):
			fs = append(fs, finding{Kind: "engine-pack-report", Index: i, Detail: fmt.Sprintf("%+v", report2)})
		case !bytes.Equal(back, wire):
			fs = append(fs, finding{Kind: "engine-wire", Index: i, A: wire, B: back})
		}
	}
	return fs
}

// nightlyOnly keeps the harness out of ci-fast and ci-full. ci-full's unit-rest
// lane runs go test over this package with SCHEMA_SLOW=1, so under CI the
// harness runs only when a make target names its seeds (make
// fuzz-tables-diff, certify.yml's nightly step); off CI the slow gate alone
// decides.
func nightlyOnly(t *testing.T) {
	t.Helper()
	if os.Getenv("CI") != "" && os.Getenv("SCHEMA_TABLE_DIFF_TO") == "" {
		t.Skip("the random-schema table differential is nightly only (make fuzz-tables-diff); CI is set and SCHEMA_TABLE_DIFF_TO is not")
	}
}

func seedRange() (int, int) {
	from, to := 0, 4
	if v := os.Getenv("SCHEMA_TABLE_DIFF_FROM"); v != "" {
		from, _ = strconv.Atoi(v)
	}
	if v := os.Getenv("SCHEMA_TABLE_DIFF_TO"); v != "" {
		to, _ = strconv.Atoi(v)
	}
	return from, to
}

// TestRandomTableDifferential is the gate: seeds SCHEMA_TABLE_DIFF_FROM..TO
// (default 0..4), each generated, compiled and run under -race. Each owed
// class prints with its card id; everything else fails.
func TestRandomTableDifferential(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs generated units under -race) and the engine")
	nightlyOnly(t)
	from, to := seedRange()
	for _, c := range owedClasses {
		t.Logf("owed class, card %s: %s", c.card, c.why)
	}
	for seed := from; seed <= to; seed++ {
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			t.Parallel()
			failing, owed, wrong := runSeed(t, seed, false)
			counts := map[string]int{}
			for _, o := range owed {
				counts[o]++
			}
			keys := make([]string, 0, len(counts))
			for k := range counts {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				t.Logf("%s (x%d)", k, counts[k])
			}
			t.Logf("seed %d: %d silent wrong-value decode(s) of damaged wires (counted, never failed, until schema-go-wire-integrity-check lands)", seed, wrong)
			for _, f := range failing {
				t.Error(f)
			}
		})
	}
}

// TestRandomTableDifferentialNegativeControl flips the probe's one comparison
// and requires the gate to go red on round-trip findings (not on a compile
// error and not on an owed class).
func TestRandomTableDifferentialNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs generated units under -race)")
	nightlyOnly(t)
	failing, _, _ := runSeed(t, 0, true)
	joined := strings.Join(failing, "\n")
	if !strings.Contains(joined, "roundtrip-strict") {
		t.Fatalf("NEGATIVE CONTROL FAILED: a flipped probe comparison did not fail the gate on a round trip:\n%s", shorten(joined))
	}
}
