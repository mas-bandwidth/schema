// THE DART TABLES SOAK (docs/PORTING.md I9: the soak is gated on the
// allocation count, with drift beside it as the weaker instrument).
//
// A FUNCTIONAL test: it runs for SOAK_SECONDS of real time and is reached from
// a make target, never from a unit test. It loops, over the rows the
// allocation gate gates (test/dart-tables/gcgate.dart), Load, the reads,
// Measure and Save into caller-owned storage that is reused every iteration,
// and holds two things for the whole run:
//
//   1. CORRECTNESS UNDER REUSE (the old Dart soak's half): every record is
//      round-tripped through the wire and the saved bytes are compared with
//      the bytes loaded, and the block and cook walks must reproduce the
//      checksum of the first pass, so reused storage never leaks state from
//      one iteration into the next.
//   2. THE SCAVENGE COUNT, SAMPLED. The Dart VM exposes no in-process counter,
//      so (as the allocation gate does) the collector's own `--verbose_gc`
//      line on stderr is the instrument, with the semi-space pinned to 1 MB.
//      A count read only at the end proves nothing about drift, so the driver
//      samples it every second after the warmup and FAILS if it ever moves.
//      Resident memory is sampled beside it and its drift is reported as the
//      weaker instrument (it is silent on a path that allocates and frees).
//
// Two modes:
//   worker  `soak work <seconds>`: allocates everything up front, warms for
//           two seconds, writes `soak: steady phase begins` to stderr, loops
//           until <seconds> have passed, then writes `soak: steady phase ends`.
//           SCHEMA_DART_SOAK_PLANT=1 (read here, never in generated code)
//           plants one small heap allocation per iteration: the negative
//           control.
//   driver  `soak drive <aot-bin> <seconds>`: refuses off the pinned SDK (the
//           gate's `pinnedDartSdk` and flags, and its escape-hatch names),
//           starts the worker under the gate's VM flags, samples count and
//           resident memory once a second, and judges both.

import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import '../../build/dart-fixed/examples/KeyedFixed.dart' as keyed;
import '../../build/dart-fixed/examples/PackFixed.dart' as pack;
import '../../build/dart-fixed/examples/TabledemoFixed.dart' as demo;
import '../../build/dart-fixed/fx1/FX1Fixed.dart' as fx1;
import '../../build/dart-fixed/fx1/Tblfx1Fixed.dart' as fx1home;
import '../../build/tables-generated-dart/block/PaddedBlock.dart' as pblk;
import '../../build/tables-generated-dart/block/RenderBlock.dart' as rblk;
import '../../build/tables-generated-dart/pointers/GraphCook.dart' as ck;
import '../../build/tables-generated-dart/pointers/GraphdemoCook.dart'
    show TableCookRef;
import 'gcgate.dart' show pinnedDartSdk, vmFlagsCsv;

const String beginMarker = 'soak: steady phase begins';
const String endMarker = 'soak: steady phase ends';

// Resident memory may breathe; a leak is steady growth. Eight MiB of growth
// between the first sample and the last is the weaker instrument's ceiling.
const int driftCeilingKb = 8 * 1024;

const int poolSize = 64;
final List<Object?> plantRing = List<Object?>.filled(64, null);
int checksum = 0;

void _fail(String msg) {
  stderr.writeln('soak: $msg');
  exit(2);
}

bool _sameBytes(Uint8List a, Uint8List b, int n) {
  for (var i = 0; i < n; i++) {
    if (a[i] != b[i]) {
      return false;
    }
  }
  return true;
}

void _worker(int seconds) {
  final fx1Bytes = File('build/fixedform-corpus/fx1.bin').readAsBytesSync();
  final keyedBytes = File('build/fixedform-corpus/keyed.bin').readAsBytesSync();
  final packBytes = File('build/fixedform-corpus/pack.bin').readAsBytesSync();

  final fx1Plan = fx1.fxRootFixedNewPlan();
  final keyedPlan = keyed.keyedConfigFixedNewPlan();
  final packPlan = pack.packConfigFixedNewPlan();
  final fx1Report = fx1home.TableFixedReport();
  final demoReport = demo.TableFixedReport();

  final fx1Values = List<fx1home.FxRoot>.generate(
    poolSize,
    (_) => fx1home.FxRoot(),
    growable: false,
  );
  final keyedValues = List<demo.KeyedConfig>.generate(
    poolSize,
    (_) => demo.KeyedConfig(),
    growable: false,
  );
  final packValues = List<demo.PackConfig>.generate(
    poolSize,
    (_) => demo.PackConfig(),
    growable: false,
  );

  final n1 = fx1.fxRootFixedLoad(
    fx1Values,
    poolSize,
    fx1Bytes,
    fx1Bytes.length,
    fx1Plan,
    fx1Report,
  );
  final n2 = keyed.keyedConfigFixedLoad(
    keyedValues,
    poolSize,
    keyedBytes,
    keyedBytes.length,
    keyedPlan,
    demoReport,
  );
  final n3 = pack.packConfigFixedLoad(
    packValues,
    poolSize,
    packBytes,
    packBytes.length,
    packPlan,
    demoReport,
  );
  if (n1 <= 0 || n2 <= 0 || n3 <= 0) {
    _fail('fixed-form seed load failed ($n1, $n2, $n3)');
  }
  for (var k = n1; k < poolSize; k++) {
    fx1Values[k] = fx1Values[k % n1];
  }
  for (var k = n2; k < poolSize; k++) {
    keyedValues[k] = keyedValues[k % n2];
  }
  for (var k = n3; k < poolSize; k++) {
    packValues[k] = packValues[k % n3];
  }

  // The batch images: what Save writes for the seeded records. Every
  // iteration loads these back into the reused records and must save them to
  // exactly these bytes again.
  final fx1Save = Uint8List(fx1.fxRootFixedMeasure(poolSize));
  final keyedSave = Uint8List(keyed.keyedConfigFixedMeasure(poolSize));
  final packSave = Uint8List(pack.packConfigFixedMeasure(poolSize));
  final fx1Len = fx1.fxRootFixedSave(fx1Values, poolSize, fx1Save);
  final keyedLen = keyed.keyedConfigFixedSave(keyedValues, poolSize, keyedSave);
  final packLen = pack.packConfigFixedSave(packValues, poolSize, packSave);
  if (fx1Len <= 0 || keyedLen <= 0 || packLen <= 0) {
    _fail('fixed-form batch save failed ($fx1Len, $keyedLen, $packLen)');
  }
  final fx1Batch = Uint8List.fromList(fx1Save);
  final keyedBatch = Uint8List.fromList(keyedSave);
  final packBatch = Uint8List.fromList(packSave);
  for (var k = 0; k < poolSize; k++) {
    fx1Values[k] = fx1home.FxRoot();
    keyedValues[k] = demo.KeyedConfig();
    packValues[k] = demo.PackConfig();
  }

  final renderBytes = File('testdata/wire/tables/block_render.bin')
      .readAsBytesSync();
  final paddedBytes = File('testdata/wire/tables/block_padded.bin')
      .readAsBytesSync();
  final rb = rblk.RenderFrameBlock.open(renderBytes, 0, renderBytes.length);
  final pb = pblk.PaddedFrameBlock.open(paddedBytes, 0, paddedBytes.length);
  if (rb == null || pb == null) {
    _fail('block fixtures did not open');
  }
  final shipCursor = rb!.shipsCursor();
  final rowCursor = pb!.rowsCursor();
  final shipsCount = rb.shipsCount;
  final rowsCount = pb.rowsCount;

  final sceneBytes = File('build/cook-fuzz/Scene.cook').readAsBytesSync();
  final treeBytes = File('build/cook-fuzz/TreeNode.cook').readAsBytesSync();
  final sc = ck.SceneCook.open(sceneBytes, 0, sceneBytes.length);
  final tc = ck.TreeNodeCook.open(treeBytes, 0, treeBytes.length);
  if (sc == null || tc == null) {
    _fail('cook fixtures did not open');
  }
  final sceneCursor = sc!.cursor();
  final treeCursor = tc!.cursor();
  final listCursor = ck.ListNodeRow(sc.view, sc.region);

  final plant = Platform.environment['SCHEMA_DART_SOAK_PLANT'] == '1';

  // 0 means every check held; otherwise the first one that did not.
  // 1 load count, 2 measure, 3 save length, 4 bytes differ.
  var walkSum = 0;

  int iterate(int i) {
    final l1 = fx1.fxRootFixedLoad(
      fx1Values,
      poolSize,
      fx1Batch,
      fx1Batch.length,
      fx1Plan,
      fx1Report,
    );
    final l2 = keyed.keyedConfigFixedLoad(
      keyedValues,
      poolSize,
      keyedBatch,
      keyedBatch.length,
      keyedPlan,
      demoReport,
    );
    final l3 = pack.packConfigFixedLoad(
      packValues,
      poolSize,
      packBatch,
      packBatch.length,
      packPlan,
      demoReport,
    );
    if (l1 != poolSize || l2 != poolSize || l3 != poolSize) {
      return 1;
    }
    var sum = 0;
    for (var k = 0; k < poolSize; k++) {
      sum =
          (sum +
              fx1Values[k].keep +
              keyedValues[k].teams[0].spawnCount +
              packValues[k].version) &
          0x3fffffff;
    }
    final m1 = fx1.fxRootFixedMeasure(poolSize);
    final m2 = keyed.keyedConfigFixedMeasure(poolSize);
    final m3 = pack.packConfigFixedMeasure(poolSize);
    if (m1 != fx1Save.length ||
        m2 != keyedSave.length ||
        m3 != packSave.length) {
      return 2;
    }
    final w1 = fx1.fxRootFixedSave(fx1Values, poolSize, fx1Save);
    final w2 = keyed.keyedConfigFixedSave(keyedValues, poolSize, keyedSave);
    final w3 = pack.packConfigFixedSave(packValues, poolSize, packSave);
    if (w1 != fx1Len || w2 != keyedLen || w3 != packLen) {
      return 3;
    }
    if (!_sameBytes(fx1Save, fx1Batch, w1) ||
        !_sameBytes(keyedSave, keyedBatch, w2) ||
        !_sameBytes(packSave, packBatch, w3)) {
      return 4;
    }
    for (var r = 0; r < shipsCount; r++) {
      rb.shipsAt(r, shipCursor);
      sum =
          (sum +
              shipCursor.objectId +
              shipCursor.team +
              shipCursor.thrust.toInt()) &
          0x3fffffff;
    }
    for (var r = 0; r < rowsCount; r++) {
      pb.rowsAt(r, rowCursor);
      sum =
          (sum + rowCursor.id + rowCursor.tag + rowCursor.value.toInt()) &
          0x3fffffff;
    }
    sc.root(sceneCursor);
    sum = (sum + sceneCursor.version + sceneCursor.nameLength) & 0x3fffffff;
    var target = sc.at(sceneCursor.at + 40, ck.ListNodeRow.rowSize);
    var steps = 0;
    while (steps < 16 &&
        target != TableCookRef.none &&
        target != TableCookRef.outside) {
      listCursor.at = target;
      sum = (sum + listCursor.value + listCursor.nameLength) & 0x3fffffff;
      target = sc.at(listCursor.at + 24, ck.ListNodeRow.rowSize);
      steps++;
    }
    tc.root(treeCursor);
    sum = (sum + treeCursor.labelLength) & 0x3fffffff;
    if (walkSum == 0) {
      walkSum = sum == 0 ? 1 : sum;
    } else if (sum != walkSum) {
      return 5;
    }
    if (plant) {
      // Negative control: one small heap allocation per iteration.
      final planted = List<int>.filled(64, i);
      plantRing[i & 63] = planted;
      checksum = (checksum + planted[0]) & 0x3fffffff;
    }
    checksum = (checksum + sum) & 0x3fffffff;
    return 0;
  }

  final stopwatch = Stopwatch()..start();
  var i = 0;
  var bad = 0;
  // Warmup: two seconds of real time, so one-time setup (lazy descriptors,
  // JIT-less first-touch, the first semi-space fill) sits before the marker.
  while (stopwatch.elapsedMilliseconds < 2000 && bad == 0) {
    bad = iterate(i++);
  }
  if (bad != 0) {
    _fail('correctness check $bad failed in warmup at iteration $i');
  }
  stderr.writeln(beginMarker);
  stopwatch
    ..reset()
    ..start();
  final limit = seconds * 1000;
  var iterations = 0;
  while (stopwatch.elapsedMilliseconds < limit) {
    bad = iterate(i++);
    if (bad != 0) {
      _fail('correctness check $bad failed at iteration $i');
    }
    iterations++;
  }
  stderr.writeln(endMarker);
  stdout.writeln(
    'soak work: seconds=$seconds iterations=$iterations checksum=$checksum plant=${plant ? 1 : 0}',
  );
}

Future<int> _residentKb(int pid) async {
  final res = await Process.run('ps', <String>['-o', 'rss=', '-p', '$pid']);
  return int.tryParse('${res.stdout}'.trim()) ?? -1;
}

Future<void> _drive(List<String> args) async {
  final aotBin = args[1];
  final seconds = int.parse(args[2]);

  final runningSdk =
      Platform.environment['SCHEMA_DART_ALLOC_SDK_VERSION'] ??
      Platform.version.split(' ').first;
  final anySdk = Platform.environment['SCHEMA_DART_ALLOC_ANY_SDK'] == '1';
  if (runningSdk != pinnedDartSdk && !anySdk) {
    stdout.writeln(
      'FAILED: soak certification requires Dart SDK $pinnedDartSdk (pinned in make/dart.mk), running $runningSdk; set SCHEMA_DART_ALLOC_ANY_SDK=1 to report observations without certifying',
    );
    stdout.writeln('NOT CERTIFIED');
    exit(1);
  }

  final env = Map<String, String>.from(Platform.environment);
  env['DART_VM_OPTIONS'] = vmFlagsCsv;
  final proc = await Process.start(aotBin, <String>[
    'work',
    '$seconds',
  ], environment: env);

  var inside = false;
  var scavenges = 0;
  var workerErr = '';
  proc.stderr.transform(utf8.decoder).transform(const LineSplitter()).listen((
    line,
  ) {
    if (line.contains(beginMarker)) {
      inside = true;
    } else if (line.contains(endMarker)) {
      inside = false;
    } else if (inside && line.contains('Scavenge')) {
      scavenges++;
    } else if (line.startsWith('soak:')) {
      workerErr += '$line\n';
    }
  });
  final out = proc.stdout.transform(utf8.decoder).join();

  stdout.writeln(
    'tables Dart soak (SDK $runningSdk, pinned $pinnedDartSdk, $seconds s):',
  );
  final counts = <int>[];
  final rss = <int>[];
  var done = false;
  unawaited(proc.exitCode.then((_) => done = true));
  var started = false;
  while (!done) {
    await Future<void>.delayed(const Duration(seconds: 1));
    if (done) {
      break;
    }
    if (!inside) {
      if (started) {
        break;
      }
      continue;
    }
    started = true;
    final kb = await _residentKb(proc.pid);
    counts.add(scavenges);
    rss.add(kb);
    stdout.writeln(
      '  sample ${counts.length.toString().padLeft(4)}: scavenges=$scavenges rss=$kb KB',
    );
  }
  final code = await proc.exitCode;
  final workerOut = (await out).trim();
  if (code != 0) {
    stdout.writeln('FAILED: soak worker exited $code\n$workerErr');
    exit(1);
  }
  stdout.writeln('  $workerOut');
  if (counts.isEmpty) {
    stdout.writeln('FAILED: no samples were taken (run too short?)');
    exit(1);
  }

  final drift = rss.last - rss.first;
  final driftOk = rss.first < 0 || drift <= driftCeilingKb;
  final countOk = scavenges == 0;
  stdout.writeln(
    '  drift:   rss ${rss.first} KB -> ${rss.last} KB (${drift >= 0 ? "+" : ""}$drift KB, ceiling +$driftCeilingKb KB) [${driftOk ? "PASS" : "FAIL"}] (weaker instrument)',
  );
  stdout.writeln(
    '  count:   $scavenges scavenges after warmup, ${counts.length} samples, ceiling 0 [${countOk ? "PASS" : "FAIL"}]',
  );
  if (anySdk && runningSdk != pinnedDartSdk) {
    stdout.writeln(
      'Dart soak observation mode (SCHEMA_DART_ALLOC_ANY_SDK=1 on SDK $runningSdk, pinned $pinnedDartSdk): reporting observations only, NOT CERTIFIED',
    );
  }
  if (!countOk || !driftOk) {
    stdout.writeln('FAILED: the soak moved the ${countOk ? "drift" : "count"}');
    exit(1);
  }
  stdout.writeln(
    'tables Dart soak: scavenge count flat at 0 after warmup over $seconds s on Dart $runningSdk',
  );
}

Future<void> main(List<String> args) async {
  if (args.length == 3 && args[0] == 'drive') {
    await _drive(args);
  } else if (args.length == 2 && args[0] == 'work') {
    _worker(int.parse(args[1]));
  } else {
    stderr.writeln(
      'usage: soak work <seconds> | soak drive <aot-bin> <seconds>',
    );
    exit(2);
  }
}
