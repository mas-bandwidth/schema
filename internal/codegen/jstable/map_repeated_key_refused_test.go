package jstable

import (
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const jsMapRepeatedKeySchema = "package probe\ntable Root { entries map[uint32]int32 }\n"

// jsMapRepeatedKeyCases is one clean map entry and the three repeated-key
// orderings. A body that carries the key's id more than once is damage
// (docs/SPEC-TABLES.md §2.8), whether the repeat comes after a widened key, an
// incompatible kind, or an exact key.
func jsMapRepeatedKeyCases(t *testing.T, u *ir.Unit) []struct {
	Name string `json:"name"`
	Wire string `json:"wire"`
} {
	t.Helper()
	f := u.Tables["Root"].Fields[0]
	wire := func(keys []byte) string {
		entry := append(append([]byte{}, keys...), 0)
		payload := append([]byte{13, 1, byte(len(entry))}, entry...)
		w := append([]byte{1, 1, 14, byte(len(payload))}, payload...)
		w = append(w, 0)
		w = binary.LittleEndian.AppendUint64(w, ir.TableFieldWireId(f))
		w = binary.LittleEndian.AppendUint64(w, ir.TableFieldWireId(f.MapEntry.Fields[0]))
		w = binary.LittleEndian.AppendUint64(w, 2)
		return hex.EncodeToString(w)
	}
	return []struct {
		Name string `json:"name"`
		Wire string `json:"wire"`
	}{
		{"clean", wire([]byte{2, 8, 7, 0, 0, 0})},
		{"bad_then_widened", wire([]byte{2, 9, 0, 0, 0, 0, 0, 0, 0, 0, 2, 6, 7})},
		{"widened_then_bad", wire([]byte{2, 6, 7, 2, 9, 0, 0, 0, 0, 0, 0, 0, 0})},
		{"bad_then_exact", wire([]byte{2, 9, 0, 0, 0, 0, 0, 0, 0, 0, 2, 8, 7, 0, 0, 0})},
	}
}

const jsMapRepeatedKeyProbe = `import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
const m = await import(pathToFileURL(process.argv[3]));
const rows = JSON.parse(readFileSync(process.argv[2], "utf8"));
for (const c of rows) {
  const wire = Uint8Array.from(Buffer.from(c.wire, "hex"));
  const value = m.RootLoad(wire, new m.TableVariableReport());
  const store = new m.TableVariableRetain(new Uint8Array(1 << 20), new Array((1 << 20) / 8));
  const retained = m.RootLoadRetain(wire, store, new m.TableVariableReport());
  console.log(c.name + ".load=" + (value === null ? "refused" : "read"));
  console.log(c.name + ".retain=" + (retained === null ? "refused" : "read"));
}
`

const jsMapRepeatedKeyWant = "clean.load=read\nclean.retain=read\n" +
	"bad_then_widened.load=refused\nbad_then_widened.retain=refused\n" +
	"widened_then_bad.load=refused\nwidened_then_bad.retain=refused\n" +
	"bad_then_exact.load=refused\nbad_then_exact.retain=refused"

func jsMapRepeatedKeyRun(t *testing.T, mutate func(files map[string][]byte, home string)) string {
	t.Helper()
	u := unitFrom(t, jsMapRepeatedKeySchema)
	files, err := generateVariableFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	home := runtimeHome(u) + "Variable.js"
	if mutate != nil {
		mutate(files, home)
	}
	return strings.TrimSpace(runJSConstructProbe(t, files, home, jsMapRepeatedKeyProbe, jsMapRepeatedKeyCases(t, u)))
}

// TestJavaScriptMapRepeatedKeyRefused: a map body that carries its key id more
// than once is damage, and the generated read refuses the whole value, plain
// and retaining alike (docs/SPEC-TABLES.md §2.8, §4). The clean entry reads on
// both paths.
func TestJavaScriptMapRepeatedKeyRefused(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node")
	if got := jsMapRepeatedKeyRun(t, nil); got != jsMapRepeatedKeyWant {
		t.Fatalf("repeated map key:\n%s\nwant\n%s", got, jsMapRepeatedKeyWant)
	}
}

// TestJavaScriptMapRepeatedKeyRefusedNegativeControl reverts the generated
// text to the tolerant last-wins read — a repeated field id lands rather than
// damaging, and a key kind mismatch no longer moves the strict mark — and
// requires a repeated row to read again.
func TestJavaScriptMapRepeatedKeyRefusedNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node")
	got := jsMapRepeatedKeyRun(t, func(files map[string][]byte, home string) {
		src := string(files[home])
		for _, patch := range []struct{ from, to string }{
			{
				"else if (rode.includes(ref)) { report.damaged = true; return false; }",
				"else if (false && rode.includes(ref)) { report.damaged = true; return false; }",
			},
			{
				"return report.mismatch + report.oor + report.repeated + report.sharedBlob;",
				"return report.oor + report.repeated + report.sharedBlob;",
			},
		} {
			if n := strings.Count(src, patch.from); n != 1 {
				t.Fatalf("the sabotage anchor %q occurs %d times in %s: the runtime moved", patch.from, n, home)
			}
			src = strings.Replace(src, patch.from, patch.to, 1)
		}
		files[home] = []byte(src)
	})
	if got == jsMapRepeatedKeyWant {
		t.Fatalf("the planted defect left every repeated row refusing:\n%s", got)
	}
	if !strings.Contains(got, "bad_then_widened.load=read") {
		t.Fatalf("the planted defect left bad_then_widened refusing; the control is blind:\n%s", got)
	}
}
