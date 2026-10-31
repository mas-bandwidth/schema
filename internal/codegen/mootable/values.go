package mootable

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE VALUE MAPPING (docs/PORT-MOO.md). A table record stores every field the
// way the packet surface (internal/codegen/moo) stores it — the same rule, so a
// packet type a table reaches by value is built by its packet constructor and
// read and written here without a second shape — and adds the table
// constructs of docs/SPEC-TABLES.md §2: a pointer is T? (nil is null), a blob
// *string and *bytes are buffer? (a blob is a node of identity), an unbounded string is string?, a
// []T and a map are plain arrays born empty, and ?T carries <Name>Present.
//
// fixedIsNumber and wideClass are the packet generator's own rules
// (internal/codegen/moo, moo.go), restated here because that package keeps
// them unexported; TestMooTableStorageMatchesThePacketSurface holds the two to
// one answer.

// fixedIsNumber: a fixed(I, 16) or ufixed(I, 16) of 32 or 64 storage bits
// whose bounds moo holds is a moo number, its own Q48.16 value.
func fixedIsNumber(f *ir.Field) bool {
	t := f.Type
	return t.Kind == ir.TFixed && t.FracBits == 16 && (t.Width == 32 || t.Width == 64) &&
		f.IntMin != nil && f.IntMax != nil &&
		f.IntMin.Cmp(neg47m1) >= 0 && f.IntMax.Cmp(two47m1) <= 0
}

// wideClass is the serialize.moo class a value of the field's kind stores as,
// or "" for a number, boolean, string or table.
func wideClass(f *ir.Field) string {
	t := f.Type
	switch t.Kind {
	case ir.TInt:
		switch {
		case t.Width == 128 && t.Signed:
			return "Int128"
		case t.Width == 128:
			return "Uint128"
		case t.Width == 64 && t.Signed:
			return "Int64"
		case t.Width == 64:
			return "Uint64"
		}
	case ir.TBits:
		if t.Width > 32 {
			return "Uint64"
		}
	case ir.TFixed:
		if fixedIsNumber(f) || t.Width <= 32 {
			return ""
		}
		switch {
		case t.Width == 128 && t.Signed:
			return "Int128"
		case t.Width == 128:
			return "Uint128"
		case t.Signed:
			return "Int64"
		}
		return "Uint64"
	case ir.TFloat64:
		return "Uint64"
	case ir.TNamed:
		if _, ok := t.Ref.(*ir.Flags); ok {
			return "Uint64"
		}
	}
	return ""
}

func classWidth(class string) int {
	if strings.HasSuffix(class, "128") {
		return 128
	}
	return 64
}

// storageClass is the runtime's name for an integer-shaped value's storage:
// "n" a moo number holding the integer, "fx" a moo number holding a Q48.16
// value in whole units, or the wide class's short name.
func storageClass(f *ir.Field) string {
	if fixedIsNumber(f) {
		return "fx"
	}
	switch wideClass(f) {
	case "Int64":
		return "I64"
	case "Uint64":
		return "U64"
	case "Int128":
		return "I128"
	case "Uint128":
		return "U128"
	}
	return "n"
}

func isByteElem(f *ir.Field) bool {
	return f.Type.Kind == ir.TInt && !f.Type.Signed && f.Type.Width == 8 && !f.HasIntRange
}

func isBuffer(t ir.FieldType) bool { return t.Kind == ir.TBytes || t.Kind == ir.TWString }

func bufferSize(t ir.FieldType) int64 {
	if t.Kind == ir.TWString {
		return 2 * t.Size
	}
	return t.Size
}

func isFresh(t ir.FieldType) bool {
	if t.Kind == ir.TNamed {
		switch t.Ref.(type) {
		case *ir.Struct, *ir.Union:
			return true
		}
	}
	return isBuffer(t)
}

// moduleOf is the module a named declaration lives in: a table, a table
// union and a map entry live in their file's <Base>Table module; a packet
// type, union, enum or flags set in the packet module <Base>.
func (g *Gen) moduleOf(name string) string {
	b := g.Unit.DeclFile[name]
	if _, ok := g.Unit.Tables[name]; ok && b != "" {
		return b + "Table"
	}
	if _, ok := g.Unit.TableUnions[name]; ok && b != "" {
		return b + "Table"
	}
	for _, f := range g.Unit.Files {
		for _, t := range f.Tables {
			if t.Name == name {
				return f.Base + "Table"
			}
		}
	}
	return g.Unit.DeclFile[name]
}

// TypeRef is a named declaration's exported type from this module.
func (g *Gen) TypeRef(name string) string {
	mod := g.moduleOf(name)
	if mod == g.File.Base+g.Suffix {
		return name
	}
	return g.Require(mod) + "." + name
}

// Ctor is the expression building a fresh value of a named type or union.
func (g *Gen) Ctor(name string) string {
	mod := g.moduleOf(name)
	if mod == g.File.Base+g.Suffix {
		return g.Self + ".New" + name + "()"
	}
	return g.Require(mod) + ".New" + name + "()"
}

// scalar is a non-array value's annotation and its construction: the
// specified default where declared, else the §5 zero form.
func (g *Gen) scalar(f *ir.Field, withDefault bool) (typ, init string) {
	t := f.Type
	def := withDefault && f.HasDefault
	defInt := new(big.Int)
	if def && f.DefInt != nil {
		defInt = f.DefInt
	}
	if c := wideClass(f); c != "" {
		v := defInt
		if t.Kind == ir.TFloat64 {
			v = new(big.Int)
			if def {
				v.SetUint64(math.Float64bits(f.DefFloat))
			}
		}
		return "any", g.K(WideNew(c, v, classWidth(c)))
	}
	switch t.Kind {
	case ir.TBool:
		return "boolean", strconv.FormatBool(def && f.DefBool)
	case ir.TFloat32:
		if def {
			return "number", fmt.Sprintf("0x%08x", math.Float32bits(float32(f.DefFloat)))
		}
		return "number", "0"
	case ir.TFixed:
		// a fixed default is held RAW (ir.Field.DefInt)
		if fixedIsNumber(f) {
			return "number", fixedNumberLit(defInt)
		}
		return "number", defInt.String()
	case ir.TInt, ir.TBits:
		return "number", defInt.String()
	case ir.TString:
		if def && len(f.DefBytes) > 0 {
			return "string", luaString(f.DefBytes)
		}
		return "string", `""`
	case ir.TNamed:
		switch r := t.Ref.(type) {
		case *ir.Enum:
			v := 0
			for i, variant := range r.Variants {
				if def && variant == f.DefVariant {
					v = i + 1
				}
			}
			return "number", strconv.Itoa(v)
		case *ir.Struct, *ir.Union:
			return g.TypeRef(t.Name), g.Ctor(t.Name)
		}
	}
	return "any", "nil"
}

type slot struct{ name, typ, init, comment string }

// elemType is an array element's annotation.
func (g *Gen) elemType(f *ir.Field) string {
	t := f.Type
	switch {
	case t.TextUnbounded():
		return "string"
	case t.Pointer && t.Blob():
		// a blob is a NODE with identity (docs/SPEC-TABLES.md §2.5): two
		// slots naming one record share one buffer, so *string is a buffer
		// too, never a moo string, whose equal values cannot be told apart
		return "buffer"
	case t.Pointer:
		return g.TypeRef(t.Name)
	case isBuffer(t):
		return "buffer"
	}
	e := *f
	e.Array = ir.ArrayNone
	e.HasDefault = false
	e.Type.Optional = false
	typ, _ := g.scalar(&e, false)
	return typ
}

// slots is a field's storage: the value, then the used-length or count
// companion beside it, then an optional field's presence.
func (g *Gen) slots(f *ir.Field) []slot {
	name := ir.GoExportName(f.Name)
	t := f.Type
	var out []slot
	switch {
	case ir.TextListPlain(f) && f.Array == ir.ArrayList:
		// a PLAIN TEXT LIST's element is its text (docs/SPEC-TABLES.md
		// §2.13): kind 12, its own L and bytes, a moo string; the optional
		// spelling keeps its generated record, whose presence frames it
		out = append(out, slot{name, "{string}", "{}", "a text list: its length is the data's (docs/SPEC-TABLES.md §2.13)"})
	case ir.TextListPlain(f):
		out = append(out, slot{name, "{string}", fmt.Sprintf("table.create(%d, \"\")", f.ArrayBound), "a text list (docs/SPEC-TABLES.md §2.13)"})
	case f.IsMap():
		out = append(out, slot{name, "{" + g.TypeRef(f.MapEntry.Name) + "}", "{}", "a map: its entries, any order; the save sorts by key (docs/SPEC-TABLES.md §2.8)"})
	case f.Array == ir.ArrayList && t.Pointer:
		out = append(out, slot{name, "{" + g.elemType(f) + " | false}", "{}", "a list of pointers: a null is false, never nil (docs/SPEC-TABLES.md §2.9)"})
	case f.Array == ir.ArrayList && isBuffer(t):
		out = append(out, slot{name, "{buffer}", "{}", "a list: its length is the data's (docs/SPEC-TABLES.md §2.9)"})
		out = append(out, slot{name + "Length", "{number}", "{}", ""})
	case f.Array == ir.ArrayList:
		out = append(out, slot{name, "{" + g.elemType(f) + "}", "{}", "a list: its length is the data's (docs/SPEC-TABLES.md §2.9)"})
	case f.Array != ir.ArrayNone && (t.Pointer || t.TextUnbounded()):
		out = append(out, slot{name, "{" + g.elemType(f) + "?}", fmt.Sprintf("table.create(%d) :: {%s?}", f.ArrayBound, g.elemType(f)), "nil is null (docs/SPEC-TABLES.md §2.4)"})
	case t.Pointer && t.Blob() && t.Kind == ir.TString:
		out = append(out, slot{name, "buffer?", "nil", "a string buffer, UTF-8: nil is absent (docs/SPEC-TABLES.md §2.5)"})
	case t.Pointer && t.Blob():
		out = append(out, slot{name, "buffer?", "nil", "a bytes buffer: nil is absent (docs/SPEC-TABLES.md §2.5)"})
	case t.TextUnbounded():
		out = append(out, slot{name, "string?", "nil", "unbounded text (docs/SPEC-TABLES.md §2.13.1)"})
	case t.Pointer:
		out = append(out, slot{name, g.TypeRef(t.Name) + "?", "nil", "a pointer: nil is null (docs/SPEC-TABLES.md §2.4)"})
	case f.Array != ir.ArrayNone && isByteElem(f):
		out = append(out, slot{name, "buffer", fmt.Sprintf("buffer.create(%d)", f.ArrayBound), "[N]uint8: a buffer, 0-based"})
	case f.Array != ir.ArrayNone && isBuffer(t):
		out = append(out, slot{name, "{buffer}", fmt.Sprintf("table.create(%d) :: {buffer}", f.ArrayBound), ""})
		out = append(out, slot{name + "Length", "{number}", fmt.Sprintf("table.create(%d, 0)", f.ArrayBound), ""})
	case f.Array != ir.ArrayNone:
		e := *f
		e.Array = ir.ArrayNone
		e.HasDefault = false
		e.Type.Optional = false
		typ, init := g.scalar(&e, false)
		if isFresh(t) {
			out = append(out, slot{name, "{" + typ + "}", fmt.Sprintf("table.create(%d) :: {%s}", f.ArrayBound, typ), ""})
		} else {
			out = append(out, slot{name, "{" + typ + "}", fmt.Sprintf("table.create(%d, %s)", f.ArrayBound, init), ""})
		}
	case isBuffer(t):
		what := fmt.Sprintf("bytes(%d)", t.Size)
		if t.Kind == ir.TWString {
			what = fmt.Sprintf("wstring(%d): UTF-16LE code units", t.Size)
		}
		out = append(out, slot{name, "buffer", fmt.Sprintf("buffer.create(%d)", bufferSize(t)), what + ", used length beside it (SPEC §4.7)"})
		out = append(out, slot{name + "Length", "number", "0", ""})
	default:
		typ, init := g.scalar(f, true)
		c := ""
		if f.HasDefault {
			c = "specified default (SPEC §5)"
		}
		out = append(out, slot{name, typ, init, c})
	}
	if f.Array == ir.ArrayCounted {
		out = append(out, slot{name + "Count", "number", strconv.FormatInt(f.BornCount(), 10), "a [A..N] count is born at A (SPEC §4.6)"})
	}
	if t.Optional {
		out = append(out, slot{ir.GoExportName(f.Name + "_present"), "boolean", "false", "?T presence (docs/SPEC-TABLES.md §2.3)"})
	}
	return out
}

// fillFresh is the statements that build a fixed or counted array's fresh
// elements and write a bytes default, after the value's table exists.
func (g *Gen) fillFresh(f *ir.Field, path, ind string) {
	name := path + "." + ir.GoExportName(f.Name)
	t := f.Type
	if f.Array != ir.ArrayNone && f.Array != ir.ArrayList && !ir.TextListPlain(f) && !t.Pointer && !t.TextUnbounded() && isFresh(t) && !isByteElem(f) && !f.IsMap() {
		var ctor string
		if isBuffer(t) {
			ctor = fmt.Sprintf("buffer.create(%d)", bufferSize(t))
		} else {
			ctor = g.Ctor(t.Name)
		}
		g.Pf("%sfor i = 1, %d do\n%s\t%s[i] = %s\n%send\n", ind, f.ArrayBound, ind, name, ctor, ind)
	}
	if f.HasDefault && t.Kind == ir.TBytes && !t.Pointer && f.Array == ir.ArrayNone && len(f.DefBytes) > 0 {
		g.Pf("%sbuffer.writestring(%s, 0, %s)\n%s%sLength = %d\n", ind, name, luaString(f.DefBytes), ind, name, len(f.DefBytes))
	}
}

// emitStruct writes a table's (or map entry's) exported type and its
// constructor, New<Name>(): every field present, at its declared default.
func (g *Gen) emitStruct(d *ir.Struct) {
	g.Pf("%s", ir.DocComment(d.Doc, "", "--"))
	what := "table"
	if d.MapEntryOf != "" {
		what = "generated record of " + d.MapEntryOf + ","
	}
	g.Pf("-- %s %s\n", what, d.Name)
	g.Pf("export type %s = {\n", d.Name)
	for _, f := range d.Fields {
		for _, s := range g.slots(f) {
			g.Pf("\t%s: %s,\n", s.name, s.typ)
		}
	}
	g.Pf("}\n\n")
	g.Pf("-- New%s builds the record at its defaults (docs/SPEC-TABLES.md §2.3, SPEC §5).\n", d.Name)
	g.Pf("function %s.New%s(): %s\n", g.Self, d.Name, d.Name)
	g.Pf("\tlocal value: %s = {\n", d.Name)
	for _, f := range d.Fields {
		g.Pf("%s", ir.DocComment(f.Doc, "\t\t", "--"))
		for _, s := range g.slots(f) {
			c := ""
			if s.comment != "" {
				c = " -- " + s.comment
			}
			g.Pf("\t\t%s = %s,%s\n", s.name, s.init, c)
		}
	}
	g.Pf("\t}\n")
	for _, f := range d.Fields {
		g.fillFresh(f, "value", "\t")
	}
	g.Pf("\treturn value\nend\n\n")
}

// armField is an arm as a field line stored under the arm's own name.
func armField(v ir.UnionVariant) *ir.Field {
	f := *v.F
	f.Name = v.Name
	return &f
}

// emitUnion writes a table-side union (docs/SPEC-TABLES.md §2.6): the tag
// table, the type (the tag beside one slot per arm) and its constructor.
func (g *Gen) emitUnion(d *ir.Union) {
	tag := d.Name + "Type"
	members := make([]string, len(d.Variants))
	for i, v := range d.Variants {
		members[i] = ir.GoExportName(v.Name)
	}
	g.Pf("-- %s: union %s's tag — None = 0, then each variant in declared order (SPEC §4.8)\n", tag, d.Name)
	g.Pf("export type %s = number\n", tag)
	g.Pf("%s.%s = table.freeze({\n\tNone = 0,\n", g.Self, tag)
	for i, m := range members {
		g.Pf("%s", ir.DocComment(d.Variants[i].Doc, "\t", "--"))
		g.Pf("\t%s = %d,\n", m, i+1)
	}
	g.Pf("})\n")
	g.Pf("%s.%sMax = %d -- the exported extent (SPEC §4.2)\n\n", g.Self, tag, d.Max)
	g.Pf("%s", ir.DocComment(d.Doc, "", "--"))
	g.Pf("-- %s — at most one arm; Type says which. Construction is the empty union\n", d.Name)
	g.Pf("-- (None); every arm is pre-allocated, and an unselected arm is unspecified.\n")
	g.Pf("export type %s = {\n\tType: number,\n", d.Name)
	for _, v := range d.Variants {
		if v.Void() {
			continue
		}
		for _, s := range g.slots(armField(v)) {
			g.Pf("\t%s: %s,\n", s.name, s.typ)
		}
	}
	g.Pf("}\n\n")
	g.Pf("function %s.New%s(): %s\n\tlocal value: %s = {\n\t\tType = 0,\n", g.Self, d.Name, d.Name, d.Name)
	for _, v := range d.Variants {
		if v.Void() {
			continue
		}
		for _, s := range g.slots(armField(v)) {
			g.Pf("\t\t%s = %s,\n", s.name, s.init)
		}
	}
	g.Pf("\t}\n")
	for _, v := range d.Variants {
		if !v.Void() {
			g.fillFresh(armField(v), "value", "\t")
		}
	}
	g.Pf("\treturn value\nend\n\n")
}
