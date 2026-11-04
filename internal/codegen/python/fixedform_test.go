package python

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE FIXED FORM, held to the C++ reference's byte oracle
// (docs/FIXED-FORM-ALGORITHM.md §5.7, §7): the reference's corpus
// (build/fixedform-corpus, make tables-fixedform-corpus) read through the
// generated Python under both pinned interpreters, every manifest value
// landing and the save written back byte for byte; every versioning row's two
// columns; and every refusal by its name, as the strict-only contract states
// them.

func fixedCorpus(t *testing.T) string {
	t.Helper()
	dir := repo("build", "fixedform-corpus")
	if _, err := os.Stat(filepath.Join(dir, "manifest.txt")); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") == "" {
			t.Skip("the C++ reference's fixed-form corpus is absent (make tables-fixedform-corpus); SCHEMA_REQUIRE_CORPUS unset")
		}
		t.Fatalf("the fixed-form corpus is absent: build it with make tables-fixedform-corpus: %v", err)
	}
	return dir
}

func corpusFile(t *testing.T, corpus, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(corpus, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// a manifest line: records= and the values, a float by its bits and a
// fixed-point value by its raw integer.
type manifestLine struct {
	records int
	values  map[string]string
}

var manifestValueSplit = regexp.MustCompile(`,(r\d+\.)`)

func readFixedManifest(t *testing.T, corpus string) map[string]manifestLine {
	t.Helper()
	out := map[string]manifestLine{}
	for line := range strings.SplitSeq(string(corpusFile(t, corpus, "manifest.txt")), "\n") {
		head, values, _ := strings.Cut(line, " values=")
		var file string
		ml := manifestLine{values: map[string]string{}}
		for field := range strings.FieldsSeq(head) {
			k, v, _ := strings.Cut(field, "=")
			switch k {
			case "file":
				file = v
			case "records":
				ml.records, _ = strconv.Atoi(v)
			}
		}
		if file == "" {
			continue
		}
		for kv := range strings.SplitSeq(manifestValueSplit.ReplaceAllString(values, "\x00$1"), "\x00") {
			if k, v, ok := strings.Cut(kv, "="); ok {
				if _, raw, ok := strings.Cut(v, "|raw="); ok {
					v = "raw=" + raw
				} else if _, bits, ok := strings.Cut(v, "|0x"); ok {
					v = "0x" + strings.ToUpper(bits)
				}
				ml.values[k] = v
			}
		}
		out[file] = ml
	}
	return out
}

// fixedUnit is a unit's generated package, with a handed lineage, written to
// a fresh directory beside the fixed probe.
type fixedUnit struct {
	dir   string
	pkg   string
	files map[string][]byte
}

func newFixedUnit(t *testing.T, u *ir.Unit, lineage map[string][]FixedLineageEntry) *fixedUnit {
	t.Helper()
	files, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := os.ReadFile(filepath.Join("testdata", "probe_fixed.py"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pyrun.WriteTree(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "main.py"), probe, 0o644); err != nil {
		t.Fatal(err)
	}
	return &fixedUnit{dir: dir, pkg: u.Package, files: files}
}

// patch rewrites one line of the unit's emitted runtime fixed.py, which must
// hold the anchor exactly once: a control's sabotage.
func (g *fixedUnit) patch(t *testing.T, anchor, with string) {
	t.Helper()
	p := filepath.Join(g.dir, g.pkg, "_schema", "fixed.py")
	src, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(src), anchor); n != 1 {
		t.Fatalf("the anchor %q is in fixed.py %d times, want once", anchor, n)
	}
	if err := os.WriteFile(p, []byte(strings.Replace(string(src), anchor, with, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// patchModule rewrites one line of a generated module, which must hold the
// anchor exactly once: a control's sabotage of the emitted identity reader.
func (g *fixedUnit) patchModule(t *testing.T, module, anchor, with string) {
	t.Helper()
	p := filepath.Join(g.dir, g.pkg, module+".py")
	src, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(src), anchor); n != 1 {
		t.Fatalf("the anchor %q is in %s.py %d times, want once", anchor, module, n)
	}
	if err := os.WriteFile(p, []byte(strings.Replace(string(src), anchor, with, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

type fixedRun struct {
	head  string
	dump  map[string]string
	saved []byte
}

// load runs the probe under one pin: mod is the schema file's module, root the
// table it reads.
func (g *fixedUnit) load(t *testing.T, p pin, mod, root string, file []byte) fixedRun {
	t.Helper()
	out := p.run(t, g.dir, "main.py", map[string][]byte{"file": file, "args": []byte(g.pkg + " " + mod + " " + root)})
	return parseFixedRun(t, string(out))
}

// try is load for a sabotaged runtime, whose probe may crash: the crash is the
// answer's head.
func (g *fixedUnit) try(t *testing.T, python, mod, root string, file []byte) string {
	t.Helper()
	stdout, stderr, err := pyrun.Try(t, python, g.dir, "main.py", map[string][]byte{"file": file, "args": []byte(g.pkg + " " + mod + " " + root)})
	if err != nil {
		return "crashed " + strings.TrimSpace(stderr)
	}
	return parseFixedRun(t, stdout).head
}

func parseFixedRun(t *testing.T, out string) fixedRun {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	r := fixedRun{head: lines[0], dump: map[string]string{}}
	for _, l := range lines[1:] {
		if h, ok := strings.CutPrefix(l, "x"); ok {
			b, err := hex.DecodeString(h)
			if err != nil {
				t.Fatal(err)
			}
			r.saved = b
			continue
		}
		k, v, _ := strings.Cut(l, "=")
		r.dump[k] = v
	}
	return r
}

// compareManifest holds every manifest value the reader names to the value it
// landed; a key the reader does not name (a wide string's units) is not
// compared. It answers how many compared.
func compareManifest(t *testing.T, what string, ml manifestLine, dump map[string]string) int {
	t.Helper()
	compared := 0
	for k, want := range ml.values {
		got, ok := dump[k]
		if !ok || strings.HasPrefix(want, "u\"") || strings.Contains(want, "\\") {
			continue
		}
		if raw, ok := strings.CutPrefix(got, "raw="); ok && !strings.HasPrefix(want, "raw=") {
			got = raw
		}
		if widened, ok := widenedFloat32(want, got); ok {
			want = widened
		}
		if got != want {
			t.Errorf("%s: %s landed %s, the reference wrote %s", what, k, got, want)
		}
		compared++
	}
	return compared
}

// widenedFloat32 is the float64 pattern a float32 pattern widens to.
func widenedFloat32(want, got string) (string, bool) {
	if len(want) != 10 || len(got) != 18 || !strings.HasPrefix(want, "0x") || !strings.HasPrefix(got, "0x") {
		return "", false
	}
	bits, err := strconv.ParseUint(want[2:], 16, 32)
	if err != nil {
		return "", false
	}
	return fmt.Sprintf("0x%016X", math.Float64bits(float64(math.Float32frombits(uint32(bits))))), true
}

func moduleOf(schema string) string {
	return strings.TrimSuffix(filepath.Base(schema), ".schema")
}

var fixedOracleVectors = []struct{ file, schema, root string }{
	{"fx1.bin", "test/tables/FX1.schema", "FxRoot"},
	{"fx2.bin", "test/tables/FX2.schema", "FxRoot"},
	{"p1.bin", "test/tables/P1.schema", "Chain"},
	{"p3.bin", "test/tables/P3.schema", "Chain"},
	{"keyed.bin", "tables/examples/Keyed.schema", "KeyedConfig"},
	{"pack.bin", "tables/examples/Pack.schema", "PackConfig"},
	{"fxw.bin", "test/tables/FXW.schema", "FxWide"},
	{"fu1.bin", "test/tables/FU1.schema", "FuRoot"},
	{"fu2.bin", "test/tables/FU2.schema", "FuRoot"},
}

// TestPythonFixedFormAgreesWithTheByteOracle is §7 row 1 over the reference's
// nine corpus files, under both pins: each reads, every manifest value lands,
// the record count is the manifest's, nothing widens, and the save is the
// reference's file byte for byte. fu1 and fu2 carry a subnormal and a -0.
func TestPythonFixedFormAgreesWithTheByteOracle(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	manifest := readFixedManifest(t, corpus)
	for _, v := range fixedOracleVectors {
		t.Run(strings.TrimSuffix(v.file, ".bin"), func(t *testing.T) {
			t.Parallel()
			g := newFixedUnit(t, pyrun.LoadUnit(t, repo(v.schema)), nil)
			oracle := corpusFile(t, corpus, v.file)
			ml := manifest[v.file]
			for _, p := range pins {
				r := g.load(t, p, moduleOf(v.schema), v.root, oracle)
				if want := fmt.Sprintf("ok %d w0", ml.records); r.head != want {
					t.Fatalf("%s: the read: %s (%s), want %s", p.name, r.head, r.dump["detail"], want)
				}
				if n := compareManifest(t, v.file, ml, r.dump); n == 0 {
					t.Fatalf("%s: no manifest value compared", p.name)
				}
				if !bytes.Equal(r.saved, oracle) {
					t.Fatalf("%s: the save is not the reference's bytes: %d saved, %d in the file", p.name, len(r.saved), len(oracle))
				}
			}
		})
	}
}

// TestPythonFixedFormByteOracleNegativeControl turns one integer store of the
// writer into a no-op and requires fx1's write-back to differ from the
// reference's bytes: the oracle comparison is what the gate holds.
func TestPythonFixedFormByteOracleNegativeControl(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	g := newFixedUnit(t, pyrun.LoadUnit(t, repo("test/tables/FX1.schema")), nil)
	oracle := corpusFile(t, corpus, "fx1.bin")
	if r := g.load(t, pins[0], "FX1", "FxRoot", oracle); !bytes.Equal(r.saved, oracle) {
		t.Fatal("the control, unpatched: the save is not the reference's bytes")
	}
	g.patch(t, `        out[off : off + node.size] = (v & ((1 << bits) - 1)).to_bytes(node.size, "little")`, `        pass`)
	if r := g.load(t, pins[0], "FX1", "FxRoot", oracle); bytes.Equal(r.saved, oracle) {
		t.Fatal("an integer store skipped still saves the reference's bytes: the gate proves nothing")
	}
}

func readTestSchema(t *testing.T, base string) *ir.Unit {
	t.Helper()
	return pyrun.LoadUnit(t, repo("test", "tables", base+".schema"))
}

// rootOf is §5.9 #9's rule: the file's root is the fixed root no other fixed
// table names by value.
func rootOf(t *testing.T, u *ir.Unit) string {
	t.Helper()
	roots := ir.TableFixedFormRoots(u)
	named := map[string]bool{}
	for _, st := range roots {
		for _, f := range st.Fields {
			named[f.Type.Name] = true
		}
	}
	for _, st := range roots {
		if !named[st.Name] {
			return st.Name
		}
	}
	t.Fatal("no fixed root")
	return ""
}

// lineageOf is the lineage older units lock, oldest first, the first retire
// of them retired; each entry carries its writer.
func lineageOf(t *testing.T, older []*ir.Unit, retire int) map[string][]FixedLineageEntry {
	t.Helper()
	out := map[string][]FixedLineageEntry{}
	for i, o := range older {
		for _, st := range ir.TableFixedFormRoots(o) {
			e, ok := FixedLineageOf(o, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			e.Retired = i < retire
			out[st.Name] = append(out[st.Name], e)
		}
	}
	return out
}

// the versioning rows the corpus carries an old_ and a new_ file for
// (docs/FIXED-FORM-ALGORITHM.md §5.7).
type versionRow struct {
	row      string
	sameHash bool   // the edit moves no layout byte and no digest byte: it reads both ways
	widened  int    // per record; -1: at least one
	refuse   string // NEW-READS-OLD refuses by this name: the pair is not a lawful widening
	noNewer  bool   // the corpus carries no new_ file: the newer writer's file is framed from its unit
}

var versionRows = []versionRow{
	{row: "array_bounded_grow"},
	{row: "array_elem_widen", widened: 4},
	{row: "array_fixed_grow"},
	{row: "bits_grow", widened: -1}, // bits(8) to bits(12) climbs the u8 to u16 rung: a widen (§5.2 EMIT)
	{row: "bytes_grow"},
	{row: "cfloat_range_widen"},
	{row: "cfloat_res_refine"},
	{row: "constant_grow"},
	{row: "enum_append"},
	{row: "enum_width", widened: -1},
	{row: "field_append"},
	{row: "field_deprecate", sameHash: true},
	{row: "field_undeprecate", sameHash: true},
	{row: "fixed_I_grow", widened: -1},
	{row: "fixed_I_grow_element", widened: -1},
	{row: "flags_append"},
	{row: "float_widen", widened: -1},
	{row: "int_widen", widened: -1},
	{row: "keyed_array_enum_append"},
	{row: "nested_append"},
	{row: "optional_add"},
	{row: "range_widen"},
	{row: "rename_without_was", sameHash: true},
	{row: "string_grow"},
	{row: "uint_widen", widened: -1},
	{row: "union_append"},
	{row: "union_arm_payload_widen"},
	// a writer field the reader cannot name is not a lawful widening under
	// the one read (docs/FIXED-FORM-ALGORITHM.md §5.2 MATCH, §5.4): no plan is
	// laid for the entry, and a file under its hash refuses layout_malformed
	{row: "unknown_census", refuse: "layout_malformed", noNewer: true},
	{row: "wstring_grow"},
}

// TestPythonFixedVersioningNewReadsOld is every row's NEW-READS-OLD column:
// the newer reader, handed the older unit's entry as its lineage, reads the
// reference's old_ file: every manifest value lands, and widened counts what
// the row widened (an append is not an event). A row the strict read refuses
// refuses by its name.
func TestPythonFixedVersioningNewReadsOld(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	manifest := readFixedManifest(t, corpus)
	for _, r := range versionRows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			older, newer := readTestSchema(t, "VOLD_"+r.row), readTestSchema(t, "VNEW_"+r.row)
			rootName := rootOf(t, older)
			g := newFixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
			file := "old_" + r.row + ".bin"
			ml := manifest[file]
			for _, p := range pins {
				got := g.load(t, p, "VNEW_"+r.row, rootName, corpusFile(t, corpus, file))
				if r.refuse != "" {
					if got.head != "refused "+r.refuse+" -" {
						t.Fatalf("%s: NEW-READS-OLD %s: %s, want refused %s", p.name, r.row, got.head, r.refuse)
					}
					continue
				}
				if !strings.HasPrefix(got.head, fmt.Sprintf("ok %d w", ml.records)) {
					t.Fatalf("%s: NEW-READS-OLD %s: %s", p.name, r.row, got.head)
				}
				w, _ := strconv.Atoi(got.head[strings.LastIndex(got.head, "w")+1:])
				switch {
				case r.widened > 0 && w != r.widened*ml.records:
					t.Errorf("%s: widened %d, the row owes exactly %d per record", p.name, w, r.widened)
				case r.widened < 0 && w == 0:
					t.Errorf("%s: a widening row moved no widened counter", p.name)
				case r.widened == 0 && w != 0:
					t.Errorf("%s: widened %d, and an append is not an event", p.name, w)
				}
				if n := compareManifest(t, file, ml, got.dump); n == 0 {
					t.Errorf("%s: no manifest value compared", p.name)
				}
			}
		})
	}
}

// TestPythonFixedVersioningOptionalAddOwesPresent (§5.9 #40): T read into ?T
// lands present, the old writer's value always being there.
func TestPythonFixedVersioningOptionalAddOwesPresent(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	older, newer := readTestSchema(t, "VOLD_optional_add"), readTestSchema(t, "VNEW_optional_add")
	g := newFixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
	got := g.load(t, pins[0], "VNEW_optional_add", "OptionalAdd", corpusFile(t, corpus, "old_optional_add.bin"))
	if got.dump["r0.link_present"] != "1" || got.dump["r0.link.value"] != "777" {
		t.Fatalf("T into ?T: %s %v", got.head, got.dump)
	}
}

// TestPythonFixedVersioningOldRefusesNew is every row's OLD-REFUSES-NEW
// column: the older reader refuses the newer file layout_unknown before any
// record, reporting the file's hash — except a row whose edit moved no
// layout byte and no digest byte, which reads both ways.
func TestPythonFixedVersioningOldRefusesNew(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	for _, r := range versionRows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			older := readTestSchema(t, "VOLD_"+r.row)
			rootName := rootOf(t, older)
			g := newFixedUnit(t, older, nil)
			var data []byte
			if r.noNewer {
				data = newerWriterFile(t, readTestSchema(t, "VNEW_"+r.row), rootName)
			} else {
				data = corpusFile(t, corpus, "new_"+r.row+".bin")
			}
			for _, p := range pins {
				got := g.load(t, p, "VOLD_"+r.row, rootName, data)
				if r.sameHash {
					if !strings.HasPrefix(got.head, "ok ") {
						t.Fatalf("%s: a row that moved no layout byte reads both ways: %s", p.name, got.head)
					}
					continue
				}
				if want := fmt.Sprintf("refused layout_unknown #%016x", binary.LittleEndian.Uint64(data[8:16])); got.head != want {
					t.Fatalf("%s: OLD-REFUSES-NEW %s: %s, want %s", p.name, r.row, got.head, want)
				}
			}
		})
	}
}

// newerWriterFile is a form-3 file as the newer unit frames it: its own
// layout and hash, and one zeroed record.
func newerWriterFile(t *testing.T, newer *ir.Unit, rootName string) []byte {
	t.Helper()
	e, ok := FixedLineageOf(newer, rootName)
	if !ok {
		t.Fatalf("no fixed root %s", rootName)
	}
	out := []byte{3, 0, 0, 0, 0, 0, 0, 0}
	out = binary.LittleEndian.AppendUint64(out, e.Wire)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(e.Layout)))
	out = append(out, e.Layout...)
	out = binary.LittleEndian.AppendUint64(out, e.Wire)
	return append(out, make([]byte, e.Record-8)...)
}

// TestPythonFixedVersioningFloor: below the floor refuses layout_unsupported
// with the file's hash, at and above it reads; the control, no floor, reads.
func TestPythonFixedVersioningFloor(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	older, mid, newer := readTestSchema(t, "VOLD_floor"), readTestSchema(t, "VMID_floor"), readTestSchema(t, "VNEW_floor")
	g := newFixedUnit(t, newer, lineageOf(t, []*ir.Unit{older, mid}, 1))
	old := corpusFile(t, corpus, "old_floor.bin")
	for _, p := range pins {
		if got := g.load(t, p, "VNEW_floor", "Floored", old).head; got != fmt.Sprintf("refused layout_unsupported #%016x", binary.LittleEndian.Uint64(old[8:16])) {
			t.Errorf("%s: below the floor: %s", p.name, got)
		}
		if got := g.load(t, p, "VNEW_floor", "Floored", corpusFile(t, corpus, "mid_floor.bin")).head; !strings.HasPrefix(got, "ok 1 ") {
			t.Errorf("%s: at the floor: %s", p.name, got)
		}
		if got := g.load(t, p, "VNEW_floor", "Floored", corpusFile(t, corpus, "new_floor.bin")).head; !strings.HasPrefix(got, "ok 1 ") {
			t.Errorf("%s: the reader's own file: %s", p.name, got)
		}
	}
	bare := newFixedUnit(t, newer, lineageOf(t, []*ir.Unit{older, mid}, 0))
	if got := bare.load(t, pins[0], "VNEW_floor", "Floored", old).head; !strings.HasPrefix(got, "ok 1 ") {
		t.Errorf("the control, no floor: %s", got)
	}
}

// TestPythonFixedVersioningLineageMerge (§5.1, bill §11.3): after a merge
// both pre-merge files read on the merged build, the lineage being the base
// and both branches; the control, the branches absent, refuses layout_unknown.
func TestPythonFixedVersioningLineageMerge(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	manifest := readFixedManifest(t, corpus)
	base := readTestSchema(t, "VOLD_lineage_merge")
	a, b := readTestSchema(t, "VBRA_lineage_merge"), readTestSchema(t, "VBRB_lineage_merge")
	merged := readTestSchema(t, "VNEW_lineage_merge")
	rootName := rootOf(t, base)
	g := newFixedUnit(t, merged, lineageOf(t, []*ir.Unit{base, a, b}, 0))
	for _, file := range []string{"a_lineage_merge.bin", "b_lineage_merge.bin"} {
		for _, p := range pins {
			got := g.load(t, p, "VNEW_lineage_merge", rootName, corpusFile(t, corpus, file))
			if got.head != fmt.Sprintf("ok %d w0", manifest[file].records) {
				t.Errorf("%s: %s: %s", p.name, file, got.head)
			}
			if n := compareManifest(t, file, manifest[file], got.dump); n == 0 {
				t.Errorf("%s: %s: no manifest value compared", p.name, file)
			}
		}
	}
	bare := newFixedUnit(t, merged, lineageOf(t, []*ir.Unit{base}, 0))
	if got := bare.load(t, pins[0], "VNEW_lineage_merge", rootName, corpusFile(t, corpus, "a_lineage_merge.bin")).head; !strings.HasPrefix(got, "refused layout_unknown") {
		t.Errorf("the control, branch a absent: %s", got)
	}
}

// TestPythonFixedVersioningNegativeControl breaks the plan's id match and
// requires a row the gate holds green to go red: the match is what the
// versioning gate measures.
func TestPythonFixedVersioningNegativeControl(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	older, newer := readTestSchema(t, "VOLD_field_append"), readTestSchema(t, "VNEW_field_append")
	g := newFixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
	file := corpusFile(t, corpus, "old_field_append.bin")
	if got := g.load(t, pins[0], "VNEW_field_append", "Lineage", file).head; got != "ok 1 w0" {
		t.Fatalf("the control, unpatched: %s", got)
	}
	g.patch(t, "        f = mine.get(c.id)\n", "        f = mine.get(c.id + 1)\n")
	if got := g.try(t, pyrun.Interpreter(t), "VNEW_field_append", "Lineage", file); got == "ok 1 w0" {
		t.Fatalf("the broken match still reads clean: %s", got)
	}
}

// TestPythonFixedLineageLockBugFailsTheBuild (§5.9 #8, #26): a handed entry
// whose layout does not frame, or whose record size is not its layout's, fails
// the build naming the LOCK; the control, a lawful entry, builds.
func TestPythonFixedLineageLockBugFailsTheBuild(t *testing.T) {
	t.Parallel()
	u := readTestSchema(t, "VNEW_field_append")
	good := lineageOf(t, []*ir.Unit{readTestSchema(t, "VOLD_field_append")}, 0)
	if _, err := GenerateLineage(u, good); err != nil {
		t.Fatalf("the control, a lawful entry: %v", err)
	}
	for name, mutate := range map[string]func(e *FixedLineageEntry){
		"layout": func(e *FixedLineageEntry) { e.Layout = e.Layout[:len(e.Layout)-1] },
		"record": func(e *FixedLineageEntry) { e.Record++ },
	} {
		bad := map[string][]FixedLineageEntry{}
		for k, v := range good {
			cp := append([]FixedLineageEntry(nil), v...)
			cp[0].Layout = append([]byte(nil), cp[0].Layout...)
			mutate(&cp[0])
			bad[k] = cp
		}
		if _, err := GenerateLineage(u, bad); err == nil || !strings.Contains(err.Error(), "LOCK") {
			t.Errorf("a lock bug in the %s did not fail the build: %v", name, err)
		}
	}
}

// TestPythonFixedFormRefusesByName is §5.3's refusal table over images cut
// from the reference's fx1.bin, under both pins: each refused by its own name,
// the layout refusals reporting the file's hash. The control, the uncut file,
// reads.
func TestPythonFixedFormRefusesByName(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	oracle := corpusFile(t, corpus, "fx1.bin")
	g := newFixedUnit(t, pyrun.LoadUnit(t, repo("test/tables/FX1.schema")), nil)
	rec := 20 + int(binary.LittleEndian.Uint32(oracle[16:20]))
	body := rec + 8
	cut := func(f func(b []byte) []byte) []byte { return f(append([]byte(nil), oracle...)) }
	// FxRoot: keep u32 @0, narrow u16 @4, renamed @6, gone @10, nested @14
	// (8), label @22 (u32 length + 8), marks @34 (u32 count + 4 x i32),
	// blob @54 (u32 length + 6)
	cases := []struct {
		name string
		file []byte
		want string
	}{
		{"empty", []byte{}, "refused truncated -"},
		{"form 1", []byte{1}, "refused previous_form -"},
		{"form 2", []byte{2, 0, 0}, "refused message_form_as_file -"},
		{"form 9", []byte{9}, "refused newer_form -"},
		{"short header", oracle[:19], "refused truncated -"},
		{"reserved byte", cut(func(b []byte) []byte { b[3] = 1; return b }), "refused reserved_not_zero -"},
		{"layout past the file", cut(func(b []byte) []byte { binary.LittleEndian.PutUint32(b[16:], uint32(len(b))); return b }), "refused layout_malformed -"},
		{"unknown hash", cut(func(b []byte) []byte { b[8] ^= 1; return b }), fmt.Sprintf("refused layout_unknown #%016x", binary.LittleEndian.Uint64(oracle[8:16])^1)},
		{"layout bytes differ", cut(func(b []byte) []byte { b[20+4+8+1] ^= 1; return b }), "refused layout_malformed -"},
		{"ragged tail", oracle[:len(oracle)-1], "refused truncated -"},
		{"record hash", cut(func(b []byte) []byte { b[rec+(len(b)-rec)/2] ^= 1; return b }), "refused no_layout -"},
		{"count past the bound", cut(func(b []byte) []byte { binary.LittleEndian.PutUint32(b[body+34:], 5); return b }), "refused count_over_bound -"},
		{"count below zero", cut(func(b []byte) []byte { binary.LittleEndian.PutUint32(b[body+34:], 0xFFFFFFFF); return b }), "refused count_over_bound -"},
		{"length past the bound", cut(func(b []byte) []byte { binary.LittleEndian.PutUint32(b[body+22:], 9); return b }), "refused length_over_bound -"},
		{"bytes past the bound", cut(func(b []byte) []byte { binary.LittleEndian.PutUint32(b[body+54:], 7); return b }), "refused count_over_bound -"},
		{"ill-formed text", cut(func(b []byte) []byte { b[body+26] = 0xFF; return b }), "refused text_ill_formed -"},
		{"a range past the writer's", cut(func(b []byte) []byte { binary.LittleEndian.PutUint32(b[body+6:], 1001); return b }), "refused out_of_range -"},
	}
	for _, p := range pins {
		for _, c := range cases {
			if got := g.load(t, p, "FX1", "FxRoot", c.file).head; got != c.want {
				t.Errorf("%s: %s: %s, want %s", p.name, c.name, got, c.want)
			}
		}
		if got := g.load(t, p, "FX1", "FxRoot", oracle).head; !strings.HasPrefix(got, "ok 2 ") {
			t.Errorf("%s: the control, the reference's file: %s", p.name, got)
		}
	}
}

// TestPythonFixedFormRefusesTheForgedNaN: the float32 the reference wrote
// into old_float_widen.bin overwritten with a signalling NaN is refused
// non_finite_float by the newer reader, before anything widens.
func TestPythonFixedFormRefusesTheForgedNaN(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	older, newer := readTestSchema(t, "VOLD_float_widen"), readTestSchema(t, "VNEW_float_widen")
	g := newFixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
	file := append([]byte(nil), corpusFile(t, corpus, "old_float_widen.bin")...)
	at := bytes.Index(file, binary.LittleEndian.AppendUint32(nil, 0xC49A5200))
	if at < 0 {
		t.Fatal("old_float_widen.bin does not hold the reference's float32 -1234.5625")
	}
	binary.LittleEndian.PutUint32(file[at:], 0x7F8ABCDE)
	for _, p := range pins {
		if got := g.load(t, p, "VNEW_float_widen", rootOf(t, older), file).head; got != "refused non_finite_float -" {
			t.Errorf("%s: the forged NaN: %s", p.name, got)
		}
	}
}

// TestPythonFixedForgedOrdinalRefusesOnBothPlans (§4.6): an enum ordinal past
// the writer's variants refuses out_of_range on the identity plan and on a
// compiled one; the unforged file reads on both.
func TestPythonFixedForgedOrdinalRefusesOnBothPlans(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	older, newer := readTestSchema(t, "VOLD_enum_append"), readTestSchema(t, "VNEW_enum_append")
	data := corpusFile(t, corpus, "old_enum_append.bin")
	needle := []byte{0x03, 0x09, 0x00, 0x00, 0x00}
	if !bytes.HasSuffix(data, needle) {
		t.Fatalf("the record body (tier=Gold, seq=9) is not the tail of the file: %x", data[len(data)-5:])
	}
	forged := append([]byte(nil), data...)
	forged[len(forged)-5] = 4 // past the writer's three variants
	for _, c := range []struct {
		name, mod string
		g         *fixedUnit
	}{
		{"identity", "VOLD_enum_append", newFixedUnit(t, older, nil)},
		{"compiled", "VNEW_enum_append", newFixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))},
	} {
		for _, p := range pins {
			if got := c.g.load(t, p, c.mod, "Lineage", forged).head; got != "refused out_of_range -" {
				t.Errorf("%s %s: the forged ordinal: %s", p.name, c.name, got)
			}
			if got := c.g.load(t, p, c.mod, "Lineage", data).head; got != "ok 1 w0" {
				t.Errorf("%s %s: the control, the unforged file: %s", p.name, c.name, got)
			}
		}
	}
}

// TestPythonFixedForgedOrdinalNegativeControl removes the ordinal's range
// test from each lane in turn (the emitted identity reader, then the
// runtime's plan) and requires the forged file not to refuse out_of_range.
func TestPythonFixedForgedOrdinalNegativeControl(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	older, newer := readTestSchema(t, "VOLD_enum_append"), readTestSchema(t, "VNEW_enum_append")
	forged := append([]byte(nil), corpusFile(t, corpus, "old_enum_append.bin")...)
	forged[len(forged)-5] = 4
	identity := newFixedUnit(t, older, nil)
	identity.patchModule(t, "VOLD_enum_append", `raise _s_rt.Refused("out_of_range", "Lineage.tier")`, "pass")
	if got := identity.try(t, pyrun.Interpreter(t), "VOLD_enum_append", "Lineage", forged); got == "refused out_of_range -" {
		t.Error("identity: the ordinal test removed, and the forged ordinal still refuses out_of_range")
	}
	compiled := newFixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
	compiled.patch(t, "        if raw > n:\n", "        if False:\n")
	if got := compiled.try(t, pyrun.Interpreter(t), "VNEW_enum_append", "Lineage", forged); got == "refused out_of_range -" {
		t.Error("compiled: the ordinal test removed, and the forged ordinal still refuses out_of_range")
	}
}

// TestPythonFixedCfloatRangeIsTheWriters (§4.6, bill §12.5): a compressed
// float rides as the float, held to the WRITER's range: both the hostile file
// (1.5, inside the new reader's [-2, 2] and past the old writer's [-1, 1]) and
// the past file (5.0) refuse out_of_range through the old writer's entry; the
// reference's own old_ file reads.
func TestPythonFixedCfloatRangeIsTheWriters(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	older, newer := readTestSchema(t, "VOLD_cfloat_range_widen"), readTestSchema(t, "VNEW_cfloat_range_widen")
	g := newFixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
	for _, p := range pins {
		for _, file := range []string{"hostile_cfloat_range_widen.bin", "past_cfloat_range_widen.bin"} {
			if got := g.load(t, p, "VNEW_cfloat_range_widen", "CfloatRangeWiden", corpusFile(t, corpus, file)).head; got != "refused out_of_range -" {
				t.Errorf("%s: %s: %s", p.name, file, got)
			}
		}
		if got := g.load(t, p, "VNEW_cfloat_range_widen", "CfloatRangeWiden", corpusFile(t, corpus, "old_cfloat_range_widen.bin")).head; got != "ok 1 w0" {
			t.Errorf("%s: the control, the reference's old file: %s", p.name, got)
		}
	}
}

// hostileBoolFile is the older Hbb saved by this leg with one byte forged:
// at is the body offset (flag@0, link present@1, link.n@2, lead@6,
// flags@10..17, trail@18).
func hostileBoolFile(t *testing.T, older *ir.Unit, at int, value byte) []byte {
	t.Helper()
	g := newFixedUnit(t, older, nil)
	main := pyrun.ImportPrelude + "from vold_hostile_bool import VOLD_hostile_bool as m\n" +
		"v = m.Hbb()\nv.lead = 0xAAAAAAAA\nv.trail = 0xBBBBBBBB\nv.link = m.Pay()\nprint(m.Hbb.save_fixed([v]).hex())\n"
	out := pins[0].run(t, g.dir, "save.py", map[string][]byte{"save.py": []byte(main)})
	b, err := hex.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatal(err)
	}
	body := b[len(b)-22:]
	if body[6] != 0xAA || body[18] != 0xBB {
		t.Fatalf("the body offsets moved: %x", body)
	}
	body[at] = value
	return b
}

// TestPythonFixedHostileBoolBytes (§4.5): a bool or present byte other than 0
// or 1 refuses wire_damaged, on the identity plan and on a compiled one.
func TestPythonFixedHostileBoolBytes(t *testing.T) {
	t.Parallel()
	older, newer := readTestSchema(t, "VOLD_hostile_bool"), readTestSchema(t, "VNEW_hostile_bool")
	units := []struct {
		name, mod string
		g         *fixedUnit
	}{
		{"identity", "VOLD_hostile_bool", newFixedUnit(t, older, nil)},
		{"compiled", "VNEW_hostile_bool", newFixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))},
	}
	for _, f := range []struct {
		name  string
		at    int
		value byte
	}{{"bool field", 0, 2}, {"present byte", 1, 3}, {"bool element", 12, 0xFF}} {
		file := hostileBoolFile(t, older, f.at, f.value)
		for _, u := range units {
			for _, p := range pins {
				if got := u.g.load(t, p, u.mod, "Hbb", file).head; got != "refused wire_damaged -" {
					t.Errorf("%s %s: %s: %s", p.name, u.name, f.name, got)
				}
			}
		}
	}
	clean := hostileBoolFile(t, older, 0, 1)
	for _, u := range units {
		if got := u.g.load(t, pins[0], u.mod, "Hbb", clean).head; !strings.HasPrefix(got, "ok 1 ") {
			t.Errorf("%s: the control, a true flag: %s", u.name, got)
		}
	}
}

// TestPythonFixedHostileBoolNegativeControl removes the bool byte's test
// from each lane in turn and requires the forged flag to read.
func TestPythonFixedHostileBoolNegativeControl(t *testing.T) {
	t.Parallel()
	older, newer := readTestSchema(t, "VOLD_hostile_bool"), readTestSchema(t, "VNEW_hostile_bool")
	file := hostileBoolFile(t, older, 0, 2)
	identity := newFixedUnit(t, older, nil)
	identity.patchModule(t, "VOLD_hostile_bool", `raise _s_rt.Refused("wire_damaged", "Hbb.flag")`, "pass")
	if got := identity.try(t, pyrun.Interpreter(t), "VOLD_hostile_bool", "Hbb", file); got == "refused wire_damaged -" {
		t.Error("identity: the bool test removed, and the forged flag still refuses")
	}
	compiled := newFixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
	compiled.patch(t, "            if b > 1:\n", "            if False:\n")
	if got := compiled.try(t, pyrun.Interpreter(t), "VNEW_hostile_bool", "Hbb", file); got == "refused wire_damaged -" {
		t.Error("compiled: the bool test removed, and the forged flag still refuses")
	}
}

const fixedWriteProbe = pyrun.ImportPrelude + `import math
from probe import Probe as m
from probe import InvalidValue

def try_(name, mutate):
    v = m.Heat()
    mutate(v)
    try:
        m.Heat.save_fixed([v])
        print(name, "saved")
    except InvalidValue as e:
        print(name, "refused", e.path)

try_("nan", lambda v: setattr(v, "a", math.nan))
try_("inf64", lambda v: setattr(v, "b", math.inf))
try_("overflow32", lambda v: setattr(v, "a", 1e300))
try_("range", lambda v: setattr(v, "n", 101))
try_("int type", lambda v: setattr(v, "n", True))
try_("float type", lambda v: setattr(v, "b", 1))
try_("count", lambda v: setattr(v, "marks", [1, 2, 3, 4, 5]))
try_("fixed length", lambda v: setattr(v, "grid", [0, 0]))
try_("text", lambda v: setattr(v, "label", "toolong"))
try_("nul", lambda v: setattr(v, "label", "a\x00"))
try_("surrogate", lambda v: setattr(v, "label", "\ud800"))
try_("enum", lambda v: setattr(v, "tier", 3))
try_("keyed", lambda v: v.slots.slots().pop())
try_("absent", lambda v: setattr(v, "inner", None))
try_("present", lambda v: setattr(v, "inner", m.Inner()))
try_("clean", lambda v: None)
`

// TestPythonFixedFormRefusesInvalidOnWrite: a save validates the whole
// batch against the table's own contract and refuses, naming the field, with
// no bytes; the clean value and an absent or present optional save.
func TestPythonFixedFormRefusesInvalidOnWrite(t *testing.T) {
	t.Parallel()
	u := pyrun.LoadUnitSource(t, "Probe.schema", `package probe
enum Tier { Low, High }
fixed table Inner { x int32 = 1 }
fixed table Heat
{
    a     float32
    b     float64
    n     int32 = 5 | min = 0, max = 100
    marks [..4]int32
    grid  [3]int32
    label string(4)
    tier  Tier
    slots [Tier]int32
    inner ?Inner
}
`)
	g := newFixedUnit(t, u, nil)
	want := strings.Join([]string{
		"nan refused Heat[0].a", "inf64 refused Heat[0].b", "overflow32 refused Heat[0].a",
		"range refused Heat[0].n", "int type refused Heat[0].n", "float type refused Heat[0].b",
		"count refused Heat[0].marks", "fixed length refused Heat[0].grid", "text refused Heat[0].label",
		"nul refused Heat[0].label", "surrogate refused Heat[0].label", "enum refused Heat[0].tier",
		"keyed refused Heat[0].slots", "absent saved", "present saved", "clean saved",
	}, "\n")
	for _, p := range pins {
		if got := strings.TrimSpace(string(p.run(t, g.dir, "write.py", map[string][]byte{"write.py": []byte(fixedWriteProbe)}))); got != want {
			t.Errorf("%s: got\n%s\nwant\n%s", p.name, got, want)
		}
	}
}

// TestPythonRefusesVariableTablesByName: a variable table, and a fixed table
// past the form's record ceiling, are refused by name; a fixed table
// generates.
func TestPythonRefusesVariableTablesByName(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ src, want string }{
		{"package p\ntable V { a int32 }\n", "declares table V, a variable table"},
		{"package p\nfixed table Big { a [70000]uint8 }\n", "declares fixed table Big, past the fixed form's record ceiling"},
	} {
		if _, err := GenerateLineage(pyrun.LoadUnitSource(t, "P.schema", c.src), nil); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: got %v, want a refusal containing %q", c.src, err, c.want)
		}
	}
	if _, err := GenerateLineage(pyrun.LoadUnitSource(t, "P.schema", "package p\nfixed table F { a int32 }\n"), nil); err != nil {
		t.Errorf("a fixed table: %v", err)
	}
}

// TestPythonFixedTreesAreStrictClean: mypy in strict mode finds nothing in
// the packages the fixed corpora generate (the table classes, the shapes, the
// roots and the runtime's fixed module).
func TestPythonFixedTreesAreStrictClean(t *testing.T) {
	t.Parallel()
	for _, v := range fixedOracleVectors {
		t.Run(moduleOf(v.schema), func(t *testing.T) {
			t.Parallel()
			u := pyrun.LoadUnit(t, repo(v.schema))
			files, err := GenerateLineage(u, nil)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			pyrun.WriteTree(t, dir, files)
			out, err := pyrun.Analyze(t, dir, u.Package)
			if err != nil || !strings.Contains(out, "Success: no issues found") {
				t.Fatalf("mypy --strict over the generated %s package: %v\n%s", u.Package, err, out)
			}
		})
	}
}

const keyedProbe = pyrun.ImportPrelude + `from probe import Probe as m

v = m.Holder()
slots = v.slots
print(len(slots), slots[1], slots[3])
slots[2] = 9
print(slots.slots())
for key in (0, 4, -1, True):
    try:
        slots[key]
        print(key, "read")
    except KeyError:
        print(key, "refused")
`

// TestPythonKeyedArrayRefusesBothEnds (docs/SPEC-TABLES.md §2.4; PORTING M5):
// a table's [E]T holds one slot per variant, the element for variant k at
// index k - 1, and indexing refuses None, a key past E.MAX, a negative key and
// a bool, so no plain list index can reach the last slot from below.
func TestPythonKeyedArrayRefusesBothEnds(t *testing.T) {
	t.Parallel()
	u := pyrun.LoadUnitSource(t, "Probe.schema", "package probe\nenum Tier { Low, Mid, High }\nfixed table Holder\n{\n    slots [Tier]int32\n}\n")
	g := newFixedUnit(t, u, nil)
	want := "3 0 0\n[0, 9, 0]\n0 refused\n4 refused\n-1 refused\nTrue refused"
	for _, p := range pins {
		if got := strings.TrimSpace(string(p.run(t, g.dir, "keyed.py", map[string][]byte{"keyed.py": []byte(keyedProbe)}))); got != want {
			t.Errorf("%s: got\n%s\nwant\n%s", p.name, got, want)
		}
	}
}

// TestPythonKeyedArrayNegativeControl lets the keyed index take any int and
// requires key 0 to reach a slot (the last): the refusal is what M5 holds.
func TestPythonKeyedArrayNegativeControl(t *testing.T) {
	t.Parallel()
	u := pyrun.LoadUnitSource(t, "Probe.schema", "package probe\nenum Tier { Low, Mid, High }\nfixed table Holder\n{\n    slots [Tier]int32\n}\n")
	g := newFixedUnit(t, u, nil)
	g.patch(t, "        if type(key) is not int or not 1 <= key <= len(self._slots):\n", "        if False:\n")
	got, _, _ := pyrun.Try(t, pyrun.Interpreter(t), g.dir, "keyed.py", map[string][]byte{"keyed.py": []byte(keyedProbe)})
	if !strings.Contains(got, "0 read") {
		t.Fatalf("the keyed refusal removed, and key 0 still refuses:\n%s", got)
	}
}
