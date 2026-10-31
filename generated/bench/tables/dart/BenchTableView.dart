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
  static dynamic _r0(dynamic v) => v.channel; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w0(dynamic v, dynamic x) { v.channel = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r1(dynamic v) => v.speaker; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w1(dynamic v, dynamic x) { v.speaker = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r2(dynamic v) => v.targetId; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w2(dynamic v, dynamic x) { v.targetId = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r3(dynamic v) => v.damage; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w3(dynamic v, dynamic x) { v.damage = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r4(dynamic v) => v.hitKind; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w4(dynamic v, dynamic x) { v.hitKind = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r5(dynamic v) => v.crit; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w5(dynamic v, dynamic x) { v.crit = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r6(dynamic v) => v.itemId; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w6(dynamic v, dynamic x) { v.itemId = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r7(dynamic v) => v.amount; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w7(dynamic v, dynamic x) { v.amount = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r8(dynamic v) => v.entityId; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w8(dynamic v, dynamic x) { v.entityId = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r9(dynamic v) => v.posX; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w9(dynamic v, dynamic x) { v.posX = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r10(dynamic v) => v.posY; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w10(dynamic v, dynamic x) { v.posY = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r11(dynamic v) => v.posZ; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w11(dynamic v, dynamic x) { v.posZ = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r12(dynamic v) => v.yaw; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w12(dynamic v, dynamic x) { v.yaw = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r13(dynamic v) => v.pitch; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w13(dynamic v, dynamic x) { v.pitch = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r14(dynamic v) => v.velX; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w14(dynamic v, dynamic x) { v.velX = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r15(dynamic v) => v.velY; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w15(dynamic v, dynamic x) { v.velY = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r16(dynamic v) => v.velZ; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w16(dynamic v, dynamic x) { v.velZ = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r17(dynamic v) => v.health; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w17(dynamic v, dynamic x) { v.health = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r18(dynamic v) => v.weapon; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w18(dynamic v, dynamic x) { v.weapon = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r19(dynamic v) => v.damage; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w19(dynamic v, dynamic x) { v.damage = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r20(dynamic v) => v.moving; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w20(dynamic v, dynamic x) { v.moving = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r21(dynamic v) => v.firing; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w21(dynamic v, dynamic x) { v.firing = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r22(dynamic v) => v.protocolMagic; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w22(dynamic v, dynamic x) { v.protocolMagic = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r23(dynamic v) => v.sequence; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w23(dynamic v, dynamic x) { v.sequence = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r24(dynamic v) => v.ackSequence; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w24(dynamic v, dynamic x) { v.ackSequence = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r25(dynamic v) => v.ackBits; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w25(dynamic v, dynamic x) { v.ackBits = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r26(dynamic v) => v.sessionId; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w26(dynamic v, dynamic x) { v.sessionId = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r27(dynamic v) => v.clientId; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w27(dynamic v, dynamic x) { v.clientId = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r28(dynamic v) => v.nonce; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w28(dynamic v, dynamic x) { v.nonce = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r29(dynamic v) => v.worldTime; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w29(dynamic v, dynamic x) { v.worldTime = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r30(dynamic v) => v.frameTick; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w30(dynamic v, dynamic x) { v.frameTick = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r31(dynamic v) => v.serverTime; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w31(dynamic v, dynamic x) { v.serverTime = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r32(dynamic v) => v.entities; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w32(dynamic v, dynamic x) { v.entities = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r33(dynamic v) => v.stats; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w33(dynamic v, dynamic x) { v.stats = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r34(dynamic v) => v.gameEvent; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w34(dynamic v, dynamic x) { v.gameEvent = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r35(dynamic v) => v.loadout; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w35(dynamic v, dynamic x) { v.loadout = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r36(dynamic v) => v.playerName; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w36(dynamic v, dynamic x) { v.playerName = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r37(dynamic v) => v.payload; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w37(dynamic v, dynamic x) { v.payload = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r38(dynamic v) => v.aimX; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w38(dynamic v, dynamic x) { v.aimX = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r39(dynamic v) => v.aimY; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w39(dynamic v, dynamic x) { v.aimY = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r40(dynamic v) => v.aimZ; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w40(dynamic v, dynamic x) { v.aimZ = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r41(dynamic v) => v.recoil; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w41(dynamic v, dynamic x) { v.recoil = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r42(dynamic v) => v.drift; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w42(dynamic v, dynamic x) { v.drift = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r43(dynamic v) => v.wideKey; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w43(dynamic v, dynamic x) { v.wideKey = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r44(dynamic v) => v.flux; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w44(dynamic v, dynamic x) { v.flux = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r45(dynamic v) => v.ping; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w45(dynamic v, dynamic x) { v.ping = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r46(dynamic v) => v.crcHint; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w46(dynamic v, dynamic x) { v.crcHint = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r47(dynamic v) => v.hasExtra; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w47(dynamic v, dynamic x) { v.hasExtra = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r48(dynamic v) => v.extra; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w48(dynamic v, dynamic x) { v.extra = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r49(dynamic v) => v.idleTicks; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w49(dynamic v, dynamic x) { v.idleTicks = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r50(dynamic v) => v.statId; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w50(dynamic v, dynamic x) { v.statId = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static dynamic _r51(dynamic v) => v.delta; // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
  static void _w51(dynamic v, dynamic x) { v.delta = x; } // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
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
const UnitViewInfo _unitView = UnitViewInfo("benchtable", 659251898228885343,
  <ViewType>[
    // type TableChatEvent
    ViewType("TableChatEvent","BenchTable.schema","",<String>[],<ViewField>[
      ViewField("channel","channel","int32",-6556890747741999708,false,4,0,4,ViewField._r0,ViewField._w0,"",<String>[]),
      ViewField("speaker","speaker","bits(12)",-292263051557875678,false,7,0,4,ViewField._r1,ViewField._w1,"",<String>[]),
    ]),
    // type TableHitEvent
    ViewType("TableHitEvent","BenchTable.schema","",<String>[],<ViewField>[
      ViewField("target_id","target_id","bits(12)",-5207116919359778736,false,7,0,4,ViewField._r2,ViewField._w2,"",<String>[]),
      ViewField("damage","damage","int32",9179190079975030720,false,4,0,4,ViewField._r3,ViewField._w3,"",<String>[]),
      ViewField("hit_kind","hit_kind","int32",142922654710085925,false,4,0,4,ViewField._r4,ViewField._w4,"",<String>[]),
      ViewField("crit","crit","bool",1324453635955533101,false,1,0,1,ViewField._r5,ViewField._w5,"",<String>[]),
    ]),
    // type TablePickupEvent
    ViewType("TablePickupEvent","BenchTable.schema","",<String>[],<ViewField>[
      ViewField("item_id","item_id","bits(10)",-7025667724851298986,false,7,0,4,ViewField._r6,ViewField._w6,"",<String>[]),
      ViewField("amount","amount","int32",-9145686598410475159,false,4,0,4,ViewField._r7,ViewField._w7,"",<String>[]),
    ]),
  ],
  <ViewType>[
    // table TableEntity
    ViewType("TableEntity","BenchTable.schema","",<String>[],<ViewField>[
      ViewField("entity_id","entity_id","bits(12)",2593226102015223570,false,7,0,4,ViewField._r8,ViewField._w8,"",<String>[]),
      ViewField("pos_x","pos_x","int32",-3797881157997612786,false,4,0,4,ViewField._r9,ViewField._w9,"",<String>[]),
      ViewField("pos_y","pos_y","int32",-3797880058485984575,false,4,0,4,ViewField._r10,ViewField._w10,"",<String>[]),
      ViewField("pos_z","pos_z","int32",-3797883357020869208,false,4,0,4,ViewField._r11,ViewField._w11,"",<String>[]),
      ViewField("yaw","yaw","bits(9)",-5382489739573192984,false,7,0,4,ViewField._r12,ViewField._w12,"",<String>[]),
      ViewField("pitch","pitch","bits(9)",6028620492700434865,false,7,0,4,ViewField._r13,ViewField._w13,"",<String>[]),
      ViewField("vel_x","vel_x","int32",7849183398884077159,false,4,0,4,ViewField._r14,ViewField._w14,"",<String>[]),
      ViewField("vel_y","vel_y","int32",7849182299372448948,false,4,0,4,ViewField._r15,ViewField._w15,"",<String>[]),
      ViewField("vel_z","vel_z","int32",7849185597907333581,false,4,0,4,ViewField._r16,ViewField._w16,"",<String>[]),
      ViewField("health","health","int32",9181103189905877455,false,4,0,4,ViewField._r17,ViewField._w17,"",<String>[]),
      ViewField("weapon","weapon","TableWeapon",-6866282850657997311,false,30,0,1,ViewField._r18,ViewField._w18,"",<String>[]),
      ViewField("damage","damage","TableDamage",9179190079975030720,false,9,0,8,ViewField._r19,ViewField._w19,"",<String>[]),
      ViewField("moving","moving","bool",1271228688681418151,false,1,0,1,ViewField._r20,ViewField._w20,"",<String>[]),
      ViewField("firing","firing","bool",8535675692938965450,false,1,0,1,ViewField._r21,ViewField._w21,"",<String>[]),
    ]),
    // table TableMixed
    ViewType("TableMixed","BenchTable.schema","",<String>[],<ViewField>[
      ViewField("protocol_magic","protocol_magic","uint16",7663561793681298941,false,7,0,2,ViewField._r22,ViewField._w22,"",<String>[]),
      ViewField("sequence","sequence","bits(16)",-6181000666011064152,false,7,0,4,ViewField._r23,ViewField._w23,"",<String>[]),
      ViewField("ack_sequence","ack_sequence","int32",990229364654832702,false,4,0,4,ViewField._r24,ViewField._w24,"",<String>[]),
      ViewField("ack_bits","ack_bits","bits(32)",-7228825333587644925,false,8,0,4,ViewField._r25,ViewField._w25,"",<String>[]),
      ViewField("session_id","session_id","uint64",-5199487799206016251,false,9,0,8,ViewField._r26,ViewField._w26,"",<String>[]),
      ViewField("client_id","client_id","uint32",7889067272199050878,false,8,0,4,ViewField._r27,ViewField._w27,"",<String>[]),
      ViewField("nonce","nonce","uint64",8334276637232054488,false,9,0,8,ViewField._r28,ViewField._w28,"",<String>[]),
      ViewField("world_time","world_time","int64",4534679873638431877,false,5,0,8,ViewField._r29,ViewField._w29,"",<String>[]),
      ViewField("frame_tick","frame_tick","bits(48)",8916005070914257170,false,9,0,8,ViewField._r30,ViewField._w30,"",<String>[]),
      ViewField("server_time","server_time","float32",4343163795393112518,false,10,0,4,ViewField._r31,ViewField._w31,"",<String>[]),
      ViewField("entities","entities","TableEntity",-7828083326636031446,false,13,8,null,ViewField._r32,ViewField._w32,"",<String>[]),
      ViewField("stats","stats","TableStat",-1268998401975805620,false,13,80,null,ViewField._r33,ViewField._w33,"",<String>[]),
      ViewField("game_event","game_event","TableEvent",3329809749115557776,false,15,0,null,ViewField._r34,ViewField._w34,"",<String>[]),
      ViewField("loadout","loadout","uint8",6294288958393922979,false,6,4,1,ViewField._r35,ViewField._w35,"",<String>[]),
      ViewField("player_name","player_name","string",1415871171432867950,false,12,15,1,ViewField._r36,ViewField._w36,"",<String>[]),
      ViewField("payload","payload","bytes",-3478843999652550171,false,6,16,1,ViewField._r37,ViewField._w37,"",<String>[]),
      ViewField("aim_x","aim_x","float32",-2616736631205226807,false,10,0,4,ViewField._r38,ViewField._w38,"",<String>[]),
      ViewField("aim_y","aim_y","float32",-2616737730716855018,false,10,0,4,ViewField._r39,ViewField._w39,"",<String>[]),
      ViewField("aim_z","aim_z","float32",-2616738830228483229,false,10,0,4,ViewField._r40,ViewField._w40,"",<String>[]),
      ViewField("recoil","recoil","float32",-7138431908913683369,false,10,0,4,ViewField._r41,ViewField._w41,"",<String>[]),
      ViewField("drift","drift","float64",6535842126802938884,false,11,0,8,ViewField._r42,ViewField._w42,"",<String>[]),
      ViewField("wide_key","wide_key","uint64",-6686572760759554282,false,9,0,8,ViewField._r43,ViewField._w43,"",<String>[]),
      ViewField("flux","flux","int64",-3018575613315307966,false,5,0,8,ViewField._r44,ViewField._w44,"",<String>[]),
      ViewField("ping","ping","float32",-4669986463835551831,false,10,0,4,ViewField._r45,ViewField._w45,"",<String>[]),
      ViewField("crc_hint","crc_hint","bits(24)",6200723483573899615,false,8,0,4,ViewField._r46,ViewField._w46,"",<String>[]),
      ViewField("has_extra","has_extra","bool",-4574933642144545166,false,1,0,1,ViewField._r47,ViewField._w47,"",<String>[]),
      ViewField("extra","extra","int32",-204370544148493463,false,4,0,4,ViewField._r48,ViewField._w48,"",<String>[]),
      ViewField("idle_ticks","idle_ticks","int32",8651444298976115966,false,4,0,4,ViewField._r49,ViewField._w49,"",<String>[]),
    ]),
    // table TableStat
    ViewType("TableStat","BenchTable.schema","",<String>[],<ViewField>[
      ViewField("stat_id","stat_id","bits(8)",-9175110139929313435,false,6,0,4,ViewField._r50,ViewField._w50,"",<String>[]),
      ViewField("delta","delta","int32",5910805692604981441,false,4,0,4,ViewField._r51,ViewField._w51,"",<String>[]),
    ]),
  ],
  <ViewVocabulary>[
    // enum TableWeapon
    ViewVocabulary("TableWeapon","BenchTable.schema",15,8,"",<String>[],<ViewVariant>[
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
    // flags TableDamage
    ViewVocabulary("TableDamage","BenchTable.schema",7,8,"",<String>[],<ViewVariant>[
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
    // union TableEvent
    ViewVocabulary("TableEvent","BenchTable.schema",3,8,"",<String>[],<ViewVariant>[
      ViewVariant(0,'None',0,'-',false,false,null,null,'-','',<String>[]),
      ViewVariant(1,"hit",3707351006901076138,"TableHitEvent",true,false,null,null,"-","",<String>[]),
      ViewVariant(2,"chat",-962770241226853237,"TableChatEvent",true,false,null,null,"-","",<String>[]),
      ViewVariant(3,"pickup",-6943525758045472923,"TablePickupEvent",true,false,null,null,"-","",<String>[]),
    ]),
  ],
  <ViewConstant>[
  ]);
UnitViewInfo UnitView() => _unitView;
