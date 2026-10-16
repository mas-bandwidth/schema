// THE DART ALLOCATION GATE (docs/SPEC-TABLES.md, docs/PORTING.md I1 and I14).
//
// The Dart VM exposes no in-process allocation counter outside the VM service
// isolate, so the gate reads the collector's own output: `--verbose_gc` prints
// a line to stderr on every new-space scavenge, and pinning the semi-space to
// 1 MB (`--new_gen_semi_initial_size=1 --new_gen_semi_max_size=1`) makes one
// 16-byte object per record trigger a scavenge every ~30,000 records. A steady
// phase of 20,000 iterations over the multi-record corpus that allocates
// nothing triggers ZERO scavenges between the two marker lines on stderr.
//
// Two modes:
//   1. Single-row worker (`gcgate <phase> [n]`): opens the fixtures, allocates
//      all caller-owned storage up front, warms the path so one-time setup sits
//      before the marker, writes `gcgate: steady phase begins` to stderr, runs
//      `n` passes of `<phase>`, and writes `gcgate: steady phase ends`.
//   2. Suite driver (`gcgate drive <dart-bin> <aot-bin> [n]`): enforces the
//      pinned SDK rule (PORTING.md I14: Dart 3.13.2, with
//      SCHEMA_DART_ALLOC_ANY_SDK=1 as the observation-only escape hatch),
//      runs every row under the AOT executable (`DART_VM_OPTIONS`) and under
//      the JIT (`$(DART)` with `DART_GC_FLAGS`), settles once if a cold runner
//      scavenges during the first AOT pass, prints the AOT and JIT counts
//      side by side, and holds every gated row to zero scavenges under AOT.
//
// Why `variable` (form-1 Card load and save) is reported without a ceiling
// (PORTING.md I1, "a row reported without a ceiling cites why"):
//   The generated variable-class entry points `cardWireLoad` and `cardWireSave`
//   in `CardsWire.dart` return a freshly allocated `Card` record (constructing
//   new `String` and `List` instances for the variable-length fields) and a
//   freshly allocated `Uint8List` wire buffer per call rather than filling
//   caller-owned scratch storage. Its scavenge count is therefore printed
//   beside the gated zero-allocation rows and not gated.

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
import '../../build/tables-generated-dart/cards/CardsWire.dart' as cards_wire;
import '../../build/tables-generated-dart/pointers/GraphCook.dart' as ck;
import '../../build/tables-generated-dart/pointers/GraphdemoCook.dart'
    show TableCookRef;

const String pinnedDartSdk = '3.13.2';

const String vmFlagsCsv =
    '--verbose_gc,--new_gen_semi_initial_size=1,--new_gen_semi_max_size=1';
const List<String> vmFlagsList = <String>[
  '--verbose_gc',
  '--new_gen_semi_initial_size=1',
  '--new_gen_semi_max_size=1',
];

final class _RowSpec {
  final String name;
  final int? ceiling;
  final int defaultIterations;
  final String why;
  const _RowSpec(this.name, this.ceiling, this.defaultIterations, this.why);
}

const List<_RowSpec> _rows = <_RowSpec>[
  _RowSpec('idle', 0, 20000, 'baseline loop over the loaded fixture bytes'),
  _RowSpec(
    'fixed-load',
    0,
    20000,
    'form-3 load into caller-owned FX1, KeyedConfig and PackConfig records with caller-owned plan and report',
  ),
  _RowSpec(
    'fixed-measure',
    0,
    20000,
    'form-3 measure over caller-owned FX1, KeyedConfig and PackConfig records',
  ),
  _RowSpec(
    'fixed-save',
    0,
    20000,
    'form-3 save from caller-owned records into caller-owned Uint8List buffers',
  ),
  _RowSpec(
    'block',
    0,
    20000,
    'RenderFrameBlock and PaddedFrameBlock row walk through caller-owned row cursors',
  ),
  _RowSpec(
    'cook',
    0,
    20000,
    'SceneCook and TreeNodeCook root and pointer-chain walk through caller-owned cursors',
  ),
  _RowSpec(
    'variable',
    null,
    2000,
    'reported (no ceiling): cardWireLoad and cardWireSave allocate a new Card tree and Uint8List per call rather than taking caller-owned scratch',
  ),
];

final List<Object?> plantRing = List<Object?>.filled(64, null);
int checksum = 0;

void _require(bool ok, String msg) {
  if (!ok) {
    stderr.writeln('gcgate: $msg');
    exit(2);
  }
}

void _runPhase(String phase, int n) {
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
  _require(
    n1Seed > 0 && n2Seed > 0 && n3Seed > 0,
    'fixed-form seed load failed ($n1Seed, $n2Seed, $n3Seed)',
  );

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
  final fx1BatchLen = fx1.fxRootFixedSave(fx1Values, poolSize, fx1SaveBuf);
  final keyedBatchLen = keyed.keyedConfigFixedSave(
    keyedValues,
    poolSize,
    keyedSaveBuf,
  );
  final packBatchLen = pack.packConfigFixedSave(
    packValues,
    poolSize,
    packSaveBuf,
  );
  _require(
    fx1BatchLen > 0 && keyedBatchLen > 0 && packBatchLen > 0,
    'fixed-form batch save failed ($fx1BatchLen, $keyedBatchLen, $packBatchLen)',
  );
  final fx1BatchBytes = Uint8List.fromList(fx1SaveBuf);
  final keyedBatchBytes = Uint8List.fromList(keyedSaveBuf);
  final packBatchBytes = Uint8List.fromList(packSaveBuf);
  for (var k = 0; k < poolSize; k++) {
    fx1Values[k] = fx1home.FxRoot();
    keyedValues[k] = demo.KeyedConfig();
    packValues[k] = demo.PackConfig();
  }
  _require(
    fx1.fxRootFixedLoad(
          fx1Values,
          poolSize,
          fx1BatchBytes,
          fx1BatchBytes.length,
          fx1Plan,
          fx1Report,
        ) ==
        poolSize,
    'fx1 batch reload failed',
  );
  _require(
    keyed.keyedConfigFixedLoad(
          keyedValues,
          poolSize,
          keyedBatchBytes,
          keyedBatchBytes.length,
          keyedPlan,
          demoReport,
        ) ==
        poolSize,
    'keyed batch reload failed',
  );
  _require(
    pack.packConfigFixedLoad(
          packValues,
          poolSize,
          packBatchBytes,
          packBatchBytes.length,
          packPlan,
          demoReport,
        ) ==
        poolSize,
    'pack batch reload failed',
  );

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
  _require(
    renderBlock != null && paddedBlock != null,
    'block fixtures did not open',
  );
  final rb = renderBlock!;
  final pb = paddedBlock!;
  final shipCursor = rb.shipsCursor();
  final rowCursor = pb.rowsCursor();
  final shipsCount = rb.shipsCount;
  final rowsCount = pb.rowsCount;

  final sceneBytes = File('build/cook-fuzz/Scene.cook').readAsBytesSync();
  final treeBytes = File('build/cook-fuzz/TreeNode.cook').readAsBytesSync();
  final sceneCook = ck.SceneCook.open(sceneBytes, 0, sceneBytes.length);
  final treeCook = ck.TreeNodeCook.open(treeBytes, 0, treeBytes.length);
  _require(sceneCook != null && treeCook != null, 'cook fixtures did not open');
  final sc = sceneCook!;
  final tc = treeCook!;
  final sceneCursor = sc.cursor();
  final treeCursor = tc.cursor();
  final listCursor = ck.ListNodeRow(sc.view, sc.region);

  final sampleCardBytes = File('tables/cards/fuzz/card-populated.wire')
      .readAsBytesSync();
  final cardWireReport = cards_wire.TableWireReport();

  final plantRow = Platform.environment['SCHEMA_DART_ALLOC_PLANT'] ?? '';
  final plantHere = plantRow == phase || phase == 'plant';

  void run(int iters) {
    final batchIters = iters ~/ poolSize < 1 ? 1 : iters ~/ poolSize;
    switch (phase) {
      case 'idle':
        for (var i = 0; i < iters; i++) {
          checksum =
              (checksum +
                  fx1Bytes[0] +
                  keyedBytes[0] +
                  packBytes[0] +
                  renderBytes[0] +
                  sceneBytes[0]) &
              0x3fffffff;
        }
      case 'fixed-load':
      case 'plant':
        for (var i = 0; i < batchIters; i++) {
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
          final n3 = (i & 7) == 0
              ? pack.packConfigFixedLoad(
                  packValues,
                  n3Seed,
                  packBytes,
                  packBytes.length,
                  packPlan,
                  demoReport,
                )
              : n3Seed;
          if (n1 != poolSize || n2 != poolSize || n3 != n3Seed) {
            exit(3);
          }
          for (var k = 0; k < poolSize; k++) {
            if (plantHere) {
              // Negative control: one small heap allocation per record.
              final planted = List<int>.filled(4, i + k);
              plantRing[(i + k) & 63] = planted;
              checksum = (checksum + planted[0]) & 0x3fffffff;
            }
            checksum =
                (checksum +
                    fx1Values[k].keep +
                    keyedValues[k].teams[0].spawnCount +
                    packValues[k].version) &
                0x3fffffff;
          }
        }
      case 'fixed-measure':
        for (var i = 0; i < iters; i++) {
          final m1 = fx1.fxRootFixedMeasure(poolSize);
          final m2 = keyed.keyedConfigFixedMeasure(poolSize);
          final m3 = pack.packConfigFixedMeasure(poolSize);
          if (plantHere) {
            final planted = List<int>.filled(4, i);
            plantRing[i & 63] = planted;
            checksum = (checksum + planted[0]) & 0x3fffffff;
          }
          checksum = (checksum + m1 + m2 + m3) & 0x3fffffff;
        }
      case 'fixed-save':
        for (var i = 0; i < batchIters; i++) {
          final w1 = fx1.fxRootFixedSave(fx1Values, poolSize, fx1SaveBuf);
          final w2 = keyed.keyedConfigFixedSave(
            keyedValues,
            poolSize,
            keyedSaveBuf,
          );
          final w3 = (i & 7) == 0
              ? pack.packConfigFixedSave(packValues, n3Seed, packSaveBuf)
              : packBatchLen;
          if (w1 <= 0 || w2 <= 0 || w3 <= 0) {
            exit(3);
          }
          for (var k = 0; k < poolSize; k++) {
            if (plantHere) {
              final planted = List<int>.filled(4, i + k);
              plantRing[(i + k) & 63] = planted;
              checksum = (checksum + planted[0]) & 0x3fffffff;
            }
          }
          checksum = (checksum + w1 + w2 + w3) & 0x3fffffff;
        }
      case 'block':
        for (var i = 0; i < iters; i++) {
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
                (checksum +
                    rowCursor.id +
                    rowCursor.tag +
                    rowCursor.value.toInt()) &
                0x3fffffff;
          }
          if (plantHere) {
            final planted = List<int>.filled(64, i);
            plantRing[i & 63] = planted;
            checksum = (checksum + planted[0]) & 0x3fffffff;
          }
        }
      case 'cook':
        for (var i = 0; i < iters; i++) {
          sc.root(sceneCursor);
          checksum =
              (checksum + sceneCursor.version + sceneCursor.nameLength) &
              0x3fffffff;
          var target = sc.at(sceneCursor.at + 40, ck.ListNodeRow.rowSize);
          var steps = 0;
          while (steps < 16 &&
              target != TableCookRef.none &&
              target != TableCookRef.outside) {
            listCursor.at = target;
            checksum =
                (checksum + listCursor.value + listCursor.nameLength) &
                0x3fffffff;
            target = sc.at(listCursor.at + 24, ck.ListNodeRow.rowSize);
            steps++;
          }
          tc.root(treeCursor);
          checksum = (checksum + treeCursor.labelLength) & 0x3fffffff;
          if (plantHere) {
            final planted = List<int>.filled(64, i);
            plantRing[i & 63] = planted;
            checksum = (checksum + planted[0]) & 0x3fffffff;
          }
        }
      case 'variable':
        for (var i = 0; i < iters; i++) {
          final decoded = cards_wire.cardWireLoad(
            sampleCardBytes,
            cardWireReport,
          );
          final encoded = cards_wire.cardWireSave(decoded);
          checksum =
              (checksum + encoded.length + decoded.id.length) & 0x3fffffff;
        }
      default:
        stderr.writeln('unknown phase: $phase');
        exit(2);
    }
  }

  final warmup = n ~/ 10 < 200 ? 200 : n ~/ 10;
  run(warmup);
  stderr.writeln('gcgate: steady phase begins');
  run(n);
  stderr.writeln('gcgate: steady phase ends');
  stdout.writeln(
    'gcgate $phase: n=$n checksum=$checksum ring=${plantRing[7] == null ? 0 : 1}',
  );
}

int _countScavenges(String errText) {
  var inside = false;
  var count = 0;
  for (final line in const LineSplitter().convert(errText)) {
    if (line.contains('gcgate: steady phase begins')) {
      inside = true;
      continue;
    }
    if (line.contains('gcgate: steady phase ends')) {
      inside = false;
      continue;
    }
    if (inside && line.contains('Scavenge')) {
      count++;
    }
  }
  return count;
}

int _runChildAot(String aotBin, String phase, int iters) {
  final env = Map<String, String>.from(Platform.environment);
  env['DART_VM_OPTIONS'] = vmFlagsCsv;
  final res = Process.runSync(aotBin, <String>[
    phase,
    '$iters',
  ], environment: env);
  if (res.exitCode != 0) {
    stderr.writeln(
      'gcgate AOT child failed on $phase (exit ${res.exitCode}):\n${res.stderr}',
    );
    exit(2);
  }
  return _countScavenges('${res.stderr}');
}

int _runChildJit(String dartBin, String scriptPath, String phase, int iters) {
  final res = Process.runSync(dartBin, <String>[
    ...vmFlagsList,
    scriptPath,
    phase,
    '$iters',
  ]);
  if (res.exitCode != 0) {
    stderr.writeln(
      'gcgate JIT child failed on $phase (exit ${res.exitCode}):\n${res.stderr}',
    );
    exit(2);
  }
  return _countScavenges('${res.stderr}');
}

void _drive(List<String> args) {
  final dartBin = args.length > 1 ? args[1] : Platform.resolvedExecutable;
  final aotBin = args.length > 2 ? args[2] : 'build/dart_gcgate';
  final scaleIters = args.length > 3 ? int.parse(args[3]) : 20000;
  final onlySet = (Platform.environment['SCHEMA_DART_ALLOC_ROWS'] ?? '')
      .split(',')
      .where((s) => s.isNotEmpty)
      .toSet();

  final runningSdk =
      Platform.environment['SCHEMA_DART_ALLOC_SDK_VERSION'] ??
      Platform.version.split(' ').first;
  final anySdk = Platform.environment['SCHEMA_DART_ALLOC_ANY_SDK'] == '1';
  final plantRow = Platform.environment['SCHEMA_DART_ALLOC_PLANT'] ?? '';

  stdout.writeln(
    'tables Dart allocation gate (SDK $runningSdk, pinned $pinnedDartSdk):',
  );
  stdout.writeln(
    '  ${"row".padRight(16)}${"AOT".padLeft(6)}${"JIT".padLeft(6)}  ${"ceiling".padRight(12)}why',
  );

  var failed = false;
  for (final spec in _rows) {
    if (onlySet.isNotEmpty && !onlySet.contains(spec.name)) {
      continue;
    }
    final iters = spec.ceiling == null
        ? (scaleIters ~/ 10 < 500 ? 500 : scaleIters ~/ 10)
        : scaleIters;
    var aotCount = _runChildAot(aotBin, spec.name, iters);
    if (spec.ceiling != null &&
        aotCount > spec.ceiling! &&
        plantRow != spec.name) {
      stderr.writeln(
        'gcgate: ${spec.name} read $aotCount scavenges on initial AOT pass — warming longer and measuring again, once',
      );
      aotCount = _runChildAot(aotBin, spec.name, iters * 2);
    }
    final jitCount = _runChildJit(
      dartBin,
      'test/dart-tables/gcgate.dart',
      spec.name,
      iters ~/ 4 < 500 ? 500 : iters ~/ 4,
    );
    final ceilingStr = spec.ceiling == null ? 'reported' : '<= ${spec.ceiling}';
    var verdict = 'REPORTED';
    if (spec.ceiling != null) {
      if (aotCount > spec.ceiling!) {
        verdict = 'FAIL';
        failed = true;
      } else {
        verdict = 'PASS';
      }
    }
    stdout.writeln(
      '  ${spec.name.padRight(16)}${aotCount.toString().padLeft(6)}${jitCount.toString().padLeft(6)}  ${ceilingStr.padRight(12)}[$verdict] ${spec.why}',
    );
  }

  if (runningSdk != pinnedDartSdk && !anySdk) {
    stdout.writeln(
      'FAILED: allocation certification requires Dart SDK $pinnedDartSdk (pinned in make/dart.mk), running $runningSdk; set SCHEMA_DART_ALLOC_ANY_SDK=1 to report observations without certifying',
    );
    stdout.writeln('NOT CERTIFIED');
    exit(1);
  }
  if (anySdk) {
    stdout.writeln(
      'Dart allocation observation mode (SCHEMA_DART_ALLOC_ANY_SDK=1 on SDK $runningSdk, pinned $pinnedDartSdk): reporting observations only, NOT CERTIFIED',
    );
    if (failed) {
      exit(1);
    }
    return;
  }
  if (failed) {
    stdout.writeln('FAILED: one or more gated rows scavenged under AOT');
    exit(1);
  }
  stdout.writeln(
    'tables Dart allocation gate: 0 scavenges under AOT across fixed-load, fixed-measure, fixed-save, block and cook on Dart $pinnedDartSdk (CERTIFIED)',
  );
}

void main(List<String> args) {
  final mode = args.isNotEmpty ? args[0] : 'idle';
  if (mode == 'drive') {
    _drive(args);
    return;
  }
  final n = args.length > 1 ? int.parse(args[1]) : 20000;
  _runPhase(mode, n);
}
