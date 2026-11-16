// Package pythontable emits the Python target's TABLE SURFACE
// (docs/SPEC-TABLES.md; the design is docs/PORT-PYTHON.md): one generated
// class per table, with the descriptor a generic walk reads, and the form-1
// runtime that loads, saves and measures the fixed and variable classes.
//
// The runtime lives under the generated package's _schema directory, one
// module per field kind beside one dispatch table (runtime/_schema/kinds.py),
// so a later card adds a kind module and one registration line rather than
// editing the body walk. A field whose type is a packet type is carried by
// value through the packet core's hosted classes (see Generate).
package pythontable

import (
	"embed"
	"fmt"
	"io/fs"
	"math/big"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// RuntimeDir is the runtime subpackage's name inside every generated package.
const RuntimeDir = python.RuntimeDir

//go:embed all:runtime/_schema
var runtimeFS embed.FS

// Generate returns the generated package's files for a unit that declares at
// least one table. A table-free unit generates nothing here; the packet layer
// owns its files.
//
// A table field, array element, union arm or map value whose type is a packet
// type is carried by value on the form-1 table wire through the packet core's
// own generated classes and their serialize-runtime codec (docs/PORT-PYTHON.md,
// THE TABLE FRAME): the packet classes are emitted into the same per-file
// module, the table runtime walks them as kind-13 bodies, and each one a table
// reaches carries the table descriptor that names it; a map value of the type
// rides as its map entry's value field.
// Every packet type and packet union of the unit is emitted, reached by a
// table or not, so nothing the unit declares is dropped. A packet type whose
// own fields use a packet kind the packet wire does not carry yet is refused
// by name, naming the kind and the packet card, never the whole unit silently.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	return generatePythonTables(u, nil)
}

func generatePythonTables(u *ir.Unit, lineage map[string][]FixedLineageEntry) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	if err := python.CheckNames(u); err != nil {
		return nil, err
	}
	if err := refuseUncarriedPacketKinds(u); err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	plans := filePlans(u)
	for _, plan := range plans {
		out[path.Join(u.Package, plan.base+".py")] = []byte(emitModule(u, plan))
	}
	out[path.Join(u.Package, "__init__.py")] = []byte(packageInit(u, plans))
	out[path.Join(u.Package, "py.typed")] = []byte{}
	runtime, err := Runtime()
	if err != nil {
		return nil, err
	}
	for rel, data := range runtime {
		out[path.Join(u.Package, rel)] = data
	}
	// The hosted packet classes carry the packet core's own codec, so the
	// table package ships the packet runtime's serialize.py beside its table
	// runtime. The table runtime's __init__ owns the errors and the shared
	// value checks, so the packet __init__ is not emitted twice.
	packetRuntime, err := python.Runtime()
	if err != nil {
		return nil, err
	}
	for rel, data := range packetRuntime {
		if rel == path.Join(python.RuntimeDir, "__init__.py") {
			continue
		}
		out[path.Join(u.Package, rel)] = data
	}
	fixedData, err := emitPythonFixedData(u, lineage)
	if err != nil {
		return nil, err
	}
	out[path.Join(u.Package, "_fixed.py")] = []byte(fixedData)
	return out, nil
}

// Runtime returns the runtime files keyed by their path inside a generated
// package (_schema/__init__.py, _schema/load.py, ...).
func Runtime() (map[string][]byte, error) {
	out := map[string][]byte{}
	err := fs.WalkDir(runtimeFS, "runtime", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := runtimeFS.ReadFile(p)
		if err != nil {
			return err
		}
		out[strings.TrimPrefix(p, "runtime/")] = data
		return nil
	})
	return out, err
}

// closureUnions is the set of table-closure unions: every union a table
// reaches through a field or an arm, transitively (ir.TableClosure names the
// structs; a union is the other direction of the same walk).
func closureUnions(u *ir.Unit) map[string]bool {
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

// filePlan is one schema file's generated module: the closure's tables and
// table-side unions it declares, and every packet type and packet union it
// declares (hosted from the packet core), whether a table reaches it or not.
// Only the ones a table closure reaches carry a table descriptor.
type filePlan struct {
	base         string
	tables       []*ir.Struct
	unions       []*ir.Union
	packets      []*ir.Struct
	packetUnions []*ir.Union
}

// filePlans groups everything one generated module holds by the schema file
// that declares it.
//
// A packet type or packet union no table reaches is still the unit's
// declaration: it is hosted exactly as the packet leg emits it for a
// packet-only unit, so nothing the unit declares is dropped.
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

// refuseUncarriedPacketKinds refuses the first packet type or packet union the
// unit declares, reached by a table or not, whose own fields use a packet kind
// the packet wire does not carry yet (docs/PORT-PYTHON.md, THE TABLE FRAME).
// Every one of them is hosted, so every one is held to the check. The refusal
// names the type, the field and the kind, and the packet card that carries it.
func refuseUncarriedPacketKinds(u *ir.Unit) error {
	names := make([]string, 0, len(u.Structs))
	for name := range u.Structs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		st := u.Structs[name]
		if st == nil || st.IsTable {
			continue
		}
		if field, kind, ok := packetBlockerStruct(st); ok {
			return packetKindRefusal(st.Name, field, kind)
		}
	}
	unionNames := make([]string, 0, len(u.Unions))
	for name := range u.Unions {
		unionNames = append(unionNames, name)
	}
	sort.Strings(unionNames)
	for _, name := range unionNames {
		for _, v := range u.Unions[name].Variants {
			if v.Void() {
				continue
			}
			if python.FieldKind(v.F) == "" {
				return packetKindRefusal(u.Unions[name].Name, v.Name, python.BlockingKind(v.F))
			}
		}
	}
	return nil
}

func packetKindRefusal(owner, field, kind string) error {
	subject := owner
	if field != "" {
		subject += "." + field
	}
	return fmt.Errorf("python: %s uses the packet kind %s, which the Python packet wire does not carry yet (docs/PORT-PYTHON.md, THE CARD MAP: P4 the packet wire)", subject, kind)
}

func packetBlockerStruct(st *ir.Struct) (field, kind string, ok bool) {
	items := st.Items
	if len(items) == 0 {
		for _, f := range st.Fields {
			items = append(items, &ir.FieldItem{F: f})
		}
	}
	return packetBlockerItems(items)
}

func packetBlockerItems(items []ir.Item) (field, kind string, ok bool) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			if python.FieldKind(item.F) == "" {
				return item.F.Name, python.BlockingKind(item.F), true
			}
		case *ir.Branch:
			if f, k, found := packetBlockerItems(item.Then); found {
				return f, k, true
			}
			if f, k, found := packetBlockerItems(item.Else); found {
				return f, k, true
			}
		case *ir.ConstItem:
			return "", "const", true
		case *ir.ReservedItem:
			return "", "reserved", true
		case *ir.AlignItem:
			return "", "align", true
		}
	}
	return "", "", false
}

func closureMember(u *ir.Unit, name string) *ir.Struct {
	if st := u.Tables[name]; st != nil {
		return st
	}
	return u.Structs[name]
}

// tableFiles groups the closure's tables and table-side unions by the schema
// file that declares them. A generated map entry or text element rides in its
// holder's module; a union reached from another file rides in the module that
// declares it, and an unnamed orphan rides the unit's generated module.
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

func emitModule(u *ir.Unit, plan *filePlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", plan.base)
	fmt.Fprintf(&b, "\"\"\"%s.schema: the generated packet and table classes and their descriptors.\"\"\"\n\n", plan.base)
	b.WriteString("from __future__ import annotations\n\n")

	// Every packet-side declaration of the file is hosted, reached by a table
	// or not: File.Decls holds no table and no table union, so the packet
	// core emits each one exactly as it does for a packet-only unit.
	host := python.NewHost(u, fileFor(u, plan.base))
	host.Emit(func(d ir.Decl) bool {
		st, ok := d.(*ir.Struct)
		return !ok || !st.IsTable
	})
	imports := host.Imports()

	typing := map[string]bool{"ClassVar": true, "Self": true}
	for _, name := range imports.Typing {
		typing[name] = true
	}
	fmt.Fprintf(&b, "from typing import %s\n\n", strings.Join(sortedSet(typing), ", "))
	for _, base := range python.SortedSiblingBases(imports.Siblings) {
		fmt.Fprintf(&b, "from . import %s as %s\n", base, imports.Siblings[base])
	}
	if len(imports.Siblings) > 0 {
		b.WriteString("\n")
	}
	schema := map[string]string{
		"InvalidValue": "InvalidValue",
		"LoadReport":   "LoadReport",
		"Refused":      "Refused",
	}
	for _, name := range imports.Schema {
		// The packet core's shared value checks are private in the table
		// runtime, whose public surface stays the names tablenames.Python
		// registers; the hosted caller aliases them to the packet codec's
		// spelling.
		if local, ok := hostedAlias(name); ok {
			schema[name] = local + " as " + name
			continue
		}
		schema[name] = name
	}
	importsList := make([]string, 0, len(schema))
	for _, name := range sortedSet2(schema) {
		importsList = append(importsList, schema[name])
	}
	fmt.Fprintf(&b, "from ._schema import %s\n", strings.Join(importsList, ", "))
	if len(imports.Serialize) > 0 {
		fmt.Fprintf(&b, "from ._schema.serialize import %s\n", strings.Join(imports.Serialize, ", "))
	}
	b.WriteString("from ._schema import kinds as _s_kinds\n")
	b.WriteString("from ._schema.fixedform import fixed_load as _s_fixed_load, fixed_save as _s_fixed_save, fixed_measure as _s_fixed_measure\n")
	b.WriteString("from ._schema.descriptor import ArmDesc, FieldDesc, TableDesc, UnionDesc, _register_value, new_by_name, new_union_by_name, register, register_union\n")
	b.WriteString("from ._schema.load import Ctx as _LoadCtx, Reader as _LoadReader, _body_end, _id_table, _resolve, _unknown_field, buffer_view, load_table\n")
	b.WriteString("from ._schema.save import Ctx as _SaveCtx, FORM as _FORM, Writer as _Writer, _validate as _validate_value, measure_table, save_table\n\n\n")

	if body := host.Body(); body != "" {
		b.WriteString(body)
		b.WriteString("\n")
	}

	// The packet classes' own table descriptors and registrations come before
	// the table classes, so a table descriptor whose default names a packet
	// class meets it registered. Only a packet type or union a table reaches
	// carries one: the rest is the packet core's alone.
	closure := ir.TableClosure(u)
	closureUn := closureUnions(u)
	packets := append([]*ir.Struct(nil), plan.packets...)
	sort.Slice(packets, func(i, j int) bool { return packets[i].Name < packets[j].Name })
	for _, st := range packets {
		if !closure[st.Name] {
			continue
		}
		emitTableDescriptor(&b, u, st, true)
	}
	packetUnions := append([]*ir.Union(nil), plan.packetUnions...)
	sort.Slice(packetUnions, func(i, j int) bool { return packetUnions[i].Name < packetUnions[j].Name })
	for _, un := range packetUnions {
		if !closureUn[un.Name] {
			continue
		}
		emitUnionDescriptor(&b, un)
		fmt.Fprintf(&b, "register_union(%s)\n\n\n", un.Name)
	}

	tables := append([]*ir.Struct(nil), plan.tables...)
	sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
	unions := append([]*ir.Union(nil), plan.unions...)
	sort.Slice(unions, func(i, j int) bool { return unions[i].Name < unions[j].Name })
	for _, st := range tables {
		for _, f := range st.Fields {
			if f.KeyEnum != "" {
				emitKeyedClass(&b, st, f)
			}
		}
	}
	for _, un := range orderUnions(unions) {
		emitUnion(&b, un)
	}
	for _, st := range tables {
		emitTable(&b, u, st)
	}
	return b.String()
}

// hostedAlias is the private spelling, in the table runtime, of a shared value
// check the hosted packet codec calls by its packet-runtime name.
func hostedAlias(name string) (string, bool) {
	switch name {
	case "validate_int", "validate_bool", "validate_float32", "validate_float64", "validate_compressed_float", "wire_input", "float32_bits_eq", "float64_bits_eq":
		return "_" + name, true
	}
	return "", false
}

func sortedSet2(set map[string]string) []string {
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func sortedSet(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// orderUnions emits a union whose arm names another union after that arm's
// union: a generated union's constructor builds every payload arm at its
// default, so the class it names must already be registered.
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

func emitTable(b *strings.Builder, u *ir.Unit, st *ir.Struct) {
	name := st.Name
	fmt.Fprintf(b, "class %s:\n", name)
	fmt.Fprintf(b, "    \"\"\"The table %s.\"\"\"\n\n", name)
	slots := make([]string, 0, len(st.Fields))
	for _, f := range st.Fields {
		slots = append(slots, strconv.Quote(f.Name))
	}
	fmt.Fprintf(b, "    __slots__ = (%s", strings.Join(slots, ", "))
	if len(slots) == 1 {
		b.WriteString(",")
	}
	b.WriteString(")\n\n")
	for _, f := range st.Fields {
		fmt.Fprintf(b, "    %s: %s\n", f.Name, pythonType(st, f))
	}
	b.WriteString("\n    descriptor: ClassVar[TableDesc]\n\n")
	b.WriteString("    def __init__(self) -> None:\n")
	if len(st.Fields) == 0 {
		b.WriteString("        pass\n")
	} else {
		for _, f := range st.Fields {
			fmt.Fprintf(b, "        self.%s = %s\n", f.Name, defaultExpr(st.Name, f))
		}
	}
	b.WriteString("\n    def __eq__(self, other: object) -> bool:\n")
	fmt.Fprintf(b, "        if not isinstance(other, %s):\n", name)
	b.WriteString("            return False\n")
	if len(st.Fields) == 0 {
		b.WriteString("        return True\n")
	} else {
		parts := make([]string, 0, len(st.Fields))
		for _, f := range st.Fields {
			parts = append(parts, fmt.Sprintf("self.%s == other.%s", f.Name, f.Name))
		}
		fmt.Fprintf(b, "        return bool(%s)\n", strings.Join(parts, " and "))
	}
	b.WriteString("\n    def __repr__(self) -> str:\n")
	if len(st.Fields) == 0 {
		fmt.Fprintf(b, "        return %q\n", name+"()")
	} else {
		parts := make([]string, 0, len(st.Fields))
		for _, f := range st.Fields {
			parts = append(parts, fmt.Sprintf("%s={self.%s!r}", f.Name, f.Name))
		}
		fmt.Fprintf(b, "        return f\"%s(%s)\"\n", name, strings.Join(parts, ", "))
	}
	if fixedRootAdmitted(u, st) {
		b.WriteString("\n    fixed_plan_max: ClassVar[int] = 0\n")
		fmt.Fprintf(b, "\n    @classmethod\n    def fixed_load(cls, data: bytes | bytearray | memoryview, capacity: int = 4096, report: LoadReport | None = None, plan_capacity: int | None = None) -> list[Self]:\n        return _s_fixed_load(cls, data, capacity, report, plan_capacity)\n")
		fmt.Fprintf(b, "\n    @classmethod\n    def fixed_save(cls, values: list[Self]) -> bytes:\n        return _s_fixed_save(cls, values)\n")
		fmt.Fprintf(b, "\n    @classmethod\n    def fixed_measure(cls, count: int) -> int:\n        return _s_fixed_measure(cls, count)\n")
	}
	b.WriteString("\n    @classmethod\n")
	if st.FixedDeclared {
		fmt.Fprintf(b, "    def load(cls, data: bytes | bytearray | memoryview, report: LoadReport | None = None) -> Self:\n        return _s_fixed_load(cls, data, 1, report)[0]\n")
	} else if scalarOnly(st) {
		fmt.Fprintf(b, "    def load(cls, data: bytes | bytearray | memoryview, report: LoadReport | None = None) -> %s:\n", name)
		fmt.Fprintf(b, "        return _s_load_%s(cls, data, report)\n", name)
	} else {
		b.WriteString("    def load(cls, data: bytes | bytearray | memoryview, report: LoadReport | None = None) -> Self:\n")
		b.WriteString("        return load_table(cls, data, report)\n")
	}
	b.WriteString("\n    def save(self) -> bytes:\n")
	if st.FixedDeclared {
		b.WriteString("        return _s_fixed_save(type(self), [self])\n")
	} else if scalarOnly(st) {
		fmt.Fprintf(b, "        return _s_save_%s(self)\n", name)
	} else {
		b.WriteString("        return save_table(self)\n")
	}
	b.WriteString("\n    def measure(self) -> int:\n")
	if st.FixedDeclared {
		b.WriteString("        return _s_fixed_measure(type(self), 1)\n\n\n")
	} else {
		b.WriteString("        return measure_table(self)\n\n\n")
	}
	emitTableDescriptor(b, u, st, false)
	if scalarOnly(st) {
		emitStraightLine(b, st)
	}
	b.WriteString("\n")
}

// emitTableDescriptor emits one generated class's table descriptor: the facts
// a generic walk reads (docs/SPEC-TABLES.md §8.1). valueClass is a packet type
// hosted from the packet core: it registers by name, because a packet type by
// value never rides the node table's type ids, and its class is the packet
// core's.
func emitTableDescriptor(b *strings.Builder, u *ir.Unit, st *ir.Struct, valueClass bool) {
	name := st.Name
	variable := false
	if !valueClass {
		variable = ir.VariableTables(u)[name]
	}
	fmt.Fprintf(b, "%s.descriptor = TableDesc(\n", name)
	fmt.Fprintf(b, "    name=%s,\n", strconv.Quote(name))
	fmt.Fprintf(b, "    type_id=%s,\n", hex64(ir.TableWireId(st.WireName())))
	fmt.Fprintf(b, "    variable=%s,\n", pyBool(variable))
	fmt.Fprintf(b, "    fields=(\n")
	for ordinal, f := range st.Fields {
		lo, hi := intRange(f)
		fmt.Fprintf(b, "        FieldDesc(\n")
		fmt.Fprintf(b, "            name=%s,\n", strconv.Quote(f.Name))
		fmt.Fprintf(b, "            field_id=%s,\n", hex64(ir.TableFieldWireId(f)))
		fmt.Fprintf(b, "            kind=%d,\n", ir.TableWireFieldKind(f))
		fmt.Fprintf(b, "            codec=%s,\n", strconv.Quote(codecOf(f)))
		if f.Type.Optional {
			fmt.Fprintf(b, "            inner=%s,\n", strconv.Quote(innerCodecOf(f)))
		}
		fmt.Fprintf(b, "            ordinal=%d,\n", ordinal)
		fmt.Fprintf(b, "            lo=%s,\n", lo)
		fmt.Fprintf(b, "            hi=%s,\n", hi)
		fmt.Fprintf(b, "            target=%s,\n", strconv.Quote(targetName(f)))
		mapEntry := ""
		if f.MapEntry != nil {
			mapEntry = f.MapEntry.Name
		}
		fmt.Fprintf(b, "            map_entry=%s,\n", strconv.Quote(mapEntry))
		fmt.Fprintf(b, "            pointer=%s,\n", pyBool(f.Type.Pointer))
		fmt.Fprintf(b, "            optional=%s,\n", pyBool(f.Type.Optional))
		fmt.Fprintf(b, "            array=%s,\n", pyBool(f.Array != ir.ArrayNone))
		fmt.Fprintf(b, "            bound=%d,\n", f.ArrayBound)
		fmt.Fprintf(b, "            key_enum=%s,\n", strconv.Quote(f.KeyEnum))
		fmt.Fprintf(b, "            array_kind=%d,\n", int(f.Array))
		fmt.Fprintf(b, "            array_min=%d,\n", f.ArrayMin)
		fmt.Fprintf(b, "            elem_kind=%d,\n", ir.TableWireElemKind(f))
		elemCodec := ""
		if codecOf(f) == "array" {
			elemCodec = elementCodecOf(f)
		}
		fmt.Fprintf(b, "            elem_codec=%s,\n", strconv.Quote(elemCodec))
		fmt.Fprintf(b, "            size=%d,\n", fieldSize(f))
		fmt.Fprintf(b, "            variant_ids=%s,\n", variantIds(f))
		if dep := deprecatedOrdinals(f); dep != "" {
			fmt.Fprintf(b, "            deprecated=%s,\n", dep)
		}
		fmt.Fprintf(b, "            default=%s,\n", descriptorDefault(f))
		if innerCodecOf(f) == "array" {
			emitOptionalArrayElem(b, f)
		}
		if codecOf(f) == "keyed" {
			emitKeyedDesc(b, f)
		}
		fmt.Fprintf(b, "        ),\n")
	}
	b.WriteString("    ),\n")
	b.WriteString(")\n")
	if valueClass {
		fmt.Fprintf(b, "_register_value(%s)\n\n\n", name)
	} else {
		fmt.Fprintf(b, "register(%s)\n", name)
	}
}

// scalarOnly reports whether every field is a scalar the generated
// straight-line shape can bind directly: a bool, an integer (a flags mask
// included), a string, a bytes or an enum. A by-value table, a pointer, a
// container or a union keeps the descriptor walk. The string, bytes and enum
// kinds joined the set when the evolution kinds' shape bench measured the pair
// faster (docs/PORT-PYTHON.md, THE TABLE FRAME, Bench).
func scalarOnly(st *ir.Struct) bool {
	for _, f := range st.Fields {
		if f.Type.Pointer || f.Type.Optional || f.IsMap() || f.Array != ir.ArrayNone || f.KeyEnum != "" {
			return false
		}
		switch codecOf(f) {
		case "bool", "int", "string", "bytes", "enum":
		default:
			return false
		}
	}
	return true
}

// emitStraightLine emits the straight-line shape for a table whose fields are
// all scalars (scalarOnly; docs/PORT-PYTHON.md, THE TABLE FRAME; D1): the field
// walk is unrolled with each field descriptor bound to a local, against the
// runtime's descriptor walk over the same wire. The bench keeps the faster.
func emitStraightLine(b *strings.Builder, st *ir.Struct) {
	name := st.Name
	fmt.Fprintf(b, "def _s_save_%s(value: %s) -> bytes:\n", name, name)
	b.WriteString("    _validate_value(value, value.descriptor, \"\", set())\n")
	b.WriteString("    ctx = _SaveCtx()\n")
	b.WriteString("    ctx.indices[id(value)] = 1\n")
	b.WriteString("    ctx.onstack.add(id(value))\n")
	b.WriteString("    sink = bytearray()\n")
	b.WriteString("    writer = _Writer(sink)\n")
	b.WriteString("    writer.byte(_FORM)\n")
	b.WriteString("    fields = value.descriptor.fields\n")
	b.WriteString("    current: object\n")
	for i, f := range st.Fields {
		fmt.Fprintf(b, "    current = value.%s\n", f.Name)
		fmt.Fprintf(b, "    if not fields[%d].is_default(current):\n", i)
		fmt.Fprintf(b, "        _s_kinds._REGISTRY[%q].write(writer, fields[%d], current, ctx)\n", codecOf(f), i)
	}
	b.WriteString("    writer.byte(0)\n")
	b.WriteString("    for table_id in ctx.ids:\n")
	b.WriteString("        writer.raw(table_id.to_bytes(8, \"little\"))\n")
	b.WriteString("    writer.raw(len(ctx.ids).to_bytes(8, \"little\"))\n")
	b.WriteString("    return bytes(sink)\n\n")
	fmt.Fprintf(b, "def _s_load_%s(cls: type[%s], data: bytes | bytearray | memoryview, report: LoadReport | None = None) -> %s:\n", name, name, name)
	b.WriteString("    view = buffer_view(data)\n")
	b.WriteString("    ids = _id_table(view)\n")
	b.WriteString("    ctx = _LoadCtx(ids)\n")
	b.WriteString("    obj = cls()\n")
	b.WriteString("    ctx.root = obj\n")
	b.WriteString("    reader = _LoadReader(view, 1, _body_end(view), ids)\n")
	b.WriteString("    fields = obj.descriptor.fields\n")
	b.WriteString("    seen: set[int] = set()\n")
	b.WriteString("    while True:\n")
	b.WriteString("        reference = reader.varint()\n")
	b.WriteString("        if reference == 0:\n")
	b.WriteString("            break\n")
	b.WriteString("        field_id = reader.id_at(reference)\n")
	b.WriteString("        kind = reader.byte()\n")
	b.WriteString("        if field_id in seen:\n")
	b.WriteString("            raise Refused(\"wire_damaged\", \"a body carries one field twice\")\n")
	b.WriteString("        seen.add(field_id)\n")
	for i, f := range st.Fields {
		if i == 0 {
			fmt.Fprintf(b, "        if field_id == fields[%d].field_id:\n", i)
		} else {
			fmt.Fprintf(b, "        elif field_id == fields[%d].field_id:\n", i)
		}
		fmt.Fprintf(b, "            if kind != fields[%d].kind:\n", i)
		fmt.Fprintf(b, "                raise Refused(\"kind_mismatch\", f\"{obj.descriptor.name}.{fields[%d].name} rides under kind {kind}, not {fields[%d].kind}\")\n", i, i)
		fmt.Fprintf(b, "            obj.%s = _s_kinds._REGISTRY[fields[%d].codec].read(reader, fields[%d], ctx)\n", f.Name, i, i)
	}
	if len(st.Fields) > 0 {
		b.WriteString("        else:\n")
		b.WriteString("            _unknown_field(reader, field_id, kind, ctx, True)\n")
	} else {
		b.WriteString("        _unknown_field(reader, field_id, kind, ctx, True)\n")
	}
	b.WriteString("    if reader.pos != reader.end:\n")
	b.WriteString("        raise Refused(\"wire_damaged\", \"bytes between the root terminator and the id table\")\n")
	b.WriteString("    _resolve(ctx)\n")
	b.WriteString("    if report is not None:\n")
	b.WriteString("        report.unknown = ctx.unknown\n")
	b.WriteString("        report.widened = 0\n")
	b.WriteString("        report.retained = 0\n")
	b.WriteString("        report.retain_lost = 0\n")
	b.WriteString("    return obj\n\n\n")
}

func keyedClassName(owner string, f *ir.Field) string {
	return owner + ir.GoExportName(f.Name)
}

// emitKeyedClass emits the [E]T keyed-array class (docs/PORT-PYTHON.md, THE
// VALUE MAPPING, Tables): a list of exactly E.Max elements with the element
// for variant k at index k-1, and a [k] that refuses None and every key
// outside 1..Max with KeyError.
func emitKeyedClass(b *strings.Builder, st *ir.Struct, f *ir.Field) {
	name := keyedClassName(st.Name, f)
	count := int64(0)
	if f.KeyEnumRef != nil {
		count = f.KeyEnumRef.Max
	}
	elem := elementDefault(st.Name, f)
	values := make([]string, 0, count)
	for i := int64(0); i < count; i++ {
		values = append(values, elem)
	}
	fmt.Fprintf(b, "class %s:\n", name)
	fmt.Fprintf(b, "    \"\"\"The enum-keyed array %s.%s, one slot per named variant.\"\"\"\n\n", st.Name, f.Name)
	b.WriteString("    __slots__ = (\"_values\",)\n\n")
	b.WriteString("    _values: list[object]\n")
	fmt.Fprintf(b, "    key_max: ClassVar[int] = %d\n\n", count)
	b.WriteString("    def __init__(self) -> None:\n")
	fmt.Fprintf(b, "        self._values = [%s]\n\n", strings.Join(values, ", "))
	b.WriteString("    def __len__(self) -> int:\n")
	b.WriteString("        return len(self._values)\n\n")
	b.WriteString("    def __getitem__(self, key: int | None) -> object:\n")
	fmt.Fprintf(b, "        if key is None or key < 1 or key > %d:\n", count)
	b.WriteString("            raise KeyError(key)\n")
	b.WriteString("        return self._values[key - 1]\n\n")
	b.WriteString("    def __setitem__(self, key: int | None, value: object) -> None:\n")
	fmt.Fprintf(b, "        if key is None or key < 1 or key > %d:\n", count)
	b.WriteString("            raise KeyError(key)\n")
	b.WriteString("        self._values[key - 1] = value\n\n")
	b.WriteString("    def __eq__(self, other: object) -> bool:\n")
	fmt.Fprintf(b, "        if not isinstance(other, %s):\n", name)
	b.WriteString("            return False\n")
	b.WriteString("        return self._values == other._values\n\n")
	b.WriteString("    def __repr__(self) -> str:\n")
	fmt.Fprintf(b, "        return f\"%s({self._values!r})\"\n\n\n", name)
}

// emitUnion emits a table-side union (docs/PORT-PYTHON.md, THE VALUE MAPPING,
// Tables): a class whose type is the arm number and whose payload arms are
// built fresh, beside its UnionDesc and ArmDesc data.
func emitUnion(b *strings.Builder, un *ir.Union) {
	slots := []string{strconv.Quote("type")}
	for _, v := range un.Variants {
		if v.F != nil {
			slots = append(slots, strconv.Quote(v.Name))
		}
	}
	fmt.Fprintf(b, "class %s:\n", un.Name)
	fmt.Fprintf(b, "    \"\"\"The table-side union %s.\"\"\"\n\n", un.Name)
	fmt.Fprintf(b, "    __slots__ = (%s", strings.Join(slots, ", "))
	if len(slots) == 1 {
		b.WriteString(",")
	}
	b.WriteString(")\n\n")
	b.WriteString("    type: int\n")
	for _, v := range un.Variants {
		if v.F != nil {
			fmt.Fprintf(b, "    %s: object\n", v.Name)
		}
	}
	b.WriteString("    descriptor: ClassVar[UnionDesc]\n\n")
	b.WriteString("    def __init__(self) -> None:\n")
	b.WriteString("        self.type = 0\n")
	for _, v := range un.Variants {
		if v.F != nil {
			fmt.Fprintf(b, "        self.%s = %s\n", v.Name, defaultExpr(un.Name, v.F))
		}
	}
	b.WriteString("\n    def __eq__(self, other: object) -> bool:\n")
	fmt.Fprintf(b, "        if not isinstance(other, %s):\n", un.Name)
	b.WriteString("            return False\n")
	parts := []string{"self.type == other.type"}
	for _, v := range un.Variants {
		if v.F != nil {
			parts = append(parts, fmt.Sprintf("self.%s == other.%s", v.Name, v.Name))
		}
	}
	fmt.Fprintf(b, "        return bool(%s)\n", strings.Join(parts, " and "))
	b.WriteString("\n    def __repr__(self) -> str:\n")
	reprParts := []string{"type={self.type!r}"}
	for _, v := range un.Variants {
		if v.F != nil {
			reprParts = append(reprParts, fmt.Sprintf("%s={self.%s!r}", v.Name, v.Name))
		}
	}
	fmt.Fprintf(b, "        return f\"%s(%s)\"\n\n", un.Name, strings.Join(reprParts, ", "))
	emitUnionDescriptor(b, un)
	fmt.Fprintf(b, "register_union(%s)\n\n\n", un.Name)
}

// emitUnionDescriptor emits one generated union's descriptor: its arms in
// declaration order (docs/SPEC-TABLES.md §8.1). It is shared by the table-side
// union class and the packet union the packet core hosts.
func emitUnionDescriptor(b *strings.Builder, un *ir.Union) {
	fmt.Fprintf(b, "%s.descriptor = UnionDesc(\n", un.Name)
	fmt.Fprintf(b, "    name=%s,\n", strconv.Quote(un.Name))
	b.WriteString("    arms=(\n")
	for tag, v := range un.Variants {
		wire := v.Name
		if v.WasName != "" {
			wire = v.WasName
		}
		kind := ir.TableKindNoPayload
		target := ""
		pointer := false
		if v.F != nil {
			kind = ir.TableWireFieldKind(v.F)
			target = targetName(v.F)
			pointer = v.F.Type.Pointer
		}
		fmt.Fprintf(b, "        ArmDesc(\n")
		fmt.Fprintf(b, "            name=%s,\n", strconv.Quote(v.Name))
		fmt.Fprintf(b, "            tag=%d,\n", tag+1)
		fmt.Fprintf(b, "            arm_id=%s,\n", hex64(ir.TableWireId(wire)))
		fmt.Fprintf(b, "            kind=%d,\n", kind)
		fmt.Fprintf(b, "            target=%s,\n", strconv.Quote(target))
		fmt.Fprintf(b, "            pointer=%s,\n", pyBool(pointer))
		fmt.Fprintf(b, "            field=%s,\n", armFieldDesc(v, tag+1))
		fmt.Fprintf(b, "        ),\n")
	}
	b.WriteString("    ),\n")
	b.WriteString(")\n")
}

// armFieldDesc is one arm's field line as data: the same descriptor facts a
// field carries (emitTableDescriptor), with the arm id standing in for the
// field id (docs/SPEC-TABLES.md §2.6, §3). A payload-free arm has no field
// line. An arm whose codec needs a walk the union module does not run (a
// keyed array, a byte buffer) refuses by name through kind_unsupported.py.
func armFieldDesc(v ir.UnionVariant, tag int) string {
	f := v.F
	if f == nil {
		return "None"
	}
	wire := v.Name
	if v.WasName != "" {
		wire = v.WasName
	}
	codec := codecOf(f)
	switch codec {
	case "keyed", "string_blob", "bytes_blob":
		codec = "unsupported"
	}
	elemCodec := ""
	if codec == "array" {
		elemCodec = elementCodecOf(f)
	}
	lo, hi := intRange(f)
	mapEntry := ""
	if f.MapEntry != nil {
		mapEntry = f.MapEntry.Name
	}
	deprecated := ""
	if dep := deprecatedOrdinals(f); dep != "" {
		deprecated = ", deprecated=" + dep
	}
	return "FieldDesc(" +
		"name=" + strconv.Quote(v.Name) +
		", field_id=" + hex64(ir.TableWireId(wire)) +
		", kind=" + strconv.Itoa(ir.TableWireFieldKind(f)) +
		", codec=" + strconv.Quote(codec) +
		", ordinal=" + strconv.Itoa(tag) +
		", lo=" + lo +
		", hi=" + hi +
		", target=" + strconv.Quote(targetName(f)) +
		", map_entry=" + strconv.Quote(mapEntry) +
		", pointer=" + pyBool(f.Type.Pointer) +
		", optional=" + pyBool(f.Type.Optional) +
		", array=" + pyBool(f.Array != ir.ArrayNone) +
		", bound=" + strconv.FormatInt(f.ArrayBound, 10) +
		", key_enum=" + strconv.Quote(f.KeyEnum) +
		", array_kind=" + strconv.Itoa(int(f.Array)) +
		", array_min=" + strconv.FormatInt(f.ArrayMin, 10) +
		", elem_kind=" + strconv.Itoa(ir.TableWireElemKind(f)) +
		", elem_codec=" + strconv.Quote(elemCodec) +
		", size=" + strconv.FormatInt(fieldSize(f), 10) +
		", variant_ids=" + variantIds(f) +
		deprecated +
		", default=" + defaultFor(f, codec) +
		")"
}

func packageInit(u *ir.Unit, plans []*filePlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Code generated by the schema compiler from the unit %s. DO NOT EDIT.\n", u.Package)
	fmt.Fprintf(&b, "\"\"\"The schema unit %s.\"\"\"\n\n", u.Package)
	b.WriteString("from typing import Final\n\n")
	b.WriteString("from ._schema import InvalidValue, LoadReport, Refused\n")
	b.WriteString("from ._schema.fixedform import FixedLoadReport\n")
	names := []string{"InvalidValue", "LoadReport", "FixedLoadReport", "PROTOCOL_ID", "Refused"}
	for _, plan := range plans {
		fmt.Fprintf(&b, "from . import %s\n", plan.base)
		names = append(names, plan.base)
		classes := make([]string, 0, len(plan.tables)+len(plan.packets))
		for _, st := range append(append([]*ir.Struct(nil), plan.tables...), plan.packets...) {
			classes = append(classes, st.Name)
			names = append(names, st.Name)
			for _, f := range st.Fields {
				if f.KeyEnum != "" {
					keyed := keyedClassName(st.Name, f)
					classes = append(classes, keyed)
					names = append(names, keyed)
				}
			}
		}
		if len(classes) > 0 {
			sort.Strings(classes)
			fmt.Fprintf(&b, "from .%s import %s\n", plan.base, strings.Join(classes, ", "))
		}
		unions := make([]string, 0, len(plan.unions)+len(plan.packetUnions))
		for _, un := range plan.unions {
			unions = append(unions, un.Name)
			names = append(names, un.Name)
		}
		for _, un := range plan.packetUnions {
			unions = append(unions, un.Name, un.Name+"Type")
			names = append(names, un.Name, un.Name+"Type")
		}
		if len(unions) > 0 {
			sort.Strings(unions)
			fmt.Fprintf(&b, "from .%s import %s\n", plan.base, strings.Join(unions, ", "))
		}
	}
	b.WriteString("from . import _fixed as _s_fixed\n")
	fmt.Fprintf(&b, "\nPROTOCOL_ID: Final = 0x%016x\n\n", u.ProtocolId)
	sort.Strings(names)
	b.WriteString("__all__ = [\n")
	for _, n := range names {
		fmt.Fprintf(&b, "    %q,\n", n)
	}
	b.WriteString("]\n")
	return b.String()
}

// emitKeyedDesc emits an enum-keyed array's element descriptor and its key
// variants' wire ids in slot order (docs/SPEC-TABLES.md §2.4, §3.2): slot i
// holds the variant whose value is i + 1, and a slot past the named variants
// carries 0, a key no variant names, which a write refuses.
func emitKeyedDesc(b *strings.Builder, f *ir.Field) {
	codec := keyedElemCodec(f)
	lo, hi := intRange(f)
	fmt.Fprintf(b, "            elem=FieldDesc(\n")
	fmt.Fprintf(b, "                name=%s,\n", strconv.Quote(f.Name))
	fmt.Fprintf(b, "                field_id=0,\n")
	fmt.Fprintf(b, "                kind=%d,\n", ir.TableWireElemKind(f))
	fmt.Fprintf(b, "                codec=%s,\n", strconv.Quote(codec))
	fmt.Fprintf(b, "                lo=%s,\n", lo)
	fmt.Fprintf(b, "                hi=%s,\n", hi)
	fmt.Fprintf(b, "                target=%s,\n", strconv.Quote(targetName(f)))
	fmt.Fprintf(b, "                default=%s,\n", defaultFor(f, codec))
	fmt.Fprintf(b, "            ),\n")
	keys := make([]string, 0, f.ArrayBound)
	for i := 0; i < int(f.ArrayBound); i++ {
		id := uint64(0)
		if i < len(f.KeyEnumRef.Variants) {
			id = ir.TableWireId(f.KeyEnumRef.VariantWireName(i))
		}
		keys = append(keys, hex64(id))
	}
	if len(keys) == 1 {
		fmt.Fprintf(b, "            keys=(%s,),\n", keys[0])
	} else {
		fmt.Fprintf(b, "            keys=(%s),\n", strings.Join(keys, ", "))
	}
}

// keyedElemCodec is the codec of an enum-keyed array's element, or "" for an
// element the keyed kind does not carry yet: bool, the integer and
// fixed-point kinds, and tables by value.
func keyedElemCodec(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TInt, ir.TFixed:
		return "int"
	case ir.TNamed:
		if ref, ok := f.Type.Ref.(*ir.Struct); ok && ref.IsTable {
			return "table"
		}
	}
	return ""
}

func codecOf(f *ir.Field) string {
	if f.KeyEnum != "" && f.KeyEnumRef != nil && !f.Type.Pointer && !f.Type.Optional && !f.IsMap() && keyedElemCodec(f) != "" {
		return "keyed"
	}
	if f.Array != ir.ArrayNone && !f.IsMap() && f.KeyEnum == "" && !f.Type.Optional {
		// An unbounded array carries every element kind elementCodecOf names.
		// A fixed or counted array carries a declared type by value: a
		// kind-14 body of kind-13 elements, the nested body the field form
		// rides (a packet type by value in an array, docs/PORT-PYTHON.md).
		// An array of unions is kind_union.py's: each element is an arm
		// header in place (§3), in a fixed, counted or unbounded array.
		if _, ok := f.Type.Ref.(*ir.Union); ok && f.Type.Kind == ir.TNamed && !f.Type.Pointer {
			return "union_array"
		}
		element := elementCodecOf(f)
		if element == "unsupported" {
			return "unsupported"
		}
		if f.Array == ir.ArrayList || element == "table" {
			return "array"
		}
		return "unsupported"
	}
	if f.Type.Pointer {
		if f.Type.Blob() {
			if f.Type.Kind == ir.TString {
				return "string_blob"
			}
			return "bytes_blob"
		}
		if ref, ok := f.Type.Ref.(*ir.Struct); ok && ref.IsTable {
			return "pointer"
		}
		return "unsupported"
	}
	if f.IsMap() {
		if f.Type.Optional {
			return "unsupported"
		}
		return "map"
	}
	if f.KeyEnum != "" {
		return "unsupported"
	}
	if f.Type.Optional {
		return "optional"
	}
	if f.Array != ir.ArrayNone {
		return "unsupported"
	}
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TInt, ir.TFixed, ir.TBits:
		return "int"
	case ir.TString:
		return "string"
	case ir.TBytes:
		return "bytes"
	case ir.TNamed:
		switch f.Type.Ref.(type) {
		case *ir.Struct:
			// a table or a packet type by value: both ride a kind-13 body
			return "table"
		case *ir.Union:
			return "union"
		case *ir.Enum:
			return "enum"
		case *ir.Flags:
			// a flags mask rides as a u64 and holds an int; its declared
			// default is the mask the declaration names.
			return "int"
		}
	}
	return "unsupported"
}

// elementCodecOf is the codec one array element rides through. An element is an
// array element and not a field, so a construct an element spelling does not
// admit is never reached here; a []*T element is the pointer-array layer's and
// stays unsupported until that card lands.
func elementCodecOf(f *ir.Field) string {
	if f.Type.Pointer {
		return "unsupported"
	}
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TInt, ir.TFixed, ir.TBits:
		return "int"
	case ir.TNamed:
		if _, ok := f.Type.Ref.(*ir.Struct); ok {
			// a table or a packet type by value: both ride a kind-13 body
			return "table"
		}
	}
	return "unsupported"
}

// innerCodecOf is the codec an optional wraps: ?T rides T's own framing, so
// the optional kind owns presence and delegates the bytes to it.
//
// An optional ARRAY, ?[N]T or ?[..N]T, wraps the "array" framing (kind 14),
// which the optional kind writes itself (runtime/_schema/kind_optional.py):
// presence decides whether the field rides, and a present array always rides,
// a counted one at count zero and a fixed one whole (docs/SPEC-TABLES.md
// §2.3). Its elements delegate to their own codecs; an element kind this cut
// does not carry keeps the array "unsupported", refused by name.
func innerCodecOf(f *ir.Field) string {
	if !f.Type.Optional {
		return ""
	}
	if f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted {
		switch optionalElemCodecOf(f) {
		case "bool", "int", "table":
			return "array"
		}
		return "unsupported"
	}
	single := *f
	single.Type.Optional = false
	return codecOf(&single)
}

// optionalElemCodecOf is the codec of one element of an optional array: the
// field's own type with neither the presence bit nor the array shape.
func optionalElemCodecOf(f *ir.Field) string {
	elem := *f
	elem.Type.Optional = false
	elem.Array = ir.ArrayNone
	elem.ArrayBound = 0
	elem.ArrayMin = 0
	return codecOf(&elem)
}

// emitOptionalArrayElem writes the elem entry of an optional array's
// descriptor: the element's own codec, wire kind, range, target and zero
// value, so the optional kind delegates each element. The outer emit writes
// array_kind and array_min (the array shape's own numbers) for every field.
func emitOptionalArrayElem(b *strings.Builder, f *ir.Field) {
	lo, hi := intRange(f)
	codec := optionalElemCodecOf(f)
	def := "None"
	switch codec {
	case "bool":
		def = "False"
	case "int":
		def = "0"
	}
	fmt.Fprintf(b, "            elem=FieldDesc(\n")
	fmt.Fprintf(b, "                name=%s,\n", strconv.Quote(f.Name))
	fmt.Fprintf(b, "                field_id=0,\n")
	fmt.Fprintf(b, "                kind=%d,\n", ir.TableWireElemKind(f))
	fmt.Fprintf(b, "                codec=%s,\n", strconv.Quote(codec))
	fmt.Fprintf(b, "                lo=%s,\n", lo)
	fmt.Fprintf(b, "                hi=%s,\n", hi)
	fmt.Fprintf(b, "                target=%s,\n", strconv.Quote(targetName(f)))
	fmt.Fprintf(b, "                default=%s,\n", def)
	fmt.Fprintf(b, "            ),\n")
}

func targetName(f *ir.Field) string {
	if f.MapEntry != nil {
		return f.MapEntry.Name
	}
	switch ref := f.Type.Ref.(type) {
	case *ir.Struct:
		return ref.Name
	case *ir.Union:
		return ref.Name
	}
	return ""
}

func pythonType(st *ir.Struct, f *ir.Field) string {
	if f.Type.Pointer {
		if f.Type.Blob() {
			if f.Type.Kind == ir.TString {
				return "str | None"
			}
			return "bytes | bytearray | memoryview | None"
		}
		return "object | None"
	}
	if f.Type.Optional {
		return "object | None"
	}
	if f.IsMap() {
		return "dict[object, object]"
	}
	if f.Array != ir.ArrayNone {
		if f.KeyEnum != "" {
			return keyedClassName(st.Name, f)
		}
		return "list[object]"
	}
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TInt, ir.TFixed, ir.TBits, ir.TFloat32, ir.TFloat64:
		return "int | float"
	case ir.TString:
		return "str"
	case ir.TBytes:
		return "bytes"
	case ir.TNamed:
		if _, ok := f.Type.Ref.(*ir.Struct); ok {
			return "object"
		}
		if _, ok := f.Type.Ref.(*ir.Union); ok {
			return "object"
		}
		return "int"
	}
	return "object"
}

// defaultExpr is the value a field is born with (docs/PORT-PYTHON.md, THE
// VALUE MAPPING): a container before its element's kind, so an array is never
// one element and a map is never a scalar.
func defaultExpr(owner string, f *ir.Field) string {
	if f.Type.Pointer || f.Type.Optional {
		return "None"
	}
	if f.IsMap() {
		return "{}"
	}
	if f.Array != ir.ArrayNone {
		if f.KeyEnum != "" {
			return keyedClassName(owner, f) + "()"
		}
		// A fresh counted array has its declared minimum, and a fixed
		// array every slot (SPEC §4.6). Every nested value is distinct.
		count := f.BornCount()
		if f.Array == ir.ArrayFixed {
			count = f.ArrayBound
		}
		if count <= 0 {
			return "[]"
		}
		elem := elementDefault(owner, f)
		values := make([]string, 0, count)
		for i := int64(0); i < count; i++ {
			values = append(values, elem)
		}
		return "[" + strings.Join(values, ", ") + "]"
	}
	return valueDefault(f)
}

// elementDefault is one array element at its own zero value: an element
// carries no container shape of the field that holds it.
func elementDefault(owner string, f *ir.Field) string {
	if f.Type.Pointer || f.Type.Optional {
		return "None"
	}
	return valueDefault(f)
}

func valueDefault(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return pyBool(f.HasDefault && f.DefBool)
	case ir.TInt, ir.TFixed, ir.TBits:
		if f.HasDefault && f.DefInt != nil {
			return f.DefInt.String()
		}
		return "0"
	case ir.TFloat32, ir.TFloat64:
		if f.HasDefault {
			value := f.DefFloat
			if f.Type.Kind == ir.TFloat32 {
				value = float64(float32(value))
			}
			return fixedLiteralOf(value)
		}
		return "0.0"
	case ir.TString:
		return strconv.Quote(string(f.DefBytes))
	case ir.TBytes:
		return "b" + goBytesLiteral(f.DefBytes)
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			// a table or a packet type by value is its generated class
			return fmt.Sprintf("new_by_name(%q)", ref.Name)
		case *ir.Union:
			return fmt.Sprintf("new_union_by_name(%q)", ref.Name)
		case *ir.Enum:
			return strconv.FormatInt(enumDefaultValue(f), 10)
		case *ir.Flags:
			if f.HasDefault && f.DefInt != nil {
				return f.DefInt.String()
			}
			return "0"
		}
		return "0"
	}
	return "None"
}

// enumDefaultValue is the ordinal a fresh enum field carries (SPEC §4.2): the
// defaulted variant's number, or the implicit None at zero.
func enumDefaultValue(f *ir.Field) int64 {
	ref, ok := f.Type.Ref.(*ir.Enum)
	if !ok || !f.HasDefault || f.DefVariant == "" || f.DefVariant == "None" {
		return 0
	}
	for i, name := range ref.Variants {
		if name == f.DefVariant {
			return int64(i + 1)
		}
	}
	return 0
}

// variantIds is an enum field's wire ids by ordinal: entry k-1 is the hash of
// variant k's wire name, so a variant renamed under `was` keeps its id.
func variantIds(f *ir.Field) string {
	ref, ok := f.Type.Ref.(*ir.Enum)
	if !ok {
		return "()"
	}
	ids := make([]string, 0, len(ref.Variants))
	for i := range ref.Variants {
		ids = append(ids, hex64(ir.TableWireId(ref.VariantWireName(i))))
	}
	if len(ids) == 1 {
		return "(" + ids[0] + ",)"
	}
	return "(" + strings.Join(ids, ", ") + ")"
}

// deprecatedOrdinals is an enum field's deprecated variants by ordinal, as a
// Python tuple, or "" when the enum deprecates none (docs/SPEC-TABLES.md
// §2.15): a read of one yields the field's declared default, and a write of one
// is refused.
func deprecatedOrdinals(f *ir.Field) string {
	ref, ok := f.Type.Ref.(*ir.Enum)
	if !ok {
		return ""
	}
	var ords []string
	for i := range ref.Variants {
		if i < len(ref.VariantTags) && slices.Contains(ref.VariantTags[i], "deprecated") {
			ords = append(ords, strconv.Itoa(i+1))
		}
	}
	switch len(ords) {
	case 0:
		return ""
	case 1:
		return "(" + ords[0] + ",)"
	}
	return "(" + strings.Join(ords, ", ") + ")"
}

// fieldSize is a string or bytes field's declared byte bound, and 0 for every
// field that declares none or is not a byte buffer.
func fieldSize(f *ir.Field) int64 {
	switch f.Type.Kind {
	case ir.TString, ir.TBytes:
		return f.Type.Size
	}
	return 0
}

func descriptorDefault(f *ir.Field) string {
	return defaultFor(f, codecOf(f))
}

// defaultFor is the descriptor default of a field, or of a container's
// element, carried under codec.
func defaultFor(f *ir.Field, codec string) string {
	switch codec {
	case "bool":
		return pyBool(f.HasDefault && f.DefBool)
	case "int":
		if f.HasDefault && f.DefInt != nil {
			return f.DefInt.String()
		}
		return "0"
	case "string":
		return strconv.Quote(string(f.DefBytes))
	case "bytes":
		return "b" + goBytesLiteral(f.DefBytes)
	case "map":
		return "{}"
	case "enum":
		return strconv.FormatInt(enumDefaultValue(f), 10)
	}
	return "None"
}

func intRange(f *ir.Field) (lo, hi string) {
	switch f.Type.Kind {
	case ir.TFixed:
		if l, h, ok := ir.TableRawRange(f); ok {
			return l.String(), h.String()
		}
	case ir.TInt:
		if f.HasIntRange && f.IntMin != nil && f.IntMax != nil {
			return f.IntMin.String(), f.IntMax.String()
		}
	case ir.TBits:
		return "0", new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1)).String()
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			// the value is the variant's ordinal, 0 for the implicit None
			return "0", strconv.FormatInt(ref.Max, 10)
		case *ir.Flags:
			// a file carries a mask whole, so its width is the u64's own
			return "0", new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1)).String()
		}
	}
	return widthRange(f.Type.Width, f.Type.Signed)
}

// widthRange is the storage width's own range, the fallback when a declaration
// names no bounds.
func widthRange(width int, signed bool) (string, string) {
	bits := width
	if bits <= 0 {
		bits = 64
	}
	one := big.NewInt(1)
	if signed {
		lo := new(big.Int).Neg(new(big.Int).Lsh(one, uint(bits-1)))
		hi := new(big.Int).Sub(new(big.Int).Lsh(one, uint(bits-1)), one)
		return lo.String(), hi.String()
	}
	hi := new(big.Int).Sub(new(big.Int).Lsh(one, uint(bits)), one)
	return "0", hi.String()
}

func pyBool(v bool) string {
	if v {
		return "True"
	}
	return "False"
}

func hex64(v uint64) string { return fmt.Sprintf("0x%016x", v) }

func goBytesLiteral(data []byte) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range data {
		fmt.Fprintf(&b, "\\x%02x", c)
	}
	b.WriteByte('"')
	return b.String()
}
