package javatable

// fixedroadmap_t03 — the java leg's retirement, retired-runtime,
// numeric-evolution and optional-values rows of docs/roadmap.sexp's node
// `fixed-tables` (ROADMAP.md "NEW Fixed Tables"), the assertions no other
// test of this leg makes. Law: docs/FIXED-FORM-ALGORITHM.md §4.6 (the
// bounds pass), §5.3 (LOAD), §5.6 (what is retired), §5.9 #23 (the retirement
// print), docs/FIXED-FORM-VERSIONING-TESTS.md ("The floor and the hash", the
// divergence rows), docs/SPEC-TABLES.md §3.4, §21.2.
//
// A clause already held by an existing test of this leg is named by that test
// in the card's verdicts and is not repeated here; this file carries the
// direct assertion of every clause that had none. [verify] tasks prove the
// holder RUNS and BITES (the bite is a planted break in the card's report);
// [owed] tasks are asserted here red-first and implemented in the emitter or
// runtime; [capability] tasks write and read the construct with non-default
// and boundary values through the leg's own generated save and load.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// t03Flat is the smallest declared fixed table with a lineage: two adjacent
// scalars, handed one older entry, so an emitted build has both lanes.
const t03Flat = `package probe

fixed table T
{
    x int32
    y int32
}
`

// t03CorpusFile is one file of the C++ reference's corpus, refused loudly
// when missing so this harness never silently skips a row it names.
func t03CorpusFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(corpusDir(t), name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the corpus has no %s: run `make tables-fixedform-corpus`", name)
	}
	return path
}

// t03Method is one emitted method's body, or "" when the class has none.
func t03Method(src, sig string) string {
	i := strings.Index(src, sig)
	if i < 0 {
		return ""
	}
	rest := src[i:]
	if j := strings.Index(rest, "\n    }\n"); j >= 0 {
		return rest[:j]
	}
	return rest
}

func TestFixedRoadmapT03Versions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"java/R3", t03R3},
		{"java/R32", t03R32},
		{"java/R10", t03R10},
		{"java/R15", t03R15},
		{"java/E3/fixed-array-element", t03E3FixedArrayElement},
		{"java/E3/other-required-widens", t03E3OtherWidens},
		{"java/C8", t03C8},
		{"java/R20", t03R20},
		{"java/R21", t03R21},
		{"java/R29", t03R29},
		{"java/E5", t03E5},
		{"java/W2", t03W2},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t03R3: R3 "the floor is 1 + the highest retired index (0 when none); below
// the floor is layout_unsupported, reporting the file's hash". The
// behavioural column is TestFixedVersioningFloor's (floor_at, floor_below
// with the file's hash, floor_raise_live); this subtest pins the FLOOR VALUE
// itself off the emitted constant, so a floor that disagreed with the entries
// it was derived from is red by name.
func t03R3(t *testing.T) {
	older := []string{"VOLD_floor.schema", "VMID_floor.schema"}
	for _, tc := range []struct {
		retire int
		want   int
	}{
		{0, 0}, // none retired: the floor is 0
		{1, 1}, // entry 0 retired: the floor is 1
		{2, 2}, // entries 0 and 1 retired: 1 + the HIGHEST retired index
	} {
		u := versioningSchemaUnit(t, "VNEW_floor.schema")
		files, err := GenerateLineage(u, lineageOf(t, older, tc.retire))
		if err != nil {
			t.Fatalf("R3: retire=%d: GenerateLineage: %v", tc.retire, err)
		}
		src := string(files["FlooredFixed.java"])
		want := fmt.Sprintf("public static final int floor = %d;", tc.want)
		if !strings.Contains(src, want) {
			t.Errorf("R3: retire=%d: the emitted floor is not %q — the floor is 1 + the highest retired index, 0 when none is", tc.retire, want)
		}
	}
}

// t03R32: R32 "retire for real: a retired version is refused by name, once,
// idempotently". The by-name half is TestFixedVersioningFloor's floor_below;
// this subtest is the ONCE and the IDEMPOTENTLY: the same retired file read
// twice in one process refuses layoutUnsupported both times, with the file's
// hash both times, every counter zero both times, and the destination still
// every field of a fresh value after each.
func t03R32(t *testing.T) {
	oldFile := t03CorpusFile(t, "old_floor.bin")
	newFile := t03CorpusFile(t, "new_floor.bin")
	dir := t.TempDir()
	classes := t03Build(t, dir, "floor", []sideSpec{
		{key: "reads", schema: "VNEW_floor.schema", older: []string{"VOLD_floor.schema", "VMID_floor.schema"}, retire: 1},
	}, map[string]string{
		"Probe_retired.java": t03TwiceProbe("Probe_retired", "vnew_floor", "Floored"),
	})

	out := t03RunTwice(t, classes, "Probe_retired", oldFile)
	wantHash := fileHash(t, oldFile)
	for _, half := range []string{"FIRST", "SECOND"} {
		r := t03ParseTwice(t, out, half)
		if r.n != -1 || !r.refused || r.reason != "layoutUnsupported" {
			t.Errorf("R32: %s read of a retired file: n=%d refused=%v reason=%s, want -1 refused layoutUnsupported by name", half, r.n, r.refused, r.reason)
		}
		if r.layoutHash != wantHash {
			t.Errorf("R32: %s read: layoutHash=%s, want the file's own hash %s (§5.9 #7)", half, r.layoutHash, wantHash)
		}
		if r.malformed || r.unknown != 0 || r.kindMismatch != 0 || r.widened != 0 || r.clamped != 0 {
			t.Errorf("R32: %s read moved a counter or set malformed: %+v; a refusal is one answer, once", half, r)
		}
		for k, want := range r.fresh {
			if have, ok := r.value[k]; ok && have != want {
				t.Errorf("R32: %s read wrote the destination: %s = %s, a fresh value holds %s", half, k, have, want)
			}
		}
	}
	// The reader's own file still reads between the two refusals, which is
	// what makes the refusal the entry's and not the build's.
	if r := runProbe(t, classes, "Probe_reads", newFile); r.n < 1 {
		t.Errorf("R32: the reader's own file must read beside the retired refusal: %+v", r)
	}
}

// t03R10: R10 "the run-time walk of a stranger's layout and the recompute of
// the header's hash are retired". The load path is t01R7's (no fnv, no
// .hash(, no compile(, no parse(); the file's hash taken as given) and the
// hash-as-given half is t02R4's; this subtest widens the ban to the WHOLE
// emitted build — the runtime holds no recompute of the header's hash anywhere,
// not only on the load path — and holds §5.9 #23's retirement print, which
// names where the stranger's-walk coverage is owed.
func t03R10(t *testing.T) {
	u := unitFrom(t, t03Flat)
	own, ok := FixedLineageOf(u, "T")
	if !ok {
		t.Fatal("R10: FixedLineageOf has no entry for T")
	}
	files, err := GenerateLineage(u, map[string][]FixedLineageEntry{"T": {{
		Wire: own.Wire ^ 0x5a5a5a5a, Layout: own.Layout, Record: own.Record,
	}}})
	if err != nil {
		t.Fatalf("R10: GenerateLineage: %v", err)
	}
	for name, src := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		// THE RUNTIME NEVER COMPUTES A HASH FROM LAYOUT BYTES IT HOLDS (§5.3
		// step 4's rule, wider than the step): the only fnv in the emitted
		// build is the TableFixed.hash function nobody calls — the writer
		// spells the compile-time constant and the load takes the header's
		// hash as given.
		if strings.Contains(string(src), "TableFixed.hash(") {
			t.Errorf("R10: %s calls TableFixed.hash: the recompute of the header's hash is retired, on every path and not only the load's", name)
		}
		load := t03Method(string(src), "public static int load(")
		if load == "" {
			continue
		}
		for _, banned := range []string{"fnv", "compile(", "parse("} {
			if strings.Contains(load, banned) {
				t.Errorf("R10: %s's load reaches %q: a stranger's layout is never walked at run time", name, banned)
			}
		}
	}
	// §5.9 #23: a leg whose suite has no skip prints one instead — the six
	// families §5.6 retires, each named at the call site with where the
	// coverage is owed. The gate runs Main (make tables-java-fixedform).
	mainSrc, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "java-fixedform", "src", "Main.java"))
	if err != nil {
		t.Fatalf("R10: read Main.java: %v", err)
	}
	for _, name := range []string{"textUnderAnArm", "anOlderWriter", "aNewerWriter", "anOptional", "theSlide", "layoutValidation"} {
		if !strings.Contains(string(mainSrc), "retired(\""+name+"\"") {
			t.Errorf("R10: Main.java names no retirement for %q: §5.9 #23 wants the retired case named at the call site with where its coverage is owed", name)
		}
	}
}

// t03R15: R15 "the four forward-read clamps are retired — count clamp across
// bounds, range clamp across versions, remap of an unknown variant to None,
// drop-and-count of an unknown field: each is layout_newer now". The four
// rows' OLD-REFUSES-NEW columns are TestFixedVersioningRows' (the rows are in
// versioningRows and none is a sameHash row, so the column runs); this
// subtest proves the four rows are in the suite and runs one of them for
// real, so a row quietly dropped from the suite is red by name.
func t03R15(t *testing.T) {
	// The four clauses' rows: field_append is the drop-and-count of an
	// unknown field, array_bounded_grow the count clamp across bounds,
	// range_widen the range clamp across versions, enum_append the remap of
	// an unknown variant to None.
	for _, row := range []string{"field_append", "array_bounded_grow", "range_widen", "enum_append"} {
		inRows := false
		for _, r := range versioningRows {
			if r.name == row {
				inRows = true
			}
		}
		if !inRows {
			t.Errorf("R15: %s is not in TestFixedVersioningRows' rows: its OLD-REFUSES-NEW column is where the retired clamp is layout_newer by name", row)
		}
		if sameHashRows[row] {
			t.Errorf("R15: %s is a same-hash row: it reads in both directions and cannot carry the refusal", row)
		}
	}
	// And the run: a newer writer's file under the older reader refuses
	// layout_newer before any record, on the file's hash alone.
	newFile := t03CorpusFile(t, "new_enum_append.bin")
	_, classes := buildRow(t, t.TempDir(), "enum_append", []sideSpec{
		{key: "reads", schema: "VNEW_enum_append.schema", older: []string{"VOLD_enum_append.schema"}},
		{key: "refuses", schema: "VOLD_enum_append.schema"},
	})
	ref := runProbe(t, classes, "Probe_refuses", newFile)
	if !ref.refused || ref.reason != "layoutNewer" {
		t.Errorf("R15: the retired remap-to-None read: refused=%v reason=%s, want a refusal named layoutNewer", ref.refused, ref.reason)
	}
	if ref.malformed || ref.n != -1 || ref.clamped != 0 {
		t.Errorf("R15: the retired clamp moved on a newer file: %+v; a forward read refuses whole, before any record", ref)
	}
}

// t03SaveLoadProbe is the capability probe: it SETS the fields a card names
// (non-default and boundary values), saves with the leg's own generated
// writer, prints the bytes, loads them back into a FRESH value and dumps the
// report and the landed fields. The setters are spelled in the probe's source
// by the Go side; the probe itself is generated, never hand-edited.
func t03SaveLoadProbe(class, pkg, root, setters string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `import java.lang.reflect.*;
import java.nio.file.*;

// GENERATED BY internal/codegen/javatable/fixedroadmap_t03_test.go. The
// capability probe: write with the leg's own writer, read with its own
// reader, assert the round trip.
public final class %s {
    static final StringBuilder out = new StringBuilder();

    public static void main(String[] args) throws Exception {
        %s.%sFixed.Value[] vals = { new %s.%sFixed.Value() };
        %s
        final byte[] bytes = new byte[%s.%sFixed.measure(1)];
        %s.%sFixed.save(vals, 1, bytes);
        final StringBuilder hex = new StringBuilder();
        for (byte x : bytes) { hex.append(String.format("%%02x", x)); }
        out.append("BYTES ").append(hex).append('\n');
        %s.%sFixed.Value[] back = { new %s.%sFixed.Value() };
        %s.TableFixed.Report report = new %s.TableFixed.Report();
        final int n = %s.%sFixed.load(back, 1, bytes,
                %s.TableFixed.plan(1 << 14), new short[1 << 14], %s.%sFixed.image(), report);
        out.append("REPORT n=").append(n)
           .append(" refused=").append(report.refused)
           .append(" reason=").append(report.reason)
           .append(" malformed=").append(report.malformed)
           .append(" unknown=").append(report.unknown)
           .append(" kindMismatch=").append(report.kindMismatch)
           .append(" widened=").append(report.widened)
           .append(" clamped=").append(report.clamped)
           .append('\n');
        dump("VALUE", back[0]);
        System.out.print(out);
    }
`, class, pkg, root, pkg, root, setters, pkg, root, pkg, root, pkg, root, pkg, root, pkg, pkg, pkg, root, pkg, pkg, root)
	b.WriteString(`
    static void dump(String tag, Object v) throws Exception {
        walk(tag, "", v);
    }

    static void walk(String tag, String path, Object v) throws Exception {
        if (v == null) { out.append(tag).append(' ').append(path).append("=null\n"); return; }
        Class<?> c = v.getClass();
        if (c.isArray()) {
            int n = Array.getLength(v);
            Class<?> el = c.getComponentType();
            if (el == byte.class) {
                byte[] a = (byte[]) v;
                StringBuilder h = new StringBuilder();
                for (byte x : a) { h.append(String.format("%02x", x)); }
                out.append(tag).append(' ').append(path).append("=bytes:").append(h).append('\n');
                return;
            }
            if (el.isPrimitive()) {
                StringBuilder s = new StringBuilder();
                for (int i = 0; i < n; i++) { if (i > 0) { s.append(','); } s.append(String.valueOf(Array.get(v, i))); }
                out.append(tag).append(' ').append(path).append("=list:").append(s).append('\n');
                return;
            }
            for (int i = 0; i < n; i++) { walk(tag, path + "[" + i + "]", Array.get(v, i)); }
            return;
        }
        if (v instanceof Float) {
            out.append(tag).append(' ').append(path).append('=').append(String.valueOf(v))
               .append("|0x").append(String.format("%08X", Float.floatToRawIntBits((Float) v))).append('\n');
            return;
        }
        if (v instanceof Double) {
            out.append(tag).append(' ').append(path).append('=').append(String.valueOf(v))
               .append("|0x").append(String.format("%016X", Double.doubleToRawLongBits((Double) v))).append('\n');
            return;
        }
        if (c == String.class || c.isPrimitive() || v instanceof Number || v instanceof Boolean
                || v instanceof Character || c.isEnum()) {
            out.append(tag).append(' ').append(path).append('=').append(String.valueOf(v)).append('\n');
            return;
        }
        Field[] fs = c.getFields();
        java.util.Arrays.sort(fs, (a, b) -> a.getName().compareTo(b.getName()));
        for (Field f : fs) {
            if (Modifier.isStatic(f.getModifiers())) { continue; }
            String p = path.isEmpty() ? f.getName() : path + "." + f.getName();
            walk(tag, p, f.get(v));
        }
    }
}
`)
	return b.String()
}

// t03Build is buildRow with room for the harness's own probes: every file of
// `extra` (name → source) is written into the same src tree and compiled by
// the same javac, so a probe sees every generated class.
func t03Build(t *testing.T, dir, row string, sides []sideSpec, extra map[string]string) string {
	t.Helper()
	if len(extra) == 0 {
		_, classes := buildRow(t, dir, row, sides)
		return classes
	}
	// write the extra probes first, then let buildRow's glob pick them up:
	// buildRow compiles every *.java at the src root beside its own probes.
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range extra {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, classes := buildRow(t, dir, row, sides)
	return classes
}

// t03TwiceProbe reads ONE file twice in one process, with the same report and
// the same destination, and dumps both halves: "refused by name, once,
// idempotently" needs the second read to answer exactly as the first did. A
// FRESH value is dumped first, so a refusal that wrote the destination is
// red by name against it.
func t03TwiceProbe(class, pkg, root string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `import java.lang.reflect.*;
import java.nio.file.*;

// GENERATED BY internal/codegen/javatable/fixedroadmap_t03_test.go. The
// idempotence probe: one file, two loads, both answers, and the fresh value
// they must both leave the destination equal to.
public final class %s {
    static final StringBuilder out = new StringBuilder();

    public static void main(String[] args) throws Exception {
        final byte[] data = Files.readAllBytes(Paths.get(args[0]));
        %s.%sFixed.Value[] vals = new %s.%sFixed.Value[4];
        for (int i = 0; i < vals.length; i++) { vals[i] = new %s.%sFixed.Value(); }
        %s.TableFixed.Report report = new %s.TableFixed.Report();
        %s.TableFixed.Entry[] plan = %s.TableFixed.plan(1 << 14);
        short[] remap = new short[1 << 14];
        byte[] image = %s.%sFixed.image();
        dump("FRESH", vals[0]);

        int n1 = %s.%sFixed.load(vals, vals.length, data, plan, remap, image, report);
        out.append("FIRST n=").append(n1)
           .append(" refused=").append(report.refused)
           .append(" reason=").append(report.reason)
           .append(" malformed=").append(report.malformed)
           .append(" unknown=").append(report.unknown)
           .append(" kindMismatch=").append(report.kindMismatch)
           .append(" widened=").append(report.widened)
           .append(" clamped=").append(report.clamped)
           .append(" layoutHash=").append("0x" + Long.toHexString(report.layoutHash))
           .append('\n');
        dump("FIRSTFIELD", vals[0]);

        int n2 = %s.%sFixed.load(vals, vals.length, data, plan, remap, image, report);
        out.append("SECOND n=").append(n2)
           .append(" refused=").append(report.refused)
           .append(" reason=").append(report.reason)
           .append(" malformed=").append(report.malformed)
           .append(" unknown=").append(report.unknown)
           .append(" kindMismatch=").append(report.kindMismatch)
           .append(" widened=").append(report.widened)
           .append(" clamped=").append(report.clamped)
           .append(" layoutHash=").append("0x" + Long.toHexString(report.layoutHash))
           .append('\n');
        dump("SECONDFIELD", vals[0]);
        System.out.print(out);
    }

    static void dump(String tag, Object v) throws Exception {
        for (Field f : v.getClass().getFields()) {
            if (Modifier.isStatic(f.getModifiers())) { continue; }
            out.append(tag).append(' ').append(f.getName()).append('=').append(String.valueOf(f.get(v))).append('\n');
        }
    }
}
`, class, pkg, root, pkg, root, pkg, root, pkg, pkg, pkg, pkg, pkg, root, pkg, root, pkg, root)
	return b.String()
}

func t03RunTwice(t *testing.T, classes, class, file string) string {
	t.Helper()
	_, javaBin := javaTools(t)
	out, err := runJava(t, javaBin, classes, class, file)
	if err != nil {
		t.Fatalf("java %s %s: %v\n%s", class, filepath.Base(file), err, out)
	}
	return out
}

// t03ParseTwice reads one half of the twice-probe's answer, with the FRESH
// dump as the destination's oracle.
func t03ParseTwice(t *testing.T, out, half string) probeRun {
	t.Helper()
	r := probeRun{fresh: map[string]string{}, value: map[string]string{}}
	fieldTag := half + "FIELD"
	for line := range strings.SplitSeq(out, "\n") {
		k, v, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		switch k {
		case "FRESH":
			name, val, _ := strings.Cut(v, "=")
			r.fresh[name] = val
		case fieldTag:
			name, val, _ := strings.Cut(v, "=")
			r.value[name] = val
		case half:
			for tok := range strings.FieldsSeq(v) {
				kk, vv, has := strings.Cut(tok, "=")
				if !has {
					continue
				}
				t03ReportField(&r, kk, vv)
			}
		}
	}
	return r
}

func t03ReportField(r *probeRun, k, v string) {
	switch k {
	case "n":
		r.n, _ = strconv.Atoi(v)
	case "refused":
		r.refused = v == "true"
	case "reason":
		r.reason = v
	case "malformed":
		r.malformed = v == "true"
	case "unknown":
		r.unknown, _ = strconv.Atoi(v)
	case "kindMismatch":
		r.kindMismatch, _ = strconv.Atoi(v)
	case "widened":
		r.widened, _ = strconv.Atoi(v)
	case "clamped":
		r.clamped, _ = strconv.Atoi(v)
	case "layoutHash":
		r.layoutHash = v
	}
}

// t03E3FixedArrayElement: E3/fixed-array-element "fixed_I_grow_element: exact
// per-slot scaled values and older-reader refusal". The per-slot exact landing
// of the reference's bytes is TestFixedVersioningRows' fixed_I_grow_element
// row; the capability this subtest adds is the WRITE: the leg's own generated
// writer saves the per-slot raw floor, zero and ceiling, the reader loads
// them back exact with no counter moved, and the OLDER reader refuses the
// saved file layout_newer on its hash alone.
func t03E3FixedArrayElement(t *testing.T) {
	classes := t03Build(t, t.TempDir(), "fixed_I_grow_element", []sideSpec{
		{key: "reads", schema: "VNEW_fixed_I_grow_element.schema", older: []string{"VOLD_fixed_I_grow_element.schema"}},
		{key: "refuses", schema: "VOLD_fixed_I_grow_element.schema"},
	}, map[string]string{
		"Probe_capability.java": t03SaveLoadProbe("Probe_capability", "vnew_fixed_i_grow_element", "FixedIGrowElement", `
        // THE PER-SLOT SCALED VALUES (the manifest's own for the old writer):
        // the raw floor -128, zero, the raw ceiling 112 — the band the
        // declaration names, min = -8 and max = 7 shifted by F = 4.
        vals[0].vals[0] = -128;
        vals[0].vals[1] = 0;
        vals[0].vals[2] = 112;
        vals[0].vals[3] = 7;
        vals[0].lead = 0xAAAAAAAA;
        vals[0].trail = 0xBBBBBBBB;`),
	})
	out := t03RunSaveLoad(t, classes, "Probe_capability")
	r := parseProbe(t, out)
	if r.n != 1 || r.refused || r.malformed {
		t.Fatalf("E3/fixed-array-element: the round trip did not read: %+v", r)
	}
	if r.clamped != 0 || r.widened != 0 || r.unknown != 0 || r.kindMismatch != 0 {
		t.Errorf("E3/fixed-array-element: a clean round trip moved a counter: %+v", r)
	}
	// The written values land EXACT, per slot: the array dumps as one list.
	if got, ok := r.value["vals"]; !ok {
		t.Error("E3/fixed-array-element: the probe dumped no vals")
	} else if want := "list:-128,0,112,7"; got != want {
		t.Errorf("E3/fixed-array-element: vals = %s, want the exact per-slot scaled values %s (the raw floor, zero, the raw ceiling 112, and 7)", got, want)
	} // THE OLDER READER REFUSES the newer writer's file, on its hash alone.
	saved := t03BytesOf(t, out)
	ref := runProbe(t, classes, "Probe_refuses", saved)
	if !ref.refused || ref.reason != "layoutNewer" {
		t.Errorf("E3/fixed-array-element: the older reader: refused=%v reason=%s, want layoutNewer", ref.refused, ref.reason)
	}
	if want := fileHash(t, saved); ref.layoutHash != want {
		t.Errorf("E3/fixed-array-element: the refusal carries %s, want the file's own hash %s", ref.layoutHash, want)
	}
	if ref.n != -1 || ref.malformed || ref.clamped != 0 {
		t.Errorf("E3/fixed-array-element: the older reader moved on a newer file: %+v", ref)
	}
}

// t03E3OtherWidens: E3/other-required-widens "All other required widen-ladder
// cases". Every widen row of the ladder the corpus carries beside the
// fixed-array element: the leg writes the row's own boundary and non-default
// values, reads them back exact, and the older reader refuses the file. The
// uint rung's 65535 is the 65536 band edge R29 names; the float rung's NaN
// is the bit-exact payload R21 names; the enum rung's 256th variant is the
// ordinal the old byte cannot hold.
func t03E3OtherWidens(t *testing.T) {
	for _, tc := range []struct {
		row     string
		root    string
		setters string
		want    map[string]string
	}{
		{"int_widen", "IntWiden", `
        vals[0].v = -32768;    // the old rung's floor, sign-extended
        vals[0].lead = 0xAAAAAAAA; vals[0].trail = 0xBBBBBBBB;`,
			map[string]string{"v": "-32768", "lead": "-1431655766", "trail": "-1145324613"}},
		{"uint_widen", "UintWiden", `
        vals[0].v = 65535;     // the u16 top: the 65536 band edge
        vals[0].lead = 0xAAAAAAAA; vals[0].trail = 0xBBBBBBBB;`,
			map[string]string{"v": "65535", "lead": "-1431655766", "trail": "-1145324613"}},
		{"float_widen", "FloatWiden", `
        vals[0].v = Double.longBitsToDouble(0x7FF1579BC0000000L); // an exact-widened sNaN
        vals[0].lead = 0xAAAAAAAA; vals[0].trail = 0xBBBBBBBB;`,
			map[string]string{"v": "NaN|0x7FF1579BC0000000", "lead": "-1431655766", "trail": "-1145324613"}},
		{"bits_grow", "BitsGrow", `
        vals[0].v = 4095;      // 2^12 - 1: the grown width's own top
        vals[0].lead = 0xAAAAAAAA; vals[0].trail = 0xBBBBBBBB;`,
			map[string]string{"v": "4095", "lead": "-1431655766", "trail": "-1145324613"}},
		{"fixed_I_grow", "FixedIGrow", `
        vals[0].v = 112;       // the raw ceiling: max = 7 shifted by F = 4
        vals[0].lead = 0xAAAAAAAA; vals[0].trail = 0xBBBBBBBB;`,
			map[string]string{"v": "112", "lead": "-1431655766", "trail": "-1145324613"}},
		{"array_elem_widen", "ArrayElemWiden", `
        vals[0].valsCount = 4;
        vals[0].vals[0] = -1; vals[0].vals[1] = -32768; vals[0].vals[2] = 32767; vals[0].vals[3] = 100000;
        vals[0].lead = 0xAAAAAAAA; vals[0].trail = 0xBBBBBBBB;`,
			map[string]string{"valsCount": "4", "vals": "list:-1,-32768,32767,100000", "lead": "-1431655766", "trail": "-1145324613"}},
		{"enum_width", "Lineage", `
        vals[0].tier = 256;    // the 256th variant: the ordinal the old byte cannot hold
        vals[0].seq = 14;`,
			map[string]string{"tier": "256", "seq": "14"}},
	} {
		t.Run(tc.row, func(t *testing.T) {
			t.Parallel()
			pkg := javaPackageOf(versioningSchemaUnit(t, "VNEW_"+tc.row+".schema"))
			if pkg == "" {
				t.Fatalf("%s: the NEW side declares no package", tc.row)
			}
			classes := t03Build(t, t.TempDir(), tc.row, []sideSpec{
				{key: "reads", schema: "VNEW_" + tc.row + ".schema", older: []string{"VOLD_" + tc.row + ".schema"}},
				{key: "refuses", schema: "VOLD_" + tc.row + ".schema"},
			}, map[string]string{
				"Probe_capability.java": t03SaveLoadProbe("Probe_capability", pkg, tc.root, tc.setters),
			})
			out := t03RunSaveLoad(t, classes, "Probe_capability")
			r := parseProbe(t, out)
			if r.n != 1 || r.refused || r.malformed {
				t.Fatalf("%s: the round trip did not read: %+v", tc.row, r)
			}
			if r.clamped != 0 || r.widened != 0 || r.unknown != 0 || r.kindMismatch != 0 {
				t.Errorf("%s: a clean round trip moved a counter: %+v", tc.row, r)
			}
			// The written values land EXACT, field by field: the want map is
			// the oracle, and the dump names every field.
			for name, want := range tc.want {
				if got, ok := r.value[name]; !ok {
					t.Errorf("%s: the probe dumped no %s", tc.row, name)
				} else if got != want {
					t.Errorf("%s: %s landed %s, want the exact written %s", tc.row, name, got, want)
				}
			}
			saved := t03BytesOf(t, out)
			ref := runProbe(t, classes, "Probe_refuses", saved)
			if !ref.refused || ref.reason != "layoutNewer" {
				t.Errorf("%s: the older reader: refused=%v reason=%s, want layoutNewer before any record", tc.row, ref.refused, ref.reason)
			}
			if want := fileHash(t, saved); ref.layoutHash != want {
				t.Errorf("%s: the refusal carries %s, want the file's own hash %s", tc.row, ref.layoutHash, want)
			}
		})
	}
}

// t03C8: C8 "fixed-point F-shift / bits(N)". ALGORITHM §4.6: "A fixed-point
// field's bounds are in VALUE UNITS and its storage is raw, so both ends are
// shifted by F first; a bits(N) clamps to 2^N - 1." A forged value past the
// WRITER's raw bound lands the bound and counts clamped == 1 EXACTLY, on the
// compiled lane (the writer's bounds, bill §12.5) and on the identity lane
// (the reader's own pass) alike.
func t03C8(t *testing.T) {
	t.Run("fixed_point_F_shift", func(t *testing.T) {
		t.Parallel()
		// fixed(4,4) | min = -8, max = 7: the raw band is [-128, 112]. Forge
		// the old writer's byte past the ceiling — 127 — and both builds
		// must land 112 with clamped == 1: the NEW build through the entry's
		// writer bounds, the OLD build through its own scatter's pass.
		oldFile := t03CorpusFile(t, "old_fixed_I_grow.bin")
		raw, err := os.ReadFile(oldFile)
		if err != nil {
			t.Fatal(err)
		}
		needle := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0xFF} // lead, then raw -1
		at := bytes.Index(raw, needle)
		if at < 0 || bytes.Contains(raw[at+1:], needle) {
			t.Fatalf("C8: the forge needle % x is not once in old_fixed_I_grow.bin", needle)
		}
		forged := append([]byte(nil), raw...)
		forged[at+4] = 127 // past the raw ceiling 112, inside the byte
		path := filepath.Join(t.TempDir(), "hostile_fixed_I_grow.bin")
		if err := os.WriteFile(path, forged, 0o644); err != nil {
			t.Fatal(err)
		}
		_, classes := buildRow(t, t.TempDir(), "fixed_I_grow", []sideSpec{
			{key: "reads", schema: "VNEW_fixed_I_grow.schema", older: []string{"VOLD_fixed_I_grow.schema"}},
			{key: "refuses", schema: "VOLD_fixed_I_grow.schema"},
		})
		got := runProbe(t, classes, "Probe_reads", path)
		if got.n != 1 || got.refused || got.malformed {
			t.Fatalf("C8: the forged file must read: %+v", got)
		}
		if v := got.value["v"]; v != "112" {
			t.Errorf("C8: the compiled lane landed v = %s, want the writer's raw ceiling 112 (min = -8, max = 7 shifted by F = 4)", v)
		}
		if got.clamped != 1 {
			t.Errorf("C8: the compiled lane counted clamped = %d, want exactly 1: the bounds pass clamps to the WRITER's bound and counts (ALG 4.6, bill 12.5)", got.clamped)
		}
		own := runProbe(t, classes, "Probe_refuses", path)
		if own.n != 1 || own.refused || own.malformed {
			t.Fatalf("C8: the old build must read its own forged file: %+v", own)
		}
		if v := own.value["v"]; v != "112" {
			t.Errorf("C8: the identity lane landed v = %s, want the reader's own raw ceiling 112", v)
		}
		if own.clamped != 1 {
			t.Errorf("C8: the identity lane counted clamped = %d, want exactly 1", own.clamped)
		}
	})
	t.Run("bits_width", func(t *testing.T) {
		t.Parallel()
		// bits(12) rides two bytes, so a forged 0xFFFF is past 2^12 - 1:
		// the width's own top is a bound the storage byte count does not
		// enforce (ALG 4.6), and the read clamps to 4095 and counts.
		newFile := t03CorpusFile(t, "new_bits_grow.bin")
		raw, err := os.ReadFile(newFile)
		if err != nil {
			t.Fatal(err)
		}
		needle := []byte{0xAA, 0xAA, 0xAA, 0xAA, 0xFF, 0x0F} // lead, then 0xFFF
		at := bytes.Index(raw, needle)
		if at < 0 || bytes.Contains(raw[at+1:], needle) {
			t.Fatalf("C8: the forge needle % x is not once in new_bits_grow.bin", needle)
		}
		forged := append([]byte(nil), raw...)
		forged[at+5] = 0xFF // 0xFFF -> 0xFFFF, past 2^12 - 1
		path := filepath.Join(t.TempDir(), "hostile_bits_grow.bin")
		if err := os.WriteFile(path, forged, 0o644); err != nil {
			t.Fatal(err)
		}
		_, classes := buildRow(t, t.TempDir(), "bits_grow", []sideSpec{
			{key: "reads", schema: "VNEW_bits_grow.schema", older: []string{"VOLD_bits_grow.schema"}},
		})
		got := runProbe(t, classes, "Probe_reads", path)
		if got.n != 1 || got.refused || got.malformed {
			t.Fatalf("C8: the forged file must read: %+v", got)
		}
		if v := got.value["v"]; v != "4095" {
			t.Errorf("C8: bits(12) landed v = %s, want the width's own top 2^12 - 1 = 4095", v)
		}
		if got.clamped != 1 {
			t.Errorf("C8: bits(12) counted clamped = %d, want exactly 1", got.clamped)
		}
	})
}

// t03R20: R20 "§21.2's landing rules". Every clause is
// TestFixedVersioningRows' NEW-READS-OLD column (assertValuesLand: an added
// field its declared default, a widened integer or float exact, the shorter
// array's and string's slack the template's zeros, an older enum's ordinals
// the reader's) and TestFixedVersioningUnknownCensus' ("a deprecated field is
// dropped and counted once under unknown per plan": unknown == 1, once per
// field per peer). This subtest holds the deprecated-field clause's exact
// count on the many-records file, which is the row's own divergence.
func t03R20(t *testing.T) {
	manyFile := t03CorpusFile(t, "many_unknown_census.bin")
	_, classes := buildRow(t, t.TempDir(), "unknown_census", []sideSpec{
		{key: "reads", schema: "VNEW_unknown_census.schema", older: []string{"VOLD_unknown_census.schema"}},
	})
	got := runProbe(t, classes, "Probe_reads", manyFile)
	if got.n < 2 {
		t.Fatalf("R20: many_unknown_census.bin carries the row's records: n=%d", got.n)
	}
	if got.unknown != 1 {
		t.Errorf("R20: unknown = %d, want exactly 1: a deprecated field is dropped and counted ONCE per plan, never per record (§21.2, §5.9 #30)", got.unknown)
	}
	if got.kindMismatch != 0 || got.widened != 0 || got.clamped != 0 || got.malformed || got.refused {
		t.Errorf("R20: the census read moved something else: %+v", got)
	}
}

// t03R21: R21 "widenf is the bit-exact widening — signalling NaNs kept, the
// quiet bit carried as the writer wrote it". The corpus's old_float_wide file
// carries a SIGNALLING NaN, 0x7F8ABCDE, whose bits are the value; the
// widened f64 must be the PATTERN widened — sign, exponent and payload
// carried, the quiet bit as the writer wrote it (clear) — and never through
// the hardware's conversion, which sets the quiet bit.
func t03R21(t *testing.T) {
	oldFile := t03CorpusFile(t, "old_float_widen.bin")
	_, classes := buildRow(t, t.TempDir(), "float_widen", []sideSpec{
		{key: "reads", schema: "VNEW_float_widen.schema", older: []string{"VOLD_float_widen.schema"}},
		{key: "refuses", schema: "VOLD_float_widen.schema"},
	})
	got := runProbe(t, classes, "Probe_reads", oldFile)
	if got.n != 1 || got.refused || got.malformed {
		t.Fatalf("R21: the old file must read: %+v", got)
	}
	const sNaN = uint32(0x7F8ABCDE)
	want := uint64(sNaN&0x80000000)<<32 | 0x7FF0000000000000 | uint64(sNaN&0x7FFFFF)<<29
	v, ok := got.value["v"]
	if !ok {
		t.Fatal("R21: the probe dumped no v")
	}
	_, hexBits, hasBits := strings.Cut(v, "|0x")
	if !hasBits {
		t.Fatalf("R21: the probe's v = %q carries no bits half", v)
	}
	bits, err := strconv.ParseUint(hexBits, 16, 64)
	if err != nil {
		t.Fatalf("R21: the probe's v bits %q: %v", hexBits, err)
	}
	if bits != want {
		t.Errorf("R21: widenf landed 0x%016X, want the pattern widened 0x%016X — the quiet bit carried as the writer wrote it, never set by the conversion", bits, want)
	}
	if got.widened != 1 || got.clamped != 0 {
		t.Errorf("R21: the counters: widened=%d clamped=%d, want widened == 1 and nothing else", got.widened, got.clamped)
	}
	// The OLD build's own identity read is the oracle: its f32 bits are the
	// writer's pattern, unchanged (SPEC §3: no canonicalisation).
	oracle := runProbe(t, classes, "Probe_refuses", oldFile)
	if o, ok := oracle.value["v"]; !ok || o != "NaN|0x7F8ABCDE" {
		t.Errorf("R21: the oracle's own read = %v, want NaN|0x7F8ABCDE", o)
	}
}

// t03R29: R29 "the band case: the widening across the 65536 ceiling, and the
// bounds pass clamping to the writer's bounds". The band edge is the u16 top
// 65535 widened into a u32 — ZERO-extended, never sign-extended — asserted
// here DIRECTLY on the landed value and not through the two-build decimal
// escape the row's comparison carries; the writer's bounds are the forged 150
// of the range row, inside the reader's 0..200 and past the writer's 0..100,
// which only the entry's own bounds can catch (bill §12.5).
func t03R29(t *testing.T) {
	t.Run("band_edge", func(t *testing.T) {
		t.Parallel()
		oldFile := t03CorpusFile(t, "old_uint_widen.bin")
		_, classes := buildRow(t, t.TempDir(), "uint_widen", []sideSpec{
			{key: "reads", schema: "VNEW_uint_widen.schema", older: []string{"VOLD_uint_widen.schema"}},
			{key: "refuses", schema: "VOLD_uint_widen.schema"},
		})
		got := runProbe(t, classes, "Probe_reads", oldFile)
		if got.n != 1 || got.refused || got.malformed {
			t.Fatalf("R29: the old file must read: %+v", got)
		}
		if v := got.value["v"]; v != "65535" {
			t.Errorf("R29: the band edge landed v = %s, want 65535 zero-extended across the 65536 ceiling, never sign-extended to -1", v)
		}
		if got.widened != 1 || got.clamped != 0 {
			t.Errorf("R29: the band edge counters: widened=%d clamped=%d, want widened == 1 and nothing else", got.widened, got.clamped)
		}
	})
	t.Run("writer_bounds", func(t *testing.T) {
		t.Parallel()
		// The writer's | 0..100, the reader's | 0..200: the forged 150 is
		// between them, and the bounds pass must land the WRITER's 100 and
		// count exactly one clamp (the reference's range_widen_hostile, bill
		// §12.5: "clamp to 100, COUNT clamped, never land 150").
		oldFile := t03CorpusFile(t, "old_range_widen.bin")
		raw, err := os.ReadFile(oldFile)
		if err != nil {
			t.Fatal(err)
		}
		needle := make([]byte, 8)
		binary.LittleEndian.PutUint32(needle[:4], 0xAAAAAAAA)
		binary.LittleEndian.PutUint32(needle[4:], 100)
		at := bytes.Index(raw, needle)
		if at < 0 || bytes.Contains(raw[at+1:], needle) {
			t.Fatalf("R29: the forge needle % x is not once in old_range_widen.bin", needle)
		}
		forged := append([]byte(nil), raw...)
		binary.LittleEndian.PutUint32(forged[at+4:], 150)
		path := filepath.Join(t.TempDir(), "hostile_range_widen.bin")
		if err := os.WriteFile(path, forged, 0o644); err != nil {
			t.Fatal(err)
		}
		_, classes := buildRow(t, t.TempDir(), "range_widen", []sideSpec{
			{key: "reads", schema: "VNEW_range_widen.schema", older: []string{"VOLD_range_widen.schema"}},
			{key: "refuses", schema: "VOLD_range_widen.schema"},
		})
		got := runProbe(t, classes, "Probe_reads", path)
		if got.n != 1 || got.refused || got.malformed {
			t.Fatalf("R29: the forged file must read: %+v", got)
		}
		if v := got.value["v"]; v != "100" {
			t.Errorf("R29: the forged 150 landed v = %s, want the WRITER's max 100 — the bounds pass clamps to the plan's bounds and never the reader's wider own", v)
		}
		if got.clamped != 1 {
			t.Errorf("R29: clamped = %d, want exactly 1 (bill 12.5, the reference's range_widen_hostile)", got.clamped)
		}
		if got.widened != 0 || got.unknown != 0 || got.kindMismatch != 0 {
			t.Errorf("R29: the hostile range read moved something else: %+v", got)
		}
		// The clean value 100 lands unclamped on the same build, so the
		// clamp above is the forged byte's and not the read's.
		clean := runProbe(t, classes, "Probe_reads", oldFile)
		if clean.clamped != 0 || clean.value["v"] != "100" {
			t.Errorf("R29: the lawful 100 must land unclamped: %+v", clean)
		}
	})
}

// t03E5: E5 "?T vs plain nesting". The clause is held twice on this leg: the
// versioning harness's optional_add row (T into ?T: present == 1, the value
// exact) and Main.java's anOptional (P1's plain Link against P3's ?Link is a
// kind that MOVED and is SEEN — one byte apart, reported rather than silent).
// This subtest runs the row's both columns for real.
func t03E5(t *testing.T) {
	oldFile := t03CorpusFile(t, "old_optional_add.bin")
	newFile := t03CorpusFile(t, "new_optional_add.bin")
	_, classes := buildRow(t, t.TempDir(), "optional_add", []sideSpec{
		{key: "reads", schema: "VNEW_optional_add.schema", older: []string{"VOLD_optional_add.schema"}},
		{key: "refuses", schema: "VOLD_optional_add.schema"},
	})
	got := runProbe(t, classes, "Probe_reads", oldFile)
	if got.n != 1 || got.refused || got.malformed {
		t.Fatalf("E5: the plain-T file must read through the ?T reader: %+v", got)
	}
	if p := got.value["linkPresent"]; p != "true" {
		t.Errorf("E5: T into ?T lands present = %s, want true — the wrapper's byte is a CONSTANT 1 beside the payload (bill 12.8)", p)
	}
	if v := got.value["link.value"]; v != "777" {
		t.Errorf("E5: the wrapped payload landed %s, want the writer's 777 exact", v)
	}
	// And the ?T writer's own file read by the ?T reader: the flag is the
	// writer's, and an ABSENT optional's payload is the template's zeros
	// (§3.4) — the plain-nesting difference, one byte, rides whole.
	own := runProbe(t, classes, "Probe_reads", newFile)
	if p := own.value["linkPresent"]; p != "false" {
		t.Errorf("E5: the ?T writer's own flag landed %s, want false", p)
	}
	if v := own.value["link.value"]; v != "0" {
		t.Errorf("E5: an absent optional's payload landed %s, want the template's zeros", v)
	}
}

// t03W2: W2 "absent optional skips store". The end-to-end holder is
// Main.java's p3Write (the gate's make tables-java-fixedform: an absent
// optional this port writes carries the template's zeros, not the caller's
// storage, and the SAME payload PRESENT does put those bytes on the wire).
// This subtest pins the emitted writer's branch directly: the store is
// SKIPPED behind a false flag and the bytes are zeroed — one `if`, and no
// store of the caller's value.
func t03W2(t *testing.T) {
	u := unitFrom(t, `package probe

fixed table Leaf
{
    n int32
}

fixed table T
{
    link ?Leaf
}
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("W2: Generate: %v", err)
	}
	src := string(files["TFixed.java"])
	body := t03Method(src, "public static void writeBody(")
	if body == "" {
		t.Fatal("W2: the emitted writer has no writeBody")
	}
	if !strings.Contains(body, "linkPresent ? 1 : 0") {
		t.Error("W2: the writer does not spell the present flag")
	}
	// The skip itself: behind a false flag the bytes are FILLED with zero
	// and the payload's store is inside the taken branch only.
	if !strings.Contains(body, "if (v.linkPresent) {") {
		t.Fatal("W2: the writer has no present branch to skip the store in")
	}
	i := strings.Index(body, "if (v.linkPresent) {")
	rest := body[i:]
	j := strings.Index(rest, "} else {")
	if j < 0 {
		t.Fatal("W2: the present branch has no else half")
	}
	taken := rest[:j]
	if !strings.Contains(taken, "LeafFixed.writeBody(") && !strings.Contains(taken, "TableFixed.put32") {
		t.Errorf("W2: the taken branch does not store the payload:\n%s", taken)
	}
	elseHalf := rest[j:]
	if k := strings.Index(elseHalf, "\n    }"); k >= 0 {
		elseHalf = elseHalf[:k]
	}
	if !strings.Contains(elseHalf, "Arrays.fill") || !strings.Contains(elseHalf, "(byte) 0") {
		t.Errorf("W2: the absent half does not zero the payload's bytes — an absent optional SKIPS the store and what rides is the template's zeros (SPEC-TABLES 3.4):\n%s", elseHalf)
	}
	if strings.Contains(elseHalf, "put32") || strings.Contains(elseHalf, "arraycopy") {
		t.Error("W2: the absent half stores the caller's storage")
	}
}

func t03RunSaveLoad(t *testing.T, classes, class string) string {
	t.Helper()
	_, javaBin := javaTools(t)
	out, err := runJava(t, javaBin, classes, class)
	if err != nil {
		t.Fatalf("java %s: %v\n%s", class, err, out)
	}
	return out
}

// t03BytesOf writes the probe's BYTES line to a file the readers can open.
func t03BytesOf(t *testing.T, out string) string {
	t.Helper()
	for line := range strings.SplitSeq(out, "\n") {
		if !strings.HasPrefix(line, "BYTES ") {
			continue
		}
		hexStr := strings.TrimPrefix(line, "BYTES ")
		raw := make([]byte, len(hexStr)/2)
		for i := range raw {
			b, err := strconv.ParseUint(hexStr[i*2:i*2+2], 16, 8)
			if err != nil {
				t.Fatalf("the probe's bytes are not hex: %v", err)
			}
			raw[i] = byte(b)
		}
		path := filepath.Join(t.TempDir(), "saved.bin")
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	t.Fatal("the probe printed no BYTES line")
	return ""
}

// runJava runs one compiled probe and answers its combined output, with the
// harness's file arguments when the probe reads any.
func runJava(t *testing.T, javaBin, classes, class string, args ...string) (string, error) {
	t.Helper()
	cmdArgs := append([]string{"-ea", "-cp", classes, class}, args...)
	out, err := exec.Command(javaBin, cmdArgs...).CombinedOutput()
	return string(out), err
}
