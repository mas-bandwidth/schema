// THE FIXED FORM'S DART LEG (docs/SPEC-TABLES.md §3.4), form byte 3.
//
// Four things are held here and the first two are the whole of the form:
//
//   1. THE BYTES ARE THE C++ REFERENCE'S. The reference writes eight form-3
//      files — the seven of `make tables-fixedform-corpus` and the paired
//      bench's own sixty-four logical records — and this leg reads each of
//      them, and writes it back, and the bytes must be IDENTICAL. The LAYOUT
//      and its fnv1a64 hash are checked against the same files, because two
//      writers whose layouts agree byte for byte agree on every id, kind, size
//      and position in the closure — and if they do not, nothing else in this
//      file means anything.
//
//   2. THE DECODED VALUES ARE CHECKED INDEPENDENTLY. A reader and a writer
//      that share one offset mistake round trip perfectly and are both wrong,
//      so the reference states the values too — by hand in
//      test/tables/fixedform_dump.cpp and as a JSON oracle beside the bench
//      corpus — and every field is compared against them.
//
//   3. THE VERSIONING CONFORMANCE, which is the invariant §3.4 states before
//      any wire: the form is positional BY PLAN, and the positions are the
//      WRITER's layout, never the reader's own declaration. This is the twin
//      of test/tables/fixedform_main.cpp, case for case — an older writer, a
//      newer one, a rename under `was =`, a widened field, an enum variant and
//      a union arm inserted IN THE MIDDLE, a keyed array whose keys slid, and
//      an optional against a value — every one of them through THE SAME LOOP
//      over a plan compiled from the other side's layout.
//
//   4. THE NEGATIVE CONTROLS. A reader given the WRONG PLAN must come out
//      WRONG; a form byte this build does not carry, a plan that does not fit
//      and ONE CORRUPTED-LAYOUT CASE PER NAMED RULE must each be refused under
//      their own name, with nothing decoded and no counter moved. A validation
//      nobody watched fail is a validation nobody has.
//
// THE LAYOUT is what form 1 called the vocabulary block. It is neither §7's
// cooked block nor §19's block form.
//
//   dart test/dart-tables/fixedform.dart <corpus> <bench-corpus>
//
// Run from the repository root, which is where the Makefile runs it. It is its
// own driver rather than a mode of test/dart-tables/fuzz.dart because it
// shares nothing with that leg: the fuzzer points at the two ACCELERATORS,
// which this form has no symbol of.

import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import '../../build/dart-fixed/bench/BenchFixed.dart' as benchHome;
import '../../build/dart-fixed/bench/FixedTableFixed.dart' as bench;
import '../../build/dart-fixed/examples/KeyedFixed.dart' as keyed;
import '../../build/dart-fixed/examples/PackFixed.dart' as pack;
import '../../build/dart-fixed/examples/TabledemoFixed.dart' as demo;
import '../../build/dart-fixed/fx1/FX1Fixed.dart' as fx1;
import '../../build/dart-fixed/fx1/Tblfx1Fixed.dart' as fx1home;
import '../../build/dart-fixed/fx2/FX2Fixed.dart' as fx2;
import '../../build/dart-fixed/fx2/Tblfx2Fixed.dart' as fx2home;
import '../../build/dart-fixed/fxw/FXWFixed.dart' as fxw;
import '../../build/dart-fixed/fxw/TblfxwFixed.dart' as fxwhome;
import '../../build/dart-fixed/p1/P1Fixed.dart' as p1;
import '../../build/dart-fixed/p1/Tblp1Fixed.dart' as p1home;
import '../../build/dart-fixed/p3/P3Fixed.dart' as p3;
import '../../build/dart-fixed/p3/Tblp3Fixed.dart' as p3home;
import '../../build/dart-fixed/v1/Tblv1Fixed.dart' as v1home;
import '../../build/dart-fixed/v1/V1.dart' as v1decl;
import '../../build/dart-fixed/v1/V1Fixed.dart' as v1;
import '../../build/dart-fixed/v2/Tblv2Fixed.dart' as v2home;
import '../../build/dart-fixed/v2/V2.dart' as v2decl;
import '../../build/dart-fixed/v2/V2Fixed.dart' as v2;

var failed = false;

void check(bool ok, String what) {
  if (!ok) {
    print('FAILED: $what');
    failed = true;
  }
}

// THE BYTE COMPARISON, which is the one this whole leg exists for: a writer
// that agrees with the reference everywhere but one byte is a writer nobody
// can read, so the first difference is NAMED and not merely counted.
void sameBytes(Uint8List got, Uint8List want, String what) {
  if (got.length != want.length) {
    check(
      false,
      '$what: ${got.length} bytes, the reference wrote ${want.length}',
    );
    return;
  }
  for (var i = 0; i < want.length; i++) {
    if (got[i] != want[i]) {
      check(
        false,
        '$what: first byte differing from the C++ reference at $i '
        '(dart ${got[i]}, cpp ${want[i]})',
      );
      return;
    }
  }
}

String text(Uint8List buffer, int length) =>
    String.fromCharCodes(buffer.sublist(0, length));

// WIDE TEXT'S OWN READER: the buffer is UTF-16 CODE UNITS and the length
// counts units, not bytes — which is the one thing that separates the text
// op's two flavours (docs/SPEC-TABLES.md §3.4).
String wide(Uint16List buffer, int length) =>
    String.fromCharCodes(buffer.sublist(0, length));

// A CLEAN READ MOVES NO COUNTER. §4's six events are what a read reports, and
// a same-schema read reports none of them.
void quiet(dynamic r, String what) {
  check(
    !r.malformed &&
        r.refused == 0 &&
        r.unknown == 0 &&
        r.kindMismatch == 0 &&
        r.clamped == 0 &&
        r.widened == 0,
    '$what: a clean read moves no counter',
  );
}

// ---------------------------------------------------------------------------
// 1 and 2. THE REFERENCE'S OWN FILES: its bytes, and its values
// ---------------------------------------------------------------------------

void corpusFiles(String dir) {
  Uint8List read(String name) => File('$dir/$name').readAsBytesSync();

  // THE FRAMING, before anything is decoded: the form byte, then the layout
  // length, then the layout — and the layout in the file is this build's own,
  // byte for byte, which is what makes the hash the same number.
  void framing(Uint8List file, Uint8List layout, int hash, String what) {
    // THE HEADER, ONE RULE FOR ALL FIVE FORMS (§3): the form byte at 0, seven
    // RESERVED ZERO bytes, the LAYOUT HASH at 8, and the body at 16.
    check(file[0] == 3, '$what: the file opens with form byte 3');
    var reserved = true;
    for (var i = 1; i < 8; i++) {
      reserved = reserved && file[i] == 0;
    }
    check(reserved, '$what: bytes 1..7 of the header are RESERVED and zero');
    final view = ByteData.sublistView(file);
    check(
      view.getUint64(8, Endian.little) == hash,
      '$what: the header names the layout once, at offset 8',
    );
    final stated = view.getUint32(layoutLengthAt, Endian.little);
    check(
      stated == layout.length,
      '$what: the layout length is $stated, this build has ${layout.length}',
    );
    var same = stated == layout.length;
    for (var i = 0; same && i < layout.length; i++) {
      same = file[layoutAt + i] == layout[i];
    }
    check(same, '$what: the LAYOUT is byte-identical to the C++ reference\'s');
    check(
      view.getUint64(layoutAt + layout.length, Endian.little) == hash,
      '$what: the first record carries this build\'s own layout hash too',
    );
  }

  // ---- fx1.bin: two records of FX1's FxRoot ----
  {
    final file = read('fx1.bin');
    framing(file, fx1.fxRootFixedLayout, fx1.fxRootFixedHash, 'fx1');
    final values = List.generate(8, (_) => fx1home.FxRoot());
    final report = fx1home.TableFixedReport();
    final n = fx1.fxRootFixedLoad(
      values,
      8,
      file,
      file.length,
      fx1.fxRootFixedNewPlan(),
      report,
    );
    check(n == 2, 'fx1: two records read (got $n)');
    quiet(report, 'fx1');
    // the values fixedform_dump.cpp sets by hand, so nothing passes by accident
    check(values[0].keep == 4242, 'fx1[0].keep');
    check(values[0].narrow == 40000, 'fx1[0].narrow');
    check(values[0].renamed == 321, 'fx1[0].renamed');
    check(values[0].gone == 654, 'fx1[0].gone');
    check(
      values[0].nested.a == 111 && values[0].nested.b == 222,
      'fx1[0].nested',
    );
    check(values[1].keep == 1 && values[1].narrow == 2, 'fx1[1] scalars');
    check(values[1].renamed == 3 && values[1].gone == 4, 'fx1[1] more scalars');
    check(values[1].nested.a == 5 && values[1].nested.b == 6, 'fx1[1].nested');
    final out = Uint8List(fx1.fxRootFixedMeasure(n));
    check(fx1.fxRootFixedSave(values, n, out) == out.length, 'fx1: save');
    sameBytes(out, file, 'fx1');

    // A DECLARED DEFAULT IS THE VALUE, NOT JUST ITS LENGTH. The round trip
    // above cannot see this: a value READ from the file already has the
    // record's bytes. C++ writes `char label[8 + 1] = "fx"`; a fresh FxRoot
    // used to claim two used bytes of NULs.
    final fresh = fx1home.FxRoot();
    check(
      fresh.labelLength == 2 && text(fresh.label, 2) == 'fx',
      'fx1: a fresh FxRoot carries label "fx", not two NULs of length 2',
    );
  }

  // ---- fx2.bin: one record of FX2's FxRoot, with the extra nested type ----
  {
    final file = read('fx2.bin');
    framing(file, fx2.fxRootFixedLayout, fx2.fxRootFixedHash, 'fx2');
    final values = List.generate(8, (_) => fx2home.FxRoot());
    final report = fx2home.TableFixedReport();
    final n = fx2.fxRootFixedLoad(
      values,
      8,
      file,
      file.length,
      fx2.fxRootFixedNewPlan(),
      report,
    );
    check(n == 1, 'fx2: one record read (got $n)');
    quiet(report, 'fx2');
    check(values[0].keep == 5150, 'fx2[0].keep');
    check(values[0].narrow == 70000, 'fx2[0].narrow');
    check(values[0].renamedTo == 808, 'fx2[0].renamed_to');
    check(values[0].added == 909, 'fx2[0].added');
    check(
      values[0].nested.a == 33 && values[0].nested.b == 44,
      'fx2[0].nested',
    );
    check(values[0].extra.x == 55 && values[0].extra.y == 66, 'fx2[0].extra');
    final out = Uint8List(fx2.fxRootFixedMeasure(n));
    check(fx2.fxRootFixedSave(values, n, out) == out.length, 'fx2: save');
    sameBytes(out, file, 'fx2');
  }

  // ---- p1.bin: a string(N) and a nested table BY VALUE ----
  {
    final file = read('p1.bin');
    framing(file, p1.chainFixedLayout, p1.chainFixedHash, 'p1');
    final values = List.generate(8, (_) => p1home.Chain());
    final report = p1home.TableFixedReport();
    final n = p1.chainFixedLoad(
      values,
      8,
      file,
      file.length,
      p1.chainFixedNewPlan(),
      report,
    );
    check(n == 1, 'p1: one record read (got $n)');
    quiet(report, 'p1');
    check(
      text(values[0].name, values[0].nameLength) == 'chain-one',
      'p1[0].name',
    );
    check(values[0].link.value == 77, 'p1[0].link.value');
    check(
      text(values[0].link.tag, values[0].link.tagLength) == 'tagged',
      'p1[0].link.tag',
    );
    final out = Uint8List(p1.chainFixedMeasure(n));
    check(p1.chainFixedSave(values, n, out) == out.length, 'p1: save');
    sameBytes(out, file, 'p1');
  }

  // ---- p3.bin: the OPTIONAL, present and absent, the payload riding WHOLE ----
  {
    final file = read('p3.bin');
    framing(file, p3.chainFixedLayout, p3.chainFixedHash, 'p3');
    final values = List.generate(8, (_) => p3home.Chain());
    final report = p3home.TableFixedReport();
    final n = p3.chainFixedLoad(
      values,
      8,
      file,
      file.length,
      p3.chainFixedNewPlan(),
      report,
    );
    check(n == 2, 'p3: two records read (got $n)');
    quiet(report, 'p3');
    check(
      text(values[0].name, values[0].nameLength) == 'present',
      'p3[0].name',
    );
    check(values[0].linkPresent, 'p3[0]: the present flag is set');
    check(values[0].link.value == 88, 'p3[0].link.value');
    check(
      text(values[0].link.tag, values[0].link.tagLength) == 'here',
      'p3[0].link.tag',
    );
    check(text(values[1].name, values[1].nameLength) == 'absent', 'p3[1].name');
    check(!values[1].linkPresent, 'p3[1]: the present flag is clear');
    // THE PAYLOAD RIDES WHOLE WHETHER OR NOT IT IS PRESENT (§3.4) — its bytes
    // are on the wire and the read moves them — AND UNDER A CLEAR FLAG WHAT
    // RIDES IS ZEROS, because there the payload is SLACK and slack is ZERO ON
    // WRITE. So the absent record's link reads as zeros, and read-then-save
    // reproduces the file because the file is what a conforming writer wrote.
    check(
      values[1].link.value == 0,
      'p3[1]: an absent optional\'s payload is zeros',
    );
    check(values[1].link.tagLength == 0, 'p3[1].link.tag is empty');
    final out = Uint8List(p3.chainFixedMeasure(n));
    check(p3.chainFixedSave(values, n, out) == out.length, 'p3: save');
    sameBytes(out, file, 'p3');
  }

  // ---- fxw.bin: WIDE TEXT (kind 33), the only oracle bytes the wide
  //      flavour of the text op has ----
  {
    final file = read('fxw.bin');
    framing(file, fxw.fxWideFixedLayout, fxw.fxWideFixedHash, 'fxw');
    final values = List.generate(4, (_) => fxwhome.FxWide());
    final report = fxwhome.TableFixedReport();
    final n = fxw.fxWideFixedLoad(
      values,
      4,
      file,
      file.length,
      fxw.fxWideFixedNewPlan(),
      report,
    );
    check(n == 2, 'fxw: two records read (got $n)');
    quiet(report, 'fxw');

    // A WIDE LENGTH IS IN CODE UNITS AND THE PAYLOAD IS TWO BYTES EACH (§3.4).
    // Seven units, one short of the bound.
    check(values[0].captionLength == 7, 'fxw[0].caption_length is in units');
    check(
      wide(values[0].caption, values[0].captionLength) == 'hello !',
      'fxw[0].caption',
    );
    // and the narrow field beside it, whose length is in BYTES
    check(values[0].labelLength == 6, 'fxw[0].label_length is in bytes');
    check(
      text(values[0].label, values[0].labelLength) == 'narrow',
      'fxw[0].label',
    );
    check(values[0].inner.textLength == 4, 'fxw[0].inner.text_length');
    check(
      wide(values[0].inner.text, values[0].inner.textLength) == 'abcd',
      'fxw[0].inner.text — the wide flavour at a NESTED offset',
    );
    check(values[0].seq == 41, 'fxw[0].seq');

    // AN ASTRAL PAIR IS TWO CODE UNITS AND THE LENGTH COUNTS BOTH, which is
    // the number a leg counting bytes gets wrong by a factor of two.
    check(
      values[1].captionLength == 5,
      'fxw[1].caption_length counts the pair',
    );
    check(values[1].caption[0] == 0xE000, 'fxw[1].caption[0]');
    check(values[1].caption[1] == 0xD83D, 'fxw[1].caption[1] — the HIGH half');
    check(values[1].caption[2] == 0xDE00, 'fxw[1].caption[2] — the LOW half');
    check(values[1].caption[3] == 0xFFFF, 'fxw[1].caption[3]');
    check(values[1].caption[4] == 0x7A, 'fxw[1].caption[4]');
    check(values[1].labelLength == 0, 'fxw[1].label is empty');
    check(values[1].inner.textLength == 0, 'fxw[1].inner.text is empty');
    check(values[1].seq == 1000, 'fxw[1].seq is the declared max');

    final out = Uint8List(fxw.fxWideFixedMeasure(n));
    check(fxw.fxWideFixedSave(values, n, out) == out.length, 'fxw: save');
    sameBytes(out, file, 'fxw');
  }

  // ---- keyed.bin: keyed arrays NESTING keyed arrays, and an optional ----
  {
    final file = read('keyed.bin');
    framing(
      file,
      keyed.keyedConfigFixedLayout,
      keyed.keyedConfigFixedHash,
      'keyed',
    );
    final values = List.generate(8, (_) => demo.KeyedConfig());
    final report = demo.TableFixedReport();
    final n = keyed.keyedConfigFixedLoad(
      values,
      8,
      file,
      file.length,
      keyed.keyedConfigFixedNewPlan(),
      report,
    );
    check(n == 2, 'keyed: two records read (got $n)');
    quiet(report, 'keyed');
    const banners = <String>['red', 'blue', 'green'];
    for (var k = 0; k < 2; k++) {
      for (var t = 0; t < 3; t++) {
        check(
          values[k].teams[t].spawnCount == 4 + t + k * 10,
          'keyed[$k].teams[$t].spawn_count',
        );
        check(
          text(values[k].teams[t].banner, values[k].teams[t].bannerLength) ==
              banners[t],
          'keyed[$k].teams[$t].banner',
        );
        check(
          values[k].scores.perTeam[t] == 1000 * (t + 1) + k,
          'keyed[$k].scores.per_team[$t]',
        );
      }
      for (var h = 0; h < 3; h++) {
        check(
          values[k].hulls[h].health == 100.0 + h + k,
          'keyed[$k].hulls[$h].health',
        );
        check(
          values[k].hulls[h].mass == 1.5 * (h + 1),
          'keyed[$k].hulls[$h].mass',
        );
        for (var w = 0; w < 3; w++) {
          final turret = values[k].hulls[h].turrets[w];
          check(
            turret.damage == 10.0 + (h * 3 + w),
            'keyed[$k].hulls[$h].turrets[$w].damage',
          );
          check(
            turret.cooldown == 0.25 * (w + 1),
            'keyed[$k].hulls[$h].turrets[$w].cooldown',
          );
          check(
            turret.gunnerPresent == ((h + w) % 2 == 0),
            'keyed[$k]…turrets[$w].gunner present',
          );
          // A PAYLOAD UNDER A CLEAR FLAG IS SLACK AND READS AS ZEROS (§3.4):
          // a present gunner carries its values, an absent one carries none —
          // not even the declared default `reaction = 0.2`, which is meaning.
          check(
            turret.gunner.tracking == (turret.gunnerPresent && (w % 2 == 1)),
            'keyed[$k]…turrets[$w].gunner.tracking',
          );
          check(
            turret.gunnerPresent || turret.gunner.reaction == 0.0,
            'keyed[$k]…turrets[$w]: an absent gunner is zeros, '
            'not the declared 0.2',
          );
        }
      }
    }
    final out = Uint8List(keyed.keyedConfigFixedMeasure(n));
    check(
      keyed.keyedConfigFixedSave(values, n, out) == out.length,
      'keyed: save',
    );
    sameBytes(out, file, 'keyed');
  }

  // ---- pack.bin: counted arrays, an enum with a declared default, a fixed
  //      array of floats, and an optional section inside a record ----
  {
    final file = read('pack.bin');
    framing(file, pack.packConfigFixedLayout, pack.packConfigFixedHash, 'pack');
    final values = List.generate(8, (_) => demo.PackConfig());
    final report = demo.TableFixedReport();
    final n = pack.packConfigFixedLoad(
      values,
      8,
      file,
      file.length,
      pack.packConfigFixedNewPlan(),
      report,
    );
    check(n == 2, 'pack: two records read (got $n)');
    quiet(report, 'pack');
    const names = <String>['fighter', 'bomber', 'scout'];
    const calls = <String>['ace', 'hammer', 'ghost'];
    const spares = <String>['spare-a', 'spare-b'];
    for (var k = 0; k < 2; k++) {
      final v = values[k];
      check(v.version == 7 + k, 'pack[$k].version');
      check(v.global.tickRate == 120 - k, 'pack[$k].global.tick_rate');
      check(
        text(v.global.buildNote, v.global.buildNoteLength) ==
            (k == 0 ? 'first build' : 'second build'),
        'pack[$k].global.build_note',
      );
      for (var i = 0; i < 3; i++) {
        check(
          v.global.spawnDelays[i] == 0.5 * (i + 1 + k),
          'pack[$k].global.spawn_delays[$i]',
        );
      }
      for (var s = 0; s < 3; s++) {
        final ship = v.ships[s];
        check(
          text(ship.displayName, ship.displayNameLength) == names[s],
          'pack[$k].ships[$s].display_name',
        );
        check(ship.health == 100.0 + (s * 10 + k), 'pack[$k].ships[$s].health');
        check(ship.mass == 1.0 + 0.25 * s, 'pack[$k].ships[$s].mass');
        check(
          ship.hardpointsCount == s + 1,
          'pack[$k].ships[$s].hardpoints_count',
        );
        for (var h = 0; h < s + 1; h++) {
          check(
            ship.hardpoints[h] == h + 1,
            'pack[$k].ships[$s].hardpoints[$h]',
          );
        }
        check(
          ship.gunnerPresent == (s % 2 == 0),
          'pack[$k].ships[$s] gunner present',
        );
        // as in keyed: an absent gunner's payload is slack, so it is zeros
        // and its callsign is empty (§3.4)
        check(
          text(ship.gunner.callsign, ship.gunner.callsignLength) ==
              (ship.gunnerPresent ? calls[s] : ''),
          'pack[$k].ships[$s].gunner.callsign',
        );
        check(
          ship.gunnerPresent || ship.gunner.reaction == 0.0,
          'pack[$k].ships[$s]: an absent gunner is zeros, '
          'not the declared 0.2',
        );
        check(v.thresholds[s] == 100 * (s + 1) + k, 'pack[$k].thresholds[$s]');
      }
      check(v.reservesCount == 2, 'pack[$k].reserves_count');
      for (var r = 0; r < 2; r++) {
        check(
          text(v.reserves[r].displayName, v.reserves[r].displayNameLength) ==
              spares[r],
          'pack[$k].reserves[$r].display_name',
        );
        check(v.reserves[r].health == 50.0 + r, 'pack[$k].reserves[$r].health');
        check(
          v.reserves[r].hardpointsCount == 1,
          'pack[$k].reserves[$r].hardpoints_count',
        );
        check(
          !v.reserves[r].gunnerPresent,
          'pack[$k].reserves[$r] gunner absent',
        );
        check(
          v.reserves[r].gunner.reaction == 0.0,
          'pack[$k].reserves[$r]: the absent gunner\'s payload is zeros',
        );
      }
    }
    final out = Uint8List(pack.packConfigFixedMeasure(n));
    check(pack.packConfigFixedSave(values, n, out) == out.length, 'pack: save');
    sameBytes(out, file, 'pack');
  }
}

// ---------------------------------------------------------------------------
// THE PAIRED CORPUS: sixty-four logical records, and the reference's values
// ---------------------------------------------------------------------------

final ByteData bits = ByteData(8);

// THE ORACLE STATES A 64-BIT VALUE AS A DECIMAL STRING, unsigned. A Dart int
// is SIGNED sixty-four bits, so a value past 2^63 is read as the BIT PATTERN
// it rides as on the wire — which is what this form carries and what the Dart
// storage holds.
int wide64(Object? decimal) =>
    BigInt.parse(decimal! as String).toSigned(64).toInt();

int f32Bits(double v) {
  bits.setFloat32(0, v, Endian.little);
  return bits.getUint32(0, Endian.little);
}

int f64Bits(double v) {
  bits.setFloat64(0, v, Endian.little);
  return bits.getUint64(0, Endian.little);
}

void pairedCorpus(String dir) {
  final file = File('$dir/bench_fixed.bin').readAsBytesSync();
  final oracle = jsonDecode(
    File('$dir/bench_fixed.oracle.json').readAsStringSync(),
  ) as List<dynamic>;
  final count = oracle.length;

  check(file[0] == 3, 'bench: the file opens with form byte 3');
  final view = ByteData.sublistView(file);
  check(
    view.getUint64(8, Endian.little) == bench.fixedTableFixedHash,
    'bench: the header names the layout once, at offset 8',
  );
  final stated = view.getUint32(layoutLengthAt, Endian.little);
  check(
    stated == bench.fixedTableFixedLayoutBytes,
    'bench: the file\'s layout length $stated is this build\'s ${bench.fixedTableFixedLayoutBytes}',
  );
  var same = stated == bench.fixedTableFixedLayoutBytes;
  for (var i = 0; same && i < stated; i++) {
    same = file[layoutAt + i] == bench.fixedTableFixedLayout[i];
  }
  check(same, 'bench: the LAYOUT is byte-identical to the C++ reference\'s');
  check(
    file.length == bench.fixedTableFixedMeasure(count),
    'bench: Measure is a constant and it is the file\'s own length',
  );
  check(
    bench.fixedTableFixedBodyBytes == 1236,
    'bench: the body is the reference\'s 1236 bytes',
  );

  final values = List.generate(count, (_) => benchHome.FixedTable());
  final report = benchHome.TableFixedReport();
  final n = bench.fixedTableFixedLoad(
    values,
    count,
    file,
    file.length,
    bench.fixedTableFixedNewPlan(),
    report,
  );
  check(n == count, 'bench: $count records read (got $n)');
  quiet(report, 'bench');

  var bad = 0;
  var allEqual = true;
  void eq(Object? got, Object? want, String what, int k) {
    if (got != want) {
      allEqual = false;
      if (bad < 8) {
        check(false, 'bench: record $k $what: $got != $want');
        bad++;
      }
    }
  }

  for (var k = 0; k < count; k++) {
    final v = values[k].value;
    final o = oracle[k] as Map<String, dynamic>;
    eq(v.sequence, o['sequence'], 'sequence', k);
    eq(v.ackSequence, o['ack_sequence'], 'ack_sequence', k);
    eq(v.ackBits, o['ack_bits'], 'ack_bits', k);
    eq(v.sessionId, wide64(o['session_id']), 'session_id', k);
    eq(v.clientId, o['client_id'], 'client_id', k);
    // A DART int IS SIGNED, so an unsigned 64-bit value past 2^63 is compared
    // as the BIT PATTERN it rides as — which is what this wire carries.
    eq(v.nonce, wide64(o['nonce']), 'nonce', k);
    eq(v.worldTime, wide64(o['world_time']), 'world_time', k);
    eq(v.frameTick, wide64(o['frame_tick']), 'frame_tick', k);
    eq(v.serverTime, o['server_time'], 'server_time', k);
    eq(v.entitiesCount, o['entities_count'], 'entities_count', k);
    final entities = o['entities'] as List<dynamic>;
    for (var i = 0; i < (o['entities_count'] as int); i++) {
      final e = v.entities[i];
      final oe = entities[i] as Map<String, dynamic>;
      eq(e.entityId, oe['entity_id'], 'entities[$i].entity_id', k);
      eq(e.posX, oe['pos_x'], 'entities[$i].pos_x', k);
      eq(e.posY, oe['pos_y'], 'entities[$i].pos_y', k);
      eq(e.posZ, oe['pos_z'], 'entities[$i].pos_z', k);
      eq(e.yaw, oe['yaw'], 'entities[$i].yaw', k);
      eq(e.pitch, oe['pitch'], 'entities[$i].pitch', k);
      eq(e.velX, oe['vel_x'], 'entities[$i].vel_x', k);
      eq(e.velY, oe['vel_y'], 'entities[$i].vel_y', k);
      eq(e.velZ, oe['vel_z'], 'entities[$i].vel_z', k);
      eq(e.health, oe['health'], 'entities[$i].health', k);
      eq(e.weapon, oe['weapon'], 'entities[$i].weapon', k);
      eq(e.damage, wide64(oe['damage']), 'entities[$i].damage', k);
      eq(e.moving, oe['moving'], 'entities[$i].moving', k);
      eq(e.firing, oe['firing'], 'entities[$i].firing', k);
    }
    eq(v.statsCount, o['stats_count'], 'stats_count', k);
    final stats = o['stats'] as List<dynamic>;
    for (var i = 0; i < (o['stats_count'] as int); i++) {
      final os = stats[i] as Map<String, dynamic>;
      eq(v.stats[i].statId, os['stat_id'], 'stats[$i].stat_id', k);
      eq(v.stats[i].delta, os['delta'], 'stats[$i].delta', k);
    }
    // THE UNION: the tag, then the arm the tag names and only that arm
    eq(v.gameEvent.type, o['game_event_type'], 'game_event.type', k);
    switch (o['game_event_type'] as int) {
      case 1:
        final hit = o['hit'] as Map<String, dynamic>;
        eq(v.gameEvent.hit.targetId, hit['target_id'], 'hit.target_id', k);
        eq(v.gameEvent.hit.damage, hit['damage'], 'hit.damage', k);
        eq(v.gameEvent.hit.hitKind, hit['hit_kind'], 'hit.hit_kind', k);
        eq(v.gameEvent.hit.crit, hit['crit'], 'hit.crit', k);
        break;
      case 2:
        final chat = o['chat'] as Map<String, dynamic>;
        eq(v.gameEvent.chat.channel, chat['channel'], 'chat.channel', k);
        eq(v.gameEvent.chat.speaker, chat['speaker'], 'chat.speaker', k);
        break;
      case 3:
        final pickup = o['pickup'] as Map<String, dynamic>;
        eq(v.gameEvent.pickup.itemId, pickup['item_id'], 'pickup.item_id', k);
        eq(v.gameEvent.pickup.amount, pickup['amount'], 'pickup.amount', k);
        break;
      default:
        break;
    }
    final loadout = o['loadout'] as List<dynamic>;
    for (var i = 0; i < 4; i++) {
      eq(v.loadout[i], loadout[i], 'loadout[$i]', k);
    }
    eq(v.playerNameLength, o['player_name_length'], 'player_name_length', k);
    final playerName = o['player_name'] as List<dynamic>;
    for (var i = 0; i < 15; i++) {
      eq(v.playerName[i], playerName[i], 'player_name[$i]', k);
    }
    eq(v.payloadLength, o['payload_length'], 'payload_length', k);
    final payload = o['payload'] as List<dynamic>;
    for (var i = 0; i < 16; i++) {
      eq(v.payload[i], payload[i], 'payload[$i]', k);
    }
    // FLOATS ARE COMPARED AS BIT PATTERNS: this form carries the IEEE-754 bit
    // pattern with NO canonicalisation, so a NaN payload is part of the value.
    eq(f32Bits(v.aimX), o['aim_x_bits'], 'aim_x', k);
    eq(f32Bits(v.aimY), o['aim_y_bits'], 'aim_y', k);
    eq(f32Bits(v.aimZ), o['aim_z_bits'], 'aim_z', k);
    eq(f32Bits(v.recoil), o['recoil_bits'], 'recoil', k);
    eq(f64Bits(v.drift), wide64(o['drift_bits']), 'drift', k);
    // 128-BIT: the LOW half then the HIGH, which is this wire's order everywhere
    eq(v.wideKey.lo, wide64(o['wide_key_lo']), 'wide_key.lo', k);
    eq(v.wideKey.hi, wide64(o['wide_key_hi']), 'wide_key.hi', k);
    eq(v.flux.lo, wide64(o['flux_lo']), 'flux.lo', k);
    eq(v.flux.hi, wide64(o['flux_hi']), 'flux.hi', k);
    eq(v.ping, o['ping'], 'ping', k);
    eq(v.crcHint, o['crc_hint'], 'crc_hint', k);
    eq(v.hasExtra, o['has_extra'], 'has_extra', k);
    eq(v.extra, o['extra'], 'extra', k);
    eq(v.idleTicks, o['idle_ticks'], 'idle_ticks', k);
  }
  check(
    allEqual,
    'bench: every field of all $count records is the value the C++ reference states',
  );

  final out = Uint8List(bench.fixedTableFixedMeasure(count));
  check(
    bench.fixedTableFixedSave(values, count, out) == file.length,
    'bench: save',
  );
  sameBytes(out, file, 'bench');

  // A PLAN COMPILED FROM MY OWN LAYOUT MUST LAND WHAT THE IDENTITY PLAN LANDS.
  // Identity uses the TEXT op for bytes(N) and never reads the dest row;
  // compileEntry kind 14 treats counted as an array. BenchMixed.payload is
  // the field that would have been silent.
  {
    final plan = bench.fixedTableFixedNewPlan();
    check(
      plan.theirs.parse(
        ByteData.sublistView(bench.fixedTableFixedLayout),
        0,
        bench.fixedTableFixedLayout.length,
      ),
      'bench: own layout parses',
    );
    final rc = benchHome.TableFixedReport();
    final made = benchHome.TableFixedCompiler.compile(
      plan,
      bench.fixedTableFixedLayout,
      ByteData.sublistView(bench.fixedTableFixedLayout),
      bench.fixedTableFixedDst,
      rc,
    );
    check(made > 0, 'bench: compiled-from-own-layout wrote $made entries');
    plan.image.setRange(
      0,
      bench.fixedTableFixedBodyBytes,
      bench.fixedTableFixedPrefill,
    );
    benchHome.tableFixedRun(
      plan.entries,
      made,
      file,
      ByteData.sublistView(file),
      bench.fixedTableFixedHeaderBytes + 8,
      plan.image,
      plan.imageView,
      plan.remap,
      plan.conv,
      rc,
    );
    final compiled = benchHome.FixedTable();
    benchHome.fixedTableFixedDecode(
      compiled,
      plan.image,
      plan.imageView,
      0,
      rc,
    );
    var payloadSame =
        compiled.value.payloadLength == values[0].value.payloadLength;
    for (var i = 0; payloadSame && i < 16; i++) {
      payloadSame = compiled.value.payload[i] == values[0].value.payload[i];
    }
    check(
      payloadSame,
      'bench: compiled-from-own-layout payload matches identity',
    );
  }
}

// ---------------------------------------------------------------------------
// 3. THE VERSIONING CONFORMANCE — the twin of test/tables/fixedform_main.cpp
// ---------------------------------------------------------------------------

Uint8List fx1Record() {
  // an FX1 record, every field off its default so nothing passes by accident
  final one = fx1home.FxRoot();
  one.keep = 4242;
  one.narrow = 40000; // a uint16 value the widened read must reproduce
  one.renamed = 321;
  one.gone = 654;
  one.nested.a = 111;
  one.nested.b = 222;
  final w = Uint8List(fx1.fxRootFixedMeasure(1));
  check(
    fx1.fxRootFixedSave(<fx1home.FxRoot>[one], 1, w) == w.length,
    'FX1 save',
  );
  return w;
}

Uint8List fx2Record() {
  final two = fx2home.FxRoot();
  two.keep = 5150;
  two.narrow = 70000; // wider than FX1 holds: a kind that MOVED, not a widening
  two.renamedTo = 808;
  two.added = 909;
  two.nested.a = 33;
  two.nested.b = 44;
  two.extra.x = 55;
  two.extra.y = 66;
  final w = Uint8List(fx2.fxRootFixedMeasure(1));
  check(
    fx2.fxRootFixedSave(<fx2home.FxRoot>[two], 1, w) == w.length,
    'FX2 save',
  );
  return w;
}

void fxCase() {
  final w1 = fx1Record();

  // 1. SAME SCHEMA — the identity plan
  {
    final back = <fx1home.FxRoot>[fx1home.FxRoot()];
    final r = fx1home.TableFixedReport();
    final n = fx1.fxRootFixedLoad(
      back,
      1,
      w1,
      w1.length,
      fx1.fxRootFixedNewPlan(),
      r,
    );
    check(n == 1, 'same schema: one record');
    check(
      back[0].keep == 4242 &&
          back[0].narrow == 40000 &&
          back[0].renamed == 321 &&
          back[0].gone == 654,
      'same schema: the scalars',
    );
    check(
      back[0].nested.a == 111 && back[0].nested.b == 222,
      'same schema: the nesting',
    );
    quiet(r, 'same schema');
  }

  // 2, 4, 5. FX2 READS FX1 — a plan compiled from FX1's layout
  {
    final back = <fx2home.FxRoot>[fx2home.FxRoot()];
    final r = fx2home.TableFixedReport();
    final n = fx2.fxRootFixedLoad(
      back,
      1,
      w1,
      w1.length,
      fx2.fxRootFixedNewPlan(),
      r,
    );
    check(n == 1, 'older writer: one record');
    check(back[0].keep == 4242, 'older writer: an unmoved field');
    check(back[0].narrow == 40000, 'WIDENED: uint16 into uint32, exactly');
    check(r.widened == 1, 'WIDENED: one widened counts (got ${r.widened})');
    check(back[0].renamedTo == 321, 'RENAMED: `was =` keeps the wire id');
    check(
      back[0].added == 11,
      'MISSING: a field the writer does not carry takes its declared default',
    );
    check(
      back[0].extra.x == 0 && back[0].extra.y == 0,
      'MISSING: a whole nested type takes its defaults',
    );
    check(
      back[0].nested.a == 111 && back[0].nested.b == 222,
      'older writer: the nesting',
    );
    check(
      r.unknown == 1,
      'older writer: `gone` is the one field this reader cannot name (got ${r.unknown})',
    );
    check(
      r.kindMismatch == 0 && !r.malformed && r.refused == 0,
      'older writer: nothing else fired',
    );
  }

  // 3. FX1 READS FX2 — an unknown field and an unknown NESTED TYPE
  {
    final w2 = fx2Record();
    final back = <fx1home.FxRoot>[fx1home.FxRoot()];
    final r = fx1home.TableFixedReport();
    final n = fx1.fxRootFixedLoad(
      back,
      1,
      w2,
      w2.length,
      fx1.fxRootFixedNewPlan(),
      r,
    );
    check(n == 1, 'newer writer: one record');
    check(
      back[0].keep == 5150,
      'newer writer: an unmoved field lands past the unknowns',
    );
    check(
      back[0].renamed == 808,
      'newer writer: `was =` reads the other way too',
    );
    check(
      back[0].gone == 9,
      'newer writer: a field the writer dropped takes its declared default',
    );
    check(
      back[0].nested.a == 33 && back[0].nested.b == 44,
      'newer writer: the nesting lands past the unknown type',
    );
    // `added` is an unknown FIELD; `extra` is an unknown nested TYPE, and
    // stepping over it by its layout size is what puts `nested` in the right
    // place above
    check(
      r.unknown == 2,
      'newer writer: two names this reader does not have (got ${r.unknown})',
    );
    check(
      r.kindMismatch == 1,
      'newer writer: uint32 into uint16 is a kind that MOVED (got ${r.kindMismatch})',
    );
    check(
      back[0].narrow == 3,
      'newer writer: a narrowing leaves the declared default',
    );
    check(
      !r.malformed && r.refused == 0,
      'newer writer: no damage and no refusal',
    );
  }

  // THE PLAN IS COMPILED ONCE PER PEER, NOT ONCE PER RECORD, and the same plan
  // handed back is reused: the cache is by hash and the hash is the layout's.
  {
    final many = List.generate(4, (_) {
      final v = fx1home.FxRoot();
      v.keep = 1;
      v.narrow = 2;
      v.renamed = 3;
      v.gone = 4;
      v.nested.a = 5;
      v.nested.b = 6;
      return v;
    });
    final buf = Uint8List(fx1.fxRootFixedMeasure(4));
    check(
      fx1.fxRootFixedSave(many, 4, buf) == buf.length,
      'FX1 save: four records',
    );
    final plan = fx2.fxRootFixedNewPlan();
    final back = List.generate(4, (_) => fx2home.FxRoot());
    final r = fx2home.TableFixedReport();
    check(
      fx2.fxRootFixedLoad(back, 4, buf, buf.length, plan, r) == 4,
      'cached plan: four records',
    );
    check(plan.ready, 'cached plan: the plan is marked ready for its hash');
    final compiled = plan.count;
    final r2 = fx2home.TableFixedReport();
    check(
      fx2.fxRootFixedLoad(back, 4, buf, buf.length, plan, r2) == 4,
      'cached plan: read again on the same plan',
    );
    check(
      plan.count == compiled,
      'cached plan: the second read compiled nothing new',
    );
    check(
      r2.widened == 4,
      'cached plan: one widened per record (got ${r2.widened})',
    );
  }
}

// 6. AN ENUM VARIANT AND A UNION ARM INSERTED IN THE MIDDLE, and a keyed
// array whose keys slid — remapped BY NAME and never by position.
void vCase() {
  final one = v1home.Cfg();
  one.a = 42;
  one.name.setRange(0, 5, 'hello'.codeUnits);
  one.nameLength = 5;
  one.grade = v1decl.Grade.gold; // V2 inserts Silver BEFORE Gold
  one.effect.type = v1decl.EffectType.ward;
  one.effect.ward.charge = 0.75; // V2 inserts hex BEFORE ward
  one.tokens[v1decl.Slot.alpha - 1] = 21;
  one.tokens[v1decl.Slot.delta - 1] = 24; // V2 slides Beta and keeps Delta
  one.tierPresent = true;
  one.tier = 77;

  final w = Uint8List(v1.cfgFixedMeasure(1));
  check(v1.cfgFixedSave(<v1home.Cfg>[one], 1, w) == w.length, 'V1 save');

  final back = <v2home.Cfg>[v2home.Cfg()];
  final r = v2home.TableFixedReport();
  final n = v2.cfgFixedLoad(
    back,
    1,
    w,
    w.length,
    v2.cfgFixedNewPlan(entryCapacity: 8192),
    r,
  );
  check(n == 1, 'V2 reads V1: one record');
  check(
    back[0].grade == v2decl.Grade.gold,
    'ENUM: a variant inserted in the middle is remapped by NAME',
  );
  check(
    back[0].effect.type == v2decl.EffectType.ward,
    'UNION: an arm inserted in the middle is remapped by NAME',
  );
  check(back[0].effect.ward.charge == 0.75, 'UNION: the arm\'s payload lands');
  check(
    text(back[0].title, back[0].titleLength) == 'hello',
    'RENAMED: title reads name\'s bytes',
  );
  check(
    back[0].tokens[v2decl.Slot.alpha - 1] == 21,
    'KEYED: a slot whose key did not move',
  );
  check(
    back[0].tokens[v2decl.Slot.delta - 1] == 24,
    'KEYED: a slot whose key SLID keeps its value',
  );
  check(
    back[0].tokens[v2decl.Slot.sigma - 1] == 0,
    'KEYED: a key the writer has no name for takes its default',
  );
  check(
    back[0].tierPresent && back[0].tier == 77,
    'OPTIONAL: the present flag and the payload',
  );
  check(back[0].c, 'MISSING: V2\'s own `c` takes its declared default');
  check(
    back[0].a == 5.0,
    'KIND MOVED: int32 -> float32 leaves the declared default',
  );
  check(
    !r.malformed && r.refused == 0,
    'V2 reads V1: no damage and no refusal',
  );
}

// 7. AN OPTIONAL AGAINST A VALUE. On FORM 1 those two are wire-identical
// (§2.3). ON THIS FORM THEY ARE NOT: an optional carries a present byte in
// front of a payload that rides whole, so the layout says kind 35 on one side
// and kind 13 on the other, and the edit is REPORTED rather than silent.
void pCase() {
  final one = p1home.Chain();
  one.name.setRange(0, 5, 'chain'.codeUnits);
  one.nameLength = 5;
  one.link.value = 500;

  final w = Uint8List(p1.chainFixedMeasure(1));
  check(p1.chainFixedSave(<p1home.Chain>[one], 1, w) == w.length, 'P1 save');

  final back = <p3home.Chain>[p3home.Chain()];
  final r = p3home.TableFixedReport();
  final n = p3.chainFixedLoad(back, 1, w, w.length, p3.chainFixedNewPlan(), r);
  check(n == 1, 'P3 reads P1: one record');
  check(
    text(back[0].name, back[0].nameLength) == 'chain',
    'P3 reads P1: the plain field lands',
  );
  check(
    r.kindMismatch == 1,
    'OPTIONAL vs VALUE is a reported kind on this form, never a silent reread',
  );
}

// 8. AN ABSENT OPTIONAL'S PAYLOAD IS SLACK, AND SLACK IS ZERO ON WRITE
// (docs/SPEC-TABLES.md §3.4). The payload rides WHOLE whether or not it is
// present — the bytes are always there and the body is always the same size —
// but under a present flag of `0` what rides is ZEROS. So a value whose
// storage holds a stale or a merely DEFAULT payload behind a clear flag writes
// zeros there, and two writers holding different rubbish under a clear flag
// write the SAME record. Read-then-save byte identity is over CONFORMING
// input, which is exactly what this rule makes reproducible.
void absentOptionalCase() {
  final one = p3home.Chain();
  one.name.setRange(0, 5, 'dirty'.codeUnits);
  one.nameLength = 5;
  // a payload set, and then the flag CLEARED: the storage still holds it
  one.linkPresent = true;
  one.link.value = 777;
  one.link.tag.setRange(0, 5, 'stale'.codeUnits);
  one.link.tagLength = 5;
  one.linkPresent = false;

  final w = Uint8List(p3.chainFixedMeasure(1));
  check(
    p3.chainFixedSave(<p3home.Chain>[one], 1, w) == w.length,
    'absent save',
  );

  // the record's body, past the eight-byte hash
  final at = p3.chainFixedHeaderBytes + 8;
  final body = w.sublist(at, at + p3.chainFixedBodyBytes);
  // name (4 + 16) then the present flag, then the payload to the body's end
  check(body[20] == 0, 'absent: the present flag is 0');
  var rubbish = 0;
  for (var i = 21; i < body.length; i++) {
    if (body[i] != 0) {
      rubbish++;
    }
  }
  check(
    rubbish == 0,
    'ABSENT OPTIONAL: the payload is ZEROS on the wire, '
    'not the storage this writer happened to hold (got $rubbish non-zero)',
  );

  // and a PRESENT payload still rides whole, so the rule cost nothing
  one.linkPresent = true;
  check(
    p3.chainFixedSave(<p3home.Chain>[one], 1, w) == w.length,
    'present save',
  );
  final back = <p3home.Chain>[p3home.Chain()];
  final r = p3home.TableFixedReport();
  check(
    p3.chainFixedLoad(back, 1, w, w.length, p3.chainFixedNewPlan(), r) == 1,
    'present: one record read',
  );
  check(
    back[0].linkPresent && back[0].link.value == 777,
    'PRESENT OPTIONAL: the payload rides whole, as it always did',
  );
  check(
    text(back[0].link.tag, back[0].link.tagLength) == 'stale',
    'PRESENT OPTIONAL: the payload\'s text rides whole',
  );
}

// ---------------------------------------------------------------------------
// 4. THE NEGATIVE CONTROLS
// ---------------------------------------------------------------------------

void negativeControl() {
  final w2 = fx2Record();

  // THE WRONG PLAN. FX1's identity plan is correct for an FX1 record and wrong
  // for an FX2 one — FX2 inserts `added` between `renamed_to` and `nested`, so
  // every offset past it has moved. Running FX1's plan over FX2's body is
  // exactly the mistake the hash exists to prevent, and it must come out
  // WRONG. If this ever comes out right, the hash is not carrying anything and
  // every case above proves nothing.
  {
    final plan = fx1.fxRootFixedNewPlan();
    final r = fx1home.TableFixedReport();
    final at = fx2.fxRootFixedHeaderBytes + 8;
    plan.image.setRange(0, fx1.fxRootFixedBodyBytes, fx1.fxRootFixedPrefill);
    fx1home.tableFixedRun(
      fx1.fxRootFixedIdentity,
      fx1.fxRootFixedIdentityCount,
      w2,
      ByteData.sublistView(w2),
      at,
      plan.image,
      plan.imageView,
      plan.remap,
      plan.conv,
      r,
    );
    final wrong = fx1home.FxRoot();
    fx1home.fxRootFixedDecode(wrong, plan.image, plan.imageView, 0, r);
    final intact =
        wrong.nested.a == 33 && wrong.nested.b == 44 && wrong.renamed == 808;
    check(
      !intact,
      'NEGATIVE CONTROL: the wrong plan must NOT reproduce the record',
    );
  }

  // and the loader never takes that path: the hash is what selects the plan
  {
    final right = <fx1home.FxRoot>[fx1home.FxRoot()];
    final r = fx1home.TableFixedReport();
    final n = fx1.fxRootFixedLoad(
      right,
      1,
      w2,
      w2.length,
      fx1.fxRootFixedNewPlan(),
      r,
    );
    check(
      n == 1 && right[0].nested.a == 33 && right[0].nested.b == 44,
      'NEGATIVE CONTROL: the loader compiles a plan from the layout and gets it right',
    );
  }

  // A FORM BYTE THIS READER DOES NOT CARRY IS A REFUSAL AND NEVER DAMAGE, AND
  // THE REFUSAL SAYS WHICH DIRECTION (§3): the registry is ORDERED, so form 1
  // is the PREVIOUS form and not a newer one, form 2 is a message batch where a
  // FILE was expected, and only a byte no form defines is newer_form. 6 is that
  // byte today — 3 is this form and 4 and 5 are reserved — and it is what
  // §4.2's unknown-form control plants.
  const planted = <int, int>{
    0: fx1home.TableFixedRefusal.newerForm,
    1: fx1home.TableFixedRefusal.previousForm,
    2: fx1home.TableFixedRefusal.messageFormAsFile,
    6: fx1home.TableFixedRefusal.newerForm,
  };
  planted.forEach((byte, want) {
    final other = Uint8List.fromList(w2);
    other[0] = byte;
    final v = <fx1home.FxRoot>[fx1home.FxRoot()];
    final r = fx1home.TableFixedReport();
    final n = fx1.fxRootFixedLoad(
      v,
      1,
      other,
      other.length,
      fx1.fxRootFixedNewPlan(),
      r,
    );
    check(
      n < 0 && r.refused == want && !r.malformed,
      'REFUSED BY NAME: form byte $byte is ${fx1home.TableFixedRefusal.name(want)} '
      '(got ${fx1home.TableFixedRefusal.name(r.refused)}), and malformed does not fire',
    );
  });

  // THE HEADER NAMES THE LAYOUT ONCE, and a header whose hash is not the hash
  // of the layout behind it is refused. It is checked LAST of the three, so a
  // broken layout is never reported as a lying header.
  {
    final bad = Uint8List.fromList(w2);
    bad[8] ^= 0xff;
    final v = <fx1home.FxRoot>[fx1home.FxRoot()];
    final r = fx1home.TableFixedReport();
    final n = fx1.fxRootFixedLoad(
      v,
      1,
      bad,
      bad.length,
      fx1.fxRootFixedNewPlan(),
      r,
    );
    check(
      n < 0 && r.refused == fx1home.TableFixedRefusal.layoutMalformed,
      'REFUSED BY NAME: a header whose hash is not the layout\'s is layout_malformed',
    );
  }

  // A RECORD WHOSE HASH NAMES NO LAYOUT THIS READER HOLDS is a refusal by name
  {
    final bad = Uint8List.fromList(w2);
    bad[fx2.fxRootFixedHeaderBytes] ^= 0xff; // the record's own hash
    final v = <fx1home.FxRoot>[fx1home.FxRoot()];
    final r = fx1home.TableFixedReport();
    final n = fx1.fxRootFixedLoad(
      v,
      1,
      bad,
      bad.length,
      fx1.fxRootFixedNewPlan(),
      r,
    );
    check(
      n < 0 && r.refused == fx1home.TableFixedRefusal.noLayout,
      'REFUSED BY NAME: a record whose hash is not the layout\'s is no_layout',
    );
  }

  // A PLAN THAT DOES NOT FIT THE CALLER'S STORAGE IS A REFUSAL BY NAME, and
  // the codec allocates nothing to get around it.
  {
    final v = <fx1home.FxRoot>[fx1home.FxRoot()];
    final r = fx1home.TableFixedReport();
    final tiny = fx1.fxRootFixedNewPlan(entryCapacity: 1, remapCapacity: 4);
    final n = fx1.fxRootFixedLoad(v, 1, w2, w2.length, tiny, r);
    check(
      n < 0 && r.refused == fx1home.TableFixedRefusal.planTooLarge,
      'REFUSED BY NAME: plan_too_large',
    );
  }

  // MORE RECORDS THAN THE CALLER'S CAPACITY is a refusal by name rather than a
  // write past the end
  {
    final one = fx1home.FxRoot();
    final many = <fx1home.FxRoot>[one, one, one];
    final buf = Uint8List(fx1.fxRootFixedMeasure(3));
    check(
      fx1.fxRootFixedSave(many, 3, buf) == buf.length,
      'FX1 save: three records',
    );
    final v = <fx1home.FxRoot>[fx1home.FxRoot()];
    final r = fx1home.TableFixedReport();
    check(
      fx1.fxRootFixedLoad(v, 1, buf, buf.length, fx1.fxRootFixedNewPlan(), r) <
              0 &&
          r.refused == fx1home.TableFixedRefusal.batchTooLarge,
      'REFUSED BY NAME: batch_too_large',
    );
  }

  // BYTES LEFT OVER ARE malformed — §3's rule, for the same reason: the two
  // ends of the file have met
  {
    final w1 = fx1Record();
    final bad = Uint8List(w1.length + 3);
    bad.setRange(0, w1.length, w1);
    final v = <fx1home.FxRoot>[fx1home.FxRoot()];
    final r = fx1home.TableFixedReport();
    check(
      fx1.fxRootFixedLoad(v, 1, bad, bad.length, fx1.fxRootFixedNewPlan(), r) <
              0 &&
          r.malformed,
      'MALFORMED: bytes left over past the last whole record',
    );
  }

  // A WRITE THAT DOES NOT FIT answers -1 and touches nothing, exactly as the
  // packet writer does
  {
    final one = fx1home.FxRoot();
    final small = Uint8List(fx1.fxRootFixedMeasure(1) - 1);
    check(
      fx1.fxRootFixedSave(<fx1home.FxRoot>[one], 1, small) == -1,
      'REFUSAL: a buffer too small for the file answers -1',
    );
  }
}

// ---------------------------------------------------------------------------
// THE LAYOUT ARRIVES FROM AN UNTRUSTED PEER (docs/SPEC-TABLES.md §3.4)
// ---------------------------------------------------------------------------
//
// It is the one structure a reader must parse before it knows anything at all,
// so every rule it is held to refuses under ITS OWN NAME, before a single
// record byte is touched. A VALIDATION NOBODY WATCHED FAIL IS A VALIDATION
// NOBODY HAS, so there is one case per named rule, each taking a layout this
// reader accepts and breaking EXACTLY ONE THING in it.
//
// The file is `form byte, u32 layout length, layout, records`, so the layout
// starts at byte 5, the entry count is the four bytes there, and entry k is the
// seventeen bytes at 5 + 4 + 17k: id (u64), kind (u8), size (u32), children
// (u32), every number little-endian.

// §3's header is sixteen bytes, the layout's own u32 length stands behind it,
// and the entries begin at 20.
const int layoutLengthAt = 16;
const int layoutAt = 20;

/// the layout's own head is the u32 ENTRY COUNT, so entry 0 is four bytes past
/// the layout's first byte
const int entry0 = layoutAt + 4;

int entryAt(int k) => entry0 + k * 17;

void putEntry(BytesBuilder b, int id, int kind, int size, int children) {
  final e = ByteData(17);
  e.setUint64(0, id, Endian.little);
  e.setUint8(8, kind);
  e.setUint32(9, size, Endian.little);
  e.setUint32(13, children, Endian.little);
  b.add(e.buffer.asUint8List());
}

// a FILE around a hand-built layout: the form byte, the layout's length, the
// layout, and no records — every rule below refuses before a record is reached
Uint8List fileOf(Uint8List layout) {
  final out = Uint8List(layoutAt + layout.length);
  out[0] = 3;
  final view = ByteData.sublistView(out);
  // THE HEADER'S HASH IS THE LAYOUT'S OWN, so the ONE break in each case below
  // is the rule under test and never a lying header standing beside it
  view.setUint64(
    8,
    fx1home.TableFixedLayout.hashOf(layout, 0, layout.length),
    Endian.little,
  );
  view.setUint32(layoutLengthAt, layout.length, Endian.little);
  out.setRange(layoutAt, layoutAt + layout.length, layout);
  return out;
}

void refuses(Uint8List broken, int want, String what) {
  final v = <fx1home.FxRoot>[fx1home.FxRoot()];
  final r = fx1home.TableFixedReport();
  final n = fx1.fxRootFixedLoad(
    v,
    1,
    broken,
    broken.length,
    fx1.fxRootFixedNewPlan(),
    r,
  );
  check(
    n < 0 && r.refused == want,
    '$what (got ${fx1home.TableFixedRefusal.name(r.refused)}, want ${fx1home.TableFixedRefusal.name(want)})',
  );
  // NOTHING WAS DECODED AND NOTHING WAS COUNTED. A refusal that half-read a
  // record would be the damage the refusal exists to prevent.
  check(
    r.unknown == 0 &&
        r.kindMismatch == 0 &&
        r.widened == 0 &&
        r.clamped == 0 &&
        !r.malformed,
    '$what: a layout refusal sets nothing and counts nothing',
  );
}

void layoutValidation() {
  // the layout this reader ACCEPTS, which every case below breaks once
  final good = fx2Record();
  {
    // and it READS, so every refusal below is the ONE break and not the file
    final v = <fx1home.FxRoot>[fx1home.FxRoot()];
    final r = fx1home.TableFixedReport();
    check(
      fx1.fxRootFixedLoad(
                v,
                1,
                good,
                good.length,
                fx1.fxRootFixedNewPlan(),
                r,
              ) ==
              1 &&
          r.refused == 0,
      'layout validation: the unbroken file reads, so the breaks below are the breaks',
    );
  }

  Uint8List copy() => Uint8List.fromList(good);

  // 1. THE ENTRY COUNT FITS THE LAYOUT'S LENGTH EXACTLY
  {
    final f = copy();
    final view = ByteData.sublistView(f);
    view.setUint32(
      layoutAt,
      view.getUint32(layoutAt, Endian.little) + 1,
      Endian.little,
    );
    refuses(
      f,
      fx1home.TableFixedRefusal.layoutCountMismatch,
      'RULE: the entry count fits the layout length exactly',
    );
  }
  {
    // a count of ZERO is not a layout either: there is no root to walk
    final f = copy();
    ByteData.sublistView(f).setUint32(layoutAt, 0, Endian.little);
    refuses(
      f,
      fx1home.TableFixedRefusal.layoutCountMismatch,
      'RULE: an entry count of zero is not a layout',
    );
  }

  // 2. EVERY KIND IS IN THE CLOSED SET. A fixed form's kind set is CLOSED, so
  //    a kind outside it means a NEWER FORM BYTE — a different form — and not
  //    a newer layout of this one. It is refused, never stepped over.
  {
    final f = copy();
    f[entryAt(1) + 8] = 200; // a kind no form byte this build carries defines
    refuses(
      f,
      fx1home.TableFixedRefusal.layoutKindUnknown,
      'RULE: a kind outside the closed set is REFUSED, not skipped',
    );
  }

  // 3. A KIND IS USED AS ITS DEFINITION ALLOWS — here, the ROOT is a table
  {
    final f = copy();
    f[entryAt(0) + 8] = 14; // an array as the root of a record
    refuses(
      f,
      fx1home.TableFixedRefusal.layoutKindInvalid,
      'RULE: the root entry is a TABLE',
    );
  }

  // 4. A CONSTANT SIZE MATCHES ITS KIND
  {
    final f = copy();
    ByteData.sublistView(f).setUint32(
      entryAt(1) + 9,
      5,
      Endian.little,
    ); // a uint32 leaf in five bytes
    refuses(
      f,
      fx1home.TableFixedRefusal.layoutSizeMismatch,
      'RULE: a constant size its kind does not admit',
    );
  }
  {
    // and a TABLE's size is the SUM of its children's, not a number of its own
    final f = copy();
    final view = ByteData.sublistView(f);
    view.setUint32(
      entryAt(0) + 9,
      view.getUint32(entryAt(0) + 9, Endian.little) + 4,
      Endian.little,
    );
    refuses(
      f,
      fx1home.TableFixedRefusal.layoutSizeMismatch,
      'RULE: a table\'s size is the sum of its fields\'',
    );
  }

  // 5. THE PRE-ORDER CHILD WALK CONSUMES EXACTLY THE ENTRIES
  {
    final f = copy();
    final view = ByteData.sublistView(f);
    view.setUint32(
      entryAt(0) + 13,
      view.getUint32(entryAt(0) + 13, Endian.little) + 1,
      Endian.little,
    );
    refuses(
      f,
      fx1home.TableFixedRefusal.layoutTreeUnclosed,
      'RULE: the tree runs out of layout',
    );
  }
  {
    // THE OTHER DIRECTION: a tree that closes EARLY leaves entries no walk
    // reaches. It takes a hand-built layout to reach, and that is itself worth
    // stating: dropping a child of a TABLE is caught one rule sooner, by the
    // size that no longer sums, so the only subtree whose loss the size rule
    // cannot see is one that contributes NO size — an enum's variants, at kind
    // 32 and size 0.
    final b = BytesBuilder();
    final head = ByteData(4);
    head.setUint32(0, 4, Endian.little);
    b.add(head.buffer.asUint8List());
    putEntry(b, 1, 13, 4, 1); // a table of one field
    putEntry(b, 2, 30, 4, 0); // an enum, its TWO variants unreached
    putEntry(b, 3, 32, 0, 0);
    putEntry(b, 4, 32, 0, 0);
    refuses(
      fileOf(b.toBytes()),
      fx1home.TableFixedRefusal.layoutTreeUnclosed,
      'RULE: the layout outlasts the tree',
    );
  }

  // 6. THE TOTAL RECORD SIZE IS WITHIN 65536 AND DOES NOT OVERFLOW
  {
    final f = copy();
    ByteData.sublistView(f).setUint32(entryAt(0) + 9, 65537, Endian.little);
    refuses(
      f,
      fx1home.TableFixedRefusal.layoutRecordTooLarge,
      'RULE: a record size past 65536',
    );
  }
  {
    // A SIZE THAT WOULD WRAP. The children's sizes are summed in a Dart int,
    // which is sixty-four bits, precisely so a u32 that overflows is CAUGHT
    // rather than wrapped into a small number that then agrees with a parent.
    final f = copy();
    ByteData.sublistView(f)
        .setUint32(entryAt(1) + 9, 0xffffffff, Endian.little);
    refuses(
      f,
      fx1home.TableFixedRefusal.layoutRecordTooLarge,
      'RULE: a size that would overflow the sum',
    );
  }

  // 7. NOTHING NESTED PAST THE READER'S WALK BOUND. A BOUND ON THE WALK AND
  //    NOT ON THE WIRE: the validation recurses, so a layout of a thousand
  //    entries each claiming one child would spend a reader's stack before any
  //    other rule could fire. Nothing in §3.4 fixes the number.
  {
    const depth = 4096; // far past any reader's own bound
    final b = BytesBuilder();
    final head = ByteData(4);
    head.setUint32(0, depth + 1, Endian.little);
    b.add(head.buffer.asUint8List());
    for (var i = 0; i < depth; i++) {
      // a table, then optional wrappers all the way down
      putEntry(b, 1, i == 0 ? 13 : 35, depth - i, 1);
    }
    putEntry(b, 2, 1, 1, 0); // a bool at the bottom
    refuses(
      fileOf(b.toBytes()),
      fx1home.TableFixedRefusal.layoutTooDeep,
      'RULE: a nesting depth past the walk\'s own bound',
    );
  }

  // AND THE RESIDUE: bytes that are not a layout at all, which is the one case
  // the seven named rules never reach.
  {
    refuses(
      fileOf(Uint8List(2)),
      fx1home.TableFixedRefusal.layoutMalformed,
      'RULE: fewer bytes than a header is layout_malformed',
    );
  }
}

// ---------------------------------------------------------------------------
// THE COUNT CLAMP (docs/FIXED-FORM-ALGORITHM.md §4.5, count op): counts below
// zero and counts past the reader's bound clamp to [0, bound] and count one
// clamp. This is the twin of test/tables/fixedform_main.cpp:count_clamp_case().
// ---------------------------------------------------------------------------

void countClampCase() {
  // A CLEAN RECORD TO FORGE.
  final one = fx1home.FxRoot();
  one.marks[0] = 7;
  one.marks[1] = 8;
  one.marksCount = 2;

  final file = Uint8List(fx1.fxRootFixedMeasure(1));
  check(
    fx1.fxRootFixedSave(<fx1home.FxRoot>[one], 1, file) == file.length,
    'count clamp: the record saves',
  );

  // THE COUNT'S OFFSET IS FOUND BY THE PLAN'S OWN ROW, by shape and not by a
  // number: a `count` op whose bound is FOUR elements is `marks` and nothing
  // else, where `blob`'s is six.
  int countAt = -1;
  int bound = 0;
  for (var i = 0; i < fx1.fxRootFixedIdentityCount; i++) {
    final e = i * fx1home.TableFixedLane.lanes;
    if (fx1.fxRootFixedIdentity[e + fx1home.TableFixedLane.op] ==
            fx1home.TableFixedOp.count &&
        fx1.fxRootFixedIdentity[e + fx1home.TableFixedLane.size] == 4) {
      countAt = fx1.fxRootFixedIdentity[e + fx1home.TableFixedLane.src];
      bound = fx1.fxRootFixedIdentity[e + fx1home.TableFixedLane.size];
      break;
    }
  }
  check(
    countAt >= 0 && bound == 4,
    'count clamp: the identity plan carries `marks`\'s count op, bound FOUR elements',
  );
  final bodyOffset = fx1.fxRootFixedHeaderBytes + 8;
  check(
    ByteData.sublistView(file).getInt32(bodyOffset + countAt, Endian.little) ==
        2,
    'count clamp: and the offset it names is where the live count really is',
  );

  // C1: A COUNT BELOW ZERO. Not a large number — zero, and one clamp.
  {
    ByteData.sublistView(file).setInt32(
      bodyOffset + countAt,
      0xffffffff,
      Endian.little,
    ); // -1, as the wire spells it
    final back = <fx1home.FxRoot>[fx1home.FxRoot()];
    final r = fx1home.TableFixedReport();
    check(
      fx1.fxRootFixedLoad(
            back,
            1,
            file,
            file.length,
            fx1.fxRootFixedNewPlan(),
            r,
          ) ==
          1,
      'C1: a forged count of -1 still READS — a clamp is not a refusal',
    );
    check(back[0].marksCount == 0, 'C1: a count below zero clamps to ZERO');
    check(r.clamped == 1, 'C1: and counts exactly one clamp');
    check(
      !r.malformed && r.refused == 0,
      'C1: a clamp is neither malformed nor a refusal',
    );
  }

  // C2: A COUNT PAST THE READER'S OWN BOUND, IN ELEMENTS.
  {
    ByteData.sublistView(file)
        .setInt32(bodyOffset + countAt, bound + 1, Endian.little);
    final back = <fx1home.FxRoot>[fx1home.FxRoot()];
    final r = fx1home.TableFixedReport();
    check(
      fx1.fxRootFixedLoad(
            back,
            1,
            file,
            file.length,
            fx1.fxRootFixedNewPlan(),
            r,
          ) ==
          1,
      'C2: a forged count of Max+1 still READS',
    );
    check(
      back[0].marksCount == bound,
      'C2: a count past Max clamps to MAX, in elements',
    );
    check(r.clamped == 1, 'C2: and counts exactly one clamp');
    check(
      back[0].marks[0] == 7 && back[0].marks[1] == 8,
      'C2: the live elements are still the record\'s',
    );
  }

  // AND THE CONTROL: the bound itself is not a clamp. A count EQUAL to Max is
  // in range, and a clamp counted there would be a clamp on a clean read.
  {
    ByteData.sublistView(file)
        .setInt32(bodyOffset + countAt, bound, Endian.little);
    final back = <fx1home.FxRoot>[fx1home.FxRoot()];
    final r = fx1home.TableFixedReport();
    check(
      fx1.fxRootFixedLoad(
            back,
            1,
            file,
            file.length,
            fx1.fxRootFixedNewPlan(),
            r,
          ) ==
          1,
      'count clamp: a count of exactly Max reads',
    );
    check(
      back[0].marksCount == bound && r.clamped == 0,
      'CONTROL: a count of exactly Max is in range and counts NOTHING',
    );
  }
}

// ---------------------------------------------------------------------------

void main(List<String> args) {
  final corpus = args.isNotEmpty ? args[0] : 'build/fixedform-corpus';
  final benchCorpus = args.length > 1
      ? args[1]
      : 'build/fixedform-bench-corpus';

  print(
    'FX1 FxRoot: body ${fx1.fxRootFixedBodyBytes}, layout ${fx1.fxRootFixedLayoutBytes}, '
    'identity plan ${fx1.fxRootFixedIdentityCount} entries',
  );
  corpusFiles(corpus);
  pairedCorpus(benchCorpus);
  fxCase();
  vCase();
  pCase();
  absentOptionalCase();
  negativeControl();
  layoutValidation();
  countClampCase();

  if (failed) {
    print('FAILED');
    exitCode = 1;
    return;
  }
  print(
    'tables Dart fixed form: the LAYOUT and its hash are the C++ reference\'s byte for '
    'byte, all eight of its files read to the values it states and write back IDENTICAL, '
    'the versioning conformance lands through a plan compiled from the other side\'s '
    'layout, and every refusal is by name',
  );
  print('OK');
}
