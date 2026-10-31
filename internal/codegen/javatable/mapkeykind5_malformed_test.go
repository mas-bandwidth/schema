package javatable

import (
	"encoding/hex"
	"fmt"
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

// A kind pass rewrites a map key's wire kind (Slots: int32 4 -> 5, Runs:
// uint16 7 -> 15; plus a bad-kind key followed by a widened key, and a
// bad-kind string key followed by a good string key). The oracle's key scan
// (§2.8) SKIPS the mis-kind key, reads a later correct key, and the desynced
// body tail counts malformed; it never reports a kind mismatch for the skipped
// occurrence, a WIDENED key leaves the mis-kind mark as it was, and a correct
// key clears it. On a mutant the oracle reads clean the Java answer is pinned
// to the oracle's (saved bytes and every report counter); on one it reads as
// damaged, DAMAGE DISCARDS THE WHOLE LOAD (docs/SPEC-TABLES.md §4) and the leg
// owes the refusal alone.
func TestJavaFixMapKeyKind5Malformed(t *testing.T) {
	t.Parallel()
	repo := filepath.Join("..", "..", "..")
	dir := filepath.Join(repo, "tables", "maps")
	u := unitFromDir(t, dir)
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
	var sources []string
	for name, data := range files {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}
	type mutant struct{ root, hex string }
	mutants := []mutant{
		{"Slots", "01010e340d02180205feffffff03100e0402040402000000050465000000001802040600000003100e0402040401000000050464000000000604050000000061b072303048657fec105b36194ac93dea0ce83094fde47c8be145c2cb04da4f8794b696a762b5a0e9ea716f0f0182bf0600000000000000"},
		{"Runs", "01010e300d021a020f0400030e120d0207040428000000000704042900000000001202070900030e0a0d010704045a000000000005040300000000166856b28afc7d43ec105b36194ac93dea0ce83094fde47c74a279448ee2e5b1e9ea716f0f0182bf0500000000000000"},
		// Slots: a bad-kind key (8) then a WIDENED key (2) as the last
		// occurrence. The widened key must leave the mis-kind mark set, so the
		// oracle resets the map (kind_mismatch 1, widened 1, saved 24); the leg
		// used to clear the mark on the widened key and keep the map.
		{"Slots", "01010e310d02150208010000000202010310080401010400000000001802040600000003100e0402040401000000050464000000000604050000000061b072303048657fec105b36194ac93dea0ce83094fde47c8be145c2cb04da4f8794b696a762b5a0e9ea716f0f0182bf0600000000000000"},
		// Docs: a bad-kind key (int32) then a good string key inside one entry.
		// The good key must clear the mark, so the map decodes whole and the
		// entry's own body counts one kind_mismatch for the bad occurrence; the
		// leg used not to clear the string-key mark and reset the map.
		{"Docs", "01010e210d0212020400000000020c05616c706861031102000b020c046265746103110300040400000000050c1a02060a66697273742070616765060b7365636f6e642070616765009f76486aa7da01abec105b36194ac93dea0ce83094fde47ce9ea716f0f0182bfffffffffffffffff58fcaffad8e04b700600000000000000"},
		// The r2 read's crafted wires (receipts java-fix-map-key-kind5-malformed.r2):
		// Slots, a bad-kind key (8) then a key WIDENED from int8 (2) for the
		// int32 key. The oracle keeps the mis-kind mark and empties the map
		// (24 bytes); the leg used to clear it on the widened key and keep the
		// map (119 bytes).
		{"Slots", "01010e370d021b02080100000002020103100e0402040402000000050465000000001802040600000003100e0402040401000000050464000000000604050000000061b072303048657fec105b36194ac93dea0ce83094fde47c8be145c2cb04da4f8794b696a762b5a0e9ea716f0f0182bf0600000000000000"},
		// Slots, a bad-kind key (8) then an exact int32 key: the exact key
		// clears the mark and the map decodes whole.
		{"Slots", "01010e3a0d021e02080100000002040100000003100e0402040402000000050465000000001802040600000003100e0402040401000000050464000000000604050000000061b072303048657fec105b36194ac93dea0ce83094fde47c8be145c2cb04da4f8794b696a762b5a0e9ea716f0f0182bf0600000000000000"},
		// Docs, a bad-kind key (int32 1) then a good string key: the string-key
		// read clears the mark by the same exact-kind rule.
		{"Docs", "01010e210d0212020401000000020c05616c706861031102000b020c046265746103110300040406000000050c1a02060a66697273742070616765060b7365636f6e642070616765009f76486aa7da01abec105b36194ac93dea0ce83094fde47ce9ea716f0f0182bfffffffffffffffff58fcaffad8e04b700600000000000000"},
	}
	for i, m := range mutants {
		root, hexs, name := m.root, m.hex, fmt.Sprintf("m%d_%s", i, m.root)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			classes := filepath.Join(t.TempDir(), "classes")
			if err := os.MkdirAll(classes, 0o755); err != nil {
				t.Fatal(err)
			}
			mutant, err := hex.DecodeString(hexs)
			if err != nil {
				t.Fatal(err)
			}
			wp := filepath.Join(t.TempDir(), "mutant.bin")
			if err := os.WriteFile(wp, mutant, 0o644); err != nil {
				t.Fatal(err)
			}
			driver := fmt.Sprintf(`
import java.nio.file.Files;
import java.nio.file.Paths;
public final class Driver {
    public static void main(String[] args) throws Exception {
        byte[] wire = Files.readAllBytes(Paths.get(args[0]));
        %s.TableVariable.Report report = new %s.TableVariable.Report();
        %s.%sVariable.Value v = %s.%sVariable.load(wire, report);
        System.out.println("R " + report.unknown + "," + report.kindMismatch + "," + report.widened + "," + report.clamped + "," + report.duplicate + "," + report.malformed + "," + report.refused);
        if (v == null) { System.out.println("S null"); return; }
        byte[] saved = %s.%sVariable.save(v);
        System.out.println("S " + java.util.HexFormat.of().formatHex(saved));
    }
}
`, pkg, pkg, pkg, root, pkg, root, pkg, root)
			dpath := filepath.Join(t.TempDir(), "Driver.java")
			if err := os.WriteFile(dpath, []byte(driver), 0o644); err != nil {
				t.Fatal(err)
			}
			src := append(append([]string{}, sources...), dpath)
			sort.Strings(src)
			javac, java := javaTools(t)
			args := append([]string{"--release", "17", "-d", classes}, src...)
			if out, err := exec.Command(javac, args...).CombinedOutput(); err != nil {
				t.Fatalf("javac: %v\n%s", err, out)
			}
			out, err := exec.Command(java, "-ea", "-cp", classes, "Driver", wp).CombinedOutput()
			if err != nil {
				t.Fatalf("java: %v\n%s", err, out)
			}
			var javaReport, javaSaved string
			for line := range strings.SplitSeq(string(out), "\n") {
				if after, ok := strings.CutPrefix(line, "R "); ok {
					javaReport = after
				}
				if after, ok := strings.CutPrefix(line, "S "); ok {
					javaSaved = after
				}
			}
			if javaReport == "" || javaSaved == "" {
				t.Fatalf("no java answer in:\n%s", out)
			}
			model := tabletext.NewModel(u)
			want := new(tabletext.Report)
			value := model.New(u.Tables[root])
			if _, err := tablewire.Decode(model, value, mutant, want); err != nil {
				t.Fatalf("oracle decode: err=%v report=%+v", err, want)
			}
			gotFields := strings.Split(javaReport, ",")
			if len(gotFields) != 7 {
				t.Fatalf("java report shape: %q", javaReport)
			}
			if want.Malformed {
				if gotFields[5] != "true" {
					t.Errorf("Java %s did not refuse a damaged load: %q", root, javaReport)
				}
				return
			}
			wantFields := []string{
				fmt.Sprintf("%d", want.Unknown),
				fmt.Sprintf("%d", want.KindMismatch),
				fmt.Sprintf("%d", want.Widened),
				fmt.Sprintf("%d", want.Clamped),
				fmt.Sprintf("%d", want.Duplicate),
				fmt.Sprintf("%t", want.Malformed),
				fmt.Sprintf("%t", want.Refused),
			}
			for i := range wantFields {
				if gotFields[i] != wantFields[i] {
					t.Errorf("report field %d: java=%s oracle=%s (java report %q, oracle %+v)", i, gotFields[i], wantFields[i], javaReport, want)
				}
			}
			encoded, err := tablewire.Encode(model, value)
			if err != nil {
				t.Fatal(err)
			}
			if javaSaved != hex.EncodeToString(encoded) {
				t.Errorf("saved bytes: java=%s oracle=%s", javaSaved, hex.EncodeToString(encoded))
			}
		})
	}
}
