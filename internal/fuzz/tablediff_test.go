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
	"encoding/binary"
	"fmt"
	"hash/crc32"
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

// The ids below are looked up with `nova-sprint card <id>`
// (verifiedClassCards in tablediff_classes_test.go pins that list): a class
// whose owner cannot be found is not owed, it is a finding.
var expectedClasses = []expectedClass{}

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

// TestRandomTableGenerator pins the generator's contract over seeds 0..399:
// every generated unit passes the checker, integrity is declared only on a
// variable table Root (never a `fixed table` Root), and the census over seeds
// 0..399 covers both units with `integrity` and units without.
func TestRandomTableGenerator(t *testing.T) {
	t.Parallel()
	census := map[string]int{}
	for seed := range 400 {
		src, u := randomSchema(rand.New(rand.NewSource(int64(seed))))
		tableUnit(t, src)
		if u.Root.Integrity != "" {
			if u.Root.Fixed {
				t.Fatalf("seed %d: fixed table Root declared integrity = %q", seed, u.Root.Integrity)
			}
			if !strings.Contains(src, `table Root | integrity = "crc32c"`) {
				t.Fatalf("seed %d: integrity unit source missing `table Root | integrity = \"crc32c\"`:\n%s", seed, src)
			}
		}
		for k := range u.Census() {
			census[k]++
		}
	}
	for _, want := range []string{
		"integrity", "no integrity", "fixed Root", "variable Root",
		"inner table", "union", "int", "uint", "float32", "float64",
		"bool", "enum", "string(N)", "bytes(N)", "timestamp",
		"[..N]T", "[N]T", "?T", "nested table",
		"range mode 0", "range mode 1", "range mode 2", "range mode 3",
	} {
		if census[want] == 0 {
			t.Errorf("no unit over seeds 0..399 holds %s", want)
		}
	}
}

func probeHelpersSource(u *ir.Unit) string {
	st := u.Tables["Root"]
	hasIntegrity := st != nil && st.Integrity != ""
	if ir.VariableTables(u)["Root"] {
		return fmt.Sprintf(`package probe

import "unsafe"

const hasIntegrity = %v
const hasLoadMeasure = true

func probeFromJSON(text []byte, rep *TableReport) (*Root, bool) {
	var b RootBuilder
	if !b.initSized(1) {
		return nil, false
	}
	if !RootFromJSON(&b, text, rep) {
		b.Shutdown()
		return nil, false
	}
	if !b.Lock() {
		b.Shutdown()
		return nil, false
	}
	return b.AsConst(), true
}

func probeFromJSONValue(v *Root, text []byte, rep *TableReport) bool {
	var b RootBuilder
	if !b.initSized(1) {
		return false
	}
	defer b.Shutdown()
	ok := RootFromJSON(&b, text, rep)
	if !ok {
		if b.GetRoot() != nil {
			*v = *b.GetRoot()
		}
		return false
	}
	if !b.Lock() {
		return false
	}
	*v = *b.AsConst()
	return true
}

func probeFromJSONTolerant(text []byte, rep *TableReport) (*Root, bool) {
	var b RootBuilder
	if !b.initSized(1) {
		return nil, false
	}
	if !RootFromJSONTolerant(&b, text, rep) {
		b.Shutdown()
		return nil, false
	}
	if !b.Lock() {
		b.Shutdown()
		return nil, false
	}
	return b.AsConst(), true
}

func probeLoad(wire []byte, rep *TableReport) (*Root, bool) {
	n := RootLoadMeasure(wire)
	var region []byte
	if n > 0 {
		raw := make([]byte, n+64)
		off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
		region = raw[off : off+uintptr(n)]
	}
	v := RootLoad(region, wire, rep)
	return v, v != nil
}

func probeLoadTolerant(wire []byte, rep *TableReport) (*Root, bool) {
	n := RootLoadMeasure(wire)
	var region []byte
	if n > 0 {
		raw := make([]byte, n+64)
		off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
		region = raw[off : off+uintptr(n)]
	}
	v := RootLoadTolerant(region, wire, rep)
	return v, v != nil
}

func probeLoadBuilderTolerant(wire []byte, rep *TableReport) (*Root, bool) {
	n := RootLoadMeasure(wire)
	if n < 0 {
		n = 0
	}
	if !probeScratchBuilder.initSized(n) {
		return nil, false
	}
	if !RootLoadBuilderTolerant(&probeScratchBuilder, wire, rep) {
		probeScratchBuilder.Shutdown()
		return nil, false
	}
	if !probeScratchBuilder.Lock() {
		probeScratchBuilder.Shutdown()
		return nil, false
	}
	v := probeScratchBuilder.AsConst()
	probeScratchBuilder.Shutdown()
	return v, true
}

var probeScratchBuilder RootBuilder

func probeLoadMeasureReason(wire []byte) (int64, error) {
	return RootLoadMeasureReason(wire)
}
`, hasIntegrity)
	}
	return fmt.Sprintf(`package probe

const hasIntegrity = %v
const hasLoadMeasure = false

func probeFromJSON(text []byte, rep *TableReport) (*Root, bool) {
	v := new(Root)
	RootReset(v)
	ok := RootFromJSON(v, text, rep)
	return v, ok
}

func probeFromJSONValue(v *Root, text []byte, rep *TableReport) bool {
	return RootFromJSON(v, text, rep)
}

func probeFromJSONTolerant(text []byte, rep *TableReport) (*Root, bool) {
	v := new(Root)
	RootReset(v)
	ok := RootFromJSONTolerant(v, text, rep)
	return v, ok
}

func probeLoad(wire []byte, rep *TableReport) (*Root, bool) {
	v := new(Root)
	RootReset(v)
	ok := RootLoad(v, wire, rep)
	return v, ok
}

func probeLoadTolerant(wire []byte, rep *TableReport) (*Root, bool) {
	v := new(Root)
	RootReset(v)
	ok := RootLoadTolerant(v, wire, rep)
	return v, ok
}

func probeLoadBuilderTolerant(wire []byte, rep *TableReport) (*Root, bool) {
	return probeLoadTolerant(wire, rep)
}

func probeLoadMeasureReason(wire []byte) (int64, error) {
	return 0, nil
}
`, hasIntegrity)
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
	root, err := filepath.Abs("../..")
	if err != nil {
		return nil, err
	}
	if ir.VariableTables(u)["Root"] && !strings.Contains(testSource, "probeFromJSON") && !strings.Contains(testSource, "RootBuilder") {
		testSource = strings.ReplaceAll(testSource, "RootFromJSON(", "probeFromJSONValue(")
	}
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/schema/v2 v2.0.0\nreplace github.com/mas-bandwidth/schema/v2 => %q\n", root))
	files["helpers_test.go"] = []byte(probeHelpersSource(u))
	files["values_test.go"] = []byte(testSource)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			return nil, err
		}
	}
	if !tableDiffOptedIn(os.Getenv) {
		slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated unit)")
	}
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
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
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

func damageOps(wire []byte, fullFlips bool) [][]byte {
	var ops [][]byte
	// 1. Every truncation shorter than the saved file.
	for cut := 0; cut < len(wire); cut++ {
		ops = append(ops, append([]byte{}, wire[:cut]...))
	}
	// 2. Single-byte flips across every byte of the saved file.
	masks := []byte{0x01, 0x80, 0xff}
	if fullFlips {
		masks = []byte{0x01, 0x02, 0x04, 0x08, 0x10, 0x20, 0x40, 0x80, 0xff}
	}
	for idx := 0; idx < len(wire); idx++ {
		for _, m := range masks {
			cp := append([]byte{}, wire...)
			cp[idx] ^= m
			ops = append(ops, cp)
		}
	}
	// 3. Appends to the saved file.
	for _, tail := range [][]byte{{0x00}, {0x01}, {0xff}, {0x00, 0x00, 0x00, 0x00}} {
		ops = append(ops, append(append([]byte{}, wire...), tail...))
	}
	// 4. Deterministic splices and inserts that change the saved file.
	dr := rand.New(rand.NewSource(1))
	for k := 0; k < 100; k++ {
		cp := append([]byte{}, wire...)
		if len(wire) > 1 && dr.Intn(2) == 0 {
			a := dr.Intn(len(wire))
			b := a + 1 + dr.Intn(len(wire)-a)
			cp = append(cp[:a], cp[b:]...)
		} else if len(wire) > 0 {
			at := dr.Intn(len(wire) + 1)
			cp = append(cp[:at], append([]byte{byte(dr.Intn(256))}, cp[at:]...)...)
		}
		if !bytes.Equal(cp, wire) {
			ops = append(ops, cp)
		}
	}
	return ops
}

func TestProbe(t *testing.T) {
	dir := os.Getenv("SCHEMA_TABLE_DIFF_DIR")
	n, _ := strconv.Atoi(os.Getenv("SCHEMA_TABLE_DIFF_N"))
	phase := os.Getenv("SCHEMA_TABLE_DIFF_PHASE")
	sabotage := os.Getenv("SCHEMA_TABLE_DIFF_SABOTAGE") == "1"
	sabotageIntegrity := os.Getenv("SCHEMA_TABLE_DIFF_SABOTAGE_INTEGRITY") == "1"
	crcTab := crc32.MakeTable(crc32.Castagnoli)
	var div []string
	wrong := 0
	plants := []string{
		"null", "-0", "1e999", "NaN", "\"\\ud800\"", "\ufeff{}", "{} {}", "{\"a\":1,\"a\":2}",
		"{" + strings.Repeat("[", 100000) + strings.Repeat("]", 100000) + "}",
	}
	if phase == "" || phase == "clean" {
		for i := 0; i < n; i++ {
			jb, err := os.ReadFile(dir + "/inst-" + strconv.Itoa(i) + ".json")
			if err != nil {
				t.Fatal(err)
			}
			var report TableReport
			v, ok := probeFromJSON(jb, &report)
			if !ok || v == nil {
				record(&div, "fromjson %d %s", i, b64(jb))
				continue
			}
			tj := make([]byte, RootToJSONMeasure(v))
			if RootToJSON(v, tj) != int64(len(tj)) {
				record(&div, "tojson %d", i)
			}
			if sabotage && i == 0 {
				tj = append(tj, ' ')
			}
			if err := os.WriteFile(dir+"/inst-"+strconv.Itoa(i)+".json", tj, 0o600); err != nil {
				t.Fatal(err)
			}
			wire := make([]byte, RootMeasure(v))
			if RootSave(v, wire) != int64(len(wire)) {
				record(&div, "save %d", i)
			}
			if err := os.WriteFile(dir+"/inst-"+strconv.Itoa(i)+".wire", wire, 0o600); err != nil {
				t.Fatal(err)
			}
			if v2, ok := probeFromJSON(tj, &report); ok && v2 != nil {
			tj2 := make([]byte, RootToJSONMeasure(v2))
				if RootToJSON(v2, tj2) != int64(len(tj2)) || string(tj2) != string(tj) {
					record(&div, "fromjsontojson %d %s %s", i, b64(tj), b64(tj2))
				}
				w2 := make([]byte, RootMeasure(v2))
				if RootSave(v2, w2) != int64(len(w2)) || string(w2) != string(wire) {
					record(&div, "fromjsontojsonresave %d", i)
				} else if got2, err := RootReadStrict(w2); err != nil || got2 == nil {
					record(&div, "fromjsontojsonwirestrict %d %v", i, err)
				}
			} else {
				record(&div, "fromjsontojsonrefuse %d %s", i, b64(tj))
			}
			got, err := RootReadStrict(wire)
			if err != nil || got == nil {
				record(&div, "strictrefuse %d", i)
			} else {
				gj := make([]byte, RootToJSONMeasure(got))
				if RootToJSON(got, gj) != int64(len(gj)) {
					record(&div, "stricttojson %d", i)
				}
				if string(gj) != string(tj) {
					record(&div, "roundtrip %s %s", b64(tj), b64(gj))
				}
				gw := make([]byte, RootMeasure(got))
				if RootSave(got, gw) != int64(len(gw)) || string(gw) != string(wire) {
					record(&div, "strictresave %d", i)
				}
			}
			st := base64.StdEncoding.EncodeToString(wire)
			if stxt, err := RootSaveText(v); err != nil || stxt != st {
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
			loaded, lok := probeLoad(wire, &report)
			if !lok || loaded == nil {
				record(&div, "loadrefuse %d", i)
			} else {
				lj := make([]byte, RootToJSONMeasure(loaded))
				if RootToJSON(loaded, lj) != int64(len(lj)) {
					record(&div, "loadtojson %d", i)
				} else if string(lj) != string(tj) {
					record(&div, "loadroundtrip %s %s", b64(tj), b64(lj))
				}
				lw := make([]byte, RootMeasure(loaded))
				if RootSave(loaded, lw) != int64(len(lw)) || string(lw) != string(wire) {
					record(&div, "loadresave %d", i)
				}
			}
			if hasLoadMeasure {
				if m, merr := probeLoadMeasureReason(wire); merr != nil || m < 0 {
					record(&div, "loadmeasurerefuse %d %d %v", i, m, merr)
				}
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
		}
	}
	if phase == "" || phase == "damage" {
		for i := 0; i < n; i++ {
			tj, err1 := os.ReadFile(dir + "/inst-" + strconv.Itoa(i) + ".json")
			wire, err2 := os.ReadFile(dir + "/inst-" + strconv.Itoa(i) + ".wire")
			if err1 != nil || err2 != nil {
				continue
			}
			// damage: every truncation, every single-byte flip, appends, then random splices/inserts
			for _, d := range damageOps(wire, i == 0) {
				func() {
					defer func() {
						if r := recover(); r != nil {
							record(&div, "damagepanic %v", r)
						}
					}()
					in := append([]byte{}, d...)
					if sabotageIntegrity && hasIntegrity && len(in) >= 5 && in[0] == 1 {
						binary.LittleEndian.PutUint32(in[len(in)-4:], crc32.Checksum(in[:len(in)-4], crcTab))
					}
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
					var tr, sr, br, ltr TableReport
					tv, tok := probeLoadTolerant(in, &tr)
					if tok && tv != nil {
						_, _ = RootMeasureReason(tv)
					}
					sv, sok := probeLoad(in, &sr)
					bv, bok := probeLoadBuilderTolerant(in, &br)
					var m int64 = -1
					var merr error
					if hasLoadMeasure {
						m, merr = probeLoadMeasureReason(in)
					}
					strictVal, serr := RootReadStrict(in)
					ltVal, lterr := RootLoadText(base64.StdEncoding.EncodeToString(in), &ltr)
				runtime.ReadMemStats(&after)
				if len(in) < 4096 && after.TotalAlloc-before.TotalAlloc > 4<<20 {
					record(&div, "damagealloc %d", after.TotalAlloc-before.TotalAlloc)
				}
					if hasIntegrity {
						wantErr := TableRefuseIntegrityMismatch
						wantMeasureErr := TableRefuseIntegrityMismatch
						if len(d) == 0 {
							wantErr = TableRefuseWireDamaged
							wantMeasureErr = TableRefuseWireDamaged
						} else if d[0] != 1 {
							wantErr = TableRefuseUnknownForm
							wantMeasureErr = TableRefuseUnknownForm
						}
					if serr == nil || strictVal != nil {
						record(&div, "integrity-read-clean %d ReadStrict %x", i, d)
					} else if !errors.Is(serr, wantErr) {
						record(&div, "integrity-wrong-reason %d ReadStrict got=%v want=%v %x", i, serr, wantErr, d)
					}
					if tok || tv != nil {
						record(&div, "integrity-read-clean %d LoadTolerant %x", i, d)
					} else if !errors.Is(tr.Err(), wantErr) || (len(d) > 0 && d[0] == 1 && (!tr.Malformed || tr.Reason != "integrity_mismatch" || tr.Verdict != TableOpenDamaged)) {
						record(&div, "integrity-wrong-reason %d LoadTolerant report=%+v err=%v want=%v %x", i, tr, tr.Err(), wantErr, d)
					}
					if sok || sv != nil {
						record(&div, "integrity-read-clean %d Load %x", i, d)
					} else if !errors.Is(sr.Err(), wantErr) {
						record(&div, "integrity-wrong-reason %d Load report=%+v err=%v want=%v %x", i, sr, sr.Err(), wantErr, d)
					}
					if bok || bv != nil {
						record(&div, "integrity-read-clean %d LoadBuilderTolerant %x", i, d)
					} else if !errors.Is(br.Err(), wantErr) || (len(d) > 0 && d[0] == 1 && (!br.Malformed || br.Reason != "integrity_mismatch" || br.Verdict != TableOpenDamaged)) {
						record(&div, "integrity-wrong-reason %d LoadBuilderTolerant report=%+v err=%v want=%v %x", i, br, br.Err(), wantErr, d)
					}
					if hasLoadMeasure {
						if merr == nil || m >= 0 {
							record(&div, "integrity-read-clean %d LoadMeasureReason %x", i, d)
						} else if !errors.Is(merr, wantMeasureErr) {
							record(&div, "integrity-wrong-reason %d LoadMeasureReason got=%v want=%v %x", i, merr, wantMeasureErr, d)
						}
					}
					if lterr == nil || ltVal != nil {
						record(&div, "integrity-read-clean %d LoadText %x", i, d)
					} else if !errors.Is(lterr, wantErr) {
						record(&div, "integrity-wrong-reason %d LoadText got=%v want=%v %x", i, lterr, wantErr, d)
					}
				} else {
					if serr == nil && strictVal != nil {
						gj := make([]byte, RootToJSONMeasure(strictVal))
						if RootToJSON(strictVal, gj) == int64(len(gj)) && !bytes.Equal(gj, tj) {
							wrong++
						}
					}
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
				var pr TableReport
				_, _ = probeFromJSON([]byte(plant), &pr)
			}()
		}
	}
		_ = os.WriteFile(dir+"/wrong.txt", []byte(strconv.Itoa(wrong)), 0o600)
	}
	if len(div) > 0 {
		existing, _ := os.ReadFile(dir + "/divergences.txt")
		_ = os.WriteFile(dir+"/divergences.txt", append(existing, []byte(strings.Join(div, "\n")+"\n")...), 0o600)
		t.Fatalf("%d divergence(s)", len(div))
	}
}
`

// runSeed generates, compiles and runs one seed, then classifies what the probe
// reported and compares the engine in both directions. It returns the
// unexpected divergences (which fail the seed) and a log of expected ones.
func runSeed(t *testing.T, r *rand.Rand, seed int, sabotage bool) (unexpected []string, expected []string) {
	t.Helper()
	unexpected, expected, _ = runSeedFull(t, r, seed, sabotage, os.Getenv("SCHEMA_TABLE_DIFF_SABOTAGE_INTEGRITY") == "1")
	return unexpected, expected
}

func runSeedFull(t *testing.T, r *rand.Rand, seed int, sabotage, sabotageIntegrity bool) (unexpected []string, expected []string, wrong int) {
	t.Helper()
	src, u := randomSchema(r)
	unit := tableUnit(t, src)
	dir := t.TempDir()
	for i := range 24 {
		j := randomJSON(r, u, i)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("inst-%d.json", i)), []byte(j), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	env := map[string]string{"SCHEMA_TABLE_DIFF_DIR": dir, "SCHEMA_TABLE_DIFF_N": "24"}
	if sabotage {
		env["SCHEMA_TABLE_DIFF_SABOTAGE"] = "1"
	}
	if sabotageIntegrity {
		env["SCHEMA_TABLE_DIFF_SABOTAGE_INTEGRITY"] = "1"
	}
	env["SCHEMA_TABLE_DIFF_PHASE"] = "clean"
	outClean, errClean := runGeneratedTableUnit(t, unit, tableDifferentialProbe, env, "-race")
	env["SCHEMA_TABLE_DIFF_PHASE"] = "damage"
	outDamage, errDamage := runGeneratedTableUnit(t, unit, tableDifferentialProbe, env, "-race")
	out := append(append([]byte(nil), outClean...), outDamage...)
	err := errClean
	if err == nil {
		err = errDamage
	}
	if wb, werr := os.ReadFile(filepath.Join(dir, "wrong.txt")); werr == nil {
		wrong, _ = strconv.Atoi(strings.TrimSpace(string(wb)))
	}
	divBytes, _ := os.ReadFile(filepath.Join(dir, "divergences.txt"))
	skip := map[int]bool{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(divBytes)), "\n") {
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
		return unexpected, expected, wrong
	}
	if len(unexpected) > 0 {
		return unexpected, expected, wrong
	}
	crcTab := crc32.MakeTable(crc32.Castagnoli)
	m := tabletext.NewModel(unit)
	for i := range 24 {
		if skip[i] {
			continue
		}
		wire, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.wire", i)))
		if err != nil {
			unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: read wire: %v", seed, i, err))
			continue
		}
		engineWire := wire
		if u.Root.Integrity != "" {
			if len(wire) < 4 {
				unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: integrity wire shorter than 4-byte trailer (%d bytes)", seed, i, len(wire)))
				continue
			}
			body, tr := wire[:len(wire)-4], wire[len(wire)-4:]
			if got, want := binary.LittleEndian.Uint32(tr), crc32.Checksum(body, crcTab); got != want {
				unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: integrity trailer 0x%08x != crc32c 0x%08x", seed, i, got, want))
				continue
			}
			engineWire = body
		}
		legJSON, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.json", i)))
		if err != nil {
			unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: read text: %v", seed, i, err))
			continue
		}
		d1 := t.TempDir()
		report, err := tablepack.UnpackOneFile(m, "Root", engineWire, d1)
		if err != nil {
			unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: UnpackOneFile: %v", seed, i, err))
			continue
		}
		if report != (tabletext.Report{}) {
			unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: UnpackOneFile report not clean: %+v", seed, i, report))
			continue
		}
		engineJSON, err := os.ReadFile(filepath.Join(d1, "Root.json"))
		if err != nil {
			unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: read engine text: %v", seed, i, err))
			continue
		}
		pack := func(legJSON []byte) ([]byte, tabletext.Report, error) {
			d2 := t.TempDir()
			if err := os.WriteFile(filepath.Join(d2, "Root.json"), legJSON, 0o600); err != nil {
				return nil, tabletext.Report{}, err
			}
			wireBack, _, report2, err := tablepack.Pack(m, "Root", d2)
			return wireBack, report2, err
		}
		un, ex := engineCheck(u, seed, i, engineWire, legJSON, engineJSON, pack)
		unexpected = append(unexpected, un...)
		expected = append(expected, ex...)
	}
	return unexpected, expected, wrong
}

// engineCheck compares one instance's engine text (tablepack.UnpackOneFile of
// the leg's wire) with the leg's own text, then runs the other direction: pack
// (tablepack.Pack of the leg's text) must give the leg's wire back. A text
// divergence an open card owns is reported, not failed.
func engineCheck(u *genUnit, seed, i int, wire, legJSON, engineJSON []byte, pack func(legJSON []byte) ([]byte, tabletext.Report, error)) (unexpected, expected []string) {
	if !bytes.Equal(engineJSON, legJSON) {
		line := "engine " + base64.StdEncoding.EncodeToString(legJSON) + " " + base64.StdEncoding.EncodeToString(engineJSON)
		owned := false
		for _, c := range expectedClasses {
			if c.match(u, line) {
				expected = append(expected, fmt.Sprintf("seed %d inst %d: %s: engine text differs from leg text", seed, i, c.id))
				owned = true
				break
			}
		}
		if !owned {
			unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: unexpected engine/leg text divergence\nleg: %s\nengine: %s", seed, i, legJSON, engineJSON))
		}
		// No early return: an owed text class never skips the Pack
		// direction below, which must still give the leg's wire back from
		// the leg's text.
	}
	wireBack, report2, err := pack(legJSON)
	if err != nil {
		unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: Pack: %v", seed, i, err))
		return unexpected, expected
	}
	if report2 != (tabletext.Report{}) {
		unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: Pack report not clean: %+v", seed, i, report2))
		return unexpected, expected
	}
	if !bytes.Equal(wireBack, wire) {
		unexpected = append(unexpected, fmt.Sprintf("seed %d inst %d: engine wire differs from leg wire", seed, i))
	}
	return unexpected, expected
}

func shorten(s string) string {
	if len(s) > 220 {
		return s[:220] + "..."
	}
	return s
}

// tableDiffOptIn skips the two tests below unless SCHEMA_TABLE_DIFF=1 (or an
// explicit seed range SCHEMA_TABLE_DIFF_FROM / SCHEMA_TABLE_DIFF_TO is set).
// They compile and run a generated unit per seed under -race, so they are a
// nightly campaign (certify.yml), never part of ci-fast or ci-full: ci-full's
// SCHEMA_SLOW=1 `go test` over ./internal/fuzz still runs the crasher corpus
// and the property and oracle tests, and skips only these two here.
func tableDiffOptIn(t *testing.T) {
	t.Helper()
	if !tableDiffOptedIn(os.Getenv) {
		slowtest.Gate(t, "the Go toolchain (it compiles and runs generated units) and the engine")
		t.Skip("SCHEMA_TABLE_DIFF: the random-schema table differential is a nightly campaign; run it with make fuzz-tables-diff (it sets SCHEMA_TABLE_DIFF=1)")
	}
}

// tableDiffOptedIn reports whether the two tests below run in this process:
// when SCHEMA_TABLE_DIFF=1 or an explicit seed range is given, never on
// SCHEMA_SLOW=1 alone.
func tableDiffOptedIn(getenv func(string) string) bool {
	return getenv("SCHEMA_TABLE_DIFF") == "1" || getenv("SCHEMA_TABLE_DIFF_FROM") != "" || getenv("SCHEMA_TABLE_DIFF_TO") != ""
}

// TestRandomTableDifferential is the gate: seeds SCHEMA_TABLE_DIFF_FROM..TO
// (default 0..4), each generated, compiled and run under -race.
func TestRandomTableDifferential(t *testing.T) {
	t.Parallel()
	tableDiffOptIn(t)
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
			unexpected, expected, wrong := runSeedFull(t, rand.New(rand.NewSource(int64(seed))), seed, false, os.Getenv("SCHEMA_TABLE_DIFF_SABOTAGE_INTEGRITY") == "1")
			for _, e := range expected {
				t.Logf("EXPECTED %s", e)
			}
			t.Logf("seed %d: %d silent wrong-value decode(s) of damaged wires", seed, wrong)
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
	tableDiffOptIn(t)
	unexpected, _ := runSeed(t, rand.New(rand.NewSource(0)), 0, true)
	if len(unexpected) == 0 {
		t.Fatalf("NEGATIVE CONTROL FAILED: a flipped probe comparison left the gate green")
	}
	if !strings.Contains(strings.Join(unexpected, "\n"), "unexpected divergence") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on an unexpected divergence: %v", unexpected)
	}
}

// integritySeeds returns the first n seeds whose generated Root table declares
// `integrity = "crc32c"`.
func integritySeeds(n int) []int {
	var out []int
	for seed := 0; len(out) < n && seed < 400; seed++ {
		_, u := randomSchema(rand.New(rand.NewSource(int64(seed))))
		if u.Root.Integrity == "crc32c" {
			out = append(out, seed)
		}
	}
	return out
}

// TestRandomTableIntegrityUnits runs three integrity seeds through the same
// runSeed path and requires zero failing findings and zero wrongvalue count,
// plus a negative control that sets SCHEMA_TABLE_DIFF_SABOTAGE_INTEGRITY=1 so
// the probe skips the integrity check on read and must fail with
// integrity-read-clean.
func TestRandomTableIntegrityUnits(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs generated units under -race) and the engine")
	if os.Getenv("CI") != "" && !tableDiffOptedIn(os.Getenv) {
		t.Skip("the random-schema table differential is nightly only; CI is set and SCHEMA_TABLE_DIFF is not")
	}
	seeds := integritySeeds(3)
	if len(seeds) < 3 {
		t.Fatalf("found %d integrity seeds in 0..399, want at least 3", len(seeds))
	}
	for _, seed := range seeds {
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			t.Parallel()
			unexpected, expected, wrong := runSeedFull(t, rand.New(rand.NewSource(int64(seed))), seed, false, os.Getenv("SCHEMA_TABLE_DIFF_SABOTAGE_INTEGRITY") == "1")
			for _, e := range expected {
				t.Logf("EXPECTED %s", e)
			}
			if wrong != 0 {
				t.Errorf("seed %d: integrity unit counted %d wrongvalue decode(s), want 0", seed, wrong)
			}
			for _, e := range unexpected {
				t.Errorf("%s", e)
			}
		})
	}
	t.Run("negative_control", func(t *testing.T) {
		t.Parallel()
		seed := seeds[0]
		unexpected, _, _ := runSeedFull(t, rand.New(rand.NewSource(int64(seed))), seed, false, true)
		joined := strings.Join(unexpected, "\n")
		if !strings.Contains(joined, "integrity-read-clean") {
			t.Fatalf("NEGATIVE CONTROL FAILED: skipping the integrity check on read did not fail with integrity-read-clean:\n%s", shorten(joined))
		}
	})
}

var _ = math.MaxInt8
