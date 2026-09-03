// the tables bench — the Dart runner.
//
// Measures ONE thing: a representative fixed table written and read on the
// TOLERANT WIRE (docs/SPEC-TABLES.md §3), through the generated table codec.
// That is the number a reader who knows protobuf or flatbuffers already has a
// comparison for, and it is the per-language release gate for the tables layer
// (bench/tables/README.md).
//
// It is a port of bench/tables/cpp/table_main.cpp — the reference
// implementation — and follows the same contract (BENCH-STANDARD.md): the
// committed variant corpus drives it, the golden gate runs before the clock,
// the loops are barriered against dead-code elimination, and the report is
// 1 warmup + 7 measured runs with the median beside min/max/spread.
//
// THIS FILE IS SHAPE-BLIND. It names the generated TYPE at one call site and
// nothing else: no field, no pinned value, no wire size. Shape knowledge lives
// in bench/corpus/BenchTable.schema, in the code generated from it, and in the
// committed data test/bench/table_main.cpp produced.
//
// WHAT DART SPELLS DIFFERENTLY, and the reason at each site. There is no
// `asm volatile` memory clobber: the write arm's buffer is a top-level
// Uint8List the codec writes THROUGH, so the store is already observable, and
// the returned length folds into a top-level sink read at the end — which is
// what the Go and C# legs do, for the same reason. THE READER AND WRITER ARE
// OWNED BY THE RUNNER and re-pointed with `attach`, because that is the shape
// a Dart consumer in a hot loop takes and the shape the soak's zero-allocation
// floor is measured over: `load` would allocate a reader per call, and
// benching the convenience entry would be benching an allocation the real
// caller does not make. The verbs are METHODS on the value, so the driver takes
// them as closures over the one generated type.
//
// Output: a human table on stderr; with --csv, CSV v2 rows on stdout.
import 'dart:io';
import 'dart:typed_data';

import '../../../generated/bench/tables/dart/BenchTableTable.dart';

// sink defeats dead-code elimination of the computed lengths. It is written
// every iteration and read once at the end.
int sink = 0;

const int maxNumRuns = 7; // median of 7 (N >= 5), after 1 warmup run
const int numVariants = 64; // read-path variant buffers
const int bufferSize = 65536; // the ceiling the runner refuses past
// §2.7's variant stride, for the same reason and by the same arithmetic as the
// type runner's: a power-of-two stride maps every head line into a handful of
// L1 set groups and a memory-bound read then feels every background conflict
// miss.
const int variantStride = bufferSize + 64;

int numRuns = maxNumRuns;
bool csv = false;
String wireDir = 'testdata/wire';
String variantDir = 'bench/corpus/variants';
bool failed = false;

// the CSV's own columns (BENCH-STANDARD.md §5.1). family `table` (§1.9): the
// tolerant table wire, a DIFFERENT wire over a different corpus, so a tools
// refusal to divide it against a `gen` row is correct and automatic. linkage
// pkg — the generated table codec is ordinary library code in this binary and
// names no runtime at all. checks contract — Dart's typed-data reads are
// bounds-checked in every configuration and the reader's wire-contract
// validation is unconditional, which is §3.4's word for exactly this. opt aot
// — the leg is compiled with `dart compile exe`, which is the language's one
// release configuration and the one a shipping consumer runs.
const String csvSuffix = 'pkg,contract,aot,unknown';

final List<String> csvRows = <String>[];
final Map<String, Uint8List> goldensLoaded = {};

final Uint8List buffer = Uint8List(bufferSize);
final Uint8List twin = Uint8List(bufferSize);
late Uint8List variants;
// ONE Uint8List PER SLOT, over the slot's own range of the staggered array:
// the reader indexes from its own zero, so a view over the whole array would
// read the first slot for every one of them.
late List<Uint8List> variantBytes;

Uint8List variant(int k) => variantBytes[k];

int fnv1a64(int h, Uint8List data) {
  for (final b in data) {
    h ^= b;
    h = (h * 0x100000001b3) & 0xffffffffffffffff;
  }
  return h;
}

String corpusId() {
  // the C++ leg's std::map iterates in sorted basename order, and the id is
  // FNV-1a-64 over the goldens THIS RUN loaded (BENCH-STANDARD.md §1.6)
  final names = goldensLoaded.keys.toList()..sort();
  var h = 0xcbf29ce484222325;
  for (final name in names) {
    h = fnv1a64(h, Uint8List.fromList(name.codeUnits));
    h = fnv1a64(h, Uint8List.fromList(<int>[0]));
    h = fnv1a64(h, goldensLoaded[name]!);
  }
  return hex64(h);
}

String hex64(int v) {
  final high = (v >> 32) & 0xffffffff;
  final low = v & 0xffffffff;
  return high.toRadixString(16).padLeft(8, '0') +
      low.toRadixString(16).padLeft(8, '0');
}

void fail(String name, String what) {
  stderr.writeln('FAILED: $name: $what');
  failed = true;
}

void flushCsv() {
  if (!csv) {
    return;
  }
  if (failed) {
    // §1.5: a failing run emits NO rows.
    stderr.writeln('refusing to emit CSV rows from a failing run');
    return;
  }
  final id = corpusId();
  for (final row in csvRows) {
    stdout.writeln('$row,$id,table,$csvSuffix');
  }
}

bool same(Uint8List a, Uint8List b, int n) {
  for (var i = 0; i < n; i++) {
    if (a[i] != b[i]) {
      return false;
    }
  }
  return true;
}

bool checkGolden(String name, Uint8List data, int n) {
  final path = '$wireDir/$name.bin';
  final file = File(path);
  if (!file.existsSync()) {
    stderr.writeln(
      'missing wire golden $path — run from the schema repo root '
      '(or pass --wire-dir)',
    );
    return false;
  }
  final expected = file.readAsBytesSync();
  goldensLoaded['$name.bin'] = expected;
  if (expected.length != n || !same(expected, data, n)) {
    stderr.writeln(
      'WIRE GOLDEN MISMATCH: $name (${expected.length} golden vs $n actual '
      'bytes) — refusing to bench code that does not match the corpus',
    );
    return false;
  }
  return true;
}

class RunStats {
  final double median;
  final double min;
  final double max;
  final double spreadPct;

  RunStats(this.median, this.min, this.max, this.spreadPct);
}

RunStats stats(List<double> rates) {
  final sorted = List<double>.from(rates)..sort();
  final median = sorted[sorted.length ~/ 2];
  final min = sorted.first;
  final max = sorted.last;
  return RunStats(median, min, max, (max - min) / median * 100.0);
}

void report(String bench, String path, int iters, int bytesPerOp, RunStats s) {
  final mbps = s.median * bytesPerOp / (1024.0 * 1024.0);
  stderr.writeln(
    '${bench.padRight(18)} ${path.padRight(11)} '
    '${(s.median / 1e6).toStringAsFixed(3).padLeft(10)} M msg/s '
    '${mbps.toStringAsFixed(1).padLeft(10)} MB/s   '
    '(min ${(s.min / 1e6).toStringAsFixed(3)}, '
    'max ${(s.max / 1e6).toStringAsFixed(3)}, '
    'spread ${s.spreadPct.toStringAsFixed(1)}%)',
  );
  if (csv) {
    csvRows.add(
      'dart,$bench,$path,$iters,$bytesPerOp,$numRuns,'
      '${s.median.toStringAsFixed(0)},${s.min.toStringAsFixed(0)},'
      '${s.max.toStringAsFixed(0)},${mbps.toStringAsFixed(2)},'
      '${s.spreadPct.toStringAsFixed(2)}',
    );
  }
}

// loadVariants loads <variant-dir>/<name>.variants.bin into the numVariants
// staggered slots and returns the record size, or -1. Records are fixed-width
// by construction — test/bench/table_main.cpp refuses to emit a corpus whose
// records differ — so the record size IS file size / numVariants.
int loadVariants(String name) {
  final path = '$variantDir/$name.variants.bin';
  final file = File(path);
  if (!file.existsSync()) {
    stderr.writeln(
      'missing variant data $path — run `make bench-table-corpus`, and run '
      'from the schema repo root (or pass --variant-dir)',
    );
    return -1;
  }
  final packed = file.readAsBytesSync();
  if (packed.isEmpty || packed.length % numVariants != 0) {
    stderr.writeln(
      'variant data $path is ${packed.length} bytes, not a multiple of '
      '$numVariants records — refusing to bench data whose stride is not the '
      'record size',
    );
    return -1;
  }
  final record = packed.length ~/ numVariants;
  if (record > bufferSize) {
    stderr.writeln(
      'variant data $path has $record-byte records, over the '
      '$bufferSize-byte buffer',
    );
    return -1;
  }
  variants = Uint8List(numVariants * variantStride);
  variantBytes = <Uint8List>[];
  for (var k = 0; k < numVariants; k++) {
    variants.setRange(
      k * variantStride,
      k * variantStride + record,
      packed,
      k * record,
    );
    final slot = Uint8List.sublistView(
      variants,
      k * variantStride,
      k * variantStride + bufferSize,
    );
    variantBytes.add(slot);
  }
  goldensLoaded['$name.variants.bin'] = packed;
  return record;
}

// benchTable is the data-driven table driver.
//
// THE READ ARM RESETS BEFORE IT LOADS, and that is not overhead the runner
// added: the tolerant wire ELIDES a field at its default (§3), so a load fills
// only what actually rode and a reused instance would otherwise keep the
// previous record's values in the elided fields. Resetting is part of a correct
// read into reused storage, in every language, so it is inside the clock rather
// than hidden outside it.
void benchTable<T>(
  String name,
  String golden,
  int baseIters,
  T Function() make,
  void Function(T) reset,
  bool Function(TableWriter, T) saveBody,
  bool Function(TableReader, T) loadBody,
) {
  final iters = baseIters;

  final bytesPerOp = loadVariants(name);
  if (bytesPerOp < 0) {
    failed = true;
    return;
  }

  // gate 1 (§1.5): variant 0 IS the pinned instance.
  if (!checkGolden(golden, variant(0), bytesPerOp)) {
    failed = true;
    return;
  }

  // the caller-owned instruments, re-pointed per call: this is the shape a
  // Dart consumer in a hot loop takes, and the one the soak's zero-allocation
  // floor is measured over
  final report_ = TableReport();
  final reader = TableReader(buffer, report_);
  final writer = TableWriter(buffer);

  // gate 2: every variant loads, re-saves, and comes back byte-identical at
  // the same length — before any clock starts.
  final instances = List<T>.generate(numVariants, (_) => make());
  for (var k = 0; k < numVariants; k++) {
    reset(instances[k]);
    report_.clear();
    reader.attach(variant(k), report_);
    reader.limit = bytesPerOp;
    if (!loadBody(reader, instances[k]) || report_.malformed) {
      fail(name, 'load of a variant failed');
      return;
    }
    writer.attach(twin);
    if (!saveBody(writer, instances[k]) ||
        writer.offset != bytesPerOp ||
        !same(twin, variant(k), bytesPerOp)) {
      fail(
        name,
        'variant round-trip bytes differ — refusing to bench a codec that does '
        'not reproduce the corpus',
      );
      return;
    }
  }

  final writeRates = <double>[];
  final roundTripRates = <double>[];

  // WRITE: save the 64 pre-loaded instances round-robin. Rotating the
  // instances is the §2.7 variation: the encoder never sees the same input
  // twice in a row, and bytes/op is constant by construction rather than by
  // assertion. The sink is the byte fold.
  for (var run = -1; run < numRuns; run++) {
    final clock = Stopwatch()..start();
    for (var i = 0; i < iters; i++) {
      writer.attach(buffer);
      if (!saveBody(writer, instances[i & (numVariants - 1)]) ||
          writer.offset != bytesPerOp) {
        fail(name, 'save failed in loop');
        return;
      }
      sink += writer.offset;
    }
    final elapsed = clock.elapsedMicroseconds / 1e6;
    if (run >= 0) {
      writeRates.add(iters / elapsed);
    }
  }

  // ROUND-TRIP: reset, load a variant buffer, then re-save what came out. The
  // load needs no sink discipline of its own — its output IS the save's input,
  // so every loaded field is observed by construction.
  final out = make();
  for (var run = -1; run < numRuns; run++) {
    final clock = Stopwatch()..start();
    for (var i = 0; i < iters; i++) {
      reset(out);
      report_.clear();
      final slot = i & (numVariants - 1);
      reader.attach(variant(slot), report_);
      reader.limit = bytesPerOp;
      if (!loadBody(reader, out)) {
        fail(name, 'load failed in loop');
        return;
      }
      writer.attach(buffer);
      if (!saveBody(writer, out) || writer.offset != bytesPerOp) {
        fail(name, 're-save failed in loop');
        return;
      }
      sink += writer.offset;
    }
    final elapsed = clock.elapsedMicroseconds / 1e6;
    if (run >= 0) {
      roundTripRates.add(iters / elapsed);
    }
  }

  final w = stats(writeRates);
  final rt = stats(roundTripRates);
  report(name, 'write', iters, bytesPerOp, w);
  report(name, 'round_trip', iters, bytesPerOp, rt);

  // READ is DERIVED, never measured: round-trip time minus write time. It
  // prints for continuity and is NOT a CSV row — a derived number in the CSV
  // would be divided as if it had been measured (§2.9).
  final readTime = 1.0 / rt.median - 1.0 / w.median;
  if (readTime > 0) {
    stderr.writeln(
      '${name.padRight(18)} ${'read'.padRight(11)} '
      '${(1e-6 / readTime).toStringAsFixed(3).padLeft(10)} M msg/s   '
      '(DERIVED: round-trip minus write, informational — not a measured row)',
    );
  }
}

void main(List<String> args) {
  for (var i = 0; i < args.length; i++) {
    if (args[i] == '--csv') {
      csv = true;
    } else if (args[i] == '--wire-dir' && i + 1 < args.length) {
      wireDir = args[++i];
    } else if (args[i] == '--variant-dir' && i + 1 < args.length) {
      variantDir = args[++i];
    } else if (args[i] == '--round' && i + 1 < args.length) {
      final k = int.tryParse(args[++i]);
      if (k == null || k < 0) {
        stderr.writeln('--round takes a non-negative integer');
        exit(1);
      }
      numRuns = 1;
    } else {
      stderr.writeln(
        'usage: table_main [--csv] [--round K] [--wire-dir <dir>] '
        '[--variant-dir <dir>]',
      );
      exit(1);
    }
  }

  stderr.writeln('schema tables bench (dart)');

  if (csv) {
    stdout.writeln(
      'lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,'
      'min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,'
      'corpus_id,family,linkage,checks,opt,inline',
    );
  }

  // The one measured shape, named once — the generated type at the call site
  // and nothing else about it (bench/SHAPE-GATE.allow).
  benchTable<TableMixed>(
    'bench_table',
    'bench_table',
    400000,
    TableMixed.new,
    (v) => v.reset(),
    (w, v) => v.saveBody(w),
    (r, v) => v.loadBody(r),
  );

  flushCsv();

  if (failed) {
    stderr.writeln('TABLES BENCH FAILED (corpus_id ${corpusId()})');
    exit(1);
  }

  stderr.writeln('OK (corpus_id ${corpusId()})');
  if (sink == 0x7fffffffffffffff) {
    stderr.writeln('unreachable');
  }
  exit(0);
}
