package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

type javaVariableFixture struct {
	name, root, wire, json string
}

// Every selected manifest instance exercises wire and JSON in both directions
// against the committed reference answers, including non-default union arms
// and map values (SPEC-TABLES §2.3, §2.4, §2.6, §2.8, §15, §16).
func TestMapsAndUnionArmsOnTheTableWireJava(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	data, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	selected := map[string]bool{"mapdemo": true, "messagedemo": true, "scalars": true}
	units := map[string]string{}
	fixtures := map[string][]javaVariableFixture{}
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "unit" && selected[fields[1]] {
			units[fields[1]] = filepath.Join(repo, fields[2])
		}
		if len(fields) == 5 && fields[0] == "instance" && selected[fields[2]] {
			fixtures[fields[2]] = append(fixtures[fields[2]], javaVariableFixture{fields[1], fields[3], filepath.Join(repo, fields[4]), filepath.Join(repo, "testdata", "conformance", "tables", "json", fields[1]+".json")})
		}
	}
	for _, name := range []string{"mapdemo", "messagedemo", "scalars"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if units[name] == "" || len(fixtures[name]) == 0 {
				t.Fatalf("manifest has no %s unit/instances", name)
			}
			javaVariableFixtures(t, units[name], fixtures[name])
		})
	}
	t.Run("armdemo", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(repo, "tables", "arms")
		u := unitFromDir(t, dir)
		model := tabletext.NewModel(u)
		var rows []javaVariableFixture
		for _, item := range []struct{ name, root string }{{"holder", "Holder"}, {"hand", "Hand"}, {"chain", "Chain"}, {"gate", "Gate"}, {"gate_text", "Gate"}, {"ring", "Ring"}, {"rack", "Rack"}, {"tray", "Tray"}, {"nest", "Nest"}} {
			wire := filepath.Join(repo, "testdata", "wire", "tables", "arms_"+item.name+".bin")
			data, err := os.ReadFile(wire)
			if err != nil {
				t.Fatal(err)
			}
			value := model.New(u.Tables[item.root])
			report := new(tabletext.Report)
			if ok, err := tablewire.Decode(model, value, data, report); !ok || err != nil || report.Refused {
				t.Fatalf("Go reference %s: %v %+v", item.name, err, report)
			}
			text, err := model.Write(value)
			if err != nil {
				t.Fatal(err)
			}
			json := filepath.Join(t.TempDir(), item.name+".json")
			if err := os.WriteFile(json, append(text, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
			rows = append(rows, javaVariableFixture{item.name, item.root, wire, json})
		}
		javaVariableFixtures(t, dir, rows)
	})
}

// Compile each unit once, then apply its native typed codecs to every row.
func javaVariableFixtures(t *testing.T, schema string, fixtures []javaVariableFixture) {
	t.Helper()
	u := unitFromDir(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range fixtures {
		if files[row.root+"Variable.java"] == nil {
			t.Fatalf("%s: %sVariable.java is not emitted", row.name, row.root)
		}
	}
	packet, err := javacodegen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Int128.java", "UInt128.java"} {
		if data := packet[name]; data != nil {
			files[name] = data
		}
	}
	pkg := javaPackageOf(u)
	dir := t.TempDir()
	classes := filepath.Join(dir, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	// A root declared fixed is a FORM 3 file (docs/SPEC-TABLES.md §5): its
	// committed wire is read and re-written whole by <Root>Fixed, and the form
	// 1 reader refuses it. The form byte tells the rows apart.
	formThree := map[string]bool{}
	for _, row := range fixtures {
		data, err := os.ReadFile(row.wire)
		if err != nil {
			t.Fatal(err)
		}
		formThree[row.name] = len(data) > 0 && data[0] == 3
	}
	seenFixed := map[string]bool{}
	for _, row := range fixtures {
		if !formThree[row.name] || seenFixed[row.root] {
			continue
		}
		seenFixed[row.root] = true
		path := filepath.Join(dir, "FixedDriver_"+row.root+".java")
		if err := os.WriteFile(path, []byte(fixedRoundTripDriver(pkg, row.root)), 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	seen := map[string]bool{}
	for _, row := range fixtures {
		if formThree[row.name] || seen[row.root] {
			continue
		}
		seen[row.root] = true
		name := "MapsDriver_" + row.root
		path := filepath.Join(dir, name+".java")
		source := strings.ReplaceAll(variableRoundTripDriver(pkg, row.root), "Driver", name)
		// A clean read of the reference moves no counter: not damaged, not
		// refused, nothing unknown and nothing widened. The shared driver may
		// spell the damage flag either way while the leg is re-pinned.
		strict := "if (report.damaged || report.refused || report.unknown != 0 || report.widened != 0)"
		says := `fail("load reported damaged="+report.damaged+" refused="+report.refused+" reason="+report.reason+" unknown="+report.unknown+" widened="+report.widened+" value="+(v == null ? "null" : new String(` + pkg + `.` + row.root + `Variable.toJson(v),StandardCharsets.UTF_8)));`
		for _, guard := range []string{"if (report.malformed || report.refused)", "if (report.damaged || report.refused)"} {
			source = strings.Replace(source, guard, strict, 1)
		}
		for _, msg := range []string{`fail("load reported malformed/refused");`, `fail("load reported damaged/refused");`} {
			source = strings.Replace(source, msg, says, 1)
		}
		if !strings.Contains(source, strict) || !strings.Contains(source, "load reported damaged=") {
			t.Fatalf("the round-trip driver no longer carries the guard this test tightens")
		}
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	sort.Strings(sources)
	javac, java := javaTools(t)
	args := append([]string{"--release", "17", "-d", classes}, sources...)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	for _, row := range fixtures {
		class := "MapsDriver_" + row.root
		if formThree[row.name] {
			class = "FixedDriver_" + row.root
		}
		out, err := exec.Command(java, "-ea", "-cp", classes, class, row.wire, row.json).CombinedOutput()
		if err != nil || !strings.Contains(string(out), "ok") {
			t.Errorf("%s: %v\n%s", row.name, err, out)
		} else {
			t.Logf("%s: wire and JSON match the reference", row.name)
		}
	}
}

// fixedRoundTripDriver holds a fixed-declared root's committed FORM 3 file:
// the form 1 reader refuses it whole, <Root>Fixed.load reads it with no
// counter moved, <Root>Fixed.save writes it back byte for byte, and the
// committed text reads clean and writes back byte for byte.
func fixedRoundTripDriver(pkg, root string) string {
	return strings.NewReplacer("PKG", pkg, "ROOT", root).Replace(`import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.Arrays;

public final class FixedDriver_ROOT {
    private FixedDriver_ROOT() {}

    static void fail(String what) {
        System.out.println("FAILED: " + what);
        System.exit(1);
    }

    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        byte[] golden = Files.readAllBytes(Paths.get(args[1]));

        PKG.TableVariable.Report vr = new PKG.TableVariable.Report();
        if (PKG.ROOTVariable.load(wire, vr) != null || !vr.refused || vr.damaged) {
            fail("the form 1 reader kept a form 3 file: refused=" + vr.refused + " damaged=" + vr.damaged);
        }

        PKG.ROOTFixed.Value[] values = { new PKG.ROOTFixed.Value() };
        PKG.TableFixed.Report fr = new PKG.TableFixed.Report();
        int read = PKG.ROOTFixed.load(values, 1, wire, PKG.TableFixed.plan(1 << 14), new short[1 << 14], PKG.ROOTFixed.image(), fr);
        if (read != 1 || fr.refused || fr.damaged || fr.unknown != 0 || fr.widened != 0) {
            fail("fixed load: read=" + read + " refused=" + fr.refused + " damaged=" + fr.damaged + " reason=" + fr.reason + " unknown=" + fr.unknown + " widened=" + fr.widened);
        }
        byte[] saved = new byte[PKG.ROOTFixed.measure(1)];
        int wrote = PKG.ROOTFixed.save(values, 1, saved);
        if (wrote != saved.length || !Arrays.equals(saved, wire)) {
            fail("fixed re-save differs at byte " + Arrays.mismatch(saved, wire) + " (wire " + wire.length + " saved " + wrote + ")");
        }

        PKG.TableVariable.Report tr = new PKG.TableVariable.Report();
        PKG.ROOTVariable.Value v = PKG.ROOTVariable.fromJson(golden, tr);
        if (v == null || tr.refused || tr.damaged || tr.unknown != 0 || tr.widened != 0) {
            fail("text load: refused=" + tr.refused + " damaged=" + tr.damaged + " reason=" + tr.reason + " unknown=" + tr.unknown);
        }
        byte[] text = PKG.ROOTVariable.toJson(v);
        if (!Arrays.equals(golden, text)) {
            fail("text re-write differs at byte " + Arrays.mismatch(golden, text));
        }
        System.out.println("ok");
    }
}
`)
}

func TestJavaVariableMapValues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	schema := `package probe
table Item { count int32 }
table Root {
 names map[string(16)]Item
 ids map[uint64]*Item
 nested map[int16]map[uint8]int32
 tail int32
}
`
	if err := os.WriteFile(filepath.Join(dir, "Root.schema"), []byte(schema), 0644); err != nil {
		t.Fatal(err)
	}
	u := unitFromDir(t, dir)
	model := tabletext.NewModel(u)
	value := model.New(u.Tables["Root"])
	report := new(tabletext.Report)
	if !model.Read(value, []byte(`{"names":{"\ue000":{"count":2},"😀":{"count":3}},"ids":{"18446744073709551615":{"count":7},"0":{"count":8}},"nested":{"-3":{"2":11},"2":{"1":12}},"tail":19}`), report) {
		t.Fatalf("reference: %+v", report)
	}
	wire, err := tablewire.Encode(model, value)
	if err != nil {
		t.Fatal(err)
	}
	text, err := model.Write(value)
	if err != nil {
		t.Fatal(err)
	}
	wp, jp := filepath.Join(dir, "wire"), filepath.Join(dir, "json")
	if err := os.WriteFile(wp, wire, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jp, append(text, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
	javaVariableFixtures(t, dir, []javaVariableFixture{{"map-order-and-recursion", "Root", wp, jp}})
}

func TestJavaVariableMapCorpus(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	data, err := os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []javaVariableFixture
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 5 && f[0] == "instance" && f[2] == "mapdemo" && f[3] != "Depth" {
			rows = append(rows, javaVariableFixture{f[1], f[3], filepath.Join(repo, f[4]), filepath.Join(repo, "testdata", "conformance", "tables", "json", f[1]+".json")})
		}
	}
	if len(rows) == 0 {
		t.Fatal("no map fixtures")
	}
	javaVariableFixtures(t, filepath.Join(repo, "tables", "maps"), rows)
	t.Log("Depth remains outside this incremental map probe; the full native matrix still requires them")
}
