// THE TEXT FORM OF THE CONSTRUCTS THE MAPS-AND-ARMS CARD BROUGHT TO FORM 1
// (docs/SPEC-TABLES.md §16), JSON in and out: maps (§2.8, an object keyed by
// the key, ascending key order), unions (§2.6, an object with ONE key, the
// arm's name; None is {}; a payload-free arm is null), arrays of unions and
// fixed positional arrays (a JSON array), enum-keyed arrays (§2.4, an object
// keyed by variant name, every slot), optional fields (§2.3, omitted when
// absent), flags (a list of the set bits' names), wstring(N) (a string) and
// the wide scalars (§15, §16.2: a 128-bit integer as its decimal integer, a
// fixed value as the shortest exact decimal in whole units with at least one
// fractional digit). The spellings are internal/tabletext write.go's and the
// reader is its tolerant read (§16.3): an unknown key is counted, a value of
// the wrong shape is a kind mismatch, a value past a bound clamps.
//
// A WIDE number is read from its TOKEN, never through a double: dart:convert's
// decoder turns every number into an int or a double and would round a Q48.16
// or a 128-bit value, so a unit whose closure holds a wide field decodes its
// text with _jsonDecodeExact, which keeps a number the int type cannot hold
// exactly as a _JNum carrying its token.
package darttable

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// needsGenericText reports whether a field's text goes through the generic
// walk below rather than the card-class emitters in textform.go.
func needsGenericText(f *ir.Field) bool {
	return f.IsMap() || unionRef(f) != nil || f.Array == ir.ArrayFixed || f.Type.Kind == ir.TWString ||
		isWide(f) || f.Type.Optional || isFlags(f)
}

func depthExpr(d int) string {
	if d == 0 {
		return "depth"
	}
	return fmt.Sprintf("depth + %d", d)
}

func flagsRef(f *ir.Field) *ir.Flags {
	if f.Type.Kind != ir.TNamed {
		return nil
	}
	fl, _ := f.Type.Ref.(*ir.Flags)
	return fl
}

func flagsFn(fl *ir.Flags, what string) string { return "_" + dartName(fl.Name) + what }

// textFlags is every flags set a closure field or arm names.
func (g *wireGen) textFlags() []*ir.Flags {
	seen := map[string]bool{}
	var out []*ir.Flags
	add := func(f *ir.Field) {
		if fl := flagsRef(f); fl != nil && !seen[fl.Name] {
			seen[fl.Name] = true
			out = append(out, fl)
		}
	}
	for _, st := range g.closure {
		for _, f := range st.Fields {
			add(f)
		}
	}
	for _, un := range g.closureUnions() {
		for _, arm := range un.Variants {
			if !arm.Void() {
				add(arm.F)
			}
		}
	}
	return out
}

// emitTextMoreRuntime emits the helpers the generic walk spends (§16.2,
// §16.3), once per unit.
func (g *wireGen) emitTextMoreRuntime() {
	g.pf(`// _JNum is a JSON number the int type cannot hold exactly, kept as its
// token so a wide field reads it without rounding (docs/SPEC-TABLES.md §16.2).
final class _JNum {
  final String token;
  final double d;
  _JNum(this.token) : d = double.parse(token);
}

// _jsonDecodeExact is JSON decoding that keeps every number's token
// (docs/SPEC-TABLES.md §16.2): an integer token an int holds is an int, and
// every other number is a _JNum.
dynamic _jsonDecodeExact(String text) {
  final p = _JParse(text);
  p.ws();
  final v = p.value(0);
  p.ws();
  if (p.i != text.length) throw const FormatException('trailing text after the JSON value');
  return v;
}

final class _JParse {
  final String s;
  int i = 0;
  _JParse(this.s);

  void ws() {
    while (i < s.length) {
      final c = s.codeUnitAt(i);
      if (c != 0x20 && c != 0x09 && c != 0x0A && c != 0x0D) return;
      i++;
    }
  }

  void expect(int c) {
    if (i >= s.length || s.codeUnitAt(i) != c) throw FormatException('expected ${String.fromCharCode(c)}', s, i);
    i++;
  }

  dynamic value(int depth) {
    if (depth > 128) throw const FormatException('nested past the depth cap of 128');
    if (i >= s.length) throw const FormatException('unexpected end of text');
    final c = s.codeUnitAt(i);
    if (c == 0x7B) {
      i++;
      final m = <String, dynamic>{};
      ws();
      if (i < s.length && s.codeUnitAt(i) == 0x7D) {
        i++;
        return m;
      }
      while (true) {
        ws();
        final k = str();
        ws();
        expect(0x3A);
        ws();
        m[k] = value(depth + 1);
        ws();
        if (i < s.length && s.codeUnitAt(i) == 0x2C) {
          i++;
          continue;
        }
        expect(0x7D);
        return m;
      }
    }
    if (c == 0x5B) {
      i++;
      final l = <dynamic>[];
      ws();
      if (i < s.length && s.codeUnitAt(i) == 0x5D) {
        i++;
        return l;
      }
      while (true) {
        ws();
        l.add(value(depth + 1));
        ws();
        if (i < s.length && s.codeUnitAt(i) == 0x2C) {
          i++;
          continue;
        }
        expect(0x5D);
        return l;
      }
    }
    if (c == 0x22) return str();
    if (s.startsWith('true', i)) {
      i += 4;
      return true;
    }
    if (s.startsWith('false', i)) {
      i += 5;
      return false;
    }
    if (s.startsWith('null', i)) {
      i += 4;
      return null;
    }
    final number = RegExp(r'-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?').matchAsPrefix(s, i);
    if (number == null) throw FormatException('unexpected character', s, i);
    i = number.end;
    if (i < s.length) {
      final next = s.codeUnitAt(i);
      if (next != 0x2C && next != 0x5D && next != 0x7D && next != 0x20 && next != 0x09 && next != 0x0A && next != 0x0D) {
        throw FormatException('invalid JSON number', s, i);
      }
    }
    final token = number.group(0)!;
    if (!token.contains('.') && !token.contains('e') && !token.contains('E')) {
      final n = int.tryParse(token);
      if (n != null) return n;
    }
    return _JNum(token);
  }

  String str() {
    expect(0x22);
    final b = StringBuffer();
    while (true) {
      if (i >= s.length) throw const FormatException('unterminated string');
      final c = s.codeUnitAt(i++);
      if (c == 0x22) return b.toString();
      if (c != 0x5C) {
        b.writeCharCode(c);
        continue;
      }
      if (i >= s.length) throw const FormatException('unterminated escape');
      final e = s.codeUnitAt(i++);
      switch (e) {
        case 0x22:
        case 0x5C:
        case 0x2F:
          b.writeCharCode(e);
        case 0x62:
          b.writeCharCode(0x08);
        case 0x66:
          b.writeCharCode(0x0C);
        case 0x6E:
          b.writeCharCode(0x0A);
        case 0x72:
          b.writeCharCode(0x0D);
        case 0x74:
          b.writeCharCode(0x09);
        case 0x75:
          if (i + 4 > s.length) throw const FormatException('short \\u escape');
          b.writeCharCode(int.parse(s.substring(i, i + 4), radix: 16));
          i += 4;
        default:
          throw FormatException('bad escape', s, i);
      }
    }
  }
}

// _jsonWide spells a wide raw value (docs/SPEC-TABLES.md §16.2,
// internal/tabletext FormatWide): a 128-bit integer as its decimal integer, a
// fixed value as the shortest exact decimal with at least one fraction digit.
String _jsonWide(BigInt raw, int frac, bool integer) {
  if (integer) return raw.toString();
  final b = StringBuffer();
  final mag = raw.abs();
  if (raw.isNegative) b.write('-');
  final mask = (BigInt.one << frac) - BigInt.one;
  b.write((mag >> frac).toString());
  b.write('.');
  var fr = mag & mask;
  if (fr == BigInt.zero) {
    b.write('0');
    return b.toString();
  }
  final ten = BigInt.from(10);
  while (fr != BigInt.zero) {
    fr = fr * ten;
    b.write((fr >> frac).toString());
    fr = fr & mask;
  }
  return b.toString();
}

// _jsonWideField reads one wide value from its token (docs/SPEC-TABLES.md
// §16.2, internal/tabletext ParseWide and WideClamp): the value times 2^frac,
// exactly; a value this Q I.F cannot spell is a kind mismatch and places
// nothing; a magnitude past the storage domain saturates and counts a clamp,
// and the declared raw range clamps after.
BigInt _jsonWideField(dynamic val, TableTextReport r, int frac, bool signed, BigInt? lo, BigInt? hi, BigInt def) {
  String token;
  if (val is int) {
    token = val.toString();
  } else if (val is _JNum) {
    token = val.token;
  } else if (val is double) {
    token = val.toString();
  } else {
    r.kindMismatch++;
    return def;
  }
  var i = 0;
  var negative = false;
  if (i < token.length && (token[i] == '-' || token[i] == '+')) {
    negative = token[i] == '-';
    i++;
  }
  final intDigits = StringBuffer();
  final fracDigits = StringBuffer();
  while (i < token.length && token.codeUnitAt(i) >= 0x30 && token.codeUnitAt(i) <= 0x39) {
    intDigits.write(token[i]);
    i++;
  }
  if (i < token.length && token[i] == '.') {
    i++;
    while (i < token.length && token.codeUnitAt(i) >= 0x30 && token.codeUnitAt(i) <= 0x39) {
      fracDigits.write(token[i]);
      i++;
    }
  }
  var exp = 0;
  if (i < token.length && (token[i] == 'e' || token[i] == 'E')) {
    i++;
    var expNegative = false;
    if (i < token.length && (token[i] == '-' || token[i] == '+')) {
      expNegative = token[i] == '-';
      i++;
    }
    while (i < token.length && token.codeUnitAt(i) >= 0x30 && token.codeUnitAt(i) <= 0x39) {
      if (exp < 100000) exp = exp * 10 + (token.codeUnitAt(i) - 0x30);
      i++;
    }
    if (expNegative) exp = -exp;
  }
  var digits = intDigits.toString() + fracDigits.toString();
  var point = intDigits.length + exp;
  while (digits.isNotEmpty && digits[0] == '0') {
    digits = digits.substring(1);
    point--;
  }
  while (digits.isNotEmpty && digits[digits.length - 1] == '0') {
    digits = digits.substring(0, digits.length - 1);
  }
  final domainLo = signed ? -(BigInt.one << 127) : BigInt.zero;
  final domainHi = signed ? (BigInt.one << 127) - BigInt.one : (BigInt.one << 128) - BigInt.one;
  BigInt raw;
  if (digits.isEmpty) {
    raw = BigInt.zero;
  } else if (point > 40) {
    raw = negative ? domainLo : domainHi;
    r.clamped++;
  } else if (point < -40) {
    r.kindMismatch++;
    return def;
  } else {
    final scale = point - digits.length;
    var v = BigInt.parse(digits) << frac;
    if (scale >= 0) {
      v = v * BigInt.from(10).pow(scale);
    } else {
      final den = BigInt.from(10).pow(-scale);
      if (v.remainder(den) != BigInt.zero) {
        r.kindMismatch++;
        return def;
      }
      v = v ~/ den;
    }
    raw = negative ? -v : v;
    if (raw < domainLo) {
      raw = domainLo;
      r.clamped++;
    } else if (raw > domainHi) {
      raw = domainHi;
      r.clamped++;
    }
  }
  if (lo != null && raw < lo) {
    raw = lo;
    r.clamped++;
  }
  if (hi != null && raw > hi) {
    raw = hi;
    r.clamped++;
  }
  return raw;
}

bool _jsonBoolField(dynamic val, TableTextReport r) {
  if (val is bool) return val;
  r.kindMismatch++;
  return false;
}

String _jsonBytesField(dynamic val, TableTextReport r, int bound) {
  if (val is String) {
    try {
      final b = base64Decode(base64.normalize(val));
      if (b.length > bound) {
        r.clamped++;
        return latin1.decode(b.sublist(0, bound));
      }
      return latin1.decode(b);
    } on FormatException {
      // falls through to the mismatch below
    }
  }
  r.kindMismatch++;
  return '';
}

String? _jsonStringBlob(dynamic val, TableTextReport r) {
  if (val == null) return null;
  if (val is String) return _jsonText(val);
  r.kindMismatch++;
  return null;
}

Uint8List? _jsonBytesBlob(dynamic val, TableTextReport r) {
  if (val == null) return null;
  if (val is String) {
    try {
      return base64Decode(base64.normalize(val));
    } on FormatException {
      // falls through to the mismatch below
    }
  }
  r.kindMismatch++;
  return null;
}

// _jsonWStringField reads wide text (docs/SPEC-TABLES.md §16.2): a string,
// clamped at its bound in CODE UNITS and never through a surrogate pair.
String _jsonWStringField(dynamic val, int bound, TableTextReport r) {
  if (val is! String) {
    r.kindMismatch++;
    return '';
  }
  if (_jsonText(val).length <= bound) return val;
  var keep = bound;
  if (keep > 0) {
    final u = val.codeUnitAt(keep - 1);
    if (u >= 0xD800 && u <= 0xDBFF) keep--;
  }
  r.clamped++;
  return val.substring(0, keep);
}

List<T> _jsonList<T>(dynamic val, TableTextReport r, int bound, T Function(dynamic) f) {
  final out = <T>[];
  if (val is! List) {
    r.kindMismatch++;
    return out;
  }
  for (final item in val) {
    if (bound >= 0 && out.length >= bound) {
      r.clamped += val.length - out.length;
      break;
    }
    out.add(f(item));
  }
  return out;
}

List<T> _jsonFixed<T>(dynamic val, TableTextReport r, int n, T Function() def, T Function(dynamic) f) {
  final out = List<T>.generate(n, (_) => def());
  if (val is! List) {
    r.kindMismatch++;
    return out;
  }
  for (var i = 0; i < val.length; i++) {
    if (i >= n) {
      r.clamped += val.length - i;
      break;
    }
    out[i] = f(val[i]);
  }
  return out;
}

List<T> _jsonKeyed<T>(dynamic val, TableTextReport r, int n, T Function() def, Map<String, int> slots, T Function(dynamic) f) {
  final out = List<T>.generate(n, (_) => def());
  if (val is! Map) {
    r.kindMismatch++;
    return out;
  }
  for (final entry in val.entries) {
    final slot = slots[entry.key];
    if (slot == null) {
      final extra = r.duplicateKeys.remove(entry.key) ?? 0;
      r.unknown += 1 + extra;
      r.duplicate -= extra;
      continue;
    }
    out[slot] = f(entry.value);
  }
  return out;
}

List<T> _jsonMap<T>(dynamic val, TableTextReport r, T Function(String, dynamic) f) {
  final out = <T>[];
  if (val is! Map) {
    r.kindMismatch++;
    return out;
  }
  for (final entry in val.entries) {
    out.add(f(entry.key as String, entry.value));
  }
  return out;
}

int _jsonIntKey(String k, TableTextReport r, int? lo, int? hi, bool unsigned) {
  final v = int.tryParse(k);
  if (v == null) {
    r.kindMismatch++;
    return 0;
  }
  return _jsonIntField(v, r, lo, hi, unsigned);
}

`)
}

// emitTextMoreDecls emits the per-declaration text helpers: one per flags
// set, one per union and one by-value reader per table (§16.2).
func (g *wireGen) emitTextMoreDecls() {
	for _, fl := range g.textFlags() {
		g.pf("void %s(_JOut out, int bits, int depth) {\n", flagsFn(fl, "ToJson"))
		g.pf("\tif (bits == 0) { out.raw('[]'); return; }\n")
		g.pf("\tout.raw('[');\n")
		g.pf("\tvar first = true;\n")
		for i, name := range fl.Variants {
			sep := "if (!first) out.raw(','); "
			if i == 0 {
				sep = "" // the first bit has no comma before it, so none is tested
			}
			g.pf("\tif (bits & %d != 0) { %sfirst = false; out.line(depth + 1); out.raw(_jsonString(%s)); }\n", uint64(1)<<i, sep, strconv.Quote(name))
		}
		g.pf("\tif (first) { throw StateError('flags %s: bits $bits name no variant (docs/SPEC-TABLES.md §16.2)'); }\n", fl.Name)
		g.pf("\tout.line(depth);\n")
		g.pf("\tout.raw(']');\n")
		g.pf("}\n\n")
		g.pf("int %s(dynamic val, TableTextReport r) {\n", flagsFn(fl, "FromJson"))
		g.pf("\tif (val is! List) { r.kindMismatch++; return 0; }\n")
		g.pf("\tvar bits = 0;\n")
		g.pf("\tfor (final item in val) {\n")
		g.pf("\t\tswitch (item) {\n")
		for i, name := range fl.Variants {
			g.pf("\t\t\tcase %s: bits |= %d;\n", strconv.Quote(name), uint64(1)<<i)
		}
		g.pf("\t\t\tdefault: r.unknown++;\n")
		g.pf("\t\t}\n")
		g.pf("\t}\n")
		g.pf("\treturn bits;\n")
		g.pf("}\n\n")
	}
	for _, un := range g.closureUnions() {
		g.pf("void %s(_JOut out, %s v, int depth, _Share sh) {\n", unionFn(un, "ToJson"), un.Name)
		g.pf("\tswitch (v.armTag) {\n")
		g.pf("\t\tcase 0:\n\t\t\tout.raw('{}');\n\t\t\treturn;\n")
		for i, arm := range un.Variants {
			g.pf("\t\tcase %d:\n", i+1)
			g.pf("\t\t\tout.raw('{');\n")
			g.pf("\t\t\tout.line(depth + 1);\n")
			g.pf("\t\t\tout.raw(_jsonString(%s));\n", strconv.Quote(arm.Name))
			g.pf("\t\t\tout.raw(': ');\n")
			if arm.Void() {
				g.pf("\t\t\tout.raw('null');\n")
			} else {
				g.emitJsonValue(arm.F, "v."+armMember(arm), 1, "\t\t\t", un.Name+"."+arm.Name)
			}
			g.pf("\t\t\tout.line(depth);\n")
			g.pf("\t\t\tout.raw('}');\n")
			g.pf("\t\t\treturn;\n")
		}
		g.pf("\t}\n")
		g.pf("\tthrow StateError('union %s: tag ${v.armTag} names no arm — the writer refuses it (docs/SPEC-TABLES.md §5)');\n", un.Name)
		g.pf("}\n\n")

		g.pf("%s %s(dynamic val, TableTextReport r, _Share sh) {\n", un.Name, unionFn(un, "FromJson"))
		g.pf("\tfinal v = %s();\n", un.Name)
		g.pf("\tif (val is! Map) { r.kindMismatch++; return v; }\n")
		g.pf("\tif (val.isEmpty) return v;\n")
		g.pf("\tif (val.length != 1) throw const FormatException('union has two keys');\n")
		g.pf("\tfinal item = val.values.first;\n")
		g.pf("\tswitch (val.keys.first) {\n")
		for i, arm := range un.Variants {
			g.pf("\t\tcase %s:\n", strconv.Quote(arm.Name))
			g.pf("\t\t\tv.armTag = %d;\n", i+1)
			if arm.Void() {
				g.pf("\t\t\tif (item != null) r.kindMismatch++;\n")
			} else {
				g.pf("\t\t\tv.%s = %s;\n", armMember(arm), g.jsonReadExpr(arm.F, "item", 1))
			}
		}
		g.pf("\t\tdefault:\n\t\t\tr.unknown++;\n")
		g.pf("\t}\n")
		g.pf("\treturn v;\n")
		g.pf("}\n\n")

		g.pf("void %s(_Share sh, %s v) {\n", unionFn(un, "Count"), un.Name)
		g.pf("\tswitch (v.armTag) {\n")
		for i, arm := range un.Variants {
			if arm.Void() {
				continue
			}
			g.pf("\t\tcase %d:\n", i+1)
			g.emitCountValue(arm.F, "v."+armMember(arm), "\t\t\t", 1)
			g.pf("\t\t\tbreak;\n")
		}
		g.pf("\t}\n")
		g.pf("}\n\n")
	}
	for _, st := range g.closure {
		g.pf("%s _%sFromJsonValue(dynamic val, TableTextReport r, _Share sh) {\n", st.Name, dartName(st.Name))
		g.pf("\tfinal v = %s();\n", st.Name)
		g.pf("\tif (val is Map<String, dynamic>) {\n")
		g.pf("\t\t%sFromJsonFields(v, val, r, sh);\n", dartName(st.Name))
		g.pf("\t} else if (val is Map) {\n")
		g.pf("\t\t%sFromJsonFields(v, Map<String, dynamic>.from(val), r, sh);\n", dartName(st.Name))
		g.pf("\t} else {\n")
		g.pf("\t\tr.kindMismatch++;\n")
		g.pf("\t}\n")
		g.pf("\treturn v;\n")
		g.pf("}\n\n")
	}
}

// mapKeyText is the Dart expression of one map entry's key as an object key
// (§2.8, §16): the string itself, or the integer's decimal digits.
func mapKeyText(f *ir.Field, entry string) string {
	key := f.MapEntry.Fields[0]
	m := entry + "." + fieldMember(key)
	if key.Type.Kind == ir.TString {
		return m
	}
	if key.Type.Kind == ir.TInt && !key.Type.Signed && key.Type.Width == 64 {
		return "BigInt.from(" + m + ").toUnsigned(64).toString()"
	}
	return m + ".toString()"
}

// emitJsonValue writes one field-shaped value (a field, an arm, an element, a
// map value) at depth + d (§16.1, §16.2; internal/tabletext writeField and
// writeScalar).
func (g *wireGen) emitJsonValue(f *ir.Field, expr string, d int, ind, where string) {
	e := fmt.Sprintf("e%d", d)
	switch {
	case f.Type.Optional:
		g.emitJsonValue(nonOptional(f), expr, d, ind, where)
	case f.IsMap():
		val := f.MapEntry.Fields[1]
		g.pf("%sif (%s.isEmpty) {\n", ind, expr)
		g.pf("%s\tout.raw('{}');\n", ind)
		g.pf("%s} else {\n", ind)
		g.pf("%s\tout.raw('{');\n", ind)
		g.pf("%s\tvar first%d = true;\n", ind, d)
		g.pf("%s\tfor (final %s in %s) {\n", ind, e, mapSorted(f, expr))
		g.pf("%s\t\tif (!first%d) out.raw(',');\n", ind, d)
		g.pf("%s\t\tfirst%d = false;\n", ind, d)
		g.pf("%s\t\tout.line(%s);\n", ind, depthExpr(d+1))
		g.pf("%s\t\tout.raw(_jsonString(%s));\n", ind, mapKeyText(f, e))
		g.pf("%s\t\tout.raw(': ');\n", ind)
		g.emitJsonValue(val, e+"."+fieldMember(val), d+1, ind+"\t\t", where)
		g.pf("%s\t}\n", ind)
		g.pf("%s\tout.line(%s);\n", ind, depthExpr(d))
		g.pf("%s\tout.raw('}');\n", ind)
		g.pf("%s}\n", ind)
	case isKeyed(f):
		en := f.KeyEnumRef
		if f.ArrayBound == 0 {
			g.pf("%sout.raw('{}');\n", ind)
			return
		}
		g.pf("%sout.raw('{');\n", ind)
		for i := 0; i < int(f.ArrayBound) && i < len(en.Variants); i++ {
			if i > 0 {
				g.pf("%sout.raw(',');\n", ind)
			}
			g.pf("%sout.line(%s);\n", ind, depthExpr(d+1))
			g.pf("%sout.raw(_jsonString(%s));\n", ind, strconv.Quote(en.Variants[i]))
			g.pf("%sout.raw(': ');\n", ind)
			g.emitJsonValue(singleOf(f), fmt.Sprintf("%s[%d]", expr, i), d+1, ind, where)
		}
		g.pf("%sout.line(%s);\n", ind, depthExpr(d))
		g.pf("%sout.raw('}');\n", ind)
	case f.Array != ir.ArrayNone:
		g.pf("%sif (%s.isEmpty) {\n", ind, expr)
		g.pf("%s\tout.raw('[]');\n", ind)
		g.pf("%s} else {\n", ind)
		g.pf("%s\tout.raw('[');\n", ind)
		g.pf("%s\tvar first%d = true;\n", ind, d)
		g.pf("%s\tfor (final %s in %s) {\n", ind, e, expr)
		g.pf("%s\t\tif (!first%d) out.raw(',');\n", ind, d)
		g.pf("%s\t\tfirst%d = false;\n", ind, d)
		g.pf("%s\t\tout.line(%s);\n", ind, depthExpr(d+1))
		g.emitJsonValue(singleOf(f), e, d+1, ind+"\t\t", where)
		g.pf("%s\t}\n", ind)
		g.pf("%s\tout.line(%s);\n", ind, depthExpr(d))
		g.pf("%s\tout.raw(']');\n", ind)
		g.pf("%s}\n", ind)
	case f.Type.Blob():
		if f.Type.Kind == ir.TString {
			g.pf("%sif (%s == null) { out.raw('null'); } else { out.raw(_jsonString(%s!)); }\n", ind, expr, expr)
		} else {
			g.pf("%sif (%s == null) { out.raw('null'); } else { out.raw(_jsonString(base64Encode(%s!))); }\n", ind, expr, expr)
		}
	case isPointerTable(f):
		ref := tableRef(f)
		g.pf("%s_writePtr(out, %s, %s, sh, (label) {\n", ind, expr, depthExpr(d))
		g.pf("%s\t%sToJsonBody(out, %s!, %s, sh, label);\n", ind, dartName(ref.Name), expr, depthExpr(d))
		g.pf("%s});\n", ind)
	case isByValueTable(f):
		g.pf("%s%sToJsonBody(out, %s, %s, sh);\n", ind, dartName(tableRef(f).Name), expr, depthExpr(d))
	case unionRef(f) != nil:
		g.pf("%s%s(out, %s, %s, sh);\n", ind, unionFn(unionRef(f), "ToJson"), expr, depthExpr(d))
	case isWide(f):
		k := wideKind(f)
		g.pf("%sout.raw(_jsonWide(%s, %d, %v));\n", ind, expr, f.Type.FracBits, k == ir.TableKindI128 || k == ir.TableKindU128)
	case isFlags(f):
		g.pf("%s%s(out, %s, %s);\n", ind, flagsFn(flagsRef(f), "ToJson"), expr, depthExpr(d))
	case f.Type.Kind == ir.TWString, f.Type.Kind == ir.TString:
		g.pf("%sout.raw(_jsonString(%s));\n", ind, expr)
	case f.Type.Kind == ir.TBytes:
		g.pf("%sout.raw(_jsonString(base64Encode(latin1.encode(%s))));\n", ind, expr)
	case f.Type.Kind == ir.TBool:
		g.pf("%sout.raw(%s ? 'true' : 'false');\n", ind, expr)
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		g.pf("%sout.raw(_jsonFloat(%s, %v));\n", ind, expr, f.Type.Kind == ir.TFloat32)
	case enumRef(f) != nil:
		g.pf("%sout.raw(_jsonEnum(%s, %sName, '%s'));\n", ind, expr, dartName(enumRef(f).Name), where)
	case f.Type.Kind == ir.TInt && !f.Type.Signed && f.Type.Width == 64:
		g.pf("%sout.raw(BigInt.from(%s).toUnsigned(64).toString());\n", ind, expr)
	default:
		g.pf("%sout.raw(%s.toString());\n", ind, expr)
	}
}

// jsonReadExpr is the Dart expression that reads one field-shaped value out of
// the decoded JSON value `val` (§16.2, §16.3).
func (g *wireGen) jsonReadExpr(f *ir.Field, val string, d int) string {
	item := fmt.Sprintf("i%d", d)
	switch {
	case f.Type.Optional:
		return g.jsonReadExpr(nonOptional(f), val, d)
	case f.IsMap():
		key, value := f.MapEntry.Fields[0], f.MapEntry.Fields[1]
		var keyExpr string
		if key.Type.Kind == ir.TString {
			keyExpr = fmt.Sprintf("_jsonStringField(k%d, %d, r)", d, key.Type.Size)
		} else {
			lo, hi := "null", "null"
			if key.HasIntRange {
				lo, hi = dartBoundLiteral(key.IntMin), dartBoundLiteral(key.IntMax)
			}
			keyExpr = fmt.Sprintf("_jsonIntKey(k%d, r, %s, %s, %s)", d, lo, hi, jsonUnsigned(key))
		}
		et := f.MapEntry.Name
		return fmt.Sprintf("_jsonMap<%s>(%s, r, (k%d, %s) { final m%d = %s(); m%d.%s = %s; m%d.%s = %s; return m%d; })",
			et, val, d, item, d, et, d, fieldMember(key), keyExpr, d, fieldMember(value), g.jsonReadExpr(value, item, d+1), d)
	case isKeyed(f):
		en := f.KeyEnumRef
		var slots strings.Builder
		slots.WriteString("const <String, int>{")
		for i := 0; i < int(f.ArrayBound) && i < len(en.Variants); i++ {
			fmt.Fprintf(&slots, "%s: %d, ", strconv.Quote(en.Variants[i]), i)
		}
		slots.WriteString("}")
		return fmt.Sprintf("_jsonKeyed<%s>(%s, r, %d, () => %s, %s, (%s) => %s)",
			g.listElemType(f), val, f.ArrayBound, g.elemDefault(f), slots.String(), item, g.jsonReadExpr(singleOf(f), item, d+1))
	case f.Array == ir.ArrayFixed:
		return fmt.Sprintf("_jsonFixed<%s>(%s, r, %d, () => %s, (%s) => %s)",
			g.listElemType(f), val, f.ArrayBound, g.elemDefault(f), item, g.jsonReadExpr(singleOf(f), item, d+1))
	case f.Array != ir.ArrayNone:
		bound := -1
		if f.Array == ir.ArrayCounted {
			bound = int(f.ArrayBound)
		}
		return fmt.Sprintf("_jsonList<%s>(%s, r, %d, (%s) => %s)",
			g.listElemType(f), val, bound, item, g.jsonReadExpr(singleOf(f), item, d+1))
	case f.Type.Blob():
		if f.Type.Kind == ir.TString {
			return fmt.Sprintf("_jsonStringBlob(%s, r)", val)
		}
		return fmt.Sprintf("_jsonBytesBlob(%s, r)", val)
	case isPointerTable(f):
		ref := tableRef(f)
		return fmt.Sprintf("_readPtr(%s, sh, r, () => %s(), (obj, m) { %sFromJsonFields(obj as %s, m, r, sh, true); }) as %s?",
			val, ref.Name, dartName(ref.Name), ref.Name, ref.Name)
	case isByValueTable(f):
		return fmt.Sprintf("_%sFromJsonValue(%s, r, sh)", dartName(tableRef(f).Name), val)
	case unionRef(f) != nil:
		return fmt.Sprintf("%s(%s, r, sh)", unionFn(unionRef(f), "FromJson"), val)
	case isWide(f):
		lo, hi := "null", "null"
		if l, h, ok := ir.TableRawRange(singleOf(f)); ok {
			lo, hi = bigLiteral(l), bigLiteral(h)
		}
		return fmt.Sprintf("_jsonWideField(%s, r, %d, %v, %s, %s, %s)", val, f.Type.FracBits, ir.TableKindSigned(wideKind(f)), lo, hi, g.wideDefault(singleOf(f)))
	case isFlags(f):
		return fmt.Sprintf("%s(%s, r)", flagsFn(flagsRef(f), "FromJson"), val)
	case f.Type.Kind == ir.TWString:
		return fmt.Sprintf("_jsonWStringField(%s, %d, r)", val, f.Type.Size)
	case f.Type.Kind == ir.TString:
		return fmt.Sprintf("_jsonStringField(%s, %d, r)", val, f.Type.Size)
	case f.Type.Kind == ir.TBytes:
		return fmt.Sprintf("_jsonBytesField(%s, r, %d)", val, f.Type.Size)
	case f.Type.Kind == ir.TBool:
		return fmt.Sprintf("_jsonBoolField(%s, r)", val)
	case f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64:
		return fmt.Sprintf("_jsonFloatField(%s, r, %t)", val, f.Type.Kind == ir.TFloat32)
	case enumRef(f) != nil:
		return fmt.Sprintf("%sFromName(%s, r)", dartName(enumRef(f).Name), val)
	case f.Type.Kind == ir.TBits:
		hi := "null"
		if f.Type.Width < 64 {
			hi = fmt.Sprintf("%d", uint64(1)<<f.Type.Width-1)
		}
		return fmt.Sprintf("_jsonIntField(%s, r, null, %s, true)", val, hi)
	default:
		lo, hi := "null", "null"
		if f.HasIntRange {
			lo, hi = dartBoundLiteral(f.IntMin), dartBoundLiteral(f.IntMax)
		}
		return fmt.Sprintf("_jsonIntField(%s, r, %s, %s, %s)", val, lo, hi, jsonUnsigned(f))
	}
}

// countable reports whether a value can hold a pointer slot the share count
// walks (§16.7): a pointer, a table or type, a union, or a collection of them.
func countable(f *ir.Field) bool {
	switch {
	case f.Type.Optional:
		return countable(nonOptional(f))
	case f.IsMap():
		return countable(f.MapEntry.Fields[1])
	case f.Type.Blob():
		return false
	}
	return tableRef(f) != nil || unionRef(f) != nil
}

// emitCountValue counts the slots that name each node under one value (§16.7),
// so a shared node is defined once and referenced after.
func (g *wireGen) emitCountValue(f *ir.Field, expr, ind string, d int) {
	c := fmt.Sprintf("c%d", d)
	if !countable(f) {
		return
	}
	switch {
	case f.Type.Optional:
		g.emitCountValue(nonOptional(f), expr, ind, d)
	case f.IsMap():
		val := f.MapEntry.Fields[1]
		g.pf("%sfor (final %s in %s) {\n", ind, c, expr)
		g.emitCountValue(val, c+"."+fieldMember(val), ind+"\t", d+1)
		g.pf("%s}\n", ind)
	case f.Array != ir.ArrayNone:
		if f.Type.Blob() {
			return
		}
		if tableRef(f) == nil && unionRef(f) == nil {
			return
		}
		g.pf("%sfor (final %s in %s) {\n", ind, c, expr)
		g.emitCountValue(singleOf(f), c, ind+"\t", d+1)
		g.pf("%s}\n", ind)
	case isPointerTable(f):
		ref := tableRef(f)
		g.pf("%sfinal node%d = %s;\n", ind, d, expr)
		g.pf("%sif (node%d != null) {\n", ind, d)
		g.pf("%s\tfinal first%d = (sh.count[node%d] ?? 0) == 0;\n", ind, d, d)
		g.pf("%s\tsh.see(node%d);\n", ind, d)
		g.pf("%s\tif (first%d) %sCount(sh, node%d);\n", ind, d, dartName(ref.Name), d)
		g.pf("%s}\n", ind)
	case isByValueTable(f):
		g.pf("%s%sCount(sh, %s);\n", ind, dartName(tableRef(f).Name), expr)
	case unionRef(f) != nil:
		g.pf("%s%s(sh, %s);\n", ind, unionFn(unionRef(f), "Count"), expr)
	}
}
