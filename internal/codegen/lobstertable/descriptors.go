// descriptors.go joins the table surface through [Register] and carries the
// REFLECTION side of the descriptors: the unit registry and the type view
// (docs/SPEC-TABLES.md §8.2, §8.3), doc comments and tags (§8.1), the
// deprecated enum variant marker (§2.15) and the enum-keyed array's key
// enum (docs/PORT-LOBSTER.md, THE LOAD). The descriptor data itself stays in
// lobstertable.go; this file adds the annotation and registry rows beside it.
package lobstertable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() { Register("descriptors", emitDescriptors) }

// emitDescriptors writes the unit registry rows, every declaration's doc and
// tags, every variant's own annotation and deprecation marker, and the key
// enum of every enum-keyed array (docs/SPEC-TABLES.md §8.1-§8.3, §2.15).
func emitDescriptors(g *lobster.Gen) error {
	// the runtime file that holds the registry and the deprecated enum read
	have := false
	for _, r := range g.Runtimes {
		have = have || r == "descriptors"
	}
	if !have {
		g.Runtimes = append(g.Runtimes, "descriptors")
	}
	base := g.File.Base
	g.Pf("schema_rt.set_unit(%s, %s)\n", lobster.StringLit([]byte(g.Unit.Package)), idLit(g.Unit.ProtocolId))
	for _, t := range g.File.Tables {
		if t.IsMapEntry() {
			continue
		}
		g.Pf("schema_rt.view_table(%sDescriptor, %s, %s, %s)\n", t.Name, lobster.StringLit([]byte(base)), lobster.StringLit([]byte(t.Doc)), tagList(t.Tags))
		for _, f := range t.Fields {
			fieldAnnotations(g, t.Name, f)
			emitMapEntry(g, t.Name, f)
		}
	}
	for _, un := range g.File.TableUnions {
		for _, v := range un.Variants {
			if v.F != nil {
				fieldAnnotations(g, un.Name, v.F)
			}
		}
	}
	for _, d := range g.File.Decls {
		switch d := d.(type) {
		case *ir.Struct:
			if _, ok := g.Unit.Tables[d.Name]; ok {
				continue
			}
			// a packet type no table reaches still gets a view: its descriptor
			// is emitted here, in this file's table module (§8.2).
			reach := ir.TableClosure(g.Unit)[d.Name]
			if !reach {
				emitDescriptor(g, d)
			}
			g.Pf("schema_rt.view_type(%sDescriptor, %s, %s, %s)\n", d.Name, lobster.StringLit([]byte(base)), lobster.StringLit([]byte(d.Doc)), tagList(d.Tags))
			for _, f := range d.Fields {
				fieldAnnotations(g, d.Name, f)
				emitMapEntry(g, d.Name, f)
			}
		case *ir.Enum:
			emitVocabulary(g, d.Name, base, "enum", d.Doc, d.Tags, d.Variants, d.VariantDocs, d.VariantTags, d.VariantDeprecated, enumIDs(d))
		case *ir.Flags:
			emitVocabulary(g, d.Name, base, "flags", d.Doc, d.Tags, d.Variants, d.VariantDocs, d.VariantTags, nil, nil)
		case *ir.Union:
			if _, ok := g.Unit.TableUnions[d.Name]; ok {
				continue
			}
			emitUnionView(g, d, base)
		case *ir.Const:
			g.Pf("schema_rt.view_constant(%s, %s, %s, %v, %s, %s, %s, %s)\n",
				lobster.StringLit([]byte(d.Name)), lobster.StringLit([]byte(base)),
				lobster.StringLit([]byte(d.Storage)), d.IsFloat,
				constInt(d), constFloat(d),
				lobster.StringLit([]byte(d.Doc)), tagList(d.Tags))
		}
	}
	for _, un := range g.File.TableUnions {
		armDocs := make([]string, len(un.Variants))
		armTags := make([][]string, len(un.Variants))
		dep := make([]bool, len(un.Variants))
		names := make([]string, len(un.Variants))
		for i, v := range un.Variants {
			names[i], armDocs[i], armTags[i] = v.Name, v.Doc, v.Tags
		}
		emitVocabulary(g, un.Name, base, "union", un.Doc, un.Tags, names, armDocs, armTags, dep, nil)
	}
	// the key enum of every enum-keyed array: the descriptor carries the key's
	// variants so the load needs no second registration (§2.4; PORT-LOBSTER,
	// THE LOAD).
	// Every struct of the file is walked, the tables and the types a table
	// reaches alike: a keyed array inside a nested type needs its keys too
	// (folded from sprint vt-optionals' emitOptionals, which registered them
	// a second time).
	keyedOwners := append([]*ir.Struct{}, g.File.Tables...)
	for _, d := range g.File.Decls {
		if st, ok := d.(*ir.Struct); ok {
			if _, isTable := g.Unit.Tables[st.Name]; !isTable {
				keyedOwners = append(keyedOwners, st)
			}
		}
	}
	for _, t := range keyedOwners {
		for _, f := range t.Fields {
			if f.KeyEnumRef == nil {
				continue
			}
			var vs []string
			for i := range f.KeyEnumRef.Variants {
				vs = append(vs, fmt.Sprintf("%s.EnumVariant { name: %s, id: %s }", rt,
					lobster.StringLit([]byte(f.KeyEnumRef.Variants[i])),
					idLit(ir.TableWireId(f.KeyEnumRef.VariantWireName(i)))))
			}
			g.Pf("schema_rt.keyed_keys(%s, %s, [%s]::%s.EnumVariant)\n", lobster.StringLit([]byte(t.Name)), lobster.StringLit([]byte(f.Name)), join(vs), rt)
			g.Pf("schema_rt.keyed_enum(%s, %s, %s)\n", lobster.StringLit([]byte(t.Name)), lobster.StringLit([]byte(f.Name)), lobster.StringLit([]byte(f.KeyEnumRef.Name)))
			emitKeyedAccessor(g, t, f)
		}
	}
	return nil
}

// emitKeyedAccessor writes the KEY ACCESSOR of one enum-keyed array
// (docs/PORTING.md M5; docs/PORT-LOBSTER.md, THE TYPE MAPPING): the array
// stays a plain vector with key k in slot k-1, and three functions take the
// key instead of the slot. <Owner><Field>Slot is the slot a key names, through
// schema_rt.keyed_slot, which refuses None (key 0), a negative key and any key
// past <Enum>Max by name in every build; <Owner><Field>At reads the value slot
// at a key; <Owner><Field>Each yields every key beside its element, so the
// caller never does the shift.
func emitKeyedAccessor(g *lobster.Gen, t *ir.Struct, f *ir.Field) {
	owner := g.TypeRef(t.Name, g.Unit.Tables[t.Name] != nil)
	enum := g.TypeRef(f.KeyEnumRef.Name, false)
	slot := ir.GoExportName(f.Name)
	fn := t.Name + slot
	what := lobster.StringLit([]byte(t.Name + "." + f.Name))
	g.Pf("// %sSlot is the slot of %s.%s that holds key: key 1 is slot 0 and %sMax\n", fn, t.Name, f.Name, enum)
	g.Pf("// the last; None and any key past %sMax are refused by name (docs/PORTING.md M5)\n", enum)
	g.Pf("def %sSlot(key:%s) -> int:\n", fn, enum)
	g.Pf("    return %s.keyed_slot(key, %sMax, %s)\n", rt, enum, what)
	g.Pf("// %sAt is %s.%s's value at key\n", fn, t.Name, f.Name)
	g.Pf("def %sAt(v:%s, key:%s):\n", fn, owner, enum)
	g.Pf("    return v.%s[%sSlot(key)]\n", slot, fn)
	g.Pf("// %sEach calls body(key, element) for every key of %s.%s, in key order\n", fn, t.Name, f.Name)
	g.Pf("def %sEach(v:%s, body):\n", fn, owner)
	g.Pf("    for(v.%s) e, i:\n", slot)
	g.Pf("        body(%s(i + 1), e)\n", enum)
}

func fieldAnnotations(g *lobster.Gen, owner string, f *ir.Field) {
	if f.Doc == "" && len(f.Tags) == 0 {
		return
	}
	g.Pf("schema_rt.field_annotation(%s, %s, %s, %s)\n",
		lobster.StringLit([]byte(owner)), lobster.StringLit([]byte(f.Name)),
		lobster.StringLit([]byte(f.Doc)), tagList(f.Tags))
}

// emitMapEntry names a map field's ENTRY TABLE in the registry, so the
// descriptor's entry table is a fact the reflection surface carries by name
// (docs/SPEC-TABLES.md §2.8, §8.1).
func emitMapEntry(g *lobster.Gen, owner string, f *ir.Field) {
	if !f.IsMap() {
		return
	}
	g.Pf("schema_rt.map_entry(%s, %s, %s)\n",
		lobster.StringLit([]byte(owner)), lobster.StringLit([]byte(f.Name)), lobster.StringLit([]byte(f.MapEntry.Name)))
}

func emitVocabulary(g *lobster.Gen, name, base, kind, doc string, tags []string, variants, docs []string, vtags [][]string, dep []bool, ids []uint64) {
	g.Pf("schema_rt.view_vocabulary(%s, %s, %s, %s, [", lobster.StringLit([]byte(name)), lobster.StringLit([]byte(base)), lobster.StringLit([]byte(doc)), tagList(tags))
	for i, v := range variants {
		d, t := "", []string(nil)
		if i < len(docs) {
			d = docs[i]
		}
		if i < len(vtags) {
			t = vtags[i]
		}
		mark := i < len(dep) && dep[i]
		id := uint64(0)
		if i < len(ids) {
			id = ids[i]
		}
		if i > 0 {
			g.Pf(", ")
		}
		g.Pf("%s.ViewVariant { name: %s, id: %s, deprecated: %v, doc: %s, tags: %s }", rt,
			lobster.StringLit([]byte(v)), idLit(id), mark, lobster.StringLit([]byte(d)), tagList(t))
	}
	g.Pf("]::%s.ViewVariant, %s)\n", rt, lobster.StringLit([]byte(kind)))
	for i, v := range variants {
		if i < len(dep) && dep[i] {
			g.Pf("schema_rt.deprecated_variant(%s, %s)\n", lobster.StringLit([]byte(name)), lobster.StringLit([]byte(v)))
		}
	}
}

func emitUnionView(g *lobster.Gen, un *ir.Union, base string) {
	docs := make([]string, len(un.Variants))
	tags := make([][]string, len(un.Variants))
	names := make([]string, len(un.Variants))
	ids := make([]uint64, len(un.Variants))
	for i, v := range un.Variants {
		names[i], docs[i], tags[i] = v.Name, v.Doc, v.Tags
		ids[i] = ir.TableWireId(v.WireName())
	}
	g.Pf("schema_rt.view_vocabulary(%s, %s, %s, %s, [", lobster.StringLit([]byte(un.Name)), lobster.StringLit([]byte(base)), lobster.StringLit([]byte(un.Doc)), tagList(un.Tags))
	for i, v := range un.Variants {
		if i > 0 {
			g.Pf(", ")
		}
		g.Pf("%s.ViewVariant { name: %s, id: %s, doc: %s, tags: %s }", rt,
			lobster.StringLit([]byte(v.Name)), idLit(ids[i]), lobster.StringLit([]byte(v.Doc)), tagList(v.Tags))
	}
	g.Pf("]::%s.ViewVariant, %s)\n", rt, lobster.StringLit([]byte("union")))
}

func enumIDs(e *ir.Enum) []uint64 {
	out := make([]uint64, len(e.Variants))
	for i := range e.Variants {
		out[i] = ir.TableWireId(e.VariantWireName(i))
	}
	return out
}

func constInt(c *ir.Const) string {
	if c.IsFloat {
		return "0"
	}
	if c.Int == nil {
		return "0"
	}
	if c.Int.IsInt64() {
		return fmt.Sprint(c.Int.Int64())
	}
	return lobster.IntLit(c.Int)
}

func constFloat(c *ir.Const) string {
	if !c.IsFloat {
		return "0.0"
	}
	return lobster.FormatFloat(c.Float)
}

func tagList(tags []string) string {
	out := "[]::string"
	if len(tags) == 0 {
		return out
	}
	parts := make([]string, len(tags))
	for i, t := range tags {
		parts[i] = lobster.StringLit([]byte(t))
	}
	return "[" + join(parts) + "]::string"
}

func join(parts []string) string {
	var out strings.Builder
	for i, p := range parts {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(p)
	}
	return out.String()
}
