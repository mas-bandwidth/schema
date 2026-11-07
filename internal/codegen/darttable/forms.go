package darttable

import "github.com/mas-bandwidth/schema/v2/ir"

// declaredBlocks selects public roots without removing storage needed by an
// allowed root's nested records. Layouts still come from the shared IR model.
func declaredBlocks(u *ir.Unit) *ir.BlockUnit {
	all := ir.Blocks(u)
	out := &ir.BlockUnit{Members: map[string]*ir.MemberLayout{}, Skipped: map[string]string{}}
	var visit func(string)
	visit = func(name string) {
		if out.Members[name] != nil {
			return
		}
		ml := all.Members[name]
		if ml == nil {
			return
		}
		out.Members[name] = ml
		for _, field := range ml.Fields {
			if child := tableRef(field.Field); child != nil {
				visit(child.Name)
			}
		}
	}
	for _, b := range all.Tables {
		if !b.Table.CarriesForm("block") {
			continue
		}
		out.Tables = append(out.Tables, b)
		for _, field := range b.Projection.Fields {
			if child := tableRef(field.Field); child != nil {
				visit(child.Name)
			}
		}
		for _, arr := range b.Arrays {
			visit(arr.ElemName)
		}
	}
	for name, why := range all.Skipped {
		if u.Tables[name].CarriesForm("block") {
			out.Skipped[name] = why
		}
	}
	for _, name := range all.Order {
		if out.Members[name] != nil {
			out.Order = append(out.Order, name)
		}
	}
	return out
}
