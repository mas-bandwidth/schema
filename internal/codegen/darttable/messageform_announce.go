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
	fmt.Fprintf(&out, "/// The unit's own build version, emitted beside the announcement.\nconst int tableBuildVersion = %d;\n\n", ir.BuildVersion(u))
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

/// THE RESERVED IDS the announcement's trailer names, and the two bounds a
/// hostile shape is held to (docs/SPEC-TABLES.md §3.3).
const int _tableBuildVersionWireId = -2; // 0xFFFFFFFFFFFFFFFE as signed
const int _tableMessageVocabularyWireId = -3; // 0xFFFFFFFFFFFFFFFD as signed

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
    if (id == _tableBuildVersionWireId) {
      if (kind != 9 || at + 8 > bodyEnd) {
        break;
      }
      vocabulary.buildVersion = view.getUint64(at, Endian.little);
      at += 8;
      versions++;
    } else if (id == _tableMessageVocabularyWireId) {
      if (kind != 14 || at >= bodyEnd) {
        break;
      }
      final elementKind = bytes[at];
      at += 1;
      if (elementKind != 6) {
        break;
      }
      final length = _leb(bytes, at);
      at = length.$2;
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
    if (vAt + 9 > vEnd) {
      report.Damage();
      return false;
    }
    final id = view.getUint64(vAt, Endian.little);
    final kind = bytes[vAt + 8];
    vAt += 9;
    vocabulary.entries[seen] = TableMessageEntry(id, kind);
    seen++;
  }
  vocabulary.count = seen;
  vocabulary.refBits = seen == 0 ? 1 : _bits(seen);
  vocabulary.announced = true;
  return true;
}

/// bits_required(0, n): the width of a reference into an n-entry vocabulary.
int _bits(int n) {
  var count = 0;
  while (n != 0) {
    count++;
    n >>= 1;
  }
  return count == 0 ? 1 : count;
}

/// LEB128, the canonical length every length of a file takes (§3).
(int, int) _leb(Uint8List bytes, int at) {
  var value = 0;
  var shift = 0;
  while (at < bytes.length) {
    final b = bytes[at];
    at++;
    value |= (b & 0x7f) << shift;
    if ((b & 0x80) == 0) {
      break;
    }
    shift += 7;
  }
  return (value, at);
}
`
