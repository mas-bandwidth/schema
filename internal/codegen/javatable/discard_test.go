package javatable

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// discardUnitJava has a fixed root and a variable root, each carrying a
// nested body between two scalars, so damage inside the nested body leaves the
// walk able to read on past its L: the case where a reader that kept what it
// decoded would hand back a value. It is the C++ gate's unit
// (internal/codegen/cpptable/discard_test.go).
const discardUnitJava = `package discard

fixed table Inner
{
    n int32 = 0
}

fixed table Root
{
    a     int32 = 0
    inner Inner
    b     int32 = 0
}

table Graph
{
    a     int32 = 0
    inner Inner
    b     int32 = 0
    next  *Graph
}
`

// The discard every Java load entry point carries before it hands back a
// value (variableform.go, retain.go) is named by the comment above it. The
// negative control turns the condition under that comment into false in the
// generated code.
var (
	discardGuardLoad   = regexp.MustCompile(`(// DAMAGE DISCARDS THE WHOLE LOAD \(§4\)\s*\n\s*if \()report\.malformed\)`)
	discardGuardJSON   = regexp.MustCompile(`(// DAMAGE DISCARDS THE WHOLE TEXT READ \(§4, §16\.2\)\s*\n\s*if \()report\.malformed\)`)
	discardGuardRetain = regexp.MustCompile(`(// DAMAGE DISCARDS THE WHOLE RETAINING LOAD \(§4, §6\.6\)\s*\n\s*if \()report\.malformed\)`)
)

// discardWireJava encodes the text as the root and then damages the nested
// body: Inner's one field, kind 4 holding 3, gets the kind 37 no reader can
// skip. The engine is asked first, so the vector is damaged by the rule and
// not by assumption.
func discardWireJava(t *testing.T, u *ir.Unit, root, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(root))
	var r tabletext.Report
	if !m.Read(inst, []byte(text), &r) {
		t.Fatalf("%s: the text did not place", root)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	at := bytes.Index(wire, []byte{byte(ir.TableKindI32), 3, 0, 0, 0, 0})
	if at < 0 {
		t.Fatalf("%s: Inner's body is not where this test expects it", root)
	}
	wire[at] = 37
	var report tabletext.Report
	if ok, err := tablewire.Decode(m, m.New(m.Lookup(root)), wire, &report); ok || err != nil || !report.Malformed {
		t.Fatalf("%s: the engine does not read the vector as damage: ok=%v err=%v %+v", root, ok, err, report)
	}
	return wire
}

// discardDriverJava loads every vector it is given through each public load
// entry point the root carries — load, loadRetain (with a store that already
// holds bytes from an earlier load), and fromJson for a damaged text — and
// prints one FAIL line per entry point that hands back a usable value or a
// report that does not say malformed. It is reflective so one driver serves
// every unit.
const discardDriverJava = `import java.lang.reflect.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;

public final class DiscardDriver {
    static int bad = 0;
    static void fail(String m) { System.out.println("FAIL " + m); bad++; }

    static Object report(String pkg) throws Exception {
        return Class.forName(pkg + ".TableVariable$Report").getConstructor().newInstance();
    }
    static boolean flag(Object r, String name) throws Exception {
        return r.getClass().getField(name).getBoolean(r);
    }

    public static void main(String[] a) throws Exception {
        String pkg = a[0];
        Class<?> rep = Class.forName(pkg + ".TableVariable$Report");
        int n = 0;
        for (int i = 1; i + 2 < a.length; i += 3) {
            String what = a[i], root = a[i + 1], arg = a[i + 2];
            Class<?> codec = Class.forName(pkg + "." + root + "Variable");
            if (what.equals("json")) {
                Object r = report(pkg);
                Object v = codec.getMethod("fromJson", byte[].class, rep).invoke(null, arg.getBytes(StandardCharsets.UTF_8), r);
                if (v != null) fail(root + " fromJson: handed back a value on damaged text");
                if (!flag(r, "malformed")) fail(root + " fromJson: the report does not say malformed");
                n++;
                continue;
            }
            byte[] data = Files.readAllBytes(Paths.get(arg));
            Object r = report(pkg);
            Object v = codec.getMethod("load", byte[].class, rep).invoke(null, data, r);
            if (v != null) fail(root + " load " + arg + ": handed back a value on damage");
            if (!flag(r, "malformed")) fail(root + " load " + arg + ": the report does not say malformed");
            n++;
            Class<?> retainCodec;
            try { retainCodec = Class.forName(pkg + "." + root + "Retain"); } catch (ClassNotFoundException e) { continue; }
            Class<?> storeType = Class.forName(pkg + ".TableRetain");
            Object store = storeType.getConstructor(int.class, int.class).newInstance(1 << 16, 256);
            storeType.getField("used").setInt(store, 9);
            storeType.getField("idUsed").setInt(store, 1);
            Object rr = report(pkg);
            Object rv = retainCodec.getMethod("loadRetain", byte[].class, storeType, rep).invoke(null, data, store, rr);
            int used = storeType.getField("used").getInt(store);
            int idUsed = storeType.getField("idUsed").getInt(store);
            if (rv != null) fail(root + " loadRetain " + arg + ": handed back a root on damage");
            if (!flag(rr, "malformed")) fail(root + " loadRetain " + arg + ": the report does not say malformed");
            if (used != 0 || idUsed != 0) fail(root + " loadRetain " + arg + ": the retention store is not empty: used=" + used + " idUsed=" + idUsed);
            n++;
        }
        System.out.println(bad == 0 ? "OK " + n : "BAD " + bad + " of " + n);
        System.exit(bad == 0 ? 0 : 1);
    }
}
`

type discardCase struct {
	what, root, arg string
}

type discardUnitRun struct {
	u     *ir.Unit
	cases []discardCase
}

// discardFramingRows reads the framing surface's rows whose answer is
// `malformed` (testdata/conformance/tables/MANIFEST.txt, docs/LAYERS/1-framing.md)
// and the schema path of every unit they name.
func discardFramingRows(t *testing.T, repo string) (map[string][]discardCase, map[string]string) {
	t.Helper()
	f, err := os.Open(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows := map[string][]discardCase{}
	paths := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		w := strings.Fields(sc.Text())
		if len(w) >= 3 && w[0] == "unit" {
			paths[w[1]] = w[2]
		}
		if len(w) >= 6 && w[0] == "framing" && w[5] == "malformed" {
			rows[w[2]] = append(rows[w[2]], discardCase{"load", w[3], filepath.Join(repo, w[4])})
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows, paths
}

// runDiscardJava generates each unit (with an optional in-memory edit of the
// emitted files), compiles it beside the reflective driver, and runs every
// case. It returns the driver's output and an error when any entry point
// handed back a value.
func runDiscardJava(t *testing.T, edit func(map[string][]byte) int) (string, error) {
	t.Helper()
	javac, java := javaToolsRequired(t)
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	td := t.TempDir()

	var runs []discardUnitRun
	u := unitFrom(t, discardUnitJava)
	fixedVec := filepath.Join(td, "discard_fixed.bin")
	variableVec := filepath.Join(td, "discard_variable.bin")
	if err := os.WriteFile(fixedVec, discardWireJava(t, u, "Root", `{"a":5,"inner":{"n":3},"b":9}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(variableVec, discardWireJava(t, u, "Graph", `{"a":5,"inner":{"n":3},"b":9}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runs = append(runs, discardUnitRun{u, []discardCase{
		{"load", "Root", fixedVec},
		{"load", "Graph", variableVec},
		{"json", "Graph", `{"a":5,"inner":{"n":3},"b":`},
		{"json", "Root", `{"a":5,"inner":{"n":3},"b":9`},
	}})

	rows, paths := discardFramingRows(t, repo)
	var names []string
	for name := range rows {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		t.Fatal("no framing row answers malformed: the manifest moved")
	}
	for _, name := range names {
		p := filepath.Join(repo, paths[name])
		st, err := os.Stat(p)
		if err != nil {
			t.Fatalf("unit %s: %v", name, err)
		}
		var fu *ir.Unit
		if st.IsDir() {
			fu = unitFromDir(t, p)
		} else {
			src, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			fu = unitFrom(t, string(src))
		}
		runs = append(runs, discardUnitRun{fu, rows[name]})
	}

	edited := 0
	var out strings.Builder
	var failed error
	for i, run := range runs {
		files, err := Generate(run.u)
		if err != nil {
			t.Fatal(err)
		}
		packet, err := javacodegen.Generate(run.u)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"Int128.java", "UInt128.java"} {
			if data := packet[name]; data != nil {
				files[name] = data
			}
		}
		if edit != nil {
			edited += edit(files)
		}
		dir := filepath.Join(td, fmt.Sprintf("u%d", i))
		classes := filepath.Join(dir, "classes")
		if err := os.MkdirAll(classes, 0o755); err != nil {
			t.Fatal(err)
		}
		var sources []string
		for name, data := range files {
			if !strings.HasSuffix(name, ".java") {
				continue
			}
			p := filepath.Join(dir, filepath.Base(name))
			if err := os.WriteFile(p, data, 0o644); err != nil {
				t.Fatal(err)
			}
			sources = append(sources, p)
		}
		driver := filepath.Join(dir, "DiscardDriver.java")
		if err := os.WriteFile(driver, []byte(discardDriverJava), 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, driver)
		args := append([]string{"--release", "17", "-nowarn", "-d", classes}, sources...)
		if b, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
			t.Fatalf("javac %s: %v\n%s", run.u.Package, err, b)
		}
		jargs := []string{"-ea", "-cp", classes, "DiscardDriver", javaPackageOf(run.u)}
		for _, c := range run.cases {
			jargs = append(jargs, c.what, c.root, c.arg)
		}
		b, err := exec.Command(java, jargs...).CombinedOutput()
		fmt.Fprintf(&out, "== %s\n%s", run.u.Package, b)
		if err != nil && failed == nil {
			failed = fmt.Errorf("%s: %v", run.u.Package, err)
		}
	}
	if edit != nil && edited == 0 {
		t.Fatal("NEGATIVE CONTROL: the sabotage patched nothing in the generated code")
	}
	return out.String(), failed
}

// TestDamageDiscardsTheWholeLoadJava is the Java leg's gate for DAMAGE
// DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4): damage inside a nested
// body with a whole field before and after it, and every framing row whose
// answer is malformed (docs/LAYERS/1-framing.md), makes load hand back null
// with the report malformed, loadRetain hand back null with the retention
// store emptied, and fromJson on damaged text hand back null. Red if any entry
// point hands back what it decoded. The message batch's own discard is held by
// TestMessageFormBatchJava (damage_in_body_two, bytes_after_pad: zero bodies).
func TestDamageDiscardsTheWholeLoadJava(t *testing.T) {
	t.Parallel()
	out, err := runDiscardJava(t, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, "FAIL") {
		t.Fatalf("a damaged load handed back a value:\n%s", out)
	}
	t.Logf("%s", out)
}

// TestDamageDiscardsTheWholeLoadJavaNegativeControl removes the discard
// guard from the generated code, one entry point at a time, and requires the
// gate's driver to go red on exactly that entry point.
func TestDamageDiscardsTheWholeLoadJavaNegativeControl(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		guard *regexp.Regexp
		file  string
		want  string
	}{
		{"load", discardGuardLoad, "Variable.java", "Root load"},
		{"fromJson", discardGuardJSON, "Variable.java", "fromJson: handed back a value"},
		{"loadRetain", discardGuardRetain, "Retain.java", "loadRetain"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, err := runDiscardJava(t, func(files map[string][]byte) int {
				n := 0
				for name, data := range files {
					if !strings.HasSuffix(name, c.file) || !c.guard.Match(data) {
						continue
					}
					files[name] = c.guard.ReplaceAll(data, []byte("${1}false /* NEGATIVE CONTROL: the discard is gone */)"))
					n++
				}
				return n
			})
			if err == nil || !strings.Contains(out, "FAIL") {
				t.Fatalf("NEGATIVE CONTROL FAILED: the %s discard is gone and the gate stayed green\n%s", c.name, out)
			}
			if !strings.Contains(out, c.want) {
				t.Fatalf("NEGATIVE CONTROL FAILED: the gate went red, but not on %q\n%s", c.want, out)
			}
			t.Logf("negative control: with the %s discard removed, the whole-load gate is RED", c.name)
		})
	}
}
