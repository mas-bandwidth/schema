// Package viewlisting produces THE PIN the unit registry's corpus gate
// byte-compares against (docs/SPEC-TABLES.md §8.7).
//
// For every unit in the corpus the listing a generated program prints from
// `UnitView()` is byte-identical to the listing the compiler produces from its
// own IR. The compiler's listing is the PIN — the IR is what was declared —
// and each backend's program byte-compares against it for the units that
// backend accepts. Completeness is the count the pin carries: every
// declaration, every field of every type, every variant of every enum, flags
// and union, and every constant, each set in DECLARATION-NAME order, a variant
// list keeping its declared order.
//
// THE LISTING IS LINE-ORIENTED, so a doc comment carrying newlines is
// FLATTENED before it is compared: each newline is written `\n` and the
// escape's own backslash `\\`. Both halves flatten by this one rule, which is
// what keeps the comparison a comparison rather than a formatting argument.
package viewlisting

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Listing renders one unit as the pin.
func Listing(u *ir.Unit) string {
	l := &listing{unit: u, closure: ir.TableClosure(u)}
	l.reached = ir.TableClosureVocabulary(u)
	l.types, l.tables = l.declarations()
	fmt.Fprintf(&l.b, "unit package=%s protocol=%016x\n", u.Package, u.ProtocolId)
	l.constants()
	l.enums()
	l.flags()
	l.unions()
	for _, st := range l.types {
		l.declaration("type", st)
	}
	for _, st := range l.tables {
		l.declaration("table", st)
	}
	return l.b.String()
}

type listing struct {
	b       strings.Builder
	unit    *ir.Unit
	closure map[string]bool
	reached map[string]bool
	types   []*ir.Struct
	tables  []*ir.Struct
}

func (l *listing) constants() {
	names := make([]string, 0, len(l.unit.Consts))
	for name := range l.unit.Consts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		c := l.unit.Consts[name]
		intValue := int64(0)
		real := uint64(0)
		if c.IsFloat {
			real = math.Float64bits(c.Float)
		} else if c.Int != nil {
			intValue = int64(c.Int.Uint64())
		}
		fmt.Fprintf(&l.b, "constant %s file=%s type=%s float=%v int=%d real=%016x %s\n",
			c.Name, l.file(c.Name), c.Storage, c.IsFloat, intValue, real, annotation(c.Doc, c.Tags))
	}
}

func (l *listing) enums() {
	names := make([]string, 0, len(l.unit.Enums))
	for name := range l.unit.Enums {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		e := l.unit.Enums[name]
		fmt.Fprintf(&l.b, "enum %s file=%s max=%d bits=%d variants=%d %s\n",
			e.Name, l.file(e.Name), e.Max, e.StorageBits, len(e.Variants)+1, annotation(e.Doc, e.Tags))
		// row 0 is None, the reserved id, then the variants in DECLARED order
		fmt.Fprintf(&l.b, "enum %s variant 0 None id=%016x %s\n", e.Name, 0, annotation("", nil))
		for i, variant := range e.Variants {
			id := uint64(0)
			if l.reached[e.Name] {
				id = ir.TableWireId(e.VariantWireName(i))
			}
			fmt.Fprintf(&l.b, "enum %s variant %d %s id=%016x %s\n",
				e.Name, i+1, variant, id, annotation(e.VariantDocs[i], e.VariantTags[i]))
		}
	}
}

func (l *listing) flags() {
	names := make([]string, 0, len(l.unit.Flags))
	for name := range l.unit.Flags {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		f := l.unit.Flags[name]
		fmt.Fprintf(&l.b, "flags %s file=%s max=%d bits=%d variants=%d %s\n",
			f.Name, l.file(f.Name), len(f.Variants)-1, flagsStorageBits(f), len(f.Variants), annotation(f.Doc, f.Tags))
		// a BIT INDEX, and no per-variant wire id: a mask's variants ride by
		// position (docs/SPEC-TABLES.md §4, §8.3)
		for i, variant := range f.Variants {
			fmt.Fprintf(&l.b, "flags %s bit %d %s id=%016x %s\n",
				f.Name, i, variant, 0, annotation(f.VariantDocs[i], f.VariantTags[i]))
		}
	}
}

func (l *listing) unions() {
	names := make([]string, 0, len(l.unit.Unions)+len(l.unit.TableUnions))
	all := map[string]*ir.Union{}
	for name, un := range l.unit.Unions {
		names, all[name] = append(names, name), un
	}
	for name, un := range l.unit.TableUnions {
		names, all[name] = append(names, name), un
	}
	sort.Strings(names)
	for _, name := range names {
		un := all[name]
		fmt.Fprintf(&l.b, "union %s file=%s max=%d bits=%d variants=%d %s\n",
			un.Name, l.file(un.Name), un.Max, un.StorageBits, len(un.Variants)+1, annotation(un.Doc, un.Tags))
		fmt.Fprintf(&l.b, "union %s arm 0 None id=%016x payload=- record=no field=no kind=- bound=- probe=- overlay=- %s\n",
			un.Name, 0, annotation("", nil))
		probed := l.unionHasHolder(un)
		for i, arm := range un.Variants {
			id := uint64(0)
			if l.reached[un.Name] {
				id = ir.TableWireId(arm.WireName())
			}
			payload, record, field := "-", "no", "no"
			kind, bound, probe, overlay := "-", "-", "-", "-"
			switch {
			case arm.Void():
			case arm.Body():
				payload, record = ir.FieldTypeSpelling(arm.F), "yes"
			default:
				payload, field = ir.FieldTypeSpelling(arm.F), "yes"
				kind = fmt.Sprintf("%d", armKind(arm.F))
				bound = fmt.Sprintf("%d", armBound(arm.F))
				if probed {
					// every arm overlays the union's payload base, so the
					// offset the registry's row carries is that base and the
					// value written there reads back as the arm's own tag
					probe, overlay = fmt.Sprintf("%d", i+1), "0"
				}
			}
			fmt.Fprintf(&l.b, "union %s arm %d %s id=%016x payload=%s record=%s field=%s kind=%s bound=%s probe=%s overlay=%s %s\n",
				un.Name, i+1, arm.Name, id, payload, record, field, kind, bound, probe, overlay,
				annotation(arm.Doc, arm.Tags))
		}
	}
}

// unionHasHolder reports whether some registry entry declares a FIELD of this
// union's type. The listing's probe is read through such a field: a generic
// walker has no union to instantiate on its own, so it takes the holder's
// storage, writes each general arm's tag at the arm's offset from the base of
// the union's storage, and reads every arm back. The search is the same on
// both halves — the tables in registry order, then the types — so the pin and
// the program agree about which arms carry a probe.
func (l *listing) unionHasHolder(un *ir.Union) bool {
	for _, set := range [][]*ir.Struct{l.tables, l.types} {
		for _, st := range set {
			for _, f := range st.Fields {
				if f.Type.Kind == ir.TNamed && f.Type.Name == un.Name {
					return true
				}
			}
		}
	}
	return false
}

func (l *listing) declaration(what string, st *ir.Struct) {
	fmt.Fprintf(&l.b, "%s %s file=%s fields=%d %s\n",
		what, st.Name, l.file(st.Name), len(st.Fields), annotation(st.Doc, st.Tags))
	outside := !st.IsTable && !l.closure[st.Name]
	for _, f := range st.Fields {
		// the two columns that describe the TABLE WIRE are empty on a `type`
		// no table closure reaches (docs/SPEC-TABLES.md §8.2)
		json, id := ir.TableFieldJsonKey(f), ir.TableFieldWireId(f)
		if outside {
			json, id = "-", 0
		}
		fmt.Fprintf(&l.b, "%s %s field %s json=%s type=%s id=%016x optional=%v %s\n",
			what, st.Name, f.Name, json, ir.TableTypeSpelling(f), id, f.Type.Optional,
			annotation(f.Doc, f.Tags))
	}
}

// declarations returns the `type` set and the `table` set, each in
// declaration-NAME order. A map's GENERATED entry table is claimed rather than
// declared (docs/SPEC-TABLES.md §2.8), so it is not a row of the registry.
func (l *listing) declarations() (types, tables []*ir.Struct) {
	for _, f := range l.unit.Files {
		for _, d := range f.Decls {
			if st, ok := d.(*ir.Struct); ok && !st.IsTable {
				types = append(types, st)
			}
		}
		for _, st := range f.Tables {
			if st.MapEntryOf == "" {
				tables = append(tables, st)
			}
		}
	}
	sort.Slice(types, func(i, j int) bool { return types[i].Name < types[j].Name })
	sort.Slice(tables, func(i, j int) bool { return tables[i].Name < tables[j].Name })
	return types, tables
}

func (l *listing) file(name string) string {
	if base, ok := l.unit.DeclFile[name]; ok {
		return base + ".schema"
	}
	return ""
}

// annotation renders a row's tags and its doc, the doc LAST because it is
// free-form text and everything after it on the line would be ambiguous.
func annotation(doc string, tags []string) string {
	return fmt.Sprintf("tags=[%s] doc=%s", strings.Join(tags, ","), Flatten(doc))
}

// Flatten writes a doc comment as ONE LINE (docs/SPEC-TABLES.md §8.7): each
// newline written `\n` and the escape's own backslash written `\\`.
func Flatten(doc string) string {
	var b strings.Builder
	for _, r := range doc {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// flagsStorageBits is the mask's storage width, the smallest of 8/16/32/64
// that holds every declared bit.
func flagsStorageBits(f *ir.Flags) int {
	for _, w := range []int{8, 16, 32, 64} {
		if f.WireBits <= w {
			return w
		}
	}
	return 64
}

// armKind and armBound are the two columns §8.7 asks a general arm's listing
// to carry off its FIELD descriptor. An arm is a field line, so both read the
// declaration the same way the descriptor's own row does: a pointer arm states
// the WIRE it rides, a byte buffer is an array of u8, and the bound is the
// array's capacity or the string's or buffer's maximum length.
func armKind(f *ir.Field) int {
	switch {
	case f.Type.Pointer:
		return ir.TableKindPointer
	case f.Type.Kind == ir.TBytes && !f.Type.Blob():
		return ir.TableKindU8
	}
	return ir.TableWireScalarKind(f)
}

func armBound(f *ir.Field) int64 {
	switch {
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		return 0
	case f.Array != ir.ArrayNone:
		return f.ArrayBound
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString:
		return f.Type.Size
	}
	return 0
}
