package cstable

// The optional compilation unit is the registry's only home (§8.3–8.5).
// Descriptor factories avoid partial-class initialization order dependencies.
import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func generateView(u *ir.Unit, closure map[string]bool) []byte {
	g := &tableGen{unit: u, outside: true}
	g.tf("%s", tableViewRuntime)
	var types, tables []*ir.Struct
	for _, f := range u.Files {
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && !st.IsTable {
				types = append(types, st)
			}
		}
		for _, st := range f.Tables {
			if !st.IsMapEntry() {
				tables = append(tables, st)
			}
		}
	}
	for _, set := range [][]*ir.Struct{types, tables} {
		sort.Slice(set, func(i, j int) bool { return set[i].Name < set[j].Name })
	}
	for _, st := range types {
		if closure[st.Name] {
			continue
		}
		g.owner = st
		g.emitTableReset(st)
		g.emitTableDescriptor(st)
	}
	g.outside = false
	file := func(name string) string {
		if s, ok := u.DeclFile[name]; ok {
			return s + ".schema"
		}
		return ""
	}
	annotation := func(doc string, tags []string) string {
		return fmt.Sprintf("Doc = %s, Tags = %s", ir.QuoteDoc(doc), armTags(tags))
	}
	typeRows := func(set []*ir.Struct) string {
		var rows []string
		for _, st := range set {
			rows = append(rows, fmt.Sprintf("new ViewType { Name = %q, File = %q, Table = %t, TypeRef = %sTableType, %s }", st.Name, file(st.Name), st.IsTable, st.Name, annotation(st.Doc, st.Tags)))
		}
		return "new ViewType[] { " + strings.Join(rows, ",\n") + " }"
	}
	reached := ir.TableClosureVocabulary(u)
	id := func(owner, name string) uint64 {
		if reached[owner] {
			return ir.TableWireId(name)
		}
		return 0
	}
	vocab := func(name string, max int64, bits int, doc string, tags []string, rows []string) string {
		return fmt.Sprintf("new ViewVocabulary { Name = %q, File = %q, Max = %d, StorageBits = %d, %s, Variants = new ViewVariant[] { %s } }", name, file(name), max, bits, annotation(doc, tags), strings.Join(rows, ",\n"))
	}
	row := func(value int, name string, identity uint64, doc string, tags []string, extra string) string {
		return fmt.Sprintf("new ViewVariant { Value = %d, Name = %q, Id = 0x%016xul, %s%s }", value, name, identity, annotation(doc, tags), extra)
	}
	var names []string
	for n := range u.Enums {
		names = append(names, n)
	}
	sort.Strings(names)
	var enums, flags, unions, constants []string
	for _, n := range names {
		e := u.Enums[n]
		rows := []string{row(0, "None", 0, "", nil, "")}
		for i, n := range e.Variants {
			rows = append(rows, row(i+1, n, id(e.Name, e.VariantWireName(i)), e.VariantDocs[i], e.VariantTags[i], ""))
		}
		enums = append(enums, vocab(e.Name, e.Max, e.StorageBits, e.Doc, e.Tags, rows))
	}
	names = nil
	for n := range u.Flags {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		f := u.Flags[n]
		var rows []string
		for i, n := range f.Variants {
			rows = append(rows, row(i, n, 0, f.VariantDocs[i], f.VariantTags[i], ""))
		}
		bits := 8
		for bits < f.WireBits && bits < 64 {
			bits *= 2
		}
		flags = append(flags, vocab(f.Name, int64(len(f.Variants)-1), bits, f.Doc, f.Tags, rows))
	}
	allUnions := make(map[string]*ir.Union)
	for n, un := range u.Unions {
		allUnions[n] = un
	}
	for n, un := range u.TableUnions {
		allUnions[n] = un
	}
	names = nil
	for n := range allUnions {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		un := allUnions[n]
		rows := []string{row(0, "None", 0, "", nil, "")}
		for i, arm := range un.Variants {
			extra := ""
			if !arm.Void() {
				extra = fmt.Sprintf(", PayloadName = %q", ir.FieldTypeSpelling(arm.F))
				if arm.Body() {
					extra += fmt.Sprintf(", PayloadRef = %sTableType", arm.Type)
				} else {
					field := &tableGen{unit: u, owner: unionOwner(un), arm: true, outside: !reached[un.Name]}
					field.emitTableFieldDescriptor(arm.F, "")
					extra += ", Field = " + strings.TrimSuffix(strings.TrimSpace(field.schema.String()), ",")
				}
			}
			rows = append(rows, row(i+1, arm.Name, id(un.Name, arm.WireName()), arm.Doc, arm.Tags, extra))
		}
		unions = append(unions, vocab(un.Name, un.Max, un.StorageBits, un.Doc, un.Tags, rows))
	}
	names = nil
	for n := range u.Consts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		c := u.Consts[n]
		integer, floating := "0", "0.0"
		if c.IsFloat {
			floating = formatFloat64(c.Float)
		} else if c.Int != nil {
			integer = fmt.Sprintf("unchecked((long)0x%016xul)", c.Int.Uint64())
			if c.Int.IsInt64() {
				integer = c.Int.String() + "L"
			}
		}
		constants = append(constants, fmt.Sprintf("new ViewConstant { Name = %q, File = %q, TypeName = %q, IsFloat = %t, IntValue = %s, FloatValue = %s, %s }", c.Name, file(c.Name), c.Storage, c.IsFloat, integer, floating, annotation(c.Doc, c.Tags)))
	}
	g.pf("private static readonly UnitViewInfo tableUnitView = new UnitViewInfo { Package = %q, ProtocolId = 0x%016xul,\n", u.Package, u.ProtocolId)
	g.pf("Types = %s, Tables = %s,\n", typeRows(types), typeRows(tables))
	g.pf("Enums = new ViewVocabulary[] { %s }, Flags = new ViewVocabulary[] { %s }, Unions = new ViewVocabulary[] { %s }, Constants = new ViewConstant[] { %s } };\n", strings.Join(enums, ",\n"), strings.Join(flags, ",\n"), strings.Join(unions, ",\n"), strings.Join(constants, ",\n"))
	g.pf("public static UnitViewInfo UnitView() { return tableUnitView; }\n")
	return g.assemble()
}

const tableViewRuntime = `
public sealed class ViewConstant
{
    public string Name { get; internal init; }
    public string File { get; internal init; }
    public string TypeName { get; internal init; }
    public bool IsFloat { get; internal init; }
    public long IntValue { get; internal init; }
    public double FloatValue { get; internal init; }
    public string Doc { get; internal init; }
    public ReadOnlyMemory<string> Tags { get; internal init; }
    public int NumTags => Tags.Length;
}
public sealed class ViewVariant
{
    public ulong Value { get; internal init; }
    public string Name { get; internal init; }
    public ulong Id { get; internal init; }
    public string PayloadName { get; internal init; }
    internal Func<TableTypeInfo> PayloadRef { get; init; }
    public TableTypeInfo Payload => PayloadRef?.Invoke();
    public TableFieldInfo Field { get; internal init; }
    public string Doc { get; internal init; }
    public ReadOnlyMemory<string> Tags { get; internal init; }
    public int NumTags => Tags.Length;
}
public sealed class ViewVocabulary
{
    public string Name { get; internal init; }
    public string File { get; internal init; }
    public long Max { get; internal init; }
    public int StorageBits { get; internal init; }
    public ReadOnlyMemory<ViewVariant> Variants { get; internal init; }
    public int NumVariants => Variants.Length;
    public string Doc { get; internal init; }
    public ReadOnlyMemory<string> Tags { get; internal init; }
    public int NumTags => Tags.Length;
}
public sealed class ViewType
{
    public string Name { get; internal init; }
    public string File { get; internal init; }
    public bool Table { get; internal init; }
    internal Func<TableTypeInfo> TypeRef { get; init; }
    public TableTypeInfo Type => TypeRef();
    public string Doc { get; internal init; }
    public ReadOnlyMemory<string> Tags { get; internal init; }
    public int NumTags => Tags.Length;
}
public sealed class UnitViewInfo
{
    public string Package { get; internal init; }
    public ulong ProtocolId { get; internal init; }
    public ReadOnlyMemory<ViewType> Types { get; internal init; }
    public ReadOnlyMemory<ViewType> Tables { get; internal init; }
    public ReadOnlyMemory<ViewVocabulary> Enums { get; internal init; }
    public ReadOnlyMemory<ViewVocabulary> Flags { get; internal init; }
    public ReadOnlyMemory<ViewVocabulary> Unions { get; internal init; }
    public ReadOnlyMemory<ViewConstant> Constants { get; internal init; }
    public int NumTypes => Types.Length;
    public int NumTables => Tables.Length;
    public int NumEnums => Enums.Length;
    public int NumFlags => Flags.Length;
    public int NumUnions => Unions.Length;
    public int NumConstants => Constants.Length;
}
`
