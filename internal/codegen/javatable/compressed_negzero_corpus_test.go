package javatable

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE COMPRESSED -0 WRITER CORPUS ON THE JAVA LEG (docs/SPEC-TABLES.md §3.3,
// docs/rulings.json fixed-compressed-negzero): a writer stores +0 for a
// compressed float's -0. The corpus is the Go reference engine's
// (testdata/conformance/feature-witnesses/compressed-negzero-go, pinned by
// internal/tablewire's TestCompressedNegZeroGoCorpus); this leg runs every row
// through the generated Java: load <base>.json with the Java text read, set
// <path> to -0 in the native value, save <form>, and owe <expect>.<form>.bin
// byte for byte. The base saved untouched owes <base>.<form>.bin: the leg
// writes the engine's bytes.
//
// Beside each row, every other Java writer the row's root has is held to the
// row's +0 twin (the same base saved untouched): the variable save and its
// retaining twin, the fixed save and the message form (a fixed table's, on
// the FixFixed value the variable value carries), the JSON writer and the
// cook writer. A compressed row's -0 output equals its twin's; the plain `p`
// rows are the control, whose -0 rides bit-exact, so their output differs.
//
// The driver runs twice, without -ea and with it: the normalisation is held
// in the build a shipping program runs, never by an assert. It runs the JDK,
// so it is a slow test; the conformance lane runs it
// (make/java-compressed-negzero.mk). TestJavaCompressedFloatStoresAreGuarded
// is its fast twin.

const javaNegZeroCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-negzero-go"

type javaNegZeroRow struct {
	name, form, root, base, path, expect string
}

func javaNegZeroRows(t *testing.T) []javaNegZeroRow {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(javaNegZeroCorpusDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []javaNegZeroRow
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("cases.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		rows = append(rows, javaNegZeroRow{f[0], f[1], f[2], f[3], f[4], f[5]})
	}
	if len(rows) != 15 {
		t.Fatalf("cases.tsv holds %d rows, want 15", len(rows))
	}
	return rows
}

// javaNegZeroWriters are the writers each root is held on.
var javaNegZeroWriters = map[string][]string{
	"Var": {"variable", "retain", "json", "cook"},
	"Fix": {"variable", "retain", "fixed", "message", "json", "cook"},
}

// javaNegZeroSet is the Java statement that sets the cell a corpus path names
// on the value `v` to `nz`: `field`, `field[index]`, `field[Variant]` for an
// enum-keyed slot (key k is slot k-1, the variant's place in the declaration;
// Level is the corpus's one key), or `field.arm` for a union's set arm (Pick
// is the corpus's one union).
func javaNegZeroSet(t *testing.T, u *ir.Unit, path string) string {
	t.Helper()
	if name, index, ok := strings.Cut(path, "["); ok {
		index = strings.TrimSuffix(index, "]")
		if index[0] < '0' || index[0] > '9' {
			slot := slices.Index(u.Enums["Level"].Variants, index)
			if slot < 0 {
				t.Fatalf("%s: Level has no variant %s", path, index)
			}
			index = strconv.Itoa(slot)
		}
		return "v." + name + ".set(" + index + ", nz);"
	}
	if name, arm, ok := strings.Cut(path, "."); ok {
		return "v." + name + ".tag = PickFixed." + arm + "; v." + name + "." + arm + " = nz;"
	}
	return "v." + path + " = nz;"
}

const javaNegZeroPrelude = `package nzgo;

import java.nio.charset.StandardCharsets;

public final class NegZeroDriver {
    static final float nz = Float.intBitsToFloat(0x80000000);

    static String hex(byte[] b) {
        if (b == null) { return "error"; }
        StringBuilder s = new StringBuilder();
        for (byte c : b) { s.append(String.format("%02x", c & 0xff)); }
        return s.toString();
    }

    static VarVariable.Value varBase(String json) {
        TableVariable.Report r = new TableVariable.Report();
        VarVariable.Value v = VarVariable.fromJson(json.getBytes(StandardCharsets.UTF_8), r);
        if (v == null || r.refused) { throw new IllegalStateException("the base does not load: " + r.reason); }
        return v;
    }

    static FixVariable.Value fixBase(String json) {
        TableVariable.Report r = new TableVariable.Report();
        FixVariable.Value v = FixVariable.fromJson(json.getBytes(StandardCharsets.UTF_8), r);
        if (v == null || r.refused) { throw new IllegalStateException("the base does not load: " + r.reason); }
        return v;
    }

    // the fixed record the variable value carries, field for field
    static FixFixed.Value toFixed(FixVariable.Value v) {
        FixFixed.Value f = new FixFixed.Value();
        f.a = v.a;
        f.z = v.z;
        for (int i = 0; i < v.xs.size(); i++) { f.xs[i] = v.xs.get(i); }
        f.ysCount = v.ys.size();
        for (int i = 0; i < v.ys.size(); i++) { f.ys[i] = v.ys.get(i); }
        for (int i = 0; i < v.ks.size(); i++) { f.ks[i] = v.ks.get(i); }
        f.pick.type = v.pick.tag;
        if (v.pick.tag == PickFixed.v) { f.pick.v = v.pick.v; }
        if (v.pick.tag == PickFixed.n) { f.pick.n = (byte) v.pick.n; }
        f.p = v.p;
        return f;
    }

    static byte[] varOut(String w, VarVariable.Value v, byte[] base) {
        switch (w) {
            case "variable": return VarVariable.save(v);
            case "retain": {
                TableRetain retain = new TableRetain(1 << 16, 1024);
                if (VarRetain.loadRetain(base, retain, new TableVariable.Report()) == null) { return null; }
                return VarRetain.saveRetain(v, retain, new TableVariable.Report());
            }
            case "json": return VarVariable.toJson(v);
            case "cook": {
                long n = VarCook.cookMeasure(v);
                if (n < 0) { return null; }
                byte[] b = new byte[(int) n];
                return VarCook.cookFrom(v, b, TableCookWrite.LITTLE) ? b : null;
            }
            default: return null;
        }
    }

    static byte[] fixOut(String w, FixVariable.Value v, byte[] base) {
        switch (w) {
            case "variable": return FixVariable.save(v);
            case "retain": {
                TableRetain retain = new TableRetain(1 << 16, 1024);
                if (FixRetain.loadRetain(base, retain, new TableVariable.Report()) == null) { return null; }
                return FixRetain.saveRetain(v, retain, new TableVariable.Report());
            }
            case "fixed": {
                FixFixed.Value[] rows = { toFixed(v) };
                byte[] b = new byte[FixFixed.measure(1)];
                int n = FixFixed.save(rows, 1, b);
                return n < 0 ? null : java.util.Arrays.copyOf(b, n);
            }
            case "message": {
                FixFixed.Value[] rows = { toFixed(v) };
                TableVocabulary.Report r = new TableVocabulary.Report();
                int n = FixMessage.measureMessages(rows, r);
                if (n < 0) { return null; }
                byte[] b = new byte[n];
                return FixMessage.saveMessages(rows, b, r) == n ? b : null;
            }
            case "json": return FixVariable.toJson(v);
            case "cook": {
                long n = FixCook.cookMeasure(v);
                if (n < 0) { return null; }
                byte[] b = new byte[(int) n];
                return FixCook.cookFrom(v, b, TableCookWrite.LITTLE) ? b : null;
            }
            default: return null;
        }
    }

    static String out(boolean var, String w, Object v, byte[] base) {
        try {
            return hex(var ? varOut(w, (VarVariable.Value) v, base) : fixOut(w, (FixVariable.Value) v, base));
        } catch (Throwable e) {
            return "error";
        }
    }

    public static void main(String[] args) {
`

// TestJavaCompressedNegZeroCorpus runs the corpus on the Java leg.
func TestJavaCompressedNegZeroCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the JDK (it compiles and runs the compressed -0 writer corpus)")
	javac, java := javaTools(t)
	src, err := os.ReadFile(filepath.Join(javaNegZeroCorpusDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(src))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(javaNegZeroCorpusDir, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}

	rows := javaNegZeroRows(t)
	var b strings.Builder
	b.WriteString(javaNegZeroPrelude)
	for _, r := range rows {
		writers := javaNegZeroWriters[r.root]
		if writers == nil {
			t.Fatalf("%s: root %s", r.name, r.root)
		}
		if r.form != "variable" && (r.form != "fixed" || r.root != "Fix") {
			t.Fatalf("%s: form %s at %s", r.name, r.form, r.root)
		}
		lower := strings.ToLower(r.root)
		json := strings.TrimSpace(string(read(r.base + ".json")))
		fmt.Fprintf(&b, "        { // %s\n", r.name)
		fmt.Fprintf(&b, "            %sVariable.Value base = %sBase(%q);\n", r.root, lower, json)
		fmt.Fprintf(&b, "            %sVariable.Value v = %sBase(%q);\n", r.root, lower, json)
		fmt.Fprintf(&b, "            byte[] saved = %sVariable.save(base);\n", r.root)
		fmt.Fprintf(&b, "            %s\n", javaNegZeroSet(t, u, r.path))
		fmt.Fprintf(&b, "            for (String w : new String[] {\"%s\"}) {\n", strings.Join(writers, `", "`))
		fmt.Fprintf(&b, "                System.out.println(\"%s \" + w + \" \" + out(%t, w, v, saved) + \" \" + out(%t, w, base, saved));\n", r.name, r.root == "Var", r.root == "Var")
		b.WriteString("            }\n        }\n")
	}
	b.WriteString("    }\n}\n")
	files["NegZeroDriver.java"] = []byte(b.String())

	dir := t.TempDir()
	var sources []string
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".java") {
			sources = append(sources, p)
		}
	}
	sort.Strings(sources)
	args := append([]string{"--release", "17", "-nowarn", "-d", dir}, sources...)
	if out, err := testproc.Command(t, javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}

	for _, mode := range []string{"-da", "-ea"} {
		out, err := testproc.Command(t, java, mode, "-cp", dir, u.Package+".NegZeroDriver").CombinedOutput()
		if err != nil {
			t.Fatalf("java %s: %v\n%s", mode, err, out)
		}
		type pair struct{ minus, plus string }
		got := map[string]map[string]pair{}
		for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
			f := strings.Fields(line)
			if len(f) != 4 {
				t.Fatalf("java %s: the driver printed %q, not a row, a writer and two outputs", mode, line)
			}
			if got[f[0]] == nil {
				got[f[0]] = map[string]pair{}
			}
			got[f[0]][f[1]] = pair{f[2], f[3]}
		}

		held, twins := 0, 0
		for _, r := range rows {
			ws := got[r.name]
			if len(ws) != len(javaNegZeroWriters[r.root]) {
				t.Errorf("java %s: %s: the driver answered %d writers, want %d", mode, r.name, len(ws), len(javaNegZeroWriters[r.root]))
				continue
			}
			plain := r.path == "p"
			// THE CORPUS ROW: the -0 save of <form> is <expect>.<form>.bin,
			// and the +0 base's is <base>.<form>.bin
			p := ws[r.form]
			want := hex.EncodeToString(read(r.expect + "." + r.form + ".bin"))
			wantBase := hex.EncodeToString(read(r.base + "." + r.form + ".bin"))
			switch {
			case p.plus != wantBase:
				t.Errorf("java %s: %s: the Java %s save of the base is not %s.%s.bin:\n have %s\n want %s", mode, r.name, r.form, r.base, r.form, p.plus, wantBase)
			case p.minus != want:
				t.Errorf("java %s: %s: the Java %s save of -0 at %s is not %s.%s.bin:\n have %s\n want %s", mode, r.name, r.form, r.path, r.expect, r.form, p.minus, want)
			default:
				held++
			}
			// THE TWINS: every writer the root has
			for _, w := range javaNegZeroWriters[r.root] {
				p := ws[w]
				if p.minus == "error" || p.plus == "error" {
					t.Errorf("java %s: %s/%s: a writer refused: -0 %s, +0 %s", mode, r.name, w, p.minus, p.plus)
					continue
				}
				switch {
				case plain && p.minus == p.plus:
					t.Errorf("java %s: %s/%s: a plain float's -0 did not ride: %s", mode, r.name, w, p.minus)
				case !plain && p.minus != p.plus:
					t.Errorf("java %s: %s/%s: a compressed float's -0 was not written as +0:\n -0 %s\n +0 %s", mode, r.name, w, p.minus, p.plus)
				default:
					twins++
				}
			}
		}
		t.Logf("java %s: %d of %d corpus rows held; %d writer twins held", mode, held, len(rows), twins)
	}
}
