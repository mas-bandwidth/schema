package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestJavaFixedRootTextForm gives the Java leg a text form for a FIXED ROOT
// (docs/SPEC-TABLES.md §3.4, §16): it reads the root's form 3 file through its
// <Root>Fixed read, carries the value into the variable form's value (the
// emitter's <Root>Variable.fromFixed), writes the §16 text, and requires it to
// equal the committed JSON; then it reads that JSON back, carries the value
// into the fixed form's value (the conformance driver's VariableCarry), and
// requires the save to equal the file. The two carries are the emitter's and
// the driver's own (internal/codegen/javatable/fixedtext.go,
// test/conformance/java/src/Driver.java), compiled here beside the unit, so
// the test reads and writes exactly the value the driver does. The three rows
// hold the fixed form's shapes: root_full's counted arrays and union arms,
// keyed_config's keyed arrays and optional, m1_open's union root.
func TestJavaFixedRootTextForm(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	vcarry := variableCarrySource(t)
	m := readCookManifest(t)
	repo := filepath.Join("..", "..", "..")
	units := []struct {
		schema, pkg, root string
		rows              []string
	}{
		{"../../../tables/examples", "tabledemo", "RootConfig", []string{"root_full"}},
		{"../../../tables/examples", "tabledemo", "KeyedConfig", []string{"keyed_config"}},
		{"../../../test/tables/M1.schema", "tblm1", "Msg", []string{"m1_open"}},
	}
	for _, u := range units {
		t.Run(u.pkg+"_"+u.root, func(t *testing.T) {
			t.Parallel()
			javac, javaBin := javaTools(t)
			class := "TextFixed" + u.root
			src := strings.NewReplacer("TEXTCLASS", class, "PKG", u.pkg, "ROOT", u.root).Replace(textFixedDriver) + vcarry
			dir := compileCookUnit(t, javac, cookUnitFiles(t, u.schema), u.pkg, map[string]string{class: src})
			for _, row := range u.rows {
				inst := m.instance[row]
				if inst[1] != u.root {
					t.Fatalf("%s: MANIFEST.txt names root %s, the test %s", row, inst[1], u.root)
				}
				wire := filepath.Join(repo, inst[2])
				if b, err := os.ReadFile(wire); err != nil {
					t.Fatal(err)
				} else if len(b) == 0 || b[0] != 3 {
					t.Fatalf("%s: the instance is no form 3 file, so it is no fixed root's", row)
				}
				cmd := exec.Command(javaBin, "-ea", "-cp", dir, class,
					wire, filepath.Join(repo, "testdata", "conformance", "tables", "json", row+".json"))
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("java text %s: %v\n%s", row, err, out)
				}
				if !strings.Contains(string(out), "text round trip") {
					t.Fatalf("java text %s: the driver did not reach the round trip\n%s", row, out)
				}
			}
		})
	}
}

// variableCarrySource is the driver's VariableCarry class, lifted out of
// Driver.java as a top-level class of the default package.
func variableCarrySource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "conformance", "java", "src", "Driver.java"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	const open, end = "    static final class VariableCarry {", "    } // VariableCarry"
	at := strings.Index(src, open)
	stop := strings.Index(src, end)
	if at < 0 || stop < at {
		t.Fatal("Driver.java holds no VariableCarry class: the conformance driver no longer carries a variable value into the fixed one's")
	}
	return "\nfinal class VariableCarry {" + src[at+len(open):stop] + "}\n"
}

const textFixedDriver = `import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;

public final class TEXTCLASS {
    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Path.of(args[0]));
        byte[] wantText = Files.readAllBytes(Path.of(args[1]));
        PKG.ROOTFixed.Value[] values = { new PKG.ROOTFixed.Value() };
        PKG.TableFixed.Report report = new PKG.TableFixed.Report();
        int read = PKG.ROOTFixed.load(values, 1, wire, PKG.TableFixed.plan(1 << 14), new short[1 << 14],
                PKG.ROOTFixed.image(), report);
        if (read != 1 || report.refused || report.damaged) {
            throw new AssertionError("fixed read: " + report.reason);
        }
        PKG.ROOTVariable.Value value = PKG.ROOTVariable.fromFixed(values[0]);
        byte[] text = PKG.ROOTVariable.toJson(value);
        if (text == null) throw new AssertionError("toJson refused the value");
        if (!Arrays.equals(text, wantText)) {
            int at = Arrays.mismatch(text, wantText);
            throw new AssertionError("toJson text differs at byte " + at);
        }
        PKG.TableVariable.Report jreport = new PKG.TableVariable.Report();
        PKG.ROOTVariable.Value back = PKG.ROOTVariable.fromJson(text, jreport);
        if (back == null || jreport.refused || jreport.damaged) {
            throw new AssertionError("fromJson refused the text");
        }
        PKG.ROOTFixed.Value[] fixedBack = { new PKG.ROOTFixed.Value() };
        VariableCarry.carry(back, fixedBack[0]);
        int size = PKG.ROOTFixed.measure(1);
        byte[] out = new byte[size];
        int written = PKG.ROOTFixed.save(fixedBack, 1, out);
        if (written != size) throw new AssertionError("save refused the value");
        if (!Arrays.equals(out, wire)) {
            int at = Arrays.mismatch(out, wire);
            throw new AssertionError("save wire differs at byte " + at);
        }
        System.out.println("text round trip");
    }
}
`
