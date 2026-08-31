// Package elixir emits the Elixir target: one .ex file per schema file —
// Constants.schema -> Constants.ex — deterministic to the byte (SPEC §6.1).
// There is no external formatter in the build path: the emitter writes clean
// 2-space-indented Elixir directly, held to `mix format`'s own style (goldens
// pin it, and the test chain runs the formatter as a refuser).
//
// The target is Elixir 1.20 on Erlang/OTP 29. Generated code is
// SELF-CONTAINED: it defines its own modules and touches nothing beyond the
// standard library — no serialize.elixir dependency, no hex packages. The
// serialize.elixir port measured the shapes this backend emits (issue #167):
// every packing intermediate stays under the BEAM's 60-bit small-integer
// boundary — values merge in at most 32-bit groups through a byte-granular
// scratch (nothing here ever reaches 2^40) and decode through the port's
// 40-bit windows (+41% write / +91% read measured against the boxing
// shapes); read and write are SINGLE FUNCTIONS PER TYPE threading the
// output binary / scratch / bit position as plain rebound accumulators —
// no stream struct and no per-field tuple (the port measured its per-op
// immutable API at a 7x allocation tax) — with arrays as tail-recursive
// function-head loops, the port's own fast form; and validation is always
// on, because the BEAM has no compile-out assert: reads validate everything
// and never raise on hostile bytes, writes carry the O(1) contract checks
// as ArgumentError raises (the port's misuse convention).
//
// Storage is the §6.1 storage principle in the BEAM value domain: one
// defstruct module per type, every field initialized at declaration with
// its construction default (folded to a literal — defstruct defaults
// evaluate at compile time, where a symbolic cross-module call would
// impose an ordering the runtime never sees). Member names are the
// snake_case of ir.GoExportName — the same ir.RustSnake mapping the Rust
// target uses, a bijection on it, so the checker's existing collision
// detection covers Elixir without a second registry. Integer fields of
// every width store plain BEAM integers (arbitrary precision makes the
// 128-bit pair emulation of other targets unnecessary); fixed point
// stores the raw scaled integer; flags store the uint64 mask pattern;
// strings and bytes store binaries whose byte_size is the used length;
// arrays store lists (construction defaults reach through fixed arrays;
// counted arrays construct empty).
//
// Functions per type live in the per-file module (Types.schema -> module
// <Ns>.Types beside the type modules — the file-scope surface of the JS and
// Dart targets, spelled the only way Elixir can spell it):
// write_<name>(value) -> the wire binary (trusted writer, O(1) contracts
// raise ArgumentError); read_<name>(data, num_bits) -> {:ok, value} |
// :error (the family read verdict as the port's ok/error convention —
// reads validate EVERYTHING and never raise on hostile bytes; the body
// throws :invalid to a catch at the surface, so refusal is an early exit
// without a per-op result tuple); measure_<name>(value) -> exact wire bits
// (static runs folded to literals at generation time); zero_<name>() ->
// the §5 zero form (construction is `%<Ns>.<Name>{}`, which carries the
// specified defaults instead).
package elixir

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Generate returns basename.ex -> file contents for every file of the unit.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if err := checkNames(u); err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	home := ir.ProtocolIdHome(u)
	for _, f := range u.Files {
		g := &gen{unit: u, file: f, home: f.Base == home,
			ns: ir.GoExportName(u.Package), helpers: map[string]string{}}
		g.emitFile()
		out[f.Base+".ex"] = g.assemble()
	}
	return out, nil
}

// elixirReserved is every spelling a generated Elixir identifier must not
// take: the reserved words of Elixir, plus the arity-0 Kernel auto-imports a
// generated 0-arity function would collide with — `def node, do: ...` is a
// compile error against Kernel.node/0.
var elixirReserved = map[string]bool{
	"true": true, "false": true, "nil": true, "when": true, "and": true,
	"or": true, "not": true, "in": true, "fn": true, "do": true, "end": true,
	"catch": true, "rescue": true, "after": true, "else": true,
	"node": true, "self": true, "make_ref": true, "binding": true,
}

// elixirName maps an exported member/constant/variant name into Elixir's
// snake_case: ir.RustSnake over ir.GoExportName. The spellings are a
// bijection on GoExportName, so the checker's PascalCase collision registry
// covers Elixir members without a second registry (the Rust target's own
// mapping, reused).
func elixirName(name string) string {
	return ir.RustSnake(ir.GoExportName(name))
}

// checkNames refuses any declaration whose Elixir spelling lands on a
// reserved word or an arity-0 Kernel auto-import, and any type/enum/union
// name colliding with a schema file base — both become modules under the
// package namespace, so the collision would merge two unrelated modules.
func checkNames(u *ir.Unit) error {
	check := func(kind, owner, name, mapped string) error {
		if elixirReserved[mapped] {
			if owner != "" {
				return fmt.Errorf("%s %s of %s maps to the reserved Elixir identifier %q; rename it", kind, name, owner, mapped)
			}
			return fmt.Errorf("%s %s maps to the reserved Elixir identifier %q; rename it", kind, name, mapped)
		}
		return nil
	}
	bases := map[string]string{}
	for _, f := range u.Files {
		bases[ir.GoExportName(f.Base)] = f.Base
	}
	for decl := range u.DeclFile {
		if _, isConst := u.Consts[decl]; isConst {
			continue // consts are file-module functions, not modules
		}
		if _, isFlags := u.Flags[decl]; isFlags {
			continue // flags are file-module functions, not modules
		}
		if base, taken := bases[decl]; taken {
			return fmt.Errorf("declaration %s collides with the module the Elixir emitter writes for schema file %s.schema; rename one", decl, base)
		}
	}
	for _, un := range u.Unions {
		// the union's tag module <Name>Type must be free too
		if base, taken := bases[un.Name+"Type"]; taken {
			return fmt.Errorf("union %s needs the tag module %sType, which collides with schema file %s.schema; rename one", un.Name, un.Name, base)
		}
		if _, taken := u.DeclFile[un.Name+"Type"]; taken {
			return fmt.Errorf("union %s needs the tag module %sType, which collides with the declaration of that name; rename one", un.Name, un.Name)
		}
	}
	for _, c := range u.Consts {
		if err := check("const", "", c.Name, elixirName(c.Name)); err != nil {
			return err
		}
	}
	for _, e := range u.Enums {
		for _, v := range e.Variants {
			if err := check("variant", e.Name, v, elixirName(v)); err != nil {
				return err
			}
		}
	}
	for _, fl := range u.Flags {
		for _, v := range fl.Variants {
			if err := check("variant", fl.Name, v, elixirName(fl.Name+v)); err != nil {
				return err
			}
		}
	}
	for _, st := range u.Structs {
		for _, f := range st.Fields {
			if err := check("field", st.Name, f.Name, elixirName(f.Name)); err != nil {
				return err
			}
		}
	}
	for _, un := range u.Unions {
		for _, v := range un.Variants {
			if err := check("variant", un.Name, v.Name, elixirName(v.Name)); err != nil {
				return err
			}
		}
	}
	return nil
}

type gen struct {
	unit *ir.Unit
	file *ir.File
	home bool   // this file carries protocol_id and the unit-level target notes
	ns   string // the package namespace: GoExportName of the unit package

	body strings.Builder

	// per-function emission state (functions.go)
	fn strings.Builder

	// pendW is the merge group open at this point of write emission: the
	// static bits merged into the scratch since the last flush. Barriers
	// close it; mergeW closes it when the next field would pass the budget.
	pendW int64

	// bindW maps a write-side dotted field access to the local the scope's
	// one map read bound; bindUsed is the names already taken in the
	// function being emitted, so a nested scope can never shadow an outer
	// local that is still live
	bindW    map[string]string
	bindDisp map[string]string // local -> the dotted access, for raise text
	bindUsed map[string]bool

	// the read group open at this point of read emission: rv carries
	// rdOff + rdAvail bits, of which rdOff are already cut out; rdRun is
	// the fused static run's remaining bits, or 0 when unknown
	rdOff, rdAvail, rdRun int64

	// helperOwner names the type whose items are being inlined — array loop
	// helpers key on (owner, field), so a nested type's loops emit once per
	// file however many callers inline it
	helperOwner string

	// per-file helper needs
	needRd     bool // rd/3 — the 40-bit window decode
	needRdw    bool // rdw/3 — the 56-bit window decode, for groups past 33 bits
	needF32    bool // f32_bits/1 + f32_value/1
	needF64    bool // f64_bits/1 + f64_value/1
	needCf     bool // cf_quantize/4 + cf_decode/4 (and their fr/1)
	usesImport bool // the module body uses Bitwise operators

	// loop helpers the current file's wire bodies need, in first-need order,
	// deduplicated by name; bodies are emitted after the public functions
	helperOrder []string
	helpers     map[string]string
}

func (g *gen) bpf(format string, args ...any) {
	fmt.Fprintf(&g.body, format, args...)
}

// mod is the namespaced module of a declaration or file base.
func (g *gen) mod(name string) string { return g.ns + "." + name }

func (g *gen) assemble() []byte {
	var h strings.Builder
	// the basename, not the invocation-relative path: output is deterministic
	// to the byte wherever the compiler runs (SPEC §6.1)
	fmt.Fprintf(&h, "# Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", g.file.Base)
	h.WriteString("# SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	h.WriteString("# your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	h.WriteString("# AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&h, "# package %s — protocol id 0x%016x\n", g.unit.Package, g.unit.ProtocolId)
	if g.home {
		// the unit-level target notes ride the home file only — said once
		// per unit, not once per file
		h.WriteString("#\n")
		h.WriteString("# The shipped Elixir wire path (issue #167): the serialize.elixir port's\n")
		h.WriteString("# measured shapes — byte-granular 32-bit-group packing and 40-bit read\n")
		h.WriteString("# windows (every intermediate a BEAM small integer), single accumulator-\n")
		h.WriteString("# threaded read/write functions per type (no stream struct, no per-field\n")
		h.WriteString("# tuple), function-head loops — with literal widths, zero dependencies.\n")
		h.WriteString("#\n")
		h.WriteString("# write_<name>(value) -> the wire binary. The writer TRUSTS the caller\n")
		h.WriteString("# (the C++ stance) up to the O(1) contract checks, which raise\n")
		h.WriteString("# ArgumentError — the BEAM has no compile-out assert, so they are always\n")
		h.WriteString("# on, matching the port.\n")
		h.WriteString("#\n")
		h.WriteString("# read_<name>(data, num_bits) -> {:ok, value} | :error, the family read\n")
		h.WriteString("# verdict. The wire is a trust boundary: reads validate everything —\n")
		h.WriteString("# bounds fused per static run, ranges, wire constants, reserved and align\n")
		h.WriteString("# padding, union tags, string content — and NEVER raise on hostile bytes.\n")
		h.WriteString("# No slack past the payload is required: the windows price themselves\n")
		h.WriteString("# inside the binary.\n")
		h.WriteString("#\n")
		h.WriteString("# measure_<name>(value) -> exact wire bits for that value (trusted, like\n")
		h.WriteString("# the writer). zero_<name>() -> the §5 zero form; construction\n")
		h.WriteString("# (%Struct{}) carries the specified defaults instead. Floats travel\n")
		h.WriteString("# bit-transparently: a non-finite IEEE-754 pattern — which no BEAM float\n")
		h.WriteString("# term can hold — reads back as {:nonfinite, bits} and writes from the\n")
		h.WriteString("# same form, the serialize.elixir convention.\n")
	}
	h.WriteString("\n")
	h.WriteString(strings.TrimRight(normalizeBlanks(g.body.String()), "\n"))
	h.WriteString("\n")
	return []byte(h.String())
}

// normalizeBlanks applies mix format's blank-line discipline to the emitted
// body: multi-line statements are emitted blank-padded (over-padding is
// harmless here), then runs collapse to one blank and blanks vanish where
// the formatter never keeps them — right after a block opener and right
// before a closer.
func normalizeBlanks(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
			continue
		}
		if len(out) == 0 {
			continue
		}
		prev := strings.TrimSpace(out[len(out)-1])
		if prev == "" || prev == "else" || strings.HasSuffix(prev, " do") || strings.HasSuffix(prev, "->") || strings.HasSuffix(prev, "(") {
			continue
		}
		out = append(out, "")
	}
	// blanks before closers drop in a second pass (the closer is not known
	// when the blank arrives)
	var final []string
	for i, l := range out {
		if strings.TrimSpace(l) == "" && i+1 < len(out) {
			next := strings.TrimSpace(out[i+1])
			if next == "end" || next == "else" || next == "}" || strings.HasPrefix(next, "catch") || strings.HasPrefix(next, "end") {
				continue
			}
		}
		final = append(final, l)
	}
	return strings.Join(final, "\n")
}

func (g *gen) emitFile() {
	// type/enum/union modules first, in EmissionOrder (a defstruct default
	// holding %Other{} needs Other compiled — same-file forward references
	// are an ordering error in Elixir's sequential compile of one file);
	// the file module of functions and constants comes last, so its struct
	// literals see every module above it.
	order := ir.EmissionOrder(g.file)
	for _, d := range order {
		switch d := d.(type) {
		case *ir.Enum:
			g.emitEnumModule(d)
		case *ir.Struct:
			g.emitStructModule(d)
		case *ir.Union:
			g.emitUnionModules(d)
		}
	}
	g.emitFileModule(order)
}

// emitEnumModule emits an integer-constant namespace — the Elixir translation
// of the family's integer-backed enums: a module of 0-arity functions,
// because storage must hold every wire-legal value and | max = ... headroom
// values have no richer Elixir value to be.
func (g *gen) emitEnumModule(d *ir.Enum) {
	g.bpf("# %s — None = 0 implicit, variants dense from 1, wire range [0, %d] (SPEC §4.2);\n", d.Name, d.Max)
	g.bpf("# an integer-constant namespace — the Elixir translation of the family's\n")
	g.bpf("# integer-backed enums: storage must hold every wire-legal value, and\n")
	g.bpf("# | max = ... headroom values have no richer Elixir value to be\n")
	g.bpf("defmodule %s do\n", g.mod(d.Name))
	g.bpf("  def none, do: 0\n")
	for i, v := range d.Variants {
		g.bpf("  def %s, do: %d\n", elixirName(v), i+1)
	}
	g.bpf("  # the exported extent (SPEC §4.2)\n")
	g.bpf("  def max, do: %s\n", intLit64(d.Max))
	g.bpf("end\n\n")
}

// emitUnionModules emits a first-class one-of (SPEC §4.8): the <Name>Type tag
// namespace, then the struct — the tag beside one arm per variant, each
// holding its payload type's construction form.
func (g *gen) emitUnionModules(d *ir.Union) {
	g.bpf("# %sType: union %s's tag — None = 0, then each variant in declared order (SPEC §4.8)\n", d.Name, d.Name)
	g.bpf("defmodule %sType do\n", g.mod(d.Name))
	g.bpf("  def none, do: 0\n")
	for i, v := range d.Variants {
		g.bpf("  def %s, do: %d\n", elixirName(v.Name), i+1)
	}
	g.bpf("  # the exported extent (SPEC §4.2)\n")
	g.bpf("  def max, do: %s\n", intLit64(d.Max))
	g.bpf("end\n\n")

	g.bpf("# %s — at most one of the arms; type says which. Construction is the empty\n", d.Name)
	g.bpf("# union (None). A read builds the selected arm from the wire; unselected\n")
	g.bpf("# arms hold their construction form (SPEC §4.8 leaves them unspecified).\n")
	g.bpf("# Consumers read the selected arm only.\n")
	fields := []string{"type: 0"}
	for _, v := range d.Variants {
		fields = append(fields, fmt.Sprintf("%s: %%%s{}", elixirName(v.Name), g.mod(v.Type)))
	}
	g.bpf("defmodule %s do\n", g.mod(d.Name))
	g.emitDefstruct(fields)
	g.bpf("end\n\n")
}

func (g *gen) emitStructModule(d *ir.Struct) {
	if len(d.Tags) > 0 {
		g.bpf("# type %s [%s] — tags are user-chosen and inert in v1 (SPEC §4.2, Type tags)\n", d.Name, strings.Join(d.Tags, ", "))
	} else {
		g.bpf("# type %s\n", d.Name)
	}
	var fields []string
	for _, f := range d.Fields {
		fields = append(fields, fmt.Sprintf("%s: %s", elixirName(f.Name), g.storageDefault(f)))
	}
	g.bpf("defmodule %s do\n", g.mod(d.Name))
	if len(d.Fields) == 0 {
		g.bpf("  # empty body — presence is the payload (SPEC §4.6)\n")
		g.bpf("  defstruct []\n")
	} else {
		g.emitFieldComments(d)
		g.emitDefstruct(fields)
	}
	g.bpf("end\n\n")
}

// emitFieldComments writes one comment block above defstruct carrying the
// per-field annotations (defaults, ranges, branch membership) — keyword-list
// entries cannot carry their own comment lines without fighting the
// formatter's join-or-break decision.
func (g *gen) emitFieldComments(d *ir.Struct) {
	var lines []string
	prevGuard := ""
	for _, f := range d.Fields {
		if f.Guard != prevGuard {
			if f.Guard != "" {
				lines = append(lines, fmt.Sprintf("%s — wire branch; storage holds both sides, a read zeroes the untaken side (SPEC §5):", f.Guard))
			}
			prevGuard = f.Guard
		}
		if c := g.fieldComment(f); c != "" {
			lines = append(lines, fmt.Sprintf("%s: %s", elixirName(f.Name), c))
		}
	}
	for _, l := range lines {
		g.bpf("  # %s\n", l)
	}
}

func (g *gen) fieldComment(f *ir.Field) string {
	var parts []string
	switch f.Type.Kind {
	case ir.TString:
		parts = append(parts, fmt.Sprintf("string(%s) — a UTF-8 binary; byte_size is the used length (SPEC §4.7)", ir.RenderExpr(f.Type.SizeExpr)))
	case ir.TBytes:
		parts = append(parts, fmt.Sprintf("bytes(%s) — a binary; byte_size is the used length (SPEC §4.7)", ir.RenderExpr(f.Type.SizeExpr)))
	case ir.TFixed:
		parts = append(parts, fmt.Sprintf("fixed point Q%d.%d — the raw scaled integer", f.Type.IntBits, f.Type.FracBits))
	case ir.TNamed:
		if _, isFlags := f.Type.Ref.(*ir.Flags); isFlags {
			parts = append(parts, fmt.Sprintf("%s — consumed as masks, uint64 storage (SPEC §4.2)", f.Type.Name))
		}
	}
	if f.Array == ir.ArrayCounted {
		parts = append(parts, fmt.Sprintf("counted array — a list of up to %s elements", ir.RenderExpr(f.ArrayExpr)))
	}
	if f.HasDefault {
		parts = append(parts, "specified default at construction; zero_* gives the §5 zero form")
	}
	if f.HasIntRange {
		parts = append(parts, fmt.Sprintf("wire [%s, %s]", f.IntMin, f.IntMax))
	}
	if f.HasFloatRange {
		parts = append(parts, fmt.Sprintf("compressed float [%s, %s] @ %s",
			formatFloat(f.FMin), formatFloat(f.FMax), formatFloat(f.Resolution)))
	}
	return strings.Join(parts, "; ")
}

// emitDefstruct renders the keyword list in the formatter's own join-or-break
// form: one line when it fits, otherwise one entry per line aligned under the
// first (mix format's call-argument style for defstruct).
func (g *gen) emitDefstruct(fields []string) {
	one := "  defstruct " + strings.Join(fields, ", ")
	if len(one) <= formatWidth {
		g.bpf("%s\n", one)
		return
	}
	g.bpf("  defstruct %s,\n", fields[0])
	for i := 1; i < len(fields); i++ {
		sep := ","
		if i == len(fields)-1 {
			sep = ""
		}
		g.bpf("            %s%s\n", fields[i], sep)
	}
}

// formatWidth is mix format's default line length.
const formatWidth = 98

// storageDefault is a field's construction default: the specified default
// where declared, else the §5 zero form — always folded to a literal, because
// defstruct defaults evaluate at compile time, where a symbolic reference
// would impose a module-compile ordering the runtime surface never has.
func (g *gen) storageDefault(f *ir.Field) string {
	switch f.Array {
	case ir.ArrayCounted:
		return "[]"
	case ir.ArrayFixed:
		return fmt.Sprintf("List.duplicate(%s, %s)", g.scalarDefault(f), intLit64(f.ArrayBound))
	}
	return g.scalarDefault(f)
}

func (g *gen) scalarDefault(f *ir.Field) string {
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		if f.HasDefault {
			return fmt.Sprintf("%v", f.DefBool)
		}
		return "false"
	case ir.TFloat32:
		if f.HasDefault {
			return formatFloat32(f.DefFloat)
		}
		return "0.0"
	case ir.TFloat64:
		if f.HasDefault {
			return formatFloat(f.DefFloat)
		}
		return "0.0"
	case ir.TString, ir.TBytes:
		return "<<>>"
	case ir.TFixed:
		// ir.Field.DefInt for a fixed default is ALREADY the raw scaled
		// integer (issue #168) — storage is that raw value directly
		if f.HasDefault {
			return intLit(f.DefInt)
		}
		return "0"
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			if f.DefVariant != "" {
				return fmt.Sprintf("%d", enumVariantValue(ref, f.DefVariant))
			}
			return "0"
		case *ir.Flags:
			return "0"
		case *ir.Struct, *ir.Union:
			return fmt.Sprintf("%%%s{}", g.mod(t.Name))
		}
	}
	// integer / bits storage as a plain BEAM integer
	if f.HasDefault {
		return intLit(f.DefInt)
	}
	return "0"
}

// enumVariantValue is the wire value of a named variant (1-based dense).
func enumVariantValue(e *ir.Enum, variant string) int {
	for i, v := range e.Variants {
		if v == variant {
			return i + 1
		}
	}
	return 0 // unreachable: the checker resolved the default
}

// ---- the per-file module: constants, flags, helpers, wire functions ----

func (g *gen) emitFileModule(order []ir.Decl) {
	g.bpf("# The file-scope surface: constants, flags masks, name helpers and the\n")
	g.bpf("# wire functions of every declaration in %s.schema.\n", g.file.Base)
	g.bpf("defmodule %s do\n", g.mod(ir.GoExportName(g.file.Base)))

	var b strings.Builder
	saved := g.body
	g.body = b

	if g.home {
		g.bpf("  # The unit's protocol id — the hash of its wire shape (SPEC §3.1). Two\n")
		g.bpf("  # sides at the same id speak identical bits; there is no other versioning.\n")
		g.bpf("  def protocol_id, do: 0x%016X\n\n", g.unit.ProtocolId)
	}

	for _, d := range order {
		switch d := d.(type) {
		case *ir.Const:
			g.emitConst(d)
		case *ir.Enum:
			g.emitEnumHelpers(d)
		case *ir.Flags:
			g.emitFlags(d)
		case *ir.Struct:
			g.emitStructFunctions(d)
		case *ir.Union:
			g.emitUnionFunctions(d)
		}
	}
	g.emitLoopHelpers()
	g.emitSupportHelpers()

	inner := strings.TrimRight(g.body.String(), "\n")
	g.body = saved
	if g.usesImport {
		g.bpf("  import Bitwise\n\n")
	}
	if g.needRd {
		// inlined by the compiler, so the literal widths at every call site
		// reach the mask arithmetic as constants
		g.bpf("  @compile {:inline, rd: 3}\n\n")
	}
	if g.needRdw {
		g.bpf("  @compile {:inline, rdw: 3}\n\n")
	}
	g.body.WriteString(inner)
	g.bpf("\nend\n")
}

// emitConst emits a schema const as a 0-arity function: symbolic where every
// referenced constant is a bare (untyped) integer schema const (rendered as
// function calls, cross-file references fully qualified — resolution is at
// runtime, so declaration order never binds), the folded literal otherwise.
func (g *gen) emitConst(d *ir.Const) {
	name := elixirName(d.Name)
	g.foldComment(d.Expr)
	if d.IsFloat {
		if d.Storage == "float32" {
			g.bpf("  def %s, do: %s\n\n", name, formatFloat32(d.Float))
			return
		}
		g.bpf("  def %s, do: %s\n\n", name, formatFloat(d.Float))
		return
	}
	g.bpf("  def %s, do: %s\n\n", name, g.renderInt(d.Expr, d.Int))
}

// foldComment writes a comment line carrying the schema expression when the
// rendered Elixir had to fold it (an E.Max reference has no Elixir twin).
func (g *gen) foldComment(e ast.Expr) {
	if e != nil && ir.ExprHasEnumMax(e) {
		g.bpf("  # = %s\n", ir.RenderExpr(e))
	}
}

// emitEnumHelpers emits the enum's debug-name function into the file module
// (the constant namespace is the enum's own module, emitted earlier).
func (g *gen) emitEnumHelpers(d *ir.Enum) {
	g.bpf("  # enum_name_%s: debug/log/tooling name for any %s wire value —\n", ir.RustSnake(d.Name), d.Name)
	g.bpf("  # out-of-set values (wire-legal up to the declared max) name as \"???\"\n")
	g.bpf("  def enum_name_%s(value) do\n", ir.RustSnake(d.Name))
	g.bpf("    case value do\n")
	g.bpf("      0 -> \"None\"\n")
	for i, v := range d.Variants {
		g.bpf("      %d -> \"%s\"\n", i+1, v)
	}
	g.bpf("      _ -> \"???\"\n")
	g.bpf("    end\n")
	g.bpf("  end\n\n")
}

func (g *gen) emitFlags(d *ir.Flags) {
	g.bpf("  # %s — one bit per variant, consumed as masks; flags-typed fields store\n", d.Name)
	g.bpf("  # the uint64 mask pattern in every target — a plain integer here — wire\n")
	g.bpf("  # %d bits (SPEC §4.2). Mask names are the family's flat spelling.\n", d.WireBits)
	g.usesImport = true
	for i, v := range d.Variants {
		g.bpf("  def %s, do: 1 <<< %d\n", elixirName(d.Name+v), i)
	}
	g.bpf("  # the declared variant count (SPEC §4.2)\n")
	g.bpf("  def %s_count, do: %d\n\n", ir.RustSnake(d.Name), len(d.Variants))

	g.usesImport = true
	snake := ir.RustSnake(d.Name)
	g.bpf("  # flag_name_%s: debug/log/tooling name for bit i of %s —\n", snake, d.Name)
	g.bpf("  # out-of-range bits name as \"???\"\n")
	g.bpf("  def flag_name_%s(bit) do\n", snake)
	g.bpf("    case bit do\n")
	for i, v := range d.Variants {
		g.bpf("      %d -> \"%s\"\n", i, v)
	}
	g.bpf("      _ -> \"???\"\n")
	g.bpf("    end\n")
	g.bpf("  end\n\n")

	g.bpf("  # flag_names_%s renders the set bits of value as \"A|B\" — \"0\" for the\n", snake)
	g.bpf("  # empty set, bits past the declared variants as hex\n")
	g.bpf("  def flag_names_%s(value) do\n", snake)
	g.bpf("    names =\n")
	g.bpf("      Enum.filter(0..%d, fn bit -> (value >>> bit &&& 1) != 0 end)\n", len(d.Variants)-1)
	g.bpf("      |> Enum.map(&flag_name_%s/1)\n", snake)
	if len(d.Variants) < 64 { // a 64-variant set has no room for unknown bits
		g.bpf("\n")
		g.bpf("    names =\n")
		g.bpf("      if value >>> %d != 0 do\n", len(d.Variants))
		g.bpf("        hex = String.downcase(Integer.to_string((value >>> %d) <<< %d, 16))\n", len(d.Variants), len(d.Variants))
		g.bpf("        names ++ [\"0x\" <> hex]\n")
		g.bpf("      else\n")
		g.bpf("        names\n")
		g.bpf("      end\n")
	}
	g.bpf("\n")
	g.bpf("    if names == [], do: \"0\", else: Enum.join(names, \"|\")\n")
	g.bpf("  end\n\n")
}

// ---- integer literal / expression rendering ----

// intLit renders an integer literal: decimal at any width — BEAM integers
// are arbitrary precision, so every schema value has a direct spelling —
// with mix format's own digit grouping (underscores every three digits on
// literals of six digits or more).
func intLit(v *big.Int) string { return groupDigits(v.String()) }

// intLit64 is intLit over an int64.
func intLit64(v int64) string { return groupDigits(strconv.FormatInt(v, 10)) }

// intLitU64 is intLit over a uint64.
func intLitU64(v uint64) string { return groupDigits(strconv.FormatUint(v, 10)) }

func groupDigits(s string) string {
	neg := strings.HasPrefix(s, "-")
	digits := strings.TrimPrefix(s, "-")
	if len(digits) < 6 {
		return s
	}
	var parts []string
	i := len(digits) % 3
	if i > 0 {
		parts = append(parts, digits[:i])
	}
	for ; i < len(digits); i += 3 {
		parts = append(parts, digits[i:i+3])
	}
	out := strings.Join(parts, "_")
	if neg {
		out = "-" + out
	}
	return out
}

// renderInt renders an integer expression for an integer Elixir context:
// symbolically where every referenced constant is a bare (untyped) schema
// const (the sibling targets' renderability rule, so all of them fold
// identically); the computed literal otherwise. Folding is always correct,
// and every fold is exact — BEAM integers are arbitrary precision.
func (g *gen) renderInt(e ast.Expr, folded *big.Int) string {
	if e == nil || ir.ExprHasEnumMax(e) || !g.renderable(e) {
		return intLit(folded)
	}
	return g.renderExpr(e)
}

// renderable: every referenced constant must be a bare (untyped) integer
// schema const — the same renderability rule as the sibling targets.
func (g *gen) renderable(e ast.Expr) bool {
	for _, name := range ir.ExprConsts(e) {
		c, ok := g.unit.Consts[name]
		if !ok || c.IsFloat || c.Explicit {
			return false
		}
	}
	return true
}

// renderExpr renders an expression in Elixir form: constants keep the schema
// spelling as 0-arity function calls (fully qualified when they live in
// another file), schema's truncating / and % become div/2 and rem/2 (both
// truncate toward zero, exactly the schema fold), and a doubled unary minus
// parenthesizes — `--` is Elixir's list-subtraction operator.
func (g *gen) renderExpr(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.BinaryExpr:
		switch e.Op {
		case "/":
			return fmt.Sprintf("div(%s, %s)", g.renderExpr(e.X), g.renderExpr(e.Y))
		case "%":
			return fmt.Sprintf("rem(%s, %s)", g.renderExpr(e.X), g.renderExpr(e.Y))
		}
		return g.renderExpr(e.X) + " " + e.Op + " " + g.renderExpr(e.Y)
	case *ast.UnaryExpr:
		if _, nested := e.X.(*ast.UnaryExpr); nested {
			return "-(" + g.renderExpr(e.X) + ")"
		}
		return "-" + g.renderExpr(e.X)
	case *ast.ParenExpr:
		return "(" + g.renderExpr(e.X) + ")"
	case *ast.IntLit:
		return intLit(e.Value)
	case *ast.IdentExpr:
		return g.constRef(e.Name)
	}
	return ir.RenderExpr(e) // unreachable for renderable expressions
}

// constRef is the call form of a schema const: bare in its own file's module,
// fully qualified from any other file.
func (g *gen) constRef(name string) string {
	base := g.unit.DeclFile[name]
	if base == g.file.Base {
		return elixirName(name) + "()"
	}
	return g.mod(ir.GoExportName(base)) + "." + elixirName(name) + "()"
}

func formatFloat(v float64) string {
	return elixirFloat(strconv.FormatFloat(v, 'g', -1, 64))
}

// formatFloat32 renders a float32-precision literal: the shortest form that
// parses to exactly float32(v) — as a BEAM float it holds that float32 value
// exactly (f32 ⊂ f64), so the wire widths agree with ir.CompressedFloatBits
// in every target.
func formatFloat32(v float64) string {
	return elixirFloat(strconv.FormatFloat(v, 'g', -1, 32))
}

// elixirFloat rewrites a Go shortest-float form into Elixir's literal
// grammar, which requires a digit on both sides of the point and a point
// before any exponent: 5e-324 -> 5.0e-324, 1e+21 -> 1.0e21.
func elixirFloat(s string) string {
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	mant, exp, hasExp := strings.Cut(s, "e")
	if !strings.Contains(mant, ".") {
		mant += ".0"
	}
	out := mant
	if hasExp {
		exp = strings.TrimPrefix(exp, "+")
		out = mant + "e" + exp
	}
	if neg {
		out = "-" + out
	}
	return out
}
