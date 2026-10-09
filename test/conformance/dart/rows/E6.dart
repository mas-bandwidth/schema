// CARD: cell-dart-field-evolution E6 — renaming uses the declared identity
// LAW: docs/FIXED-FORM-ALGORITHM.md:425 — "A rename THROUGH `was =` is not a
// version at all: an id is the hash of the wire name and `was` keeps the old
// one, so the layout bytes do not move and the two generations share one
// hash."; docs/FIXED-FORM-ALGORITHM.md:1549 — "A `was =` RENAME IS PAIRED BY
// WIRE ID IN ANY HARNESS, NEVER BY NAME. §5.2 matches a field by
// `fnv1a64(wire name)`, so `b was = a` matches the OLD name while the VALUE
// SURFACE is spelled with the NEW one."; docs/SPEC-TABLES.md:3259 — "Renaming
// is not a change: `was = "old_name"` keeps a field's identity through a
// rename: the wire id stays the hash of the old name."
// CELL: dart/E6
// TEST: In the generated fixed-form tables code, the renamed field's layout
// entry carries the hash of the OLD wire name and never of the new one, both
// generations hold that one declared identity, and the field round-trips on
// it: the value surface spells the new name, the wire id is the old name's.
//
// V1/V2 (test/tables/V1.schema, V2.schema) are the evolution pair: V2 declares
// `title string(32) | was = "name"` where V1 declares `name string(32)`.
//
// DERIVATIONS (every constant this test pins):
// - fnv1a64: the wire id is the hash of the wire name (docs/SPEC-TABLES.md §5)
//   — offset basis 0xCBF29CE484222325, prime 0x100000001B3, one byte at a
//   time, little-endian order as spelled in ir/tablewire.go TableWireId.
// - A LAYOUT is a u32 entry count then 17-byte entries: u64 id, u8 kind, u32
//   size, u32 children, all little-endian (TableFixedLimits.entryBytes = 17 in
//   the generated runtime; the walk is a pre-order in the writer's declared
//   order).
// - kind 12 is a fixed-form text row (u32 length then the bytes) and
//   string(32)'s size is 36: 32 bytes of buffer + 4 of length (the frozen §3
//   kind vocabulary, docs/SPEC-TABLES.md §3; the Cell label row decodes the
//   same way).

import 'dart:typed_data';
import 'dart:io';

import '../../../../build/tables-generated-dart/v1/V1Fixed.dart' as v1;
import '../../../../build/tables-generated-dart/v2/V2Fixed.dart' as v2;
import '../../../../build/tables-generated-dart/v2/Tblv2Fixed.dart' as v2rt;

/// fnv1a64 of a wire name, as ir.TableWireId computes it.
int fnv1a64(String name) {
  var h = 0xCBF29CE484222325;
  for (var i = 0; i < name.length; i++) {
    h ^= name.codeUnitAt(i) & 0xFF;
    h *= 0x100000001B3;
  }
  return h;
}

/// Every layout entry whose id is `want`, decoded off the 17-byte rows.
List<int> entriesWithId(Uint8List layout, int want) {
  final view = ByteData.sublistView(layout);
  final count = view.getUint32(0, Endian.little);
  final found = <int>[];
  for (var i = 0; i < count; i++) {
    final at = 4 + i * 17;
    if (view.getUint64(at, Endian.little) == want) {
      found.add(i);
    }
  }
  return found;
}

bool entryLooksLikeText(Uint8List layout, int entry) {
  final view = ByteData.sublistView(layout);
  final at = 4 + entry * 17;
  return layout[at + 8] == 12 && // TableKindString, the frozen §3 code
      view.getUint32(at + 9, Endian.little) == 36 && // string(32): 32 + 4
      view.getUint32(at + 13, Endian.little) == 0; // a text row has no children
}

void main() {
  var failed = false;

  void check(bool ok, String msg) {
    print('${ok ? 'ok' : 'FAIL'}: $msg');
    if (!ok) {
      failed = true;
    }
  }

  final nameId = fnv1a64('name');
  final titleId = fnv1a64('title');
  check(nameId != titleId, 'E6: the two spellings hash apart');

  // ---- THE NEW BUILD'S LAYOUT CARRIES THE OLD NAME'S ID ----
  // V2's `title was = "name"`: the entry the renamed field rides is the hash
  // of the OLD wire name, spelled with the NEW name on the value surface.
  final v2NameEntries = entriesWithId(v2.cfgFixedLayout, nameId);
  check(
    v2NameEntries.length == 1,
    'E6: v2\'s Cfg layout carries exactly ONE entry for id(fnv1a64("name")) '
    '(found ${v2NameEntries.length})',
  );
  check(
    v2NameEntries.isNotEmpty && entryLooksLikeText(v2.cfgFixedLayout, v2NameEntries.first),
    'E6: that entry is the text row kind=12 size=36 — V2\'s string(32), the '
    'renamed field\'s slot',
  );
  check(
    entriesWithId(v2.cfgFixedLayout, titleId).isEmpty,
    'E6: no entry carries id(fnv1a64("title")) — the NEW name contributes no '
    'wire identity',
  );

  // ---- THE OLD BUILD HOLDS THE SAME DECLARED IDENTITY ----
  final v1NameEntries = entriesWithId(v1.cfgFixedLayout, nameId);
  check(
    v1NameEntries.length == 1 &&
        entryLooksLikeText(v1.cfgFixedLayout, v1NameEntries.first),
    'E6: v1\'s Cfg layout holds the SAME id for its `name` field — one '
    'declared identity under both spellings',
  );

  // ---- THE BUILD'S VERSION IS THAT LAYOUT ----
  // The rename moved not one byte: the recorded lineage entry IS the writer's
  // layout, the one whose renamed entry holds the old name's id, and the
  // build's own hash is the hash the lock recorded of it.
  check(
    v2.cfgFixedKnown.length == 1,
    'E6: v2\'s Cfg lineage holds exactly one generation',
  );
  check(
    v2.cfgFixedKnown[0].hash == v2.cfgFixedHash,
    'E6: the recorded entry\'s hash is the build\'s own',
  );
  var same = v2.cfgFixedKnown[0].layout.length == v2.cfgFixedLayout.length;
  for (var i = 0; same && i < v2.cfgFixedLayout.length; i++) {
    same = v2.cfgFixedKnown[0].layout[i] == v2.cfgFixedLayout[i];
  }
  check(
    same,
    'E6: the recorded layout bytes are the writer\'s — the rename moved no '
    'layout byte',
  );

  // ---- THE VALUE SURFACE IS SPELLED WITH THE NEW NAME ----
  // The identity read: the field round-trips under the declared identity, and
  // the surface a consumer reads is `title` — the new spelling.
  {
    final one = v2rt.Cfg();
    one.title.setRange(0, 5, 'hello'.codeUnits);
    one.titleLength = 5;
    final file = Uint8List(v2.cfgFixedMeasure(1));
    check(
      v2.cfgFixedSave(<v2rt.Cfg>[one], 1, file) == file.length,
      'E6: v2 saves the renamed field',
    );
    final back = <v2rt.Cfg>[v2rt.Cfg()];
    final r = v2rt.TableFixedReport();
    final n = v2.cfgFixedLoad(
      back,
      1,
      file,
      file.length,
      v2.cfgFixedNewPlan(),
      r,
    );
    check(n == 1, 'E6: the identity read returns');
    final text = String.fromCharCodes(
      back[0].title.sublist(0, back[0].titleLength),
    );
    check(
      text == 'hello',
      'E6: `title` reads back the bytes it wrote under the declared identity',
    );
    check(
      !r.malformed &&
          r.refused == 0 &&
          r.clamped == 0 &&
          r.unknown == 0 &&
          r.widened == 0,
      'E6: the identity read is clean — no refusal, no counter moved',
    );
  }

  if (failed) {
    exit(1);
  }
  print('E6: renaming uses the declared identity — PASS');
}
