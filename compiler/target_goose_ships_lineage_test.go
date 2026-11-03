//go:build schema_leg_goose

// The Goose target takes the lock's lineage as data (docs/FIXED-FORM-ALGORITHM.md
// §5.2, §5.9 #1), generated through the driver and never handed in by the test.
//
// The first test is the static statement compiler/fixedlineageship_test.go
// makes for the built-in targets: the same REAL LOCK (schema lock, a widening,
// schema lock again), and the OLDER layout's wire hash and its whole record
// size are in the emitted module beside the current one.
//
// The second runs it: the C++ reference's old_field_append.bin
// (make tables-fixedform-corpus) under the lock the operator's commands write
// for that row's two schemas, read by the Goose program Compiler.Generate
// emits. The older layout reads with the widened field at its declared
// default; after `schema lock --retire` the same file is refused
// layout_unsupported; a build with no lineage refuses it layout_unknown.
// When the tag drops, goose joins fixedLineageShipTargets and the first test
// goes.
package compiler

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestGooseShipsTheLockLineage(t *testing.T) {
	c, u, older, current, lin := fixedLineageShipLockedUnit(t)
	files, err := c.Generate(u, "goose", nil)
	if err != nil {
		t.Fatalf("goose: generate: %v", err)
	}
	for _, h := range []uint64{older, current} {
		needle := fmt.Sprintf("0x%016x", h)
		found := false
		for _, data := range files {
			if strings.Contains(string(data), needle) {
				found = true
			}
		}
		if !found {
			t.Errorf("no emitted module carries layout %s: the driver handed goose no lineage", needle)
		}
	}
	want := 8 + lin[0].Record
	entry := regexp.MustCompile(fmt.Sprintf(`h == 0x%016x \{\n\s*pick = \d+;\n\s*known = \w+;\n\s*record_bytes = (\d+);`, older))
	matches := 0
	for name, data := range files {
		for _, m := range entry.FindAllStringSubmatch(string(data), -1) {
			matches++
			if m[1] != fmt.Sprint(want) {
				t.Errorf("%s: the OLDER layout 0x%016x ships record_bytes %s, want the whole record 8 + %d = %d", name, older, m[1], lin[0].Record, want)
			}
		}
	}
	if matches != 1 {
		t.Errorf("the emitted modules pick the older layout 0x%016x %d times, want exactly once", older, matches)
	}
}

// gooseLineageCorpus is the C++ reference's byte oracle, skipped when it is not
// built unless SCHEMA_REQUIRE_CORPUS asks for it.
func gooseLineageCorpus(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "build", "fixedform-corpus"))
	if err == nil {
		if st, serr := os.Stat(filepath.Join(dir, "manifest.txt")); serr == nil && !st.IsDir() {
			return dir
		}
	}
	why := fmt.Sprintf("the C++ reference's byte oracle is not built: make tables-fixedform-corpus (%v)", err)
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
		t.Fatal(why)
	}
	t.Skip(why)
	return ""
}

// gooseLineageRow writes one side of the field_append row into dir as the
// unit's one schema file, under one package so the two sides are one unit's
// history, and loads it.
func gooseLineageRow(t *testing.T, c *Compiler, dir, side string) (*ir.Unit, []string) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "test", "tables", side+"_field_append.schema"))
	if err != nil {
		t.Fatal(err)
	}
	pkg := regexp.MustCompile(`(?m)^package \w+$`)
	text := pkg.ReplaceAllString(string(src), "package gooselineage")
	if err := os.WriteFile(filepath.Join(dir, "Lineage.schema"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.Load(paths)
	if err != nil {
		t.Fatalf("load %s: %v", side, err)
	}
	return u, paths
}

// gooseLineageRead generates u through the driver and runs a Goose program
// that reads data through LineageFixedEvolveLoad, returning the verdict and
// the record: "<n> <reason> <x> <y> <z> <w>", or "<n> <reason>" on a refusal.
func gooseLineageRead(t *testing.T, c *Compiler, u *ir.Unit, data []byte) string {
	t.Helper()
	files, err := c.Generate(u, "goose", nil)
	if err != nil {
		t.Fatalf("goose: generate: %v", err)
	}
	ns := goose.Namespace(u)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, name := range slices.Sorted(maps.Keys(files)) {
		if mod, ok := strings.CutSuffix(name, ".goose"); ok {
			fmt.Fprintf(&b, "import .%s;\n", mod)
		}
	}
	b.WriteString("\nfn main() {\n")
	b.WriteString("    var bytes: u8[>..] = [];\n    if !read_file(\"old.bin\", bytes) { print(\"read-failed\"); return; }\n")
	fmt.Fprintf(&b, "    var values: %s::Lineage[1] = [%s::Lineage { .. }; 1];\n", ns, ns)
	b.WriteString("    var report = schema_rt::FixedEvolveReport { .. };\n")
	fmt.Fprintf(&b, "    let n = %s::LineageFixedEvolveLoad(values[0..1], bytes, report);\n", ns)
	b.WriteString("    print(n, \" \", schema_rt::fixed_evolve_refusal_name(report.reason));\n")
	b.WriteString("    if n < 0 { return; }\n")
	b.WriteString("    print(\" \", values[0].X, \" \", values[0].Y, \" \", values[0].Z, \" \", values[0].W);\n")
	b.WriteString("}\n")
	dir := t.TempDir()
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), src, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	out := gooserun.Run(t, dir, "main.goose", map[string][]byte{"old.bin": data})
	return strings.Join(strings.Fields(string(out)), " ")
}

func TestGooseReadsTheLockedOlderLayout(t *testing.T) {
	t.Parallel()
	corpus := gooseLineageCorpus(t)
	data, err := os.ReadFile(filepath.Join(corpus, "old_field_append.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 16 {
		t.Fatalf("old_field_append.bin is %d bytes, too short for a fixed-form header", len(data))
	}
	fileHash := binary.LittleEndian.Uint64(data[8:16])

	c := New()
	dir := t.TempDir()

	// NO LOCK, NO LINEAGE: the widened build alone knows its own layout and
	// refuses the older file by name. This is the build the target shipped
	// before it took the lineage.
	bare, _ := gooseLineageRow(t, c, dir, "VNEW")
	if got := gooseLineageRead(t, c, bare, data); got != "-1 layout_unknown" {
		t.Fatalf("an unlocked widened build reads the older file as %q, want -1 layout_unknown", got)
	}

	// THE OPERATOR'S COMMANDS: lock the old declaration, widen, lock again.
	older, paths := gooseLineageRow(t, c, dir, "VOLD")
	if _, _, err := UpdateSchemaLock(older, paths); err != nil {
		t.Fatalf("the first lock: %v", err)
	}
	widened, paths := gooseLineageRow(t, c, dir, "VNEW")
	if _, _, err := UpdateSchemaLock(widened, paths); err != nil {
		t.Fatalf("the second lock: %v", err)
	}
	lock, ok, err := lockfile.Open(paths)
	if err != nil || !ok {
		t.Fatalf("the lock: ok=%v err=%v", ok, err)
	}
	lin := lockfile.Lineage(lock, "Lineage")
	if len(lin) != 2 || lin[0].Wire != fileHash {
		t.Fatalf("the lock's lineage must open on the reference file's layout 0x%016x: %v", fileHash, lin)
	}

	// THE LOCKED BUILD READS THE OLDER FILE: the manifest's values, and the
	// appended field at its declared default (§4.3).
	if got := gooseLineageRead(t, c, widened, data); got != "1 ok 11 22 33 77" {
		t.Fatalf("the locked widened build reads the older file as %q, want 1 ok 11 22 33 77", got)
	}

	// RETIRED: the same file is a layout this build once served and no longer
	// does (§5.2's floor).
	if _, rewrote, err := RetireSchemaLock(widened, paths, fmt.Sprintf("Lineage@0x%016x", fileHash), "the test retires the old layout"); err != nil || !rewrote {
		t.Fatalf("retire: rewrote=%v err=%v", rewrote, err)
	}
	retired, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if got := gooseLineageRead(t, c, retired, data); got != "-1 layout_unsupported" {
		t.Fatalf("the retired build reads the older file as %q, want -1 layout_unsupported", got)
	}
}
