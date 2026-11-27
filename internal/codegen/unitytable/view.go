package unitytable

import (
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v3/ir"
)

const ViewFile = "Tables.View.cs"

func emitViewFile(u *ir.Unit, closure map[string]bool) []byte {
	var schema strings.Builder
	schema.WriteString(unityViewRuntimeSource)

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
	sort.Slice(types, func(i, j int) bool { return types[i].Name < types[j].Name })
	sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })

	reached := ir.TableClosureVocabulary(u)

	var unionNames []string
	for name := range u.Unions {
		if !reached[name] && u.TableUnions[name] == nil {
			unionNames = append(unionNames, name)
		}
	}
	sort.Strings(unionNames)
	for _, name := range unionNames {
		emitCarrierUnion(&schema, u, u.Unions[name])
	}

	for _, st := range types {
		if closure[st.Name] {
			continue
		}
		emitCarrierStruct(&schema, u, st)
		emitTableDescriptor(&schema, u, st, true, nil, nil)
	}

	fileOf := func(name string) string {
		if s, ok := u.DeclFile[name]; ok {
			return s + ".schema"
		}
		return ""
	}
	annotation := func(doc string, tags []string) string {
		return fmt.Sprintf("Doc = %s, Tags = %s", csQuote(doc), armTags(tags))
	}
	typeRows := func(set []*ir.Struct) string {
		var rows []string
		for _, st := range set {
			rows = append(rows, fmt.Sprintf("new ViewType { Name = %q, File = %q, Table = %t, TypeRef = delegate { return %sTableType; }, %s }",
				st.Name, fileOf(st.Name), st.IsTable, st.Name, annotation(st.Doc, st.Tags)))
		}
		return "new ViewType[] " + csList(rows, ",\n")
	}
	idOf := func(owner, name string) uint64 {
		if reached[owner] {
			return ir.TableWireId(name)
		}
		return 0
	}
	vocab := func(name string, max int64, bits int, doc string, tags []string, rows []string) string {
		return fmt.Sprintf("new ViewVocabulary { Name = %q, File = %q, Max = %d, StorageBits = %d, %s, Variants = new ViewVariant[] %s }",
			name, fileOf(name), max, bits, annotation(doc, tags), csList(rows, ",\n"))
	}
	row := func(value int, name string, identity uint64, doc string, tags []string, extra string) string {
		return fmt.Sprintf("new ViewVariant { Value = %dUL, Name = %q, Id = 0x%016xUL, %s%s }",
			value, name, identity, annotation(doc, tags), extra)
	}

	var names []string
	var enums, flags, unions, constants []string

	for n := range u.Enums {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		e := u.Enums[n]
		rows := []string{row(0, "None", 0, "", nil, "")}
		for i, v := range e.Variants {
			var vdoc string
			if i < len(e.VariantDocs) {
				vdoc = e.VariantDocs[i]
			}
			var vtags []string
			if i < len(e.VariantTags) {
				vtags = e.VariantTags[i]
			}
			rows = append(rows, row(i+1, v, idOf(e.Name, e.VariantWireNameOf(v)), vdoc, vtags, ""))
		}
		enums = append(enums, vocab(e.Name, e.Max, e.StorageBits, e.Doc, e.Tags, rows))
	}

	names = nil
	for n := range u.Flags {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fl := u.Flags[n]
		var rows []string
		for i, v := range fl.Variants {
			var vdoc string
			if i < len(fl.VariantDocs) {
				vdoc = fl.VariantDocs[i]
			}
			var vtags []string
			if i < len(fl.VariantTags) {
				vtags = fl.VariantTags[i]
			}
			rows = append(rows, row(i, v, 0, vdoc, vtags, ""))
		}
		flags = append(flags, vocab(fl.Name, int64(len(fl.Variants))-1, 64, fl.Doc, fl.Tags, rows))
	}

	allUnions := map[string]*ir.Union{}
	maps.Copy(allUnions, u.Unions)
	maps.Copy(allUnions, u.TableUnions)
	names = nil
	for n := range allUnions {
		names = append(names, n)
	}
	sort.Strings(names)
	pkg := namespaceOf(u.Package)
	for _, n := range names {
		un := allUnions[n]
		rows := []string{row(0, "None", 0, "", nil, "")}
		for i, arm := range un.Variants {
			extra := ""
			if !arm.Void() {
				extra = fmt.Sprintf(", PayloadName = %q", ir.FieldTypeSpelling(arm.F))
				if arm.Body() {
					extra += fmt.Sprintf(", PayloadRef = delegate { return %sTableType; }", arm.Type)
				} else {
					sub := &descCtx{
						unit: u, pkg: pkg, owner: unionOwner(un),
						arm: true, armName: ir.GoExportName(arm.Name),
						outside: !reached[un.Name],
					}
					extra += ", Field = " + sub.fieldExpr(arm.F, "")
				}
			}
			rows = append(rows, row(i+1, arm.Name, idOf(un.Name, arm.WireName()), arm.Doc, arm.Tags, extra))
		}
		maxArm := un.Max
		if maxArm == 0 {
			maxArm = int64(len(un.Variants))
		}
		sbits := un.StorageBits
		if sbits == 0 {
			sbits = ir.StorageBitsFor(int64(len(un.Variants)))
		}
		unions = append(unions, vocab(un.Name, maxArm, sbits, un.Doc, un.Tags, rows))
	}

	names = nil
	for n := range u.Consts {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		c := u.Consts[n]
		integer, floating := "0L", "0.0"
		if c.IsFloat {
			floating = formatFloat64Lit(c.Float)
		} else if c.Int != nil {
			if c.Int.IsInt64() {
				if c.Int.Cmp(minI64Big) == 0 {
					integer = "long.MinValue"
				} else {
					integer = c.Int.String() + "L"
				}
			} else {
				integer = fmt.Sprintf("unchecked((long)0x%016xUL)", c.Int.Uint64())
			}
		}
		constants = append(constants, fmt.Sprintf("new ViewConstant { Name = %q, File = %q, TypeName = %q, IsFloat = %t, IntValue = %s, FloatValue = %s, %s }",
			c.Name, fileOf(c.Name), c.Storage, c.IsFloat, integer, floating, annotation(c.Doc, c.Tags)))
	}

	schema.WriteString("private static readonly UnitViewInfo tableUnitView = new UnitViewInfo\n{\n")
	fmt.Fprintf(&schema, "    Package = %q,\n    ProtocolId = 0x%016xUL,\n", u.Package, u.ProtocolId)
	fmt.Fprintf(&schema, "    Types = %s,\n    Tables = %s,\n", typeRows(types), typeRows(tables))
	fmt.Fprintf(&schema, "    Enums = new ViewVocabulary[] %s,\n    Flags = new ViewVocabulary[] %s,\n    Unions = new ViewVocabulary[] %s,\n    Constants = new ViewConstant[] %s\n};\n\n",
		csList(enums, ",\n"), csList(flags, ",\n"), csList(unions, ",\n"), csList(constants, ",\n"))
	schema.WriteString("public static UnitViewInfo UnitView() { return tableUnitView; }\n")

	usings := []string{"System", "Serialize"}
	return []byte(header(u, "the unit's view", usings...) + wrap(u, "", schema.String()))
}

const unityViewRuntimeSource = `public sealed class ViewConstant
{
    public string Name { get; internal set; }
    public string File { get; internal set; }
    public string TypeName { get; internal set; }
    public bool IsFloat { get; internal set; }
    public long IntValue { get; internal set; }
    public double FloatValue { get; internal set; }
    public string Doc { get; internal set; }
    public ReadOnlyMemory<string> Tags { get; internal set; }
    public int NumTags => Tags.Length;
}

public sealed class ViewVariant
{
    public ulong Value { get; internal set; }
    public string Name { get; internal set; }
    public ulong Id { get; internal set; }
    public string PayloadName { get; internal set; }
    internal Func<TableTypeInfo> PayloadRef { get; set; }
    public TableTypeInfo Payload => PayloadRef == null ? null : PayloadRef();
    public TableFieldInfo Field { get; internal set; }
    public string Doc { get; internal set; }
    public ReadOnlyMemory<string> Tags { get; internal set; }
    public int NumTags => Tags.Length;
}

public sealed class ViewVocabulary
{
    public string Name { get; internal set; }
    public string File { get; internal set; }
    public long Max { get; internal set; }
    public int StorageBits { get; internal set; }
    public ReadOnlyMemory<ViewVariant> Variants { get; internal set; }
    public int NumVariants => Variants.Length;
    public string Doc { get; internal set; }
    public ReadOnlyMemory<string> Tags { get; internal set; }
    public int NumTags => Tags.Length;
}

public sealed class ViewType
{
    public string Name { get; internal set; }
    public string File { get; internal set; }
    public bool Table { get; internal set; }
    internal Func<TableTypeInfo> TypeRef { get; set; }
    public TableTypeInfo Type => TypeRef();
    public string Doc { get; internal set; }
    public ReadOnlyMemory<string> Tags { get; internal set; }
    public int NumTags => Tags.Length;
}

public sealed class UnitViewInfo
{
    public string Package { get; internal set; }
    public ulong ProtocolId { get; internal set; }
    public ReadOnlyMemory<ViewType> Types { get; internal set; }
    public ReadOnlyMemory<ViewType> Tables { get; internal set; }
    public ReadOnlyMemory<ViewVocabulary> Enums { get; internal set; }
    public ReadOnlyMemory<ViewVocabulary> Flags { get; internal set; }
    public ReadOnlyMemory<ViewVocabulary> Unions { get; internal set; }
    public ReadOnlyMemory<ViewConstant> Constants { get; internal set; }
    public int NumTypes => Types.Length;
    public int NumTables => Tables.Length;
    public int NumEnums => Enums.Length;
    public int NumFlags => Flags.Length;
    public int NumUnions => Unions.Length;
    public int NumConstants => Constants.Length;
}

`
