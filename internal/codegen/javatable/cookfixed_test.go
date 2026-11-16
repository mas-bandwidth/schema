package javatable

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestJavaCookWriteFixedRoots cooks FIXED ROOTS (docs/SPEC-TABLES.md §3.4,
// §7.6) through the generated Java writer and holds both byte orders to the
// committed cook-write fixtures. The value is the one the root's form 3 file
// gives through its <Root>Fixed read, carried into the cook's value by the
// conformance driver's own FixedCarry class (test/conformance/java/src/
// Driver.java), compiled here beside the unit, so the test cooks exactly what
// the driver cooks. The three rows hold the cook's two dead-bytes rules
// (§7.2; docs/rulings.json fixed-dead-bytes-are-zero): root_full's counted
// arrays are short of their bound, and a slot past the count is zero, not the
// element's declared defaults; keyed_config holds an absent optional, whose
// payload is zero whatever its default; m1_open is a union root.
func TestJavaCookWriteFixedRoots(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	carry := fixedCarrySource(t)
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
			class := "CookFixed" + u.root
			src := strings.NewReplacer("COOKCLASS", class, "PKG", u.pkg, "ROOT", u.root).Replace(cookFixedDriver) + carry
			dir := compileCookUnit(t, javac, cookUnitFiles(t, u.schema), u.pkg, map[string]string{class: src})
			for _, row := range u.rows {
				inst, cook := m.instance[row], m.cook[row]
				if inst[1] != u.root {
					t.Fatalf("%s: MANIFEST.txt names root %s, the test %s", row, inst[1], u.root)
				}
				wire, err := os.ReadFile(filepath.Join(repo, inst[2]))
				if err != nil {
					t.Fatal(err)
				}
				if len(wire) == 0 || wire[0] != 3 {
					t.Fatalf("%s: the instance is no form 3 file, so it is no fixed root's", row)
				}
				cmd := exec.Command(javaBin, "-ea", "-cp", dir, class,
					filepath.Join(repo, inst[2]), filepath.Join(repo, cook[0]), filepath.Join(repo, cook[1]))
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("java cook %s: %v\n%s", row, err, out)
				}
				if !strings.Contains(string(out), "cooked both orders") {
					t.Fatalf("java cook %s: the driver did not reach both orders\n%s", row, out)
				}
			}
		})
	}
}

// fixedCarrySource is the driver's FixedCarry class, lifted out of
// Driver.java as a top-level class of the default package.
func fixedCarrySource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "conformance", "java", "src", "Driver.java"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	const open, end = "    static final class FixedCarry {", "    } // FixedCarry"
	at := strings.Index(src, open)
	stop := strings.Index(src, end)
	if at < 0 || stop < at {
		t.Fatal("Driver.java holds no FixedCarry class: the conformance driver no longer carries a fixed root's value into the cook's")
	}
	return "\nfinal class FixedCarry {" + src[at+len(open):stop] + "}\n"
}

const cookFixedDriver = `import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;

public final class COOKCLASS {
    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Path.of(args[0]));
        PKG.ROOTFixed.Value[] values = { new PKG.ROOTFixed.Value() };
        PKG.TableFixed.Report report = new PKG.TableFixed.Report();
        int read = PKG.ROOTFixed.load(values, 1, wire, PKG.TableFixed.plan(1 << 14), new short[1 << 14],
                PKG.ROOTFixed.image(), report);
        if (read != 1 || report.refused || report.damaged) throw new AssertionError("fixed read: " + report.reason);
        PKG.ROOTVariable.Value value = new PKG.ROOTVariable.Value();
        FixedCarry.carry(values[0], value);
        long n = PKG.ROOTCook.cookMeasure(value);
        if (n < 1) throw new AssertionError("measure " + n);
        for (int order = 1; order <= 2; order++) {
            byte[] out = new byte[(int) n];
            Arrays.fill(out, (byte) 0x5a);
            if (!PKG.ROOTCook.cookFrom(value, out, order)) throw new AssertionError("cook order " + order);
            byte[] want = Files.readAllBytes(Path.of(args[order]));
            if (!Arrays.equals(out, want)) {
                int at = Arrays.mismatch(out, want);
                throw new AssertionError("cook bytes order " + order + " first mismatch " + at +
                        " got " + (at < out.length ? (out[at] & 255) : -1) +
                        " want " + (at < want.length ? (want[at] & 255) : -1));
            }
        }
        System.out.println("cooked both orders");
    }
}
`
