package jstable

import (
	"github.com/mas-bandwidth/schema/v2/ir"
	"maps"
	"sort"
)

// A table declaration restricts public roots, never the helper layout needed
// by another root's closure. This follows SPEC-TABLES section 2.12.
func formAllowed(u *ir.Unit, form string) bool {
	for _, st := range u.Tables {
		if !st.IsMapEntry() && st.CarriesForm(form) {
			return true
		}
	}
	return false
}

func filterBlocks(b *ir.BlockUnit) *ir.BlockUnit {
	if b == nil {
		return nil
	}
	restricted := false
	for _, bl := range b.Tables {
		if !bl.Table.CarriesForm("block") {
			restricted = true
			break
		}
	}
	if !restricted {
		return b
	}
	out := &ir.BlockUnit{
		Members: map[string]*ir.MemberLayout{},
		Skipped: maps.Clone(b.Skipped),
	}
	var add func(name string)
	add = func(name string) {
		if _, done := out.Members[name]; done {
			return
		}
		ml := b.Members[name]
		if ml == nil {
			return
		}
		out.Members[name] = ml
		for _, fl := range ml.Fields {
			if fl.Field.Type.Kind == ir.TNamed {
				if ref, ok := fl.Field.Type.Ref.(*ir.Struct); ok {
					add(ref.Name)
				}
			}
		}
	}
	for _, bl := range b.Tables {
		if !bl.Table.CarriesForm("block") {
			out.Skipped[bl.Table.Name] = "it omits the block form in its forms declaration"
			continue
		}
		out.Tables = append(out.Tables, bl)
		for _, arr := range bl.Arrays {
			add(arr.ElemName)
		}
		for _, fl := range bl.Projection.Fields {
			if fl.Field.Type.Kind == ir.TNamed {
				if ref, ok := fl.Field.Type.Ref.(*ir.Struct); ok && !ir.BlockOutOfLine(fl.Field) {
					add(ref.Name)
				}
			}
		}
	}
	for name := range out.Members {
		out.Order = append(out.Order, name)
	}
	sort.Strings(out.Order)
	return out
}
