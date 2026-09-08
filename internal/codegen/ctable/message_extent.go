package ctable

import (
	"fmt"
	"slices"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func (g *tableGen) emitMessageExtentDeclarations(members []*ir.Struct, unions []*ir.Union) {
	for _, st := range members {
		g.pf("static SCHEMA_UNUSED int %s(TableMessageReader *,int64_t *);\n", g.sym(st.Name, "message_extent"))
	}
	for _, un := range unions {
		g.pf("static SCHEMA_UNUSED int %s(TableMessageReader *,int64_t *);\n", g.sym(un.Name, "message_extent"))
	}
}

func (g *tableGen) emitMessageExtent(st *ir.Struct) {
	g.pf("static SCHEMA_UNUSED int %s(TableMessageReader * r,int64_t * at)\n{\n (void)at;for(;;){const TableMessageEntry * entry;if(!table_message_ref(r,&entry,0))return 0;if(entry==NULL)return 1;\n switch(entry->id){\n", g.sym(st.Name, "message_extent"))
	step := 0
	for _, f := range st.Fields {
		if !g.messageFieldHasExtent(f) {
			continue
		}
		e := ir.TableFieldEntry(f)
		g.pf(" case UINT64_C(0x%016x):if(entry->kind==%d && (entry->elem_kind==%d || table_kind_widens(entry->elem_kind,%d))){\n", e.Id, e.Kind, e.Shape.Elem, e.Shape.Elem)
		g.messageExtentValue(f, "entry", " ", &step)
		g.pf(" continue;}break;\n")
	}
	g.pf(" default:break;}if(!table_message_skip(r,entry))return 0;\n }\n}\n")
}

func (g *tableGen) emitMessageUnionExtent(un *ir.Union) {
	g.pf("static SCHEMA_UNUSED int %s(TableMessageReader * r,int64_t * at)\n{\n const TableMessageEntry * entry;(void)at;if(!table_message_ref(r,&entry,0))return 0;if(entry==NULL)return 1;if(entry->kind==0)return 0;\n switch(entry->id){\n", g.sym(un.Name, "message_extent"))
	step := 0
	for _, v := range un.Variants {
		if v.Void() || !g.messageFieldHasExtent(v.F) {
			continue
		}
		e := ir.TableArmEntry(v)
		g.pf(" case UINT64_C(0x%016x):if(entry->kind==%d && (entry->elem_kind==%d || table_kind_widens(entry->elem_kind,%d))){\n", e.Id, e.Kind, e.Shape.Elem, e.Shape.Elem)
		g.messageExtentValue(v.F, "entry", " ", &step)
		g.pf(" return 1;}break;\n")
	}
	g.pf(" default:break;}return table_message_skip(r,entry);\n}\n")
}

func (g *tableGen) messageExtentValue(f *ir.Field, e, ind string, step *int) {
	*step++
	label := fmt.Sprintf("extent%d", *step)
	if f.IsList() || f.IsMap() || f.Array != ir.ArrayNone {
		g.pf("%s{uint64_t %s_n,%s_i;TableMessageEntry %s_shape=table_message_element(%s);const TableMessageEntry * %s=&%s_shape;\n", ind, label, label, label, e, label, label)
		g.pf("%s if(!table_message_count(r,%s,&%s_n))return 0;\n", ind, e, label)
		elem := f
		if f.IsList() || f.IsMap() {
			elem = sequenceElement(f)
			g.pf("%s if(%s_n>INT32_MAX || !table_extent_reserve(at,(int64_t)%s_n,sizeof(%s),SCHEMA_TABLE_ALIGNOF(%s))){r->extent_refused=1;return 0;}\n", ind, label, label, g.sequenceType(f), g.sequenceType(f))
		}
		if !g.messageFieldHasExtent(elem) || f.Type.Pointer {
			g.pf("%s if(%s->value_bits>=0 && %s->kind!=16){if(!table_message_skip_run(r,%s_n,%s->value_bits))return 0;}else{\n", ind, label, e, label, label)
		} else {
			g.pf("%s {\n", ind)
		}
		g.pf("%s for(%s_i=0;%s_i<%s_n;%s_i++){\n", ind, label, label, label, label)
		condition := "1"
		if f.KeyEnum != "" {
			g.pf("%s const TableMessageEntry * %s_key;int %s_known=0;if(!table_message_ref(r,&%s_key,1)||%s_key==NULL)return 0;switch(%s_key->id){\n", ind, label, label, label, label, label)
			for i := range f.KeyEnumRef.Variants {
				g.pf("%s case UINT64_C(0x%016x):%s_known=1;break;\n", ind, ir.TableWireId(f.KeyEnumRef.VariantWireName(i)), label)
			}
			g.pf("%s default:break;}\n", ind)
			condition = label + "_known"
		} else if !f.IsList() && !f.IsMap() {
			condition = fmt.Sprintf("%s_i<%d", label, f.ArrayBound)
		}
		g.pf("%s if(%s){\n", ind, condition)
		g.messageExtentElement(elem, label, ind+" ")
		g.pf("%s }else if(!table_message_skip(r,%s))return 0;\n%s } } }\n", ind, label, ind)
		return
	}
	g.messageExtentElement(f, e, ind)
}

func (g *tableGen) messageExtentElement(f *ir.Field, e, ind string) {
	if !f.Type.Pointer {
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			g.pf("%sif(!%s(r,at))return 0;\n", ind, g.sym(ref.Name, "message_extent"))
			return
		case *ir.Union:
			g.pf("%sif(!%s(r,at))return 0;\n", ind, g.sym(ref.Name, "message_extent"))
			return
		}
	}
	g.pf("%sif(!table_message_skip(r,%s))return 0;\n", ind, e)
}

func (g *tableGen) messageHasExtent(st *ir.Struct) bool {
	return slices.ContainsFunc(st.Fields, g.messageFieldHasExtent)
}
func (g *tableGen) messageFieldHasExtent(f *ir.Field) bool {
	if f.IsMap() || f.IsList() {
		return true
	}
	if f.Type.Pointer {
		return false
	}
	switch ref := f.Type.Ref.(type) {
	case *ir.Struct:
		return g.messageHasExtent(ref)
	case *ir.Union:
		for _, v := range ref.Variants {
			if !v.Void() && g.messageFieldHasExtent(v.F) {
				return true
			}
		}
	}
	return false
}
