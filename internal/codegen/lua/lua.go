// Package lua emits the Lua target's TYPE SURFACE: one module per schema
// file — Constants.schema -> Constants.lua — deterministic to the byte (SPEC
// §6.1). The design is docs/PORT-LUA.md; this file is THE TYPE MAPPING and
// THE RUNTIME LAYOUT of that page in code.
//
// A module is a Lua chunk that returns one table, `_M`: the unit's protocol
// id (home file only), every constant, every enum and union tag table (None,
// the variants, Max; an enum exports no Count, SPEC §4.2), every flags mask, and a constructor New<Name>()
// per type and union that builds the value at its declared defaults. A module
// requires the modules it references by basename (require("Enums")), so the
// directory a unit is generated into goes on package.path; nothing is global.
//
// There is NO CODEC here. The type wire's read and write sides are later
// files of this package (read.go, write.go) that join Generate through the
// pass hook below, so no later card edits this file; the type-wire runtime
// they bring (runtime/*.lua, embedded by glob in those files) is emitted
// beside the modules through the runtime hook.
//
// THE VALUE MODEL (docs/PORT-LUA.md, 64-BIT AND 128-BIT INTEGERS): Lua 5.4's
// integer subtype is 64-bit two's complement, so every integer of 64 bits or
// fewer — intN, uintN, bits(N), an enum, a flags mask, a fixed(I, F) raw
// value whose storage is 64 bits or fewer — is ONE Lua integer, a uint64 being
// the same 64 bits read unsigned (math.ult, %u-free decimal writers in the
// text card). A 128-bit value (int128, uint128, a 128-bit fixed raw) is a
// table { lo = <low 64 bits>, hi = <high 64 bits> }, each half an integer.
// Floats are Lua floats (IEEE-754 doubles); a float32 field holds the double
// that is exactly its float32 value. string(N) and bytes(N) are Lua strings
// (immutable byte strings: the length IS the used length). wstring(N) is a
// sequence of UTF-16 code units. Arrays are 1-based sequences pre-filled to
// their bound (the storage principle, SPEC §6.1), and a counted array carries
// <Name>Count beside its storage, as the JavaScript target does.
package lua

import (
	"fmt"
	"io/fs"
	"maps"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"

	schemacheck "github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const ext = "lua"

// Module is one schema file's Lua module while it is being emitted. A pass
// (read.go, write.go) appends to Body and records the modules it references
// with Require; the header and the require block are assembled after every
// pass has run.
type Module struct {
	Unit *ir.Unit
	File *ir.File
	Body strings.Builder

	requires map[string]bool
	helpers  map[string]string
}

// Helper records a module-local helper function, emitted once after `local _M`
// and before the body, so every constructor that calls it finds it defined.
func (m *Module) Helper(name, src string) { m.helpers[name] = src }

// Printf appends formatted text to the module body.
func (m *Module) Printf(format string, args ...any) { fmt.Fprintf(&m.Body, format, args...) }

// Require records that the module references the declaration decl and
// returns the Lua expression that names it: `_M.<decl>` in its own file, or
// `<Base>.<decl>` with a require of <Base> otherwise.
func (m *Module) Require(decl string) string {
	base, ok := m.Unit.DeclFile[decl]
	if !ok || base == m.File.Base {
		return "_M." + decl
	}
	m.requires[base] = true
	return moduleLocal(base) + "." + decl
}

// Pass is a later surface's emission step, run once per schema file after the
// type declarations, in registration order. read.go and write.go join by
// init() (docs/PORT-LUA.md, THE RUNTIME LAYOUT).
type Pass func(m *Module) error

var passes []Pass

// RegisterPass adds a pass. Called from init() only.
func RegisterPass(p Pass) { passes = append(passes, p) }

var runtimes []fs.FS

// RegisterRuntime adds a file system whose runtime/*.lua files are emitted
// beside the modules under their own names. A later file embeds the runtime
// by glob (//go:embed runtime/*.lua) and registers it from init().
func RegisterRuntime(fsys fs.FS) { runtimes = append(runtimes, fsys) }

// Generate returns basename.lua -> file contents for every file of the unit,
// plus the registered runtime files.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	out := map[string][]byte{}
	rt, err := runtimeFiles()
	if err != nil {
		return nil, err
	}
	for _, f := range u.Files {
		if f.Base == "_M" {
			return nil, fmt.Errorf("schema file _M.schema collides with the module table every Lua module names _M; rename the file (docs/PORT-LUA.md)")
		}
		if _, clash := rt[f.Base+".lua"]; clash {
			return nil, fmt.Errorf("schema file %s.schema collides with the Lua runtime module %s.lua emitted beside it; rename the file (docs/PORT-LUA.md)", f.Base, f.Base)
		}
	}
	home := ir.ProtocolIdHome(u)
	for _, f := range u.Files {
		m := &Module{Unit: u, File: f, requires: map[string]bool{}, helpers: map[string]string{}}
		emitDecls(m, f.Base == home)
		for _, p := range passes {
			if err := p(m); err != nil {
				return nil, err
			}
		}
		m.Body.WriteString("return _M\n")
		out[f.Base+"."+ext] = Format(assemble(m, f.Base == home))
	}
	maps.Copy(out, rt)
	return out, nil
}

// runtimeFiles collects runtime/*.lua from every registered file system. Two
// registrations may carry the same file (read.go and write.go embed one glob);
// the same name with different bytes is a build error.
func runtimeFiles() (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, fsys := range runtimes {
		names, err := fs.Glob(fsys, "runtime/*.lua")
		if err != nil {
			return nil, err
		}
		for _, n := range names {
			data, err := fs.ReadFile(fsys, n)
			if err != nil {
				return nil, err
			}
			base := n[len("runtime/"):]
			if prev, ok := out[base]; ok && string(prev) != string(data) {
				return nil, fmt.Errorf("lua runtime %s is registered twice with different bytes", base)
			}
			out[base] = data
		}
	}
	return out, nil
}

// moduleLocal is the local a required module is bound to: its basename, with
// a trailing underscore so it can never shadow a Lua keyword or _M.
func moduleLocal(base string) string { return base + "_" }

func assemble(m *Module, home bool) []byte {
	var h strings.Builder
	// the basename, not the invocation-relative path (SPEC §6.1)
	fmt.Fprintf(&h, "-- Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", m.File.Base)
	h.WriteString("-- SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("-- your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("-- AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "-- package %s — protocol id 0x%016x\n", m.Unit.Package, m.Unit.ProtocolId)
	if home {
		h.WriteString("--\n")
		h.WriteString("-- Lua 5.4 values (docs/PORT-LUA.md): integers of 64 bits or fewer are Lua\n")
		h.WriteString("-- integers (a uint64 is the same bits read unsigned); 128-bit values are\n")
		h.WriteString("-- { lo = <int>, hi = <int> }; string(N) and bytes(N) are Lua strings.\n")
	}
	h.WriteString("\n")
	bases := make([]string, 0, len(m.requires))
	for b := range m.requires {
		bases = append(bases, b)
	}
	sort.Strings(bases)
	for _, b := range bases {
		fmt.Fprintf(&h, "local %s = require(%q)\n", moduleLocal(b), b)
	}
	if len(bases) > 0 {
		h.WriteString("\n")
	}
	h.WriteString("local _M = {}\n\n")
	names := make([]string, 0, len(m.helpers))
	for n := range m.helpers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		h.WriteString(m.helpers[n])
		h.WriteString("\n")
	}
	h.WriteString(m.Body.String())
	return []byte(h.String())
}

// emitDecls emits every declaration of the file in ir.EmissionOrder: a
// constant's initializer is a folded literal, so order matters only for
// reading, and the order is the other targets' (SPEC §6.1).
func emitDecls(m *Module, home bool) {
	if home {
		m.Printf("-- The unit's protocol id — the hash of its wire shape (SPEC §3.1), as the\n")
		m.Printf("-- 64 bits of a Lua integer (a hex literal wraps; read it unsigned).\n")
		m.Printf("_M.ProtocolId = 0x%016x\n\n", m.Unit.ProtocolId)
	}
	for _, d := range ir.EmissionOrder(m.File) {
		switch d := d.(type) {
		case *ir.Const:
			emitConst(m, d)
		case *ir.Enum:
			emitEnum(m, d)
		case *ir.Flags:
			emitFlags(m, d)
		case *ir.Struct:
			emitStruct(m, d)
		case *ir.Union:
			emitUnion(m, d)
		}
	}
}

// emitConst emits a schema constant (SPEC §4.2) as a folded literal, the
// schema expression riding as a comment where it was not a bare literal.
func emitConst(m *Module, d *ir.Const) {
	m.Printf("%s", ir.DocComment(d.Doc, "", "--"))
	note := ""
	if d.Expr != nil {
		r := ir.RenderExpr(d.Expr)
		if d.IsFloat || d.Int == nil || r != d.Int.String() {
			note = " -- = " + r
		}
	}
	if d.IsFloat {
		lit := FloatLiteral(d.Float)
		if d.Storage == "float32" {
			lit = FloatLiteral(float64(float32(d.Float)))
		}
		m.Printf("%s = %s%s\n\n", key("_M", d.Name), lit, note)
		return
	}
	wide := d.Storage == "int128" || d.Storage == "uint128" || !fitsUint64OrInt64(d.Int)
	if wide {
		m.Printf("%s = %s%s\n\n", key("_M", d.Name), Int128Literal(d.Int), note)
		return
	}
	m.Printf("%s = %s%s\n\n", key("_M", d.Name), IntLiteral(d.Int), note)
}

func emitEnum(m *Module, d *ir.Enum) {
	m.Printf("%s", ir.DocComment(d.Doc, "", "--"))
	m.Printf("-- %s — None = 0 implicit, variants dense from 1, wire range [0, %d] (SPEC §4.2)\n", d.Name, d.Max)
	emitTagTable(m, d.Name, d.Variants, d.VariantDocs, d.Max)
}

// emitTagTable emits an enum's or a union tag's value table and its
// EnumName<Name> debug namer (SPEC §4.2). Values are Lua integers.
func emitTagTable(m *Module, name string, members, docs []string, max int64) {
	m.Printf("_M.%s = {\n", name)
	m.Printf("  None = 0,\n")
	for i, v := range members {
		m.Printf("%s", ir.DocComment(docs[i], "  ", "--"))
		m.Printf("  %s = %d,\n", tableKey(v), i+1)
	}
	m.Printf("  Max = %d, -- the exported extent (SPEC §4.2)\n", max)
	m.Printf("}\n\n")
	m.Printf("-- EnumName%s: debug/log/tooling name for any %s wire value;\n", name, name)
	m.Printf("-- out-of-set values (wire-legal up to the declared max) name as \"???\"\n")
	m.Printf("do\n  local names = { [0] = \"None\"")
	for i, v := range members {
		m.Printf(", [%d] = %q", i+1, v)
	}
	m.Printf(" }\n")
	m.Printf("  function _M.EnumName%s(value)\n    return names[value] or \"???\"\n  end\nend\n\n", name)
}

// emitFlags emits a flags declaration's masks (SPEC §4.2): bit i is variant
// i, as a Lua integer, so bit 63 is math.mininteger's pattern.
func emitFlags(m *Module, d *ir.Flags) {
	m.Printf("%s", ir.DocComment(d.Doc, "", "--"))
	m.Printf("-- %s — one bit per variant, consumed as masks; storage is the uint64's 64\n", d.Name)
	m.Printf("-- bits in a Lua integer, wire %d bits (SPEC §4.2)\n", d.WireBits)
	for i, v := range d.Variants {
		m.Printf("%s", ir.DocComment(d.VariantDocs[i], "", "--"))
		m.Printf("_M.%s%s = 1 << %d\n", d.Name, v, i)
	}
	m.Printf("_M.%sCount = %d -- the declared variant count (SPEC §4.2)\n\n", d.Name, len(d.Variants))
	m.Printf("do\n  local bits = {")
	for i, v := range d.Variants {
		if i > 0 {
			m.Printf(",")
		}
		m.Printf(" [%d] = %q", i, v)
	}
	m.Printf(" }\n")
	m.Printf("  -- FlagName%s: the name of bit i, \"???\" past the declared variants\n", d.Name)
	m.Printf("  function _M.FlagName%s(bit)\n    return bits[bit] or \"???\"\n  end\n", d.Name)
	m.Printf("  -- FlagNames%s renders the set bits as \"A|B\", \"0\" for the empty set,\n", d.Name)
	m.Printf("  -- bits past the declared variants as hex\n")
	m.Printf("  function _M.FlagNames%s(value)\n", d.Name)
	m.Printf("    local names = {}\n")
	m.Printf("    for bit = 0, %d do\n", len(d.Variants)-1)
	m.Printf("      if value & (1 << bit) ~= 0 then names[#names + 1] = bits[bit] end\n")
	m.Printf("    end\n")
	if len(d.Variants) < 64 {
		m.Printf("    local rest = (value >> %d) << %d\n", len(d.Variants), len(d.Variants))
		m.Printf("    if rest ~= 0 then names[#names + 1] = string.format(\"0x%%x\", rest) end\n")
	}
	m.Printf("    if #names == 0 then return \"0\" end\n")
	m.Printf("    return table.concat(names, \"|\")\n")
	m.Printf("  end\nend\n\n")
}

// emitStruct emits a type's constructor: every field at its declared default
// (SPEC §5), nested types and union arms pre-built (the storage principle,
// SPEC §6.1). Field keys are ir.GoExportName, the checker's one mapping.
func emitStruct(m *Module, d *ir.Struct) {
	m.Printf("%s", ir.DocComment(d.Doc, "", "--"))
	m.Printf("-- type %s\n", d.Name)
	m.Printf("function _M.New%s()\n", d.Name)
	if len(d.Fields) == 0 {
		m.Printf("  -- empty body — presence is the payload (SPEC §4.6)\n")
		m.Printf("  return {}\n")
		m.Printf("end\n\n")
		return
	}
	m.Printf("  return {\n")
	prevGuard := ""
	for _, f := range d.Fields {
		if f.Guard != prevGuard {
			if f.Guard != "" {
				m.Printf("    -- %s — wire branch; storage holds both sides (SPEC §5)\n", f.Guard)
			}
			prevGuard = f.Guard
		}
		m.Printf("%s", ir.DocComment(f.Doc, "    ", "--"))
		emitStorageField(m, f)
	}
	m.Printf("  }\n")
	m.Printf("end\n\n")
}

// emitStorageField emits one constructor entry, per docs/PORT-LUA.md THE TYPE
// MAPPING.
func emitStorageField(m *Module, f *ir.Field) {
	name := ir.GoExportName(f.Name)
	k := tableKey(name)
	switch {
	case f.Array != ir.ArrayNone:
		elem := ZeroValue(m, f.Type)
		if isTableValue(elem) {
			// a fresh table per element: never one table aliased N times
			m.Printf("    %s = build(%d, function()\n      return %s\n    end),\n", k, f.ArrayBound, elem)
			m.Helper("build", "local function build(n, make)\n  local t = {}\n  for i = 1, n do\n    t[i] = make()\n  end\n  return t\nend\n")
		} else {
			m.Printf("    %s = fill(%d, %s),\n", k, f.ArrayBound, elem)
			m.Helper("fill", "local function fill(n, v)\n  local t = {}\n  for i = 1, n do\n    t[i] = v\n  end\n  return t\nend\n")
		}
		if f.Array == ir.ArrayCounted {
			// a [A..B] count is born at A (SPEC §4.6); [..N] is born empty
			m.Printf("    %s = %d,\n", tableKey(name+"Count"), f.BornCount())
		}
	case f.HasDefault:
		m.Printf("    %s = %s,%s\n", k, DefaultValue(m, f), fieldComment(f))
	default:
		m.Printf("    %s = %s,%s\n", k, ZeroValue(m, f.Type), fieldComment(f))
	}
}

// isTableValue reports a Lua expression that builds a table (a constructor
// call, a 128-bit pair, a code-unit sequence): one an array must build per
// element rather than share.
func isTableValue(expr string) bool {
	return strings.HasPrefix(expr, "{") || strings.HasSuffix(expr, ")")
}

func emitUnion(m *Module, d *ir.Union) {
	members := make([]string, len(d.Variants))
	docs := make([]string, len(d.Variants))
	for i, v := range d.Variants {
		members[i] = ir.GoExportName(v.Name)
		docs[i] = v.Doc
	}
	m.Printf("-- %sType: union %s's tag — None = 0, then each variant in declared order (SPEC §4.8)\n", d.Name, d.Name)
	emitTagTable(m, d.Name+"Type", members, docs, int64(len(members)))
	m.Printf("%s", ir.DocComment(d.Doc, "", "--"))
	m.Printf("-- %s — at most one of the arms; Type says which. Construction is the empty\n", d.Name)
	m.Printf("-- union (None), with one pre-built arm per payload-carrying variant.\n")
	m.Printf("function _M.New%s()\n", d.Name)
	m.Printf("  return {\n")
	m.Printf("    Type = 0,\n")
	for _, v := range d.Variants {
		if v.Void() {
			continue // a payload-free arm has no storage (SPEC §4.8)
		}
		if v.Type != "" {
			m.Printf("    %s = %s(),\n", tableKey(ir.GoExportName(v.Name)), constructor(m, v.Type))
			continue
		}
		m.Printf("    %s = %s,\n", tableKey(ir.GoExportName(v.Name)), ZeroValue(m, v.F.Type))
	}
	m.Printf("  }\n")
	m.Printf("end\n\n")
}

// constructor names the New<Decl> function of a type or union, required from
// its own module when it lives in another file.
func constructor(m *Module, decl string) string {
	ref := m.Require(decl)
	i := strings.LastIndexByte(ref, '.')
	return ref[:i+1] + "New" + ref[i+1:]
}

// isWide reports a 128-bit storage (docs/PORT-LUA.md, 64-BIT AND 128-BIT
// INTEGERS): int128, uint128 and a fixed(I, F) with I+F = 128.
func isWide(t ir.FieldType) bool {
	return (t.Kind == ir.TInt || t.Kind == ir.TFixed) && t.Width > 64
}

// ZeroValue is the §5 zero form of a storage slot in Lua.
func ZeroValue(m *Module, t ir.FieldType) string {
	switch t.Kind {
	case ir.TBool:
		return "false"
	case ir.TFloat32, ir.TFloat64:
		return "0.0"
	case ir.TString, ir.TBytes:
		return `""`
	case ir.TWString:
		return "{}"
	case ir.TNamed:
		switch t.Ref.(type) {
		case *ir.Struct, *ir.Union:
			return constructor(m, t.Name) + "()"
		}
		return "0" // an enum's None, a flags set's empty mask
	}
	if isWide(t) {
		return "{ lo = 0, hi = 0 }"
	}
	return "0"
}

// DefaultValue is a field's specified default (SPEC §4.2, §5) in Lua.
func DefaultValue(m *Module, f *ir.Field) string {
	switch {
	case f.Type.Kind == ir.TBool:
		return strconv.FormatBool(f.DefBool)
	case f.DefVariant != "":
		return key(m.Require(f.Type.Name), f.DefVariant)
	case f.Type.Kind == ir.TFloat32:
		return FloatLiteral(float64(float32(f.DefFloat)))
	case f.Type.Kind == ir.TFloat64:
		return FloatLiteral(f.DefFloat)
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		return ByteString(f.DefBytes)
	case f.Type.Kind == ir.TWString:
		return "{}"
	case isWide(f.Type):
		return Int128Literal(f.DefInt)
	case f.DefInt != nil:
		return IntLiteral(f.DefInt)
	}
	return ZeroValue(m, f.Type)
}

func fieldComment(f *ir.Field) string {
	var parts []string
	if f.HasIntRange {
		parts = append(parts, fmt.Sprintf("wire [%s, %s]", f.IntMin, f.IntMax))
	}
	if f.HasFloatRange {
		parts = append(parts, fmt.Sprintf("compressed float [%s, %s] @ %s",
			FloatLiteral(f.FMin), FloatLiteral(f.FMax), FloatLiteral(f.Resolution)))
	}
	switch f.Type.Kind {
	case ir.TString, ir.TBytes, ir.TWString:
		parts = append(parts, fmt.Sprintf("at most %d", f.Type.Size))
	}
	if len(parts) == 0 {
		return ""
	}
	return " -- " + strings.Join(parts, "; ")
}

var (
	minInt64  = big.NewInt(math.MinInt64)
	maxInt64  = big.NewInt(math.MaxInt64)
	maxUint64 = new(big.Int).SetUint64(math.MaxUint64)
)

func fitsUint64OrInt64(v *big.Int) bool {
	return v == nil || (v.Cmp(minInt64) >= 0 && v.Cmp(maxUint64) <= 0)
}

// IntLiteral renders a 64-bit value as a Lua integer literal: decimal inside
// int64, math.mininteger for its floor (a decimal 9223372036854775808 would
// lex as a float), and a wrapping hex literal for a uint64 above int64's
// ceiling — the same 64 bits, read unsigned (docs/PORT-LUA.md).
func IntLiteral(v *big.Int) string {
	switch {
	case v == nil:
		return "0"
	case v.Cmp(minInt64) == 0:
		return "math.mininteger"
	case v.Cmp(minInt64) > 0 && v.Cmp(maxInt64) <= 0:
		return v.String()
	case v.Sign() > 0 && v.Cmp(maxUint64) <= 0:
		return fmt.Sprintf("0x%016x", v.Uint64())
	}
	panic(fmt.Sprintf("lua: %s does not fit 64 bits", v))
}

// Int128Literal renders a 128-bit value as { lo = <low 64>, hi = <high 64> },
// two's complement for a negative value, each half a wrapping hex literal.
func Int128Literal(v *big.Int) string {
	if v == nil {
		return "{ lo = 0, hi = 0 }"
	}
	u := new(big.Int).Set(v)
	if u.Sign() < 0 {
		u.Add(u, new(big.Int).Lsh(big.NewInt(1), 128))
	}
	mask := new(big.Int).SetUint64(math.MaxUint64)
	lo := new(big.Int).And(u, mask).Uint64()
	hi := new(big.Int).And(new(big.Int).Rsh(u, 64), mask).Uint64()
	return fmt.Sprintf("{ lo = 0x%016x, hi = 0x%016x }", lo, hi)
}

// FloatLiteral renders a double so the Lua lexer reads back exactly it: the
// shortest round-trip form with a radix point, math.huge for the infinities,
// and 0.0/0.0 for a NaN (a default never carries a payload).
func FloatLiteral(v float64) string {
	switch {
	case math.IsNaN(v):
		return "(0.0 / 0.0)"
	case math.IsInf(v, 1):
		return "math.huge"
	case math.IsInf(v, -1):
		return "(-math.huge)"
	case v == 0 && math.Signbit(v):
		return "-0.0"
	}
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eEn") {
		s += ".0"
	}
	return s
}

// ByteString renders bytes as a Lua string literal: printable ASCII as
// itself, every other byte as \xNN.
func ByteString(b []byte) string {
	var s strings.Builder
	s.WriteByte('"')
	for _, c := range b {
		switch {
		case c == '"' || c == '\\':
			s.WriteByte('\\')
			s.WriteByte(c)
		case c >= 0x20 && c < 0x7f:
			s.WriteByte(c)
		default:
			fmt.Fprintf(&s, "\\x%02x", c)
		}
	}
	s.WriteByte('"')
	return s.String()
}

// luaKeywords is Lua's reserved words, read from the one table (check.ReservedIn,
// SPEC §4.6) and never copied here.
var luaKeywords = schemacheck.ReservedIn("Lua")

// IsName reports a Lua name usable after a dot: an identifier that is not a
// reserved word.
func IsName(s string) bool {
	if s == "" || luaKeywords[s] {
		return false
	}
	for i, r := range s {
		if r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

// tableKey renders a key inside a table constructor.
func tableKey(s string) string {
	if IsName(s) {
		return s
	}
	return "[" + strconv.Quote(s) + "]"
}

// key renders tbl.name, or tbl["name"] where the name is not a Lua name.
func key(tbl, name string) string {
	if IsName(name) {
		return tbl + "." + name
	}
	return tbl + "[" + strconv.Quote(name) + "]"
}
