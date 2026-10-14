// CARD: cell-dart-field-evolution E8 — append and deprecate under the
// backward-read contract
// LAW: docs/SPEC-TABLES.md:116 — a fixed table is "versioned by appending and
// deprecating in place."; docs/SPEC-TABLES.md:16416 — "A newer reader reads
// every older file. An older reader given a newer file refuses it by name,
// before any record, and reads nothing."; docs/SPEC-TABLES.md:3171 — "A fixed
// table evolves APPEND-ONLY. A new field goes at the BOTTOM and nowhere else.
// A field that has outlived its use is DEPRECATED IN PLACE — it keeps its
// slot forever."
// CELL: dart/E8
// TEST: In the generated fixed-form tables code the appended field rides the
// new generation's versioned layout under its own declared identity and the
// old generation has no such slot; and the backward-read contract holds — an
// older reader refuses the newer file (the file that carries the appended
// slot) BY NAME, before any record, reading nothing, counters unmoved, the
// caller's poisoned storage untouched, the file's own hash reported.
//
// V1/V2 (test/tables/V1.schema, V2.schema) are the evolution pair: V2 carries
// `c bool = true` where V1 has no such field.
//
// DERIVATIONS (every constant this test pins):
// - fnv1a64: the wire id is the hash of the wire name (docs/SPEC-TABLES.md §5)
//   — offset basis 0xCBF29CE484222325, prime 0x100000001B3, one byte at a
//   time, as spelled in ir/tablewire.go TableWireId.
// - A LAYOUT is a u32 entry count then 17-byte entries: u64 id, u8 kind, u32
//   size, u32 children, all little-endian (TableFixedLimits.entryBytes = 17 in
//   the generated runtime; the walk is a pre-order in the writer's declared
//   order).
// - kind 1 is a bool (the frozen §3 kind vocabulary, docs/SPEC-TABLES.md §3);
//   a bool's slot is one byte.
// - refusal 15 is `layout_newer`: the integers are each leg's own, taken in
//   the order §5.3 names the refusals, and this leg's 15 agrees with the JS
//   leg's TableFixedRefusal.LayoutNewer = 15 (internal/codegen/jstable/
//   fixedruntime.go:135); the NAME is the contract.
// - 0x0EADBEEFCAFEF00D is a forged header hash no lineage entry holds, the
//   versioning gate's hash_unknown case.
// - the 0x5A POISON is §5.7/§5.9 #17's discipline: a refusal that ran a decode
//   step would leave a mark in the caller's storage.

import 'dart:typed_data';
import 'dart:io';

import '../../../../build/tables-generated-dart/v1/V1Fixed.dart' as v1;
import '../../../../build/tables-generated-dart/v1/Tblv1Fixed.dart' as v1rt;
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

bool entryIsBoolSlot(Uint8List layout, int entry) {
  final view = ByteData.sublistView(layout);
  final at = 4 + entry * 17;
  return layout[at + 8] == 1 && // TableKindBool, the frozen §3 code
      view.getUint32(at + 9, Endian.little) == 1 && // one byte
      view.getUint32(at + 13, Endian.little) == 0;
}

void main() {
  var failed = false;

  void check(bool ok, String msg) {
    print('${ok ? 'ok' : 'FAIL'}: $msg');
    if (!ok) {
      failed = true;
    }
  }

  // ---- THE APPENDED FIELD RIDES THE NEW GENERATION'S LAYOUT ----
  final cId = fnv1a64('c');
  final v2C = entriesWithId(v2.cfgFixedLayout, cId);
  check(
    v2C.length == 1 && entryIsBoolSlot(v2.cfgFixedLayout, v2C.first),
    'E8: v2\'s Cfg layout carries exactly ONE entry for the appended field '
    '`c` — kind=1 (bool), one byte',
  );
  check(
    entriesWithId(v1.cfgFixedLayout, cId).isEmpty,
    'E8: v1\'s Cfg layout carries no such slot — the old writer could not '
    'have written one',
  );

  // ---- THE BACKWARD-READ CONTRACT ----
  // One V2 record, carrying the appended slot at a NON-default value, offered
  // to the OLD build's reader.
  final one = v2rt.Cfg();
  one.title.setRange(0, 5, 'hello'.codeUnits);
  one.titleLength = 5;
  one.c = false; // c's declared default is true
  final file = Uint8List(v2.cfgFixedMeasure(1));
  check(
    v2.cfgFixedSave(<v2rt.Cfg>[one], 1, file) == file.length,
    'E8: v2 saves its record — a file whose layout carries the appended slot',
  );

  // THE POISON: the caller's record storage, filled before the load.
  final plan = v1.cfgFixedNewPlan();
  plan.image.fillRange(0, plan.image.length, 0x5A);

  final back = <v1rt.Cfg>[v1rt.Cfg()];
  final r = v1rt.TableFixedReport();
  final n = v1.cfgFixedLoad(back, 1, file, file.length, plan, r);

  // REFUSED BY NAME, before any record: `layout_newer`, the older build's
  // answer to a newer file. The integer 15 is this leg's own spelling of the
  // §5.3 name (the JS leg agrees); the name is the contract.
  check(n == -1, 'E8: the older reader refuses the newer file (n=$n)');
  check(r.refused == 15, 'E8: the refusal is 15, the legs\' shared spelling '
      'of layout_newer (got ${r.refused})');
  check(
    v1rt.TableFixedRefusal.name(r.refused) == 'layout_newer',
    'E8: refused BY NAME — ${v1rt.TableFixedRefusal.name(r.refused)}',
  );
  check(!r.malformed, 'E8: a refusal by name is not damage (malformed=false)');

  // REFUSE IS TOTAL: no counter moved, and the poisoned storage is untouched —
  // the refusal happened before any record, so nothing was decoded.
  check(
    r.unknown == 0 &&
        r.kindMismatch == 0 &&
        r.clamped == 0 &&
        r.widened == 0 &&
        r.duplicate == 0,
    'E8: refuse moves no counter (unknown=${r.unknown} kind=${r.kindMismatch} '
    'clamped=${r.clamped} widened=${r.widened} duplicate=${r.duplicate})',
  );
  var poison = true;
  for (var i = 0; i < plan.image.length; i++) {
    if (plan.image[i] != 0x5A) {
      poison = false;
      break;
    }
  }
  check(
    poison,
    'E8: the reader READS NOTHING — every poisoned byte of the caller\'s '
    'record storage is still 0x5A',
  );

  // THE REFUSAL CARRIES THE FILE'S HASH — read back from the bytes themselves.
  final fileHash = ByteData.sublistView(
    file,
  ).getUint64(8, Endian.little);
  check(
    r.layoutHash == fileHash,
    'E8: the refusal reports the FILE\'S hash '
    '(0x${r.layoutHash.toRadixString(16)})',
  );

  // A STRANGER'S HASH: a forged header hash no lineage entry holds is refused
  // the same way, and reports the FORGED value.
  ByteData.sublistView(
    file,
  ).setUint64(8, 0x0EADBEEFCAFEF00D, Endian.little);
  final plan2 = v1.cfgFixedNewPlan()..image.fillRange(0, plan.image.length, 0x5A);
  final r2 = v1rt.TableFixedReport();
  final n2 = v1.cfgFixedLoad(back, 1, file, file.length, plan2, r2);
  check(
    n2 == -1 &&
        r2.refused == 15 &&
        v1rt.TableFixedRefusal.name(r2.refused) == 'layout_newer' &&
        r2.layoutHash == 0x0EADBEEFCAFEF00D,
    'E8: a hash in no lineage entry refuses layout_newer and reports the '
    'file\'s (forged) hash',
  );
  var poison2 = true;
  for (var i = 0; i < plan2.image.length; i++) {
    if (plan2.image[i] != 0x5A) {
      poison2 = false;
      break;
    }
  }
  check(poison2, 'E8: the stranger-hash refusal reads nothing either');

  if (failed) {
    exit(1);
  }
  print('E8: append and deprecate under the backward-read contract — PASS');
}
