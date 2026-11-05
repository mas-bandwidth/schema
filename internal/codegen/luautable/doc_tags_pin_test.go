package luautable_test

import (
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	schemaparser "github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func docTagsUnit(t *testing.T, base, src string) *ir.Unit {
	t.Helper()
	name := base + ".schema"
	f, perrs := schemaparser.Parse(name, []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse %s: %v", name, perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: name, Name: name, Base: base, Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", name, cerrs[0])
	}
	return u
}

func docTagsUnitFromPaths(t *testing.T, paths []string) *ir.Unit {
	t.Helper()
	sources := make([]check.SourceFile, 0, len(paths))
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(p)
		base := strings.TrimSuffix(name, ".schema")
		f, perrs := schemaparser.Parse(name, raw)
		if len(perrs) > 0 {
			t.Fatalf("parse %s: %v", p, perrs[0])
		}
		sources = append(sources, check.SourceFile{
			Path: p, Name: name, Base: base, Bytes: raw, AST: f,
		})
	}
	u, cerrs := check.Unit(sources)
	if len(cerrs) > 0 {
		t.Fatalf("check %v: %v", paths, cerrs[0])
	}
	return u
}

// docTagsProbeSchema exercises single-line and multi-line doc comments (with
// embedded quotes, backslashes, and blank doc lines), single and multi-tag
// ordered lists, reached types inside the table closure, packet types outside
// the table closure, and unannotated tables and fields (docs/SPEC-TABLES.md
// §8.1, §8.2, §8.3).
const docTagsProbeSchema = `package doctagsprobe

/// Outside packet type doc with "quotes" and a \ backslash.
type OutsidePacket | packet_only, cold_tier
{
    /// Outside field doc.
    label string(16) | cold_field
    plain int32
}

/// Reached struct doc in the table closure.
type TunedStep | reached_struct, step_tag
{
    /// Step weight before clamping.
    ///
    /// Written "w \ dt" in the solver notes.
    weight float32 = 1.0 | slider, hot, solver_group
    raw    int32 = 0
}

type BareReached
{
    count int32 = 0
}

/// Primary tuning table for the probe unit.
/// Second line of the table doc.
fixed table TunedTable | designer_facing, hot_reload
{
    /// Damage per hit, before armor.
    ///
    /// The armor curve is written "hardness \ hit" in the design notes.
    damage   float32 = 21.0 | ui_slider, hot, tuning
    /// Velocity in meters per second.
    speed    float32 = 500.0 | was = "velocity"
    untagged int32 = 7
    /// Tagged without a doc comment.
    flagged  bool | toggle_only
    step     TunedStep
    bare     BareReached
}

table VariableTaggedTable | var_table
{
    /// Optional count with a doc and two tags.
    opt_count ?int32 | headline, localized
    items     []int32
    /// Guarded field doc.
    extra     int32 | guarded_tag
}

fixed table UnannotatedTable
{
    alpha int32
    beta  string(8)
}
`

const docTagsLuauProbe = `--!strict
local view = require("./schema_view")
local uv = view.UnitView()

local function hexStr(s: string): string
	local parts = table.create(#s)
	for i = 1, #s do
		parts[i] = string.format("%02x", string.byte(s, i))
	end
	return table.concat(parts)
end

local function emitSet(kind: string, entries: { any })
	for _, e in entries do
		local d = e.descriptor
		local frozen = table.isfrozen(uv)
			and table.isfrozen(entries)
			and table.isfrozen(e)
			and table.isfrozen(e.tags)
			and table.isfrozen(e.fields)
			and d ~= nil
			and table.isfrozen(d)
			and d.fields ~= nil
			and table.isfrozen(d.fields)
		local dName = if d ~= nil then tostring(d.name) else ""
		local dTable = if d ~= nil then tostring(d.table == true) else "false"
		local dId = if d ~= nil and d.id ~= nil then string.format("%016x", d.id) else ""
		local dCount = if d ~= nil and d.fields ~= nil then #d.fields else -1
		local tagParts = table.create(#e.tags)
		for i, tag in e.tags do
			tagParts[i] = hexStr(tag)
		end
		print(string.format(
			"D\t%s\t%s\t%s\t%s\t%s\t%d\t%s\t%d\t%s",
			kind,
			e.name,
			tostring(frozen),
			dName,
			dTable,
			dCount,
			dId,
			#e.tags,
			hexStr(e.doc) .. (if #tagParts > 0 then "\t" .. table.concat(tagParts, "\t") else "")
		))
		for i, f in e.fields do
			local df = if d ~= nil and d.fields ~= nil then d.fields[i] else nil
			local fFrozen = table.isfrozen(f)
				and table.isfrozen(f.tags)
				and df ~= nil
				and table.isfrozen(df)
			local dfName = if df ~= nil then tostring(df.name) else ""
			local dfJson = if df ~= nil and df.json ~= nil then tostring(df.json) else "-"
			local dfId = if df ~= nil and df.id ~= nil then string.format("%016x", df.id) else ""
			local fTags = table.create(#f.tags)
			for j, tag in f.tags do
				fTags[j] = hexStr(tag)
			end
			print(string.format(
				"F\t%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s",
				kind,
				e.name,
				i,
				f.name,
				f.json,
				f.type,
				f.id,
				tostring(f.optional == true),
				tostring(fFrozen),
				dfName,
				dfJson,
				dfId,
				#f.tags,
				hexStr(f.doc) .. (if #fTags > 0 then "\t" .. table.concat(fTags, "\t") else "")
			))
		end
	end
end

emitSet("table", uv.tables)
emitSet("type", uv.types)
`

type goRefField struct {
	Name             string
	Json             string
	DeclaredTypeName string
	Id               uint64
	Optional         bool
	Doc              string
	DocIsNone        bool
	NumTags          int
	Tags             []string
	TagsIsNil        bool
}

type goRefTable struct {
	Owner     string
	Name      string
	IsTable   bool
	InClosure bool
	NumFields int
	Doc       string
	DocIsNone bool
	NumTags   int
	Tags      []string
	TagsIsNil bool
	Fields    []goRefField
}

type luauProbeField struct {
	Name     string
	Json     string
	Type     string
	Id       string
	Optional bool
	Frozen   bool
	DescName string
	DescJson string
	DescId   string
	Doc      string
	Tags     []string
}

type luauProbeDecl struct {
	Kind      string
	Name      string
	Frozen    bool
	DescName  string
	DescTable bool
	DescCount int
	DescId    string
	Doc       string
	Tags      []string
	Fields    []luauProbeField
}

// goReferenceDescriptors extracts every <T>TableInfo and <T>TableFields (and
// their static <T>Table*Tags slices) from the Go leg's generated *Table.go
// files, plus outside-closure packet type descriptors from *View.go.
func goReferenceDescriptors(t *testing.T, u *ir.Unit) map[string]goRefTable {
	t.Helper()
	goFiles, err := gotable.Generate(u)
	if err != nil {
		t.Fatalf("gotable.Generate(%s): %v", u.Package, err)
	}
	fset := token.NewFileSet()
	tagSlices := map[string][]string{}
	fieldSlices := map[string][]goRefField{}
	tableInfos := map[string]goRefTable{}
	viewPacketTypes := map[string]goRefTable{}

	parseStringSlice := func(comp *ast.CompositeLit) []string {
		out := make([]string, 0, len(comp.Elts))
		for _, elt := range comp.Elts {
			lit, ok := elt.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Fatalf("expected string literal in tag slice, got %T", elt)
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatalf("unquote tag literal %s: %v", lit.Value, err)
			}
			out = append(out, s)
		}
		return out
	}

	parseFieldComp := func(comp *ast.CompositeLit) goRefField {
		var rf goRefField
		for _, elt := range comp.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			switch key.Name {
			case "Name":
				rf.Name, _ = strconv.Unquote(kv.Value.(*ast.BasicLit).Value)
			case "Json", "JSON":
				rf.Json, _ = strconv.Unquote(kv.Value.(*ast.BasicLit).Value)
			case "DeclaredTypeName":
				rf.DeclaredTypeName, _ = strconv.Unquote(kv.Value.(*ast.BasicLit).Value)
			case "Id", "ID":
				v, err := strconv.ParseUint(kv.Value.(*ast.BasicLit).Value, 0, 64)
				if err != nil {
					t.Fatalf("parse field Id %s: %v", kv.Value.(*ast.BasicLit).Value, err)
				}
				rf.Id = v
			case "Optional":
				rf.Optional = kv.Value.(*ast.Ident).Name == "true"
			case "Doc":
				switch v := kv.Value.(type) {
				case *ast.Ident:
					if v.Name != "TableDocNone" {
						t.Fatalf("unexpected Doc ident %s", v.Name)
					}
					rf.Doc = ""
					rf.DocIsNone = true
				case *ast.BasicLit:
					s, err := strconv.Unquote(v.Value)
					if err != nil {
						t.Fatalf("unquote Doc %s: %v", v.Value, err)
					}
					rf.Doc = s
				default:
					t.Fatalf("unexpected Doc expr %T", kv.Value)
				}
			case "NumTags":
				n, err := strconv.Atoi(kv.Value.(*ast.BasicLit).Value)
				if err != nil {
					t.Fatalf("parse NumTags: %v", err)
				}
				rf.NumTags = n
			case "Tags":
				switch v := kv.Value.(type) {
				case *ast.Ident:
					if v.Name == "nil" {
						rf.TagsIsNil = true
					} else {
						tags, ok := tagSlices[v.Name]
						if !ok {
							t.Fatalf("unknown tag slice variable %s", v.Name)
						}
						rf.Tags = slices.Clone(tags)
					}
				case *ast.CompositeLit:
					rf.Tags = parseStringSlice(v)
				default:
					t.Fatalf("unexpected Tags expr %T", kv.Value)
				}
			}
		}
		return rf
	}

	parseTableInfoComp := func(owner string, comp *ast.CompositeLit) goRefTable {
		var rt goRefTable
		rt.Owner = owner
		for _, elt := range comp.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			switch key.Name {
			case "Name":
				rt.Name, _ = strconv.Unquote(kv.Value.(*ast.BasicLit).Value)
			case "NumFields":
				n, err := strconv.Atoi(kv.Value.(*ast.BasicLit).Value)
				if err != nil {
					t.Fatalf("parse NumFields: %v", err)
				}
				rt.NumFields = n
			case "Doc":
				switch v := kv.Value.(type) {
				case *ast.Ident:
					if v.Name != "TableDocNone" {
						t.Fatalf("unexpected TableInfo Doc ident %s", v.Name)
					}
					rt.Doc = ""
					rt.DocIsNone = true
				case *ast.BasicLit:
					s, err := strconv.Unquote(v.Value)
					if err != nil {
						t.Fatalf("unquote TableInfo Doc %s: %v", v.Value, err)
					}
					rt.Doc = s
				default:
					t.Fatalf("unexpected TableInfo Doc expr %T", kv.Value)
				}
			case "NumTags":
				n, err := strconv.Atoi(kv.Value.(*ast.BasicLit).Value)
				if err != nil {
					t.Fatalf("parse TableInfo NumTags: %v", err)
				}
				rt.NumTags = n
			case "Tags":
				switch v := kv.Value.(type) {
				case *ast.Ident:
					if v.Name == "nil" {
						rt.TagsIsNil = true
					} else {
						tags, ok := tagSlices[v.Name]
						if !ok {
							t.Fatalf("unknown TableInfo tag slice variable %s", v.Name)
						}
						rt.Tags = slices.Clone(tags)
					}
				case *ast.CompositeLit:
					rt.Tags = parseStringSlice(v)
				default:
					t.Fatalf("unexpected TableInfo Tags expr %T", kv.Value)
				}
			case "Fields":
				switch v := kv.Value.(type) {
				case *ast.Ident:
					fields, ok := fieldSlices[v.Name]
					if !ok {
						t.Fatalf("unknown TableFields slice variable %s", v.Name)
					}
					rt.Fields = slices.Clone(fields)
				case *ast.CompositeLit:
					for _, felt := range v.Elts {
						fcomp, ok := felt.(*ast.CompositeLit)
						if !ok {
							t.Fatalf("expected field composite literal, got %T", felt)
						}
						rt.Fields = append(rt.Fields, parseFieldComp(fcomp))
					}
				}
			}
		}
		return rt
	}

	parsedFiles := map[string]*ast.File{}
	sortedNames := slices.Sorted(maps.Keys(goFiles))
	for _, name := range sortedNames {
		if !strings.HasSuffix(name, "Table.go") && !strings.HasSuffix(name, "View.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, goFiles[name], 0)
		if err != nil {
			t.Fatalf("parse generated %s: %v", name, err)
		}
		parsedFiles[name] = f
	}

	// Pass 1: collect all static tag slices across all *Table.go files.
	for _, name := range sortedNames {
		f, ok := parsedFiles[name]
		if !ok {
			continue
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
					continue
				}
				varName := vs.Names[0].Name
				comp, ok := vs.Values[0].(*ast.CompositeLit)
				if !ok {
					continue
				}
				if strings.HasSuffix(varName, "Tags") && strings.Contains(varName, "Table") {
					tagSlices[varName] = parseStringSlice(comp)
				}
			}
		}
	}

	// Pass 2: collect all <T>TableFields across all *Table.go files.
	for _, name := range sortedNames {
		f, ok := parsedFiles[name]
		if !ok {
			continue
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || len(vs.Names) != 1 || len(vs.Values) != 1 {
					continue
				}
				varName := vs.Names[0].Name
				comp, ok := vs.Values[0].(*ast.CompositeLit)
				if !ok {
					continue
				}
				if strings.HasSuffix(varName, "TableFields") {
					fields := make([]goRefField, 0, len(comp.Elts))
					for _, elt := range comp.Elts {
						fcomp, ok := elt.(*ast.CompositeLit)
						if !ok {
							t.Fatalf("%s: expected composite literal in %s, got %T", name, varName, elt)
						}
						fields = append(fields, parseFieldComp(fcomp))
					}
					fieldSlices[varName] = fields
				}
			}
		}
	}

	// Pass 3: collect all <T>TableInfo and tableViewPacketTypes across *Table.go and *View.go.
	for _, name := range sortedNames {
		f, ok := parsedFiles[name]
		if !ok {
			continue
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Name.Name != "init" || fd.Body == nil {
				continue
			}
			for _, stmt := range fd.Body.List {
				as, ok := stmt.(*ast.AssignStmt)
				if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
					continue
				}
				comp, ok := as.Rhs[0].(*ast.CompositeLit)
				if !ok {
					continue
				}
				switch lhs := as.Lhs[0].(type) {
				case *ast.Ident:
					if owner, ok := strings.CutSuffix(lhs.Name, "TableInfo"); ok {
						tableInfos[owner] = parseTableInfoComp(owner, comp)
					}
				case *ast.IndexExpr:
					if ident, ok := lhs.X.(*ast.Ident); ok && ident.Name == "tableViewPacketTypes" {
						rt := parseTableInfoComp("", comp)
						rt.Owner = rt.Name
						viewPacketTypes[rt.Name] = rt
					}
				}
			}
		}
	}

	closure := ir.TableClosure(u)
	out := map[string]goRefTable{}
	for name, st := range u.Tables {
		if st.IsMapEntry() {
			continue
		}
		rt, ok := tableInfos[name]
		if !ok {
			t.Fatalf("%s: missing Go reference %sTableInfo", u.Package, name)
		}
		rt.IsTable = true
		rt.InClosure = true
		out[name] = rt
	}
	for name := range u.Structs {
		if closure[name] {
			rt, ok := tableInfos[name]
			if !ok {
				t.Fatalf("%s: missing Go reference %sTableInfo for closure-reached type", u.Package, name)
			}
			rt.IsTable = false
			rt.InClosure = true
			out[name] = rt
		} else {
			rt, ok := viewPacketTypes[name]
			if !ok {
				t.Fatalf("%s: missing Go reference tableViewPacketTypes entry for outside type %s", u.Package, name)
			}
			rt.IsTable = false
			rt.InClosure = false
			out[name] = rt
		}
	}
	return out
}

func decodeHexField(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func runLuauDocTagsProbe(t *testing.T, files map[string][]byte) (map[string]luauProbeDecl, error) {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.luau"), []byte(docTagsLuauProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := string(luaurun.Run(t, dir, "main.luau", nil))
	decls := map[string]luauProbeDecl{}
	for line := range strings.SplitSeq(strings.TrimSuffix(raw, "\n"), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		switch parts[0] {
		case "D":
			if len(parts) < 9 {
				return nil, fmt.Errorf("malformed declaration probe line %q", line)
			}
			dCount, err := strconv.Atoi(parts[6])
			if err != nil {
				return nil, fmt.Errorf("parse descriptor field count in %q: %w", line, err)
			}
			nTags, err := strconv.Atoi(parts[8])
			if err != nil {
				return nil, fmt.Errorf("parse declaration tag count in %q: %w", line, err)
			}
			if len(parts) != 10+nTags {
				return nil, fmt.Errorf("declaration line %q has %d columns, want %d", line, len(parts), 10+nTags)
			}
			doc, err := decodeHexField(parts[9])
			if err != nil {
				return nil, fmt.Errorf("decode declaration doc in %q: %w", line, err)
			}
			tags := make([]string, 0, nTags)
			for _, tp := range parts[10:] {
				tag, err := decodeHexField(tp)
				if err != nil {
					return nil, fmt.Errorf("decode declaration tag in %q: %w", line, err)
				}
				tags = append(tags, tag)
			}
			d := luauProbeDecl{
				Kind:      parts[1],
				Name:      parts[2],
				Frozen:    parts[3] == "true",
				DescName:  parts[4],
				DescTable: parts[5] == "true",
				DescCount: dCount,
				DescId:    parts[7],
				Doc:       doc,
				Tags:      tags,
			}
			if _, dup := decls[d.Name]; dup {
				return nil, fmt.Errorf("duplicate Luau declaration %s", d.Name)
			}
			decls[d.Name] = d
		case "F":
			if len(parts) < 14 {
				return nil, fmt.Errorf("malformed field probe line %q", line)
			}
			owner := parts[2]
			d, ok := decls[owner]
			if !ok {
				return nil, fmt.Errorf("field line for unknown owner %s: %q", owner, line)
			}
			nTags, err := strconv.Atoi(parts[13])
			if err != nil {
				return nil, fmt.Errorf("parse field tag count in %q: %w", line, err)
			}
			if len(parts) != 15+nTags {
				return nil, fmt.Errorf("field line %q has %d columns, want %d", line, len(parts), 15+nTags)
			}
			doc, err := decodeHexField(parts[14])
			if err != nil {
				return nil, fmt.Errorf("decode field doc in %q: %w", line, err)
			}
			tags := make([]string, 0, nTags)
			for _, tp := range parts[15:] {
				tag, err := decodeHexField(tp)
				if err != nil {
					return nil, fmt.Errorf("decode field tag in %q: %w", line, err)
				}
				tags = append(tags, tag)
			}
			d.Fields = append(d.Fields, luauProbeField{
				Name:     parts[4],
				Json:     parts[5],
				Type:     parts[6],
				Id:       parts[7],
				Optional: parts[8] == "true",
				Frozen:   parts[9] == "true",
				DescName: parts[10],
				DescJson: parts[11],
				DescId:   parts[12],
				Doc:      doc,
				Tags:     tags,
			})
			decls[owner] = d
		default:
			return nil, fmt.Errorf("unexpected probe line %q", line)
		}
	}
	return decls, nil
}

// verifyLuauDocAndTagsAgainstGoReference compares every Luau table and type
// descriptor (and every field descriptor on them) against the Go leg's
// <T>TableInfo and <T>TableFields reference descriptors for the same schema.
func verifyLuauDocAndTagsAgainstGoReference(t *testing.T, label string, u *ir.Unit, files map[string][]byte) error {
	t.Helper()
	goRefs := goReferenceDescriptors(t, u)
	luauDecls, err := runLuauDocTagsProbe(t, files)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	if len(luauDecls) != len(goRefs) {
		return fmt.Errorf("%s: Luau emitted %d table/type descriptors, Go reference has %d", label, len(luauDecls), len(goRefs))
	}
	for _, name := range slices.Sorted(maps.Keys(goRefs)) {
		want := goRefs[name]
		got, ok := luauDecls[name]
		if !ok {
			return fmt.Errorf("%s: Luau UnitView is missing %s (present in Go %sTableInfo)", label, name, name)
		}
		wantKind := "type"
		if want.IsTable {
			wantKind = "table"
		}
		if got.Kind != wantKind {
			return fmt.Errorf("%s.%s: Luau kind is %q, Go reference is %q", label, name, got.Kind, wantKind)
		}
		if !got.Frozen {
			return fmt.Errorf("%s.%s: Luau view entry, tags, fields, or descriptor is not frozen", label, name)
		}
		if got.DescName != want.Name {
			return fmt.Errorf("%s.%s: Luau descriptor.name is %q, Go %sTableInfo.Name is %q", label, name, got.DescName, name, want.Name)
		}
		if got.DescTable != want.IsTable {
			return fmt.Errorf("%s.%s: Luau descriptor.table is %v, Go IsTable is %v", label, name, got.DescTable, want.IsTable)
		}
		if got.DescCount != want.NumFields || len(got.Fields) != want.NumFields {
			return fmt.Errorf("%s.%s: Luau field counts (view=%d, descriptor=%d) do not match Go %sTableInfo.NumFields=%d", label, name, len(got.Fields), got.DescCount, name, want.NumFields)
		}
		if got.Doc != want.Doc {
			return fmt.Errorf("%s.%s: Luau table doc is %q, Go %sTableInfo.Doc is %q (TableDocNone=%v)", label, name, got.Doc, name, want.Doc, want.DocIsNone)
		}
		if want.DocIsNone && got.Doc != "" {
			return fmt.Errorf("%s.%s: Go %sTableInfo.Doc is TableDocNone, but Luau carries %q", label, name, name, got.Doc)
		}
		if len(got.Tags) != want.NumTags || !slices.Equal(got.Tags, want.Tags) {
			return fmt.Errorf("%s.%s: Luau table tags %q (len %d) do not match Go %sTableInfo.Tags %q (NumTags=%d, nil=%v)", label, name, got.Tags, len(got.Tags), name, want.Tags, want.NumTags, want.TagsIsNil)
		}
		for i, wf := range want.Fields {
			gf := got.Fields[i]
			if !gf.Frozen {
				return fmt.Errorf("%s.%s.%s: Luau field view entry, tags, or descriptor field is not frozen", label, name, wf.Name)
			}
			if gf.Name != wf.Name || gf.DescName != wf.Name {
				return fmt.Errorf("%s.%s[%d]: Luau field name (view=%q, descriptor=%q) does not match Go %sTableFields[%d].Name=%q", label, name, i, gf.Name, gf.DescName, name, i, wf.Name)
			}
			wantJson := wf.Json
			if !want.InClosure && wantJson == "" {
				wantJson = "-"
			}
			if gf.Json != wantJson || gf.DescJson != wantJson {
				return fmt.Errorf("%s.%s.%s: Luau json (view=%q, descriptor=%q) does not match Go %sTableFields[%d].Json=%q", label, name, wf.Name, gf.Json, gf.DescJson, name, i, wantJson)
			}
			if gf.Type != wf.DeclaredTypeName {
				return fmt.Errorf("%s.%s.%s: Luau type %q does not match Go %sTableFields[%d].DeclaredTypeName=%q", label, name, wf.Name, gf.Type, name, i, wf.DeclaredTypeName)
			}
			wantId := fmt.Sprintf("%016x", wf.Id)
			if gf.Id != wantId || gf.DescId != wantId {
				return fmt.Errorf("%s.%s.%s: Luau id (view=%s, descriptor=%s) does not match Go %sTableFields[%d].Id=%s", label, name, wf.Name, gf.Id, gf.DescId, name, i, wantId)
			}
			if gf.Optional != wf.Optional {
				return fmt.Errorf("%s.%s.%s: Luau optional=%v does not match Go %sTableFields[%d].Optional=%v", label, name, wf.Name, gf.Optional, name, i, wf.Optional)
			}
			if gf.Doc != wf.Doc {
				return fmt.Errorf("%s.%s.%s: Luau field doc is %q, Go %sTableFields[%d].Doc is %q (TableDocNone=%v)", label, name, wf.Name, gf.Doc, name, i, wf.Doc, wf.DocIsNone)
			}
			if len(gf.Tags) != wf.NumTags || !slices.Equal(gf.Tags, wf.Tags) {
				return fmt.Errorf("%s.%s.%s: Luau field tags %q (len %d) do not match Go %sTableFields[%d].Tags %q (NumTags=%d, nil=%v)", label, name, wf.Name, gf.Tags, len(gf.Tags), name, i, wf.Tags, wf.NumTags, wf.TagsIsNil)
			}
		}
	}
	return nil
}

func generateLuauAllFiles(t *testing.T, u *ir.Unit) map[string][]byte {
	t.Helper()
	pkt, err := luau.Generate(u)
	if err != nil {
		t.Fatalf("luau.Generate(%s): %v", u.Package, err)
	}
	tbl, err := luautable.Generate(u)
	if err != nil {
		t.Fatalf("luautable.Generate(%s): %v", u.Package, err)
	}
	files := maps.Clone(pkt)
	maps.Copy(files, tbl)
	return files
}

func schemaFilesInDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".schema") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

// TestLuauTableFieldsDocAndTagsMatchTheReference pins the generated Luau
// table/type and field doc comments and tags against the Go leg's
// <T>TableInfo and <T>TableFields descriptors for the same schemas
// (docs/SPEC-TABLES.md §8.1, §8.2, §8.3).
func TestLuauTableFieldsDocAndTagsMatchTheReference(t *testing.T) {
	t.Parallel()

	probeUnit := docTagsUnit(t, "DocTagsProbe", docTagsProbeSchema)
	probeFiles := generateLuauAllFiles(t, probeUnit)
	if err := verifyLuauDocAndTagsAgainstGoReference(t, "DocTagsProbe", probeUnit, probeFiles); err != nil {
		t.Fatal(err)
	}

	// Verify luau-analyze --!strict passes over the generated probe and view modules.
	analyzeDir := t.TempDir()
	var analyzeNames []string
	for name, data := range probeFiles {
		if err := os.WriteFile(filepath.Join(analyzeDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
		analyzeNames = append(analyzeNames, name)
	}
	sort.Strings(analyzeNames)
	luaurun.Analyze(t, analyzeDir, analyzeNames...)

	// Also pin corpus schemas (annotated and unannotated) against Go's <T>TableInfo and <T>TableFields.
	corpusUnits := []struct {
		name  string
		paths []string
	}{
		{"examples", schemaFilesInDir(t, filepath.Join(root, "tables", "examples"))},
		{"arms", schemaFilesInDir(t, filepath.Join(root, "tables", "arms"))},
		{"maps", schemaFilesInDir(t, filepath.Join(root, "tables", "maps"))},
		{"cs-view", []string{filepath.Join(root, "test", "tables", "CsView.schema")}},
	}
	for _, cu := range corpusUnits {
		t.Run(cu.name, func(t *testing.T) {
			t.Parallel()
			u := docTagsUnitFromPaths(t, cu.paths)
			files := generateLuauAllFiles(t, u)
			if err := verifyLuauDocAndTagsAgainstGoReference(t, cu.name, u, files); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestLuauTableFieldsDocAndTagsMatchTheReferenceNegativeControl edits a doc
// and a tag (on both fields and tables, plus unannotated rows and descriptor
// field alignment) in a broken twin of the generated Luau descriptors and
// requires the reference pin to fail on that row.
func TestLuauTableFieldsDocAndTagsMatchTheReferenceNegativeControl(t *testing.T) {
	t.Parallel()
	u := docTagsUnit(t, "DocTagsProbe", docTagsProbeSchema)
	baseFiles := generateLuauAllFiles(t, u)

	controls := []struct {
		name   string
		file   string
		from   string
		to     string
		expect string
	}{
		{
			name:   "field-doc",
			file:   "schema_view.luau",
			from:   `doc="Damage per hit, before armor.\n\nThe armor curve is written \"hardness \\ hit\" in the design notes.",tags={"ui_slider","hot","tuning"}`,
			to:     `doc="Damage per hit (drifted).",tags={"ui_slider","hot","tuning"}`,
			expect: "DocTagsProbe.TunedTable.damage: Luau field doc",
		},
		{
			name:   "field-tag",
			file:   "schema_view.luau",
			from:   `doc="Damage per hit, before armor.\n\nThe armor curve is written \"hardness \\ hit\" in the design notes.",tags={"ui_slider","hot","tuning"}`,
			to:     `doc="Damage per hit, before armor.\n\nThe armor curve is written \"hardness \\ hit\" in the design notes.",tags={"ui_knob","hot","tuning"}`,
			expect: "DocTagsProbe.TunedTable.damage: Luau field tags",
		},
		{
			name:   "table-doc",
			file:   "schema_view.luau",
			from:   `name="TunedTable",file="DocTagsProbe.schema",doc="Primary tuning table for the probe unit.\nSecond line of the table doc.",tags={"designer_facing","hot_reload"}`,
			to:     `name="TunedTable",file="DocTagsProbe.schema",doc="Primary tuning table (drifted).",tags={"designer_facing","hot_reload"}`,
			expect: "DocTagsProbe.TunedTable: Luau table doc",
		},
		{
			name:   "table-tag",
			file:   "schema_view.luau",
			from:   `name="TunedTable",file="DocTagsProbe.schema",doc="Primary tuning table for the probe unit.\nSecond line of the table doc.",tags={"designer_facing","hot_reload"}`,
			to:     `name="TunedTable",file="DocTagsProbe.schema",doc="Primary tuning table for the probe unit.\nSecond line of the table doc.",tags={"internal_only","hot_reload"}`,
			expect: "DocTagsProbe.TunedTable: Luau table tags",
		},
		{
			name:   "unannotated-spurious-doc",
			file:   "schema_view.luau",
			from:   `optional=false,doc="",tags={}},{name="beta"`,
			to:     `optional=false,doc="spurious",tags={}},{name="beta"`,
			expect: "DocTagsProbe.UnannotatedTable.alpha: Luau field doc",
		},
		{
			name:   "unannotated-spurious-tag",
			file:   "schema_view.luau",
			from:   `optional=false,doc="",tags={}},{name="beta"`,
			to:     `optional=false,doc="",tags={"spurious"}},{name="beta"`,
			expect: "DocTagsProbe.UnannotatedTable.alpha: Luau field tags",
		},
		{
			name:   "descriptor-field-drift",
			file:   "DocTagsProbeTable.luau",
			from:   `name = "damage", slot = "Damage", json = "damage"`,
			to:     `name = "damage", slot = "Damage", json = "damage_drifted"`,
			expect: "DocTagsProbe.TunedTable.damage: Luau json",
		},
	}

	for _, ctl := range controls {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			files := maps.Clone(baseFiles)
			src := string(files[ctl.file])
			if strings.Count(src, ctl.from) != 1 {
				t.Fatalf("%s carries %q %d times, want 1", ctl.file, ctl.from, strings.Count(src, ctl.from))
			}
			files[ctl.file] = []byte(strings.Replace(src, ctl.from, ctl.to, 1))
			err := verifyLuauDocAndTagsAgainstGoReference(t, "DocTagsProbe", u, files)
			if err == nil {
				t.Fatalf("NEGATIVE CONTROL FAILED (%s): broken twin passed verifyLuauDocAndTagsAgainstGoReference", ctl.name)
			}
			if !strings.Contains(err.Error(), ctl.expect) {
				t.Fatalf("NEGATIVE CONTROL (%s) failed with %q, want substring %q", ctl.name, err.Error(), ctl.expect)
			}
			t.Logf("red as required (%s): %v", ctl.name, err)
		})
	}
}
