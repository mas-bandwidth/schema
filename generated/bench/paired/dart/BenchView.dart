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
  ViewField(this.name, this.json, this.typeName, this.id,
      this.optional, this.kind, this.bound, this.elemSize,
      this.read, this.write, this.doc, this.tags);
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
final UnitViewInfo _unitView = UnitViewInfo("bench", -3949331657491202337,
  <ViewType>[
    // type BenchBits
    ViewType("BenchBits","Bench.schema","",<String>[],<ViewField>[
      ViewField("b7","-","bits(7)",0,false,6,0,4,(dynamic v) => v.b7,(dynamic v, dynamic x) { v.b7 = x; },"",<String>[]),
      ViewField("b13","-","bits(13)",0,false,7,0,4,(dynamic v) => v.b13,(dynamic v, dynamic x) { v.b13 = x; },"",<String>[]),
      ViewField("b23","-","bits(23)",0,false,8,0,4,(dynamic v) => v.b23,(dynamic v, dynamic x) { v.b23 = x; },"",<String>[]),
      ViewField("b3","-","bits(3)",0,false,6,0,4,(dynamic v) => v.b3,(dynamic v, dynamic x) { v.b3 = x; },"",<String>[]),
      ViewField("b32","-","bits(32)",0,false,8,0,4,(dynamic v) => v.b32,(dynamic v, dynamic x) { v.b32 = x; },"",<String>[]),
      ViewField("b11","-","bits(11)",0,false,7,0,4,(dynamic v) => v.b11,(dynamic v, dynamic x) { v.b11 = x; },"",<String>[]),
      ViewField("b19","-","bits(19)",0,false,8,0,4,(dynamic v) => v.b19,(dynamic v, dynamic x) { v.b19 = x; },"",<String>[]),
      ViewField("b48","-","bits(48)",0,false,9,0,8,(dynamic v) => v.b48,(dynamic v, dynamic x) { v.b48 = x; },"",<String>[]),
    ]),
    // type BenchInts
    ViewType("BenchInts","Bench.schema","",<String>[],<ViewField>[
      ViewField("f0","-","int32",0,false,4,0,4,(dynamic v) => v.f0,(dynamic v, dynamic x) { v.f0 = x; },"",<String>[]),
      ViewField("f1","-","int32",0,false,4,0,4,(dynamic v) => v.f1,(dynamic v, dynamic x) { v.f1 = x; },"",<String>[]),
      ViewField("f2","-","int32",0,false,4,0,4,(dynamic v) => v.f2,(dynamic v, dynamic x) { v.f2 = x; },"",<String>[]),
      ViewField("f3","-","int32",0,false,4,0,4,(dynamic v) => v.f3,(dynamic v, dynamic x) { v.f3 = x; },"",<String>[]),
      ViewField("f4","-","int32",0,false,4,0,4,(dynamic v) => v.f4,(dynamic v, dynamic x) { v.f4 = x; },"",<String>[]),
      ViewField("f5","-","int32",0,false,4,0,4,(dynamic v) => v.f5,(dynamic v, dynamic x) { v.f5 = x; },"",<String>[]),
      ViewField("f6","-","int32",0,false,4,0,4,(dynamic v) => v.f6,(dynamic v, dynamic x) { v.f6 = x; },"",<String>[]),
      ViewField("f7","-","int32",0,false,4,0,4,(dynamic v) => v.f7,(dynamic v, dynamic x) { v.f7 = x; },"",<String>[]),
      ViewField("f8","-","int32",0,false,4,0,4,(dynamic v) => v.f8,(dynamic v, dynamic x) { v.f8 = x; },"",<String>[]),
      ViewField("f9","-","int32",0,false,4,0,4,(dynamic v) => v.f9,(dynamic v, dynamic x) { v.f9 = x; },"",<String>[]),
    ]),
    // type BenchMixed
    ViewType("BenchMixed","Bench.schema","",<String>[],<ViewField>[
      ViewField("sequence","sequence","bits(16)",-6181000666011064152,false,7,0,4,(dynamic v) => v.sequence,(dynamic v, dynamic x) { v.sequence = x; },"",<String>[]),
      ViewField("ack_sequence","ack_sequence","int32",990229364654832702,false,4,0,4,(dynamic v) => v.ackSequence,(dynamic v, dynamic x) { v.ackSequence = x; },"",<String>[]),
      ViewField("ack_bits","ack_bits","bits(32)",-7228825333587644925,false,8,0,4,(dynamic v) => v.ackBits,(dynamic v, dynamic x) { v.ackBits = x; },"",<String>[]),
      ViewField("session_id","session_id","uint64",-5199487799206016251,false,9,0,8,(dynamic v) => v.sessionId,(dynamic v, dynamic x) { v.sessionId = x; },"",<String>[]),
      ViewField("client_id","client_id","uint32",7889067272199050878,false,8,0,4,(dynamic v) => v.clientId,(dynamic v, dynamic x) { v.clientId = x; },"",<String>[]),
      ViewField("nonce","nonce","uint64",8334276637232054488,false,9,0,8,(dynamic v) => v.nonce,(dynamic v, dynamic x) { v.nonce = x; },"",<String>[]),
      ViewField("world_time","world_time","int64",4534679873638431877,false,5,0,8,(dynamic v) => v.worldTime,(dynamic v, dynamic x) { v.worldTime = x; },"",<String>[]),
      ViewField("frame_tick","frame_tick","bits(48)",8916005070914257170,false,9,0,8,(dynamic v) => v.frameTick,(dynamic v, dynamic x) { v.frameTick = x; },"",<String>[]),
      ViewField("server_time","server_time","fixed(24, 8)",4343163795393112518,false,22,0,4,(dynamic v) => v.serverTime,(dynamic v, dynamic x) { v.serverTime = x; },"",<String>[]),
      ViewField("entities","entities","MixedEntity",-7828083326636031446,false,13,8,null,(dynamic v) => v.entities,(dynamic v, dynamic x) { v.entities = x; },"",<String>[]),
      ViewField("stats","stats","MixedStat",-1268998401975805620,false,13,80,null,(dynamic v) => v.stats,(dynamic v, dynamic x) { v.stats = x; },"",<String>[]),
      ViewField("game_event","game_event","MixedEvent",3329809749115557776,false,15,0,null,(dynamic v) => v.gameEvent,(dynamic v, dynamic x) { v.gameEvent = x; },"",<String>[]),
      ViewField("loadout","loadout","uint8",6294288958393922979,false,6,4,1,(dynamic v) => v.loadout,(dynamic v, dynamic x) { v.loadout = x; },"",<String>[]),
      ViewField("player_name","player_name","string",1415871171432867950,false,12,15,1,(dynamic v) => v.playerName,(dynamic v, dynamic x) { v.playerName = x; },"",<String>[]),
      ViewField("payload","payload","bytes",-3478843999652550171,false,6,16,1,(dynamic v) => v.payload,(dynamic v, dynamic x) { v.payload = x; },"",<String>[]),
      ViewField("aim_x","aim_x","float32",-2616736631205226807,false,10,0,4,(dynamic v) => v.aimX,(dynamic v, dynamic x) { v.aimX = x; },"",<String>[]),
      ViewField("aim_y","aim_y","float32",-2616737730716855018,false,10,0,4,(dynamic v) => v.aimY,(dynamic v, dynamic x) { v.aimY = x; },"",<String>[]),
      ViewField("aim_z","aim_z","float32",-2616738830228483229,false,10,0,4,(dynamic v) => v.aimZ,(dynamic v, dynamic x) { v.aimZ = x; },"",<String>[]),
      ViewField("recoil","recoil","float32",-7138431908913683369,false,10,0,4,(dynamic v) => v.recoil,(dynamic v, dynamic x) { v.recoil = x; },"",<String>[]),
      ViewField("drift","drift","float64",6535842126802938884,false,11,0,8,(dynamic v) => v.drift,(dynamic v, dynamic x) { v.drift = x; },"",<String>[]),
      ViewField("wide_key","wide_key","uint128",-6686572760759554282,false,19,0,16,(dynamic v) => v.wideKey,(dynamic v, dynamic x) { v.wideKey = x; },"",<String>[]),
      ViewField("flux","flux","int128",-3018575613315307966,false,18,0,16,(dynamic v) => v.flux,(dynamic v, dynamic x) { v.flux = x; },"",<String>[]),
      ViewField("ping","ping","ufixed(8, 8)",-4669986463835551831,false,26,0,2,(dynamic v) => v.ping,(dynamic v, dynamic x) { v.ping = x; },"",<String>[]),
      ViewField("crc_hint","crc_hint","bits(24)",6200723483573899615,false,8,0,4,(dynamic v) => v.crcHint,(dynamic v, dynamic x) { v.crcHint = x; },"",<String>[]),
      ViewField("has_extra","has_extra","bool",-4574933642144545166,false,1,0,1,(dynamic v) => v.hasExtra,(dynamic v, dynamic x) { v.hasExtra = x; },"",<String>[]),
      ViewField("extra","extra","int32",-204370544148493463,false,4,0,4,(dynamic v) => v.extra,(dynamic v, dynamic x) { v.extra = x; },"",<String>[]),
      ViewField("idle_ticks","idle_ticks","int32",8651444298976115966,false,4,0,4,(dynamic v) => v.idleTicks,(dynamic v, dynamic x) { v.idleTicks = x; },"",<String>[]),
    ]),
    // type BenchPacket
    ViewType("BenchPacket","Bench.schema","",<String>[],<ViewField>[
      ViewField("a","-","int32",0,false,4,0,4,(dynamic v) => v.a,(dynamic v, dynamic x) { v.a = x; },"",<String>[]),
      ViewField("b","-","int32",0,false,4,0,4,(dynamic v) => v.b,(dynamic v, dynamic x) { v.b = x; },"",<String>[]),
      ViewField("c","-","int32",0,false,4,0,4,(dynamic v) => v.c,(dynamic v, dynamic x) { v.c = x; },"",<String>[]),
      ViewField("bits7","-","bits(7)",0,false,6,0,4,(dynamic v) => v.bits7,(dynamic v, dynamic x) { v.bits7 = x; },"",<String>[]),
      ViewField("bits13","-","bits(13)",0,false,7,0,4,(dynamic v) => v.bits13,(dynamic v, dynamic x) { v.bits13 = x; },"",<String>[]),
      ViewField("bits23","-","bits(23)",0,false,8,0,4,(dynamic v) => v.bits23,(dynamic v, dynamic x) { v.bits23 = x; },"",<String>[]),
      ViewField("flag","-","bool",0,false,1,0,1,(dynamic v) => v.flag,(dynamic v, dynamic x) { v.flag = x; },"",<String>[]),
      ViewField("x","-","float32",0,false,10,0,4,(dynamic v) => v.x,(dynamic v, dynamic x) { v.x = x; },"",<String>[]),
      ViewField("y","-","float32",0,false,10,0,4,(dynamic v) => v.y,(dynamic v, dynamic x) { v.y = x; },"",<String>[]),
      ViewField("z","-","float32",0,false,10,0,4,(dynamic v) => v.z,(dynamic v, dynamic x) { v.z = x; },"",<String>[]),
      ViewField("big","-","uint64",0,false,9,0,8,(dynamic v) => v.big,(dynamic v, dynamic x) { v.big = x; },"",<String>[]),
      ViewField("blob","-","uint8",0,false,6,17,1,(dynamic v) => v.blob,(dynamic v, dynamic x) { v.blob = x; },"",<String>[]),
    ]),
    // type MixedChatEvent
    ViewType("MixedChatEvent","Bench.schema","",<String>[],<ViewField>[
      ViewField("channel","channel","int32",-6556890747741999708,false,4,0,4,(dynamic v) => v.channel,(dynamic v, dynamic x) { v.channel = x; },"",<String>[]),
      ViewField("speaker","speaker","bits(12)",-292263051557875678,false,7,0,4,(dynamic v) => v.speaker,(dynamic v, dynamic x) { v.speaker = x; },"",<String>[]),
    ]),
    // type MixedEntity
    ViewType("MixedEntity","Bench.schema","",<String>[],<ViewField>[
      ViewField("entity_id","entity_id","bits(12)",2593226102015223570,false,7,0,4,(dynamic v) => v.entityId,(dynamic v, dynamic x) { v.entityId = x; },"",<String>[]),
      ViewField("pos_x","pos_x","int32",-3797881157997612786,false,4,0,4,(dynamic v) => v.posX,(dynamic v, dynamic x) { v.posX = x; },"",<String>[]),
      ViewField("pos_y","pos_y","int32",-3797880058485984575,false,4,0,4,(dynamic v) => v.posY,(dynamic v, dynamic x) { v.posY = x; },"",<String>[]),
      ViewField("pos_z","pos_z","int32",-3797883357020869208,false,4,0,4,(dynamic v) => v.posZ,(dynamic v, dynamic x) { v.posZ = x; },"",<String>[]),
      ViewField("yaw","yaw","bits(9)",-5382489739573192984,false,7,0,4,(dynamic v) => v.yaw,(dynamic v, dynamic x) { v.yaw = x; },"",<String>[]),
      ViewField("pitch","pitch","bits(9)",6028620492700434865,false,7,0,4,(dynamic v) => v.pitch,(dynamic v, dynamic x) { v.pitch = x; },"",<String>[]),
      ViewField("vel_x","vel_x","int32",7849183398884077159,false,4,0,4,(dynamic v) => v.velX,(dynamic v, dynamic x) { v.velX = x; },"",<String>[]),
      ViewField("vel_y","vel_y","int32",7849182299372448948,false,4,0,4,(dynamic v) => v.velY,(dynamic v, dynamic x) { v.velY = x; },"",<String>[]),
      ViewField("vel_z","vel_z","int32",7849185597907333581,false,4,0,4,(dynamic v) => v.velZ,(dynamic v, dynamic x) { v.velZ = x; },"",<String>[]),
      ViewField("health","health","int32",9181103189905877455,false,4,0,4,(dynamic v) => v.health,(dynamic v, dynamic x) { v.health = x; },"",<String>[]),
      ViewField("weapon","weapon","MixedWeapon",-6866282850657997311,false,30,0,1,(dynamic v) => v.weapon,(dynamic v, dynamic x) { v.weapon = x; },"",<String>[]),
      ViewField("damage","damage","MixedDamage",9179190079975030720,false,9,0,8,(dynamic v) => v.damage,(dynamic v, dynamic x) { v.damage = x; },"",<String>[]),
      ViewField("moving","moving","bool",1271228688681418151,false,1,0,1,(dynamic v) => v.moving,(dynamic v, dynamic x) { v.moving = x; },"",<String>[]),
      ViewField("firing","firing","bool",8535675692938965450,false,1,0,1,(dynamic v) => v.firing,(dynamic v, dynamic x) { v.firing = x; },"",<String>[]),
    ]),
    // type MixedHitEvent
    ViewType("MixedHitEvent","Bench.schema","",<String>[],<ViewField>[
      ViewField("target_id","target_id","bits(12)",-5207116919359778736,false,7,0,4,(dynamic v) => v.targetId,(dynamic v, dynamic x) { v.targetId = x; },"",<String>[]),
      ViewField("damage","damage","int32",9179190079975030720,false,4,0,4,(dynamic v) => v.damage,(dynamic v, dynamic x) { v.damage = x; },"",<String>[]),
      ViewField("hit_kind","hit_kind","int32",142922654710085925,false,4,0,4,(dynamic v) => v.hitKind,(dynamic v, dynamic x) { v.hitKind = x; },"",<String>[]),
      ViewField("crit","crit","bool",1324453635955533101,false,1,0,1,(dynamic v) => v.crit,(dynamic v, dynamic x) { v.crit = x; },"",<String>[]),
    ]),
    // type MixedPickupEvent
    ViewType("MixedPickupEvent","Bench.schema","",<String>[],<ViewField>[
      ViewField("item_id","item_id","bits(10)",-7025667724851298986,false,7,0,4,(dynamic v) => v.itemId,(dynamic v, dynamic x) { v.itemId = x; },"",<String>[]),
      ViewField("amount","amount","int32",-9145686598410475159,false,4,0,4,(dynamic v) => v.amount,(dynamic v, dynamic x) { v.amount = x; },"",<String>[]),
    ]),
    // type MixedStat
    ViewType("MixedStat","Bench.schema","",<String>[],<ViewField>[
      ViewField("stat_id","stat_id","bits(8)",-9175110139929313435,false,6,0,4,(dynamic v) => v.statId,(dynamic v, dynamic x) { v.statId = x; },"",<String>[]),
      ViewField("delta","delta","int32",5910805692604981441,false,4,0,4,(dynamic v) => v.delta,(dynamic v, dynamic x) { v.delta = x; },"",<String>[]),
    ]),
  ],
  <ViewType>[
    // table FixedTable
    ViewType("FixedTable","FixedTable.schema","",<String>[],<ViewField>[
      ViewField("value","value","BenchMixed",8999596768310594794,false,13,0,null,(dynamic v) => v.value,(dynamic v, dynamic x) { v.value = x; },"",<String>[]),
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
