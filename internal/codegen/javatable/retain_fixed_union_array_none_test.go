package javatable

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// TestRetainFixedUnionArrayNoneElides holds the Java retaining save of a FIXED
// ARRAY OF UNIONS whose every element is None to the engine elision
// (docs/SPEC-TABLES.md §3, §6.6; docs/SPEC-CONFORMANCE.md §7,
// retain_fixed_union_array_none): the field is left out, exactly as the
// variable codec leaves it out.
//
// The wire is the mutant the scheduled full nightly found (run 45130315968,
// negative controls (java-wire-fuzz), seed 24845619678): a
// messagedemo.ToolEnvelope whose Transaction carries no pending field
// ([2]EditBody) and an unknown field the retaining load keeps. The retaining
// save tested the list for emptiness, and a fixed array is always its bound
// long, so it wrote pending as two Nones: 206 bytes where the engine writes
// 191. The plant puts that guard back and must turn the pair red.
func TestRetainFixedUnionArrayNoneElides(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	schema := filepath.Join("..", "..", "..", "tables", "messages", "Messages.schema")
	wire, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "tables", "retain_fixed_union_array_none.bin"))
	if err != nil {
		t.Fatal(err)
	}

	// the oracle: the engine retaining load and save (internal/tablewire)
	m := tabletext.NewModel(unitFromDir(t, schema))
	inst := m.New(m.Lookup("ToolEnvelope"))
	rt := &tablewire.Retain{Capacity: 1 << 20, IdCapacity: 1 << 17}
	var lr tabletext.Report
	ok, err := tablewire.DecodeRetain(m, inst, wire, rt, &lr)
	if !ok || err != nil || lr.Refused || lr.Retained == 0 {
		t.Fatalf("the engine retaining load: ok=%v err=%v report=%+v", ok, err, lr)
	}
	var sr tabletext.Report
	want, err := tablewire.EncodeRetain(m, inst, rt, &sr)
	if err != nil {
		t.Fatalf("the engine retaining save: %v", err)
	}
	if len(want) != 191 {
		t.Fatalf("the engine saves %d bytes, the pinned finding says 191: the wire no longer witnesses the elision", len(want))
	}

	const guard = "if (rides_pending) {"
	for _, plant := range []struct{ name, new string }{
		{"intact", ""},
		{"empty_list_guard", "if (!v.pending.isEmpty()) {"},
	} {
		t.Run(plant.name, func(t *testing.T) {
			t.Parallel()
			javac, javaBin := javaTools(t)
			files := cookUnitFiles(t, schema)
			if plant.new != "" {
				src := string(files["TransactionRetain.java"])
				if strings.Count(src, guard) != 1 {
					t.Fatalf("%s: the guard %q is not in TransactionRetain.java once, so the plant no longer aims at the save", plant.name, guard)
				}
				files["TransactionRetain.java"] = []byte(strings.Replace(src, guard, plant.new, 1))
			}
			dir := compileCookUnit(t, javac, files, "messagedemo", map[string]string{"RetainFixedUnionArrayDriver": retainFixedUnionArrayDriver})
			wirePath := filepath.Join(t.TempDir(), "wire.bin")
			if err := os.WriteFile(wirePath, wire, 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := testproc.Command(t, javaBin, "-ea", "-cp", dir, "RetainFixedUnionArrayDriver", wirePath).CombinedOutput()
			if err != nil {
				t.Fatalf("the Java retaining pair failed: %v\n%s", err, out)
			}
			got, err := hex.DecodeString(strings.TrimSpace(string(out)))
			if err != nil {
				t.Fatalf("the driver printed no hex: %v\n%s", err, out)
			}
			same := bytes.Equal(got, want)
			if plant.new == "" && !same {
				t.Fatalf("the Java retaining save writes %d bytes, the engine %d\njava   %x\nengine %x", len(got), len(want), got, want)
			}
			if plant.new != "" && same {
				t.Fatalf("%s: the planted save still matched the engine, so the wire does not witness it", plant.name)
			}
		})
	}
}

// retainFixedUnionArrayDriver loads its wire retaining, saves it retaining and
// prints the saved bytes as hex.
const retainFixedUnionArrayDriver = "import java.nio.file.Files;\n" +
	"import java.nio.file.Path;\n" +
	"public final class RetainFixedUnionArrayDriver {\n" +
	"    public static void main(String[] args) throws Exception {\n" +
	"        byte[] wire = Files.readAllBytes(Path.of(args[0]));\n" +
	"        messagedemo.TableRetain store = new messagedemo.TableRetain(1 << 20, 1 << 17);\n" +
	"        messagedemo.TableVariable.Report load = new messagedemo.TableVariable.Report();\n" +
	"        messagedemo.ToolEnvelopeVariable.Value v = messagedemo.ToolEnvelopeRetain.loadRetain(wire, store, load);\n" +
	"        if (v == null || load.damaged || load.refused) throw new AssertionError(\"retaining load refused\");\n" +
	"        messagedemo.TableVariable.Report save = new messagedemo.TableVariable.Report();\n" +
	"        long size = messagedemo.ToolEnvelopeRetain.measureRetain(v, store);\n" +
	"        byte[] got = messagedemo.ToolEnvelopeRetain.saveRetain(v, store, save);\n" +
	"        if (got == null || got.length != size) throw new AssertionError(\"save disagrees with its measure\");\n" +
	"        StringBuilder b = new StringBuilder();\n" +
	"        for (byte x : got) b.append(String.format(\"%02x\", x & 255));\n" +
	"        System.out.println(b);\n" +
	"    }\n" +
	"}\n"
