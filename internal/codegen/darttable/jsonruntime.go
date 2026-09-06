// The reflection descriptor classes and THE TEXT FORM's generic walk
// (docs/SPEC-TABLES.md §8, §16), emitted once per unit into the runtime home.
//
// The walk is the C++ reference's, structure for structure. Two things Dart
// forces are written at their sites in the source below:
//
//   - THE STORAGE TRIPLE IS (owner, field index, element index), because the
//     descriptors are const and a const cannot hold a closure over a field.
//     C++ takes the storage's address; C# takes a per-field delegate; this
//     takes a per-TYPE static method and the field's index. Same role, same
//     one place a walker reaches storage through.
//   - C's "%.*g" IS SPELLED OUT, exactly, over the double's own bits: Dart has
//     no printf and its own number formatting rounds ties AWAY FROM ZERO while
//     C rounds them TO EVEN. The digits are generated from the exact value with
//     BigInt, so the bytes agree with the reference's snprintf.
package darttable

// tableJsonSource is the descriptor classes plus the walk. It does not vary
// with the unit — the generic-walk property the reference states for C++ and
// C# holds here too.
const tableJsonSource = `
// ---- reflection (tables only, docs/SPEC-TABLES.md §8) ----
//
// Static field descriptors for every type in the table closure: name, wire id
// and kind, bounds, ranges, the enum/union vocabulary and its wire ids, and
// branch guards — enough to walk, print, diff or bind any table value at
// runtime with no schema files on hand. <name>TableType is the descriptor, and
// it is a compile-time CONSTANT.
//
// THE MEMORY COLUMNS ARE ON THE TYPE, NOT ON THE FIELD, and that is the whole
// of this surface's divergence from C++'s. C++ locates a field with an offset
// and a width, because its storage is one flat struct; a Dart field has no
// address, so the type's descriptor carries static methods the emitter wrote
// and a walker reaches storage as (owner, field index, element index). A
// closure per field would say the same thing and could not be const.

// A union field's arms, indexed by the tag: index 0 is the EMPTY arm and
// carries no descriptor.
final class TableUnionInfo {
  final List<TableTypeInfo?> arms;

  const TableUnionInfo(this.arms);
}

// THE SHARED EMPTY DOC (docs/SPEC-TABLES.md §8.1): a declaration with no ///
// block carries a doc column naming this one definition, so absence costs a
// unit no string data and a printer concatenates doc columns with no null
// test. Dart gives a string no address identity, so what the rule is READ OFF
// here is the emitted text: the unit defines the empty doc ONCE and every
// unannotated row names that definition, rather than each row carrying an
// inline '' of its own.
const TableDocNone = '';

final class TableFieldInfo {
  final String name; // schema field name, e.g. "health"
  // the TEXT form's key: the json = "key" attribute, else the field's name
  final String json;
  final String typeName; // schema type name, e.g. "float32", "Grade"
  final int id; // table-wire field id (the was alias's hash after a rename)
  final int kind; // table-wire kind; for arrays/strings/bytes, the ELEMENT kind
  final bool isArray; // fixed or counted array (bytes included)
  final bool counted; // a <name>Count/<name>Length companion exists
  // a ?T field: a <name>Present bool decides whether it rides
  final bool optional;
  // array capacity / string max length; 0 for plain scalars
  final int arrayBound;

  // the STORAGE width of one element in bytes, C++'s elem_size: the last bound
  // a numeric read clamps to (§16.2). 0 on every kind whose storage is not a
  // fixed-width number.
  final int elemWidth;
  final bool hasRange; // a declared [min, max] (int or float)
  final double rangeMin; // NOTE: int64 ranges beyond 2^53 lose precision here
  final double rangeMax;

  // enums: the highest valid value (None = 0 is always valid); unions: the arm
  // count (the tag range is [0, enumMax]); flags: the highest bit; else -1.
  final int enumMax;

  // the vocabulary this field names: an enum's values, a union's arms, or a
  // flags mask's BITS. A vocabulary with no ids is a FLAGS mask — a flags
  // variant is a bit position and rides under no wire id (§4), and that
  // missing id is what tells the two apart at runtime.
  final TableEnumVocab? vocab;

  // an ENUM-KEYED array (docs/SPEC-TABLES.md §2.4, §8): the array has one slot per
  // variant of keyTypeName, indexed by the variant's value, and its slots ride
  // under variant ids rather than positions. SLOT 0 IS NONE'S AND IS NEVER
  // VALID, so a walker enumerating slots skips it rather than printing a None
  // row. Both are null on every other field.
  final String? keyTypeName;
  final TableEnumVocab? keyVocab;

  // branch guard, e.g. "at_rest" or "!at_rest"; "" if unguarded
  final String guard;
  final TableTypeInfo? table; // the nested table's descriptor, or null
  final TableUnionInfo? arms; // a union field's arms, or null

  // what a PERSON wrote about the field (docs/SPEC-TABLES.md §8.1): the ///
  // block above it, verbatim (SPEC §4.1). It is TableDocNone when there is
  // none, never null. Its tags (SPEC §4.2) follow in declared order, and an
  // untagged field is 0 beside a null list. Both are const data, allocating
  // nothing.
  final String doc;
  final int numTags;
  final List<String>? tags;

  const TableFieldInfo({
    required this.name,
    required this.json,
    required this.typeName,
    required this.id,
    required this.kind,
    required this.isArray,
    required this.counted,
    required this.optional,
    required this.arrayBound,
    required this.elemWidth,
    required this.hasRange,
    required this.rangeMin,
    required this.rangeMax,
    required this.enumMax,
    required this.vocab,
    required this.keyTypeName,
    required this.keyVocab,
    required this.guard,
    required this.table,
    required this.arms,
    required this.doc,
    required this.numTags,
    required this.tags,
  });
}

final class TableTypeInfo {
  final String name; // schema type name
  final List<TableFieldInfo> fields;

  // put one instance back at its declared defaults, in place. A generic walker
  // that FILLS a value has to establish the defaults an absent field takes,
  // and it holds no type to spell — this is the one thing the columns could
  // not express without a function (docs/SPEC-TABLES.md §8.1). It is <name>Reset,
  // the same prefill the wire's read path calls.
  final void Function(Object owner) reset;

  // ---- the storage location, in Dart's own currency ----
  //
  // getRaw/setRaw carry one NUMERIC element: an integer as itself, a bool as 0
  // or 1, an enum or a flags mask as its value, a float as its IEEE-754 bit
  // pattern. child hands back the OBJECT a nested table, a union or a
  // class-typed element is stored as. buffer hands back a string(N)'s or
  // bytes(N)'s Uint8List. The int is the element index — the array slot, or a
  // keyed array's STORAGE index — and 0 for a field that is not an array.
  final int Function(Object owner, int field, int index) getRaw;
  final void Function(Object owner, int field, int index, int raw) setRaw;
  final Object Function(Object owner, int field, int index) child;
  final Uint8List Function(Object owner, int field) buffer;
  final int Function(Object owner, int field) getCount;
  final void Function(Object owner, int field, int n) setCount;
  final bool Function(Object owner, int field) getPresent;
  final void Function(Object owner, int field, bool p) setPresent;
  final int Function(Object owner, int field) getTag;
  final void Function(Object owner, int field, int tag) setTag;
  final Object Function(Object owner, int field, int arm) armPayload;

  // the declaration's own doc and tags, on the same terms as a field's
  // (docs/SPEC-TABLES.md §8.1, SPEC §4.1, §4.2)
  final String doc;
  final int numTags;
  final List<String>? tags;

  const TableTypeInfo({
    required this.name,
    required this.fields,
    required this.reset,
    required this.getRaw,
    required this.setRaw,
    required this.child,
    required this.buffer,
    required this.getCount,
    required this.setCount,
    required this.getPresent,
    required this.setPresent,
    required this.getTag,
    required this.setTag,
    required this.armPayload,
    required this.doc,
    required this.numTags,
    required this.tags,
  });

  int get numFields => fields.length;
}

// ---- json walk: begin ----
//
// The TEXT form (docs/SPEC-TABLES.md §16): one table, one text, one walk over the
// reflection descriptors (§8). Reading fills ONE caller-owned instance;
// writing targets a caller buffer with the wire's measure/write symmetry.
// Everything AROUND this — which file goes with which instance, what key an
// instance is filed under, how instances link into a root table's collections
// — is a packer's opinion and stays with the tool that holds it.
//
// The dialect: trailing commas are accepted on read (the authoring files this
// exists for carry them) and never written; comments are not JSON and are
// refused; unknown keys are skipped and counted; a duplicate key is last-wins
// and counted; a key present with the wrong JSON type is skipped and counted,
// never coerced. The canonical text ends with exactly ONE newline, which the
// writer emits and the reader accepts with or without.
//
// Everything below is a member of this class, so the walk claims exactly one
// name at library scope (docs/SPEC-TABLES.md §11).
abstract final class TableJson {
  static const int maxDepth = 128;

  // A key longer than this cannot name a field, so it is skipped as unknown.
  static const int maxKey = 256;

  // The longest numeric token the walk will convert. Anything longer is a
  // value no field can hold and counts as a kind mismatch.
  static const int maxNumber = 512;

  static const String base64Alphabet =
      'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';

  // The empty buffer a MEASURING write targets: measuring writes nothing, so
  // one shared instance serves every call and no measure allocates.
  static final Uint8List empty = Uint8List(0);

  // A vocabulary entry the descriptor could not spell. A vocabulary answers
  // '???' for a value outside the declared set, and that is not a name —
  // writing it would put a spelling in the text that the reader then counts as
  // unknown, turning a refusal into a silent loss.
  static bool named(String name) => name != '???';

  // a counted field's companion: a string's length, a bytes' length, a counted
  // array's count. Bounded by the declared extent on the way out, so a storage
  // invariant a caller broke cannot walk off the end of the array.
  static int count(Object value, TableTypeInfo info, int index) {
    final f = info.fields[index];
    if (!f.counted) {
      return f.arrayBound;
    }
    var n = info.getCount(value, index);
    if (n < 0) {
      n = 0;
    }
    if (n > f.arrayBound) {
      n = f.arrayBound;
    }
    return n;
  }

  static void putCount(Object value, TableTypeInfo info, int index, int n) {
    if (info.fields[index].counted) {
      info.setCount(value, index, n);
    }
  }

  // ---- what a field's kind expects to see in the text ----
  //
  // One classifier, consulted by both directions, so a reader and a writer can
  // never disagree about a kind's JSON form. 'o' object, 'a' array, 's'
  // string, 'n' number, 'b' boolean.
  //
  // bytes(N) is the one kind whose element kind does not decide its form: it
  // shares u8 with a plain array of u8, and rides as base64. The schema type
  // name settles it, and 'bytes' is a keyword no declaration can claim.
  static bool isBytes(TableFieldInfo f) =>
      f.isArray && f.kind == 6 && f.typeName == 'bytes';

  // An ENUM-KEYED array (docs/SPEC-TABLES.md §2.4): its JSON form is an OBJECT keyed
  // by variant name, not a positional array, because that is what the storage
  // is — one slot per variant, addressed by the variant.
  static bool isKeyed(TableFieldInfo f) => f.keyVocab != null;

  // THE KEY A STORAGE SLOT HOLDS (§2.4, §8): the storage shifts left, so slot i
  // holds the key i + 1 and nothing is stored for None. This is the ONE place
  // the walker spells the shift.
  static int keyedSlotKey(int slot) => slot + 1;

  // A slot whose key names a variant of the keying enum. Every slot in
  // [0, arrayBound) does, unless the enum carries max-headroom variants, where
  // a reserved value names nothing and its key id is 0 — the reserved id no
  // declared name can fold to (§5).
  static bool keyedSlotValid(TableFieldInfo f, int slot) =>
      f.keyVocab!.idOf(keyedSlotKey(slot)) > 0;

  // a vocabulary with no ids is a FLAGS mask: a flags variant is a bit
  // position and rides under no wire id (§4)
  static bool isFlags(TableFieldInfo f) =>
      f.vocab != null && f.vocab!.variantIds.isEmpty;

  static bool isEnum(TableFieldInfo f) =>
      f.vocab != null && f.vocab!.variantIds.isNotEmpty && f.arms == null;

  static int shape(TableFieldInfo f) {
    if (f.kind == 12) {
      return 0x73; // 's': string
    }
    if (isBytes(f)) {
      return 0x73; // 's': base64
    }
    if (isKeyed(f)) {
      return 0x6f; // 'o': an object keyed by variant NAME
    }
    if (f.isArray) {
      return 0x61; // 'a'
    }
    if (f.arms != null) {
      return 0x6f; // 'o': a union is an object with ONE key
    }
    if (f.kind == 13) {
      return 0x6f; // 'o': nested table or type
    }
    if (isEnum(f)) {
      return 0x73;
    }
    if (isFlags(f)) {
      return 0x61;
    }
    if (f.kind == 1) {
      return 0x62; // 'b'
    }
    return 0x6e; // 'n'
  }

  // the ELEMENT shape of an array field — the same classifier one level down
  static int elementShape(TableFieldInfo f) {
    if (f.kind == 13) {
      return 0x6f;
    }
    if (isEnum(f)) {
      return 0x73;
    }
    if (isFlags(f)) {
      return 0x61;
    }
    if (f.kind == 1) {
      return 0x62;
    }
    return 0x6e;
  }

  // A guarded group rides only when its guard reads true — the wire's own
  // elision (§4), carried into the text so a text and a wire written from one
  // instance say the same thing. The guard is spelled as its branch condition
  // over bool fields of the SAME type ("at_rest", "!at_rest",
  // "active && has_target"), so evaluating it is a walk of the same descriptor.
  // Nothing is inferred in the other direction: reading places every key it can
  // name, and the guard is a plain bool key (§16.2).
  static bool guardHolds(Object value, TableTypeInfo info, String guard) {
    var p = 0;
    for (;;) {
      while (p < guard.length &&
          (guard.codeUnitAt(p) == 0x20 || guard.codeUnitAt(p) == 0x26)) {
        p++;
      }
      if (p >= guard.length) {
        return true;
      }
      var want = true;
      if (guard.codeUnitAt(p) == 0x21) {
        want = false;
        p++;
      }
      final start = p;
      while (p < guard.length &&
          guard.codeUnitAt(p) != 0x20 &&
          guard.codeUnitAt(p) != 0x26) {
        p++;
      }
      final name = guard.substring(start, p);
      var held = false;
      for (var i = 0; i < info.fields.length; i++) {
        if (info.fields[i].name == name) {
          held = info.getRaw(value, i, 0) != 0;
          break;
        }
      }
      if (held != want) {
        return false;
      }
    }
  }

  // ---- writing ----

  // The writer sink MEASURES when measuring is set and WRITES when it is not,
  // over one code path — so measure and write agree byte for byte, the wire's
  // invariant (§9) carried across.
  static Uint8List _outBuffer = empty;
  static bool _outMeasuring = false;
  static int _outOffset = 0;
  static bool _outOverflow = false;

  static void outRaw(Uint8List data, int from, int length) {
    if (!_outMeasuring) {
      if (_outOffset + length > _outBuffer.length) {
        _outOverflow = true;
        return;
      }
      _outBuffer.setRange(_outOffset, _outOffset + length, data, from);
    }
    _outOffset += length;
  }

  static void put(int c) {
    if (!_outMeasuring) {
      if (_outOffset + 1 > _outBuffer.length) {
        _outOverflow = true;
        return;
      }
      _outBuffer[_outOffset] = c;
    }
    _outOffset += 1;
  }

  // an ASCII literal of the walk's own — "true", ": ", "[]" — never a value,
  // so widening each unit is the whole encoding
  static void text(String s) {
    for (var i = 0; i < s.length; i++) {
      put(s.codeUnitAt(i));
    }
  }

  static void line(int depth) {
    put(0x0a);
    for (var i = 0; i < depth; i++) {
      put(0x20);
      put(0x20);
    }
  }

  static void writeBase64(Uint8List data, int length) {
    put(0x22);
    var i = 0;
    for (; i + 3 <= length; i += 3) {
      final triple = (data[i] << 16) | (data[i + 1] << 8) | data[i + 2];
      put(base64Alphabet.codeUnitAt((triple >> 18) & 0x3f));
      put(base64Alphabet.codeUnitAt((triple >> 12) & 0x3f));
      put(base64Alphabet.codeUnitAt((triple >> 6) & 0x3f));
      put(base64Alphabet.codeUnitAt(triple & 0x3f));
    }
    if (i < length) {
      final left = length - i;
      var triple = data[i] << 16;
      if (left == 2) {
        triple |= data[i + 1] << 8;
      }
      put(base64Alphabet.codeUnitAt((triple >> 18) & 0x3f));
      put(base64Alphabet.codeUnitAt((triple >> 12) & 0x3f));
      put(left == 2 ? base64Alphabet.codeUnitAt((triple >> 6) & 0x3f) : 0x3d);
      put(0x3d);
    }
    put(0x22);
  }

  // One UTF-8 sequence at s[at], or -1 when the bytes there are not one.
  // Rejects the lot: a stray continuation, an overlong form, a surrogate half,
  // and anything past U+10FFFF. The sequence's width lands in utf8Width.
  static int utf8Width = 0;

  static int utf8At(Uint8List s, int at, int end) {
    utf8Width = 0;
    final lead = s[at];
    int want;
    int code;
    if (lead < 0x80) {
      utf8Width = 1;
      return lead;
    } else if (lead >= 0xc2 && lead <= 0xdf) {
      want = 2;
      code = lead & 0x1f;
    } else if (lead >= 0xe0 && lead <= 0xef) {
      want = 3;
      code = lead & 0x0f;
    } else if (lead >= 0xf0 && lead <= 0xf4) {
      want = 4;
      code = lead & 0x07;
    } else {
      return -1;
    }
    if (end - at < want) {
      return -1;
    }
    for (var i = 1; i < want; i++) {
      final next = s[at + i];
      if ((next & 0xc0) != 0x80) {
        return -1;
      }
      code = (code << 6) | (next & 0x3f);
    }
    if (want == 3 && code < 0x800) {
      return -1; // overlong
    }
    if (want == 4 && code < 0x10000) {
      return -1; // overlong
    }
    if (code >= 0xd800 && code <= 0xdfff) {
      return -1; // a surrogate half
    }
    if (code > 0x10ffff) {
      return -1;
    }
    utf8Width = want;
    return code;
  }

  // The escape rule, spelled once and reached from both string sources: a code
  // point the JSON grammar names, a control character as \u00XX, and anything
  // else as itself. Returns false when the caller must emit the code point's
  // own bytes instead.
  static bool writeEscape(int code) {
    switch (code) {
      case 0x22:
        text('\\"');
        return true;
      case 0x5c:
        text('\\\\');
        return true;
      case 0x08:
        text('\\b');
        return true;
      case 0x0c:
        text('\\f');
        return true;
      case 0x0a:
        text('\\n');
        return true;
      case 0x0d:
        text('\\r');
        return true;
      case 0x09:
        text('\\t');
        return true;
    }
    if (code < 0x20) {
      const hex = '0123456789abcdef';
      text('\\u00');
      put(hex.codeUnitAt(code >> 4));
      put(hex.codeUnitAt(code & 0xf));
      return true;
    }
    return false;
  }

  static void writeUtf8(int code) {
    if (code < 0x80) {
      put(code);
      return;
    }
    if (code < 0x800) {
      put(0xc0 | (code >> 6));
      put(0x80 | (code & 0x3f));
      return;
    }
    if (code < 0x10000) {
      put(0xe0 | (code >> 12));
      put(0x80 | ((code >> 6) & 0x3f));
      put(0x80 | (code & 0x3f));
      return;
    }
    put(0xf0 | (code >> 18));
    put(0x80 | ((code >> 12) & 0x3f));
    put(0x80 | ((code >> 6) & 0x3f));
    put(0x80 | (code & 0x3f));
  }

  // A JSON text MUST be valid UTF-8 (RFC 8259 §8.1). The read path is
  // byte-transparent — the wire imposes no encoding (§3) and a string may hold
  // anything — so the WRITER is where that obligation is met: a byte that is
  // not part of a well-formed sequence is written as U+FFFD, one per bad byte,
  // and never raw. The cost is stated plainly: for a string holding invalid
  // UTF-8, the round trip is NOT byte-identical, because the alternative is
  // emitting a text that is not JSON.
  static void writeString(Uint8List s, int length) {
    put(0x22);
    for (var i = 0; i < length; i++) {
      final c = s[i];
      if (writeEscape(c)) {
        continue;
      }
      if (c < 0x80) {
        put(c);
        continue;
      }
      if (utf8At(s, i, length) < 0) {
        writeUtf8(0xfffd); // one per bad byte
      } else {
        outRaw(s, i, utf8Width);
        i += utf8Width - 1;
      }
    }
    put(0x22);
  }

  // The same rule at the OTHER source: a descriptor's key or vocabulary name,
  // which Dart holds as a UTF-16 String. A surrogate pair is recombined and a
  // lone half — which a schema identifier cannot produce and a json = "key"
  // attribute could not carry either — reads as U+FFFD, the same answer the
  // byte path gives an ill-formed sequence.
  static void writeName(String s) {
    put(0x22);
    for (var i = 0; i < s.length; i++) {
      var code = s.codeUnitAt(i);
      if (code >= 0xd800 &&
          code <= 0xdbff &&
          i + 1 < s.length &&
          s.codeUnitAt(i + 1) >= 0xdc00 &&
          s.codeUnitAt(i + 1) <= 0xdfff) {
        code =
            0x10000 + ((code - 0xd800) << 10) + (s.codeUnitAt(i + 1) - 0xdc00);
        i++;
      } else if (code >= 0xd800 && code <= 0xdfff) {
        code = 0xfffd;
      }
      if (writeEscape(code)) {
        continue;
      }
      writeUtf8(code);
    }
    put(0x22);
  }

  static void writeUnsigned(int value) {
    if (value == 0) {
      put(0x30);
      return;
    }
    if (value < 0) {
      // a u64 past 2^63 rides as a negative int; its decimal form needs the
      // unsigned magnitude, which only a wider integer can name
      text(BigInt.from(value).toUnsigned(64).toString());
      return;
    }
    final digits = List<int>.filled(24, 0);
    var n = 0;
    var v = value;
    while (v != 0) {
      digits[n++] = 0x30 + v % 10;
      v = v ~/ 10;
    }
    for (var i = n - 1; i >= 0; i--) {
      put(digits[i]);
    }
  }

  static void writeSigned(int value) {
    if (value < 0) {
      put(0x2d);
      if (value == -9223372036854775807 - 1) {
        text('9223372036854775808');
        return;
      }
      writeUnsigned(-value);
      return;
    }
    writeUnsigned(value);
  }

  // ---- C's "%.*g", spelled out ----
  //
  // Dart has no printf, and its own toStringAsFixed/toStringAsExponential round
  // a TIE away from zero while C rounds it to even. So the digits are generated
  // from the double's EXACT value — every finite double is an exact decimal —
  // and rounded half-to-even, which is what the reference's snprintf does under
  // the default rounding mode. The two then agree byte for byte, which is the
  // point: one text form, nine languages.
  //
  // The cost is BigInt arithmetic per float written, and it is stated rather
  // than hidden: this is the TEXT form, not the wire, and the wire path below
  // touches none of it.

  // digitsOf writes the first prec significant decimal digits of |value|,
  // rounded half-to-even, and returns the decimal exponent of the first digit.
  static int decimalExponent = 0;

  static String significantDigits(double value, int prec) {
    final bits = tableDoubleToBits(value);
    var mantissa = bits & 0x000fffffffffffff;
    final exponent = (bits >> 52) & 0x7ff;
    var e2 = 0;
    if (exponent == 0) {
      e2 = -1074; // subnormal
    } else {
      mantissa |= 0x0010000000000000;
      e2 = exponent - 1075;
    }
    if (mantissa == 0) {
      decimalExponent = 0;
      return '0' * prec;
    }
    // the exact value is mantissa * 2^e2; find d10 = floor(log10(value)) by
    // estimate and exact correction
    var num = BigInt.from(mantissa);
    var den = BigInt.one;
    if (e2 > 0) {
      num <<= e2;
    } else {
      den <<= -e2;
    }
    var d10 = 0;
    {
      final approx = value.abs();
      d10 = (approx == 0 ? 0 : _log10Floor(approx));
      // exact correction: 10^d10 <= value < 10^(d10+1)
      while (_lessThanPower(num, den, d10)) {
        d10--;
      }
      while (!_lessThanPower(num, den, d10 + 1)) {
        d10++;
      }
    }
    // scale so the answer has exactly prec digits
    final scale = d10 - prec + 1;
    if (scale > 0) {
      den *= BigInt.from(10).pow(scale);
    } else if (scale < 0) {
      num *= BigInt.from(10).pow(-scale);
    }
    var q = num ~/ den;
    final r = num - q * den;
    final twice = r << 1;
    if (twice > den || (twice == den && q.isOdd)) {
      q += BigInt.one;
    }
    var s = q.toString();
    if (s.length > prec) {
      // the rounding carried into a new leading digit
      s = s.substring(0, prec);
      d10++;
    }
    decimalExponent = d10;
    return s;
  }

  // floor(log10(x)) for a finite positive double, as an ESTIMATE the caller
  // corrects exactly.
  static int _log10Floor(double x) {
    var e = 0;
    var v = x;
    while (v >= 10.0) {
      v /= 10.0;
      e++;
    }
    while (v < 1.0) {
      v *= 10.0;
      e--;
    }
    return e;
  }

  // num/den < 10^p, exactly.
  static bool _lessThanPower(BigInt num, BigInt den, int p) {
    if (p >= 0) {
      return num < den * BigInt.from(10).pow(p);
    }
    return num * BigInt.from(10).pow(-p) < den;
  }

  static String trimZeros(String s) {
    if (!s.contains('.')) {
      return s;
    }
    var n = s.length;
    while (n > 0 && s.codeUnitAt(n - 1) == 0x30) {
      n--;
    }
    if (n > 0 && s.codeUnitAt(n - 1) == 0x2e) {
      n--;
    }
    return s.substring(0, n);
  }

  // C's %g for a finite double at the given precision: the exponent form when
  // the decimal exponent is below -4 or at least the precision, the plain form
  // otherwise; trailing zeros and a trailing point removed, and an exponent of
  // at least two digits, always signed.
  static String formatG(double value, int prec) {
    if (prec < 1) {
      prec = 1;
    }
    final negative = value.isNegative;
    final digits = significantDigits(value.abs(), prec);
    final exp = value == 0.0 ? 0 : decimalExponent;
    final sign = negative ? '-' : '';
    if (exp < -4 || exp >= prec) {
      var mantissa = digits.substring(0, 1);
      if (digits.length > 1) {
        mantissa = '$mantissa.${digits.substring(1)}';
      }
      mantissa = trimZeros(mantissa);
      final magnitude = exp < 0 ? -exp : exp;
      final expSign = exp < 0 ? '-' : '+';
      final expText = magnitude < 10 ? '0$magnitude' : '$magnitude';
      return '$sign${mantissa}e$expSign$expText';
    }
    String plain;
    if (exp >= 0) {
      if (exp + 1 >= digits.length) {
        plain = digits + '0' * (exp + 1 - digits.length);
      } else {
        plain = '${digits.substring(0, exp + 1)}.${digits.substring(exp + 1)}';
      }
    } else {
      plain = '0.${'0' * (-exp - 1)}$digits';
    }
    return sign + trimZeros(plain);
  }

  // A float writes at the SHORTEST precision that reads back as the same value
  // at the field's own width, so a round trip is exact and a text stays
  // readable. Non-finite values have no JSON spelling at all, and the writer
  // REFUSES rather than losing one silently — the same rule measure and save
  // already apply to an enum value no variant names (§5).
  static bool writeFloat(double value, bool single) {
    if (value.isNaN || value.isInfinite) {
      return false;
    }
    final low = single ? 6 : 15;
    final high = single ? 9 : 17;
    var out = '';
    for (var digits = low; ; digits++) {
      out = formatG(value, digits);
      if (digits >= high) {
        break;
      }
      final back = double.tryParse(out);
      if (back == null) {
        continue;
      }
      if (single) {
        if (tableNarrowFloat(back) == value) {
          break;
        }
      } else if (back == value) {
        break;
      }
    }
    text(out);
    return true;
  }

  // one scalar, at one storage slot: a nested object, a union, a vocabulary, or
  // a number. C++ takes the storage's ADDRESS here; the (owner, field, index)
  // triple is the same thing said in this language.
  static bool writeScalar(
    Object owner,
    TableTypeInfo info,
    int field,
    int index,
    int depth,
  ) {
    final f = info.fields[field];
    if (f.arms != null) {
      // a union is an object with ONE key, the arm's name; None is {}
      final tag = info.getTag(owner, field);
      if (tag == 0) {
        text('{}');
        return true;
      }
      if (tag > f.enumMax || tag < 0) {
        return false; // a tag no arm names, exactly as measure refuses it
      }
      final arm = f.vocab!.nameOf(tag);
      // and refuse on the NAME, not merely on the bound: §16.2 says a value no
      // variant NAMES is refused, so the check is the name. Writing whatever
      // came back would emit "???", a spelling the reader counts as unknown —
      // a silent round-trip loss in place of a refusal.
      if (!named(arm)) {
        return false;
      }
      put(0x7b);
      line(depth + 1);
      writeName(arm);
      text(': ');
      final payload = info.armPayload(owner, field, tag);
      if (!writeValue(payload, f.arms!.arms[tag]!, depth + 1)) {
        return false;
      }
      line(depth);
      put(0x7d);
      return true;
    }
    if (f.kind == 13) {
      return writeValue(info.child(owner, field, index), f.table!, depth);
    }
    if (isEnum(f)) {
      final value = info.getRaw(owner, field, index);
      // a value no variant names has no text spelling, exactly as it has no
      // wire identity: the writer REFUSES rather than writing None over it,
      // the rule measure and save already apply (§5)
      if (value > f.enumMax || value < 0) {
        return false;
      }
      if (value != 0 && f.vocab!.idOf(value) <= 0) {
        return false;
      }
      final name = f.vocab!.nameOf(value);
      if (!named(name)) {
        return false;
      }
      writeName(name);
      return true;
    }
    if (isFlags(f)) {
      final bits = info.getRaw(owner, field, index);
      if (bits == 0) {
        text('[]');
        return true;
      }
      put(0x5b);
      var first = true;
      for (var bit = 0; bit < 64; bit++) {
        if ((bits & (1 << bit)) == 0) {
          continue;
        }
        if (bit > f.enumMax) {
          return false; // a bit no variant names has no text spelling
        }
        final name = f.vocab!.nameOf(bit);
        if (!named(name)) {
          return false;
        }
        if (!first) {
          put(0x2c);
        }
        first = false;
        line(depth + 1);
        writeName(name);
      }
      line(depth);
      put(0x5d);
      return true;
    }
    switch (f.kind) {
      case 1:
        text(info.getRaw(owner, field, index) != 0 ? 'true' : 'false');
        return true;
      case 10:
        return writeFloat(
          tableBitsToFloat(info.getRaw(owner, field, index)),
          true,
        );
      case 11:
        return writeFloat(
          tableBitsToDouble(info.getRaw(owner, field, index)),
          false,
        );
      case 2:
      case 3:
      case 4:
      case 5:
        writeSigned(info.getRaw(owner, field, index));
        return true;
      default:
        writeUnsigned(info.getRaw(owner, field, index));
        return true;
    }
  }

  static bool writeField(
    Object value,
    TableTypeInfo info,
    int field,
    int depth,
  ) {
    final f = info.fields[field];
    if (f.kind == 12) {
      writeString(info.buffer(value, field), count(value, info, field));
      return true;
    }
    if (isBytes(f)) {
      writeBase64(info.buffer(value, field), count(value, info, field));
      return true;
    }
    if (isKeyed(f)) {
      // one entry per SLOT, keyed by the variant that owns it, so inserting a
      // variant next season moves nothing in the text either. Slot i holds the
      // key i + 1: nothing is stored for None, so nothing is written for it.
      put(0x7b);
      var first = true;
      for (var slot = 0; slot < f.arrayBound; slot++) {
        if (!keyedSlotValid(f, slot)) {
          continue;
        }
        if (!first) {
          put(0x2c);
        }
        first = false;
        line(depth + 1);
        writeName(f.keyVocab!.nameOf(keyedSlotKey(slot)));
        text(': ');
        if (!writeScalar(value, info, field, slot, depth + 1)) {
          return false;
        }
      }
      if (first) {
        put(0x7d);
        return true;
      }
      line(depth);
      put(0x7d);
      return true;
    }
    if (f.isArray) {
      final n = count(value, info, field);
      if (n == 0) {
        text('[]');
        return true;
      }
      put(0x5b);
      for (var i = 0; i < n; i++) {
        if (i > 0) {
          put(0x2c);
        }
        line(depth + 1);
        if (!writeScalar(value, info, field, i, depth + 1)) {
          return false;
        }
      }
      line(depth);
      put(0x5d);
      return true;
    }
    return writeScalar(value, info, field, 0, depth);
  }

  // One instance, every field, in DECLARATION ORDER, defaults included — a text
  // is for people and tools, and a text that elides is a text a reader has to
  // know the schema to complete.
  static bool writeValue(Object value, TableTypeInfo info, int depth) {
    var any = false;
    for (var i = 0; i < info.fields.length; i++) {
      final f = info.fields[i];
      if (f.guard.isNotEmpty && !guardHolds(value, info, f.guard)) {
        continue;
      }
      // an ABSENT optional writes no key: presence of the key IS the presence
      // (§16.2), so an absent field is an absent key and nothing else would
      // read back as absent
      if (f.optional && !info.getPresent(value, i)) {
        continue;
      }
      if (!any) {
        put(0x7b);
      } else {
        put(0x2c);
      }
      any = true;
      line(depth + 1);
      writeName(f.json);
      text(': ');
      if (!writeField(value, info, i, depth + 1)) {
        return false;
      }
    }
    if (!any) {
      text('{}');
      return true;
    }
    line(depth);
    put(0x7d);
    return true;
  }

  // ---- reading ----
  //
  // The reference holds the text, the cursor and the report in one struct over
  // the bytes. Dart has no value types and no stack buffers, so the reader's
  // state is static and the per-frame key scratch is a POOL indexed by depth:
  // it grows once, to the depth a text actually reaches, and never per call.

  static Uint8List _text = empty;
  static int _pos = 0;
  static late TableReport _report;
  // the text is not JSON: the walk stops and keeps what it placed
  static bool _bad = false;
  static final List<Uint8List> _keys = <Uint8List>[];

  static Uint8List keyScratch(int depth) {
    while (_keys.length <= depth) {
      _keys.add(Uint8List(maxKey));
    }
    return _keys[depth];
  }

  static void space() {
    while (_pos < _text.length) {
      final c = _text[_pos];
      if (c == 0x20 || c == 0x09 || c == 0x0a || c == 0x0d) {
        _pos++;
        continue;
      }
      // comments are not JSON, and a walk that guessed at one would be reading
      // a dialect nobody wrote down
      if (c == 0x2f) {
        _bad = true;
      }
      return;
    }
  }

  static int peek() {
    space();
    return _pos < _text.length ? _text[_pos] : 0;
  }

  // the shape of the value sitting at the cursor, without consuming it
  static int valueShape() {
    final c = peek();
    switch (c) {
      case 0x7b:
        return 0x6f; // 'o'
      case 0x5b:
        return 0x61; // 'a'
      case 0x22:
        return 0x73; // 's'
      case 0x74:
      case 0x66:
        return 0x62; // 'b'
      case 0x6e:
        return 0x7a; // 'z': null
      case 0:
        return 0;
      default:
        return 0x6e; // 'n'
    }
  }

  static bool literal(String word) {
    if (_pos + word.length > _text.length) {
      _bad = true;
      return false;
    }
    for (var i = 0; i < word.length; i++) {
      if (_text[_pos + i] != word.codeUnitAt(i)) {
        _bad = true;
        return false;
      }
    }
    _pos += word.length;
    return true;
  }

  // one \u escape body; -1 when the four hex digits are not there
  static int hex4() {
    if (_pos + 4 > _text.length) {
      return -1;
    }
    var value = 0;
    for (var i = 0; i < 4; i++) {
      final c = _text[_pos + i];
      int digit;
      if (c >= 0x30 && c <= 0x39) {
        digit = c - 0x30;
      } else if (c >= 0x61 && c <= 0x66) {
        digit = c - 0x61 + 10;
      } else if (c >= 0x41 && c <= 0x46) {
        digit = c - 0x41 + 10;
      } else {
        return -1;
      }
      value = (value << 4) | digit;
    }
    _pos += 4;
    return value;
  }

  static final Uint8List _unit = Uint8List(4);

  static int encodeUtf8(int code) {
    if (code < 0x80) {
      _unit[0] = code;
      return 1;
    }
    if (code < 0x800) {
      _unit[0] = 0xc0 | (code >> 6);
      _unit[1] = 0x80 | (code & 0x3f);
      return 2;
    }
    if (code < 0x10000) {
      _unit[0] = 0xe0 | (code >> 12);
      _unit[1] = 0x80 | ((code >> 6) & 0x3f);
      _unit[2] = 0x80 | (code & 0x3f);
      return 3;
    }
    _unit[0] = 0xf0 | (code >> 18);
    _unit[1] = 0x80 | ((code >> 12) & 0x3f);
    _unit[2] = 0x80 | ((code >> 6) & 0x3f);
    _unit[3] = 0x80 | (code & 0x3f);
    return 4;
  }

  // Scan one JSON string into the caller's buffer. Bytes are appended ONE CODE
  // POINT AT A TIME — an escape's encoding, or a UTF-8 sequence read whole — so
  // a string longer than the field is clamped AT A CODE POINT BOUNDARY and
  // never cut through a multi-byte character. Clamping is counted, never fatal,
  // exactly as it is on the wire (§4). A null destination scans past a string
  // without keeping it, and counts no clamp for what it dropped.
  static int scanLength = 0;

  static bool scanString(Uint8List? destination, int bound) {
    scanLength = 0;
    if (peek() != 0x22) {
      _bad = true;
      return false;
    }
    _pos++;
    var placed = 0;
    var clamped = false;
    for (;;) {
      if (_pos >= _text.length) {
        _bad = true;
        return false;
      }
      final c = _text[_pos];
      if (c == 0x22) {
        _pos++;
        break;
      }
      var unitLength = 0;
      if (c == 0x5c) {
        _pos++;
        if (_pos >= _text.length) {
          _bad = true;
          return false;
        }
        final escape = _text[_pos++];
        switch (escape) {
          case 0x22:
            _unit[0] = 0x22;
            unitLength = 1;
          case 0x5c:
            _unit[0] = 0x5c;
            unitLength = 1;
          case 0x2f:
            _unit[0] = 0x2f;
            unitLength = 1;
          case 0x62:
            _unit[0] = 0x08;
            unitLength = 1;
          case 0x66:
            _unit[0] = 0x0c;
            unitLength = 1;
          case 0x6e:
            _unit[0] = 0x0a;
            unitLength = 1;
          case 0x72:
            _unit[0] = 0x0d;
            unitLength = 1;
          case 0x74:
            _unit[0] = 0x09;
            unitLength = 1;
          case 0x75:
            final high = hex4();
            if (high < 0) {
              _bad = true;
              return false;
            }
            var code = high;
            if (high >= 0xd800 &&
                high <= 0xdbff &&
                _pos + 2 <= _text.length &&
                _text[_pos] == 0x5c &&
                _text[_pos + 1] == 0x75) {
              final mark = _pos;
              _pos += 2;
              final low = hex4();
              if (low >= 0xdc00 && low <= 0xdfff) {
                code = 0x10000 + ((high - 0xd800) << 10) + (low - 0xdc00);
              } else {
                _pos = mark; // a lone lead surrogate rides as itself
              }
            }
            // a surrogate half that never found its partner has no UTF-8
            // encoding: encoding it anyway would manufacture CESU-8 — invalid
            // UTF-8 — out of input that was valid JSON, so it reads as the
            // replacement character
            if (code >= 0xd800 && code <= 0xdfff) {
              code = 0xfffd;
            }
            unitLength = encodeUtf8(code);
          default:
            _bad = true;
            return false;
        }
      } else if (c < 0x20) {
        _bad = true; // a raw control character is not a JSON string body
        return false;
      } else {
        // a UTF-8 sequence read WHOLE, so the clamp below can only land between
        // code points. Only bytes that ACTUALLY look like continuations are
        // taken: the wire imposes no encoding (§3), so a string may
        // legitimately hold a stray lead byte, and one at the end of a text
        // must not swallow the closing quote.
        var want = 1;
        if ((c & 0xe0) == 0xc0) {
          want = 2;
        } else if ((c & 0xf0) == 0xe0) {
          want = 3;
        } else if ((c & 0xf8) == 0xf0) {
          want = 4;
        }
        _unit[0] = c;
        _pos++;
        unitLength = 1;
        while (unitLength < want &&
            _pos < _text.length &&
            (_text[_pos] & 0xc0) == 0x80) {
          _unit[unitLength++] = _text[_pos++];
        }
      }
      if (destination != null) {
        if (placed + unitLength <= bound) {
          destination.setRange(placed, placed + unitLength, _unit);
          placed += unitLength;
        } else {
          clamped = true;
        }
      }
    }
    if (clamped) {
      _report.clamped++;
    }
    scanLength = placed;
    return true;
  }

  // Scan one number, to JSON's OWN grammar (RFC 8259 §6) and not to a run of
  // number-ish characters:
  //
  //     number = [ "-" ] int [ frac ] [ exp ]
  //     int    = "0" / ( digit1-9 *digit )
  //     frac   = "." 1*digit
  //     exp    = ( "e" / "E" ) [ "-" / "+" ] 1*digit
  //
  // Scanning the production is what makes a typo in an authoring file a
  // DIAGNOSTIC rather than a value: "1-2" scans as 1 and leaves "-2" where the
  // object expects a comma, so the text is malformed — which is what §16.2
  // already promises. Leading "+", leading zeros, ".5" and "3." are not JSON
  // either.
  static bool numberIntegral = false;

  static bool walkNumber() {
    space();
    numberIntegral = true;
    if (_pos < _text.length && _text[_pos] == 0x2d) {
      _pos++;
    }
    if (_pos >= _text.length) {
      return false;
    }
    if (_text[_pos] == 0x30) {
      _pos++;
    } else if (_text[_pos] >= 0x31 && _text[_pos] <= 0x39) {
      while (_pos < _text.length &&
          _text[_pos] >= 0x30 &&
          _text[_pos] <= 0x39) {
        _pos++;
      }
    } else {
      return false;
    }
    if (_pos < _text.length && _text[_pos] == 0x2e) {
      _pos++;
      if (_pos >= _text.length || _text[_pos] < 0x30 || _text[_pos] > 0x39) {
        return false;
      }
      while (_pos < _text.length &&
          _text[_pos] >= 0x30 &&
          _text[_pos] <= 0x39) {
        _pos++;
      }
      numberIntegral = false;
    }
    if (_pos < _text.length && (_text[_pos] == 0x65 || _text[_pos] == 0x45)) {
      _pos++;
      if (_pos < _text.length && (_text[_pos] == 0x2d || _text[_pos] == 0x2b)) {
        _pos++;
      }
      if (_pos >= _text.length || _text[_pos] < 0x30 || _text[_pos] > 0x39) {
        return false;
      }
      while (_pos < _text.length &&
          _text[_pos] >= 0x30 &&
          _text[_pos] <= 0x39) {
        _pos++;
      }
      numberIntegral = false;
    }
    return true;
  }

  // the same production, with the token kept for conversion. The token is ASCII
  // by that grammar, so a String of it needs no decoder.
  static String numberToken = '';

  static bool scanNumber() {
    numberToken = '';
    space();
    final start = _pos;
    if (!walkNumber()) {
      return false;
    }
    final n = _pos - start;
    if (n <= 0 || n >= maxNumber) {
      return false;
    }
    numberToken = String.fromCharCodes(_text, start, _pos);
    return true;
  }

  // the token's exact double, through the runtime's own converter. A float64
  // field is done here: double.parse is correctly rounded, so the nearest
  // double to the token IS the answer.
  static double tokenDouble(String token, bool single) {
    final value = double.tryParse(token);
    if (value == null) {
      return double.nan;
    }
    return single ? tokenFloat32(token, value) : value;
  }

  // ---- the token's nearest float32, exactly ----
  //
  // Narrowing the nearest DOUBLE would round twice, and the second rounding can
  // go the wrong way: the nearest double to a decimal just under a float32
  // midpoint can BE that midpoint, and narrowing a midpoint ties to even. At
  // the top of the range that turns FLT_MAX into an infinity — a value the walk
  // would then count as the wrong shape for the field, out of a text that names
  // a perfectly good float32. C# has a float parser and needs none of this;
  // Dart has one width, so the exact decimal decides among the float32
  // candidates the double lands between.
  static double tokenFloat32(String token, double approx) {
    if (approx.isNaN) {
      return approx;
    }
    final negative = approx.isNegative;
    final magnitude = approx.abs();
    final narrowed = tableNarrowFloat(magnitude);
    var bits = narrowed.isInfinite ? 0x7f7fffff : tableFloatToBits(narrowed);
    if (bits >= 0x7f800000) {
      bits = 0x7f7fffff;
    }
    // walk to the largest float32 whose value is at most the token's
    while (bits > 0 && _cmpToken(token, bits) < 0) {
      bits--;
    }
    while (bits < 0x7f7fffff && _cmpToken(token, bits + 1) >= 0) {
      bits++;
    }
    // then the midpoint above decides between it and its successor
    final cmp = _cmpTokenMidpoint(token, bits);
    var chosen = bits;
    if (cmp > 0) {
      chosen = bits + 1;
    } else if (cmp == 0 && (bits & 1) != 0) {
      chosen = bits + 1; // ties to even, on the float32's own mantissa
    }
    if (chosen >= 0x7f800000) {
      return negative ? double.negativeInfinity : double.infinity;
    }
    final value = tableBitsToFloat(chosen);
    return negative ? -value : value;
  }

  // the (mantissa, exponent) a float32 bit pattern names: value = m * 2^e.
  static BigInt _f32Mantissa(int bits) {
    final e = (bits >> 23) & 0xff;
    final m = bits & 0x7fffff;
    return BigInt.from(e == 0 ? m : m + 0x800000);
  }

  static int _f32Exponent(int bits) {
    final e = (bits >> 23) & 0xff;
    return e == 0 ? -149 : e - 150;
  }

  // |token| against m * 2^e2, exactly: -1 below, 0 equal, 1 above.
  static int _cmpExact(String token, BigInt m, int e2) {
    // the token's own decimal: an integer D and a power of ten
    var digits = BigInt.zero;
    var tenth = 0;
    var exponent = 0;
    var i = 0;
    if (i < token.length &&
        (token.codeUnitAt(i) == 0x2d || token.codeUnitAt(i) == 0x2b)) {
      i++;
    }
    var afterPoint = false;
    for (; i < token.length; i++) {
      final c = token.codeUnitAt(i);
      if (c == 0x2e) {
        afterPoint = true;
        continue;
      }
      if (c == 0x65 || c == 0x45) {
        exponent = int.parse(token.substring(i + 1));
        break;
      }
      digits = digits * BigInt.from(10) + BigInt.from(c - 0x30);
      if (afterPoint) {
        tenth++;
      }
    }
    exponent -= tenth;
    var left = digits;
    var right = m;
    if (exponent > 0) {
      left *= BigInt.from(10).pow(exponent);
    } else if (exponent < 0) {
      right *= BigInt.from(10).pow(-exponent);
    }
    if (e2 > 0) {
      right <<= e2;
    } else if (e2 < 0) {
      left <<= -e2;
    }
    return left.compareTo(right);
  }

  // |token| against the value a float32 bit pattern names.
  static int _cmpToken(String token, int bits) =>
      _cmpExact(token, _f32Mantissa(bits), _f32Exponent(bits));

  // |token| against the midpoint between a float32 and its successor, which is
  // (2m + 1) * 2^(e - 1) — exact whatever the successor is, the infinity
  // included.
  static int _cmpTokenMidpoint(String token, int bits) => _cmpExact(
    token,
    _f32Mantissa(bits) * BigInt.two + BigInt.one,
    _f32Exponent(bits) - 1,
  );

  // the token's exact integer, parsed digit by digit so no width can move it.
  // Saturation is reported as a clamp, the wire's rule for a value outside what
  // the reader can hold (§4).
  static bool tokenSaturated = false;

  static int tokenInteger(String token, bool isSigned) {
    var i = 0;
    var negative = false;
    if (i < token.length &&
        (token.codeUnitAt(i) == 0x2d || token.codeUnitAt(i) == 0x2b)) {
      negative = token.codeUnitAt(i) == 0x2d;
      i++;
    }
    var magnitude = BigInt.zero;
    var over = false;
    for (; i < token.length; i++) {
      magnitude =
          magnitude * BigInt.from(10) + BigInt.from(token.codeUnitAt(i) - 0x30);
      if (magnitude > _maxUnsigned) {
        over = true;
        break;
      }
    }
    if (!isSigned) {
      // -0 IS zero, and clamping it would report an event that did not happen;
      // only a real negative magnitude is out of range here
      if (negative) {
        tokenSaturated = magnitude != BigInt.zero;
        return 0;
      }
      if (over) {
        tokenSaturated = true;
        return -1; // the bit pattern of 2^64 - 1
      }
      tokenSaturated = false;
      return magnitude.toUnsigned(64).toInt();
    }
    if (negative) {
      if (over || magnitude > _minSignedMagnitude) {
        tokenSaturated = true;
        return -9223372036854775807 - 1;
      }
      tokenSaturated = false;
      if (magnitude == _minSignedMagnitude) {
        return -9223372036854775807 - 1;
      }
      return -magnitude.toInt();
    }
    if (over || magnitude > _maxSigned) {
      tokenSaturated = true;
      return 9223372036854775807;
    }
    tokenSaturated = false;
    return magnitude.toInt();
  }

  static final BigInt _maxUnsigned = BigInt.parse('18446744073709551615');
  static final BigInt _maxSigned = BigInt.parse('9223372036854775807');
  static final BigInt _minSignedMagnitude = BigInt.parse('9223372036854775808');

  static bool skipContainer(int close, int depth) {
    if (depth > maxDepth) {
      _bad = true;
      return false;
    }
    _pos++; // the opening bracket
    for (;;) {
      var c = peek();
      if (c == close) {
        _pos++;
        return true;
      }
      if (c == 0) {
        _bad = true;
        return false;
      }
      if (close == 0x7d) {
        if (!scanString(null, 0)) {
          return false;
        }
        if (peek() != 0x3a) {
          _bad = true;
          return false;
        }
        _pos++;
      }
      if (!skipValue(depth + 1)) {
        return false;
      }
      c = peek();
      if (c == 0x2c) {
        _pos++; // a trailing comma is accepted
        continue;
      }
      if (c == close) {
        _pos++;
        return true;
      }
      _bad = true;
      return false;
    }
  }

  static bool skipValue(int depth) {
    final c = peek();
    switch (c) {
      case 0x7b:
        return skipContainer(0x7d, depth);
      case 0x5b:
        return skipContainer(0x5d, depth);
      case 0x22:
        return scanString(null, 0);
      case 0x74:
        return literal('true');
      case 0x66:
        return literal('false');
      case 0x6e:
        return literal('null');
      case 0:
        _bad = true;
        return false;
      default:
        // consumed, never converted: skipping needs no buffer, and this is the
        // one walk a hostile text drives to the depth cap. It is the SAME
        // production the value path scans, so an unknown key cannot smuggle
        // past a number a named key would refuse.
        if (!walkNumber()) {
          _bad = true;
          return false;
        }
        return true;
    }
  }

  // compare a scanned UTF-8 key against a descriptor's string. Schema
  // identifiers are ASCII, so the common case is a byte walk; a json = "key"
  // that is not falls to the encoder.
  static bool same(Uint8List scanned, int length, String name) {
    for (var i = 0; i < name.length; i++) {
      final c = name.codeUnitAt(i);
      if (c >= 0x80) {
        return sameEncoded(scanned, length, name);
      }
      if (i >= length || scanned[i] != c) {
        return false;
      }
    }
    return name.length == length;
  }

  static bool sameEncoded(Uint8List scanned, int length, String name) {
    var n = 0;
    for (final code in name.runes) {
      final width = encodeUtf8(code);
      for (var i = 0; i < width; i++) {
        if (n >= length || scanned[n] != _unit[i]) {
          return false;
        }
        n++;
      }
    }
    return n == length;
  }

  // place one scalar at one storage slot
  static bool readScalar(
    Object owner,
    TableTypeInfo info,
    int field,
    int index,
    int depth,
  ) {
    final f = info.fields[field];
    if (f.arms != null) {
      // a union is an object with ONE key, the arm's name; {} is None, and two
      // keys is a text this walk will not guess at
      if (peek() != 0x7b) {
        _bad = true;
        return false;
      }
      _pos++;
      info.setTag(owner, field, 0);
      if (peek() == 0x7d) {
        _pos++;
        return true;
      }
      final key = keyScratch(depth);
      if (!scanString(key, maxKey)) {
        return false;
      }
      final keyLength = scanLength;
      if (peek() != 0x3a) {
        _bad = true;
        return false;
      }
      _pos++;
      var tag = 0;
      for (var t = 1; t <= f.enumMax; t++) {
        if (same(key, keyLength, f.vocab!.nameOf(t))) {
          tag = t;
          break;
        }
      }
      if (tag == 0) {
        _report.unknown++;
        if (!skipValue(depth + 1)) {
          return false;
        }
      } else {
        final payload = info.armPayload(owner, field, tag);
        final arm = f.arms!.arms[tag]!;
        arm.reset(payload);
        if (!readTable(payload, arm, depth + 1)) {
          return false;
        }
        info.setTag(owner, field, tag);
      }
      var c = peek();
      if (c == 0x2c) {
        _pos++;
        c = peek();
      }
      if (c == 0x7d) {
        _pos++;
        return true;
      }
      _bad = true; // a second key: a one-of with two arms is not a value
      return false;
    }
    if (f.kind == 13) {
      final child = info.child(owner, field, index);
      f.table!.reset(child);
      return readTable(child, f.table!, depth + 1);
    }
    if (isEnum(f)) {
      final name = keyScratch(depth);
      if (!scanString(name, maxKey)) {
        return false;
      }
      final nameLength = scanLength;
      for (var v = 0; v <= f.enumMax; v++) {
        if (same(name, nameLength, f.vocab!.nameOf(v))) {
          info.setRaw(owner, field, index, v);
          return true;
        }
      }
      // a name this build cannot name reads as None and counts as unknown,
      // exactly as an unknown variant id does on the wire (§4)
      info.setRaw(owner, field, index, 0);
      _report.unknown++;
      return true;
    }
    if (isFlags(f)) {
      if (peek() != 0x5b) {
        _bad = true;
        return false;
      }
      _pos++;
      var bits = 0;
      for (;;) {
        var c = peek();
        if (c == 0x5d) {
          _pos++;
          break;
        }
        if (c == 0) {
          _bad = true;
          return false;
        }
        if (c != 0x22) {
          _report.kindMismatch++;
          if (!skipValue(depth + 1)) {
            return false;
          }
        } else {
          final name = keyScratch(depth);
          if (!scanString(name, maxKey)) {
            return false;
          }
          final nameLength = scanLength;
          var found = false;
          for (var bit = 0; bit <= f.enumMax; bit++) {
            if (same(name, nameLength, f.vocab!.nameOf(bit))) {
              bits |= 1 << bit;
              found = true;
              break;
            }
          }
          if (!found) {
            _report.unknown++;
          }
        }
        c = peek();
        if (c == 0x2c) {
          _pos++;
          continue;
        }
        if (c == 0x5d) {
          _pos++;
          break;
        }
        _bad = true;
        return false;
      }
      info.setRaw(owner, field, index, bits);
      return true;
    }
    if (f.kind == 1) {
      final b = peek();
      if (b == 0x74) {
        if (!literal('true')) {
          return false;
        }
        info.setRaw(owner, field, index, 1);
        return true;
      }
      if (!literal('false')) {
        return false;
      }
      info.setRaw(owner, field, index, 0);
      return true;
    }
    if (!scanNumber()) {
      _bad = true;
      return false;
    }
    final token = numberToken;
    final integral = numberIntegral;
    if (f.kind == 10 || f.kind == 11) {
      final single = f.kind == 10;
      var value = tokenDouble(token, single);
      // A magnitude the field's format cannot hold is the WRONG SHAPE for the
      // kind, and it never reaches storage: 1e400 is not a float64 and 1e300 is
      // not a float32. Storing the infinity the conversion produced would leave
      // an instance this walk called CLEAN that ToJsonMeasure then refuses
      // forever (a non-finite float has no JSON spelling), and §16.1's one
      // invariant is that a text which reads clean writes back.
      if (value.isNaN || value.isInfinite) {
        _report.kindMismatch++;
        return true;
      }
      if (f.hasRange) {
        if (value < f.rangeMin) {
          value = f.rangeMin;
          _report.clamped++;
        } else if (value > f.rangeMax) {
          value = f.rangeMax;
          _report.clamped++;
        }
      }
      if (single) {
        final narrow = tableNarrowFloat(value);
        if (narrow.isNaN || narrow.isInfinite) {
          _report.kindMismatch++;
          return true;
        }
        info.setRaw(owner, field, index, tableFloatToBits(narrow));
      } else {
        info.setRaw(owner, field, index, tableDoubleToBits(value));
      }
      return true;
    }
    // JSON HAS ONE NUMBER TYPE. 2.0 IS the integer 2 and 1e3 IS 1000, and a
    // library that round-trips numbers through a double emits them that way —
    // this walker's own float writer emits 1e+21. So an integer field takes any
    // number whose VALUE is integral, however it was spelled; only a genuinely
    // fractional value is the wrong shape for it.
    final isSigned = f.kind >= 2 && f.kind <= 5;
    var saturated = false;
    var placed = 0;
    if (integral) {
      placed = tokenInteger(token, isSigned);
      saturated = tokenSaturated;
    } else {
      final d = tokenDouble(token, false);
      if (d.isNaN || d.isInfinite) {
        _report.kindMismatch++;
        return true;
      }
      if (isSigned) {
        if (d >= 9223372036854775808.0) {
          placed = 9223372036854775807;
          saturated = true;
        } else if (d < -9223372036854775808.0) {
          placed = -9223372036854775807 - 1;
          saturated = true;
        } else if (d != d.truncateToDouble()) {
          _report.kindMismatch++;
          return true;
        } else {
          placed = d.toInt();
        }
      } else {
        if (d < 0.0) {
          // a negative for an unsigned field clamps to zero, as the exact digit
          // path already does
          if (d != d.truncateToDouble()) {
            _report.kindMismatch++;
            return true;
          }
          placed = 0;
          saturated = true;
        } else if (d >= 18446744073709551616.0) {
          placed = -1; // the bit pattern of 2^64 - 1
          saturated = true;
        } else if (d != d.truncateToDouble()) {
          _report.kindMismatch++;
          return true;
        } else {
          placed = BigInt.from(d).toUnsigned(64).toInt();
        }
      }
    }
    if (saturated) {
      _report.clamped++;
    }
    if (f.hasRange) {
      if (placed.toDouble() < f.rangeMin) {
        placed = f.rangeMin.toInt();
        _report.clamped++;
      } else if (placed.toDouble() > f.rangeMax) {
        placed = f.rangeMax.toInt();
        _report.clamped++;
      }
    }
    // the field's own storage width is the last bound: a value past it clamps
    // rather than wrapping, which is what the wire does too
    if (f.elemWidth < 8 && f.elemWidth > 0) {
      if (isSigned) {
        final high = (1 << (f.elemWidth * 8 - 1)) - 1;
        final low = -high - 1;
        if (placed > high) {
          placed = high;
          _report.clamped++;
        } else if (placed < low) {
          placed = low;
          _report.clamped++;
        }
      } else {
        final high = (1 << (f.elemWidth * 8)) - 1;
        if (placed < 0) {
          placed = 0;
          _report.clamped++;
        } else if (placed > high) {
          placed = high;
          _report.clamped++;
        }
      }
    }
    // at eight bytes the storage IS the parser's width, and an unsigned value
    // past the signed top rides here as a negative int by design — the token
    // parser already turned a NEGATIVE token for an unsigned field into a
    // clamped zero, so there is nothing left to bound.
    info.setRaw(owner, field, index, placed);
    return true;
  }

  // put one array field's every slot back at its declared defaults. A table
  // element's defaults are its own (the reset hook); every other element kind's
  // storage default is zero. There is no union arm here because an ARRAY OF
  // UNIONS is refused by name (docs/SPEC-TABLES.md §11).
  static void resetSlots(Object value, TableTypeInfo info, int field) {
    final f = info.fields[field];
    for (var i = 0; i < f.arrayBound; i++) {
      if (f.kind == 13) {
        f.table!.reset(info.child(value, field, i));
      } else {
        info.setRaw(value, field, i, 0);
      }
    }
  }

  static bool readField(
    Object value,
    TableTypeInfo info,
    int field,
    int depth,
  ) {
    final f = info.fields[field];
    if (f.kind == 12) {
      final storage = info.buffer(value, field);
      if (!scanString(storage, f.arrayBound)) {
        return false;
      }
      putCount(value, info, field, scanLength);
      return true;
    }
    if (isBytes(f)) {
      // base64 decodes STRAIGHT INTO the field's storage, six bits at a time —
      // no window, no temporary, so a bytes(N) of any declared extent reads the
      // same way. A base64 body carries no escapes, so a backslash in one is
      // simply not an alphabet character.
      if (peek() != 0x22) {
        _bad = true;
        return false;
      }
      _pos++;
      final storage = info.buffer(value, field);
      storage.fillRange(0, f.arrayBound, 0);
      putCount(value, info, field, 0);
      var placed = 0;
      var accumulator = 0;
      var held = 0;
      var clamped = false;
      var malformed = false;
      for (;;) {
        if (_pos >= _text.length) {
          _bad = true;
          return false;
        }
        final c = _text[_pos++];
        if (c == 0x22) {
          break;
        }
        if (c == 0x3d || malformed) {
          continue;
        }
        final at = base64Alphabet.indexOf(String.fromCharCode(c));
        if (at < 0) {
          malformed = true;
          continue;
        }
        accumulator = (accumulator << 6) | at;
        held += 6;
        if (held >= 8) {
          held -= 8;
          if (placed < f.arrayBound) {
            storage[placed++] = (accumulator >> held) & 0xff;
          } else {
            clamped = true;
          }
        }
      }
      if (malformed) {
        // a body that is not base64 is the wrong shape for the kind: the field
        // keeps its default and the event is counted
        _report.kindMismatch++;
        return true;
      }
      if (clamped) {
        _report.clamped++;
      }
      putCount(value, info, field, placed);
      return true;
    }
    if (isKeyed(f)) {
      if (peek() != 0x7b) {
        _bad = true;
        return false;
      }
      _pos++;
      // every slot back to its declared defaults first, so a key the text omits
      // keeps them and a repeated field key cannot leave an earlier
      // occurrence's slots standing
      resetSlots(value, info, field);
      final want = elementShape(f);
      // A KEYED OBJECT'S KEYS ARE KEYS: a variant named twice is a duplicate
      // key like any other, last-wins and counted (§16.2). Tracked the way a
      // table's own field keys are — a bounded, allocation-free bitmask; a
      // vocabulary wider than this still reads, its repeats simply stop being
      // counted.
      var seen = 0;
      for (;;) {
        var c = peek();
        if (c == 0x7d) {
          _pos++;
          break;
        }
        if (c == 0) {
          _bad = true;
          return false;
        }
        final key = keyScratch(depth);
        if (!scanString(key, maxKey)) {
          return false;
        }
        final keyLength = scanLength;
        if (peek() != 0x3a) {
          _bad = true;
          return false;
        }
        _pos++;
        var slot = -1;
        for (var v = 0; v < f.arrayBound; v++) {
          // nothing is stored for None, so "None" finds no slot and is an
          // unknown key like any other name this reader cannot place
          if (!keyedSlotValid(f, v)) {
            continue;
          }
          if (same(key, keyLength, f.keyVocab!.nameOf(keyedSlotKey(v)))) {
            slot = v;
            break;
          }
        }
        if (slot >= 0 && slot < 63) {
          final bit = 1 << slot;
          if ((seen & bit) != 0) {
            _report.duplicate++;
          }
          seen |= bit;
        }
        if (slot < 0) {
          _report.unknown++;
          if (!skipValue(depth + 1)) {
            return false;
          }
        } else if (valueShape() != want) {
          _report.kindMismatch++;
          if (!skipValue(depth + 1)) {
            return false;
          }
        } else if (!readScalar(value, info, field, slot, depth + 1)) {
          return false;
        }
        c = peek();
        if (c == 0x2c) {
          _pos++; // a trailing comma is accepted
          continue;
        }
        if (c == 0x7d) {
          _pos++;
          break;
        }
        _bad = true;
        return false;
      }
      return true;
    }
    if (f.isArray) {
      if (peek() != 0x5b) {
        _bad = true;
        return false;
      }
      _pos++;
      // LAST WINS has to be true of a repeated ARRAY key too, and it is
      // wire-visible: a fixed array writes every slot, so a second, shorter
      // occurrence overlaying a prefix would leave the first occurrence's tail
      // standing. The field goes back to its declared defaults before this
      // occurrence's elements are placed.
      resetSlots(value, info, field);
      putCount(value, info, field, 0);
      var placed = 0;
      final want = elementShape(f);
      for (;;) {
        var c = peek();
        if (c == 0x5d) {
          _pos++;
          break;
        }
        if (c == 0) {
          _bad = true;
          return false;
        }
        if (placed >= f.arrayBound) {
          // more elements than the reader's bound: the bounded prefix is kept
          // and the excess counts, the wire's rule (§4)
          _report.clamped++;
          if (!skipValue(depth + 1)) {
            return false;
          }
        } else if (valueShape() != want) {
          _report.kindMismatch++;
          if (!skipValue(depth + 1)) {
            return false;
          }
          placed++;
        } else {
          if (!readScalar(value, info, field, placed, depth + 1)) {
            return false;
          }
          placed++;
        }
        c = peek();
        if (c == 0x2c) {
          _pos++;
          continue;
        }
        if (c == 0x5d) {
          _pos++;
          break;
        }
        _bad = true;
        return false;
      }
      // a fixed array's tail keeps the defaults the prefill left there, exactly
      // as a short wire count does
      putCount(value, info, field, placed);
      return true;
    }
    return readScalar(value, info, field, 0, depth);
  }

  // ONE table object: keys are field keys, unknown ones are skipped and
  // counted, a repeated key is last-wins and counted. The instance is already
  // at its declared defaults when this is entered, so a key the text never
  // mentions keeps the default an absent field takes on the wire (§4).
  static bool readTable(Object value, TableTypeInfo info, int depth) {
    if (depth > maxDepth) {
      _bad = true;
      return false;
    }
    if (peek() != 0x7b) {
      _bad = true;
      return false;
    }
    _pos++;
    // duplicate tracking, bounded and allocation-free: a table with more fields
    // than this still reads, its repeats simply stop being counted
    var seen = 0;
    for (;;) {
      var c = peek();
      if (c == 0x7d) {
        _pos++;
        return true;
      }
      if (c == 0) {
        _bad = true;
        return false;
      }
      final key = keyScratch(depth);
      if (!scanString(key, maxKey)) {
        return false;
      }
      final keyLength = scanLength;
      if (peek() != 0x3a) {
        _bad = true;
        return false;
      }
      _pos++;
      var index = -1;
      for (var i = 0; i < info.fields.length; i++) {
        if (same(key, keyLength, info.fields[i].json)) {
          index = i;
          break;
        }
      }
      if (keyLength > 0 && key[0] == 0x26) {
        // THE AMPERSAND PREFIX IS RESERVED TO THE FORM (docs/SPEC-TABLES.md
        // §16.7): never a field this build lacks, always a construct it
        // cannot honor. Malformed and refused, never counted as unknown.
        _report.malformed = true;
        _bad = true;
        return false;
      }
      if (index < 0) {
        _report.unknown++;
        if (!skipValue(depth + 1)) {
          return false;
        }
      } else {
        final f = info.fields[index];
        if (index < 63) {
          final bit = 1 << index;
          if ((seen & bit) != 0) {
            _report.duplicate++;
          }
          seen |= bit;
        }
        // PRESENCE OF THE KEY IS THE PRESENCE (§16.2): reaching this line is
        // the key being present, so an optional is set present whatever its
        // value — with one exception the page names: a JSON null, which reads
        // as ABSENT rather than as a value.
        final got = valueShape();
        if (f.optional && got == 0x7a) {
          if (!literal('null')) {
            return false;
          }
          // absent, and back at its defaults: a repeated key whose last
          // occurrence is null must not leave an earlier value standing
          if (f.table != null) {
            f.table!.reset(info.child(value, index, 0));
          } else {
            info.setRaw(value, index, 0, 0);
          }
          info.setPresent(value, index, false);
        } else {
          if (got != shape(f)) {
            // the wrong JSON type for the kind: skipped, never coerced
            _report.kindMismatch++;
            if (!skipValue(depth + 1)) {
              return false;
            }
          } else if (!readField(value, info, index, depth)) {
            return false;
          }
          if (f.optional) {
            info.setPresent(value, index, true);
          }
        }
      }
      c = peek();
      if (c == 0x2c) {
        _pos++; // a trailing comma is accepted
        continue;
      }
      if (c == 0x7d) {
        _pos++;
        return true;
      }
      _bad = true;
      return false;
    }
  }

  // ---- the two entry points the per-table wrappers name ----

  static bool read(
    Object value,
    TableTypeInfo info,
    Uint8List text,
    TableReport report,
  ) {
    _text = text;
    _pos = 0;
    _report = report;
    _bad = false;
    info.reset(value);
    final ok = readTable(value, info, 0);
    if (ok) {
      // the canonical text ends with ONE newline and a text without one is the
      // same text: whitespace after the object is skipped either way, and
      // anything else is trailing rubbish rather than one text
      space();
      if (_pos != _text.length) {
        _bad = true;
      }
    }
    _text = empty;
    if (_bad || !ok) {
      report.malformed = true;
      return false;
    }
    return true;
  }

  static int write(
    Object value,
    TableTypeInfo info,
    Uint8List buffer,
    bool measuring,
  ) {
    _outBuffer = buffer;
    _outMeasuring = measuring;
    _outOffset = 0;
    _outOverflow = false;
    if (!writeValue(value, info, 0)) {
      _outBuffer = empty;
      return -1;
    }
    // THE CANONICAL TEXT ENDS WITH EXACTLY ONE NEWLINE (§16.1). Every writer
    // emits it — this one, the C++ walk and schema unpack — and every reader
    // accepts a text with or without.
    put(0x0a);
    final n = _outOffset;
    final overflow = _outOverflow;
    _outBuffer = empty;
    return overflow ? -1 : n;
  }
}
// ---- json walk: end ----
`
