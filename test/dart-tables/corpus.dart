// THE DART TABLES CORPUS, shared by the soak and the allocation gate.
//
// Every buffer, every value, every reader and every writer is built HERE, once,
// so a steady phase owns nothing it has to make. The records are the
// conformance corpus's own instances of the `tabledemo` unit, each with the
// wire golden it must reproduce and the §16 text it must read and write.
//
// A case's verbs are closures BOUND TO THE VALUE — `(r) => value.loadBody(r)`
// — rather than a generic wrapper that casts an Object back to its class on
// every call: the steady phase calls exactly what a consumer's hot loop calls,
// and nothing between the loop and the codec can allocate on its behalf.
import 'dart:io';
import 'dart:typed_data';

import '../../build/tables-generated-dart/examples/TabledemoTable.dart';
import '../../build/tables-generated-dart/examples/KeyedTable.dart' as demo;
import '../../build/tables-generated-dart/examples/NestedTable.dart' as demo;
import '../../build/tables-generated-dart/examples/TablesTable.dart' as demo;
import '../../build/tables-generated-dart/examples/WideTable.dart' as demo;

class Case {
  final String name;
  final Uint8List wire;
  final Uint8List text;
  final Uint8List scratch;
  final bool Function(TableReader) loadBody;
  final int Function() measure;
  final bool Function(TableWriter) saveBody;
  final bool Function(Uint8List, TableReport) fromJson;
  final int Function() toJsonMeasure;
  final int Function(Uint8List) toJson;

  Case(
    String name,
    Uint8List wire,
    Uint8List text,
    bool Function(TableReader) loadBody,
    int Function() measure,
    bool Function(TableWriter) saveBody,
    bool Function(Uint8List, TableReport) fromJson,
    int Function() toJsonMeasure,
    int Function(Uint8List) toJson,
  ) : this._(
        name,
        wire,
        text,
        Uint8List(1 << 20),
        loadBody,
        measure,
        saveBody,
        fromJson,
        toJsonMeasure,
        toJson,
      );

  Case._(
    this.name,
    this.wire,
    this.text,
    this.scratch,
    this.loadBody,
    this.measure,
    this.saveBody,
    this.fromJson,
    this.toJsonMeasure,
    this.toJson,
  );
}

Uint8List read(String path) => File(path).readAsBytesSync();

List<Case> corpus() {
  const dir = 'testdata/wire/tables';
  const json = 'testdata/conformance/tables/json';
  Case root(String name, demo.RootConfig v) => Case(
    name,
    read('$dir/$name.bin'),
    read('$json/$name.json'),
    (r) => v.loadBody(r),
    () => v.measure(),
    (w) => v.saveBody(w),
    (b, rep) => v.fromJson(b, rep),
    () => v.toJsonMeasure(),
    (b) => v.toJson(b),
  );
  Case profile(String name, demo.ProfileConfig v) => Case(
    name,
    read('$dir/$name.bin'),
    read('$json/$name.json'),
    (r) => v.loadBody(r),
    () => v.measure(),
    (w) => v.saveBody(w),
    (b, rep) => v.fromJson(b, rep),
    () => v.toJsonMeasure(),
    (b) => v.toJson(b),
  );
  Case loadout(String name, demo.LoadoutConfig v) => Case(
    name,
    read('$dir/$name.bin'),
    read('$json/$name.json'),
    (r) => v.loadBody(r),
    () => v.measure(),
    (w) => v.saveBody(w),
    (b, rep) => v.fromJson(b, rep),
    () => v.toJsonMeasure(),
    (b) => v.toJson(b),
  );
  Case wide(String name, demo.WideBlob v) => Case(
    name,
    read('$dir/$name.bin'),
    read('$json/$name.json'),
    (r) => v.loadBody(r),
    () => v.measure(),
    (w) => v.saveBody(w),
    (b, rep) => v.fromJson(b, rep),
    () => v.toJsonMeasure(),
    (b) => v.toJson(b),
  );
  Case archive(String name, demo.ArchiveConfig v) => Case(
    name,
    read('$dir/$name.bin'),
    read('$json/$name.json'),
    (r) => v.loadBody(r),
    () => v.measure(),
    (w) => v.saveBody(w),
    (b, rep) => v.fromJson(b, rep),
    () => v.toJsonMeasure(),
    (b) => v.toJson(b),
  );
  Case keyed(String name, demo.KeyedConfig v) => Case(
    name,
    read('$dir/$name.bin'),
    read('$json/$name.json'),
    (r) => v.loadBody(r),
    () => v.measure(),
    (w) => v.saveBody(w),
    (b, rep) => v.fromJson(b, rep),
    () => v.toJsonMeasure(),
    (b) => v.toJson(b),
  );
  return <Case>[
    root('root_full', demo.RootConfig()),
    root('root_default', demo.RootConfig()),
    profile('profile_elide', demo.ProfileConfig()),
    loadout('loadout_full', demo.LoadoutConfig()),
    wide('wide_blob', demo.WideBlob()),
    archive('archive', demo.ArchiveConfig()),
    keyed('keyed_config', demo.KeyedConfig()),
    keyed('keyed_default', demo.KeyedConfig()),
  ];
}

// the caller-owned instruments: one reader, one writer, one report, for a whole
// run — the shape a Dart consumer in a hot loop takes, and the one whose floor
// is zero
final TableReport report = TableReport();
final TableReader reader = TableReader(Uint8List(0), report);
final TableWriter writer = TableWriter(Uint8List(0));

bool same(Uint8List a, Uint8List b, int n) {
  if (b.length < n) {
    return false;
  }
  for (var i = 0; i < n; i++) {
    if (a[i] != b[i]) {
      return false;
    }
  }
  return true;
}
