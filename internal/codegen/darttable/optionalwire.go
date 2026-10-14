package darttable

import "github.com/mas-bandwidth/schema/v2/ir"

// Presence replaces content elision on the same framing (§2.3).
func optionalRide(f *ir.Field, otherwise string) string {
	if f.Type.Optional {
		return "true"
	}
	return otherwise
}

func (g *wireGen) optionalWalk(f *ir.Field, ind string) func() {
	if !f.Type.Optional {
		return func() {}
	}
	g.pf("%sif(v.%sPresent){\n", ind, fieldMember(f))
	return func() { g.pf("%s}\n", ind) }
}

// Keep newly carried authoring shapes outside the earlier graph cook slice
// until their write paths have independent cook-oracle gates (§7.6).
func cookGraphHasUncarriedFields(st *ir.Struct) bool {
	seen := map[string]bool{}
	var walk func(*ir.Struct) bool
	walk = func(st *ir.Struct) bool {
		if st == nil || seen[st.Name] {
			return false
		}
		seen[st.Name] = true
		for _, f := range st.Fields {
			if wireWide(f) || unionRef(f) != nil || f.Type.Optional || f.KeyEnum != "" || f.Array == ir.ArrayFixed {
				return true
			}
			if f.IsMap() && walk(f.MapEntry) {
				return true
			}
			if walk(tableRef(f)) {
				return true
			}
		}
		return false
	}
	return walk(st)
}

// Probe only the field framing, with a private cursor and no report events.
// Nested mismatches do not undo the enclosing optional's accepted presence.
func (g *wireGen) emitOptionalReadPresence(f *ir.Field, ind string) {
	if !f.Type.Optional {
		return
	}
	kind := ir.TableWireFieldKind(f)
	g.pf("%s{final frame=TableWireReader.sub(r,r.pos,r.limit);\n", ind)
	if wirePositionalArray(f) {
		elem := listElementKind(f)
		g.pf("%s if(kind==14){final length=frame.readLeb();if(length!=null&&frame.has(length)){if(length<2){v.%sPresent=true;}else{final end=frame.pos+length;final body=TableWireReader.sub(frame,frame.pos,end);final elem=body.readByte();final count=body.readLeb();if(count==null||elem==%d||_kindWidens(elem,%d))v.%sPresent=true;}}}\n", ind, fieldMember(f), elem, elem, fieldMember(f))
	} else if isByValueTable(f) {
		g.pf("%s if(kind==13){final length=frame.readLeb();if(length!=null&&frame.has(length))v.%sPresent=true;}\n", ind, fieldMember(f))
	} else {
		width := ir.TableKindWidth(kind)
		if kind == wireKindEnum {
			g.pf("%s if(kind==30&&frame.readLeb()!=null)v.%sPresent=true;\n", ind, fieldMember(f))
		} else {
			g.pf("%s if((kind==%d||_kindWidens(kind,%d))&&frame.has(kind==%d?%d:(kind==2||kind==6?1:(kind==3||kind==7?2:(kind==4||kind==8||kind==10?4:8)))))v.%sPresent=true;\n", ind, kind, kind, kind, width, fieldMember(f))
		}
	}
	g.pf("%s}\n", ind)
}
