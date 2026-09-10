// THE GUARD'S WIDTH (docs/SPEC-TABLES.md §3.4). A union tag is one, two, four
// or eight bytes, and the identity plan stamps that width onto every guarded
// leaf so the read loop compares the tag whole. A one-byte compare fires arm 1
// on a foreign 0x0101; this file holds the stamp, and the C/C++ runtimes hold
// the compare.
package compiler

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedGuardWidthOneByteTag(t *testing.T) {
	u := leafCapUnit(t, `package probe

type Cell { n int32 }

union Pick {
    a Cell
    b Cell
}

table Root { pick Pick }
`)
	st := u.Tables["Root"]
	if st == nil {
		t.Fatal("the probe declares Root")
	}
	plan, _ := ir.TableFixedBuildPlan(u, st)
	guarded := 0
	for _, e := range plan {
		if e.Guard == ir.TableFixedNoGuard {
			continue
		}
		guarded++
		if e.ArgW != 1 {
			t.Fatalf("a two-arm union's tag is one byte, ArgW=%d on %s", e.ArgW, e.Note)
		}
	}
	if guarded == 0 {
		t.Fatal("a union's arms must produce guarded leaves")
	}
}

func TestFixedGuardWidthTwoByteTag(t *testing.T) {
	var b strings.Builder
	b.WriteString("package probe\n\ntype Cell { n int32 }\n\nunion Wide {\n")
	for i := 0; i < 256; i++ {
		fmt.Fprintf(&b, "    a%d Cell\n", i)
	}
	b.WriteString("}\n\ntable Root { pick Wide }\n")
	u := leafCapUnit(t, b.String())
	st := u.Tables["Root"]
	if st == nil {
		t.Fatal("the probe declares Root")
	}
	un := u.Unions["Wide"]
	if un == nil {
		un = u.TableUnions["Wide"]
	}
	if un == nil {
		t.Fatal("Wide is a union")
	}
	if bits := ir.StorageBitsFor(un.Max); bits != 16 {
		t.Fatalf("256 arms: StorageBitsFor(%d)=%d, want 16", un.Max, bits)
	}
	plan, _ := ir.TableFixedBuildPlan(u, st)
	guarded := 0
	for _, e := range plan {
		if e.Guard == ir.TableFixedNoGuard {
			continue
		}
		guarded++
		if e.ArgW != 2 {
			t.Fatalf("a 256-arm union's tag is two bytes, ArgW=%d on %s", e.ArgW, e.Note)
		}
	}
	if guarded == 0 {
		t.Fatal("a union's arms must produce guarded leaves")
	}
}
