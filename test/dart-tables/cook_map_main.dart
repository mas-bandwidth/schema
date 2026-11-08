// THE DART COOK'S MAP AND LIST GATE (docs/PORTING.md M19, docs/SPEC-TABLES.md
// §2.8, §2.9, §7.6), run by `make tables-dart-cook-map`.
//
//   dart cook_map_main.dart <manifest>
//
// For every map and list cook-write row of the manifest (the mapdemo,
// listdemo and widelist units), the instance's wire is loaded and cooked in
// both byte orders, and the bytes must be the two files `schema cook` wrote.
// Then THE WRITER HOLDS THE SORT: every map of the loaded value is reversed,
// at every depth, and the cook must not move by one byte — nothing upstream of
// the writer sorts a Dart map, which is a List of entries in the caller's
// order. Then the READER: every key of map_full's maps, at both depths, is
// found in place by `<field>Find`, a key the map lacks is not, and every list
// of list_tables is read in place by `<field>Count` and `<field>At`.
//
// The working directory is the repository root, which the manifest's paths
// are relative to.
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import '../../build/tables-generated-dart/lists/ListdemoWire.dart' as listdemo;
import '../../build/tables-generated-dart/lists/SaveCook.dart' as lsave;
import '../../build/tables-generated-dart/lists/SharedCook.dart' as lshared;
import '../../build/tables-generated-dart/lists/HoldersCook.dart' as lholders;
import '../../build/tables-generated-dart/lists/MigrateCook.dart' as lmigrate;
import '../../build/tables-generated-dart/maps/MapdemoWire.dart' as mapdemo;
import '../../build/tables-generated-dart/maps/CellsCook.dart' as mcells;
import '../../build/tables-generated-dart/maps/ChunksCook.dart' as mchunks;
import '../../build/tables-generated-dart/maps/CrewsCook.dart' as mcrews;
import '../../build/tables-generated-dart/maps/DepthCook.dart' as mdepth;
import '../../build/tables-generated-dart/maps/DocsCook.dart' as mdocs;
import '../../build/tables-generated-dart/maps/FleetCook.dart' as mfleet;
import '../../build/tables-generated-dart/maps/PairsCook.dart' as mpairs;
import '../../build/tables-generated-dart/maps/RunsCook.dart' as mruns;
import '../../build/tables-generated-dart/maps/SlotsCook.dart' as mslots;
import '../../build/tables-generated-dart/maps/SpansCook.dart' as mspans;
import '../../build/tables-generated-dart/maps/TextCook.dart' as mtext;
import '../../build/tables-generated-dart/maps/TrailsCook.dart' as mtrails;
import '../../build/tables-generated-dart/widelist/WidelistdemoWire.dart'
    as widelist;
import '../../build/tables-generated-dart/widelist/WidesCook.dart' as mwide;

var failures = 0;
var checked = 0;

void fail(String what) {
  stderr.writeln('FAIL: $what');
  failures++;
}

// One row's codec: the measure and the writer over a loaded value, and the
// reversal of every map the value holds, at every depth (null for a unit
// whose root holds no map).
final class Codec {
  final Object Function(Uint8List wire) load;
  final int Function(Object value) measure;
  final bool Function(Object value, Uint8List out, Endian order) cook;
  final void Function(Object value)? reverse;
  const Codec(this.load, this.measure, this.cook, [this.reverse]);
}

void rev<T>(List<T> l) {
  final r = l.reversed.toList();
  l
    ..clear()
    ..addAll(r);
}

void revSquad(mapdemo.Squad s) => rev(s.roster);

final Map<String, Codec> codecs = {
  'listdemo Save': Codec(
    (w) => listdemo.saveWireLoad(w),
    (v) => lsave.SaveCook.cookMeasure(v as listdemo.Save),
    (v, o, e) => lsave.SaveCook.cook(v as listdemo.Save, o, e),
  ),
  'listdemo Mixed': Codec(
    (w) => listdemo.mixedWireLoad(w),
    (v) => lsave.MixedCook.cookMeasure(v as listdemo.Mixed),
    (v, o, e) => lsave.MixedCook.cook(v as listdemo.Mixed, o, e),
  ),
  'listdemo Album': Codec(
    (w) => listdemo.albumWireLoad(w),
    (v) => lshared.AlbumCook.cookMeasure(v as listdemo.Album),
    (v, o, e) => lshared.AlbumCook.cook(v as listdemo.Album, o, e),
  ),
  'listdemo Sheet': Codec(
    (w) => listdemo.sheetWireLoad(w),
    (v) => lholders.SheetCook.cookMeasure(v as listdemo.Sheet),
    (v, o, e) => lholders.SheetCook.cook(v as listdemo.Sheet, o, e),
  ),
  'listdemo Army': Codec(
    (w) => listdemo.armyWireLoad(w),
    (v) => lholders.ArmyCook.cookMeasure(v as listdemo.Army),
    (v, o, e) => lholders.ArmyCook.cook(v as listdemo.Army, o, e),
    (v) {
      for (final s in (v as listdemo.Army).squads) {
        rev(s.roster);
      }
    },
  ),
  'listdemo Unbounded': Codec(
    (w) => listdemo.unboundedWireLoad(w),
    (v) => lmigrate.UnboundedCook.cookMeasure(v as listdemo.Unbounded),
    (v, o, e) => lmigrate.UnboundedCook.cook(v as listdemo.Unbounded, o, e),
  ),
  'mapdemo Cells': Codec(
    (w) => mapdemo.cellsWireLoad(w),
    (v) => mcells.CellsCook.cookMeasure(v as mapdemo.Cells),
    (v, o, e) => mcells.CellsCook.cook(v as mapdemo.Cells, o, e),
    (v) => rev((v as mapdemo.Cells).rows),
  ),
  'mapdemo Chunks': Codec(
    (w) => mapdemo.chunksWireLoad(w),
    (v) => mchunks.ChunksCook.cookMeasure(v as mapdemo.Chunks),
    (v, o, e) => mchunks.ChunksCook.cook(v as mapdemo.Chunks, o, e),
    (v) => rev((v as mapdemo.Chunks).blobs),
  ),
  'mapdemo Crews': Codec(
    (w) => mapdemo.crewsWireLoad(w),
    (v) => mcrews.CrewsCook.cookMeasure(v as mapdemo.Crews),
    (v, o, e) => mcrews.CrewsCook.cook(v as mapdemo.Crews, o, e),
    (v) => rev((v as mapdemo.Crews).members),
  ),
  'mapdemo Depth': Codec(
    (w) => mapdemo.depthWireLoad(w),
    (v) => mdepth.DepthCook.cookMeasure(v as mapdemo.Depth),
    (v, o, e) => mdepth.DepthCook.cook(v as mapdemo.Depth, o, e),
    (v) {
      final d = v as mapdemo.Depth;
      revSquad(d.one);
      d.many.forEach(revSquad);
      d.keyed.forEach(revSquad);
      revSquad(d.arm.squad);
    },
  ),
  'mapdemo Docs': Codec(
    (w) => mapdemo.docsWireLoad(w),
    (v) => mdocs.DocsCook.cookMeasure(v as mapdemo.Docs),
    (v, o, e) => mdocs.DocsCook.cook(v as mapdemo.Docs, o, e),
    (v) => rev((v as mapdemo.Docs).pages),
  ),
  'mapdemo Fleet': Codec(
    (w) => mapdemo.fleetWireLoad(w),
    (v) => mfleet.FleetCook.cookMeasure(v as mapdemo.Fleet),
    (v, o, e) => mfleet.FleetCook.cook(v as mapdemo.Fleet, o, e),
    (v) {
      final f = v as mapdemo.Fleet;
      rev(f.ships);
      rev(f.byId);
      for (final l in f.loadouts) {
        rev(l.value);
      }
      rev(f.loadouts);
      rev(f.tiers);
    },
  ),
  'mapdemo Pairs': Codec(
    (w) => mapdemo.pairsWireLoad(w),
    (v) => mpairs.PairsCook.cookMeasure(v as mapdemo.Pairs),
    (v, o, e) => mpairs.PairsCook.cook(v as mapdemo.Pairs, o, e),
    (v) => rev((v as mapdemo.Pairs).slots),
  ),
  'mapdemo Runs': Codec(
    (w) => mapdemo.runsWireLoad(w),
    (v) => mruns.RunsCook.cookMeasure(v as mapdemo.Runs),
    (v, o, e) => mruns.RunsCook.cook(v as mapdemo.Runs, o, e),
    (v) => rev((v as mapdemo.Runs).spans),
  ),
  'mapdemo Slots': Codec(
    (w) => mapdemo.slotsWireLoad(w),
    (v) => mslots.SlotsCook.cookMeasure(v as mapdemo.Slots),
    (v, o, e) => mslots.SlotsCook.cook(v as mapdemo.Slots, o, e),
    (v) => rev((v as mapdemo.Slots).seats),
  ),
  'mapdemo Spans': Codec(
    (w) => mapdemo.spansWireLoad(w),
    (v) => mspans.SpansCook.cookMeasure(v as mapdemo.Spans),
    (v, o, e) => mspans.SpansCook.cook(v as mapdemo.Spans, o, e),
    (v) => rev((v as mapdemo.Spans).tracks),
  ),
  'mapdemo Text': Codec(
    (w) => mapdemo.textWireLoad(w),
    (v) => mtext.TextCook.cookMeasure(v as mapdemo.Text),
    (v, o, e) => mtext.TextCook.cook(v as mapdemo.Text, o, e),
    (v) {
      final t = v as mapdemo.Text;
      rev(t.names);
      rev(t.wide);
      rev(t.blobs);
    },
  ),
  'mapdemo Trails': Codec(
    (w) => mapdemo.trailsWireLoad(w),
    (v) => mtrails.TrailsCook.cookMeasure(v as mapdemo.Trails),
    (v, o, e) => mtrails.TrailsCook.cook(v as mapdemo.Trails, o, e),
    (v) => rev((v as mapdemo.Trails).steps),
  ),
  'widelist Wide': Codec(
    (w) => widelist.wideWireLoad(w),
    (v) => mwide.WideCook.cookMeasure(v as widelist.Wide),
    (v, o, e) => mwide.WideCook.cook(v as widelist.Wide, o, e),
  ),
};

bool same(Uint8List a, Uint8List b) {
  if (a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}

// cookBoth answers the value's cook in each byte order, or null when the
// writer refused it.
List<Uint8List>? cookBoth(Codec c, Object value) {
  final size = c.measure(value);
  if (size < 1) return null;
  final out = <Uint8List>[];
  for (final order in [Endian.little, Endian.big]) {
    final b = Uint8List(size);
    if (!c.cook(value, b, order)) return null;
    out.add(b);
  }
  return out;
}

void main(List<String> args) {
  final lines = <List<String>>[];
  for (final raw in File(args[0]).readAsLinesSync()) {
    final t = raw.trim();
    if (t.isEmpty || t.startsWith('#')) continue;
    lines.add(t.split(RegExp(r'[ \t]+')));
  }
  final instances = {
    for (final l in lines)
      if (l[0] == 'instance') l[1]: l,
  };
  var rows = 0;
  for (final l in lines.where((l) => l[0] == 'cook-write')) {
    final inst = instances[l[1]];
    if (inst == null) continue;
    final codec = codecs['${inst[2]} ${inst[3]}'];
    if (codec == null) continue;
    rows++;
    final name = l[1];
    final wire = File(inst[4]).readAsBytesSync();
    final want = [File(l[2]).readAsBytesSync(), File(l[3]).readAsBytesSync()];
    final got = cookBoth(codec, codec.load(wire));
    if (got == null) {
      fail('$name: the writer refused the loaded value');
      continue;
    }
    for (var i = 0; i < 2; i++) {
      checked++;
      if (!same(got[i], want[i])) {
        fail('$name: the cook (${i == 0 ? 'little' : 'big'}) is not schema cook\'s');
      }
    }
    final reverse = codec.reverse;
    if (reverse == null) continue;
    final reversed = codec.load(wire);
    reverse(reversed);
    final again = cookBoth(codec, reversed);
    if (again == null) {
      fail('$name: the writer refused the value with its maps reversed');
      continue;
    }
    for (var i = 0; i < 2; i++) {
      checked++;
      if (!same(again[i], want[i])) {
        fail('$name: every map reversed moved the cook (${i == 0 ? 'little' : 'big'}): the writer does not hold the sort');
      }
    }
    if (name == 'map_full') findFleet(mapdemo.fleetWireLoad(wire), got[0]);
    if (name == 'list_tables') spansSave(listdemo.saveWireLoad(wire), got[0]);
  }
  if (rows == 0) fail('the manifest names no map or list cook-write row');
  if (failures > 0) {
    stderr.writeln('dart cook map: $failures failure(s)');
    exit(1);
  }
  print('dart cook map: $rows rows, $checked cooks byte for byte, both orders, '
      'maps reversed at every depth; Find and the list spans read in place');
}

// findFleet holds `<field>Find` to every key of map_full's maps, at both
// depths, and to a key each map lacks (§2.8).
void findFleet(mapdemo.Fleet value, Uint8List image) {
  final cook = mfleet.FleetCook.open(image, 0, image.length);
  if (cook == null) {
    fail('map_full: the cook does not open');
    return;
  }
  final root = cook.root(cook.cursor());
  final ship = mfleet.FleetShipsEntryRow(cook.view, 0);
  final config = mfleet.ShipConfigRow(cook.view, 0);
  if (root.shipsCount != value.ships.length) fail('map_full: ships count');
  for (final e in value.ships) {
    final found = root.shipsFind(utf8.encode(e.key), ship);
    checked++;
    if (found == null || found.value(config).health != e.value.health) {
      fail('map_full: ships Find "${e.key}"');
    }
  }
  if (root.shipsFind(utf8.encode('~absent~'), ship) != null) {
    fail('map_full: ships Find answered a key the map lacks');
  }
  final byId = mfleet.FleetByIdEntryRow(cook.view, 0);
  for (final e in value.byId) {
    checked++;
    if (root.byIdFind(e.key, byId)?.key != e.key) fail('map_full: byId Find ${e.key}');
  }
  final tier = mfleet.FleetTiersEntryRow(cook.view, 0);
  final item = mfleet.ItemRow(cook.view, 0);
  for (final e in value.tiers) {
    checked++;
    final found = root.tiersFind(e.key, tier);
    if (found == null || found.value(item).count != e.value.count) {
      fail('map_full: tiers Find ${e.key}');
    }
  }
  final loadout = mfleet.FleetLoadoutsEntryRow(cook.view, 0);
  final inner = mfleet.FleetLoadoutsEntryValueEntryRow(cook.view, 0);
  for (final e in value.loadouts) {
    final found = root.loadoutsFind(utf8.encode(e.key), loadout);
    checked++;
    if (found == null || found.valueCount != e.value.length) {
      fail('map_full: loadouts Find "${e.key}"');
      continue;
    }
    for (final v in e.value) {
      checked++;
      final hit = found.valueFind(v.key, inner);
      if (hit == null || hit.value(item).count != v.value.count) {
        fail('map_full: loadouts["${e.key}"] Find ${v.key}');
      }
    }
    if (found.valueFind(255, inner) != null && !e.value.any((v) => v.key == 255)) {
      fail('map_full: an inner Find answered a key the map lacks');
    }
  }
}

// spansSave reads list_tables' lists in place: the counts, every scalar and
// every record element where it lies (§2.9).
void spansSave(listdemo.Save value, Uint8List image) {
  final cook = lsave.SaveCook.open(image, 0, image.length);
  if (cook == null) {
    fail('list_tables: the cook does not open');
    return;
  }
  final root = cook.root(cook.cursor());
  if (root.scoresCount != value.scores.length) fail('list_tables: scores count');
  for (var i = 0; i < value.scores.length; i++) {
    checked++;
    if (root.scoresAt(i) != value.scores[i]) fail('list_tables: scores[$i]');
  }
  if (root.placementsCount != value.placements.length) {
    fail('list_tables: placements count');
  }
  final p = lsave.PlacementRow(cook.view, 0);
  for (var i = 0; i < value.placements.length; i++) {
    checked++;
    if (root.placementsAt(i, p).model != value.placements[i].model) {
      fail('list_tables: placements[$i]');
    }
  }
  if (root.logCount != value.log.length) fail('list_tables: log count');
}
