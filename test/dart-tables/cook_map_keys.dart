// THE DART COOK OF A MAP FIELD over every KEY KIND §2.8 admits: a bounded
// string, a signed 64-bit integer and an unsigned 64-bit one (an enum key is
// refused by name, §2.8, so there is none to read). The Go oracle cooked the
// unit TestCookMapFieldExposesItsEntriesLikeGo declares; this program reads
// every entry and looks keys up, printing the oracle's spelling line for line.
// A uint64 key above 2^63 arrives as a negative Dart int, so it is printed
// through BigInt as the unsigned value the oracle prints.
import 'dart:convert';
import 'dart:io';

import 'ProbeCook.dart';

String u64(int v) => BigInt.from(v).toUnsigned(64).toString();

void main(List<String> args) {
  final bytes = File('${args[0]}/keys.cook').readAsBytesSync();
  final cook = KeysCook.open(bytes, 0, bytes.length);
  if (cook == null) {
    stderr.writeln('KeysCook.open refused the oracle cook');
    exit(1);
  }
  final keys = cook.root(cook.cursor());
  final names = KeysNamesEntryRow(cook.view, 0);
  final child = ChildRow(cook.view, 0);
  print('ORACLE names count ${keys.namesCount}');
  for (var i = 0; i < keys.namesCount; i++) {
    keys.namesAt(i, names);
    final key = utf8.decode(names.keyBytes(bytes));
    print('ORACLE names entry $i key "$key" value ${names.value(child).n}');
  }
  for (final key in ['', 'a', 'b', 'mm', 'z', 'abcdefg', 'abcdefgh']) {
    final hit = keys.namesFind(utf8.encode(key), names);
    print(hit == null
        ? 'ORACLE names find "$key" none'
        : 'ORACLE names find "$key" ${hit.value(child).n}');
  }
  final wide = KeysWideEntryRow(cook.view, 0);
  print('ORACLE wide count ${keys.wideCount}');
  for (var i = 0; i < keys.wideCount; i++) {
    keys.wideAt(i, wide);
    print('ORACLE wide entry $i key ${wide.key} value ${wide.value}');
  }
  const low = -9223372036854775808, high = 9223372036854775807;
  for (final key in [low, -2, -1, 0, 1, 7, high]) {
    final hit = keys.wideFind(key, wide);
    print(hit == null
        ? 'ORACLE wide find $key none'
        : 'ORACLE wide find $key ${hit.value}');
  }
  final big = KeysBigEntryRow(cook.view, 0);
  print('ORACLE big count ${keys.bigCount}');
  for (var i = 0; i < keys.bigCount; i++) {
    keys.bigAt(i, big);
    print('ORACLE big entry $i key ${u64(big.key)} value ${big.value}');
  }
  for (final key in [0, 1, 5, 6, 1 << 63, -1]) {
    final hit = keys.bigFind(key, big);
    print(hit == null
        ? 'ORACLE big find ${u64(key)} none'
        : 'ORACLE big find ${u64(key)} ${hit.value}');
  }
  print('ORACLE after ${keys.after}');
}
