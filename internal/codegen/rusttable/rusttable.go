// Package rusttable emits the Rust TABLE surface (docs/SPEC-TABLES.md), mirroring
// internal/codegen/cpptable — the reference — the way internal/codegen/cstable
// does. One module per unit file that declares tables (<base>_table.rs), one
// shared runtime module per unit (table_runtime.rs), and the two ACCELERATORS
// beside them: the block form's <base>_block.rs and the cooked form's read
// side.
//
// The wire is neutral, evolution-tolerant TLV: field identity is the name
// hash, unknown fields skip, absent fields default, changed kinds skip (never
// misdecode), out-of-range values clamp, framing damage stops the decode with
// a partial result — and every event lands in the TableReport. Plain byte
// code with no serialize dependency, so a table module compiles into any
// crate; the encode surface is a measure/save split, so a caller can measure
// nested tables in parallel, prefix-sum offsets, and scatter-write disjoint
// ranges from N workers. Generated codecs allocate nothing: the caller owns
// every buffer.
//
// TWO DEVIATIONS FROM THE C++ REFERENCE, both forced by the language and both
// named here rather than discovered in the source:
//
//   - THE REPORT IS NOT INSIDE THE READER. C++'s TableReader carries a
//     TableReport pointer; a Rust reader carrying `&mut TableReport` could
//     not hand a sub-reader out of its own buffer while that borrow stood.
//     The report is therefore the codecs' second parameter, threaded down
//     beside the reader. One report, one caller, same counters.
//   - THE COOKED AND BLOCKED RECORDS ARE THEIR OWN <Name>Row STRUCTS, as
//     they are in C#. C++'s cooked record IS the table's storage struct
//     because both spell `string(N)` as `char[N + 1]`; Rust's storage column
//     (SPEC §6.1) spells it `[u8; N]`, so the two families cannot be one
//     without moving the whole Rust target's storage. The layout model
//     (ir.RecordLayout) is the contract, and the Row structs meet it, asserted
//     at COMPILE TIME with const asserts over core::mem::offset_of!.
package rusttable

import (
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// table-wire kinds (docs/SPEC-TABLES.md §3), named locally over the one
// target-independent definition in ir — the vocabulary is wire law.
const (
	tkBool   = ir.TableKindBool
	tkI8     = ir.TableKindI8
	tkI16    = ir.TableKindI16
	tkI32    = ir.TableKindI32
	tkI64    = ir.TableKindI64
	tkU8     = ir.TableKindU8
	tkU16    = ir.TableKindU16
	tkU32    = ir.TableKindU32
	tkU64    = ir.TableKindU64
	tkF32    = ir.TableKindF32
	tkF64    = ir.TableKindF64
	tkString = ir.TableKindString
	tkTable  = ir.TableKindTable
	tkArray  = ir.TableKindArray
	tkUnion  = ir.TableKindUnion
	tkKeyed  = ir.TableKindKeyed
)

// RuntimeModule and BlockRuntimeModule are the two SHARED modules a unit with
// tables grows. They are module names, so they are claimed the way the crate
// root is: a schema file lowering to one of them would be silently replaced.
const (
	RuntimeModule      = "table_runtime"
	BlockRuntimeModule = "block_runtime"
)

// Generate returns module filename -> file contents for the unit's table
// surface. Empty when the unit declares no table: a table-free unit's Rust
// output is byte-identical with this backend in the chain or out of it.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return nil, nil
	}
	out := map[string][]byte{}

	variable := ir.VariableTables(u)
	closure := ir.TableClosure(u)
	blocks := ir.Blocks(u)

	// the WIRE surface is refused by name for a unit that declares a
	// variable-length table, exactly as the C# backend refuses it
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

	if len(refused) == 0 {
		out[RuntimeModule+".rs"] = runtimeModule(u, closure)
	}

	for _, f := range u.Files {
		g := &gen{unit: u, file: f, variable: variable, closure: closure, blocks: blocks, banner: banner}
		if len(refused) == 0 {
			if body := g.tableModule(); body != nil {
				out[strings.ToLower(f.Base)+"_table.rs"] = body
			}
		}
		if body := g.cookModule(); body != nil {
			out[strings.ToLower(f.Base)+"_cook.rs"] = body
		}
	}

	// the BLOCK form: nothing declares it, every fixed table has one, and it
	// lives in its own modules so a consumer that never opens a block pays
	// only for a module it does not call (docs/SPEC-TABLES.md §19).
	if blocks != nil {
		blockOut, err := generateBlocks(u, blocks, banner)
		if err != nil {
			return nil, err
		}
		for name, data := range blockOut {
			out[name] = data
		}
	}
	return out, nil
}

// Modules returns the module names Generate would produce for the unit, so
// the Rust crate root can declare them without the caller reparsing filenames.
func Modules(out map[string][]byte) []string {
	names := make([]string, 0, len(out))
	for name := range out {
		names = append(names, strings.TrimSuffix(name, ".rs"))
	}
	sort.Strings(names)
	return names
}

func refusalBanner(refused []string) string {
	var b strings.Builder
	b.WriteString("// THE RUST WIRE SURFACE OF THIS UNIT IS REFUSED, BY NAME (docs/SPEC-TABLES.md §11).\n")
	b.WriteString("//\n")
	fmt.Fprintf(&b, "// It declares variable-length tables (%s), and the Rust table\n", englishList(refused))
	b.WriteString("// backend's VARIABLE CLASS — the arena, the builder, the region and the node-table\n")
	b.WriteString("// codec — is a named follow-on (§15). No <base>_table.rs is emitted for this unit,\n")
	b.WriteString("// so a consumer reaching for measure, save or load gets a missing name from its own\n")
	b.WriteString("// compiler, beside this file, which says why.\n")
	b.WriteString("//\n")
	b.WriteString("// What IS emitted is the two ACCELERATORS, because neither needs a codec: a block\n")
	b.WriteString("// (§19) and a cook (§7) are pointed at, not parsed. A build that loads this unit's\n")
	b.WriteString("// cooked assets is served in full; one that wants the tolerant wire is not, and\n")
	b.WriteString("// runs the tool or the C++ backend for it.\n\n")
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
	unit     *ir.Unit
	file     *ir.File
	variable map[string]bool
	closure  map[string]bool
	blocks   *ir.BlockUnit
	banner   string

	// owner is the closure member whose codec is being emitted. It decides
	// how an enum-keyed array is REACHED: a `table` declaration's storage is
	// this backend's own TableKeyed, and a closure `type`'s comes from the
	// packet emitter, where the same declaration is a plain [T; E.Max].
	owner *ir.Struct

	body   strings.Builder
	indent string
}

// keyedSlots is the expression a codec walks an enum-keyed array's storage
// with, by STORAGE INDEX.
func (g *gen) keyedSlots(f *ir.Field) string {
	if g.owner != nil && g.owner.IsTable {
		return "value." + f.Name + ".slots"
	}
	return "value." + f.Name
}

func (g *gen) pf(format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	if g.indent != "" && s != "" {
		trailing := strings.HasSuffix(s, "\n")
		if trailing {
			s = s[:len(s)-1]
		}
		s = g.indent + strings.ReplaceAll(s, "\n", "\n"+g.indent)
		if trailing {
			s += "\n"
		}
	}
	g.body.WriteString(s)
}

// header is the generated-file banner every module carries.
func header(base, pkg string, what string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by the schema compiler from %s.schema. DO NOT EDIT.\n", base)
	b.WriteString("// SPDX-License-Identifier: NONE — this generated output is yours, under terms of\n")
	b.WriteString("// your choice. See the LICENSE exception in the schema compiler; the compiler is\n")
	b.WriteString("// AGPL-3.0, its output is not.\n")
	fmt.Fprintf(&b, "// package %s — %s\n", pkg, what)
	return b.String()
}

// ---- names ----

// fn is the name-first free-function spelling: Cfg + "measure" -> cfg_measure.
func fn(typeName, verb string) string { return ir.RustSnake(typeName) + "_" + verb }

// rustUint / rustInt name a width's Rust storage.
func rustUint(bits int) string {
	switch {
	case bits <= 8:
		return "u8"
	case bits <= 16:
		return "u16"
	case bits <= 32:
		return "u32"
	}
	return "u64"
}

func rustInt(bits int) string {
	switch {
	case bits <= 8:
		return "i8"
	case bits <= 16:
		return "i16"
	case bits <= 32:
		return "i32"
	}
	return "i64"
}

// rustFieldType is a field's Rust ELEMENT storage type, matching the packet
// emitter's column (SPEC §6.1).
func rustFieldType(t ir.FieldType) string {
	switch t.Kind {
	case ir.TInt:
		if t.Signed {
			return rustInt(t.Width)
		}
		return rustUint(t.Width)
	case ir.TBits:
		if t.Width <= 32 {
			return "u32"
		}
		return "u64"
	case ir.TBool:
		return "bool"
	case ir.TFloat32:
		return "f32"
	case ir.TFloat64:
		return "f64"
	case ir.TString, ir.TBytes:
		// the ELEMENT of a string or a bytes buffer; the buffer itself is
		// spelled at its declaration site, which knows the extent
		return "u8"
	case ir.TNamed:
		if _, isFlags := t.Ref.(*ir.Flags); isFlags {
			return "u64"
		}
		return t.Name
	}
	return "u8"
}

// scalarKindWidth is the payload width a table-wire kind carries.
func tableKindWidth(kind int) int {
	switch kind {
	case tkBool, tkI8, tkU8:
		return 1
	case tkI16, tkU16:
		return 2
	case tkI32, tkU32, tkF32:
		return 4
	case tkI64, tkU64, tkF64:
		return 8
	}
	return 0
}

func putFn(width int) string { return fmt.Sprintf("put%d", width*8) }
func getFn(width int) string { return fmt.Sprintf("get%d", width*8) }

// ---- literals ----

// formatFloat renders a float literal at the storage type's own precision, so
// the emitted clamp bounds and defaults are exactly the values the runtime
// compares against. Rust needs an explicit decimal point on every float
// literal.
func formatFloat(v float64, single bool) string {
	bitSize := 64
	if single {
		bitSize = 32
	}
	s := strconv.FormatFloat(v, 'g', -1, bitSize)
	if strings.ContainsAny(s, "eE") {
		// 1e-05 is a valid Rust float literal only with a point before the e
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

// intLit renders an integer literal at a Rust storage type: i64::MIN has no
// negative literal form (the token would be an unsigned literal negated).
func intLit(v *big.Int, typ string) string {
	s := v.String()
	if typ == "i64" && s == "-9223372036854775808" {
		return "i64::MIN"
	}
	return s
}

// keyedType is the Rust spelling of an enum-keyed array's storage: the extent
// comes from the key enum's own MAX and is named nowhere else (§2.4).
func keyedType(f *ir.Field) string {
	return fmt.Sprintf("TableKeyed<%s, { %s::MAX.0 as usize }>", rustFieldType(f.Type), f.KeyEnum)
}

// keyedTypeExpr is the same type in EXPRESSION position, where Rust wants the
// turbofish.
func keyedTypeExpr(f *ir.Field) string {
	return fmt.Sprintf("TableKeyed::<%s, { %s::MAX.0 as usize }>", rustFieldType(f.Type), f.KeyEnum)
}

// arrayLen renders a field's array extent as a Rust expression — parenthesised,
// never braced, because a braced block in a for-range position is the loop's
// own body.
func arrayLen(f *ir.Field) string {
	if f.KeyEnum != "" {
		return fmt.Sprintf("(%s::MAX.0 as usize)", f.KeyEnum)
	}
	return strconv.FormatInt(f.ArrayBound, 10)
}

// isEnum / isFlags / isUnion / isStruct classify a TNamed field.
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

func unionOf(f *ir.Field) *ir.Union {
	un, _ := f.Type.Ref.(*ir.Union)
	return un
}
