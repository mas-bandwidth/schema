// THE FORGERY FUZZER over the Dart accelerators (docs/SPEC-TABLES.md §19.2,
// §19.5, §7.4).
//
// The conformance harness's forgery batteries are fixed lists — eleven rows for
// the block, a hundred and eleven for the cook, one per fact an Open checks.
// This is the standing gate beside them: valid images from the corpus, mutated
// by the mutators below, and ONE ORACLE over every mutant —
//
//   REFUSE, or OPEN and be WHOLE. A mutant either makes open answer null, or it
//   opens, and then every row of every array is addressable inside the extent
//   the caller passed, every pitch is this build's own, every count is inside
//   its declared maximum, and a full walk that READS every byte of every row
//   stays inside that extent.
//
// AND NOTHING THROWS. That clause is Dart's own and it is the point of the
// gate here: an out-of-bounds index in Dart is a RangeError, and a reader that
// RAISES on hostile bytes is not a reader that REFUSES them. Every call below
// is wrapped, and an escaping exception is a finding — from open, from a row
// read, or from a deref.
//
// THE ORACLE RE-DERIVES ITS BOUNDS from the descriptors and from the triples in
// the instance, never from open's own arithmetic — that independence is the
// only reason it can disagree with open at all, and a gate that shared the code
// under test's arithmetic would stay green through any change to it.
//
// Every mutant is a pure function of (seed, image, pass, index), so a failure
// names the one case that reproduces it.
//
//   usage: fuzz [mutants-per-fixture]
//   env:   SEED=<n>
import 'dart:io';
import 'dart:typed_data';

import '../../build/tables-generated-dart/block/BlockdemoBlock.dart';
import '../../build/tables-generated-dart/block/PaddedBlock.dart' as blk;
import '../../build/tables-generated-dart/block/RenderBlock.dart' as blk;
import '../../build/tables-generated-dart/pointers/GraphdemoCook.dart';
import '../../build/tables-generated-dart/pointers/GraphCook.dart' as ck;

// a reproducible stream: the same seed gives the same mutants, so a failure
// names the one case that reproduces it
class Rng {
  int state;

  Rng(this.state);

  int next() {
    // xorshift64*, entirely inside the 64 bits Dart's int holds
    state ^= state >>> 12;
    state ^= (state << 25) & 0xffffffffffffffff;
    state ^= state >>> 27;
    return (state * 0x2545f4914f6cdd1d) & 0x7fffffffffffffff;
  }

  int below(int n) => n <= 0 ? 0 : next() % n;
}

// ---- the mutators ----
//
// Each returns a fresh image; the original is never touched, so a pass cannot
// carry a defect into the next.

Uint8List mutate(Uint8List source, Rng rng, int pass) {
  final out = Uint8List.fromList(source);
  final view = ByteData.view(out.buffer);
  switch (pass % 6) {
    case 0: // one byte flipped
      final at = rng.below(out.length);
      out[at] ^= 1 << rng.below(8);
    case 1: // one byte splatted
      out[rng.below(out.length)] = rng.below(256);
    case 2: // one 64-bit word, anywhere it fits
      if (out.length >= 8) {
        final at = rng.below(out.length - 7);
        view.setUint64(at, rng.next(), Endian.little);
      }
    case 3: // one 32-bit word — the counts and the strides live at this width
      if (out.length >= 4) {
        final at = rng.below(out.length - 3);
        view.setUint32(at, rng.next() & 0xffffffff, Endian.little);
      }
    case 4: // a run zeroed
      final at = rng.below(out.length);
      final run = rng.below(64) + 1;
      final end = at + run > out.length ? out.length : at + run;
      out.fillRange(at, end, 0);
    case 5: // the PROLOGUE, where every open looks first
      final at = rng.below(64 < out.length ? 64 : out.length);
      out[at] = rng.below(256);
  }
  return out;
}

// ---- the BLOCK oracle ----

class Finding {
  final String what;

  Finding(this.what);

  @override
  String toString() => what;
}

// blockWhole re-derives every bound from the descriptors and from the triples
// the INSTANCE carries, then reads every byte of every row. It throws a Finding
// when open accepted an image that is not whole, and lets a RangeError escape
// as itself — which the caller reports as the harder failure it is.
void blockWhole(
  Uint8List bytes,
  int base,
  int extent,
  TableBlockInfo info,
  int projectionSize,
) {
  final view = ByteData.view(
    bytes.buffer,
    bytes.offsetInBytes,
    bytes.lengthInBytes,
  );
  for (final f in info.fields) {
    if (!f.outOfLine) {
      continue;
    }
    final offsetOf = view.getUint64(base + f.offsetOfOffset, Endian.little);
    final count = view.getUint32(base + f.countOffset, Endian.little);
    final stride = view.getUint32(base + f.strideOffset, Endian.little);
    if (stride != f.stride) {
      throw Finding(
        '${f.name}: opened with a pitch of $stride, and this build\'s is ${f.stride}',
      );
    }
    if (count > f.arrayBound) {
      throw Finding(
        '${f.name}: opened with a count of $count, past the declared ${f.arrayBound}',
      );
    }
    if (offsetOf < projectionSize || offsetOf < 0) {
      throw Finding(
        '${f.name}: opened with an offset_of of $offsetOf, inside the prologue',
      );
    }
    if (offsetOf % 64 != 0) {
      throw Finding(
        '${f.name}: opened with an offset_of of $offsetOf, unaligned',
      );
    }
    final span = count * stride;
    if (offsetOf > extent || span > extent - offsetOf) {
      throw Finding(
        '${f.name}: opened with $count rows of $stride at $offsetOf, past the claimed $extent',
      );
    }
    final row = f.element;
    if (row == null) {
      throw Finding(
        '${f.name}: an out-of-line array whose descriptor names no row',
      );
    }
    for (var r = 0; r < count; r++) {
      readRecord(view, base + offsetOf + r * stride, row);
    }
  }
  readRecord(view, base, info);
}

// readRecord reads EVERY byte a record's descriptors name, so a row that does
// not fit is a read that leaves the buffer rather than a number nobody looked
// at.
void readRecord(ByteData view, int at, TableBlockInfo info) {
  for (final f in info.fields) {
    if (f.outOfLine) {
      continue;
    }
    final element = f.element;
    if (element != null) {
      final slots = f.isArray ? f.arrayBound : 1;
      for (var s = 0; s < slots; s++) {
        readRecord(view, at + f.offset + s * f.elemSize, element);
      }
      continue;
    }
    for (var i = 0; i < f.size; i++) {
      view.getUint8(at + f.offset + i);
    }
    if (f.countOffset >= 0) {
      view.getInt32(at + f.countOffset, Endian.little);
    }
    if (f.presentOffset >= 0) {
      view.getUint8(at + f.presentOffset);
    }
  }
}

// ---- the COOK oracle ----

// THE COOK'S ORACLE, and it is narrower than the block's on purpose. §7's rule
// is MATCH AND POINT: Open checks the HEADER and nothing per node, so a byte
// forged INSIDE the region is data the reader never promised to validate —
// `schema cook-check` is the pass that does. What this tier does promise, and
// what the oracle holds it to, is:
//
//   - the DEREF answers an offset INSIDE the region, a null, or a refusal, and
//     never something else. That is the one arithmetic Open does not do and the
//     reader does, so it is the one place a forged delta can escape.
//   - and NOTHING THROWS. A walk over a forged region meets counts and offsets
//     no build wrote; it clamps them and stops rather than raising, because a
//     reader that raises on hostile bytes is not a reader that refuses them.
void cookWhole(
  Uint8List bytes,
  int region,
  int length,
  TableCookInfo root,
  int Function(int slot) at,
) {
  final view = ByteData.view(
    bytes.buffer,
    bytes.offsetInBytes,
    bytes.lengthInBytes,
  );
  final seen = <int>{};

  late void Function(int, TableCookInfo, int) storageOf;

  void node(int offset, TableCookInfo type, int depth) {
    if (depth > 4096) {
      throw Finding('the walk nested past any depth a region can hold');
    }
    if (!seen.add(offset)) {
      return;
    }
    if (offset < 0 || offset > length || type.size > length - offset) {
      // a node a forged delta pointed at that does not FIT is a region
      // cook-check refuses; the walk stops rather than reading it
      return;
    }
    storageOf(region + offset, type, depth);
  }

  void storageBody(int start, TableCookInfo type, int depth) {
    for (final f in type.fields) {
      if (f.isPointer) {
        final target = at(start + f.offset);
        if (target == TableCookRef.none || target == TableCookRef.outside) {
          continue;
        }
        if (target < region || target >= region + length) {
          throw Finding(
            '${type.name}.${f.name} resolved to $target, outside the region '
            '[$region, ${region + length})',
          );
        }
        final record = f.info;
        if (record == null) {
          throw Finding('${type.name}.${f.name} is a pointer naming no record');
        }
        node(target - region, record, depth + 1);
        continue;
      }
      if (f.storage == TableCookStorage.record) {
        final slots = f.isArray ? f.arrayBound : 1;
        for (var s = 0; s < slots; s++) {
          final child = start + f.offset + s * f.elemSize;
          final info = f.info;
          if (info == null || child + info.size > region + length) {
            continue;
          }
          storageOf(child, info, depth);
        }
        continue;
      }
      // every byte the descriptor names, bounded by the region: a forged count
      // cannot make this walk index past what it was given
      final end = start + f.offset + f.size;
      if (end > region + length) {
        continue;
      }
      for (var i = start + f.offset; i < end; i++) {
        view.getUint8(i);
      }
    }
  }

  storageOf = storageBody;
  node(0, root, 0);
}

// ---- the fixtures ----

class BlockCase {
  final String name;
  final Uint8List image;
  final Object? Function(Uint8List, int, int) open;
  final TableBlockInfo info;
  final int projectionSize;

  BlockCase(this.name, this.image, this.open, this.info, this.projectionSize);
}

class CookCase {
  final String name;
  final Uint8List file;
  final Object? Function(Uint8List, int, int) open;
  final TableCookInfo info;

  CookCase(this.name, this.file, this.open, this.info);
}

Uint8List load(String path) => File(path).readAsBytesSync();

int failures = 0;

void report(String fixture, int pass, int index, int seed, Object what) {
  failures++;
  stdout.write(
    'FUZZ FINDING  $fixture pass=$pass index=$index seed=$seed\n  $what\n',
  );
}

void main(List<String> args) {
  final seed = int.parse(Platform.environment['SEED'] ?? '20260903');
  final mutants = args.isEmpty ? 4000 : int.parse(args[0]);

  final blocks = <BlockCase>[
    BlockCase(
      'block_render',
      load('testdata/wire/tables/block_render.bin'),
      (b, base, n) => blk.RenderFrameBlock.open(b, base, n),
      blk.RenderFrameBlock.type,
      blk.RenderFrameBlock.projectionSize,
    ),
    BlockCase(
      'block_padded',
      load('testdata/wire/tables/block_padded.bin'),
      (b, base, n) => blk.PaddedFrameBlock.open(b, base, n),
      blk.PaddedFrameBlock.type,
      blk.PaddedFrameBlock.projectionSize,
    ),
  ];

  final cooks = <CookCase>[];
  for (final root in ['Scene', 'Depot', 'Album', 'TreeNode', 'ListNode']) {
    final path = 'build/cook-fuzz/$root.cook';
    if (!File(path).existsSync()) {
      continue;
    }
    cooks.add(
      CookCase(root, load(path), (b, base, n) {
        switch (root) {
          case 'Scene':
            return ck.SceneCook.open(b, base, n);
          case 'Depot':
            return ck.DepotCook.open(b, base, n);
          case 'Album':
            return ck.AlbumCook.open(b, base, n);
          case 'TreeNode':
            return ck.TreeNodeCook.open(b, base, n);
        }
        return ck.ListNodeCook.open(b, base, n);
      }, cookType(root)),
    );
  }

  var checked = 0;
  for (final c in blocks) {
    for (var i = 0; i < mutants; i++) {
      final rng = Rng(seed ^ (i * 0x9e3779b97f4a7c15) ^ c.name.hashCode);
      final image = mutate(c.image, rng, i);
      checked++;
      try {
        final opened = c.open(image, 0, image.length);
        if (opened == null) {
          continue; // REFUSED, which is always a legal answer
        }
        blockWhole(image, 0, image.length, c.info, c.projectionSize);
      } on Finding catch (e) {
        report(c.name, i % 6, i, seed, e);
      } catch (e) {
        report(c.name, i % 6, i, seed, 'an exception ESCAPED the reader: $e');
      }
    }
  }

  for (final c in cooks) {
    for (var i = 0; i < mutants; i++) {
      final rng = Rng(seed ^ (i * 0x9e3779b97f4a7c15) ^ c.name.hashCode);
      final file = mutate(c.file, rng, i);
      checked++;
      try {
        final opened = c.open(file, 0, file.length);
        if (opened == null) {
          continue;
        }
        final (region, length) = cookRegionOf(opened);
        cookWhole(file, region, length, c.info, derefOf(opened));
      } on Finding catch (e) {
        report(c.name, i % 6, i, seed, e);
      } catch (e) {
        report(c.name, i % 6, i, seed, 'an exception ESCAPED the reader: $e');
      }
    }
  }

  stdout.write(
    'dart tables fuzz: $checked mutants over ${blocks.length} blocks and '
    '${cooks.length} cooks, seed $seed — $failures finding(s)\n',
  );
  exit(failures == 0 ? 0 : 1);
}

TableCookInfo cookType(String root) {
  switch (root) {
    case 'Scene':
      return ck.SceneCook.type;
    case 'Depot':
      return ck.DepotCook.type;
    case 'Album':
      return ck.AlbumCook.type;
    case 'TreeNode':
      return ck.TreeNodeCook.type;
  }
  return ck.ListNodeCook.type;
}

(int, int) cookRegionOf(Object cook) {
  if (cook is ck.SceneCook) return (cook.region, cook.length);
  if (cook is ck.DepotCook) return (cook.region, cook.length);
  if (cook is ck.AlbumCook) return (cook.region, cook.length);
  if (cook is ck.TreeNodeCook) return (cook.region, cook.length);
  if (cook is ck.ListNodeCook) return (cook.region, cook.length);
  throw StateError('not a cook');
}

int Function(int) derefOf(Object cook) {
  if (cook is ck.SceneCook) return cook.at;
  if (cook is ck.DepotCook) return cook.at;
  if (cook is ck.AlbumCook) return cook.at;
  if (cook is ck.TreeNodeCook) return cook.at;
  if (cook is ck.ListNodeCook) return cook.at;
  throw StateError('not a cook');
}
