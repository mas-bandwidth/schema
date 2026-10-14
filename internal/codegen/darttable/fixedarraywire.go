package darttable

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Positional arrays share kind 14. Enum-keyed arrays have a different layout.
func wirePositionalArray(f *ir.Field) bool {
	return f.CountedOnWire() || f.Array == ir.ArrayFixed && f.KeyEnum == ""
}

func (g *wireGen) wireElementDefault(f *ir.Field) string {
	switch {
	case f.Type.Pointer:
		return "null"
	case tableRef(f) != nil:
		return f.Type.Name + "()"
	case enumRef(f) != nil:
		return g.enumDefault(f)
	case f.Type.Kind == ir.TBool:
		return fmt.Sprint(f.HasDefault && f.DefBool)
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		return g.floatDefault(f)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		return g.stringDefault(f)
	default:
		return g.intDefault(f)
	}
}

func (g *wireGen) emitFixedArrayStorage(f *ir.Field, ind string) {
	typ, def := dartListElemType(f), g.wireElementDefault(f)
	if tableRef(f) != nil && !f.Type.Pointer {
		g.pf("%sList<%s> %s = List<%s>.generate(%d, (_) => %s);\n", ind, typ, fieldMember(f), typ, f.ArrayBound, def)
	} else {
		g.pf("%sList<%s> %s = List<%s>.filled(%d, %s);\n", ind, typ, fieldMember(f), typ, f.ArrayBound, def)
	}
}

// The caller owns optional presence. Decide elision before interning any ids.
// Fixed table elements always ride: their position is identity (§3).
func (g *wireGen) emitSaveFixedArray(f *ir.Field, expr, ind string, id uint64, ordinal int) {
	g.pf("%sif (%s.length != %d) throw StateError('invalid_value');\n", ind, expr, f.ArrayBound)
	ride := "true"
	if !f.Type.Optional && (tableRef(f) == nil || f.Type.Pointer) {
		ride = fmt.Sprintf("%s.any((e) => e != %s)", expr, g.wireElementDefault(f))
	}
	g.pf("%sif (%s) {\n", ind, ride)
	// Existing list encoders supply the common framing and first-use ordering.
	// Force the inner writer to ride even at zero slots.
	whole := *f
	whole.Type.Optional = true
	switch {
	case f.Type.Pointer && tableRef(f) != nil:
		g.emitSavePointerList(&whole, expr, ind+"  ", id)
	case tableRef(f) != nil:
		g.emitSaveTableList(&whole, expr, ind+"  ", id, ordinal)
	default:
		g.emitSaveScalarList(&whole, expr, ind+"  ", id)
	}
	g.pf("%s}\n", ind)
}

// The caller has emitted the case label and optional-presence preflight.
// Commit a fresh padded value only after the array header is accepted. Move
// the parent to L's end before decoding so damaged children cannot escape it.
func (g *wireGen) emitLoadFixedArray(f *ir.Field, ind string, ordinal int) {
	n, typ, def, elem := fieldMember(f), dartListElemType(f), g.wireElementDefault(f), listElementKind(f)
	g.pf("%sif(kind!=14){r.report.kindMismatch++;if(!r.skipKind(kind)){r.report.malformed=true;return;}break;}\n", ind)
	g.pf("%s{\n%s final length=r.readLeb();\n%s if(length==null||!r.has(length)){r.report.malformed=true;return;}\n%s final end=r.pos+length;final arr=TableWireReader.sub(r,r.pos,end);r.pos=end;\n%s if(!arr.has(2)){r.report.malformed=true;break;}\n%s final elemKind=arr.readByte();final count=arr.readLeb();\n%s if(count==null||count<0){r.report.malformed=true;break;}\n", ind, ind, ind, ind, ind, ind, ind)
	g.pf("%s if(elemKind!=%d){if(!_kindWidens(elemKind,%d)){r.report.kindMismatch++;break;}r.report.widened++;}\n", ind, elem, elem)
	g.pf("%s final values=<%s>[];\n%s final keep=count>%d?%d:count;if(count!=keep)r.report.clamped++;\n%s for(var i=0;i<keep;i++){\n", ind, typ, ind, f.ArrayBound, f.ArrayBound, ind)
	switch elem {
	case wireKindTable:
		g.pf("%s  final size=arr.readLeb();if(size==null||!arr.has(size)){r.report.malformed=true;break;}\n%s  var e=%s;final body=TableWireReader.sub(arr,arr.pos,arr.pos+size);arr.pos+=size;\n%s  body.path=[...r.path,%d,i];%sWireLoadBody(body,e,true);\n%s  if(body.pos!=body.limit){r.report.malformed=true;e=%s;}values.add(e);\n", ind, ind, def, ind, ordinal, dartName(f.Type.Name), ind, def)
	case wireKindPtr:
		g.pf("%s  final idx=arr.readLeb();if(idx==null){r.report.malformed=true;break;}values.add(r.resolveTable(idx,%s) as %s);\n", ind, typeConst(tableRef(f)), typ)
	default:
		g.emitLoadScalarListElem(f, "values", "arr", ind)
	}
	g.pf("%s }\n%s if(count<=%d&&arr.pos!=arr.limit)r.report.malformed=true;\n%s while(values.length<%d){values.add(%s);}\n%s v.%s=values;\n%s}\n%sbreak;\n", ind, ind, f.ArrayBound, ind, f.ArrayBound, def, ind, n, ind, ind)
}
