package cstable

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Under the strict-only read contract (SPEC-TABLES §6.5), a repeated field id
// inside a map entry body is wire damage and refuses the whole load across all
// read modes.
func TestMapRepeatedKeyLastKindWinsCSharp(t *testing.T) {
	slowtest.Gate(t, "the .NET SDK")
	const schema = "package probe\ntable Root { entries map[uint32]int32 }\n"
	m := tabletext.NewModel(unitFrom(t, schema))
	var cases strings.Builder
	for _, tc := range []struct {
		name string
		keys []byte
	}{
		{"bad_then_widened", []byte{2, 9, 0, 0, 0, 0, 0, 0, 0, 0, 2, 6, 7}},
		{"widened_then_bad", []byte{2, 6, 7, 2, 9, 0, 0, 0, 0, 0, 0, 0, 0}},
		{"bad_then_exact", []byte{2, 9, 0, 0, 0, 0, 0, 0, 0, 0, 2, 8, 7, 0, 0, 0}},
	} {
		entry := append(append([]byte{}, tc.keys...), 0)
		payload := append([]byte{13, 1, byte(len(entry))}, entry...)
		wire := append([]byte{1, 1, 14, byte(len(payload))}, payload...)
		wire = append(wire, 0)
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableFieldWireId(m.Lookup("Root").Fields[0]))
		wire = binary.LittleEndian.AppendUint64(wire, ir.MapKeyWireId)
		wire = binary.LittleEndian.AppendUint64(wire, 2)
		v := m.New(m.Lookup("Root"))
		var report tabletext.Report
		if ok, _ := tablewire.Decode(m, v, wire, &report); ok || !report.Refused {
			t.Fatalf("%s reference accepted repeated key field: %+v", tc.name, report)
		}
		fmt.Fprintf(&cases, "CheckRefused(%q,Convert.FromHexString(%q));\n", tc.name, hex.EncodeToString(wire))
	}
	t15RunCS(t, schema, mapRepeatedKeyCSharpProbe+cases.String())
}

const mapRepeatedKeyCSharpProbe = `
unsafe void CheckRefused(string name,byte[] wire) {
 var r0=new Probe.TableReport();
 if(Probe.Schema.RootLoad(new Probe.Root(),wire,r0) || (!r0.Refused && !r0.Damaged)) bad += ProbeLog.Fail(name+" managed load accepted");
 using (var builder=new Probe.RootBuilder()) {
  var r1=new Probe.TableReport();
  if(builder.Load(wire,r1) || (!r1.Refused && !r1.Damaged)) bad += ProbeLog.Fail(name+" builder load accepted");
 }
 void* region=System.Runtime.InteropServices.NativeMemory.AlignedAlloc(4096,16);
 try {
  var r2=new Probe.TableReport();
  if(Probe.Schema.RootLoad((IntPtr)region,4096,wire,r2)!=null || (!r2.Refused && !r2.Damaged)) bad += ProbeLog.Fail(name+" region load accepted");
  byte* capture=stackalloc byte[4096];
  Probe.TableRetain.Id* ids=stackalloc Probe.TableRetain.Id[128];
  var retain=new Probe.TableRetain { Bytes=capture,Capacity=4096,Ids=ids,IdCapacity=128 };
  var r3=new Probe.TableReport();
  if(Probe.Schema.RootLoadRetain((IntPtr)region,4096,wire,ref retain,r3)!=null || (!r3.Refused && !r3.Damaged)) bad += ProbeLog.Fail(name+" retain load accepted");
 } finally { System.Runtime.InteropServices.NativeMemory.AlignedFree(region); }
}
`
