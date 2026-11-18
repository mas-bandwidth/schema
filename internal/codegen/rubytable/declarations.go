package rubytable

import (
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// valueDecls returns a file's constants, enums and flags sets in emission order.
func valueDecls(f *ir.File) []ir.Decl {
	if f == nil {
		return nil
	}
	var out []ir.Decl
	for _, d := range ir.EmissionOrder(f) {
		switch d.(type) {
		case *ir.Const, *ir.Enum, *ir.Flags:
			out = append(out, d)
		}
	}
	return out
}

func closureMember(u *ir.Unit, name string) *ir.Struct {
	if st := u.Tables[name]; st != nil {
		return st
	}
	return u.Structs[name]
}

// tableUnionsReached returns every union (table-side or packet-side) reached
// by the unit's table closure.
func tableUnionsReached(u *ir.Unit) map[string]bool {
	out := map[string]bool{}
	seenStruct := map[string]bool{}
	seenUnion := map[string]bool{}
	var walkStruct func(name string)
	var walkUnion func(un *ir.Union)
	walkUnion = func(un *ir.Union) {
		if un == nil || seenUnion[un.Name] {
			return
		}
		seenUnion[un.Name] = true
		out[un.Name] = true
		for _, v := range un.Variants {
			if v.F == nil || v.F.Type.Kind != ir.TNamed {
				continue
			}
			switch ref := v.F.Type.Ref.(type) {
			case *ir.Struct:
				walkStruct(ref.Name)
			case *ir.Union:
				walkUnion(ref)
			}
		}
	}
	walkStruct = func(name string) {
		if seenStruct[name] {
			return
		}
		seenStruct[name] = true
		st := closureMember(u, name)
		if st == nil {
			return
		}
		for _, f := range st.Fields {
			if f.IsMap() {
				walkStruct(f.MapEntry.Name)
				continue
			}
			if f.Type.Kind != ir.TNamed {
				continue
			}
			switch ref := f.Type.Ref.(type) {
			case *ir.Struct:
				walkStruct(ref.Name)
			case *ir.Union:
				walkUnion(ref)
			}
		}
	}
	names := make([]string, 0, len(u.Tables))
	for name := range u.Tables {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		walkStruct(name)
	}
	return out
}

type filePlan struct {
	base         string
	tables       []*ir.Struct
	unions       []*ir.Union
	packets      []*ir.Struct
	packetUnions []*ir.Union
}

func filePlans(u *ir.Unit) []*filePlan {
	tablesByFile, unionsByFile := tableFiles(u)
	byBase := map[string]*filePlan{}
	get := func(base string) *filePlan {
		plan := byBase[base]
		if plan == nil {
			plan = &filePlan{base: base}
			byBase[base] = plan
		}
		return plan
	}
	for base, tables := range tablesByFile {
		get(base).tables = tables
	}
	for base, unions := range unionsByFile {
		get(base).unions = unions
	}
	for _, f := range u.Files {
		if len(valueDecls(f)) > 0 {
			get(f.Base)
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ir.Struct:
				if !d.IsTable {
					plan := get(f.Base)
					plan.packets = append(plan.packets, d)
				}
			case *ir.Union:
				plan := get(f.Base)
				plan.packetUnions = append(plan.packetUnions, d)
			}
		}
	}
	plans := make([]*filePlan, 0, len(byBase))
	for _, plan := range byBase {
		plans = append(plans, plan)
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].base < plans[j].base })
	return plans
}

func fileFor(u *ir.Unit, base string) *ir.File {
	for _, f := range u.Files {
		if f.Base == base {
			return f
		}
	}
	return &ir.File{Base: base}
}

func tableFiles(u *ir.Unit) (map[string][]*ir.Struct, map[string][]*ir.Union) {
	tables := map[string][]*ir.Struct{}
	unions := map[string][]*ir.Union{}
	seenTable := map[string]bool{}
	seenUnion := map[string]bool{}
	for _, f := range u.Files {
		for _, st := range f.Tables {
			tables[f.Base] = append(tables[f.Base], st)
			seenTable[st.Name] = true
		}
		for _, un := range f.TableUnions {
			unions[f.Base] = append(unions[f.Base], un)
			seenUnion[un.Name] = true
		}
	}
	var tableOrphans []*ir.Struct
	names := make([]string, 0, len(ir.TableClosure(u)))
	for name := range ir.TableClosure(u) {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if seenTable[name] {
			continue
		}
		if st := closureMember(u, name); st != nil && st.IsTable {
			tableOrphans = append(tableOrphans, st)
		}
	}
	if len(tableOrphans) > 0 {
		tables[moduleBase(u)] = append(tables[moduleBase(u)], tableOrphans...)
	}
	var unionOrphans []*ir.Union
	unionNames := make([]string, 0, len(u.TableUnions))
	for name := range u.TableUnions {
		unionNames = append(unionNames, name)
	}
	sort.Strings(unionNames)
	for _, name := range unionNames {
		if seenUnion[name] {
			continue
		}
		unionOrphans = append(unionOrphans, u.TableUnions[name])
	}
	if len(unionOrphans) > 0 {
		unions[moduleBase(u)] = append(unions[moduleBase(u)], unionOrphans...)
	}
	return tables, unions
}

func moduleBase(u *ir.Unit) string {
	if u.Package == "" {
		return "Tables"
	}
	return strings.ToUpper(u.Package[:1]) + u.Package[1:] + "Tables"
}

func orderUnions(unions []*ir.Union) []*ir.Union {
	byName := make(map[string]*ir.Union, len(unions))
	for _, un := range unions {
		byName[un.Name] = un
	}
	sorted := append([]*ir.Union(nil), unions...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	out := make([]*ir.Union, 0, len(sorted))
	done := map[string]bool{}
	active := map[string]bool{}
	var emit func(un *ir.Union)
	emit = func(un *ir.Union) {
		if done[un.Name] || active[un.Name] {
			return
		}
		active[un.Name] = true
		for _, v := range un.Variants {
			if v.F == nil {
				continue
			}
			if ref, ok := v.F.Type.Ref.(*ir.Union); ok && byName[ref.Name] != nil {
				emit(byName[ref.Name])
			}
		}
		active[un.Name] = false
		done[un.Name] = true
		out = append(out, un)
	}
	for _, un := range sorted {
		emit(un)
	}
	return out
}

func orderStructs(structs []*ir.Struct) []*ir.Struct {
	byName := make(map[string]*ir.Struct, len(structs))
	for _, st := range structs {
		byName[st.Name] = st
	}
	out := make([]*ir.Struct, 0, len(structs))
	done := map[string]bool{}
	active := map[string]bool{}
	var emit func(st *ir.Struct)
	emit = func(st *ir.Struct) {
		if done[st.Name] || active[st.Name] {
			return
		}
		active[st.Name] = true
		for _, f := range st.Fields {
			if f.Type.Pointer {
				continue
			}
			if f.MapEntry != nil && byName[f.MapEntry.Name] != nil {
				emit(byName[f.MapEntry.Name])
			}
			if ref, ok := f.Type.Ref.(*ir.Struct); ok && byName[ref.Name] != nil {
				emit(byName[ref.Name])
			}
		}
		active[st.Name] = false
		done[st.Name] = true
		out = append(out, st)
	}
	for _, st := range structs {
		emit(st)
	}
	return out
}
