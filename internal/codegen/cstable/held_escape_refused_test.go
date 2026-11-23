package cstable

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestCSharpHeldKind31RefusesTheLoad: kind 31 is held by name and never
// written (R17, docs/SPEC-TABLES.md §3: the newer reader reads old data, never
// the other way). A C# reader meets it only as damage and refuses the whole
// load wherever it stands: a field it cannot name (with payload bytes or with
// L 0), a field it declares, a union arm, an array or a keyed element kind,
// exactly as it refuses 33 and 34. Every case runs through the plain, region,
// retaining and builder loads and the load measure. Red on the old escape,
// which skipped an unnamed 31 by its L and counted it unknown. The control, an
// unnamed string, reads, so the driver can tell a read from a refusal.
func TestCSharpHeldKind31RefusesTheLoad(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for _, c := range csHeldEscapeCases(t, unitFrom(t, csHeldEscapeSchema)) {
		fmt.Fprintf(&b, csHeldEscapeCase, c.name, fmt.Sprintf("%x", c.wire), c.read)
	}
	out := t08RunCS(t, csHeldEscapeSchema, b.String(), "global using Probe;")
	if strings.Contains(out, "FAIL ") || !strings.Contains(out, "PROBE OK") {
		t.Fatalf("a C# load read kind 31, or the control was refused:\n%s", out)
	}
}

const csHeldEscapeSchema = `package probe
table Root { n int32 }
`

type csHeldEscape struct {
	name string
	read bool
	wire []byte
}

// csHeldEscapeCases are form-1 files (docs/SPEC-TABLES.md §3) that each carry
// kind 31 at one position, and the control.
func csHeldEscapeCases(t *testing.T, u *ir.Unit) []csHeldEscape {
	t.Helper()
	var n *ir.Field
	for _, f := range u.Tables["Root"].Fields {
		if f.Name == "n" {
			n = f
		}
	}
	if n == nil {
		t.Fatal("the probe declares no n")
	}
	file := func(ids []uint64, field ...byte) []byte {
		w := append([]byte{byte(ir.TableWireForm)}, field...)
		w = append(w, 0)
		for _, id := range ids {
			w = binary.LittleEndian.AppendUint64(w, id)
		}
		return binary.LittleEndian.AppendUint64(w, uint64(len(ids)))
	}
	zzz, other := ir.TableWireId("zzz"), ir.TableWireId("zzz_arm_or_key")
	k := byte(ir.TableKindHeldEscape)
	return []csHeldEscape{
		{"control, an unnamed string", true, file([]uint64{zzz}, 1, byte(ir.TableKindString), 2, 'h', 'i')},
		{"unnamed field", false, file([]uint64{zzz}, 1, k, 2, 0xAA, 0xBB)},
		{"unnamed field, L 0", false, file([]uint64{zzz}, 1, k, 0)},
		{"named field", false, file([]uint64{ir.TableFieldWireId(n)}, 1, k, 1, 0)},
		{"union arm", false, file([]uint64{zzz, other}, 1, byte(ir.TableKindUnion), 2, k, 1, 0xAA)},
		{"array element kind", false, file([]uint64{zzz}, 1, byte(ir.TableKindArray), 4, k, 1, 1, 0xAA)},
		{"keyed element kind", false, file([]uint64{zzz, other}, 1, byte(ir.TableKindKeyed), 5, k, 1, 2, 1, 0xAA)},
	}
}

const csHeldEscapeCase = `{
 string name = %q;
 byte[] wire = Convert.FromHexString(%q);
 bool expect = %t;
 var value = new Root();
 var report = new TableReport();
 var verdict = Schema.RootLoadVerdict(value, wire, report);
 bool ok = verdict == Schema.TableWire.Verdict.Ok && !report.Refused && !report.Damaged;
 if (ok != expect) { bad += ProbeLog.Fail(name + ": plain verdict " + verdict + " reason=" + report.Reason + " unknown=" + report.Unknown); }
 unsafe {
  long cap = 1L << 20;
  void* mem = System.Runtime.InteropServices.NativeMemory.AlignedAlloc((nuint)cap, 16);
  try {
   var rr = new TableReport();
   RootRow* p = Schema.RootLoad((IntPtr)mem, cap, wire, rr);
   if ((p != null && !rr.Refused && !rr.Damaged) != expect) { bad += ProbeLog.Fail(name + ": region reason=" + rr.Reason); }
   TableRetain retain = default;
   var tr = new TableReport();
   RootRow* tp = Schema.RootLoadRetain((IntPtr)mem, cap, wire, ref retain, tr);
   if ((tp != null && !tr.Refused && !tr.Damaged) != expect) { bad += ProbeLog.Fail(name + ": retaining reason=" + tr.Reason); }
  } finally { System.Runtime.InteropServices.NativeMemory.AlignedFree(mem); }
 }
 unsafe { using (var builder = new RootBuilder()) {
  var br = new TableReport();
  if (builder.Load(wire, br) != expect) { bad += ProbeLog.Fail(name + ": builder reason=" + br.Reason); }
 } }
 if ((Schema.RootLoadMeasure(wire) > 0) != expect) { bad += ProbeLog.Fail(name + ": measure"); }
}
`
