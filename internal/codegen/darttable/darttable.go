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
// in their id-table form — is not emitted for Dart. The port that once stood
// here wrote the form that preceded the id-table wire, which the current
// specification does not describe and the C++ reference does not open; it was
// removed rather than carried (schema#514 is the row that brings the wire to
// Dart). ROADMAP.md marks the cells.
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

	"github.com/mas-bandwidth/schema/v2/ir"
)

// tkU8 is the table-wire kind of a uint8 (docs/SPEC-TABLES.md §3), the one
// kind the block emitter names by number: a string or bytes field's storage
// row is a run of uint8 cells. The number is the wire's, duplicated from
// cpptable deliberately, so a disagreement shows in the shared golden bytes.
const tkU8 = 6

func tableScalarKind(f *ir.Field) int { return ir.TableScalarKind(f) }

// dartReserved is the Dart surface a generated spelling must not land on. It
// is the packet emitter's list plus the members every object inherits, which
// matter here because the per-enum vocabularies are static MEMBERS of
// TableEnumVocab and the per-type accessors are static members of
// <Name>TableFields.
var dartReserved = map[string]bool{
	"assert": true, "break": true, "case": true, "catch": true,
	"class": true, "const": true, "continue": true, "default": true,
	"do": true, "else": true, "enum": true, "extends": true, "false": true,
	"final": true, "finally": true, "for": true, "if": true, "in": true,
	"is": true, "new": true, "null": true, "rethrow": true, "return": true,
	"super": true, "switch": true, "this": true, "throw": true, "true": true,
	"try": true, "var": true, "void": true, "when": true, "while": true,
	"with": true,
	"bool": true, "double": true, "int": true, "num": true, "String": true,
	"List": true, "Object": true, "Endian": true, "ByteData": true,
	"dynamic": true,
	// Object's members: a static member sharing one of these names is a
	// declaration error in Dart, and these are reachable from an enum name
	// (an enum `HashCode` spells the member `hashCode`).
	"hashCode": true, "runtimeType": true, "toString": true,
	"noSuchMethod": true,
}

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
	if len(u.Tables) == 0 {
		return map[string][]byte{}, nil
	}
	if err := checkNames(u); err != nil {
		return nil, err
	}
	// THE WIDE KINDS (docs/SPEC-TABLES.md §15) ARE A REFUSAL OF THE
	// ACCELERATORS, NOT OF THE FIXED FORM. A block row and a cooked node are
	// laid out in the ID-TABLE's kind vocabulary, which has no fixed-point and
	// no 128-bit kind in this backend yet (schema#366) — but §3.4's fixed form
	// carries both by its own constant-size table, a `fixed(I, F)` riding as
	// the raw scaled integer at its storage width and an `int128`/`uint128` as
	// sixteen bytes, the low half then the high. So the refusal is SCOPED to
	// the two accelerators and stated by name in every library this unit gets,
	// rather than taken out on a form that carries the kinds fine.
	wide := ir.TableWideFields(u)
	// AND WIDE TEXT IS THE SAME SHAPE OF REFUSAL. `wstring(N)` rides a table
	// under kind 33 (docs/SPEC-TABLES.md §3), which the block row and the
	// cooked node have not been taught here either — but §3.4's fixed form
	// lays it out at its bound, a length in CODE UNITS and 2N bytes behind
	// it, so the same scoping applies: the accelerators go, the form stays.
	text := ir.TableWideTextFields(u)
	out := map[string][]byte{}
	if len(wide) == 0 && len(text) == 0 {
		blocks := ir.Blocks(u)
		var err error
		out, err = generateBlockFiles(u, blocks)
		if err != nil {
			return nil, err
		}
		cooks, err := generateCookFiles(u, blocks)
		if err != nil {
			return nil, err
		}
		maps.Copy(out, cooks)
	}
	// THE FIXED FORM (docs/SPEC-TABLES.md §3.4), form byte 3: this backend's
	// FIRST table wire. Form 1 is still deferred to schema#514 and nothing
	// here reads or writes one.
	fixed, err := generateFixedFiles(u, wide)
	if err != nil {
		return nil, err
	}
	maps.Copy(out, fixed)
	// A unit whose wide kinds cost it the accelerators AND that has no fixed
	// form to put in their place has nothing to emit, so it is refused whole
	// and by name, exactly as it was before the form arrived.
	if len(fixed) == 0 {
		if len(wide) > 0 {
			return nil, ir.RefuseWideTableKinds(u, "Dart")
		}
		if len(text) > 0 {
			return nil, fmt.Errorf("the Dart table backend carries wstring(N) on the FIXED form and on no other — %s "+
				"(docs/SPEC-TABLES.md §3, §3.4): this unit's tables take no fixed form, so there is nothing here to lay it out in",
				strings.Join(text, ", "))
		}
	}
	return out, nil
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
