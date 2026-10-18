package fuzz_test

// tablediff_test.go is the differential half of the random-schema harness
// (card schema-go-fuzz-random-table-differential). For each seed it builds a
// random VALID unit (tablegen_test.go), generates the Go leg with
// compiler.New().Generate(u, "go", nil), writes a probe beside it and runs the
// probe through the house runGeneratedUnit pattern under -race. The probe
// drives 24 values through every read and write path, damages each wire, plants
// hostile JSON and keeps a divergence list instead of stopping at the first
// one. This file then classifies those divergences against the expected-class
// table and runs the compiler's engine (tablepack.UnpackOneFile/Pack) in both
// directions.
//
// A divergence is a finding unless an open card already owns its class; the
// owning card's id is printed with every instance the harness suppresses.

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
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

// expectedClass is one class of divergence an open card already owns. The
// harness reports it with the card id and does not fail on it.
type expectedClass struct {
	id    string
	why   string
	match func(u *genUnit, line string) bool
}

var expectedClasses = []expectedClass{
	{
		id:  "schema-go-engine-timestamp-text",
		why: "the engine's text walk has no timestamp arm, so it writes the raw count (0) where the Go leg writes RFC 3339 (docs/SPEC-TABLES.md §2.11, §16)",
		match: func(u *genUnit, line string) bool {
			return u.hasTimestamp() && (strings.HasPrefix(line, "roundtrip") || strings.HasPrefix(line, "loadtext") || strings.HasPrefix(line, "engine"))
		},
	},
	{
		id:  "schema-go-plain-float-negative-zero",
		why: "a plain (non-compressed) float's -0 is written as 0 by the leg's wire read, so the round-trip text differs in the sign bit alone",
		match: func(u *genUnit, line string) bool {
			return u.hasFloat() && strings.HasPrefix(line, "roundtrip") && onlyNegativeZero(line)
		},
	},
	{
		id:  "schema-go-json-64-bit-range-clamp",
		why: "a 64-bit integer at the implied extreme is refused by FromJson (the JSON number walk clamps through a narrower carrier)",
		match: func(u *genUnit, line string) bool {
			if !strings.HasPrefix(line, "fromjson ") {
				return false
			}
			parts := strings.Fields(line)
			if len(parts) != 3 {
				return false
			}
			raw, err := base64.StdEncoding.DecodeString(parts[2])
			if err != nil {
				return false
			}
			s := string(raw)
			return strings.Contains(s, "9223372036854775807") || strings.Contains(s, "-9223372036854775808") || strings.Contains(s, "18446744073709551615")
		},
	},
	{
		id:  "schema-go-wire-integrity-check",
		why: "a damaged wire may decode to a silent wrong value until the wire integrity check lands; wrong-value decodes are counted and printed, never failed",
		match: func(u *genUnit, line string) bool {
			return strings.HasPrefix(line, "wrongvalue")
		},
	},
}

func (u *genUnit) hasTimestamp() bool {
	for _, f := range u.Fields {
		if fieldHasKind(f, gTimestamp) {
			return true
		}
	}
	return false
}

func (u *genUnit) hasFloat() bool {
	for _, f := range u.Fields {
		if fieldHasKind(f, gFloat32) || fieldHasKind(f, gFloat64) {
			return true
		}
	}
	return false
}

func fieldHasKind(f *genField, k genKind) bool {
	if f == nil {
		return false
	}
	if f.Kind == k {
		return true
	}
	return fieldHasKind(f.Elem, k)
}

// onlyNegativeZero reports whether two base64 payloads on a divergence line
// differ only by a -0/0 spelling.
func onlyNegativeZero(line string) bool {
	parts := strings.Fields(line)
	if len(parts) != 3 {
		return false
	}
	a, err1 := base64.StdEncoding.DecodeString(parts[1])
	b, err2 := base64.StdEncoding.DecodeString(parts[2])
	if err1 != nil || err2 != nil {
		return false
	}
	return string(a) != string(b) && string(bytes.ReplaceAll(a, []byte("-0"), []byte("0"))) == string(bytes.ReplaceAll(b, []byte("-0"), []byte("0")))
}

func tableUnit(t *testing.T, src string) *ir.Unit {
	t.Helper()
	ast, perrs := parser.Parse("probe.schema", []byte(src))
	if len(perrs) > 0 || ast == nil {
		t.Fatalf("parse: %v", perrs)
	}
	unit, cerrs := check.Unit([]check.SourceFile{{Path: "probe.schema", Name: "probe.schema", Base: "probe", Bytes: []byte(src), AST: ast}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs)
	}
	return unit
}

// runGeneratedTableUnit mirrors the house runGeneratedUnit pattern
// (internal/codegen/gotable/values_test.go:34) so the differential probe runs in
// a t.TempDir against the serialize.go replace line.
func runGeneratedTableUnit(t *testing.T, u *ir.Unit, testSource string, env map[string]string, flags ...string) ([]byte, error) {
	t.Helper()
	files, err := compiler.New().Generate(u, "go", nil)
	if err != nil {
		return nil, err
	}
	dir := t.TempDir()
	runtime, err := filepath.Abs("../../serialize.go")
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
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	args := append([]string{"test", "-count=1"}, flags...)
	cmd := exec.Command("go", append(args, ".")...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	return cmd.CombinedOutput()
}

var tableDifferentialProbe = `package probe

import (
	"encoding/base64"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func record(div *[]string, format string, args ...any) {
	*div = append(*div, fmt.Sprintf(format, args...))
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func onlyZero(a, b string) bool {
	return a != b && strings.ReplaceAll(a, "-0", "0") == strings.ReplaceAll(b, "-0", "0")
}

func TestProbe(t *testing.T) {
	dir := os.Getenv("SCHEMA_TABLE_DIFF_DIR")
	n, _ := strconv.Atoi(os.Getenv("SCHEMA_TABLE_DIFF_N"))
	sabotage := os.Getenv("SCHEMA_TABLE_DIFF_SABOTAGE") == "1"
	var div []string
	plants := []string{
		"null", "-0", "1e999", "NaN", "\"\\ud800\"", "\ufeff{}", "{} {}", "{\"a\":1,\"a\":2}",
		"{" + strings.Repeat("[", 100000) + strings.Repeat("]", 100000) + "}",
	}
	for i := 0; i < n; i++ {
		jb, err := os.ReadFile(dir + "/inst-" + strconv.Itoa(i) + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var v Root
		var report TableReport
		RootReset(&v)
		if !RootFromJson(&v, jb, &report) {
			record(&div, "fromjson %d %s", i, b64(jb))
			continue
		}
		tj := make([]byte, RootToJsonMeasure(&v))
		if RootToJson(&v, tj) != int64(len(tj)) {
			record(&div, "tojson %d", i)
		}
		if sabotage && i == 0 {
			tj = append(tj, ' ')
		}
		if err := os.WriteFile(dir+"/inst-"+strconv.Itoa(i)+".json", tj, 0o600); err != nil {
			t.Fatal(err)
		}
		var v2 Root
		RootReset(&v2)
		if RootFromJson(&v2, tj, &report) {
			tj2 := make([]byte, RootToJsonMeasure(&v2))
			if RootToJson(&v2, tj2) != int64(len(tj2)) || string(tj2) != string(tj) {
				record(&div, "fromjsontojson %d %s %s", i, b64(tj), b64(tj2))
			}
		} else {
			record(&div, "fromjsontojsonrefuse %d %s", i, b64(tj))
		}
		wire := make([]byte, RootMeasure(&v))
		if RootSave(&v, wire) != int64(len(wire)) {
			record(&div, "save %d", i)
		}
		if err := os.WriteFile(dir+"/inst-"+strconv.Itoa(i)+".wire", wire, 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := RootReadStrict(wire)
		if err != nil || got == nil {
			record(&div, "strictrefuse %d", i)
		} else {
			gj := make([]byte, RootToJsonMeasure(got))
			if RootToJson(got, gj) != int64(len(gj)) {
				record(&div, "stricttojson %d", i)
			}
			if string(gj) != string(tj) {
				record(&div, "roundtrip %s %s", b64(tj), b64(gj))
			}
		}
		st := base64.StdEncoding.EncodeToString(wire)
		if stxt, err := RootSaveText(&v); err != nil || stxt != st {
			record(&div, "savetext %s %s", st, stxt)
		}
		lt, err := RootLoadText(st, &report)
		if err != nil {
			record(&div, "loadtext %s %s", b64([]byte(st)), b64([]byte("error")))
		} else {
			lw := make([]byte, RootMeasure(lt))
			if RootSave(lt, lw) != int64(len(lw)) {
				record(&div, "loadtextsave %d", i)
			}
			if base64.StdEncoding.EncodeToString(lw) != st {
				record(&div, "loadtext %s %s", b64([]byte(st)), b64([]byte(base64.StdEncoding.EncodeToString(lw))))
			}
		}
		var loaded Root
		RootReset(&loaded)
		if !RootLoad(&loaded, wire, &report) {
			record(&div, "loadrefuse %d", i)
		}
		var wg sync.WaitGroup
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = RootReadStrict(wire)
			}()
		}
		wg.Wait()
		// damage: every truncation, every single-byte xor, then random splices/inserts
		var ops [][]byte
		for cut := 0; cut < len(wire); cut++ {
			ops = append(ops, append([]byte{}, wire[:cut]...))
		}
		for idx := 0; idx < len(wire); idx++ {
			for _, m := range []byte{0x01, 0x80, 0xff} {
				cp := append([]byte{}, wire...)
				cp[idx] ^= m
				ops = append(ops, cp)
			}
		}
		dr := rand.New(rand.NewSource(1))
		for k := 0; k < 100; k++ {
			cp := append([]byte{}, wire...)
			if len(wire) > 1 && dr.Intn(2) == 0 {
				a := dr.Intn(len(wire))
				b := a + dr.Intn(len(wire)-a)
				cp = append(cp[:a], cp[b:]...)
			} else if len(wire) > 1 {
				at := dr.Intn(len(wire))
				cp = append(cp[:at], append([]byte{byte(dr.Intn(256))}, cp[at:]...)...)
			}
			ops = append(ops, cp)
		}
		for _, d := range ops {
			func() {
				defer func() {
					if r := recover(); r != nil {
						record(&div, "damagepanic %v", r)
					}
				}()
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				var dv Root
				var dr TableReport
				RootReset(&dv)
				_ = RootLoad(&dv, d, &dr)
				_, _ = RootMeasureReason(&dv)
				_, _ = RootReadStrict(d)
				_, _ = RootLoadText(base64.StdEncoding.EncodeToString(d), &dr)
				runtime.ReadMemStats(&after)
				if len(d) < 4096 && after.TotalAlloc-before.TotalAlloc > 4<<20 {
					record(&div, "damagealloc %d", after.TotalAlloc-before.TotalAlloc)
				}
			}()
		}
		// hostile JSON plants, with the whole call under recover
		for _, plant := range plants {
			func() {
				defer func() {
					if r := recover(); r != nil {
						record(&div, "plantpanic %v", r)
					}
				}()
				var pv Root
				var pr TableReport
				RootReset(&pv)
				_ = RootFromJson(&pv, []byte(plant), &pr)
			}()
		}
	}
	if len(div) > 0 {
		_ = os.WriteFile(dir+"/divergences.txt", []byte(strings.Join(div, "\n")+"\n"), 0o600)
		t.Fatalf("%d divergence(s)", len(div))
	}
}
`

// runSeed generates, compiles and runs one seed, then classifies what the probe
// reported and compares the engine in both directions. It returns the
// unexpected divergences (which fail the seed) and a log of expected ones.
func runSeed(t *testing.T, r *rand.Rand, seed int, sabotage bool) (unexpected []string, expected []string) {
	t.Helper()
	src, u := randomSchema(r)
	unit := tableUnit(t, src)
	dir := t.TempDir()
	for i := 0; i < 24; i++ {
		j := randomJSON(r, u, i)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("inst-%d.json", i)), []byte(j), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	env := map[string]string{"SCHEMA_TABLE_DIFF_DIR": dir, "SCHEMA_TABLE_DIFF_N": "24"}
	if sabotage {
		env["SCHEMA_TABLE_DIFF_SABOTAGE"] = "1"
	}
	out, err := runGeneratedTableUnit(t, unit, tableDifferentialProbe, env, "-race")
	divBytes, _ := os.ReadFile(filepath.Join(dir, "divergences.txt"))
	skip := map[int]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(divBytes)), "\n") {
		if strings.HasPrefix(line, "fromjson ") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				if n, err := strconv.Atoi(f[1]); err == nil {
					skip[n] = true
				}
			}
		}
		if line == "" {
			continue
		}
		owned := false
		for _, c := range expectedClasses {
			if c.match(u, line) {
				expected = append(expected, fmt.Sprintf("seed %d: %s (%s): %s", seed, c.id, c.why, shorten(line)))
				owned = true
				break
			}
		}
		if !owned {
			unexpected = append(unexpected, fmt.Sprintf("seed %d: unexpected divergence: %s", seed, shorten(line)))
		}
	}
	if err != nil && len(divBytes) == 0 {
		unexpected = append(unexpected, fmt.Sprintf("seed %d: probe failed with no divergence report: %v\n%s", seed, err, out))
		return unexpected, expected
	}
	if len(unexpected) > 0 {
		return unexpected, expected
	}
	m := tabletext.NewModel(unit)
	for i := 0; i < 24; i++ {
		if skip[i] {
			continue
		}
		wire, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.wire", i)))
		if err != nil {
			unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: read wire: %v", seed, i, err))
			continue
		}
		legJSON, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.json", i)))
		if err != nil {
			unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: read text: %v", seed, i, err))
			continue
		}
		d1 := t.TempDir()
		report, err := tablepack.UnpackOneFile(m, "Root", wire, d1)
		if err != nil {
			if u.hasTimestamp() {
				expected = append(expected, fmt.Sprintf("seed %d inst %d: schema-go-engine-timestamp-text: UnpackOneFile: %v", seed, i, err))
			} else {
				unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: UnpackOneFile: %v", seed, i, err))
			}
			continue
		}
		if report != (tabletext.Report{}) {
			if u.hasTimestamp() {
				expected = append(expected, fmt.Sprintf("seed %d inst %d: schema-go-engine-timestamp-text: UnpackOneFile report %+v", seed, i, report))
			} else {
				unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: UnpackOneFile report not clean: %+v", seed, i, report))
			}
			continue
		}
		engineJSON, err := os.ReadFile(filepath.Join(d1, "Root.json"))
		if err != nil {
			unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: read engine text: %v", seed, i, err))
			continue
		}
		if !bytes.Equal(engineJSON, legJSON) {
			line := "engine " + base64.StdEncoding.EncodeToString(legJSON) + " " + base64.StdEncoding.EncodeToString(engineJSON)
			owned := false
			for _, c := range expectedClasses {
				if c.match(u, line) || (u.hasFloat() && c.id == "schema-go-plain-float-negative-zero" && onlyNegativeZero(line)) {
					expected = append(expected, fmt.Sprintf("seed %d inst %d: %s: engine text differs from leg text", seed, i, c.id))
					owned = true
					break
				}
			}
			if !owned {
				unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: unexpected engine/leg text divergence\nleg: %s\nengine: %s", seed, i, legJSON, engineJSON))
			}
			continue
		}
		d2 := t.TempDir()
		if err := os.WriteFile(filepath.Join(d2, "Root.json"), legJSON, 0o600); err != nil {
			t.Fatal(err)
		}
		wireBack, _, report2, err := tablepack.Pack(m, "Root", d2)
		if err != nil {
			if u.hasTimestamp() {
				expected = append(expected, fmt.Sprintf("seed %d inst %d: schema-go-engine-timestamp-text: Pack: %v", seed, i, err))
			} else {
				unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: Pack: %v", seed, i, err))
			}
			continue
		}
		if report2 != (tabletext.Report{}) {
			unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: Pack report not clean: %+v", seed, i, report2))
			continue
		}
		if !bytes.Equal(wireBack, wire) {
			unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: engine wire differs from leg wire", seed, i))
		}
	}
	return unexpected, expected
}

func shorten(s string) string {
	if len(s) > 220 {
		return s[:220] + "..."
	}
	return s
}

// TestRandomTableDifferential is the gate: seeds SCHEMA_TABLE_DIFF_FROM..TO
// (default 0..4), each generated, compiled and run under -race.
func TestRandomTableDifferential(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs generated units) and the engine")
	from, to := 0, 4
	if v := os.Getenv("SCHEMA_TABLE_DIFF_FROM"); v != "" {
		from, _ = strconv.Atoi(v)
	}
	if v := os.Getenv("SCHEMA_TABLE_DIFF_TO"); v != "" {
		to, _ = strconv.Atoi(v)
	}
	for _, c := range expectedClasses {
		t.Logf("expected class owned by %s: %s", c.id, c.why)
	}
	for seed := from; seed <= to; seed++ {
		seed := seed
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			t.Parallel()
			unexpected, expected := runSeed(t, rand.New(rand.NewSource(int64(seed))), seed, false)
			for _, e := range expected {
				t.Logf("EXPECTED %s", e)
			}
			for _, e := range unexpected {
				t.Errorf("%s", e)
			}
		})
	}
}

// TestRandomTableDifferentialNegativeControl flips one probe comparison and
// requires the gate to go red on an unexpected divergence, not on a compile
// error.
func TestRandomTableDifferentialNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs generated units)")
	unexpected, _ := runSeed(t, rand.New(rand.NewSource(0)), 0, true)
	if len(unexpected) == 0 {
		t.Fatalf("NEGATIVE CONTROL FAILED: a flipped probe comparison left the gate green")
	}
	if !strings.Contains(strings.Join(unexpected, "\n"), "unexpected divergence") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on an unexpected divergence: %v", unexpected)
	}
}

var _ = math.MaxInt8
