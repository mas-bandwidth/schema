// Unit reflection (docs/SPEC-TABLES.md §8.1–8.5).
// This library is optional and carries every declared item.

final class ViewField {
  final String name, json, typeName, doc;
  final int id, kind, bound;
  // §8.1's managed-language storage columns: no fictitious byte offset.
  final int? elemSize;
  final dynamic Function(dynamic) read;
  final void Function(dynamic, dynamic) write;
  final bool optional;
  final List<String> tags;
  const ViewField(this.name, this.json, this.typeName, this.id,
      this.optional, this.kind, this.bound, this.elemSize,
      this.read, this.write, this.doc, this.tags);
  static dynamic _r0(dynamic v) => v.b7; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w0(dynamic v, dynamic x) { v.b7 = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r1(dynamic v) => v.b13; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w1(dynamic v, dynamic x) { v.b13 = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r2(dynamic v) => v.b23; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w2(dynamic v, dynamic x) { v.b23 = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r3(dynamic v) => v.b3; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w3(dynamic v, dynamic x) { v.b3 = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r4(dynamic v) => v.b32; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w4(dynamic v, dynamic x) { v.b32 = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r5(dynamic v) => v.b11; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w5(dynamic v, dynamic x) { v.b11 = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r6(dynamic v) => v.b19; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w6(dynamic v, dynamic x) { v.b19 = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r7(dynamic v) => v.b48; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w7(dynamic v, dynamic x) { v.b48 = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r8(dynamic v) => v.f0; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w8(dynamic v, dynamic x) { v.f0 = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r9(dynamic v) => v.f1; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w9(dynamic v, dynamic x) { v.f1 = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r10(dynamic v) => v.f2; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w10(dynamic v, dynamic x) { v.f2 = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r11(dynamic v) => v.f3; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w11(dynamic v, dynamic x) { v.f3 = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r12(dynamic v) => v.f4; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w12(dynamic v, dynamic x) { v.f4 = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r13(dynamic v) => v.f5; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w13(dynamic v, dynamic x) { v.f5 = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r14(dynamic v) => v.f6; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w14(dynamic v, dynamic x) { v.f6 = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r15(dynamic v) => v.f7; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w15(dynamic v, dynamic x) { v.f7 = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r16(dynamic v) => v.f8; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w16(dynamic v, dynamic x) { v.f8 = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r17(dynamic v) => v.f9; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w17(dynamic v, dynamic x) { v.f9 = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r18(dynamic v) => v.sequence; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w18(dynamic v, dynamic x) { v.sequence = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r19(dynamic v) => v.ackSequence; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w19(dynamic v, dynamic x) { v.ackSequence = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r20(dynamic v) => v.ackBits; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w20(dynamic v, dynamic x) { v.ackBits = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r21(dynamic v) => v.sessionId; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w21(dynamic v, dynamic x) { v.sessionId = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r22(dynamic v) => v.clientId; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w22(dynamic v, dynamic x) { v.clientId = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r23(dynamic v) => v.nonce; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w23(dynamic v, dynamic x) { v.nonce = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r24(dynamic v) => v.worldTime; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w24(dynamic v, dynamic x) { v.worldTime = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r25(dynamic v) => v.frameTick; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w25(dynamic v, dynamic x) { v.frameTick = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r26(dynamic v) => v.serverTime; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w26(dynamic v, dynamic x) { v.serverTime = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r27(dynamic v) => v.entities; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w27(dynamic v, dynamic x) { v.entities = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r28(dynamic v) => v.stats; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w28(dynamic v, dynamic x) { v.stats = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r29(dynamic v) => v.gameEvent; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w29(dynamic v, dynamic x) { v.gameEvent = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r30(dynamic v) => v.loadout; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w30(dynamic v, dynamic x) { v.loadout = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r31(dynamic v) => v.playerName; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w31(dynamic v, dynamic x) { v.playerName = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r32(dynamic v) => v.payload; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w32(dynamic v, dynamic x) { v.payload = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r33(dynamic v) => v.aimX; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w33(dynamic v, dynamic x) { v.aimX = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r34(dynamic v) => v.aimY; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w34(dynamic v, dynamic x) { v.aimY = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r35(dynamic v) => v.aimZ; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w35(dynamic v, dynamic x) { v.aimZ = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r36(dynamic v) => v.recoil; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w36(dynamic v, dynamic x) { v.recoil = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r37(dynamic v) => v.drift; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w37(dynamic v, dynamic x) { v.drift = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r38(dynamic v) => v.wideKey; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w38(dynamic v, dynamic x) { v.wideKey = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r39(dynamic v) => v.flux; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w39(dynamic v, dynamic x) { v.flux = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r40(dynamic v) => v.ping; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w40(dynamic v, dynamic x) { v.ping = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r41(dynamic v) => v.crcHint; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w41(dynamic v, dynamic x) { v.crcHint = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r42(dynamic v) => v.hasExtra; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w42(dynamic v, dynamic x) { v.hasExtra = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r43(dynamic v) => v.extra; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w43(dynamic v, dynamic x) { v.extra = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r44(dynamic v) => v.idleTicks; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w44(dynamic v, dynamic x) { v.idleTicks = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r45(dynamic v) => v.a; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w45(dynamic v, dynamic x) { v.a = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r46(dynamic v) => v.b; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w46(dynamic v, dynamic x) { v.b = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r47(dynamic v) => v.c; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w47(dynamic v, dynamic x) { v.c = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r48(dynamic v) => v.bits7; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w48(dynamic v, dynamic x) { v.bits7 = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r49(dynamic v) => v.bits13; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w49(dynamic v, dynamic x) { v.bits13 = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r50(dynamic v) => v.bits23; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w50(dynamic v, dynamic x) { v.bits23 = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r51(dynamic v) => v.flag; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w51(dynamic v, dynamic x) { v.flag = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r52(dynamic v) => v.x; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w52(dynamic v, dynamic x) { v.x = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r53(dynamic v) => v.y; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w53(dynamic v, dynamic x) { v.y = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r54(dynamic v) => v.z; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w54(dynamic v, dynamic x) { v.z = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r55(dynamic v) => v.big; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w55(dynamic v, dynamic x) { v.big = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r56(dynamic v) => v.blob; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w56(dynamic v, dynamic x) { v.blob = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r57(dynamic v) => v.channel; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w57(dynamic v, dynamic x) { v.channel = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r58(dynamic v) => v.speaker; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w58(dynamic v, dynamic x) { v.speaker = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r59(dynamic v) => v.entityId; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w59(dynamic v, dynamic x) { v.entityId = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r60(dynamic v) => v.posX; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w60(dynamic v, dynamic x) { v.posX = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r61(dynamic v) => v.posY; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w61(dynamic v, dynamic x) { v.posY = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r62(dynamic v) => v.posZ; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w62(dynamic v, dynamic x) { v.posZ = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r63(dynamic v) => v.yaw; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w63(dynamic v, dynamic x) { v.yaw = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r64(dynamic v) => v.pitch; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w64(dynamic v, dynamic x) { v.pitch = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r65(dynamic v) => v.velX; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w65(dynamic v, dynamic x) { v.velX = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r66(dynamic v) => v.velY; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w66(dynamic v, dynamic x) { v.velY = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r67(dynamic v) => v.velZ; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w67(dynamic v, dynamic x) { v.velZ = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r68(dynamic v) => v.health; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w68(dynamic v, dynamic x) { v.health = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r69(dynamic v) => v.weapon; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w69(dynamic v, dynamic x) { v.weapon = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r70(dynamic v) => v.damage; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w70(dynamic v, dynamic x) { v.damage = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r71(dynamic v) => v.moving; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w71(dynamic v, dynamic x) { v.moving = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r72(dynamic v) => v.firing; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w72(dynamic v, dynamic x) { v.firing = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r73(dynamic v) => v.targetId; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w73(dynamic v, dynamic x) { v.targetId = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r74(dynamic v) => v.damage; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w74(dynamic v, dynamic x) { v.damage = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r75(dynamic v) => v.hitKind; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w75(dynamic v, dynamic x) { v.hitKind = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r76(dynamic v) => v.crit; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w76(dynamic v, dynamic x) { v.crit = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r77(dynamic v) => v.itemId; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w77(dynamic v, dynamic x) { v.itemId = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r78(dynamic v) => v.amount; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w78(dynamic v, dynamic x) { v.amount = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r79(dynamic v) => v.statId; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w79(dynamic v, dynamic x) { v.statId = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r80(dynamic v) => v.delta; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w80(dynamic v, dynamic x) { v.delta = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r81(dynamic v) => v.value; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w81(dynamic v, dynamic x) { v.value = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
}
final class ViewType {
  final String name, file, doc;
  final List<String> tags;
  final List<ViewField> fields;
  const ViewType(this.name, this.file, this.doc, this.tags, this.fields);
}
final class ViewVariant {
  final int value, id;
  final String name, payload, overlay, doc;
  final bool record, field;
  final int? kind, bound;
  final List<String> tags;
  const ViewVariant(this.value, this.name, this.id, this.payload,
      this.record, this.field, this.kind, this.bound, this.overlay,
      this.doc, this.tags);
}
final class ViewVocabulary {
  final String name, file, doc;
  final int max, storageBits;
  final List<String> tags;
  final List<ViewVariant> variants;
  const ViewVocabulary(this.name, this.file, this.max, this.storageBits,
      this.doc, this.tags, this.variants);
}
final class ViewConstant {
  final String name, file, typeName, doc;
  final bool isFloat;
  final int intValue, floatBits;
  final List<String> tags;
  const ViewConstant(this.name, this.file, this.typeName, this.isFloat,
      this.intValue, this.floatBits, this.doc, this.tags);
}
final class UnitViewInfo {
  final String package;
  final int protocolId;
  final List<ViewType> types, tables;
  final List<ViewVocabulary> enums, flags, unions;
  final List<ViewConstant> constants;
  const UnitViewInfo(this.package, this.protocolId, this.types, this.tables,
      this.enums, this.flags, this.unions, this.constants);
}

// The registry is name-ordered; generated map-entry tables are not declarations (§2.8).
const UnitViewInfo _unitView = UnitViewInfo("bench", -3949331657491202337,
  <ViewType>[
    // type BenchBits
    ViewType("BenchBits","Bench.schema","",<String>[],<ViewField>[
      ViewField("b7","-","bits(7)",0,false,6,0,4,ViewField._r0,ViewField._w0,"",<String>[]),
      ViewField("b13","-","bits(13)",0,false,7,0,4,ViewField._r1,ViewField._w1,"",<String>[]),
      ViewField("b23","-","bits(23)",0,false,8,0,4,ViewField._r2,ViewField._w2,"",<String>[]),
      ViewField("b3","-","bits(3)",0,false,6,0,4,ViewField._r3,ViewField._w3,"",<String>[]),
      ViewField("b32","-","bits(32)",0,false,8,0,4,ViewField._r4,ViewField._w4,"",<String>[]),
      ViewField("b11","-","bits(11)",0,false,7,0,4,ViewField._r5,ViewField._w5,"",<String>[]),
      ViewField("b19","-","bits(19)",0,false,8,0,4,ViewField._r6,ViewField._w6,"",<String>[]),
      ViewField("b48","-","bits(48)",0,false,9,0,8,ViewField._r7,ViewField._w7,"",<String>[]),
    ]),
    // type BenchInts
    ViewType("BenchInts","Bench.schema","",<String>[],<ViewField>[
      ViewField("f0","-","int32",0,false,4,0,4,ViewField._r8,ViewField._w8,"",<String>[]),
      ViewField("f1","-","int32",0,false,4,0,4,ViewField._r9,ViewField._w9,"",<String>[]),
      ViewField("f2","-","int32",0,false,4,0,4,ViewField._r10,ViewField._w10,"",<String>[]),
      ViewField("f3","-","int32",0,false,4,0,4,ViewField._r11,ViewField._w11,"",<String>[]),
      ViewField("f4","-","int32",0,false,4,0,4,ViewField._r12,ViewField._w12,"",<String>[]),
      ViewField("f5","-","int32",0,false,4,0,4,ViewField._r13,ViewField._w13,"",<String>[]),
      ViewField("f6","-","int32",0,false,4,0,4,ViewField._r14,ViewField._w14,"",<String>[]),
      ViewField("f7","-","int32",0,false,4,0,4,ViewField._r15,ViewField._w15,"",<String>[]),
      ViewField("f8","-","int32",0,false,4,0,4,ViewField._r16,ViewField._w16,"",<String>[]),
      ViewField("f9","-","int32",0,false,4,0,4,ViewField._r17,ViewField._w17,"",<String>[]),
    ]),
    // type BenchMixed
    ViewType("BenchMixed","Bench.schema","",<String>[],<ViewField>[
      ViewField("sequence","sequence","bits(16)",-6181000666011064152,false,7,0,4,ViewField._r18,ViewField._w18,"",<String>[]),
      ViewField("ack_sequence","ack_sequence","int32",990229364654832702,false,4,0,4,ViewField._r19,ViewField._w19,"",<String>[]),
      ViewField("ack_bits","ack_bits","bits(32)",-7228825333587644925,false,8,0,4,ViewField._r20,ViewField._w20,"",<String>[]),
      ViewField("session_id","session_id","uint64",-5199487799206016251,false,9,0,8,ViewField._r21,ViewField._w21,"",<String>[]),
      ViewField("client_id","client_id","uint32",7889067272199050878,false,8,0,4,ViewField._r22,ViewField._w22,"",<String>[]),
      ViewField("nonce","nonce","uint64",8334276637232054488,false,9,0,8,ViewField._r23,ViewField._w23,"",<String>[]),
      ViewField("world_time","world_time","int64",4534679873638431877,false,5,0,8,ViewField._r24,ViewField._w24,"",<String>[]),
      ViewField("frame_tick","frame_tick","bits(48)",8916005070914257170,false,9,0,8,ViewField._r25,ViewField._w25,"",<String>[]),
      ViewField("server_time","server_time","fixed(24, 8)",4343163795393112518,false,22,0,4,ViewField._r26,ViewField._w26,"",<String>[]),
      ViewField("entities","entities","MixedEntity",-7828083326636031446,false,13,8,null,ViewField._r27,ViewField._w27,"",<String>[]),
      ViewField("stats","stats","MixedStat",-1268998401975805620,false,13,80,null,ViewField._r28,ViewField._w28,"",<String>[]),
      ViewField("game_event","game_event","MixedEvent",3329809749115557776,false,15,0,null,ViewField._r29,ViewField._w29,"",<String>[]),
      ViewField("loadout","loadout","uint8",6294288958393922979,false,6,4,1,ViewField._r30,ViewField._w30,"",<String>[]),
      ViewField("player_name","player_name","string",1415871171432867950,false,12,15,1,ViewField._r31,ViewField._w31,"",<String>[]),
      ViewField("payload","payload","bytes",-3478843999652550171,false,6,16,1,ViewField._r32,ViewField._w32,"",<String>[]),
      ViewField("aim_x","aim_x","float32",-2616736631205226807,false,10,0,4,ViewField._r33,ViewField._w33,"",<String>[]),
      ViewField("aim_y","aim_y","float32",-2616737730716855018,false,10,0,4,ViewField._r34,ViewField._w34,"",<String>[]),
      ViewField("aim_z","aim_z","float32",-2616738830228483229,false,10,0,4,ViewField._r35,ViewField._w35,"",<String>[]),
      ViewField("recoil","recoil","float32",-7138431908913683369,false,10,0,4,ViewField._r36,ViewField._w36,"",<String>[]),
      ViewField("drift","drift","float64",6535842126802938884,false,11,0,8,ViewField._r37,ViewField._w37,"",<String>[]),
      ViewField("wide_key","wide_key","uint128",-6686572760759554282,false,19,0,16,ViewField._r38,ViewField._w38,"",<String>[]),
      ViewField("flux","flux","int128",-3018575613315307966,false,18,0,16,ViewField._r39,ViewField._w39,"",<String>[]),
      ViewField("ping","ping","ufixed(8, 8)",-4669986463835551831,false,26,0,2,ViewField._r40,ViewField._w40,"",<String>[]),
      ViewField("crc_hint","crc_hint","bits(24)",6200723483573899615,false,8,0,4,ViewField._r41,ViewField._w41,"",<String>[]),
      ViewField("has_extra","has_extra","bool",-4574933642144545166,false,1,0,1,ViewField._r42,ViewField._w42,"",<String>[]),
      ViewField("extra","extra","int32",-204370544148493463,false,4,0,4,ViewField._r43,ViewField._w43,"",<String>[]),
      ViewField("idle_ticks","idle_ticks","int32",8651444298976115966,false,4,0,4,ViewField._r44,ViewField._w44,"",<String>[]),
    ]),
    // type BenchPacket
    ViewType("BenchPacket","Bench.schema","",<String>[],<ViewField>[
      ViewField("a","-","int32",0,false,4,0,4,ViewField._r45,ViewField._w45,"",<String>[]),
      ViewField("b","-","int32",0,false,4,0,4,ViewField._r46,ViewField._w46,"",<String>[]),
      ViewField("c","-","int32",0,false,4,0,4,ViewField._r47,ViewField._w47,"",<String>[]),
      ViewField("bits7","-","bits(7)",0,false,6,0,4,ViewField._r48,ViewField._w48,"",<String>[]),
      ViewField("bits13","-","bits(13)",0,false,7,0,4,ViewField._r49,ViewField._w49,"",<String>[]),
      ViewField("bits23","-","bits(23)",0,false,8,0,4,ViewField._r50,ViewField._w50,"",<String>[]),
      ViewField("flag","-","bool",0,false,1,0,1,ViewField._r51,ViewField._w51,"",<String>[]),
      ViewField("x","-","float32",0,false,10,0,4,ViewField._r52,ViewField._w52,"",<String>[]),
      ViewField("y","-","float32",0,false,10,0,4,ViewField._r53,ViewField._w53,"",<String>[]),
      ViewField("z","-","float32",0,false,10,0,4,ViewField._r54,ViewField._w54,"",<String>[]),
      ViewField("big","-","uint64",0,false,9,0,8,ViewField._r55,ViewField._w55,"",<String>[]),
      ViewField("blob","-","uint8",0,false,6,17,1,ViewField._r56,ViewField._w56,"",<String>[]),
    ]),
    // type MixedChatEvent
    ViewType("MixedChatEvent","Bench.schema","",<String>[],<ViewField>[
      ViewField("channel","channel","int32",-6556890747741999708,false,4,0,4,ViewField._r57,ViewField._w57,"",<String>[]),
      ViewField("speaker","speaker","bits(12)",-292263051557875678,false,7,0,4,ViewField._r58,ViewField._w58,"",<String>[]),
    ]),
    // type MixedEntity
    ViewType("MixedEntity","Bench.schema","",<String>[],<ViewField>[
      ViewField("entity_id","entity_id","bits(12)",2593226102015223570,false,7,0,4,ViewField._r59,ViewField._w59,"",<String>[]),
      ViewField("pos_x","pos_x","int32",-3797881157997612786,false,4,0,4,ViewField._r60,ViewField._w60,"",<String>[]),
      ViewField("pos_y","pos_y","int32",-3797880058485984575,false,4,0,4,ViewField._r61,ViewField._w61,"",<String>[]),
      ViewField("pos_z","pos_z","int32",-3797883357020869208,false,4,0,4,ViewField._r62,ViewField._w62,"",<String>[]),
      ViewField("yaw","yaw","bits(9)",-5382489739573192984,false,7,0,4,ViewField._r63,ViewField._w63,"",<String>[]),
      ViewField("pitch","pitch","bits(9)",6028620492700434865,false,7,0,4,ViewField._r64,ViewField._w64,"",<String>[]),
      ViewField("vel_x","vel_x","int32",7849183398884077159,false,4,0,4,ViewField._r65,ViewField._w65,"",<String>[]),
      ViewField("vel_y","vel_y","int32",7849182299372448948,false,4,0,4,ViewField._r66,ViewField._w66,"",<String>[]),
      ViewField("vel_z","vel_z","int32",7849185597907333581,false,4,0,4,ViewField._r67,ViewField._w67,"",<String>[]),
      ViewField("health","health","int32",9181103189905877455,false,4,0,4,ViewField._r68,ViewField._w68,"",<String>[]),
      ViewField("weapon","weapon","MixedWeapon",-6866282850657997311,false,30,0,1,ViewField._r69,ViewField._w69,"",<String>[]),
      ViewField("damage","damage","MixedDamage",9179190079975030720,false,9,0,8,ViewField._r70,ViewField._w70,"",<String>[]),
      ViewField("moving","moving","bool",1271228688681418151,false,1,0,1,ViewField._r71,ViewField._w71,"",<String>[]),
      ViewField("firing","firing","bool",8535675692938965450,false,1,0,1,ViewField._r72,ViewField._w72,"",<String>[]),
    ]),
    // type MixedHitEvent
    ViewType("MixedHitEvent","Bench.schema","",<String>[],<ViewField>[
      ViewField("target_id","target_id","bits(12)",-5207116919359778736,false,7,0,4,ViewField._r73,ViewField._w73,"",<String>[]),
      ViewField("damage","damage","int32",9179190079975030720,false,4,0,4,ViewField._r74,ViewField._w74,"",<String>[]),
      ViewField("hit_kind","hit_kind","int32",142922654710085925,false,4,0,4,ViewField._r75,ViewField._w75,"",<String>[]),
      ViewField("crit","crit","bool",1324453635955533101,false,1,0,1,ViewField._r76,ViewField._w76,"",<String>[]),
    ]),
    // type MixedPickupEvent
    ViewType("MixedPickupEvent","Bench.schema","",<String>[],<ViewField>[
      ViewField("item_id","item_id","bits(10)",-7025667724851298986,false,7,0,4,ViewField._r77,ViewField._w77,"",<String>[]),
      ViewField("amount","amount","int32",-9145686598410475159,false,4,0,4,ViewField._r78,ViewField._w78,"",<String>[]),
    ]),
    // type MixedStat
    ViewType("MixedStat","Bench.schema","",<String>[],<ViewField>[
      ViewField("stat_id","stat_id","bits(8)",-9175110139929313435,false,6,0,4,ViewField._r79,ViewField._w79,"",<String>[]),
      ViewField("delta","delta","int32",5910805692604981441,false,4,0,4,ViewField._r80,ViewField._w80,"",<String>[]),
    ]),
  ],
  <ViewType>[
    // table FixedTable
    ViewType("FixedTable","FixedTable.schema","",<String>[],<ViewField>[
      ViewField("value","value","BenchMixed",8999596768310594794,false,13,0,null,ViewField._r81,ViewField._w81,"",<String>[]),
    ]),
  ],
  <ViewVocabulary>[
    // enum MixedWeapon
    ViewVocabulary("MixedWeapon","Bench.schema",15,8,"",<String>[],<ViewVariant>[
      ViewVariant(0,'None',0,'-',false,false,null,null,'-','',<String>[]),
      ViewVariant(1,"Fists",-6372330821933150452,'-',false,false,null,null,'-',"",<String>[]),
      ViewVariant(2,"Pistol",-6935561395390217098,'-',false,false,null,null,'-',"",<String>[]),
      ViewVariant(3,"Shotgun",1768712988339112597,'-',false,false,null,null,'-',"",<String>[]),
      ViewVariant(4,"Rifle",3185847833948484337,'-',false,false,null,null,'-',"",<String>[]),
      ViewVariant(5,"Sniper",912176952613754358,'-',false,false,null,null,'-',"",<String>[]),
      ViewVariant(6,"Smg",-7467029643251279282,'-',false,false,null,null,'-',"",<String>[]),
      ViewVariant(7,"Rocket",2494154474894420565,'-',false,false,null,null,'-',"",<String>[]),
      ViewVariant(8,"Grenade",80074449652715421,'-',false,false,null,null,'-',"",<String>[]),
      ViewVariant(9,"Plasma",-8505234340531936929,'-',false,false,null,null,'-',"",<String>[]),
      ViewVariant(10,"Railgun",-8250835097599916951,'-',false,false,null,null,'-',"",<String>[]),
      ViewVariant(11,"Flamer",-2752528626751932394,'-',false,false,null,null,'-',"",<String>[]),
      ViewVariant(12,"Mine",350179810443481718,'-',false,false,null,null,'-',"",<String>[]),
      ViewVariant(13,"Turret",3883575963473318423,'-',false,false,null,null,'-',"",<String>[]),
      ViewVariant(14,"Drone",3226873318306908209,'-',false,false,null,null,'-',"",<String>[]),
      ViewVariant(15,"Repair",2358437193313989428,'-',false,false,null,null,'-',"",<String>[]),
    ]),
  ],
  <ViewVocabulary>[
    // flags MixedDamage
    ViewVocabulary("MixedDamage","Bench.schema",7,8,"",<String>[],<ViewVariant>[
      ViewVariant(0,"Bleeding",0,'-',false,false,null,null,'-',"",<String>[]),
      ViewVariant(1,"Burning",0,'-',false,false,null,null,'-',"",<String>[]),
      ViewVariant(2,"Stunned",0,'-',false,false,null,null,'-',"",<String>[]),
      ViewVariant(3,"Slowed",0,'-',false,false,null,null,'-',"",<String>[]),
      ViewVariant(4,"Poisoned",0,'-',false,false,null,null,'-',"",<String>[]),
      ViewVariant(5,"Shielded",0,'-',false,false,null,null,'-',"",<String>[]),
      ViewVariant(6,"Airborne",0,'-',false,false,null,null,'-',"",<String>[]),
      ViewVariant(7,"Downed",0,'-',false,false,null,null,'-',"",<String>[]),
    ]),
  ],
  <ViewVocabulary>[
    // union MixedEvent
    ViewVocabulary("MixedEvent","Bench.schema",3,8,"",<String>[],<ViewVariant>[
      ViewVariant(0,'None',0,'-',false,false,null,null,'-','',<String>[]),
      ViewVariant(1,"hit",3707351006901076138,"MixedHitEvent",true,false,null,null,"-","",<String>[]),
      ViewVariant(2,"chat",-962770241226853237,"MixedChatEvent",true,false,null,null,"-","",<String>[]),
      ViewVariant(3,"pickup",-6943525758045472923,"MixedPickupEvent",true,false,null,null,"-","",<String>[]),
    ]),
  ],
  <ViewConstant>[
  ]);
UnitViewInfo UnitView() => _unitView;
