package javatable

import (
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// EVERY DEAD BYTE IN A FIXED RECORD IS ZERO (docs/FIXED-FORM-ALGORITHM.md
// §3.1, §4.5, §5.3; docs/SPEC-TABLES.md §3.4, §13.10; docs/rulings.json
// fixed-dead-bytes-are-zero). The shared vectors are fixedvector.DeadBytes.
// Each vector is read by both the identity reader and a newer reader carrying
// the writer's layout in its lineage, with wider array, string and bytes
// capacities. A file the list marks refused is refused by the generated load —
// the verdict only, never the reason — and the caller's destination is the value
// it was handed. Identity controls save back byte for byte.
func TestFixedDeadBytesAreZero(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "javac and java (it runs the generated reader over the dead-byte corpus)")
	corpus := corpusDir(t)
	javac, javaBin := javaToolsRequired(t)
	classes, pkg, root := buildDeadBytesReader(t, javac)
	lineageClasses, lineagePkg, lineageRoot := buildDeadBytesLineageReader(t, javac)

	for _, vec := range fixedvector.DeadBytes {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			if want.Refused == vec.Reads {
				t.Fatalf("%s: the manifest says refused=%v, DeadBytes says reads=%v", vec.File, want.Refused, vec.Reads)
			}
			file := filepath.Join(corpus, vec.File)
			r := runDeadBytesProbe(t, javaBin, classes, pkg, root, file)
			checkDeadBytesResult(t, vec, r, "identity", true)
			if vec.Reads && vec.File == "absent_zero.bin" {
				for key, want := range map[string]string{
					"optPresent":       "false",
					"opt.n":            "0",
					"opt.kind":         "1",
					"opt.childPresent": "false",
					"opt.child":        "0",
				} {
					if got := r.value[key]; got != want {
						t.Fatalf("identity absent optional retained stale %s=%q, want %q", key, got, want)
					}
				}
			}
			r = runDeadBytesProbe(t, javaBin, lineageClasses, lineagePkg, lineageRoot, file)
			checkDeadBytesResult(t, vec, r, "lineage", false)
			if vec.Reads && vec.File == "absent_zero.bin" && (r.value["freshPresent"] != "false" || r.value["fresh"] != "0") {
				t.Fatalf("lineage reader did not reset absent fresh payload: present=%q value=%q", r.value["freshPresent"], r.value["fresh"])
			}
		})
	}

	nestedClasses, nestedPkg, nestedRoot, nestedWriter := buildNestedDeadBytesLineageReader(t, javac)
	for _, test := range []struct {
		name     string
		innerTag byte
		deadAt   int
	}{
		{name: "nested-tag-zero", innerTag: 0, deadAt: 2},
		{name: "nested-narrow-tail", innerTag: 2, deadAt: 6},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			data := nestedDeadBytesFile(nestedWriter, test.innerTag, test.deadAt)
			file := filepath.Join(t.TempDir(), "nested-dead.bin")
			if err := os.WriteFile(file, data, 0o600); err != nil {
				t.Fatal(err)
			}
			r := runDeadBytesProbe(t, javaBin, nestedClasses, nestedPkg, nestedRoot, file)
			if r.n == 1 && !r.refused && !r.damaged {
				t.Fatal("nested lineage reader accepted non-zero union dead bytes")
			}
			if !mapsEqual(r.before, r.value) {
				t.Fatal("nested lineage refusal wrote the caller's destination")
			}
		})
	}
}

func TestFixedOptionalResetUsesStaticWideUnionDefault(t *testing.T) {
	files := generate(t, `package probe

union Choice
{
    wide uint128
    narrow int32
}

fixed table Payload
{
    high int128 = -250 | min = -5000000000, max = 5000000000
    choice Choice
}

fixed table Root
{
    opt ?Payload
}
`)
	choice := string(files["ChoiceFixed.java"])
	payload := string(files["PayloadFixed.java"])
	root := string(files["RootFixed.java"])
	if !strings.Contains(choice, "static final UInt128 __default_wide = UInt128.zero;") {
		t.Fatal("union Value does not hoist its wide zero default")
	}
	if !strings.Contains(choice, "wide = Value.__default_wide;") {
		t.Fatal("union reset would allocate or lose its wide default")
	}
	if !strings.Contains(payload, "static final Int128 __default_high = new Int128(") || !strings.Contains(payload, "high = Value.__default_high;") {
		t.Fatal("nested nonzero wide default is not restored from its static value")
	}
	if !strings.Contains(root, "opt.reset();") {
		t.Fatal("optional nested table reset was inlined instead of calling the per-type helper")
	}
}

func checkDeadBytesResult(t *testing.T, vec fixedvector.DeadVector, r deadProbeRun, reader string, saveBack bool) {
	t.Helper()
	read := r.n == 1 && !r.refused && !r.damaged
	if !vec.Reads {
		if read {
			t.Fatalf("%s reader: %s read the file, want a refusal", reader, vec.File)
		}
		if !mapsEqual(r.before, r.value) {
			t.Fatalf("%s reader: %s refusal wrote the caller's destination", reader, vec.File)
		}
		return
	}
	if !read {
		t.Fatalf("%s reader: %s control did not read: n=%d refused=%v damaged=%v", reader, vec.File, r.n, r.refused, r.damaged)
	}
	if saveBack && !r.saved {
		t.Fatalf("%s reader: %s control did not save back byte for byte (written=%d file=%d)", reader, vec.File, r.written, r.file)
	}
}

func buildDeadBytesReader(t *testing.T, javac string) (classes, pkg, root string) {
	t.Helper()
	u := versioningSchemaUnit(t, "DeadBytes.schema")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	pkg = javaPackageOf(u)
	root = rootOf(t, u).Name
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	classes = filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixedJava(t, src, pkg, files)
	if err := os.WriteFile(filepath.Join(src, "DeadProbe.java"), []byte(deadBytesProbe(pkg, root)), 0o644); err != nil {
		t.Fatal(err)
	}
	sources, err := filepath.Glob(filepath.Join(src, "*", "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)
	args = append(args, filepath.Join(src, "DeadProbe.java"))
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	return classes, pkg, root
}

func buildDeadBytesLineageReader(t *testing.T, javac string) (classes, pkg, root string) {
	t.Helper()
	old := versioningSchemaUnit(t, "DeadBytes.schema")
	path := filepath.Join("..", "..", "..", "test", "tables", "DeadBytes.schema")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	newerSrc := string(src)
	for _, change := range [][2]string{
		{"    arr  [..3]int16", "    arr  [..4]int16"},
		{"    text string(8)", "    text string(12)"},
		{"    blob bytes(4)", "    blob bytes(8)"},
		{"    pick DeadPick\n}", "    pick DeadPick\n    newer int32 = 0\n    fresh ?int32\n}"},
	} {
		updated := strings.Replace(newerSrc, change[0], change[1], 1)
		if updated == newerSrc {
			t.Fatalf("could not apply lineage schema change %q", change[0])
		}
		newerSrc = updated
	}
	newer := unitFrom(t, newerSrc)
	oldEntry, ok := FixedLineageOf(old, "DeadRoot")
	if !ok {
		t.Fatal("no fixed DeadRoot writer layout")
	}
	files, err := GenerateLineage(newer, map[string][]FixedLineageEntry{"DeadRoot": {oldEntry}})
	if err != nil {
		t.Fatal(err)
	}
	pkg = javaPackageOf(newer)
	root = rootOf(t, newer).Name
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	classes = filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixedJava(t, srcDir, pkg, files)
	if err := os.WriteFile(filepath.Join(srcDir, "DeadProbe.java"), []byte(deadBytesProbe(pkg, root)), 0o644); err != nil {
		t.Fatal(err)
	}
	sources, err := filepath.Glob(filepath.Join(srcDir, "*", "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)
	args = append(args, filepath.Join(srcDir, "DeadProbe.java"))
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac lineage reader: %v\n%s", err, out)
	}
	return classes, pkg, root
}

func buildNestedDeadBytesLineageReader(t *testing.T, javac string) (classes, pkg, root string, writer FixedLineageEntry) {
	t.Helper()
	const olderSource = `package probe

union Inner
{
    wide int64
    narrow int32
}

union Outer
{
    nested Inner
    other int32
}

fixed table Root
{
    choice Outer
    direct Inner
}
`
	const newerSource = `package probe

union Inner
{
    wide int64
    narrow int32
}

union Outer
{
    nested Inner
    other int32
}

fixed table Root
{
    choice Outer
    direct Inner
    newer int32 = 0
}
`
	older := unitFrom(t, olderSource)
	newer := unitFrom(t, newerSource)
	var ok bool
	writer, ok = FixedLineageOf(older, "Root")
	if !ok {
		t.Fatal("no writer layout for nested union reader")
	}
	files, err := GenerateLineage(newer, map[string][]FixedLineageEntry{"Root": {writer}})
	if err != nil {
		t.Fatal(err)
	}
	pkg = javaPackageOf(newer)
	root = rootOf(t, newer).Name
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	classes = filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixedJava(t, srcDir, pkg, files)
	if err := os.WriteFile(filepath.Join(srcDir, "DeadProbe.java"), []byte(deadBytesProbe(pkg, root)), 0o644); err != nil {
		t.Fatal(err)
	}
	sources, err := filepath.Glob(filepath.Join(srcDir, "*", "*.java"))
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)
	args = append(args, filepath.Join(srcDir, "DeadProbe.java"))
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac nested lineage reader: %v\n%s", err, out)
	}
	return classes, pkg, root, writer
}

func nestedDeadBytesFile(writer FixedLineageEntry, innerTag byte, deadAt int) []byte {
	layout := writer.Layout
	data := make([]byte, 16+4+len(layout)+int(writer.Record))
	data[0] = 3
	binary.LittleEndian.PutUint64(data[8:16], writer.Wire)
	binary.LittleEndian.PutUint32(data[16:20], uint32(len(layout)))
	copy(data[20:], layout)
	recordAt := 20 + len(layout)
	binary.LittleEndian.PutUint64(data[recordAt:recordAt+8], writer.Wire)
	body := data[recordAt+8:]
	body[0] = 1 // select Outer.nested
	body[1] = innerTag
	body[deadAt] = 0x80
	return data
}

type deadProbeRun struct {
	n, written, file int
	refused, damaged bool
	saved            bool
	before, value    map[string]string
}

func runDeadBytesProbe(t *testing.T, javaBin, classes, pkg, root, file string) deadProbeRun {
	t.Helper()
	cmd := exec.Command(javaBin, "-ea", "-cp", classes, "DeadProbe", file)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("java %s: %v\n%s", filepath.Base(file), err, out)
	}
	r := deadProbeRun{before: map[string]string{}, value: map[string]string{}}
	for line := range strings.SplitSeq(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "REPORT "):
			r.n, r.refused, r.damaged = parseDeadReport(line)
		case strings.HasPrefix(line, "BEFORE "):
			k, v, _ := strings.Cut(strings.TrimPrefix(line, "BEFORE "), "=")
			r.before[k] = v
		case strings.HasPrefix(line, "VALUE "):
			k, v, _ := strings.Cut(strings.TrimPrefix(line, "VALUE "), "=")
			r.value[k] = v
		case strings.HasPrefix(line, "SAVE "):
			r.saved, r.written, r.file = parseDeadSave(line)
		}
	}
	if len(r.before) == 0 || len(r.value) == 0 {
		t.Fatalf("java %s printed no destination:\n%s", filepath.Base(file), out)
	}
	return r
}

func parseDeadReport(line string) (n int, refused, damaged bool) {
	for tok := range strings.FieldsSeq(strings.TrimPrefix(line, "REPORT ")) {
		k, v, ok := strings.Cut(tok, "=")
		if !ok {
			continue
		}
		switch k {
		case "n":
			fmt.Sscanf(v, "%d", &n)
		case "refused":
			refused = v == "true"
		case "damaged":
			damaged = v == "true"
		}
	}
	return n, refused, damaged
}

func parseDeadSave(line string) (match bool, written, file int) {
	for tok := range strings.FieldsSeq(strings.TrimPrefix(line, "SAVE ")) {
		k, v, ok := strings.Cut(tok, "=")
		if !ok {
			continue
		}
		switch k {
		case "match":
			match = v == "true"
		case "written":
			fmt.Sscanf(v, "%d", &written)
		case "file":
			fmt.Sscanf(v, "%d", &file)
		}
	}
	return match, written, file
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func deadBytesProbe(pkg, root string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `import java.lang.reflect.*;
import java.nio.file.*;

// One probe for the dead-byte vectors. The destination is poisoned before the
// load, so a refusal that zero-fills or prefills is not the value that was handed in.
public final class DeadProbe {
    static final StringBuilder out = new StringBuilder();

    public static void main(String[] args) throws Exception {
        byte[] data = Files.readAllBytes(Paths.get(args[0]));
        %s.%sFixed.Value[] vals = new %s.%sFixed.Value[1];
        vals[0] = new %s.%sFixed.Value();
        poison(vals[0]);
        poisonFinals(vals[0]);
        dump("BEFORE", vals[0]);
        %s.TableFixed.Report report = new %s.TableFixed.Report();
        int n = %s.%sFixed.load(vals, vals.length, data,
            %s.TableFixed.plan(1 << 14), new short[1 << 14], %s.%sFixed.image(), report);
        out.append("REPORT n=").append(n)
           .append(" refused=").append(report.refused)
           .append(" damaged=").append(report.damaged)
           .append('\n');
        dump("VALUE", vals[0]);
        if (n == 1 && !report.refused && !report.damaged) {
            byte[] back = new byte[%s.%sFixed.measure(1)];
            int w = %s.%sFixed.save(vals, 1, back);
            boolean match = w == data.length && java.util.Arrays.equals(back, data);
            out.append("SAVE match=").append(match)
               .append(" written=").append(w)
               .append(" file=").append(data.length)
               .append('\n');
        }
        System.out.print(out);
    }

    static void dump(String tag, Object v) throws Exception { walk(tag, "", v); }

    static void walk(String tag, String path, Object v) throws Exception {
        if (v == null) { out.append(tag).append(' ').append(path).append("=null\n"); return; }
        Class<?> c = v.getClass();
        if (c.isArray()) {
            int n = Array.getLength(v);
            Class<?> el = c.getComponentType();
            if (el == byte.class) {
                byte[] a = (byte[]) v;
                StringBuilder h = new StringBuilder();
                for (byte x : a) { h.append(String.format("%%02x", x)); }
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
               .append("|0x").append(String.format("%%08X", Float.floatToRawIntBits((Float) v))).append('\n');
            return;
        }
        if (v instanceof Double) {
            out.append(tag).append(' ').append(path).append('=').append(String.valueOf(v))
               .append("|0x").append(String.format("%%016X", Double.doubleToRawLongBits((Double) v))).append('\n');
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

    static void poisonFinals(Object v) throws Exception {
        if (v == null) { return; }
        Class<?> c = v.getClass();
        if (c.isArray()) {
            if (c.getComponentType().isPrimitive()) { return; }
            for (int i = 0, n = Array.getLength(v); i < n; i++) {
                Object kid = Array.get(v, i);
                if (kid != null) { poison(kid); poisonFinals(kid); }
            }
            return;
        }
        for (Field f : c.getFields()) {
            if (Modifier.isStatic(f.getModifiers())) { continue; }
            Class<?> t = f.getType();
            if (t.isPrimitive() || t == String.class) { continue; }
            Object kid = f.get(v);
            if (kid != null) { poison(kid); poisonFinals(kid); }
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
`, pkg, root, pkg, root, pkg, root, pkg, pkg, pkg, root, pkg, pkg, root, pkg, root, pkg, root)
	return b.String()
}
