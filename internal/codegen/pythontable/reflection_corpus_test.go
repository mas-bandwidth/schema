package pythontable

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/viewlisting"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const reflectionListingProbe = pyrun.ImportPrelude + `import importlib, struct, sys
v = importlib.import_module(sys.argv[1]).UnitView()
def ann(x):
    return 'tags=[' + ','.join(x.tags) + '] doc=' + x.doc.replace('\\', '\\\\').replace('\n', '\\n')
def boolean(x): return 'true' if x else 'false'
def write(*v): print(*v)
write('unit package=%s protocol=%016x' % (v.package, v.protocol_id))
for c in v.constants:
    bits = struct.unpack('<Q', struct.pack('<d', c.float_value))[0] if c.is_float else 0
    write('constant %s file=%s type=%s float=%s int=%d real=%016x %s' % (c.name,c.file,c.type_name,boolean(c.is_float),c.int_value,bits,ann(c)))
for label, vocab in [('enum', v.enums), ('flags', v.flags), ('union', v.unions)]:
    for e in vocab:
        write('%s %s file=%s max=%d bits=%d variants=%d %s' % (label,e.name,e.file,e.max,e.storage_bits,len(e.variants),ann(e)))
        holder = any(f.target == e.name for d in v.types + v.tables for f in d.type.fields)
        for a in e.variants:
            if label == 'enum':
                write('enum %s variant %d %s id=%016x %s' % (e.name,a.value,a.name,a.id,ann(a)))
            elif label == 'flags':
                write('flags %s bit %d %s id=%016x %s' % (e.name,a.value,a.name,a.id,ann(a)))
            else:
                kind = str(a.field.elem_kind or a.field.kind) if a.field else '-'
                bound = str(a.field.bound if a.field.array else (0 if a.field.pointer else a.field.size)) if a.field else '-'
                overlay = '0' if a.field and holder else '-'
                write('union %s arm %d %s id=%016x payload=%s record=%s field=%s kind=%s bound=%s overlay=%s %s' % (e.name,a.value,a.name,a.id,a.payload_name or '-', 'yes' if a.payload else 'no', 'yes' if a.field else 'no',kind,bound,overlay,ann(a)))
for label, group in [('type', v.types), ('table', v.tables)]:
    for d in group:
        write('%s %s file=%s fields=%d %s' % (label,d.name,d.file,len(d.type.fields),ann(d)))
        for f in d.type.fields:
            write('%s %s field %s json=%s type=%s id=%016x optional=%s %s' % (label,d.name,f.name,f.json_key or '-',f.type_name,f.field_id,boolean(f.optional),ann(f)))
`

type reflectionUnit struct {
	name  string
	paths []string
}

func reflectionUnits(t *testing.T) []reflectionUnit {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	entries := viewlisting.CorpusEntries(string(makefile))
	if len(entries) == 0 {
		t.Fatal("empty VIEW_CORPUS")
	}
	var units []reflectionUnit
	for _, entry := range entries {
		dir, _, ok := strings.Cut(entry, ":")
		if !ok {
			t.Fatalf("bad corpus entry %q", entry)
		}
		src := filepath.Join(root, "tables", dir)
		if dir == "wide" {
			src = filepath.Join(root, "examples-wide")
		}
		units = append(units, reflectionUnit{"corpus-" + dir, []string{src}})
	}
	_, manifest := blobManifestRows(t, filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt"))
	names := make([]string, 0, len(manifest))
	for name := range manifest {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		var paths []string
		for _, p := range manifest[name] {
			paths = append(paths, filepath.Join(root, p))
		}
		units = append(units, reflectionUnit{"manifest-" + name, paths})
	}
	return units
}

func reflectionGenerate(u *ir.Unit) (map[string][]byte, error) {
	if len(u.Tables) > 0 {
		return Generate(u)
	}
	files, err := python.Generate(u)
	if err != nil {
		return nil, err
	}
	if err = AddView(u, files); err != nil {
		return nil, err
	}
	return files, nil
}

func reflectionListing(t *testing.T, u *ir.Unit, files map[string][]byte, floor bool) string {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs := map[string][]byte{"main.py": []byte(strings.Replace(reflectionListingProbe, "sys.argv[1]", strconv.Quote(u.Package), 1))}
	bin := pyrun.Interpreter(t)
	if floor {
		bin = pyrun.Floor(t)
	}
	out, stderr, err := pyrun.Try(t, bin, dir, "main.py", inputs)
	if err != nil {
		t.Fatalf("view probe: %v\n%s", err, stderr)
	}
	return out
}

var reflectionFieldName = regexp.MustCompile(`(?:^|[[:space:]\(])name="([^"]+)"`)

var reflectionUnsupportedKind = regexp.MustCompile(`(?m)(?:^|[[:space:]])(?:codec|elem_codec|inner)="unsupported"`)

func TestPythonReflectionCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	units := reflectionUnits(t)
	for _, row := range units {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			u := loadUnitPaths(t, row.paths...)
			files, err := reflectionGenerate(u)
			if err != nil {
				t.Fatalf("kind hole; unit was not compared: %v", err)
			}
			for path, body := range files {
				if strings.Contains(path, "/_schema/") || !strings.HasSuffix(path, ".py") {
					continue
				}
				for _, loc := range reflectionUnsupportedKind.FindAllIndex(body, -1) {
					line := 1 + strings.Count(string(body[:loc[0]]), "\n")
					field := "unknown"
					names := reflectionFieldName.FindAllSubmatch(body[:loc[0]], -1)
					if len(names) > 0 {
						field = string(names[len(names)-1][1])
					}
					t.Errorf("%s:%d: unsupported field or element %s", path, line, field)
				}
			}
			want := viewlisting.Listing(u)
			for _, floor := range []bool{false, true} {
				if got := reflectionListing(t, u, files, floor); got != want {
					t.Errorf("floor=%v listing differs:\n%s", floor, reflectionFirstDiff(want, got))
				}
			}
		})
	}
}

func reflectionFirstDiff(want, got string) string {
	a, b := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(a) || i < len(b); i++ {
		x, y := "<missing>", "<missing>"
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return fmt.Sprintf("line %d\nwant: %s\ngot:  %s", i+1, x, y)
		}
	}
	return ""
}

func TestPythonReflectionCorpusNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreter")
	cases := []struct{ name, file, from, to, line string }{
		{"drop-table", "_schema/unit_view.py", `    ViewType("Record", "Records.schema", True, _descriptors["Record"], "Record documentation.", ("visible", )),` + "\n", "", "union Choice arm 1"},
		{"table-doc", "_schema/unit_view.py", `"Record documentation.", ("visible", ))`, `"drift", ("visible", ))`, "table Record"},
		{"arm-doc", "_schema/unit_view.py", `"Arm documentation.", ("arm_tag", ))`, `"drift", ("arm_tag", ))`, "union Choice arm 1"},
		{"field-key", "Core.py", `json_key="value", type_name="int32"`, `json_key="drift", type_name="int32"`, "table Record field value"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			u := loadUnit(t, reflectionDocsSchema)
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.ToSlash(filepath.Join(u.Package, tc.file))
			src := string(files[path])
			if !strings.Contains(src, tc.from) {
				t.Fatalf("%s control anchor absent", tc.name)
			}
			files[path] = []byte(strings.Replace(src, tc.from, tc.to, 1))
			for _, floor := range []bool{false, true} {
				got := reflectionListing(t, u, files, floor)
				want := viewlisting.Listing(u)
				diff := reflectionFirstDiff(want, got)
				if tc.name == "drop-table" && strings.Contains(got, "table Record file=") {
					t.Fatal("dropped row still listed")
				}
				if got == want || !strings.Contains(diff, tc.line) {
					t.Fatalf("%s sabotage did not fail on %s: %s", tc.name, tc.line, diff)
				}
			}
		})
	}
}

func TestPythonReflectionViewIsLazy(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, reflectionDocsSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `import sys
import core
import typing
assert 'return' in typing.get_type_hints(core.UnitView)
assert 'core._schema.unit_view' not in sys.modules
assert core.UnitView() is core.UnitView()
assert 'core._schema.unit_view' in sys.modules
`
	for _, floor := range []bool{false, true} {
		if floor {
			pyrun.RunFloor(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		} else {
			pyrun.Run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		}
	}
}

const reflectionVocabularySchema = `package vocabcheck
const Limit = 42
/// Reached vocabulary.
enum Grade { Red, Blue }
enum Loose { North, South }
type Unreached { value Loose }
type Shared { value Grade }
type Slots { values [Loose]int32 }
table Record { rank Grade }
`

func TestPythonReflectionVocabularyNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned Python interpreter")
	u := loadUnit(t, reflectionVocabularySchema)
	for _, name := range []string{"constant", "enum-id", "type-id"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.ToSlash(filepath.Join(u.Package, "_schema/unit_view.py"))
			src := string(files[path])
			from, to, line := "", "", ""
			switch name {
			case "constant":
				from = `ViewConstant("Limit", "Records.schema", "int64", False, 42, 0.0`
				to = `ViewConstant("Limit", "Records.schema", "int64", False, 43, 0.0`
				line = "constant Limit"
			case "enum-id":
				id := ir.TableWireId(u.Enums["Grade"].VariantWireName(0))
				from = fmt.Sprintf(`ViewVariant(1, "Red", %d,`, id)
				to = fmt.Sprintf(`ViewVariant(1, "Red", %d,`, id+1)
				line = "enum Grade variant 1"
			case "type-id":
				from = `name="value", field_id=0,`
				to = `name="value", field_id=1,`
				line = "type Shared field value"
			}
			if !strings.Contains(src, from) {
				t.Fatalf("%s anchor absent: %q", name, from)
			}
			files[path] = []byte(strings.Replace(src, from, to, 1))
			for _, floor := range []bool{false, true} {
				got := reflectionListing(t, u, files, floor)
				want := viewlisting.Listing(u)
				diff := reflectionFirstDiff(want, got)
				if got == want || !strings.Contains(diff, line) {
					t.Fatalf("%s drift not detected on %s: %s", name, line, diff)
				}
			}
		})
	}
}

// reflectionStrictShards bounds the per-commit rows: strict mypy over one unit
// takes 9-20 s at two cores, so the whole corpus cannot fit one row; two units
// per shard keeps the slowest shard near 35 s. The make target
// python-reflection still runs every shard, and the test with no selector
// runs every unit.
const reflectionStrictShards = 24

func reflectionStrictShard(value string) (int, error) {
	if value == "" {
		return -1, nil
	}
	shard, err := strconv.Atoi(value)
	if err != nil || shard < 0 || shard >= reflectionStrictShards || strconv.Itoa(shard) != value {
		return 0, fmt.Errorf("invalid PYTHON_REFLECTION_STRICT_SHARD %q", value)
	}
	return shard, nil
}

func reflectionStrictSelected(shard, index int) bool {
	return shard < 0 || index%reflectionStrictShards == shard
}

func TestPythonReflectionStrictShardPlan(t *testing.T) {
	units := reflectionUnits(t)
	if len(units) < reflectionStrictShards {
		t.Fatalf("%d units cannot fill %d shards", len(units), reflectionStrictShards)
	}
	seen := make(map[string]int)
	for shard := range reflectionStrictShards {
		selected := 0
		for index, unit := range units {
			if reflectionStrictSelected(shard, index) {
				seen[unit.name]++
				selected++
			}
		}
		if selected == 0 {
			t.Fatalf("shard %d is empty", shard)
		}
	}
	if len(seen) != len(units) {
		t.Fatalf("partition covered %d units, want %d", len(seen), len(units))
	}
	for name, count := range seen {
		if count != 1 {
			t.Errorf("unit %s selected %d times", name, count)
		}
	}
	for index := range units {
		if !reflectionStrictSelected(-1, index) {
			t.Fatalf("no selector skipped unit %d", index)
		}
	}
	for _, invalid := range []string{"-1", strconv.Itoa(reflectionStrictShards), "00", "x"} {
		if _, err := reflectionStrictShard(invalid); err == nil {
			t.Errorf("accepted invalid shard %q", invalid)
		}
	}
}

func TestPythonReflectionStrictTypes(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "pinned strict mypy")
	shard, err := reflectionStrictShard(os.Getenv("PYTHON_REFLECTION_STRICT_SHARD"))
	if err != nil {
		t.Fatal(err)
	}
	for index, row := range reflectionUnits(t) {
		if !reflectionStrictSelected(shard, index) {
			continue
		}
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			u := loadUnitPaths(t, row.paths...)
			files, err := reflectionGenerate(u)
			if err != nil {
				t.Fatalf("kind hole: %v", err)
			}
			dir := t.TempDir()
			writeTree(t, dir, files)
			if out, err := pyrun.Analyze(t, dir, u.Package); err != nil {
				t.Fatalf("strict mypy:\n%s", out)
			}
		})
	}
}

func TestPythonReflectionDetachedVocabularyIDs(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, reflectionVocabularySchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	red := ir.TableWireId(u.Enums["Grade"].VariantWireName(0))
	probe := pyrun.ImportPrelude + fmt.Sprintf(`import vocabcheck
v=vocabcheck.UnitView()
types={x.name:x.type for x in v.types}
assert types['Shared'].fields[0].variant_ids[0] == %d
assert all(x == 0 for x in types['Unreached'].fields[0].variant_ids)
assert all(x == 0 for x in types['Slots'].fields[0].keys)
`, red)
	for _, floor := range []bool{false, true} {
		if floor {
			pyrun.RunFloor(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		} else {
			pyrun.Run(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		}
	}
}

func TestPythonReflectionKindHoleNegativeControl(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, reflectionDocsSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.ToSlash(filepath.Join(u.Package, "Core.py"))
	src := string(files[path])
	if reflectionUnsupportedKind.MatchString(src) {
		t.Fatal("clean field was already unsupported")
	}
	anchor := `codec="int"`
	if !strings.Contains(src, anchor) {
		t.Fatal("kind control anchor absent")
	}
	broken := strings.Replace(src, anchor, `codec="unsupported"`, 1)
	if !reflectionUnsupportedKind.MatchString(broken) {
		t.Fatal("unsupported descriptor escaped the reflection kind check")
	}
}
