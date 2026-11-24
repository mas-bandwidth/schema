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

import '../../../build/packet-write-cfloat/dart/PacketCFloat.dart';

var failed = 0;

void check(bool ok, String what) {
  if (!ok) {
    print('FAILED: $what');
    failed++;
  }
}

// The case being run and the failures before it: answer() prints its
// "case <name> ok|FAILED: ..." line, the answer tools/casegate reads.
String? pending;
var pendingFailed = 0;

void answer() {
  final name = pending;
  if (name != null) {
    print(
      'case $name ${failed == pendingFailed ? 'ok' : 'FAILED: see the FAILED line above'}',
    );
  }
  pending = null;
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
  var ran = 0;
  for (final line in lines) {
    final text = line.trim();
    if (text.isEmpty || text.startsWith('#')) {
      continue;
    }
    ran++;
    final fields = text.split(RegExp(r'\s+'));
    final name = fields[0];
    answer();
    pending = name;
    pendingFailed = failed;
    final x = parseValue(fields[1]);
    if (fields[2] != 'written' && fields[2] != 'refused') {
      throw StateError(
        'cases: $name: verdict "${fields[2]}" is neither written nor refused',
      );
    }
    final refused = fields[2] == 'refused';
    // [reads-back <v>] [type RatioEndpoint] [wire <hex>] [bits <n>], in pairs
    var readsBack = x;
    var ratio = false;
    // a RatioPair writes the value to both fields
    var pair = false;
    String? wireHex;
    int? wantBits;
    if (fields.length % 2 != 1) {
      throw StateError('cases: $name: a modifier has no operand');
    }
    for (var i = 3; i + 1 < fields.length; i += 2) {
      switch (fields[i]) {
        case 'reads-back':
          readsBack = parseValue(fields[i + 1]);
        case 'type':
          if (fields[i + 1] != 'RatioEndpoint' &&
              fields[i + 1] != 'RatioPair') {
            throw StateError('cases: $name: unknown type ${fields[i + 1]}');
          }
          ratio = true;
          pair = fields[i + 1] == 'RatioPair';
        case 'wire':
          wireHex = fields[i + 1].toLowerCase();
        case 'bits':
          wantBits = int.tryParse(fields[i + 1]);
          if (wantBits == null || wantBits < 0) {
            throw StateError('cases: $name: bits ${fields[i + 1]}');
          }
        default:
          throw StateError('cases: $name: unknown modifier ${fields[i]}');
      }
    }

    final value = CFloat();
    value.x = x;
    final ratioValue = RatioEndpoint();
    ratioValue.ratio = x;
    final pairValue = RatioPair();
    pairValue.first = x;
    pairValue.second = x;
    final view = ByteData(
      pair
          ? ratioPairMaxBytes
          : (ratio ? ratioEndpointMaxBytes : cFloatMaxBytes),
    );

    var fired = false;
    var written = 0;
    try {
      written = pair
          ? writeRatioPair(pairValue, view)
          : (ratio
                ? writeRatioEndpoint(ratioValue, view)
                : writeCFloat(value, view));
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
    if (wireHex != null) {
      final got = [
        for (var i = 0; i < written; i++)
          view.getUint8(i).toRadixString(16).padLeft(2, '0'),
      ].join();
      check(
        got == wireHex,
        'packet-write-cfloat $name: wrote $got, want $wireHex',
      );
    }
    // The writer returns bytes, not bits: a case that pins its bits holds
    // the declared width (the generated <t>MaxBits) to them, and the bytes
    // must read back through exactly that many bits.
    var readBits = written * 8;
    if (wantBits != null) {
      final maxBits = pair
          ? ratioPairMaxBits
          : (ratio ? ratioEndpointMaxBits : cFloatMaxBits);
      check(
        maxBits == wantBits,
        'packet-write-cfloat $name: the declared width is $maxBits bits, want $wantBits',
      );
      check(
        written == (wantBits + 7) >> 3,
        'packet-write-cfloat $name: wrote $written bytes for $wantBits bits',
      );
      readBits = wantBits;
    }
    final back = CFloat();
    final ratioBack = RatioEndpoint();
    final pairBack = RatioPair();
    final read = pair
        ? readRatioPair(pairBack, view, readBits)
        : (ratio
              ? readRatioEndpoint(ratioBack, view, readBits)
              : readCFloat(back, view, readBits));
    check(read, 'packet-write-cfloat $name: the writer bytes read back');
    if (read) {
      check(
        !pair || pairBack.first == pairBack.second,
        'packet-write-cfloat $name: the second field read back ${pairBack.second}, the first ${pairBack.first}',
      );
      final got = pair ? pairBack.first : (ratio ? ratioBack.ratio : back.x);
      check(got == readsBack, 'packet-write-cfloat $name: read back $got');
    }
  }

  answer();
  // A summary; tools/casegate holds every case of the file to an answer. (An
  // AOT build still walks every case; a refused case has no assert to observe.)
  print('packet-write-cfloat Dart: ran $ran, failed $failed');
  if (failed != 0 || ran == 0) {
    exit(1);
  }
}
