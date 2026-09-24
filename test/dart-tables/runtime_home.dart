// THE DART RUNTIME-HOME DRIVER (docs/PORTING.md §J2). The two imports below
// are the assertion: they name TabledemoBlock.dart and TabledemoCook.dart, and
// a home picked off the FILE ORDER would emit GuardedBlock.dart (or AaaBlock.dart
// once the added file joins) instead of TabledemoBlock.dart, so the import
// would not resolve and this program would not run. The constants read back
// here prove the file that carries the name also carries the runtime, and the
// negative control repoints these same two paths at the sabotaged tree to
// require this program to go red.
import 'dart:io';

import '../../build/runtime-home-dart/base/TabledemoBlock.dart' as blk;
import '../../build/runtime-home-dart/base/TabledemoCook.dart' as ck;

void main() {
  if (blk.tableBlockMagic != 0x4b4c42414d484353) {
    stderr.writeln('runtime home driver: tableBlockMagic mismatch');
    exit(1);
  }
  if (blk.tableBlockByteOrder != 1) {
    stderr.writeln('runtime home driver: tableBlockByteOrder mismatch');
    exit(1);
  }
  if (ck.tableCookMagic != 0x4b4f4f434d484353) {
    stderr.writeln('runtime home driver: tableCookMagic mismatch');
    exit(1);
  }
  stdout.writeln(
    'runtime home driver: TabledemoBlock and TabledemoCook resolve and carry the runtime',
  );
}
