package mootable

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE LITERAL: an engine instance (internal/tabletext) spelled as the moo that
// builds the same record, field for field, by the storage rules of
// values.go's slots — so a moo value can be put in front of the writer
// without going through the reader, and the reader's value compared against
// one the reader did not build. This is what keeps json-read and json-write
// two gates rather than one round trip (docs/SPEC-TABLES.md §16.5, the
// per-backend negative control: the read goes red, the write stays green).

type literal struct {
	u    *ir.Unit
	b    strings.Builder
	next int
	// nodes are pointer targets and blobs by identity: one slot of R each,
	// so a shared node is one moo table and a shared blob one buffer
	nodes map[any]int
}

func newLiteral(u *ir.Unit) *literal {
	return &literal{u: u, nodes: map[any]int{}}
}

func (l *literal) pf(format string, args ...any) { fmt.Fprintf(&l.b, format, args...) }

func (l *literal) slot() string {
	l.next++
	return fmt.Sprintf("R[%d]", l.next)
}

// ctor is the constructor of a named record: a table, table union, map
// entry or text list element lives in <Base>Table, a packet type in <Base>.
func (l *literal) ctor(name string) string {
	base := l.u.DeclFile[name]
	if base == "" {
		// a generated record (a map entry, a text list element) is not a
		// declaration: it lives in the table module of the file holding it
		for _, f := range l.u.Files {
			for _, t := range f.Tables {
				if t.Name == name {
					base = f.Base
				}
			}
		}
		return fmt.Sprintf("MOD[%q].New%s()", base+"Table", name)
	}
	mod := base
	if _, ok := l.u.Tables[name]; ok {
		mod = base + "Table"
	}
	if _, ok := l.u.TableUnions[name]; ok {
		mod = base + "Table"
	}
	return fmt.Sprintf("MOD[%q].New%s()", mod, name)
}

// modules is every module the literal's constructors name.
func (l *literal) modules() []string {
	seen := map[string]bool{}
	for _, f := range l.u.Files {
		seen[f.Base] = true
		seen[f.Base+"Table"] = true
	}
	for name := range l.u.DeclFile {
		base := l.u.DeclFile[name]
		seen[base] = true
		if _, ok := l.u.Tables[name]; ok {
			seen[base+"Table"] = true
		}
		if _, ok := l.u.TableUnions[name]; ok {
			seen[base+"Table"] = true
		}
	}
	return sortedKeys(seen)
}

func mooString(b []byte) string {
	var s strings.Builder
	s.WriteByte('"')
	for _, c := range b {
		fmt.Fprintf(&s, "\\x%02x", c)
	}
	s.WriteByte('"')
	return s.String()
}

func limbs(v *big.Int, n int) string {
	x := new(big.Int).Set(v)
	if x.Sign() < 0 {
		x.Add(x, new(big.Int).Lsh(big.NewInt(1), uint(32*n)))
	}
	parts := make([]string, n)
	mask := big.NewInt(0xffffffff)
	for i := range n {
		parts[i] = new(big.Int).And(new(big.Int).Rsh(x, uint(32*i)), mask).String()
	}
	return strings.Join(parts, ", ")
}

func wideLit(class string, v *big.Int) string {
	return fmt.Sprintf("S.%s.new(%s)", class, limbs(v, classWidth(class)/32))
}

// scalarExpr is one non-array value of field f (element semantics when the
// caller strips the array): the moo expression of its storage.
func (l *literal) scalarExpr(f *ir.Field, cell *tabletext.Cell) string {
	t := f.Type
	switch {
	case t.Pointer && t.Blob():
		if cell.Blob == nil {
			return "nil"
		}
		if k, ok := l.nodes[cell.Blob]; ok {
			return fmt.Sprintf("R[%d]", k)
		}
		s := l.slot()
		l.nodes[cell.Blob] = l.next
		l.pf("%s = buffer.fromstring(%s)\n", s, mooString(cell.Blob.Data))
		return s
	case t.Pointer:
		if cell.Node == nil {
			return "nil"
		}
		return l.instance(cell.Node)
	case t.TextUnbounded():
		return mooString(cell.Str)
	}
	if t.Timestamp {
		return wideLit("Int64", big.NewInt(cell.I))
	}
	switch t.Kind {
	case ir.TBool:
		return fmt.Sprint(cell.B)
	case ir.TFloat32:
		return fmt.Sprintf("0x%08x", math.Float32bits(float32(cell.F)))
	case ir.TFloat64:
		b := math.Float64bits(cell.F)
		return fmt.Sprintf("S.Uint64.new(0x%08x, 0x%08x)", uint32(b), uint32(b>>32))
	case ir.TString:
		return mooString(cell.Str)
	}
	if t.Kind == ir.TNamed {
		switch r := t.Ref.(type) {
		case *ir.Enum:
			return fmt.Sprint(cell.U)
		case *ir.Flags:
			return wideLit("Uint64", new(big.Int).SetUint64(cell.U))
		case *ir.Struct:
			if cell.Tab == nil {
				return l.ctor(r.Name)
			}
			return l.instance(cell.Tab)
		case *ir.Union:
			return l.union(r, cell)
		}
	}
	if kind := ir.TableScalarKind(f); ir.TableKindWide(kind) {
		raw := tabletext.WideValue(cell)
		switch storageClass(f) {
		case "fx":
			return fmt.Sprintf("S.Int64.toNumberRaw(%s)", wideLit("Int64", raw))
		case "n":
			return raw.String()
		}
		return wideLit(wideClass(f), raw)
	}
	switch c := wideClass(f); c {
	case "Int64", "Int128":
		return wideLit(c, big.NewInt(cell.I))
	case "Uint64", "Uint128":
		return wideLit(c, new(big.Int).SetUint64(cell.U))
	}
	if t.Kind == ir.TInt && t.Signed {
		return fmt.Sprint(cell.I)
	}
	return fmt.Sprint(cell.U)
}

func (l *literal) union(un *ir.Union, cell *tabletext.Cell) string {
	s := l.slot()
	l.pf("%s = %s\n", s, l.ctor(un.Name))
	if cell.U == 0 {
		return s
	}
	v := un.Variants[cell.U-1]
	l.pf("%s.Type = %d\n", s, cell.U)
	switch {
	case v.Void():
	case cell.Tab != nil:
		l.pf("%s.%s = %s\n", s, ir.GoExportName(v.Name), l.instance(cell.Tab))
	case cell.Arm != nil:
		arm := armField(v)
		fv := *cell.Arm
		fv.Def = arm
		l.field(s, &fv)
	}
	return s
}

// instance builds one record into a slot of R and returns the slot.
func (l *literal) instance(inst *tabletext.Instance) string {
	if k, ok := l.nodes[inst]; ok {
		return fmt.Sprintf("R[%d]", k)
	}
	s := l.slot()
	l.nodes[inst] = l.next
	l.pf("%s = %s\n", s, l.ctor(inst.Def.Name))
	for i := range inst.Fields {
		l.field(s, &inst.Fields[i])
	}
	return s
}

func bufferLit(size int64, data []byte) string {
	return fmt.Sprintf("(function() local b = buffer.create(%d); buffer.writestring(b, 0, %s); return b end)()", size, mooString(data))
}

func unitsBytes(units []uint16) []byte {
	out := make([]byte, 0, 2*len(units))
	for _, u := range units {
		out = binary.LittleEndian.AppendUint16(out, u)
	}
	return out
}

// field assigns one field's storage on the record at rec.
func (l *literal) field(rec string, fv *tabletext.Field) {
	f := fv.Def
	t := f.Type
	name := ir.GoExportName(f.Name)
	at := rec + "." + name
	elem := func(i int) string {
		e := *f
		e.Array = ir.ArrayNone
		return l.scalarExpr(&e, &fv.Elems[i])
	}
	count := len(fv.Elems)
	if f.CountedOnWire() && f.Array != ir.ArrayList {
		count = fv.Count
	}
	switch {
	case f.IsMap():
		l.pf("%s = {}\n", at)
		for _, e := range fv.Entries {
			l.pf("table.insert(%s, %s)\n", at, l.instance(e.Tab))
		}
	case f.Array == ir.ArrayList && (t.Pointer || t.TextUnbounded()):
		l.pf("%s = {}\n", at)
		for i := range fv.Elems {
			v := elem(i)
			if v == "nil" {
				v = "false"
			}
			l.pf("table.insert(%s, %s)\n", at, v)
		}
	case f.Array == ir.ArrayList && isBuffer(t):
		l.pf("%s = {}\n%sLength = {}\n", at, at)
		for i := range fv.Elems {
			data, n := fv.Elems[i].Str, len(fv.Elems[i].Str)
			if t.Kind == ir.TWString {
				data, n = unitsBytes(fv.Elems[i].Units), len(fv.Elems[i].Units)
			}
			l.pf("table.insert(%s, %s)\ntable.insert(%sLength, %d)\n", at, bufferLit(bufferSize(t), data), at, n)
		}
	case f.Array == ir.ArrayList:
		l.pf("%s = {}\n", at)
		for i := range fv.Elems {
			l.pf("table.insert(%s, %s)\n", at, elem(i))
		}
	case f.Array != ir.ArrayNone && (t.Pointer || t.TextUnbounded()):
		for i := 0; i < count; i++ {
			l.pf("%s[%d] = %s\n", at, i+1, elem(i))
		}
	case f.Array != ir.ArrayNone && isByteElem(f):
		for i := 0; i < count; i++ {
			l.pf("buffer.writeu8(%s, %d, %d)\n", at, i, fv.Elems[i].U)
		}
	case f.Array != ir.ArrayNone && isBuffer(t):
		for i := 0; i < count; i++ {
			data, n := fv.Elems[i].Str, len(fv.Elems[i].Str)
			if t.Kind == ir.TWString {
				data, n = unitsBytes(fv.Elems[i].Units), len(fv.Elems[i].Units)
			}
			l.pf("%s[%d] = %s\n%sLength[%d] = %d\n", at, i+1, bufferLit(bufferSize(t), data), at, i+1, n)
		}
	case f.Array != ir.ArrayNone:
		for i := 0; i < count; i++ {
			l.pf("%s[%d] = %s\n", at, i+1, elem(i))
		}
	case t.Pointer || t.TextUnbounded():
		l.pf("%s = %s\n", at, l.scalarExpr(f, &fv.Cell))
	case isBuffer(t):
		data, n := fv.Cell.Str, len(fv.Cell.Str)
		if t.Kind == ir.TWString {
			data, n = unitsBytes(fv.Cell.Units), len(fv.Cell.Units)
		}
		l.pf("%s = %s\n%sLength = %d\n", at, bufferLit(bufferSize(t), data), at, n)
	default:
		l.pf("%s = %s\n", at, l.scalarExpr(f, &fv.Cell))
	}
	if f.Array == ir.ArrayCounted {
		l.pf("%sCount = %d\n", at, fv.Count)
	}
	if t.Optional {
		l.pf("%s.%s = %v\n", rec, ir.GoExportName(f.Name+"_present"), fv.Present)
	}
}

// literalPrelude is what a probe built on literals carries: the serialize
// classes, the module table and R, and deepEqual, which compares two values
// as storage (tables key by key both ways, buffers by bytes, wide values by
// their class's ==) and returns the first path that differs.
const literalPrelude = `local S: any = require("./serialize")
local MOD: { [string]: any } = {}
local R: { [number]: any } = {}
local function deepEqual(a: any, b: any, path: string): string?
	if type(a) ~= type(b) then
		return path .. ": " .. type(a) .. " vs " .. type(b)
	end
	if type(a) == "buffer" then
		if buffer.tostring(a) ~= buffer.tostring(b) then
			return path .. ": buffers differ"
		end
		return nil
	end
	if type(a) == "table" then
		if getmetatable(a) ~= nil then
			if a == b then
				return nil
			end
			return path .. ": " .. tostring(a) .. " vs " .. tostring(b)
		end
		for k, v in a do
			local d = deepEqual(v, b[k], path .. "." .. tostring(k))
			if d ~= nil then
				return d
			end
		end
		for k, v in b do
			if a[k] == nil then
				return path .. "." .. tostring(k) .. ": missing on the left (" .. tostring(v) .. ")"
			end
		end
		return nil
	end
	if a ~= b then
		return path .. ": " .. tostring(a) .. " vs " .. tostring(b)
	end
	return nil
end
local _ = deepEqual
local _ = R
`
