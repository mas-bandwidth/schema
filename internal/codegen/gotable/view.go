package gotable

import (
	"fmt"
	"maps"
	"slices"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The optional view file is a leaf: nothing in the table codecs refers to it.
// Outside packet fields must describe actual packet storage. In a variable
// unit that can differ from a reached type's POD Row, so the view carries
// private packet descriptors for their dependencies, without adding codecs.
func generateViewFile(u *ir.Unit, closure map[string]bool, arms map[string]int) (map[string][]byte, error) {
	base := ir.GoExportName(u.Package) + "View"
	types := slices.Sorted(maps.Keys(u.Structs))
	tables := slices.Sorted(maps.Keys(u.Tables))
	unions := slices.Sorted(maps.Keys(u.Unions))
	g := &tableGen{unit: u, file: &ir.File{Base: base}, viewPacket: map[string]int{}, viewUnions: map[string]int{}, unionArmSlot: arms}
	for i, n := range types {
		g.viewPacket[n] = i
	}
	for i, n := range unions {
		g.viewUnions[n] = i
	}
	g.pf("%s", tableViewSource)
	if len(types) > 0 {
		g.needsUnsafe()
	}
	g.pf("var tableViewPacketTypes [%d]TableTypeInfo\nvar tableViewPacketUnions [%d]TableUnionInfo\n", len(types), len(unions))
	g.pf("func init(){\n")
	for _, n := range types {
		st := u.Structs[n]
		g.viewNoIds = !closure[n]
		g.pf("tableViewPacketTypes[%d]=TableTypeInfo{Name:%q,Size:uint32(unsafe.Sizeof(%s{})),NumFields:%d,Reset:func(p unsafe.Pointer){*(*%s)(p)=%s},%s,Fields:[]TableFieldInfo{\n", g.viewPacket[n], n, n, len(st.Fields), n, viewPacketDefault(st), viewAnnotation(st.Doc, st.Tags))
		guards := tableGuardStrings(st)
		for _, f := range st.Fields {
			g.emitTableFieldDescriptor(st, f, guards[f.Name])
		}
		g.pf("}}\n")
	}
	reached := ir.TableClosureVocabulary(u)
	for _, n := range unions {
		un := u.Unions[n]
		g.viewNoIds = !reached[n]
		g.pf("tableViewPacketUnions[%d]=TableUnionInfo{TagOffset:uint32(unsafe.Offsetof(%s{}.Type)),TagSize:uint32(unsafe.Sizeof(%s{}.Type)),Arms:[]TableUnionArmInfo{{Void:true},\n", g.viewUnions[n], n, n)
		for _, arm := range un.Variants {
			if arm.Void() {
				g.pf("{Void:true},\n")
				continue
			}
			f := *arm.F
			f.Name = arm.Name
			g.pf("{Offset:uint32(unsafe.Offsetof(%s{}.%s)),Table:func()*TableTypeInfo{return &tableViewPacketTypes[%d]},Reset:func(p unsafe.Pointer){(*%s)(p).%s=%s},Field:TableFieldInfo", n, member(&f), g.viewPacket[arm.Type], n, member(&f), viewPacketDefault(arm.Ref))
			g.emitTableFieldDescriptor(&ir.Struct{Name: n}, &f, "")
			g.pf("},\n")
		}
		g.pf("}}\n")
	}
	g.pf("}\n")
	for _, n := range types {
		if !closure[n] {
			g.pf("func %sReset(value *%s){*value=%s}\nfunc %sTableType()*TableTypeInfo{return &tableViewPacketTypes[%d]}\n", n, n, viewPacketDefault(u.Structs[n]), n, g.viewPacket[n])
		}
	}
	g.pf("var tableUnitView = UnitViewInfo{Package:%q,ProtocolId:0x%016x,\n", u.Package, u.ProtocolId)
	for _, set := range []struct {
		name  string
		names []string
		table bool
	}{{"Types", types, false}, {"Tables", tables, true}} {
		count := 0
		for _, n := range set.names {
			if set.table && u.Tables[n].IsMapEntry() {
				continue
			}
			count++
		}
		g.pf("Num%s:%d,%s:[]ViewType{\n", set.name, count, set.name)
		for _, n := range set.names {
			st := u.Structs[n]
			if set.table {
				st = u.Tables[n]
			}
			if st.IsMapEntry() {
				continue
			}
			ref := "&" + n + "TableInfo"
			if !closure[n] {
				ref = fmt.Sprintf("&tableViewPacketTypes[%d]", g.viewPacket[n])
			}
			g.pf("{Name:%q,File:%q,Table:%v,Type:%s,%s},\n", n, viewFile(u, n), set.table, ref, viewAnnotation(st.Doc, st.Tags))
		}
		g.pf("},\n")
	}
	for _, set := range []string{"Enums", "Flags", "Unions"} {
		var names []string
		switch set {
		case "Enums":
			names = slices.Sorted(maps.Keys(u.Enums))
		case "Flags":
			names = slices.Sorted(maps.Keys(u.Flags))
		default:
			names = append(slices.Sorted(maps.Keys(u.Unions)), slices.Sorted(maps.Keys(u.TableUnions))...)
			slices.Sort(names)
		}
		g.pf("Num%s:%d,%s:[]ViewVocabulary{\n", set, len(names), set)
		for _, n := range names {
			var doc string
			var tags []string
			var max int64
			var bits, count int
			switch set {
			case "Enums":
				e := u.Enums[n]
				doc, tags, max, bits, count = e.Doc, e.Tags, e.Max, e.StorageBits, len(e.Variants)+1
			case "Flags":
				f := u.Flags[n]
				doc, tags, max, count = f.Doc, f.Tags, int64(len(f.Variants)-1), len(f.Variants)
				bits = 64
				for _, width := range []int{8, 16, 32, 64} {
					if f.WireBits <= width {
						bits = width
						break
					}
				}
			default:
				un := u.Unions[n]
				if un == nil {
					un = u.TableUnions[n]
				}
				doc, tags, max, bits, count = un.Doc, un.Tags, un.Max, un.StorageBits, len(un.Variants)+1
			}
			extra := ""
			if un := u.TableUnions[n]; un != nil {
				for _, arm := range un.Variants {
					if !arm.Void() {
						g.needsUnsafe()
						extra = fmt.Sprintf("PayloadOffset:uint32(unsafe.Offsetof(%sRow{}.Payload)),", n)
						break
					}
				}
			}
			g.pf("{Name:%q,File:%q,Max:%d,StorageBits:%d,NumVariants:%d,%s%s,Variants:[]ViewVariant{\n", n, viewFile(u, n), max, bits, count, extra, viewAnnotation(doc, tags))
			if set != "Flags" {
				g.pf("{Name:\"None\",%s},\n", viewAnnotation("", nil))
			}
			for i := 0; i < count; i++ {
				if set != "Flags" && i == count-1 {
					break
				}
				id := uint64(0)
				var variant, doc, extra string
				var tags []string
				value := i + 1
				switch set {
				case "Enums":
					e := u.Enums[n]
					variant, doc, tags = e.Variants[i], e.VariantDocs[i], e.VariantTags[i]
					if reached[n] {
						id = ir.TableWireId(e.VariantWireName(i))
					}
				case "Flags":
					f := u.Flags[n]
					value = i
					variant, doc, tags = f.Variants[i], f.VariantDocs[i], f.VariantTags[i]
				default:
					un := u.Unions[n]
					if un == nil {
						un = u.TableUnions[n]
					}
					arm := un.Variants[i]
					variant, doc, tags = arm.Name, arm.Doc, arm.Tags
					if reached[n] {
						id = ir.TableWireId(arm.WireName())
					}
					if !arm.Void() {
						extra = fmt.Sprintf("PayloadName:%q,", ir.FieldTypeSpelling(arm.F))
						if arm.Body() {
							extra += fmt.Sprintf("Payload:%sTableType,", arm.Type)
						} else {
							// The checker permits field-shaped arms only in a table closure.
							extra += fmt.Sprintf("Field:func()*TableFieldInfo{return &tableUnionArms[%d].Arms[%d].Field},", arms[n], i+1)
						}
					}
				}
				g.pf("{Value:%d,Name:%q,Id:0x%016x,%s%s},\n", value, variant, id, extra, viewAnnotation(doc, tags))
			}
			g.pf("}},\n")
		}
		g.pf("},\n")
	}
	constants := slices.Sorted(maps.Keys(u.Consts))
	g.pf("NumConstants:%d,Constants:[]ViewConstant{\n", len(constants))
	for _, n := range constants {
		c := u.Consts[n]
		integer := int64(0)
		real := "0.0"
		if c.IsFloat {
			real = formatFloat64(c.Float)
		} else if c.Int != nil {
			integer = int64(c.Int.Uint64())
		}
		g.pf("{Name:%q,File:%q,TypeName:%q,IsFloat:%v,IntValue:%d,FloatValue:%s,%s},\n", n, viewFile(u, n), c.Storage, c.IsFloat, integer, real, viewAnnotation(c.Doc, c.Tags))
	}
	g.pf("}}\nfunc UnitView()*UnitViewInfo{return &tableUnitView}\n")
	src, err := g.assemble()
	if err != nil {
		return nil, err
	}
	return map[string][]byte{base + ".go": src}, nil
}

func viewFile(u *ir.Unit, n string) string {
	if f, ok := u.DeclFile[n]; ok {
		return f + ".schema"
	}
	return ""
}
func viewAnnotation(doc string, tags []string) string {
	text := "TableDocNone"
	if doc != "" {
		text = ir.QuoteDoc(doc)
	}
	list := "nil"
	if len(tags) > 0 {
		list = "[]string{" + ir.QuotedTags(tags) + "}"
	}
	return fmt.Sprintf("Doc:%s,NumTags:%d,Tags:%s", text, len(tags), list)
}
func viewPacketDefault(st *ir.Struct) string {
	seen := map[string]bool{}
	var specified func(*ir.Struct) bool
	specified = func(s *ir.Struct) bool {
		if seen[s.Name] {
			return false
		}
		seen[s.Name] = true
		for _, f := range s.Fields {
			if f.HasDefault || f.BornCount() > 0 {
				return true
			}
			if inner, ok := f.Type.Ref.(*ir.Struct); ok && specified(inner) {
				return true
			}
		}
		return false
	}
	if specified(st) {
		return "New" + st.Name + "()"
	}
	return st.Name + "{}"
}

const tableViewSource = `// UnitView returns this build's registry. Treat its descriptors and slices as
// immutable; lookups allocate nothing. Packet-only types gain no table codec.
// A packet dependency can use different storage from its table Row; its
// descriptor follows that storage, so every offset remains valid for the value.
type ViewConstant struct {Name,File,TypeName string;IsFloat bool;IntValue int64;FloatValue float64;Doc string;NumTags int32;Tags []string}
type ViewVariant struct {Value uint64;Name string;Id uint64;PayloadName string;Payload func()*TableTypeInfo;Field func()*TableFieldInfo;Doc string;NumTags int32;Tags []string}
type ViewVocabulary struct {Name,File string;Max int64;StorageBits,NumVariants int32;PayloadOffset uint32;Variants []ViewVariant;Doc string;NumTags int32;Tags []string}
type ViewType struct {Name,File string;Table bool;Type *TableTypeInfo;Doc string;NumTags int32;Tags []string}
type UnitViewInfo struct {Package string;ProtocolId uint64;NumTypes,NumTables,NumEnums,NumFlags,NumUnions,NumConstants int32;Types,Tables []ViewType;Enums,Flags,Unions []ViewVocabulary;Constants []ViewConstant}
`
