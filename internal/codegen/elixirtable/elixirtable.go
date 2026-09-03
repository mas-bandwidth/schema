// Package elixirtable emits the Elixir TABLE surface (docs/SPEC-TABLES.md),
// mirroring internal/codegen/cpptable — the reference — the way
// internal/codegen/cstable and internal/codegen/rusttable do. One module per
// unit file that declares tables (<Base>Table.ex), one shared runtime module
// per unit (TableRuntime.ex), and the two ACCELERATORS beside them: the block
// form's <Base>Block.ex and the cooked form's <Base>Cook.ex.
//
// The wire is neutral, evolution-tolerant TLV: field identity is the name
// hash, unknown fields skip, absent fields default, changed kinds skip (never
// misdecode), out-of-range values clamp, framing damage stops the decode with
// a partial result — and every event lands in the report. Plain binary
// pattern matching with no serialize dependency, so a table module compiles
// into any project on the pinned BEAM alone.
//
// THIS IS THE READING TIER. Elixir has no struct layout control, so the two
// accelerators are READ-ONLY here: a block image and a cooked file are opened
// and pointed into at the offsets the compiler computed (ir.BlockLayout,
// ir.RecordLayout), never produced. A producer is C++, C# or Rust; this
// backend is the consumer, and the harness gates both halves of that against
// the same fixtures.
//
// FIVE DEVIATIONS FROM THE C++ REFERENCE, all forced by the language and all
// named here rather than discovered in the source:
//
//   - THE REPORT IS A VALUE THE CALLER THREADS, not a member of the reader:
//     the BEAM has no mutable struct, so `load` returns `{value, report}` and
//     every nested codec takes and returns the report beside the value. One
//     report, one caller, the same five counters.
//   - STORAGE IS THE ELIXIR COLUMN, not a byte image. A `string(N)` and a
//     `bytes(N)` are binaries whose `byte_size` IS the used length; an array
//     is a list whose `length` IS the count; an enum is its ordinal integer
//     and a `flags` its mask, exactly as the packet emitter spells them
//     (SPEC §6.1). There is no separate `_length` or `_count` companion to
//     keep in step, because there is nothing to keep it in step with.
//   - AN OPTIONAL `?T` IS `nil` WHEN ABSENT. Presence is a value the language
//     already has, so a `_present` companion would be a second spelling of
//     the same fact. Presence still decides the wire (§3): a present optional
//     rides even at its defaults.
//   - AN ENUM-KEYED ARRAY'S STORAGE IS A TUPLE of E.Max slots, so a slot is
//     reached in constant time — a list would make the keyed accessor O(E) at
//     every call site. A closure `type` that spells `[E]T` keeps the packet
//     emitter's list, because that declaration's storage is the packet
//     emitter's and one declaration cannot have two.
//   - A NON-FINITE FLOAT TRAVELS AS `{:nonfinite, bits}`, the convention the
//     packet emitter already established: no BEAM float term can hold one, so
//     the pattern rides beside the value rather than being lost.
package elixirtable

import (
	"fmt"
	"maps"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// table-wire kinds (docs/SPEC-TABLES.md §3), named locally over the one
// target-independent definition in ir — the vocabulary is wire law.
const (
	tkTable = ir.TableKindTable
	tkUnion = ir.TableKindUnion
)

// RuntimeModule, BlockRuntimeModule and BuildVersionModule are the SHARED
// modules a unit with tables grows. They are file basenames as well as module
// names, so they are claimed the way every other generated spelling is: a
// schema file named for one of them would collide (docs/SPEC-TABLES.md §11).
const (
	RuntimeModule      = "TableRuntime"
	BuildVersionModule = "BuildVersion"
)

// Generate returns filename -> file contents for the unit's table surface.
// Empty when the unit declares no table: a table-free unit's Elixir output is
// byte-identical with this backend in the chain or out of it.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return nil, nil
	}
	out := map[string][]byte{}

	variable := ir.VariableTables(u)
	closure := ir.TableClosure(u)
	blocks := ir.Blocks(u)

	// the WIRE surface is refused by name for a unit that declares a
	// variable-length table, exactly as the C# and Rust backends refuse it
	// (docs/SPEC-TABLES.md §11): the arena, the builder, the region and the
	// node-table codec are a named follow-on (§15). The two ACCELERATORS need
	// no codec — a block and a cook are POINTED AT, not parsed — so both are
	// emitted in full.
	var refused []string
	for name := range u.Tables {
		if variable[name] {
			refused = append(refused, name)
		}
	}
	sort.Strings(refused)

	banner := ""
	if len(refused) > 0 {
		banner = refusalBanner(refused)
	}

	ns := ir.GoExportName(u.Package)

	// A DECLARATION LOWERS TO A MODULE under the unit's namespace, so one named
	// for a generated file's module would merge two unrelated modules. The
	// unit-level runtime names are the CHECKER's claim (internal/tablenames,
	// docs/SPEC-TABLES.md §11); these three are derived from a schema FILE's own
	// basename, which no unit-level registry can hold, so they are refused here
	// — beside the same refusal the packet emitter already makes for <Base>.
	for decl := range u.DeclFile {
		for _, suffix := range []string{"Table", "Block", "Cook"} {
			for _, f := range u.Files {
				if decl == ir.GoExportName(f.Base)+suffix {
					return nil, fmt.Errorf("declaration %s collides with the module the Elixir table backend writes for schema file %s.schema (%s.%s%s); rename one (docs/SPEC-TABLES.md §11)",
						decl, f.Base, ns, ir.GoExportName(f.Base), suffix)
				}
			}
		}
	}

	if len(refused) == 0 {
		out[RuntimeModule+".ex"] = runtimeModule(u, ns)
	}
	if anyCookable(u, closure) {
		out[CookRuntimeModule+".ex"] = append([]byte(banner), cookRuntimeModule(u, ns)...)
	}
	out[BuildVersionModule+".ex"] = append([]byte(banner), buildVersionModule(u, ns)...)

	for _, f := range u.Files {
		g := &gen{unit: u, ns: ns, file: f, closure: closure, banner: banner}
		if len(refused) == 0 {
			if body := g.tableModule(); body != nil {
				out[f.Base+"Table.ex"] = body
			}
		}
		if body := g.cookModule(); body != nil {
			out[f.Base+"Cook.ex"] = body
		}
	}

	// the BLOCK form: nothing declares it, every fixed table has one, and it
	// lives in its own module so a consumer that never opens a block pays only
	// for a module it never calls (docs/SPEC-TABLES.md §19).
	if blocks != nil {
		blockOut, err := generateBlocks(u, ns, blocks, banner)
		if err != nil {
			return nil, err
		}
		maps.Copy(out, blockOut)
	}
	return out, nil
}

func refusalBanner(refused []string) string {
	var b strings.Builder
	b.WriteString("# THE ELIXIR WIRE SURFACE OF THIS UNIT IS REFUSED, BY NAME (docs/SPEC-TABLES.md §11).\n")
	b.WriteString("#\n")
	fmt.Fprintf(&b, "# It declares variable-length tables (%s), and the Elixir table\n", englishList(refused))
	b.WriteString("# backend's VARIABLE CLASS — the arena, the builder, the region and the node-table\n")
	b.WriteString("# codec — is a named follow-on (§15). No <Base>Table.ex is emitted for this unit,\n")
	b.WriteString("# so a consumer reaching for measure, save or load gets an undefined function from\n")
	b.WriteString("# its own compiler, beside this file, which says why.\n")
	b.WriteString("#\n")
	b.WriteString("# What IS emitted is the two ACCELERATORS, because neither needs a codec: a block\n")
	b.WriteString("# (§19) and a cook (§7) are pointed at, not parsed. A build that loads this unit's\n")
	b.WriteString("# cooked assets is served in full; one that wants the tolerant wire is not, and\n")
	b.WriteString("# runs the tool or the C++ backend for it.\n\n")
	return b.String()
}

func englishList(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

type gen struct {
	unit    *ir.Unit
	ns      string
	file    *ir.File
	closure map[string]bool
	banner  string

	// owner is the closure member whose codec is being emitted. It decides how
	// an enum-keyed array is REACHED: a `table` declaration's storage is this
	// backend's own tuple, and a closure `type`'s is the packet emitter's list.
	owner *ir.Struct

	body   strings.Builder
	indent string
}

func (g *gen) pf(format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	if g.indent != "" && s != "" {
		trailing := strings.HasSuffix(s, "\n")
		if trailing {
			s = s[:len(s)-1]
		}
		// a BLANK line takes no indent: trailing whitespace is what `mix
		// format` refuses, and the gate that runs it is the point
		lines := strings.Split(s, "\n")
		for i, line := range lines {
			if line != "" {
				lines[i] = g.indent + line
			}
		}
		s = strings.Join(lines, "\n")
		if trailing {
			s += "\n"
		}
	}
	g.body.WriteString(s)
}

// header is the generated-file banner every module carries.
func header(base, pkg, what string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", base)
	b.WriteString("# SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	b.WriteString("# your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	b.WriteString("# AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&b, "# package %s — %s\n", pkg, what)
	return b.String()
}

// ---- names ----

// elixirName maps an exported member/constant/variant name into Elixir's
// snake_case, the same mapping the packet emitter uses (ir.RustSnake over
// ir.GoExportName), so the two backends spell one declaration one way.
func elixirName(name string) string {
	return ir.RustSnake(ir.GoExportName(name))
}

// fn is the name-first free-function spelling: Cfg + "measure" -> measure_cfg.
// Elixir has no free functions, so these are public functions of the file's
// own <Base>Table module — the file-scope surface the packet emitter already
// established for this target.
func fn(verb, typeName string) string { return verb + "_" + ir.RustSnake(typeName) }

// mod is a declaration's struct module: <Ns>.<Name>.
func (g *gen) mod(name string) string { return g.ns + "." + name }

// moduleBase is a schema FILE's basename as an Elixir ALIAS SEGMENT, and every
// generated module name goes through it.
//
// A module name is not a filename. `my_frame.schema` emits `my_frameTable.ex`,
// which is the packet emitter's own file convention, and the module inside it
// is `<Ns>.MyFrameTable` — because an alias segment must begin upper-case and
// `defmodule Probe.my_frameTable` is an ArgumentError at compile time, not a
// style opinion. The packet emitter already exports the segment
// (internal/codegen/elixir's emitFileModule), and this backend's own
// collision check already reads the exported form, so anything else here would
// leave the check and the emission naming two different modules.
func moduleBase(base string) string { return ir.GoExportName(base) }

// fileMod is the module a declaration's codecs live in: the <Base>Table module
// of the file that DECLARES it, so a unit with several files defines each once.
func (g *gen) fileMod(name string) string {
	base, ok := g.unit.DeclFile[name]
	if !ok {
		base = g.file.Base
	}
	return g.ns + "." + moduleBase(base) + "Table"
}

// call renders a call to one of a closure member's codecs, qualified when the
// member is declared in another file of the unit.
func (g *gen) call(verb, typeName string) string {
	if home, ok := g.unit.DeclFile[typeName]; ok && home != g.file.Base {
		return g.ns + "." + moduleBase(home) + "Table." + fn(verb, typeName)
	}
	return fn(verb, typeName)
}

// keyedSlots renders the expression a codec walks an enum-keyed array's
// storage with, and the two spellings the deviation note names: a table's is
// this backend's tuple, a closure type's is the packet emitter's list.
func (g *gen) keyedIsTuple() bool { return g.owner != nil && g.owner.IsTable }

// keyedAt renders the slot at STORAGE INDEX index.
func (g *gen) keyedAt(f *ir.Field, index string) string {
	if g.keyedIsTuple() {
		return fmt.Sprintf("elem(value.%s, %s)", f.Name, index)
	}
	return fmt.Sprintf("Enum.at(value.%s, %s)", f.Name, index)
}

// tableKindWidth is the payload width a table-wire kind carries.
func tableKindWidth(kind int) int {
	switch kind {
	case ir.TableKindBool, ir.TableKindI8, ir.TableKindU8:
		return 1
	case ir.TableKindI16, ir.TableKindU16:
		return 2
	case ir.TableKindI32, ir.TableKindU32, ir.TableKindF32:
		return 4
	case ir.TableKindI64, ir.TableKindU64, ir.TableKindF64:
		return 8
	}
	return 0
}

// wireSpec renders the bitstring segment spelling one scalar kind's payload,
// explicit little-endian throughout (docs/SPEC-TABLES.md §3).
func wireSpec(kind int) string {
	switch kind {
	case ir.TableKindBool, ir.TableKindU8:
		return "little-unsigned-8"
	case ir.TableKindI8:
		return "little-signed-8"
	case ir.TableKindI16:
		return "little-signed-16"
	case ir.TableKindU16:
		return "little-unsigned-16"
	case ir.TableKindI32:
		return "little-signed-32"
	case ir.TableKindU32, ir.TableKindF32:
		return "little-unsigned-32"
	case ir.TableKindI64:
		return "little-signed-64"
	case ir.TableKindU64, ir.TableKindF64:
		return "little-unsigned-64"
	}
	return "little-unsigned-8"
}

// ---- literals ----

// formatFloat renders a float literal the way this target's STORAGE holds it.
//
// A float32 field's Elixir storage is a BEAM float, which is a double, so the
// value it holds after a wire read is the double the float32 pattern decodes
// to — 0.2f is 0.20000000298023224, not 0.2. A declared default therefore
// narrows to float32 and prints at full double precision, or the write side's
// elision test would compare a read-back value against a number no float32
// equals and ride a field the reference elides.
func formatFloat(v float64, single bool) string {
	if single {
		v = float64(float32(v))
	}
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if strings.ContainsAny(s, "eE") {
		if !strings.Contains(s, ".") {
			i := strings.IndexAny(s, "eE")
			s = s[:i] + ".0" + s[i:]
		}
		return s
	}
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

func intLit(v *big.Int) string { return v.String() }

// ---- field classification ----

func isEnum(f *ir.Field) bool {
	if f.Type.Kind != ir.TNamed {
		return false
	}
	_, ok := f.Type.Ref.(*ir.Enum)
	return ok
}

func isFlags(f *ir.Field) bool {
	if f.Type.Kind != ir.TNamed {
		return false
	}
	_, ok := f.Type.Ref.(*ir.Flags)
	return ok
}

func isUnion(f *ir.Field) bool {
	if f.Type.Kind != ir.TNamed {
		return false
	}
	_, ok := f.Type.Ref.(*ir.Union)
	return ok
}

func isStruct(f *ir.Field) bool {
	if f.Type.Kind != ir.TNamed {
		return false
	}
	_, ok := f.Type.Ref.(*ir.Struct)
	return ok
}

func enumOf(f *ir.Field) *ir.Enum {
	e, _ := f.Type.Ref.(*ir.Enum)
	return e
}

func flagsOf(f *ir.Field) *ir.Flags {
	fl, _ := f.Type.Ref.(*ir.Flags)
	return fl
}

func unionOf(f *ir.Field) *ir.Union {
	un, _ := f.Type.Ref.(*ir.Union)
	return un
}

// tableFieldTypeName is the schema type name a descriptor and a comment carry
// for one field.
func tableFieldTypeName(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TFloat32:
		return "float32"
	case ir.TFloat64:
		return "float64"
	case ir.TString:
		return "string"
	case ir.TBytes:
		return "bytes"
	case ir.TBits:
		return fmt.Sprintf("bits(%d)", f.Type.Width)
	case ir.TInt:
		if f.Type.Signed {
			return fmt.Sprintf("int%d", f.Type.Width)
		}
		return fmt.Sprintf("uint%d", f.Type.Width)
	case ir.TNamed:
		return f.Type.Name
	}
	return "?"
}

// storageRange is the inclusive range an integer storage of the given width
// can hold.
func storageRange(signed bool, bits int) (*big.Int, *big.Int) {
	one := big.NewInt(1)
	if signed {
		hi := new(big.Int).Lsh(one, uint(bits-1))
		return new(big.Int).Neg(hi), new(big.Int).Sub(hi, one)
	}
	return big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(one, uint(bits)), one)
}

// clampEnds answers which ends of a declared min/max range a read can actually
// clamp at. The decoded value is the wire kind's own width, so a bound sitting
// ON that width's limit is a comparison no decoded value can satisfy and the
// emitter drops it (docs/SPEC-TABLES.md §4: an elided end is one that could
// never have clamped, so no read report moves).
func clampEnds(f *ir.Field, widthBytes int) (low, high bool) {
	signed := f.Type.Kind == ir.TInt && f.Type.Signed
	lo, hi := storageRange(signed, widthBytes*8)
	return f.IntMin.Cmp(lo) > 0, f.IntMax.Cmp(hi) < 0
}

// arrayLen renders a field's array extent.
func arrayLen(f *ir.Field) int64 {
	return f.ArrayBound
}

// members is the file's table declarations, ordered so a same-file table
// precedes its by-value users — Elixir compiles a struct default of `%Other{}`
// only where Other is already compiled — plus every closure type declared in
// the file.
func (g *gen) members() []*ir.Struct {
	var members []*ir.Struct
	members = append(members, orderTables(g.file.Tables)...)
	for _, d := range g.file.Decls {
		if st, ok := d.(*ir.Struct); ok && g.closure[st.Name] {
			members = append(members, st)
		}
	}
	return members
}

// orderTables returns a file's tables with every same-file table preceding its
// by-value users. Stable: declaration order survives wherever no dependency
// forces otherwise.
func orderTables(tables []*ir.Struct) []*ir.Struct {
	n := len(tables)
	byName := map[string]int{}
	for i, st := range tables {
		byName[st.Name] = i
	}
	adj := make([][]int, n)
	indeg := make([]int, n)
	for i, st := range tables {
		for _, f := range st.Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			if ref, ok := f.Type.Ref.(*ir.Struct); ok && ref.IsTable {
				if j, ok := byName[ref.Name]; ok && j != i {
					adj[j] = append(adj[j], i)
					indeg[i]++
				}
			}
		}
	}
	order := make([]*ir.Struct, 0, n)
	done := make([]bool, n)
	for len(order) < n {
		pick := -1
		for i := range n {
			if !done[i] && indeg[i] == 0 {
				pick = i
				break
			}
		}
		if pick == -1 {
			for i := range n {
				if !done[i] {
					pick = i
					break
				}
			}
		}
		done[pick] = true
		order = append(order, tables[pick])
		for _, t := range adj[pick] {
			indeg[t]--
		}
	}
	return order
}
