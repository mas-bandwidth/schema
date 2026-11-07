package squirreltable_test

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/goname"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// viewPrelude prints the Squirrel view in one canonical, line-oriented text:
// one INFO row per table and one VIEW row per field (docs/SPEC-TABLES.md §8.1,
// §8.3). The Go oracle below prints the same text from gotable's emitted
// TableFields, so a disagreement in any column is one red line.
const viewPrelude = `local join = function(a) {
  local s = "";
  for (local i = 0; i < a.len(); i++) { if (i > 0) s += "|"; s += a[i]; }
  return s;
};
local hexOf = function(s) {
  local out = "";
  for (local i = 0; i < s.len(); i++) out += format("%02x", s[i] & 0xff);
  return out;
};
local tagsOf = function(tags) {
  local out = "";
  for (local i = 0; i < tags.len(); i++) { if (i > 0) out += ","; out += hexOf(tags[i]); }
  return out;
};
local boolOf = function(x) { return x ? "true" : "false"; };
local widePart = function(on, pair, i) { return on ? format("%u", pair[i]) : "-"; };
local viewTable = function(unit, table, info) {
  print("INFO|" + join([unit + "." + table, info.name, format("%016x", info.typeId),
    boolOf(info.table), boolOf(info.variable), format("%d", info.numFields),
    hexOf(info.doc), format("%d", info.numTags), tagsOf(info.tags)]) + "\n");
  local fs = info.fields;
  for (local i = 0; i < fs.len(); i++) {
    local f = fs[i];
    print("VIEW|" + join([unit, table, f.name, f.json, f.typeName, f.declaredTypeName,
      format("%016x", f.id), format("%d", f.kind), boolOf(f.isArray), boolOf(f.counted),
      boolOf(f.optional), format("%d", f.arrayBound), boolOf(f.unbounded), boolOf(f.hasRange),
      format("%d", f.rangeIntMin), format("%d", f.rangeIntMax),
      format("%u", f.rangeUintMin), format("%u", f.rangeUintMax),
      format("%d", f.fracBits), boolOf(f.pointer), format("%016x", f.targetId),
      boolOf(f.wide), widePart(f.wide, f.wideMin, 0), widePart(f.wide, f.wideMin, 1),
      widePart(f.wide, f.wideMax, 0), widePart(f.wide, f.wideMax, 1),
      format("%d", f.enumMax), hexOf(f.doc), format("%d", f.numTags), tagsOf(f.tags)]) + "\n");
  }
};
`

// viewGoTest is the Go oracle: it reads gotable's emitted <T>TableFields and
// <T>TableInfo, in the same order and with the same columns, and writes them
// to $VIEW_OUT. It is compiled with the generated unit in a t.TempDir().
const viewGoTest = `package %s

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestSquirrelViewPrint(t *testing.T) {
	var b strings.Builder
	writeInfo := func(unit, table string, isTable bool, info TableTypeInfo) {
		fmt.Fprintf(&b, "INFO|%%s|%%s|%%016x|%%t|%%t|%%d|%%s|%%d|%%s\n", unit+"."+table, info.Name,
			info.ID, isTable, info.Variable, info.NumFields, hex.EncodeToString([]byte(info.Doc)),
			info.NumTags, tagsHex(info.Tags))
	}
	writeField := func(unit, table string, f TableFieldInfo) {
		widePart := func(on bool, pair [2]uint64, i int) string {
			if !on {
				return "-"
			}
			return fmt.Sprintf("%%d", pair[i])
		}
		fmt.Fprintf(&b, "VIEW|%%s|%%s|%%s|%%s|%%s|%%s|%%016x|%%d|%%t|%%t|%%t|%%d|%%t|%%t|%%d|%%d|%%d|%%d|%%d|%%t|%%016x|%%t|%%s|%%s|%%s|%%s|%%d|%%s|%%d|%%s\n",
			unit, table, f.Name, f.JSON, f.TypeName, f.DeclaredTypeName, f.ID, f.Kind,
			f.IsArray, f.Counted, f.Optional, f.ArrayBound, f.UnboundedContainer, f.HasRange,
			f.RangeIntMin, f.RangeIntMax, f.RangeUintMin, f.RangeUintMax, f.FracBits,
			f.Pointer, f.TargetID, f.WideRange,
			widePart(f.WideRange, f.WideMin, 0), widePart(f.WideRange, f.WideMin, 1),
			widePart(f.WideRange, f.WideMax, 0), widePart(f.WideRange, f.WideMax, 1),
			f.EnumMax, hex.EncodeToString([]byte(f.Doc)), f.NumTags, tagsHex(f.Tags))
	}
%s
	if err := os.WriteFile(os.Getenv("VIEW_OUT"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func tagsHex(tags []string) string {
	parts := make([]string, len(tags))
	for i, tag := range tags {
		parts[i] = hex.EncodeToString([]byte(tag))
	}
	return strings.Join(parts, ",")
}
`

// viewUnit is one unit the Squirrel view and the Go view are compared over.
type viewUnit struct {
	key     string
	unit    *ir.Unit
	names   []string
	dir     string
	modName string
}

// TestSquirrelViewMatchesTheGoView prints the Squirrel view of every manifest
// unit the table surface generates, plus the exact-range probe of
// TestGoExactIntegerRangeBounds and an unbounded-container probe, and requires
// each to equal the Go view of the same unit printed the same way. The Go side
// reads gotable's emitted TableFields through a generated Go program in a
// t.TempDir().
func TestSquirrelViewMatchesTheGoView(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Squirrel interpreter and the Go toolchain")
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	sqDir := t.TempDir()
	var probe strings.Builder
	probe.WriteString(viewPrelude)

	var units []viewUnit
	add := func(key string, u *ir.Unit, files, tables map[string][]byte) {
		if len(tables) == 0 {
			return
		}
		out := filepath.Join(sqDir, key)
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			writeFile(t, filepath.Join(out, name), data)
		}
		for name, data := range tables {
			writeFile(t, filepath.Join(out, name), data)
		}
		var names []string
		for name := range ir.TableClosure(u) {
			names = append(names, name)
		}
		sort.Strings(names)
		mod := squirreltable.ModuleName(u)
		fmt.Fprintf(&probe, "local m = dofile(%q, true).require(%q);\n", key+"/"+squirrel.RuntimePrefix+"module.nut", mod)
		for _, name := range names {
			fmt.Fprintf(&probe, "viewTable(%q, %q, m[%q]);\n", key, name, name+"TableInfo")
		}
		units = append(units, viewUnit{key: key, unit: u, names: names, dir: out, modName: mod})
	}

	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	for _, mu := range manifestUnits(t, manifest) {
		u := loadUnit(t, root, mu.paths)
		files, err := squirrel.Generate(u)
		if err != nil {
			t.Fatalf("unit %s: the type surface refused: %v", mu.key, err)
		}
		tables, err := squirreltable.Generate(u)
		if err != nil {
			t.Logf("refused %s: %v", mu.key, err)
			continue
		}
		add(mu.key, u, files, tables)
	}

	// THE EXACT-RANGE PROBE (TestGoExactIntegerRangeBounds): a signed int64
	// past 2^53, a negative int64, a uint64 past 2^63 and an int128 whose exact
	// pair is the wide [lo, hi] lanes (docs/SPEC-TABLES.md §8.1).
	probeDir := t.TempDir()
	probeSchema := filepath.Join(probeDir, "Probe.schema")
	writeFile(t, probeSchema, []byte(`package probe
fixed table Big {
	n int64 | min = 0, max = 9007199254740993
	m int64 | min = -9223372036854775807, max = 7
	u uint64 | min = 0, max = 18446744073709551614
	w int128 | min = -1267650600228229401496703205376, max = 1267650600228229401496703205376
}
`))
	pu := loadUnit(t, probeDir, []string{"Probe.schema"})
	pfiles, err := squirrel.Generate(pu)
	if err != nil {
		t.Fatalf("exact-range probe: the type surface refused: %v", err)
	}
	ptables, err := squirreltable.Generate(pu)
	if err != nil {
		t.Fatalf("exact-range probe: the table surface refused: %v", err)
	}
	add("exactrange", pu, pfiles, ptables)

	// THE UNBOUNDED-CONTAINER PROBE (docs/SPEC-TABLES.md §8.1): a list field
	// carries the explicit unbounded marker, which reverting `unbounded :=
	// f.IsList() || f.IsMap()` must redden.
	unboundedDir := t.TempDir()
	unboundedSchema := filepath.Join(unboundedDir, "Probe.schema")
	writeFile(t, unboundedSchema, []byte(`package probe
table UnboundedProbe {
	items []int32
	names []string
}
`))
	uu := loadUnit(t, unboundedDir, []string{"Probe.schema"})
	ufiles, err := squirrel.Generate(uu)
	if err != nil {
		t.Fatalf("unbounded probe: the type surface refused: %v", err)
	}
	utables, err := squirreltable.Generate(uu)
	if err != nil {
		t.Fatalf("unbounded probe: the table surface refused: %v", err)
	}
	add("unboundedprobe", uu, ufiles, utables)

	writeFile(t, filepath.Join(sqDir, "main.nut"), []byte(probe.String()))
	sqLines := viewLines(sqrun.Run(t, sqDir, "main.nut", nil))

	var goLines []string
	for _, gu := range units {
		goLines = append(goLines, runGoView(t, root, gu)...)
	}

	sort.Strings(sqLines)
	sort.Strings(goLines)
	if len(sqLines) != len(goLines) {
		t.Fatalf("the Squirrel view printed %d rows, the Go view printed %d", len(sqLines), len(goLines))
	}
	fields := 0
	for i := range sqLines {
		if strings.HasPrefix(sqLines[i], "VIEW|") {
			fields++
		}
		if sqLines[i] != goLines[i] {
			t.Errorf("view row %d differs\n squirrel: %s\n       go: %s", i, sqLines[i], goLines[i])
		}
	}
	if fields == 0 {
		t.Fatal("compared no fields; the view is not reached")
	}
	if len(units) < 2 {
		t.Fatalf("compared %d units; the manifest is not reached", len(units))
	}
	t.Logf("compared %d fields across %d units", fields, len(units))
}

// viewLines splits a probe's output into sorted-edge-preserving rows.
func viewLines(out []byte) []string {
	var lines []string
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		if strings.HasPrefix(line, "INFO|") || strings.HasPrefix(line, "VIEW|") {
			lines = append(lines, line)
		}
	}
	return lines
}

// runGoView generates the Go table surface for one unit, adds a probe that
// prints TableFields and TableInfo, and runs it in the module's t.TempDir().
func runGoView(t *testing.T, root string, gu viewUnit) []string {
	t.Helper()
	files, err := golang.Generate(gu.unit)
	if err != nil {
		t.Fatalf("unit %s: the Go type surface refused: %v", gu.key, err)
	}
	tables, err := gotable.GenerateRuntime(gu.unit, nil, gotable.RuntimeStandalone)
	if err != nil {
		t.Fatalf("unit %s: gotable refused: %v", gu.key, err)
	}
	maps.Copy(files, tables)
	dir := filepath.Join(t.TempDir(), gu.key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	needsSerialize := false
	for name, data := range files {
		if strings.Contains(string(data), `"github.com/mas-bandwidth/serialize.go/v2"`) {
			needsSerialize = true
		}
		writeFile(t, filepath.Join(dir, filepath.Base(name)), data)
	}
	goMod := "module probe\n\ngo 1.26\n"
	if needsSerialize {
		// the Go oracle's runtime is the one the module pins (go.mod), found
		// the way the gotable probes find it
		runtime := slowtest.SerializeGo(t, "../../../")
		goMod += fmt.Sprintf("\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime)
	}
	writeFile(t, filepath.Join(dir, "go.mod"), []byte(goMod))

	var calls strings.Builder
	for _, name := range gu.names {
		// gotable spells every identifier in Go's initialisms (a generated map
		// entry FleetByIdEntry is FleetByIDEntry there); the row keeps the
		// schema name both legs print
		goName := goname.Initialisms(name)
		fmt.Fprintf(&calls, "\twriteInfo(%q, %q, %t, %sTableInfo)\n\tfor _, f := range %sTableFields {\n\t\twriteField(%q, %q, f)\n\t}\n",
			gu.key, name, gu.unit.Tables[name] != nil && gu.unit.Tables[name].IsTable, goName, goName, gu.key, name)
	}
	writeFile(t, filepath.Join(dir, "view_test.go"),
		[]byte(fmt.Sprintf(viewGoTest, gu.unit.Package, calls.String())))

	outPath := filepath.Join(dir, "view.txt")
	cmd := exec.Command("go", "test", "-count=1", "-timeout", "120s", "-run", "^TestSquirrelViewPrint$", ".")
	cmd.Dir = dir
	cmd.Env = viewEnv(outPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("unit %s: the Go view probe failed: %v\n%s", gu.key, err, out)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("unit %s: the Go view probe wrote no output: %v", gu.key, err)
	}
	return viewLines(data)
}

// viewEnv is the Go probe's environment: its output file, and module flags
// that keep resolution local to the t.TempDir() module and the cache.
func viewEnv(out string) []string {
	env := make([]string, 0, len(os.Environ())+4)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GOFLAGS=") || strings.HasPrefix(kv, "GOPROXY=") || strings.HasPrefix(kv, "VIEW_OUT=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GOFLAGS=-mod=mod", "GOPROXY=off", "VIEW_OUT="+out)
}
