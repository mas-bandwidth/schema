package javatable

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestCookWriteUnionsJavaReversed holds the four things the Java cook writer
// gained when it began to carry unions (docs/SPEC-TABLES.md §7.2) to the
// committed `schema cook` bytes, each with its REVERSED WITNESS: the old
// behaviour planted back into the generated Java must turn the row red.
//
//   - a UNION is its tag and then ONLY the set arm's bytes; a writer that
//     stores the tag and skips the arm (message_open) is wrong.
//   - an OPTIONAL scalar's slot is its value piece; a store at the field's
//     whole size runs over the presence bool written before it (v1_seams).
//   - a COUNTED array's slots past the live count hold the element record's
//     declared defaults; a writer that leaves them zero is wrong (root_full).
//   - a 128-bit slot is its low half first in a little-endian cook and its
//     high half first in a big-endian one; a writer with the halves the other
//     way round is wrong (scalars_full).
//
// The intact unit passing is cook_<pkg> in TestBlockBuildAndCookWriteJava;
// here each plant must fail, and must have patched exactly one line.
func TestCookWriteUnionsJavaReversed(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	plants := []struct {
		name, schema, pkg, root, row, file, old, new string
	}{
		{"union_arm_skipped", "../../../tables/messages", "messagedemo", "ToolMessage", "message_open",
			"TableCookWrite.java",
			"store(p, out, order, at, source, a);",
			"/* planted: the set arm is not stored */"},
		{"optional_slot_overrun", "../../../test/tables/V1.schema", "tblv1", "Cfg", "v1_seams",
			"TableCookWrite.java",
			"put(out, order, at, bits(source, f.storage, f.elemSize), f.elemSize);",
			"put(out, order, at, bits(source, f.storage, f.elemSize), f.size);"},
		{"wide_halves_swapped", "../../../tables/scalars", "scalardemo", "SimState", "scalars_full",
			"TableCookWrite.java",
			"put(out, order, at, order == BIG ? hi : lo, 8);",
			"put(out, order, at, order == BIG ? lo : hi, 8);"},
		{"slot_defaults_zero", "../../../tables/examples", "tabledemo", "RootConfig", "root_full",
			"TableCookWrite.java",
			"Object held = source != null ? source : (r.defaults != null ? r.defaults.get() : null);",
			"Object held = source;"},
	}
	m := readCookManifest(t)
	for _, pl := range plants {
		t.Run(pl.name, func(t *testing.T) {
			t.Parallel()
			javac, javaBin := javaTools(t)
			files := cookUnitFiles(t, pl.schema)
			src := string(files[pl.file])
			if strings.Count(src, pl.old) != 1 {
				t.Fatalf("%s: the plant's line occurs %d times in %s, want exactly 1 — the witness no longer aims at the writer",
					pl.name, strings.Count(src, pl.old), pl.file)
			}
			files[pl.file] = []byte(strings.Replace(src, pl.old, pl.new, 1))
			class := "CookGolden" + pl.root
			dir := compileCookUnit(t, javac, files, pl.pkg,
				map[string]string{class: cookDriverSource(class, pl.pkg, pl.root)})
			repo := filepath.Join("..", "..", "..")
			inst, cook := m.instance[pl.row], m.cook[pl.row]
			cmd := exec.Command(javaBin, "-ea", "-cp", dir, class,
				filepath.Join(repo, inst[2]), filepath.Join(repo, cook[0]), filepath.Join(repo, cook[1]))
			if out, err := cmd.CombinedOutput(); err == nil {
				t.Fatalf("%s: the planted writer still matched %s byte for byte, so the row does not witness it\n%s",
					pl.name, pl.row, out)
			}
		})
	}
}

// TestCookWriteUnionTagRefusedJava pins the plan's refusal of a union tag no
// arm carries (§7.6): the value cannot be held, so it measures -1 and names
// invalid_value, and the same driver accepts the value before it is damaged.
func TestCookWriteUnionTagRefusedJava(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	javac, javaBin := javaTools(t)
	dir := compileCookUnit(t, javac, cookUnitFiles(t, "../../../test/tables/V1.schema"), "tblv1",
		map[string]string{"CookUnionTagDriver": cookUnionTagDriver})
	m := readCookManifest(t)
	cmd := exec.Command(javaBin, "-ea", "-cp", dir, "CookUnionTagDriver",
		filepath.Join("..", "..", "..", m.instance["v1_cfg"][2]))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("java union tag refusal: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "refused invalid_value tag 3") {
		t.Fatalf("the driver did not reach the tag check:\n%s", out)
	}
}

const cookUnionTagDriver = `import java.nio.file.Files;
import java.nio.file.Path;

public final class CookUnionTagDriver {
    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Path.of(args[0]));
        tblv1.TableVariable.Report report = new tblv1.TableVariable.Report();
        tblv1.CfgVariable.Value value = tblv1.CfgVariable.load(wire, report);
        if (value == null || report.refused || report.malformed) throw new AssertionError("wire load");
        // Effect declares two arms, so tags 0..2 are values; each one cooks
        for (int tag = 0; tag <= 2; tag++) {
            value.effect.tag = tag;
            if (tblv1.CfgCook.cookMeasure(value) < 0) throw new AssertionError("tag " + tag + " refused");
        }
        value.effect.tag = 3;
        tblv1.CfgCook.MeasureReason r = tblv1.CfgCook.cookMeasureReason(value);
        if (tblv1.CfgCook.cookMeasure(value) != -1 || r.bytes() != -1 || !r.reason().equals("invalid_value")) {
            throw new AssertionError("tag 3 measured " + r);
        }
        byte[] out = new byte[4096];
        if (tblv1.CfgCook.cookFrom(value, out, 1)) throw new AssertionError("tag 3 cooked");
        for (byte b : out) if (b != 0) throw new AssertionError("a refused cook wrote bytes");
        System.out.println("refused invalid_value tag 3");
    }
}
`
