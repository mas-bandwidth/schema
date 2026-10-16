// THE DART TABLES SOAK (docs/SPEC-TABLES.md, docs/PORTING.md I9). The C soak
// (test/c-tables/soak_main.c) is the model and the allocation gate
// (test/dart-tables/gcgate.dart) is the instrument.
//
// Worker + sampling driver, mirroring gcgate's two modes:
//   1. Worker (`soak <seconds>`): opens the fixtures, allocates all caller-owned
//      storage up front, warms the path so one-time setup sits before the
//      marker, writes `soak: steady phase begins` to stderr, loops Load, the
//      reads, Measure and Save over the rows the allocation gate gates (fixed
//      load, measure, save, block, cook) for `seconds` of real time, and writes
//      `soak: steady phase ends`.
//   2. Driver (`soak drive <dart-bin> <aot-bin> [seconds]`): enforces the
//      pinned SDK rule (reusing gcgate's pinnedDartSdk and the escape hatch),
//      runs the worker under the AOT executable with the gate's VM flags, reads
//      the scavenge count off its stderr and samples it every second after the
//      warmup, failing if it moves, and reports the worker's resident-memory
//      drift beside it as the weaker instrument that sees retention.
//
// The soak is a FUNCTIONAL test run from a make target with real time and the
// VM; it is never a unit test.

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
import 'gcgate.dart' show vmFlagsCsv, pinnedDartSdk;

int checksum = 0;
final List<Object?> plantRing = List<Object?>.filled(64, null);

bool _sameBytes(Uint8List a, Uint8List b, int length) {
  for (var i = 0; i < length; i++) {
    if (a[i] != b[i]) {
      return false;
    }
  }
  return true;
}

int _childRss(int pid) {
  try {
    final status = File('/proc/$pid/status').readAsStringSync();
    for (final line in const LineSplitter().convert(status)) {
      if (line.startsWith('VmRSS:')) {
        final parts = line.trim().split(RegExp(r'\s+'));
        return parts.length >= 2 ? int.tryParse(parts[1]) ?? -1 : -1;
      }
    }
    return -1;
  } catch (_) {
    return -1;
  }
}

void _runPhase(int seconds) {
  final fx1Bytes = File('build/fixedform-corpus/fx1.bin').readAsBytesSync();
  final keyedBytes = File('build/fixedform-corpus/keyed.bin').readAsBytesSync();
  final packBytes = File('build/fixedform-corpus/pack.bin').readAsBytesSync();

  final fx1Plan = fx1.fxRootFixedNewPlan();
  final keyedPlan = keyed.keyedConfigFixedNewPlan();
  final packPlan = pack.packConfigFixedNewPlan();
  final fx1Report = fx1home.TableFixedReport();
  final demoReport = demo.TableFixedReport();

  const poolSize = 64;
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

  final n1Seed = fx1.fxRootFixedLoad(
    fx1Values,
    poolSize,
    fx1Bytes,
    fx1Bytes.length,
    fx1Plan,
    fx1Report,
  );
  final n2Seed = keyed.keyedConfigFixedLoad(
    keyedValues,
    poolSize,
    keyedBytes,
    keyedBytes.length,
    keyedPlan,
    demoReport,
  );
  final n3Seed = pack.packConfigFixedLoad(
    packValues,
    poolSize,
    packBytes,
    packBytes.length,
    packPlan,
    demoReport,
  );
  if (n1Seed <= 0 || n2Seed <= 0 || n3Seed <= 0) {
    stderr.writeln('soak: fixed-form seed load failed');
    exit(2);
  }

  final fx1SaveBuf = Uint8List(fx1.fxRootFixedMeasure(poolSize));
  final keyedSaveBuf = Uint8List(keyed.keyedConfigFixedMeasure(poolSize));
  final packSaveBuf = Uint8List(pack.packConfigFixedMeasure(poolSize));
  for (var k = n1Seed; k < poolSize; k++) {
    fx1Values[k] = fx1Values[k % n1Seed];
  }
  for (var k = n2Seed; k < poolSize; k++) {
    keyedValues[k] = keyedValues[k % n2Seed];
  }
  for (var k = n3Seed; k < poolSize; k++) {
    packValues[k] = packValues[k % n3Seed];
  }
  if (fx1.fxRootFixedSave(fx1Values, poolSize, fx1SaveBuf) <= 0 ||
      keyed.keyedConfigFixedSave(keyedValues, poolSize, keyedSaveBuf) <= 0 ||
      pack.packConfigFixedSave(packValues, poolSize, packSaveBuf) <= 0) {
    stderr.writeln('soak: fixed-form batch save failed');
    exit(2);
  }
  final fx1BatchBytes = Uint8List.fromList(fx1SaveBuf);
  final keyedBatchBytes = Uint8List.fromList(keyedSaveBuf);
  final packBatchBytes = Uint8List.fromList(packSaveBuf);
  for (var k = 0; k < poolSize; k++) {
    fx1Values[k] = fx1home.FxRoot();
    keyedValues[k] = demo.KeyedConfig();
    packValues[k] = demo.PackConfig();
  }
  if (fx1.fxRootFixedLoad(
            fx1Values,
            poolSize,
            fx1BatchBytes,
            fx1BatchBytes.length,
            fx1Plan,
            fx1Report,
          ) !=
          poolSize ||
      keyed.keyedConfigFixedLoad(
            keyedValues,
            poolSize,
            keyedBatchBytes,
            keyedBatchBytes.length,
            keyedPlan,
            demoReport,
          ) !=
          poolSize ||
      pack.packConfigFixedLoad(
            packValues,
            poolSize,
            packBatchBytes,
            packBatchBytes.length,
            packPlan,
            demoReport,
          ) !=
          poolSize) {
    stderr.writeln('soak: fixed-form batch reload failed');
    exit(2);
  }

  final renderBytes = File('testdata/wire/tables/block_render.bin')
      .readAsBytesSync();
  final paddedBytes = File('testdata/wire/tables/block_padded.bin')
      .readAsBytesSync();
  final renderBlock = rblk.RenderFrameBlock.open(
    renderBytes,
    0,
    renderBytes.length,
  );
  final paddedBlock = pblk.PaddedFrameBlock.open(
    paddedBytes,
    0,
    paddedBytes.length,
  );
  if (renderBlock == null || paddedBlock == null) {
    stderr.writeln('soak: block fixtures did not open');
    exit(2);
  }
  final rb = renderBlock;
  final pb = paddedBlock;
  final shipCursor = rb.shipsCursor();
  final rowCursor = pb.rowsCursor();
  final shipsCount = rb.shipsCount;
  final rowsCount = pb.rowsCount;

  final sceneBytes = File('build/cook-fuzz/Scene.cook').readAsBytesSync();
  final treeBytes = File('build/cook-fuzz/TreeNode.cook').readAsBytesSync();
  final sceneCook = ck.SceneCook.open(sceneBytes, 0, sceneBytes.length);
  final treeCook = ck.TreeNodeCook.open(treeBytes, 0, treeBytes.length);
  if (sceneCook == null || treeCook == null) {
    stderr.writeln('soak: cook fixtures did not open');
    exit(2);
  }
  final sc = sceneCook;
  final tc = treeCook;
  final sceneCursor = sc.cursor();
  final treeCursor = tc.cursor();

  final plant = Platform.environment['SCHEMA_DART_ALLOC_PLANT'] == '1';
  var iter = 0;
  var fixedValueChecksum = 0;
  var fixedValuesSeen = false;

  void step() {
    if (plant) {
      final planted = List<int>.filled(4, iter);
      plantRing[iter & 63] = planted;
      checksum = (checksum + planted[0]) & 0x3fffffff;
    }
    iter++;

    final n1 = fx1.fxRootFixedLoad(
      fx1Values,
      poolSize,
      fx1BatchBytes,
      fx1BatchBytes.length,
      fx1Plan,
      fx1Report,
    );
    final n2 = keyed.keyedConfigFixedLoad(
      keyedValues,
      poolSize,
      keyedBatchBytes,
      keyedBatchBytes.length,
      keyedPlan,
      demoReport,
    );
    final n3 = pack.packConfigFixedLoad(
      packValues,
      poolSize,
      packBatchBytes,
      packBatchBytes.length,
      packPlan,
      demoReport,
    );
    if (n1 != poolSize || n2 != poolSize || n3 != poolSize) {
      exit(3);
    }
    var loadedChecksum = 0;
    for (var k = 0; k < poolSize; k++) {
      loadedChecksum =
          (loadedChecksum +
              fx1Values[k].keep +
              keyedValues[k].teams[0].spawnCount +
              packValues[k].version) &
          0x3fffffff;
    }
    if (!fixedValuesSeen) {
      fixedValueChecksum = loadedChecksum;
      fixedValuesSeen = true;
    } else if (loadedChecksum != fixedValueChecksum) {
      stderr.writeln('soak: fixed-form loaded values changed');
      exit(3);
    }
    final m1 = fx1.fxRootFixedMeasure(poolSize);
    final m2 = keyed.keyedConfigFixedMeasure(poolSize);
    final m3 = pack.packConfigFixedMeasure(poolSize);
    final w1 = fx1.fxRootFixedSave(fx1Values, poolSize, fx1SaveBuf);
    final w2 = keyed.keyedConfigFixedSave(keyedValues, poolSize, keyedSaveBuf);
    final w3 = pack.packConfigFixedSave(packValues, poolSize, packSaveBuf);
    if (w1 <= 0 || w2 <= 0 || w3 <= 0) {
      exit(3);
    }
    if (w1 != fx1BatchBytes.length ||
        w2 != keyedBatchBytes.length ||
        w3 != packBatchBytes.length ||
        !_sameBytes(fx1SaveBuf, fx1BatchBytes, w1) ||
        !_sameBytes(keyedSaveBuf, keyedBatchBytes, w2) ||
        !_sameBytes(packSaveBuf, packBatchBytes, w3)) {
      stderr.writeln('soak: fixed-form batch bytes changed');
      exit(3);
    }
    checksum =
        (checksum +
            loadedChecksum +
            n1 +
            n2 +
            n3 +
            m1 +
            m2 +
            m3 +
            w1 +
            w2 +
            w3) &
        0x3fffffff;
    for (var r = 0; r < shipsCount; r++) {
      rb.shipsAt(r, shipCursor);
      checksum =
          (checksum +
              shipCursor.objectId +
              shipCursor.team +
              shipCursor.thrust.toInt()) &
          0x3fffffff;
    }
    for (var r = 0; r < rowsCount; r++) {
      pb.rowsAt(r, rowCursor);
      checksum =
          (checksum + rowCursor.id + rowCursor.tag + rowCursor.value.toInt()) &
          0x3fffffff;
    }
    sc.root(sceneCursor);
    checksum =
        (checksum + sceneCursor.version + sceneCursor.nameLength) & 0x3fffffff;
    tc.root(treeCursor);
    checksum = (checksum + treeCursor.labelLength) & 0x3fffffff;
  }

  // warmup: settle one-time lazy initialisation before the marker
  final sw = Stopwatch()..start();
  while (sw.elapsedMilliseconds < 4000) {
    step();
  }
  stderr.writeln('soak: steady phase begins');
  sw.reset();
  while (sw.elapsedMilliseconds ~/ 1000 < seconds) {
    step();
  }
  stderr.writeln('soak: steady phase ends');
  stdout.writeln('soak: seconds=$seconds checksum=$checksum');
}

Future<int> _drive(List<String> args) async {
  final aotBin = args.length > 2 ? args[2] : 'build/dart_tables_soak';
  final seconds = args.length > 3 ? int.parse(args[3]) : 20;

  final runningSdk =
      Platform.environment['SCHEMA_DART_ALLOC_SDK_VERSION'] ??
      Platform.version.split(' ').first;
  final anySdk = Platform.environment['SCHEMA_DART_ALLOC_ANY_SDK'] == '1';
  if (runningSdk != pinnedDartSdk && !anySdk) {
    stdout.writeln(
      'FAILED: allocation certification requires Dart SDK $pinnedDartSdk (pinned in make/dart.mk), running $runningSdk; set SCHEMA_DART_ALLOC_ANY_SDK=1 to report observations without certifying',
    );
    stdout.writeln('NOT CERTIFIED');
    return 1;
  }

  final env = Map<String, String>.from(Platform.environment);
  env['DART_VM_OPTIONS'] = vmFlagsCsv;
  final proc = await Process.start(aotBin, <String>[
    '$seconds',
  ], environment: env);

  var scavenges = 0;
  var started = false;
  var baseline = -1;
  var failed = false;
  final samples = <String>[];
  final rssSamples = <int>[];

  final sampler = Timer.periodic(const Duration(seconds: 1), (_) {
    if (!started) {
      return;
    }
    final rss = _childRss(proc.pid);
    if (baseline < 0) {
      baseline = scavenges;
    } else if (scavenges != baseline) {
      failed = true;
    }
    rssSamples.add(rss);
    samples.add(
      't=${samples.length + 1}s scavenges=$scavenges rss_kb=${rss < 0 ? "?" : rss}',
    );
  });

  await for (final line
      in proc.stderr.transform(utf8.decoder).transform(const LineSplitter())) {
    if (line.contains('soak: steady phase begins')) {
      started = true;
      continue;
    }
    if (line.contains('soak: steady phase ends')) {
      started = false;
      continue;
    }
    if (line.contains('Scavenge')) {
      scavenges++;
    }
  }
  sampler.cancel();
  final workerCode = await proc.exitCode;

  if (workerCode != 0) {
    stdout.writeln('SOAK FAILED: worker exited $workerCode');
    return 1;
  }
  if (rssSamples.isEmpty) {
    stdout.writeln('SOAK FAILED: no post-warmup RSS samples were taken');
    return 1;
  }

  for (final s in samples) {
    stdout.writeln('  $s');
  }
  const rssDriftBoundKb = 8 * 1024;
  final firstRss = rssSamples.first;
  final lastRss = rssSamples.last;
  final rssDrift = lastRss - firstRss;
  final rssDriftOk =
      firstRss >= 0 && lastRss >= 0 && rssDrift <= rssDriftBoundKb;
  stdout.writeln(
    'soak: rss drift ${firstRss < 0 ? "?" : firstRss} KB -> '
    '${lastRss < 0 ? "?" : lastRss} KB '
    '(${rssDrift >= 0 ? "+" : ""}$rssDrift KB, '
    'bound +$rssDriftBoundKb KB) '
    '[${rssDriftOk ? "PASS" : "FAIL"}] (weaker instrument)',
  );
  if (failed) {
    stdout.writeln('SOAK FAILED: the scavenge count moved after warmup');
    stdout.writeln(
      'soak: count moved — the generated read/write path allocates',
    );
    return 1;
  }
  if (!rssDriftOk) {
    stdout.writeln('SOAK FAILED: RSS drift exceeded the post-warmup bound');
    return 1;
  }
  stdout.writeln(
    'soak: scavenge count flat at $scavenges after warmup over $seconds s',
  );
  return 0;
}

Future<void> main(List<String> args) async {
  final mode = args.isNotEmpty ? args[0] : 'idle';
  if (mode == 'drive') {
    exit(await _drive(args));
  }
  final seconds = args.length > 1 ? int.parse(args[1]) : 20;
  _runPhase(seconds);
}
