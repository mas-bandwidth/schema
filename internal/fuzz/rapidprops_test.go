package fuzz_test

// rapidprops_test.go is the property-based half of the random-schema harness
// (card schema-go-rapid-property-tests). It drives tablegen_test.go's existing
// randomSchema and randomJSON from pgregory.net/rapid: every math/rand draw is
// a rapid draw, so a failure shrinks to a smaller schema and a smaller value
// instead of stopping at the seed that found it.
//
// For each drawn unit the probe is compiled and run through
// runGeneratedTableUnit, exactly as the seeded differential does. The probe
// checks, over a batch of drawn values:
//
//	TestPropRoundTripEveryForm   load(save(v)) == v and re-saves byte-identically
//	                             for every form the unit declares (wire, text,
//	                             JSON, and the message form of a fixed root).
//	TestPropRefusalNeverSilent   every not-ok read of a drawn wire/JSON (and its
//	                             truncations and single-byte flips) carries a
//	                             non-empty report, and a tolerant read that is ok
//	                             with an all-zero report is accepted strictly.
//	TestPropStrictTolerantAgree  on a clean Save and a clean ToJson both reads
//	                             accept, give the same value and report clean.
//
// The property is expensive (it compiles a unit per check), so it is behind
// slowtest.Gate and the make target sets a bounded -rapid.checks. A default
// run caps rapid at a few checks so `go test ./internal/fuzz/` stays cheap.

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"

	"pgregory.net/rapid"
)

// TestMain caps rapid's default check count for this package's expensive
// properties; an explicit -rapid.checks (the make target passes one) wins. It
// also keeps a failing run from leaving a fail file in the tree; the seed is
// still printed as the replay hint.
func TestMain(m *testing.M) {
	if !flagWasSet("rapid.checks") {
		_ = flag.Set("rapid.checks", "24")
	}
	if !flagWasSet("rapid.nofailfile") {
		_ = flag.Set("rapid.nofailfile", "true")
	}
	os.Exit(m.Run())
}

func flagWasSet(name string) bool {
	set := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

// scramble diffuses the low-bit bias of a rapid Uint64 draw across all 64 bits
// (splitmix64's finalizer). math/rand's Intn/Int31n read the HIGH bits of
// Int63, and a raw rapid draw favours small values, so without this most
// generator choices land at 0 (the variable Root branch of tablegen and a
// one-field table), and the fixed form is almost never exercised. The mix is
// deterministic and maps the draw 0 to 0, so a draw shrunk to 0 still gives
// choice 0 and the shrunk case stays reproducible.
func scramble(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}

// rapidRand is a rand.Source whose every draw is a rapid draw, so rapid shrinks
// the schema and the values together.
type rapidRand struct {
	t *rapid.T
}

func (r rapidRand) Int63() int64 {
	return int64(scramble(rapid.Uint64().Draw(r.t, "rand")) >> 1)
}
func (r rapidRand) Seed(int64)     {}
func (r rapidRand) Uint64() uint64 { return scramble(rapid.Uint64().Draw(r.t, "rand")) }

// rapidUnit parses and checks a generated schema, failing the property with the
// source when the generator produced something the checker refuses.
func rapidUnit(rt *rapid.T, src string) *ir.Unit {
	ast, perrs := parser.Parse("probe.schema", []byte(src))
	if len(perrs) > 0 || ast == nil {
		rt.Fatalf("rapid: generated schema does not parse: %v\n%s", perrs, src)
	}
	unit, cerrs := check.Unit([]check.SourceFile{{
		Path: "probe.schema", Name: "probe.schema", Base: "probe",
		Bytes: []byte(src), AST: ast,
	}})
	if len(cerrs) > 0 {
		rt.Fatalf("rapid: generated schema does not check: %v\n%s", cerrs, src)
	}
	return unit
}

// rapidDrawUnit draws a schema and a batch of values from the one rapid stream.
func rapidDrawUnit(rt *rapid.T) (string, *ir.Unit, []string) {
	rng := rand.New(rapidRand{t: rt})
	src, u := randomSchema(rng)
	unit := rapidUnit(rt, src)
	n := 1 + rapid.IntRange(0, 2).Draw(rt, "values")
	vals := make([]string, n)
	for i := range vals {
		vals[i] = randomJSON(rng, u, i)
	}
	return src, unit, vals
}

// rapidProbeDir writes the drawn values where the generated probe reads them.
func rapidProbeDir(t *testing.T, vals []string) string {
	t.Helper()
	dir, err := os.MkdirTemp(t.TempDir(), "rapid")
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range vals {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("inst-%d.json", i)), []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// runRapidProbe compiles the drawn unit with the property probe and runs the
// named property over the drawn values.
func runRapidProbe(t *testing.T, rt *rapid.T, src string, unit *ir.Unit, vals []string, prop string, sabotage bool) {
	t.Helper()
	dir := rapidProbeDir(t, vals)
	env := map[string]string{
		"SCHEMA_RAPID_DIR":  dir,
		"SCHEMA_RAPID_N":    strconv.Itoa(len(vals)),
		"SCHEMA_RAPID_PROP": prop,
	}
	if sabotage {
		env["SCHEMA_RAPID_SABOTAGE"] = "1"
	}
	hasMessage := !ir.VariableTables(unit)["Root"]
	hasFixed := unit.Tables["Root"] != nil && ir.TableFixedEmitted(unit, unit.Tables["Root"])
	out, err := runGeneratedTableUnit(t, unit, rapidProbeSource(hasMessage, hasFixed), env, "-run", "^TestProbe$")
	if err != nil {
		rt.Fatalf("rapid: property %s failed\nschema (%d bytes):\n%s\nvalues:\n%s\nprobe:\n%s",
			prop, len(src), src, joinLines(vals), out)
	}
}

func joinLines(vals []string) string {
	out := ""
	for i, v := range vals {
		out += fmt.Sprintf("  value[%d]: %s\n", i, v)
	}
	return out
}

// rapidPropsGate keeps the property tests out of ci-fast and ci-full: they
// compile a unit per check, so they are a nightly campaign. The make target and
// an explicit SCHEMA_RAPID_PROPS=1 opt them in.
func rapidPropsGate(t *testing.T) {
	t.Helper()
	if os.Getenv("CI") != "" && os.Getenv("SCHEMA_RAPID_PROPS") != "1" {
		t.Skip("the property-based random-schema tests are nightly only; run them with make fuzz-tables-props")
	}
}

// rapidDrawnFixed reports whether the drawn unit emits the fixed form: the
// `fixed table Root` wire form (and, for a fixed Root, the message form). The
// properties only exercise the forms the unit declares, so they count fixed
// Roots over the checks and refuse to pass without seeing any: the fixed leg
// must never be dead code in this campaign.
func rapidDrawnFixed(unit *ir.Unit) bool {
	return unit.Tables["Root"] != nil && ir.TableFixedEmitted(unit, unit.Tables["Root"])
}

// coverageFloor fails the test when a counter that the property's checks
// incremented never moved: the property ran, but never exercised the form the
// counter tracks (the fixed Root), so it proved nothing about that leg.
func coverageFloor(t *testing.T, what string, count int) {
	t.Helper()
	if count == 0 {
		t.Fatalf("coverage floor: %s was never exercised over the checks", what)
	}
}

// TestPropRoundTripEveryForm: every form load(save(v)) == v and re-saves the
// same bytes.
func TestPropRoundTripEveryForm(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	rapidPropsGate(t)
	fixed := 0
	rapid.Check(t, func(rt *rapid.T) {
		src, unit, vals := rapidDrawUnit(rt)
		if rapidDrawnFixed(unit) {
			fixed++
		}
		runRapidProbe(t, rt, src, unit, vals, "roundtrip", false)
	})
	coverageFloor(t, "the fixed Root form", fixed)
}

// TestPropRefusalNeverSilent: a refused input never comes back ok with an empty
// report, and a tolerant read that is ok with an all-zero report is accepted by
// the strict read too.
func TestPropRefusalNeverSilent(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	rapidPropsGate(t)
	fixed := 0
	rapid.Check(t, func(rt *rapid.T) {
		src, unit, vals := rapidDrawUnit(rt)
		if rapidDrawnFixed(unit) {
			fixed++
		}
		runRapidProbe(t, rt, src, unit, vals, "refusal", false)
	})
	coverageFloor(t, "the fixed Root form", fixed)
}

// TestPropStrictTolerantAgree: on a clean Save and a clean ToJson the strict
// and tolerant reads both accept, give the same value and both report clean.
func TestPropStrictTolerantAgree(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	rapidPropsGate(t)
	fixed := 0
	rapid.Check(t, func(rt *rapid.T) {
		src, unit, vals := rapidDrawUnit(rt)
		if rapidDrawnFixed(unit) {
			fixed++
		}
		runRapidProbe(t, rt, src, unit, vals, "agree", false)
	})
	coverageFloor(t, "the fixed Root form", fixed)
}

// rapidRecorder is a rapid.TB that records a check's failure instead of failing
// the enclosing test, so the negative control can require the check to fail.
type rapidRecorder struct {
	mu     sync.Mutex
	failed bool
	text   strings.Builder
}

func (r *rapidRecorder) Helper()      {}
func (r *rapidRecorder) Name() string { return "TestRapidPropsNegativeControl" }
func (r *rapidRecorder) SkipNow()     {}
func (r *rapidRecorder) FailNow()     { r.setFailed() }
func (r *rapidRecorder) Fail()        { r.setFailed() }
func (r *rapidRecorder) Failed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.failed
}

func (r *rapidRecorder) Logf(format string, args ...any) {
	r.record(fmt.Sprintf(format, args...))
}

func (r *rapidRecorder) Log(args ...any) { r.record(fmt.Sprint(args...)) }

func (r *rapidRecorder) Skipf(format string, args ...any) {
	r.record(fmt.Sprintf(format, args...))
}

func (r *rapidRecorder) Skip(args ...any) { r.record(fmt.Sprint(args...)) }

func (r *rapidRecorder) Errorf(format string, args ...any) {
	r.fail(fmt.Sprintf(format, args...))
}

func (r *rapidRecorder) Error(args ...any) { r.fail(fmt.Sprint(args...)) }

func (r *rapidRecorder) Fatalf(format string, args ...any) {
	r.fail(fmt.Sprintf(format, args...))
}

func (r *rapidRecorder) Fatal(args ...any) { r.fail(fmt.Sprint(args...)) }

func (r *rapidRecorder) record(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.text.WriteString(s)
	r.text.WriteByte('\n')
}

func (r *rapidRecorder) fail(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed = true
	r.text.WriteString(s)
	r.text.WriteByte('\n')
}

func (r *rapidRecorder) setFailed() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failed = true
}

func (r *rapidRecorder) output() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.text.String()
}

var shrunkSchemaRE = regexp.MustCompile(`schema \((\d+) bytes\)`)

// shrunkSchemaBytes returns the smallest schema size named in a recorded rapid
// failure, or -1 when the failure names none.
func shrunkSchemaBytes(out string) int {
	best := -1
	for _, m := range shrunkSchemaRE.FindAllStringSubmatch(out, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if best < 0 || n < best {
			best = n
		}
	}
	return best
}

// maxShrunkSchemaBytes bounds the shrunk schema the negative control requires.
// A schema that cannot shrink below this is not shrinking, it is a probe or
// compile failure that merely happens to be large.
const maxShrunkSchemaBytes = 512

// TestRapidPropsNegativeControl flips one probe comparison. It runs rapid's
// check against a recording TB, then requires both a failure and a shrunk
// schema under maxShrunkSchemaBytes.
func TestRapidPropsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	rapidPropsGate(t)
	rec := &rapidRecorder{}
	rapid.Check(rec, func(rt *rapid.T) {
		src, unit, vals := rapidDrawUnit(rt)
		runRapidProbe(t, rt, src, unit, vals, "roundtrip", true)
	})
	if !rec.Failed() {
		t.Fatalf("NEGATIVE CONTROL FAILED: flipping one probe comparison left rapid's check green")
	}
	out := rec.output()
	if !strings.Contains(out, "negative control: flipped comparison") {
		t.Fatalf("NEGATIVE CONTROL FAILED: rapid failed, but not on the flipped comparison:\n%s", out)
	}
	size := shrunkSchemaBytes(out)
	if size < 0 {
		t.Fatalf("NEGATIVE CONTROL FAILED: the shrunk case names no schema size:\n%s", out)
	}
	t.Logf("negative control: rapid failed and shrank the schema to %d bytes", size)
	if size > maxShrunkSchemaBytes {
		t.Fatalf("NEGATIVE CONTROL FAILED: the shrunk schema is %d bytes, want at most %d (rapid did not shrink):\n%s", size, maxShrunkSchemaBytes, out)
	}
}

// rapidProbeSource is the generated probe's values_test.go. It reuses the
// helpers_test.go runGeneratedTableUnit already writes (probeFromJSON,
// probeFromJSONTolerant, probeLoad, probeLoadTolerant).
func rapidProbeSource(hasMessage, hasFixed bool) string {
	msgFuncs := ""
	msgRoundTrip := ""
	msgRefusal := ""
	msgAgree := ""
	fixedRoundTrip := ""
	if hasFixed {
		fixedRoundTrip = rapidFixedRoundTrip
	}
	if hasMessage {
		msgFuncs = rapidMessageFuncs
		msgAgree = rapidMessageAgree
		msgRoundTrip = `
		mbuf, vocab, err := rapidMessage(t, v)
		if err != nil {
			t.Fatalf("value %d: %v", i, err)
		}
		mj, mok, mrep := rapidMessageLoad(mbuf, &vocab, false)
		if !mok {
			t.Fatalf("value %d: message strict refused: %+v", i, mrep)
		}
		if sabotage {
			if mj == tj {
				t.Fatalf("negative control: flipped comparison: value %d message round-tripped identically", i)
			}
		} else if mj != tj {
			t.Fatalf("value %d: message round trip JSON differs\nwant %s\ngot  %s", i, tj, mj)
		}
		if mal, merr := rapidMessageSave(t, mj); merr != nil || mal != string(mbuf) {
			t.Fatalf("value %d: message re-save differs: %v", i, merr)
		}
`
		msgRefusal = `
		mbuf, vocab, err := rapidMessage(t, v)
		if err != nil {
			t.Fatalf("value %d: %v", i, err)
		}
		for _, op := range rapidDamage(mbuf) {
			_, sok, srep := rapidMessageLoad(op, &vocab, false)
			if !sok && srep.Err() == nil && srep == (TableReport{}) {
				t.Fatalf("value %d: message strict refused silently:%x", i, op)
			}
			_, tok, trep := rapidMessageLoad(op, &vocab, true)
			if tok && trep == (TableReport{}) {
				if _, sok2, _ := rapidMessageLoad(op, &vocab, false); !sok2 {
					t.Fatalf("value %d: message tolerant ok clean but strict refused: %x", i, op)
				}
			}
		}
`
	}
	return `package probe

import (
	"fmt"
	"os"
	"strconv"
	"testing"
)

func rapidJSON(v *Root) (string, error) {
	n := RootToJsonMeasure(v)
	if n < 0 {
		return "", fmt.Errorf("ToJsonMeasure %d", n)
	}
	b := make([]byte, n)
	if got := RootToJson(v, b); got != n {
		return "", fmt.Errorf("ToJson %d want %d", got, n)
	}
	return string(b), nil
}

func rapidWire(v *Root) ([]byte, error) {
	n := RootMeasure(v)
	if n < 0 {
		return nil, fmt.Errorf("Measure %d", n)
	}
	b := make([]byte, n)
	if got := RootSave(v, b); got != n {
		return nil, fmt.Errorf("Save %d want %d", got, n)
	}
	return b, nil
}

func rapidDamage(in []byte) [][]byte {
	var ops [][]byte
	for cut := 0; cut < len(in); cut++ {
		ops = append(ops, append([]byte{}, in[:cut]...))
	}
	for i := 0; i < len(in); i++ {
		for _, m := range []byte{0x01, 0x80, 0xff} {
			cp := append([]byte{}, in...)
			cp[i] ^= m
			ops = append(ops, cp)
		}
	}
	return ops
}

func rapidFromJSON(t *testing.T, jb []byte) (*Root, string, error) {
	var rep TableReport
	v, ok := probeFromJSON(jb, &rep)
	if !ok || v == nil {
		return nil, "", fmt.Errorf("FromJson refused: %+v", rep)
	}
	js, err := rapidJSON(v)
	return v, js, err
}

func testRoundTrip(t *testing.T, vals [][]byte) {
	sabotage := os.Getenv("SCHEMA_RAPID_SABOTAGE") == "1"
	for i, jb := range vals {
		v, tj, err := rapidFromJSON(t, jb)
		if err != nil {
			t.Fatalf("value %d: %v\ntext: %s", i, err, jb)
		}
		wire, err := rapidWire(v)
		if err != nil {
			t.Fatalf("value %d: %v", i, err)
		}
		var rep TableReport
		sv, ok := probeLoad(wire, &rep)
		if !ok || sv == nil {
			t.Fatalf("value %d: wire strict refused a clean Save: %+v", i, rep)
		}
		gj, err := rapidJSON(sv)
		if err != nil {
			t.Fatalf("value %d: %v", i, err)
		}
		if sabotage {
			if gj == tj {
				t.Fatalf("negative control: flipped comparison: value %d wire round-tripped identically", i)
			}
		} else if gj != tj {
			t.Fatalf("value %d: wire JSON differs\nwant %s\ngot  %s", i, tj, gj)
		}
		gw, err := rapidWire(sv)
		if err != nil || string(gw) != string(wire) {
			t.Fatalf("value %d: wire re-save differs: %v", i, err)
		}
		var trep TableReport
		tv, tok := probeLoadTolerant(wire, &trep)
		if !tok || tv == nil {
			t.Fatalf("value %d: wire tolerant refused a clean Save: %+v", i, trep)
		}
		if tj2, _ := rapidJSON(tv); tj2 != tj {
			t.Fatalf("value %d: wire tolerant JSON differs\nwant %s\ngot  %s", i, tj, tj2)
		}
		txt, err := RootSaveText(v)
		if err != nil {
			t.Fatalf("value %d: SaveText: %v", i, err)
		}
		lv, err := RootLoadText(txt, &rep)
		if err != nil || lv == nil {
			t.Fatalf("value %d: LoadText: %v %+v", i, err, rep)
		}
		lj, err := rapidJSON(lv)
		if err != nil || lj != tj {
			t.Fatalf("value %d: text JSON differs: %v\nwant %s\ngot  %s", i, err, tj, lj)
		}
		txt2, err := RootSaveText(lv)
		if err != nil || txt2 != txt {
			t.Fatalf("value %d: text re-save differs: %v", i, err)
		}
		fv, ok := probeFromJSON([]byte(tj), &rep)
		if !ok || fv == nil {
			t.Fatalf("value %d: strict FromJson refused canonical text: %+v", i, rep)
		}
		fj, err := rapidJSON(fv)
		if err != nil || fj != tj {
			t.Fatalf("value %d: JSON round trip differs: %v\nwant %s\ngot  %s", i, err, tj, fj)
		}
		fw, err := rapidWire(fv)
		if err != nil || string(fw) != string(wire) {
			t.Fatalf("value %d: JSON round trip wire differs: %v", i, err)
		}
		var tfrep TableReport
		tfv, tok := probeFromJSONTolerant([]byte(tj), &tfrep)
		if !tok || tfv == nil {
			t.Fatalf("value %d: tolerant FromJson refused canonical text: %+v", i, tfrep)
		}
		if tfj, _ := rapidJSON(tfv); tfj != tj {
			t.Fatalf("value %d: tolerant JSON differs\nwant %s\ngot  %s", i, tj, tfj)
		}
` + fixedRoundTrip + msgRoundTrip + `
	}
}

func testRefusal(t *testing.T, vals [][]byte) {
	for i, jb := range vals {
		v, tj, err := rapidFromJSON(t, jb)
		if err != nil {
			t.Fatalf("value %d: %v\ntext: %s", i, err, jb)
		}
		wire, err := rapidWire(v)
		if err != nil {
			t.Fatalf("value %d: %v", i, err)
		}
		for _, op := range rapidDamage(wire) {
			var rep TableReport
			_, ok := probeLoad(op, &rep)
			if !ok && rep.Err() == nil && rep == (TableReport{}) {
				t.Fatalf("value %d: wire strict refused silently: %x", i, op)
			}
			var trep TableReport
			_, tok := probeLoadTolerant(op, &trep)
			if tok && trep == (TableReport{}) {
				var srep TableReport
				if sv, sok := probeLoad(op, &srep); !sok || sv == nil {
					t.Fatalf("value %d: wire tolerant ok clean but strict refused: %x report=%+v", i, op, srep)
				}
			}
		}
		for _, op := range rapidDamage([]byte(tj)) {
			var rep TableReport
			_, ok := probeFromJSON(op, &rep)
			if !ok && rep.Err() == nil && rep == (TableReport{}) {
				t.Fatalf("value %d: JSON strict refused silently: %s", i, op)
			}
			var trep TableReport
			_, tok := probeFromJSONTolerant(op, &trep)
			if tok && trep == (TableReport{}) {
				var srep TableReport
				if sv, sok := probeFromJSON(op, &srep); !sok || sv == nil {
					t.Fatalf("value %d: JSON tolerant ok clean but strict refused: %s report=%+v", i, op, srep)
				}
			}
		}
` + msgRefusal + `
	}
}

func testAgree(t *testing.T, vals [][]byte) {
	for i, jb := range vals {
		v, tj, err := rapidFromJSON(t, jb)
		if err != nil {
			t.Fatalf("value %d: %v\ntext: %s", i, err, jb)
		}
		wire, err := rapidWire(v)
		if err != nil {
			t.Fatalf("value %d: %v", i, err)
		}
		var srep, trep TableReport
		sv, sok := probeLoad(wire, &srep)
		tv, tok := probeLoadTolerant(wire, &trep)
		if !sok || !tok {
			t.Fatalf("value %d: wire strict=%v tolerant=%v reports %+v %+v", i, sok, tok, srep, trep)
		}
		if srep != (TableReport{}) || trep != (TableReport{}) {
			t.Fatalf("value %d: wire clean reads reported %+v %+v", i, srep, trep)
		}
		sj, _ := rapidJSON(sv)
		tj2, _ := rapidJSON(tv)
		if sj != tj || tj2 != tj {
			t.Fatalf("value %d: wire strict/tolerant values differ\nwant %s\nstrict %s\ntolerant %s", i, tj, sj, tj2)
		}
		var fsrep, ftrep TableReport
		fv, fok := probeFromJSON([]byte(tj), &fsrep)
		tfv, ftok := probeFromJSONTolerant([]byte(tj), &ftrep)
		if !fok || !ftok {
			t.Fatalf("value %d: JSON strict=%v tolerant=%v reports %+v %+v", i, fok, ftok, fsrep, ftrep)
		}
		if fsrep != (TableReport{}) || ftrep != (TableReport{}) {
			t.Fatalf("value %d: JSON clean reads reported %+v %+v", i, fsrep, ftrep)
		}
		fj, _ := rapidJSON(fv)
		tfj, _ := rapidJSON(tfv)
		if fj != tj || tfj != tj {
			t.Fatalf("value %d: JSON strict/tolerant values differ\nwant %s\nstrict %s\ntolerant %s", i, tj, fj, tfj)
		}
` + msgAgree + `
	}
}

func TestProbe(t *testing.T) {
	dir := os.Getenv("SCHEMA_RAPID_DIR")
	n, _ := strconv.Atoi(os.Getenv("SCHEMA_RAPID_N"))
	vals := make([][]byte, n)
	for i := range vals {
		b, err := os.ReadFile(dir + "/inst-" + strconv.Itoa(i) + ".json")
		if err != nil {
			t.Fatal(err)
		}
		vals[i] = b
	}
	switch os.Getenv("SCHEMA_RAPID_PROP") {
	case "roundtrip":
		testRoundTrip(t, vals)
	case "refusal":
		testRefusal(t, vals)
	case "agree":
		testAgree(t, vals)
	default:
		t.Fatalf("unknown property %q", os.Getenv("SCHEMA_RAPID_PROP"))
	}
}
` + msgFuncs
}

// rapidFixedRoundTrip is spliced into testRoundTrip only when the unit emits
// the fixed form (ir.TableFixedEmitted): load(save(v)) gives v back and the
// re-save is byte-identical.
var rapidFixedRoundTrip = `
		fb := make([]byte, RootFixedMeasure(1))
		if n := RootFixedSave([]Root{*v}, fb); n != int64(len(fb)) {
			t.Fatalf("value %d: fixed Save %d want %d", i, n, len(fb))
		}
		fback := make([]Root, 1)
		plan := make([]TableFixedEntry, 4096)
		var frep TableReport
		if n := RootFixedLoad(fback, fb, plan, &frep); n != 1 || frep != (TableReport{}) {
			t.Fatalf("value %d: fixed Load %d report %+v", i, n, frep)
		}
		ffj, ferr := rapidJSON(&fback[0])
		if ferr != nil {
			t.Fatalf("value %d: fixed load JSON: %v", i, ferr)
		}
		if sabotage {
			if ffj == tj {
				t.Fatalf("negative control: flipped comparison: value %d fixed round-tripped identically", i)
			}
		} else if ffj != tj {
			t.Fatalf("value %d: fixed round trip JSON differs\nwant %s\ngot  %s", i, tj, ffj)
		}
		fb2 := make([]byte, RootFixedMeasure(1))
		if n := RootFixedSave(fback, fb2); n != int64(len(fb2)) || string(fb2) != string(fb) {
			t.Fatalf("value %d: fixed re-save differs: n=%d", i, n)
		}
`

// rapidMessageAgree is spliced into testAgree only for a fixed Root: a clean
// message read is strict and tolerant ok, both report clean, and both give the
// same value.
var rapidMessageAgree = `
		mbuf, vocab, err := rapidMessage(t, v)
		if err != nil {
			t.Fatalf("value %d: %v", i, err)
		}
		mj, mok, mrep := rapidMessageLoad(mbuf, &vocab, false)
		if !mok {
			t.Fatalf("value %d: message strict refused a clean message: %+v", i, mrep)
		}
		mtj, mtok, mtrep := rapidMessageLoad(mbuf, &vocab, true)
		if !mtok {
			t.Fatalf("value %d: message tolerant refused a clean message: %+v", i, mtrep)
		}
		if mrep != (TableReport{}) || mtrep != (TableReport{}) {
			t.Fatalf("value %d: message clean reads reported %+v %+v", i, mrep, mtrep)
		}
		if mj != tj || mtj != tj {
			t.Fatalf("value %d: message strict/tolerant values differ\nwant %s\nstrict %s\ntolerant %s", i, tj, mj, mtj)
		}
`

var rapidMessageFuncs = `
func rapidMessage(t *testing.T, v *Root) ([]byte, TableVocabulary, error) {
	values := []*Root{v}
	n := RootMeasureMessages(values, nil)
	if n < 0 {
		return nil, TableVocabulary{}, fmt.Errorf("MeasureMessages %d", n)
	}
	buf := make([]byte, n)
	var wrep TableReport
	if got := RootSaveMessages(values, buf, &wrep); got != n {
		return nil, TableVocabulary{}, fmt.Errorf("SaveMessages %d want %d %+v", got, n, wrep)
	}
	ann := make([]byte, AnnounceMeasure())
	Announce(ann)
	var vocab TableVocabulary
	vocab.Init(make([]TableMessageEntry, TableMessageEntriesHere))
	var arep TableReport
	if !AnnounceRead(&vocab, ann, &arep) {
		return nil, vocab, fmt.Errorf("announce refused: %+v", arep)
	}
	return buf, vocab, nil
}

func rapidMessageLoad(buf []byte, vocab *TableVocabulary, tolerant bool) (string, bool, TableReport) {
	got := make([]Root, 1)
	var rep TableReport
	var ok bool
	if tolerant {
		_, ok = RootLoadMessagesTolerant(got, vocab, buf, &rep)
	} else {
		_, ok = RootLoadMessages(got, vocab, buf, &rep)
	}
	if !ok {
		return "", false, rep
	}
	js, err := rapidJSON(&got[0])
	if err != nil {
		return "", false, rep
	}
	return js, true, rep
}

func rapidMessageSave(t *testing.T, js string) (string, error) {
	var v Root
	var rep TableReport
	if !RootFromJson(&v, []byte(js), &rep) {
		return "", fmt.Errorf("FromJson %+v", rep)
	}
	values := []*Root{&v}
	n := RootMeasureMessages(values, nil)
	if n < 0 {
		return "", fmt.Errorf("MeasureMessages %d", n)
	}
	buf := make([]byte, n)
	if got := RootSaveMessages(values, buf, &rep); got != n {
		return "", fmt.Errorf("SaveMessages %d want %d", got, n)
	}
	return string(buf), nil
}
`
