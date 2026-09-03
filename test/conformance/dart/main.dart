// THE DART CONFORMANCE DRIVER (test/conformance/README.md).
//
// The twin of test/conformance/cpp/main.cpp and of the C# one, and it is
// deliberately the same shape: one process per surface, every expectation in
// the data, nothing literal here.
//
//   driver <manifest> list
//   driver <manifest> <surface> <outdir>
//
// The working directory is the repository root, which is the contract: every
// path in the manifest is repo-relative, so this driver never resolves one.
import 'dart:io';
import 'dart:typed_data';

import '../../../build/tables-generated-dart/examples/TabledemoTable.dart'
    as demort;
import '../../../build/tables-generated-dart/p1/Tblp1Table.dart' as p1rt;
import '../../../build/tables-generated-dart/p3/Tblp3Table.dart' as p3rt;
import '../../../build/tables-generated-dart/v1/Tblv1Table.dart' as v1rt;
import '../../../build/tables-generated-dart/v2/Tblv2Table.dart' as v2rt;
import '../../../build/tables-generated-dart/examples/KeyedTable.dart' as demo;
import '../../../build/tables-generated-dart/examples/NestedTable.dart' as demo;
import '../../../build/tables-generated-dart/examples/PackTable.dart' as demo;
import '../../../build/tables-generated-dart/examples/TablesTable.dart' as demo;
import '../../../build/tables-generated-dart/examples/WideTable.dart' as demo;
import '../../../build/tables-generated-dart/p1/P1Table.dart' as p1;
import '../../../build/tables-generated-dart/p3/P3Table.dart' as p3;
import '../../../build/tables-generated-dart/v1/V1Table.dart' as v1;
import '../../../build/tables-generated-dart/v2/V2Table.dart' as v2;
import '../../../build/tables-generated-dart/block/BlockdemoBlock.dart';
import '../../../build/tables-generated-dart/block/PaddedBlock.dart' as blk;
import '../../../build/tables-generated-dart/block/RenderBlock.dart' as blk;
import '../../../build/tables-generated-dart/pointers/GraphdemoCook.dart';
import '../../../build/tables-generated-dart/pointers/GraphCook.dart' as ck;

// ---- the manifest, exactly as testdata/conformance/tables/FORMAT.md states it

final List<List<String>> lines = [];

void readManifest(String path) {
  for (final raw in File(path).readAsLinesSync()) {
    final text = raw.trim();
    if (text.isEmpty || text.startsWith('#')) {
      continue;
    }
    lines.add(text.split(RegExp(r'[ \t]+')));
  }
}

Iterable<List<String>> kind(String want) => lines.where((f) => f[0] == want);

// ---- the codec table: one row per (unit, root) the corpus names
//
// Every row is the same four calls whatever the unit — make, load, measure,
// save — so the table is closures over the generated name-first surface and
// nothing else.
//
// Each unit carries ITS OWN TableReport, because each unit carries its own
// runtime home: five identical shapes Dart has no structural typing to unify,
// exactly as the C# leg found. So the driver holds one report type and every
// row copies into it — five counters is the whole of §4.

class Report {
  int unknown = 0;
  int kindMismatch = 0;
  int clamped = 0;
  int duplicate = 0;
  bool malformed = false;
}

class Codec {
  final String unit;
  final String root;
  final Object Function() make;
  final bool Function(Object, Uint8List, Report) load;
  final int Function(Object) measure;
  final int Function(Object, Uint8List) save;
  // the TEXT form (docs/SPEC-TABLES.md §16), the same three per row
  final bool Function(Object, Uint8List, Report) fromJson;
  final int Function(Object) toJsonMeasure;
  final int Function(Object, Uint8List) toJson;

  const Codec(
    this.unit,
    this.root,
    this.make,
    this.load,
    this.measure,
    this.save,
    this.fromJson,
    this.toJsonMeasure,
    this.toJson,
  );
}

Codec row<T extends Object, R extends Object>(
  String unit,
  String root,
  T Function() make,
  R Function() makeReport,
  void Function(R, Report) copy,
  bool Function(T, Uint8List, R) load,
  int Function(T) measure,
  int Function(T, Uint8List) save,
  bool Function(T, Uint8List, R) fromJson,
  int Function(T) toJsonMeasure,
  int Function(T, Uint8List) toJson,
) => Codec(
  unit,
  root,
  make,
  (v, b, out) {
    final inner = makeReport();
    final ok = load(v as T, b, inner);
    copy(inner, out);
    return ok;
  },
  (v) => measure(v as T),
  (v, b) => save(v as T, b),
  (v, b, out) {
    final inner = makeReport();
    final ok = fromJson(v as T, b, inner);
    copy(inner, out);
    return ok;
  },
  (v) => toJsonMeasure(v as T),
  (v, b) => toJson(v as T, b),
);

void copyDemo(demort.TableReport r, Report out) {
  out.unknown = r.unknown;
  out.kindMismatch = r.kindMismatch;
  out.clamped = r.clamped;
  out.duplicate = r.duplicate;
  out.malformed = r.malformed;
}

void copyV1(v1rt.TableReport r, Report out) {
  out.unknown = r.unknown;
  out.kindMismatch = r.kindMismatch;
  out.clamped = r.clamped;
  out.duplicate = r.duplicate;
  out.malformed = r.malformed;
}

void copyV2(v2rt.TableReport r, Report out) {
  out.unknown = r.unknown;
  out.kindMismatch = r.kindMismatch;
  out.clamped = r.clamped;
  out.duplicate = r.duplicate;
  out.malformed = r.malformed;
}

void copyP1(p1rt.TableReport r, Report out) {
  out.unknown = r.unknown;
  out.kindMismatch = r.kindMismatch;
  out.clamped = r.clamped;
  out.duplicate = r.duplicate;
  out.malformed = r.malformed;
}

void copyP3(p3rt.TableReport r, Report out) {
  out.unknown = r.unknown;
  out.kindMismatch = r.kindMismatch;
  out.clamped = r.clamped;
  out.duplicate = r.duplicate;
  out.malformed = r.malformed;
}

Codec demoRow<T extends Object>(
  String root,
  T Function() make,
  bool Function(T, Uint8List, demort.TableReport) load,
  int Function(T) measure,
  int Function(T, Uint8List) save,
  bool Function(T, Uint8List, demort.TableReport) fromJson,
  int Function(T) toJsonMeasure,
  int Function(T, Uint8List) toJson,
) => row(
  'tabledemo',
  root,
  make,
  demort.TableReport.new,
  copyDemo,
  load,
  measure,
  save,
  fromJson,
  toJsonMeasure,
  toJson,
);

final List<Codec> codecs = [
  demoRow(
    'RootConfig',
    demo.RootConfig.new,
    (v, b, r) => v.load(b, r),
    (v) => v.measure(),
    (v, b) => v.save(b),
    (v, b, r) => v.fromJson(b, r),
    (v) => v.toJsonMeasure(),
    (v, b) => v.toJson(b),
  ),
  demoRow(
    'ProfileConfig',
    demo.ProfileConfig.new,
    (v, b, r) => v.load(b, r),
    (v) => v.measure(),
    (v, b) => v.save(b),
    (v, b, r) => v.fromJson(b, r),
    (v) => v.toJsonMeasure(),
    (v, b) => v.toJson(b),
  ),
  demoRow(
    'LoadoutConfig',
    demo.LoadoutConfig.new,
    (v, b, r) => v.load(b, r),
    (v) => v.measure(),
    (v, b) => v.save(b),
    (v, b, r) => v.fromJson(b, r),
    (v) => v.toJsonMeasure(),
    (v, b) => v.toJson(b),
  ),
  demoRow(
    'WideBlob',
    demo.WideBlob.new,
    (v, b, r) => v.load(b, r),
    (v) => v.measure(),
    (v, b) => v.save(b),
    (v, b, r) => v.fromJson(b, r),
    (v) => v.toJsonMeasure(),
    (v, b) => v.toJson(b),
  ),
  demoRow(
    'ArchiveConfig',
    demo.ArchiveConfig.new,
    (v, b, r) => v.load(b, r),
    (v) => v.measure(),
    (v, b) => v.save(b),
    (v, b, r) => v.fromJson(b, r),
    (v) => v.toJsonMeasure(),
    (v, b) => v.toJson(b),
  ),
  demoRow(
    'KeyedConfig',
    demo.KeyedConfig.new,
    (v, b, r) => v.load(b, r),
    (v) => v.measure(),
    (v, b) => v.save(b),
    (v, b, r) => v.fromJson(b, r),
    (v) => v.toJsonMeasure(),
    (v, b) => v.toJson(b),
  ),
  demoRow(
    'PackConfig',
    demo.PackConfig.new,
    (v, b, r) => v.load(b, r),
    (v) => v.measure(),
    (v, b) => v.save(b),
    (v, b, r) => v.fromJson(b, r),
    (v) => v.toJsonMeasure(),
    (v, b) => v.toJson(b),
  ),
  row(
    'tblv1',
    'Cfg',
    v1.Cfg.new,
    v1rt.TableReport.new,
    copyV1,
    (v, b, r) => v.load(b, r),
    (v) => v.measure(),
    (v, b) => v.save(b),
    (v, b, r) => v.fromJson(b, r),
    (v) => v.toJsonMeasure(),
    (v, b) => v.toJson(b),
  ),
  row(
    'tblv2',
    'Cfg',
    v2.Cfg.new,
    v2rt.TableReport.new,
    copyV2,
    (v, b, r) => v.load(b, r),
    (v) => v.measure(),
    (v, b) => v.save(b),
    (v, b, r) => v.fromJson(b, r),
    (v) => v.toJsonMeasure(),
    (v, b) => v.toJson(b),
  ),
  row(
    'tblp1',
    'Chain',
    p1.Chain.new,
    p1rt.TableReport.new,
    copyP1,
    (v, b, r) => v.load(b, r),
    (v) => v.measure(),
    (v, b) => v.save(b),
    (v, b, r) => v.fromJson(b, r),
    (v) => v.toJsonMeasure(),
    (v, b) => v.toJson(b),
  ),
  row(
    'tblp3',
    'Chain',
    p3.Chain.new,
    p3rt.TableReport.new,
    copyP3,
    (v, b, r) => v.load(b, r),
    (v) => v.measure(),
    (v, b) => v.save(b),
    (v, b, r) => v.fromJson(b, r),
    (v) => v.toJsonMeasure(),
    (v, b) => v.toJson(b),
  ),
];

Codec? find(String unit, String root) {
  for (final c in codecs) {
    if (c.unit == unit && c.root == root) {
      return c;
    }
  }
  return null;
}

// ---- the surfaces

String reportLine(Report r, bool malformed) =>
    '${r.unknown},${r.kindMismatch},${r.clamped},${r.duplicate},'
    '${malformed ? 'true' : 'false'}\n';

// absent says this backend cannot answer THIS CASE — a feature it lacks, not a
// test it failed: `<case>.absent`, an empty file beside where the answer would
// go, which the harness counts and the matrix prints beside what the leg did
// answer (test/conformance/README.md).
void absent(String outDir, String name) {
  File('$outDir/$name.absent').writeAsBytesSync(const []);
}

// noText marks an instance the corpus carries on the WIRE only — past the text
// form's depth cap by the form's own rule (docs/SPEC-TABLES.md §16.7) — so no
// leg is asked for its text.
bool noText(List<String> f) => f.length > 5 && f[5] == 'no-text';

int surfaceWire(String outDir) {
  for (final f in kind('instance')) {
    final codec = find(f[2], f[3]);
    if (codec == null) {
      // no codec for this unit's root: the Dart backend refuses a pointered
      // unit's wire by name (§11), which is a missing FEATURE
      absent(outDir, f[1]);
      continue;
    }
    final wire = File(f[4]).readAsBytesSync();
    final report = Report();
    final value = codec.make();
    if (!codec.load(value, wire, report)) {
      stderr.writeln('driver: ${f[1]} does not load');
      return 1;
    }
    final size = codec.measure(value);
    if (size < 0) {
      stderr.writeln(
        'driver: ${f[1]} measures as unsaveable after a clean read',
      );
      return 1;
    }
    final buffer = Uint8List(size);
    if (codec.save(value, buffer) != size) {
      stderr.writeln('driver: ${f[1]} saves a size its measure did not name');
      return 1;
    }
    File('$outDir/${f[1]}').writeAsBytesSync(buffer);
  }
  return 0;
}

int surfaceReport(String outDir) {
  for (final f in kind('report')) {
    final codec = find(f[2], f[3]);
    if (codec == null) {
      absent(outDir, f[1]);
      continue;
    }
    final wire = File(f[4]).readAsBytesSync();
    final report = Report();
    final value = codec.make();
    final ok = codec.load(value, wire, report);
    File('$outDir/${f[1]}')
        .writeAsStringSync(reportLine(report, report.malformed || !ok));
  }
  return 0;
}

// json-read: the text is the input and the WIRE is the answer, so the pass
// proves the reader against bytes this driver did not write.
int surfaceJsonRead(String outDir) {
  for (final f in kind('instance')) {
    if (noText(f)) {
      continue;
    }
    final codec = find(f[2], f[3]);
    if (codec == null) {
      // the Dart backend refuses a pointered unit's wire by name (§11), so it
      // has no text form for one either and says so per case
      absent(outDir, f[1]);
      continue;
    }
    final path = 'testdata/conformance/tables/json/${f[1]}.json';
    final text = File(path).readAsBytesSync();
    final report = Report();
    final value = codec.make();
    if (!codec.fromJson(value, text, report)) {
      stderr.writeln('driver: ${f[1]} does not read as JSON');
      return 1;
    }
    final size = codec.measure(value);
    if (size < 0) {
      stderr.writeln(
        'driver: ${f[1]} measures as unsaveable after a clean read',
      );
      return 1;
    }
    final buffer = Uint8List(size);
    if (codec.save(value, buffer) != size) {
      stderr.writeln('driver: ${f[1]} saves a size its measure did not name');
      return 1;
    }
    File('$outDir/${f[1]}').writeAsBytesSync(buffer);
  }
  return 0;
}

// json-write: the wire is the input and the TEXT is the answer, compared
// against a text a third implementation wrote.
int surfaceJsonWrite(String outDir) {
  for (final f in kind('instance')) {
    if (noText(f)) {
      continue;
    }
    final codec = find(f[2], f[3]);
    if (codec == null) {
      absent(outDir, '${f[1]}.json');
      continue;
    }
    final wire = File(f[4]).readAsBytesSync();
    final report = Report();
    final value = codec.make();
    if (!codec.load(value, wire, report)) {
      stderr.writeln('driver: ${f[1]} does not load');
      return 1;
    }
    final size = codec.toJsonMeasure(value);
    if (size < 0) {
      stderr.writeln('driver: ${f[1]} holds a value ToJson refuses');
      return 1;
    }
    final text = Uint8List(size);
    if (codec.toJson(value, text) != size) {
      stderr.writeln('driver: ${f[1]} writes a text its measure did not name');
      return 1;
    }
    File('$outDir/${f[1]}.json').writeAsBytesSync(text);
  }
  return 0;
}

// json-hostile: one tree per rule the text form states (§16.2, §16.3, §17.5).
// The answer is the REPORT the read produces, or `refused` — the same
// two-valued verdict the engine's own gate holds, over the same data.
int surfaceJsonHostile(String outDir) {
  for (final f in kind('json-hostile')) {
    final codec = find(f[2], f[3]);
    if (codec == null) {
      absent(outDir, f[1]);
      continue;
    }
    // the tree is what `schema pack` reads, so the text is <tree>/<root>.json
    final text = File('${f[4]}/${f[3]}.json').readAsBytesSync();
    final report = Report();
    final value = codec.make();
    final ok = codec.fromJson(value, text, report);
    final verdict = !ok || report.malformed
        ? 'refused\n'
        : '${report.unknown},${report.kindMismatch},${report.clamped},'
              '${report.duplicate},false\n';
    File('$outDir/${f[1]}').writeAsStringSync(verdict);
  }
  return 0;
}

// ---- the BLOCK form (docs/SPEC-TABLES.md §19) ----
//
// A block's base is 64-byte aligned by construction (§19.1), and `extent` is
// the length the CALLER claims, which a forgery may set past the bytes the
// image carries. THE ALLOCATION IS THE CLAIM, so a reader that walks past what
// it was given walks into a buffer this process owns and nothing else's — the
// same property the C++ and C# legs get from allocating the claim themselves.
//
// The Dart base is (buffer, offset) rather than a pointer, because that is the
// only base this language holds; the driver places it as the pointer column
// says and the reader checks its alignment.
Uint8List blockBuffer(Uint8List image, int extent, int pointer) {
  final claim = extent < 0 || extent < image.length ? image.length : extent;
  final base = pointer < 0 ? 0 : pointer;
  final buffer = Uint8List(base + claim);
  buffer.setRange(base, base + image.length, image);
  return buffer;
}

String openBlock(String name, Uint8List image, int extent, int pointer) {
  if (pointer < 0) {
    return 'refuse\n'; // no buffer at all
  }
  final claim = extent < 0 || extent < image.length ? image.length : extent;
  final buffer = blockBuffer(image, extent, pointer);
  final Object? block = name.startsWith('block_render')
      ? blk.RenderFrameBlock.open(buffer, pointer, claim)
      : name.startsWith('block_padded')
      ? blk.PaddedFrameBlock.open(buffer, pointer, claim)
      : throw StateError('driver: no block named $name');
  return block == null ? 'refuse\n' : 'open\n';
}

int surfaceBlock(String outDir) {
  for (final f in kind('block')) {
    final image = File(f[3]).readAsBytesSync();
    File('$outDir/${f[1]}').writeAsStringSync(openBlock(f[1], image, -1, 0));
  }
  return 0;
}

// foreign returns the image with its MAGIC word — the eight bytes at offset 0
// — reversed, which is what that word looks like to a reader of the other byte
// order (docs/SPEC-TABLES.md §19.1, §7.1).
//
// It makes the file foreign to WHOEVER READS IT rather than to a particular
// host: whatever this build's order is, the magic it now reads is not this
// build's, so the refusal lands on the magic check every open puts first. That
// is the only shape a cross-endian expectation can take without depending on
// the host it runs on.
Uint8List foreign(Uint8List image) {
  final out = Uint8List.fromList(image);
  if (out.length >= 8) {
    for (var i = 0; i < 4; i++) {
      final swap = out[i];
      out[i] = out[7 - i];
      out[7 - i] = swap;
    }
  }
  return out;
}

int surfaceBlockForeign(String outDir) {
  for (final f in kind('block')) {
    final image = foreign(File(f[3]).readAsBytesSync());
    File('$outDir/${f[1]}').writeAsStringSync(openBlock(f[1], image, -1, 0));
  }
  return 0;
}

int surfaceForgery(String outDir) {
  for (final f in kind('forgery')) {
    if (f[2] != 'block') {
      continue; // the cook's battery is its own
    }
    final image = File(f[4]).readAsBytesSync();
    final extent = int.parse(f[5]);
    final pointer = f[6] == 'null' ? -1 : int.parse(f[6]);
    File('$outDir/${f[1]}')
        .writeAsStringSync(openBlock(f[3], image, extent, pointer));
  }
  return 0;
}

// ---- the BLOCK ROW DUMP (testdata/conformance/tables/FORMAT.md) ----
//
// The twin of the C++ leg's walk, and like it, written against §8's descriptors
// and NOTHING ELSE: no generated row cursor, no field named in this file. That
// is the claim §19.2 makes for the descriptors, and a walk that reached for a
// cursor would be proving something else. A FLOAT is its IEEE-754 bit pattern,
// because a block row is a byte-identical projection and its bits are the fact.

void dumpScalar(StringBuffer into, ByteData view, int at, int kind, int width) {
  switch (kind) {
    case 1:
      into.write(view.getUint8(at) != 0 ? 'true' : 'false');
      return;
    case 10:
      into.write(
        '0x${view.getUint32(at, Endian.little).toRadixString(16).padLeft(8, '0')}',
      );
      return;
    case 11:
      into.write('0x${hex64(view.getUint64(at, Endian.little))}');
      return;
    case 2:
    case 3:
    case 4:
    case 5:
      final v = width == 1
          ? view.getInt8(at)
          : width == 2
          ? view.getInt16(at, Endian.little)
          : width == 4
          ? view.getInt32(at, Endian.little)
          : view.getInt64(at, Endian.little);
      into.write(v.toString());
      return;
    default:
      final v = width == 1
          ? view.getUint8(at)
          : width == 2
          ? view.getUint16(at, Endian.little)
          : width == 4
          ? view.getUint32(at, Endian.little)
          : view.getUint64(at, Endian.little);
      into.write(unsignedText(v));
      return;
  }
}

// a u64 bit pattern held in a signed int renders as its unsigned magnitude
String unsignedText(int v) =>
    v >= 0 ? v.toString() : BigInt.from(v).toUnsigned(64).toString();

String hex64(int v) {
  final high = (v >> 32) & 0xffffffff;
  final low = v & 0xffffffff;
  return high.toRadixString(16).padLeft(8, '0') +
      low.toRadixString(16).padLeft(8, '0');
}

void dumpText(StringBuffer into, Uint8List bytes, int at, int used) {
  if (used < 0) {
    used = 0;
  }
  into.write('"');
  for (var i = 0; i < used; i++) {
    final c = bytes[at + i];
    if (c >= 0x20 && c < 0x7f && c != 0x22 && c != 0x5c) {
      into.writeCharCode(c);
    } else {
      into.write('\\x${c.toRadixString(16).padLeft(2, '0')}');
    }
  }
  into.write('" len=$used');
}

String dumpJoin(String prefix, String name) =>
    prefix.isEmpty ? name : '$prefix.$name';

bool dumpRecord(
  StringBuffer into,
  Uint8List bytes,
  ByteData view,
  int storage,
  TableBlockInfo? info,
  String path,
) {
  if (info == null) {
    stderr.writeln('driver: a descriptor names no record');
    return false;
  }
  for (final f in info.fields) {
    if (f.outOfLine) {
      continue;
    }
    final name = dumpJoin(path, f.name);
    if (f.counted) {
      final used = view.getInt32(storage + f.countOffset, Endian.little);
      if (used < 0 || used > f.arrayBound) {
        stderr.writeln(
          'driver: ${info.name}.${f.name} carries a used length of $used, '
          'outside [ 0, ${f.arrayBound} ]',
        );
        return false;
      }
      into.write('  $name = ');
      dumpText(into, bytes, storage + f.offset, used);
      into.write('\n');
    } else {
      final slots = f.isArray ? f.arrayBound : 1;
      for (var s = 0; s < slots; s++) {
        final at = f.isArray ? '$name[$s]' : name;
        final value = storage + f.offset + s * f.elemSize;
        if (f.element != null) {
          if (!dumpRecord(into, bytes, view, value, f.element, at)) {
            return false;
          }
        } else {
          into.write('  $at = ');
          dumpScalar(into, view, value, f.kind, f.elemSize);
          into.write('\n');
        }
      }
    }
    if (f.optional) {
      final present = view.getUint8(storage + f.presentOffset) != 0;
      into.write('  $name#present = ${present ? 'true' : 'false'}\n');
    }
  }
  return true;
}

bool dumpBlock(
  StringBuffer into,
  Uint8List bytes,
  ByteData view,
  int base,
  TableBlockInfo info,
) {
  into.write('projection ${info.name} @0\n');
  if (!dumpRecord(into, bytes, view, base, info, '')) {
    return false;
  }
  for (final f in info.fields) {
    if (!f.outOfLine) {
      continue;
    }
    final offsetOf = view.getUint64(base + f.offsetOfOffset, Endian.little);
    final count = view.getUint32(base + f.countOffset, Endian.little);
    final stride = view.getUint32(base + f.strideOffset, Endian.little);
    final row = f.element;
    if (row == null) {
      stderr.writeln('driver: ${f.name} names no element');
      return false;
    }
    into.write(
      'array ${f.name} ${row.name} @$offsetOf count=$count stride=$stride\n',
    );
    for (var r = 0; r < count; r++) {
      final at = offsetOf + r * stride;
      into.write('row $r @$at\n');
      if (!dumpRecord(into, bytes, view, base + at, row, '')) {
        return false;
      }
    }
  }
  return true;
}

int surfaceBlockDump(String outDir) {
  for (final f in kind('block')) {
    final image = File(f[3]).readAsBytesSync();
    final buffer = blockBuffer(image, -1, 0);
    final view = ByteData.view(buffer.buffer);
    final into = StringBuffer();
    final bool ok;
    if (f[1].startsWith('block_render')) {
      final block = blk.RenderFrameBlock.open(buffer, 0, image.length);
      ok =
          block != null &&
          dumpBlock(into, buffer, view, 0, blk.RenderFrameBlock.type);
    } else if (f[1].startsWith('block_padded')) {
      final block = blk.PaddedFrameBlock.open(buffer, 0, image.length);
      ok =
          block != null &&
          dumpBlock(into, buffer, view, 0, blk.PaddedFrameBlock.type);
    } else {
      stderr.writeln('driver: no block named ${f[1]}');
      return 1;
    }
    if (!ok) {
      return 1;
    }
    File('$outDir/${f[1]}').writeAsStringSync(into.toString());
  }
  return 0;
}

// ---- the COOKED form (docs/SPEC-TABLES.md §7) ----
//
// cookAlignment is the alignment the header NAMES, which is where a forged
// buffer has to be placed for the base-alignment check to mean anything. A
// forged word that is not an alignment at all puts the buffer at the format's
// own floor instead.
int cookAlignment(Uint8List source) {
  if (source.length < 48) {
    return 8;
  }
  final view = ByteData.view(
    source.buffer,
    source.offsetInBytes,
    source.lengthInBytes,
  );
  final a = view.getUint64(40, Endian.little);
  if (a < 1 || a > 64 || a & (a - 1) != 0) {
    return 8;
  }
  return a;
}

// place is the forgery contract, exactly (testdata/conformance/tables/FORMAT.md):
// the buffer is EXACTLY the extent the caller claims, its base `lead` bytes past
// an aligned offset, what fits copied in and the rest zero. A Dart base is
// (buffer, offset), so the alignment the reader checks is the offset's.
(Uint8List, int, int) place(
  Uint8List data,
  int extent,
  int lead,
  int alignment,
) {
  final bytes = extent < 0 ? data.length : extent;
  final base = lead;
  final buffer = Uint8List(base + bytes);
  final copy = bytes < data.length ? bytes : data.length;
  buffer.setRange(base, base + copy, data);
  return (buffer, base, bytes);
}

Object? openCookAt(String root, Uint8List? buffer, int base, int bytes) {
  switch (root) {
    case 'Scene':
      return ck.SceneCook.open(buffer, base, bytes);
    case 'Depot':
      return ck.DepotCook.open(buffer, base, bytes);
    case 'Album':
      return ck.AlbumCook.open(buffer, base, bytes);
    case 'TreeNode':
      return ck.TreeNodeCook.open(buffer, base, bytes);
    case 'ListNode':
      return ck.ListNodeCook.open(buffer, base, bytes);
  }
  throw StateError('driver: no cook root named $root');
}

TableCookInfo cookType(String root) {
  switch (root) {
    case 'Scene':
      return ck.SceneCook.type;
    case 'Depot':
      return ck.DepotCook.type;
    case 'Album':
      return ck.AlbumCook.type;
    case 'TreeNode':
      return ck.TreeNodeCook.type;
    case 'ListNode':
      return ck.ListNodeCook.type;
  }
  throw StateError('driver: no cook root named $root');
}

// the region a cook was opened over: its first byte and its data length, read
// back through the one accessor pair every root's cook carries
(int, int) cookRegion(Object cook) {
  if (cook is ck.SceneCook) {
    return (cook.region, cook.length);
  }
  if (cook is ck.DepotCook) {
    return (cook.region, cook.length);
  }
  if (cook is ck.AlbumCook) {
    return (cook.region, cook.length);
  }
  if (cook is ck.TreeNodeCook) {
    return (cook.region, cook.length);
  }
  if (cook is ck.ListNodeCook) {
    return (cook.region, cook.length);
  }
  throw StateError('driver: not a cook');
}

String openCookForged(
  String root,
  Uint8List data,
  int extent,
  int lead,
  bool nil,
) {
  final (buffer, base, bytes) = place(data, extent, lead, cookAlignment(data));
  final opened = openCookAt(root, nil ? null : buffer, base, bytes);
  return opened == null ? 'refuse\n' : 'open\n';
}

int surfaceCookForgery(String outDir) {
  for (final f in kind('forgery')) {
    if (f[2] != 'cook') {
      continue; // the block's battery is its own
    }
    final data = File(f[4]).readAsBytesSync();
    final extent = int.parse(f[5]);
    final pointer = f[6] == 'null' ? -1 : int.parse(f[6]);
    File('$outDir/${f[1]}').writeAsStringSync(
      openCookForged(
        f[3],
        data,
        extent,
        pointer < 0 ? 0 : pointer,
        pointer < 0,
      ),
    );
  }
  return 0;
}

int surfaceCookForeign(String outDir) {
  for (final f in kind('cook')) {
    final data = foreign(File(f[4]).readAsBytesSync());
    File('$outDir/${f[1]}')
        .writeAsStringSync(openCookForged(f[3], data, -1, 0, false));
  }
  return 0;
}

// ---- THE COOK'S NODE DUMP (testdata/conformance/tables/FORMAT.md) ----
//
// Every node this side reaches through its OWN derefs, written as canonical
// text so the C++ leg's walk and this one are byte-compared. It is GENERIC over
// the cook descriptors, which is the whole point of them: a pointer slot is
// eight bytes at `offset` holding the SIGNED SELF-RELATIVE delta of §6.3, and a
// delta of zero is null. A by-value nesting is not a node; it is storage inside
// one, and the walk descends through it to reach the pointer slots inside.

class CookWalk {
  final Uint8List bytes;
  final ByteData view;
  final int region;
  final int length;
  final List<int> offsets = <int>[];
  final List<TableCookInfo> types = <TableCookInfo>[];
  final StringBuffer dump = StringBuffer();

  CookWalk(this.bytes, this.view, this.region, this.length);

  int find(int offset) {
    for (var i = 0; i < offsets.length; i++) {
      if (offsets[i] == offset) {
        return i;
      }
    }
    return -1;
  }

  void node(int offset, TableCookInfo type, int depth) {
    if (depth > 4096) {
      throw StateError(
        'the walk nested past any depth a region can hold — '
        'a cycle the deref did not close',
      );
    }
    final at = find(offset);
    if (at >= 0) {
      if (!identical(types[at], type)) {
        throw StateError(
          'two references name the node at offset $offset as two different '
          'tables: ${types[at].name} and ${type.name}',
        );
      }
      // one node, one visit: sharing and a back-reference are the same fact
      return;
    }
    if (offset > length || type.size > length - offset) {
      throw StateError(
        'the node at offset $offset (${type.name}, size ${type.size}) does not '
        'fit inside the region\'s $length bytes',
      );
    }
    final index = offsets.length;
    offsets.add(offset);
    types.add(type);
    dump.write('node $index ${type.name} @$offset\n');
    storage(region + offset, type, '', depth);
  }

  void emit(String path, String value) {
    dump.write('  $path = $value\n');
  }

  void storage(int at, TableCookInfo type, String path, int depth) {
    for (final f in type.fields) {
      final name = path.isEmpty ? f.name : '$path.${f.name}';

      // every COUNT COMPANION, against its declared bound, and a NEGATIVE one
      // refuses too — an extent is never negative, and a walker handed one
      // indexes backwards out of the region (§7.4's pass two)
      var used = -1;
      if (f.countOffset >= 0) {
        used = view.getInt32(at + f.countOffset, Endian.little);
        if (used < 0 || used > f.arrayBound) {
          throw StateError(
            '${type.name}.${f.name} carries a count companion of $used, '
            'outside [ 0, ${f.arrayBound} ]',
          );
        }
      }

      if (f.isPointer) {
        final slot = at + f.offset;
        final delta = view.getInt64(slot, Endian.little);
        if (delta == 0) {
          emit(name, 'null'); // NULL IN A REGION IS A DELTA OF ZERO (§6.3)
          continue;
        }
        final target = slot + delta;
        if (target < region || target >= region + length) {
          throw StateError(
            '${type.name}.${f.name} resolves outside the region — a delta of '
            '$delta from a slot at ${slot - region}',
          );
        }
        final record = f.info;
        if (record == null) {
          throw StateError(
            '${type.name}.${f.name} is a pointer whose descriptor names no record',
          );
        }
        emit(name, '-> @${target - region}');
        node(target - region, record, depth + 1);
        continue;
      }

      switch (f.storage) {
        case TableCookStorage.string:
        case TableCookStorage.bytes:
          final into = StringBuffer();
          dumpText(into, bytes, at + f.offset, used);
          emit(name, into.toString());
        case TableCookStorage.record:
          // a nested record — by value, or every slot of an array of them. A
          // COUNTED array writes all N slots (§7.2), and a slot past the live
          // count holds the value-initialized element, whose pointer slots are
          // zero: walking all of them is what the check does too.
          final slots = f.isArray ? f.arrayBound : 1;
          for (var s = 0; s < slots; s++) {
            final element = f.isArray ? '$name[$s]' : name;
            storage(at + f.offset + s * f.elemSize, f.info!, element, depth);
          }
        default:
          final slots = f.isArray ? f.arrayBound : 1;
          for (var s = 0; s < slots; s++) {
            final element = f.isArray ? '$name[$s]' : name;
            emit(
              element,
              cookScalar(at + f.offset + s * f.elemSize, f.storage, f.elemSize),
            );
          }
      }

      if (f.countOffset >= 0 &&
          f.storage != TableCookStorage.string &&
          f.storage != TableCookStorage.bytes) {
        emit('$name#count', used.toString());
      }
      if (f.presentOffset >= 0) {
        emit(
          '$name#present',
          view.getUint8(at + f.presentOffset) != 0 ? 'true' : 'false',
        );
      }
    }
  }

  String cookScalar(int at, int storage, int width) {
    switch (storage) {
      case TableCookStorage.boolean:
        return view.getUint8(at) != 0 ? 'true' : 'false';
      case TableCookStorage.float:
        // Nothing in the pointered corpus is a float, and a canonical
        // cross-language spelling of one is a decision this gate should not
        // make in passing. The day a float arrives, the gate says so rather
        // than drifting.
        throw StateError(
          'the dump met a float, whose canonical cross-language spelling this '
          'gate does not fix',
        );
      case TableCookStorage.signed:
        switch (width) {
          case 1:
            return view.getInt8(at).toString();
          case 2:
            return view.getInt16(at, Endian.little).toString();
          case 4:
            return view.getInt32(at, Endian.little).toString();
          default:
            return view.getInt64(at, Endian.little).toString();
        }
      default:
        switch (width) {
          case 1:
            return view.getUint8(at).toString();
          case 2:
            return view.getUint16(at, Endian.little).toString();
          case 4:
            return view.getUint32(at, Endian.little).toString();
          default:
            return unsignedText(view.getUint64(at, Endian.little));
        }
    }
  }
}

int surfaceCook(String outDir) {
  for (final f in kind('cook')) {
    final data = File(f[4]).readAsBytesSync();
    final (buffer, base, bytes) = place(data, -1, 0, cookAlignment(data));
    final cook = openCookAt(f[3], buffer, base, bytes);
    if (cook == null) {
      stderr.writeln('driver: ${f[1]} does not open');
      return 1;
    }
    final (region, length) = cookRegion(cook);
    final view = ByteData.view(buffer.buffer);
    final walk = CookWalk(buffer, view, region, length);
    walk.node(0, cookType(f[3]), 0);
    File('$outDir/${f[1]}').writeAsStringSync(walk.dump.toString());
  }
  return 0;
}

const String surfaces =
    'wire\nreport\njson-read\njson-write\njson-hostile\n'
    'cook\ncook-foreign\nblock\nblock-foreign\nblock-dump\n'
    'forgery\ncook-forgery\n';

void main(List<String> args) {
  exit(run(args));
}

int run(List<String> args) {
  if (args.length < 2) {
    stderr.writeln(
      'usage: driver <manifest> list\n'
      '       driver <manifest> <surface> <outdir>',
    );
    return 2;
  }
  readManifest(args[0]);
  final surface = args[1];
  if (surface == 'list') {
    stdout.write(surfaces);
    return 0;
  }
  if (args.length < 3) {
    stderr.writeln('usage: driver <manifest> <surface> <outdir>');
    return 2;
  }
  final out = args[2];
  switch (surface) {
    case 'wire':
      return surfaceWire(out);
    case 'report':
      return surfaceReport(out);
    case 'json-read':
      return surfaceJsonRead(out);
    case 'json-write':
      return surfaceJsonWrite(out);
    case 'json-hostile':
      return surfaceJsonHostile(out);
    case 'cook':
      return surfaceCook(out);
    case 'cook-foreign':
      return surfaceCookForeign(out);
    case 'cook-forgery':
      return surfaceCookForgery(out);
    case 'block':
      return surfaceBlock(out);
    case 'block-foreign':
      return surfaceBlockForeign(out);
    case 'block-dump':
      return surfaceBlockDump(out);
    case 'forgery':
      return surfaceForgery(out);
  }
  return 2;
}
