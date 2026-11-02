package jstable

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The count is encoded independently of the generated writer. Unknown fields
// surround a nested map so refusal must stop the read and discard retention.
func TestCountCapRefusesWithoutDamageJavaScript(t *testing.T) {
	u := loadSchemaString(t, "count.schema", "package countprobe\ntable Child { entries map[int32]int32 }\ntable Root { inner Child }\n")
	model := tabletext.NewModel(u)
	makeWire := func(count uint64) []byte {
		payload := binary.AppendUvarint([]byte{13}, count)
		if count != 0 {
			payload = append(payload, 1, 0)
		}
		child := binary.AppendUvarint([]byte{2, 14}, uint64(len(payload)))
		child = append(child, payload...)
		child = append(child, 0)
		wire := []byte{1, 3, 4, 7, 0, 0, 0, 1, 13}
		wire = binary.AppendUvarint(wire, uint64(len(child)))
		wire = append(wire, child...)
		wire = append(wire, 4, 4, 9, 0, 0, 0, 0)
		for _, name := range []string{"inner", "entries", "before", "after"} {
			wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId(name))
		}
		return binary.LittleEndian.AppendUint64(wire, 4)
	}
	files := map[string][]byte{"clean.bin": makeWire(0), "short.bin": makeWire(0x7fffffff)}
	for i, count := range []uint64{1 << 31, 1 << 32, 1<<32 + 1} {
		wire := makeWire(count)
		report := tabletext.Report{Unknown: 3}
		ok, err := tablewire.Decode(model, model.New(model.Lookup("Root")), wire, &report)
		var refusal *tablewire.CountRefusal
		if ok || !errors.As(err, &refusal) || report.Malformed || report.Unknown != 4 {
			t.Fatalf("engine count %d: ok=%v err=%v report=%+v", count, ok, err, report)
		}
		files[fmt.Sprintf("cap%d.bin", i)] = wire
	}
	jsDiscardRun(t, u, files, `
const wire = (name) => new Uint8Array(readFileSync(name));
for (let i = 0; i < 3; i++) {
 for (const retaining of [false, true]) {
  const r = new m.TableVariableReport();
  r.unknown = 3;
  const keep = new m.TableVariableRetain(new Uint8Array(1024), new Array(64));
  const value = retaining ? m.RootLoadRetain(wire("cap" + i + ".bin"), keep, r)
                          : m.RootLoad(wire("cap" + i + ".bin"), r);
  if (value !== null || !r.refused || r.reason !== "count_over_extent_cap" ||
      r.malformed || r.unknown !== 4 || keep.used !== 0 || keep.count !== 0) {
   fail("count-cap must refuse without damage, later counters, or retained output: " + JSON.stringify(r));
  }
  const clean = m.RootLoad(wire("clean.bin"), r);
  if (clean === null || r.refused || r.malformed) fail("prior count refusal poisoned a clean load");
 }
}
const short = new m.TableVariableReport();
if (m.RootLoad(wire("short.bin"), short) !== null || !short.malformed || short.refused)
 fail("INT32_MAX with insufficient body must remain damage, not storage-cap refusal");
`)
}
