// The packet Write's compressed-float value check (SPEC §5.1 row 10,
// PACKET-WIRE-ALGORITHM §2.5), the Dart leg.
//
// Reads the shared cases beside the schema and writes every one of them. A
// refused case must fire the writer's debug assert (the checked twin); a
// written case must write and read back to its quantized value in both twins.
// The release twin trusts the caller by design, so it does not require the
// assert.
//
// Prints OK and exits 0; every failure is one FAILED line, then exit 1.
import 'dart:io';
import 'dart:typed_data';

import '../../../build/packet-write-cfloat/dart/CFloat.dart';

var failed = false;

void check(bool ok, String what) {
  if (!ok) {
    print('FAILED: $what');
    failed = true;
  }
}

final bool assertsEnabled = () {
  var on = false;
  assert(on = true);
  return on;
}();

double parseValue(String text) {
  switch (text) {
    case 'NaN':
      return double.nan;
    case '+Inf':
      return double.infinity;
    case '-Inf':
      return double.negativeInfinity;
    default:
      return double.parse(text);
  }
}

void main(List<String> args) {
  final lines = File(args[0]).readAsLinesSync();
  for (final line in lines) {
    final text = line.trim();
    if (text.isEmpty || text.startsWith('#')) {
      continue;
    }
    final fields = text.split(RegExp(r'\s+'));
    final name = fields[0];
    final x = parseValue(fields[1]);
    final refused = fields[2] == 'refused';
    final readsBack = fields.length > 4 ? parseValue(fields[4]) : x;

    final value = CFloat();
    value.x = x;
    final view = ByteData(cFloatMaxBytes);

    var fired = false;
    var written = 0;
    try {
      written = writeCFloat(value, view);
    } on AssertionError {
      fired = true;
    }

    if (refused) {
      if (assertsEnabled) {
        check(fired, 'packet-write-cfloat $name: off-range value was written');
      }
      continue;
    }

    if (fired) {
      check(false, 'packet-write-cfloat $name: lawful value was refused');
      continue;
    }
    final back = CFloat();
    final read = readCFloat(back, view, written * 8);
    check(read, 'packet-write-cfloat $name: the writer bytes read back');
    if (read) {
      check(
        back.x == readsBack,
        'packet-write-cfloat $name: read back ${back.x}',
      );
    }
  }

  if (failed) {
    exit(1);
  }
  print('packet-write-cfloat Dart: every shared case has its verdict');
}
