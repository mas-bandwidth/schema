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
	"strconv"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"

	"pgregory.net/rapid"
)

// TestMain caps rapid's default check count for this package's expensive
// properties; an explicit -rapid.checks (the make target passes one) wins.
func TestMain(m *testing.M) {
	set := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "rapid.checks" {
			set = true
		}
	})
	if !set {
		_ = flag.Set("rapid.checks", "8")
	}
	os.Exit(m.Run())
}

// rapidRand is a rand.Source whose every draw is a rapid draw, so rapid shrinks
// the schema and the values together.
type rapidRand struct {
	t *rapid.T
}

func (r rapidRand) Int63() int64   { return int64(rapid.Uint64().Draw(r.t, "rand") >> 1) }
func (r rapidRand) Seed(int64)     {}
func (r rapidRand) Uint64() uint64 { return rapid.Uint64().Draw(r.t, "rand") }

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
	n := 1 + int(rapid.IntRange(0, 2).Draw(rt, "values"))
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
	out, err := runGeneratedTableUnit(t, unit, rapidProbeSource(hasMessage), env, "-run", "^TestProbe$")
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

// TestPropRoundTripEveryForm: every form load(save(v)) == v and re-saves the
// same bytes.
func TestPropRoundTripEveryForm(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	rapidPropsGate(t)
	rapid.Check(t, func(rt *rapid.T) {
		src, unit, vals := rapidDrawUnit(rt)
		runRapidProbe(t, rt, src, unit, vals, "roundtrip", false)
	})
}

// TestPropRefusalNeverSilent: a refused input never comes back ok with an empty
// report, and a tolerant read that is ok with an all-zero report is accepted by
// the strict read too.
func TestPropRefusalNeverSilent(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	rapidPropsGate(t)
	rapid.Check(t, func(rt *rapid.T) {
		src, unit, vals := rapidDrawUnit(rt)
		runRapidProbe(t, rt, src, unit, vals, "refusal", false)
	})
}

// TestPropStrictTolerantAgree: on a clean Save and a clean ToJson the strict
// and tolerant reads both accept, give the same value and both report clean.
func TestPropStrictTolerantAgree(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	rapidPropsGate(t)
	rapid.Check(t, func(rt *rapid.T) {
		src, unit, vals := rapidDrawUnit(rt)
		runRapidProbe(t, rt, src, unit, vals, "agree", false)
	})
}

// TestRapidPropsNegativeControl flips one probe comparison. It must fail and
// rapid must shrink the drawn schema; run it with a small -rapid.checks.
func TestRapidPropsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	rapidPropsGate(t)
	rapid.Check(t, func(rt *rapid.T) {
		src, unit, vals := rapidDrawUnit(rt)
		runRapidProbe(t, rt, src, unit, vals, "roundtrip", true)
	})
}

// rapidProbeSource is the generated probe's values_test.go. It reuses the
// helpers_test.go runGeneratedTableUnit already writes (probeFromJSON,
// probeFromJSONTolerant, probeLoad, probeLoadTolerant).
func rapidProbeSource(hasMessage bool) string {
	msgFuncs := ""
	msgRoundTrip := ""
	msgRefusal := ""
	if hasMessage {
		msgFuncs = rapidMessageFuncs
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
` + msgRoundTrip + `
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
