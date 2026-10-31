package javatable

// fixedroadmap_t18 — the java leg's shared byte oracle (java/W13, java/R27)
// and hostile-input (java/P3, java/P4) rows of docs/roadmap.sexp's node
// `fixed-tables`. Law: docs/FIXED-FORM-ALGORITHM.md §2 (the header: form byte
// at 0, hash at 8, layout length at 16, layout at 20) and §7 (the block-equals-
// the-reference, manifest-is-the-wire, hostile sweep and wrong-plan checks);
// docs/SPEC-TABLES.md §3.4 (the record and its constant sizes).

import (
	"fmt"
	"maps"
	"math"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestFixedJavaT18ManifestAndHeader is the unit tier, pure Go: no javac, no
// java, no corpus beside a synthetic manifest written in t.TempDir(). It proves
// the manifest reader (readManifest) returns a forged row's `forged` values and
// a plain row's `values`, and names a refused row; and that a header built by
// hand (§2) parses to its hash and layout, with a one-hash-byte flip caught.
func TestFixedJavaT18ManifestAndHeader(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	manifest := "" +
		"file=hostile_cfloat_range_widen.bin row=cfloat_range_widen side=hostile root=CfloatRangeWiden records=1 " +
		"forged=r0.aim@104=1.5|0x3FC00000 " +
		"values=r0.lead=1,r0.aim=0.5|0x3F000000,r0.trail=2\n" +
		"file=plain.bin row=plain side=none root=Plain records=2 values=a=7,b=8,c=9\n" +
		"file=text.bin row=text side=none root=Text records=1 values=r0.note=\"first build\",r0.note_length=11,r0.tag=\"a\\,b\",r0.after=5\n"
	if err := os.WriteFile(filepath.Join(dir, "manifest.txt"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	rows := manifestRows(t, dir)
	forged := rows["hostile_cfloat_range_widen.bin"]
	if forged.Values["r0.lead"] != "1" || forged.Values["r0.aim"] != "0.5|0x3F000000" {
		t.Errorf("forged row's values = %v, want the OLD writer's values before the forge", forged.Values)
	}
	if got := forged.Forged["r0.aim@104"]; got != "1.5|0x3FC00000" {
		t.Errorf("forged row's forged r0.aim@104 = %q, want the forged value", got)
	}
	plain := rows["plain.bin"]
	if plain.Values["a"] != "7" || plain.Values["b"] != "8" || plain.Values["c"] != "9" {
		t.Errorf("plain row's values = %v, want the declared values", plain.Values)
	}
	if len(plain.Forged) != 0 {
		t.Errorf("plain row carries forged values %v, want none", plain.Forged)
	}

	// A text value may hold a space and an escaped comma (man_quote): the line
	// is not split on white space past `values=` and a `\,` stays in its pair.
	text := rows["text.bin"]
	if text.Values["r0.note"] != `"first build"` || text.Values["r0.note_length"] != "11" {
		t.Errorf("a text value with a space was cut: %v", text.Values)
	}
	if text.Values["r0.tag"] != `"a\,b"` || text.Values["r0.after"] != "5" {
		t.Errorf("a text value with an escaped comma was split: %v", text.Values)
	}

	// A refused row is named: the reader must fail loudly for a file the
	// manifest does not carry, never return an empty oracle, and must name the
	// value a section does not hold.
	if _, ok, err := manifestLookup(rows, "no_such_file.bin", "values", "x"); ok {
		t.Error("the lookup found a file the manifest does not carry")
	} else if !strings.Contains(err, "no_such_file.bin") {
		t.Errorf("the refused row was not named: %q", err)
	}
	if _, ok, err := manifestLookup(rows, "plain.bin", "values", "nothere"); ok {
		t.Error("the lookup found a value the section does not hold")
	} else if !strings.Contains(err, "nothere") {
		t.Errorf("the missing value was not named: %q", err)
	}

	// The header (§2): form byte at 0, hash at 8, layout length at 16, layout
	// at 20. Build one in Go and parse it; flip one hash byte and the parse
	// must catch it.
	hash := uint64(0x0123456789ABCDEF)
	layout := []byte{3, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3, 0, 0, 0}
	good := buildFixedHeader(hash, layout)
	gotHash, gotLayout, ok := parseFixedHeader(good)
	if !ok {
		t.Fatal("the good header did not parse")
	}
	if gotHash != hash {
		t.Errorf("header hash = %#x, want %#x", gotHash, hash)
	}
	if string(gotLayout) != string(layout) {
		t.Errorf("header layout = % x, want % x", gotLayout, layout)
	}
	bad := append([]byte(nil), good...)
	bad[8] ^= 0xFF
	badHash, _, ok := parseFixedHeader(bad)
	if !ok {
		t.Fatal("the corrupted header did not parse; the form and lengths are still lawful")
	}
	if badHash == hash {
		t.Error("a header with one hash byte flipped parsed to the same hash; the corruption is not caught")
	}
}

// buildFixedHeader writes a form-3 file header per FIXED-FORM-ALGORITHM §2:
// the form byte at 0, seven zero bytes, the eight-byte hash at 8, the u32
// layout length at 16, and the layout at 20.
func buildFixedHeader(hash uint64, layout []byte) []byte {
	b := make([]byte, 20+len(layout))
	b[0] = 3
	for i := range 8 {
		b[8+i] = byte(hash >> (8 * i))
	}
	lePut32(b[16:], uint32(len(layout)))
	copy(b[20:], layout)
	return b
}

// parseFixedHeader answers the header's hash and layout, or false when the form
// byte is not 3, the file is shorter than the fixed header, or the declared
// layout length runs past the file.
func parseFixedHeader(b []byte) (hash uint64, layout []byte, ok bool) {
	if len(b) < 20 || b[0] != 3 {
		return 0, nil, false
	}
	for i := range 8 {
		hash |= uint64(b[8+i]) << (8 * i)
	}
	l := uint64(le32(b[16:]))
	if 20+l > uint64(len(b)) {
		return 0, nil, false
	}
	return hash, b[20 : 20+l], true
}

func lePut32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// ---------------------------------------------------------------------------
// THE FUNCTIONAL TIER: TestFixedRoadmapJavaT18OracleHostile. Every subtest here
// reads the C++ reference's corpus (build/fixedform-corpus, `make
// tables-fixedform-corpus`) or compiles and runs the generated Java, so none of
// it is a unit test; `make tables-java-fixed-t18` runs it.
// ---------------------------------------------------------------------------

func TestFixedRoadmapJavaT18OracleHostile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"java/W13", t18W13},
		{"java/R27", t18R27},
		{"java/P3", t18P3},
		{"java/P4", t18P4},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t18Plain names the schema that wrote each corpus file that is not a side of a
// versioning row (test/tables/fixedform_dump.cpp: emit(dir, "fx1.bin", ...)).
var t18Plain = map[string]string{
	"fx1.bin":   "test/tables/FX1.schema",
	"fx2.bin":   "test/tables/FX2.schema",
	"p1.bin":    "test/tables/P1.schema",
	"p3.bin":    "test/tables/P3.schema",
	"keyed.bin": "tables/examples/Keyed.schema",
	"pack.bin":  "tables/examples/Pack.schema",
	"fxw.bin":   "test/tables/FXW.schema",
	"fu1.bin":   "test/tables/FU1.schema",
	"fu2.bin":   "test/tables/FU2.schema",
}

// t18Prefix is the schema file name prefix of a versioning row's side
// (fixedform_dump.cpp: `many_` is the OLD generation's several records).
var t18Prefix = map[string]string{
	"old": "VOLD_", "new": "VNEW_", "mid": "VMID_", "a": "VBRA_", "b": "VBRB_",
	"many": "VOLD_", "hostile": "VOLD_", "past": "VOLD_",
}

// t18Row is one manifest row with the two units it names: the WRITER's, whose
// header the file carries (W13), and the READER's, which loads it (R27). They
// are the same unit except on the forged sides, which the OLD generation wrote
// and the NEW generation reads through its lineage (fixedversioning_cfloat_
// range_widen_test.go: the reader's bound is the one that counts).
type t18Row struct {
	m      manifestRow
	writer string   // schema path under the repo root
	reader string   // schema path under the repo root
	older  []string // lineage schema names under test/tables, for the reader
}

func t18Rows(t *testing.T, corpus string) []t18Row {
	t.Helper()
	rows := manifestRows(t, corpus)
	var names []string
	for f := range rows {
		names = append(names, f)
	}
	sort.Strings(names)
	var out []t18Row
	for _, f := range names {
		m := rows[f]
		r := t18Row{m: m}
		if s, ok := t18Plain[f]; ok {
			r.writer, r.reader = s, s
		} else {
			pre, ok := t18Prefix[m.Side]
			if !ok {
				t.Fatalf("manifest row %s names side %q, which the Java oracle harness has no schema for", f, m.Side)
			}
			r.writer = "test/tables/" + pre + m.Row + ".schema"
			r.reader = r.writer
			if m.Side == "hostile" || m.Side == "past" {
				r.reader = "test/tables/VNEW_" + m.Row + ".schema"
				r.older = []string{"VOLD_" + m.Row + ".schema"}
			}
		}
		for _, s := range []string{r.writer, r.reader} {
			if _, err := os.Stat(filepath.Join("..", "..", "..", s)); err != nil {
				t.Fatalf("manifest row %s names schema %s, which is not there: %v", f, s, err)
			}
		}
		out = append(out, r)
	}
	return out
}

// t18LoadUnit parses and checks one schema file named relative to the repo root.
func t18LoadUnit(t *testing.T, rel string) *ir.Unit {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	name := filepath.Base(rel)
	f, perrs := parser.Parse(name, src)
	if len(perrs) > 0 {
		t.Fatalf("parse %s: %v", rel, perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: name, Name: name, Base: strings.TrimSuffix(name, ".schema"), Bytes: src, AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", rel, cerrs[0])
	}
	return u
}

// t18Root finds the manifest's root among the roots the Java leg emits a fixed
// form for, and says whether its closure holds a union (which the registered
// java target refuses: compiler/target_java.go, refuseTableArms).
func t18Root(u *ir.Unit, name string) (st *ir.Struct, hasUnion bool, emitted bool) {
	for _, r := range fixedRoots(u) {
		if r.Name == name {
			seen := map[string]bool{}
			var order []*ir.Struct
			fixedCollectTypes(r, seen, &order)
			return r, len(fixedUnions(order)) > 0, true
		}
	}
	return nil, false, false
}

// t18W13: java/W13 "layout+hash == C++ reference" — FIXED-FORM-ALGORITHM §7
// item 1 (the block-equals-the-reference test) over §2 (the header: form byte
// at 0, hash at 8, layout length at 16, layout at 20) and SPEC-TABLES §3.4. For
// every reference file of a corpus row the Java leg emits a fixed form for, the
// header's hash and layout bytes in that file equal the Java leg's own for the
// same unit: the layout the emitter lays down (fixedLayoutBytes over
// fixedWalkRoot, the calls emitRoot makes), the hash it computes over it, and the
// hash constant in the Java it actually generates. The manifest line carries no
// layout (fixedform_dump.cpp, man_finish), so the file is where it is read.
func t18W13(t *testing.T) {
	corpus := corpusDir(t)
	var compared, notEmitted []string
	for _, r := range t18Rows(t, corpus) {
		u := t18LoadUnit(t, r.writer)
		st, _, ok := t18Root(u, r.m.Root)
		if !ok {
			notEmitted = append(notEmitted, r.m.File)
			continue
		}
		raw, err := os.ReadFile(filepath.Join(corpus, r.m.File))
		if err != nil {
			t.Errorf("W13: %s: the corpus file is not readable: %v", r.m.File, err)
			continue
		}
		fileHash, fileLayout, ok := parseFixedHeader(raw)
		if !ok {
			t.Errorf("W13: %s: the reference file's header does not parse (§2)", r.m.File)
			continue
		}
		own := fixedLayoutBytes(fixedWalkRoot(st).entries)
		ownHash := ir.TableFixedLayoutHash(own, st)
		if string(own) != string(fileLayout) {
			t.Errorf("W13: %s: the Java leg's layout for %s differs from the reference's (§7 item 1):\n  reference % x\n  java      % x", r.m.File, st.Name, fileLayout, own)
		}
		if ownHash != fileHash {
			t.Errorf("W13: %s: the Java leg's hash for %s is %#016x, the reference's is %#016x", r.m.File, st.Name, ownHash, fileHash)
		}
		files, err := Generate(u)
		if err != nil {
			t.Errorf("W13: %s: Generate(%s): %v", r.m.File, r.writer, err)
			continue
		}
		want := fmt.Sprintf("public static final long hash = 0x%016xL;", fileHash)
		if src := string(files[fixedClass(st.Name)+".java"]); !strings.Contains(src, want) {
			t.Errorf("W13: %s: the generated %s does not carry the reference's hash constant %q", r.m.File, fixedClass(st.Name), want)
		}
		compared = append(compared, r.m.File)
	}
	t.Logf("W13: compared %d reference files (header hash + layout bytes) against the Java leg: %s", len(compared), strings.Join(compared, " "))
	if len(notEmitted) > 0 {
		t.Logf("W13: %d reference files are of a root the Java leg emits no fixed form for, named and not compared: %s", len(notEmitted), strings.Join(notEmitted, " "))
	}
	if len(compared) == 0 {
		t.Fatal("W13: zero rows compared: the oracle checked nothing")
	}
}

// ---- R27: the manifest is the wire ------------------------------------------

// t18LoadProbe loads one corpus file with the row's generated reader and dumps
// the report and every landed leaf in the shape t18Parse reads. Every leaf of
// every landed record is printed: Boolean, Byte, Short, Character, Integer,
// Long as `I width value`, Float as `F 32 bits`, Double as `D 64 bits`.
const t18LoadProbe = `import java.lang.reflect.*;
import java.nio.file.*;

// GENERATED BY internal/codegen/javatable/fixedroadmap_t18_test.go (java/R27).
public final class @CLASS@ {
    static final StringBuilder out = new StringBuilder();

    public static void main(String[] args) throws Exception {
        byte[] data = Files.readAllBytes(Paths.get(args[0]));
        @PKG@.@ROOT@.Value[] vals = new @PKG@.@ROOT@.Value[64];
        for (int i = 0; i < vals.length; i++) { vals[i] = new @PKG@.@ROOT@.Value(); }
        @PKG@.TableFixed.Report report = new @PKG@.TableFixed.Report();
        @PKG@.TableFixed.Entry[] plan = @PKG@.TableFixed.plan(1 << 14);
        short[] remap = new short[1 << 14];
        byte[] image = @PKG@.@ROOT@.image();
        int n = @PKG@.@ROOT@.load(vals, vals.length, data, plan, remap, image, report);
        out.append("REPORT n=").append(n)
           .append(" refused=").append(report.refused)
           .append(" reason=").append(report.reason)
           .append(" malformed=").append(report.malformed)
           .append(" unknown=").append(report.unknown)
           .append(" kindMismatch=").append(report.kindMismatch)
           .append(" widened=").append(report.widened)
           .append(" clamped=").append(report.clamped)
           .append(" nonFiniteFloat=").append(report.nonFiniteFloat)
           .append('\n');
        for (int i = 0; i < n; i++) { walk("r" + i, vals[i]); }
        System.out.print(out);
    }

    static void leaf(String path, String kind, int width, long v) {
        out.append("L ").append(path).append(' ').append(kind).append(' ').append(width).append(' ').append(v).append('\n');
    }

    static void walk(String path, Object v) throws Exception {
        if (v == null) { return; }
        Class<?> c = v.getClass();
        if (c.isArray()) {
            int n = Array.getLength(v);
            for (int i = 0; i < n; i++) { walk(path + "[" + i + "]", Array.get(v, i)); }
            return;
        }
        if (v instanceof Boolean) { leaf(path, "I", 1, ((Boolean) v) ? 1L : 0L); return; }
        if (v instanceof Byte) { leaf(path, "I", 8, (Byte) v); return; }
        if (v instanceof Short) { leaf(path, "I", 16, (Short) v); return; }
        if (v instanceof Character) { leaf(path, "I", 16, (Character) v); return; }
        if (v instanceof Integer) { leaf(path, "I", 32, (Integer) v); return; }
        if (v instanceof Long) { leaf(path, "I", 64, (Long) v); return; }
        if (v instanceof Float) { leaf(path, "F", 32, Float.floatToRawIntBits((Float) v) & 0xFFFFFFFFL); return; }
        if (v instanceof Double) { leaf(path, "D", 64, Double.doubleToRawLongBits((Double) v)); return; }
        Field[] fs = c.getFields();
        java.util.Arrays.sort(fs, (a, b) -> a.getName().compareTo(b.getName()));
        for (Field f : fs) {
            if (Modifier.isStatic(f.getModifiers())) { continue; }
            walk(path + "." + f.getName(), f.get(v));
        }
    }
}
`

type t18Leaf struct {
	kind  string
	width int
	bits  uint64
}

type t18Result struct {
	probeRun
	leaves map[string]t18Leaf // keyed by t18Norm(path)
}

// t18Norm makes a manifest path and a Java path comparable: the manifest spells
// a field in snake_case (`label_length`, `marks_count`, `link_present`) and the
// Java storage in lowerCamel (`labelLength`, `marksCount`, `linkPresent`), so the
// comparison is over the lower-cased path with its underscores removed.
func t18Norm(p string) string { return strings.ToLower(strings.ReplaceAll(p, "_", "")) }

func t18Parse(t *testing.T, out string) t18Result {
	t.Helper()
	l := t18Result{probeRun: parseProbe(t, out), leaves: map[string]t18Leaf{}}
	for line := range strings.SplitSeq(out, "\n") {
		f := strings.Fields(line)
		if len(f) != 5 || f[0] != "L" {
			continue
		}
		w, _ := strconv.Atoi(f[3])
		v, err := strconv.ParseInt(f[4], 10, 64)
		if err != nil {
			t.Fatalf("unreadable leaf line %q: %v", line, err)
		}
		l.leaves[t18Norm(f[1])] = t18Leaf{kind: f[2], width: w, bits: uint64(v)}
	}
	return l
}

// t18Unquote reads the dump's text spelling (man_quote, man_wtext): `"…"` for a
// byte string and `u"…"` for wide text, a backslash keeping a quote, a backslash
// or a comma, and `\uXXXX` any other unit. It answers the units.
func t18Unquote(v string) (units []uint16, wide bool, ok bool) {
	if strings.HasPrefix(v, "u\"") {
		wide, v = true, v[1:]
	}
	if len(v) < 2 || v[0] != '"' || v[len(v)-1] != '"' {
		return nil, false, false
	}
	v = v[1 : len(v)-1]
	for i := 0; i < len(v); i++ {
		c := v[i]
		if c != '\\' {
			units = append(units, uint16(c))
			continue
		}
		if i+1 >= len(v) {
			return nil, false, false
		}
		if v[i+1] == 'u' {
			if i+6 > len(v) {
				return nil, false, false
			}
			n, err := strconv.ParseUint(v[i+2:i+6], 16, 16)
			if err != nil {
				return nil, false, false
			}
			units = append(units, uint16(n))
			i += 5
			continue
		}
		units = append(units, uint16(v[i+1]))
		i++
	}
	return units, wide, true
}

// t18Number reads a numeric manifest value: a decimal, a float's `<digits>|0x<bits>`
// (the bits are the value) or a fixed-point's `<real>|raw=<int>` (the raw int is).
func t18Number(v string) (*big.Int, bool) {
	if _, hexPart, ok := strings.Cut(v, "|0x"); ok {
		n, ok := new(big.Int).SetString(hexPart, 16)
		return n, ok
	}
	if _, raw, ok := strings.Cut(v, "|raw="); ok {
		v = raw
	}
	n, ok := new(big.Int).SetString(v, 10)
	return n, ok
}

func t18Mask(w int) *big.Int {
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(w)), big.NewInt(1))
}

// t18Compare checks every entry of `want` (manifest spelling) against what the
// Java load landed, and answers what was compared and what disagrees, by key.
func t18Compare(l t18Result, want map[string]string) (compared int, bad []string) {
	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := want[k]
		if units, wide, ok := t18Unquote(v); ok {
			length, lok := l.leaves[t18Norm(k+"Length")]
			if !lok || int(length.bits) != len(units) {
				bad = append(bad, fmt.Sprintf("%s: text length %d landed %d (found=%v)", k, len(units), int64(length.bits), lok))
				continue
			}
			mask := uint64(0xFF)
			if wide {
				mask = 0xFFFF
			}
			good := true
			for i, u := range units {
				leaf, found := l.leaves[t18Norm(fmt.Sprintf("%s[%d]", k, i))]
				if !found || leaf.bits&mask != uint64(u)&mask {
					bad = append(bad, fmt.Sprintf("%s: unit %d landed %#x, the manifest says %#x (found=%v)", k, i, leaf.bits&mask, u, found))
					good = false
					break
				}
			}
			if good {
				compared++
			}
			continue
		}
		n, ok := t18Number(v)
		if !ok {
			bad = append(bad, fmt.Sprintf("%s: the manifest value %q is not a number or a text", k, v))
			continue
		}
		leaf, found := l.leaves[t18Norm(k)]
		if !found {
			bad = append(bad, fmt.Sprintf("%s: the Java load landed no such leaf (manifest %s)", k, v))
			continue
		}
		got := new(big.Int).SetUint64(leaf.bits)
		got.And(got, t18Mask(leaf.width))
		if new(big.Int).And(n, t18Mask(leaf.width)).Cmp(got) != 0 {
			bad = append(bad, fmt.Sprintf("%s: landed %s (%d bits), the manifest says %s", k, got, leaf.width, v))
			continue
		}
		compared++
	}
	return compared, bad
}

// t18Gen is one generated reader of a tree: a unit (with its lineage), its Java
// package and fixed class, and the load probe that reads files with it. Two
// units of one Java package cannot share a tree.
type t18Gen struct {
	key    string
	reader string
	older  []string
	pkg    string
	root   string // the Fixed class, e.g. FxRootFixed
	class  string // the probe class
}

func t18BuildTree(t *testing.T, dir string, gens []*t18Gen) string {
	t.Helper()
	javac, _ := javaTools(t)
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, g := range gens {
		u := t18LoadUnit(t, g.reader)
		files, err := GenerateLineage(u, lineageOf(t, g.older, 0))
		if err != nil {
			t.Fatalf("R27: generate %s: %v", g.reader, err)
		}
		writeFixedJava(t, src, g.pkg, files)
		if t18Needs128(files) {
			pkt, err := javacodegen.Generate(u)
			if err != nil {
				t.Fatalf("R27: packet generate %s: %v", g.reader, err)
			}
			for _, name := range []string{"Int128.java", "UInt128.java"} {
				if data, ok := pkt[name]; ok {
					if err := os.WriteFile(filepath.Join(src, g.pkg, name), data, 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
		}
		probe := strings.NewReplacer("@CLASS@", g.class, "@PKG@", g.pkg, "@ROOT@", g.root).Replace(t18LoadProbe)
		if err := os.WriteFile(filepath.Join(src, g.class+".java"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sources, _ := filepath.Glob(filepath.Join(src, "*", "*.java"))
	probes, _ := filepath.Glob(filepath.Join(src, "*.java"))
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)
	args = append(args, probes...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("R27: javac: %v\n%s", err, out)
	}
	return classes
}

func t18Needs128(files map[string][]byte) bool {
	for _, b := range files {
		if strings.Contains(string(b), "Int128") {
			return true
		}
	}
	return false
}

// t18Expect is what a file's bytes hold, in the manifest's own words: `values=`
// for a plain file, and for a forged one `values=` with the forged entries laid
// over it (the dump's `values=` is what the OLD writer wrote BEFORE the forge,
// fixedform_dump.cpp, "THE HOSTILE PASS"). A forged key spells its byte offset
// (`r0.aim@104`), which is not part of the path.
func t18Expect(m manifestRow) map[string]string {
	want := map[string]string{}
	maps.Copy(want, m.Values)
	for k, v := range m.Forged {
		path, _, _ := strings.Cut(k, "@")
		want[path] = v
	}
	return want
}

// t18Bound is the reader's own declared bound on a value the manifest states
// (ALGORITHM §4.6; the cfloat_range_widen row, schema#1164 ruling B: THE BOUNDS
// PASS IS THE READER'S). The reference's writer stores what it is handed and
// does not enforce the range it declares, so a manifest value past the READER's
// own declared range (a float32 `min`/`max`, an integer's or fixed-point's raw
// range, ir.TableRawRange) lands clamped to that bound and counts exactly one.
// It answers what the reader must land, read off the reader's schema and never
// from a literal here, and whether the value was past the bound.
func t18Bound(reader *ir.Unit, root, path, value string) (string, bool) {
	var st *ir.Struct
	for _, r := range fixedRoots(reader) {
		if r.Name == root {
			st = r
		}
	}
	_, last, _ := strings.Cut(path, ".")
	name, _, _ := strings.Cut(last, "[")
	if st == nil || strings.Contains(last, ".") {
		return "", false
	}
	n, ok := t18Number(value)
	if !ok {
		return "", false
	}
	for _, f := range st.Fields {
		if f.Name != name {
			continue
		}
		if f.HasFloatRange && f.Type.Kind == ir.TFloat32 {
			v := math.Float32frombits(uint32(n.Uint64()))
			switch {
			case float64(v) > f.FMax:
				return fmt.Sprintf("bound|0x%08X", math.Float32bits(float32(f.FMax))), true
			case float64(v) < f.FMin:
				return fmt.Sprintf("bound|0x%08X", math.Float32bits(float32(f.FMin))), true
			}
			return "", false
		}
		switch f.Type.Kind {
		case ir.TInt, ir.TBits, ir.TFixed:
			if lo, hi, ok := ir.TableRawRange(f); ok {
				switch {
				case n.Cmp(hi) > 0:
					return hi.String(), true
				case n.Cmp(lo) < 0:
					return lo.String(), true
				}
			}
		}
	}
	return "", false
}

// t18R27: java/R27 "the manifest is the wire" — FIXED-FORM-ALGORITHM §5.9 #32
// and #37 ("assert the manifest, never read the dump") over SPEC-TABLES §3.4.
// Each manifest row whose schema the Java target emits is loaded with the
// row's own unit through the Java leg's GENERATED load and compared value for
// value against the manifest: a forged file (hostile_*, past_*) against its
// forged values, every other file against `values=`. A value past the
// READER's own declared bound (a forged past_ value, and the two fixed-point rows
// the reference writes outside their declared range) lands clamped to that
// bound, counts exactly one, and is asserted so (the bound read off the
// reader's schema). A row the Java
// target refuses (a union closure, compiler/target_java.go refuseTableArms) or
// a root the leg emits no fixed form for is named and not loaded.
func t18R27(t *testing.T) {
	corpus := corpusDir(t)
	type task struct {
		r    t18Row
		g    *t18Gen
		tree int
	}
	var tasks []*task
	var refused []string
	for _, r := range t18Rows(t, corpus) {
		u := t18LoadUnit(t, r.reader)
		st, hasUnion, emitted := t18Root(u, r.m.Root)
		switch {
		case !emitted:
			refused = append(refused, r.m.File+" (no fixed form emitted for "+r.m.Root+")")
			continue
		case hasUnion:
			refused = append(refused, r.m.File+" (union closure: the registered java target refuses it)")
			continue
		}
		tasks = append(tasks, &task{r: r, g: &t18Gen{
			key: r.reader + "|" + strings.Join(r.older, ","), reader: r.reader, older: r.older,
			pkg: u.Package, root: fixedClass(st.Name),
		}})
	}
	if len(tasks) == 0 {
		t.Fatal("R27: zero rows compared: the oracle loaded nothing")
	}

	// TREES. A reader is built once per (schema, lineage); two readers of one
	// Java package (Keyed and Pack are both `tabledemo`) take separate trees.
	var trees []map[string]*t18Gen // package -> reader
	byKey := map[string]*t18Gen{}
	treeOf := map[string]int{}
	for _, tk := range tasks {
		if prior, ok := byKey[tk.g.key]; ok {
			tk.g = prior
			tk.tree = treeOf[prior.key]
			continue
		}
		tk.g.class = fmt.Sprintf("Probe_%d", len(byKey))
		placed := -1
		for i, tr := range trees {
			if _, clash := tr[tk.g.pkg]; !clash {
				placed = i
				break
			}
		}
		if placed < 0 {
			trees = append(trees, map[string]*t18Gen{})
			placed = len(trees) - 1
		}
		trees[placed][tk.g.pkg] = tk.g
		byKey[tk.g.key] = tk.g
		treeOf[tk.g.key] = placed
		tk.tree = placed
	}
	classes := make([]string, len(trees))
	for i, tr := range trees {
		var gens []*t18Gen
		for _, g := range tr {
			gens = append(gens, g)
		}
		sort.Slice(gens, func(a, b int) bool { return gens[a].class < gens[b].class })
		classes[i] = t18BuildTree(t, filepath.Join(t.TempDir(), fmt.Sprintf("tree%d", i)), gens)
	}

	_, javaBin := javaTools(t)
	nonFinite := manifestNonFinite(t, corpus)
	var loaded, clampedKeys []string
	values := 0
	for _, tk := range tasks {
		m := tk.r.m
		out, err := exec.Command(javaBin, "-ea", "-cp", classes[tk.tree], tk.g.class, filepath.Join(corpus, m.File)).CombinedOutput()
		if err != nil {
			t.Errorf("R27: %s: the Java load failed: %v\n%s", m.File, err, out)
			continue
		}
		l := t18Parse(t, string(out))
		if leaf := nonFinite[m.File]; leaf != "" {
			// A REFERENCE FILE HOLDING A NaN OR AN INFINITY is a must-refuse
			// vector under the finite rule (docs/SPEC-TABLES.md §3.3) until
			// tables-finite/cpp regenerates the corpus: the load refuses it by
			// name and there is no value to compare.
			if l.n != -1 || !l.refused || l.reason != "nonFiniteFloat" || !strings.HasSuffix(l.nonFiniteFloat, "."+leaf) {
				t.Errorf("R27: %s: the reference's non-finite %s was not refused by name: n=%d refused=%v reason=%s nonFiniteFloat=%s", m.File, leaf, l.n, l.refused, l.reason, l.nonFiniteFloat)
			}
			refused = append(refused, m.File+" (a non-finite "+leaf+", refused by name as nonFiniteFloat: owed finite by tables-finite/cpp)")
			continue
		}
		if l.n != int(m.Records) || l.refused || l.malformed {
			t.Errorf("R27: %s: the load answered n=%d refused=%v reason=%s malformed=%v, the manifest holds %d records", m.File, l.n, l.refused, l.reason, l.malformed, m.Records)
			continue
		}
		want := t18Expect(m)
		wantClamped := 0
		reader := t18LoadUnit(t, tk.r.reader)
		keys := make([]string, 0, len(want))
		for k := range want {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if bound, past := t18Bound(reader, m.Root, k, want[k]); past {
				clampedKeys = append(clampedKeys, m.File+":"+k+"="+want[k])
				want[k] = bound
				wantClamped++
			}
		}
		if l.clamped != wantClamped {
			t.Errorf("R27: %s: clamped=%d, want exactly %d (a forged value past the reader's own bound counts one; every other value lands whole)", m.File, l.clamped, wantClamped)
		}
		if m.Side != "hostile" && m.Side != "past" && (l.unknown != 0 || l.kindMismatch != 0 || l.widened != 0) {
			t.Errorf("R27: %s: a read under the file's own layout moved a counter: %+v", m.File, l.probeRun)
		}
		n, bad := t18Compare(l, want)
		for _, b := range bad {
			t.Errorf("R27: %s: %s", m.File, b)
		}
		if n == 0 {
			t.Errorf("R27: %s: zero values compared", m.File)
		}
		values += n
		loaded = append(loaded, m.File)
	}
	t.Logf("R27: compared %d rows value for value against the manifest (%d values): %s", len(loaded), values, strings.Join(loaded, " "))
	t.Logf("R27: %d manifest values are past the reader's own declared bound and land clamped to it, counted once each: %s", len(clampedKeys), strings.Join(clampedKeys, " "))
	t.Logf("R27: %d rows refused or not emitted, named and not loaded: %s", len(refused), strings.Join(refused, "; "))
}

// ---- P3 and P4: the hostile checks, inline units ---------------------------

// t18RequireJava is javaTools with the gate the card asks for: a missing
// toolchain skips by name, and under SCHEMA_REQUIRE_CORPUS it FAILS by name.
func t18RequireJava(t *testing.T) {
	t.Helper()
	if os.Getenv("SCHEMA_REQUIRE_CORPUS") == "" {
		javaTools(t) // skips, naming the missing JDK, when there is none
		return
	}
	ok := func(jc, j string) bool {
		if jc == "" || j == "" {
			return false
		}
		return exec.Command(jc, "-version").Run() == nil && exec.Command(j, "-version").Run() == nil
	}
	if ok(os.Getenv("JAVAC"), os.Getenv("JAVA")) {
		return
	}
	jc, e1 := exec.LookPath("javac")
	j, e2 := exec.LookPath("java")
	if e1 == nil && e2 == nil && ok(jc, j) {
		return
	}
	if home := "/Users/glenn/toolchains/schema-dist/jdk-21.0.12.1/Contents/Home"; ok(home+"/bin/javac", home+"/bin/java") {
		return
	}
	t.Fatal("SCHEMA_REQUIRE_CORPUS is set and no working javac and java are on PATH or named by JAVAC and JAVA")
}

// t18Compile generates an inline unit's fixed form and compiles it with the
// probes (name -> source, in package `probe`).
func t18Compile(t *testing.T, schema string, probes map[string]string) string {
	t.Helper()
	u := unitFrom(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	javac, _ := javaTools(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixedJava(t, src, u.Package, files)
	for name, body := range probes {
		if err := os.WriteFile(filepath.Join(src, u.Package, name+".java"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := filepath.Glob(filepath.Join(src, u.Package, "*.java"))
	args := append([]string{"--release", "17", "-nowarn", "-d", classes}, all...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	return classes
}

const t18P3Schema = `package probe

fixed table T
{
    a int32
    b int32
}
`

// t18P3Probe is the byte-flip sweep (the C# twin: cstable/fixedroadmap_t08_test.go,
// t08P3). One record through the generated save; every bit of every byte of the
// file flipped in turn; each copy loaded through the generated load. A load
// answers one of three ways and never a fourth: refused by name, malformed, or
// values landed. A throw is the fourth answer; the JVM's bounds checks are the
// sanitizer.
const t18P3Probe = `package probe;

// GENERATED BY internal/codegen/javatable/fixedroadmap_t18_test.go (java/P3).
public final class Probe_p3 {
    public static void main(String[] args) {
        final TFixed.Value v = new TFixed.Value();
        v.a = 12345;
        v.b = -67890;
        final byte[] good = new byte[TFixed.measure(1)];
        if (TFixed.save(new TFixed.Value[] { v }, 1, good) != good.length) {
            System.out.println("P3-FAIL save did not fill the measure");
            System.exit(1);
        }
        final TableFixed.Entry[] plan = TableFixed.plan(1 << 14);
        final short[] remap = new short[1 << 14];
        int refused = 0, malformed = 0, landed = 0, bad = 0, flips = 0;
        final java.util.TreeMap<String, Integer> names = new java.util.TreeMap<>();
        for (int off = 0; off < good.length; off++) {
            for (int bit = 0; bit < 8; bit++) {
                final byte[] f = good.clone();
                f[off] ^= (byte) (1 << bit);
                flips++;
                try {
                    final TFixed.Value[] vals = { new TFixed.Value() };
                    final TableFixed.Report r = new TableFixed.Report();
                    final int n = TFixed.load(vals, 1, f, plan, remap, TFixed.image(), r);
                    if (r.refused && r.malformed) {
                        bad++;
                        System.out.println("P3-FAIL byte " + off + " bit " + bit + ": refused AND malformed, two answers at once");
                    } else if (r.refused) {
                        refused++;
                        names.merge(String.valueOf(r.reason), 1, Integer::sum);
                        if (n != -1) {
                            bad++;
                            System.out.println("P3-FAIL byte " + off + " bit " + bit + ": refused " + r.reason + " with n=" + n);
                        }
                    } else if (r.malformed) {
                        malformed++;
                    } else if (n >= 1) {
                        landed++;
                    } else {
                        bad++;
                        System.out.println("P3-FAIL byte " + off + " bit " + bit + ": a fourth answer, n=" + n + " reason=" + r.reason);
                    }
                } catch (Throwable e) {
                    bad++;
                    System.out.println("P3-FAIL byte " + off + " bit " + bit + ": the load threw " + e);
                }
            }
        }
        System.out.println("P3-COUNTS flips=" + flips + " refused=" + refused + " malformed=" + malformed
                + " landed=" + landed + " fourth=" + bad + " refusals=" + names);
    }
}
`

// t18P3: java/P3 "hostile bytes, sweep + sanitizer" — FIXED-FORM-ALGORITHM §7
// item 5 over SPEC-TABLES §3.4 (the record). The three answers must all occur,
// so the sweep is proven to have reached the refusal, framing and value paths.
func t18P3(t *testing.T) {
	t18RequireJava(t)
	classes := t18Compile(t, t18P3Schema, map[string]string{"Probe_p3": t18P3Probe})
	out := t07RunClass(t, classes, "probe.Probe_p3")
	var flips, refused, malformed, landed, fourth int
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "P3-FAIL") {
			t.Errorf("P3: %s", line)
		}
		if rest, ok := strings.CutPrefix(line, "P3-COUNTS "); ok {
			for tok := range strings.FieldsSeq(rest) {
				k, v, _ := strings.Cut(tok, "=")
				n, _ := strconv.Atoi(v)
				switch k {
				case "flips":
					flips = n
				case "refused":
					refused = n
				case "malformed":
					malformed = n
				case "landed":
					landed = n
				case "fourth":
					fourth = n
				}
			}
			t.Logf("P3: %s", line)
		}
	}
	if flips == 0 {
		t.Fatalf("P3: the sweep reported no flips:\n%s", out)
	}
	if fourth != 0 {
		t.Errorf("P3: %d flips answered a fourth way (a throw, or no answer)", fourth)
	}
	if refused == 0 || malformed == 0 || landed == 0 {
		t.Errorf("P3: sweep saw refused=%d malformed=%d landed=%d, want all three answers and never a fourth", refused, malformed, landed)
	}
	if refused+malformed+landed+fourth != flips {
		t.Errorf("P3: the answers (%d) do not add up to the flips (%d)", refused+malformed+landed+fourth, flips)
	}
}

const t18P4Schema = `package probe

fixed table A
{
    p int64
}

fixed table B
{
    x int32
    y int32
}
`

// t18P4WrongClass is the class whose reader the probe feeds B's record body
// through: A's, the wrong one. (The planted break of this card's report feeds B
// its own reader here, once, and is restored.)
const t18P4WrongClass = "A"

// t18P4WrongField is the field of that class's value that B's x would land in.
const t18P4WrongField = "p"

const t18P4Probe = `package probe;

// GENERATED BY internal/codegen/javatable/fixedroadmap_t18_test.go (java/P4).
// B's record body, run through ONE reader's plan by the emitted reader's own
// loop (TableFixed.run) and scatter: first B's own (the control, it must give B
// back), then the wrong one (it must not).
public final class Probe_p4 {
    public static void main(String[] args) {
        final BFixed.Value b = new BFixed.Value();
        b.x = 111;
        b.y = 222;
        final byte[] file = new byte[BFixed.measure(1)];
        if (BFixed.save(new BFixed.Value[] { b }, 1, file) != file.length) {
            System.out.println("P4-FAIL save did not fill the measure");
            System.exit(1);
        }
        final int bodyAt = BFixed.headerBytes + 8;

        // the control: B's own plan, B's own scatter
        final BFixed.Value own = new BFixed.Value();
        final TableFixed.Report ownReport = new TableFixed.Report();
        final byte[] ownImage = BFixed.image();
        TableFixed.run(BFixed.identityPlan(), BFixed.identityPlan().length, new short[1 << 14],
                file, bodyAt, BFixed.bodyBytes, ownImage, ownReport);
        BFixed.scatter(ownImage, 0, own, ownReport);
        if (ownReport.refused || own.x != b.x || own.y != b.y) {
            System.out.println("P4-FAIL the control: B's own plan did not give B back: x=" + own.x + " y=" + own.y);
            System.exit(1);
        }
        System.out.println("P4-CONTROL ok: B's own plan reproduces B (x=" + own.x + " y=" + own.y + ")");

        // the wrong plan
        final @WRONG@Fixed.Value got = new @WRONG@Fixed.Value();
        final TableFixed.Report r = new TableFixed.Report();
        final byte[] image = @WRONG@Fixed.image();
        TableFixed.run(@WRONG@Fixed.identityPlan(), @WRONG@Fixed.identityPlan().length, new short[1 << 14],
                file, bodyAt, BFixed.bodyBytes, image, r);
        @WRONG@Fixed.scatter(image, 0, got, r);
        if (r.refused) {
            System.out.println("P4-REFUSED " + r.reason);
        } else if (got.@FIELD@ == b.x) {
            System.out.println("P4-REPRODUCED the wrong plan gave B's x back: " + got.@FIELD@);
        } else {
            System.out.println("P4-DIFFERS the wrong plan read " + got.@FIELD@ + ", B's x is " + b.x);
        }
    }
}
`

// t18P4: java/P4 "wrong plan goes red" — FIXED-FORM-ALGORITHM §7 item 4 ("the
// wrong plan must go red") over SPEC-TABLES §3.4. A's identity plan run over B's
// record body (two int32 against one int64) splices the two fields into one
// value, so it must not reproduce B's record; the run is the emitted reader's
// own loop fed the wrong plan, tools/sabotage is not used.
func t18P4(t *testing.T) {
	t18RequireJava(t)
	probe := strings.NewReplacer("@WRONG@", t18P4WrongClass, "@FIELD@", t18P4WrongField).Replace(t18P4Probe)
	classes := t18Compile(t, t18P4Schema, map[string]string{"Probe_p4": probe})
	out := t07RunClass(t, classes, "probe.Probe_p4")
	switch {
	case strings.Contains(out, "P4-FAIL"):
		t.Fatalf("P4: the probe's control failed:\n%s", out)
	case !strings.Contains(out, "P4-CONTROL ok"):
		t.Fatalf("P4: the probe's control did not run:\n%s", out)
	case strings.Contains(out, "P4-REPRODUCED"):
		t.Errorf("P4: the wrong plan reproduced B's record: a plan nobody wrote this record under must go red (§7 item 4):\n%s", out)
	case strings.Contains(out, "P4-DIFFERS"), strings.Contains(out, "P4-REFUSED"):
		t.Logf("P4: %s", strings.TrimSpace(out))
	default:
		t.Fatalf("P4: the probe answered none of the three ways:\n%s", out)
	}
}
