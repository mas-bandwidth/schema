package darttable

// THE MESSAGE FORM'S ANNOUNCEMENT ON THE DART LEG (docs/SPEC-TABLES.md §3.3).
//
// The message form is a batch of bitpacked bodies under ONE announced
// vocabulary. "It is delivered ONCE, RELIABLY, BEFORE THE FIRST BODY, and
// never again for the life of the connection" (§3.3), and "the ANNOUNCEMENT IS
// A COMPILE-TIME CONSTANT BYTE ARRAY WITH A LENGTH" (§3.3): the compiler
// settles every byte, so a backend emits it as a constant rather than walking
// a unit at run time.
//
// The wire is the C++ reference's, pinned by testdata/conformance/tables/
// MANIFEST.txt's `connection` rows and rebuilt here by tablewire.Announce.
// The surface and its names follow the C# leg (internal/codegen/cstable/
// message.go), which is the reference for this form: TableVocabulary, Announce
// and AnnounceMeasure (this card), AnnounceRead (this card), then
// MeasureMessages, SaveMessages and LoadMessages (the next card).
//
// The message form is "A FIXED TABLE'S, and that is a declaration, not a
// derivation" (§3.3), so this surface is emitted for a unit that declares a
// fixed root — the same home rule the fixed form already follows: the bytes
// land in the unit's one runtime home, beside the fixed runtime.

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// emitMessageAnnouncement writes the unit's announcement constant and the
// announcement runtime. It is called from the fixed home module, so a unit
// with no fixed root emits nothing here.
func (g *fixedModule) emitMessageAnnouncement() {
	g.pf("%s", dartMessageAnnounceRuntime)
	g.pf("%s", dartTableAnnouncementConstant(g.unit))
}

// dartTableAnnouncementConstant renders tablewire.Announce(u) as a Dart
// constant, the same way the reference legs emit it: a const byte list and its
// length. `final` rather than `const` because Uint8List is not a const type.
func dartTableAnnouncementConstant(u *ir.Unit) string {
	b := tablewire.Announce(u)
	var out strings.Builder
	out.WriteString("/// THE ANNOUNCEMENT: the unit's id table, settled byte for byte by the\n")
	out.WriteString("/// compiler. It is an ordinary form-`1` file whose body carries the build\n")
	out.WriteString("/// version and the vocabulary and whose trailer is the two reserved ids\n")
	out.WriteString("/// (docs/SPEC-TABLES.md §3.3).\n")
	out.WriteString("final Uint8List tableAnnouncement = Uint8List.fromList(const <int>[\n")
	for i := 0; i < len(b); i += 12 {
		end := min(i+12, len(b))
		out.WriteString(" ")
		for _, v := range b[i:end] {
			fmt.Fprintf(&out, " 0x%02x,", v)
		}
		out.WriteString("\n")
	}
	out.WriteString("]);\n")
	fmt.Fprintf(&out, "const int tableAnnouncementLength = %d;\n", len(b))
	fmt.Fprintf(&out, "/// The unit's own build version, emitted beside the announcement.\nconst int tableBuildVersion = %d;\n\n", int64(ir.BuildVersion(u)))
	return out.String()
}

// dartMessageAnnounceRuntime is the shared Dart runtime for the announcement
// surface. Its classes and its free functions are all DECLARATIONS at library
// scope, so every one of them claims a name: each is registered in
// internal/tablenames for the Dart backend and a schema declaring one is
// refused rather than generating Dart that does not compile. The free
// functions take Dart's own convention, lowerCamelCase (docs/SPEC-TABLES.md
// §6.1: `TableVocabulary`, `announce`, `announceMeasure`, `announceRead`), the
// same shape the fixed runtime's free helpers take, and the classes keep the
// shared PascalCase.
const dartMessageAnnounceRuntime = `
// ---------------------------------------------------------------------------
// THE MESSAGE FORM'S ANNOUNCEMENT, form byte 2 (docs/SPEC-TABLES.md §3.3)
// ---------------------------------------------------------------------------

/// ONE RECEIVED ANNOUNCEMENT per direction. "A failed first announcement is
/// terminal too; a second never changes its vocabulary or build version."
/// (§3.3). The storage the caller hands in is never replaced, so a resolved
/// vocabulary allocates nothing.
class TableVocabulary {
  List<TableMessageEntry> entries;
  int count = 0;
  int refBits = 1;
  int buildVersion = 0;
  bool announced = false;
  bool attempted = false;

  /// "THE ONE-ANNOUNCEMENT WORK BUDGET" (§3.3): the caller declares the bound
  /// the payload length and the entry count are held to.
  int maxEntries;
  int maxBytes;

  /// "STRICT MODE IS OPT-IN, and the default is unchanged." (§3.3).
  bool strict = false;

  /// THE RESERVED IDS the announcement's trailer names (§3.3), and the node
  /// table's, which a second entry may not name. They are members so nothing
  /// lands at library scope, the leg's own rule.
  static const int buildVersionWireId = -2; // 0xFFFFFFFFFFFFFFFE as signed
  static const int vocabularyWireId = -3; // 0xFFFFFFFFFFFFFFFD as signed
  static const int nodeWireId = -1; // 0xFFFFFFFFFFFFFFFF as signed

  TableVocabulary({
    required this.entries,
    this.maxEntries = 4096,
    this.maxBytes = 65536,
  });

  int get BuildVersion => buildVersion;
}

/// ONE ANNOUNCED ENTRY: an id, a kind, and a shape. The entry carries the
/// width facts a bitpacked body needs to step over a field it cannot name
/// (docs/SPEC-TABLES.md §3.3).
class TableMessageEntry {
  final int id;
  final int kind;
  /// The entry's wire bytes, its triple's identity: §3.3 lets one NAME appear
  /// at two shapes, so a repeated entry is the same bytes and not the same id.
  final Uint8List signature;
  /// The shape announceRead decoded and checked, which the batch's
  /// unknown-entry skip reads rather than walking the signature a second time.
  TableMessageShape? shape;
  TableMessageEntry(this.id, this.kind, [Uint8List? signature, this.shape])
      : signature = signature ?? Uint8List(0) {
    if (shape == null && this.signature.length >= 9) {
      final decoded = TableMessageShape.decode(this.signature, 9, this.signature.length, this.kind);
      if (decoded != null) shape = decoded.$1;
    }
  }
}

/// ONE ANNOUNCED ENTRY'S SHAPE: the width facts a reader needs to step over a
/// field it cannot name (docs/SPEC-TABLES.md §3.3). decode is the ONE checked
/// decoder, the mirror of ir.DecodeShape, so a shape is accepted exactly when
/// the engine accepts it.
final class TableMessageShape {
  int packing = 0;
  int bits = 0;
  int min = 0;
  int max = 0;
  int elem = 0;
  TableMessageShape? inner;

  /// Decodes ONE shape at at in an announced entry's signature, mirroring
  /// ir.DecodeShape. Answers the shape and the offset past it, or null where
  /// the bytes run out or a width is one no kind can hold.
  static (TableMessageShape, int)? decode(Uint8List sig, int at, int end, int kind) {
    final s = TableMessageShape();
    if ((kind >= 2 && kind <= 10) || (kind >= 18 && kind <= 29)) {
      if (at >= end) return null;
      s.packing = sig[at];
      at += 1;
      if (s.packing == 0) {
        return (s, at);
      }
      if (s.packing == 1 && kind != 10) {
        final n = tableMessageLeb(sig, at, end);
        if (!n.$3 || n.$1 < 0 || n.$1 > tableMessageKindWidth(kind) * 8) return null;
        s.bits = n.$1;
        at = n.$2;
        if (kind >= 18) {
          if (at + 16 > end) return null;
          return (s, at + 16);
        }
        final b = tableMessageLeb(sig, at, end);
        return b.$3 ? (s, b.$2) : null;
      }
      if (s.packing == 2 && kind == 10) {
        final bits = quantizedBits(sig, at, end);
        if (bits == null) return null;
        s.bits = bits;
        return (s, at + 12);
      }
      return null;
    }
    if (kind == 12) {
      final n = tableMessageLeb(sig, at, end);
      if (!n.$3 || n.$1 < 0 || n.$1 > 0x7fffffff) return null;
      s.max = n.$1;
      return (s, n.$2);
    }
    if (kind == 14 || kind == 16) {
      if (kind == 14) {
        final lo = tableMessageLeb(sig, at, end);
        if (!lo.$3 || lo.$1 < 0 || lo.$1 > 0xffffffff) return null;
        s.min = lo.$1;
        at = lo.$2;
      }
      final hi = tableMessageLeb(sig, at, end);
      if (!hi.$3 || hi.$1 < 0 || hi.$1 > 0xffffffff || hi.$1 < s.min) return null;
      s.max = hi.$1;
      at = hi.$2;
      if (at >= end) return null;
      s.elem = sig[at];
      at += 1;
      if (!tableMessageKnownKind(s.elem)) return null;
      final inner = decode(sig, at, end, s.elem);
      if (inner == null) return null;
      s.inner = inner.$1;
      return (s, inner.$2);
    }
    return (s, at);
  }

  /// The width of a quantized f32 (ir.DecodeShape, ir.TableMessageQuantization):
  /// bits.Len32 of ceil(float32((max - min) / res)), clamped to [1, 2^32 - 256],
  /// or null where the triple is not conforming.
  static int? quantizedBits(Uint8List sig, int at, int end) {
    if (at + 12 > end) return null;
    final view = ByteData.sublistView(sig, at, at + 12);
    final f = Float32List(3);
    f[0] = view.getFloat32(0, Endian.little);
    f[1] = view.getFloat32(4, Endian.little);
    f[2] = view.getFloat32(8, Endian.little);
    if (!(f[0] < f[1]) || !(f[2] > 0)) return null;
    f[1] = f[1] - f[0];
    f[2] = f[1] / f[2];
    final delta = f[1];
    var values = f[2];
    if (delta - delta != 0 || values - values != 0) return null;
    if (!(values >= 1)) {
      values = 1;
    } else if (values > 4294967040.0) {
      values = 4294967040.0;
    }
    var count = values.ceil();
    var width = 0;
    while (count != 0) {
      width++;
      count >>= 1;
    }
    return width;
  }

  /// bits_required(min, max): the width of max - min, and zero where the two
  /// are equal (ir.TableMessageBitsRequired). The difference is taken in the
  /// 64-bit domain Go's int64 subtraction wraps in, and a span near 2^64 reads
  /// back negative in a Dart int: its uint64 bit length is 64, exactly what
  /// bits.Len64 answers after the same wrap.
  static int bitsRequired(int min, int max) {
    if (max <= min) {
      return 0;
    }
    final value = max - min;
    if (value < 0) {
      return 64;
    }
    var width = 0;
    var v = value;
    while (v != 0) {
      width++;
      v >>= 1;
    }
    return width;
  }

  /// ONE value's width in bits under a shape (ir.TableMessageValueBits), or -1
  /// where the kind has no fixed-width payload.
  static int valueBits(int kind, TableMessageShape s) {
    if (kind == 1) {
      return 1;
    }
    if (kind == 11 || kind == 36) {
      return 64;
    }
    if (kind == 10) {
      return s.packing == 2 ? s.bits : 32;
    }
    if ((kind >= 2 && kind <= 9) || (kind >= 18 && kind <= 29)) {
      if (s.packing == 1) {
        return s.bits;
      }
      return tableMessageKindWidth(kind) * 8;
    }
    return -1;
  }
}

/// THE REPORT the announcement read answers on. A refusal is terminal: it
/// sets 'refused' and 'reason' and clears the counts.
class TableReport {
  bool refused = false;
  int unknown = 0;
  int widened = 0;
  int buildVersionRemote = 0;
  int buildVersionOwn = 0;
  String reason = '';
  /// The float whose raw bits a read found to be a NaN or an infinity, as
  /// <Table>.<field>, '' when it found none (docs/SPEC-TABLES.md §3.3).
  String nonFiniteFloat = '';

  // A refused read keeps no count of what it met before the refusal: the
  // report is the refusal alone, as the Go reference's (tablewire refuseReport).
  void Damage() {
    refused = true;
    reason = 'wire_damaged';
    unknown = 0;
    widened = 0;
  }

  bool Refuse(String why) {
    refused = true;
    reason = why;
    unknown = 0;
    widened = 0;
    return false;
  }

  /// EVERY READ REFUSES A NaN OR AN INFINITY (docs/SPEC-TABLES.md §3.3), the
  /// tolerant batch read included: the reason names the field, and the batch
  /// lands nothing.
  bool RefuseNonFiniteFloat(String field) {
    nonFiniteFloat = field;
    return Refuse('non_finite_float: $field');
  }
}

/// "The ANNOUNCEMENT IS A COMPILE-TIME CONSTANT BYTE ARRAY WITH A LENGTH."
/// (§3.3) — the length, emitted beside the bytes.
int announceMeasure() => tableAnnouncementLength;

/// Copies the compiled announcement into the caller's buffer and answers its
/// length, or -1 when the buffer is too small. Nothing here allocates.
int announce(Uint8List buffer) {
  if (buffer.length < tableAnnouncementLength) {
    return -1;
  }
  buffer.setRange(0, tableAnnouncementLength, tableAnnouncement);
  return tableAnnouncementLength;
}

/// THE RESERVED IDS the announcement's trailer names live on TableVocabulary.

/// Reads one announcement into 'vocabulary'. "NO RE-ANNOUNCEMENT, EVER."
/// (§3.3): a vocabulary that already announced, or whose first announcement
/// was refused, answers 'second_announcement' and is terminal.
bool announceRead(TableVocabulary vocabulary, Uint8List bytes, TableReport report) {
  if (vocabulary.attempted) {
    return report.Refuse('second_announcement');
  }
  vocabulary.attempted = true;
  if (bytes.length == 0) {
    report.Damage();
    return false;
  }
  // "the FORM BYTE is a whole byte and is read FIRST" (§3.3): a form this
  // reader does not carry is a refusal by name and never damage.
  if (bytes[0] != 1) {
    return report.Refuse(bytes[0] == 2 ? 'message_form_as_file' : 'newer_form');
  }
  if (bytes.length < 9) {
    report.Damage();
    return false;
  }
  final view = ByteData.sublistView(bytes);
  // the trailer: a run of u64 ids and a u64 count at the very end
  final trailerCount = view.getUint64(bytes.length - 8, Endian.little);
  if (trailerCount > (bytes.length - 9) ~/ 8) {
    report.Damage();
    return false;
  }
  final end = bytes.length - 8 - trailerCount * 8;
  // A BODY'S REFERENCES RESOLVE against the trailer's ids, which begin here.
  final ids = <int>[];
  for (var i = end; i < end + trailerCount * 8; i += 8) {
    ids.add(view.getUint64(i, Endian.little));
  }
  for (var i = 0; i < ids.length; i++) {
    for (var j = 0; j < i; j++) {
      if (ids[i] == ids[j]) {
        report.Damage();
        return false;
      }
    }
  }
  // THE BODY walks references into those ids. THE TWO STRICT CHECKS: exactly
  // one build-version field and exactly one vocabulary field, both answered
  // 'malformed'.
  var at = 1;
  var versions = 0;
  var vocabularies = 0;
  var bodyEnd = end;
  var vocabularyStart = 0;
  var vocabularyLength = 0;
  var terminated = false;
  while (at < bodyEnd) {
    final ref = bytes[at];
    at += 1;
    if (ref == 0) {
      terminated = true;
      break;
    }
    if (ref > ids.length) {
      break;
    }
    if (at >= bodyEnd) {
      break;
    }
    final id = ids[ref - 1];
    final kind = bytes[at];
    at += 1;
    if (id == TableVocabulary.buildVersionWireId) {
      if (kind != 9 || at + 8 > bodyEnd) {
        break;
      }
      vocabulary.buildVersion = view.getUint64(at, Endian.little);
      at += 8;
      versions++;
    } else if (id == TableVocabulary.vocabularyWireId) {
      if (kind != 14) {
        break;
      }
      // THE FIELD IS LENGTH-PREFIXED (§3): its body is a LEB length, then the
      // element kind, then the vocabulary's own LEB length, then the entries.
      final fieldLength = tableMessageLeb(bytes, at, bodyEnd);
      if (!fieldLength.$3 || fieldLength.$1 < 0) {
        break;
      }
      at = fieldLength.$2;
      final fieldEnd = at + fieldLength.$1;
      if (fieldEnd > bodyEnd) {
        break;
      }
      final elementKind = bytes[at];
      at += 1;
      if (elementKind != 6) {
        break;
      }
      final length = tableMessageLeb(bytes, at, fieldEnd);
      if (!length.$3 || length.$1 < 0) {
        break;
      }
      at = length.$2;
      if (at + length.$1 != fieldEnd) {
        break;
      }
      if (length.$1 > vocabulary.maxBytes) {
        return report.Refuse('vocabulary_too_large');
      }
      vocabularyStart = at;
      vocabularyLength = length.$1;
      at += length.$1;
      vocabularies++;
    } else {
      // §3.3: "Every other field of its body is ordinary and tolerant, so an
      // unknown one is skipped and counted and the announcement can GAIN a
      // field in a later minor without a lockstep redeploy." Only a failed
      // strict check is malformed, so the payload is stepped over by its kind
      // and the walk continues; a payload that will not step is the one break.
      final skipped = tableMessageSkipField(bytes, at, bodyEnd, kind);
      if (skipped < 0) {
        break;
      }
      at = skipped;
      report.unknown++;
    }
  }
  if (!terminated || versions != 1 || vocabularies != 1) {
    report.Damage();
    return false;
  }
  if (vocabulary.strict && vocabulary.buildVersion != tableBuildVersion) {
    report.buildVersionRemote = vocabulary.buildVersion;
    report.buildVersionOwn = tableBuildVersion;
    return report.Refuse('build_version_mismatch');
  }
  // THE VOCABULARY PAYLOAD is a run of entries, each an id, a kind and a
  // shape. A payload longer than the caller's entry bound refuses by name.
  var vAt = vocabularyStart;
  final vEnd = vocabularyStart + vocabularyLength;
  var seen = 0;
  var nodeSeen = false;
  while (vAt < vEnd) {
    if (seen >= vocabulary.maxEntries || seen >= vocabulary.entries.length) {
      return report.Refuse('vocabulary_too_large');
    }
    final start = vAt;
    if (vAt + 9 > vEnd) {
      report.Damage();
      return false;
    }
    final id = view.getUint64(vAt, Endian.little);
    final kind = bytes[vAt + 8];
    if (!tableMessageKnownKind(kind)) {
      report.Damage();
      return false;
    }
    final decoded = TableMessageShape.decode(bytes, vAt + 9, vEnd, kind);
    if (decoded == null) {
      report.Damage();
      return false;
    }
    final next = decoded.$2;
    final shape = decoded.$1;
    // §3.3: "THE TWO RESERVED IDS OF THE ANNOUNCEMENT ITSELF ARE NOT IN THE
    // VOCABULARY." An entry carrying one is malformed; the node table's id may
    // name at most one entry; a repeated entry is malformed too.
    if (id == TableVocabulary.buildVersionWireId ||
        id == TableVocabulary.vocabularyWireId ||
        (id == TableVocabulary.nodeWireId && nodeSeen)) {
      report.Damage();
      return false;
    }
    if (id == TableVocabulary.nodeWireId) {
      nodeSeen = true;
    }
    for (var i = 0; i < seen; i++) {
      if (tableMessageSameEntry(bytes, start, next, vocabulary.entries[i].signature)) {
        report.Damage();
        return false;
      }
    }
    vAt = next;
    vocabulary.entries[seen] = TableMessageEntry(id, kind, bytes.sublist(start, next), shape);
    seen++;
  }
  vocabulary.count = seen;
  vocabulary.refBits = seen == 0 ? 1 : tableMessageBits(seen);
  vocabulary.announced = true;
  return true;
}

/// tableMessageKnownKind reports whether a kind is one of the closed set.
bool tableMessageKnownKind(int kind) => kind >= 0 && (kind <= 32 || kind == 36);

/// tableMessageSameEntry reports whether ONE entry's wire bytes equal an
/// earlier entry's signature (docs/SPEC-TABLES.md §3.3): a repeated entry is
/// malformed, and the identity is the bytes because one name may carry two
/// shapes.
bool tableMessageSameEntry(Uint8List bytes, int start, int end, Uint8List signature) {
  if (end - start != signature.length) {
    return false;
  }
  for (var i = 0; i < signature.length; i++) {
    if (bytes[start + i] != signature[i]) {
      return false;
    }
  }
  return true;
}

/// tableMessageKindWidth is the fixed width of a scalar kind's payload, or 0
/// where the kind is not a fixed-width scalar (docs/SPEC-TABLES.md §3).
int tableMessageKindWidth(int kind) {
  switch (kind) {
    case 1:
    case 2:
    case 6:
    case 20:
    case 25:
      return 1;
    case 3:
    case 7:
    case 21:
    case 26:
      return 2;
    case 4:
    case 8:
    case 10:
    case 22:
    case 27:
      return 4;
    case 5:
    case 9:
    case 11:
    case 23:
    case 28:
    case 36:
      return 8;
    case 18:
    case 19:
    case 24:
    case 29:
      return 16;
    default:
      return 0;
  }
}

/// tableMessageSkipField steps over ONE unknown body field's payload by its
/// kind (docs/SPEC-TABLES.md §3.3), the reference's Skip(kind) rule: a
/// fixed-width scalar is its width, a reference or a union is a LEB, and a
/// length-prefixed slice — 12, 13, 14, 16, 31, 32 — is a LEB length then
/// that many bytes. Answers the offset past the payload, or -1
/// where the bytes run out or a kind this reader cannot step is named.
int tableMessageSkipField(Uint8List bytes, int at, int end, int kind) {
  final width = tableMessageKindWidth(kind);
  if (width != 0) {
    return at + width <= end ? at + width : -1;
  }
  switch (kind) {
    case 17:
    case 30:
      final v = tableMessageLeb(bytes, at, end);
      return (!v.$3 || v.$1 < 0) ? -1 : v.$2;
    case 12:
    case 13:
    case 14:
    case 16:
    case 31:
    case 32:
      final n = tableMessageLeb(bytes, at, end);
      if (!n.$3 || n.$1 < 0 || n.$2 + n.$1 > end) {
        return -1;
      }
      return n.$2 + n.$1;
    case 15:
      final arm = tableMessageLeb(bytes, at, end);
      if (!arm.$3 || arm.$1 < 0) {
        return -1;
      }
      if (arm.$1 == 0) {
        return arm.$2;
      }
      if (arm.$2 >= end) {
        return -1;
      }
      final n = tableMessageLeb(bytes, arm.$2 + 1, end);
      if (!n.$3 || n.$1 < 0 || n.$2 + n.$1 > end) {
        return -1;
      }
      return n.$2 + n.$1;
    default:
      return -1;
  }
}

/// tableMessageBits is the width of a reference into an n-entry vocabulary.
int tableMessageBits(int n) {
  var count = 0;
  while (n != 0) {
    count++;
    n >>= 1;
  }
  return count == 0 ? 1 : count;
}

/// LEB128, the canonical length every length of a file takes (§3). Answers the
/// value, the offset past it, and whether the bytes spelled one whole (the Go
/// reference's readLebBytes). A value above 2^63 reads back NEGATIVE in a Dart
/// int; that is the uint64's bit pattern, not a failure, and the flag is what
/// tells the two apart. A signed ranged base near -2^63 is exactly such a
/// value (ir.DecodeShape, tableMessageUnzigzag), so the base read must keep it.
(int, int, bool) tableMessageLeb(Uint8List bytes, int at, int end) {
  final start = at;
  var value = 0;
  var shift = 0;
  while (at < end && shift < 64) {
    final b = bytes[at];
    at++;
    if (shift == 63 && b > 1) {
      return (0, at, false); // a tenth byte with a bit above the 64th value bit
    }
    value |= (b & 0x7f) << shift;
    if ((b & 0x80) == 0) {
      if (at - start > 1 && b == 0) {
        return (0, at, false); // a longer spelling of the same number
      }
      return (value, at, true);
    }
    shift += 7;
  }
  return (0, at, false);
}
`
