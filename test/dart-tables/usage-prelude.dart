// THE HEAD OF THE DOCUMENTED-SURFACE GATE (`make tables-dart-usage`).
//
// docs/USAGE.md's Dart table examples are appended to this file VERBATIM —
// every ```dart block of the page's table section, inside one main() — and the
// result is analyzed and run over the corpus, so the page goes red with the
// code. This prelude is what the page's prose assumes into scope: the two
// imports the examples name, the wire bytes "another build wrote", the records
// the hot loop walks, and the check at the end that the example did what the
// page says it does.
import 'dart:io';
import 'dart:typed_data';

import '../../build/tables-generated-dart/examples/TablesTable.dart';
// the page's blocks name TableReport and TableReader; the prelude alone does not
// ignore: unused_import
import '../../build/tables-generated-dart/examples/TabledemoTable.dart';

// bytes another build wrote: the conformance corpus's own RootConfig golden
final Uint8List wire = File('testdata/wire/tables/root_full.bin')
    .readAsBytesSync();

// what a hot loop walks: each record's bytes
final class Record {
  final Uint8List bytes;
  Record(this.bytes);
}

final List<Record> records = [
  for (final name in ['root_full', 'root_default'])
    Record(File('testdata/wire/tables/$name.bin').readAsBytesSync()),
];

void usageHolds(RootConfig config, Uint8List out) {
  // the loop's last record was root_default, so config now holds defaults
  if (config.measure() != records.last.bytes.length) {
    throw StateError('the hot loop did not leave the last record in config');
  }
  // and the bytes written back before the loop are the golden's
  if (out.length != wire.length) {
    throw StateError(
      'save wrote ${out.length} bytes for a ${wire.length} golden',
    );
  }
  for (var i = 0; i < wire.length; i++) {
    if (out[i] != wire[i]) {
      throw StateError('byte $i of the re-saved wire differs from the golden');
    }
  }
  stdout.writeln(
    'usage: the documented Dart surface runs, and reproduces the golden',
  );
}
