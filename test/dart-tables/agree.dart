// THE DART ACCESSOR/DESCRIPTOR AGREEMENT GATE (docs/PORTING.md J1; issue #421).
//
// Every record of the block and cook corpora the fuzzer loads (tables/block,
// tables/pointers) is read TWICE, field by field, and the two must agree:
//
//   - THROUGH THE ACCESSOR: the generated row class (RenderCameraRow, ListNodeRow,
//     ...) or block class, whose getters read at a LITERAL offset;
//   - THROUGH THE DESCRIPTOR: the TableBlockInfo / TableCookInfo constants, read
//     here by offset, kind, width and signedness with ByteData and nothing the
//     generated code owns.
//
// They are two derivations of one layout (the emitter writes the literal and
// writes the descriptor). A moved accessor or a moved descriptor offset is a
// DISAGREE line naming the type, the row and the field, with no pinned dump that
// happens to cover the field.
//
// Three passes, all counted:
//   block images  every row of block_render.bin and block_padded.bin, reached
//                 through each out-of-line array's triple read from the instance
//   cook images   every node of the five cook images, reached through the
//                 reference slots (each deref also resolved here, independently)
//   synthetic     every record type either descriptor graph reaches, read over a
//                 seeded pseudo-random buffer (all-bits patterns, NaNs and
//                 infinities, which every float accessor refuses and answers 0
//                 for, forged used lengths), so a type with no row in an image
//                 is still held
//
// A POINTER SLOT is an eight-byte signed self-relative delta (descriptor:
// isPointer, size 8). The generated row getter for it reads ONE byte of the slot
// (see the report on this card), so the accessor side is compared with the low
// byte of the delta the descriptor reads, and the deref the cook gives is
// compared with the resolution computed here from the descriptor's offset.
//
//   usage: agree
import 'dart:io';
import 'dart:typed_data';

import '../../build/tables-generated-dart/block/BlockdemoBlock.dart';
import '../../build/tables-generated-dart/block/PaddedBlock.dart' as pb;
import '../../build/tables-generated-dart/block/RenderBlock.dart' as rb;
import '../../build/tables-generated-dart/pointers/GraphdemoCook.dart';
import '../../build/tables-generated-dart/pointers/GraphCook.dart' as ck;
import '../../build/tables-generated-dart/pointers/MarksCook.dart' as mk;
import '../../build/tables-generated-dart/pointers/PartsCook.dart' as pt;

Uint8List load(String path) => File(path).readAsBytesSync();

int disagreements = 0;
int reads = 0;
int blockRows = 0;
int cookNodes = 0;
int cookRecords = 0;
int syntheticRecords = 0;

void fail(String where, String what) {
  disagreements++;
  stdout.writeln('DISAGREE  $where: $what');
}

// the accessor spells a schema field snake_case -> lowerCamel
String camel(String name) {
  final parts = name.split('_');
  return parts.first +
      parts
          .skip(1)
          .map((p) => p.isEmpty ? p : p[0].toUpperCase() + p.substring(1))
          .join();
}

// ---- comparing what the two sides read ----

bool same(Object? a, Object? b) {
  if (a is double && b is double) {
    return (a.isNaN && b.isNaN) || (a == b && a.isNegative == b.isNegative);
  }
  if (a is List && b is List) {
    if (a.length != b.length) {
      return false;
    }
    for (var i = 0; i < a.length; i++) {
      if (!same(a[i], b[i])) {
        return false;
      }
    }
    return true;
  }
  return a == b;
}

String show(Object? v) {
  if (v is Uint8List) {
    return 'bytes[${v.length}]${v.length > 8 ? v.sublist(0, 8) : v}';
  }
  if (v is List) {
    return '[${v.map(show).join(', ')}]';
  }
  return '$v';
}

// compare the two maps of one record: every described field on the descriptor
// side must be read by the accessor, with the same value, and the accessor may
// name nothing the descriptor does not.
void compareRecord(
  String type,
  String where,
  Map<String, Object?> viaAccessor,
  Map<String, Object?> viaDescriptor,
) {
  for (final k in viaDescriptor.keys) {
    reads++;
    if (!viaAccessor.containsKey(k)) {
      fail(
        '$type $where.$k',
        'the descriptor names it and the accessor has no read of it',
      );
      continue;
    }
    if (!same(viaAccessor[k], viaDescriptor[k])) {
      fail(
        '$type $where.$k',
        'accessor ${show(viaAccessor[k])} != descriptor ${show(viaDescriptor[k])}',
      );
    }
  }
  for (final k in viaAccessor.keys) {
    if (!viaDescriptor.containsKey(k)) {
      fail(
        '$type $where.$k',
        'the accessor reads it and the descriptor does not name it',
      );
    }
  }
}

// ---- the DESCRIPTOR readers: kind, width, signedness, offset, nothing else ----

// a scalar by the table-wire kind (docs/SPEC-TABLES.md §8.1): the width and the
// signedness come out of the kind number
//
// An ENUM or FLAGS slot carries an unsigned kind whose number is not its storage
// width (a one-byte enum is kind 7), so the unsigned kinds read at the slot's
// own width, and every other kind at the width its number gives.
Object scalarByKind(ByteData v, int at, int kind, int slotWidth) {
  if (kind >= 6 && kind <= 9) {
    if (slotWidth != 1 && slotWidth != 2 && slotWidth != 4 && slotWidth != 8) {
      throw StateError('an unsigned slot of $slotWidth bytes');
    }
    return slotWidth == 1
        ? v.getUint8(at)
        : slotWidth == 2
        ? v.getUint16(at, Endian.little)
        : slotWidth == 4
        ? v.getUint32(at, Endian.little)
        : v.getUint64(at, Endian.little);
  }
  if (widthOfKind(kind) != slotWidth) {
    throw StateError('kind $kind is not a $slotWidth-byte slot');
  }
  switch (kind) {
    case 1:
      return v.getUint8(at) != 0;
    case 2:
      return v.getInt8(at);
    case 3:
      return v.getInt16(at, Endian.little);
    case 4:
      return v.getInt32(at, Endian.little);
    case 5:
      return v.getInt64(at, Endian.little);
    case 6:
      return v.getUint8(at);
    case 7:
      return v.getUint16(at, Endian.little);
    case 8:
      return v.getUint32(at, Endian.little);
    case 9:
      return v.getUint64(at, Endian.little);
    case 10:
      return finite(v.getFloat32(at, Endian.little));
    case 11:
      return finite(v.getFloat64(at, Endian.little));
  }
  throw StateError('kind $kind is not a scalar kind this gate reads');
}

// A FLOAT ACCESSOR NEVER RETURNS A NaN OR AN INFINITY (docs/SPEC-TABLES.md
// §3.3, ruling 2 of 2026-10-27): it answers 0 and latches non_finite_float on
// the view. So the descriptor side reads a non-finite cell as that 0, and the
// all-ones round below holds every float accessor to the refusal.
int nonFiniteCells = 0;

double finite(double d) {
  if (d.isFinite) {
    return d;
  }
  nonFiniteCells++;
  return 0.0;
}

int widthOfKind(int kind) {
  switch (kind) {
    case 1:
    case 2:
    case 6:
      return 1;
    case 3:
    case 7:
      return 2;
    case 4:
    case 8:
    case 10:
      return 4;
  }
  return 8;
}

// a cooked slot by its storage class and width (docs/SPEC-TABLES.md §7.5)
Object scalarByStorage(ByteData v, int at, int storage, int width) {
  switch (storage) {
    case TableCookStorage.boolean:
      return v.getUint8(at) != 0;
    case TableCookStorage.float:
      return finite(
        width == 4
            ? v.getFloat32(at, Endian.little)
            : v.getFloat64(at, Endian.little),
      );
    case TableCookStorage.signed:
      return width == 1
          ? v.getInt8(at)
          : width == 2
          ? v.getInt16(at, Endian.little)
          : width == 4
          ? v.getInt32(at, Endian.little)
          : v.getInt64(at, Endian.little);
    case TableCookStorage.unsigned:
      return width == 1
          ? v.getUint8(at)
          : width == 2
          ? v.getUint16(at, Endian.little)
          : width == 4
          ? v.getUint32(at, Endian.little)
          : v.getUint64(at, Endian.little);
  }
  throw StateError('storage $storage is not a scalar storage');
}

// [used, the used bytes], clamped the way a reader must: a forged used length
// reads as zero
List<Object> usedBytes(
  ByteData v,
  Uint8List b,
  int at,
  int offset,
  int countOffset,
  int bound,
) {
  final used = v.getInt32(at + countOffset, Endian.little);
  final n = used < 0 || used > bound ? 0 : used;
  return <Object>[n, Uint8List.sublistView(b, at + offset, at + offset + n)];
}

// ---- BLOCK: the descriptor half of one record ----

Map<String, Object?> blockViaDescriptor(
  ByteData v,
  Uint8List b,
  int at,
  TableBlockInfo info,
  String where,
) {
  final out = <String, Object?>{};
  for (final f in info.fields) {
    if (f.outOfLine) {
      continue; // the triple: the block's own walk compares it
    }
    final slots = f.isArray ? f.arrayBound : 1;
    final key = camel(f.name);
    if (f.element != null) {
      out[key] = f.isArray
          ? <Object>[
              for (var s = 0; s < slots; s++) at + f.offset + s * f.elemSize,
            ]
          : at + f.offset;
    } else if (f.counted) {
      out[key] = usedBytes(v, b, at, f.offset, f.countOffset, f.arrayBound);
    } else if (f.isArray) {
      out[key] = <Object>[
        for (var s = 0; s < slots; s++)
          scalarByKind(v, at + f.offset + s * f.elemSize, f.kind, f.elemSize),
      ];
    } else {
      // an optional's size includes its presence byte: the kind gives the width
      out[key] = scalarByKind(
        v,
        at + f.offset,
        f.kind,
        f.optional ? widthOfKind(f.kind) : f.size,
      );
    }
    if (f.optional) {
      out['${key}Present'] = v.getUint8(at + f.presentOffset) != 0;
    }
  }
  return out;
}

// ---- BLOCK: the accessor half, one function per row type ----

Map<String, Object?> rb3(ByteData v, int at) {
  final r = rb.RenderVector3Row(v, at);
  return {'x': r.x, 'y': r.y, 'z': r.z};
}

int pos(ByteData v, rb.RenderVector3Row Function(rb.RenderVector3Row) f) =>
    f(rb.RenderVector3Row(v, 0)).at;
int rot(
  ByteData v,
  rb.RenderQuaternionRow Function(rb.RenderQuaternionRow) f,
) => f(rb.RenderQuaternionRow(v, 0)).at;

final Map<String, Map<String, Object?> Function(ByteData, Uint8List, int)>
blockAccessors = {
  'RenderVector3': (v, b, at) => rb3(v, at),
  'RenderQuaternion': (v, b, at) {
    final r = rb.RenderQuaternionRow(v, at);
    return {'x': r.x, 'y': r.y, 'z': r.z, 'w': r.w};
  },
  'RenderCamera': (v, b, at) {
    final r = rb.RenderCameraRow(v, at);
    return {
      'position': pos(v, r.position),
      'rotation': rot(v, r.rotation),
      'cameraId': r.cameraId,
      'cameraType': r.cameraType,
      'targetObjectId': r.targetObjectId,
      'fov': r.fov,
    };
  },
  'RenderCosmeticProp': (v, b, at) {
    final r = rb.RenderCosmeticPropRow(v, at);
    return {
      'position': pos(v, r.position),
      'rotation': rot(v, r.rotation),
      'scale': r.scale,
      'flags': r.flags,
      'cosmeticPropId': r.cosmeticPropId,
      'propSequence': r.propSequence,
      'propType': r.propType,
      'team': r.team,
    };
  },
  'RenderDynamicProp': (v, b, at) {
    final r = rb.RenderDynamicPropRow(v, at);
    return {
      'position': pos(v, r.position),
      'rotation': rot(v, r.rotation),
      'flags': r.flags,
      'objectId': r.objectId,
      'objectSequence': r.objectSequence,
      'propType': r.propType,
      'team': r.team,
    };
  },
  'RenderExplosion': (v, b, at) {
    final r = rb.RenderExplosionRow(v, at);
    return {
      'position': pos(v, r.position),
      'rotation': rot(v, r.rotation),
      't': r.t,
      'explosionId': r.explosionId,
      'parentObjectId': r.parentObjectId,
      'explosionType': r.explosionType,
      'team': r.team,
    };
  },
  'RenderLaser': (v, b, at) {
    final r = rb.RenderLaserRow(v, at);
    return {
      'start': pos(v, r.start),
      'finish': pos(v, r.finish),
      't': r.t,
      'laserId': r.laserId,
      'laserType': r.laserType,
      'team': r.team,
    };
  },
  'RenderMissile': (v, b, at) {
    final r = rb.RenderMissileRow(v, at);
    return {
      'position': pos(v, r.position),
      'rotation': rot(v, r.rotation),
      'flags': r.flags,
      'objectId': r.objectId,
      'objectSequence': r.objectSequence,
      'missileType': r.missileType,
      'team': r.team,
    };
  },
  'RenderShip': (v, b, at) {
    final r = rb.RenderShipRow(v, at);
    return {
      'position': pos(v, r.position),
      'rotation': rot(v, r.rotation),
      'flags': r.flags,
      'objectId': r.objectId,
      'targetObjectId': r.targetObjectId,
      'thrust': r.thrust,
      'objectSequence': r.objectSequence,
      'shipType': r.shipType,
      'team': r.team,
      'hasTargetLock': r.hasTargetLock,
      'predictedExplode': r.predictedExplode,
    };
  },
  'RenderStaticProp': (v, b, at) {
    final r = rb.RenderStaticPropRow(v, at);
    return {
      'position': pos(v, r.position),
      'rotation': rot(v, r.rotation),
      'scale': r.scale,
      'flags': r.flags,
      'staticPropId': r.staticPropId,
      'propType': r.propType,
      'team': r.team,
    };
  },
  'RenderTurret': (v, b, at) {
    final r = rb.RenderTurretRow(v, at);
    return {
      'rotation': rot(v, r.rotation),
      'flags': r.flags,
      'objectId': r.objectId,
      'parentObjectId': r.parentObjectId,
      'turretIndex': r.turretIndex,
      'targetObjectId': r.targetObjectId,
      'objectSequence': r.objectSequence,
      'team': r.team,
      'hasTargetLock': r.hasTargetLock,
    };
  },
  'PaddedRow': (v, b, at) {
    final r = pb.PaddedRowRow(v, at);
    return {
      'tag': r.tag,
      'value': r.value,
      'flag': r.flag,
      'id': r.id,
      'label': <Object>[r.labelLength, r.labelBytes(b)],
      'slots': <Object>[
        for (var i = 0; i < pb.PaddedRowRow.slotsBound; i++) r.slotsAt(i),
      ],
      'teams': <Object>[
        for (var i = 0; i < pb.PaddedRowRow.teamsBound; i++) r.teamsAt(i),
      ],
      'counter': r.counter,
      'counterPresent': r.counterPresent,
    };
  },
};

// The two block PROJECTIONS (the frame's own fields at the base), read through
// the block class's getters, and each out-of-line array's triple and row mover.
class BlockCase {
  final String name;
  final Uint8List image;
  final TableBlockInfo info;
  final Object? Function(Uint8List) open;
  final Map<String, Object?> Function(Object block, ByteData v, Uint8List b)
  projection;
  final Map<String, List<Object>> Function(Object block) triples;
  final int Function(Object block, String array, int index) rowAt;

  BlockCase(
    this.name,
    this.image,
    this.info,
    this.open,
    this.projection,
    this.triples,
    this.rowAt,
  );
}

Map<String, Object?> renderProjection(Object o, ByteData v, Uint8List b) {
  // the frame's scalar: its version; the nine triples are compared separately
  return {'version': (o as rb.RenderFrameBlock).version};
}

Map<String, Object?> paddedProjection(Object o, ByteData v, Uint8List b) {
  final f = o as pb.PaddedFrameBlock;
  return {
    'marker': f.marker,
    'stamp': f.stamp,
    'blob': <Object>[f.blobLength, f.blobBytes],
  };
}

// [offsetOf, count, pitch] as the block's getters give them
Map<String, List<Object>> renderTriples(Object o) {
  final f = o as rb.RenderFrameBlock;
  return {
    'cameras': [f.camerasOffsetOf, f.camerasCount, f.camerasPitch],
    'ships': [f.shipsOffsetOf, f.shipsCount, f.shipsPitch],
    'turrets': [f.turretsOffsetOf, f.turretsCount, f.turretsPitch],
    'missiles': [f.missilesOffsetOf, f.missilesCount, f.missilesPitch],
    'dynamicProps': [
      f.dynamicPropsOffsetOf,
      f.dynamicPropsCount,
      f.dynamicPropsPitch,
    ],
    'staticProps': [
      f.staticPropsOffsetOf,
      f.staticPropsCount,
      f.staticPropsPitch,
    ],
    'cosmeticProps': [
      f.cosmeticPropsOffsetOf,
      f.cosmeticPropsCount,
      f.cosmeticPropsPitch,
    ],
    'lasers': [f.lasersOffsetOf, f.lasersCount, f.lasersPitch],
    'explosions': [f.explosionsOffsetOf, f.explosionsCount, f.explosionsPitch],
  };
}

Map<String, List<Object>> paddedTriples(Object o) {
  final f = o as pb.PaddedFrameBlock;
  return {
    'rows': [f.rowsOffsetOf, f.rowsCount, f.rowsPitch],
  };
}

int renderRowAt(Object o, String array, int i) {
  final f = o as rb.RenderFrameBlock;
  switch (array) {
    case 'cameras':
      return f.camerasAt(i, f.camerasCursor()).at;
    case 'ships':
      return f.shipsAt(i, f.shipsCursor()).at;
    case 'turrets':
      return f.turretsAt(i, f.turretsCursor()).at;
    case 'missiles':
      return f.missilesAt(i, f.missilesCursor()).at;
    case 'dynamicProps':
      return f.dynamicPropsAt(i, f.dynamicPropsCursor()).at;
    case 'staticProps':
      return f.staticPropsAt(i, f.staticPropsCursor()).at;
    case 'cosmeticProps':
      return f.cosmeticPropsAt(i, f.cosmeticPropsCursor()).at;
    case 'lasers':
      return f.lasersAt(i, f.lasersCursor()).at;
    case 'explosions':
      return f.explosionsAt(i, f.explosionsCursor()).at;
  }
  throw StateError('the render frame has no array $array');
}

int paddedRowAt(Object o, String array, int i) {
  final f = o as pb.PaddedFrameBlock;
  if (array == 'rows') {
    return f.rowsAt(i, f.rowsCursor()).at;
  }
  throw StateError('the padded frame has no array $array');
}

// one record of a block image, both ways
void checkBlockRecord(
  ByteData v,
  Uint8List b,
  int at,
  TableBlockInfo info,
  String where,
  bool image,
) {
  final acc = blockAccessors[info.name];
  if (acc == null) {
    fail(where, 'no accessor is registered for the record type ${info.name}');
    return;
  }
  if (image) {
    blockRows++;
  } else {
    syntheticRecords++;
  }
  compareRecord(
    info.name,
    where,
    acc(v, b, at),
    blockViaDescriptor(v, b, at, info, where),
  );
  // descend through the nested records and the inline arrays of records
  for (final f in info.fields) {
    final element = f.element;
    if (f.outOfLine || element == null) {
      continue;
    }
    final slots = f.isArray ? f.arrayBound : 1;
    for (var s = 0; s < slots; s++) {
      checkBlockRecord(
        v,
        b,
        at + f.offset + s * f.elemSize,
        element,
        '$where.${f.name}',
        image,
      );
    }
  }
}

void checkBlockImage(BlockCase c) {
  final v = ByteData.view(
    c.image.buffer,
    c.image.offsetInBytes,
    c.image.lengthInBytes,
  );
  final opened = c.open(c.image);
  if (opened == null) {
    fail(c.name, 'the committed image does not open');
    return;
  }
  // the projection: the frame's own fields, at the base
  final projection = c.projection(opened, v, c.image);
  final described = blockViaDescriptor(v, c.image, 0, c.info, c.name);
  // the triples are compared below, so the descriptor map has no entry for them
  compareRecord(c.info.name, c.name, projection, described);
  final triples = c.triples(opened);
  for (final f in c.info.fields) {
    if (!f.outOfLine) {
      continue;
    }
    final t = triples[camel(f.name)];
    if (t == null) {
      fail(
        '${c.name}.${f.name}',
        'the descriptor names an array the block has no getters for',
      );
      continue;
    }
    reads += 3;
    final viaDescriptor = <Object>[
      v.getUint64(f.offsetOfOffset, Endian.little),
      v.getUint32(f.countOffset, Endian.little),
      v.getUint32(f.strideOffset, Endian.little),
    ];
    if (!same(t, viaDescriptor)) {
      fail(
        '${c.name}.${f.name}',
        'triple accessor ${show(t)} != descriptor ${show(viaDescriptor)}',
      );
    }
    if (f.stride != t[2]) {
      fail(
        '${c.name}.${f.name}',
        'the descriptor stride ${f.stride} is not the pitch ${t[2]} the image holds',
      );
    }
    final row = f.element!;
    final count = viaDescriptor[1] as int;
    final stride = viaDescriptor[2] as int;
    for (var r = 0; r < count; r++) {
      final where = '${c.name}.${f.name}[$r]';
      final at = (viaDescriptor[0] as int) + r * stride;
      reads++;
      final moved = c.rowAt(opened, camel(f.name), r);
      if (moved != at) {
        fail(
          where,
          'the accessor moves a cursor to $moved, the descriptor says $at',
        );
      }
      checkBlockRecord(v, c.image, at, row, where, true);
    }
  }
}

// ---- COOK ----

Map<String, Object?> cookViaDescriptor(
  ByteData v,
  Uint8List b,
  int at,
  TableCookInfo info,
  String where,
) {
  final out = <String, Object?>{};
  for (final f in info.fields) {
    final slots = f.isArray ? f.arrayBound : 1;
    final key = camel(f.name);
    if (f.isPointer) {
      // the slot is an eight-byte signed delta; the generated getter reads its
      // first byte (see the header)
      if (f.size != 8) {
        fail('$where.${f.name}', 'a pointer slot of ${f.size} bytes, wanted 8');
      }
      out[key] = v.getInt64(at + f.offset, Endian.little) & 0xff;
    } else if (f.storage == TableCookStorage.record) {
      out[key] = f.isArray
          ? <Object>[
              for (var s = 0; s < slots; s++) at + f.offset + s * f.elemSize,
            ]
          : at + f.offset;
    } else if (f.storage == TableCookStorage.string ||
        f.storage == TableCookStorage.bytes) {
      out[key] = usedBytes(v, b, at, f.offset, f.countOffset, f.arrayBound);
    } else if (f.isArray) {
      out[key] = <Object>[
        for (var s = 0; s < slots; s++)
          scalarByStorage(
            v,
            at + f.offset + s * f.elemSize,
            f.storage,
            f.elemSize,
          ),
      ];
    } else {
      out[key] = scalarByStorage(v, at + f.offset, f.storage, f.elemSize);
    }
    if (f.presentOffset >= 0) {
      out['${key}Present'] = v.getUint8(at + f.presentOffset) != 0;
    }
  }
  return out;
}

final Map<String, Map<String, Object?> Function(ByteData, Uint8List, int)>
cookAccessors = {
  'Album': (v, b, at) {
    final r = ck.AlbumRow(v, at);
    return {
      'name': <Object>[r.nameLength, r.nameBytes(b)],
      'tint': r.tint(pt.ColourRow(v, 0)).at,
      'stamp': r.stamp(pt.StampRow(v, 0)).at,
      'marker': r.marker(mk.MarkerRow(v, 0)).at,
      'pin': r.pin,
      'head': r.head,
    };
  },
  'Depot': (v, b, at) {
    final r = ck.DepotRow(v, at);
    return {
      'name': <Object>[r.nameLength, r.nameBytes(b)],
      'banks': <Object>[
        for (var i = 0; i < ck.DepotRow.banksBound; i++)
          r.banksAt(i, ck.LayerRow(v, 0)).at,
      ],
      'spare': r.spare(ck.MetaRow(v, 0)).at,
      'sparePresent': r.sparePresent,
      'head': r.head,
    };
  },
  'Layer': (v, b, at) {
    final r = ck.LayerRow(v, at);
    return {'depth': r.depth, 'head': r.head};
  },
  'ListNode': (v, b, at) {
    final r = ck.ListNodeRow(v, at);
    return {
      'value': r.value,
      'name': <Object>[r.nameLength, r.nameBytes(b)],
      'next': r.next,
    };
  },
  'Meta': (v, b, at) {
    final r = ck.MetaRow(v, at);
    return {
      'build': r.build,
      'tag': <Object>[r.tagLength, r.tagBytes(b)],
    };
  },
  'Scene': (v, b, at) {
    final r = ck.SceneRow(v, at);
    return {
      'name': <Object>[r.nameLength, r.nameBytes(b)],
      'version': r.version,
      'head': r.head,
      'tree': r.tree,
      'settings': r.settings,
      'alias': r.alias,
      'ground': r.ground(ck.LayerRow(v, 0)).at,
      'layers': <Object>[
        for (var i = 0; i < ck.SceneRow.layersBound; i++)
          r.layersAt(i, ck.LayerRow(v, 0)).at,
      ],
      'meta': r.meta(ck.MetaRow(v, 0)).at,
    };
  },
  'Settings': (v, b, at) {
    final r = ck.SettingsRow(v, at);
    return {
      'quality': r.quality,
      'label': <Object>[r.labelLength, r.labelBytes(b)],
    };
  },
  'TreeNode': (v, b, at) {
    final r = ck.TreeNodeRow(v, at);
    return {
      'label': <Object>[r.labelLength, r.labelBytes(b)],
      'left': r.left,
      'right': r.right,
    };
  },
  'Colour': (v, b, at) {
    final r = pt.ColourRow(v, at);
    return {'r': r.r, 'g': r.g, 'b': r.b};
  },
  'Stamp': (v, b, at) {
    final r = pt.StampRow(v, at);
    return {
      'tag': <Object>[r.tagLength, r.tagBytes(b)],
      'seq': r.seq,
    };
  },
  'Tally': (v, b, at) => {'hits': mk.TallyRow(v, at).hits},
  'Marker': (v, b, at) {
    final r = mk.MarkerRow(v, at);
    return {
      'label': <Object>[r.labelLength, r.labelBytes(b)],
      'note': r.note,
    };
  },
};

class CookCase {
  final String name;
  final Uint8List image;
  final TableCookInfo info;
  final Object? Function(Uint8List) open;
  final (int, int) Function(Object) region;
  final int Function(int, int) Function(Object) deref;

  CookCase(
    this.name,
    this.image,
    this.info,
    this.open,
    this.region,
    this.deref,
  );
}

// one cooked record, both ways; returns nothing and descends through by-value
// nesting only (the references are followed by the node walk)
void checkCookRecord(
  ByteData v,
  Uint8List b,
  int at,
  TableCookInfo info,
  String where,
  bool image,
) {
  final acc = cookAccessors[info.name];
  if (acc == null) {
    fail(where, 'no accessor is registered for the record type ${info.name}');
    return;
  }
  if (image) {
    cookRecords++;
  } else {
    syntheticRecords++;
  }
  compareRecord(
    info.name,
    where,
    acc(v, b, at),
    cookViaDescriptor(v, b, at, info, where),
  );
  for (final f in info.fields) {
    if (f.isPointer || f.storage != TableCookStorage.record) {
      continue;
    }
    final slots = f.isArray ? f.arrayBound : 1;
    for (var s = 0; s < slots; s++) {
      checkCookRecord(
        v,
        b,
        at + f.offset + s * f.elemSize,
        f.info!,
        '$where.${f.name}',
        image,
      );
    }
  }
}

void checkCookImage(CookCase c) {
  final v = ByteData.view(
    c.image.buffer,
    c.image.offsetInBytes,
    c.image.lengthInBytes,
  );
  final opened = c.open(c.image);
  if (opened == null) {
    fail(c.name, 'the image does not open');
    return;
  }
  final (region, length) = c.region(opened);
  final at = c.deref(opened);
  final seen = <int>{};
  void node(int offset, TableCookInfo type, String where) {
    if (!seen.add(offset)) {
      return;
    }
    cookNodes++;
    final start = region + offset;
    checkCookRecord(v, c.image, start, type, where, true);
    for (final f in type.fields) {
      if (!f.isPointer) {
        continue;
      }
      final target = f.info!;
      final slot = start + f.offset;
      // the descriptor's resolution, computed here
      reads++;
      final delta = v.getInt64(slot, Endian.little);
      var wanted = TableCookRef.none;
      if (delta != 0) {
        final to = slot + delta;
        wanted = to >= region && to + target.size <= region + length
            ? to
            : TableCookRef.outside;
      }
      final got = at(slot, target.size);
      if (got != wanted) {
        fail(
          '$where.${f.name}',
          'the deref resolves to $got, the descriptor slot to $wanted',
        );
      }
      if (wanted >= 0) {
        node(wanted - region, target, '$where.${f.name}->${target.name}');
      }
    }
  }

  node(0, c.info, c.name);
}

// ---- SYNTHETIC: every record type the descriptors reach, over seeded bytes ----

int lcg = 0x2545f4914f6cdd1d;

int nextWord() {
  lcg ^= lcg >>> 12;
  lcg ^= (lcg << 25) & 0xffffffffffffffff;
  lcg ^= lcg >>> 27;
  return (lcg * 0x2545f4914f6cdd1d) & 0x7fffffffffffffff;
}

Uint8List noise(int n, int round) {
  final out = Uint8List(n);
  for (var i = 0; i < n; i++) {
    out[i] = nextWord() & 0xff;
  }
  // rounds 0-1 are the all-ones and all-zero patterns (NaNs, refused by every
  // float accessor, and the sign bit);
  // later rounds also forge small and wild used lengths at every int32 slot
  if (round == 0) {
    out.fillRange(0, n, 0xff);
  } else if (round == 1) {
    out.fillRange(0, n, 0);
  } else if (round % 2 == 0) {
    for (var i = 0; i + 4 <= n; i += 4) {
      out[i] = nextWord() % 20;
      out[i + 1] = 0;
      out[i + 2] = 0;
      out[i + 3] = 0;
    }
  }
  return out;
}

const int syntheticRounds = 8;

// a ROW type of the block graph (a frame's projection is not a row and is held
// by the image pass): every row type its out-of-line arrays and nestings reach
void syntheticBlock(TableBlockInfo info, Set<String> seen, {bool root = true}) {
  if (!root) {
    if (!seen.add(info.name)) {
      return;
    }
    for (var round = 0; round < syntheticRounds; round++) {
      final b = noise(info.size + 16, round);
      final v = ByteData.view(b.buffer);
      for (final at in <int>[0, 8, 16]) {
        if (at + info.size <= b.length) {
          checkBlockRecordSynthetic(v, b, at, info);
        }
      }
    }
  }
  for (final f in info.fields) {
    if (f.element != null) {
      syntheticBlock(f.element!, seen, root: false);
    }
  }
}

void checkBlockRecordSynthetic(
  ByteData v,
  Uint8List b,
  int at,
  TableBlockInfo info,
) {
  final acc = blockAccessors[info.name];
  if (acc == null) {
    fail(info.name, 'no accessor is registered for the record type');
    return;
  }
  syntheticRecords++;
  compareRecord(
    info.name,
    'synthetic@$at',
    acc(v, b, at),
    blockViaDescriptor(v, b, at, info, 'synthetic ${info.name}'),
  );
}

void syntheticCook(TableCookInfo info, Set<String> seen) {
  if (!seen.add(info.name)) {
    return;
  }
  for (var round = 0; round < syntheticRounds; round++) {
    final b = noise(info.size + 16, round);
    final v = ByteData.view(b.buffer);
    for (final at in <int>[0, 8, 16]) {
      if (at + info.size <= b.length) {
        final acc = cookAccessors[info.name];
        if (acc == null) {
          fail(info.name, 'no accessor is registered for the record type');
          return;
        }
        syntheticRecords++;
        compareRecord(
          info.name,
          'synthetic@$at',
          acc(v, b, at),
          cookViaDescriptor(v, b, at, info, 'synthetic ${info.name}'),
        );
      }
    }
  }
  for (final f in info.fields) {
    if (f.info != null) {
      syntheticCook(f.info!, seen);
    }
  }
}

CookCase cookCase(String root) {
  Uint8List image = load('build/cook-fuzz/$root.cook');
  switch (root) {
    case 'Scene':
      return CookCase(
        root,
        image,
        ck.SceneCook.type,
        (b) => ck.SceneCook.open(b, 0, b.length),
        (o) => ((o as ck.SceneCook).region, o.length),
        (o) => (o as ck.SceneCook).at,
      );
    case 'Depot':
      return CookCase(
        root,
        image,
        ck.DepotCook.type,
        (b) => ck.DepotCook.open(b, 0, b.length),
        (o) => ((o as ck.DepotCook).region, o.length),
        (o) => (o as ck.DepotCook).at,
      );
    case 'Album':
      return CookCase(
        root,
        image,
        ck.AlbumCook.type,
        (b) => ck.AlbumCook.open(b, 0, b.length),
        (o) => ((o as ck.AlbumCook).region, o.length),
        (o) => (o as ck.AlbumCook).at,
      );
    case 'TreeNode':
      return CookCase(
        root,
        image,
        ck.TreeNodeCook.type,
        (b) => ck.TreeNodeCook.open(b, 0, b.length),
        (o) => ((o as ck.TreeNodeCook).region, o.length),
        (o) => (o as ck.TreeNodeCook).at,
      );
  }
  return CookCase(
    root,
    image,
    ck.ListNodeCook.type,
    (b) => ck.ListNodeCook.open(b, 0, b.length),
    (o) => ((o as ck.ListNodeCook).region, o.length),
    (o) => (o as ck.ListNodeCook).at,
  );
}

void main(List<String> args) {
  final blocks = <BlockCase>[
    BlockCase(
      'block_render',
      load('testdata/wire/tables/block_render.bin'),
      rb.RenderFrameBlock.type,
      (b) => rb.RenderFrameBlock.open(b, 0, b.length),
      renderProjection,
      renderTriples,
      renderRowAt,
    ),
    BlockCase(
      'block_padded',
      load('testdata/wire/tables/block_padded.bin'),
      pb.PaddedFrameBlock.type,
      (b) => pb.PaddedFrameBlock.open(b, 0, b.length),
      paddedProjection,
      paddedTriples,
      paddedRowAt,
    ),
  ];
  for (final c in blocks) {
    checkBlockImage(c);
  }

  final cooks = <CookCase>[];
  for (final root in ['Scene', 'Depot', 'Album', 'TreeNode', 'ListNode']) {
    if (!File('build/cook-fuzz/$root.cook').existsSync()) {
      fail(
        'cook_$root',
        'build/cook-fuzz/$root.cook is missing (make build/cook-fuzz/.stamp)',
      );
      continue;
    }
    cooks.add(cookCase(root));
  }
  for (final c in cooks) {
    checkCookImage(c);
  }

  final seenBlock = <String>{};
  for (final c in blocks) {
    syntheticBlock(c.info, seenBlock);
  }
  final seenCook = <String>{};
  for (final c in cooks) {
    syntheticCook(c.info, seenCook);
  }

  if (blockRows == 0 || cookNodes == 0 || syntheticRecords == 0) {
    fail(
      'agree',
      'a pass read nothing: $blockRows block rows, $cookNodes cook nodes, '
          '$syntheticRecords synthetic records',
    );
  }
  // the all-ones round puts a NaN in every float cell, so a run that met none
  // never held an accessor to the refusal (§3.3)
  if (nonFiniteCells == 0) {
    fail('agree', 'no float accessor met a NaN or an infinity');
  }
  stdout.writeln(
    'dart tables agree: $blockRows block rows, $cookNodes cook nodes '
    '($cookRecords cook records), ${seenBlock.length} block and ${seenCook.length} '
    'cook record types over $syntheticRecords synthetic reads, $reads field reads, '
    '$nonFiniteCells non-finite float cells refused, $disagreements disagreement(s)',
  );
  exit(disagreements == 0 ? 0 : 1);
}
