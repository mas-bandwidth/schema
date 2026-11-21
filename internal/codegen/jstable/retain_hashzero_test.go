package jstable

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v3/ir"
)

// The retained payload must distinguish a named hash zero from reference zero
// (a body terminator or None). These six independent wire shapes are shared
// with the reference engine's retained-hash-zero regression.
func TestJavaScriptRetainNestedHashZero(t *testing.T) {
	u := loadSchemaString(t, "hashzero.schema", "package hashzero\ntable Root { n int32 }\n")
	cases := []struct {
		name string
		body []byte
	}{
		{"enum", []byte{1, 30, 2, 0}},
		{"union", []byte{1, 15, 2, 6, 1, 7, 0}},
		{"body", []byte{1, 13, 4, 2, 6, 7, 0, 0}},
		{"keyed", []byte{1, 16, 5, 6, 1, 2, 1, 7, 0}},
		{"enum-array-none", []byte{1, 14, 4, 30, 2, 2, 0, 0}},
		{"union-array-none", []byte{1, 14, 7, 15, 2, 2, 6, 1, 7, 0, 0}},
	}
	files := map[string][]byte{}
	for i, c := range cases {
		for j, id := range []uint64{0, ir.TableWireId("future_inner_hash")} {
			wire := append([]byte{1}, c.body...)
			wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId("future_outer_hash"))
			wire = binary.LittleEndian.AppendUint64(wire, id)
			wire = binary.LittleEndian.AppendUint64(wire, 2)
			files[fmt.Sprintf("%d-%d.bin", i, j)] = wire
		}
	}
	for i, body := range [][]byte{{1, 30, 0, 0}, {1, 15, 0, 0}} {
		wire := append([]byte{1}, body...)
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId("future_outer_hash"))
		files[fmt.Sprintf("none-%d.bin", i)] = binary.LittleEndian.AppendUint64(wire, 1)
	}
	jsDiscardRun(t, u, files, `
const names=[];
for (let i=0; i<6; i++) for (let j=0; j<2; j++) names.push(i+"-"+j+".bin");
names.push("none-0.bin", "none-1.bin");
for (const name of names) {
 let input = new Uint8Array(readFileSync(name));
 for (let pass=0; pass<2; pass++) {
  const plain = new m.TableVariableReport();
  if (m.RootLoad(input, plain)===null || plain.damaged || plain.refused) fail("plain "+name);
  const report = new m.TableVariableReport();
  const keep = new m.TableVariableRetain(new Uint8Array(4096), new Array(32));
  const value = m.RootLoadRetain(input, keep, report);
  if (value===null || report.damaged || report.refused || report.retained!==1 || report.retainLost!==0) {
   fail("retain "+name+" "+JSON.stringify(report)); continue;
  }
  const saved = m.RootSaveRetain(value, keep, new m.TableVariableReport());
  if (saved===null || Buffer.compare(Buffer.from(saved),Buffer.from(input))!==0) {
   fail("save "+name); continue;
  }
  input=saved;
 }
}
`)
}
