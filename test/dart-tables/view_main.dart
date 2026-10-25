// THE EDITOR GATE (SPEC-TABLES §8.7): this source knows no schema name.
// The build replaces VIEW_LIBRARY with the generated unit view's path.
import 'dart:io';

import 'VIEW_LIBRARY' as view;

String hex64(int value) =>
    ((value >> 32) & 0xffffffff).toRadixString(16).padLeft(8, '0') +
    (value & 0xffffffff).toRadixString(16).padLeft(8, '0');

String flatten(String doc) {
  final out = StringBuffer();
  for (final rune in doc.runes) {
    if (rune == 0x5c) {
      out.write(r'\\');
    } else if (rune == 0x0a) {
      out.write(r'\n');
    } else {
      out.writeCharCode(rune);
    }
  }
  return out.toString();
}

String annotation(String doc, List<String> tags) =>
    'tags=[${tags.join(',')}] doc=${flatten(doc)}';

void vocabulary(StringBuffer out, String kind, view.ViewVocabulary v) {
  out.writeln('$kind ${v.name} file=${v.file} max=${v.max} '
      'bits=${v.storageBits} variants=${v.variants.length} '
      '${annotation(v.doc, v.tags)}');
  for (final r in v.variants) {
    if (kind == 'flags') {
      out.writeln('flags ${v.name} bit ${r.value} ${r.name} '
          'id=${hex64(r.id)} ${annotation(r.doc, r.tags)}');
    } else if (kind == 'enum') {
      out.writeln('enum ${v.name} variant ${r.value} ${r.name} '
          'id=${hex64(r.id)} ${annotation(r.doc, r.tags)}');
    } else {
      out.writeln('union ${v.name} arm ${r.value} ${r.name} '
          'id=${hex64(r.id)} payload=${r.payload} '
          'record=${r.record ? 'yes' : 'no'} '
          'field=${r.field ? 'yes' : 'no'} '
          'kind=${r.kind?.toString() ?? '-'} '
          'bound=${r.bound?.toString() ?? '-'} '
          'overlay=${r.overlay} ${annotation(r.doc, r.tags)}');
    }
  }
}

void declaration(StringBuffer out, String kind, view.ViewType v) {
  out.writeln('$kind ${v.name} file=${v.file} fields=${v.fields.length} '
      '${annotation(v.doc, v.tags)}');
  for (final f in v.fields) {
    out.writeln('$kind ${v.name} field ${f.name} json=${f.json} '
        'type=${f.typeName} id=${hex64(f.id)} optional=${f.optional} '
        '${annotation(f.doc, f.tags)}');
  }
}

void main() {
  final u = view.UnitView();
  final out = StringBuffer();
  out.writeln('unit package=${u.package} protocol=${hex64(u.protocolId)}');
  for (final c in u.constants) {
    out.writeln('constant ${c.name} file=${c.file} type=${c.typeName} '
        'float=${c.isFloat} int=${c.intValue} real=${hex64(c.floatBits)} '
        '${annotation(c.doc, c.tags)}');
  }
  for (final v in u.enums) { vocabulary(out, 'enum', v); }
  for (final v in u.flags) { vocabulary(out, 'flags', v); }
  for (final v in u.unions) { vocabulary(out, 'union', v); }
  for (final v in u.types) { declaration(out, 'type', v); }
  for (final v in u.tables) { declaration(out, 'table', v); }
  stdout.write(out.toString());
}
