// The per-PR happy path, Dart leg (make/happy-path.mk): one ShipCreate
// written with the generated packet writer, read back into a fresh value,
// every field compared. Run with --enable-asserts so the write contracts
// are live.
import 'dart:io';
import 'dart:typed_data';

import '../../../build/happy/dart/Happy.dart';

int fail(String why) {
  stderr.writeln('happy dart: FAILED: $why');
  return 1;
}

int run() {
  final sent = ShipCreate()
    ..shipType = ShipType.corvette
    ..health = 777
    ..alive = true;
  sent.position
    ..x = 1.5
    ..y = -2.25
    ..z = 3.0;

  final buffer = Uint8List(shipCreateMaxBytes);
  final bytes = writeShipCreate(sent, ByteData.sublistView(buffer));
  if (bytes <= 0 || bytes > shipCreateMaxBytes) {
    return fail('writeShipCreate returned $bytes bytes');
  }
  final wire = Uint8List.fromList(buffer.sublist(0, bytes));

  final got = ShipCreate();
  if (!readShipCreate(got, ByteData.sublistView(wire), bytes * 8)) {
    return fail('readShipCreate refused the wire');
  }

  if (got.shipType != ShipType.corvette) return fail('ship_type ${got.shipType}');
  if (got.position.x != 1.5) return fail('position.x ${got.position.x}');
  if (got.position.y != -2.25) return fail('position.y ${got.position.y}');
  if (got.position.z != 3.0) return fail('position.z ${got.position.z}');
  if (got.health != 777) return fail('health ${got.health}');
  if (!got.alive) return fail('alive ${got.alive}');

  stdout.writeln('happy dart: ok ($bytes bytes)');
  return 0;
}

void main() {
  exitCode = run();
}
