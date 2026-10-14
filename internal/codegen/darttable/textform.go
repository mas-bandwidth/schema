// THE TEXT FORM (docs/SPEC-TABLES.md §16), JSON in and out, for exactly the
// card record's constructs — a plain `table` root, tables as unbounded list
// elements, bool/int32/int64/enum fields, `string(N)`, `*string`, `*bytes`
// (base64 in the text) and `[]T` of tables — the same slice the wire form
// (wireform.go) carries on form byte 1.
//
// The writer renders one instance as one JSON text with the byte layout the
// Go, C, C++ and C# backends write (docs/SPEC-TABLES.md §16.1): two-space
// indent, `": "` between a key and its value, a comma closing the previous
// line, `{}` and `[]` for the empty forms, and exactly one trailing newline.
// The reader walks a decoded JSON value with §16.2's field-kind mapping and
// §16.3's refusals: an unknown property is skipped and counted, an enum value
// no variant names reads as None and counts unknown, and a string past its
// bound is clamped at a code-point boundary and counted — never cut through a
// multi-byte character. Every function below cites the section it reads.
//
// This is the card's SLICE of the variable-class port the conformance README
// tracks (schema#349): every other variable-class construct is refused by name
// in checkUnsupportedConstructs (§11), and nothing here reads or writes one.
package darttable

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// textEnums is every enum a closure field names, in declaration-name order:
// the text form spells an enum by variant NAME (§16.2), so each such enum owes
// a name<->value helper emitted once beside the tables.
func (g *wireGen) textEnums() []*ir.Enum {
	seen := map[string]bool{}
	var out []*ir.Enum
	for _, st := range g.closure {
		for _, f := range st.Fields {
			if e := enumRef(f); e != nil && !seen[e.Name] {
				seen[e.Name] = true
				out = append(out, e)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// emitTextForm emits the JSON text half of the wire library, once the wire
// load/save have landed: the generic writer/reader runtime, the enum helpers,
// and per-table toJson/fromJson bodies (roots get the top-level entry points).
func (g *wireGen) emitTextForm() {
	g.emitTextRuntime()
	for _, e := range g.textEnums() {
		g.emitTextEnum(e)
	}
	for _, st := range g.closure {
		g.emitTextToJsonBody(st)
		g.emitTextFromJsonFields(st)
	}
	for _, st := range g.roots {
		g.emitTextToJson(st)
		g.emitTextFromJson(st)
	}
}

// emitTextRuntime emits the shared JSON text machinery (§16.1, §16.3), one per
// unit: the report, the writer sink, the string/number/enum helpers. It
// invents no contract of its own — the byte layout is §16.1's, read down to the
// card's closure.
func (g *wireGen) emitTextRuntime() {
	g.pf(`// ---- the JSON text form (docs/SPEC-TABLES.md §16) ----

// TableTextReport is a tolerant text read's report (§16.3): the three events
// the card's constructs can move — an unknown property, a kind mismatch, a
// clamp.
final class TableTextReport {
  int unknown = 0;
  int kindMismatch = 0;
  int clamped = 0;
}

// _JOut is the writer's sink (§16.1): raw text and the two-space indent.
final class _JOut {
  final StringBuffer b = StringBuffer();
  void raw(String s) => b.write(s);
  void line(int depth) {
    b.write('\n');
    for (var i = 0; i < depth; i++) {
      b.write('  ');
    }
  }

  @override
  String toString() => b.toString();
}

const String _jsonHex = '0123456789abcdef';

// _jsonString renders one Dart string as one JSON string (§16.3): the escapes
// JSON names and every other code point whole, so a multi-byte character rides
// as itself.
String _jsonString(String s) {
  final b = StringBuffer();
  b.write('"');
  for (final rune in s.runes) {
    if (rune == 0x22) {
      b.write('\\"');
    } else if (rune == 0x5C) {
      b.write('\\\\');
    } else if (rune == 0x08) {
      b.write('\\b');
    } else if (rune == 0x0C) {
      b.write('\\f');
    } else if (rune == 0x0A) {
      b.write('\\n');
    } else if (rune == 0x0D) {
      b.write('\\r');
    } else if (rune == 0x09) {
      b.write('\\t');
    } else if (rune < 0x20) {
      b.write('\\u00');
      b.write(_jsonHex[rune >> 4]);
      b.write(_jsonHex[rune & 0xF]);
    } else {
      b.write(String.fromCharCode(rune));
    }
  }
  b.write('"');
  return b.toString();
}

// _jsonEnum spells an enum value by NAME (§16.2); a value no variant names has
// no text spelling and the writer refuses it, exactly as measure and save do
// (docs/SPEC-TABLES.md §5, §16.3).
String _jsonEnum(int v, String Function(int) name, String where) {
  final n = name(v);
  if (n.isEmpty) {
    throw StateError('$where: enum value $v names no variant (docs/SPEC-TABLES.md §5, §16.2)');
  }
  return _jsonString(n);
}

// _jsonStringField reads one string(N) field (§16.2): a non-string is a kind
// mismatch; a string past N is clamped at a CODE POINT boundary and counted,
// never cut through a multi-byte character (§16.3).
String _jsonStringField(dynamic val, int bound, TableTextReport r) {
  if (val is! String) {
    r.kindMismatch++;
    return '';
  }
  if (utf8.encode(val).length <= bound) return val;
  final kept = StringBuffer();
  var used = 0;
  for (final rune in val.runes) {
    final len = utf8.encode(String.fromCharCode(rune)).length;
    if (used + len > bound) break;
    kept.write(String.fromCharCode(rune));
    used += len;
  }
  r.clamped++;
  return kept.toString();
}

// _jsonIntField reads one int field (§16.2): an integer token, or an integral
// float, placed and range-clamped; anything else is a kind mismatch.
int _jsonIntField(dynamic val, TableTextReport r, int? lo, int? hi) {
  if (val is int) {
    var v = val;
    if (lo != null && v < lo) { v = lo; r.clamped++; }
    if (hi != null && v > hi) { v = hi; r.clamped++; }
    return v;
  }
  if (val is num && val.toDouble() == val.toDouble().truncateToDouble()) {
    var v = val.toInt();
    if (lo != null && v < lo) { v = lo; r.clamped++; }
    if (hi != null && v > hi) { v = hi; r.clamped++; }
    return v;
  }
  r.kindMismatch++;
  return 0;
}

// _jsonFloat spells a float at the shortest precision that reads back the same
// value (§16.2); a non-finite float has no JSON spelling and the writer refuses
// it rather than losing it silently (docs/SPEC-TABLES.md §16.2).
String _jsonFloat(double v) {
  if (v.isNaN || v.isInfinite) {
    throw StateError('float $v has no JSON spelling (docs/SPEC-TABLES.md §16.2)');
  }
  return v.toString();
}

// _jsonFloatField reads one float field (§16.2): a number, placed; anything
// else is a kind mismatch.
double _jsonFloatField(dynamic val, TableTextReport r) {
  if (val is num) return val.toDouble();
  if (val is String) {
    final d = double.tryParse(val);
    if (d != null) return d;
  }
  r.kindMismatch++;
  return 0.0;
}
`)
}

// emitTextEnum emits one enum's name<->value helpers (§16.2): the storage
// value 1..N to its declared name, and a scanned name back to its value (an
// unknown name reads as None and counts unknown, §16.3).
func (g *wireGen) emitTextEnum(e *ir.Enum) {
	dn := dartName(e.Name)
	g.pf("String %sName(int v) {\n", dn)
	g.pf("\tswitch (v) {\n")
	g.pf("\t\tcase 0: return \"None\";\n")
	for i, name := range e.Variants {
		g.pf("\t\tcase %d: return %s;\n", i+1, strconv.Quote(name))
	}
	g.pf("\t}\n\treturn '';\n}\n\n")
	g.pf("int %sFromName(dynamic val, TableTextReport r) {\n", dn)
	g.pf("\tif (val is! String) { r.kindMismatch++; return 0; }\n")
	g.pf("\tswitch (val) {\n")
	g.pf("\t\tcase \"None\": return 0;\n")
	for i, name := range e.Variants {
		g.pf("\t\tcase %s: return %d;\n", strconv.Quote(name), i+1)
	}
	g.pf("\t}\n\tr.unknown++;\n\treturn 0;\n}\n\n")
}

// emitTextToJsonBody emits one table's text write body: every field, in
// declaration order, defaults included (§16.1) — a text is for people and
// tools, and a text that elides is a text a reader has to know the schema to
// complete. The comma that closes the previous line is unrolled at generation
// time, so the emitted text carries no dead branch for the analyzer to flag.
func (g *wireGen) emitTextToJsonBody(st *ir.Struct) {
	fn := dartName(st.Name) + "ToJsonBody"
	g.pf("void %s(_JOut out, %s v, int depth) {\n", fn, st.Name)
	if len(st.Fields) == 0 {
		g.pf("\tout.raw('{}');\n")
		g.pf("}\n\n")
		return
	}
	g.pf("\tout.raw('{');\n")
	for i, f := range st.Fields {
		if i > 0 {
			g.pf("\tout.raw(',');\n")
		}
		g.emitTextToJsonField(st, f)
	}
	g.pf("\tout.line(depth);\n")
	g.pf("\tout.raw('}');\n")
	g.pf("}\n\n")
}

func (g *wireGen) emitTextToJsonField(st *ir.Struct, f *ir.Field) {
	key := ir.TableFieldJsonKey(f)
	name := fieldMember(f)
	g.pf("\tout.line(depth + 1);\n")
	g.pf("\tout.raw(_jsonString(%s));\n", strconv.Quote(key))
	g.pf("\tout.raw(': ');\n")
	switch {
	case f.Type.Blob(): // *string rides as a string or null; *bytes as base64 or null (§2.5, §16.2)
		if f.Type.Kind == ir.TString {
			g.pf("\tif (v.%s == null) { out.raw('null'); } else { out.raw(_jsonString(v.%s!)); }\n", name, name)
		} else {
			g.pf("\tif (v.%s == null) { out.raw('null'); } else { out.raw(_jsonString(base64Encode(v.%s!))); }\n", name, name)
		}
	case f.IsList(): // []T rides as an array of objects or of its element's own spelling (§2.9, §16.2)
		if listElementKind(f) == wireKindTable {
			g.pf("\tif (v.%s.isEmpty) {\n", name)
			g.pf("\t\tout.raw('[]');\n")
			g.pf("\t} else {\n")
			g.pf("\t\tout.raw('[');\n")
			g.pf("\t\tvar first = true;\n")
			g.pf("\t\tfor (final e in v.%s) {\n", name)
			g.pf("\t\t\tif (!first) { out.raw(','); }\n")
			g.pf("\t\t\tfirst = false;\n")
			g.pf("\t\t\tout.line(depth + 2);\n")
			g.pf("\t\t\t%sToJsonBody(out, e, depth + 2);\n", dartName(f.Type.Name))
			g.pf("\t\t}\n")
			g.pf("\t\tout.line(depth + 1);\n")
			g.pf("\t\tout.raw(']');\n")
			g.pf("\t}\n")
		} else {
			g.pf("\tif (v.%s.isEmpty) {\n", name)
			g.pf("\t\tout.raw('[]');\n")
			g.pf("\t} else {\n")
			g.pf("\t\tout.raw('[');\n")
			g.pf("\t\tvar first = true;\n")
			g.pf("\t\tfor (final e in v.%s) {\n", name)
			g.pf("\t\t\tif (!first) { out.raw(','); }\n")
			g.pf("\t\t\tfirst = false;\n")
			switch listElementKind(f) {
			case wireKindBool:
				g.pf("\t\t\tout.raw(e ? 'true' : 'false');\n")
			case wireKindF32, wireKindF64:
				g.pf("\t\t\tout.raw(_jsonFloat(e));\n")
			case wireKindEnum:
				g.pf("\t\t\tout.raw(_jsonEnum(e, %sName, '%s.%s'));\n", dartName(enumRef(f).Name), st.Name, f.Name)
			default:
				g.pf("\t\t\tout.raw(e.toString());\n")
			}
			g.pf("\t\t}\n")
			g.pf("\t\tout.raw(']');\n")
			g.pf("\t}\n")
		}
	case f.Type.Kind == ir.TBool:
		g.pf("\tout.raw(v.%s ? 'true' : 'false');\n", name)
	case f.Type.Kind == ir.TInt, f.Type.Kind == ir.TBits:
		g.pf("\tout.raw(v.%s.toString());\n", name)
	case f.Type.Kind == ir.TString:
		g.pf("\tout.raw(_jsonString(v.%s));\n", name)
	case f.Type.Kind == ir.TBytes: // bytes(N) rides as base64 in the text (§16.7)
		g.pf("\tout.raw(_jsonString(base64Encode(utf8.encode(v.%s))));\n", name)
	case f.Type.Kind == ir.TFloat32, f.Type.Kind == ir.TFloat64:
		g.pf("\tout.raw(_jsonFloat(v.%s));\n", name)
	case enumRef(f) != nil:
		g.pf("\tout.raw(_jsonEnum(v.%s, %sName, '%s.%s'));\n", name, dartName(enumRef(f).Name), st.Name, f.Name)
	default:
		// No other construct reaches this emitter: checkUnsupportedConstructs
		// refuses maps, *bytes, pointers and the wide kinds by name (§11).
		g.pf("\t// %s: not a card construct; refused by name (§11)\n", name)
	}
}

// emitTextFromJsonFields emits one table's tolerant text read body (§16.3): a
// named key places its value, an unknown key is skipped and counted, and the
// table's storage defaults survive any key the text does not carry.
func (g *wireGen) emitTextFromJsonFields(st *ir.Struct) {
	fn := dartName(st.Name) + "FromJsonFields"
	g.pf("void %s(%s v, Map<String, dynamic> obj, TableTextReport r) {\n", fn, st.Name)
	g.pf("\tfor (final entry in obj.entries) {\n")
	g.pf("\t\tfinal key = entry.key;\n")
	g.pf("\t\tfinal val = entry.value;\n")
	g.pf("\t\tswitch (key) {\n")
	for _, f := range st.Fields {
		g.emitTextFromJsonField(f)
	}
	g.pf("\t\t\tdefault:\n\t\t\t\tr.unknown++;\n")
	g.pf("\t\t}\n")
	g.pf("\t}\n")
	g.pf("}\n\n")
}

func (g *wireGen) emitTextFromJsonField(f *ir.Field) {
	key := ir.TableFieldJsonKey(f)
	name := fieldMember(f)
	g.pf("\t\t\tcase %s:\n", strconv.Quote(key))
	switch {
	case f.Type.Blob():
		if f.Type.Kind == ir.TString {
			g.pf("\t\t\t\tif (val == null) { v.%s = null; }\n", name)
			g.pf("\t\t\t\telse if (val is String) { v.%s = val; }\n", name)
			g.pf("\t\t\t\telse { r.kindMismatch++; }\n")
		} else {
			g.pf("\t\t\t\tif (val == null) { v.%s = null; }\n", name)
			g.pf("\t\t\t\telse if (val is String) { v.%s = base64Decode(val); }\n", name)
			g.pf("\t\t\t\telse { r.kindMismatch++; }\n")
		}
		g.pf("\t\t\t\tbreak;\n")
	case f.IsList():
		g.pf("\t\t\t\tif (val is! List) { r.kindMismatch++; break; }\n")
		et := dartListElemType(f)
		g.pf("\t\t\t\tv.%s = <%s>[];\n", name, et)
		g.pf("\t\t\t\tfor (final item in val) {\n")
		if listElementKind(f) == wireKindTable {
			g.pf("\t\t\t\t\tif (item is Map<String, dynamic>) {\n")
			g.pf("\t\t\t\t\t\tfinal e = %s();\n", f.Type.Name)
			g.pf("\t\t\t\t\t\t%sFromJsonFields(e, item, r);\n", dartName(f.Type.Name))
			g.pf("\t\t\t\t\t\tv.%s.add(e);\n", name)
			g.pf("\t\t\t\t\t} else {\n")
			g.pf("\t\t\t\t\t\tr.kindMismatch++;\n")
			g.pf("\t\t\t\t\t}\n")
		} else {
			switch listElementKind(f) {
			case wireKindBool:
				g.pf("\t\t\t\t\tif (item is bool) { v.%s.add(item); } else { r.kindMismatch++; }\n", name)
			case wireKindF32, wireKindF64:
				g.pf("\t\t\t\t\tv.%s.add(_jsonFloatField(item, r));\n", name)
			case wireKindEnum:
				g.pf("\t\t\t\t\tv.%s.add(%sFromName(item, r));\n", name, dartName(enumRef(f).Name))
			default:
				lo, hi := "null", "null"
				if f.HasIntRange {
					lo, hi = f.IntMin.String(), f.IntMax.String()
				}
				if f.Type.Kind == ir.TBits && f.Type.Width < 64 {
					hi = fmt.Sprintf("%d", uint64(1)<<f.Type.Width-1)
				}
				g.pf("\t\t\t\t\tv.%s.add(_jsonIntField(item, r, %s, %s));\n", name, lo, hi)
			}
		}
		g.pf("\t\t\t\t}\n")
		g.pf("\t\t\t\tbreak;\n")
	case f.Type.Kind == ir.TBool:
		g.pf("\t\t\t\tif (val is bool) { v.%s = val; } else { r.kindMismatch++; }\n", name)
		g.pf("\t\t\t\tbreak;\n")
	case f.Type.Kind == ir.TInt:
		lo, hi := "null", "null"
		if f.HasIntRange {
			lo, hi = f.IntMin.String(), f.IntMax.String()
		}
		g.pf("\t\t\t\tv.%s = _jsonIntField(val, r, %s, %s);\n", name, lo, hi)
		g.pf("\t\t\t\tbreak;\n")
	case f.Type.Kind == ir.TString:
		g.pf("\t\t\t\tv.%s = _jsonStringField(val, %d, r);\n", name, f.Type.Size)
		g.pf("\t\t\t\tbreak;\n")
	case f.Type.Kind == ir.TBits:
		hi := "null"
		if f.Type.Width < 64 {
			hi = fmt.Sprintf("%d", uint64(1)<<f.Type.Width-1)
		}
		g.pf("\t\t\t\tv.%s = _jsonIntField(val, r, null, %s);\n", name, hi)
		g.pf("\t\t\t\tbreak;\n")
	case f.Type.Kind == ir.TBytes:
		g.pf("\t\t\t\tif (val is String) { v.%s = utf8.decode(base64Decode(val)); } else { r.kindMismatch++; }\n", name)
		g.pf("\t\t\t\tbreak;\n")
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		g.pf("\t\t\t\tv.%s = _jsonFloatField(val, r);\n", name)
		g.pf("\t\t\t\tbreak;\n")
	case enumRef(f) != nil:
		g.pf("\t\t\t\tv.%s = %sFromName(val, r);\n", name, dartName(enumRef(f).Name))
		g.pf("\t\t\t\tbreak;\n")
	default:
		g.pf("\t\t\t\tr.kindMismatch++;\n")
		g.pf("\t\t\t\tbreak;\n")
	}
}

// emitTextToJson emits a root's top-level text entry point: the body at depth
// zero plus the canonical single trailing newline (§16.1).
func (g *wireGen) emitTextToJson(st *ir.Struct) {
	fn := dartName(st.Name) + "ToJson"
	g.pf("String %s(%s v) {\n", fn, st.Name)
	g.pf("\tfinal out = _JOut();\n")
	g.pf("\t%sToJsonBody(out, v, 0);\n", dartName(st.Name))
	g.pf("\tout.raw('\\n');\n")
	g.pf("\treturn out.toString();\n")
	g.pf("}\n\n")
}

// emitTextFromJson emits a root's top-level text entry point: decode the JSON
// and place every key the body can name into a fresh instance, defaults
// surviving (§16.3).
func (g *wireGen) emitTextFromJson(st *ir.Struct) {
	fn := dartName(st.Name) + "FromJson"
	g.pf("%s %s(String text, [TableTextReport? report]) {\n", st.Name, fn)
	g.pf("\tfinal r = report ?? TableTextReport();\n")
	g.pf("\tfinal v = %s();\n", st.Name)
	g.pf("\tfinal root = jsonDecode(text);\n")
	g.pf("\tif (root is Map<String, dynamic>) {\n")
	g.pf("\t\t%sFromJsonFields(v, root, r);\n", dartName(st.Name))
	g.pf("\t}\n")
	g.pf("\treturn v;\n")
	g.pf("}\n\n")
}
