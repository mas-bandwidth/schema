package javatable

// THE VERSIONING HALF OF THE FIXED FORM ON THE JAVA LEG (§5 of
// docs/FIXED-FORM-ALGORITHM.md; the rows of docs/FIXED-FORM-VERSIONING-TESTS.md).
//
// RED FIRST, and it is the point of this file (§5.7 step 1): every row is
// brought over with BOTH columns and watched to FAIL before a line of the
// reader exists. A row that was green before the reader was written is a row
// asserting nothing.
//
// THE BYTES ARE THE C++ REFERENCE'S. `make tables-fixedform-corpus` writes
// `old_<row>.bin` and `new_<row>.bin` per row, and the rows that are not a pair
// carry the names only the dump states (§5.9 #28): `old_floor.bin`,
// `mid_floor.bin`, `new_floor.bin`, and `old_`/`a_`/`b_`/`new_lineage_merge.bin`.
// Nothing here writes a wire byte.
//
// THE PROBES ARE GENERATED, ONE PER ROW PER COLUMN (§5.9 #18), and they are
// compiled and run with this leg's own toolchain. Two generations of one table
// name DO have a spelling inside one Java classpath — the pair's schemas differ
// by package — so the two probes of a row share one `javac`, and the column is
// still the unit.
//
// THE VALUE ORACLE IS THE LEG'S OWN OLDER BUILD, never a number restated here.
// NEW-READS-OLD is asserted by reading `old_<row>.bin` TWICE: once with the OLD
// build, whose identity plan is the writer's own layout, and once with the NEW
// build through its lineage. Every field the two builds share must be EQUAL
// (that is "every old value lands exactly") and every field only the NEW build
// names must equal what a FRESH new value holds (that is "the reader's tail is
// its declared default"). The destination is POISONED before the read (§5.7,
// §5.9 #17) — in a language whose value is an object and not a byte image, the
// poison is 0x5A through every field by reflection, which is the same test: a
// prefill that did not run leaves the poison where a default belongs.
//
// OLD-REFUSES-NEW reads `new_<row>.bin` with the OLD build into a FRESH value:
// `layout_newer` by name, the FILE's hash on the report and nothing else, no
// counter moved, `malformed` false, and the destination still every field of a
// fresh value (§5.3, §5.7).

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// versioningRow is one definition change: one row of
// docs/FIXED-FORM-VERSIONING-TESTS.md, with the counters §5.4 and §5.9 #10 say
// it owes. An APPEND row owes every counter at zero; a WIDTH row owes `widened`
// nonzero and every other counter zero. No clean row asserts `clamped` at all.
type versioningRow struct {
	name  string
	widen bool
}

var versioningRows = []versioningRow{
	// the LIST rows (§5.7)
	{name: "field_append"},
	{name: "field_deprecate"},
	{name: "field_undeprecate"},
	{name: "enum_append"},
	{name: "enum_width", widen: true},
	{name: "union_append"},
	{name: "union_arm_payload_widen"}, // an APPEND despite its name (§5.7)
	{name: "flags_append"},
	{name: "keyed_array_enum_append"},
	{name: "nested_append"},
	{name: "rename_without_was"},
	// the NUMBER rows (§5.7)
	{name: "array_bounded_grow"},
	{name: "array_elem_widen", widen: true},
	{name: "array_fixed_grow"},
	{name: "bits_grow", widen: true},
	{name: "bytes_grow"},
	{name: "constant_grow"},
	{name: "fixed_I_grow", widen: true},
	{name: "fixed_I_grow_element", widen: true},
	{name: "float_widen", widen: true},
	{name: "int_widen", widen: true},
	{name: "optional_add"},
	{name: "range_widen"},
	{name: "string_grow"},
	{name: "uint_widen", widen: true},
	{name: "wstring_grow"},
}

// sameHashRows move no layout byte and no digest byte, so they have NO second
// column: both sides take the identity plan and the file reads in BOTH
// directions, which is the test that the hash, and only the hash, is the
// version (§5.7).
var sameHashRows = map[string]bool{
	"rename_without_was": true,
	"field_deprecate":    true,
	"field_undeprecate":  true,
}

// corpusDir answers where the C++ reference's bytes are, or skips — unless
// SCHEMA_REQUIRE_CORPUS is set, under which a missing corpus is a FAILURE. A
// bare `go test ./...` on a tree that never built the oracle is right to skip;
// CI is not, which is what the environment variable is for.
func corpusDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("SCHEMA_FIXEDFORM_CORPUS")
	if dir == "" {
		dir = filepath.Join("..", "..", "..", "build", "fixedform-corpus")
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and %s is not there: run `make tables-fixedform-corpus`", dir)
		}
		t.Skipf("no fixed-form corpus at %s: run `make tables-fixedform-corpus`", dir)
	}
	return dir
}

func versioningSchemaUnit(t *testing.T, name string) *ir.Unit {
	t.Helper()
	path := filepath.Join("..", "..", "..", "test", "tables", name)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	f, perrs := parser.Parse(name, src)
	if len(perrs) > 0 {
		t.Fatalf("parse %s: %v", name, perrs[0])
	}
	base := strings.TrimSuffix(name, ".schema")
	u, cerrs := check.Unit([]check.SourceFile{{Path: name, Name: name, Base: base, Bytes: src, AST: f}})
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", name, cerrs[0])
	}
	return u
}

// rootOf is §5.9 #9's rule read off the unit: every declared fixed table is a
// root, and the one a corpus file was written from is the OUTER one — the fixed
// table no other fixed table of the unit names by value. The dump is the
// authority; this picks the same table on every row of the corpus.
func rootOf(t *testing.T, u *ir.Unit) *ir.Struct {
	t.Helper()
	roots := ir.TableFixedRoots(u)
	if len(roots) == 0 {
		t.Fatal("no fixed table in the unit")
	}
	named := map[string]bool{}
	for _, st := range roots {
		for _, f := range st.Fields {
			named[fieldTypeName(f)] = true
		}
	}
	var outer *ir.Struct
	for _, st := range roots {
		if !named[st.Name] {
			outer = st
		}
	}
	if outer == nil {
		outer = roots[len(roots)-1]
	}
	return outer
}

// fieldTypeName is the declared type's name where a field names one, for the
// "reached by value" question rootOf asks. An array or an optional of a table
// carries the element's own name here, so one accessor answers every spelling.
func fieldTypeName(f *ir.Field) string { return f.Type.Name }

func javaPackageOf(u *ir.Unit) string { return u.Package }

// wasNames is the `was =` map of a build: the NEW spelling to the OLD one, for
// every field of every struct the unit declares. A rename THROUGH `was` is not a
// version at all — the id is the hash of the WIRE name and `was` keeps the old
// one, so the layout bytes do not move (§5.1) — but the leg's VALUE surface is
// spelled with the NEW name, so a comparison across two generations pairs the
// two spellings through this map or it pairs nothing.
func wasNames(u *ir.Unit) map[string]string {
	out := map[string]string{}
	for _, f := range u.Files {
		for _, st := range f.Tables {
			collectWas(st, out)
		}
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok {
				collectWas(st, out)
			}
		}
	}
	return out
}

func collectWas(st *ir.Struct, out map[string]string) {
	for _, f := range st.Fields {
		if f.WasName != "" {
			out[f.Name] = f.WasName
		}
	}
}

// wireName translates a dotted field path through the `was =` map, segment by
// segment: the name the WRITER knew is the one the older build's dump is keyed
// by.
func wireName(path string, was map[string]string) string {
	if len(was) == 0 {
		return path
	}
	parts := strings.Split(path, ".")
	for i, p := range parts {
		name, idx, hasIdx := strings.Cut(p, "[")
		if old, ok := was[name]; ok {
			if hasIdx {
				parts[i] = old + "[" + idx
			} else {
				parts[i] = old
			}
		}
	}
	return strings.Join(parts, ".")
}

// probeSource is the generated probe: a reflection dump of the destination
// value, the report, and nothing else. One per row per column (§5.9 #18).
//
// WHY REFLECTION. §5.7's two columns compare VALUES and not a struct's slack
// (§5.9 #17), and a generated Java value derives no equality of its own, so the
// conforming comparison is over the fields themselves — which in Java is the
// view reflection gives, and it is also where the 0x5A poison goes.
func probeSource(class, pkg, root string, poison bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, `import java.lang.reflect.*;
import java.nio.file.*;

// GENERATED BY internal/codegen/javatable/fixedversioning_test.go. One probe,
// one row, one column of docs/FIXED-FORM-VERSIONING-TESTS.md.
public final class %s {
    static final StringBuilder out = new StringBuilder();

    public static void main(String[] args) throws Exception {
        byte[] data = Files.readAllBytes(Paths.get(args[0]));
        %s.%sFixed.Value[] vals = new %s.%sFixed.Value[64];
        for (int i = 0; i < vals.length; i++) { vals[i] = new %s.%sFixed.Value(); }
        %s.TableFixed.Report report = new %s.TableFixed.Report();
        %s.TableFixed.Entry[] plan = %s.TableFixed.plan(1 << 14);
        short[] remap = new short[1 << 14];
        byte[] image = %s.%sFixed.image();

        // A FRESH value, printed first: what every default §5.1 let through
        // must come back as.
        dump("FRESH", new %s.%sFixed.Value());
`, class, pkg, root, pkg, root, pkg, root, pkg, pkg, pkg, pkg, pkg, root, pkg, root)
	if poison {
		b.WriteString(`        // THE POISON (§5.7): 0x5A through every field, because a zero fill
        // looks exactly like a zero default and neither can tell a prefill
        // that ran from one that never did.
        poison(vals[0]);
`)
	}
	fmt.Fprintf(&b, `        int n = %s.%sFixed.load(vals, vals.length, data, plan, remap, image, report);
        out.append("REPORT n=").append(n)
           .append(" refused=").append(report.refused)
           .append(" reason=").append(report.reason)
           .append(" malformed=").append(report.malformed)
           .append(" unknown=").append(report.unknown)
           .append(" kindMismatch=").append(report.kindMismatch)
           .append(" widened=").append(report.widened)
           .append(" clamped=").append(report.clamped)
           .append(" layoutHash=").append(layoutHash(report))
           .append('\n');
        dump("VALUE", vals[0]);
        System.out.print(out);
    }

    // layoutHash: the field §5.9 #15 puts LAST on the report. A build that does
    // not carry it yet answers "absent", and the row that needs it goes red by
    // name rather than by a compile error.
    static String layoutHash(%s.TableFixed.Report r) {
        try {
            Field f = r.getClass().getField("layoutHash");
            return "0x" + Long.toHexString(((Number) f.get(r)).longValue());
        } catch (NoSuchFieldException e) {
            return "absent";
        } catch (Exception e) {
            return "error";
        }
    }
`, pkg, root, pkg)
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

    static void poison(Object v) throws Exception {
        if (v == null) { return; }
        Class<?> c = v.getClass();
        if (c.isArray()) {
            int n = Array.getLength(v);
            Class<?> el = c.getComponentType();
            for (int i = 0; i < n; i++) {
                if (el == byte.class) { Array.setByte(v, i, (byte) 0x5A); }
                else if (el == short.class) { Array.setShort(v, i, (short) 0x5A5A); }
                else if (el == int.class) { Array.setInt(v, i, 0x5A5A5A5A); }
                else if (el == long.class) { Array.setLong(v, i, 0x5A5A5A5A5A5A5A5AL); }
                else if (el == float.class) { Array.setFloat(v, i, Float.intBitsToFloat(0x5A5A5A5A)); }
                else if (el == double.class) { Array.setDouble(v, i, Double.longBitsToDouble(0x5A5A5A5A5A5A5A5AL)); }
                else if (el == boolean.class) { Array.setBoolean(v, i, true); }
                else if (el == char.class) { Array.setChar(v, i, (char) 0x5A5A); }
                else { poison(Array.get(v, i)); }
            }
            return;
        }
        for (Field f : c.getFields()) {
            if (Modifier.isStatic(f.getModifiers()) || Modifier.isFinal(f.getModifiers())) { continue; }
            Class<?> t = f.getType();
            if (t == byte.class) { f.setByte(v, (byte) 0x5A); }
            else if (t == short.class) { f.setShort(v, (short) 0x5A5A); }
            else if (t == int.class) { f.setInt(v, 0x5A5A5A5A); }
            else if (t == long.class) { f.setLong(v, 0x5A5A5A5A5A5A5A5AL); }
            else if (t == float.class) { f.setFloat(v, Float.intBitsToFloat(0x5A5A5A5A)); }
            else if (t == double.class) { f.setDouble(v, Double.longBitsToDouble(0x5A5A5A5A5A5A5A5AL)); }
            else if (t == boolean.class) { f.setBoolean(v, true); }
            else if (t == char.class) { f.setChar(v, (char) 0x5A5A); }
            else if (t == String.class) { f.set(v, "ZZZZZZZZ"); }
            else { poison(f.get(v)); }
        }
    }
}
`)
	return b.String()
}

// probeRun is one probe's answer: the report line's fields and the two dumps.
type probeRun struct {
	n            int
	refused      bool
	reason       string
	malformed    bool
	unknown      int
	kindMismatch int
	widened      int
	clamped      int
	layoutHash   string
	fresh        map[string]string
	value        map[string]string
}

func parseProbe(t *testing.T, out string) probeRun {
	t.Helper()
	r := probeRun{fresh: map[string]string{}, value: map[string]string{}}
	for line := range strings.SplitSeq(out, "\n") {
		switch {
		case strings.HasPrefix(line, "REPORT "):
			for tok := range strings.FieldsSeq(strings.TrimPrefix(line, "REPORT ")) {
				k, v, ok := strings.Cut(tok, "=")
				if !ok {
					continue
				}
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
		case strings.HasPrefix(line, "FRESH "):
			k, v, _ := strings.Cut(strings.TrimPrefix(line, "FRESH "), "=")
			r.fresh[k] = v
		case strings.HasPrefix(line, "VALUE "):
			k, v, _ := strings.Cut(strings.TrimPrefix(line, "VALUE "), "=")
			r.value[k] = v
		}
	}
	return r
}

// fileHash is the eight bytes at offset 8 — the hash the header carries, which
// is what `layout_newer` reports and nothing else (§5.3).
func fileHash(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if len(b) < 16 {
		t.Fatalf("%s is %d bytes: not a file of this form", path, len(b))
	}
	var h uint64
	for i := 7; i >= 0; i-- {
		h = h<<8 | uint64(b[8+i])
	}
	return "0x" + strconv.FormatUint(h, 16)
}

// javaBuild is one generated build of one schema: its package, its root table
// and the probe class that reads files with it.
type javaBuild struct {
	pkg  string
	root string
}

// sideSpec is one generated build a row needs: the schema, the OLDER schemas
// whose locked entries are handed to it as its LINEAGE (§5.9 #1 — the backend
// takes the lineage as data through a second entry point, and a test plays the
// lock in one line), and how many of the oldest of them are marked RETIRED,
// which is what moves the floor.
type sideSpec struct {
	key    string
	schema string
	older  []string
	retire int
}

// lineageOf is the lock a test plays: every fixed root of every older unit, in
// order, oldest first, with the first `retire` entries marked retired.
func lineageOf(t *testing.T, older []string, retire int) map[string][]FixedLineageEntry {
	t.Helper()
	out := map[string][]FixedLineageEntry{}
	for i, schema := range older {
		u := versioningSchemaUnit(t, schema)
		for _, st := range ir.TableFixedRoots(u) {
			e, ok := FixedLineageOf(u, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s in %s", st.Name, schema)
			}
			e.Retired = i < retire
			if e.Retired {
				e.Reason = "retired by the test's lock"
			}
			out[st.Name] = append(out[st.Name], e)
		}
	}
	return out
}

// buildRow generates every side of a row with its lineage, writes the probes and
// compiles the lot with this leg's own toolchain. One `javac` per row; the column
// is still the unit (§5.9 #18).
func buildRow(t *testing.T, dir, row string, sides []sideSpec) (map[string]javaBuild, string) {
	t.Helper()
	javac, _ := javaTools(t)
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	builds := map[string]javaBuild{}
	for _, side := range sides {
		u := versioningSchemaUnit(t, side.schema)
		files, err := GenerateLineage(u, lineageOf(t, side.older, side.retire))
		if err != nil {
			t.Fatalf("%s: generate %s: %v", row, side.schema, err)
		}
		pkg := javaPackageOf(u)
		if pkg == "" {
			t.Fatalf("%s: %s declares no package", row, side.schema)
		}
		root := rootOf(t, u)
		writeFixedJava(t, src, pkg, files)
		builds[side.key] = javaBuild{pkg: pkg, root: root.Name}
		class := "Probe_" + side.key
		probe := probeSource(class, pkg, root.Name, side.key != "refuses")
		if err := os.WriteFile(filepath.Join(src, class+".java"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sources, err := filepath.Glob(filepath.Join(src, "*", "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	probes, err := filepath.Glob(filepath.Join(src, "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)
	args = append(args, probes...)
	cmd := exec.Command(javac, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: javac: %v\n%s", row, err, out)
	}
	return builds, classes
}

func runProbe(t *testing.T, classes, class, file string) probeRun {
	t.Helper()
	_, javaBin := javaTools(t)
	cmd := exec.Command(javaBin, "-ea", "-cp", classes, class, file)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("java %s %s: %v\n%s", class, filepath.Base(file), err, out)
	}
	return parseProbe(t, string(out))
}

// TestFixedVersioningRows is §5.7's two columns, every row, against the C++
// reference's bytes.
func TestFixedVersioningRows(t *testing.T) {
	corpus := corpusDir(t)
	for _, row := range versioningRows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			oldFile := filepath.Join(corpus, "old_"+row.name+".bin")
			newFile := filepath.Join(corpus, "new_"+row.name+".bin")
			for _, f := range []string{oldFile, newFile} {
				if _, err := os.Stat(f); err != nil {
					t.Fatalf("the corpus has no %s: run `make tables-fixedform-corpus`", filepath.Base(f))
				}
			}
			dir := t.TempDir()
			old := "VOLD_" + row.name + ".schema"
			// THE NEWER BUILD IS HANDED THE OLDER ONE'S LOCKED ENTRY as its
			// lineage, which is what a real build reads out of the lock.
			builds, classes := buildRow(t, dir, row.name, []sideSpec{
				{key: "reads", schema: "VNEW_" + row.name + ".schema", older: []string{old}},
				{key: "refuses", schema: old},
			})
			_ = builds

			// NEW-READS-OLD, and the OLD build's own identity read of the same
			// file is the value oracle (never a number restated here).
			oracle := runProbe(t, classes, "Probe_refuses", oldFile)
			if oracle.n < 1 || oracle.refused || oracle.malformed {
				t.Fatalf("the OLD build could not read its own file: %+v", oracle)
			}
			got := runProbe(t, classes, "Probe_reads", oldFile)
			if got.refused || got.malformed || got.n != oracle.n {
				t.Errorf("NEW-READS-OLD: n=%d refused=%v reason=%s malformed=%v, want %d records and no refusal",
					got.n, got.refused, got.reason, got.malformed, oracle.n)
			}
			assertValuesLand(t, oracle, got, wasNames(versioningSchemaUnit(t, "VNEW_"+row.name+".schema")))
			assertCounters(t, "NEW-READS-OLD", row, got)

			// OLD-REFUSES-NEW: `layout_newer`, the FILE's hash and nothing
			// else, no counter moved, nothing malformed, the destination still
			// a fresh value (§5.3, §5.7).
			if sameHashRows[row.name] {
				reads := runProbe(t, classes, "Probe_refuses", newFile)
				if reads.refused || reads.malformed || reads.n < 1 {
					t.Errorf("%s moves no layout byte, so it must READ in both directions: %+v", row.name, reads)
				}
				return
			}
			ref := runProbe(t, classes, "Probe_refuses", newFile)
			if !ref.refused || ref.reason != "layoutNewer" {
				t.Errorf("OLD-REFUSES-NEW: refused=%v reason=%s, want a refusal named layoutNewer", ref.refused, ref.reason)
			}
			if ref.malformed {
				t.Error("OLD-REFUSES-NEW: malformed is set beside a reason; the report is two answers and never both")
			}
			if ref.n != -1 {
				t.Errorf("OLD-REFUSES-NEW: load answered %d, want -1", ref.n)
			}
			if want := fileHash(t, newFile); ref.layoutHash != want {
				t.Errorf("OLD-REFUSES-NEW: layoutHash=%s, want the file's own hash %s (§5.9 #15)", ref.layoutHash, want)
			}
			if ref.unknown != 0 || ref.kindMismatch != 0 || ref.widened != 0 || ref.clamped != 0 {
				t.Errorf("OLD-REFUSES-NEW moved a counter: %+v; REFUSE is total", ref)
			}
			assertFresh(t, ref)
		})
	}
}

// assertValuesLand is §5.7's NEW-READS-OLD column: every field the two builds
// share lands EXACTLY, and every field only the newer build names comes back
// its declared default.
func assertValuesLand(t *testing.T, oracle, got probeRun, was map[string]string) {
	t.Helper()
	// THE NEWER BUILD'S FIELD NAMES, TRANSLATED THROUGH `was =` to the names the
	// writer knew: one field, two spellings, one wire id.
	mine := map[string]string{}
	for k, v := range got.value {
		mine[wireName(k, was)] = v
	}
	fresh := map[string]string{}
	for k, v := range got.fresh {
		fresh[wireName(k, was)] = v
	}
	got = probeRun{n: got.n, fresh: fresh, value: mine}
	for k, want := range oracle.value {
		have, ok := got.value[k]
		if !ok {
			continue // a field the newer build spells differently is the row's own business
		}
		if have == want {
			continue
		}
		if sameNumber(want, have) {
			continue // one number, two Java storage widths (see sameNumber)
		}
		if strings.HasPrefix(want, "bytes:") && strings.HasPrefix(have, "bytes:") {
			if strings.HasPrefix(strings.TrimPrefix(have, "bytes:"), strings.TrimPrefix(want, "bytes:")) {
				continue // a grown buffer keeps the writer's units and prefills the rest
			}
		}
		if strings.HasPrefix(want, "list:") && strings.HasPrefix(have, "list:") {
			w := strings.Split(strings.TrimPrefix(want, "list:"), ",")
			h := strings.Split(strings.TrimPrefix(have, "list:"), ",")
			if len(h) >= len(w) && equalPrefix(w, h) {
				tail := h[len(w):]
				if fresh, ok := got.fresh[k]; ok {
					f := strings.Split(strings.TrimPrefix(fresh, "list:"), ",")
					if len(f) == len(h) && equalPrefix(tail, f[len(w):]) {
						continue // the slots past the writer's take the ELEMENT's defaults
					}
				}
			}
		}
		t.Errorf("NEW-READS-OLD: %s = %s, the writer wrote %s", k, have, want)
	}
	for k, have := range got.value {
		if _, both := oracle.value[k]; both {
			continue
		}
		want, ok := got.fresh[k]
		if !ok {
			continue
		}
		// THE PRESENT COMPANION IS THE ONE NEWER-ONLY FIELD THAT IS NOT A
		// DEFAULT. `T` into `?T` lands `present` — a constant 1 — because the
		// writer sent the value bare and it IS there (bill §12.8, §5.7's
		// `optional_add` row: "present == 1, value exact"). On this leg the
		// companion is spelled `<field>Present` beside the field, so a boolean
		// by that name whose payload the writer carried owes TRUE and not the
		// fresh value's false.
		if strings.HasSuffix(k, "Present") && have == "true" {
			payload := strings.TrimSuffix(k, "Present")
			if _, carried := oracle.value[payload]; carried {
				continue
			}
			nested := false
			for name := range oracle.value {
				if strings.HasPrefix(name, payload+".") {
					nested = true
					break
				}
			}
			if nested {
				continue
			}
		}
		if have != want {
			t.Errorf("NEW-READS-OLD: the appended %s = %s, want its declared default %s", k, have, want)
		}
	}
}

// sameNumber is ONE NUMBER read back at two Java storage widths, and it is a
// fact about this language rather than a softening of the column. Java has no
// unsigned integer: a `uint16` field is a `short` on the older build and an
// `int` on the newer one, so the writer's 0xFFFF prints as -1 on one side and
// 65535 on the other and the VALUE landed exactly. The only difference this
// admits is the unsigned reinterpretation of a NARROWER storage — the newer
// value equals the older plus 2^8, 2^16, 2^32 or 2^64 — and nothing else: a
// wrong value, a sign-extension where a zero-extension was owed, or a moved
// neighbour still fails.
func sameNumber(want, have string) bool {
	w, err1 := strconv.ParseInt(want, 10, 64)
	h, err2 := strconv.ParseInt(have, 10, 64)
	if err1 != nil || err2 != nil || w >= 0 || h <= 0 {
		return false
	}
	for _, bits := range []uint{8, 16, 32} {
		if h == w+int64(1)<<bits {
			return true
		}
	}
	return false
}

func equalPrefix(a, b []string) bool {
	for i := range a {
		if i >= len(b) || a[i] != b[i] {
			return false
		}
	}
	return true
}

// assertFresh is "the destination is still every field of a fresh value":
// REFUSE writes not one destination byte, the prefill included (§5.3).
func assertFresh(t *testing.T, r probeRun) {
	t.Helper()
	for k, want := range r.fresh {
		if have, ok := r.value[k]; ok && have != want {
			t.Errorf("OLD-REFUSES-NEW wrote the destination: %s = %s, a fresh value holds %s", k, have, want)
		}
	}
}

// assertCounters is §5.4 multiplied out by §5.9 #10: an APPEND row owes every
// counter at zero, a WIDTH row owes `widened` nonzero and the rest at zero, and
// no clean row asserts `clamped` at all. The census is structurally zero on a
// lawful lineage (§5.9 #30).
func assertCounters(t *testing.T, column string, row versioningRow, r probeRun) {
	t.Helper()
	if r.unknown != 0 {
		t.Errorf("%s %s: unknown=%d, want 0: a lawful lineage names every field of every writer it selects (§5.9 #30)", column, row.name, r.unknown)
	}
	if r.kindMismatch != 0 {
		t.Errorf("%s %s: kindMismatch=%d, want 0 (§5.9 #30)", column, row.name, r.kindMismatch)
	}
	if r.clamped != 0 {
		t.Errorf("%s %s: clamped=%d, want 0: no clean row asserts a clamp (§5.4)", column, row.name, r.clamped)
	}
	if row.widen && r.widened == 0 {
		t.Errorf("%s %s: widened=0, want nonzero: a width row widens once per entry per record (§5.4)", column, row.name)
	}
	if !row.widen && r.widened != 0 {
		t.Errorf("%s %s: widened=%d, want 0: an append the reader knows is not an event (§5.4)", column, row.name, r.widened)
	}
}

// TestFixedVersioningFloor is §5.7's three floor rows: a file AT the floor
// reads, a file ONE BELOW refuses `layout_unsupported` (the file's hash on the
// report, no counter, nothing decoded), and the floor RAISED BY ONE makes
// yesterday's file refuse today. Three layouts of one table, the reader always
// the newest, and the retire count walks the floor up through them.
func TestFixedVersioningFloor(t *testing.T) {
	corpus := corpusDir(t)
	oldFile := filepath.Join(corpus, "old_floor.bin")
	midFile := filepath.Join(corpus, "mid_floor.bin")
	newFile := filepath.Join(corpus, "new_floor.bin")
	for _, f := range []string{oldFile, midFile, newFile} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("the corpus has no %s: run `make tables-fixedform-corpus`", filepath.Base(f))
		}
	}
	older := []string{"VOLD_floor.schema", "VMID_floor.schema"}
	build := func(t *testing.T, retire int) string {
		t.Helper()
		_, classes := buildRow(t, t.TempDir(), "floor", []sideSpec{
			{key: "reads", schema: "VNEW_floor.schema", older: older, retire: retire},
		})
		return classes
	}

	t.Run("floor_at", func(t *testing.T) {
		t.Parallel()
		// Nothing retired: the floor is 0, the OLDEST file is at it, and all
		// three of the lineage read.
		classes := build(t, 0)
		for _, f := range []string{oldFile, midFile, newFile} {
			got := runProbe(t, classes, "Probe_reads", f)
			if got.refused || got.malformed || got.n < 1 {
				t.Errorf("floor_at: %s must read: n=%d refused=%v reason=%s", filepath.Base(f), got.n, got.refused, got.reason)
			}
		}
	})
	t.Run("floor_below", func(t *testing.T) {
		t.Parallel()
		// Entry 0 retired: the floor is 1, and the file one below it refuses
		// layout_unsupported with the FILE'S hash, nothing decoded and no
		// counter moved — while the file AT the raised floor still reads, which
		// is what makes the floor an index cut rather than a blanket refusal.
		classes := build(t, 1)
		got := runProbe(t, classes, "Probe_reads", oldFile)
		if got.n != -1 || got.reason != "layoutUnsupported" {
			t.Errorf("floor_below: n=%d reason=%s, want -1 and layoutUnsupported", got.n, got.reason)
		}
		if want := fileHash(t, oldFile); got.layoutHash != want {
			t.Errorf("floor_below: layoutHash=%s, want the file's own hash %s (§5.9 #7)", got.layoutHash, want)
		}
		if got.malformed || got.unknown != 0 || got.kindMismatch != 0 || got.widened != 0 || got.clamped != 0 {
			t.Errorf("floor_below moved a counter or set malformed: %+v; REFUSE is total", got)
		}
		if mid := runProbe(t, classes, "Probe_reads", midFile); mid.n < 1 {
			t.Errorf("floor_below: the file AT the raised floor must still read: %+v", mid)
		}
	})
	t.Run("floor_raise_live", func(t *testing.T) {
		t.Parallel()
		// The floor raised by one more: the file that read yesterday refuses
		// today, by name.
		classes := build(t, 2)
		got := runProbe(t, classes, "Probe_reads", midFile)
		if got.n != -1 || got.reason != "layoutUnsupported" {
			t.Errorf("floor_raise_live: yesterday's file must refuse: n=%d reason=%s", got.n, got.reason)
		}
		if own := runProbe(t, classes, "Probe_reads", newFile); own.n < 1 {
			t.Errorf("floor_raise_live: the reader's own file must still read: %+v", own)
		}
	})
}

// TestFixedVersioningHash is §5.7's hash rows: an unknown hash is
// `layout_newer`, a known hash whose bytes differ is `layout_malformed`, and
// the reader's own hash selects the identity plan.
func TestFixedVersioningHash(t *testing.T) {
	corpus := corpusDir(t)
	own := filepath.Join(corpus, "new_field_append.bin")
	if _, err := os.Stat(own); err != nil {
		t.Fatalf("the corpus has no new_field_append.bin: run `make tables-fixedform-corpus`")
	}
	dir := t.TempDir()
	_, classes := buildRow(t, dir, "hash", []sideSpec{
		{key: "reads", schema: "VNEW_field_append.schema", older: []string{"VOLD_field_append.schema"}},
	})
	// hash_identity: the reader's own hash selects the identity plan.
	got := runProbe(t, classes, "Probe_reads", own)
	if got.refused || got.malformed || got.n < 1 {
		t.Errorf("hash_identity: the reader's own file does not read: n=%d refused=%v reason=%s", got.n, got.refused, got.reason)
	}
	// hash_unknown: one byte of the header's hash moved is a hash in no lineage.
	raw, err := os.ReadFile(own)
	if err != nil {
		t.Fatal(err)
	}
	stranger := append([]byte(nil), raw...)
	stranger[8] ^= 0xFF
	path := filepath.Join(dir, "stranger.bin")
	if err := os.WriteFile(path, stranger, 0o644); err != nil {
		t.Fatal(err)
	}
	unknown := runProbe(t, classes, "Probe_reads", path)
	if !unknown.refused || unknown.reason != "layoutNewer" {
		t.Errorf("hash_unknown: refused=%v reason=%s, want layoutNewer", unknown.refused, unknown.reason)
	}
	if want := fileHash(t, path); unknown.layoutHash != want {
		t.Errorf("hash_unknown: layoutHash=%s, want the file's own hash %s and nothing else", unknown.layoutHash, want)
	}
	if unknown.malformed {
		t.Error("hash_unknown: malformed is set beside a reason")
	}
	// hash_known_bytes_differ: a KNOWN hash whose layout bytes differ is
	// `layout_malformed`, one name for all seven §1.1 malformations (§5.3).
	lie := append([]byte(nil), raw...)
	lie[20+4] ^= 0xFF // the first entry's id, behind a hash this reader holds
	liePath := filepath.Join(dir, "lie.bin")
	if err := os.WriteFile(liePath, lie, 0o644); err != nil {
		t.Fatal(err)
	}
	bad := runProbe(t, classes, "Probe_reads", liePath)
	if !bad.refused || bad.reason != "layoutMalformed" {
		t.Errorf("hash_known_bytes_differ: refused=%v reason=%s, want layoutMalformed", bad.refused, bad.reason)
	}
	_ = hex.EncodeToString
}

// TestFixedVersioningLineageMerge is §5.7's `lineage_merge`: two branches
// append different fields, and after the merge BOTH pre-merge files read on the
// merged build (the name-subset rule, bill §8a.1).
func TestFixedVersioningLineageMerge(t *testing.T) {
	corpus := corpusDir(t)
	files := []string{"old_lineage_merge.bin", "a_lineage_merge.bin", "b_lineage_merge.bin", "new_lineage_merge.bin"}
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(corpus, f)); err != nil {
			t.Fatalf("the corpus has no %s: run `make tables-fixedform-corpus`", f)
		}
	}
	dir := t.TempDir()
	_, classes := buildRow(t, dir, "lineage_merge", []sideSpec{
		{key: "reads", schema: "VNEW_lineage_merge.schema", older: []string{
			"VOLD_lineage_merge.schema", "VBRA_lineage_merge.schema", "VBRB_lineage_merge.schema",
		}},
	})
	for _, f := range files {
		got := runProbe(t, classes, "Probe_reads", filepath.Join(corpus, f))
		if got.refused || got.malformed || got.n < 1 {
			t.Errorf("lineage_merge: %s does not read on the merged build: n=%d refused=%v reason=%s",
				f, got.n, got.refused, got.reason)
		}
	}
}
