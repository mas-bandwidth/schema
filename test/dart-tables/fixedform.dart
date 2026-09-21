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

import '../../build/dart-fixed/bench/Bench.dart' as benchDecl;
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
import '../../build/dart-fixed/fu1/FU1.dart' as fu1decl;
import '../../build/dart-fixed/fu1/FU1Fixed.dart' as fu1;
import '../../build/dart-fixed/fu1/Tblfu1Fixed.dart' as fu1home;
import '../../build/dart-fixed/fu2/FU2.dart' as fu2decl;
import '../../build/dart-fixed/fu2/FU2Fixed.dart' as fu2;
import '../../build/dart-fixed/fu2/Tblfu2Fixed.dart' as fu2home;
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

// THE TESTS SECTION 5.6 RETIRES, SKIPPED BY NAME AND NEVER DELETED
// (docs/FIXED-FORM-ALGORITHM.md §5.7 step 5). A deleted test is a coverage
// claim nobody can audit — and this suite is a `main` that counts failures and
// has no skip verb, so the skip is A PRINTED LINE AT THE CALL SITE naming the
// function, §5.6 and where the coverage is owed, plus a COUNT at the end
// (§5.9 #23). Every retired function stays in this file and stays compiling.
int retired = 0;

void retire(String fn, String where) {
  retired++;
  print('RETIRED by §5.6: $fn — $where');
}

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
      bench.fixedTableFixedCover,
      bench.fixedTableFixedCoverCount,
      rc,
    );
    check(made > 0, 'bench: compiled-from-own-layout wrote $made entries');
    benchHome.tableFixedFillRun(
      plan.fill,
      plan.fillCount,
      bench.fixedTableFixedPrefill,
      plan.image,
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
// 2b. THE DECLARED BOUNDS ON THE RAW SCALE (§4.6) — C8, fixed-point F-shift
// ---------------------------------------------------------------------------
//
// A FIXED-POINT FIELD'S DECLARED BOUNDS ARE IN VALUE UNITS AND ITS STORAGE IS
// RAW (docs/FIXED-FORM-ALGORITHM.md §4.6, docs/SPEC-TABLES.md §4): the reader
// shifts both ends by F before comparing, so `BenchMixed.server_time`
// `fixed(24, 8) | min = 0, max = 65535` is held to the RAW range
// [0, 65535 << 8] = [0, 16776960] and NOTHING here divides by 2^F. A `bits(N)`
// is the older half of the same pass and clamps to 2^N - 1, written only where
// N is narrower than the storage the form gives it.
//
// The reference is test/tables/fixedform_properties.cpp:1279 (`probe_clamp_op`):
// `w.position = 2000LL * 65536` is inside the OLDER writer's range and past the
// newer reader's `1000`, and the newer read lands `1000LL * 65536` and counts
// ONE `clamped`. This port forges the same one raw step past the shifted end,
// on the identity plan, and holds the value AND the count.
//
// THE SITE IS internal/codegen/darttable/fixeddart.go:969 `emitClamp` (with
// `fixedClampEnds` at :888 reading `ir.TableRawRange`'s F-shifted ends and
// `fixedBitsClamp` at :899 answering 2^N - 1). THE POINT OF THIS CASE IS THAT
// THE PASS RUNS AT ALL: a clean record already proves it cannot fire on a
// legitimate value, and the forged record proves it fires and counts.
void fixedBoundsCase() {
  final layoutBytes = bench.fixedTableFixedLayoutBytes;
  final body =
      layoutAt + layoutBytes + 8; // the 8-byte record hash, then values

  // A LEGITIMATE RECORD. Every value is inside its declaration, so the pass
  // writes nothing at all — a bound test that clamps a clean value is a test
  // of something else.
  final one = benchHome.FixedTable();
  one.value.serverTime = 100; // raw Q24.8, inside [0, 16776960]
  one.value.frameTick = 7;
  one.value.sequence = 9;
  final clean = Uint8List(bench.fixedTableFixedMeasure(1));
  check(
    bench.fixedTableFixedSave(<benchHome.FixedTable>[one], 1, clean) ==
        clean.length,
    'C8 bounds: save',
  );

  {
    final back = <benchHome.FixedTable>[benchHome.FixedTable()];
    final r = benchHome.TableFixedReport();
    final n = bench.fixedTableFixedLoad(
      back,
      1,
      clean,
      clean.length,
      bench.fixedTableFixedNewPlan(),
      r,
    );
    check(n == 1, 'C8 bounds: the legitimate record reads');
    check(
      back[0].value.serverTime == 100,
      'C8 bounds: the raw Q24.8 value lands exact',
    );
    check(
      r.clamped == 0,
      'C8 bounds: a value inside the declaration moves no clamp '
      '(the pass is not vacuous)',
    );
    check(
      !r.malformed && r.refused == 0,
      'C8 bounds: a clean read is not damage',
    );
  }

  // THE FORGED RECORD. One raw step past the SHIFTED maximum (65535 << 8) and
  // one past the width end of each bits field; the record's hash and layout are
  // untouched, so this is the identity plan and the pass is the reader's own.
  final v = ByteData.sublistView(clean);
  v.setInt32(body + 48, 16776961, Endian.little); // server_time, one past max
  v.setUint64(body + 40, 281474976710656, Endian.little); // frame_tick, 2^48
  v.setUint32(body + 0, 65536, Endian.little); // sequence, 2^16

  final r = benchHome.TableFixedReport();
  final back = <benchHome.FixedTable>[benchHome.FixedTable()];
  final n = bench.fixedTableFixedLoad(
    back,
    1,
    clean,
    clean.length,
    bench.fixedTableFixedNewPlan(),
    r,
  );
  check(n == 1, 'C8 bounds: the forged record still reads');
  check(
    back[0].value.serverTime == 65535 * 256,
    'C8 bounds: the SHIFTED maximum holds a raw past it '
    '(got ${back[0].value.serverTime}, want ${65535 * 256})',
  );
  check(
    back[0].value.frameTick == 281474976710655,
    'C8 bounds: bits(48) clamps to 2^48 - 1',
  );
  check(
    back[0].value.sequence == 65535,
    'C8 bounds: bits(16) clamps to 2^16 - 1',
  );
  check(
    r.clamped == 3,
    'C8 bounds: each clamp counts exactly once (got ${r.clamped})',
  );
  check(
    !r.malformed && r.refused == 0,
    'C8 bounds: damage in a VALUE is not framing damage',
  );
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

// ---------------------------------------------------------------------------
// 3b. THE DECLARED RANGE, CLAMPED ON LOAD AND COUNTED (docs/SPEC-TABLES.md §4;
//     the C++ reference's `bounds_case`, test/tables/fixedform_main.cpp:1197).
// ---------------------------------------------------------------------------
//
// A RANGED SCALAR holds a bound a caller can cross. The write side's own bounds
// are DEBUG-ONLY by rule and a range is not one of them, so a caller CAN put an
// out-of-range value on the wire; the READ side clamps it to the declared min
// and max and counts each end once. This case writes its poison THROUGH THE
// WRITER (never by poking bytes) and proves both ends on the identity plan, the
// path this form reads through. The cross-version half is retired (§5.6): a
// peer the lineage does not hold refuses by name before a plan exists.
//
// THE NEGATIVE HALF is the in-range value in the same two fields: it lands
// whole and moves no counter, so the two clamps the hostile record counts are
// the declared bound and not a pass that clamps everything.
void rangedScalarClampCase() {
  // ---- 1. THE IDENTITY PLAN: both ends, both counted ----
  {
    final one = fx1home.FxRoot();
    one.keep = 1;
    one.narrow = 2;
    one.renamed = 5000; // declared | min = 0, max = 1000
    one.gone = -7; // and the low end of the same declaration
    one.nested.a = 111;
    one.nested.b = 222;
    final w = Uint8List(fx1.fxRootFixedMeasure(1));
    check(
      fx1.fxRootFixedSave(<fx1home.FxRoot>[one], 1, w) == w.length,
      'RANGE: the out-of-range record saves',
    );

    final back = <fx1home.FxRoot>[fx1home.FxRoot()];
    final r = fx1home.TableFixedReport();
    final n = fx1.fxRootFixedLoad(
      back,
      1,
      w,
      w.length,
      fx1.fxRootFixedNewPlan(),
      r,
    );
    check(n == 1, 'RANGE: the record reads');
    check(back[0].renamed == 1000, 'RANGE: a value past max lands at max');
    check(back[0].gone == 0, 'RANGE: a value under min lands at min');
    check(r.clamped == 2, 'RANGE: two clamps, counted');
    check(
      back[0].nested.a == 111 && back[0].nested.b == 222,
      'RANGE: an in-range neighbour is untouched',
    );
    check(
      !r.malformed && r.refused == 0 && r.unknown == 0 && r.kindMismatch == 0,
      'RANGE: nothing else fired',
    );
  }

  // ---- 2. THE CONTROL: a legitimate value is not clamped, and counts nothing ----
  {
    final one = fx1home.FxRoot();
    one.renamed = 321; // inside [0, 1000]
    one.gone = 654; // inside [0, 1000]
    final w = Uint8List(fx1.fxRootFixedMeasure(1));
    check(
      fx1.fxRootFixedSave(<fx1home.FxRoot>[one], 1, w) == w.length,
      'RANGE control: the in-range record saves',
    );
    final back = <fx1home.FxRoot>[fx1home.FxRoot()];
    final r = fx1home.TableFixedReport();
    final n = fx1.fxRootFixedLoad(
      back,
      1,
      w,
      w.length,
      fx1.fxRootFixedNewPlan(),
      r,
    );
    check(n == 1, 'RANGE control: the record reads');
    check(
      back[0].renamed == 321 && back[0].gone == 654,
      'RANGE control: an in-range value lands whole',
    );
    check(
      r.clamped == 0,
      'RANGE control: a value inside the declared range clamps nothing',
    );
  }
}

// TEXT UNDER AN ARM (docs/SPEC-TABLES.md §3.4, §15). FU1 writes a string(8)
// in the union's SECOND arm; FU2 appends `extra` so a read of those bytes is
// a COMPILED plan. Both reads go through fuRootFixedLoad — the same one-path
// load the rest of this form uses. Hash chooses the plan. The compiled read
// has to see "hello".
void fuCase() {
  check(
    fu1.fuRootFixedHash != fu2.fuRootFixedHash,
    'text under an arm: FU2 extra changed the layout hash',
  );

  final labelled = fu1home.FuRoot();
  labelled.flag = true;
  labelled.note = 44;
  labelled.notePresent = true;
  labelled.pick.type = fu1decl.PickType.labelled; // the SECOND arm
  labelled.pick.labelled.lead = 101;
  labelled.pick.labelled.label.setRange(0, 5, 'hello'.codeUnits);
  labelled.pick.labelled.labelLength = 5;
  labelled.pick.labelled.trail = 202;
  labelled.tail = 11;

  final plain = fu1home.FuRoot();
  plain.pick.type = fu1decl.PickType.plain;
  plain.pick.plain.n = 303;
  plain.tail = 12;

  final w = Uint8List(fu1.fuRootFixedMeasure(2));
  check(
    fu1.fuRootFixedSave(<fu1home.FuRoot>[labelled, plain], 2, w) == w.length,
    'FU1 save',
  );

  {
    final back = <fu1home.FuRoot>[fu1home.FuRoot(), fu1home.FuRoot()];
    final r = fu1home.TableFixedReport();
    final n = fu1.fuRootFixedLoad(
      back,
      2,
      w,
      w.length,
      fu1.fuRootFixedNewPlan(),
      r,
    );
    check(n == 2, 'text under an arm: the identity read takes both records');
    check(
      back[0].pick.type == fu1decl.PickType.labelled,
      'text under an arm: identity, the SECOND arm',
    );
    check(
      back[0].pick.labelled.lead == 101,
      'text under an arm: identity, the scalar BEFORE the text',
    );
    check(
      back[0].pick.labelled.labelLength == 5 &&
          text(
                back[0].pick.labelled.label,
                back[0].pick.labelled.labelLength,
              ) ==
              'hello',
      'text under an arm: the IDENTITY read lands the text',
    );
    check(
      back[0].pick.labelled.trail == 202,
      'text under an arm: identity, the scalar AFTER the text',
    );
    check(
      back[0].flag &&
          back[0].notePresent &&
          back[0].note == 44 &&
          back[0].tail == 11,
      'text under an arm: identity, the rest of the labelled record',
    );
    check(
      back[1].pick.type == fu1decl.PickType.plain &&
          back[1].pick.plain.n == 303 &&
          back[1].tail == 12,
      'text under an arm: identity, the FIRST arm as well',
    );
    quiet(r, 'text under an arm: identity');
  }

  {
    final back = <fu2home.FuRoot>[fu2home.FuRoot(), fu2home.FuRoot()];
    final r = fu2home.TableFixedReport();
    final n = fu2.fuRootFixedLoad(
      back,
      2,
      w,
      w.length,
      fu2.fuRootFixedNewPlan(),
      r,
    );
    check(n == 2, 'text under an arm: the compiled read takes both records');
    check(
      back[0].pick.type == fu2decl.PickType.labelled,
      'text under an arm: compiled, the SECOND arm',
    );
    check(
      back[0].pick.labelled.lead == 101,
      'text under an arm: compiled, the scalar BEFORE the text',
    );
    check(
      back[0].pick.labelled.labelLength == 5 &&
          text(
                back[0].pick.labelled.label,
                back[0].pick.labelled.labelLength,
              ) ==
              'hello',
      'text under an arm: the COMPILED read still sees the text',
    );
    check(
      back[0].pick.labelled.trail == 202,
      'text under an arm: compiled, the scalar AFTER the text',
    );
    check(
      back[0].flag &&
          back[0].notePresent &&
          back[0].note == 44 &&
          back[0].tail == 11,
      'text under an arm: compiled, the rest of the labelled record',
    );
    check(
      back[0].extra == 11,
      'text under an arm: the field FU1 does not carry took its declared default',
    );
    check(
      back[1].pick.type == fu2decl.PickType.plain &&
          back[1].pick.plain.n == 303 &&
          back[1].tail == 12,
      'text under an arm: compiled, the FIRST arm as well',
    );
    check(
      back[1].extra == 11,
      'text under an arm: compiled, extra defaults on the FIRST arm too',
    );
    check(
      !r.malformed && r.refused == 0 && r.clamped == 0,
      'text under an arm: compiled, a clean read moves no counter',
    );
  }
}

// TWO LANES, BECAUSE THEY ARE TWO FACTS (docs/FIXED-FORM-ALGORITHM.md §4.1
// fix 12; docs/SPEC-TABLES.md §3.4). A plan entry carries the ordinal a union
// arm's GUARD byte must hold for the entry to run AND the argument the entry's
// OWN op takes — for a text entry, its flavour. They must be two lanes: one
// shared lane gives whichever fact was written last, so a `string(N)` under a
// union arm reads with the wrong flavour or under the wrong arm, and neither is
// visible to this build's own records; only to a peer's. FU1 puts a string(8)
// under the union's SECOND arm, so the arm ordinal (2) and the flavour (1) are
// different numbers and a collision is caught by NAME. This is the dart leg's
// twin of the C++ reference's union_text_case (test/tables/fixedform_main.cpp,
// its UT1 half at :885-887) ported into this file's idiom: the plan is the
// build's own static data (fuRootFixedIdentity), so the two facts are read out
// of their lanes directly rather than through C++'s plan array.
void twoLanesCase() {
  const lanes = fu1home.TableFixedLane.lanes;
  final plan = fu1.fuRootFixedIdentity;

  // THE TEXT ENTRY UNDER THE ARM. FU1 declares exactly one string, so the
  // identity plan carries exactly one text entry, and it is guarded.
  var found = -1;
  var texts = 0;
  for (var i = 0; i < fu1.fuRootFixedIdentityCount; i++) {
    if (plan[i * lanes + fu1home.TableFixedLane.op] ==
        fu1home.TableFixedOp.text) {
      found = i;
      texts++;
    }
  }
  check(
    texts == 1,
    'two lanes: the identity plan carries exactly one text entry (got $texts)',
  );
  check(
    found >= 0,
    "two lanes: the identity plan carries the arm's text entry",
  );
  if (found < 0) {
    return;
  }
  final b = found * lanes;
  final guard = plan[b + fu1home.TableFixedLane.guard];
  final arg = plan[b + fu1home.TableFixedLane.arg];
  final meta = plan[b + fu1home.TableFixedLane.meta];

  check(
    guard != fu1home.tableFixedNoGuard,
    "two lanes: the text entry is guarded by the arm's tag byte",
  );
  check(arg == 2, "two lanes: arg is the SECOND arm's ordinal (got $arg)");
  check(
    meta == fu1home.TableFixedOp.textUtf8,
    'two lanes: meta is the utf8 flavour (got $meta)',
  );
  // THE WHOLE POINT: the ordinal and the flavour are DIFFERENT facts. If one
  // lane carried both, the guard's ordinal 2 would be the flavour (wide, 2)
  // and this entry's string would read as WIDE — or the guard would test the
  // tag against the flavour 1 and the entry would never run.
  check(
    arg != meta,
    'two lanes: the guard ordinal and the op flavour are DIFFERENT facts in '
    'different lanes (arg $arg, meta $meta)',
  );
  check(
    plan[b + fu1home.TableFixedLane.argW] == 1,
    "two lanes: the guard's width rides in its own lane",
  );

  // AND THE TWO LANES ARE USED. A record whose SECOND arm carries "seven77"
  // round-trips whole: an entry guarded by the wrong ordinal never runs (the
  // text is lost) and an entry whose flavour is the arm ordinal halves the
  // bound and terminates two bytes at a time. Both are silent; the value is
  // what says so.
  final one = fu1home.FuRoot();
  one.pick.type = fu1decl.PickType.labelled; // the SECOND arm
  one.pick.labelled.lead = 101;
  one.pick.labelled.label.setRange(0, 7, 'seven77'.codeUnits);
  one.pick.labelled.labelLength = 7;
  one.pick.labelled.trail = 202;
  final w = Uint8List(fu1.fuRootFixedMeasure(1));
  check(
    fu1.fuRootFixedSave(<fu1home.FuRoot>[one], 1, w) == w.length,
    'two lanes: save',
  );
  final back = <fu1home.FuRoot>[fu1home.FuRoot()];
  final r = fu1home.TableFixedReport();
  check(
    fu1.fuRootFixedLoad(back, 1, w, w.length, fu1.fuRootFixedNewPlan(), r) == 1,
    'two lanes: the record reads',
  );
  check(
    back[0].pick.labelled.labelLength == 7 &&
        text(back[0].pick.labelled.label, back[0].pick.labelled.labelLength) ==
            'seven77',
    "two lanes: the arm's string(8), whole",
  );
  check(
    back[0].pick.labelled.lead == 101 && back[0].pick.labelled.trail == 202,
    'two lanes: the scalars around the text land',
  );
  quiet(r, 'two lanes');
  // A PROOF THE CASE RAN: the file's other cases only speak on failure, and
  // this one is called before the reference corpus is read so its line is
  // visible even where the corpus cannot be built.
  print(
    'two lanes: arg=$arg and meta=$meta ride in their own lanes, and the arm\'s '
    'string reads whole',
  );
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

// 9. THE UNION'S SLACK (docs/FIXED-FORM-ALGORITHM.md §3.1): "A union writes the
// tag and the taken arm only, zero behind a narrower one." The byte gate above
// DETECTS a stray byte behind a short arm without ever naming the rule, and it
// only covers the one arm pair the corpus happens to hold, so this case names
// the rule and drives it directly.
//
// THE UNION IS `game_event` of the bench's BenchMixed, the live one on this
// leg. Its tag is one byte at body offset 1108 and its WIDEST arm is `hit`,
// 4 + 4 + 4 + 1 = 13 bytes, so the arm field runs to body[1122]. `chat` and
// `pickup` are 8 bytes (4 + 4 each), which leaves FIVE bytes of slack behind
// them. The offsets are constants of the generated writer
// (build/dart-fixed/bench/BenchFixed.dart, benchMixedFixedWriteBody) exactly as
// the body's 1236 bytes are a constant this file already pins.
void unionSlackCase() {
  const tagAt = 1108; // BenchMixed body offset of the union tag
  const widestArm = 13; // MixedHitEvent: 4 + 4 + 4 + 1
  const narrowArm = 8; // MixedChatEvent: 4 + 4
  const slack = widestArm - narrowArm;

  // THE RECORD'S BODY sits eight bytes past the record's own hash.
  final bodyAt = bench.fixedTableFixedHeaderBytes + 8;
  final armAt = bodyAt + tagAt + 1; // the arm starts past the one-byte tag

  final w = Uint8List(bench.fixedTableFixedMeasure(1));

  // A WIDE-ARM RECORD IS LAID DOWN FIRST, so the whole arm region is DIRTY
  // with a payload before the narrow arm is written over it. A case that wrote
  // onto a clean buffer would pass whether or not the writer zeroed, which is
  // the vacuous case CONTROL 1 exists to rule out.
  final wide = benchHome.FixedTable();
  wide.value.gameEvent.type = benchDecl.MixedEventType.hit; // the WIDEST arm
  wide.value.gameEvent.hit.targetId = 0x0abc;
  wide.value.gameEvent.hit.damage = 0x0def;
  wide.value.gameEvent.hit.hitKind = 7;
  wide.value.gameEvent.hit.crit = true;
  check(
    bench.fixedTableFixedSave(<benchHome.FixedTable>[wide], 1, w) == w.length,
    'union slack: the wide arm saves',
  );

  // THE NARROW ARM OVER THE REGION THE WIDE ONE DIRTIED.
  final narrow = benchHome.FixedTable();
  narrow.value.gameEvent.type = benchDecl.MixedEventType.chat; // a NARROWER arm
  narrow.value.gameEvent.chat.channel = 3;
  narrow.value.gameEvent.chat.speaker = 0x0abc;
  check(
    bench.fixedTableFixedSave(<benchHome.FixedTable>[narrow], 1, w) == w.length,
    'union slack: the narrow arm saves',
  );

  // THE TAG AND THE TAKEN ARM LAND, so the case cannot pass on an empty record.
  final view = ByteData.sublistView(w);
  check(
    w[bodyAt + tagAt] == benchDecl.MixedEventType.chat,
    'UNION TAG: the wire names the arm the value names',
  );
  check(
    view.getInt32(armAt, Endian.little) == 3 &&
        view.getUint32(armAt + 4, Endian.little) == 0x0abc,
    'UNION ARM: the taken arm\'s payload lands, and only it',
  );

  // THE RULE, BY NAME: from the end of the taken arm to the end of the WIDEST
  // arm, every byte is ZERO. This is the words the write section uses.
  var rubbish = 0;
  for (var i = armAt + narrowArm; i < armAt + widestArm; i++) {
    if (w[i] != 0) {
      rubbish++;
    }
  }
  check(
    rubbish == 0,
    'UNION SLACK: a union writes the tag and the taken arm only, and ZERO '
    'behind a narrower one (docs/FIXED-FORM-ALGORITHM.md §3.1) — the '
    '$widestArm-byte widest arm leaves $slack bytes behind `chat`, and '
    '$rubbish of them are non-zero',
  );

  // THE WIDEST ARM HAS NO SLACK BEHIND IT: the same scan reads an empty range
  // and cannot fire. This is CONTROL 1 pointed at the widest arm.
  final widest = benchHome.FixedTable();
  widest.value.gameEvent.type = benchDecl.MixedEventType.hit;
  check(
    bench.fixedTableFixedSave(<benchHome.FixedTable>[widest], 1, w) == w.length,
    'union slack: the widest arm saves',
  );
  var behindWidest = 0;
  for (var i = armAt + widestArm; i < armAt + widestArm; i++) {
    if (w[i] != 0) {
      behindWidest++;
    }
  }
  check(
    behindWidest == 0,
    'UNION SLACK: there is no arm wider than the widest, so there is no '
    'slack behind it',
  );
}

// ---------------------------------------------------------------------------

// C3: THE TEXT LENGTH CLAMP (docs/FIXED-FORM-ALGORITHM.md §4.5 and §6, THE
// TEXT OP). A text field's used length is four bytes a STRANGER wrote:
// `v := SLE(4, record+src)` clamped into `[0, cap]` where `cap := size / unit`,
// and `COUNT clamped` ONCE for the field. THE FORGERY IS IN THE BYTES and not
// through the writer: the write side's bound checks are DEBUG ONLY by rule, so
// a length of -1 or one past the cap is a thing only the wire can say, and the
// read side has to answer for it.
//
// THE OFFSET IS FOUND BY THE PLAN'S OWN ROW, by shape and not by a number in
// this file: FX1's identity plan carries exactly TWO text ops — `label`, the
// utf8 one, and `blob`, the bytes one — so the utf8 row IS `label`, and its
// `size` is then a REAL assertion: a text op's `size` is the reader's own CAP,
// in bytes for the narrow flavour, which for `label string(8)` is eight.
//
// THE CASE ASSERTS THE LOOP'S OWN IMAGE, not only the decoded value. For a
// direct text field the loop's text op and the generated decode clamp the SAME
// length, so an assertion on the decoded value alone cannot tell a broken loop
// from a working decode. Running `tableFixedRun` directly and reading the
// length word it landed pins the loop; the full `fxRootFixedLoad` beside it is
// the integration the two paths share.
void textLengthClampCase() {
  final v = fx1home.FxRoot();
  fx1home.initFxRoot(v);
  v.keep = 1234;
  v.nested.a = 7;
  // "abcdefgh": every unit of the buffer is well-formed, so a clamp to the cap
  // does not trip a content rule and the number this case asserts is the
  // length alone
  for (var i = 0; i < 8; i++) {
    v.label[i] = 0x61 + i;
  }
  v.labelLength = 3;
  final file = Uint8List(fx1.fxRootFixedMeasure(1));
  check(
    fx1.fxRootFixedSave(<fx1home.FxRoot>[v], 1, file) == file.length,
    'C3: the record saves',
  );

  var labelAt = -1;
  var cap = -1;
  var rows = 0;
  for (var i = 0; i < fx1.fxRootFixedIdentityCount; i++) {
    final b = i * fx1home.TableFixedLane.lanes;
    final isText =
        fx1.fxRootFixedIdentity[b + fx1home.TableFixedLane.op] ==
        fx1home.TableFixedOp.text;
    final isUtf8 =
        fx1.fxRootFixedIdentity[b + fx1home.TableFixedLane.meta] ==
        fx1home.TableFixedOp.textUtf8;
    if (!isText || !isUtf8) {
      continue;
    }
    rows++;
    labelAt = fx1.fxRootFixedIdentity[b + fx1home.TableFixedLane.src];
    cap = fx1.fxRootFixedIdentity[b + fx1home.TableFixedLane.size];
  }
  check(
    rows == 1,
    'C3: the identity plan carries exactly ONE utf8 text op, '
    'so the row below is `label`\'s',
  );
  check(
    labelAt >= 0 && cap == 8,
    'C3: and its cap is EIGHT BYTES — `label string(8)`\'s own bound',
  );
  final body = fx1.fxRootFixedHeaderBytes + 8;
  final fileView = ByteData.sublistView(file);
  check(
    fileView.getInt32(body + labelAt, Endian.little) == 3,
    'C3: and the offset it names is where the live length really is',
  );

  // THE LOOP ALONE. Run the identity plan over the forged body and read the
  // length word it LANDS in the reader's image, plus the one counter.
  (int, int) loopRun(int forged) {
    fileView.setInt32(body + labelAt, forged, Endian.little);
    final plan = fx1.fxRootFixedNewPlan();
    plan.image.setRange(0, fx1.fxRootFixedBodyBytes, fx1.fxRootFixedPrefill);
    final r = fx1home.TableFixedReport();
    fx1home.tableFixedRun(
      fx1.fxRootFixedIdentity,
      fx1.fxRootFixedIdentityCount,
      file,
      fileView,
      body,
      plan.image,
      plan.imageView,
      plan.remap,
      plan.conv,
      r,
    );
    return (plan.imageView.getInt32(labelAt, Endian.little), r.clamped);
  }

  // C3a: A LENGTH BELOW ZERO. Not a large number — zero, and one clamp.
  {
    final (landed, counted) = loopRun(-1);
    check(landed == 0, 'C3a: the loop lands a length below zero as ZERO');
    check(counted == 1, 'C3a: and the loop counts exactly one clamp');
  }

  // C3b: A LENGTH PAST THE READER'S OWN CAP, IN BYTES.
  {
    final (landed, counted) = loopRun(cap + 1);
    check(
      landed == cap,
      'C3b: the loop lands a length past the cap AT the cap',
    );
    check(counted == 1, 'C3b: and the loop counts exactly one clamp');
  }

  // AND THE CONTROL: EVERY length the rule ADMITS is not a clamp. The cap
  // itself, zero, and anything between both land WHOLE and count nothing; a
  // clamp counted here would be a clamp on a clean read. This list is the one
  // line CONTROL 1 edits: point the case at a legitimate value and it stays
  // green, because an in-range length has nothing to fire.
  for (final legit in <int>[0, 3, cap]) {
    final (landed, counted) = loopRun(legit);
    check(
      landed == legit && counted == 0,
      'CONTROL: a length of $legit is in range and counts NOTHING',
    );
  }

  // THE FULL READ, the two paths together: a forged length still READS (a
  // clamp is not a refusal), the decoded value is the clamped one, and the
  // report carries the one clamp and no damage.
  {
    fileView.setInt32(body + labelAt, -1, Endian.little);
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
      'C3: a forged length of -1 still READS — a clamp is not a refusal',
    );
    check(back[0].labelLength == 0, 'C3: a length below zero clamps to ZERO');
    check(r.clamped == 1, 'C3: and counts exactly one clamp');
    check(
      !r.malformed && r.refused == 0,
      'C3: a clamp is neither malformed nor a refusal',
    );
  }
}

// ---------------------------------------------------------------------------
// 3b. P3: THE HOSTILE-BYTES SWEEP (docs/FIXED-FORM-ALGORITHM.md §7 item 5)
// ---------------------------------------------------------------------------
//
// The reference is test/tables/fixedform_properties.cpp:522-638, "mutate every
// record byte, both paths": every byte of a lawful record — the eight-byte
// layout hash and the whole body — is set to each value of {00,01,02,7f,80,ff}
// and read back. Every mutant is answered one of THREE ways and never a fourth:
// a refusal BY NAME, a `malformed` read, or a read that LANDS.
//
// THIS LEG OWES THE THIRD, SANITIZER-SHAPED CLAUSE the reference gets from its
// ASan+UBSan twin (Makefile's tables-fixed-properties). On Dart the runtime
// bounds-check IS the sanitizer — an index past a Uint8List is a RangeError —
// and a reader that RAISES on hostile bytes is not one that refuses them, so
// nothing below may throw. And a read that lands is held to its declared bound:
// the clamp the read loop performs is the only thing between a forged count and
// an index a consumer will use.
const List<int> p3Poison = <int>[0x00, 0x01, 0x02, 0x7f, 0x80, 0xff];

int p3Mutants = 0;
int p3Threw = 0;

void p3Sweep(
  String name,
  Uint8List file,
  int recordAt,
  int recordBytes,
  void Function(Uint8List mutant, int at, int poison) read,
) {
  for (var i = 0; i < recordBytes; i++) {
    for (final poison in p3Poison) {
      final mutant = Uint8List.fromList(file);
      mutant[recordAt + i] = poison;
      p3Mutants++;
      try {
        read(mutant, i, poison);
      } catch (e) {
        p3Threw++;
        check(
          false,
          'P3 $name byte $i poison 0x${poison.toRadixString(16)}: '
          'an exception ESCAPED the reader: $e',
        );
      }
    }
  }
}

// THE IDENTITY PATH, on FX1: a text length, a counted array, a byte length and
// four ranged integers. The count is the one value a landed read may NOT leave
// out of bounds, because the decode trusts it to size its own element loop.
void p3Fx1() {
  final one = fx1home.FxRoot();
  one.keep = 4242;
  one.narrow = 40000;
  one.renamed = 321;
  one.gone = 654;
  one.nested.a = 111;
  one.nested.b = 222;
  one.label.setRange(0, 3, 'fx1'.codeUnits);
  one.labelLength = 3;
  one.marks[0] = 101;
  one.marks[1] = 202;
  one.marksCount = 2;
  one.blob.setRange(0, 4, <int>[0xDE, 0xAD, 0xBE, 0xEF]);
  one.blobLength = 4;
  final file = Uint8List(fx1.fxRootFixedMeasure(1));
  check(
    fx1.fxRootFixedSave(<fx1home.FxRoot>[one], 1, file) == file.length,
    'P3 fx1: the lawful record saves',
  );

  p3Sweep('fx1', file, fx1.fxRootFixedHeaderBytes, fx1.fxRootFixedRecordBytes, (
    mutant,
    at,
    poison,
  ) {
    final back = <fx1home.FxRoot>[fx1home.FxRoot()];
    final report = fx1home.TableFixedReport();
    final n = fx1.fxRootFixedLoad(
      back,
      1,
      mutant,
      mutant.length,
      fx1.fxRootFixedNewPlan(),
      report,
    );
    final what = 'P3 fx1 byte $at poison 0x${poison.toRadixString(16)}';
    if (n < 0) {
      check(
        report.refused != 0 || report.malformed,
        '$what: a negative read is a named refusal or malformed',
      );
      return;
    }
    check(n == 1, '$what: a one-record file answers one record (got $n)');
    final v = back[0];
    check(v.renamed >= 0 && v.renamed <= 1000, '$what: renamed in bound');
    check(v.gone >= 0 && v.gone <= 1000, '$what: gone in bound');
    check(v.nested.a >= 0 && v.nested.a <= 1000, '$what: nested.a in bound');
    check(v.nested.b >= 0 && v.nested.b <= 1000, '$what: nested.b in bound');
    check(
      v.labelLength >= 0 && v.labelLength <= 8,
      '$what: label length in bound',
    );
    check(
      v.marksCount >= 0 && v.marksCount <= 4,
      '$what: marks count CLAMPED to the declared bound',
    );
    check(
      v.blobLength >= 0 && v.blobLength <= 6,
      '$what: blob length in bound',
    );
  });
}

// THE SECOND WIRE BYTE A FIXED READER NORMALISES, swept where it lives: P3's
// Chain carries an OPTIONAL, so byte 20 of the body is its PRESENT flag — `0`
// or `1` on the wire and nothing else. A forged `2` must land as the language's
// own true, never verbatim (§4.5). This sweep drives the text length and the
// ranged payload beside it too.
void p3Optional() {
  final one = p3home.Chain();
  one.name.setRange(0, 5, 'dirty'.codeUnits);
  one.nameLength = 5;
  one.linkPresent = true;
  one.link.value = 777;
  one.link.tag.setRange(0, 5, 'stale'.codeUnits);
  one.link.tagLength = 5;
  final file = Uint8List(p3.chainFixedMeasure(1));
  check(
    p3.chainFixedSave(<p3home.Chain>[one], 1, file) == file.length,
    'P3 p3: the lawful record saves',
  );

  p3Sweep('p3', file, p3.chainFixedHeaderBytes, p3.chainFixedRecordBytes, (
    mutant,
    at,
    poison,
  ) {
    final back = <p3home.Chain>[p3home.Chain()];
    final report = p3home.TableFixedReport();
    final n = p3.chainFixedLoad(
      back,
      1,
      mutant,
      mutant.length,
      p3.chainFixedNewPlan(),
      report,
    );
    final what = 'P3 p3 byte $at poison 0x${poison.toRadixString(16)}';
    if (n < 0) {
      check(
        report.refused != 0 || report.malformed,
        '$what: a negative read is a named refusal or malformed',
      );
      return;
    }
    check(n == 1, '$what: a one-record file answers one record (got $n)');
    final v = back[0];
    check(
      v.nameLength >= 0 && v.nameLength <= 16,
      '$what: name length in bound',
    );
    check(
      v.link.value >= 0 && v.link.value <= 1000,
      '$what: link value in bound',
    );
    check(
      v.link.tagLength >= 0 && v.link.tagLength <= 8,
      '$what: link tag length in bound',
    );
  });
}

// THE COMPILED PATH, which P3's "both paths" names: an OLD P1 record read by
// the P3 loader finds a hash that is not its own and runs a plan the emitter
// compiled from P1's layout. The SAME read loop with the SAME clamps, so a
// mutant must answer the same three ways.
void p3Compiled() {
  final one = p1home.Chain();
  one.name.setRange(0, 5, 'chain'.codeUnits);
  one.nameLength = 5;
  one.link.value = 500;
  one.link.tag.setRange(0, 3, 'tag'.codeUnits);
  one.link.tagLength = 3;
  final file = Uint8List(p1.chainFixedMeasure(1));
  check(
    p1.chainFixedSave(<p1home.Chain>[one], 1, file) == file.length,
    'P3 p1->p3: the lawful record saves',
  );

  p3Sweep('p1->p3', file, p1.chainFixedHeaderBytes, p1.chainFixedRecordBytes, (
    mutant,
    at,
    poison,
  ) {
    final back = <p3home.Chain>[p3home.Chain()];
    final report = p3home.TableFixedReport();
    final n = p3.chainFixedLoad(
      back,
      1,
      mutant,
      mutant.length,
      p3.chainFixedNewPlan(),
      report,
    );
    final what = 'P3 p1->p3 byte $at poison 0x${poison.toRadixString(16)}';
    if (n < 0) {
      check(
        report.refused != 0 || report.malformed,
        '$what: a negative read is a named refusal or malformed',
      );
      return;
    }
    check(n == 1, '$what: a one-record file answers one record (got $n)');
    final v = back[0];
    check(
      v.nameLength >= 0 && v.nameLength <= 16,
      '$what: name length in bound',
    );
    check(
      v.link.value >= 0 && v.link.value <= 1000,
      '$what: link value in bound',
    );
    check(
      v.link.tagLength >= 0 && v.link.tagLength <= 8,
      '$what: link tag length in bound',
    );
  });
}

void hostileBytesCase() {
  p3Mutants = 0;
  p3Threw = 0;
  p3Fx1();
  p3Optional();
  p3Compiled();
  print('P3 hostile bytes: $p3Mutants mutants swept, $p3Threw threw');
}

// ---------------------------------------------------------------------------

// 9. WRITE SLACK IS THE TEMPLATE'S ZEROS (docs/FIXED-FORM-ALGORITHM.md fix 1,
// docs/SPEC-TABLES.md §3.4). A `string(8)` used to two units, a `[..4]int32`
// with one live slot and a `bytes(6)` used to two bytes are DECLARED bytes
// carrying no value; what rides in the slack behind them is the ZEROS the
// template laid down, never this writer's own storage past the used length or
// the live count.
//
// THE ROUND TRIP CANNOT SEE THIS: a read lands the WIRE's zeros in the slack,
// so saving it back is byte-identical whether or not the writer copies whole
// spans. The stain has to be put in the WRITER's storage, which is what this
// case does. The reference's own slack_case (test/tables/fixedform_main.cpp)
// and the Rust, C, C# and JS legs hold the same case.
//
// THE CONTROL IS THE STAIN, as it is for every other kind of slack: the
// storage past the used length and the live count is filled with a byte a
// clean record carries nowhere. The case first proves the stain IS in the
// storage (or it is checking nothing), then proves the WIRE carries none of
// it, and then proves a whole-span copy of the same storage WOULD have
// carried it — the wrong behaviour, watched failing.
void writeSlackCase() {
  final v = fx1home.FxRoot();
  v.keep = 11;
  v.narrow = 22;
  v.renamed = 33;
  v.gone = 44;
  v.nested.a = 55;
  v.nested.b = 66;
  v.label.fillRange(0, 8, 0xAA);
  v.label[0] = 0x68; // 'h'
  v.label[1] = 0x69; // 'i'
  v.labelLength = 2;
  v.marks.fillRange(0, 4, 0x5A5A5A5A);
  v.marks[0] = 7;
  v.marksCount = 1;
  v.blob.fillRange(0, 6, 0x11);
  v.blob[0] = 0xDE;
  v.blob[1] = 0xAD;
  v.blobLength = 2;

  // the stain is really in the storage, so nothing below is vacuous
  check(
    v.label[2] == 0xAA,
    'W1 CONTROL: the text slack really is stained in storage',
  );
  check(
    v.marks[1] == 0x5A5A5A5A,
    'W1 CONTROL: the array slack really is stained in storage',
  );
  check(
    v.blob[2] == 0x11,
    'W1 CONTROL: the bytes slack really is stained in storage',
  );

  final w = Uint8List(fx1.fxRootFixedMeasure(1));
  check(
    fx1.fxRootFixedSave(<fx1home.FxRoot>[v], 1, w) == w.length,
    'W1 slack: the record saves',
  );
  // the record's BODY, past the sixteen-byte header, the layout and the
  // record's own eight-byte hash. THE OFFSETS ARE FX1's OWN LAYOUT
  // (test/tables/FX1.schema): label's length at 22 and its 8 content bytes at
  // 26, marks' count at 34 and its four int32 slots at 38, blob's length at 54
  // and its 6 content bytes at 58 — the same body offsets absentOptionalCase
  // reads.
  final at = fx1.fxRootFixedHeaderBytes + 8;
  final body = w.sublist(at, at + fx1.fxRootFixedBodyBytes);
  final wire = ByteData.sublistView(body);
  // THE SCAN IS OVER THE SLACK AND NOT THE WHOLE BODY, and the wire's OWN
  // length and count say where the live extent ends. A legitimate FULLY-LIVE
  // value puts the storage's bytes there, so a whole-body marker scan would
  // fire on a value the rule admits — the wrong question. The slack is the
  // bytes past the live extent, and only those; a fully-live value leaves the
  // range empty.
  bool slackClean(int from, int to, int stain) {
    for (var i = from; i < to; i++) {
      if (body[i] == stain) {
        return false;
      }
    }
    return true;
  }

  check(
    slackClean(26 + wire.getInt32(22, Endian.little), 34, 0xAA),
    'WRITE SLACK IS TEMPLATE ZEROS: not one stained TEXT byte reached the wire',
  );
  check(
    slackClean(38 + wire.getInt32(34, Endian.little) * 4, 54, 0x5A),
    'WRITE SLACK IS TEMPLATE ZEROS: not one stained ARRAY byte reached the wire',
  );
  check(
    slackClean(58 + wire.getInt32(54, Endian.little), 64, 0x11),
    'WRITE SLACK IS TEMPLATE ZEROS: not one stained BYTES byte reached the wire',
  );

  // NEGATIVE CONTROL: the same storage copied WHOLE — which is what the
  // writer did before fix 1 — carries the stain, so the checks above
  // discriminate and are not passing for some other reason.
  check(
    v.label.sublist(2).contains(0xAA),
    'W1 NEGATIVE CONTROL: a whole-span copy WOULD have carried the text stain',
  );
  check(
    v.marks[3] == 0x5A5A5A5A,
    'W1 NEGATIVE CONTROL: a whole-span copy WOULD have carried the array stain',
  );
  check(
    v.blob[5] == 0x11,
    'W1 NEGATIVE CONTROL: a whole-span copy WOULD have carried the bytes stain',
  );

  // and the record still reads back as itself: the used length and the live
  // count are what the reader validates, and both are inside the bound, so
  // nothing clamps. THE EXPECTATIONS ARE THE VALUE'S OWN, so pointing the case
  // at the rule's other admitted value — a fully-live extent, or none at all —
  // is one change to the lengths above and nothing else.
  final back = <fx1home.FxRoot>[fx1home.FxRoot()];
  final r = fx1home.TableFixedReport();
  final n = fx1.fxRootFixedLoad(
    back,
    1,
    w,
    w.length,
    fx1.fxRootFixedNewPlan(),
    r,
  );
  check(n == 1, 'W1 slack: the record reads');
  check(
    back[0].labelLength == v.labelLength &&
        text(back[0].label, v.labelLength) == text(v.label, v.labelLength),
    'W1 slack: the used length reads, and the content with it',
  );
  check(back[0].marksCount == v.marksCount, 'W1 slack: the live count reads');
  for (var i = 0; i < v.marksCount; i++) {
    check(back[0].marks[i] == v.marks[i], 'W1 slack: live slot $i reads');
  }
  check(
    back[0].blobLength == v.blobLength,
    'W1 slack: the live bytes length reads',
  );
  for (var i = 0; i < v.blobLength; i++) {
    check(back[0].blob[i] == v.blob[i], 'W1 slack: live byte $i reads');
  }
  // and the slack the record did not use lands as the wire's zero, never as
  // this writer's leftover
  for (var i = v.labelLength; i < 8; i++) {
    check(
      back[0].label[i] == 0,
      'W1 slack: unused text byte $i lands as the wire\'s zero',
    );
  }
  for (var i = v.marksCount; i < 4; i++) {
    check(
      back[0].marks[i] == 0,
      'W1 slack: an unused slot lands as the wire\'s zero and not as some writer\'s leftover',
    );
  }
  for (var i = v.blobLength; i < 6; i++) {
    check(
      back[0].blob[i] == 0,
      'W1 slack: unused bytes land as the wire\'s zero',
    );
  }
  quiet(r, 'W1 slack');
}

// W4: READ SLACK IS UNSPECIFIED (docs/FIXED-FORM-ALGORITHM.md §3.1). The rule,
// by name: "Slack is UNSPECIFIED on read and not a refusal: a reader validates
// the USED UNITS only, and non-zero slack is not `malformed`, not a refusal,
// and moves no counter." `absentOptionalCase` above is the WRITE half of the
// same rule; this is the read half, and the one that faces a STRANGER's bytes.
//
// THE STAIN IS ON THE WIRE, which is what separates this from every other slack
// case in this file. A clean writer lays the template's zeros in the slack, so
// reading a record THIS BUILD wrote cannot tell a reader that walks the USED
// UNITS from one that walks the DECLARED BOUND. Here the slack of V1's
// `items [..8]int32` (a RANGED element: | min = 0, max = 255) is stained AFTER
// the write with four 0xFF bytes per unused slot — a value past the range — so
// a decode that walked the bound would clamp seven times and a decode that
// walks the live count clamps none. `clamped` is the observable, and the doc's
// "moves no counter" is the claim.
void readSlackUnspecified() {
  // THE COUNT'S OFFSET IS FOUND, NOT SPELLED: two saves that differ only in the
  // live count, and the first byte that differs is the count's own. A hardcoded
  // offset here would quietly stop naming `items` the day a field moves.
  Uint8List save(int count) {
    final one = v1home.Cfg();
    one.itemsCount = count;
    one.items[0] = 7;
    final w = Uint8List(v1.cfgFixedMeasure(1));
    check(
      v1.cfgFixedSave(<v1home.Cfg>[one], 1, w) == w.length,
      'read slack: the count=$count record saves',
    );
    return w;
  }

  final live = save(1);
  final twin = save(2);
  final view = ByteData.sublistView(live);
  var countAt = -1;
  for (var i = v1.cfgFixedHeaderBytes + 8; i < live.length; i++) {
    if (live[i] != twin[i]) {
      countAt = i;
      break;
    }
  }
  check(
    countAt >= 0,
    'read slack: the count field is the one byte two saves differ in',
  );
  check(
    view.getInt32(countAt, Endian.little) == 1,
    'read slack: the wire count is the live 1, found by shape and not spelled',
  );

  // STAIN THE SLACK: every byte of the seven unused int32 slots, past the count
  // word and the one live element, to the end of the declared eight.
  final slackAt = countAt + 4 + 4;
  final slackEnd = countAt + 4 + 8 * 4;
  // ONE BYTE, SO CONTROL 1 IS ONE LINE: point it at the conforming zero a
  // clean writer lays there and the case must stay green; 0xFF is the hostile
  // value past the ranged element's bound that a bound-walker would clamp.
  const stain = 0xFF;
  for (var i = slackAt; i < slackEnd; i++) {
    live[i] = stain;
  }
  var stained = 0;
  for (var i = slackAt; i < slackEnd; i++) {
    if (live[i] == stain) {
      stained++;
    }
  }
  check(
    stained == slackEnd - slackAt,
    'CONTROL: the wire slack really is stained, so the read below is not vacuous',
  );

  final back = <v1home.Cfg>[v1home.Cfg()];
  final r = v1home.TableFixedReport();
  final n = v1.cfgFixedLoad(
    back,
    1,
    live,
    live.length,
    v1.cfgFixedNewPlan(),
    r,
  );
  check(n == 1, 'read slack: the record reads (got $n)');
  check(
    !r.malformed && r.refused == 0,
    'SLACK IS UNSPECIFIED: stained slack is neither malformed nor a refusal',
  );
  check(
    back[0].itemsCount == 1 && back[0].items[0] == 7,
    'SLACK IS UNSPECIFIED: the USED UNITS land and the slack behind them is not read',
  );
  check(
    r.clamped == 0,
    'SLACK IS UNSPECIFIED: a reader validates the USED UNITS only and moves no '
    'counter (clamped ${r.clamped})',
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

  // AND THE OTHER HALF OF THAT CONTROL IS RETIRED (§5.6): it asserted that the
  // loader COMPILES a plan from the layout a file carried and gets it right,
  // which is the mechanism this section replaced — the load selects by hash
  // against the lineage and compiles nothing. The coverage is owed by the
  // lineage harness's NEW-READS-OLD column.
  retire(
    'negativeControl: the loader compiles a plan from the layout',
    'the load path compiles nothing now: internal/codegen/darttable/'
        'fixedversioning_test.go reads every row through a plan the BUILD laid '
        'down from the lock',
  );

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
  // A REFUSAL WRITES NOTHING. §5.3's REFUSE is total — no counter moves,
  // nothing is decoded, and not one destination byte is written, the prefill
  // included — so the destination record is pre-poisoned here with sentinels
  // no value of this type holds and must come back exactly as it was. The
  // report is the other half: it names the reason and nothing else claims a
  // read (no `malformed` beside a name, no counter moved, no hash reported).
  fx1home.FxRoot poisonedRoot() {
    final v = fx1home.FxRoot();
    v.keep = 0x7f7f7f7f;
    v.narrow = 0x5a5a;
    v.renamed = 0x3c3c;
    v.gone = 0x1e1e;
    v.nested.a = 0x1111;
    v.nested.b = 0x2222;
    return v;
  }

  bool rootIntact(fx1home.FxRoot v) =>
      v.keep == 0x7f7f7f7f &&
      v.narrow == 0x5a5a &&
      v.renamed == 0x3c3c &&
      v.gone == 0x1e1e &&
      v.nested.a == 0x1111 &&
      v.nested.b == 0x2222;

  bool namedOnly(fx1home.TableFixedReport r, int want) =>
      r.refused == want &&
      !r.malformed &&
      r.unknown == 0 &&
      r.kindMismatch == 0 &&
      r.clamped == 0 &&
      r.widened == 0 &&
      r.hash == 0 &&
      r.layoutHash == 0;

  bool malformedOnly(fx1home.TableFixedReport r) =>
      r.malformed &&
      r.refused == fx1home.TableFixedRefusal.none &&
      r.unknown == 0 &&
      r.kindMismatch == 0 &&
      r.clamped == 0 &&
      r.widened == 0 &&
      r.hash == 0 &&
      r.layoutHash == 0;

  planted.forEach((byte, want) {
    final other = Uint8List.fromList(w2);
    other[0] = byte;
    final v = <fx1home.FxRoot>[poisonedRoot()];
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
      n < 0 && namedOnly(r, want),
      'REFUSED BY NAME: form byte $byte is ${fx1home.TableFixedRefusal.name(want)} '
      '(got ${fx1home.TableFixedRefusal.name(r.refused)}), and malformed does not fire',
    );
    check(
      rootIntact(v[0]),
      'REFUSAL WRITES NOTHING: form byte $byte leaves the destination record unchanged',
    );
  });

  // THE FIVE FORM-HEADER PROBES (schema#876 comment 5654419328, card 411411),
  // EACH ON ITS EXACT INPUT. §5.3 step 1 and SPEC-TABLES §3.4 read the FORM
  // BYTE BEFORE THE FILE'S LENGTH, so the REAL committed lengths of the two
  // older forms — a form-1 file is TEN bytes and a form-2 batch THREE — are
  // each answered by NAME and never `malformed`; only a file with no first
  // byte at all is `malformed` before any byte can earn a name, and the
  // twenty-byte minimum is step 2, after the byte has answered.

  // CASE empty-file: ZERO BYTES is the residue `malformed`, with no name,
  // because there is no first byte to earn one (§5.3 step 1).
  {
    final v = <fx1home.FxRoot>[poisonedRoot()];
    final r = fx1home.TableFixedReport();
    final n = fx1.fxRootFixedLoad(
      v,
      1,
      Uint8List(0),
      0,
      fx1.fxRootFixedNewPlan(),
      r,
    );
    check(
      n < 0 && malformedOnly(r),
      'CASE empty-file (zero bytes): malformed and no reason, and nothing else claims a read',
    );
    check(
      rootIntact(v[0]),
      'CASE empty-file: REFUSAL WRITES NOTHING to the destination record',
    );
  }

  // CASE short-form1: a TEN-byte form-1 file is `previous_form`, NOT
  // `malformed` — the variable form is OLDER than this one.
  {
    final v = <fx1home.FxRoot>[poisonedRoot()];
    final r = fx1home.TableFixedReport();
    final n = fx1.fxRootFixedLoad(
      v,
      1,
      Uint8List.fromList(<int>[1, 0, 0, 0, 0, 0, 0, 0, 0, 0]),
      10,
      fx1.fxRootFixedNewPlan(),
      r,
    );
    check(
      n < 0 && namedOnly(r, fx1home.TableFixedRefusal.previousForm),
      'CASE short-form1 (a ten-byte form-1 file): previous_form, not malformed, '
      'and nothing else claims a read',
    );
    check(
      rootIntact(v[0]),
      'CASE short-form1: REFUSAL WRITES NOTHING to the destination record',
    );
  }

  // CASE short-form2: a THREE-byte form-2 batch is `message_form_as_file`,
  // NOT `malformed` — form 2 is a batch where a FILE was expected.
  {
    final v = <fx1home.FxRoot>[poisonedRoot()];
    final r = fx1home.TableFixedReport();
    final n = fx1.fxRootFixedLoad(
      v,
      1,
      Uint8List.fromList(<int>[2, 1, 0]),
      3,
      fx1.fxRootFixedNewPlan(),
      r,
    );
    check(
      n < 0 && namedOnly(r, fx1home.TableFixedRefusal.messageFormAsFile),
      'CASE short-form2 (a three-byte form-2 batch): message_form_as_file, not '
      'malformed, and nothing else claims a read',
    );
    check(
      rootIntact(v[0]),
      'CASE short-form2: REFUSAL WRITES NOTHING to the destination record',
    );
  }

  // CASE reserved-byte3: a valid form-3 fixture with reserved byte 3 set
  // nonzero is `malformed` — the seven reserved bytes are REFUSED rather than
  // ignored, which is what keeps them spendable later (§3.4, §5.3 step 2).
  {
    final bad = Uint8List.fromList(fx1Record());
    bad[3] = 1; // offset 3 is one of the header's seven reserved bytes
    final v = <fx1home.FxRoot>[poisonedRoot()];
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
      n < 0 && malformedOnly(r),
      'CASE reserved-byte3 (a valid form-3 fixture with reserved byte 3 nonzero): '
      'malformed and no reason, and nothing else claims a read',
    );
    check(
      rootIntact(v[0]),
      'CASE reserved-byte3: REFUSAL WRITES NOTHING to the destination record',
    );
  }

  // THE RECOMPUTE OF THE HEADER'S HASH FROM THE LAYOUT BEHIND IT IS RETIRED
  // (§5.6): the digest is not on the wire, so the hash cannot be re-derived
  // from a file at all — it is TAKEN AS GIVEN and the layout is held to a BYTE
  // COMPARISON against the bytes the lock recorded. A hash nothing in the
  // lineage holds is `layout_newer`; a KNOWN hash whose bytes differ is
  // `layout_malformed`.
  retire(
    'negativeControl: a header whose hash is not the layout hash',
    'there is no third thing to check: hash_known_bytes_differ in '
        'internal/codegen/darttable/fixedversioning_test.go holds a known hash '
        'with changed layout bytes to layout_malformed, and hash_unknown holds '
        'an unknown hash to layout_newer',
  );

  // A RECORD WHOSE HASH NAMES NO LAYOUT THIS READER HOLDS is a refusal by name.
  // IT IS REACHED THROUGH THIS READER'S OWN FILE now: under §5 the lineage
  // select runs FIRST, so a stranger's file never reaches the record loop at
  // all — the rule, the name and the assertion are unchanged, the file it is
  // planted in is this build's own (§5.3 step 11).
  {
    final bad = Uint8List.fromList(fx1Record());
    bad[fx1.fxRootFixedHeaderBytes] ^= 0xff; // the record's own hash
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

  // A PLAN THAT DOES NOT FIT THE CALLER'S STORAGE IS STILL A REFUSAL BY NAME
  // (§5.9 #5: the plan slice stays a CAPACITY DECLARATION, checked and never
  // written through) — but this control planted it on a STRANGER's file, and a
  // stranger is `layout_newer` before a plan is ever looked up. The name is
  // live in the generated load; what has no fixture on this leg is a LINEAGE
  // ENTRY big enough to overflow a one-entry plan, because these libraries are
  // generated with no lock. NAMED AND OWED rather than quietly dropped
  // (§5.9 #31).
  retire(
    'negativeControl: plan_too_large over a stranger file',
    'the refusal is wired in the generated load against the selected lineage '
        'entry\'s own entry count; a fixture wants a LOCKED lineage for '
        'test/tables, which this leg does not have yet',
  );

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

// 9. THE BOUNDS PASS WALKS LIVE THINGS ONLY (docs/FIXED-FORM-ALGORITHM.md
// §4.6). The pass that clamps ranged scalars and remaps ordinals runs over
// STORAGE after the plan run, which is what lets ONE pass cover both plans —
// and it walks only what a read can have written: a counted array's LIVE
// elements and never its slack. The reason is a counter: the prefill's
// defaults are in range by construction, so clamping storage nobody wrote
// would count a clamp on every clean read.
//
// The pack coverage above reads a CONFORMING file, whose slack is ZEROS — and
// zeros are in range, so a pass that walked the bound would count nothing
// there and `clamped == 0` would still hold. That is detection, not assertion.
// This case STAINs the slack THROUGH STORAGE, with the same out-of-range value
// CONTROL 1 puts in a live element, and holds the read to `clamped == 0` — the
// number only a pass that walks the LIVE COUNT can produce.
void boundsCase() {
  // a record with a PARTLY-FILLED counted array: reservesCount = 2, and the
  // third ShipEntry is slack. Every LIVE value is in range, so a clean read
  // counts nothing on its own; the stain then goes into the slack.
  final one = demo.PackConfig();
  one.version = 7;
  one.global.tickRate = 120;
  one.global.buildNote.setRange(0, 5, 'clean'.codeUnits);
  one.global.buildNoteLength = 5;
  for (var s = 0; s < 3; s++) {
    one.ships[s].hardpointsCount = 1;
    one.ships[s].hardpoints[0] = s + 1;
  }
  one.thresholds[0] = 100;
  one.thresholds[1] = 200;
  one.thresholds[2] = 300;
  one.reservesCount = 2;
  one.reserves[0].displayName.setRange(0, 5, 'live0'.codeUnits);
  one.reserves[0].displayNameLength = 5;
  one.reserves[0].hardpointsCount = 1;
  one.reserves[0].hardpoints[0] = 1;
  one.reserves[1].displayName.setRange(0, 5, 'live1'.codeUnits);
  one.reserves[1].displayNameLength = 5;
  one.reserves[1].hardpointsCount = 1;
  one.reserves[1].hardpoints[0] = 2;

  final w = Uint8List(pack.packConfigFixedMeasure(1));
  check(
    pack.packConfigFixedSave(<demo.PackConfig>[one], 1, w) == w.length,
    'bounds: the record saves',
  );

  // THE STAIN IS LAID THROUGH STORAGE, never the wire: the offsets are the
  // write template's, reserves sitting at body 383 with elements at +i*98 and
  // a ShipEntry's hardpoints count at element+44, its first value at
  // element+48 — the same numbers the identity plan's own entries carry (623
  // and 627). The slack element's COUNT stays in range (the plan clamps
  // counts, and that clamp would be the wrong thing to test); its first VALUE
  // goes past the declared `max = 8`, which only the decode can answer for.
  final body = pack.packConfigFixedHeaderBytes + 8;
  final view = ByteData.sublistView(w);
  view.setInt32(body + 623, 1, Endian.little);
  view.setInt32(body + 627, 0x7F, Endian.little);

  final back = <demo.PackConfig>[demo.PackConfig()];
  final r = demo.TableFixedReport();
  final n = pack.packConfigFixedLoad(
    back,
    1,
    w,
    w.length,
    pack.packConfigFixedNewPlan(),
    r,
  );
  check(n == 1, 'bounds: the stained record still reads (got $n)');
  check(
    back[0].reservesCount == 2 &&
        back[0].reserves[0].hardpoints[0] == 1 &&
        back[0].reserves[1].hardpoints[0] == 2,
    'bounds: the two live elements land, and the slack\'s stain is never decoded',
  );
  check(
    r.clamped == 0,
    'THE BOUNDS PASS WALKS A COUNTED ARRAY\'S LIVE ELEMENTS AND NEVER ITS '
    'SLACK (docs/FIXED-FORM-ALGORITHM.md §4.6): clamping storage nobody wrote '
    'would count a clamp on every clean read — the out-of-range value stained '
    'into the slack counts no clamp (clamped=${r.clamped})',
  );

  // CONTROL 1 — THE SAME VALUE IN A LIVE ELEMENT. reserves[1] is live, so its
  // out-of-range value clamps to max, counts EXACTLY one, and an in-range
  // neighbour is untouched. Without this, a pass that never ran at all would
  // also report `clamped == 0`.
  final w2 = Uint8List(pack.packConfigFixedMeasure(1));
  check(
    pack.packConfigFixedSave(<demo.PackConfig>[one], 1, w2) == w2.length,
    'bounds: control saves',
  );
  final view2 = ByteData.sublistView(w2);
  view2.setInt32(body + 529, 0x7F, Endian.little); // reserves[1], LIVE
  final back2 = <demo.PackConfig>[demo.PackConfig()];
  final r2 = demo.TableFixedReport();
  check(
    pack.packConfigFixedLoad(
          back2,
          1,
          w2,
          w2.length,
          pack.packConfigFixedNewPlan(),
          r2,
        ) ==
        1,
    'bounds: control reads',
  );
  check(
    back2[0].reserves[1].hardpoints[0] == 8,
    'CONTROL 1: a live element past max lands at max '
    '(got ${back2[0].reserves[1].hardpoints[0]})',
  );
  check(
    back2[0].reserves[0].hardpoints[0] == 1,
    'CONTROL 1: an in-range neighbour is untouched',
  );
  check(
    r2.clamped == 1,
    'CONTROL 1: the live clamp is counted exactly once (clamped=${r2.clamped})',
  );
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
// W14: THE PLAN'S DESTINATIONS ARE THIS BUILD'S OWN OFFSETS, AND THEY FIT THE
// BODY (docs/FIXED-FORM-ALGORITHM.md §7). The contract says it in one line:
//
//   "Every port owes that test, and the build-time assertion that the plan's
//    destinations equal its own `offsetof` and `sizeof`."
//
// The C++ reference emits that assertion as generated `static_assert` lines
// comparing the numbers the plan is built from against the compiler's own
// `offsetof`/`sizeof` (internal/codegen/cpptable/fixedform.go,
// emitFixedLayoutAsserts). Dart has no struct ABI to take an `offsetof` of, so
// THIS leg's offsets are its own storage walk: the five-lane `FixedDst` rows,
// resolved through the pre-order layout into absolute body offsets, and
// `FixedBodyBytes` for the sizeof. The assertion below is named: every
// destination the identity plan lands must be one of those offsets and must
// lie inside that size. A plan that drifts from the storage the writer and the
// decoder use is caught HERE, at build time, and never on the wire.
// ---------------------------------------------------------------------------

// w14Offsets resolves the five-lane `FixedDst` rows — dst, stride, aux,
// counted, arg — into the set of absolute byte offsets this build's storage
// walk declares. The layout is a u32 count then 17-byte entries (id u64, kind
// u8, size u32, children u32), pre-order: each entry's dst and aux are measured
// from its PARENT, so the walk carries the parent's absolute offset down.
Set<int> w14Offsets(Uint8List layout, Int32List dstRows) {
  final view = ByteData.sublistView(layout);
  final rows = dstRows.length ~/ 5;
  final offsets = <int>{};
  var index = 0;
  void walk(int parent) {
    if (index >= rows) {
      return;
    }
    final row = index * 5;
    final at = parent + dstRows[row];
    offsets.add(at);
    // a text row's aux is the buffer, a counted array's aux is the count, an
    // optional's aux is the present flag: each is an offset the plan may name
    if (dstRows[row + 2] != 0) {
      offsets.add(parent + dstRows[row + 2]);
    }
    final children = view.getUint32(17 + index * 17, Endian.little);
    index++;
    for (var c = 0; c < children; c++) {
      walk(at);
    }
  }

  walk(0);
  return offsets;
}

// w14Check is W14 for ONE root: the sizeof, then every destination the identity
// plan lands against the offsets the storage walk declares.
void w14Check(
  String who,
  Uint8List layout,
  Int32List dstRows,
  Int32List plan,
  int planCount,
  int bodyBytes,
) {
  final offsets = w14Offsets(layout, dstRows);
  final view = ByteData.sublistView(layout);
  check(
    view.getUint32(13, Endian.little) == bodyBytes,
    'W14: $who — the root layout entry states $bodyBytes bytes (the sizeof)',
  );
  for (var i = 0; i < planCount; i++) {
    final base = i * 9;
    final op = plan[base];
    final dst = plan[base + 2];
    final size = plan[base + 3];
    final aux = plan[base + 4];
    check(
      dst >= 0 && dst < bodyBytes,
      'W14: $who entry $i dst $dst inside the body $bodyBytes',
    );
    check(
      offsets.contains(dst),
      'W14: $who entry $i dst $dst is a storage offset of this build',
    );
    if (op == 1) {
      // a COUNT lands the four bytes of the count itself
      check(
        dst + 4 <= bodyBytes,
        'W14: $who entry $i count lands inside the body',
      );
    } else if (op == 2) {
      // a TEXT lands its four-byte length at dst and its units at aux
      check(
        offsets.contains(aux),
        'W14: $who entry $i aux $aux is a storage offset of this build',
      );
      check(
        dst + 4 <= bodyBytes && aux + size <= bodyBytes,
        'W14: $who entry $i text lands inside the body',
      );
    } else {
      check(
        dst + size <= bodyBytes,
        'W14: $who entry $i copy lands inside the body',
      );
    }
  }
}

void planDstCase() {
  w14Check(
    'fx1.fxRoot',
    fx1.fxRootFixedLayout,
    fx1.fxRootFixedDst,
    fx1.fxRootFixedIdentity,
    fx1.fxRootFixedIdentityCount,
    fx1.fxRootFixedBodyBytes,
  );
  w14Check(
    'fx2.fxRoot',
    fx2.fxRootFixedLayout,
    fx2.fxRootFixedDst,
    fx2.fxRootFixedIdentity,
    fx2.fxRootFixedIdentityCount,
    fx2.fxRootFixedBodyBytes,
  );
  w14Check(
    'p1.link',
    p1.linkFixedLayout,
    p1.linkFixedDst,
    p1.linkFixedIdentity,
    p1.linkFixedIdentityCount,
    p1.linkFixedBodyBytes,
  );
  w14Check(
    'p1.chain',
    p1.chainFixedLayout,
    p1.chainFixedDst,
    p1.chainFixedIdentity,
    p1.chainFixedIdentityCount,
    p1.chainFixedBodyBytes,
  );
  w14Check(
    'p3.chain',
    p3.chainFixedLayout,
    p3.chainFixedDst,
    p3.chainFixedIdentity,
    p3.chainFixedIdentityCount,
    p3.chainFixedBodyBytes,
  );
  w14Check(
    'fu1.fuRoot',
    fu1.fuRootFixedLayout,
    fu1.fuRootFixedDst,
    fu1.fuRootFixedIdentity,
    fu1.fuRootFixedIdentityCount,
    fu1.fuRootFixedBodyBytes,
  );
  w14Check(
    'fu2.fuRoot',
    fu2.fuRootFixedLayout,
    fu2.fuRootFixedDst,
    fu2.fuRootFixedIdentity,
    fu2.fuRootFixedIdentityCount,
    fu2.fuRootFixedBodyBytes,
  );
  w14Check(
    'fxw.fxWide',
    fxw.fxWideFixedLayout,
    fxw.fxWideFixedDst,
    fxw.fxWideFixedIdentity,
    fxw.fxWideFixedIdentityCount,
    fxw.fxWideFixedBodyBytes,
  );
  w14Check(
    'fxw.fxCaption',
    fxw.fxCaptionFixedLayout,
    fxw.fxCaptionFixedDst,
    fxw.fxCaptionFixedIdentity,
    fxw.fxCaptionFixedIdentityCount,
    fxw.fxCaptionFixedBodyBytes,
  );
  w14Check(
    'pack.globalSettings',
    pack.globalSettingsFixedLayout,
    pack.globalSettingsFixedDst,
    pack.globalSettingsFixedIdentity,
    pack.globalSettingsFixedIdentityCount,
    pack.globalSettingsFixedBodyBytes,
  );
  w14Check(
    'pack.gunnerSettings',
    pack.gunnerSettingsFixedLayout,
    pack.gunnerSettingsFixedDst,
    pack.gunnerSettingsFixedIdentity,
    pack.gunnerSettingsFixedIdentityCount,
    pack.gunnerSettingsFixedBodyBytes,
  );
  w14Check(
    'pack.shipEntry',
    pack.shipEntryFixedLayout,
    pack.shipEntryFixedDst,
    pack.shipEntryFixedIdentity,
    pack.shipEntryFixedIdentityCount,
    pack.shipEntryFixedBodyBytes,
  );
  w14Check(
    'keyed.teamConfig',
    keyed.teamConfigFixedLayout,
    keyed.teamConfigFixedDst,
    keyed.teamConfigFixedIdentity,
    keyed.teamConfigFixedIdentityCount,
    keyed.teamConfigFixedBodyBytes,
  );
  w14Check(
    'keyed.gunnerConfig',
    keyed.gunnerConfigFixedLayout,
    keyed.gunnerConfigFixedDst,
    keyed.gunnerConfigFixedIdentity,
    keyed.gunnerConfigFixedIdentityCount,
    keyed.gunnerConfigFixedBodyBytes,
  );
  w14Check(
    'keyed.turretConfig',
    keyed.turretConfigFixedLayout,
    keyed.turretConfigFixedDst,
    keyed.turretConfigFixedIdentity,
    keyed.turretConfigFixedIdentityCount,
    keyed.turretConfigFixedBodyBytes,
  );
  w14Check(
    'bench.fixedTable',
    bench.fixedTableFixedLayout,
    bench.fixedTableFixedDst,
    bench.fixedTableFixedIdentity,
    bench.fixedTableFixedIdentityCount,
    bench.fixedTableFixedBodyBytes,
  );

  // THE NEGATIVE, so the case is not vacuous: the SAME question asked of a
  // destination one byte off answers NO. If a drifted destination still read
  // as a storage offset the case above would prove nothing.
  final offsets = w14Offsets(fx1.fxRootFixedLayout, fx1.fxRootFixedDst);
  final drifted = Int32List.fromList(fx1.fxRootFixedIdentity);
  drifted[2] += 1; // entry 0's dst
  check(
    !offsets.contains(drifted[2]),
    'W14 NEGATIVE CONTROL: a drifted destination is not a storage offset',
  );
  print(
    'W14: 16 roots — every identity-plan destination is the build\'s own '
    'storage offset and lands inside FixedBodyBytes (the sizeof)',
  );
}

// ---------------------------------------------------------------------------
// F4: A LAYOUT LENGTH THAT RUNS PAST THE FILE (docs/FIXED-FORM-ALGORITHM.md
// §1.1 step 3, §5.3 step 3: `20 + L > bytes` REFUSE `layout_malformed`)
// ---------------------------------------------------------------------------
//
// THIS IS FRAMING, NOT ONE OF §1.1'S SEVEN RULES. §5.3 retired the run-time
// WALK of a stranger layout, but this check runs BEFORE the hash is ever
// selected, so it did not retire with the walk and is asserted live here. A
// twenty-four-byte file stating a layout of one hundred would make a reader
// that trusted the length step past the bytes it was handed before a hash could
// name a plan. THE CHECK FIRES FIRST, so the header's hash is left zero and
// selects nothing — which is why the name is `layout_malformed` and not
// `layout_newer`.
void layoutTruncatedCase() {
  final f = Uint8List(layoutAt + 4); // the header and four bytes of "layout"
  f[0] = 3; // this form
  // L = 100: 20 + 100 = 120 runs past the file's twenty-four bytes.
  ByteData.sublistView(f).setUint32(layoutLengthAt, 100, Endian.little);
  refuses(
    f,
    fx1home.TableFixedRefusal.layoutMalformed,
    'RULE: a layout length that runs past the file is layout_malformed',
  );
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
  planDstCase();
  // W7, THE TWO LANES (docs/FIXED-FORM-ALGORITHM.md §4.1 fix 12). It reads
  // only the build's own static plan and a record it writes itself, so it is
  // called BEFORE the reference corpus is opened: its line is then visible
  // even where the C++ oracle is absent, which is the one thing this file's
  // other cases cannot say for themselves.
  twoLanesCase();
  corpusFiles(corpus);
  pairedCorpus(benchCorpus);
  fixedBoundsCase();
  // THE FOUR CROSS-SCHEMA CASES ARE RETIRED (§5.6): each compiled a plan from
  // a layout THE FILE carried and read FORWARD, and under §5 a fixed table
  // reads BACKWARD only — a peer the lineage does not hold is `layout_newer`
  // before a plan exists. The coverage moved to the lineage harness, where the
  // older generation is a LOCKED ENTRY of the newer build rather than a
  // stranger on the wire.
  const harness =
      'the coverage is owed by internal/codegen/darttable/'
      'fixedversioning_test.go, both columns of every row of '
      'docs/FIXED-FORM-VERSIONING-TESTS.md, run by `make tables-dart-versioning`';
  retire('fxCase', 'the plan path, FX1 <-> FX2 both ways: $harness');
  retire('fuCase', 'text under an arm on a compiled plan: $harness');
  retire('vCase', 'a variant, an arm and a keyed slot mid-list: $harness');
  retire('pCase', 'a value against ?T across two schemas: $harness');
  absentOptionalCase();
  unionSlackCase();
  boundsCase();
  textLengthClampCase();
  rangedScalarClampCase();
  hostileBytesCase();
  writeSlackCase();
  readSlackUnspecified();
  negativeControl();
  // F4: the sample lives behind the corpus reads, and it does not need them —
  // it is the reader's OWN header and a length that lies.
  layoutTruncatedCase();
  // AND §1.1'S SEVEN RULES NO LONGER RUN AT READ TIME (§5.6): a layout arriving
  // on the wire is never walked, so a malformation under a KNOWN hash is ONE
  // name, `layout_malformed`. The coverage is owed by the LOCK's validation of
  // what it records — and `hash_known_bytes_differ` in the lineage harness
  // holds the read side to that single name.
  retire(
    'layoutValidation',
    'the run-time walk of a stranger layout: the seven rules move to the LOCK '
        'validation of what it records, and the read side is one name, '
        'layout_malformed, asserted by hash_known_bytes_differ in '
        'internal/codegen/darttable/fixedversioning_test.go',
  );

  if (failed) {
    print('FAILED');
    exitCode = 1;
    return;
  }
  print(
    'tables Dart fixed form: the LAYOUT and its hash are the C++ reference\'s byte for '
    'byte, all eight of its files read to the values it states and write back IDENTICAL, '
    'a fixed table reads BACKWARD through the lineage the build laid down, and every '
    'refusal is by name',
  );
  print(
    '$retired retired by §5.6, each named at its call site and still in the tree',
  );
  print('OK');
}
