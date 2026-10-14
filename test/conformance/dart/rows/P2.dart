// test/conformance/dart/rows/P2.dart — the dart leg of the matrix cell
// P2, "write-read-write byte-identical" (docs/roadmap.sexp:3869; audit
// schema#898, matrix schema#876; row group "interoperability: Shared byte
// oracle and round-trip conformance").
//
// THE LAW. docs/FIXED-FORM-ALGORITHM.md:1682, the fixed form's proof table:
//
//   "Read a file and save it back; the bytes must be identical — a byte a
//    port encodes differently is a byte that does not come back."
//
// and docs/SPEC-TABLES.md §3.3's round-trip paragraph (docs/SPEC-TABLES.md:6120):
//
//   "The round trip across the forms. ... Red if one byte differs in either
//    direction, which is the negative control on every rule here that says
//    the VALUE does not move."
//
// THE PRODUCTION PATH. The fixed form's generated Dart: this file calls the
// SAME emitter output the conformance driver reaches the generated tables
// through — build/tables-generated-dart/, laid down by
// `make build/conformance-dart` — at its fixed-form half:
//
//   P1Fixed.dart: chainFixedMeasure -> chainFixedSave -> chainFixedLoad
//                 -> chainFixedSave again
//   Tblp1Fixed.dart: the Chain/Link value classes, chainFixedWriteBody and
//                    linkFixedDecode, the bytes' stores and loads.
//
// chainFixedSave is the production entrypoint: the write path a caller holds.
// The test drives Write -> Read -> Write over a real value and requires the
// two writes to be byte-identical, then requires the decoded values to be the
// values written, so the identity is not the identity of two empty buffers.
//
// THE VECTOR. P1 is the pre-pointer side of the pointer evolution pair: a
// string(16) and a nested fixed table Link { int32 value | min = 0, max =
// 1000; string(8) tag }. The vector is CONSTRUCTED here — the tree carries no
// fixed-form bytes for this cell under testdata/conformance/tables — and it
// exercises the two slack rules the round trip is about: a name at its exact
// bound (no slack) beside one short of it (slack that must come back zero),
// and a tag present beside one empty, across two records so the record stride
// is exercised too.
//
// RED FIRST. This file did not exist at base 205a85684bc0, so the RUN command
// fails there; at the head it passes. A control that moves one value byte is
// recorded in RESULT.md: it turns the round trip red, because the writer and
// the reader disagree on the value.
//
// Run from the repository root:  dart run test/conformance/dart/rows/P2.dart

import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/p1/P1Fixed.dart' as p1;
import '../../../../build/tables-generated-dart/p1/Tblp1Fixed.dart' as p1home;

var failed = false;

void check(bool ok, String what) {
  stdout.writeln('${ok ? 'ok' : 'FAIL'} $what');
  if (!ok) {
    failed = true;
  }
}

// THE BYTE COMPARISON the law is: the first difference is NAMED, not merely
// counted, because "one byte differs" is the whole red condition.
bool sameBytes(Uint8List got, Uint8List want, String what) {
  if (got.length != want.length) {
    check(
      false,
      '$what: ${got.length} bytes, the first write was '
      '${want.length}',
    );
    return false;
  }
  for (var i = 0; i < want.length; i++) {
    if (got[i] != want[i]) {
      check(
        false,
        '$what: first byte differing at $i '
        '(second write ${got[i]}, first write ${want[i]})',
      );
      return false;
    }
  }
  return true;
}

String text(Uint8List buffer, int length) =>
    String.fromCharCodes(buffer.sublist(0, length));

void putText(Uint8List buffer, String value) {
  buffer.setRange(0, value.length, value.codeUnits);
}

// THE TWO RECORDS. Record 0 spends the string(16) to its exact bound and
// carries link.value = 7 and a non-empty tag; record 1 is short of the bound
// and carries the declared default and an empty tag, so the round trip lands
// both the value-bearing bytes and the zeroed slack.
List<p1home.Chain> makeValues() {
  final full = p1home.Chain();
  putText(full.name, 'write-read-write'); // 16 bytes, the bound exactly
  full.nameLength = 16;
  full.link.value = 7;
  putText(full.link.tag, 'p2-row');
  full.link.tagLength = 6;

  final short = p1home.Chain();
  putText(short.name, 'p2');
  short.nameLength = 2;
  short.link.value = 0; // the declared default
  short.link.tagLength = 0; // an empty tag, so the 8 bytes are slack

  return <p1home.Chain>[full, short];
}

Uint8List write(List<p1home.Chain> values) {
  final out = Uint8List(p1.chainFixedMeasure(values.length));
  final n = p1.chainFixedSave(values, values.length, out);
  check(
    n == out.length,
    'write: chainFixedSave answers the measured ${out.length} bytes (got $n)',
  );
  check(
    out[0] == p1home.tableFixedForm,
    'write: the file opens with form byte 3',
  );
  final view = ByteData.sublistView(out);
  check(
    view.getUint64(p1home.TableFixedLimits.hashAt, Endian.little) ==
        p1.chainFixedHash,
    'write: the header names this build\'s layout hash at offset 8',
  );
  return out;
}

List<p1home.Chain> read(Uint8List bytes, int count) {
  final values = List<p1home.Chain>.generate(count, (_) => p1home.Chain());
  final report = p1home.TableFixedReport();
  final n = p1.chainFixedLoad(
    values,
    count,
    bytes,
    bytes.length,
    p1.chainFixedNewPlan(),
    report,
  );
  check(n == count, 'read: chainFixedLoad answers $count records (got $n)');
  check(
    !report.malformed && report.refused == 0,
    'read: a clean read moves no counter',
  );
  return values;
}

// THE ROUND TRIP, in one direction and then back: the values are written, the
// bytes are read into fresh values, and those values are written again. The
// second write must reproduce the first byte for byte, and the values read
// must be the values written — so a reader that silently drops a byte or a
// writer that reorders one is caught even where the two agree with each other.
void p2RoundTrip() {
  final written = makeValues();
  final first = write(written);

  final back = read(first, written.length);

  void sameValue(
    String what,
    Uint8List gotBytes,
    int gotLength,
    Uint8List wantBytes,
    int wantLength,
  ) {
    check(gotLength == wantLength, '$what: the length moved');
    check(
      text(gotBytes, gotLength) == text(wantBytes, wantLength),
      '$what: the text moved',
    );
  }

  sameValue(
    'record 0 name',
    back[0].name,
    back[0].nameLength,
    written[0].name,
    written[0].nameLength,
  );
  check(back[0].link.value == 7, 'record 0 link.value did not move');
  sameValue(
    'record 0 link.tag',
    back[0].link.tag,
    back[0].link.tagLength,
    written[0].link.tag,
    written[0].link.tagLength,
  );
  sameValue(
    'record 1 name',
    back[1].name,
    back[1].nameLength,
    written[1].name,
    written[1].nameLength,
  );
  check(back[1].link.value == 0, 'record 1 link.value did not move');
  check(back[1].link.tagLength == 0, 'record 1 link.tag stayed empty');
  // THE SLACK BEHIND A SHORT NAME IS ZERO AND IS NOT A VALUE: the reader's
  // storage behind nameLength must be zeros, which is what the fresh Chain
  // held and what the wire's template wrote.
  var slackZero = true;
  for (var i = back[1].nameLength; i < back[1].name.length; i++) {
    slackZero = slackZero && back[1].name[i] == 0;
  }
  check(slackZero, 'record 1: the name slack is the template\'s zeros');

  final second = write(back);
  sameBytes(second, first, 'P2 write-read-write byte-identical');
}

void main() {
  p2RoundTrip();
  if (failed) {
    stdout.writeln('P2 dart: FAILED');
    exit(1);
  }
  stdout.writeln('P2 dart: write-read-write byte-identical');
}
