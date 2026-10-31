package darttable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE FORM-1 COOK OF A FIXED-CLASS ROOT (docs/SPEC-TABLES.md §7.6): a fixed
// table's value as the form-1 wire library holds it (<Home>Wire.dart, which
// carries fixed tables too, §3) cooked to `schema cook`'s bytes in either
// byte order. The fixed-root writer (emitCookWriteFixed) cooks the form-3
// class; a value loaded from a form-1 wire is the wire library's class, and a
// fixed root whose closure holds a union or whose record is past the form-3
// ceiling has no form-3 writer at all. This writer is the variable cook's
// graph walk (emitCookWriteVariable) over that class, in its own
// <Root>CookWire class so no record writer meets the fixed writer's
// `_cook<Record>` names. A fixed region is one node: the root, its record and
// one attribution pair (§7.1).

// form1CookClass is the class that holds a fixed root's form-1 cook writer.
func form1CookClass(st *ir.Struct) string { return st.Name + "CookWire" }

// emitCookWriteForm1 writes the form-1 cook of a fixed-class root as its own
// class, or a comment naming the first storage it does not carry (§7.6).
func (g *cookGen) emitCookWriteForm1(st *ir.Struct) {
	if st.MapEntryOf != "" || ir.VariableTables(g.unit)[st.Name] || !wireCarries(g.unit, st) {
		return
	}
	g.form1 = true
	defer func() { g.form1 = false }()
	if why := g.variableCookRefusal(st); why != "" {
		g.pf("// %s has no form-1 cook writer here: %s.\n\n", st.Name, why)
		return
	}
	g.needData = true
	g.needHome = true
	if len(g.blocks.Tables) > 0 {
		// the BUILD VERSION's home, as emitCook names it (§20.7)
		g.needBlock = true
	}
	g.pf("// %s's FORM-1 COOK (docs/SPEC-TABLES.md §7.6): the wire library's value of\n", st.Name)
	g.pf("// this fixed table cooked to `schema cook`'s bytes, measured before written.\n")
	g.pf("final class %s {\n", form1CookClass(st))
	g.emitCookWriteVariable(st)
	if len(g.form1Unions) > 0 {
		g.pf("\n")
		g.emitCookForm1UnionWriters()
		g.trimBlank()
	}
	g.pf("}\n\n")
}

// wireCarries reports whether the unit's form-1 wire library declares a value
// class for st: the wire closure of a unit with a form-1 root (§3).
func wireCarries(u *ir.Unit, st *ir.Struct) bool {
	if len(variableRoots(u)) == 0 {
		return false
	}
	for _, w := range wireClosure(u) {
		if w.Name == st.Name {
			return true
		}
	}
	return false
}

// cookGraphRecords is every record one root's cooked region can hold, in
// cookWalk's order, and — for a fixed root's form-1 cook — every record a
// union arm holds by value as well, since the arm is written in place (§2.6).
func (g *cookGen) cookGraphRecords(name string) []string {
	var records []string
	if !g.form1 {
		// every record the graph's nodes hold, and each MAP's generated entry
		// (§2.8) with every record it reaches: an entry is no node, but its
		// writer lays it in the holder's extent, and a record reached only
		// through an entry (an `Item` a `*Item` value names) is a node
		seen := map[string]bool{}
		var walk func(string)
		walk = func(n string) {
			cookWalk(g.unit, n, func(r string, _ *ir.MemberLayout) {
				if seen[r] {
					return
				}
				seen[r] = true
				records = append(records, r)
			})
		}
		walk(name)
		for i := 0; i < len(records); i++ {
			for _, f := range cookMember(g.unit, records[i]).Fields {
				if f.IsMap() {
					walk(f.MapEntry.Name)
				}
			}
			if st := cookMember(g.unit, records[i]); st != nil {
				for _, f := range st.Fields {
					if un := unionRef(f); un != nil {
						for _, arm := range un.Variants {
							if !arm.Void() && arm.F.IsMap() {
								walk(arm.F.MapEntry.Name)
							}
						}
					}
				}
			}
		}
		return records
	}
	seen := map[string]bool{}
	var walk func(string)
	walk = func(n string) {
		cookWalk(g.unit, n, func(r string, _ *ir.MemberLayout) {
			if seen[r] {
				return
			}
			seen[r] = true
			records = append(records, r)
			for _, f := range cookMember(g.unit, r).Fields {
				if un, ok := f.Type.Ref.(*ir.Union); ok && f.Type.Kind == ir.TNamed {
					for _, arm := range un.Variants {
						if arm.F != nil {
							if ref := tableRef(arm.F); ref != nil {
								walk(ref.Name)
							}
						}
					}
				}
			}
		})
	}
	walk(name)
	return records
}

// form1CookField reports whether the form-1 cook carries a field of a fixed
// root's closure (§7.2's storage kinds): everything the variable writer
// carries, and the fixed class's own constructs — an optional (§2.3), an
// enum-keyed array (§2.4), a fixed positional array, an enum, a flags mask and
// a union (§2.6), whose every arm must be carried in turn.
func form1CookField(f *ir.Field) bool {
	if f.IsMap() || f.IsList() || f.Type.Pointer || f.Type.Blob() || f.Guard != "" {
		return false
	}
	if f.Type.Optional {
		inner := *f
		inner.Type.Optional = false
		return form1CookField(&inner)
	}
	if f.KeyEnum != "" || f.Array != ir.ArrayNone {
		if f.KeyEnum == "" && f.Array != ir.ArrayCounted && f.Array != ir.ArrayFixed {
			return false
		}
		if f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes || f.Type.Kind == ir.TWString {
			return false
		}
		// an ARRAY OF UNIONS (§2.6, §3) is carried element by element: each
		// slot is the union's own storage, written by `_cook<Union>`
		elem := *f
		elem.Array, elem.ArrayBound, elem.KeyEnum, elem.KeyEnumRef = ir.ArrayNone, 0, "", nil
		return form1CookField(&elem)
	}
	switch f.Type.Kind {
	case ir.TBool, ir.TBits, ir.TFloat32, ir.TFloat64, ir.TString, ir.TBytes:
		return true
	case ir.TFixed:
		// the fixed-point kinds (§3, §15): the wire library holds the RAW
		// value as a BigInt, written at its storage width
		return isWide(f)
	case ir.TInt:
		return f.Type.Width != 128 || isWide(f)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct, *ir.Enum, *ir.Flags:
			return true
		case *ir.Union:
			for _, arm := range ref.Variants {
				if arm.F != nil && !form1CookField(arm.F) {
					return false
				}
			}
			return true
		}
	}
	return false
}

// emitCookGraphField writes one field of a graph record: the variable writer's
// own field, or on a fixed root's form-1 cook the fixed class's constructs
// as well (§7.2).
func (g *cookGen) emitCookGraphField(f *ir.Field, offset int64, expr, ind string) {
	if !g.form1 {
		g.emitCookVariableField(f, offset, expr, ind)
		return
	}
	g.emitCookForm1Field(f, offset, expr, ind)
}

// emitCookForm1Field writes one field of a fixed root's form-1 value at the
// cook's offsets, every piece in the target byte order (§7.2, §7.6).
func (g *cookGen) emitCookForm1Field(f *ir.Field, offset int64, expr, ind string) {
	pieces := ir.FieldPieces(g.unit, f, offset)
	if f.Type.Optional {
		// §2.3 on the cooked storage: the presence bool is the LAST piece, and
		// the value's pieces are written whole either way, as the reference's
		// region writer writes them
		owner := expr
		if i := lastDot(expr); i >= 0 {
			owner = expr[:i]
		}
		g.pf("%sout?.setUint8(at + %d, %s.%s ? 1 : 0);\n", ind, pieces[len(pieces)-1].Offset, owner, hasMember(f))
		inner := *f
		inner.Type.Optional = false
		g.emitCookForm1Field(&inner, offset, expr, ind)
		return
	}
	if f.KeyEnum != "" || f.Array == ir.ArrayFixed {
		// every stored slot exists: a fixed array's bound, or an enum-keyed
		// array's E.Max with the key k at index k-1 (§2.4)
		bound := f.ArrayBound
		if f.KeyEnum != "" {
			bound = f.KeyEnumRef.Max
		}
		stride := pieces[0].Size / bound
		g.pf("%sif (%s.length != %d) return false;\n", ind, expr, bound)
		g.pf("%sfor (var i = 0; i < %d; i++) {\n", ind, bound)
		if un := unionRef(f); un != nil {
			g.emitCookForm1UnionElement(un, expr+"[i]", fmt.Sprintf("at + %d + i * %d", offset, stride), ind+"  ")
		} else if isWide(f) {
			g.pf("%s  final e = %s[i];\n", ind, expr)
			g.emitCookElementWide(f, "e", offset, stride, ind+"  ")
		} else if ref := tableRef(f); ref != nil {
			call := fmt.Sprintf("%s  if (!_cook%s(%s[i], c, at + %d + i * %d)) return false;", ind, ref.Name, expr, offset, stride)
			if len(call) <= 80 {
				g.pf("%s\n", call)
			} else {
				g.pf("%s  final e = %s[i];\n", ind, expr)
				g.pf("%s  if (!_cook%s(e, c, at + %d + i * %d)) return false;\n", ind, ref.Name, offset, stride)
			}
		} else {
			g.pf("%s  final e = %s[i];\n", ind, expr)
			g.emitCookFixedScalar(f, "e", fmt.Sprintf("at + %d + i * %d", offset, stride), stride, ind+"  ")
		}
		g.pf("%s}\n", ind)
		return
	}
	if un := unionRef(f); un != nil && f.Array == ir.ArrayCounted {
		// a COUNTED ARRAY OF UNIONS (§2.6, §3): the count, then each live
		// slot; a dead slot is the union's value-initialized storage, tag 0
		// and nothing else, which the zero-filled region already holds
		stride := pieces[0].Size / f.ArrayBound
		g.pf("%sif (%s.length > %d) return false;\n", ind, expr, f.ArrayBound)
		g.pf("%sout?.setInt32(at + %d, %s.length, order);\n", ind, pieces[1].Offset, expr)
		g.pf("%sfor (var i = 0; i < %s.length; i++) {\n", ind, expr)
		g.emitCookForm1UnionElement(un, expr+"[i]", fmt.Sprintf("at + %d + i * %d", offset, stride), ind+"  ")
		g.pf("%s}\n", ind)
		return
	}
	if f.Array == ir.ArrayNone && f.Type.Kind == ir.TNamed {
		switch ref := f.Type.Ref.(type) {
		case *ir.Union:
			g.emitCookForm1Union(f, ref, offset, expr, ind)
			return
		case *ir.Enum, *ir.Flags:
			g.emitCookFixedScalar(f, expr, fmt.Sprintf("at + %d", offset), pieces[0].Size, ind)
			return
		}
	}
	if f.Array == ir.ArrayNone && f.Type.Kind == ir.TBytes {
		// bytes(N) is held as a Latin-1 String by the wire library, one char
		// per byte, so its bytes are latin1's, never UTF-8's (§2.5)
		g.needConvert = true
		text := dartName(f.Name) + "Bytes"
		g.pf("%sfinal %s = latin1.encode(%s);\n", ind, text, expr)
		g.pf("%sif (%s.length > %d) return false;\n", ind, text, f.Type.Size)
		g.pf("%sif (out != null) {\n", ind)
		g.pf("%s  out.setInt32(at + %d, %s.length, order);\n", ind, pieces[1].Offset, text)
		g.pf("%s  for (var i = 0; i < %s.length; i++) {\n", ind, text)
		g.pf("%s    out.setUint8(at + %d + i, %s[i]);\n", ind, offset, text)
		g.pf("%s  }\n%s}\n", ind, ind)
		return
	}
	if isByValueTable(f) {
		// a record held by value, in the spelling `dart format` writes: a
		// union arm's deeper indent splits the long call (tables-dart-clean)
		ref := tableRef(f).Name
		if line := fmt.Sprintf("%sif (!_cook%s(%s, c, at + %d)) return false;", ind, ref, expr, offset); len(line) <= 80 {
			g.pf("%s\n", line)
		} else if head := fmt.Sprintf("%sif (!_cook%s(%s, c, at + %d))", ind, ref, expr, offset); len(head) <= 80 {
			g.pf("%s\n%s  return false;\n", head, ind)
		} else {
			g.pf("%sif (!_cook%s(\n%s  %s,\n%s  c,\n%s  at + %d,\n%s))\n%s  return false;\n", ind, ref, ind, expr, ind, ind, offset, ind, ind)
		}
		return
	}
	g.emitCookVariableField(f, offset, expr, ind)
}

// emitCookForm1Union writes a union where it lies (§2.6, §7.2): the TAG at
// offset 0 at its own width, the SET ARM at the arms' shared offset, and no
// other byte, so the extent outside the set arm stays the zero the region was
// filled with — what internal/tablecook's region writer writes. A tag past
// the declared arms is refused as `invalid_value`.
func (g *cookGen) emitCookForm1Union(f *ir.Field, un *ir.Union, offset int64, expr, ind string) {
	_, _, tag, armOffset := ir.UnionLayout(g.unit, un)
	arm := dartName(f.Name) + "Arm"
	g.pf("%sfinal %s = %s.armTag;\n", ind, arm, expr)
	g.pf("%sif (%s < 0 || %s > %d) return false;\n", ind, arm, arm, len(un.Variants))
	switch tag {
	case 1:
		g.pf("%sout?.setUint8(at + %d, %s);\n", ind, offset, arm)
	default:
		g.pf("%sout?.setUint%d(at + %d, %s, order);\n", ind, tag*8, offset, arm)
	}
	g.pf("%sswitch (%s) {\n", ind, arm)
	for i, v := range un.Variants {
		if v.F == nil {
			continue
		}
		g.pf("%s  case %d:\n", ind, i+1)
		g.emitCookForm1Field(v.F, offset+armOffset, expr+"."+armMember(v), ind+"    ")
	}
	g.pf("%s}\n", ind)
}

// queueForm1Union queues a union's element writer, once.
func (g *cookGen) queueForm1Union(un *ir.Union) {
	if g.form1UnionsSeen == nil {
		g.form1UnionsSeen = map[*ir.Union]bool{}
	}
	if !g.form1UnionsSeen[un] {
		g.form1UnionsSeen[un] = true
		g.form1Unions = append(g.form1Unions, un)
	}
}

// emitCookForm1UnionElement writes one union array element where it lies,
// through the union's element writer, and queues that writer (§2.6, §7.2).
func (g *cookGen) emitCookForm1UnionElement(un *ir.Union, expr, addr, ind string) {
	g.queueForm1Union(un)
	if line := fmt.Sprintf("%sif (!_cook%s(%s, c, %s)) return false;", ind, un.Name, expr, addr); len(line) <= 80 {
		g.pf("%s\n", line)
	} else if head := fmt.Sprintf("%sif (!_cook%s(%s, c, %s))", ind, un.Name, expr, addr); len(head) <= 80 {
		g.pf("%s\n%s  return false;\n", head, ind)
	} else {
		g.pf("%sif (!_cook%s(\n%s  %s,\n%s  c,\n%s  %s,\n%s))\n%s  return false;\n", ind, un.Name, ind, expr, ind, ind, addr, ind, ind)
	}
}

// emitCookForm1UnionWriters writes the element writer of every union the
// form-1 cook met as an array element: the union's storage at `at`, the tag
// and the set arm exactly as a union field's (§2.6, §7.2). A writer's arms
// may queue another union, so the queue drains until it is empty. The name
// `_cook<Union>` cannot meet a record writer's `_cook<Table>`: a union and
// a table share the unit's one namespace of type names.
func (g *cookGen) emitCookForm1UnionWriters() {
	for i := 0; i < len(g.form1Unions); i++ {
		un := g.form1Unions[i]
		outer := g.body
		g.body = strings.Builder{}
		elem := &ir.Field{Name: "value"}
		elem.Type.Kind, elem.Type.Ref = ir.TNamed, un
		g.emitCookForm1Union(elem, un, 0, "value", "    ")
		fields := g.body.String()
		g.body = outer
		g.pf("  // One %s array element where it lies (§2.6, §7.2).\n", un.Name)
		if sig := fmt.Sprintf("  static bool _cook%s(wire.%s value, TableCookGraph c, int at) {", un.Name, un.Name); len(sig) <= 80 {
			g.pf("%s\n", sig)
		} else {
			g.pf("  static bool _cook%s(\n    wire.%s value,\n    TableCookGraph c,\n    int at,\n  ) {\n", un.Name, un.Name)
		}
		if cookUsesOut.MatchString(fields) {
			g.pf("    final out = c.out;\n")
		}
		if cookUsesOrder.MatchString(fields) {
			g.pf("    final order = c.order;\n")
		}
		g.pf("%s", fields)
		g.pf("    return true;\n  }\n\n")
	}
	g.form1Unions, g.form1UnionsSeen = nil, nil
}

func lastDot(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}
