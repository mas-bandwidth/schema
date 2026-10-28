package javatable

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestRetainSaveJava holds the Java retaining pair to the reference's pinned
// RT1 saves of RT2's wire (docs/SPEC-TABLES.md §6.6; MANIFEST.txt `retain`
// rows retain_round_trip and retain_ids_short): the saved bytes, the load's
// three counters and the retain_lost the save adds. Each plant puts one old
// behaviour back into the generated Java and must turn the pair red — the
// REVERSED WITNESSES of the three fixes this test landed with:
//
//   - a counted array of tables was not written at all by the retaining save;
//   - a pointer field (kind 17) was not written by the retaining save;
//   - a keyed slot whose element body was empty rode as an entry, where the
//     plain write elides a default slot;
//   - a retained record that failed on the id list counted its loss twice.
func TestRetainSaveJava(t *testing.T) {
	slowtest.Gate(t, "the JDK (it compiles and runs the generated Java)")
	t.Parallel()
	plants := []struct {
		name, file, old, new string
	}{
		{"", "", "", ""},
		{"counted_table_array_dropped", "NodeRetain.java", "if (!v.items.isEmpty()) {", "if (false) {"},
		{"pointer_dropped", "NodeRetain.java", "if (v.head != null) {", "if (false) {"},
		{"empty_keyed_slot_kept", "NodeVariable.java", "if (w.off > body + 2) {", "if (w.off > body + 1) {"},
		{"loss_counted_twice", "NodeRetain.java", "TableRetain.emit(rec, retain, null,", "TableRetain.emit(rec, retain, report,"},
	}
	for _, pl := range plants {
		name := pl.name
		if name == "" {
			name = "intact"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			javac, javaBin := javaTools(t)
			files := cookUnitFiles(t, "../../../test/tables/RT1.schema")
			if pl.file != "" {
				src := string(files[pl.file])
				if !strings.Contains(src, pl.old) {
					t.Fatalf("%s: the plant's line is not in %s — the witness no longer aims at the save", pl.name, pl.file)
				}
				files[pl.file] = []byte(strings.ReplaceAll(src, pl.old, pl.new))
			}
			dir := compileCookUnit(t, javac, files, "tblrt1", map[string]string{"RetainSaveDriver": retainSaveDriver})
			wires := filepath.Join("..", "..", "..", "testdata", "wire", "tables")
			out, err := exec.Command(javaBin, "-ea", "-cp", dir, "RetainSaveDriver",
				filepath.Join(wires, "retain_rt2.bin"),
				filepath.Join(wires, "retain_rt1_save.bin"),
				filepath.Join(wires, "retain_rt1_save_id_short.bin")).CombinedOutput()
			if pl.file == "" {
				if err != nil {
					t.Fatalf("the retaining pair does not reproduce the reference: %v\n%s", err, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("%s: the planted save still matched the reference, so the rows do not witness it\n%s", pl.name, out)
			}
		})
	}
}

const retainSaveDriver = `import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Arrays;

public final class RetainSaveDriver {
    static void pair(byte[] wire, int ids, byte[] want, String counters) {
        tblrt1.TableRetain store = new tblrt1.TableRetain(1 << 20, ids);
        tblrt1.TableVariable.Report load = new tblrt1.TableVariable.Report();
        tblrt1.NodeVariable.Value v = tblrt1.NodeRetain.loadRetain(wire, store, load);
        if (v == null || load.malformed || load.refused) throw new AssertionError("retaining load refused");
        tblrt1.TableVariable.Report save = new tblrt1.TableVariable.Report();
        long size = tblrt1.NodeRetain.measureRetain(v, store);
        byte[] got = tblrt1.NodeRetain.saveRetain(v, store, save);
        if (got == null || got.length != size) throw new AssertionError("save disagrees with its measure");
        String said = load.retained + "," + load.retainLost + "," + load.unknown + " " + save.retainLost;
        if (!said.equals(counters)) throw new AssertionError("ids " + ids + ": counters " + said + ", want " + counters);
        if (!Arrays.equals(got, want)) throw new AssertionError("ids " + ids + ": " + got.length + " bytes, want " + want.length);
    }

    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Path.of(args[0]));
        pair(wire, 1 << 17, Files.readAllBytes(Path.of(args[1])), "9,2,11 0");
        pair(wire, 2, Files.readAllBytes(Path.of(args[2])), "9,2,11 1");
    }
}
`
