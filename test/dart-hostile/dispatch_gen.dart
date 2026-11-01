// THE DART HOSTILE RUNNER'S DISPATCH, WRITTEN FROM THE MANIFEST.
//
// Dart binds a reader at compile time, so the runner's text, wire,
// wire_retain and packet carriers reach the generated code through one
// generated file, build/dart-hostile/dispatch.dart: one import per unit and one
// case per (schema, table) the manifest names. A function the generated unit
// does not carry gets no case, and the runner answers that row `unbound`.
//
//   dart run test/dart-hostile/dispatch_gen.dart <corpus-dir> <build-dir>
//
// <build-dir> holds generated/<unit>/ (the table units) and packet/ (the packet
// units make/hostile-packet-units.sh writes, with its units.tsv). The working
// directory is the repository root, so a packet schema's path reads as written.
import 'dart:io';

// the table units, by the schema path the manifest names them with
const Map<String, String> tableUnits = <String, String>{
  'tables/examples': 'tabledemo',
  'tables/messages': 'messagedemo',
  'tables/blobs': 'blobdemo',
  'tables/pointers': 'graphdemo',
  'tables/scalars': 'scalardemo',
  'tables/stream': 'streamdemo',
  'tables/maps': 'mapdemo',
  'tables/lists': 'listdemo',
  'schemas/probe.schema': 'probe',
};

String lower(String s) => s.isEmpty ? s : s[0].toLowerCase() + s.substring(1);

String quote(String s) => "'${s.replaceAll(r'\', r'\\').replaceAll("'", r"\'")}'";

void main(List<String> args) {
  if (args.length != 2) {
    stderr.writeln('usage: dispatch_gen.dart <corpus-dir> <build-dir>');
    exit(2);
  }
  final corpus = args[0], build = args[1];
  final rows = File('$corpus/manifest.tsv')
      .readAsLinesSync()
      .where((l) => l.isNotEmpty && !l.startsWith('#'))
      .map((l) => l.split('\t'))
      .toList();

  final imports = <String>[];
  final prefixes = <String, String>{}; // import path -> prefix
  final sources = <String, String>{}; // import path -> source text
  String prefixFor(String path) => prefixes.putIfAbsent(path, () {
        final p = 'u${prefixes.length}';
        imports.add("import ${quote(path)} as $p;");
        sources[path] = File('$build/$path').readAsStringSync();
        return p;
      });

  String? wireFile(String schema) {
    final unit = tableUnits[schema];
    if (unit == null) return null;
    final dir = Directory('$build/generated/$unit');
    if (!dir.existsSync()) return null;
    for (final f in dir.listSync()) {
      final name = f.uri.pathSegments.last;
      if (name.endsWith('Wire.dart')) return 'generated/$unit/$name';
    }
    return null;
  }

  final packetUnits = <String, String>{};
  final unitsFile = File('$build/packet/units.tsv');
  if (unitsFile.existsSync()) {
    for (final l in unitsFile.readAsLinesSync().where((l) => l.isNotEmpty)) {
      final f = l.split('\t');
      packetUnits[f[0]] = f[1];
    }
  }

  final text = StringBuffer(), wire = StringBuffer(), retain = StringBuffer(), packet = StringBuffer();
  final seen = <String>{};
  for (final m in rows) {
    final carrier = m[1], schema = m[2], table = m[3];
    if (!seen.add('$carrier $schema $table')) continue;
    final key = quote('$schema $table');
    final t = lower(table);
    if (carrier == 'text' || carrier == 'wire' || carrier == 'wire_retain') {
      final path = wireFile(schema);
      if (path == null) continue;
      final p = prefixFor(path);
      final src = sources[path]!;
      bool has(String fn) => src.contains('\n$table $fn(') || src.contains('\nUint8List $fn(') || src.contains('\nint $fn(') || src.contains('\nString $fn(');
      if (!has('${t}ToJson') || !has('${t}WireSave') || !has('${t}WireMeasure')) continue;
      if (carrier == 'text' && has('${t}FromJson')) {
        text.writeln('    case $key:');
        text.writeln('      final v = $p.${t}FromJson(text, $p.TableTextReport());');
        text.writeln('      return Loaded(false, false, \'\', () => $p.${t}ToJson(v), () => $p.${t}WireMeasure($p.${t}WireSave(v)));');
      } else if (carrier == 'wire' && has('${t}WireLoad')) {
        wire.writeln('    case $key:');
        wire.writeln('      final r = $p.TableWireReport();');
        wire.writeln('      final v = $p.${t}WireLoad(data, r);');
        wire.writeln('      return Loaded(r.refused, r.malformed, r.reason, () => $p.${t}ToJson(v), () {');
        wire.writeln('        $p.${t}WireMeasure(data);');
        wire.writeln('        $p.${t}WireMeasure($p.${t}WireSave(v));');
        wire.writeln('      });');
      } else if (carrier == 'wire_retain' && has('${t}WireLoadRetain') && has('${t}WireSaveRetain') && has('${t}WireMeasureRetain')) {
        retain.writeln('    case $key:');
        retain.writeln('      final r = $p.TableWireReport();');
        retain.writeln('      final keep = $p.TableRetain(retainBytes, List<int>.filled(retainBytes ~/ 8, 0));');
        retain.writeln('      final v = $p.${t}WireLoadRetain(data, keep, r);');
        retain.writeln('      return Loaded(r.refused, r.malformed, r.reason, () => $p.${t}ToJson(v), () {');
        retain.writeln('        $p.${t}WireMeasureRetain(v, keep);');
        retain.writeln('        $p.${t}WireSaveRetain(v, keep, $p.TableWireReport());');
        retain.writeln('      });');
      }
    } else if (carrier == 'packet') {
      final unit = packetUnits[schema];
      if (unit == null) continue;
      final base = schema.split('/').last.replaceFirst(RegExp(r'\.schema$'), '');
      final path = 'packet/$unit/$base.dart';
      if (!File('$build/$path').existsSync()) continue;
      final p = prefixFor(path);
      final src = sources[path]!;
      final maxBytes = RegExp(r'^const int (\w+)MaxBytes = ', multiLine: true)
          .allMatches(src)
          .map((x) => x.group(1)!)
          .where((n) => n.toLowerCase() == table.toLowerCase())
          .firstOrNull;
      if (maxBytes == null || !src.contains('\nbool read$table(') || !src.contains('\nint write$table(')) continue;
      packet.writeln('    case $key:');
      packet.writeln('      final v = $p.$table();');
      packet.writeln('      if (!$p.read$table(v, ByteData.sublistView(data), data.length * 8)) return null;');
      packet.writeln('      final out = ByteData((($p.${maxBytes}MaxBytes + 7) ~/ 8) * 8 + 8);');
      packet.writeln('      final n = $p.write$table(v, out);');
      packet.writeln('      return out.buffer.asUint8List(0, n);');
    }
  }

  final out = StringBuffer()
    ..writeln('// Code generated by test/dart-hostile/dispatch_gen.dart from the hostile corpus manifest. DO NOT EDIT.')
    ..writeln('//')
    ..writeln('// The Dart hostile runner\'s bindings: one case per (schema, table) the manifest')
    ..writeln('// names, for the text, wire, wire_retain and packet carriers.')
    ..writeln("import 'dart:typed_data';")
    ..writeln()
    ..writeAll(imports, '\n')
    ..writeln()
    ..writeln()
    ..writeln('// the retaining load\'s byte store: roomy, so the verdict is the load\'s and')
    ..writeln('// never the caller\'s capacity')
    ..writeln('const int retainBytes = 1 << 20;')
    ..writeln()
    ..writeln('// A read\'s outcome: its report, the text writer on the value, and the')
    ..writeln('// Measure and Save the runner runs after an accept.')
    ..writeln('final class Loaded {')
    ..writeln('  final bool refused;')
    ..writeln('  final bool malformed;')
    ..writeln('  final String reason;')
    ..writeln('  final String Function() written;')
    ..writeln('  final void Function() exercise;')
    ..writeln('  const Loaded(this.refused, this.malformed, this.reason, this.written, this.exercise);')
    ..writeln('}')
    ..writeln()
    ..writeln('// null: no case binds this (schema, table)')
    ..writeln('Loaded? textLoad(String schema, String table, String text) {')
    ..writeln(r"  switch ('$schema $table') {")
    ..write(text)
    ..writeln('  }')
    ..writeln('  return null;')
    ..writeln('}')
    ..writeln()
    ..writeln('Loaded? wireLoad(String schema, String table, Uint8List data) {')
    ..writeln(r"  switch ('$schema $table') {")
    ..write(wire)
    ..writeln('  }')
    ..writeln('  return null;')
    ..writeln('}')
    ..writeln()
    ..writeln('Loaded? wireLoadRetain(String schema, String table, Uint8List data) {')
    ..writeln(r"  switch ('$schema $table') {")
    ..write(retain)
    ..writeln('  }')
    ..writeln('  return null;')
    ..writeln('}')
    ..writeln()
    ..writeln('// whether a packet case is bound at all')
    ..writeln('bool packetBound(String schema, String type) => _packetKeys.contains(\'\$schema \$type\');')
    ..writeln()
    ..writeln('const Set<String> _packetKeys = <String>{')
    ..writeAll(rows.where((m) => m[1] == 'packet').map((m) => '${m[2]} ${m[3]}').toSet().where((k) => packet.toString().contains('case ${quote(k)}:')).map((k) => '  ${quote(k)},\n'))
    ..writeln('};')
    ..writeln()
    ..writeln('// the bytes the port\'s own writer emits for the decoded value, or null when')
    ..writeln('// the read refuses the packet')
    ..writeln('Uint8List? packetRun(String schema, String type, Uint8List data) {')
    ..writeln(r"  switch ('$schema $type') {")
    ..write(packet)
    ..writeln('  }')
    ..writeln('  return null;')
    ..writeln('}');
  File('$build/dispatch.dart').writeAsStringSync(out.toString());
}
