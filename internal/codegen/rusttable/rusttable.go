// Package rusttable emits the Rust table accelerators (docs/SPEC-TABLES.md):
// the block form's <base>_block.rs and the cooked form's read side, with
// their shared runtimes (block_runtime.rs and cook_runtime.rs) and the unit's
// build_version.rs.
//
// THE COOKED AND BLOCKED RECORDS ARE THEIR OWN <Name>Row STRUCTS, as they are
// in C#. C++'s cooked record IS the table's storage struct because both spell
// `string(N)` as `char[N + 1]`; Rust's storage column (SPEC §6.1) spells it
// `[u8; N]`, so the two families cannot be one without moving the whole Rust
// target's storage. The layout model (ir.RecordLayout) is the contract, and the
// Row structs meet it, asserted at COMPILE TIME with const asserts over
// core::mem::offset_of!.
package rusttable

import (
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// BlockRuntimeModule, CookRuntimeModule and BuildVersionModule are the shared
// modules a unit with tables grows.
const (
	BlockRuntimeModule = "block_runtime"
	CookRuntimeModule  = "cook_runtime"
	BuildVersionModule = "build_version"
)

// Generate returns module filename -> file contents for the unit's table
// surface (block form, cooked form, and build version). Empty when the unit
// declares no table: a table-free unit's Rust output is byte-identical with
// this backend in the chain or out of it.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	return GenerateLineage(u, nil)
}

// GenerateLineage is Generate with the LOCK'S LINEAGE handed in as data
// (docs/FIXED-FORM-ALGORITHM.md §5.2's COMPILE, and §5.9 #1: the backend takes
// the lineage AS DATA through a second entry point and opens no file itself).
// `lineage` is per fixed table the lock's entries OLDEST FIRST, each carrying
// §5.2's facts; the table's own layout is appended last when the lineage does
// not already end on it, and the floor is 1 + the highest retired index.
//
// Generate is the NO-LINEAGE case — a unit that was never locked promises
// nothing, so it emits the identity plan and a lineage of one.
func GenerateLineage(u *ir.Unit, lineage map[string][]FixedLineageEntry) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return nil, nil
	}
	out := map[string][]byte{}

	closure := ir.TableClosure(u)
	blocks := ir.Blocks(u)

	// THE VARIABLE FORM (docs/SPEC-TABLES.md §3, §3.1), form byte 1, for the
	// card record's constructs (§2.5, §2.9). The wire is emitted for the
	// variable tables that pass the card boundary; a variable table with any
	// other construct stays ABSENT (its wire is not emitted, never mis-encoded).
	// The by-name refusal (§11) is the card's own path, in cardVariableModules.
	maps.Copy(out, scopedVariableModules(u))

	// THE WIDE KINDS (docs/SPEC-TABLES.md §15): this backend carries the
	// fixed-point and 128-bit table-wire kinds on every form, the variable
	// wire's u128/i128 storage and its raw fixed-point integer included, so it
	// owes ir.WideTableKinds' refusal to no unit and every accelerator rides.
	fixed := anyFixedRoot(u, closure)
	var accelerators error

	if accelerators == nil && anyCookable(u, closure) {
		out[CookRuntimeModule+".rs"] = cookRuntimeModule(u)
	}
	// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3. It is a WIRE
	// form and not an accelerator, so it rides unconditionally the way form 1
	// does in C++ — there is no cargo feature over it, because a wire a
	// consumer can be handed is not something they opt into.
	if fixed {
		out[FixedRuntimeModule+".rs"] = fixedRuntimeModule(u)
		// THE MESSAGE FORM (§3.3) is a fixed table's, so its announcement rides
		// with the fixed form: a unit with a fixed root carries the constant.
		// THE ONE REPORT (§4, §3.3): the variable runtime, when this unit carries
		// it, owns TableReport, and the message runtime then emits none.
		_, variable := out[VariableRuntimeModule+".rs"]
		out[MessageRuntimeModule+".rs"] = messageRuntimeModule(u, !variable)
	} else if anyCookable(u, closure) {
		// a unit with no fixed root still has cooked records, and those name
		// the fixed runtime's row types (TableU128, TableCookSpan)
		out[FixedRuntimeModule+".rs"] = fixedRuntimeModule(u)
	}
	out[BuildVersionModule+".rs"] = buildVersionModule(u)
	// THE VIEW (docs/SPEC-TABLES.md §8): every table's and every type's
	// descriptors, the vocabulary and the unit registry, constant data beside
	// the wire and read by none of it (view.go).
	out[UnitViewModule+".rs"] = unitViewModule(u)

	for _, f := range u.Files {
		g := &gen{unit: u, file: f, closure: closure, lineage: lineage}
		if body := g.recordsModule(); body != nil {
			out[strings.ToLower(f.Base)+"_records.rs"] = body
		}
		if accelerators == nil {
			if body := g.cookModule(); body != nil {
				out[strings.ToLower(f.Base)+"_cook.rs"] = body
			}
		}

		if body := g.fixedModule(); body != nil {
			out[strings.ToLower(f.Base)+"_fixed.rs"] = body
		}
		// THE MESSAGE FORM's bodies (§3.3) ride beside the fixed form they
		// reset from: a fixed root's batch and the walkers of what it reaches.
		if fixed {
			if body := g.messageModule(); body != nil {
				out[strings.ToLower(f.Base)+"_message.rs"] = body
			}
		}
	}

	// the BLOCK form: nothing declares it, every fixed table has one, and it
	// lives in its own modules so a consumer that never opens a block pays
	// only for a module it does not call (docs/SPEC-TABLES.md §19).
	if accelerators == nil && blocks != nil {
		blockOut, err := generateBlocks(u, blocks, "")
		if err != nil {
			return nil, err
		}
		maps.Copy(out, blockOut)
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

// runtimeSourceName is the file the runtime is attributed to: the unit's
// first file that declares a table, so the banner names a real schema file
// rather than the unit key.
func runtimeSourceName(u *ir.Unit) string {
	names := make([]string, 0, len(u.Files))
	for _, f := range u.Files {
		if len(f.Tables) > 0 {
			names = append(names, f.Base)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return u.Package
	}
	return names[0]
}

type gen struct {
	unit    *ir.Unit
	file    *ir.File
	closure map[string]bool
	// lineage is the LOCK'S, per fixed table, OLDEST FIRST (§5.2); nil for a
	// unit that was never locked, whose lineage is its own layout alone.
	lineage map[string][]FixedLineageEntry
	// vc is the unit's carried variable class (variablerefusal.go), shared by
	// every file of one emission so the set is decided once.
	vc *variableClass
	// armIndex is the Rust expression for the element index of the union
	// value being emitted, "" for a union FIELD (index 0). A body arm's
	// retain path is step(ordinal, armIndex): each element of an array of
	// unions is its own body occurrence (docs/SPEC-TABLES.md §6.6), as the
	// oracle keys records by the body instance and never by the field.
	armIndex string

	body strings.Builder
}

// withArmIndex emits with armIndex set to idx, restoring it after.
func (g *gen) withArmIndex(idx string, emit func()) {
	prev := g.armIndex
	g.armIndex = idx
	emit()
	g.armIndex = prev
}

// armPath is the retain path of a union's body arm (docs/SPEC-TABLES.md §6.6).
func (g *gen) armPath(ordinal int) string {
	idx := g.armIndex
	if idx == "" {
		idx = "0"
	}
	return fmt.Sprintf("path.step(%d, %s)", ordinal, idx)
}

func (g *gen) pf(format string, args ...any) {
	fmt.Fprintf(&g.body, format, args...)
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

// rustUint / rustInt name a width's Rust storage. THE 128-BIT ARM IS NATIVE
// HERE: Rust's u128/i128 are sixteen bytes at sixteen on every target this
// backend emits for, which is the piece ir.RecordLayout models for a 128-bit
// integer and the alignment a table's C++ storage spells `alignas( 16 )`
// (docs/SPEC-TABLES.md §7.2, §19.3), so the layout contract's own const
// asserts hold this arm to the model rather than trusting it.
func rustUint(bits int) string {
	switch {
	case bits <= 8:
		return "u8"
	case bits <= 16:
		return "u16"
	case bits <= 32:
		return "u32"
	case bits <= 64:
		return "u64"
	}
	return "u128"
}

func rustInt(bits int) string {
	switch {
	case bits <= 8:
		return "i8"
	case bits <= 16:
		return "i16"
	case bits <= 32:
		return "i32"
	case bits <= 64:
		return "i64"
	}
	return "i128"
}

func isStruct(f *ir.Field) bool {
	if f.Type.Kind != ir.TNamed {
		return false
	}
	_, ok := f.Type.Ref.(*ir.Struct)
	return ok
}

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

// tableFieldTypeName is the schema type name a descriptor and a comment
// carry for one field.
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
	case ir.TFixed:
		return ir.FieldTypeSpelling(f)
	case ir.TNamed:
		return f.Type.Name
	}
	return "?"
}
