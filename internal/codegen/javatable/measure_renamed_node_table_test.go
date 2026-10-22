package javatable

import (
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	javacodegen "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestJavaFixMeasureRenamedNodeTable holds the generated loadMeasure to
// tablewire.FileRegionMeasure on the wire fuzzer's cause C1 (docs/SPEC-TABLES.md
// §3.1, §6.5): the reference pass renamed a field id so the reader meets an
// unknown field, and the oracle still sizes every map and list the body
// carries (at the running, aligned extent), every pointer-reachable node table
// record's own layout and containers, and the +16 directory. Each mutant is
// the harness's minimal one for its root at seed 24845619678.
//
// The guarded mutant was cut against tblg1 before test/tables/G1.schema grew
// its `optional ?Wrapped` half (schema#526), so the case carries that
// declaration itself: the pin is the wire and the schema it was cut from.
const measureRenamedNodeG1 = `package tblg1

fixed table Node
{
    value int32
}

table Guarded
{
    at_rest bool
    always  *Node
    if at_rest
    {
        resting *Node
    }
}
`

func TestJavaFixMeasureRenamedNodeTable(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	for _, tc := range []struct {
		name, dir, file, root string
		src                   string // the declaration itself, when the case pins one
		want                  int64
		wire                  string
	}{
		// tblg1.Guarded, pass reference #1: a node table record of a
		// pointer-reachable TABLE commands its own rounded record region
		{"guarded", "test/tables", "G1.schema", "Guarded", measureRenamedNodeG1, 64,
			"01041102020c0a010307040401000000000044e5f7fadfa6c4f7ffffffffffffffff8db6f6d2c61cbd66ea0ce83094fde47c0400000000000000"},
		// listdemo.Save, pass reference #1: the lists of tables inside each
		// list element's body are walked, not only the root's own list
		{"lists-save", "tables/lists", "", "Save", "", 128,
			"01090e380d030d020a0000803f0308030000000013020a00000040040a000000400308040000000013020a00004040040a0000804003080500000000050e051103020200060e0e04030a000000140000001e000000070c0a0108070908070000000000094b4d57aa3347d2071702864cf563af3a706e3e9343e59d541502864cf463af3154af1d19735012b20f40270b6b9801ffffffffffffffff5f8258ac3615785e8476bc2eef83761e0900000000000000"},
		// mapdemo.Depth, pass reference #1: a map's entries are walked and
		// the maps nested in them are counted, aligned on the running extent
		{"maps-depth", "tables/maps", "", "Depth", "", 184,
			"010b0d15020e110d010e030603040d07050421000000000000060e2e0d0215020e110d010e030601040d0705040b00000000000015020e110d010e030602040d070504160000000000000710190d010815020e110d010e030605040d07050437000000000000090f0a0d15020e110d010e030607040d0705044d0000000000000b040900000000af5cca2119aa081a424f4f300d39841cec105b36194ac93dea0ce83094fde47c74a279448ee2e5b102fca1cea259641f5df15095f21f55708be145c2cb04da4fb5cc700519c056e731633ed695bbc2d5e9ea716f0f0182bf0b00000000000000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var u *ir.Unit
			if tc.file != "" {
				dir := t.TempDir()
				schema := []byte(tc.src)
				if tc.src == "" {
					var err error
					if schema, err = os.ReadFile(filepath.Join(repo, tc.dir, tc.file)); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(filepath.Join(dir, tc.file), schema, 0o644); err != nil {
					t.Fatal(err)
				}
				u = unitFromDir(t, dir)
			} else {
				u = unitFromDir(t, filepath.Join(repo, tc.dir))
			}
			root := u.Tables[tc.root]
			if root == nil {
				t.Fatalf("%s has no %s root", tc.dir, tc.root)
			}
			wire, err := hex.DecodeString(tc.wire)
			if err != nil {
				t.Fatal(err)
			}
			oracle, whole, refused := tablewire.FileRegionMeasure(u, root, wire)
			if !whole || refused || oracle != tc.want {
				t.Fatalf("oracle = %d whole=%t refused=%t, want %d whole and not refused", oracle, whole, refused, tc.want)
			}
			got := javaLoadMeasure(t, u, tc.root, wire)
			if got != oracle {
				t.Fatalf("Java %sVariable.loadMeasure = %d, oracle FileRegionMeasure = %d", tc.root, got, oracle)
			}
		})
	}
}

// javaLoadMeasure generates and compiles the unit's Java, then answers
// <root>Variable.loadMeasure over wire.
func javaLoadMeasure(t *testing.T, u *ir.Unit, rootName string, wire []byte) int64 {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
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
	build := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(build, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	driver := `import java.nio.file.Files;
import java.nio.file.Paths;

public final class MeasureDriver {
    private MeasureDriver() {}
    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        System.out.println(` + pkg + `.` + rootName + `Variable.loadMeasure(wire));
    }
}
`
	driverPath := filepath.Join(build, "MeasureDriver.java")
	if err := os.WriteFile(driverPath, []byte(driver), 0o644); err != nil {
		t.Fatal(err)
	}
	classes := filepath.Join(build, "classes")
	if err := os.MkdirAll(classes, 0o755); err != nil {
		t.Fatal(err)
	}
	var sources []string
	for name := range files {
		sources = append(sources, filepath.Join(build, name))
	}
	sources = append(sources, driverPath)
	javac, javaBin := javaTools(t)
	if out, err := exec.Command(javac, append([]string{"--release", "17", "-d", classes}, sources...)...).CombinedOutput(); err != nil {
		t.Fatalf("javac: %v\n%s", err, out)
	}
	wirePath := filepath.Join(build, "mutant.bin")
	if err := os.WriteFile(wirePath, wire, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(javaBin, "-ea", "-cp", classes, "MeasureDriver", wirePath).CombinedOutput()
	if err != nil {
		t.Fatalf("java: %v\n%s", err, out)
	}
	var got int64
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if n, err := strconv.ParseInt(strings.TrimSpace(line), 10, 64); err == nil {
			got = n
		}
	}
	return got
}
