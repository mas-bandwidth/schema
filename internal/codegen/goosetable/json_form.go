// THE JSON TEXT FORM (docs/SPEC-TABLES.md §16) for Goose: card
// schema-goose-json-form and the text walk of schema-goose-text-form
// (docs/PORT-GOOSE.md, THE CARD MAP). The walk is ONE runtime,
// runtime/json.goose, emitted byte for byte into every unit (PORTING M9): it
// reads the descriptors for every decision — keys, bounds, ranges, variant
// names — and reaches a record's storage through the per-table ACCESSOR this
// file emits, `_<T>JsonRead` and `_<T>JsonWrite`, a dispatch on the field
// ordinal that is the only per-table code the form writes (docs/PORT-GOOSE.md,
// How a walk reaches a field). Goose has no reflection, no offsetof and no
// function values in data, so the accessor is handed to the generic walk as a
// static function value and every instantiation is monomorphized.
//
// Per table, the surface is §16.1's, in Goose's spelling:
//
//	<T>FromJsonTolerant(v: T&, text: const u8[:], report: schema_rt::JsonReport&) -> bool
//	<T>FromJson(v: T&, text: const u8[:], report: schema_rt::JsonReport&) -> bool  // strict: a clean report
//	<T>ToJson(v: const T&, out: u8[>..]&) -> i64                                  // appends; -1 refused
//	<T>ToJsonMeasure(v: const T&) -> i64
//
// THE FIXED CLASS IS WHAT THIS FILE CARRIES: every scalar kind up to 64 bits,
// compressed floats, string(N), bytes(N) as base64, enums, flags, nested
// tables and types, unions (payload-free arms included), [N]T, [..N]T, [E]T
// and ?T. A table whose closure holds anything else — a pointer (the &node
// label, PORTING M14), a blob, a list, a map, unbounded or wide text, a
// timestamp, a 128-bit or fixed-point kind, a guarded branch, a `json =
// "array"` or `"value"` table — gets NO text form here, by name in
// [jsonUncarried], and the conformance leg answers its cases absent.
package goosetable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func init() {
	Register("json_form", emitJSONFormPart)
}

// jsonUncarried names the first construct in st's closure the text form does
// not carry yet, or "" when the whole closure is carried.
func jsonUncarried(st *ir.Struct) string {
	seen := map[string]bool{}
	var visitStruct func(st *ir.Struct) string
	var visitField func(owner string, f *ir.Field) string
	visitField = func(owner string, f *ir.Field) string {
		t := f.Type
		at := owner + "." + f.Name
		switch {
		case f.Guard != "":
			return at + ": a guarded branch"
		case f.IsMap() || t.Kind == ir.TMap:
			return at + ": a map"
		case f.Array == ir.ArrayList:
			return at + ": an unbounded array"
		case t.Pointer:
			return at + ": a pointer or a blob"
		case t.TextUnbounded() || t.Unbounded:
			return at + ": unbounded text"
		case t.Kind == ir.TWString:
			return at + ": wstring"
		case t.Timestamp:
			return at + ": a timestamp"
		case t.Kind == ir.TFixed:
			return at + ": a fixed-point kind"
		case t.Kind == ir.TInt && t.Width > 64:
			return at + ": a 128-bit kind"
		case t.Optional && f.Array != ir.ArrayNone:
			return at + ": an optional array"
		}
		if t.Kind != ir.TNamed {
			return ""
		}
		switch r := t.Ref.(type) {
		case *ir.Struct:
			return visitStruct(r)
		case *ir.Union:
			if f.Array != ir.ArrayNone {
				return at + ": an array of unions"
			}
			if seen["union "+r.Name] {
				return ""
			}
			seen["union "+r.Name] = true
			for _, v := range r.Variants {
				if v.F == nil {
					continue
				}
				if why := visitField(r.Name, armField(v)); why != "" {
					return why
				}
			}
		}
		return ""
	}
	visitStruct = func(st *ir.Struct) string {
		if seen[st.Name] {
			return ""
		}
		seen[st.Name] = true
		if st.JsonArray {
			return st.Name + ": json = \"array\""
		}
		for _, f := range st.Fields {
			if why := visitField(st.Name, f); why != "" {
				return why
			}
		}
		return ""
	}
	return visitStruct(st)
}

// jsonCarried is every struct and union some carried table reaches by value,
// and the carried tables themselves.
func jsonCarried(u *ir.Unit) (tables map[string]bool, structs map[string]*ir.Struct, unions map[string]*ir.Union) {
	tables, structs, unions = map[string]bool{}, map[string]*ir.Struct{}, map[string]*ir.Union{}
	var visitStruct func(st *ir.Struct)
	var visitField func(f *ir.Field)
	visitField = func(f *ir.Field) {
		if f.Type.Kind != ir.TNamed {
			return
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			visitStruct(r)
		case *ir.Union:
			if unions[r.Name] != nil {
				return
			}
			unions[r.Name] = r
			for _, v := range r.Variants {
				if v.F != nil {
					visitField(armField(v))
				}
			}
		}
	}
	visitStruct = func(st *ir.Struct) {
		if structs[st.Name] != nil {
			return
		}
		structs[st.Name] = st
		for _, f := range st.Fields {
			visitField(f)
		}
	}
	for _, f := range u.Files {
		for _, st := range f.Tables {
			if jsonUncarried(st) != "" {
				continue
			}
			tables[st.Name] = true
			visitStruct(st)
		}
	}
	return tables, structs, unions
}

func emitJSONFormPart(g *goose.Gen) error {
	tables, structs, unions := jsonCarried(g.Unit)
	if len(structs) == 0 {
		return nil
	}
	e := &jsonEmit{fixedEmit: &fixedEmit{g: g}}
	var mine []string
	for _, t := range g.File.Tables {
		if structs[t.Name] != nil {
			mine = append(mine, t.Name)
		}
	}
	for _, un := range g.File.TableUnions {
		if unions[un.Name] != nil {
			mine = append(mine, un.Name)
		}
	}
	for _, d := range g.File.Decls {
		switch d := d.(type) {
		case *ir.Struct:
			if structs[d.Name] != nil && !d.IsTable {
				mine = append(mine, d.Name)
			}
		case *ir.Union:
			if unions[d.Name] != nil && g.Unit.TableUnions[d.Name] == nil {
				mine = append(mine, d.Name)
			}
		}
	}
	if len(mine) == 0 {
		return nil
	}
	g.Import("schema_json")
	g.Pf("// ---- the JSON text form (docs/SPEC-TABLES.md §16): the accessors the one walk in json.goose reaches storage through ----\n\n")
	for _, name := range mine {
		if st := structs[name]; st != nil {
			e.emitStruct(st)
			if tables[name] {
				e.emitRoot(st)
			}
		} else {
			e.emitUnion(unions[name])
		}
	}
	return e.err
}

type jsonEmit struct {
	*fixedEmit
}

// shapeOf is the JSON type a value of f's element kind is written as
// (docs/SPEC-TABLES.md §16.2): o object, a array, s string, b bool, n number.
func shapeOf(f *ir.Field) byte {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		return 'b'
	case ir.TString, ir.TBytes:
		return 's'
	case ir.TNamed:
		switch t.Ref.(type) {
		case *ir.Enum:
			return 's'
		case *ir.Flags:
			return 'a'
		default:
			return 'o'
		}
	}
	return 'n'
}

// fieldShape is a whole field's JSON type: an [E]T is an object, any other
// array an array.
func fieldShape(f *ir.Field) byte {
	if f.KeyEnum != "" {
		return 'o'
	}
	if f.Array != ir.ArrayNone {
		return 'a'
	}
	return shapeOf(f)
}

// names declares a flags set's or a keying enum's names once per module:
// bit i, or slot i (the key i + 1).
func (e *jsonEmit) names(name string, variants []string) string {
	g := e.g
	id := "_" + name + "JsonNames"
	if g.DeclaredBySurface(id) {
		return id
	}
	g.Declare(id)
	q := make([]string, len(variants))
	for i, v := range variants {
		q[i] = fmt.Sprintf("%q", v)
	}
	g.Pf("let %s: (const u8[:])[%d] = [%s];\n", id, len(q), strings.Join(q, ", "))
	return id
}

// declareNames declares, ahead of the accessors, the name arrays a field's
// flags or keying enum reads by.
func (e *jsonEmit) declareNames(f *ir.Field) {
	if f.KeyEnum != "" {
		e.names(f.KeyEnumRef.Name, f.KeyEnumRef.Variants)
	}
	if r, ok := f.Type.Ref.(*ir.Flags); ok && f.Type.Kind == ir.TNamed {
		e.names(r.Name, r.Variants)
	}
}

// zero is the value an element slot is put back to before a read refills it
// (a repeated key replaces the whole value, §16.2): a table or a type at its
// own defaults, every other kind at zero, as the Go walk clears it.
func (e *jsonEmit) zero(f *ir.Field) string {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		return "false"
	case ir.TFloat32, ir.TFloat64:
		return "0.0"
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Struct:
			return e.structType(r) + " { .. }"
		case *ir.Union:
			return e.unionType(r) + " { .. }"
		}
	}
	return "0"
}

// read returns the statements that read ONE value of f's element kind at pl,
// ending in a return (or a block's tail expression when tail), at depth.
func (e *jsonEmit) read(f *ir.Field, pl place, desc, depth string, tail bool) []string {
	t := f.Type
	ret := func(x string) string {
		if tail {
			return x
		}
		return "return " + x + ";"
	}
	switch t.Kind {
	case ir.TBool:
		return []string{ret(fmt.Sprintf("schema_rt::json_read_bool(jr, %s)", pl.val))}
	case ir.TFloat32, ir.TFloat64:
		return []string{ret(fmt.Sprintf("schema_rt::json_read_float(jr, %s, %s)", pl.val, desc))}
	case ir.TString:
		return []string{ret(fmt.Sprintf("schema_rt::json_read_str(jr, %s, %s)", pl.val, pl.length))}
	case ir.TBytes:
		return []string{ret(fmt.Sprintf("schema_rt::json_read_bytes(jr, %s, %s)", pl.val, pl.length))}
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Enum:
			return []string{ret(fmt.Sprintf("schema_rt::json_read_enum(jr, %s, %s)", pl.val, desc))}
		case *ir.Flags:
			return []string{ret(fmt.Sprintf("schema_rt::json_read_flags(jr, %s, %s, %s)", pl.val, e.names(r.Name, r.Variants), depth))}
		case *ir.Struct:
			typ := e.structType(r)
			return []string{
				fmt.Sprintf("%s = %s { .. };", pl.val, typ),
				ret(fmt.Sprintf("schema_rt::json_read_table(jr, %s, %sDescriptor, _%sJsonShapes, %s + 1, _%sJsonRead)", pl.val, r.Name, r.Name, depth, r.Name)),
			}
		case *ir.Union:
			e.unionType(r)
			return []string{ret(fmt.Sprintf("schema_rt::json_read_union(jr, %s, %sDescriptor, _%sJsonShapes, %s, _%sJsonRead)", pl.val, r.Name, r.Name, depth, r.Name))}
		}
	}
	return []string{ret(fmt.Sprintf("schema_rt::json_read_int(jr, %s, %s)", pl.val, desc))}
}

// write is the expression that writes ONE value of f's element kind.
func (e *jsonEmit) write(f *ir.Field, pl place, desc, depth string) string {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		return fmt.Sprintf("schema_rt::json_write_bool(out, %s)", pl.val)
	case ir.TFloat32, ir.TFloat64:
		return fmt.Sprintf("schema_rt::json_write_float(out, %s)", pl.val)
	case ir.TString:
		return fmt.Sprintf("schema_rt::json_write_str(out, %s, %s)", pl.val, pl.length)
	case ir.TBytes:
		return fmt.Sprintf("schema_rt::json_write_bytes(out, %s, %s)", pl.val, pl.length)
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Enum:
			return fmt.Sprintf("schema_rt::json_write_enum(out, %s, %s)", pl.val, desc)
		case *ir.Flags:
			return fmt.Sprintf("schema_rt::json_write_flags(out, %s, %s, %s)", pl.val, e.names(r.Name, r.Variants), depth)
		case *ir.Struct:
			e.structType(r)
			return fmt.Sprintf("schema_rt::json_write_table(out, %s, %sDescriptor, %s, _%sJsonWrite)", pl.val, r.Name, depth, r.Name)
		case *ir.Union:
			e.unionType(r)
			return fmt.Sprintf("schema_rt::json_write_union(out, %s, %sDescriptor, %s.Type, %s, _%sJsonWrite)", pl.val, r.Name, pl.val, depth, r.Name)
		}
	}
	return fmt.Sprintf("schema_rt::json_write_int(out, %s, %s)", pl.val, desc)
}

// readField is one field's (or arm's) read: an array's reset and element
// loop, or one value. desc and elem name the field's and its element's
// descriptors.
func (e *jsonEmit) readField(ind int, base string, f *ir.Field, desc, elem string) {
	pl := fieldPlace(base, f)
	if f.Array == ir.ArrayNone {
		for _, s := range e.read(f, pl, desc, "depth", false) {
			e.p(ind, "%s", s)
		}
		return
	}
	el := elemOf(f)
	n := slotCount(f)
	e.p(ind, "var j: i64 = 0;")
	e.p(ind, "while j < %d {", n)
	e.p(ind+1, "%s[j] = %s;", pl.val, e.zero(el))
	if el.Type.Kind == ir.TString || el.Type.Kind == ir.TBytes {
		e.p(ind+1, "%s[j] = 0;", pl.length)
	}
	e.p(ind+1, "j += 1;")
	e.p(ind, "}")
	body := e.read(el, pl.at("k"), elem, "d", true)
	if f.KeyEnum != "" {
		names := e.names(f.KeyEnumRef.Name, f.KeyEnumRef.Variants)
		e.p(ind, "return schema_rt::json_read_keyed(jr, %s, '%c', depth) { r, k, d =>", names, shapeOf(el))
	} else {
		count := pl.val + "Count"
		if f.Array != ir.ArrayCounted {
			e.p(ind, "var count: i32 = 0;")
			count = "count"
		}
		e.p(ind, "return schema_rt::json_read_array(jr, %s, %d, '%c', depth) { r, k, d =>", count, n, shapeOf(el))
	}
	for _, s := range body {
		e.p(ind+1, "%s", strings.ReplaceAll(s, "(jr, ", "(r, "))
	}
	e.p(ind, "};")
}

// writeField is one field's (or arm's) write.
func (e *jsonEmit) writeField(ind int, base string, f *ir.Field, desc, elem string) {
	pl := fieldPlace(base, f)
	if f.Array == ir.ArrayNone {
		e.p(ind, "return %s;", e.write(f, pl, desc, "depth"))
		return
	}
	el := elemOf(f)
	body := strings.ReplaceAll(e.write(el, pl.at("k"), elem, "d"), "(out, ", "(o, ")
	if f.KeyEnum != "" {
		names := e.names(f.KeyEnumRef.Name, f.KeyEnumRef.Variants)
		e.p(ind, "return schema_rt::json_write_keyed(out, %s, depth) { o, k, d => %s };", names, body)
		return
	}
	count := fmt.Sprint(slotCount(f))
	if f.Array == ir.ArrayCounted {
		count = pl.val + "Count"
	}
	e.p(ind, "return schema_rt::json_write_array(out, %s, depth) { o, k, d => %s };", count, body)
}

func (e *jsonEmit) emitStruct(st *ir.Struct) {
	n := st.Name
	typ := e.structType(st)
	shapes := make([]byte, len(st.Fields))
	for i, f := range st.Fields {
		shapes[i] = fieldShape(f)
	}
	for _, f := range st.Fields {
		e.declareNames(f)
	}
	e.declare("_"+n+"JsonShapes", "_"+n+"JsonRead", "_"+n+"JsonWrite")
	e.p(0, "let _%sJsonShapes: const u8[:] = %q;\n", n, string(shapes))

	e.p(0, "// _%sJsonRead is %s's accessor for the text walk's reads (docs/SPEC-TABLES.md §16.2).", n, n)
	e.p(0, "fn _%sJsonRead(jr: schema_rt::JsonIn&, v: %s&, o: i64, op: i64, depth: i64) -> bool {", n, typ)
	for i, f := range st.Fields {
		desc := fmt.Sprintf("_%sFields[%d]", n, i)
		elem := fmt.Sprintf("_%s_e%d", n, i)
		e.p(1, "if o == %d {", i)
		if f.Type.Optional {
			present := presentOf("v", f)
			e.p(2, "if op == schema_rt::JsonOpAbsent {")
			e.p(3, "%s = %s;", fieldPlace("v", f).val, e.zero(f))
			e.p(3, "%s = false;", present)
			e.p(3, "return true;")
			e.p(2, "}")
			e.p(2, "if op == schema_rt::JsonOpPresent {")
			e.p(3, "%s = true;", present)
			e.p(3, "return true;")
			e.p(2, "}")
		}
		e.readField(2, "v", f, desc, elem)
		e.p(1, "}")
	}
	e.p(1, "return false;")
	e.p(0, "}\n")

	e.p(0, "// _%sJsonWrite is %s's accessor for the text walk's writes (docs/SPEC-TABLES.md §16.1).", n, n)
	e.p(0, "fn _%sJsonWrite(out: u8[>..]&, v: const %s&, o: i64, op: i64, depth: i64) -> bool {", n, typ)
	for i, f := range st.Fields {
		desc := fmt.Sprintf("_%sFields[%d]", n, i)
		elem := fmt.Sprintf("_%s_e%d", n, i)
		e.p(1, "if o == %d {", i)
		ask := "true"
		if f.Type.Optional {
			ask = presentOf("v", f)
		}
		e.p(2, "if op == schema_rt::JsonOpAsk { return %s; }", ask)
		e.writeField(2, "v", f, desc, elem)
		e.p(1, "}")
	}
	e.p(1, "return false;")
	e.p(0, "}\n")
}

func (e *jsonEmit) emitUnion(un *ir.Union) {
	n := un.Name
	typ := e.unionType(un)
	shapes := make([]byte, len(un.Variants))
	for i, v := range un.Variants {
		if v.F == nil {
			shapes[i] = 'z'
			continue
		}
		shapes[i] = fieldShape(armField(v))
		e.declareNames(armField(v))
	}
	e.declare("_"+n+"JsonShapes", "_"+n+"JsonRead", "_"+n+"JsonWrite")
	e.p(0, "let _%sJsonShapes: const u8[:] = %q;\n", n, string(shapes))
	e.p(0, "// _%sJsonRead reads union %s's arm `tag`, or sets its tag (docs/SPEC-TABLES.md §16.2).", n, n)
	e.p(0, "fn _%sJsonRead(jr: schema_rt::JsonIn&, v: %s&, tag: i64, op: i64, depth: i64) -> bool {", n, typ)
	e.p(1, "if op == schema_rt::JsonOpTag {")
	e.p(2, "v.Type = tag as! %sType;", n)
	e.p(2, "return true;")
	e.p(1, "}")
	for i, v := range un.Variants {
		if v.F == nil {
			continue
		}
		f := armField(v)
		e.p(1, "if tag == %d {", i+1)
		if f.Array == ir.ArrayNone && !isStructOrUnion(f) && f.Type.Kind != ir.TString && f.Type.Kind != ir.TBytes {
			e.p(2, "%s = %s;", fieldPlace("v", f).val, e.zero(f))
		}
		e.readField(2, "v", f, fmt.Sprintf("_%sArms[%d]", n, i), fmt.Sprintf("_%s_a%d", n, i))
		e.p(1, "}")
	}
	e.p(1, "return false;")
	e.p(0, "}\n")
	e.p(0, "// _%sJsonWrite writes union %s's arm `tag` (docs/SPEC-TABLES.md §16.2).", n, n)
	e.p(0, "fn _%sJsonWrite(out: u8[>..]&, v: const %s&, tag: i64, depth: i64) -> bool {", n, typ)
	for i, v := range un.Variants {
		if v.F == nil {
			continue
		}
		e.p(1, "if tag == %d {", i+1)
		e.writeField(2, "v", armField(v), fmt.Sprintf("_%sArms[%d]", n, i), fmt.Sprintf("_%s_a%d", n, i))
		e.p(1, "}")
	}
	e.p(1, "return false;")
	e.p(0, "}\n")
}

func isStructOrUnion(f *ir.Field) bool {
	if f.Type.Kind != ir.TNamed {
		return false
	}
	switch f.Type.Ref.(type) {
	case *ir.Struct, *ir.Union:
		return true
	}
	return false
}

// emitRoot is §16.1's surface for one table.
func (e *jsonEmit) emitRoot(st *ir.Struct) {
	n := st.Name
	typ := e.structType(st)
	e.declare(n+"FromJsonTolerant", n+"FromJson", n+"ToJson", n+"ToJsonMeasure")
	e.p(0, "// %sFromJsonTolerant fills v from one text (docs/SPEC-TABLES.md §16.1): false, with", n)
	e.p(0, "// report.malformed, when the text is not JSON or a value is refused by name, and v is")
	e.p(0, "// then back at its declared defaults.")
	e.p(0, "fn %sFromJsonTolerant(v: %s&, text: const u8[:], report: schema_rt::JsonReport&) -> bool {", n, typ)
	e.p(1, "v = %s { .. };", typ)
	e.p(1, "if schema_rt::json_from(v, %sDescriptor, _%sJsonShapes, text, report, _%sJsonRead) { return true; }", n, n, n)
	e.p(1, "v = %s { .. };", typ)
	e.p(1, "return false;")
	e.p(0, "}\n")
	e.p(0, "// %sFromJson is the strict read: the text must match the schema exactly (§6.5).", n)
	e.p(0, "fn %sFromJson(v: %s&, text: const u8[:], report: schema_rt::JsonReport&) -> bool {", n, typ)
	e.p(1, "if !%sFromJsonTolerant(v, text, report) { return false; }", n)
	e.p(1, "return schema_rt::json_clean(report);")
	e.p(0, "}\n")
	e.p(0, "// %sToJson appends v's canonical text and answers its length, or -1 (§16.1, §16.3).", n)
	e.p(0, "fn %sToJson(v: const %s&, out: u8[>..]&) -> i64 {", n, typ)
	e.p(1, "return schema_rt::json_to(v, %sDescriptor, out, _%sJsonWrite);", n, n)
	e.p(0, "}\n")
	e.p(0, "// %sToJsonMeasure is %sToJson's exact length, or -1.", n, n)
	e.p(0, "fn %sToJsonMeasure(v: const %s&) -> i64 {", n, typ)
	e.p(1, "return schema_rt::json_measure(v, %sDescriptor, _%sJsonWrite);", n, n)
	e.p(0, "}\n")
}
