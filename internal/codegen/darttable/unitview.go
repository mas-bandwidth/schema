package darttable

// The optional unit view is a leaf of the table emitter (SPEC-TABLES §8.2–8.5).
// It names declarations from the IR rather than from the card's wire closure.
import (
	"fmt"
	"maps"
	"math"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func viewDartString(s string) string {
	return strings.ReplaceAll(fmt.Sprintf("%q", s), "$", `\$`)
}

func viewDartTags(tags []string) string {
	var b strings.Builder
	b.WriteString("<String>[")
	for i, tag := range tags {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(viewDartString(tag))
	}
	b.WriteString("]")
	return b.String()
}

func viewDartFile(u *ir.Unit, name string) string {
	if f, ok := u.DeclFile[name]; ok {
		return f + ".schema"
	}
	return ""
}

func viewDartArmKind(f *ir.Field) int {
	switch {
	case f.Type.Pointer:
		return ir.TableKindPointer
	case f.Type.Kind == ir.TBytes && !f.Type.Blob():
		return ir.TableKindU8
	default:
		return ir.TableWireScalarKind(f)
	}
}

func viewDartArmBound(f *ir.Field) int64 {
	switch {
	case f.Type.Pointer && f.Array == ir.ArrayNone:
		return 0
	case f.Array != ir.ArrayNone:
		return f.ArrayBound
	case f.Type.Kind == ir.TBytes, f.Type.Kind == ir.TString, f.Type.Kind == ir.TWString:
		return f.Type.Size
	default:
		return 0
	}
}

func viewDartUnionHeld(u *ir.Unit, name string) bool {
	for _, st := range u.Structs {
		for _, f := range st.Fields {
			if f.Type.Kind == ir.TNamed && f.Type.Name == name {
				return true
			}
		}
	}
	for _, st := range u.Tables {
		if st.IsMapEntry() {
			continue
		}
		for _, f := range st.Fields {
			if f.Type.Kind == ir.TNamed && f.Type.Name == name {
				return true
			}
		}
	}
	return false
}

// Dart has no addressable record offsets (§8.1). A scalar's fixed-form
// element width is still useful to a walker; managed values have no such size.
func viewDartElementSize(f *ir.Field) string {
	if f.Type.Pointer || f.Type.Blob() || f.IsMap() {
		return "null"
	}
	switch f.Type.Kind {
	case ir.TString, ir.TBytes:
		return "1"
	case ir.TWString:
		return "2"
	}
	if f.Type.Kind == ir.TNamed {
		switch f.Type.Ref.(type) {
		case *ir.Enum, *ir.Flags:
		default:
			return "null"
		}
	}
	size := ir.TableFixedStorageBytes(f.Type)
	if size <= 0 {
		return "null"
	}
	return fmt.Sprintf("%d", size)
}

// The §8.1 descriptor names an array's element kind, not the wire's array
// wrapper kind. A map's element is its generated entry table.
func viewDartFieldKind(f *ir.Field) int {
	if f.IsMap() {
		return ir.TableKindTable
	}
	if f.Type.Kind == ir.TBytes && !f.Type.Blob() {
		return ir.TableKindU8
	}
	return ir.TableWireScalarKind(f)
}

// generateUnitViewFiles emits the §8 registry even when a declaration is
// outside the wire closure. The separate library makes the vocabulary opt-in.
func generateUnitViewFiles(u *ir.Unit) map[string][]byte {
	var b strings.Builder
	pf := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	pf("// Unit reflection (docs/SPEC-TABLES.md §8.1–8.5).\n")
	pf("// This library is optional and carries every declared item.\n\n")
	pf(`final class ViewField {
  final String name, json, typeName, doc;
  final int id, kind, bound;
  // §8.1's managed-language storage columns: no fictitious byte offset.
  final int? elemSize;
  final dynamic Function(dynamic) read;
  final void Function(dynamic, dynamic) write;
  final bool optional;
  final List<String> tags;
  const ViewField(this.name, this.json, this.typeName, this.id,
      this.optional, this.kind, this.bound, this.elemSize,
      this.read, this.write, this.doc, this.tags);
@@VIEWFIELD_ACCESS@@}
final class ViewType {
  final String name, file, doc;
  final List<String> tags;
  final List<ViewField> fields;
  const ViewType(this.name, this.file, this.doc, this.tags, this.fields);
}
final class ViewVariant {
  final int value, id;
  final String name, payload, overlay, doc;
  final bool record, field;
  final int? kind, bound;
  final List<String> tags;
  const ViewVariant(this.value, this.name, this.id, this.payload,
      this.record, this.field, this.kind, this.bound, this.overlay,
      this.doc, this.tags);
}
final class ViewVocabulary {
  final String name, file, doc;
  final int max, storageBits;
  final List<String> tags;
  final List<ViewVariant> variants;
  const ViewVocabulary(this.name, this.file, this.max, this.storageBits,
      this.doc, this.tags, this.variants);
}
final class ViewConstant {
  final String name, file, typeName, doc;
  final bool isFloat;
  final int intValue, floatBits;
  final List<String> tags;
  const ViewConstant(this.name, this.file, this.typeName, this.isFloat,
      this.intValue, this.floatBits, this.doc, this.tags);
}
final class UnitViewInfo {
  final String package;
  final int protocolId;
  final List<ViewType> types, tables;
  final List<ViewVocabulary> enums, flags, unions;
  final List<ViewConstant> constants;
  const UnitViewInfo(this.package, this.protocolId, this.types, this.tables,
      this.enums, this.flags, this.unions, this.constants);
}

`)
	pf("// The registry is name-ordered; generated map-entry tables are not declarations (§2.8).\n")
	pf("const UnitViewInfo _unitView = UnitViewInfo(%s, %s,\n", viewDartString(u.Package), dartU64Literal(u.ProtocolId))
	closure := ir.TableClosure(u)
	// a field's storage accessors are static members of ViewField: a static
	// tear-off is a constant and a closure is not, so the registry is one const
	// value (docs/PORTING.md M13), and the members live in a claimed class so no
	// schema name collides with them (SPEC §4.6)
	var access []string
	for _, group := range []struct {
		kind string
		set  map[string]*ir.Struct
	}{{"type", u.Structs}, {"table", u.Tables}} {
		pf("  <ViewType>[\n")
		for _, name := range sortedKeys(group.set) {
			st := group.set[name]
			if st.IsMapEntry() {
				continue
			}
			pf("    // %s %s\n", group.kind, name)
			pf("    ViewType(%s,%s,%s,%s,<ViewField>[\n", viewDartString(name), viewDartString(viewDartFile(u, name)), viewDartString(st.Doc), viewDartTags(st.Tags))
			for _, f := range st.Fields {
				json, id := ir.TableFieldJsonKey(f), ir.TableFieldWireId(f)
				if group.kind == "type" && !closure[name] {
					json, id = "-", 0
				}
				n := len(access)
				member := fieldMember(f)
				access = append(access, fmt.Sprintf("  static dynamic _r%d(dynamic v) => v.%s; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design\n  static void _w%d(dynamic v, dynamic x) { v.%s = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design\n", n, member, n, member))
				pf("      ViewField(%s,%s,%s,%s,%t,%d,%d,%s,ViewField._r%d,ViewField._w%d,%s,%s),\n", viewDartString(f.Name), viewDartString(json), viewDartString(ir.TableTypeSpelling(f)), dartU64Literal(id), f.Type.Optional, viewDartFieldKind(f), viewDartArmBound(f), viewDartElementSize(f), n, n, viewDartString(f.Doc), viewDartTags(f.Tags))
			}
			pf("    ]),\n")
		}
		pf("  ],\n")
	}
	reached := ir.TableClosureVocabulary(u)
	for _, group := range []string{"enum", "flags", "union"} {
		pf("  <ViewVocabulary>[\n")
		var names []string
		switch group {
		case "enum":
			names = sortedKeys(u.Enums)
		case "flags":
			names = sortedKeys(u.Flags)
		case "union":
			all := map[string]*ir.Union{}
			maps.Copy(all, u.Unions)
			maps.Copy(all, u.TableUnions)
			names = sortedKeys(all)
		}
		for _, name := range names {
			var max int64
			var bits int
			var doc string
			var tags []string
			switch group {
			case "enum":
				x := u.Enums[name]
				max, bits, doc, tags = x.Max, x.StorageBits, x.Doc, x.Tags
			case "flags":
				x := u.Flags[name]
				max, doc, tags = int64(len(x.Variants)-1), x.Doc, x.Tags
				bits = 64
				for _, n := range []int{8, 16, 32, 64} {
					if x.WireBits <= n {
						bits = n
						break
					}
				}
			case "union":
				x := u.Unions[name]
				if x == nil {
					x = u.TableUnions[name]
				}
				max, bits, doc, tags = x.Max, x.StorageBits, x.Doc, x.Tags
			}
			pf("    // %s %s\n", group, name)
			pf("    ViewVocabulary(%s,%s,%d,%d,%s,%s,<ViewVariant>[\n", viewDartString(name), viewDartString(viewDartFile(u, name)), max, bits, viewDartString(doc), viewDartTags(tags))
			if group != "flags" {
				pf("      ViewVariant(0,'None',0,'-',false,false,null,null,'-','',<String>[]),\n")
			}
			switch group {
			case "enum":
				x := u.Enums[name]
				for i, v := range x.Variants {
					id := uint64(0)
					if reached[name] {
						id = ir.TableWireId(x.VariantWireName(i))
					}
					pf("      ViewVariant(%d,%s,%s,'-',false,false,null,null,'-',%s,%s),\n", i+1, viewDartString(v), dartU64Literal(id), viewDartString(x.VariantDocs[i]), viewDartTags(x.VariantTags[i]))
				}
			case "flags":
				x := u.Flags[name]
				for i, v := range x.Variants {
					pf("      ViewVariant(%d,%s,0,'-',false,false,null,null,'-',%s,%s),\n", i, viewDartString(v), viewDartString(x.VariantDocs[i]), viewDartTags(x.VariantTags[i]))
				}
			case "union":
				x := u.Unions[name]
				if x == nil {
					x = u.TableUnions[name]
				}
				for i, arm := range x.Variants {
					id := uint64(0)
					if reached[name] {
						id = ir.TableWireId(arm.WireName())
					}
					payload, record, field, kind, bound, overlay := "-", false, false, "null", "null", "-"
					if !arm.Void() {
						payload = ir.FieldTypeSpelling(arm.F)
						record, field = arm.Body(), !arm.Body()
						if field {
							kind = fmt.Sprintf("%d", viewDartArmKind(arm.F))
							bound = fmt.Sprintf("%d", viewDartArmBound(arm.F))
							if viewDartUnionHeld(u, name) {
								overlay = "0"
							}
						}
					}
					pf("      ViewVariant(%d,%s,%s,%s,%t,%t,%s,%s,%s,%s,%s),\n", i+1, viewDartString(arm.Name), dartU64Literal(id), viewDartString(payload), record, field, kind, bound, viewDartString(overlay), viewDartString(arm.Doc), viewDartTags(arm.Tags))
				}
			}
			pf("    ]),\n")
		}
		pf("  ],\n")
	}
	pf("  <ViewConstant>[\n")
	for _, name := range sortedKeys(u.Consts) {
		c := u.Consts[name]
		integer := int64(0)
		real := uint64(0)
		// docs/SPEC-TABLES.md §8.3: int_value is the folded value, so a negative
		// constant keeps its sign, and a value outside int64 keeps the
		// two's-complement pattern the column holds (internal/viewlisting, the
		// compiler's own listing, is the oracle)
		if c.IsFloat {
			real = math.Float64bits(c.Float)
		} else if c.Int != nil {
			if c.Int.IsInt64() {
				integer = c.Int.Int64()
			} else {
				integer = int64(c.Int.Uint64())
			}
		}
		pf("    // constant %s\n", name)
		pf("    ViewConstant(%s,%s,%s,%t,%d,%s,%s,%s),\n", viewDartString(name), viewDartString(viewDartFile(u, name)), viewDartString(c.Storage), c.IsFloat, integer, dartU64Literal(real), viewDartString(c.Doc), viewDartTags(c.Tags))
	}
	pf("  ]);\nUnitViewInfo UnitView() => _unitView;\n")
	view := strings.Replace(b.String(), "@@VIEWFIELD_ACCESS@@", strings.Join(access, ""), 1)
	return map[string][]byte{runtimeHome(u) + "View.dart": []byte(view)}
}
