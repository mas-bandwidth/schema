package cpptable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// A union whose arm is a TABLE (docs/SPEC-TABLES.md §2.6) has no packet wire,
// so its shape — the tag enum and the tagged-union struct — is emitted HERE, in
// the Table header of the file that declares it, after the tables its arms
// name, rather than in the packet header, which precedes every table. The
// spelling is the packet emitter's to the character, so a table arm and a type
// arm read as one family.

// tableUnionsOf returns the unions this file declares that carry a table arm,
// in declaration order: the checker assembles them beside the decl stream.
func tableUnionsOf(f *ir.File) []*ir.Union { return f.TableUnions }

// tableUnionsHeldBy returns, in field order, the unions of `unions` that st
// holds by value — the ones that must be emitted before st's struct.
func tableUnionsHeldBy(st *ir.Struct, unions []*ir.Union) []*ir.Union {
	var out []*ir.Union
	for _, f := range st.Fields {
		un, ok := f.Type.Ref.(*ir.Union)
		if !ok || f.Type.Kind != ir.TNamed {
			continue
		}
		for _, cand := range unions {
			if cand == un {
				out = append(out, un)
			}
		}
	}
	return out
}

// emitTableUnion emits one table-armed union: the <Name>Type tag enum, then
// the tagged-union shape — a struct holding the tag over an anonymous union of
// the arms, constructed as None, trivially copyable (SPEC §4.8).
func (g *tableGen) emitTableUnion(un *ir.Union) {
	storage := fmt.Sprintf("uint%d_t", ir.StorageBitsFor(int64(len(un.Variants))))
	g.pf("// %sType: union %s's tag — None = 0, then each variant in declared order (SPEC §4.8)\n", un.Name, un.Name)
	g.pf("enum class %sType : %s {\n", un.Name, storage)
	g.pf("    None = 0,\n")
	for i, v := range un.Variants {
		g.pf("    %s = %d,\n", ir.GoExportName(v.Name), i+1)
	}
	g.pf("    Max = %d, // the exported extent (SPEC §4.2)\n", len(un.Variants))
	g.pf("};\n\n")

	arm, typ := "arm", "Arm"
	if len(un.Variants) > 0 {
		arm, typ = un.Variants[0].Name, un.Variants[0].Type
	}
	g.pf("// union %s — at most one of the arms; the tag says which. An arm is a TABLE\n", un.Name)
	g.pf("// where it names one (docs/SPEC-TABLES.md §2.6), so the union has no packet wire and\n")
	g.pf("// lives here, after its arms. Construction is None: the tag alone is\n")
	g.pf("// initialized; an arm's storage is established when the arm is selected — by\n")
	g.pf("// %sLoadBody before it decodes, or by assigning it: value.%s = %s{}.\n", un.Name, arm, typ)
	g.pf("// Bytes of unselected arms are indeterminate.\n")
	g.pf("struct %s\n{\n", un.Name)
	g.pf("    %sType type;\n", un.Name)
	if len(un.Variants) > 0 {
		g.pf("\n    union\n    {\n")
		for _, v := range un.Variants {
			g.noteRef(v.Type)
			g.pf("        %s %s;\n", v.Type, v.Name)
		}
		g.pf("    };\n")
	}
	g.pf("\n    %s() : type( %sType::None ) {} // the tag only — arms are established at selection\n", un.Name, un.Name)
	g.pf("};\n\n")
}
