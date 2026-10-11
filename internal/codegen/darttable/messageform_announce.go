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
// surface. It is a static class so nothing lands at library scope, the same
// rule the fixed runtime follows. The names are the reference's own — the C#
// leg spells them `TableVocabulary`, `Announce`, `AnnounceMeasure`,
// `AnnounceRead` — so a reader crossing legs reads one vocabulary of names.
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

  /// THE RESERVED IDS the announcement's trailer names (§3.3). They are
  /// members so nothing lands at library scope, the leg's own rule.
  static const int buildVersionWireId = -2; // 0xFFFFFFFFFFFFFFFE as signed
  static const int vocabularyWireId = -3; // 0xFFFFFFFFFFFFFFFD as signed

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
  TableMessageEntry(this.id, this.kind);
}

/// THE REPORT the announcement read answers on. A refusal is terminal: it
/// sets 'refused' and 'reason' and nothing else moves.
class TableReport {
  bool refused = false;
  bool malformed = false;
  int unknown = 0;
  int buildVersionPeer = 0;
  int buildVersionOwn = 0;
  String reason = '';

  void Damage() {
    malformed = true;
  }

  bool Refuse(String why) {
    refused = true;
    reason = why;
    return false;
  }
}

/// "The ANNOUNCEMENT IS A COMPILE-TIME CONSTANT BYTE ARRAY WITH A LENGTH."
/// (§3.3) — the length, emitted beside the bytes.
int AnnounceMeasure() => tableAnnouncementLength;

/// Copies the compiled announcement into the caller's buffer and answers its
/// length, or -1 when the buffer is too small. Nothing here allocates.
int Announce(Uint8List buffer) {
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
bool AnnounceRead(TableVocabulary vocabulary, Uint8List bytes, TableReport report) {
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
      if (fieldLength.$1 < 0) {
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
      if (length.$1 < 0) {
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
      report.unknown++;
      break;
    }
  }
  if (!terminated || versions != 1 || vocabularies != 1) {
    report.Damage();
    return false;
  }
  if (vocabulary.strict && vocabulary.buildVersion != tableBuildVersion) {
    report.buildVersionPeer = vocabulary.buildVersion;
    report.buildVersionOwn = tableBuildVersion;
    return report.Refuse('build_version_mismatch');
  }
  // THE VOCABULARY PAYLOAD is a run of entries, each an id, a kind and a
  // shape. A payload longer than the caller's entry bound refuses by name.
  var vAt = vocabularyStart;
  final vEnd = vocabularyStart + vocabularyLength;
  var seen = 0;
  while (vAt < vEnd) {
    if (seen >= vocabulary.maxEntries) {
      return report.Refuse('vocabulary_too_large');
    }
    final next = tableMessageEntryEnd(bytes, vAt, vEnd);
    if (next < 0) {
      report.Damage();
      return false;
    }
    final id = view.getUint64(vAt, Endian.little);
    final kind = bytes[vAt + 8];
    vAt = next;
    vocabulary.entries[seen] = TableMessageEntry(id, kind);
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
      final n = tableMessageLeb(bytes, at, end);
      if (n.$1 < 0) {
        return -1;
      }
      at = n.$2;
      if (kind >= 18) {
        return at + 16 <= end ? at + 16 : -1;
      }
      final base = tableMessageLeb(bytes, at, end);
      return base.$1 < 0 ? -1 : base.$2;
    }
    if (packing == 2 && kind == 10) {
      return at + 12 <= end ? at + 12 : -1;
    }
    return -1;
  }
  if (kind == 12 || kind == 33) {
    final n = tableMessageLeb(bytes, at, end);
    return n.$1 < 0 ? -1 : n.$2;
  }
  if (kind == 14 || kind == 16) {
    if (kind == 14) {
      final lo = tableMessageLeb(bytes, at, end);
      if (lo.$1 < 0) {
        return -1;
      }
      at = lo.$2;
    }
    final hi = tableMessageLeb(bytes, at, end);
    return hi.$1 < 0 ? -1 : hi.$2;
  }
  return at;
}

/// tableMessageKnownKind reports whether a kind is one of the closed set.
bool tableMessageKnownKind(int kind) => kind <= 33;

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
/// value and the offset past it, or -1 where the bytes run out.
(int, int) tableMessageLeb(Uint8List bytes, int at, int end) {
  var value = 0;
  var shift = 0;
  while (at < end && shift < 64) {
    final b = bytes[at];
    at++;
    value |= (b & 0x7f) << shift;
    if ((b & 0x80) == 0) {
      return (value, at);
    }
    shift += 7;
  }
  return (-1, at);
}
`
