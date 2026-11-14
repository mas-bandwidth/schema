package javatable

// A text write NAMES every value it writes (docs/SPEC-TABLES.md §16.3): an
// enum value no variant names and a flags value with a set bit no variant
// names have no text spelling, so the writer's debug contract refuses them
// under -ea exactly where it refuses a non-finite float. The Go writer
// (internal/tabletext) is the reference, and a lawful value — the last
// variant, every named bit set — writes the same text and reads back equal.
//
// The probe builds one instance per site: the enum and flags as scalar
// fields, as array elements, and inside a nested table. Under -ea toJson must
// throw AssertionError at each; without -ea the writer trusts, so the same
// calls return a text (the debug idiom, not a release-build check).

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const namedValuesSchema = `package probe

enum Color { Red, Green, Blue }
flags Perm { Read, Write, Exec }

table Nested {
    color Color
    perm Perm
    colors []Color
    perms []Perm
}

table Root {
    color Color
    perm Perm
    colors []Color
    perms []Perm
    nested Nested
}
`

const namedValuesJavaProbe = `import java.nio.file.Files;
import java.nio.file.Paths;

public final class NamedValuesProbe {
    private NamedValuesProbe() {}

    static final String[] SITES = {
        "enum field", "flags field",
        "enum array element", "flags array element",
        "nested enum field", "nested flags field",
        "nested enum array element", "nested flags array element"
    };

    static void fail(String what) {
        System.out.println("PROBE FAILED: " + what);
        System.exit(1);
    }

    static probe.RootVariable.Value invalid(int site) {
        probe.RootVariable.Value v = new probe.RootVariable.Value();
        switch (site) {
            case 0: v.color = 9; break;
            case 1: v.perm = 8L; break;
            case 2: v.colors.add(9); break;
            case 3: v.perms.add(8L); break;
            case 4: v.nested.color = 9; break;
            case 5: v.nested.perm = 8L; break;
            case 6: v.nested.colors.add(9); break;
            case 7: v.nested.perms.add(8L); break;
            default: fail("unknown site " + site);
        }
        return v;
    }

    static void expectAssertion(String what, Runnable r) {
        try {
            r.run();
        } catch (AssertionError e) {
            return;
        }
        fail("toJson did not assert on " + what);
    }

    static probe.RootVariable.Value lawful() {
        probe.RootVariable.Value v = new probe.RootVariable.Value();
        v.color = 3;
        v.perm = 7L;
        v.colors.add(1);
        v.colors.add(3);
        v.perms.add(1L);
        v.perms.add(7L);
        v.nested.color = 3;
        v.nested.perm = 7L;
        v.nested.colors.add(3);
        v.nested.perms.add(7L);
        return v;
    }

    static void checkLawful(probe.RootVariable.Value v) {
        if (v.color != 3 || v.perm != 7L) fail("scalar lawful twin changed");
        if (!v.colors.equals(java.util.List.of(1, 3))) fail("enum array changed: " + v.colors);
        if (!v.perms.equals(java.util.List.of(1L, 7L))) fail("flags array changed: " + v.perms);
        if (v.nested.color != 3 || v.nested.perm != 7L) fail("nested scalar lawful twin changed");
        if (!v.nested.colors.equals(java.util.List.of(3))) fail("nested enum array changed");
        if (!v.nested.perms.equals(java.util.List.of(7L))) fail("nested flags array changed");
    }

    public static void main(String[] args) throws Exception {
        String mode = args[0];
        if (mode.equals("assert")) {
            for (int i = 0; i < SITES.length; i++) {
                final int site = i;
                expectAssertion(SITES[i], () -> probe.RootVariable.toJson(invalid(site)));
            }
            System.out.println("probe: every unnamed value asserted under -ea");
            return;
        }
        if (mode.equals("trust")) {
            for (int i = 0; i < SITES.length; i++) {
                if (probe.RootVariable.toJson(invalid(i)) == null) {
                    fail("toJson refused without -ea: " + SITES[i]);
                }
            }
            System.out.println("probe: unnamed values write without -ea");
            return;
        }
        if (mode.equals("lawful")) {
            probe.RootVariable.Value v = lawful();
            byte[] text = probe.RootVariable.toJson(v);
            if (text == null) fail("toJson refused the lawful twin");
            Files.write(Paths.get(args[1]), text);
            probe.TableVariable.Report r = new probe.TableVariable.Report();
            probe.RootVariable.Value back = probe.RootVariable.fromJson(text, r);
            if (back == null || r.damaged || r.refused) fail("fromJson of the lawful text reported loss");
            checkLawful(back);
            System.out.println("probe: lawful twin wrote and read back equal");
            return;
        }
        fail("unknown mode " + mode);
    }
}
`

func namedValuesField(t *testing.T, inst *tabletext.Instance, key string) *tabletext.Field {
	t.Helper()
	fv, ok := inst.FieldByKey(key)
	if !ok {
		t.Fatalf("%s has no field %q", inst.Def.Name, key)
	}
	return fv
}

func namedValuesLawful(t *testing.T, m *tabletext.Model, u *ir.Unit) *tabletext.Instance {
	t.Helper()
	root := m.New(u.Tables["Root"])
	color := namedValuesField(t, root, "color")
	color.Cell.U, color.Cell.I = 3, 3
	namedValuesField(t, root, "perm").Cell.U = 7
	colors := namedValuesField(t, root, "colors")
	colors.Elems = []tabletext.Cell{{U: 1, I: 1}, {U: 3, I: 3}}
	colors.Count = len(colors.Elems)
	perms := namedValuesField(t, root, "perms")
	perms.Elems = []tabletext.Cell{{U: 1, I: 1}, {U: 7, I: 7}}
	perms.Count = len(perms.Elems)

	nested := namedValuesField(t, root, "nested").Cell.Tab
	if nested == nil {
		t.Fatal("nested table has no instance")
	}
	ncolor := namedValuesField(t, nested, "color")
	ncolor.Cell.U, ncolor.Cell.I = 3, 3
	namedValuesField(t, nested, "perm").Cell.U = 7
	ncolors := namedValuesField(t, nested, "colors")
	ncolors.Elems = []tabletext.Cell{{U: 3, I: 3}}
	ncolors.Count = len(ncolors.Elems)
	nperms := namedValuesField(t, nested, "perms")
	nperms.Elems = []tabletext.Cell{{U: 7, I: 7}}
	nperms.Count = len(nperms.Elems)
	return root
}

// TestJavaToJsonAssertsNamedValues holds the text writer to naming every value
// it writes: an unnamed enum value or flags bit is a debug assert under -ea,
// and a lawful twin writes the Go writer's text and reads back equal.
func TestJavaToJsonAssertsNamedValues(t *testing.T) {
	t.Parallel()
	javac, javaBin := javaToolsRequired(t)

	u := unitFrom(t, namedValuesSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	rootDir := t.TempDir()
	srcdir := filepath.Join(rootDir, "src", "probe")
	classes := filepath.Join(rootDir, "classes")
	for _, d := range []string{srcdir, classes} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	var srcs []string
	for name, data := range files {
		if !strings.HasSuffix(name, ".java") {
			continue
		}
		p := filepath.Join(srcdir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		srcs = append(srcs, p)
	}
	probe := filepath.Join(rootDir, "NamedValuesProbe.java")
	if err := os.WriteFile(probe, []byte(namedValuesJavaProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--release", "17", "-d", classes}, srcs...)
	args = append(args, probe)
	if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}

	run := func(ea bool, mode string, extra ...string) ([]byte, error) {
		cmdArgs := []string{"-cp", classes}
		if ea {
			cmdArgs = append(cmdArgs, "-ea")
		}
		cmdArgs = append(cmdArgs, "NamedValuesProbe", mode)
		cmdArgs = append(cmdArgs, extra...)
		return exec.Command(javaBin, cmdArgs...).CombinedOutput()
	}

	if out, err := run(true, "assert"); err != nil {
		t.Fatalf("toJson did not assert every unnamed value under -ea: %v\n%s", err, out)
	}
	if out, err := run(false, "trust"); err != nil {
		t.Fatalf("toJson did not trust unnamed values without -ea: %v\n%s", err, out)
	}

	lawful := filepath.Join(rootDir, "lawful.json")
	if out, err := run(true, "lawful", lawful); err != nil {
		t.Fatalf("lawful twin round trip: %v\n%s", err, out)
	}
	got, err := os.ReadFile(lawful)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	want, err := m.Write(namedValuesLawful(t, m, u))
	if err != nil {
		t.Fatalf("Go writer refused the lawful twin: %v", err)
	}
	if !bytes.Equal(got, append(want, '\n')) {
		t.Fatalf("Java text differs from the Go writer\n got: %q\nwant: %q", got, append(want, '\n'))
	}
}
