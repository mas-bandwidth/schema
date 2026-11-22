package unitytable_test

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestUnityHeldKind31RefusesTheLoad: kind 31 is held by name and never
// written (R17, docs/SPEC-TABLES.md §3: the newer reader reads old data, never
// the other way). A C# (unity) reader meets it only as damage and refuses the
// whole load wherever it stands: a field it cannot name (with payload bytes or
// with L 0), a field it declares, a union arm, an array or a keyed element
// kind, exactly as it refuses 33 and 34. Every case runs through the plain and
// the retaining load and the load measure. Red on the old escape, which
// skipped an unnamed 31 by its L and counted it unknown. The control, an
// unnamed string, reads, so the probe can tell a read from a refusal.
func TestUnityHeldKind31RefusesTheLoad(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "dotnet Roslyn csc Unity dialect kind 31 refusal")
	u := fixedproof.ParseCheckUnit(t, unityHeldEscapeSchema)
	files, err := unityGenerateFiles(u, nil)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, c := range unityHeldEscapeCases(t, u) {
		fmt.Fprintf(&b, unityHeldEscapeCase, c.name, fmt.Sprintf("%x", c.wire), c.read)
	}
	out := mustRunUnityVTProbe(t, files, fmt.Sprintf(unityHeldEscapeMain, b.String()))
	if !strings.Contains(out, "PROBE OK") {
		t.Fatalf("the probe did not finish:\n%s", out)
	}
}

const unityHeldEscapeSchema = `package probe
table Root { n int32 }
`

type unityHeldEscape struct {
	name string
	read bool
	wire []byte
}

// unityHeldEscapeCases are form-1 files (docs/SPEC-TABLES.md §3) that each carry
// kind 31 at one position, and the control.
func unityHeldEscapeCases(t *testing.T, u *ir.Unit) []unityHeldEscape {
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
	return []unityHeldEscape{
		{"control, an unnamed string", true, file([]uint64{zzz}, 1, byte(ir.TableKindString), 2, 'h', 'i')},
		{"unnamed field", false, file([]uint64{zzz}, 1, k, 2, 0xAA, 0xBB)},
		{"unnamed field, L 0", false, file([]uint64{zzz}, 1, k, 0)},
		{"named field", false, file([]uint64{ir.TableFieldWireId(n)}, 1, k, 1, 0)},
		{"union arm", false, file([]uint64{zzz, other}, 1, byte(ir.TableKindUnion), 2, k, 1, 0xAA)},
		{"array element kind", false, file([]uint64{zzz}, 1, byte(ir.TableKindArray), 4, k, 1, 1, 0xAA)},
		{"keyed element kind", false, file([]uint64{zzz, other}, 1, byte(ir.TableKindKeyed), 5, k, 1, 2, 1, 0xAA)},
	}
}

const unityHeldEscapeMain = `using System;
using Probe;

class Program
{
    static int Main()
    {
        int bad = 0;
%s
        Console.WriteLine(bad == 0 ? "PROBE OK" : "FAILED " + bad);
        return bad == 0 ? 0 : 1;
    }
}
`

const unityHeldEscapeCase = `{
    string name = %q;
    byte[] wire = Convert.FromHexString(%q);
    bool expect = %t;
    Root v = default;
    Schema.TableReport rep = default;
    bool ok = Schema.RootLoad(ref v, wire, ref rep) && !rep.Refused && !rep.Damaged;
    if (ok != expect) { Console.WriteLine("FAIL " + name + ": load " + rep.ReasonText + " unknown=" + rep.Unknown); bad++; }
    var retain = new Schema.TableRetain(4096, 64);
    Root rv = default;
    Schema.TableReport rr = default;
    bool rok = Schema.RootLoadRetain(ref rv, wire, retain, ref rr) && !rr.Refused && !rr.Damaged;
    if (rok != expect) { Console.WriteLine("FAIL " + name + ": retaining load " + rr.ReasonText); bad++; }
    if ((Schema.RootLoadMeasure(wire) >= 0) != expect) { Console.WriteLine("FAIL " + name + ": load measure"); bad++; }
}
`
