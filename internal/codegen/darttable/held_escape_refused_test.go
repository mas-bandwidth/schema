package darttable

import (
	"encoding/binary"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestDartHeldKind31RefusesTheLoad: kind 31 is held by name and never written
// (R17, docs/SPEC-TABLES.md §3: the newer reader reads old data, never the
// other way). A Dart reader meets it only as damage and refuses the whole load
// wherever it stands: a field it cannot name (with payload bytes or with L 0),
// a field it declares, a union arm, an array or a keyed element kind, exactly
// as it refuses 33 and 34. Every case runs through the plain and the retaining
// load. Red on the old escape, which skipped an unnamed 31 by its L and counted
// it unknown. The control, an unnamed string, reads, so the probe can tell a
// read from a refusal.
func TestDartHeldKind31RefusesTheLoad(t *testing.T) {
	slowtest.Gate(t, "dart")
	u := unitFrom(t, dartHeldEscapeSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	var b strings.Builder
	b.WriteString(dartClosurePrelude)
	for _, c := range dartHeldEscapeCases(t, u) {
		fmt.Fprintf(&b, dartHeldEscapeCase, fmt.Sprintf("%x", c.wire), c.read, c.name)
	}
	b.WriteString("}")
	runFormsProbe(t, files, b.String())
}

const dartHeldEscapeSchema = `package probe
table Root { n int32 }
`

type dartHeldEscape struct {
	name string
	read bool
	wire []byte
}

// dartHeldEscapeCases are form-1 files (docs/SPEC-TABLES.md §3) that each carry
// kind 31 at one position, and the control.
func dartHeldEscapeCases(t *testing.T, u *ir.Unit) []dartHeldEscape {
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
	return []dartHeldEscape{
		{"control, an unnamed string", true, file([]uint64{zzz}, 1, byte(ir.TableKindString), 2, 'h', 'i')},
		{"unnamed field", false, file([]uint64{zzz}, 1, k, 2, 0xAA, 0xBB)},
		{"unnamed field, L 0", false, file([]uint64{zzz}, 1, k, 0)},
		{"named field", false, file([]uint64{ir.TableFieldWireId(n)}, 1, k, 1, 0)},
		{"union arm", false, file([]uint64{zzz, other}, 1, byte(ir.TableKindUnion), 2, k, 1, 0xAA)},
		{"array element kind", false, file([]uint64{zzz}, 1, byte(ir.TableKindArray), 4, k, 1, 1, 0xAA)},
		{"keyed element kind", false, file([]uint64{zzz, other}, 1, byte(ir.TableKindKeyed), 5, k, 1, 2, 1, 0xAA)},
	}
}

const dartHeldEscapeCase = `for(final retaining in [false,true]){
 final data=bytes(%q);final store=w.TableRetain(65536,List.filled(1024,0));final r=w.TableWireReport();
 if(retaining){w.rootWireLoadRetain(data,store,r);}else{w.rootWireLoad(data,r);}
 check(r.refused==!%t,%q+' retaining='+retaining.toString()+' refused='+r.refused.toString()+' reason='+r.reason+' unknown='+r.unknown.toString());
}
`
