// the tables bench — the Dart runner, THE FIXED FORM ONLY.
//
// The Dart twin of bench/tables/js/table_main.mjs, itself a port of
// bench/tables/cpp/table_main.cpp (the reference implementation), against the
// generated Dart table codec. It measures ONE thing: the paired bench's
// sixty-four logical records written and read in the FIXED FORM, form byte 3
// (docs/SPEC-TABLES.md §3.4), through the generated codec.
//
// WHY THERE IS NO TOLERANT HALF HERE, and why that is not a gap this runner
// can close: Dart has NO FORM-1 TABLE WIRE (schema#514). The backend carries
// form 3 and only form 3, so there is no tolerant Save/Load for this leg to
// gate or to time. What it CAN do with the tolerant corpus, it does — see
// `checkTolerantCorpus` below: the framing arithmetic is checkable without a
// decoder, and the bytes are what the corpus id is made of.
//
// WHY THERE IS NO PACKET LEG, so `bench/paired` runs dart as a TABLE-ONLY
// language and never divides a ratio for it: the packet runner
// (bench/dart/main.dart) is the TYPE BOARD's, with neither `--gate` nor
// `--iterations`, the two flags the paired driver's every invocation passes,
// and the paired set is the four the board is defined over. Its packet number
// is the type board's own, over the same sixty-four records. Faking a packet
// row to fill the driver's second wire would be inventing a measurement, so
// the driver accepts a table-only row instead, as it does for java and js.
//
// THE FORM THAT IS TIMED IS THE AOT EXECUTABLE (`dart compile exe`), the form
// that ships, exactly as bench/dart/main.dart documents for the packet leg:
//
//   dart compile exe bench/tables/dart/table_main.dart -o build/paired/table-dart
//   build/paired/table-dart --indexed --wire-dir bench/paired/corpus \
//       --variant-dir bench/paired/corpus [--gate] [--csv] [--round K]
//
// The paired driver builds and runs it so (bench/paired/main.go). `dart
// table_main.dart` (JIT) runs the same contract for iteration and is never
// the number.
//
// THE DART COSTS THIS LEG DOCUMENTS, which C and C++ do not pay:
//
//   - OBJECT PROJECTION. The read is a prefill of the reader's own image, one
//     plan run that copies the record's body into it, and then the generated
//     decode, which projects those bytes into Dart objects — a nested object
//     per entity, a list per bounded array, a String per text field. C and
//     C++ read into storage that IS the image and pay nothing for that step.
//     So this leg's round_trip carries a cost the C legs do not have, and the
//     derived read line is where it shows; it is a property of the
//     language's value model, not of the form.
//   - THE TYPED-DATA VIEWS. Every Save and every Load takes a ByteData view
//     over the caller's Uint8List for its little-endian stores and loads (the
//     image has a view of its own, made once with the plan). That is one
//     small allocation per call — per 64 records here — and every multi-byte
//     access goes through the view's endian-explicit accessor rather than a
//     raw store.
//   - THE SINK. Dart has no empty-asm memory clobber, so the escape barrier
//     reads one written byte back per call (see the write loop) and the
//     accumulated sink is published at exit under an environment variable
//     the compiler cannot rule out, as bench/dart/main.dart does.
//
// THE PLAN READ IS NOT A SEPARATE ROW. The read is ONE loop over ONE plan —
// the identity plan when the file's layout hash is this build's own, a plan
// compiled from the writer's layout otherwise — and the generated module says
// why there is no second reader to keep honest against the first. The paired
// corpus is written under this build's own layout, so the read measured here
// is the identity plan, which is what the C++, java and js legs measure too.
//
// It follows the same contract as every other leg (BENCH-STANDARD.md): the
// committed corpus drives it, the gates run before the clock, a failing gate
// emits NO rows, and the report is 1 warmup + 7 measured runs with the median
// beside min/max/spread — `--round K` drops that to one warmup and one
// measured run so the driver aggregates across rounds itself.
//
// THIS FILE IS SHAPE-BLIND. It names the generated type and its generated
// Save/Load at those call sites and nothing else: no field, no pinned value,
// no wire size. `make shape-gate` holds that mechanically and
// bench/tables/dart/SHAPE-GATE.allow carries the exact count.
//
// Output: a human table on stderr; with --csv, CSV v2 rows on stdout.
//
// Run from the repository root (the paired driver does). Imports are
// library-relative and compiled in, so the working directory only ever names
// the corpus.
import 'dart:io';
import 'dart:typed_data';

import '../../../generated/bench/paired/dart/BenchFixed.dart'
    show FixedTable, TableFixedReport;
import '../../../generated/bench/paired/dart/FixedTableFixed.dart'
    show
        fixedTableFixedLayout,
        fixedTableFixedLoad,
        fixedTableFixedMeasure,
        fixedTableFixedNewPlan,
        fixedTableFixedSave;

const int maxNumRuns = 7; // median of 7 (N >= 5), after 1 warmup run
const int numVariants = 64; // the corpus is 64 records, and an OP is one record
const int defaultIterations = 400000; // the tables bench's standard table count

int gNumRuns = maxNumRuns; // --round K drops this to 1 (§2.4)
int gIterations = 0; // explicit diagnostic count; the default stays standard
bool gCsv = false;
bool gGate = false;
bool gIndexed = false;
String gWireDir = 'testdata/wire';
String gVariantDir = 'bench/corpus/variants';
bool failed = false;

final Stopwatch _clock = Stopwatch()..start();

double now() => _clock.elapsedMicroseconds * 1e-6;

// ---- CSV v2 (BENCH-STANDARD.md §5.1) ----
// family `table` (§1.9): a different wire over a different corpus, so a tools
// refusal to divide it against a `gen` row is correct and automatic.
// linkage aot — the generated table codec is compiled with this caller into
// one whole-program AOT executable, no library boundary, which is the packet
// Dart leg's word for the same thing. checks contract — the codec's caller-
// error asserts are dormant in the AOT product form and its reader's
// wire-contract validation is unconditional, which is §3.4's word for exactly
// this; it is also what the paired driver requires of every table leg. opt
// default — Dart has no operator-visible optimization level. inline unknown —
// the §4.1 verdict pass has no dart branch.
//
// NO `codec` COLUMN: the fixed form has one codec, so there is nothing to name
// and the row is the plain seventeen.
const String csvSuffix = 'aot,contract,default,unknown';
final List<String> gCsvRows = [];
final Map<String, Uint8List> gGoldensLoaded = {}; // basename -> bytes

int gSink = 0; // defeats dead code elimination of computed values
int gProbe = 0; // the rotating byte the write loop observes; see the loops

// §1.6: FNV-1a-64 over the goldens THIS RUN LOADED — for each file in sorted
// basename order, the basename bytes, a 0x00 byte, then the contents. The
// paired driver computes the same hash over the same five files and refuses a
// row whose id does not match it. Dart's int is 64 bits and wraps two's
// complement, which IS arithmetic mod 2^64.
String corpusId() {
  var h = 0xcbf29ce484222325;
  void mix(int byte) {
    h = (h ^ byte) * 0x100000001b3;
  }

  final names = gGoldensLoaded.keys.toList()..sort();
  for (final name in names) {
    for (final b in name.codeUnits) {
      mix(b);
    }
    mix(0);
    for (final b in gGoldensLoaded[name]!) {
      mix(b);
    }
  }
  // a native int is signed, and toRadixString would print a negative id:
  // format the two logical-shifted 32-bit halves instead
  final hi = (h >>> 32) & 0xffffffff;
  final lo = h & 0xffffffff;
  return hi.toRadixString(16).padLeft(8, '0') +
      lo.toRadixString(16).padLeft(8, '0');
}

void flushCsv() {
  if (!gCsv) {
    return;
  }
  if (failed) {
    // §1.5: a failing run emits NO rows. Numbers from a run whose gate
    // refused are not numbers.
    stderr.write('refusing to emit CSV rows from a failing run\n');
    return;
  }
  final id = corpusId();
  final out = StringBuffer(
    'lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,'
    'max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline\n',
  );
  for (final row in gCsvRows) {
    out.write('$row,$id,table,$csvSuffix\n');
  }
  stdout.write(out.toString());
}

void fail(String name, String what) {
  stderr.write('FAILED: $name: $what\n');
  failed = true;
}

Uint8List? readCorpus(String dir, String basename) {
  final path = '$dir/$basename';
  Uint8List bytes;
  try {
    bytes = File(path).readAsBytesSync();
  } on FileSystemException {
    stderr.write(
      'missing corpus $path — run from the schema repo root '
      '(or pass --wire-dir/--variant-dir)\n',
    );
    return null;
  }
  gGoldensLoaded[basename] = bytes;
  return bytes;
}

bool bytesEqual(Uint8List a, Uint8List b) {
  if (a.length != b.length) {
    return false;
  }
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) {
      return false;
    }
  }
  return true;
}

final class Stats {
  final double median;
  final double min;
  final double max;
  final double spread;
  Stats(this.median, this.min, this.max, this.spread);
}

Stats stats(List<double> rates) {
  final sorted = List<double>.from(rates)..sort();
  final n = sorted.length;
  final median = sorted[n ~/ 2];
  return Stats(
    median,
    sorted.first,
    sorted.last,
    (sorted.last - sorted.first) / median * 100.0,
  );
}

void report(String bench, String path, int iters, double bytesPerOp, Stats s) {
  final mbps = s.median * bytesPerOp / (1024.0 * 1024.0);
  stderr.write(
    '${bench.padRight(18)} ${path.padRight(11)} '
    '${(s.median / 1e6).toStringAsFixed(3).padLeft(10)} M msg/s '
    '${mbps.toStringAsFixed(1).padLeft(10)} MB/s   '
    '(min ${(s.min / 1e6).toStringAsFixed(3)}, '
    'max ${(s.max / 1e6).toStringAsFixed(3)}, '
    'spread ${s.spread.toStringAsFixed(1)}%)\n',
  );
  if (gCsv) {
    gCsvRows.add(
      'dart,$bench,$path,$iters,$bytesPerOp,$gNumRuns,'
      '${s.median.toStringAsFixed(0)},${s.min.toStringAsFixed(0)},'
      '${s.max.toStringAsFixed(0)},'
      '${mbps.toStringAsFixed(2)},${s.spread.toStringAsFixed(2)}',
    );
  }
}

// THE TOLERANT CORPUS, read for what this leg can honestly say about it.
//
// The corpus is ONE corpus (docs/SPEC-TABLES.md §3.4): the same sixty-four
// logical records ride both forms, and the corpus id is over all five files,
// which is what makes a fixed-form row and a tolerant-form row answerable to
// each other and to one packet row. So this leg loads all five. It has no
// form-1 decoder to gate the tolerant three with, and it does not pretend to:
// what it checks is the FRAMING, which is arithmetic over the length index and
// needs no codec —
//
//   - the index is 64 little-endian uint32 lengths,
//   - they sum to exactly the concatenation's size, none of them zero,
//   - and variant 0 IS the pinned instance, byte for byte.
//
// A leg that skipped these files would report a corpus id the paired driver
// could not match, which is §1.6 doing its job rather than a workaround.
bool checkTolerantCorpus(String name, String golden) {
  final packed = readCorpus(gVariantDir, '$name.variants.bin');
  final pinned = readCorpus(gWireDir, '$golden.bin');
  if (packed == null || pinned == null) {
    failed = true;
    return false;
  }
  final lengths = List<int>.filled(numVariants, 0);
  if (gIndexed) {
    final index = readCorpus(gVariantDir, '$name.lengths');
    if (index == null || index.length != numVariants * 4) {
      fail(name, 'the length index is not 64 little-endian uint32 lengths');
      return false;
    }
    final view = ByteData.sublistView(index);
    for (var k = 0; k < numVariants; k++) {
      lengths[k] = view.getUint32(k * 4, Endian.little);
    }
  } else {
    // the historical fixed-stride format, retained exactly as the C++ leg
    // retains it
    if (packed.isEmpty || packed.length % numVariants != 0) {
      fail(name, 'the unindexed corpus is not 64 equal records');
      return false;
    }
    lengths.fillRange(0, numVariants, packed.length ~/ numVariants);
  }
  var offset = 0;
  for (var k = 0; k < numVariants; k++) {
    if (lengths[k] <= 0 || offset + lengths[k] > packed.length) {
      fail(name, "variant $k's length does not fit the corpus");
      return false;
    }
    offset += lengths[k];
  }
  if (offset != packed.length) {
    fail(name, 'the lengths do not sum to the corpus size');
    return false;
  }
  if (!bytesEqual(pinned, Uint8List.sublistView(packed, 0, lengths[0]))) {
    stderr.write(
      'WIRE GOLDEN MISMATCH: $golden (${pinned.length} golden vs '
      '${lengths[0]} corpus bytes) — refusing to bench against a corpus '
      'whose variant 0 is not the pinned instance\n',
    );
    failed = true;
    return false;
  }
  return true;
}

// ------------------------------------------------------------------------
// THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4)
// ------------------------------------------------------------------------
//
// A FILE is the unit here and not a record: the layout rides once, the
// records follow it to the end, and every one of them is the same size — so
// the write is one call for all 64 and the read is one call back, and an OP
// is one RECORD of that call.
//
// The gates before the clock are the C++ leg's four, in the same order and
// for the same reasons. The first is the one this form adds: THE LAYOUT THIS
// BUILD EMITS IS THE CORPUS'S LAYOUT, byte for byte. Every other fact about
// the form — the positions, the ids, the kinds, the record's size, the hash
// every record carries — is settled by those bytes, so it is checked first
// and by itself, and a leg that matches it is speaking the form and not a
// near miss.
void benchFixed(String name, int baseIters) {
  final iters = gIterations != 0 ? gIterations : baseIters;
  final file = readCorpus(gVariantDir, 'bench_fixed.bin');
  final layout = readCorpus(gVariantDir, 'bench_fixed.layout');
  if (file == null || layout == null || file.isEmpty) {
    failed = true;
    return;
  }
  final bytesPerOp = file.length / numVariants;
  final twin = Uint8List(file.length);

  // gate 1: THE LAYOUT IS THE CORPUS'S LAYOUT.
  if (!bytesEqual(layout, fixedTableFixedLayout)) {
    fail(name, "this build's layout is not the corpus's, byte for byte");
    return;
  }

  // The measured shape, named once — the generated type at the point this
  // driver declares its storage and hands it to the generated Save/Load, and
  // nothing else about it (bench/tables/dart/SHAPE-GATE.allow).
  final values = List.generate(numVariants, (_) => FixedTable());
  final out = List.generate(numVariants, (_) => FixedTable());
  final plan = fixedTableFixedNewPlan();
  final outPlan = fixedTableFixedNewPlan();
  final report_ = TableFixedReport();

  // gate 2: the whole file loads clean.
  if (fixedTableFixedLoad(
        values,
        numVariants,
        file,
        file.length,
        plan,
        report_,
      ) !=
      numVariants) {
    fail(name, 'the fixed corpus did not load (refusal ${report_.refused})');
    return;
  }

  // gate 3: writing them back reproduces the file, byte for byte.
  if (fixedTableFixedSave(values, numVariants, twin) != file.length ||
      !bytesEqual(twin, file)) {
    fail(
      name,
      'round-trip bytes differ — refusing to bench a codec that does not '
      'reproduce the corpus',
    );
    return;
  }

  // gate 4: reused storage round-trips too. The load owns the prefill, so
  // nothing is reset between the two passes.
  for (var k = 0; k < 2; k++) {
    if (fixedTableFixedLoad(
              out,
              numVariants,
              file,
              file.length,
              outPlan,
              report_,
            ) !=
            numVariants ||
        fixedTableFixedSave(out, numVariants, twin) != file.length ||
        !bytesEqual(twin, file)) {
      fail(name, 'reused target round-trip bytes differ');
      return;
    }
  }

  stderr.write(
    '$name: gates passed before the clock — layout, load, round-trip, '
    'reused-target round-trip (4 of 4)\n',
  );

  if (gGate) {
    return;
  }

  final need = fixedTableFixedMeasure(numVariants);
  final writeRates = List<double>.filled(gNumRuns, 0);
  final roundTripRates = List<double>.filled(gNumRuns, 0);

  // WRITE: one call lays down all 64 records; an op is one record.
  //
  // The escape barrier is the sink, and the sink OBSERVES A WRITTEN BYTE.
  // Dart has no empty-asm memory clobber, and the byte count this form
  // returns is a constant of the record count — a sink fed only by that would
  // let the stores themselves be dead. So the sink reads one byte back out of
  // the buffer per call, at a rotating offset, which is one load per 64
  // records and is the closest thing this runtime has to the clobber.
  for (var run = -1; run < gNumRuns; run++) {
    final start = now();
    for (var i = 0; i < iters; i += numVariants) {
      final wrote = fixedTableFixedSave(values, numVariants, twin);
      if (wrote != file.length) {
        fail(name, 'save failed in loop');
        return;
      }
      gProbe = gProbe + 1 < need ? gProbe + 1 : 0;
      gSink = gSink + wrote + twin[gProbe];
    }
    final elapsed = now() - start;
    if (run >= 0) {
      writeRates[run] = iters / elapsed;
    }
  }

  // ROUND-TRIP: read the file back, then write what came out. The read needs
  // no sink discipline of its own — its output IS the write's input, so every
  // decoded field is observed by construction. This is the arm that carries
  // the object projection: the plan run fills the reader's image and the
  // decode projects it into Dart objects, per record, every time.
  for (var run = -1; run < gNumRuns; run++) {
    final start = now();
    for (var i = 0; i < iters; i += numVariants) {
      if (fixedTableFixedLoad(
            out,
            numVariants,
            file,
            file.length,
            outPlan,
            report_,
          ) !=
          numVariants) {
        fail(name, 'load failed in loop');
        return;
      }
      final wrote = fixedTableFixedSave(out, numVariants, twin);
      if (wrote != file.length) {
        fail(name, 're-save failed in loop');
        return;
      }
      gProbe = gProbe + 1 < need ? gProbe + 1 : 0;
      gSink = gSink + wrote + twin[gProbe];
    }
    final elapsed = now() - start;
    if (run >= 0) {
      roundTripRates[run] = iters / elapsed;
    }
  }

  final w = stats(writeRates);
  final rt = stats(roundTripRates);
  report(name, 'write', iters, bytesPerOp, w);
  report(name, 'round_trip', iters, bytesPerOp, rt);

  // READ is DERIVED, never measured: round-trip time minus write time. It
  // prints for continuity and is NOT a CSV row — a derived number in the CSV
  // would be divided as if it had been measured (§2.9). For this leg it is
  // also where the object projection shows.
  final readTime = 1.0 / rt.median - 1.0 / w.median;
  if (readTime > 0) {
    stderr.write(
      '${name.padRight(18)} ${'read'.padRight(11)} '
      '${(1e-6 / readTime).toStringAsFixed(3).padLeft(10)} M msg/s   '
      '(DERIVED: round-trip minus write, informational — not a measured row)\n',
    );
  }
}

Never usage() {
  stderr.write(
    'usage: table-dart [--indexed] [--gate] [--csv] [--round K] '
    '[--iterations N] [--wire-dir <dir>] [--variant-dir <dir>]\n',
  );
  exit(1);
}

void main(List<String> args) {
  for (var i = 0; i < args.length; i++) {
    final a = args[i];
    if (a == '--indexed') {
      gIndexed = true;
    } else if (a == '--gate') {
      gGate = true;
    } else if (a == '--csv') {
      gCsv = true;
    } else if (a == '--wire-dir' && i + 1 < args.length) {
      gWireDir = args[++i];
    } else if (a == '--variant-dir' && i + 1 < args.length) {
      gVariantDir = args[++i];
    } else if (a == '--iterations' && i + 1 < args.length) {
      final n = int.tryParse(args[++i]);
      if (n == null || n <= 0 || n > 2147483584 || n % numVariants != 0) {
        stderr.write(
          '--iterations requires a positive multiple of 64 up to 2147483584\n',
        );
        exit(1);
      }
      gIterations = n;
    } else if (a == '--round' && i + 1 < args.length) {
      // §2.4: one warmup + one measured run, then exit. K only identifies the
      // round to the interleaved driver, which aggregates across rounds itself.
      final k = int.tryParse(args[++i]);
      if (k == null || k < 0) {
        stderr.write(
          "--round takes a non-negative integer, got '${args[i]}'\n",
        );
        exit(1);
      }
      gNumRuns = 1;
    } else {
      usage();
    }
  }

  stderr.write(
    'schema tables bench (dart ${Platform.version.split(' ').first}, '
    'the FIXED FORM)\n',
  );

  // The tolerant half's bytes, and every check this leg can make of them
  // without a form-1 decoder it does not have (schema#514).
  if (checkTolerantCorpus('bench_table', 'bench_table')) {
    benchFixed('bench_fixed', defaultIterations);
  }

  flushCsv(); // rows carry the corpus_id of the goldens this run loaded

  if (failed) {
    stderr.write('TABLES BENCH FAILED (corpus_id ${corpusId()})\n');
    exit(1);
  }
  stderr.write(
    'OK (corpus_id ${corpusId()}, sink ${gSink == 0 ? '0' : 'nonzero'})\n',
  );

  // the g_sink escape: the compiler cannot prove the env var absent, so the
  // accumulated sink is observable and no loop's work can be deleted
  if (Platform.environment['SERIALIZE_BENCH_SINK'] != null) {
    stderr.write('sink: $gSink\n');
  }
}
