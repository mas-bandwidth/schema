// Package elixir emits the Elixir target: one .ex file per schema file —
// Constants.schema -> Constants.ex — plus the unit's support module
// PacketRuntime.ex, deterministic to the byte (SPEC §6.1). There is no
// external formatter in the build path: the emitter writes clean
// 2-space-indented Elixir directly, held to `mix format`'s own style (goldens
// pin it, and the test chain runs the formatter as a refuser).
//
// The target is Elixir 1.20 on Erlang/OTP 29. Generated code is
// SELF-CONTAINED: it defines its own modules and touches nothing beyond the
// standard library — no serialize.elixir dependency, no hex packages. The
// shapes are the serialize.elixir port's measured ones: every packing
// intermediate stays under the BEAM's 60-bit small-integer boundary — values
// merge in at most 32-bit groups through a byte-granular scratch and decode
// through 40/56-bit windows; read and write are SINGLE FUNCTIONS PER TYPE
// threading the output binary / scratch / bit position as plain rebound
// accumulators — no stream struct and no per-field tuple — with arrays as
// tail-recursive function-head loops; and validation is always on, because
// the BEAM has no compile-out assert: reads validate everything and never
// raise on hostile bytes, writes carry the O(1) contract checks as
// ArgumentError raises.
//
// Storage is the §6.1 storage principle in the BEAM value domain: one
// defstruct module per type, every field initialized at declaration with
// its construction default (folded to a literal — defstruct defaults
// evaluate at compile time). Member names are the snake_case of
// ir.GoExportName — the same ir.RustSnake mapping the Rust target uses, a
// bijection on it, so the checker's existing collision detection covers
// Elixir without a second registry. Integer fields of every width store plain
// BEAM integers; fixed point stores the raw scaled integer; flags store the
// uint64 mask pattern; strings and bytes store binaries whose byte_size is the
// used length; arrays store lists. An ENUM VALUE IS AN ATOM — :none for 0, the
// variant's snake_case otherwise, and a plain integer only for an unnamed
// `| max = K` headroom value — with E.to_int/1 and E.from_int/1 at the wire.
// A UNION VALUE IS nil (None), {:arm, payload}, or the bare :arm of a
// payload-free arm: a tagged tuple is the BEAM's one-of, and since no BEAM
// term is mutable, the pre-allocated arms other targets keep buy nothing here.
//
// Functions per type live in the per-file module (Types.schema -> module
// <Ns>.Types beside the type modules): write_<name>!(value) -> the wire
// binary, raising ArgumentError on a contract violation, and
// write_<name>(value) -> {:ok, binary} | {:error, message} beside it;
// read_<name>(data, num_bits) -> {:ok, value} | :error, the family read
// verdict (the body throws :invalid to a catch at the surface, so refusal is
// an early exit without a per-op result tuple), and read_<name>_bits, which
// also reports the bits consumed; zero_<name>() -> the §5 zero form
// (construction is `%<Ns>.<Name>{}`, which carries the specified defaults
// instead). There is no generated measure function (SPEC §6.1). Every module
// carries @moduledoc and @type t, and every public function @doc and @spec.
package elixir

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/ast"
	schemacheck "github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixirtable/fixedclosure"
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
	if unitNeedsRuntime(u) {
		out[RuntimeModule+".ex"] = runtimeFile(u)
	}
	return out, nil
}

// elixirReserved is Elixir's reserved words, read from the one table (check.ReservedIn,
// SPEC §4.6) and never copied here.
var elixirReserved = schemacheck.ReservedIn("Elixir")

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
	// The module names under <Ns> (moduleNamesUnderNs). A file whose <Base>
	// is taken by one of them STEPS ASIDE to <Base>File; a declaration or tag
	// module name the stepped-aside file module would take is still refused,
	// by name.
	taken := moduleNamesUnderNs(u)
	for _, un := range u.Unions {
		// the union's tag module <Name>Type colliding with a declaration of
		// that name is a declaration-vs-declaration collision — the file
		// module's step-aside cannot resolve it, so the refusal stands.
		if _, isDecl := u.DeclFile[un.Name+"Type"]; isDecl {
			return fmt.Errorf("union %s needs the tag module %sType, which collides with the declaration of that name; rename one", un.Name, un.Name)
		}
	}
	ns := ir.GoExportName(u.Package)
	// the unit's support module <Ns>.PacketRuntime, in PacketRuntime.ex: a
	// declaration or a schema file of that name would take the module or the
	// output file, so the name is claimed for both
	if unitNeedsRuntime(u) {
		if _, isDecl := u.DeclFile[RuntimeModule]; isDecl {
			return fmt.Errorf("declaration %s collides with the Elixir support module %s.%s; rename it", RuntimeModule, ns, RuntimeModule)
		}
		for _, un := range u.Unions {
			if un.Name+"Type" == RuntimeModule {
				return fmt.Errorf("union %s needs the tag module %s, which collides with the Elixir support module %s.%s; rename it", un.Name, RuntimeModule, ns, RuntimeModule)
			}
		}
		for _, f := range u.Files {
			if ir.GoExportName(f.Base) == RuntimeModule || f.Base == RuntimeModule {
				return fmt.Errorf("schema file %s.schema collides with the Elixir support module %s.%s (%s.ex); rename the file", f.Base, ns, RuntimeModule, RuntimeModule)
			}
		}
	}
	for _, f := range u.Files {
		base := ir.GoExportName(f.Base)
		fileMod := base
		if taken[base] {
			fileMod = base + "File"
		}
		if taken[fileMod] {
			return fmt.Errorf("declaration %s collides with the module the Elixir emitter writes for schema file %s.schema (%s.%s); rename one", fileMod, f.Base, ns, fileMod)
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
	// static bits merged into the scratch since the last spill. Barriers
	// close it; mergeW spills when the next field would pass the budget.
	pendW int64

	// segW is the barrier's pending binary construction: the registers the
	// full-group spills emptied the scratch into, waiting to ride ONE append
	// as several segments. segN names the next one. A barrier drains them,
	// so both are empty across every emission boundary.
	segW []wseg
	segN int

	// the scratch's STATIC state, where the emitter knows it. A write
	// function starts at a known zero and stays known until a value the
	// wire's own data sizes — a loop helper's call, a branch whose arms
	// disagree — makes the position depend on the message. While sbKnown
	// holds, every shift amount and every flush width is a literal and the
	// scratch_bits variable is not maintained at all; scZero additionally
	// records that the scratch is empty, so the first merge of a group is a
	// bind rather than an or.
	sbKnown bool
	sbVal   int64
	scZero  bool

	// whether emitted code has BOUND scratch / scratch_bits yet. A write
	// surface opens with neither: under static offsets the first merge is a
	// bind rather than a read, so the zero the surface used to open with is
	// emitted only where something actually reads it first.
	scBound bool
	sbBound bool

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

	// feed, while non-nil, redirects readR: the fast array-read clause has
	// already matched this iteration's stream bytes into register segments,
	// and fields cut their bits out of chained fixnum windows instead of
	// opening rd/rdw windows (functions.go, readFeed)
	//
	// feed needs no save/restore the way segW needs its reset at a join,
	// and the reason is an exclusion, not luck: plainFastElement admits
	// only scalars and structs of scalars, so no branch, union or nested
	// array can be emitted while a feed is live and no read helper is ever
	// re-entered under one. Widening that gate means giving feed a
	// save/restore at every construct that can re-enter read emission —
	// without it a nested read would cut its bits from the outer clause's
	// registers, which is the silent wire-corruption class.
	feed *feedState

	// compressed-float decode tables (functions.go, cfTable): one module
	// attribute per distinct small declaration, computed at Elixir compile
	// time by the same arithmetic cf_decode runs, deduplicated by parameters
	cfTabs   map[string]int
	cfTabDef []string

	// helperOwner names the type whose items are being inlined — array loop
	// helpers key on (owner, field), so a nested type's loops emit once per
	// file however many callers inline it
	helperOwner string

	// the runtime functions this file's codecs call (runtimeOrder's
	// spelling), which is exactly what its import names
	uses map[string]bool

	// THE READ POSITION'S STATIC STATE. bits_read is the runtime variable;
	// rdPend is how far the emitted reads have moved past it, so a field no
	// longer rebinds bits_read: positions render as `bits_read + <pend>`, or
	// as a literal while rdKnown says bits_read itself is the literal rdBase
	// (a read surface starts at 0 and stays known until a length the wire
	// decides). rdBound says the variable exists at all — under a known
	// position nothing binds it until something reads it at runtime.
	// rdAligned says the runtime bits_read sits on a byte, which an align and
	// the bytes of a string leave behind.
	rdKnown   bool
	rdBase    int64
	rdPend    int64
	rdBound   bool
	rdAligned bool

	// dataBound says the write surface has bound `data`: its first append is
	// a construction (`data = <<...>>`), never an append to an empty binary
	dataBound bool
	lastSeg   segRecord

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

// fileModuleName is the file-scope module's name for file f: <Ns>.<Base>,
// or <Ns>.<Base>File when a declaration (type, enum, union or union tag
// module) of the unit already takes <Ns>.<Base>. The colliding case was
// refused before this card, so no existing generated name changes; only the
// refused case gains the renamed file module.
func fileModuleName(u *ir.Unit, f *ir.File) string {
	base := ir.GoExportName(f.Base)
	if fileModuleStepsAside(u, base) {
		return ir.GoExportName(u.Package) + "." + base + "File"
	}
	return ir.GoExportName(u.Package) + "." + base
}

// fileModuleStepsAside reports whether a declaration, union tag module or
// fixed table value module of the unit takes the module name <Ns>.<base>,
// forcing the file module for a file whose base is `base` to step aside to
// <Ns>.<base>File. It reads the same set checkNames refuses against.
func fileModuleStepsAside(u *ir.Unit, base string) bool {
	return moduleNamesUnderNs(u)[base]
}

// moduleNamesUnderNs is THE ONE SKIP LIST: every module name under <Ns> that is
// not a file module, which checkNames refuses against and fileModuleStepsAside
// steps aside for. A declaration lowers to <Ns>.<Name> except a const or a
// flags (file-module functions, not modules) and a table (absent from the
// packet wire, ir.go Tables); a union adds its tag module <Name>Type; and a
// FIXED table adds the value module <Ns>.<Name> the table backend emits for it
// (docs/SPEC-TABLES.md §3.4, fixedTableModuleNames).
func moduleNamesUnderNs(u *ir.Unit) map[string]bool {
	taken := map[string]bool{}
	for decl := range u.DeclFile {
		if _, isConst := u.Consts[decl]; isConst {
			continue // consts are file-module functions, not modules
		}
		if _, isFlags := u.Flags[decl]; isFlags {
			continue // flags are file-module functions, not modules
		}
		if _, isTable := u.Tables[decl]; isTable {
			continue // a fixed table's value module is added below, by the backend's own closure
		}
		taken[decl] = true
	}
	for _, un := range u.Unions {
		taken[un.Name+"Type"] = true
	}
	for n := range fixedTableModuleNames(u) {
		taken[n] = true
	}
	return taken
}

// fixedTableModuleNames is the set of table names whose FIXED FORM value module
// (<Ns>.<Name>) the table backend emits (elixirtable/fixedelixir.go structModule)
// beside the file module. A fixed table named after a schema file takes the
// module name <Ns>.<Base>, so the packet emitter's file module must step aside
// to <Base>File rather than merge two unrelated modules. The roots and closure
// are the fixed backend's own (fixedclosure.Roots and fixedclosure.Closure,
// which elixirtable's fixedRoots and fixedClosure are), never a copy of them.
func fixedTableModuleNames(u *ir.Unit) map[string]bool {
	names := map[string]bool{}
	for _, st := range fixedclosure.Closure(fixedclosure.Roots(u)) {
		if st.IsTable {
			names[st.Name] = true
		}
	}
	return names
}

// use records the runtime functions (runtimeOrder's spelling) this file's
// codecs call.
func (g *gen) use(names ...string) {
	if g.uses == nil {
		g.uses = map[string]bool{}
	}
	for _, n := range names {
		g.uses[n] = true
	}
}

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
		h.WriteString("# The Elixir wire path: byte-granular 32-bit-group packing and 40/56-bit\n")
		h.WriteString("# read windows (every intermediate a BEAM small integer), single\n")
		h.WriteString("# accumulator-threaded read/write functions per type (no stream struct, no\n")
		h.WriteString("# per-field tuple), function-head loops — with literal widths, zero\n")
		h.WriteString("# dependencies. The shared helpers live in PacketRuntime.ex.\n")
		h.WriteString("#\n")
		h.WriteString("# write_<name>!(value) -> the wire binary. The writer TRUSTS the caller\n")
		h.WriteString("# (the C++ stance) up to the O(1) contract checks, which raise\n")
		h.WriteString("# ArgumentError — the BEAM has no compile-out assert, so they are always\n")
		h.WriteString("# on. write_<name>(value) -> {:ok, binary} | {:error, message} beside it.\n")
		h.WriteString("#\n")
		h.WriteString("# read_<name>(data, num_bits) -> {:ok, value} | :error, the family read\n")
		h.WriteString("# verdict. The wire is a trust boundary: reads validate everything —\n")
		h.WriteString("# bounds fused per static run, ranges, wire constants, reserved and align\n")
		h.WriteString("# padding, union tags, string content — and NEVER raise on hostile bytes.\n")
		h.WriteString("# No slack past the payload is required: the windows price themselves\n")
		h.WriteString("# inside the binary.\n")
		h.WriteString("#\n")
		h.WriteString("# zero_<name>() -> the §5 zero form; construction (%Struct{}) carries the\n")
		h.WriteString("# specified defaults instead. Enum values are atoms, union values are nil,\n")
		h.WriteString("# {:arm, payload} or a payload-free :arm. Floats travel bit-transparently:\n")
		h.WriteString("# a non-finite IEEE-754 pattern — which no BEAM float term can hold — reads\n")
		h.WriteString("# back as {:nonfinite, bits} and writes from the same form.\n")
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
	for _, d := range tableUnionsOrder(g.file) {
		g.emitUnionModules(d)
	}
	g.emitFileModule(order)
}

func tableUnionsOrder(f *ir.File) []*ir.Union {
	if len(f.TableUnions) <= 1 {
		return f.TableUnions
	}
	byName := make(map[string]*ir.Union, len(f.TableUnions))
	for _, u := range f.TableUnions {
		byName[u.Name] = u
	}
	seen := make(map[string]bool, len(f.TableUnions))
	var out []*ir.Union
	var visit func(u *ir.Union)
	visit = func(u *ir.Union) {
		if seen[u.Name] {
			return
		}
		seen[u.Name] = true
		for _, v := range u.Variants {
			if v.F != nil && v.F.Type.Kind == ir.TNamed {
				if dep := byName[v.F.Type.Name]; dep != nil {
					visit(dep)
				}
			}
		}
		out = append(out, u)
	}
	for _, u := range f.TableUnions {
		visit(u)
	}
	return out
}

// emitEnumModule emits an enum's module: its values are ATOMS — :none for 0,
// a variant's snake_case otherwise — because an atom is what a guard and a
// pattern can name, and the integer the wire carries is to_int/1's and
// from_int/1's business. An unnamed `| max = K` headroom value has no atom to
// be, so it stays the integer: every wire-legal value is still representable
// (SPEC §4.2, §6.1).
func (g *gen) emitEnumModule(d *ir.Enum) {
	g.bpf("# %s — None = 0 implicit, variants dense from 1, wire range [0, %d] (SPEC §4.2)\n", d.Name, d.Max)
	g.bpf("defmodule %s do\n", g.mod(d.Name))
	g.emitModuleDoc(d.Doc,
		fmt.Sprintf("The enum %s: each value is an atom, :none being the implicit 0.", d.Name),
		"An unnamed value up to the declared max is the integer itself.")
	g.emitEnumSurface(d.Name, d.Variants, d.Max)
	g.bpf("end\n\n")
}

// emitEnumSurface writes an enum surface's type and its three functions: the
// two wire conversions and the exported extent.
func (g *gen) emitEnumSurface(name string, variants []string, max int64) {
	g.bpf("\n")
	g.emitTypeAlts(enumSpec(variants, max))
	n := int64(len(variants))
	wire := "0.." + intLit64(max)
	if max == 0 {
		wire = "0"
	}
	g.bpf("\n")
	g.emitDoc(fmt.Sprintf("The wire value of a %s value; anything else raises ArgumentError.", name))
	g.emitSpec("to_int(t())", wire)
	g.bpf("  def to_int(:none), do: 0\n")
	for i, v := range variants {
		g.bpf("  def to_int(%s), do: %d\n", enumAtom(v), i+1)
	}
	if max > n {
		g.bpf("  def to_int(v) when is_integer(v) and v > %s and v <= %s, do: v\n", intLit64(n), intLit64(max))
	}
	g.bpf("\n")
	g.bpf("  def to_int(v) do\n")
	g.bpf("    raise ArgumentError, \"not a %s value: #{inspect(v)}\"\n", name)
	g.bpf("  end\n\n")
	g.emitDoc(fmt.Sprintf("The %s value of a wire value: `{:ok, value}`, or `:error` past the max.", name))
	g.emitSpec("from_int(integer())", "{:ok, t()} | :error")
	g.bpf("  def from_int(0), do: {:ok, :none}\n")
	for i, v := range variants {
		g.bpf("  def from_int(%d), do: {:ok, %s}\n", i+1, enumAtom(v))
	}
	if max > n {
		g.bpf("  def from_int(v) when is_integer(v) and v > %s and v <= %s, do: {:ok, v}\n", intLit64(n), intLit64(max))
	}
	g.bpf("  def from_int(_), do: :error\n\n")
	g.emitDoc("The exported extent: the highest wire-legal value (SPEC §4.2).")
	g.emitSpec("max()", "non_neg_integer()")
	g.bpf("  def max, do: %s\n", intLit64(max))
}

// emitUnionModules emits a first-class one-of (SPEC §4.8): the <Name>Type tag
// enum, then the union's own module, which holds its type: nil for None,
// {:arm, payload} for an arm with a payload, the bare atom for a payload-free
// arm. There is no struct: a tagged tuple is the BEAM's one-of, and since no
// BEAM term is mutable, pre-allocated storage per arm buys nothing.
func (g *gen) emitUnionModules(d *ir.Union) {
	g.bpf("# %sType: union %s's tag — None = 0, then each variant in declared order (SPEC §4.8)\n", d.Name, d.Name)
	g.bpf("defmodule %sType do\n", g.mod(d.Name))
	g.emitModuleDoc("",
		fmt.Sprintf("The tag of union %s: :none, then one atom per arm in declared order.", d.Name))
	arms := make([]string, len(d.Variants))
	for i, v := range d.Variants {
		arms[i] = v.Name
	}
	g.emitEnumSurface(d.Name+"Type", arms, d.Max)
	g.bpf("end\n\n")

	g.bpf("# %s — nil (None), {:arm, payload}, or a payload-free arm's bare atom (SPEC §4.8)\n", d.Name)
	g.bpf("defmodule %s do\n", g.mod(d.Name))
	g.emitModuleDoc(d.Doc,
		fmt.Sprintf("The union %s: `nil` is None, an arm with a payload is", d.Name),
		"`{:arm, payload}`, and a payload-free arm is its bare atom.")
	alts := []string{"nil"}
	for _, v := range d.Variants {
		if v.Void() {
			alts = append(alts, enumAtom(v.Name))
			continue
		}
		alts = append(alts, fmt.Sprintf("{%s, %s}", enumAtom(v.Name), g.typeSpec(v.F)))
	}
	g.bpf("\n")
	g.emitTypeAlts(alts)
	g.bpf("end\n\n")
}

func (g *gen) emitStructModule(d *ir.Struct) {
	g.bpf("# type %s\n", d.Name)
	var fields, specs []string
	for _, f := range d.Fields {
		fields = append(fields, fmt.Sprintf("%s: %s", elixirName(f.Name), g.storageDefault(f)))
		specs = append(specs, fmt.Sprintf("%s: %s", elixirName(f.Name), g.typeSpec(f)))
	}
	g.bpf("defmodule %s do\n", g.mod(d.Name))
	g.emitModuleDoc(d.Doc,
		fmt.Sprintf("The type %s. Construction carries its specified defaults; the", d.Name),
		fmt.Sprintf("codecs are in %s.", fileModuleName(g.unit, g.file)))
	g.bpf("\n")
	g.emitStructType(specs)
	g.bpf("\n")
	if len(d.Fields) == 0 {
		g.bpf("  # empty body — presence is the payload (SPEC §4.6)\n")
		g.bpf("  defstruct []\n")
	} else {
		g.emitFieldComments(d)
		g.emitDefstruct(fields)
	}
	emitKeyedAccessors(&g.body, d, g.ns)
	g.bpf("end\n\n")
}

// EmitKeyedAccessors emits the 1-based keyed array accessor pair (<field>_at/2,
// put_<field>_at/3) and the key-element iterator (<field>_entries/1) on a
// struct's module for every enum-keyed array field [E]T (SPEC §4.3, §4.6,
// docs/PORTING.md M5). Key 0 (None), keys past E.Max, negative keys, and
// non-integer keys raise ArgumentError naming the field, the key, and the bound.
func EmitKeyedAccessors(b *strings.Builder, st *ir.Struct) {
	emitKeyedAccessors(b, st, "")
}

// emitKeyedAccessors is EmitKeyedAccessors with the unit namespace: a packet
// type's accessors also take the key as the enum's own atom, which is what
// a value of that enum is in this target. A table's accessors (ns "") keep
// the integer keys the table surface reads.
func emitKeyedAccessors(b *strings.Builder, st *ir.Struct, ns string) {
	for _, f := range st.Fields {
		if f.KeyEnum == "" || f.KeyEnumRef == nil || f.KeyEnumRef.Max <= 0 {
			continue
		}
		fn := elixirName(f.Name)
		if st.IsTable {
			fn = f.Name
		}
		enumName := f.KeyEnum
		maxLit := intLit64(f.KeyEnumRef.Max)
		maxPlain := strconv.FormatInt(f.KeyEnumRef.Max, 10)
		b.WriteString("\n  # [" + enumName + "] — 1-based keyed access (1.." + maxPlain + "); 0 (None) and > " + enumName + ".max raise ArgumentError (SPEC §4.3).\n")
		atoms := []string{":none"}
		for _, v := range f.KeyEnumRef.Variants {
			atoms = append(atoms, enumAtom(v))
		}
		// an atom that names no variant falls through to the refusal below
		atomGuard := "is_atom(key) and key in [" + strings.Join(atoms, ", ") + "]"
		key := "pos_integer()"
		if ns != "" {
			key = ns + "." + enumName + ".t()"
			b.WriteString("  @doc \"The " + fn + " element at an " + enumName + " key, by atom or by its 1-based wire value.\"\n")
			b.WriteString("  @spec " + fn + "_at(t(), " + key + ") :: term()\n")
		}
		b.WriteString("  def " + fn + "_at(s, key) when is_integer(key) and key >= 1 and key <= " + maxLit + " do\n")
		b.WriteString("    Enum.at(s." + fn + ", key - 1)\n")
		b.WriteString("  end\n\n")
		if ns != "" {
			b.WriteString("  def " + fn + "_at(s, key) when " + atomGuard + ",\n    do: " + fn + "_at(s, " + ns + "." + enumName + ".to_int(key))\n\n")
		}
		b.WriteString("  def " + fn + "_at(_s, key), do: refuse_" + fn + "_key(key)\n\n")
		if ns != "" {
			b.WriteString("  @doc \"Replaces the " + fn + " element at an " + enumName + " key.\"\n")
			b.WriteString("  @spec put_" + fn + "_at(t(), " + key + ", term()) :: t()\n")
		}
		b.WriteString("  def put_" + fn + "_at(s, key, v) when is_integer(key) and key >= 1 and key <= " + maxLit + " do\n")
		b.WriteString("    %{s | " + fn + ": List.replace_at(s." + fn + ", key - 1, v)}\n")
		b.WriteString("  end\n\n")
		if ns != "" {
			b.WriteString("  def put_" + fn + "_at(s, key, v) when " + atomGuard + ",\n    do: put_" + fn + "_at(s, " + ns + "." + enumName + ".to_int(key), v)\n\n")
		}
		b.WriteString("  def put_" + fn + "_at(_s, key, _v), do: refuse_" + fn + "_key(key)\n\n")
		if ns != "" {
			b.WriteString("  @doc \"The " + fn + " elements as {key, element} pairs, keyed by wire value.\"\n")
			b.WriteString("  @spec " + fn + "_entries(t()) :: [{pos_integer(), term()}]\n")
		}
		b.WriteString("  def " + fn + "_entries(s) do\n")
		b.WriteString("    Enum.with_index(s." + fn + ", 1) |> Enum.map(fn {v, k} -> {k, v} end)\n")
		b.WriteString("  end\n\n")
		b.WriteString("  defp refuse_" + fn + "_key(0) do\n")
		b.WriteString("    raise ArgumentError, \"" + fn + ": key 0 is None and names no slot\"\n")
		b.WriteString("  end\n\n")
		b.WriteString("  defp refuse_" + fn + "_key(key) when is_integer(key) and key > " + maxLit + " do\n")
		b.WriteString("    raise ArgumentError, \"" + fn + ": key #{key} is past " + enumName + ".max (" + maxPlain + ")\"\n")
		b.WriteString("  end\n\n")
		b.WriteString("  defp refuse_" + fn + "_key(key) do\n")
		b.WriteString("    raise ArgumentError, \"" + fn + ": key #{inspect(key)} is not a variant of " + enumName + "\"\n")
		b.WriteString("  end\n")
	}
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
	case ir.TWString:
		parts = append(parts, fmt.Sprintf("wstring(%s): a little-endian UTF-16 code-unit binary (SPEC §4.12)", ir.RenderExpr(f.Type.SizeExpr)))
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
	if f.Type.Optional || f.Type.Pointer {
		return "nil"
	}
	if f.IsMap() {
		return "%{}"
	}
	if f.KeyEnum != "" {
		return fmt.Sprintf("List.duplicate(%s, %s)", g.scalarDefault(f), intLit64(f.KeyEnumRef.Max))
	}
	switch f.Array {
	case ir.ArrayList:
		return "[]"
	case ir.ArrayCounted:
		// a [A..B] count is born at A, the one wire-legal count a fresh
		// value can carry (SPEC §4.6), so the list is born holding A
		// elements in their construction form. A [..N] list is born empty
		if n := f.BornCount(); n > 0 {
			return fmt.Sprintf("List.duplicate(%s, %s)", g.scalarDefault(f), intLit64(n))
		}
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
	case ir.TString, ir.TBytes, ir.TWString:
		if f.HasDefault {
			var bytes []string
			for _, b := range f.DefBytes {
				bytes = append(bytes, fmt.Sprintf("0x%02X", b))
			}
			return "<<" + strings.Join(bytes, ", ") + ">>"
		}
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
			if f.DefVariant != "" && enumVariantValue(ref, f.DefVariant) != 0 {
				return enumAtom(f.DefVariant)
			}
			return ":none"
		case *ir.Flags:
			if f.HasDefault {
				return intLit(f.DefInt)
			}
			return "0"
		case *ir.Struct:
			if ref.IsTable {
				return "nil"
			}
			return fmt.Sprintf("%%%s{}", g.mod(t.Name))
		case *ir.Union:
			return "nil" // None (SPEC §4.8)
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
	g.bpf("defmodule %s do\n", fileModuleName(g.unit, g.file))
	g.emitModuleDoc("",
		fmt.Sprintf("The file-scope surface of %s.schema: its constants, flags masks and", g.file.Base),
		"name helpers, and the wire functions of every declaration in it.")
	g.bpf("\n")

	var b strings.Builder
	saved := g.body
	g.body = b

	if g.home {
		g.emitDoc("The unit's protocol id — the hash of its wire shape (SPEC §3.1). Two",
			"sides at the same id speak identical bits; there is no other versioning.")
		g.emitSpec("protocol_id()", "non_neg_integer()")
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

	inner := strings.TrimRight(g.body.String(), "\n")
	g.body = saved
	if g.usesImport {
		g.bpf("  import Bitwise\n")
	}
	if len(g.uses) > 0 {
		var only []string
		for _, n := range runtimeOrder {
			if g.uses[n] {
				name, arity, _ := strings.Cut(n, "/")
				only = append(only, name+": "+arity)
			}
		}
		one := fmt.Sprintf("  import %s, only: [%s]", g.mod(RuntimeModule), strings.Join(only, ", "))
		if len(one) <= formatWidth {
			g.bpf("%s\n", one)
		} else {
			g.bpf("\n  import %s,\n    only: [\n", g.mod(RuntimeModule))
			for i, o := range only {
				sep := ","
				if i == len(only)-1 {
					sep = ""
				}
				g.bpf("      %s%s\n", o, sep)
			}
			g.bpf("    ]\n")
		}
	}
	if g.usesImport || len(g.uses) > 0 {
		g.bpf("\n")
	}
	for _, def := range g.cfTabDef {
		g.body.WriteString(def)
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
	if d.Doc != "" {
		g.bpf("  @doc %s\n", ir.QuoteDocElixir(d.Doc))
	} else {
		g.emitDoc(fmt.Sprintf("The constant %s.", d.Name))
	}
	if d.IsFloat {
		g.emitSpec(name+"()", "float()")
		if d.Storage == "float32" {
			g.bpf("  def %s, do: %s\n\n", name, formatFloat32(d.Float))
			return
		}
		g.bpf("  def %s, do: %s\n\n", name, formatFloat(d.Float))
		return
	}
	g.emitSpec(name+"()", "integer()")
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
// (the enum's own surface is its module, emitted earlier).
func (g *gen) emitEnumHelpers(d *ir.Enum) {
	g.emitNameFunction(ir.RustSnake(d.Name), d.Name, d.Variants, d.Variants)
}

// emitNameFunction emits enum_name_<snake>/1: the debug/log/tooling name of
// any value of an enum surface, given as its atom or as its wire value;
// anything past the named variants names as "???".
func (g *gen) emitNameFunction(snake, name string, variants, names []string) {
	g.emitDoc(fmt.Sprintf("The debug/log/tooling name of a %s value, by atom or by wire", name),
		"value; anything past the named variants names as \"???\".")
	g.emitSpec(fmt.Sprintf("enum_name_%s(%s.t() | integer())", snake, g.mod(name)), "String.t()")
	g.bpf("  def enum_name_%s(value) do\n", snake)
	g.bpf("    case value do\n")
	g.bpf("      :none -> \"None\"\n")
	g.bpf("      0 -> \"None\"\n")
	for i, v := range variants {
		g.bpf("      %s -> \"%s\"\n", enumAtom(v), names[i])
		g.bpf("      %d -> \"%s\"\n", i+1, names[i])
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
		mask := elixirName(d.Name + v)
		if i < len(d.VariantDocs) && d.VariantDocs[i] != "" {
			g.bpf("  @doc %s\n", ir.QuoteDocElixir(d.VariantDocs[i]))
		} else {
			g.emitDoc(fmt.Sprintf("The %s mask of %s (bit %d).", v, d.Name, i))
		}
		g.emitSpec(mask+"()", "non_neg_integer()")
		g.bpf("  def %s, do: 1 <<< %d\n\n", mask, i)
	}
	snake := ir.RustSnake(d.Name)
	g.emitDoc(fmt.Sprintf("The declared variant count of %s (SPEC §4.2).", d.Name))
	g.emitSpec(snake+"_count()", "non_neg_integer()")
	g.bpf("  def %s_count, do: %d\n\n", snake, len(d.Variants))

	g.emitDoc(fmt.Sprintf("The debug/log/tooling name of bit i of %s; out-of-range bits", d.Name),
		"name as \"???\".")
	g.emitSpec(fmt.Sprintf("flag_name_%s(integer())", snake), "String.t()")
	g.bpf("  def flag_name_%s(bit) do\n", snake)
	g.bpf("    case bit do\n")
	for i, v := range d.Variants {
		g.bpf("      %d -> \"%s\"\n", i, v)
	}
	g.bpf("      _ -> \"???\"\n")
	g.bpf("    end\n")
	g.bpf("  end\n\n")

	g.emitDoc(fmt.Sprintf("Renders the set bits of a %s value as \"A|B\": \"0\" for the empty", d.Name),
		"set, bits past the declared variants as hex.")
	g.emitSpec(fmt.Sprintf("flag_names_%s(non_neg_integer())", snake), "String.t()")
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
	return fileModuleName(g.unit, &ir.File{Base: base}) + "." + elixirName(name) + "()"
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
