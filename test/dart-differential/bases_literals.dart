// THE COMPILE-REFUSAL SIDE OF THE DART VM / dart2js DIFFERENTIAL.
//
// test/tables/Bases.schema declares uint64 defaults above 2^53 as int literals.
// dart2js must refuse them by name rather than round them, so this import is
// expected to fail compilation with "can't be represented exactly in
// JavaScript". If dart2js ever compiles it, the gate runs the program and the
// printed values must still agree with the VM's (the writer and reader below).
import '../../build/schema-dart-vm-dart2js-differential/gen/bases/BasesdemoWire.dart';

void main() {
  final v = Bases();
  print('high_a=${BigInt.from(v.highA).toUnsigned(64)}');
  print('top_a=${BigInt.from(v.topA).toUnsigned(64)}');
  final wire = basesWireSave(v);
  final back = basesWireLoad(wire);
  print('high_a_read=${BigInt.from(back.highA).toUnsigned(64)}');
  print('top_a_read=${BigInt.from(back.topA).toUnsigned(64)}');
}
