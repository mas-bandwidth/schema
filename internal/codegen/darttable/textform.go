// THE TEXT FORM (docs/SPEC-TABLES.md §16, §16.7), JSON in and out, for the
// variable-class constructs: a plain `table` root, tables as unbounded list
// elements, pointered tables (*T) participating in §16.7's &node sharing,
// blobs (*string and *bytes), scalar kinds (integers, bits(N), floats,
// bytes(N), string(N), bool), enums, and unbounded arrays of scalars, strings,
// enums, and tables/pointers.
//
// The writer renders one instance as one JSON text with the byte layout the
// Go, C, C++ and C# backends write (docs/SPEC-TABLES.md §16.1): two-space
// indent, `": "` between a key and its value, a comma closing the previous
// line, `{}` and `[]` for the empty forms, and exactly one trailing newline.
// Shared table nodes are defined on their first visit under `&node: N` as the
// first key of their object, and referenced on later visits by `{"&node": N}`
// alone (§16.7).
// The reader walks a decoded JSON value with §16.2's field-kind mapping,
// §16.7's label resolution, and §16.3's refusals: an unknown property is
// skipped and counted, an enum value no variant names reads as None and counts
// unknown, a string past its bound is clamped at a code-point boundary and
// counted, and a byte buffer is decoded from base64.
package darttable

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func isTable(f *ir.Field) bool {
	if ref, ok := f.Type.Ref.(*ir.Struct); ok && ref.IsTable {
		return true
	}
	return false
}

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
		g.emitTextCountUses(st)
		g.emitTextToJsonBody(st)
		g.emitTextFromJsonFields(st)
	}
	for _, st := range g.roots {
		g.emitTextToJson(st)
		g.emitTextFromJson(st)
	}
}

// emitTextRuntime emits the shared JSON text machinery (§16.1, §16.3, §16.7),
// one per unit: the report, the writer sink, the string/number/enum helpers,
// and the graph sharing state for &node traversal.
func (g *wireGen) emitTextRuntime() {
	g.pf(`// ---- the JSON text form (docs/SPEC-TABLES.md §16, §16.7) ----

// TableTextReport is a tolerant text read's report (§16.3, §16.7): the events
// variable-class constructs can move — an unknown property, a kind mismatch,
// a clamp, duplicate keys, and a malformed marker.
final class TableTextReport {
  int unknown = 0;
  int kindMismatch = 0;
  int clamped = 0;
  int duplicate = 0;
  bool malformed = false;
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
// (docs/SPEC-TABLES.md §5, §16.3). Storage value 0 spells "None".
String _jsonEnum(int v, String Function(int) name, String where) {
  final n = name(v);
  if (n.isEmpty) {
    throw StateError('$where: enum value $v names no variant (docs/SPEC-TABLES.md §5, §16.2)');
  }
  return _jsonString(n);
}

// _jsonDouble renders a double at canonical shortest representation (§16.1, §16.2).
// Non-finite floats have no JSON spelling and throw StateError.
String _jsonDouble(double d) {
  if (d.isNaN || d.isInfinite) {
    throw StateError('cannot serialize non-finite double to JSON (docs/SPEC-TABLES.md §16.2)');
  }
  if (d == 0.0) return '0';
  final s = d.toString();
  if (s.endsWith('.0')) {
    return s.substring(0, s.length - 2);
  }
  return s;
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

// _jsonBytesField reads one bytes(N) field (§16.2): base64 decoded and clamped
// to N bytes if longer, counting clamped. Invalid base64 or non-string is kind mismatch.
String _jsonBytesField(dynamic val, int bound, TableTextReport r) {
  if (val is! String) {
    r.kindMismatch++;
    return '';
  }
  try {
    var bytes = base64.decode(val);
    if (bytes.length > bound) {
      bytes = Uint8List.sublistView(bytes, 0, bound);
      r.clamped++;
    }
    return utf8.decode(bytes, allowMalformed: true);
  } catch (_) {
    r.kindMismatch++;
    return '';
  }
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

// _jsonDoubleField reads one float field (§16.2): non-finite is kind mismatch;
// float32 overflow is kind mismatch; declared range clamps and counts.
double _jsonDoubleField(dynamic val, TableTextReport r, bool single, [double? lo, double? hi]) {
  if (val is! num) {
    r.kindMismatch++;
    return 0.0;
  }
  final d = val.toDouble();
  if (d.isNaN || d.isInfinite) {
    r.kindMismatch++;
    return 0.0;
  }
  if (single && (d > 3.4028234663852886e+38 || d < -3.4028234663852886e+38)) {
    r.kindMismatch++;
    return 0.0;
  }
  var v = d;
  if (lo != null && v < lo) { v = lo; r.clamped++; }
  if (hi != null && v > hi) { v = hi; r.clamped++; }
  return v;
}

// _JsonWriteState tracks graph traversal for writing &node sharing (§16.7).
final class _JsonWriteState {
  final Map<Object, int> uses = Map<Object, int>.identity();
  final Map<Object, int> labels = Map<Object, int>.identity();
  int nextLabel = 1;
}

// _writePointer writes a table pointer (*T), either in-place or under &node (§16.7).
void _writePointer<T>(
    _JOut out,
    T? child,
    _JsonWriteState state,
    int depth,
    void Function(_JOut, T, _JsonWriteState, int, [int?]) writeBody) {
  if (depth > 128) {
    throw StateError('depth limit 128 exceeded (docs/SPEC-TABLES.md §16.2, §16.7)');
  }
  if (child == null) {
    out.raw('null');
    return;
  }
  final count = state.uses[child] ?? 0;
  if (count <= 1) {
    writeBody(out, child, state, depth);
    return;
  }
  final label = state.labels[child];
  if (label != null) {
    out.raw('{');
    out.line(depth + 1);
    out.raw('"&node": ');
    out.raw(label.toString());
    out.line(depth);
    out.raw('}');
    return;
  }
  final newLabel = state.nextLabel++;
  state.labels[child] = newLabel;
  writeBody(out, child, state, depth, newLabel);
}

// _JsonLabelEntry tracks one node label during tolerant text reading (§16.7).
final class _JsonLabelEntry {
  Object? value;
  String typeName;
  bool open;
  _JsonLabelEntry(this.value, this.typeName, this.open);
}

// _JsonReadState tracks defined and dropped labels during tolerant text reading (§16.7).
final class _JsonReadState {
  final Map<int, _JsonLabelEntry> labels = <int, _JsonLabelEntry>{};
}

// _dropLabels records any &node definitions inside a dropped or unknown value (§16.7).
void _dropLabels(dynamic val, _JsonReadState state) {
  if (val is Map<String, dynamic>) {
    final nodeVal = val['&node'];
    if (nodeVal is int && nodeVal > 0) {
      if (!state.labels.containsKey(nodeVal)) {
        state.labels[nodeVal] = _JsonLabelEntry(null, '', false);
      }
    }
    for (final v in val.values) {
      _dropLabels(v, state);
    }
  } else if (val is List) {
    for (final v in val) {
      _dropLabels(v, state);
    }
  }
}

// _readPointer reads one table pointer (*T), resolving &node definitions and references (§16.7).
T? _readPointer<T>(
    Map<String, dynamic> val,
    _JsonReadState state,
    TableTextReport r,
    String typeName,
    T Function() create,
    void Function(T, Map<String, dynamic>, _JsonReadState, TableTextReport, [bool]) readFields) {
  final nodeVal = val['&node'];
  if (val.containsKey('&node')) {
    if (val.keys.first != '&node') {
      r.malformed = true;
      return null;
    }
    if (nodeVal is! int || nodeVal <= 0) {
      r.malformed = true;
      return null;
    }
    if (val.length == 1) {
      // Reference: {"&node": N}
      final entry = state.labels[nodeVal];
      if (entry == null || entry.open) {
        r.malformed = true;
        return null;
      }
      if (entry.value == null) {
        return null;
      }
      if (entry.typeName != typeName || entry.value is! T) {
        r.kindMismatch++;
        return null;
      }
      return entry.value as T;
    } else {
      // Definition: {"&node": N, ...fields...}
      if (state.labels.containsKey(nodeVal)) {
        r.malformed = true;
        return null;
      }
      final child = create();
      final entry = _JsonLabelEntry(child, typeName, true);
      state.labels[nodeVal] = entry;
      readFields(child, val, state, r, true);
      entry.open = false;
      return child;
    }
  } else {
    // Tree node in place
    final child = create();
    readFields(child, val, state, r, true);
    return child;
  }
}
`)
}

// emitTextCountUses walks a table's reachable pointers to count occurrences for &node (§16.7).
func (g *wireGen) emitTextCountUses(st *ir.Struct) {
	fn := "_countUses" + dartName(st.Name)
	g.pf("void %s(%s v, Map<Object, int> uses, Set<Object> active) {\n", fn, st.Name)
	for _, f := range st.Fields {
		name := fieldMember(f)
		switch {
		case f.Type.Pointer && isTable(f):
			g.pf("\tif (v.%s != null) {\n", name)
			g.pf("\t\tfinal child = v.%s!;\n", name)
			g.pf("\t\tif (active.contains(child)) {\n")
			g.pf("\t\t\tthrow StateError('cannot serialize graph with cycle to JSON (docs/SPEC-TABLES.md §3.1, §16.7)');\n")
			g.pf("\t\t}\n")
			g.pf("\t\tfinal count = (uses[child] ?? 0) + 1;\n")
			g.pf("\t\tuses[child] = count;\n")
			g.pf("\t\tif (count == 1) {\n")
			g.pf("\t\t\tactive.add(child);\n")
			g.pf("\t\t\t_countUses%s(child, uses, active);\n", dartName(f.Type.Name))
			g.pf("\t\t\tactive.remove(child);\n")
			g.pf("\t\t}\n")
			g.pf("\t}\n")
		case f.IsList() && f.Type.Pointer && isTable(f):
			g.pf("\tfor (final child in v.%s) {\n", name)
			g.pf("\t\tif (child != null) {\n")
			g.pf("\t\t\tif (active.contains(child)) {\n")
			g.pf("\t\t\t\tthrow StateError('cannot serialize graph with cycle to JSON (docs/SPEC-TABLES.md §3.1, §16.7)');\n")
			g.pf("\t\t\t}\n")
			g.pf("\t\t\tfinal count = (uses[child] ?? 0) + 1;\n")
			g.pf("\t\t\tuses[child] = count;\n")
			g.pf("\t\t\tif (count == 1) {\n")
			g.pf("\t\t\t\tactive.add(child);\n")
			g.pf("\t\t\t\t_countUses%s(child, uses, active);\n", dartName(f.Type.Name))
			g.pf("\t\t\t\tactive.remove(child);\n")
			g.pf("\t\t\t}\n")
			g.pf("\t\t}\n")
			g.pf("\t}\n")
		case (f.IsList() || f.Array == ir.ArrayCounted) && isTable(f) && !f.Type.Pointer:
			g.pf("\tfor (final child in v.%s) {\n", name)
			g.pf("\t\t_countUses%s(child, uses, active);\n", dartName(f.Type.Name))
			g.pf("\t}\n")
		case !f.Type.Pointer && isTable(f):
			g.pf("\t_countUses%s(v.%s, uses, active);\n", dartName(f.Type.Name), name)
		}
	}
	g.pf("}\n\n")
}

// emitTextEnum emits one enum's name<->value helpers (§16.2): storage value
// 0 spells "None" on write and reads back as 0 with no unknown count; declared
// variants 1..N spell their declared names; an unknown name reads as 0 and
// counts unknown (§16.3).
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
// declaration order, defaults included (§16.1), with &node definition on
// shared nodes (§16.7).
func (g *wireGen) emitTextToJsonBody(st *ir.Struct) {
	fn := dartName(st.Name) + "ToJsonBody"
	g.pf("void %s(_JOut out, %s v, _JsonWriteState state, int depth, [int? label]) {\n", fn, st.Name)
	if len(st.Fields) == 0 {
		g.pf("\tif (label != null) {\n")
		g.pf("\t\tthrow StateError('%s: shared node with no fields has no JSON definition (§16.7)');\n", st.Name)
		g.pf("\t}\n")
		g.pf("\tout.raw('{}');\n")
		g.pf("}\n\n")
		return
	}
	g.pf("\tout.raw('{');\n")
	g.pf("\tvar first = true;\n")
	g.pf("\tif (label != null) {\n")
	g.pf("\t\tout.line(depth + 1);\n")
	g.pf("\t\tout.raw('\"&node\": ');\n")
	g.pf("\t\tout.raw(label.toString());\n")
	g.pf("\t\tfirst = false;\n")
	g.pf("\t}\n")
	for _, f := range st.Fields {
		g.pf("\tif (!first) { out.raw(','); }\n")
		g.pf("\tfirst = false;\n")
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
	case f.Type.Blob():
		if f.Type.Kind == ir.TBytes {
			// *bytes rides as base64 string or null (§2.5, §16.2)
			g.pf("\tif (v.%s == null) { out.raw('null'); } else { out.raw(_jsonString(base64.encode(v.%s!))); }\n", name, name)
		} else {
			// *string rides as string or null (§2.5, §16.2)
			g.pf("\tif (v.%s == null) { out.raw('null'); } else { out.raw(_jsonString(v.%s!)); }\n", name, name)
		}
	case f.Type.Pointer && isTable(f):
		// *T table pointer: in-place object or &node reference (§16.7)
		g.pf("\t_writePointer<%s>(out, v.%s, state, depth + 1, %sToJsonBody);\n",
			f.Type.Name, name, dartName(f.Type.Name))
	case f.IsList():
		switch {
		case f.Type.Pointer && isTable(f):
			// []*T pointer array (§2.9, §16.7)
			g.pf("\tif (v.%s.isEmpty) {\n", name)
			g.pf("\t\tout.raw('[]');\n")
			g.pf("\t} else {\n")
			g.pf("\t\tout.raw('[');\n")
			g.pf("\t\tvar listFirst = true;\n")
			g.pf("\t\tfor (final e in v.%s) {\n", name)
			g.pf("\t\t\tif (!listFirst) { out.raw(','); }\n")
			g.pf("\t\t\tlistFirst = false;\n")
			g.pf("\t\t\tout.line(depth + 2);\n")
			g.pf("\t\t\t_writePointer<%s>(out, e, state, depth + 2, %sToJsonBody);\n",
				f.Type.Name, dartName(f.Type.Name))
			g.pf("\t\t}\n")
			g.pf("\t\tout.line(depth + 1);\n")
			g.pf("\t\tout.raw(']');\n")
			g.pf("\t}\n")
		case isTable(f) && !f.Type.Pointer:
			// []T table array by value (§2.9, §16.2)
			g.pf("\tif (v.%s.isEmpty) {\n", name)
			g.pf("\t\tout.raw('[]');\n")
			g.pf("\t} else {\n")
			g.pf("\t\tout.raw('[');\n")
			g.pf("\t\tvar listFirst = true;\n")
			g.pf("\t\tfor (final e in v.%s) {\n", name)
			g.pf("\t\t\tif (!listFirst) { out.raw(','); }\n")
			g.pf("\t\t\tlistFirst = false;\n")
			g.pf("\t\t\tout.line(depth + 2);\n")
			g.pf("\t\t\t%sToJsonBody(out, e, state, depth + 2);\n", dartName(f.Type.Name))
			g.pf("\t\t}\n")
			g.pf("\t\tout.line(depth + 1);\n")
			g.pf("\t\tout.raw(']');\n")
			g.pf("\t}\n")
		case enumRef(f) != nil:
			// []enum array of variant names (§2.9, §16.2)
			e := enumRef(f)
			g.pf("\tif (v.%s.isEmpty) {\n", name)
			g.pf("\t\tout.raw('[]');\n")
			g.pf("\t} else {\n")
			g.pf("\t\tout.raw('[');\n")
			g.pf("\t\tvar listFirst = true;\n")
			g.pf("\t\tfor (final e in v.%s) {\n", name)
			g.pf("\t\t\tif (!listFirst) { out.raw(','); }\n")
			g.pf("\t\t\tlistFirst = false;\n")
			g.pf("\t\t\tout.line(depth + 2);\n")
			g.pf("\t\t\tout.raw(_jsonEnum(e, %sName, '%s.%s'));\n", dartName(e.Name), st.Name, f.Name)
			g.pf("\t\t}\n")
			g.pf("\t\tout.line(depth + 1);\n")
			g.pf("\t\tout.raw(']');\n")
			g.pf("\t}\n")
		case f.Type.Kind == ir.TString:
			// []string array (§2.9, §16.2)
			g.pf("\tif (v.%s.isEmpty) {\n", name)
			g.pf("\t\tout.raw('[]');\n")
			g.pf("\t} else {\n")
			g.pf("\t\tout.raw('[');\n")
			g.pf("\t\tvar listFirst = true;\n")
			g.pf("\t\tfor (final e in v.%s) {\n", name)
			g.pf("\t\t\tif (!listFirst) { out.raw(','); }\n")
			g.pf("\t\t\tlistFirst = false;\n")
			g.pf("\t\t\tout.line(depth + 2);\n")
			g.pf("\t\t\tout.raw(_jsonString(e));\n")
			g.pf("\t\t}\n")
			g.pf("\t\tout.line(depth + 1);\n")
			g.pf("\t\tout.raw(']');\n")
			g.pf("\t}\n")
		case f.Type.Kind == ir.TBool:
			// []bool array
			g.pf("\tif (v.%s.isEmpty) {\n", name)
			g.pf("\t\tout.raw('[]');\n")
			g.pf("\t} else {\n")
			g.pf("\t\tout.raw('[');\n")
			g.pf("\t\tvar listFirst = true;\n")
			g.pf("\t\tfor (final e in v.%s) {\n", name)
			g.pf("\t\t\tif (!listFirst) { out.raw(','); }\n")
			g.pf("\t\t\tlistFirst = false;\n")
			g.pf("\t\t\tout.line(depth + 2);\n")
			g.pf("\t\t\tout.raw(e ? 'true' : 'false');\n")
			g.pf("\t\t}\n")
			g.pf("\t\tout.line(depth + 1);\n")
			g.pf("\t\tout.raw(']');\n")
			g.pf("\t}\n")
		case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
			// []float array
			g.pf("\tif (v.%s.isEmpty) {\n", name)
			g.pf("\t\tout.raw('[]');\n")
			g.pf("\t} else {\n")
			g.pf("\t\tout.raw('[');\n")
			g.pf("\t\tvar listFirst = true;\n")
			g.pf("\t\tfor (final e in v.%s) {\n", name)
			g.pf("\t\t\tif (!listFirst) { out.raw(','); }\n")
			g.pf("\t\t\tlistFirst = false;\n")
			g.pf("\t\t\tout.line(depth + 2);\n")
			g.pf("\t\t\tout.raw(_jsonDouble(e));\n")
			g.pf("\t\t}\n")
			g.pf("\t\tout.line(depth + 1);\n")
			g.pf("\t\tout.raw(']');\n")
			g.pf("\t}\n")
		default:
			// []scalar integer/bits array (§2.9, §16.2)
			g.pf("\tif (v.%s.isEmpty) {\n", name)
			g.pf("\t\tout.raw('[]');\n")
			g.pf("\t} else {\n")
			g.pf("\t\tout.raw('[');\n")
			g.pf("\t\tvar listFirst = true;\n")
			g.pf("\t\tfor (final e in v.%s) {\n", name)
			g.pf("\t\t\tif (!listFirst) { out.raw(','); }\n")
			g.pf("\t\t\tlistFirst = false;\n")
			g.pf("\t\t\tout.line(depth + 2);\n")
			g.pf("\t\t\tout.raw(e.toString());\n")
			g.pf("\t\t}\n")
			g.pf("\t\tout.line(depth + 1);\n")
			g.pf("\t\tout.raw(']');\n")
			g.pf("\t}\n")
		}
	case f.Array == ir.ArrayCounted && isTable(f) && !f.Type.Pointer:
		// [..N]T bounded array of tables (§2.1, §16.2)
		g.pf("\tif (v.%s.isEmpty) {\n", name)
		g.pf("\t\tout.raw('[]');\n")
		g.pf("\t} else {\n")
		g.pf("\t\tout.raw('[');\n")
		g.pf("\t\tvar listFirst = true;\n")
		g.pf("\t\tfor (final e in v.%s) {\n", name)
		g.pf("\t\t\tif (!listFirst) { out.raw(','); }\n")
		g.pf("\t\t\tlistFirst = false;\n")
		g.pf("\t\t\tout.line(depth + 2);\n")
		g.pf("\t\t\t%sToJsonBody(out, e, state, depth + 2);\n", dartName(f.Type.Name))
		g.pf("\t\t}\n")
		g.pf("\t\tout.line(depth + 1);\n")
		g.pf("\t\tout.raw(']');\n")
		g.pf("\t}\n")
	case !f.Type.Pointer && isTable(f):
		// Table nested by value
		g.pf("\t%sToJsonBody(out, v.%s, state, depth + 1);\n", dartName(f.Type.Name), name)
	case f.Type.Kind == ir.TBool:
		g.pf("\tout.raw(v.%s ? 'true' : 'false');\n", name)
	case f.Type.Kind == ir.TInt || f.Type.Kind == ir.TBits:
		g.pf("\tout.raw(v.%s.toString());\n", name)
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		g.pf("\tout.raw(_jsonDouble(v.%s));\n", name)
	case f.Type.Kind == ir.TBytes:
		// bytes(N) rides as base64 string (§16.2)
		g.pf("\tout.raw(_jsonString(base64.encode(utf8.encode(v.%s))));\n", name)
	case f.Type.Kind == ir.TString:
		g.pf("\tout.raw(_jsonString(v.%s));\n", name)
	case enumRef(f) != nil:
		g.pf("\tout.raw(_jsonEnum(v.%s, %sName, '%s.%s'));\n", name, dartName(enumRef(f).Name), st.Name, f.Name)
	default:
		g.pf("\t// %s: not a carried construct\n", name)
	}
}

// emitTextFromJsonFields emits one table's tolerant text read body (§16.3, §16.7): a
// named key places its value, an unknown key is skipped and counted, and the
// table's storage defaults survive any key the text does not carry.
func (g *wireGen) emitTextFromJsonFields(st *ir.Struct) {
	fn := dartName(st.Name) + "FromJsonFields"
	g.pf("void %s(%s v, Map<String, dynamic> obj, _JsonReadState state, TableTextReport r, [bool allowLabel = false]) {\n", fn, st.Name)
	g.pf("\tfor (final entry in obj.entries) {\n")
	g.pf("\t\tfinal key = entry.key;\n")
	g.pf("\t\tfinal val = entry.value;\n")
	g.pf("\t\tswitch (key) {\n")
	g.pf("\t\t\tcase '&node':\n")
	g.pf("\t\t\t\tif (!allowLabel) { r.malformed = true; }\n")
	g.pf("\t\t\t\tbreak;\n")
	for _, f := range st.Fields {
		g.emitTextFromJsonField(f)
	}
	g.pf("\t\t\tdefault:\n")
	g.pf("\t\t\t\tif (key.startsWith('&')) {\n")
	g.pf("\t\t\t\t\tr.malformed = true;\n")
	g.pf("\t\t\t\t} else {\n")
	g.pf("\t\t\t\t\t_dropLabels(val, state);\n")
	g.pf("\t\t\t\t\tr.unknown++;\n")
	g.pf("\t\t\t\t}\n")
	g.pf("\t\t\t\tbreak;\n")
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
		if f.Type.Kind == ir.TBytes {
			// *bytes: base64 decode or null (§2.5, §16.2)
			g.pf("\t\t\t\tif (val == null) { v.%s = null; }\n", name)
			g.pf("\t\t\t\telse if (val is String) {\n")
			g.pf("\t\t\t\t\ttry { v.%s = base64.decode(val); } catch (_) { r.kindMismatch++; }\n", name)
			g.pf("\t\t\t\t} else { r.kindMismatch++; }\n")
			g.pf("\t\t\t\tbreak;\n")
		} else {
			// *string: string or null (§2.5, §16.2)
			g.pf("\t\t\t\tif (val == null) { v.%s = null; }\n", name)
			g.pf("\t\t\t\telse if (val is String) { v.%s = val; }\n", name)
			g.pf("\t\t\t\telse { r.kindMismatch++; }\n")
			g.pf("\t\t\t\tbreak;\n")
		}
	case f.Type.Pointer && isTable(f):
		// *T table pointer (§16.7)
		g.pf("\t\t\t\tif (val == null) { v.%s = null; }\n", name)
		g.pf("\t\t\t\telse if (val is Map<String, dynamic>) {\n")
		g.pf("\t\t\t\t\tv.%s = _readPointer<%s>(val, state, r, %s, () => %s(), %sFromJsonFields);\n",
			name, f.Type.Name, strconv.Quote(f.Type.Name), f.Type.Name, dartName(f.Type.Name))
		g.pf("\t\t\t\t} else { r.kindMismatch++; }\n")
		g.pf("\t\t\t\tbreak;\n")
	case f.IsList():
		switch {
		case f.Type.Pointer && isTable(f):
			// []*T pointer array (§2.9, §16.7)
			g.pf("\t\t\t\tif (val is! List) { r.kindMismatch++; break; }\n")
			g.pf("\t\t\t\tv.%s = <%s?>[];\n", name, f.Type.Name)
			g.pf("\t\t\t\tfor (final item in val) {\n")
			g.pf("\t\t\t\t\tif (item == null) {\n")
			g.pf("\t\t\t\t\t\tv.%s.add(null);\n", name)
			g.pf("\t\t\t\t\t} else if (item is Map<String, dynamic>) {\n")
			g.pf("\t\t\t\t\t\tv.%s.add(_readPointer<%s>(item, state, r, %s, () => %s(), %sFromJsonFields));\n",
				name, f.Type.Name, strconv.Quote(f.Type.Name), f.Type.Name, dartName(f.Type.Name))
			g.pf("\t\t\t\t\t} else {\n")
			g.pf("\t\t\t\t\t\tr.kindMismatch++;\n")
			g.pf("\t\t\t\t\t}\n")
			g.pf("\t\t\t\t}\n")
			g.pf("\t\t\t\tbreak;\n")
		case isTable(f) && !f.Type.Pointer:
			// []T table array by value (§2.9, §16.2)
			g.pf("\t\t\t\tif (val is! List) { r.kindMismatch++; break; }\n")
			g.pf("\t\t\t\tv.%s = <%s>[];\n", name, f.Type.Name)
			g.pf("\t\t\t\tfor (final item in val) {\n")
			g.pf("\t\t\t\t\tif (item is Map<String, dynamic>) {\n")
			g.pf("\t\t\t\t\t\tfinal e = %s();\n", f.Type.Name)
			g.pf("\t\t\t\t\t\t%sFromJsonFields(e, item, state, r, false);\n", dartName(f.Type.Name))
			g.pf("\t\t\t\t\t\tv.%s.add(e);\n", name)
			g.pf("\t\t\t\t\t} else {\n")
			g.pf("\t\t\t\t\t\tr.kindMismatch++;\n")
			g.pf("\t\t\t\t\t}\n")
			g.pf("\t\t\t\t}\n")
			g.pf("\t\t\t\tbreak;\n")
		case enumRef(f) != nil:
			// []enum array of variant names (§2.9, §16.2)
			e := enumRef(f)
			g.pf("\t\t\t\tif (val is! List) { r.kindMismatch++; break; }\n")
			g.pf("\t\t\t\tv.%s = <int>[];\n", name)
			g.pf("\t\t\t\tfor (final item in val) {\n")
			g.pf("\t\t\t\t\tv.%s.add(%sFromName(item, r));\n", name, dartName(e.Name))
			g.pf("\t\t\t\t}\n")
			g.pf("\t\t\t\tbreak;\n")
		case f.Type.Kind == ir.TString:
			// []string array (§2.9, §16.2)
			g.pf("\t\t\t\tif (val is! List) { r.kindMismatch++; break; }\n")
			g.pf("\t\t\t\tv.%s = <String>[];\n", name)
			g.pf("\t\t\t\tfor (final item in val) {\n")
			g.pf("\t\t\t\t\tv.%s.add(_jsonStringField(item, %d, r));\n", name, f.Type.Size)
			g.pf("\t\t\t\t}\n")
			g.pf("\t\t\t\tbreak;\n")
		case f.Type.Kind == ir.TBool:
			// []bool array
			g.pf("\t\t\t\tif (val is! List) { r.kindMismatch++; break; }\n")
			g.pf("\t\t\t\tv.%s = <bool>[];\n", name)
			g.pf("\t\t\t\tfor (final item in val) {\n")
			g.pf("\t\t\t\t\tif (item is bool) { v.%s.add(item); } else { r.kindMismatch++; }\n", name)
			g.pf("\t\t\t\t}\n")
			g.pf("\t\t\t\tbreak;\n")
		case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
			// []float array
			lo, hi := floatBounds(f)
			single := f.Type.Kind == ir.TFloat32
			g.pf("\t\t\t\tif (val is! List) { r.kindMismatch++; break; }\n")
			g.pf("\t\t\t\tv.%s = <double>[];\n", name)
			g.pf("\t\t\t\tfor (final item in val) {\n")
			g.pf("\t\t\t\t\tv.%s.add(_jsonDoubleField(item, r, %v, %s, %s));\n", name, single, lo, hi)
			g.pf("\t\t\t\t}\n")
			g.pf("\t\t\t\tbreak;\n")
		default:
			// []scalar integer/bits array (§2.9, §16.2)
			lo, hi := intBounds(f)
			g.pf("\t\t\t\tif (val is! List) { r.kindMismatch++; break; }\n")
			g.pf("\t\t\t\tv.%s = <int>[];\n", name)
			g.pf("\t\t\t\tfor (final item in val) {\n")
			g.pf("\t\t\t\t\tv.%s.add(_jsonIntField(item, r, %s, %s));\n", name, lo, hi)
			g.pf("\t\t\t\t}\n")
			g.pf("\t\t\t\tbreak;\n")
		}
	case f.Array == ir.ArrayCounted && isTable(f) && !f.Type.Pointer:
		// [..N]T bounded array of tables (§2.1, §16.2)
		g.pf("\t\t\t\tif (val is! List) { r.kindMismatch++; break; }\n")
		g.pf("\t\t\t\tv.%s = <%s>[];\n", name, f.Type.Name)
		g.pf("\t\t\t\tfor (final item in val) {\n")
		g.pf("\t\t\t\t\tif (v.%s.length >= %d) { _dropLabels(item, state); r.clamped++; break; }\n", name, f.ArrayBound)
		g.pf("\t\t\t\t\tif (item is Map<String, dynamic>) {\n")
		g.pf("\t\t\t\t\t\tfinal e = %s();\n", f.Type.Name)
		g.pf("\t\t\t\t\t\t%sFromJsonFields(e, item, state, r, false);\n", dartName(f.Type.Name))
		g.pf("\t\t\t\t\t\tv.%s.add(e);\n", name)
		g.pf("\t\t\t\t\t} else {\n")
		g.pf("\t\t\t\t\t\tr.kindMismatch++;\n")
		g.pf("\t\t\t\t\t}\n")
		g.pf("\t\t\t\t}\n")
		g.pf("\t\t\t\tbreak;\n")
	case !f.Type.Pointer && isTable(f):
		// Table nested by value
		g.pf("\t\t\t\tif (val is Map<String, dynamic>) {\n")
		g.pf("\t\t\t\t\t%sFromJsonFields(v.%s, val, state, r, false);\n", dartName(f.Type.Name), name)
		g.pf("\t\t\t\t} else { r.kindMismatch++; }\n")
		g.pf("\t\t\t\tbreak;\n")
	case f.Type.Kind == ir.TBool:
		g.pf("\t\t\t\tif (val is bool) { v.%s = val; } else { r.kindMismatch++; }\n", name)
		g.pf("\t\t\t\tbreak;\n")
	case f.Type.Kind == ir.TInt, f.Type.Kind == ir.TBits:
		lo, hi := intBounds(f)
		g.pf("\t\t\t\tv.%s = _jsonIntField(val, r, %s, %s);\n", name, lo, hi)
		g.pf("\t\t\t\tbreak;\n")
	case f.Type.Kind == ir.TFloat32, f.Type.Kind == ir.TFloat64:
		lo, hi := floatBounds(f)
		single := f.Type.Kind == ir.TFloat32
		g.pf("\t\t\t\tv.%s = _jsonDoubleField(val, r, %v, %s, %s);\n", name, single, lo, hi)
		g.pf("\t\t\t\tbreak;\n")
	case f.Type.Kind == ir.TBytes:
		// bytes(N) rides as base64 string (§16.2)
		g.pf("\t\t\t\tv.%s = _jsonBytesField(val, %d, r);\n", name, f.Type.Size)
		g.pf("\t\t\t\tbreak;\n")
	case f.Type.Kind == ir.TString:
		g.pf("\t\t\t\tv.%s = _jsonStringField(val, %d, r);\n", name, f.Type.Size)
		g.pf("\t\t\t\tbreak;\n")
	case enumRef(f) != nil:
		g.pf("\t\t\t\tv.%s = %sFromName(val, r);\n", name, dartName(enumRef(f).Name))
		g.pf("\t\t\t\tbreak;\n")
	default:
		g.pf("\t\t\t\tr.kindMismatch++;\n")
		g.pf("\t\t\t\tbreak;\n")
	}
}

func floatBounds(f *ir.Field) (string, string) {
	if f.HasFloatRange {
		return fmt.Sprintf("%g", f.FloatMin), fmt.Sprintf("%g", f.FloatMax)
	}
	return "null", "null"
}

func intBounds(f *ir.Field) (string, string) {
	if f.HasIntRange {
		return f.IntMin.String(), f.IntMax.String()
	}
	switch f.Type.Kind {
	case ir.TBits:
		if f.Type.Bits < 63 {
			return "0", fmt.Sprintf("%d", (uint64(1)<<f.Type.Bits)-1)
		}
		return "0", "null"
	case ir.TInt:
		switch f.Type.Width {
		case 8:
			if f.Type.Signed {
				return "-128", "127"
			}
			return "0", "255"
		case 16:
			if f.Type.Signed {
				return "-32768", "32767"
			}
			return "0", "65535"
		case 32:
			if !f.Type.Signed {
				return "0", "4294967295"
			}
			return "null", "null"
		case 64:
			if !f.Type.Signed {
				return "0", "null"
			}
		}
	}
	return "null", "null"
}

// emitTextToJson emits a root's top-level text entry point: the body at depth
// zero plus the canonical single trailing newline (§16.1).
func (g *wireGen) emitTextToJson(st *ir.Struct) {
	fn := dartName(st.Name) + "ToJson"
	g.pf("String %s(%s v) {\n", fn, st.Name)
	g.pf("\tfinal state = _JsonWriteState();\n")
	g.pf("\tfinal active = Set<Object>.identity();\n")
	g.pf("\tactive.add(v);\n")
	g.pf("\t_countUses%s(v, state.uses, active);\n", dartName(st.Name))
	g.pf("\tfinal out = _JOut();\n")
	g.pf("\t%sToJsonBody(out, v, state, 0);\n", dartName(st.Name))
	g.pf("\tout.raw('\\n');\n")
	g.pf("\treturn out.toString();\n")
	g.pf("}\n\n")
}

// emitTextFromJson emits a root's top-level text entry point: decode the JSON
// and place every key the body can name into a fresh instance, defaults
// surviving (§16.3, §16.7).
func (g *wireGen) emitTextFromJson(st *ir.Struct) {
	fn := dartName(st.Name) + "FromJson"
	g.pf("%s %s(String text, [TableTextReport? report]) {\n", st.Name, fn)
	g.pf("\tfinal r = report ?? TableTextReport();\n")
	g.pf("\tfinal v = %s();\n", st.Name)
	g.pf("\tdynamic root;\n")
	g.pf("\ttry {\n")
	g.pf("\t\troot = jsonDecode(text);\n")
	g.pf("\t} catch (_) {\n")
	g.pf("\t\tr.malformed = true;\n")
	g.pf("\t\treturn v;\n")
	g.pf("\t}\n")
	g.pf("\tif (root is Map<String, dynamic>) {\n")
	g.pf("\t\tfinal state = _JsonReadState();\n")
	g.pf("\t\t%sFromJsonFields(v, root, state, r, false);\n", dartName(st.Name))
	g.pf("\t} else if (root is Map) {\n")
	g.pf("\t\tfinal state = _JsonReadState();\n")
	g.pf("\t\t%sFromJsonFields(v, root.cast<String, dynamic>(), state, r, false);\n", dartName(st.Name))
	g.pf("\t} else {\n")
	g.pf("\t\tr.malformed = true;\n")
	g.pf("\t}\n")
	g.pf("\treturn v;\n")
	g.pf("}\n\n")
}
