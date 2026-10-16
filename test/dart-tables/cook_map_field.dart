// THE DART COOK OF A MAP FIELD, read as its ENTRIES (docs/SPEC-TABLES.md §2.8,
// §7.2): tables/lists declares `roster map[uint8]Item` in table Squad, and a
// cooked Squad's roster is its sorted entry array where the cook put it. This
// program opens the cooks the Go oracle wrote (a full roster, an absent map and
// an emptied one), reads EVERY entry through rosterCount / rosterAt, looks keys
// up through rosterFind, and prints one line per fact in the oracle's own
// spelling, so internal/codegen/darttable's
// TestCookMapFieldExposesItsEntriesLikeGo compares the two entry for entry.
import 'dart:io';

import 'HoldersCook.dart';

void main(List<String> args) {
  for (final name in ['full', 'absent', 'empty']) {
    final bytes = File('${args[0]}/$name.cook').readAsBytesSync();
    final cook = SquadCook.open(bytes, 0, bytes.length);
    if (cook == null) {
      stderr.writeln('SquadCook.open refused the oracle cook $name');
      exit(1);
    }
    final squad = cook.root(cook.cursor());
    final entry = SquadRosterEntryRow(cook.view, 0);
    final item = ItemRow(cook.view, 0);
    final count = squad.rosterCount;
    print('ORACLE $name count $count');
    for (var i = 0; i < count; i++) {
      squad.rosterAt(i, entry);
      final value = entry.value(item).count;
      print('ORACLE $name entry $i key ${entry.key} value $value');
    }
    for (final key in [0, 3, 4, 77, 200, 255]) {
      final hit = squad.rosterFind(key, entry);
      print(hit == null
          ? 'ORACLE $name find $key none'
          : 'ORACLE $name find $key ${hit.value(item).count}');
    }
    print('ORACLE $name name ${squad.name}');
  }
}
