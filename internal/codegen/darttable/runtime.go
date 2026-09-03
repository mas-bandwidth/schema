// The unit's shared Dart table runtime (docs/SPEC-TABLES.md §3, §4): the report,
// the writer, the reader, the enum vocabulary and the keyed-array storage.
//
// It is emitted ONCE per unit, into <Package>Table.dart, and every other
// <Base>Table.dart of the unit imports it. Dart's privacy is per LIBRARY and a
// generated file is a library, so a runtime shared across files has to be
// public — which is why every spelling here is registered in
// internal/tablenames and claimed by the front end (§11).
//
// THE READER AND THE WRITER ARE OBJECTS THE CALLER MAY OWN. C++ puts them on
// the stack and C# spells them ref structs; Dart has neither, so the two
// classes carry an `attach` that re-points an existing instance at a new
// buffer. A caller that owns one — the soak does, and so does any hot loop —
// allocates nothing per iteration; the convenience entry points
// (<name>Save, <name>Load) allocate exactly one reader or writer and the
// ByteData view over the caller's bytes, and nothing per FIELD either way.
//
// A NESTED BODY IS A LIMIT, NOT A SLICE. C++ and C# hand an inner decode its
// own view of the bytes, which costs nothing in either language; a Dart
// sublist view is an allocation, so the reader carries `limit` — the exclusive
// end of the body being read — and a nested read saves it, narrows it, and
// restores it. The property that matters is the same one: an inner decode can
// never reach past its own framing, because every `has` is against `limit`.
package darttable

import "strings"

// tableRuntimeSource is the runtime's text. It does not vary with the unit
// except in two places: the per-enum vocabularies, which are static members of
// TableEnumVocab, and the keyed-array storage, which is emitted only into a
// unit that declares one (the same rule C++ and C# hold).
func tableRuntimeSource(anyKeyed bool, vocabularies string) string {
	s := strings.Replace(tableRuntimeCore, vocabularyMark+"\n", vocabularies, 1)
	if anyKeyed {
		s += tableKeyedSource
	}
	return s + tableJsonSource
}

// vocabularyMark is where emitEnumVocabularies splices its static members into
// TableEnumVocab's body.
const vocabularyMark = "  // VOCABULARIES"

const tableRuntimeCore = `
// The table-wire read report — the permissive contract's ledger. Silence (all
// zero) means the data matched this reader's schema exactly.
final class TableReport {
  int unknown = 0; // unknown field ids skipped (newer data)
  int kindMismatch = 0; // known id, changed type — skipped, never misdecoded
  int clamped = 0; // out-of-range values clamped to declared bounds

  // duplicate is the TEXT FORM's counter and the WIRE NEVER RAISES IT
  // (docs/SPEC-TABLES.md §4, §16.2): a body carrying an id twice is legal input
  // whose last occurrence wins, silently. It rides on this class because a
  // caller has one report type, not two — so a wire read always leaves it
  // zero, and <name>FromJson is what raises it.
  int duplicate = 0;
  bool malformed = false; // framing damage; decode stopped, partial result kept

  // Back to silence, reusing this instance. A caller in a loop owns one
  // report and clears it; nothing here allocates.
  void clear() {
    unknown = 0;
    kindMismatch = 0;
    clamped = 0;
    duplicate = 0;
    malformed = false;
  }
}

// One enum's TABLE-WIRE vocabulary (docs/SPEC-TABLES.md §5): a value rides as the
// u16 hash of its VARIANT NAME, so a variant may be added anywhere, removed,
// or reordered and old data still reads. None is the one reserved id, 0.
//
// The per-enum vocabularies are static const members of this class, named by
// the enum. They are members rather than library-scope constants precisely so
// they claim no name a schema could declare: one registered spelling covers
// every enum in the unit.
final class TableEnumVocab {
  // variantIds[v] is the wire id of value v; index 0 is None's reserved 0.
  // The list's length is the count of NAMEABLE values: a "| max = K" widening
  // leaves wire-legal values above it that no variant names.
  final List<int> variantIds;
  final List<String> variantNames;

  const TableEnumVocab(this.variantIds, this.variantNames);

  // The wire id of a value, or -1 when no variant names it — which is what
  // makes an unnameable value unwritable rather than silently zero.
  int idOf(int value) =>
      value >= 0 && value < variantIds.length ? variantIds[value] : -1;

  // The value an id names, or -1 when this build cannot name it. Linear over
  // the vocabulary, which is a handful of entries; a map would allocate at
  // load and buy nothing at this size.
  int valueOf(int id) {
    for (var v = 0; v < variantIds.length; v++) {
      if (variantIds[v] == id) {
        return v;
      }
    }
    return -1;
  }

  String nameOf(int value) =>
      value >= 0 && value < variantNames.length ? variantNames[value] : '???';
  // VOCABULARIES
}

// TableWriter writes the wire into the caller's bytes, in place. Nothing here
// allocates: the buffer and this object are the caller's.
//
// ITS CURRENCY IS THE Uint8List, AND NOTHING ELSE. A multi-byte scalar is
// assembled from bytes rather than stored through a ByteData, because a
// ByteData is a SECOND OBJECT describing the same memory: lending both to
// attach is two arguments for one fact and a pair a caller can drift apart,
// and deriving either from the other is an allocation on the very path whose
// floor is zero. Byte assembly costs about a nanosecond a scalar beside the
// ByteData intrinsics, measured in both builds, and setRange — a memmove —
// stays available for a string(N) and a bytes(N).
final class TableWriter {
  Uint8List bytes;
  int offset = 0;
  bool overflow = false;

  TableWriter(this.bytes);

  // Re-point this writer at another buffer, reusing the instance — the entry a
  // hot loop uses, and the one that allocates nothing at all.
  void attach(Uint8List buffer) {
    bytes = buffer;
    offset = 0;
    overflow = false;
  }

  // Copy length bytes of data from an offset: a memmove. Every caller is a
  // string(N) or a bytes(N) bounded by its declared size.
  void raw(Uint8List data, int from, int length) {
    if (offset + length > bytes.length) {
      overflow = true;
      return;
    }
    bytes.setRange(offset, offset + length, data, from);
    offset += length;
  }

  // A typed-data store keeps the low eight bits, so no masking rides here.
  void put8(int v) {
    if (offset + 1 > bytes.length) {
      overflow = true;
      return;
    }
    bytes[offset] = v;
    offset += 1;
  }

  void put16(int v) {
    if (offset + 2 > bytes.length) {
      overflow = true;
      return;
    }
    final b = bytes;
    final o = offset;
    b[o] = v;
    b[o + 1] = v >> 8;
    offset = o + 2;
  }

  void put32(int v) {
    if (offset + 4 > bytes.length) {
      overflow = true;
      return;
    }
    final b = bytes;
    final o = offset;
    b[o] = v;
    b[o + 1] = v >> 8;
    b[o + 2] = v >> 16;
    b[o + 3] = v >> 24;
    offset = o + 4;
  }

  void put64(int v) {
    if (offset + 8 > bytes.length) {
      overflow = true;
      return;
    }
    final b = bytes;
    final o = offset;
    b[o] = v;
    b[o + 1] = v >> 8;
    b[o + 2] = v >> 16;
    b[o + 3] = v >> 24;
    b[o + 4] = v >> 32;
    b[o + 5] = v >> 40;
    b[o + 6] = v >> 48;
    b[o + 7] = v >> 56;
    offset = o + 8;
  }

  void patch32(int at, int v) {
    if (at + 4 > bytes.length) {
      overflow = true;
      return;
    }
    final b = bytes;
    b[at] = v;
    b[at + 1] = v >> 8;
    b[at + 2] = v >> 16;
    b[at + 3] = v >> 24;
  }
}

// TableReader reads the wire out of the caller's bytes. A nested body narrows
// limit for the length of that body and restores it after, so an inner
// decode can never reach past its own framing — the property C++ and C# get
// from handing the inner decode its own view.
final class TableReader {
  Uint8List bytes;
  int offset = 0;
  int limit;
  TableReport report;

  TableReader(Uint8List buffer, TableReport reportIn)
    : bytes = buffer,
      limit = buffer.length,
      report = reportIn;

  // Re-point this reader at another buffer, reusing the instance — the entry a
  // hot loop uses, and the one that allocates nothing at all. One argument
  // carries the memory (TableWriter says why it is the Uint8List), so there
  // is no pair here to drift apart.
  void attach(Uint8List buffer, TableReport reportIn) {
    bytes = buffer;
    offset = 0;
    limit = buffer.length;
    report = reportIn;
  }

  // Copy the bytes at the cursor into a buffer: a memmove.
  void copyInto(Uint8List into, int length) {
    into.setRange(0, length, bytes, offset);
  }

  bool has(int count) => count >= 0 && offset + count <= limit;

  int get8() => bytes[offset++];

  // The SIGNED narrow reads have their own entries because Dart's int is 64
  // bits wide: a raw byte read would not sign-extend, and the wire's i8/i16/i32
  // must land in storage as the number they name. The shift pair is the
  // sign extension.
  int getI8() => (bytes[offset++] << 56) >> 56;

  int getI16() => (get16() << 48) >> 48;

  int getI32() => (get32() << 32) >> 32;

  int get16() {
    final b = bytes;
    final o = offset;
    offset = o + 2;
    return b[o] | (b[o + 1] << 8);
  }

  int get32() {
    final b = bytes;
    final o = offset;
    offset = o + 4;
    return b[o] | (b[o + 1] << 8) | (b[o + 2] << 16) | (b[o + 3] << 24);
  }

  // The 64 bits, bit-transparently, in Dart's signed int — the same
  // representation every u64 field's storage uses; the top byte's shift wraps
  // into the sign, which is the transparency.
  int get64() {
    final b = bytes;
    final o = offset;
    offset = o + 8;
    return b[o] |
        (b[o + 1] << 8) |
        (b[o + 2] << 16) |
        (b[o + 3] << 24) |
        (b[o + 4] << 32) |
        (b[o + 5] << 40) |
        (b[o + 6] << 48) |
        (b[o + 7] << 56);
  }

  // Skip one payload by kind; false = framing damage.
  bool skip(int kind) {
    switch (kind) {
      case 1:
      case 2:
      case 6:
        if (!has(1)) {
          return false;
        }
        offset += 1;
        return true;
      case 3:
      case 7:
        if (!has(2)) {
          return false;
        }
        offset += 2;
        return true;
      case 4:
      case 8:
      case 10:
      case 17: // a POINTER is a u32 node index (docs/SPEC-TABLES.md §3.1)
        if (!has(4)) {
          return false;
        }
        offset += 4;
        return true;
      case 5:
      case 9:
      case 11:
        if (!has(8)) {
          return false;
        }
        offset += 8;
        return true;
      case 12:
      case 13:
      case 14:
      case 16:
        if (!has(4)) {
          return false;
        }
        final n = get32();
        if (!has(n)) {
          return false;
        }
        offset += n;
        return true;
      case 15: // union: u16 arm id, then the arm length-prefixed (id 0 = empty)
        if (!has(2)) {
          return false;
        }
        if (get16() == 0) {
          return true;
        }
        if (!has(4)) {
          return false;
        }
        final n = get32();
        if (!has(n)) {
          return false;
        }
        offset += n;
        return true;
    }
    return false;
  }
}

// EVERY CONVERSION BELOW IS prefer-inline, and it is a measured decision
// rather than a decoration: a Dart double crossing a call boundary is BOXED,
// and loadBody and saveBody are large enough bodies that the JIT's inlining
// budget runs out partway through and leaves some of these calls out of line
// — where, is the optimizer's decision and moves with the loop around the
// codec. AOT inlines them all. The allocation gate
// (test/dart-tables/gcgate.dart) holds AOT at zero and prints the JIT's count
// beside it.
//
// THE NaN WIDENING IS A SEPARATE, never-inline FUNCTION for the same reason
// in the other direction: a conversion with two exits — the f32 view on one
// path and the f64 view on the other — is one the JIT boxes on every call
// even when inlined, and a conversion with one exit and a cold call for the
// NaN case is one it does not. So a NaN with a payload costs one boxed double
// under the JIT, and every other float32 costs nothing, in either build.
//
// One 8-byte conversion scratch under overlaid typed-data views — the same
// device the packet emitter uses, and for the same reason: a float's IEEE-754
// bit pattern is what the wire carries (docs/SPEC-TABLES.md §3), and Dart has no
// reinterpret. Single threaded per isolate, always consumed in the operation
// that fills it, so it allocates once for the program rather than per call.
//
// The four views are MEMBERS. This backend spells no private library-scope
// name at all — Dart privacy is per library, and a schema may declare an
// identifier beginning with an underscore, so a private top-level name would
// be an unclaimed collision (§11). Members claim nothing.
final class TableScratch {
  static final Float64List f64 = Float64List(1);
  static final Float32List f32 = Float32List.view(f64.buffer);
  static final Uint32List u32 = Uint32List.view(f64.buffer);
  static final Uint64List u64 = Uint64List.view(f64.buffer);

  // The double a single-precision NaN PATTERN names, payload and sign kept:
  // the cold half of tableBitsToFloat, out of line so the hot half has one
  // exit (above).
  @pragma('vm:never-inline')
  static double nanFloat(int bits) {
    u64[0] =
        ((bits >>> 31) << 63) | 0x7ff0000000000000 | ((bits & 0x7fffff) << 29);
    return f64[0];
  }
}

// The double a u32 IEEE-754 single pattern names, and the inverse. NaN
// patterns cross in SOFTWARE — the hardware widening quiets a signaling NaN
// and the narrowing would not give the pattern back — so every bit pattern the
// wire can carry round trips. This is the packet emitter's own pair, for the
// same reason and in the same words.
@pragma('vm:prefer-inline')
double tableBitsToFloat(int bits) {
  if ((bits & 0x7f800000) == 0x7f800000 && (bits & 0x7fffff) != 0) {
    return TableScratch.nanFloat(bits);
  }
  TableScratch.u32[0] = bits;
  return TableScratch.f32[0];
}

@pragma('vm:prefer-inline')
int tableFloatToBits(double value) {
  if (!value.isNaN) {
    TableScratch.f32[0] = value;
    return TableScratch.u32[0];
  }
  TableScratch.f64[0] = value;
  final bits64 = TableScratch.u64[0];
  final sign = (bits64 >>> 63) << 31;
  var mantissa = (bits64 >>> 29) & 0x7fffff;
  if (mantissa == 0) {
    mantissa = 0x400000;
  }
  return sign | 0x7f800000 | mantissa;
}

// A float32 field's storage is a Dart double, so a value a caller assigned may
// hold precision the wire cannot. Narrowing before every comparison is what
// gives the elision decision C's own float semantics — -0.0 equal to 0.0, a
// NaN equal to nothing — rather than the double's, and it is why a Dart wire
// byte agrees with the C++ one for a value neither language could store the
// same way. A value that came off the wire is already narrow, so this is the
// identity on every read-modify-write path.
@pragma('vm:prefer-inline')
double tableNarrowFloat(double value) {
  TableScratch.f32[0] = value;
  return TableScratch.f32[0];
}

@pragma('vm:prefer-inline')
double tableBitsToDouble(int bits) {
  TableScratch.u64[0] = bits;
  return TableScratch.f64[0];
}

@pragma('vm:prefer-inline')
int tableDoubleToBits(double value) {
  TableScratch.f64[0] = value;
  return TableScratch.u64[0];
}

// Unsigned comparison over the bit-transparent int a u64 rides in.
bool tableUnsignedLess(int a, int b) =>
    (a ^ 0x8000000000000000) < (b ^ 0x8000000000000000);
`

// tableKeyedSource is the ENUM-KEYED array's storage (docs/SPEC-TABLES.md §2.4),
// emitted only into a unit that declares one — so a unit without a keyed array
// is byte-identical to what it was.
const tableKeyedSource = `
/// An ENUM-KEYED array's slots (docs/SPEC-TABLES.md §2.4): one per NAMED variant of
/// the key enum, so E.Max slots and none for None.
///
/// THE KEY IS THE ONLY INDEX. A slot is reached by the enum value that owns it
/// — config.teams[Team.red] — and the storage behind it is private, so the
/// one mental model a consumer meets is the page's: None is the null key and
/// stores nothing, and keys run 1 to E.Max.
///
/// The indexer REFUSES None and any value past the declared variants: 0 is the
/// reserved id no declared name can hold, so indexing by it is a caller defect,
/// not a slot that happens to be empty. A refusal here is an ArgumentError out
/// of a caller's own bad index — never something a wire read can raise, because
/// the codecs index by a value the vocabulary already resolved.
final class TableKeyed<T> {
  // the key k lives at storage index k - 1
  final List<T> _slots;

  /// A CLASS element is preallocated once per slot: every buffer exists at
  /// construction, so nothing on the read path allocates.
  TableKeyed.generate(int count, T Function() make)
    : _slots = List<T>.generate(count, (_) => make(), growable: false);

  /// A SCALAR element starts at its zero.
  TableKeyed.filled(int count, T zero)
    : _slots = List<T>.filled(count, zero, growable: false);

  @pragma('vm:prefer-inline')
  int _index(int key) {
    if (key < 1 || key > _slots.length) {
      throw ArgumentError.value(
        key,
        'key',
        'not a declared variant of this keyed array — '
            'None is the null key and stores nothing (docs/SPEC-TABLES.md §2.4)',
      );
    }
    return key - 1;
  }

  /// The slot a key owns.
  T operator [](int key) => _slots[_index(key)];

  /// Store into the slot a key owns.
  void operator []=(int key, T value) => _slots[_index(key)] = value;

  /// Every slot back to one value — what reset does for a scalar element.
  void fill(T zero) => _slots.fillRange(0, _slots.length, zero);

  /// How many slots, which is the key enum's declared variant count.
  int get length => _slots.length;

  /// The declared KEYS, ascending — iterating yields the enum value that owns
  /// each slot, never a storage index.
  Iterable<int> get keys => Iterable<int>.generate(_slots.length, (i) => i + 1);
}
`
