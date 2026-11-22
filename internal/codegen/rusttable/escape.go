package rusttable

import (
	"maps"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// rustEscapeTables is the Rust escape (SPEC.md §4.6, the Go escape's twin): a
// DECLARED table whose name is a standard-library name the generated modules
// spell unqualified (rustStdShadowed: `fixed table Vec`) is emitted under
// rustEscape's spelling, so its storage struct (`VecTable`) and every name
// derived from it (`VecTableRow`, `vec_table_load`) no longer shadow std's
// `Vec` in a crate that glob-exports every module. It returns a COPY of the
// unit with those tables respelled and every reference following, the
// declared name kept as SchemaName (ir.Struct.DeclaredName), which every wire
// id reads through WireName, so the wire, the fixed form's layout and every
// id are the unescaped unit's own. The caller's unit is never touched, since
// another target may emit it next, and a unit with no such table is returned
// as it is, so its output is byte-identical.
//
// A `type` keeps its name: the packet backend declares it too, so a `type`
// by a standard name stays off the variable wire and generation refuses it
// by name (variablerefusal.go, docs/SPEC-TABLES.md §11).
func rustEscapeTables(u *ir.Unit) *ir.Unit {
	rename := map[string]string{}
	taken := func(n string) bool {
		_, t := u.Tables[n]
		_, s := u.Structs[n]
		_, e := u.Enums[n]
		_, un := u.Unions[n]
		return t || s || e || un
	}
	for name, st := range u.Tables {
		if st.MapEntryOf == "" && !st.TextElem && rustStdShadowed[name] {
			to := rustEscape(name)
			for taken(to) {
				to = rustEscape(to)
			}
			rename[name] = to
		}
	}
	if len(rename) == 0 {
		return u
	}
	structs := map[*ir.Struct]*ir.Struct{}
	unions := map[*ir.Union]*ir.Union{}
	fields := map[*ir.Field]*ir.Field{}
	var copyStruct func(*ir.Struct) *ir.Struct
	var copyUnion func(*ir.Union) *ir.Union
	var copyField func(*ir.Field) *ir.Field
	var copyItems func([]ir.Item) []ir.Item
	respell := func(name string) string {
		if to, ok := rename[name]; ok {
			return to
		}
		return name
	}
	copyStruct = func(st *ir.Struct) *ir.Struct {
		if st == nil {
			return nil
		}
		if c, ok := structs[st]; ok {
			return c
		}
		c := *st
		structs[st] = &c
		if to, ok := rename[st.Name]; ok && st.IsTable && u.Tables[st.Name] == st {
			c.SchemaName, c.Name = st.Name, to
		}
		c.Fields = make([]*ir.Field, len(st.Fields))
		for i, f := range st.Fields {
			c.Fields[i] = copyField(f)
		}
		c.Items = copyItems(st.Items)
		return &c
	}
	copyUnion = func(un *ir.Union) *ir.Union {
		if un == nil {
			return nil
		}
		if c, ok := unions[un]; ok {
			return c
		}
		c := *un
		unions[un] = &c
		c.Variants = make([]ir.UnionVariant, len(un.Variants))
		for i, v := range un.Variants {
			v.Ref = copyStruct(v.Ref)
			v.F = copyField(v.F)
			v.Type = respell(v.Type)
			c.Variants[i] = v
		}
		return &c
	}
	copyField = func(f *ir.Field) *ir.Field {
		if f == nil {
			return nil
		}
		if c, ok := fields[f]; ok {
			return c
		}
		c := *f
		fields[f] = &c
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			c.Type.Ref = copyStruct(ref)
		case *ir.Union:
			c.Type.Ref = copyUnion(ref)
		}
		if f.Type.Kind == ir.TNamed {
			c.Type.Name = respell(f.Type.Name)
		}
		c.MapEntry = copyStruct(f.MapEntry)
		return &c
	}
	copyItems = func(items []ir.Item) []ir.Item {
		if items == nil {
			return nil
		}
		out := make([]ir.Item, len(items))
		for i, it := range items {
			switch it := it.(type) {
			case *ir.FieldItem:
				out[i] = &ir.FieldItem{F: copyField(it.F)}
			case *ir.Branch:
				b := *it
				b.Then = copyItems(it.Then)
				b.Else = copyItems(it.Else)
				out[i] = &b
			default:
				out[i] = it // const, reserved, align: no name, never written
			}
		}
		return out
	}
	copyDecl := func(d ir.Decl) ir.Decl {
		switch d := d.(type) {
		case *ir.Struct:
			return copyStruct(d)
		case *ir.Union:
			return copyUnion(d)
		}
		return d
	}

	c := *u
	c.Structs = map[string]*ir.Struct{}
	for k, st := range u.Structs {
		c.Structs[k] = copyStruct(st)
	}
	c.Tables = map[string]*ir.Struct{}
	for k, st := range u.Tables {
		c.Tables[respell(k)] = copyStruct(st)
	}
	c.Unions = map[string]*ir.Union{}
	for k, un := range u.Unions {
		c.Unions[k] = copyUnion(un)
	}
	c.TableUnions = map[string]*ir.Union{}
	for k, un := range u.TableUnions {
		c.TableUnions[k] = copyUnion(un)
	}
	c.DeclFile = map[string]string{}
	for k, base := range u.DeclFile {
		c.DeclFile[respell(k)] = base
	}
	c.Files = make([]*ir.File, len(u.Files))
	for i, f := range u.Files {
		cf := *f
		cf.Decls = make([]ir.Decl, len(f.Decls))
		for j, d := range f.Decls {
			cf.Decls[j] = copyDecl(d)
		}
		cf.Tables = make([]*ir.Struct, len(f.Tables))
		for j, st := range f.Tables {
			cf.Tables[j] = copyStruct(st)
		}
		cf.TableUnions = make([]*ir.Union, len(f.TableUnions))
		for j, un := range f.TableUnions {
			cf.TableUnions[j] = copyUnion(un)
		}
		c.Files[i] = &cf
	}
	return &c
}

// rustEscape is the escaped spelling of a table named after a standard name.
func rustEscape(name string) string { return name + "Table" }

// rustEscapeLineage keys a lineage by the escaped unit's table names: the
// lock names a table by its schema name.
func rustEscapeLineage(esc *ir.Unit, lineage map[string][]FixedLineageEntry) map[string][]FixedLineageEntry {
	if lineage == nil {
		return nil
	}
	out := make(map[string][]FixedLineageEntry, len(lineage))
	maps.Copy(out, lineage)
	for name, st := range esc.Tables {
		if st.SchemaName == "" {
			continue
		}
		if v, ok := lineage[st.SchemaName]; ok {
			delete(out, st.SchemaName)
			out[name] = v
		}
	}
	return out
}
