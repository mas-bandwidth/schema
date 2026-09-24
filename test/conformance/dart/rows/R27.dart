// R27, the dart leg — THE MANIFEST IS THE WIRE (docs/roadmap.sexp "dart/R27").
//
// `build/fixedform-corpus/manifest.txt` is the single value oracle every leg
// reads to. THE LAW, quoted: docs/FIXED-FORM-ALGORITHM.md:1081 (§5.7 step 1) —
// "**WHAT EACH FILE HOLDS IS IN `build/fixedform-corpus/manifest.txt`** ...
// **ASSERT THE MANIFEST, NEVER READ THE DUMP** (§5.9 #32, #37): the declared
// default is not the value on the wire — `int_widen`'s `lead` and `trail` are
// `2863311530` and `3149642683` there while the schema says 1 and 2 — and a
// field absent from a line carries its schema default"; §5.9 #37 (:1508) —
// "**The oracle is `manifest.txt` (#32) and nothing else.**"; §5.9 #32 (:1416,
// :1434) — one plain-text line per file ... "**The manifest is the card's
// source and the dump is off limits**".
//
// THE RUN, from the repository root (the driver contract's working directory):
//
//   dart run test/conformance/dart/rows/R27.dart
//
// Exit 0 green, 1 red; one printed line per assertion. It depends on no other
// rows/ file and edits no shared file.
//
// THE READER UNDER TEST is the leg's production fixed-form load,
// `chainFixedLoad` in build/tables-generated-dart/p1/P1Fixed.dart — the same
// load the leg's fixed-form gate (test/dart-tables/fixedform.dart) and the
// generated versioning probes call. The test drives THAT path, not a helper.
//
// TWO FEEDING PATHS, ONE COMPARISON (the law's: what the reader lands ==
// what the manifest declares):
//
//   CORPUS PRESENT  build/fixedform-corpus/manifest.txt exists: the full
//                   law — one line per .bin, the line's shape, a lineage
//                   pair's shared `root=` and per-file `row=`, the law's own
//                   int_widen example, and the WIRE: `p1.bin` read by the
//                   generated reader with every declared value landed off
//                   the parsed line and every field the line omits at its
//                   schema default.
//   CORPUS ABSENT   a bare bench (nothing under build/ is committed, and the
//                   corpus needs the sibling serialize checkout the bench
//                   does not carry): the law's PARSE contract pinned on the
//                   law's own quoted numbers, and the smallest form-3 vector
//                   CONSTRUCTED FROM THE LAW — derivation below — read by
//                   the same generated reader and compared by the same code.
//
// THE NEGATIVE CONTROL this guard answers to: break one constant in
// build/tables-generated-dart/ (a decode offset is enough) and the value
// lines go RED; restore and they go GREEN.

import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import '../../../../build/tables-generated-dart/p1/P1Fixed.dart'
    show
        chainFixedHash,
        chainFixedHeaderBytes,
        chainFixedLayout,
        chainFixedLayoutBytes,
        chainFixedLoad,
        chainFixedNewPlan,
        chainFixedRecordBytes;
import '../../../../build/tables-generated-dart/p1/Tblp1Fixed.dart'
    show Chain, TableFixedReport, TableFixedRefusal;

int failed = 0;

void check(bool ok, String what) {
  print('${ok ? 'ok' : 'FAILED'}: $what');
  if (!ok) {
    failed++;
  }
}

// ---------------------------------------------------------------------------
// THE ORACLE'S LINE, exactly as §5.7 step 1 states the shape:
//
//   file=<name> row=<row> side=<...> root=<Table> records=<n> [forged=...]
//     values=<field>=<value>[,<field>=<value>...]
//
// `values=` is the LAST field and runs to the end of the line: split the head
// on spaces and take the rest whole, because a quoted value may carry spaces
// and the commas inside one are escaped. `row=` is the per-file name while
// `root=` is shared by a lineage pair. Records are prefixed `r<i>.`, nested
// fields are dotted, arrays are indexed, text is quoted (`u"…"` for wide),
// a float rides as its digits AND its bits, and a field the dump did not set
// carries its schema default.
// ---------------------------------------------------------------------------

final class OracleValue {
  OracleValue(this.path, this.text, this.quoted);

  final String path;
  final String text;
  final bool quoted;

  /// THE VALUE AS BYTES: a quoted text's own characters with the dumper's
  /// escapes processed (`\"`, `\\`, `\,`, `\xNN`, `\uNNNN`), so a manifest
  /// value can be held against the bytes a reader landed.
  late final List<int> bytes = _valueBytes(text, quoted);

  int get number => int.parse(text);
}

final class OracleLine {
  final Map<String, String> head = {};
  final List<OracleValue> values = [];

  OracleValue? value(String path) {
    for (final v in values) {
      if (v.path == path) {
        return v;
      }
    }
    return null;
  }

  bool names(String path) => value(path) != null;
}

/// The dumper's own escapes (test/tables/fixedform_dump.cpp man_quote /
/// man_wtext): a backslash quotes `"`, `\`, `,`; `\xNN` is one byte; `\uNNNN`
/// is one UTF-16 code unit, encoded here as UTF-8 for the byte comparison.
List<int> _valueBytes(String text, bool quoted) {
  var body = text;
  if (quoted && body.startsWith('u"')) {
    body = body.substring(2, body.length - 1);
  } else if (quoted && body.startsWith('"')) {
    body = body.substring(1, body.length - 1);
  }
  final out = BytesBuilder();
  for (var i = 0; i < body.length; i++) {
    final c = body[i];
    if (c != r'\') {
      out.addByte(body.codeUnitAt(i) & 0xFF);
      continue;
    }
    i++;
    if (i >= body.length) {
      break;
    }
    final e = body[i];
    switch (e) {
      case 'x':
        out.addByte(int.parse(body.substring(i + 1, i + 3), radix: 16));
        i += 2;
      case 'u':
        final unit = int.parse(body.substring(i + 1, i + 5), radix: 16);
        out.add(utf8.encode(String.fromCharCode(unit)));
        i += 4;
      default:
        out.addByte(e.codeUnitAt(0) & 0xFF);
    }
  }
  return out.toBytes();
}

/// THE SPLIT: the head on spaces, `values=` whole to the end of the line,
/// the values on commas that are not escaped. Answers null for a line with
/// no `values=` field — the caller refuses that by name.
OracleLine? parseOracleLine(String line) {
  final at = line.indexOf('values=');
  if (at < 0) {
    return null;
  }
  final parsed = OracleLine();
  for (final field in line.substring(0, at).trim().split(RegExp(r' +'))) {
    final eq = field.indexOf('=');
    if (eq <= 0) {
      continue;
    }
    parsed.head[field.substring(0, eq)] = field.substring(eq + 1);
  }
  var blob = line.substring(at + 7);
  var start = 0;
  var quoted = false;
  for (var i = 0; i <= blob.length; i++) {
    final atEnd = i == blob.length;
    if (!atEnd && blob[i] == r'\') {
      i++; // the escape rides whole, whatever it quotes
      continue;
    }
    if (!atEnd && blob[i] == '"') {
      quoted = !quoted;
    }
    if (atEnd || (blob[i] == ',' && !quoted)) {
      final pair = blob.substring(start, i);
      start = i + 1;
      final eq = pair.indexOf('=');
      if (eq <= 0) {
        continue;
      }
      final path = pair.substring(0, eq);
      var text = pair.substring(eq + 1);
      final isQuoted = text.startsWith('"') || text.startsWith('u"');
      if (isQuoted) {
        text = _unquote(text);
      }
      parsed.values.add(OracleValue(path, text, isQuoted));
    }
  }
  return parsed;
}

String _unquote(String text) {
  final open = text.startsWith('u"') ? 2 : 1;
  // the closing quote is the LAST unescaped one; the splitter has already
  // kept escaped characters whole, so a trailing quote is the terminator
  return text.substring(open, text.length - 1);
}

// ---------------------------------------------------------------------------
// THE WIRE COMPARISON — the law's one assertion, fed twice (the corpus's real
// p1.bin, and the vector constructed from the law below). Every expected
// value flows OUT OF THE PARSED LINE and from nowhere else: no literal stands
// at a compare site.
// ---------------------------------------------------------------------------

final class ChainField {
  ChainField.int(this.path, this.intGet, this.intDefault)
    : isText = false,
      byteGet = null,
      byteDefault = null;
  ChainField.text(this.path, this.byteGet, this.byteDefault)
    : isText = true,
      intGet = null,
      intDefault = null;

  final String path;
  final bool isText;
  final int? Function(Chain)? intGet;
  final int? intDefault;
  final List<int>? Function(Chain)? byteGet;
  final List<int>? byteDefault;
}

/// Chain's whole value surface, at the reader's own names (§5.9 #41: the
/// manifest names the field as the wire names it, so the paths here are the
/// record paths the manifest carries: `r<i>.`, nested dotted).
final List<ChainField> chainFields = <ChainField>[
  ChainField.text('r0.name', (v) => v.name, List<int>.filled(16, 0)),
  ChainField.int('r0.name_length', (v) => v.nameLength, 0),
  ChainField.int('r0.link.value', (v) => v.link.value, 0),
  ChainField.text('r0.link.tag', (v) => v.link.tag, List<int>.filled(8, 0)),
  ChainField.int('r0.link.tag_length', (v) => v.link.tagLength, 0),
];

bool bytesEq(List<int>? a, List<int>? b) {
  if (a == null || b == null || a.length != b.length) {
    return false;
  }
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) {
      return false;
    }
  }
  return true;
}

/// THE COMPARISON: one form-3 file of Chain records against one parsed
/// oracle line. The line's `root=` names the generated reader's table, its
/// `records=` the count the reader lands, and every value it declares the
/// value the record holds — while every field the line OMITS carries its
/// schema default, the law's second half.
void wireAssert(Uint8List bytes, OracleLine line, String what) {
  check(
    line.head['root'] == 'Chain',
    "$what: the line's root= is the generated reader's table"
    ' (root=${line.head['root']}, want Chain)',
  );
  final values = List.generate(1, (_) => Chain());
  final report = TableFixedReport();
  final n = chainFixedLoad(
    values,
    1,
    bytes,
    bytes.length,
    chainFixedNewPlan(),
    report,
  );
  check(
    n == int.parse(line.head['records']!),
    "$what: the reader lands records=${line.head['records']} as the line declares (got $n)",
  );
  check(
    report.refused == TableFixedRefusal.none &&
        !report.malformed &&
        report.clamped == 0,
    '$what: the read refuses nothing, is not malformed and clamps nothing',
  );
  final record = values[0];
  for (final field in chainFields) {
    final declared = line.value(field.path);
    if (declared == null) {
      // THE ABSENT-FIELD RULE (§5.7 step 1): a field absent from a line
      // carries its schema default — the reader must land exactly that and
      // nothing else.
      final ok = field.isText
          ? bytesEq(field.byteGet!(record), field.byteDefault)
          : field.intGet!(record) == field.intDefault;
      check(
        ok,
        "$what: ${field.path} is absent from the line and landed its schema default",
      );
      continue;
    }
    final ok = field.isText
        ? bytesEq(
            field.byteGet!(record),
            _padded(declared.bytes, field.byteDefault!.length),
          )
        : field.intGet!(record) == declared.number;
    check(
      ok,
      "$what: ${field.path} landed what the manifest declares (${field.isText ? '"${declared.text}"' : declared.text})",
    );
  }
  for (final declared in line.values) {
    check(
      chainFields.any((f) => f.path == declared.path),
      "$what: the line's ${declared.path} names a field this reader holds",
    );
  }
}

/// THE TEXT FIELD'S EXPECTED BYTES: the manifest's characters, zero-padded to
/// the field's declared width (the write template zero-fills the slack).
List<int> _padded(List<int> text, int width) {
  if (text.length > width) {
    return text.sublist(0, width);
  }
  return [...text, ...List<int>.filled(width - text.length, 0)];
}

// ---------------------------------------------------------------------------
// CORPUS PRESENT: the full law against build/fixedform-corpus.
// ---------------------------------------------------------------------------

void corpusLaw(String corpus) {
  print('--- the corpus is present: the full law against $corpus');
  final manifest = File('$corpus/manifest.txt');
  check(
    manifest.existsSync() && manifest.lengthSync() > 0,
    'the corpus manifest exists and is not empty',
  );
  if (!manifest.existsSync()) {
    return;
  }
  final lines = <OracleLine>[];
  for (final raw in manifest.readAsLinesSync()) {
    final text = raw.trim();
    if (text.isEmpty || text.startsWith('#')) {
      continue;
    }
    final parsed = parseOracleLine(text);
    if (parsed == null) {
      check(
        false,
        'a manifest line without values=: ${text.substring(0, 40)}…',
      );
      continue;
    }
    check(
      const [
            'file',
            'row',
            'side',
            'root',
            'records',
          ].every(parsed.head.containsKey) &&
          parsed.values.isNotEmpty,
      'the line carries file, row, side, root, records and values=: '
      'file=${parsed.head['file']}',
    );
    lines.add(parsed);
  }
  final bins = Directory(corpus)
      .listSync()
      .whereType<File>()
      .map((f) => f.uri.pathSegments.last)
      .where((name) => name.endsWith('.bin'))
      .toSet();
  final named = lines.map((l) => l.head['file']!).toSet();
  check(
    bins.length == named.length && bins.containsAll(named),
    'one line per corpus file, and no line without a file (${bins.length} files, ${lines.length} lines)',
  );
  // `root=` IS SHARED BY A LINEAGE PAIR, `row=` IS THE PER-FILE NAME: every
  // line of one row (a pair's two sides, the branch case's four files) names
  // one root.
  final byRow = <String, Set<String>>{};
  for (final line in lines) {
    byRow.putIfAbsent(line.head['row']!, () => {}).add(line.head['root']!);
  }
  check(
    byRow.values.every((roots) => roots.length == 1),
    'every row= names one root= across its files (the pair shares it by construction)',
  );
  // THE LAW'S OWN EXAMPLE (§5.7 step 1, quoted at the head of this file):
  // int_widen's lead and trail are 2863311530 and 3149642683 on the wire —
  // NOT the schema's declared 1 and 2 — so only the manifest could name them.
  final widen = lines
      .where((l) => l.head['file'] == 'old_int_widen.bin')
      .toList();
  check(
    widen.length == 1,
    'the manifest carries exactly one line for old_int_widen.bin',
  );
  if (widen.isNotEmpty) {
    final lead = widen[0].value('r0.lead');
    final trail = widen[0].value('r0.trail');
    check(
      lead?.text == '2863311530' && trail?.text == '3149642683',
      "the law's example on the manifest itself: old_int_widen.bin carries "
      'lead=2863311530 and trail=3149642683 — the declared default (1 and 2) '
      'is not the value on the wire',
    );
  }
  // THE WIRE: the corpus's own p1.bin, read by the leg's generated reader,
  // asserted against the line and nothing else.
  final p1 = lines.where((l) => l.head['file'] == 'p1.bin').toList();
  check(p1.length == 1, 'the manifest carries exactly one line for p1.bin');
  if (p1.isNotEmpty) {
    final bytes = File('$corpus/p1.bin').readAsBytesSync();
    wireAssert(bytes, p1[0], 'p1.bin');
  }
}

// ---------------------------------------------------------------------------
// CORPUS ABSENT: the law's parse contract on the law's own numbers, and the
// smallest form-3 vector the law frames.
//
// THE VECTOR, DERIVED FROM THE LAW (docs/SPEC-TABLES.md §3.4 as the leg's own
// reader states the framing, docs/FIXED-FORM-ALGORITHM.md §5.7 step 1):
//
//   byte 0         the form byte, 3
//   bytes 1..7     reserved, zero
//   bytes 8..15    the layout hash — TAKEN AS GIVEN (§5.3); the reader's own,
//                  so the select resolves
//   bytes 16..19   the layout's u32 length
//   bytes 20..     the layout, byte for byte this build's own (the law: the
//                  file's layout is the build's own, which is what makes the
//                  hash the same number)
//   records        the 8-byte hash again, then the body — Chain's fields in
//                  declared order, little-endian, nothing padded:
//                  name_length i32, name text(16), link.value i32,
//                  link.tag_length i32, link.tag text(8)
//
// The record's every declared value is written OUT OF THE PARSED ORACLE LINE
// and from nowhere else; a path the line omits keeps the schema's default
// (zero). The layout, hash and sizes are the generated code's own published
// constants — the same bytes the reader selects against — so what each byte
// holds is said by the manifest line and by nothing in this file.
// ---------------------------------------------------------------------------

const String oracleIntWidenLine =
    'file=old_int_widen.bin row=int_widen side=old root=IntWiden records=3 '
    'values=r0.lead=2863311530,r0.trail=3149642683';

const String oracleP1Line =
    'file=oracle_p1.bin row=oracle_p1 side=none root=Chain records=1 '
    'values=r0.name="oracle",r0.name_length=6,r0.link.value=7';

const String oracleTextLine =
    'file=oracle_text.bin row=oracle_text side=none root=Chain records=1 '
    'values=r0.name="two words\, one",r0.name_length=14,r0.link.value=0';

const String oracleFloatLine =
    'file=oracle_float.bin row=oracle_float side=none root=Chain records=1 '
    'values=r0.name="a",r0.name_length=1,'
    'r0.link.value=0,r0.link.tag="a",r0.link.tag_length=1,'
    'r0.aim=0.300000012|0x3E99999A';

Uint8List u32le(int v) {
  final out = Uint8List(4);
  ByteData.sublistView(out).setUint32(0, v, Endian.little);
  return out;
}

Uint8List u64le(int v) {
  final out = Uint8List(8);
  ByteData.sublistView(out).setUint64(0, v, Endian.little);
  return out;
}

/// THE SMALLEST FORM-3 FILE THE LAW FRAMES: the header, the layout, one
/// record — and the record's values out of the parsed line.
Uint8List wireFromOracle(OracleLine line, String what) {
  final body = Uint8List(36); // chainFixedBodyBytes: the default image is zeros
  for (final declared in line.values) {
    switch (declared.path) {
      case 'r0.name_length':
        ByteData.sublistView(body).setInt32(0, declared.number, Endian.little);
      case 'r0.name':
        body.setRange(4, 4 + declared.bytes.length, declared.bytes);
      case 'r0.link.value':
        ByteData.sublistView(body).setInt32(20, declared.number, Endian.little);
      case 'r0.link.tag_length':
        ByteData.sublistView(body).setInt32(24, declared.number, Endian.little);
      case 'r0.link.tag':
        body.setRange(28, 28 + declared.bytes.length, declared.bytes);
      default:
        // THE MANIFEST NAMES THE FIELD AS THE WIRE NAMES IT: a path the
        // template does not hold is a named failure, never a skipped one.
        check(
          false,
          '$what: the oracle names a path the wire does not hold: ${declared.path}',
        );
    }
  }
  final out = BytesBuilder()
    ..addByte(3) // the form byte
    ..add(Uint8List(7)) // reserved
    ..add(u64le(chainFixedHash)) // the layout hash, taken as given
    ..add(u32le(chainFixedLayoutBytes))
    ..add(chainFixedLayout)
    ..add(u64le(chainFixedHash)) // the record's own hash
    ..add(body);
  final file = out.toBytes();
  check(
    file.length == chainFixedHeaderBytes + chainFixedRecordBytes,
    '$what: the vector is ${file.length} bytes — the header (${chainFixedHeaderBytes}) '
    'plus one record (${chainFixedRecordBytes})',
  );
  return file;
}

void constructedLaw() {
  print('--- the corpus is absent: the law on what the tree carries');
  // THE LAW'S OWN NUMBERS, through the parse: int_widen's wire values are
  // 2863311530 and 3149642683 (docs/FIXED-FORM-ALGORITHM.md:1081) — NOT the
  // schema's declared 1 and 2.
  final widen = parseOracleLine(oracleIntWidenLine);
  check(
    widen != null,
    'the law\'s int_widen line parses (values= to end of line)',
  );
  check(
    widen?.value('r0.lead')?.text == '2863311530' &&
        widen?.value('r0.trail')?.text == '3149642683',
    "the law's own numbers survive the parse: int_widen's wire values are "
    "2863311530 and 3149642683 — NOT the schema's declared 1 and 2",
  );
  check(
    widen?.head['row'] == 'int_widen' && widen?.head['side'] == 'old',
    'row= is the per-file name and side= comes off the file name',
  );
  // values= RUNS TO THE END OF THE LINE and a quoted value may carry spaces:
  // the escaped comma rides whole and the space survives.
  final text = parseOracleLine(oracleTextLine);
  check(
    text?.value('r0.name')?.text == 'two words, one' &&
        text?.value('r0.name')?.quoted == true,
    'a quoted value keeps its spaces and its escaped comma: "two words, one"',
  );
  // A FLOAT RIDES AS ITS DIGITS AND ITS BITS, kept whole by the split — the
  // exact spelling the corpus manifest carries for the compressed-float row
  // (r0.aim=0.300000012|0x3E99999A).
  final float = parseOracleLine(oracleFloatLine);
  check(
    float?.value('r0.aim')?.text == '0.300000012|0x3E99999A',
    "a float's digits AND its bits survive the parse whole: 0.300000012|0x3E99999A",
  );
  // THE WIRE: the constructed vector, values out of the parsed line, read by
  // the leg's own generated reader, compared by the same code the corpus
  // path runs. The line OMITS r0.link.tag and r0.link.tag_length, so the
  // absent-field rule is live here: the reader must land the schema's
  // defaults for both.
  final oracle = parseOracleLine(oracleP1Line);
  check(
    oracle != null &&
        oracle.names('r0.link.value') &&
        !oracle.names('r0.link.tag'),
    "the oracle line declares name, name_length and link.value and omits the tag pair",
  );
  if (oracle != null) {
    wireAssert(
      wireFromOracle(oracle, 'the constructed vector'),
      oracle,
      'the constructed vector',
    );
    // THE ORACLE IS NOT THE SCHEMA: link.value is 7 on the wire where the
    // schema declares 0 — the law's int_widen property at p1's scale, so the
    // comparison above could only pass by way of the parsed manifest line.
    check(
      oracle.value('r0.link.value')!.number != 0,
      "the wire value the oracle declares (7) is not the schema's declared default (0): "
      'the assertion can only pass by way of the manifest',
    );
  }
}

void main() {
  final corpus = 'build/fixedform-corpus';
  final present =
      Directory(corpus).existsSync() &&
      File('$corpus/manifest.txt').existsSync();
  if (present) {
    corpusLaw(corpus);
  } else {
    constructedLaw();
    print(
      'note: build/fixedform-corpus is absent on this bench (it is generated '
      'by `make tables-fixedform-corpus` from the C++ reference, whose sibling '
      'serialize checkout the bench does not carry), so the full law ran on '
      'the vector the law frames; on a bench with the corpus this test runs '
      'the corpus path instead.',
    );
  }
  if (failed != 0) {
    print('FAILED: $failed assertion(s)');
    exit(1);
  }
  print(
    'OK: the manifest is the wire — every value the reader landed is the '
    "manifest's, and every field the manifest omits is the schema's default",
  );
  exit(0);
}
