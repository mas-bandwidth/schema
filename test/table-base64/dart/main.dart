import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import '../../../build/table-base64/dart/BytesTable.dart';
import '../../../build/table-base64/dart/Base64testTable.dart';

String hex(List<int> data) => data.isEmpty
    ? '-'
    : data.map((b) => b.toRadixString(16).padLeft(2, '0')).join();

Future<void> main() async {
  await for (final line
      in stdin.transform(utf8.decoder).transform(const LineSplitter())) {
    final text = Uint8List.fromList([
      for (var i = 0; i < line.length; i += 2)
        int.parse(line.substring(i, i + 2), radix: 16),
    ]);
    final value = Blob();
    final report = TableReport();
    value.fromJson(text, report);
    if (report.malformed) {
      print('1 0 0 - -');
      continue;
    }
    final output = Uint8List(value.toJsonMeasure());
    if (value.toJson(output) != output.length) {
      throw StateError('writer size');
    }
    print(
      '0 ${report.clamped} ${report.kindMismatch} ${hex(value.payload.sublist(0, value.payloadLength))} ${hex(output)}',
    );
  }
}
