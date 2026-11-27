// THE DART CELL dart/W13: "layout+hash == C++ reference"
// (docs/roadmap.sexp:3865, matrix schema#876 row W13, audit schema#898).
//
// THE LAW. docs/SPEC-TABLES.md:267 — "Section 3.4's FIXED form is carried by
// the C++ reference, by Dart, and by JavaScript, whose layout/block, hash and
// record bytes are held against the reference's own over the paired bench's
// corpus, the seven reference files (`tables-dart-fixed-form`)". The bill's
// own words, docs/FIXED-FORM-ALGORITHM.md:1670-1673: "a Go test comparing the
// emitted layout arrays and hash constants against the reference entry ...
// **Every port owes that test**". A fixed-table record is the writer's LAYOUT
// hash and then the values; two backends whose layouts differ anywhere are two
// backends that never read each other's records — silently, as `no_layout`
// (docs/FIXED-FORM-ALGORITHM.md §5.3, §5.9 #47: every hash a runtime holds is
// a HANDED CONSTANT, none computed from layout bytes).
//
// WHAT THIS ASSERTS, AND NOTHING ELSE. The Dart leg's emitted fixed-form
// LAYOUT BYTES and HASH CONSTANTS for the versioning fixture V1 (fixed tables
// `Cell` and `Cfg`) are byte-for-byte the C++ reference's, held as the handed
// constants the reference emitter produces for the SAME unit.
//
// THE PRODUCTION PATH, from the entrypoint to the effect:
//
//   bin/schema generate --lang dart --out <d> test/tables/V1.schema
//     └─ internal/codegen/darttable/fixeddart.go (the Dart fixed-form emitter)
//         └─ <d>/v1/V1Fixed.dart: cellFixedLayout / cellFixedHash,
//            cfgFixedLayout / cfgFixedHash — the layout bytes and the
//            fnv1a64 the reader hands to its framing and its record hashes
//            (V1Fixed.dart:196-209, 819-832 use them as the prefill's hash
//            and the frame's layout)
//
// The REFERENCE side of the equality is the C++ emitter, internal/codegen/
// cpptable/fixedform.go: `./bin/schema generate --lang cpp --out <d>
// test/tables/V1.schema` emits `CellFixedLayout[]`/`CellFixedHash` and
// `CfgFixedLayout[]`/`CfgFixedHash` in <d>/V1Table.h. Those bytes are also
// what the reference writes into build/fixedform-corpus (SPEC-TABLES.md:267),
// which `make tables-dart-fixed-form` round-trips for the corpus roots. V1 is
// not a corpus root, so this row reads the reference emitter's header for it
// (below).
//
// The generated tree is the driver's own: test/conformance/dart/main.dart
// imports build/tables-generated-dart/... relative from the repo root, and
// make/dart.mk's build/tables-generated-dart/.stamp generates v1 from the
// same test/tables/V1.schema. This file edits nothing and depends on no other
// rows/ file; it is run alone from the repo root:
//
//   dart run test/conformance/dart/rows/W13.dart
//
// Exit 0 green / exit 1 red, one printed line per assertion.

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/v1/V1Fixed.dart'
    show cellFixedHash, cellFixedLayout, cfgFixedHash, cfgFixedLayout;

// ---- the C++ reference's handed constants for the SAME unit, V1.schema ----
//
// READ FROM THE REFERENCE'S OWN EMITTED HEADER, never copied into this file:
// make/conformance-rows.mk builds build/tables-generated/.stamp (the C++
// emitter over test/tables/V1.schema, among the rest) before the dart rows run,
// and this row parses `CellFixedHash`, `CellFixedLayout[]`, `CfgFixedHash` and
// `CfgFixedLayout[]` out of build/tables-generated/v1/V1Table.h. A hand copy of
// them went stale at WireLaw 3 (docs/FIXED-FORM-ALGORITHM.md §5.2, the
// prospective v2 definitions digest), when every hash moved and this row held
// Dart to the RETIRED reference numbers; reading the header makes the
// reference the live one. A header that is missing, or a constant it does not
// carry exactly once, is RED by name — never a skip.

const referenceHeader = 'build/tables-generated/v1/V1Table.h';

String readReferenceHeader() {
  final f = File(referenceHeader);
  if (!f.existsSync()) {
    print('FAIL: $referenceHeader is missing — make build/tables-generated/.stamp '
        '(a prerequisite of conformance-rows-dart) emits it');
    exit(1);
  }
  return f.readAsStringSync();
}

// `constexpr uint64_t <name> = 0x...ull;`, exactly once
int referenceHash(String header, String name) {
  final re = RegExp('constexpr uint64_t ${RegExp.escape(name)} = 0x([0-9a-fA-F]{1,16})ull;');
  final hits = re.allMatches(header).toList();
  if (hits.length != 1) {
    print('FAIL: $referenceHeader carries $name ${hits.length} times, want 1');
    exit(1);
  }
  return BigInt.parse(hits.single.group(1)!, radix: 16).toSigned(64).toInt();
}

// `constexpr uint8_t <name>[] = { 0x.., ... };`, exactly once
List<int> referenceLayout(String header, String name) {
  final re = RegExp('constexpr uint8_t ${RegExp.escape(name)}\\[\\] = \\{([^}]*)\\};');
  final hits = re.allMatches(header).toList();
  if (hits.length != 1) {
    print('FAIL: $referenceHeader carries $name[] ${hits.length} times, want 1');
    exit(1);
  }
  final bytes = <int>[];
  for (final m in RegExp(r'0x([0-9a-fA-F]{2})').allMatches(hits.single.group(1)!)) {
    bytes.add(int.parse(m.group(1)!, radix: 16));
  }
  if (bytes.isEmpty) {
    print('FAIL: $referenceHeader carries an empty $name[]');
    exit(1);
  }
  return bytes;
}

var failed = false;

void check(bool ok, String what) {
  print('${ok ? "PASS" : "FAIL"}: $what');
  if (!ok) {
    failed = true;
  }
}

// a Dart int is a full signed 64-bit value, so a hash above 2^63 renders
// negative; its BITS are the fact, so spell it as the unsigned 64-bit pattern
String hashText(int v) =>
    BigInt.from(v).toUnsigned(64).toRadixString(16).padLeft(16, '0');

// The layout is byte-for-byte the reference's; a mismatch is named at the
// FIRST differing byte, because a layout that agrees with the reference
// everywhere but one byte is a layout nobody can interoperate with.
void sameLayout(Uint8List got, List<int> want, String what) {
  if (got.length != want.length) {
    check(
      false,
      '$what layout: ${got.length} bytes, the C++ reference wrote ${want.length}',
    );
    return;
  }
  var first = -1;
  for (var i = 0; i < want.length; i++) {
    if (got[i] != want[i]) {
      first = i;
      break;
    }
  }
  check(
    first < 0,
    first < 0
        ? '$what layout: ${want.length} bytes, byte-for-byte the C++ reference'
        : '$what layout: first byte differing from the C++ reference at $first '
              '(dart 0x${got[first].toRadixString(16)}, cpp 0x${want[first].toRadixString(16)})',
  );
}

void main() {
  final header = readReferenceHeader();
  final referenceCellHash = referenceHash(header, 'CellFixedHash');
  final referenceCellLayout = referenceLayout(header, 'CellFixedLayout');
  final referenceCfgHash = referenceHash(header, 'CfgFixedHash');
  final referenceCfgLayout = referenceLayout(header, 'CfgFixedLayout');

  // the generated Dart's constants, from the SAME tree the conformance driver
  // reads (build/tables-generated-dart/v1/V1Fixed.dart)
  sameLayout(cellFixedLayout, referenceCellLayout, 'Cell');
  check(
    cellFixedHash == referenceCellHash,
    'Cell hash: dart 0x${hashText(cellFixedHash)}, '
    'C++ reference 0x${hashText(referenceCellHash)}',
  );
  sameLayout(cfgFixedLayout, referenceCfgLayout, 'Cfg');
  check(
    cfgFixedHash == referenceCfgHash,
    'Cfg hash: dart 0x${hashText(cfgFixedHash)}, '
    'C++ reference 0x${hashText(referenceCfgHash)}',
  );
  print(failed ? 'RED: layout+hash drifted from the C++ reference' : 'GREEN');
  exit(failed ? 1 : 0);
}
