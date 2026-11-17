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

// jsMapRepeatedKeyCases is one clean map entry, the three repeated-key
// orderings, and a key field repeated under its own declared kind. A body that
// carries the key's id more than once is damage (docs/SPEC-TABLES.md §2.8),
// whether the repeat comes after a widened key, an incompatible kind, or an
// exact key, and the exact repeat refuses on the repeat alone.
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
		{"exact_then_exact", wire([]byte{2, 8, 7, 0, 0, 0, 2, 8, 7, 0, 0, 0})},
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
	"bad_then_exact.load=refused\nbad_then_exact.retain=refused\n" +
	"exact_then_exact.load=refused\nexact_then_exact.retain=refused"

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
// damaging — and requires the right-kind repeated row to read again.
func TestJavaScriptMapRepeatedKeyRefusedNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "node")
	got := jsMapRepeatedKeyRun(t, func(files map[string][]byte, home string) {
		src := string(files[home])
		const from = "else if (rode.includes(ref)) { report.damaged = true; return false; }"
		const to = "else if (false && rode.includes(ref)) { report.damaged = true; return false; }"
		if n := strings.Count(src, from); n != 1 {
			t.Fatalf("the sabotage anchor %q occurs %d times in %s: the runtime moved", from, n, home)
		}
		files[home] = []byte(strings.Replace(src, from, to, 1))
	})
	if got == jsMapRepeatedKeyWant {
		t.Fatalf("the planted defect left every repeated row refusing:\n%s", got)
	}
	if !strings.Contains(got, "exact_then_exact.load=read\nexact_then_exact.retain=read") {
		t.Fatalf("the planted defect left the right-kind repeated row refusing; the control is blind:\n%s", got)
	}
}
