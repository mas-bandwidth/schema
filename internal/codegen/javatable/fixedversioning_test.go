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
	"sort"
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
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "REPORT "):
			for _, tok := range strings.Fields(strings.TrimPrefix(line, "REPORT ")) {
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
// and the classes directory its probes run against.
type javaBuild struct {
	pkg  string
	root string
}

// buildRow generates both sides of a row, writes the probes and compiles
// everything with this leg's own toolchain. One `javac` per row; the column is
// still the unit (§5.9 #18).
func buildRow(t *testing.T, dir, row string, sides map[string]string) (map[string]javaBuild, string) {
	t.Helper()
	javac, _ := javaTools(t)
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	builds := map[string]javaBuild{}
	var names []string
	for side, schema := range sides {
		u := versioningSchemaUnit(t, schema)
		files, err := Generate(u)
		if err != nil {
			t.Fatalf("%s: generate %s: %v", row, schema, err)
		}
		pkg := javaPackageOf(u)
		if pkg == "" {
			t.Fatalf("%s: %s declares no package", row, schema)
		}
		root := rootOf(t, u)
		writeFixedJava(t, src, pkg, files)
		builds[side] = javaBuild{pkg: pkg, root: root.Name}
		names = append(names, side)
	}
	sort.Strings(names)
	for _, side := range names {
		b := builds[side]
		class := "Probe_" + side
		poison := side != "refuses"
		probe := probeSource(class, b.pkg, b.root, poison)
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
		row := row
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
			builds, classes := buildRow(t, dir, row.name, map[string]string{
				"reads":   "VNEW_" + row.name + ".schema",
				"refuses": "VOLD_" + row.name + ".schema",
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
			assertValuesLand(t, oracle, got)
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
func assertValuesLand(t *testing.T, oracle, got probeRun) {
	t.Helper()
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
// reads, a file ONE BELOW refuses `layout_unsupported` (the hash on the report,
// no counter, nothing decoded), and the floor raised by one makes yesterday's
// file refuse today.
func TestFixedVersioningFloor(t *testing.T) {
	corpus := corpusDir(t)
	files := map[string]string{
		"old": filepath.Join(corpus, "old_floor.bin"),
		"mid": filepath.Join(corpus, "mid_floor.bin"),
		"new": filepath.Join(corpus, "new_floor.bin"),
	}
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("the corpus has no %s: run `make tables-fixedform-corpus`", filepath.Base(f))
		}
	}
	dir := t.TempDir()
	_, classes := buildRow(t, dir, "floor", map[string]string{
		"reads": "VNEW_floor.schema",
	})
	// The reader built from the newest layout has a lineage of three, and its
	// floor is 1 + the highest RETIRED index (§5.2). With nothing retired every
	// one of the three reads; with entry 0 retired the oldest refuses
	// `layout_unsupported` and the other two read.
	for name, file := range files {
		got := runProbe(t, classes, "Probe_reads", file)
		if got.refused || got.malformed || got.n < 1 {
			t.Errorf("floor: the %s file does not read on a lineage of three: n=%d refused=%v reason=%s", name, got.n, got.refused, got.reason)
		}
	}
	t.Log("floor_below and floor_raise_live want the lineage handed in with entry 0 RETIRED, " +
		"which needs GenerateLineage on this leg (§5.9 #1): recorded RED")
	t.Error("floor_below: a file one below the floor must refuse layout_unsupported with the file's hash; " +
		"this leg has no floor yet")
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
	_, classes := buildRow(t, dir, "hash", map[string]string{
		"reads": "VNEW_field_append.schema",
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
	_, classes := buildRow(t, dir, "lineage_merge", map[string]string{
		"reads": "VNEW_lineage_merge.schema",
	})
	for _, f := range files {
		got := runProbe(t, classes, "Probe_reads", filepath.Join(corpus, f))
		if got.refused || got.malformed || got.n < 1 {
			t.Errorf("lineage_merge: %s does not read on the merged build: n=%d refused=%v reason=%s",
				f, got.n, got.refused, got.reason)
		}
	}
}
