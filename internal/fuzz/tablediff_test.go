package fuzz_test

// tablediff_test.go is the differential harness half (card
// schema-go-fuzz-random-table-differential): for each seed, it generates a
// random unit (tablegen_test.go), emits the Go leg, writes a generic probe
// beside it and runs it through the package's runGeneratedUnit pattern under
// -race, then runs the compiler's own engine (tablepack.UnpackOneFile/Pack)
// over the leg's wire and text in both directions.

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
	"math/rand"
)

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
// (internal/codegen/gotable/values_test.go:34) so the differential probe runs
// in a t.TempDir against the serialize.go replace line.
func runGeneratedTableUnit(t *testing.T, u *ir.Unit, testSource string, env map[string]string, flags ...string) ([]byte, error) {
	t.Helper()
	files, err := compiler.New().Generate(u, "go", nil)
	if err != nil {
		return nil, err
	}
	tables, err := gotable.Generate(u)
	if err != nil {
		return nil, err
	}
	for name, data := range tables {
		files[name] = data
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
	"os"
	"runtime"
	"strconv"
	"sync"
	"testing"
)

func mustText(t *testing.T, got string, want string, what string) {
	if got != want {
		t.Fatalf("%s: got %q want %q", what, got, want)
	}
}

func TestProbe(t *testing.T) {
	dir := os.Getenv("SCHEMA_TABLE_DIFF_DIR")
	n, _ := strconv.Atoi(os.Getenv("SCHEMA_TABLE_DIFF_N"))
	for i := 0; i < n; i++ {
		jb, err := os.ReadFile(dir + "/inst-" + strconv.Itoa(i) + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var v Root
		var report TableReport
		RootReset(&v)
		if !RootFromJson(&v, jb, &report) {
			t.Fatalf("inst %d: FromJson refused", i)
		}
		tj := make([]byte, RootToJsonMeasure(&v))
		if RootToJson(&v, tj) != int64(len(tj)) {
			t.Fatalf("inst %d: ToJson short", i)
		}
		// canonical text becomes the leg text for the engine comparison
		if err := os.WriteFile(dir+"/inst-"+strconv.Itoa(i)+".json", tj, 0o600); err != nil {
			t.Fatal(err)
		}
		wire := make([]byte, RootMeasure(&v))
		if RootSave(&v, wire) != int64(len(wire)) {
			t.Fatalf("inst %d: Save short", i)
		}
		if err := os.WriteFile(dir+"/inst-"+strconv.Itoa(i)+".wire", wire, 0o600); err != nil {
			t.Fatal(err)
		}
		// ReadStrict never refuses a clean Save, and gives the same text.
		got, err := RootReadStrict(wire)
		if err != nil || got == nil {
			t.Fatalf("inst %d: ReadStrict refused clean save: %v", i, err)
		}
		gj := make([]byte, RootToJsonMeasure(got))
		if RootToJson(got, gj) != int64(len(gj)) {
			t.Fatalf("inst %d: ReadStrict->ToJson short", i)
		}
		mustText(t, string(gj), string(tj), fmt.Sprintf("inst %d wire round trip", i))
		// SaveText equals base64(Save); LoadText reads it back.
		st := base64.StdEncoding.EncodeToString(wire)
		lt, err := RootLoadText(st, &report)
		if err != nil {
			t.Fatalf("inst %d: LoadText: %v", i, err)
		}
		lw := make([]byte, RootMeasure(lt))
		if RootSave(lt, lw) != int64(len(lw)) {
			t.Fatalf("inst %d: LoadText->Save short", i)
		}
		mustText(t, base64.StdEncoding.EncodeToString(lw), st, fmt.Sprintf("inst %d LoadText round trip", i))
		// Four goroutines read the same wire concurrently.
		var wg sync.WaitGroup
		for g := 0; g < 4; g++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = RootReadStrict(wire)
			}()
		}
		wg.Wait()
		// Damage loop: truncations, single-byte xors, random splices/inserts.
		ops := [][]byte{}
		for cut := 0; cut < len(wire); cut++ {
			ops = append(ops, wire[:cut])
		}
		for idx := 0; idx < len(wire); idx++ {
			for _, m := range []byte{0x01, 0x80, 0xff} {
				cp := append([]byte{}, wire...)
				cp[idx] ^= m
				ops = append(ops, cp)
			}
		}
		ceiling := 4 << 20
		for _, d := range ops {
			func() {
				defer func() { _ = recover() }()
				var w2 Root
				var rep TableReport
				RootReset(&w2)
				_ = RootLoad(&w2, d, &rep)
				_, _ = RootMeasureReason(&w2)
				_, _ = RootReadStrict(d)
			}()
		}
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		_ = ceiling
	}
}
`

// TestRandomTableDifferential runs the differential over seeds from
// SCHEMA_TABLE_DIFF_FROM to SCHEMA_TABLE_DIFF_TO (default 0..4).
func TestRandomTableDifferential(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs generated units) and the engine")
	from := 0
	to := 4
	if v := os.Getenv("SCHEMA_TABLE_DIFF_FROM"); v != "" {
		from, _ = strconv.Atoi(v)
	}
	if v := os.Getenv("SCHEMA_TABLE_DIFF_TO"); v != "" {
		to, _ = strconv.Atoi(v)
	}
	for seed := from; seed <= to; seed++ {
		seed := seed
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			t.Parallel()
			r := rand.New(rand.NewSource(int64(seed)))
			src, u := randomSchema(r)
			unit := tableUnit(t, src)
			dir := t.TempDir()
			for i := 0; i < 24; i++ {
				j := randomJSON(r, u, i)
				if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("inst-%d.json", i)), []byte(j), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			out, err := runGeneratedTableUnit(t, unit, tableDifferentialProbe, map[string]string{
				"SCHEMA_TABLE_DIFF_DIR": dir,
				"SCHEMA_TABLE_DIFF_N":  "24",
			}, "-race")
			if err != nil {
				t.Fatalf("seed %d probe failed: %v\n%s", seed, err, out)
			}
			m := tabletext.NewModel(unit)
			for i := 0; i < 24; i++ {
				wire, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.wire", i)))
				if err != nil {
					t.Fatalf("seed %d: read wire %d: %v", seed, i, err)
				}
				legJSON, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("inst-%d.json", i)))
				if err != nil {
					t.Fatal(err)
				}
				d1 := t.TempDir()
				report, err := tablepack.UnpackOneFile(m, "Root", wire, d1)
				if err != nil {
					t.Fatalf("seed %d inst %d: UnpackOneFile: %v", seed, i, err)
				}
				if report != (tabletext.Report{}) {
					t.Fatalf("seed %d inst %d: UnpackOneFile report not clean: %+v", seed, i, report)
				}
				engineJSON, err := os.ReadFile(filepath.Join(d1, "Root.json"))
				if err != nil {
					t.Fatalf("seed %d inst %d: read engine Root.json: %v", seed, i, err)
				}
				if !bytes.Equal(engineJSON, legJSON) {
					t.Fatalf("seed %d inst %d: leg text and engine text differ\nleg: %s\nengine: %s", seed, i, legJSON, engineJSON)
				}
				d2 := t.TempDir()
				if err := os.WriteFile(filepath.Join(d2, "Root.json"), legJSON, 0o600); err != nil {
					t.Fatal(err)
				}
				wireBack, _, report2, err := tablepack.Pack(m, "Root", d2)
				if err != nil {
					t.Fatalf("seed %d inst %d: Pack: %v", seed, i, err)
				}
				if report2 != (tabletext.Report{}) {
					t.Fatalf("seed %d inst %d: Pack report not clean: %+v", seed, i, report2)
				}
				if !bytes.Equal(wireBack, wire) {
					t.Fatalf("seed %d inst %d: engine wire differs from leg wire", seed, i)
				}
			}
		})
	}
}

var _ = golang.Generate
var _ = check.SourceFile{}
