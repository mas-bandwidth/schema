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
final UnitViewInfo _unitView = UnitViewInfo("benchtable", 659251898228885343,
  <ViewType>[
    // type TableChatEvent
    ViewType("TableChatEvent","BenchTable.schema","",<String>[],<ViewField>[
      ViewField("channel","channel","int32",-6556890747741999708,false,4,0,4,(dynamic v) => v.channel,(dynamic v, dynamic x) { v.channel = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("speaker","speaker","bits(12)",-292263051557875678,false,7,0,4,(dynamic v) => v.speaker,(dynamic v, dynamic x) { v.speaker = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
    ]),
    // type TableHitEvent
    ViewType("TableHitEvent","BenchTable.schema","",<String>[],<ViewField>[
      ViewField("target_id","target_id","bits(12)",-5207116919359778736,false,7,0,4,(dynamic v) => v.targetId,(dynamic v, dynamic x) { v.targetId = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("damage","damage","int32",9179190079975030720,false,4,0,4,(dynamic v) => v.damage,(dynamic v, dynamic x) { v.damage = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("hit_kind","hit_kind","int32",142922654710085925,false,4,0,4,(dynamic v) => v.hitKind,(dynamic v, dynamic x) { v.hitKind = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("crit","crit","bool",1324453635955533101,false,1,0,1,(dynamic v) => v.crit,(dynamic v, dynamic x) { v.crit = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
    ]),
    // type TablePickupEvent
    ViewType("TablePickupEvent","BenchTable.schema","",<String>[],<ViewField>[
      ViewField("item_id","item_id","bits(10)",-7025667724851298986,false,7,0,4,(dynamic v) => v.itemId,(dynamic v, dynamic x) { v.itemId = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("amount","amount","int32",-9145686598410475159,false,4,0,4,(dynamic v) => v.amount,(dynamic v, dynamic x) { v.amount = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
    ]),
  ],
  <ViewType>[
    // table TableEntity
    ViewType("TableEntity","BenchTable.schema","",<String>[],<ViewField>[
      ViewField("entity_id","entity_id","bits(12)",2593226102015223570,false,7,0,4,(dynamic v) => v.entityId,(dynamic v, dynamic x) { v.entityId = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("pos_x","pos_x","int32",-3797881157997612786,false,4,0,4,(dynamic v) => v.posX,(dynamic v, dynamic x) { v.posX = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("pos_y","pos_y","int32",-3797880058485984575,false,4,0,4,(dynamic v) => v.posY,(dynamic v, dynamic x) { v.posY = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("pos_z","pos_z","int32",-3797883357020869208,false,4,0,4,(dynamic v) => v.posZ,(dynamic v, dynamic x) { v.posZ = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("yaw","yaw","bits(9)",-5382489739573192984,false,7,0,4,(dynamic v) => v.yaw,(dynamic v, dynamic x) { v.yaw = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("pitch","pitch","bits(9)",6028620492700434865,false,7,0,4,(dynamic v) => v.pitch,(dynamic v, dynamic x) { v.pitch = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("vel_x","vel_x","int32",7849183398884077159,false,4,0,4,(dynamic v) => v.velX,(dynamic v, dynamic x) { v.velX = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("vel_y","vel_y","int32",7849182299372448948,false,4,0,4,(dynamic v) => v.velY,(dynamic v, dynamic x) { v.velY = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("vel_z","vel_z","int32",7849185597907333581,false,4,0,4,(dynamic v) => v.velZ,(dynamic v, dynamic x) { v.velZ = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("health","health","int32",9181103189905877455,false,4,0,4,(dynamic v) => v.health,(dynamic v, dynamic x) { v.health = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("weapon","weapon","TableWeapon",-6866282850657997311,false,30,0,1,(dynamic v) => v.weapon,(dynamic v, dynamic x) { v.weapon = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("damage","damage","TableDamage",9179190079975030720,false,9,0,8,(dynamic v) => v.damage,(dynamic v, dynamic x) { v.damage = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("moving","moving","bool",1271228688681418151,false,1,0,1,(dynamic v) => v.moving,(dynamic v, dynamic x) { v.moving = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("firing","firing","bool",8535675692938965450,false,1,0,1,(dynamic v) => v.firing,(dynamic v, dynamic x) { v.firing = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
    ]),
    // table TableMixed
    ViewType("TableMixed","BenchTable.schema","",<String>[],<ViewField>[
      ViewField("protocol_magic","protocol_magic","uint16",7663561793681298941,false,7,0,2,(dynamic v) => v.protocolMagic,(dynamic v, dynamic x) { v.protocolMagic = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("sequence","sequence","bits(16)",-6181000666011064152,false,7,0,4,(dynamic v) => v.sequence,(dynamic v, dynamic x) { v.sequence = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("ack_sequence","ack_sequence","int32",990229364654832702,false,4,0,4,(dynamic v) => v.ackSequence,(dynamic v, dynamic x) { v.ackSequence = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("ack_bits","ack_bits","bits(32)",-7228825333587644925,false,8,0,4,(dynamic v) => v.ackBits,(dynamic v, dynamic x) { v.ackBits = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("session_id","session_id","uint64",-5199487799206016251,false,9,0,8,(dynamic v) => v.sessionId,(dynamic v, dynamic x) { v.sessionId = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("client_id","client_id","uint32",7889067272199050878,false,8,0,4,(dynamic v) => v.clientId,(dynamic v, dynamic x) { v.clientId = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("nonce","nonce","uint64",8334276637232054488,false,9,0,8,(dynamic v) => v.nonce,(dynamic v, dynamic x) { v.nonce = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("world_time","world_time","int64",4534679873638431877,false,5,0,8,(dynamic v) => v.worldTime,(dynamic v, dynamic x) { v.worldTime = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("frame_tick","frame_tick","bits(48)",8916005070914257170,false,9,0,8,(dynamic v) => v.frameTick,(dynamic v, dynamic x) { v.frameTick = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("server_time","server_time","float32",4343163795393112518,false,10,0,4,(dynamic v) => v.serverTime,(dynamic v, dynamic x) { v.serverTime = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("entities","entities","TableEntity",-7828083326636031446,false,13,8,null,(dynamic v) => v.entities,(dynamic v, dynamic x) { v.entities = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("stats","stats","TableStat",-1268998401975805620,false,13,80,null,(dynamic v) => v.stats,(dynamic v, dynamic x) { v.stats = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("game_event","game_event","TableEvent",3329809749115557776,false,15,0,null,(dynamic v) => v.gameEvent,(dynamic v, dynamic x) { v.gameEvent = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("loadout","loadout","uint8",6294288958393922979,false,6,4,1,(dynamic v) => v.loadout,(dynamic v, dynamic x) { v.loadout = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("player_name","player_name","string",1415871171432867950,false,12,15,1,(dynamic v) => v.playerName,(dynamic v, dynamic x) { v.playerName = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("payload","payload","bytes",-3478843999652550171,false,6,16,1,(dynamic v) => v.payload,(dynamic v, dynamic x) { v.payload = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("aim_x","aim_x","float32",-2616736631205226807,false,10,0,4,(dynamic v) => v.aimX,(dynamic v, dynamic x) { v.aimX = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("aim_y","aim_y","float32",-2616737730716855018,false,10,0,4,(dynamic v) => v.aimY,(dynamic v, dynamic x) { v.aimY = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("aim_z","aim_z","float32",-2616738830228483229,false,10,0,4,(dynamic v) => v.aimZ,(dynamic v, dynamic x) { v.aimZ = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("recoil","recoil","float32",-7138431908913683369,false,10,0,4,(dynamic v) => v.recoil,(dynamic v, dynamic x) { v.recoil = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("drift","drift","float64",6535842126802938884,false,11,0,8,(dynamic v) => v.drift,(dynamic v, dynamic x) { v.drift = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("wide_key","wide_key","uint64",-6686572760759554282,false,9,0,8,(dynamic v) => v.wideKey,(dynamic v, dynamic x) { v.wideKey = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("flux","flux","int64",-3018575613315307966,false,5,0,8,(dynamic v) => v.flux,(dynamic v, dynamic x) { v.flux = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("ping","ping","float32",-4669986463835551831,false,10,0,4,(dynamic v) => v.ping,(dynamic v, dynamic x) { v.ping = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("crc_hint","crc_hint","bits(24)",6200723483573899615,false,8,0,4,(dynamic v) => v.crcHint,(dynamic v, dynamic x) { v.crcHint = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("has_extra","has_extra","bool",-4574933642144545166,false,1,0,1,(dynamic v) => v.hasExtra,(dynamic v, dynamic x) { v.hasExtra = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("extra","extra","int32",-204370544148493463,false,4,0,4,(dynamic v) => v.extra,(dynamic v, dynamic x) { v.extra = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("idle_ticks","idle_ticks","int32",8651444298976115966,false,4,0,4,(dynamic v) => v.idleTicks,(dynamic v, dynamic x) { v.idleTicks = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
    ]),
    // table TableStat
    ViewType("TableStat","BenchTable.schema","",<String>[],<ViewField>[
      ViewField("stat_id","stat_id","bits(8)",-9175110139929313435,false,6,0,4,(dynamic v) => v.statId,(dynamic v, dynamic x) { v.statId = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
      ViewField("delta","delta","int32",5910805692604981441,false,4,0,4,(dynamic v) => v.delta,(dynamic v, dynamic x) { v.delta = x; },"",<String>[]), // ignore: avoid_dynamic_calls -- the unit view registry is type-agnostic by design
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
