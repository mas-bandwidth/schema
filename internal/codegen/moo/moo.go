// Package moo emits the moo target's packet surface: one .moo module per
// schema file (Constants.schema -> Constants.moo), deterministic to the byte
// (SPEC §6.1), with serialize.moo v2.1.2, the port's runtime, emitted beside
// the modules from runtime/. docs/PORT-MOO.md is the port's design: THE VALUE
// MAPPING is what each field kind stores as, THE WIRE is how each one rides.
//
// moo is the deterministic fork of Luau whose one number is Q48.16 fixed
// point: it holds every integer below 2^47 exactly, and has no float, double
// or 64-bit integer. So the value domain is: a number for every value of 32
// bits or fewer and for a fixed(I, 16) field whose bounds moo can hold (its
// own representation, the game-state type); serialize.moo's frozen wide
// classes (Int64, Uint64, Int128, Uint128) for 64- and 128-bit storage; and a
// float32 or float64 field as its IEEE BIT PATTERN (a number for binary32, a
// Uint64 for binary64), because SPEC §4.3 requires every finite value, -0 and
// the subnormals bit exact, and no moo number can hold them. Those are
// serialize.moo's Bits forms, moo's primary float API.
package moo

import (
	"embed"
	"fmt"
	"maps"
	"math"
	"math/big"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const ext = "moo"

//go:embed runtime/*.moo
var runtimeFS embed.FS

// Runtime returns every runtime/*.moo file keyed by its basename: the
// vendored serialize.moo src/ directory, emitted beside the generated modules
// so a module requires it as require("./serialize") and nothing else
// resolves (docs/PORT-MOO.md, LOADING).
func Runtime() map[string][]byte {
	out := map[string][]byte{}
	entries, err := runtimeFS.ReadDir("runtime")
	if err != nil {
		panic(err) // the embed is compiled in; a read failure is a build defect
	}
	for _, e := range entries {
		data, err := runtimeFS.ReadFile(path.Join("runtime", e.Name()))
		if err != nil {
			panic(err)
		}
		out[e.Name()] = data
	}
	return out
}

// Generate returns basename.moo -> contents for every schema file of the
// unit, plus the runtime modules.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if err := checkBases(u); err != nil {
		return nil, err
	}
	if err := checkAcyclic(u); err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	home := ir.ProtocolIdHome(u)
	for _, f := range u.Files {
		g := &gen{unit: u, file: f, requires: map[string]bool{}, konst: map[string]string{}}
		if err := g.emitFile(f.Base == home); err != nil {
			return nil, fmt.Errorf("moo: %s.schema: %w", f.Base, err)
		}
		out[f.Base+"."+ext] = g.assemble()
	}
	maps.Copy(out, Runtime())
	return out, nil
}

// checkBases refuses a schema file whose module would collide with a runtime
// module or another module, folding case as macOS and Windows do.
func checkBases(u *ir.Unit) error {
	type claim struct{ name, owner string }
	var claims []claim
	for _, f := range u.Files {
		claims = append(claims, claim{f.Base + "." + ext, f.Base + ".schema"})
	}
	var names []string
	for name := range Runtime() {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, n := range names {
		claims = append(claims, claim{n, "the serialize.moo runtime"})
	}
	for i := range claims {
		for j := i + 1; j < len(claims); j++ {
			if strings.EqualFold(claims[i].name, claims[j].name) {
				return fmt.Errorf("moo: %s (claimed by %s) and %s (claimed by %s) are equal ignoring case; a case-insensitive file system writes one file for the pair — rename the schema file", claims[i].name, claims[i].owner, claims[j].name, claims[j].owner)
			}
		}
	}
	return nil
}

// checkAcyclic refuses a unit whose modules would require each other in a
// cycle: a moo require cycle does not load, so the port names it here.
func checkAcyclic(u *ir.Unit) error {
	deps := map[string]map[string]bool{}
	for _, f := range u.Files {
		set := map[string]bool{}
		add := func(t ir.FieldType) {
			if t.Kind != ir.TNamed {
				return
			}
			if b, ok := u.DeclFile[t.Name]; ok && b != f.Base {
				set[b] = true
			}
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ir.Struct:
				for _, fl := range d.Fields {
					add(fl.Type)
				}
			case *ir.Union:
				for _, v := range d.Variants {
					if v.F != nil {
						add(v.F.Type)
					}
				}
			}
		}
		deps[f.Base] = set
	}
	state := map[string]int{}
	var visit func(string, []string) error
	visit = func(b string, stack []string) error {
		switch state[b] {
		case 1:
			return fmt.Errorf("moo: the modules of this unit require each other in a cycle (%s -> %s); a moo require cycle does not load — move the shared declarations into one file", strings.Join(stack, " -> "), b)
		case 2:
			return nil
		}
		state[b] = 1
		var next []string
		for d := range deps[b] {
			next = append(next, d)
		}
		sort.Strings(next)
		for _, d := range next {
			if err := visit(d, append(stack, b)); err != nil {
				return err
			}
		}
		state[b] = 2
		return nil
	}
	for _, f := range u.Files {
		if err := visit(f.Base, nil); err != nil {
			return err
		}
	}
	return nil
}

// gen is one module being written.
type gen struct {
	unit     *ir.Unit
	file     *ir.File
	body     strings.Builder
	requires map[string]bool
	// konst is the module's constant pool: a wide literal or a Float32 is a
	// frozen object, built once at load and named K<n>, so no write, read or
	// constructor allocates one per call.
	konst     map[string]string
	konstList []string
}

func (g *gen) pf(format string, args ...any) { fmt.Fprintf(&g.body, format, args...) }

// k interns a load-time expression and returns the local naming it.
func (g *gen) k(expr string) string {
	if name, ok := g.konst[expr]; ok {
		return name
	}
	name := fmt.Sprintf("K%d", len(g.konstList)+1)
	g.konst[expr] = name
	g.konstList = append(g.konstList, expr)
	return name
}

// mod names a declaration's module from this one: "M" for its own, the
// require alias "_<Base>" for another file's. No schema name starts with an
// underscore, so an alias never shadows a declaration.
func (g *gen) mod(decl string) string {
	if b, ok := g.unit.DeclFile[decl]; ok && b != g.file.Base {
		g.requires[b] = true
		return "_" + b
	}
	return "M"
}

// typeRef is a named declaration's exported type from this module.
func (g *gen) typeRef(decl string) string {
	m := g.mod(decl)
	if m == "M" {
		return decl
	}
	return m + "." + decl
}

func (g *gen) assemble() []byte {
	body := g.body.String()
	var h strings.Builder
	h.WriteString("--!strict\n")
	fmt.Fprintf(&h, "-- Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", g.file.Base)
	h.WriteString("-- SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("-- your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("-- AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "-- package %s — protocol id 0x%016x — the moo packet surface (docs/PORT-MOO.md)\n\n", g.unit.Package, g.unit.ProtocolId)
	used := func(name string) bool {
		return regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`).MatchString(body)
	}
	if used("serialize") || len(g.konstList) > 0 {
		h.WriteString("local serialize = require(\"./serialize\")\n")
	}
	var mods []string
	for m := range g.requires {
		mods = append(mods, m)
	}
	sort.Strings(mods)
	for _, m := range mods {
		fmt.Fprintf(&h, "local _%s = require(\"./%s\")\n", m, m)
	}
	if len(g.konstList) > 0 {
		h.WriteString("\n-- the constant pool: wide literals and Float32 declarations, built once at load\n")
		for i, e := range g.konstList {
			fmt.Fprintf(&h, "local K%d = %s\n", i+1, e)
		}
	}
	var helpers []string
	for _, hp := range helperOrder {
		if used(hp) {
			helpers = append(helpers, helperSrc[hp])
		}
	}
	if len(helpers) > 0 {
		h.WriteString("\n")
		h.WriteString(strings.Join(helpers, "\n"))
	}
	h.WriteString("\nlocal M = {}\n\n")
	h.WriteString(body)
	h.WriteString("return M\n")
	return []byte(h.String())
}

// The module-local holders and guards. A holder is consumed in the call that
// fills it (moo is single threaded, and generated code never holds one
// across a nested Write or Read), so one of each serves the whole module.
var helperOrder = []string{"NUM", "WIDE", "BOOL", "STR", "isInt", "isWide", "validText", "validWide"}

var helperSrc = map[string]string{
	"NUM":  "local NUM: { value: any } = { value = 0 }\n",
	"WIDE": "local WIDE: { value: any } = { value = 0 }\n",
	"BOOL": "local BOOL: { value: any } = { value = false }\n",
	"STR":  "local STR: { value: any } = { value = \"\" }\n",
	"isInt": `-- isInt: an integer-valued number in [lo, hi] — the write guard of every
-- value of 32 bits or fewer (an out-of-contract write returns false, unlatched)
local function isInt(x: any, lo: number, hi: number): boolean
	return type(x) == "number" and x >= lo and x <= hi and x == math.floor(x)
end
`,
	"isWide": `-- isWide: a value of wide class C (or an integer-valued number) in [lo, hi],
-- compared in C's own signedness; lo == nil checks the class alone
local function isWide(C: any, x: any, lo: any, hi: any): boolean
	if type(x) == "number" then
		if x ~= math.floor(x) or (not C.signed and x < 0) then
			return false
		end
	elseif not C.is(x) then
		return false
	end
	return lo == nil or (C.cmp(x, lo) >= 0 and C.cmp(x, hi) <= 0)
end
`,
	"validText": `-- validText: well-formed UTF-8 with no zero byte (SPEC §4.7), checked on a
-- write so an out-of-contract string returns false rather than trapping
local function validText(s: string): boolean
	local n = #s
	local i = 1
	while i <= n do
		local lead = string.byte(s, i) :: number
		i += 1
		if lead == 0 then
			return false
		end
		if lead >= 0x80 then
			local rem, point
			if lead >= 0xC2 and lead <= 0xDF then
				rem, point = 1, bit32.band(lead, 0x1F)
			elseif lead >= 0xE0 and lead <= 0xEF then
				rem, point = 2, bit32.band(lead, 0x0F)
			elseif lead >= 0xF0 and lead <= 0xF4 then
				rem, point = 3, bit32.band(lead, 0x07)
			else
				return false
			end
			if rem > n - i + 1 then
				return false
			end
			for _ = 1, rem do
				local c = string.byte(s, i) :: number
				i += 1
				if c < 0x80 or c > 0xBF then
					return false
				end
				point = bit32.bor(bit32.lshift(point, 6), bit32.band(c, 0x3F))
			end
			if rem == 2 and (point < 0x800 or (point >= 0xD800 and point <= 0xDFFF)) then
				return false
			end
			if rem == 3 and (point < 0x10000 or point > 0x10FFFF) then
				return false
			end
		end
	end
	return true
end
`,
	"validWide": `-- validWide: n UTF-16 code units, none zero, every surrogate paired (SPEC §4.12)
local function validWide(b: buffer, n: number): boolean
	local i = 0
	while i < n do
		local u = buffer.readu16(b, i * 2)
		if u == 0 then
			return false
		end
		if u >= 0xD800 and u <= 0xDBFF then
			if i + 1 >= n then
				return false
			end
			local lo = buffer.readu16(b, i * 2 + 2)
			if lo < 0xDC00 or lo > 0xDFFF then
				return false
			end
			i += 2
		elseif u >= 0xDC00 and u <= 0xDFFF then
			return false
		else
			i += 1
		end
	end
	return true
end
`,
}

func (g *gen) emitFile(carriesProtocolId bool) error {
	if carriesProtocolId {
		g.pf("-- The unit's protocol id — the hash of its wire shape (SPEC §3.1), a Uint64.\n")
		g.pf("M.ProtocolId = %s\n\n", g.k(wideNew("Uint64", new(big.Int).SetUint64(g.unit.ProtocolId), 64)))
	}
	for _, d := range ir.EmissionOrder(g.file) {
		switch d := d.(type) {
		case *ir.Const:
			if err := g.emitConst(d); err != nil {
				return err
			}
		case *ir.Enum:
			g.emitEnum(d)
		case *ir.Flags:
			g.emitFlags(d)
		case *ir.Struct:
			if d.IsTable {
				continue
			}
			if err := g.emitStruct(d); err != nil {
				return err
			}
		case *ir.Union:
			if g.unit.TableUnions[d.Name] != nil {
				continue
			}
			if err := g.emitUnion(d); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Literals.

var (
	two47m1 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 47), big.NewInt(1))
	neg47m1 = new(big.Int).Neg(two47m1)
)

func fitsNumber(v *big.Int) bool { return v.Cmp(neg47m1) >= 0 && v.Cmp(two47m1) <= 0 }

// wideNew renders v as serialize.<class>.new(limbs...): its two's-complement
// bits at width, in 32-bit limbs, least significant first.
func wideNew(class string, v *big.Int, width int) string {
	mod := new(big.Int).Lsh(big.NewInt(1), uint(width))
	bits := new(big.Int).Mod(v, mod)
	var limbs []string
	mask := big.NewInt(0xFFFFFFFF)
	for i := 0; i < width/32; i++ {
		l := new(big.Int).And(new(big.Int).Rsh(bits, uint(32*i)), mask)
		limbs = append(limbs, fmt.Sprintf("0x%08x", l.Uint64()))
	}
	return fmt.Sprintf("serialize.%s.new(%s)", class, strings.Join(limbs, ", "))
}

// float32Bits is float32(v)'s binary32 pattern, as Go's conversion rounds it.
func float32Bits(v float64) uint32 { return math.Float32bits(float32(v)) }

// fixedNumberLit renders raw / 2^16 exactly (a dyadic with at most 16
// fraction bits has a finite decimal expansion, which moo reads back exactly).
func fixedNumberLit(raw *big.Int) string {
	r := new(big.Rat).SetFrac(raw, big.NewInt(1<<16))
	s := r.FloatString(16)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	if s == "-0" {
		s = "0"
	}
	return s
}

// ---------------------------------------------------------------------------
// Declarations.

func (g *gen) emitConst(d *ir.Const) error {
	g.pf("%s", ir.DocComment(d.Doc, "", "--"))
	var lit, note string
	switch {
	case d.IsFloat:
		// a float constant is a moo number: the nearest Q48.16 value, ties to
		// even (moo's boundary rule), written exactly
		if math.IsNaN(d.Float) || math.IsInf(d.Float, 0) || math.Abs(d.Float) >= 1<<47 {
			return fmt.Errorf("const %s = %v has no moo number (moo holds |x| < 2^47, finite)", d.Name, d.Float)
		}
		raw := roundHalfEven(d.Float * 65536)
		lit = fixedNumberLit(raw)
		if new(big.Float).SetFloat64(d.Float).Cmp(new(big.Float).Quo(new(big.Float).SetInt(raw), big.NewFloat(65536))) != 0 {
			note = fmt.Sprintf(" -- %s %s, the nearest Q48.16 value", d.Storage, strconv.FormatFloat(d.Float, 'g', -1, 64))
		}
	case d.Storage == "int128":
		lit = g.k(wideNew("Int128", d.Int, 128))
	case d.Storage == "uint128":
		lit = g.k(wideNew("Uint128", d.Int, 128))
	case d.Explicit && d.Storage == "uint64", !d.Explicit && d.Int.Sign() >= 0 && !fitsNumber(d.Int) && d.Int.BitLen() > 63:
		lit = g.k(wideNew("Uint64", d.Int, 64))
	case d.Explicit && d.Storage == "int64", !fitsNumber(d.Int):
		lit = g.k(wideNew("Int64", d.Int, 64))
	default:
		lit = d.Int.String()
	}
	if note == "" && d.Expr != nil {
		if r := ir.RenderExpr(d.Expr); r != d.Int.String() && !d.IsFloat {
			note = " -- = " + r
		}
	}
	g.pf("M.%s = %s%s\n\n", d.Name, lit, note)
	return nil
}

func roundHalfEven(x float64) *big.Int {
	r := math.RoundToEven(x)
	b, _ := new(big.Float).SetFloat64(r).Int(nil)
	return b
}

func (g *gen) emitEnum(d *ir.Enum) {
	g.pf("%s", ir.DocComment(d.Doc, "", "--"))
	g.pf("-- %s — None = 0 implicit, variants dense from 1, wire range [0, %d] (SPEC §4.2);\n", d.Name, d.Max)
	g.pf("-- a frozen table of numbers; | max = ... headroom values are plain numbers\n")
	g.pf("export type %s = number\n", d.Name)
	g.pf("M.%s = table.freeze({\n\tNone = 0,\n", d.Name)
	for i, v := range d.Variants {
		g.pf("%s", ir.DocComment(d.VariantDocs[i], "\t", "--"))
		g.pf("\t%s = %d,\n", v, i+1)
	}
	g.pf("})\n")
	g.pf("M.%sMax = %d -- the exported extent (SPEC §4.2)\n\n", d.Name, d.Max)
	g.emitNames("EnumName"+d.Name, d.Name, append([]string{"None"}, d.Variants...), 0)
}

// emitNames is the debug/log name of a value; out-of-set values name "???".
func (g *gen) emitNames(fn, what string, names []string, first int) {
	g.pf("local _%s = table.freeze({", fn)
	for i, n := range names {
		if i > 0 {
			g.pf(",")
		}
		g.pf(" [%d] = %q", i+first, n)
	}
	g.pf(" })\n")
	g.pf("-- %s: the debug/log name of a %s value; out-of-set values name \"???\"\n", fn, what)
	g.pf("function M.%s(value: number): string\n\treturn _%s[value] or \"???\"\nend\n\n", fn, fn)
}

func (g *gen) emitFlags(d *ir.Flags) {
	g.pf("%s", ir.DocComment(d.Doc, "", "--"))
	g.pf("-- %s — one bit per variant, consumed as masks; flags store uint64 in every\n", d.Name)
	g.pf("-- target (SPEC §4.2), a serialize.Uint64 here, wire %d bits. Masks are flat\n", d.WireBits)
	g.pf("-- PascalCase, the Go spelling; combine them with serialize.Uint64.bor.\n")
	g.pf("export type %s = any\n", d.Name)
	for i, v := range d.Variants {
		g.pf("%s", ir.DocComment(d.VariantDocs[i], "", "--"))
		g.pf("M.%s%s = %s\n", d.Name, v, g.k(wideNew("Uint64", new(big.Int).Lsh(big.NewInt(1), uint(i)), 64)))
	}
	g.pf("M.%sCount = %d -- the declared variant count (SPEC §4.2)\n\n", d.Name, len(d.Variants))
	g.emitNames("FlagName"+d.Name, "bit index of "+d.Name, d.Variants, 0)
}

// ---------------------------------------------------------------------------
// The value mapping (docs/PORT-MOO.md, THE VALUE MAPPING).

// fixedIsNumber: a fixed(I, 16) or ufixed(I, 16) of 32 or 64 storage bits
// whose bounds moo holds is a moo number, its own Q48.16 value: the wire is
// serializeFixedNumber's, which is serializeFixed64(raw, 48, 16, min, max),
// and SPEC §4.3's fixed wire does not depend on I.
func fixedIsNumber(f *ir.Field) bool {
	t := f.Type
	return t.Kind == ir.TFixed && t.FracBits == 16 && (t.Width == 32 || t.Width == 64) &&
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

// scalar is a non-array value's annotation and its construction (the
// specified default where declared, else the §5 zero form).
func (g *gen) scalar(f *ir.Field, withDefault bool) (typ, init string) {
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
		return "any", g.k(wideNew(c, v, classWidth(c)))
	}
	switch t.Kind {
	case ir.TBool:
		return "boolean", strconv.FormatBool(def && f.DefBool)
	case ir.TFloat32:
		if def {
			return "number", fmt.Sprintf("0x%08x", float32Bits(f.DefFloat))
		}
		return "number", "0"
	case ir.TFixed:
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
			return g.typeRef(t.Name), strconv.Itoa(v)
		case *ir.Struct, *ir.Union:
			return g.typeRef(t.Name), g.mod(t.Name) + ".New" + t.Name + "()"
		}
	}
	return "any", "nil"
}

func luaString(b []byte) string {
	var s strings.Builder
	s.WriteByte('"')
	for _, c := range b {
		fmt.Fprintf(&s, "\\x%02x", c)
	}
	s.WriteByte('"')
	return s.String()
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

type slot struct{ name, typ, init, comment string }

// slots is a field's storage: the value, then the used-length or count
// companion beside it.
func (g *gen) slots(f *ir.Field, withDefault bool) []slot {
	name := ir.GoExportName(f.Name)
	t := f.Type
	var out []slot
	switch {
	case f.Array != ir.ArrayNone && isByteElem(f):
		out = append(out, slot{name, "buffer", fmt.Sprintf("buffer.create(%d)", f.ArrayBound), "[N]uint8: a buffer, 0-based"})
	case f.Array != ir.ArrayNone && isBuffer(t):
		out = append(out, slot{name, "{buffer}", fmt.Sprintf("table.create(%d) :: {buffer}", f.ArrayBound), ""})
		out = append(out, slot{name + "Length", "{number}", fmt.Sprintf("table.create(%d, 0)", f.ArrayBound), ""})
	case f.Array != ir.ArrayNone:
		e := *f
		e.Array = ir.ArrayNone
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
		typ, init := g.scalar(f, withDefault)
		c := ""
		if withDefault && f.HasDefault {
			c = "specified default (SPEC §5)"
		}
		out = append(out, slot{name, typ, init, c})
	}
	if f.Array == ir.ArrayCounted {
		born := "0"
		if withDefault {
			born = strconv.FormatInt(f.BornCount(), 10)
		}
		out = append(out, slot{name + "Count", "number", born, ""})
	}
	return out
}

// fillFresh is the statements that build an array's fresh elements and write
// a bytes default, after the value's table exists.
func (g *gen) fillFresh(f *ir.Field, path, ind string, withDefault bool) {
	name := path + "." + ir.GoExportName(f.Name)
	t := f.Type
	if f.Array != ir.ArrayNone && isFresh(t) && !isByteElem(f) {
		var ctor string
		if isBuffer(t) {
			ctor = fmt.Sprintf("buffer.create(%d)", bufferSize(t))
		} else {
			ctor = g.mod(t.Name) + ".New" + t.Name + "()"
		}
		g.pf("%sfor i = 1, %d do\n%s\t%s[i] = %s\n%send\n", ind, f.ArrayBound, ind, name, ctor, ind)
	}
	if withDefault && f.HasDefault && t.Kind == ir.TBytes && f.Array == ir.ArrayNone && len(f.DefBytes) > 0 {
		g.pf("%sbuffer.writestring(%s, 0, %s)\n%s%sLength = %d\n", ind, name, luaString(f.DefBytes), ind, name, len(f.DefBytes))
	}
}

func (g *gen) emitStruct(d *ir.Struct) error {
	g.pf("%s", ir.DocComment(d.Doc, "", "--"))
	g.pf("-- type %s\n", d.Name)
	g.pf("export type %s = {\n", d.Name)
	for _, f := range d.Fields {
		for _, s := range g.slots(f, true) {
			g.pf("\t%s: %s,\n", s.name, s.typ)
		}
	}
	g.pf("}\n\n")
	bits := ir.MaxBitsStruct(d)
	g.pf("-- %sMaxBits is the longest wire path; align pads at worst case (SPEC §6.1).\n", d.Name)
	g.pf("-- %sMaxBytes is rounded up to the 8-byte write-buffer granularity.\n", d.Name)
	g.pf("M.%sMaxBits = %d\nM.%sMaxBytes = %d\n\n", d.Name, bits, d.Name, ir.MaxBytes(bits))

	g.pf("-- New%s builds the value at its defaults (SPEC §5).\n", d.Name)
	g.pf("function M.New%s(): %s\n", d.Name, d.Name)
	g.pf("\tlocal value: %s = {\n", d.Name)
	for _, f := range d.Fields {
		g.pf("%s", ir.DocComment(f.Doc, "\t\t", "--"))
		for _, s := range g.slots(f, true) {
			c := ""
			if s.comment != "" {
				c = " -- " + s.comment
			}
			g.pf("\t\t%s = %s,%s\n", s.name, s.init, c)
		}
	}
	g.pf("\t}\n")
	for _, f := range d.Fields {
		g.fillFresh(f, "value", "\t", true)
	}
	g.pf("\treturn value\nend\n\n")

	g.pf("-- Init%s restores fresh construction values in place, preserving storage.\n", d.Name)
	g.pf("function M.Init%s(value: %s)\n", d.Name, d.Name)
	for _, f := range d.Fields {
		g.emitReset(f, "value", "\t", true)
	}
	g.pf("end\n\n")

	g.pf("-- Zero%s resets the §5 zero form: all-zero storage, without declared defaults.\n", d.Name)
	g.pf("function M.Zero%s(value: %s)\n", d.Name, d.Name)
	for _, f := range d.Fields {
		g.emitReset(f, "value", "\t", false)
	}
	g.pf("end\n\n")

	if err := g.emitWriteStruct(d); err != nil {
		return err
	}
	return g.emitReadStruct(d)
}

// emitReset writes a field back to its construction value (withDefault) or
// its zero form, in place: nested values and buffers keep their storage.
func (g *gen) emitReset(f *ir.Field, path, ind string, withDefault bool) {
	name := path + "." + ir.GoExportName(f.Name)
	t := f.Type
	fn := "Zero"
	if withDefault {
		fn = "Init"
	}
	resetNamed := func(target string) {
		g.pf("%s%s.%s%s(%s)\n", ind, g.mod(t.Name), fn, t.Name, target)
	}
	switch {
	case f.Array != ir.ArrayNone && isByteElem(f):
		g.pf("%sbuffer.fill(%s, 0, 0)\n", ind, name)
	case f.Array != ir.ArrayNone && isBuffer(t):
		g.pf("%sfor i = 1, %d do\n%s\tbuffer.fill(%s[i], 0, 0)\n%s\t%sLength[i] = 0\n%send\n", ind, f.ArrayBound, ind, name, ind, name, ind)
	case f.Array != ir.ArrayNone && isFresh(t):
		g.pf("%sfor i = 1, %d do\n", ind, f.ArrayBound)
		resetNamed(name + "[i]")
		g.pf("%send\n", ind)
	case f.Array != ir.ArrayNone:
		e := *f
		e.Array = ir.ArrayNone
		_, init := g.scalar(&e, false)
		g.pf("%sfor i = 1, %d do\n%s\t%s[i] = %s\n%send\n", ind, f.ArrayBound, ind, name, init, ind)
	case isBuffer(t):
		g.pf("%sbuffer.fill(%s, 0, 0)\n%s%sLength = 0\n", ind, name, ind, name)
		if withDefault {
			g.fillFresh(f, path, ind, true)
		}
	case isFresh(t):
		resetNamed(name)
	default:
		_, init := g.scalar(f, withDefault)
		g.pf("%s%s = %s\n", ind, name, init)
	}
	if f.Array == ir.ArrayCounted {
		born := int64(0)
		if withDefault {
			born = f.BornCount()
		}
		g.pf("%s%sCount = %d\n", ind, name, born)
	}
}

func (g *gen) emitUnion(d *ir.Union) error {
	tag := d.Name + "Type"
	members := make([]string, len(d.Variants))
	for i, v := range d.Variants {
		members[i] = ir.GoExportName(v.Name)
	}
	g.pf("-- %s: union %s's tag — None = 0, then each variant in declared order (SPEC §4.8)\n", tag, d.Name)
	g.pf("export type %s = number\n", tag)
	g.pf("M.%s = table.freeze({\n\tNone = 0,\n", tag)
	for i, m := range members {
		g.pf("%s", ir.DocComment(d.Variants[i].Doc, "\t", "--"))
		g.pf("\t%s = %d,\n", m, i+1)
	}
	g.pf("})\n")
	g.pf("M.%sMax = %d -- the exported extent (SPEC §4.2)\n\n", tag, d.Max)
	g.emitNames("EnumName"+tag, tag, append([]string{"None"}, members...), 0)

	g.pf("%s", ir.DocComment(d.Doc, "", "--"))
	g.pf("-- %s — at most one arm; Type says which. Construction is the empty union\n", d.Name)
	g.pf("-- (None); every arm is pre-allocated, and an unselected arm is unspecified.\n")
	g.pf("export type %s = {\n\tType: number,\n", d.Name)
	for _, v := range d.Variants {
		if v.Void() {
			continue
		}
		for _, s := range g.slots(armField(v), true) {
			g.pf("\t%s: %s,\n", s.name, s.typ)
		}
	}
	g.pf("}\n\n")
	bits := ir.MaxBitsUnion(d)
	g.pf("-- %sMaxBits is the tag plus the largest arm; None costs the tag only (SPEC §4.8).\n", d.Name)
	g.pf("M.%sMaxBits = %d\nM.%sMaxBytes = %d\n\n", d.Name, bits, d.Name, ir.MaxBytes(bits))
	g.pf("function M.New%s(): %s\n\tlocal value: %s = {\n\t\tType = 0,\n", d.Name, d.Name, d.Name)
	for _, v := range d.Variants {
		if v.Void() {
			continue
		}
		for _, s := range g.slots(armField(v), true) {
			g.pf("\t\t%s = %s,\n", s.name, s.init)
		}
	}
	g.pf("\t}\n")
	for _, v := range d.Variants {
		if !v.Void() {
			g.fillFresh(armField(v), "value", "\t", true)
		}
	}
	g.pf("\treturn value\nend\n\n")
	g.pf("-- Init%s and Zero%s reset the union to None; dormant arms keep their storage.\n", d.Name, d.Name)
	g.pf("function M.Init%s(value: %s)\n\tvalue.Type = 0\nend\n\n", d.Name, d.Name)
	g.pf("function M.Zero%s(value: %s)\n\tvalue.Type = 0\nend\n\n", d.Name, d.Name)
	if err := g.emitWriteUnion(d); err != nil {
		return err
	}
	return g.emitReadUnion(d)
}

// armField is an arm as a field line stored under the arm's own name.
func armField(v ir.UnionVariant) *ir.Field {
	f := *v.F
	f.Name = v.Name
	return &f
}
