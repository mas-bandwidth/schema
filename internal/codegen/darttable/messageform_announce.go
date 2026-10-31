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
// MANIFEST.txt's `connection` rows and rebuilt here by ir.TableAnnouncement.
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

	"github.com/mas-bandwidth/schema/v2/ir"
)

// emitMessageAnnouncement writes the unit's announcement constant and the
// announcement runtime. It is called from the fixed home module, so a unit
// with no fixed root emits nothing here.
func (g *fixedModule) emitMessageAnnouncement() {
	g.pf("%s", dartMessageAnnounceRuntime)
	g.pf("%s", dartTableAnnouncementConstant(g.unit))
}

// dartTableAnnouncementConstant renders ir.TableAnnouncement(u) as a Dart
// constant, the same way the reference legs emit it: a const byte list and its
// length. `final` rather than `const` because Uint8List is not a const type.
func dartTableAnnouncementConstant(u *ir.Unit) string {
	b := ir.TableAnnouncement(u)
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
  TableMessageEntry(this.id, this.kind, [Uint8List? signature])
      : signature = signature ?? Uint8List(0);
}

/// THE REPORT the announcement read answers on. A refusal is terminal: it
/// sets 'refused' and 'reason' and nothing else moves.
class TableReport {
  bool refused = false;
  bool malformed = false;
  int unknown = 0;
  /// §4's other events, carried here so a message read answers the same
  /// six-counter report the Go reference does (internal/tabletext.Report).
  int kindMismatch = 0;
  int widened = 0;
  int clamped = 0;
  int duplicate = 0;
  int buildVersionRemote = 0;
  int buildVersionOwn = 0;
  String reason = '';
  /// The float whose raw bits a read found to be a NaN or an infinity, as
  /// <Table>.<field>, '' when it found none (docs/SPEC-TABLES.md §3.3).
  String nonFiniteFloat = '';

  void Damage() {
    malformed = true;
    reason = 'malformed';
  }

  bool Refuse(String why) {
    refused = true;
    reason = why;
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
    if (seen >= vocabulary.maxEntries) {
      return report.Refuse('vocabulary_too_large');
    }
    final start = vAt;
    final next = tableMessageEntryEnd(bytes, vAt, vEnd);
    if (next < 0) {
      report.Damage();
      return false;
    }
    final id = view.getUint64(vAt, Endian.little);
    final kind = bytes[vAt + 8];
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
    vocabulary.entries[seen] = TableMessageEntry(id, kind, bytes.sublist(start, next));
    seen++;
  }
  vocabulary.count = seen;
  vocabulary.refBits = seen == 0 ? 1 : tableMessageBits(seen);
  vocabulary.announced = true;
  return true;
}

/// Steps over ONE entry: a u64 id, a kind byte, and the SHAPE the kind gives it
/// (docs/SPEC-TABLES.md §3.3). Answers the offset past the entry, or -1 where
/// the bytes run out or a kind is outside the closed set. This is the mirror of
/// ir.DecodeShape, so an entry's shape is stepped over by its kind and never
/// mistaken for the next entry.
int tableMessageEntryEnd(Uint8List bytes, int at, int end) {
  if (at + 9 > end) {
    return -1;
  }
  final kind = bytes[at + 8];
  if (!tableMessageKnownKind(kind)) {
    return -1;
  }
  var p = tableMessageShapeEnd(bytes, at + 9, end, kind);
  if (p < 0) {
    return -1;
  }
  if (kind == 14 || kind == 16) {
    if (p >= end) {
      return -1;
    }
    final elem = bytes[p];
    p += 1;
    if (!tableMessageKnownKind(elem) || elem == 12 || elem == 33) {
      return -1;
    }
    p = tableMessageShapeEnd(bytes, p, end, elem);
    if (p < 0) {
      return -1;
    }
    if (elem == 14 || elem == 16) {
      if (p >= end) {
        return -1;
      }
      final inner = bytes[p];
      p += 1;
      if (!tableMessageKnownKind(inner) || inner == 12 || inner == 33) {
        return -1;
      }
    }
  }
  return p;
}

/// Steps over one SHAPE, the width facts the kind names (docs/SPEC-TABLES.md
/// §3.3), mirroring ir.DecodeShape's lengths: a packing byte, then its width
/// and base, or a length, or an array's two counts. -1 where the bytes run out.
int tableMessageShapeEnd(Uint8List bytes, int at, int end, int kind) {
  if ((kind >= 2 && kind <= 10) || (kind >= 18 && kind <= 29)) {
    if (at >= end) {
      return -1;
    }
    final packing = bytes[at];
    at += 1;
    if (packing == 0) {
      return at;
    }
    if (packing == 1 && kind != 10) {
      // A WIDTH ABOVE THE KIND'S OWN DOMAIN IS A HOSTILE WIDTH, checked once
      // here and never again (ir.DecodeShape checks the width against TableMessageKindBits). The
      // value is read whole, so a width near 2^63 still trips this test rather
      // than wrapping into a small number.
      final n = tableMessageLeb(bytes, at, end);
      if (!n.$3 || n.$1 < 0 || n.$1 > tableMessageKindWidth(kind) * 8) {
        return -1;
      }
      at = n.$2;
      if (kind >= 18) {
        return at + 16 <= end ? at + 16 : -1;
      }
      // A RANGED BASE IS A WHOLE LEB VALUE, and a signed kind's is a zigzag
      // near 2^64 for a minimum near -2^63 (ir.DecodeShape's
      // tableMessageUnzigzag). The bit pattern may read back negative in a
      // Dart int; that is a value, never a failed read.
      final base = tableMessageLeb(bytes, at, end);
      return base.$3 ? base.$2 : -1;
    }
    if (packing == 2 && kind == 10) {
      return at + 12 <= end ? at + 12 : -1;
    }
    return -1;
  }
  if (kind == 12 || kind == 33) {
    final n = tableMessageLeb(bytes, at, end);
    return (!n.$3 || n.$1 < 0) ? -1 : n.$2;
  }
  if (kind == 14 || kind == 16) {
    if (kind == 14) {
      final lo = tableMessageLeb(bytes, at, end);
      if (!lo.$3 || lo.$1 < 0) {
        return -1;
      }
      at = lo.$2;
    }
    final hi = tableMessageLeb(bytes, at, end);
    return (!hi.$3 || hi.$1 < 0) ? -1 : hi.$2;
  }
  return at;
}

/// tableMessageKnownKind reports whether a kind is one of the closed set.
bool tableMessageKnownKind(int kind) => kind <= 33;

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
/// length-prefixed slice — 12, 13, 14, 16, 31, 32, 33 — is a LEB length then
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
    case 33:
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
  var value = 0;
  var shift = 0;
  while (at < end && shift < 64) {
    final b = bytes[at];
    at++;
    value |= (b & 0x7f) << shift;
    if ((b & 0x80) == 0) {
      return (value, at, true);
    }
    shift += 7;
  }
  return (0, at, false);
}
`
