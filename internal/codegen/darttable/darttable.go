// Package darttable emits a unit's Dart table surface (docs/SPEC-TABLES.md):
// the BLOCK form's read half in <Base>Block.dart (§19) and the COOKED form's
// read half in <Base>Cook.dart (§7), each with its unit-wide runtime home,
// emitted only when the unit declares tables — and nothing when it declares
// none: a table-free unit's generated Dart is byte-identical with or without
// this package.
//
// THE ACCELERATOR TIER. A block is POINTED AT and a cook is OPENED; neither
// parses a wire, so both reach every fixed table of the unit whatever its
// closure declares. The TABLE WIRE itself — measure, save, load, the reflection
// descriptors and the JSON text form of docs/SPEC-TABLES.md §3, §4, §8 and §16
// in their id-table form — is emitted in two forms below: the VARIABLE form
// (form byte 1) for the card record's closure it owns, and the FIXED form
// (form byte 3) for fixed tables. The full variable class — the remaining
// scalar kinds, blobs, unbounded scalar/string/enum arrays and arrays of
// pointers — is carried by the variable-class card, byte for byte against the
// Go oracle. ROADMAP.md marks the cells.
//
// The C++ backend (internal/codegen/cpptable) is the REFERENCE for the two
// forms that remain: this port mirrors its prologue and header checks, its
// offsets and its descriptor rows, and invents no contract of its own.
package darttable

import (
	"fmt"
	"maps"
	"sort"
	"strings"

	schemacheck "github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// tkU8 is the table-wire kind of a uint8 (docs/SPEC-TABLES.md §3), the one
// kind the block emitter names by number: a string or bytes field's storage
// row is a run of uint8 cells. The number is the wire's, duplicated from
// cpptable deliberately, so a disagreement shows in the shared golden bytes.
const tkU8 = 6

func tableScalarKind(f *ir.Field) int { return ir.TableScalarKind(f) }

// dartReserved is Dart's reserved words, read from the one table (check.ReservedIn,
// SPEC §4.6) and never copied here.
var dartReserved = schemacheck.ReservedIn("Dart")

// dartName maps a schema member/variant name into Dart's lowerCamelCase: the
// first-letter-lowered form of ir.GoExportName. It is the packet emitter's
// mapping, spelled again here because the two packages share no unexported
// helper — and it must stay the same mapping, since the two emitters name the
// same fields of the same storage classes.
func dartName(name string) string {
	s := ir.GoExportName(name)
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// runtimeHome is the basename of the file carrying the unit's shared Dart
// table runtime, and it is THE PACKAGE — the same rule the C# backend holds,
// for a related reason: any rule that picks a FILE picks it off the file
// order and relocates the runtime the day the unit gains a file that sorts
// earlier. Dart differs from C# in that a library is a file, so every other
// <Base>Table.dart IMPORTS this one rather than merely compiling beside it.
func runtimeHome(u *ir.Unit) string {
	home := capitalize(u.Package)
	for _, f := range u.Files {
		if strings.EqualFold(f.Base, home) {
			return f.Base
		}
	}
	return home
}

// Generate emits <Base>Block.dart and <Base>Cook.dart, with their runtime
// homes, for every unit that declares tables, and nothing for one that
// declares none.
func Generate(u *ir.Unit) (map[string][]byte, error) {
	return GenerateLineage(u, nil)
}

// GenerateLineage is [Generate] with the LINEAGE the build holds for each fixed
// table: per table the lock's entries, OLDEST FIRST, the current layout last
// (docs/FIXED-FORM-ALGORITHM.md §5.2, §5.9 #1). The backend opens no file — the
// caller makes `lockfile.Open`, `lockfile.Lineage` and `lockfile.Floor` — so the
// disk is read in one place and a test can play the lock in one line.
func GenerateLineage(u *ir.Unit, lineage map[string][]FixedLineageEntry) (map[string][]byte, error) {
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	if err := checkNames(u); err != nil {
		return nil, err
	}
	closure := ir.TableClosure(u)
	// THE VARIABLE FORM'S BOUNDARY (docs/SPEC-TABLES.md §11): form 1 is
	// emitted for supported roots whether their closure holds a fixed table or
	// not. An unsupported construct in a variable-only unit must refuse the
	// unit by name. In a unit also served by block, cook or fixed form, the
	// unsupported root is omitted from form 1, leaving those independent forms
	// intact; wireClosure admits every fixed or variable root it can encode.
	if !closureHoldsFixedTable(u, closure) {
		if err := checkUnsupportedConstructs(u); err != nil {
			return nil, err
		}
	}
	// THE WIDE KINDS (docs/SPEC-TABLES.md §3, §15). The block row's columns
	// have no fixed-point and no 128-bit kind in this backend yet
	// (schema#366), so the BLOCK form stands down. The COOKED form does carry
	// them: the variable cook writer writes a wide scalar as its raw integer
	// at its kind's width (§7.6), and §3.4's fixed form lays both out by the
	// constant-size table. So a wide unit loses only the block form.
	wide := ir.TableWideFields(u)
	// AND WIDE TEXT STILL STANDS BOTH ACCELERATORS DOWN. `wstring(N)` rides a
	// table under kind 33 (docs/SPEC-TABLES.md §3), which the block row and
	// the cooked node have not been taught here — but §3.4's fixed form lays
	// it out at its bound, a length in CODE UNITS and 2N bytes behind it, so
	// the same scoping applies: the accelerators go, the form stays.
	text := ir.TableWideTextFields(u)
	out := map[string][]byte{}
	if len(text) == 0 {
		blocks := ir.Blocks(u)
		var err error
		if len(wide) == 0 {
			out, err = generateBlockFiles(u, blocks)
			if err != nil {
				return nil, err
			}
		}
		// A wide unit has no BLOCK form to hang a cursor or the build version
		// on, so the cook emits both itself.
		cookBlocks := blocks
		if len(wide) > 0 {
			cookBlocks = &ir.BlockUnit{}
		}
		cooks, err := generateCookFiles(u, cookBlocks)
		if err != nil {
			return nil, err
		}
		maps.Copy(out, cooks)
	}
	// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3: emitted for a
	// unit that declares a fixed table, beside the VARIABLE FORM (form byte 1)
	// emitted for the unit the variable form owns (below).
	fixed, err := generateFixedFiles(u, wide, lineage)
	if err != nil {
		return nil, err
	}
	maps.Copy(out, fixed)
	// THE VARIABLE FORM (docs/SPEC-TABLES.md §3), form byte 1, for EVERY root
	// this backend carries, fixed or variable: form 1 is the table wire every
	// table speaks, and form 3 (above) is a second encoding of a fixed table,
	// not a replacement for it (§2.2, §3.4). A unit with fixed tables therefore
	// emits both libraries, and the tables/messages, tables/scalars and
	// examples-wide oracle rows (fixed roots, form-1 wires) pin the bytes.
	wire, err := generateWireFiles(u)
	if err != nil {
		return nil, err
	}
	maps.Copy(out, wire)
	// The §8 unit registry is a separate, optional Dart library: a card
	// reader may inspect every declaration, not only the wire closure.
	maps.Copy(out, generateUnitViewFiles(u))
	// WIDE TEXT with no fixed form to lay it out in has nothing left to
	// emit, so it is refused whole and by name. The wide SCALARS no longer
	// refuse the unit: the variable cook writer carries them (above, §7.6).
	if len(fixed) == 0 && len(text) > 0 {
		return nil, fmt.Errorf("the Dart table backend carries wstring(N) on the FIXED form and on no other — %s "+
			"(docs/SPEC-TABLES.md §3, §3.4): this unit's tables take no fixed form, so there is nothing here to lay it out in",
			strings.Join(text, ", "))
	}
	return out, nil
}

// closureHoldsFixedTable reports whether the unit's table closure holds a
// `fixed table` (docs/SPEC-TABLES.md §2.2). Such a unit is served by the
// block, cook and fixed forms, so the variable form's REFUSAL in
// checkUnsupportedConstructs does not bind it (docs/SPEC-TABLES.md §11): a
// construct form 1 cannot carry is left out of the form-1 library (no codec
// is claimed for a table holding one) rather than refusing the unit's other
// forms.
func closureHoldsFixedTable(u *ir.Unit, closure map[string]bool) bool {
	variable := ir.VariableTables(u)
	for name := range closure {
		// A GENERATED map-entry table is not a declared `fixed table` and does
		// not buy the unit the fixed form (§2.8): the map FIELD that generated
		// it is a variable-class construct the boundary refuses by name.
		st := u.Tables[name]
		if st != nil && !st.IsMapEntry() && !variable[name] {
			return true
		}
	}
	return false
}

// checkNames refuses a table or field whose Dart spelling is a reserved
// identifier: the block and cook surfaces spell members by dartName, and a
// member named `class` or `hashCode` is a declaration error in Dart.
func checkNames(u *ir.Unit) error {
	for _, name := range sortedKeys(u.Tables) {
		st := u.Tables[name]
		if dartReserved[st.Name] {
			return fmt.Errorf("table %s maps to the reserved Dart identifier %q; rename it", st.Name, st.Name)
		}
		for _, f := range st.Fields {
			// Skip underscore-prefixed fields (e.g., _new) - they map to valid Dart identifiers
			// that don't collide with reserved words (e.g., _new -> _new)
			if strings.HasPrefix(f.Name, "_") {
				continue
			}
			if dartReserved[dartName(f.Name)] {
				return fmt.Errorf("field %s of table %s maps to the reserved Dart identifier %q; rename it",
					f.Name, st.Name, dartName(f.Name))
			}
		}
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// tableFieldTypeName renders a field's schema-facing type name for the
// descriptor and the comments ("float32", "bits(9)", "Grade").
func tableFieldTypeName(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TInt:
		prefix := "int"
		if !f.Type.Signed {
			prefix = "uint"
		}
		return fmt.Sprintf("%s%d", prefix, f.Type.Width)
	case ir.TBits:
		return fmt.Sprintf("bits(%d)", f.Type.Width)
	case ir.TFloat32:
		return "float32"
	case ir.TFloat64:
		return "float64"
	case ir.TString:
		return fmt.Sprintf("string(%d)", f.Type.Size)
	case ir.TBytes:
		return fmt.Sprintf("bytes(%d)", f.Type.Size)
	case ir.TNamed:
		return f.Type.Name
	}
	return "?"
}

// checkUnsupportedConstructs refuses variable-class constructs that are not
// part of the card record's closure (docs/SPEC-TABLES.md §2.1, §2.3, §2.4,
// §2.5, §2.6, §2.8, §11): every construct the card does not use is refused
// by name — the table, the field and the construct — so it is never mis-
// encoded and never silently dropped. The card's own closure — a variable table,
// bool/int32/int64/enum fields, string(N), *string (the string kind of §2.5)
// and unbounded []T of table elements (§2.9) — passes, and so do a map, an
// enum-keyed array and a union whose every part form 1 carries (§2.4, §2.6,
// §2.8).
func checkUnsupportedConstructs(u *ir.Unit) error {
	variable := ir.VariableTables(u)
	for _, name := range sortedKeys(u.Tables) {
		st := u.Tables[name]
		for _, f := range st.Fields {
			// A MAP, AN ENUM-KEYED ARRAY AND A UNION ARM are carried on form 1
			// (wireunion.go, wirearrays.go) exactly when every construct they
			// hold is: the refusals below fire only for one that is not, so a
			// construct this form cannot encode is still refused by name and
			// never mis-encoded (§11).
			carried := cardWireField(f)
			// maps (docs/SPEC-TABLES.md §2.8)
			if (f.Type.Kind == ir.TMap || f.MapEntry != nil) && !carried {
				return fmt.Errorf("table %s field %s: map[K]V is refused in the Dart table backend (docs/SPEC-TABLES.md §2.8, §11)",
					name, f.Name)
			}
			// The three refusals below are SCOPED to the variable form: the
			// card's form-1 wire carries none of them, so a variable table that
			// reaches form 1 must refuse them by name rather than mis-encode or
			// silently drop them. A `fixed table` is untouched — the fixed form
			// (form 3) and its block/cook/message forms carry optional ?T,
			// enum-keyed arrays and union arms by their own layouts (§3.4), and
			// this card does not change them.
			if !variable[name] {
				continue
			}
			// optional ?T (docs/SPEC-TABLES.md §2.3): an optional by-value field
			// carries a generated presence bool beside its value; the card's
			// form-1 wire has no cell for it, so it must be refused, never
			// mis-encoded as its bare value.
			if f.Type.Optional && !carried {
				return fmt.Errorf("table %s field %s: optional ?T is refused in the Dart table backend (docs/SPEC-TABLES.md §2.3, §11)",
					name, f.Name)
			}
			// enum-keyed arrays (docs/SPEC-TABLES.md §2.4): `[E]T` is an
			// ArrayFixed of one slot per named variant, keyed by variant id on
			// the wire; the card's form-1 writer has no keyed body, so it must
			// be refused, never mis-encoded as a plain scalar enum.
			if f.KeyEnum != "" && !carried {
				return fmt.Errorf("table %s field %s: enum-keyed array [E]T is refused in the Dart table backend (docs/SPEC-TABLES.md §2.4, §11)",
					name, f.Name)
			}
			// union arms (docs/SPEC-TABLES.md §2.6): a field whose type names a
			// union carries an arm id and a per-arm body; the card's form-1
			// writer has no union tag, so it must be refused, never silently
			// dropped from the variable form.
			if _, isUnion := f.Type.Ref.(*ir.Union); isUnion && !carried {
				return fmt.Errorf("table %s field %s: a union arm is refused in the Dart table backend (docs/SPEC-TABLES.md §2.6, §11)",
					name, f.Name)
			}
		}
	}
	return nil
}
