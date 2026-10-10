package javatable

// fixedroadmap_t04 — the java leg's field-evolution, reports and text rows of
// docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"), the
// assertions no other test of this leg makes. Law: docs/FIXED-FORM-ALGORITHM.md
// §4.5 (the scatter and its content rules, fix 11), §4.6 (the bounds pass),
// §5.1 (the monotone law), §5.4 (the counters), §5.7 (the tests),
// docs/FIXED-FORM-VERSIONING-TESTS.md (the rows), docs/SPEC-TABLES.md §3.4.
//
// One subtest per task id. A clause an existing test of this leg already makes
// is named in the card's verdicts; this file carries the direct assertion of
// every clause that had none, and it needs no C++ corpus: each row is built and
// read with the leg's own generated writer and reader, and the byte fixtures
// are the leg's own saves, forged where a refusal is the subject.
//
// The [verify] tasks prove the holder RUNS and BITES (the bite is a planted
// break, named in the card's report); the [owed] tasks assert red-first and are
// implemented in this leg's emitter or runtime.

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapJavaT04Evolution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"java/E6", t04E6},
		{"java/E8", t04E8},
		{"java/R19", t04R19},
		{"java/E9", t04E9},
		{"java/R16", t04R16},
		{"java/R18", t04R18},
		{"java/C3", t04C3},
		{"java/C4", t04C4},
		{"java/C5", t04C5},
		{"java/R24", t04R24},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// ---- the leg's own build, without the C++ corpus --------------------------

// t04Build generates an inline unit with the leg's own emitter, writes it and
// the given probes (name -> source) into one src tree and compiles the lot with
// the leg's own javac. It answers the package, the root table and the classes
// directory.
func t04Build(t *testing.T, dir, src string, probes map[string]string) (pkg, root, classes string) {
	t.Helper()
	u := unitFrom(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("t04: Generate: %v", err)
	}
	pkg = javaPackageOf(u)
	if pkg == "" {
		t.Fatal("t04: the unit declares no package")
	}
	root = rootOf(t, u).Name
	srcDir := filepath.Join(dir, "src")
	writeFixedJava(t, srcDir, pkg, files)
	for name, body := range probes {
		if err := os.WriteFile(filepath.Join(srcDir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	classes = filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	javac, _ := javaTools(t)
	gen, _ := filepath.Glob(filepath.Join(srcDir, "*", "*.java"))
	pr, _ := filepath.Glob(filepath.Join(srcDir, "*.java"))
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, gen...)
	args = append(args, pr...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("t04: javac: %v\n%s", err, out)
	}
	return pkg, root, classes
}

// t04RecordsAt is where a file's records begin: the sixteen-byte header, the
// u32 layout length and the layout, exactly as §3.4 lays them.
func t04RecordsAt(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 20 {
		t.Fatalf("%s is %d bytes: not a file of this form", path, len(raw))
	}
	layoutLen := int(binary.LittleEndian.Uint32(raw[16:]))
	if 20+layoutLen > len(raw) {
		t.Fatalf("%s: the layout length %d runs past the file", path, layoutLen)
	}
	return 20 + layoutLen
}

// t04Saved runs a save probe and answers the file its BYTES line spells, with
// the record body's offset beside it (the first record's body is recordsAt+8).
func t04Saved(t *testing.T, classes, class string) (path string, body int) {
	t.Helper()
	out := t03RunSaveLoad(t, classes, class)
	path = t03BytesOf(t, out)
	return path, t04RecordsAt(t, path) + 8
}

// t04TextProbe is a single-field text table and the four ingredients every text
// row needs: the leg's save probe (with the caller's setters), the leg's read
// probe, one saved file and the body offset.
type t04Text struct {
	pkg, root, classes string
	saved              string
	body               int
	raw                []byte
}

func t04TextBuild(t *testing.T, src, setters string) t04Text {
	t.Helper()
	dir := t.TempDir()
	// the unit is read once to learn the package and root the probes must spell.
	u := unitFrom(t, src)
	pkg := javaPackageOf(u)
	root := rootOf(t, u).Name
	_, _, classes := t04Build(t, dir, src, map[string]string{
		"Probe_save.java":  t03SaveLoadProbe("Probe_save", pkg, root, setters),
		"Probe_reads.java": probeSource("Probe_reads", pkg, root, true),
	})
	saved, body := t04Saved(t, classes, "Probe_save")
	raw, err := os.ReadFile(saved)
	if err != nil {
		t.Fatal(err)
	}
	return t04Text{pkg: pkg, root: root, classes: classes, saved: saved, body: body, raw: raw}
}

// forge writes a copy of the saved bytes through mutate and answers its path.
func (x t04Text) forge(t *testing.T, name string, mutate func(b []byte) []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, mutate(append([]byte(nil), x.raw...)), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// ---- E6: renaming uses the declared identity ------------------------------

// t04E6: E6 "Renaming uses the declared identity". A field renamed THROUGH
// `was` keeps the OLD name's wire id (docs/SPEC-TABLES.md §3.4, the id is
// fnv1a64 of the WIRE name), so the two layouts are byte-identical and the
// read is identity. The pair reads in both directions with no lineage at all
// (docs/FIXED-FORM-ALGORITHM.md §5.7, the `rename_without_was` row).
func t04E6(t *testing.T) {
	oldU := versioningSchemaUnit(t, "VOLD_rename_without_was.schema")
	newU := versioningSchemaUnit(t, "VNEW_rename_without_was.schema")
	oSt := rootOf(t, oldU)
	nSt := rootOf(t, newU)

	oLayout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(oSt))
	nLayout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(nSt))
	if len(oLayout) != len(nLayout) {
		t.Fatalf("E6: the rename moved the layout: %d bytes against %d", len(oLayout), len(nLayout))
	}
	if oHash, nHash := ir.TableFixedLayoutHash(oLayout, oSt), ir.TableFixedLayoutHash(nLayout, nSt); oHash != nHash {
		t.Errorf("E6: the rename moved the layout hash (%#x against %#x): `was` keeps the OLD name's id, so the identity is declared and not derived", oHash, nHash)
	}
	oID := ir.TableFieldWireId(findField(t, oSt, "a"))
	nID := ir.TableFieldWireId(findField(t, nSt, "b"))
	if oID != nID {
		t.Errorf("E6: the renamed field's wire id moved (%#x against %#x): the declared identity is the OLD name's hash", nID, oID)
	}
	// And the read is identity in BOTH directions: one build takes the other's
	// file under its own hash, with no lineage entry between them.
	classes := t03Build(t, t.TempDir(), "rename_without_was", []sideSpec{
		{key: "reads", schema: "VNEW_rename_without_was.schema", older: []string{"VOLD_rename_without_was.schema"}},
		{key: "refuses", schema: "VOLD_rename_without_was.schema"},
	}, map[string]string{
		"Probe_saveold.java": t03SaveLoadProbe("Probe_saveold", "vold_rename_without_was", "Lineage", `vals[0].a = 5; vals[0].seq = 9;`),
	})
	saved, _ := t04Saved(t, classes, "Probe_saveold")
	r := runProbe(t, classes, "Probe_reads", saved)
	if r.n != 1 || r.refused || r.malformed {
		t.Fatalf("E6: the renamed layout must read in both directions: %+v", r)
	}
	if r.value["b"] != "5" {
		t.Errorf("E6: b=%s, want the writer's a=5 read across the rename", r.value["b"])
	}
	if r.value["seq"] != "9" {
		t.Errorf("E6: seq=%s, want the writer's 9 across the rename", r.value["seq"])
	}
	if r.unknown != 0 || r.kindMismatch != 0 || r.widened != 0 || r.clamped != 0 {
		t.Errorf("E6: the rename is not a version: a counter moved (%+v)", r)
	}
}

func findField(t *testing.T, st *ir.Struct, name string) *ir.Field {
	t.Helper()
	for _, f := range st.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("field %s not found in %s", name, st.Name)
	return nil
}

// ---- E8: append and deprecate under the backward-read contract ------------

// t04E8: E8 "Append and deprecate under the backward-read contract". An
// appended field lands its declared default in the older writer's record, a
// deprecated field keeps its slot and reads the writer's value, and neither is
// an event: every counter stays zero (docs/FIXED-FORM-ALGORITHM.md §5.7,
// docs/SPEC-TABLES.md §3.4's deprecated-slot rule).
func t04E8(t *testing.T) {
	t.Run("append", func(t *testing.T) {
		t.Parallel()
		classes := t03Build(t, t.TempDir(), "field_append", []sideSpec{
			{key: "reads", schema: "VNEW_field_append.schema", older: []string{"VOLD_field_append.schema"}},
			{key: "refuses", schema: "VOLD_field_append.schema"},
		}, map[string]string{
			"Probe_saveold.java": t03SaveLoadProbe("Probe_saveold", "vold_field_append", "Lineage", `
        vals[0].x = 11; vals[0].y = 22; vals[0].z = 33;`),
		})
		saved, _ := t04Saved(t, classes, "Probe_saveold")
		r := runProbe(t, classes, "Probe_reads", saved)
		if r.n != 1 || r.refused || r.malformed {
			t.Fatalf("E8/append: the old file must read through the appended build: %+v", r)
		}
		for k, want := range map[string]string{"x": "11", "y": "22", "z": "33", "w": "77"} {
			if got := r.value[k]; got != want {
				t.Errorf("E8/append: %s=%s, want %s (the appended w lands its declared default)", k, got, want)
			}
		}
		if r.unknown != 0 || r.kindMismatch != 0 || r.widened != 0 || r.clamped != 0 {
			t.Errorf("E8/append: an append the reader knows is not an event: %+v", r)
		}
	})
	t.Run("deprecate", func(t *testing.T) {
		t.Parallel()
		classes := t03Build(t, t.TempDir(), "field_deprecate", []sideSpec{
			{key: "reads", schema: "VNEW_field_deprecate.schema", older: []string{"VOLD_field_deprecate.schema"}},
			{key: "refuses", schema: "VOLD_field_deprecate.schema"},
		}, map[string]string{
			"Probe_saveold.java": t03SaveLoadProbe("Probe_saveold", "vold_field_deprecate", "Lineage", `
        vals[0].a = 11; vals[0].b = 22; vals[0].c = 33;`),
		})
		saved, _ := t04Saved(t, classes, "Probe_saveold")
		r := runProbe(t, classes, "Probe_reads", saved)
		if r.n != 1 || r.refused || r.malformed {
			t.Fatalf("E8/deprecate: the old file must read through the deprecating build: %+v", r)
		}
		// THE DEPRECATED FIELD KEEPS ITS SLOT: `c` does not slide, and the
		// reader yields the value the writer put there without a counter.
		for k, want := range map[string]string{"a": "11", "b": "22", "c": "33"} {
			if got := r.value[k]; got != want {
				t.Errorf("E8/deprecate: %s=%s, want %s — the slot stays where it is", k, got, want)
			}
		}
		if r.unknown != 0 || r.kindMismatch != 0 || r.widened != 0 || r.clamped != 0 {
			t.Errorf("E8/deprecate: a deprecation is not an event: %+v", r)
		}
	})
}

// ---- R19: a clean NEW-READS-OLD moves no counter at all -------------------

// t04R19: R19 "a clean NEW-READS-OLD of an appended field, variant, arm, flag
// or keyed slot moves no counter at all" (§5.4). Each append row the corpus
// carries is written by its OLDER build and read by its NEWER one; every
// counter stays at zero because an append the reader knows is not an event.
func t04R19(t *testing.T) {
	for _, row := range []string{"field_append", "enum_append", "union_append", "flags_append", "keyed_array_enum_append"} {
		t.Run(row, func(t *testing.T) {
			t.Parallel()
			classes := t03Build(t, t.TempDir(), row, []sideSpec{
				{key: "reads", schema: "VNEW_" + row + ".schema", older: []string{"VOLD_" + row + ".schema"}},
				{key: "refuses", schema: "VOLD_" + row + ".schema"},
			}, map[string]string{
				"Probe_saveold.java": t03SaveLoadProbe("Probe_saveold", "vold_"+row, "Lineage", "// the declared defaults"),
			})
			saved, _ := t04Saved(t, classes, "Probe_saveold")
			r := runProbe(t, classes, "Probe_reads", saved)
			if r.n < 1 || r.refused || r.malformed {
				t.Fatalf("R19/%s: the old file must read through the appended build: %+v", row, r)
			}
			if r.unknown != 0 || r.kindMismatch != 0 || r.widened != 0 || r.clamped != 0 {
				t.Errorf("R19/%s: a clean append moved a counter: unknown=%d kindMismatch=%d widened=%d clamped=%d, want all 0",
					row, r.unknown, r.kindMismatch, r.widened, r.clamped)
			}
		})
	}
}

// ---- E9: duplicate never raised -------------------------------------------

// t04E9: E9 "duplicate never raised". The fixed form raises all of §4's
// counters but `duplicate` (docs/FIXED-FORM-ALGORITHM.md §0: "this form raises
// all but `duplicate`"). The emitted report carries no such counter anywhere,
// so no read can raise one.
func t04E9(t *testing.T) {
	files := generate(t, `package t04e9

fixed table T
{
    x int32
    y int32
}
`)
	runtime := string(files["TableFixed.java"])
	if runtime == "" {
		t.Fatal("E9: no TableFixed.java emitted")
	}
	if strings.Contains(runtime, "duplicate") {
		t.Error("E9: the fixed runtime names `duplicate`; the fixed form raises all of §4's counters but that one")
	}
	report := t02Block(t, runtime, "public static final class Report {", "\n    }")
	for _, counter := range []string{"unknown", "kindMismatch", "widened", "clamped", "malformed"} {
		if !strings.Contains(report, counter) {
			t.Errorf("E9: the report is missing the counter %q it does carry", counter)
		}
	}
}

// ---- R16: §5.4's counters exactly -----------------------------------------

// t04R16: R16 "§5.4's counters exactly". The clauses asserted here directly:
// the `unknown` census lands ONCE PER PEER and NEVER PER RECORD (a three-record
// file of one peer reports 1), it lands AFTER the record loop and only on a
// read that RETURNS, and `widened` is counted once per entry per record. The
// clean-read clauses (copy/const/present/ordinal move nothing, the bounds pass
// counts a forged ordinal on BOTH plans) are held by
// TestFixedVersioningForgedOrdinalBothPlans / TestFixedVersioningUnionTagBothPlans
// and by t04R19's zero counters, and are named in the card's verdict.
func t04R16(t *testing.T) {
	// unknown ONCE PER PEER, NEVER PER RECORD: the deliberately unlawful pair,
	// its entry handed in through GenerateLineage (the lock played in one line).
	classes := t03Build(t, t.TempDir(), "unknown_census", []sideSpec{
		{key: "reads", schema: "VNEW_unknown_census.schema", older: []string{"VOLD_unknown_census.schema"}},
		{key: "refuses", schema: "VOLD_unknown_census.schema"},
	}, map[string]string{
		"Probe_saveold.java": t03SaveLoadProbe("Probe_saveold", "vold_unknown_census", "Census", `
        vals[0].lead = 1; vals[0].trail = 2;
        vals[0].items[0].a = 10; vals[0].items[1].a = 11;
        vals[0].items[2].a = 12; vals[0].items[3].a = 13;`),
	})
	saved, _ := t04Saved(t, classes, "Probe_saveold")
	raw, err := os.ReadFile(saved)
	if err != nil {
		t.Fatal(err)
	}
	recAt := t04RecordsAt(t, saved)
	rec := raw[recAt:]
	triple := append([]byte(nil), raw[:recAt]...)
	for range 3 {
		triple = append(triple, rec...)
	}
	path := filepath.Join(t.TempDir(), "many_unknown_census.bin")
	if err := os.WriteFile(path, triple, 0o644); err != nil {
		t.Fatal(err)
	}
	r := runProbe(t, classes, "Probe_reads", path)
	if r.n != 3 {
		t.Fatalf("R16: three records of one peer: n=%d, want 3", r.n)
	}
	if r.refused || r.malformed {
		t.Fatalf("R16: the census read is clean: %+v", r)
	}
	if r.unknown != 1 {
		t.Errorf("R16: unknown=%d, want exactly 1 — Item.drop is ONE field of ONE peer, once per peer and never per record", r.unknown)
	}
	if r.kindMismatch != 0 || r.widened != 0 || r.clamped != 0 {
		t.Errorf("R16: the census read moved another counter: %+v", r)
	}

	// The census LANDS AFTER THE RECORD LOOP on a read that RETURNS (§5.9 #6),
	// and `widened` is one count per entry per record.
	files := generate(t, t02Flat)
	load := methodBody(string(files["TFixed.java"]), "public static int load(")
	if load == "" {
		t.Fatal("R16: no emitted load")
	}
	loopAt := strings.Index(load, "for (int k = 0; k < n; k++)")
	unknownAt := strings.Index(load, "report.unknown += censusUnknown;")
	if loopAt < 0 || unknownAt < 0 || unknownAt < loopAt {
		t.Errorf("R16: the unknown census is not landed once after the record loop (loop at %d, census at %d)", loopAt, unknownAt)
	}
	if strings.Count(load, "report.unknown += censusUnknown;") != 1 {
		t.Error("R16: the unknown census must land exactly once per read")
	}
	runtime := string(files["TableFixed.java"])
	if n := strings.Count(runtime, "report.widened++;"); n != 2 {
		t.Errorf("R16: the runtime counts widened %d times, want exactly 2 — once in opWiden and once in opWidenF", n)
	}
}

// ---- R18: a clamp that cannot fire is not emitted -------------------------

// t04R18: R18 "a clamp that cannot fire is not emitted and nothing moves"
// (§5.4): an ordinal whose extent FILLS its storage width — 255 variants in a
// byte — has no read-side clamp, because `q > extent` is a value of that very
// width and no value satisfies it. The emitted scatter carries no such
// comparison, and a read of the top ordinal moves no counter.
func t04R18(t *testing.T) {
	var b strings.Builder
	b.WriteString("package t04r18\n\nenum Big\n{\n")
	for i := range 255 {
		fmt.Fprintf(&b, "    V%d\n", i)
	}
	b.WriteString("}\n\nfixed table T\n{\n    e Big\n}\n")
	src := b.String()

	files := generate(t, src)
	emitted := string(files["TFixed.java"])
	if strings.Contains(emitted, "q > 255L") {
		t.Error("R18: the scatter emits a clamp that cannot fire: 255 variants FILL a byte, so `q > 255` is the tautology §5.4 elides")
	}

	// And nothing moves: the top ordinal — the whole byte, 0xFF — lands whole.
	// A one-byte enum's storage is a Java `byte`, so the ordinal 255 prints as
	// the same bit pattern's signed value, -1; the wire byte is asserted first.
	x := t04TextBuild(t, src, "vals[0].e = (byte) 255;")
	if got := x.raw[x.body]; got != 0xFF {
		t.Fatalf("R18: the saved top ordinal byte = %#x, want 0xFF", got)
	}
	r := runProbe(t, x.classes, "Probe_reads", x.saved)
	if r.n != 1 || r.refused || r.malformed {
		t.Fatalf("R18: the top ordinal must read: %+v", r)
	}
	if r.value["e"] != "-1" { // (byte) 0xFF
		t.Errorf("R18: e=%s, want the 0xFF byte read whole", r.value["e"])
	}
	if r.clamped != 0 || r.unknown != 0 || r.kindMismatch != 0 || r.widened != 0 {
		t.Errorf("R18: an elided clamp moved a counter: %+v", r)
	}
}

// ---- C3: text length clamp ------------------------------------------------

// t04C3: C3 "text length clamp". ALGORITHM §4.5's `text` op clamps a stated
// length into `[0, cap]` and counts `clamped` once when it fired; the count
// rides in BYTES for `string(N)` and the slack behind the used bytes is never
// read. A forged length past the capacity lands the capacity and counts
// exactly one.
func t04C3(t *testing.T) {
	x := t04TextBuild(t, `package t04c3

fixed table T
{
    s string(8)
}
`, `
        byte[] s = "hello".getBytes(java.nio.charset.StandardCharsets.UTF_8);
        System.arraycopy(s, 0, vals[0].s, 0, s.length);
        vals[0].sLength = s.length;`)
	if got := binary.LittleEndian.Uint32(x.raw[x.body:]); got != 5 {
		t.Fatalf("C3: the saved length word = %d, want 5", got)
	}
	forged := x.forge(t, "clamp.bin", func(b []byte) []byte {
		binary.LittleEndian.PutUint32(b[x.body:], 9) // past the capacity 8
		return b
	})
	r := runProbe(t, x.classes, "Probe_reads", forged)
	if r.n != 1 || r.refused || r.malformed {
		t.Fatalf("C3: the forged length must clamp, not refuse: %+v", r)
	}
	if r.value["sLength"] != "8" {
		t.Errorf("C3: sLength=%s, want the capacity 8 — a stated length past the bound clamps to the bound", r.value["sLength"])
	}
	if r.clamped != 1 {
		t.Errorf("C3: clamped=%d, want exactly 1 — the text op counts once per entry per record (§5.4)", r.clamped)
	}
	if r.unknown != 0 || r.kindMismatch != 0 || r.widened != 0 {
		t.Errorf("C3: a text clamp moved another counter: %+v", r)
	}
}

// ---- C4: text content refuses by name -------------------------------------

// t04C4: C4 "text content refuses BY NAME". ALGORITHM §4.5 fix 11: the CONTENT
// RULES apply to the USED units, and a content violation refuses by name — the
// packet reader's verdict — rather than defaulting. A kind 12 payload that is
// not well-formed UTF-8 REFUSES `textIllFormed`: nothing decoded, no counter
// moved, `malformed` false.
func t04C4(t *testing.T) {
	x := t04TextBuild(t, `package t04c4

fixed table T
{
    s string(8)
}
`, `
        byte[] s = "hello".getBytes(java.nio.charset.StandardCharsets.UTF_8);
        System.arraycopy(s, 0, vals[0].s, 0, s.length);
        vals[0].sLength = s.length;`)
	// The control: the lawful payload reads, so the refusal below is the forge's.
	clean := runProbe(t, x.classes, "Probe_reads", x.forge(t, "clean.bin", func(b []byte) []byte { return b }))
	if clean.n != 1 || clean.refused || clean.malformed {
		t.Fatalf("C4: the lawful file must read: %+v", clean)
	}
	if clean.clamped != 0 || clean.unknown != 0 || clean.kindMismatch != 0 || clean.widened != 0 {
		t.Errorf("C4: a clean text read moved a counter: %+v", clean)
	}
	forged := x.forge(t, "illformed.bin", func(b []byte) []byte {
		b[x.body+4] = 0xFF // a lead byte no UTF-8 sequence starts with
		return b
	})
	r := runProbe(t, x.classes, "Probe_reads", forged)
	if !r.refused || r.reason != "textIllFormed" {
		t.Errorf("C4: refused=%v reason=%s, want a refusal named textIllFormed", r.refused, r.reason)
	}
	if r.n != -1 {
		t.Errorf("C4: n=%d, want -1", r.n)
	}
	if r.malformed {
		t.Errorf("C4: malformed=%v, want false — a refusal is never damage", r.malformed)
	}
	if r.unknown != 0 || r.kindMismatch != 0 || r.widened != 0 || r.clamped != 0 {
		t.Errorf("C4: a content refusal moved a counter: %+v", r)
	}
}

// ---- C5: wide text code units ---------------------------------------------

// t04C5: C5 "wide text code units". ALGORITHM §4.5: a `wstring(N)`'s length is
// in CODE UNITS, the clamp is in units (`cap := size / 2`), the content check
// runs over the used units, and an astral pair counts TWO code units. A lone
// (unpaired) surrogate is ill-formed and refuses by name; a forged length past
// the unit capacity clamps to the capacity and counts one.
func t04C5(t *testing.T) {
	x := t04TextBuild(t, `package t04c5

fixed table T
{
    w wstring(4)
}
`, `
        vals[0].w[0] = 0xD83D; vals[0].w[1] = 0xDE00; // U+1F600, TWO code units
        vals[0].wLength = 2;`)
	if got := binary.LittleEndian.Uint32(x.raw[x.body:]); got != 2 {
		t.Errorf("C5: the saved length word = %d, want 2 CODE UNITS for an astral pair (a code point would be 1)", got)
	}
	// The astral pair lands whole.
	clean := runProbe(t, x.classes, "Probe_reads", x.forge(t, "clean.bin", func(b []byte) []byte { return b }))
	if clean.refused || clean.malformed || clean.n != 1 {
		t.Fatalf("C5: the astral pair must read: %+v", clean)
	}
	if clean.value["wLength"] != "2" {
		t.Errorf("C5: wLength=%s, want 2 — the astral pair is TWO code units and a code point would be one", clean.value["wLength"])
	}
	if clean.clamped != 0 || clean.unknown != 0 || clean.kindMismatch != 0 || clean.widened != 0 {
		t.Errorf("C5: a clean wide read moved a counter: %+v", clean)
	}
	// A lone high surrogate is ill-formed and refuses by name.
	ill := x.forge(t, "lone.bin", func(b []byte) []byte {
		binary.LittleEndian.PutUint16(b[x.body+4:], 0xD83D)
		binary.LittleEndian.PutUint16(b[x.body+6:], 0x0041)
		return b
	})
	r := runProbe(t, x.classes, "Probe_reads", ill)
	if !r.refused || r.reason != "textIllFormed" {
		t.Errorf("C5: a lone surrogate: refused=%v reason=%s, want textIllFormed", r.refused, r.reason)
	}
	if r.n != -1 || r.malformed {
		t.Errorf("C5: the wide refusal is total and not damage: n=%d malformed=%v", r.n, r.malformed)
	}
	// The clamp is in UNITS: a forged length past 4 units lands 4 and counts 1.
	clamp := x.forge(t, "clamp.bin", func(b []byte) []byte {
		binary.LittleEndian.PutUint32(b[x.body:], 6) // past the four-unit capacity
		return b
	})
	c := runProbe(t, x.classes, "Probe_reads", clamp)
	if c.refused || c.malformed {
		t.Fatalf("C5: the forged wide length must clamp, not refuse: %+v", c)
	}
	if c.value["wLength"] != "4" {
		t.Errorf("C5: wLength=%s, want the unit capacity 4 — the wide clamp is in UNITS", c.value["wLength"])
	}
	if c.clamped != 1 {
		t.Errorf("C5: clamped=%d, want exactly 1", c.clamped)
	}
}

// ---- R24: ill-formed text in the used units -------------------------------

// t04R24: R24 "ill-formed text in the USED units refuses by name
// (text_ill_formed); text is counted in bytes with the clamp in units; slack is
// unspecified on read and not a refusal". Three clauses, each asserted:
//   - a truncated multi-byte sequence IN the used units refuses textIllFormed;
//   - a `string(N)`'s length is a BYTE count (U+00E9 is two bytes);
//   - NON-ZERO SLACK past the used units is read normally and moves no counter.
func t04R24(t *testing.T) {
	x := t04TextBuild(t, `package t04r24

fixed table T
{
    s string(8)
}
`, `
        byte[] s = "hi".getBytes(java.nio.charset.StandardCharsets.UTF_8);
        System.arraycopy(s, 0, vals[0].s, 0, s.length);
        vals[0].sLength = s.length;`)

	// (c) NON-ZERO SLACK is unspecified on read and not a refusal: garbage past
	// the used bytes leaves the read clean and the length untouched.
	slack := x.forge(t, "slack.bin", func(b []byte) []byte {
		for i := 2; i < 8; i++ {
			b[x.body+4+i] = 0xFF
		}
		return b
	})
	s := runProbe(t, x.classes, "Probe_reads", slack)
	if s.refused || s.malformed || s.n != 1 {
		t.Fatalf("R24: non-zero slack must read, not refuse: %+v", s)
	}
	if s.value["sLength"] != "2" {
		t.Errorf("R24: sLength=%s, want the stated 2: slack is never read", s.value["sLength"])
	}
	if s.clamped != 0 || s.unknown != 0 || s.kindMismatch != 0 || s.widened != 0 {
		t.Errorf("R24: non-zero slack moved a counter: %+v", s)
	}

	// (a) ill-formed IN the used units refuses by name: a lead byte whose
	// continuation is missing.
	ill := x.forge(t, "trunc.bin", func(b []byte) []byte {
		binary.LittleEndian.PutUint32(b[x.body:], 1)
		b[x.body+4] = 0xC3 // a two-byte lead with no continuation inside the used byte
		return b
	})
	r := runProbe(t, x.classes, "Probe_reads", ill)
	if !r.refused || r.reason != "textIllFormed" {
		t.Errorf("R24: refused=%v reason=%s, want a refusal named textIllFormed", r.refused, r.reason)
	}
	if r.n != -1 || r.malformed {
		t.Errorf("R24: the refusal is total and not damage: n=%d malformed=%v", r.n, r.malformed)
	}
	if r.unknown != 0 || r.kindMismatch != 0 || r.widened != 0 || r.clamped != 0 {
		t.Errorf("R24: an ill-formed refusal moved a counter: %+v", r)
	}

	// (b) a `string(N)`'s length is a BYTE count: U+00E9 is two bytes, so the
	// stated length is 2 and not 1.
	y := t04TextBuild(t, `package t04r24b

fixed table T
{
    s string(8)
}
`, `
        byte[] s = "\u00e9".getBytes(java.nio.charset.StandardCharsets.UTF_8);
        System.arraycopy(s, 0, vals[0].s, 0, s.length);
        vals[0].sLength = s.length;`)
	if got := binary.LittleEndian.Uint32(y.raw[y.body:]); got != 2 {
		t.Errorf("R24: a two-byte code point's length word = %d, want 2 — the string length is counted in BYTES", got)
	}
	yr := runProbe(t, y.classes, "Probe_reads", y.forge(t, "clean.bin", func(b []byte) []byte { return b }))
	if yr.refused || yr.malformed || yr.value["sLength"] != "2" {
		t.Errorf("R24: the two-byte code point must land at length 2: %+v", yr)
	}
}
