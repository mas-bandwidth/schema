// THE TEXT FORM (docs/SPEC-TABLES.md §16), JSON in and out, for the variable
// class this card carries: scalars, bits, bytes(N), enums, string(N), *string
// and *bytes, pointers, by-value tables, []T and []*T and [..N]T (§16.7).
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
// Maps, optional ?T, enum-keyed arrays and union arms stay refused by name
// in checkUnsupportedConstructs (§11). A shared node is "&node" (§16.7).
package darttable

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// jsonUnsigned is the fifth argument of _jsonIntField (§16.2, §4): an unsigned
// int or a bits field compares its bounds as 64-bit patterns, because a Dart
// int literal above 2^63-1 is that pattern's signed mint.
func jsonUnsigned(f *ir.Field) string {
	if f.Type.Kind == ir.TBits || (f.Type.Kind == ir.TInt && !f.Type.Signed) {
		return "true"
	}
	return "false"
}

// jsonIntBounds carries the declared storage width into exact JSON placement.
func jsonIntBounds(f *ir.Field) (string, string) {
	if f.HasIntRange {
		return dartBoundLiteral(f.IntMin), dartBoundLiteral(f.IntMax)
	}
	width := f.Type.Width
	if width == 0 {
		return "null", "null"
	}
	if f.Type.Kind == ir.TInt && f.Type.Signed {
		if width >= 64 {
			return "null", "null"
		}
		return fmt.Sprint(-(int64(1) << (width - 1))), fmt.Sprint((int64(1) << (width - 1)) - 1)
	}
	if width >= 64 {
		return "0", "-1"
	}
	return "0", fmt.Sprint((uint64(1) << width) - 1)
}

func jsonIntText(f *ir.Field, expr string) string {
	if jsonUnsigned(f) == "true" && f.Type.Width == 64 {
		return "(BigInt.from(" + expr + ").toUnsigned(64)).toString()"
	}
	return expr + ".toString()"
}

func jsonFloatBounds(f *ir.Field) (string, string) {
	if f.HasFloatRange {
		return fmtFloat(f.FMin), fmtFloat(f.FMax)
	}
	return "null", "null"
}

// textEnums is every enum a closure field names, in declaration-name order:
// the text form spells an enum by variant NAME (§16.2), so each such enum owes
// a name<->value helper emitted once beside the tables.
func (g *wireGen) textEnums() []*ir.Enum {
	seen := map[string]bool{}
	var out []*ir.Enum
	for _, st := range g.closure {
		for _, f := range st.Fields {
			if e := f.KeyEnumRef; e != nil && !seen[e.Name] {
				seen[e.Name] = true
				out = append(out, e)
			}
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
		g.emitTextCount(st)
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
` + textNumberMembers + `
  bool malformed = false;
  final Expando<List<MapEntry<String,dynamic>>> _entries = Expando();
  Iterable<MapEntry<dynamic,dynamic>> entries(Map obj) => _entries[obj] ?? obj.entries;

  // Keep object properties in source order, including repeated identical keys.
  // Core jsonDecode supplies primitive validation without losing map resets.
  dynamic parse(String text) {
    var pos=0;
    void space(){while(pos<text.length && (text.codeUnitAt(pos)==32 || text.codeUnitAt(pos)==10 || text.codeUnitAt(pos)==13 || text.codeUnitAt(pos)==9)){pos++;}}
    Never bad()=>throw FormatException('invalid JSON',text,pos);
    String string(){
      final start=pos++;var escaped=false;
      while(pos<text.length){final c=text.codeUnitAt(pos++);if(escaped){escaped=false;continue;}if(c==92){escaped=true;continue;}if(c==34)return jsonDecode(text.substring(start,pos)) as String;}
      return bad();
    }
    late dynamic Function() value;
    value=(){
      space();if(pos>=text.length)bad();
      final c=text.codeUnitAt(pos);
      if(c==34)return string();
      if(c==123){
        pos++;space();final out=<String,dynamic>{};final pairs=<MapEntry<String,dynamic>>[];_entries[out]=pairs;
        if(pos<text.length&&text.codeUnitAt(pos)==125){pos++;return out;}
        while(true){
          space();if(pos>=text.length||text.codeUnitAt(pos)!=34)bad();final key=string();space();if(pos>=text.length||text.codeUnitAt(pos++)!=58)bad();
          final item=value();out[key]=item;pairs.add(MapEntry(key,item));space();if(pos>=text.length)bad();final end=text.codeUnitAt(pos++);if(end==125)return out;if(end!=44)bad();
        }
      }
      if(c==91){
        pos++;space();final out=<dynamic>[];if(pos<text.length&&text.codeUnitAt(pos)==93){pos++;return out;}
        while(true){out.add(value());space();if(pos>=text.length)bad();final end=text.codeUnitAt(pos++);if(end==93)return out;if(end!=44)bad();}
      }
      final start=pos;while(pos<text.length&&!<int>[32,9,10,13,44,93,125].contains(text.codeUnitAt(pos))){pos++;}
      if(pos==start)bad();final token=text.substring(start,pos);
      if(token=='null'||token=='true'||token=='false')return jsonDecode(token);
      return TableTextReport.number(token);
    };
    final out=value();space();if(pos!=text.length)bad();return out;
  }
  int duplicate = 0;
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
int _jsonIntField(dynamic val, TableTextReport r, int? lo, int? hi, [bool unsigned = false,int fallback=0]) =>
  TableTextReport.integerField(val,r,lo,hi,unsigned,fallback);

// _jsonFloat spells a float at the shortest precision that reads back the same
// value (§16.2), the same '%%g' spelling the Go leg writes. single is a float32 field.
// A non-finite float has no JSON spelling and the writer refuses it.
String _jsonFloat(double v, bool single) {
  if (v.isNaN || v.isInfinite) {
    throw StateError('float $v has no JSON spelling (docs/SPEC-TABLES.md §16.2)');
  }
  final low = single ? 6 : 15;
  final high = single ? 9 : 17;
  for (var digits = low; ; digits++) {
    final text = _formatG(v, digits);
    if (digits >= high) return text;
    final back = double.tryParse(text);
    if (back == null) continue;
    if (single) {
      if (_f32Bits(back) == _f32Bits(v)) return text;
    } else if (back == v) {
      return text;
    }
  }
}

String _formatG(double value, int prec) {
  if (prec < 1) prec = 1;
  final sci = value.toStringAsExponential(prec - 1);
  final at = sci.indexOf('e');
  final exp = int.parse(sci.substring(at + 1));
  if (exp < -4 || exp >= prec) {
    return _trimZeros(sci.substring(0, at)) + 'e' + _expDigits(exp);
  }
  final frac = prec - 1 - exp;
  final plain = value.toStringAsFixed(frac < 0 ? 0 : frac);
  return _trimZeros(plain);
}

String _trimZeros(String s) {
  if (!s.contains('.')) return s;
  while (s.endsWith('0')) {
    s = s.substring(0, s.length - 1);
  }
  if (s.endsWith('.')) s = s.substring(0, s.length - 1);
  return s;
}

String _expDigits(int exp) {
  var sign = '+';
  if (exp < 0) {
    sign = '-';
    exp = -exp;
  }
  if (exp < 10) return sign + '0' + exp.toString();
  return sign + exp.toString();
}

// _Share is one text's node identity (§16.7): how many slots name each node,
// the label a shared node took at its first write, and the object a label
// defined on the way back in.
final class _Share {
  final Map<Object, int> count = <Object, int>{};
  final Map<Object, int> label = <Object, int>{};
  final Map<int, Object> defined = <int, Object>{};
  int next = 0;

  void see(Object obj) {
    count[obj] = (count[obj] ?? 0) + 1;
  }

  int define(Object obj) {
    next++;
    label[obj] = next;
    return next;
  }
}

// _writePtr writes one pointer slot (§16.7): null, a node named once in place,
// a shared node defined at its first write with "&node" as the first key, and
// a reference as "&node" alone after that.
void _writePtr(_JOut out, Object? node, int depth, _Share sh, void Function(int? label) body) {
  if (node == null) {
    out.raw('null');
    return;
  }
  if ((sh.count[node] ?? 0) <= 1) {
    body(null);
    return;
  }
  final existing = sh.label[node];
  if (existing != null) {
    out.raw('{');
    out.line(depth + 1);
    out.raw('"&node": ');
    out.raw(existing.toString());
    out.line(depth);
    out.raw('}');
    return;
  }
  body(sh.define(node));
}

Object? _readPtr(dynamic val, _Share sh, TableTextReport r, Object Function() make, void Function(Object obj, Map<String, dynamic> m) fill) {
  if (val == null) return null;
  if (val is! Map) {
    r.kindMismatch++;
    return null;
  }
  final obj = val is Map<String,dynamic> ? val : Map<String, dynamic>.from(val);
  if (obj.containsKey('&node')) {
    final raw = obj['&node'];
    if (raw is! int || raw < 1) {
      r.kindMismatch++;
      return null;
    }
    if (obj.length == 1) return sh.defined[raw];
    final have = sh.defined[raw];
    if (have != null) return have;
    final made = make();
    sh.defined[raw] = made;
    fill(made, obj);
    return made;
  }
  final made = make();
  fill(made, obj);
  return made;
}

// _jsonFloatField reads one float field (§16.2): a number, placed; anything
// else is a kind mismatch.
double _jsonFloatField(dynamic val, TableTextReport r,[bool single=false,double fallback=0.0,double? lo,double? hi]) {
  var value=TableTextReport.floatValue(val);
  if(value==null||!value.isFinite){r.kindMismatch++;return fallback;}
  if(single){value=_f32FromBits(_f32Bits(value));if(!value.isFinite){r.kindMismatch++;return fallback;}}
  var moved=false;
  if(lo!=null&&value<lo){value=lo;moved=true;}
  if(hi!=null&&value>hi){value=hi;moved=true;}
  if(moved)r.clamped++;
  return value;
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
	g.pf("void %s(_JOut out, %s v, int depth, _Share sh, [int? label]) {\n", fn, st.Name)
	if len(st.Fields) == 0 {
		g.pf("\tif (label == null) { out.raw('{}'); return; }\n")
	}
	g.pf("\tout.raw('{');\n")
	g.pf("\tvar comma = false;\n")
	g.pf("\tif (label != null) {\n")
	g.pf("\t\tout.line(depth + 1);\n")
	g.pf("\t\tout.raw('\"&node\": ');\n")
	g.pf("\t\tout.raw(label.toString());\n")
	g.pf("\t\tcomma = true;\n")
	g.pf("\t}\n")
	for _, f := range st.Fields {
		close := g.optionalWalk(f, "\t")
		g.pf("\tif (comma) out.raw(',');\n")
		g.pf("\tcomma = true;\n")
		g.emitTextToJsonField(st, f)
		close()
	}
	g.pf("\tif(!comma){out.raw('}');return;}\n")
	g.pf("\tout.line(depth);\n")
	g.pf("\tout.raw('}');\n")
	g.pf("}\n\n")
}

func (g *wireGen) emitTextToJsonField(st *ir.Struct, f *ir.Field) {
	key := ir.TableFieldJsonKey(f)
	g.pf("\tout.line(depth + 1);\n")
	g.pf("\tout.raw(_jsonString(%s));\n", strconv.Quote(key))
	g.pf("\tout.raw(': ');\n")
	g.emitTextToJsonValue(st, f)
}

func (g *wireGen) emitTextToJsonValue(st *ir.Struct, f *ir.Field) {
	name := fieldMember(f)
	if f.Array == ir.ArrayFixed && f.KeyEnum == "" {
		g.pf("if(v.%s.length!=%d)throw StateError('invalid_value');\n", name, f.ArrayBound)
	}
	switch {
	case f.KeyEnum != "":
		g.emitTextKeyedValue(st, f)
	case f.IsMap():
		g.emitTextMapValue(st, f)
	case f.Type.Blob(): // *string rides as a string or null; *bytes as base64 or null (§2.5, §16.2)
		if f.Type.Kind == ir.TString {
			g.pf("\tif (v.%s == null) { out.raw('null'); } else { out.raw(_jsonString(v.%s!)); }\n", name, name)
		} else {
			g.pf("\tif (v.%s == null) { out.raw('null'); } else { out.raw(_jsonString(base64Encode(v.%s!))); }\n", name, name)
		}
	case isPointerTable(f):
		ref := tableRef(f)
		g.pf("\t_writePtr(out, v.%s, depth + 1, sh, (label) {\n", name)
		g.pf("\t\t%sToJsonBody(out, v.%s!, depth + 1, sh, label);\n", dartName(ref.Name), name)
		g.pf("\t});\n")
	case isByValueTable(f):
		ref := tableRef(f)
		g.pf("\t%sToJsonBody(out, v.%s, depth + 1, sh);\n", dartName(ref.Name), name)
	case wirePositionalArray(f): // []T and [..N]T (§2.9, §16.2, §16.7)
		switch listElementKind(f) {
		case wireKindTable:
			g.pf("\tif (v.%s.isEmpty) {\n", name)
			g.pf("\t\tout.raw('[]');\n")
			g.pf("\t} else {\n")
			g.pf("\t\tout.raw('[');\n")
			g.pf("\t\tvar first = true;\n")
			g.pf("\t\tfor (final e in v.%s) {\n", name)
			g.pf("\t\t\tif (!first) { out.raw(','); }\n")
			g.pf("\t\t\tfirst = false;\n")
			g.pf("\t\t\tout.line(depth + 2);\n")
			g.pf("\t\t\t%sToJsonBody(out, e, depth + 2, sh);\n", dartName(f.Type.Name))
			g.pf("\t\t}\n")
			g.pf("\t\tout.line(depth + 1);\n")
			g.pf("\t\tout.raw(']');\n")
			g.pf("\t}\n")
		case wireKindPtr:
			ref := tableRef(f)
			g.pf("\tif (v.%s.isEmpty) {\n", name)
			g.pf("\t\tout.raw('[]');\n")
			g.pf("\t} else {\n")
			g.pf("\t\tout.raw('[');\n")
			g.pf("\t\tvar first = true;\n")
			g.pf("\t\tfor (final e in v.%s) {\n", name)
			g.pf("\t\t\tif (!first) { out.raw(','); }\n")
			g.pf("\t\t\tfirst = false;\n")
			g.pf("\t\t\tout.line(depth + 2);\n")
			g.pf("\t\t\t_writePtr(out, e, depth + 2, sh, (label) {\n")
			g.pf("\t\t\t\t%sToJsonBody(out, e!, depth + 2, sh, label);\n", dartName(ref.Name))
			g.pf("\t\t\t});\n")
			g.pf("\t\t}\n")
			g.pf("\t\tout.line(depth + 1);\n")
			g.pf("\t\tout.raw(']');\n")
			g.pf("\t}\n")
		default:
			g.pf("\tif (v.%s.isEmpty) {\n", name)
			g.pf("\t\tout.raw('[]');\n")
			g.pf("\t} else {\n")
			g.pf("\t\tout.raw('[');\n")
			g.pf("\t\tvar first = true;\n")
			g.pf("\t\tfor (final e in v.%s) {\n", name)
			g.pf("\t\t\tif (!first) { out.raw(','); }\n")
			g.pf("\t\t\tfirst = false;\n")
			g.pf("\t\t\tout.line(depth + 2);\n")
			switch listElementKind(f) {
			case wireKindBool:
				g.pf("\t\t\tout.raw(e ? 'true' : 'false');\n")
			case wireKindF32:
				g.pf("\t\t\tout.raw(_jsonFloat(e, true));\n")
			case wireKindF64:
				g.pf("\t\t\tout.raw(_jsonFloat(e, false));\n")
			case wireKindString:
				g.pf("\t\t\tout.raw(_jsonString(e));\n")
			case wireKindEnum:
				g.pf("\t\t\tout.raw(_jsonEnum(e, %sName, '%s.%s'));\n", dartName(enumRef(f).Name), st.Name, f.Name)
			default:
				g.pf("\t\t\tout.raw(%s);\n", jsonIntText(f, "e"))
			}
			g.pf("\t\t}\n")
			g.pf("\t\tout.line(depth + 1);\n")
			g.pf("\t\tout.raw(']');\n")
			g.pf("\t}\n")
		}
	case f.Type.Kind == ir.TBool:
		g.pf("\tout.raw(v.%s ? 'true' : 'false');\n", name)
	case f.Type.Kind == ir.TInt, f.Type.Kind == ir.TBits:
		g.pf("\tout.raw(%s);\n", jsonIntText(f, "v."+name))
	case f.Type.Kind == ir.TString:
		g.pf("\tout.raw(_jsonString(v.%s));\n", name)
	case f.Type.Kind == ir.TBytes: // bytes(N) rides as base64 in the text (§16.7)
		g.pf("\tout.raw(_jsonString(base64Encode(utf8.encode(v.%s))));\n", name)
	case f.Type.Kind == ir.TFloat32, f.Type.Kind == ir.TFloat64:
		single := "false"
		if f.Type.Kind == ir.TFloat32 {
			single = "true"
		}
		g.pf("\tout.raw(_jsonFloat(v.%s, %s));\n", name, single)
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
	g.pf("void %s(%s v, Map<String, dynamic> obj, TableTextReport r, _Share sh) {\n", fn, st.Name)
	g.pf("\tfor (final entry in obj.entries) {\n")
	g.pf("\t\tif(r.malformed)return;\n\t\tfinal key = entry.key;\n")
	g.pf("\t\tfinal val = entry.value;\n")
	g.pf("\t\tswitch (key) {\n")
	g.pf("\t\t\tcase '&node':\n\t\t\t\tbreak;\n")
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
	if f.Type.Optional {
		g.pf("if(val==null){v.%sPresent=false;break;}v.%sPresent=true;\n", name, name)
	}
	switch {
	case f.KeyEnum != "":
		g.emitTextReadKeyed(f)
	case f.IsMap():
		g.emitTextReadMap(f)
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
	case isPointerTable(f):
		ref := tableRef(f)
		g.pf("\t\t\t\tv.%s = _readPtr(val, sh, r, () => %s(), (obj, m) { %sFromJsonFields(obj as %s, m, r, sh); }) as %s?;\n", name, ref.Name, dartName(ref.Name), ref.Name, ref.Name)
		g.pf("\t\t\t\tbreak;\n")
	case isByValueTable(f):
		ref := tableRef(f)
		g.pf("\t\t\t\tif (val is Map<String, dynamic>) { %sFromJsonFields(v.%s, val, r, sh); }\n", dartName(ref.Name), name)
		g.pf("\t\t\t\telse if (val is Map) { %sFromJsonFields(v.%s, Map<String, dynamic>.from(val), r, sh); }\n", dartName(ref.Name), name)
		g.pf("\t\t\t\telse { r.kindMismatch++; }\n")
		g.pf("\t\t\t\tbreak;\n")
	case wirePositionalArray(f):
		g.pf("\t\t\t\tif (val is! List) { r.kindMismatch++; break; }\n")
		et := dartListElemType(f)
		g.pf("\t\t\t\tv.%s = <%s>[];\n", name, et)
		if f.Array == ir.ArrayCounted || f.Array == ir.ArrayFixed {
			g.pf("if(val.length>%d)r.clamped++;\n", f.ArrayBound)
			g.pf("for(final item in val.take(%d)){\n", f.ArrayBound)
		} else {
			g.pf("\t\t\t\tfor (final item in val) {\n")
		}
		switch listElementKind(f) {
		case wireKindTable:
			g.pf("\t\t\t\t\tif (item is Map<String, dynamic>) {\n")
			g.pf("\t\t\t\t\t\tfinal e = %s();\n", f.Type.Name)
			g.pf("\t\t\t\t\t\t%sFromJsonFields(e, item, r, sh);\n", dartName(f.Type.Name))
			g.pf("\t\t\t\t\t\tv.%s.add(e);\n", name)
			g.pf("\t\t\t\t\t} else if (item is Map) {\n")
			g.pf("\t\t\t\t\t\tfinal e = %s();\n", f.Type.Name)
			g.pf("\t\t\t\t\t\t%sFromJsonFields(e, Map<String, dynamic>.from(item), r, sh);\n", dartName(f.Type.Name))
			g.pf("\t\t\t\t\t\tv.%s.add(e);\n", name)
			g.pf("\t\t\t\t\t} else {\n")
			g.pf("\t\t\t\t\t\tr.kindMismatch++;\n")
			g.pf("\t\t\t\t\t}\n")
		case wireKindPtr:
			ref := tableRef(f)
			g.pf("\t\t\t\t\tv.%s.add(_readPtr(item, sh, r, () => %s(), (obj, m) { %sFromJsonFields(obj as %s, m, r, sh); }) as %s?);\n", name, ref.Name, dartName(ref.Name), ref.Name, ref.Name)
		default:
			switch listElementKind(f) {
			case wireKindBool:
				g.pf("\t\t\t\t\tif (item is bool) { v.%s.add(item); } else { r.kindMismatch++; }\n", name)
			case wireKindF32, wireKindF64:
				lo, hi := jsonFloatBounds(f)
				g.pf("\t\t\t\t\tv.%s.add(_jsonFloatField(item,r,%v,%s,%s,%s));\n", name, f.Type.Kind == ir.TFloat32, g.floatDefault(f), lo, hi)
			case wireKindEnum:
				g.pf("\t\t\t\t\tv.%s.add(%sFromName(item, r));\n", name, dartName(enumRef(f).Name))
			case wireKindString:
				g.pf("\t\t\t\t\tif (item is String) { v.%s.add(item); } else { r.kindMismatch++; }\n", name)
			default:
				lo, hi := jsonIntBounds(f)
				g.pf("\t\t\t\t\tv.%s.add(_jsonIntField(item,r,%s,%s,%s,%s));\n", name, lo, hi, jsonUnsigned(f), g.wireElementDefault(f))
			}
		}
		g.pf("\t\t\t\t}\n")
		if f.Array == ir.ArrayFixed {
			g.pf("while(v.%s.length<%d){v.%s.add(%s); }\n", name, f.ArrayBound, name, g.wireElementDefault(f))
		}
		g.pf("\t\t\t\tbreak;\n")
	case f.Type.Kind == ir.TBool:
		g.pf("\t\t\t\tif (val is bool) { v.%s = val; } else { r.kindMismatch++; }\n", name)
		g.pf("\t\t\t\tbreak;\n")
	case f.Type.Kind == ir.TInt:
		lo, hi := jsonIntBounds(f)
		g.pf("\t\t\t\tv.%s=_jsonIntField(val,r,%s,%s,%s,v.%s);\n", name, lo, hi, jsonUnsigned(f), name)
		g.pf("\t\t\t\tbreak;\n")
	case f.Type.Kind == ir.TString:
		g.pf("\t\t\t\tv.%s = _jsonStringField(val, %d, r);\n", name, f.Type.Size)
		g.pf("\t\t\t\tbreak;\n")
	case f.Type.Kind == ir.TBits:
		lo, hi := jsonIntBounds(f)
		g.pf("\t\t\t\tv.%s=_jsonIntField(val,r,%s,%s,true,v.%s);\n", name, lo, hi, name)
		g.pf("\t\t\t\tbreak;\n")
	case f.Type.Kind == ir.TBytes:
		g.pf("\t\t\t\tif (val is String) { v.%s = utf8.decode(base64Decode(val)); } else { r.kindMismatch++; }\n", name)
		g.pf("\t\t\t\tbreak;\n")
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		lo, hi := jsonFloatBounds(f)
		g.pf("\t\t\t\tv.%s=_jsonFloatField(val,r,%v,v.%s,%s,%s);\n", name, f.Type.Kind == ir.TFloat32, name, lo, hi)
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
func (g *wireGen) emitTextCount(st *ir.Struct) {
	g.pf("void %sCount(_Share sh, %s v) {\n", dartName(st.Name), st.Name)
	for _, f := range st.Fields {
		g.emitTextCountField(f, "\t")
	}
	g.pf("}\n\n")
}

func (g *wireGen) emitTextCountField(f *ir.Field, ind string) {
	defer g.optionalWalk(f, ind)()
	expr := "v." + fieldMember(f)
	switch {
	case f.KeyEnum != "":
		if ref := tableRef(f); ref != nil {
			g.pf("%sfor(final slot in %s.entries){%sCount(sh,slot.$2); }\n", ind, expr, dartName(ref.Name))
		}
	case f.IsMap():
		g.pf("%sfor(final entry in %s.ordered(%s)){%sCount(sh,entry); }\n", ind, f.MapEntry.Name, expr, dartName(f.MapEntry.Name))
	case isPointerTable(f):
		ref := tableRef(f)
		g.pf("%sif (%s != null) {\n", ind, expr)
		g.pf("%s\tfinal node = %s!;\n", ind, expr)
		g.pf("%s\tfinal first = (sh.count[node] ?? 0) == 0;\n", ind)
		g.pf("%s\tsh.see(node);\n", ind)
		g.pf("%s\tif (first) %sCount(sh, node);\n", ind, dartName(ref.Name))
		g.pf("%s}\n", ind)
	case isByValueTable(f):
		ref := tableRef(f)
		g.pf("%s%sCount(sh, %s);\n", ind, dartName(ref.Name), expr)
	case wirePositionalArray(f):
		ref := tableRef(f)
		if ref == nil {
			return
		}
		if f.Type.Pointer {
			g.pf("%sfor (final e in %s) {\n", ind, expr)
			g.pf("%s\tif (e != null) {\n", ind)
			g.pf("%s\t\tfinal node = e;\n", ind)
			g.pf("%s\t\tfinal first = (sh.count[node] ?? 0) == 0;\n", ind)
			g.pf("%s\t\tsh.see(node);\n", ind)
			g.pf("%s\t\tif (first) %sCount(sh, node);\n", ind, dartName(ref.Name))
			g.pf("%s\t}\n", ind)
			g.pf("%s}\n", ind)
		} else {
			g.pf("%sfor (final e in %s) { %sCount(sh, e); }\n", ind, expr, dartName(ref.Name))
		}
	}
}

func (g *wireGen) emitTextToJson(st *ir.Struct) {
	fn := dartName(st.Name) + "ToJson"
	g.pf("String %s(%s v) {\n", fn, st.Name)
	g.pf("\tfinal sh = _Share();\n")
	g.pf("\t%sCount(sh, v);\n", dartName(st.Name))
	g.pf("\tfinal out = _JOut();\n")
	g.pf("\t%sToJsonBody(out, v, 0, sh);\n", dartName(st.Name))
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
	g.pf("\tfinal root = r.parse(text);\n")
	g.pf("\tif (root is Map<String, dynamic>) {\n")
	g.pf("\t\t%sFromJsonFields(v, root, r, _Share());\n", dartName(st.Name))
	g.pf("\t} else if (root is Map) {\n")
	g.pf("\t\t%sFromJsonFields(v, Map<String, dynamic>.from(root), r, _Share());\n", dartName(st.Name))
	g.pf("\t}\n")
	g.pf("\treturn v;\n")
	g.pf("}\n\n")
}
