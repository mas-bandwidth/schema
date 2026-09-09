package darttable

// fixedRuntime is THE FIXED FORM's shared Dart runtime (docs/SPEC-TABLES.md
// §3.4): the plan, the ONE read loop, the LAYOUT reader with its seven named
// rules, and the plan compiler. It is emitted ONCE PER UNIT into the unit's
// <Home>Fixed.dart — the same runtime-home rule the block and cook halves
// already follow — and every other <Base>Fixed.dart of the unit imports it.
//
// THE SHAPE IS THE PACKET CODEC'S, not the id-table wire's. The owner's rule
// for every leg of this form is that the port looks at the equivalent PACKET
// codec and takes its shape, and in Dart that is internal/codegen/dart: a
// caller-owned buffer with a `ByteData` over it, `Endian.little` passed
// EXPLICITLY at every single call, constant offsets and constant widths
// inlined at each field, a destination the reader FILLS rather than returns,
// storage allocated at construction and never replaced, and a VERDICT — a byte
// count or -1, a record count or -1 — instead of an exception. Nothing here
// throws on hostile bytes and nothing here allocates per record.
//
// WHAT IT DOES NOT TAKE FROM THE PACKET CODEC is the bit packer. That codec
// stages a 64-bit word and a bit cursor because the packet wire is bitpacked;
// the fixed form has no bit cursor at all — every field rides at its DECLARED
// STORAGE WIDTH at a constant BYTE offset — so the staging word, the window
// loads and the whole tail-word discipline are gone, and what is left is the
// store itself.
//
// THE READER'S OWN STORAGE IN THIS LANGUAGE IS THE CANONICAL BODY IMAGE. Dart
// has no struct layout — no offsetof, no sizeof, no way to place a field — so
// where the C++ reference's plan lands bytes at `offsetof( T, member )` this
// one lands them in a Uint8List holding THIS BUILD's declared order at THIS
// BUILD's own widths. That image IS the wire's layout, so the identity plan's
// source and destination advance together; a plan compiled from another
// writer's layout moves, widens, clamps and remaps into the same image through
// the same loop. A generated straight-line decode then projects the image into
// the language's own objects, which is the step C++ gets for free because
// there a struct IS its bytes.
//
// NOTHING AT LIBRARY SCOPE IS PRIVATE. Dart's privacy is per library and a
// schema identifier may begin with an underscore, so a generated `_foo` at
// library scope is a collision no registry covers (compiler's
// TestDartTableRuntimeNamesAreClaimed). Every helper below is therefore a
// static MEMBER of a claimed class, which claims nothing of its own.
const fixedRuntime = `
// ---------------------------------------------------------------------------
// THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4)
// ---------------------------------------------------------------------------

/// The form byte. Forms 1 and 2 do not move and nothing here touches one.
const int tableFixedForm = 3;

/// The guard offset an entry that belongs to no union arm carries.
const int tableFixedNoGuard = -1;

/// THE OPS ARE THE WHOLE SET. The identity plan carries the first three; the
/// other four are what a plan compiled from another writer's layout adds.
abstract final class TableFixedOp {
  static const int copy = 0; // move size bytes
  static const int count = 1; // a count: clamp it to the reader's own bound
  static const int text = 2; // a length, then the units
  static const int ordinal = 3; // an ordinal, through the plan's own remap
  static const int widen = 4; // a narrower source into a wider destination
  static const int constant = 5; // a constant this reader's storage takes
  static const int widenFloat = 6; // f32 into f64, §4's float rung

  // the flavours a text entry lands its units under
  static const int textUtf8 = 1;
  static const int textWide = 2;
  static const int textBytes = 3;
}

/// A PLAN ENTRY is eight int32 lanes of one flat Int32List. A flat list of a
/// fixed stride is what keeps the read loop monomorphic: the loop never
/// touches a field name and nothing is allocated.
abstract final class TableFixedLane {
  static const int lanes = 8;
  static const int op = 0;
  static const int src = 1;
  static const int dst = 2;
  static const int size = 3;
  static const int aux = 4;
  static const int guard = 5;
  static const int arg = 6;
  static const int meta = 7;

  // MY SIDE of the layout, five lanes per entry of my own: the storage facts a
  // layout entry cannot carry. In C++ these are offsetof rows; the reader's own
  // storage in Dart is the canonical image, so they are its offsets.
  static const int dstLanes = 5;
  static const int dstOffset = 0;
  static const int dstStride = 1;
  static const int dstAux = 2;
  static const int dstCounted = 3;
  static const int dstArg = 4;
}

/// The bounds this reader holds a layout to. THE LAYOUT'S OWN FORMAT NEVER
/// MOVES INSIDE A FORM: an entry is seventeen bytes and the layout is a count
/// and a run of them, and THERE IS NO VERSION BYTE INSIDE IT — the form byte
/// versions everything behind it.
abstract final class TableFixedLimits {
  /// THE HEADER, ONE RULE FOR ALL FIVE FORMS (docs/SPEC-TABLES.md §3, "THE
  /// FIRST BYTE"): the FORM BYTE at offset 0, seven RESERVED ZERO bytes, the
  /// form's own EIGHT-BYTE HASH at offset 8, and the body at 16 — the alignment
  /// a memory-mapped body needs. The fixed form does not need that alignment
  /// today; it pads anyway, so the bytes do not move again the day the cook and
  /// the block form join the registry under the same header.
  ///
  /// The hash here is the LAYOUT's. Each record still carries its own
  /// eight-byte hash, which the header does not replace: the header names the
  /// layout ONCE for the file, and a record names the layout it was stamped by.
  static const int headerBytes = 16;
  static const int hashAt = 8;

  /// where the layout's own bytes begin: past the header and past the u32
  /// length that stands in front of them
  static const int layoutAt = headerBytes + 4;

  /// the two form bytes a fixed reader NAMES rather than lumps together. The
  /// registry is ORDERED, so a byte this reader does not carry is named by
  /// where it sits relative to this form (docs/SPEC-TABLES.md §3).
  static const int variableForm = 1;
  static const int messageForm = 2;

  static const int entryBytes = 17;
  // the u32 entry count, and nothing else
  static const int layoutHeaderBytes = 4;

  /// §3.4's RECORD CEILING, and a reader holds an untrusted peer's layout to
  /// it for the reason the compiler holds a declaration to it: a record past
  /// it is one this build will not decode.
  static const int recordMaxBytes = 65536;

  /// A BOUND ON THE WALK, not on the wire. The validation is recursive, so a
  /// hostile layout of three thousand entries each claiming one child would
  /// otherwise spend a reader's stack before any rule fired.
  static const int maxDepth = 64;
}

/// WHY A READ WAS REFUSED, BY NAME. None of these is one of §4's six events:
/// in each of them nothing was decoded and there is nothing to count.
abstract final class TableFixedRefusal {
  static const int none = 0;

  /// A FORM BYTE'S REFUSAL SAYS WHICH DIRECTION. The registry is ORDERED, so
  /// one word for both directions is one word too few: form 1 is OLDER than
  /// this one and 'newer_form' there would send a caller looking for a build
  /// that does not exist (docs/SPEC-TABLES.md §3).
  static const int previousForm = 1; // form 1, the variable form
  static const int messageFormAsFile = 2; // form 2 where a FILE was expected
  static const int newerForm = 3; // a byte no form defines

  static const int noLayout = 4; // a hash naming no layout this reader holds
  static const int layoutMalformed = 5; // bytes that are not a layout at all
  static const int layoutCountMismatch = 6;
  static const int layoutKindUnknown = 7;
  static const int layoutSizeMismatch = 8;
  static const int layoutKindInvalid = 9;
  static const int layoutTreeUnclosed = 10;
  static const int layoutRecordTooLarge = 11;
  static const int layoutTooDeep = 12;
  static const int planTooLarge = 13; // a plan that does not fit the caller's
  // more records than the caller's capacity
  static const int batchTooLarge = 14;

  /// The refusal's own name, for a report a person reads.
  static String name(int refusal) {
    switch (refusal) {
      case none:
        return 'none';
      case previousForm:
        return 'previous_form';
      case messageFormAsFile:
        return 'message_form_as_file';
      case newerForm:
        return 'newer_form';
      case noLayout:
        return 'no_layout';
      case layoutMalformed:
        return 'layout_malformed';
      case layoutCountMismatch:
        return 'layout_count_mismatch';
      case layoutKindUnknown:
        return 'layout_kind_unknown';
      case layoutSizeMismatch:
        return 'layout_size_mismatch';
      case layoutKindInvalid:
        return 'layout_kind_invalid';
      case layoutTreeUnclosed:
        return 'layout_tree_unclosed';
      case layoutRecordTooLarge:
        return 'layout_record_too_large';
      case layoutTooDeep:
        return 'layout_too_deep';
      case planTooLarge:
        return 'plan_too_large';
      case batchTooLarge:
        return 'batch_too_large';
      default:
        return '???';
    }
  }
}

/// A REPORT is §4's six counters and the refusal beside them. The caller owns
/// it and the codec never makes one.
final class TableFixedReport {
  bool malformed = false;
  int refused = TableFixedRefusal.none;
  int unknown = 0;
  int kindMismatch = 0;
  int clamped = 0;
  int widened = 0;
  int duplicate = 0;

  /// the layout hash the record carried, so an application can ask a peer for
  /// the layout a 'no_layout' refusal names
  int hash = 0;

  void reset() {
    malformed = false;
    refused = TableFixedRefusal.none;
    unknown = 0;
    kindMismatch = 0;
    clamped = 0;
    widened = 0;
    duplicate = 0;
    hash = 0;
  }
}

/// A LAYOUT VIEW is the bytes and the entry count. Every read of an entry is
/// arithmetic on the seventeen-byte stride, so nothing is parsed twice and
/// nothing is materialised.
final class TableFixedLayout {
  ByteData? view;
  int at = 0;
  int count = 0;

  /// the rule the last parse failed, by name; TableFixedRefusal.none when it
  /// passed. It is a field of the view rather than an out parameter so a parse
  /// allocates nothing at all.
  int refusal = TableFixedRefusal.none;

  int entryAt(int i) =>
      at + TableFixedLimits.layoutHeaderBytes + i * TableFixedLimits.entryBytes;

  int id(int i) => view!.getUint64(entryAt(i), Endian.little);

  int kind(int i) => view!.getUint8(entryAt(i) + 8);

  int size(int i) => view!.getUint32(entryAt(i) + 9, Endian.little);

  int children(int i) => view!.getUint32(entryAt(i) + 13, Endian.little);

  /// How many entries the subtree rooted at i occupies, so a walk steps over a
  /// child it does not want without knowing what is in it. THIS IS WHAT MAKES
  /// SKIPPING FREE: an unknown field, and an unknown NESTED TYPE, are stepped
  /// over by the size their entry states. It is only ever called on a layout
  /// that has already passed every rule below, so the tree closes.
  int subtree(int i) {
    if (i < 0 || i >= count) {
      return 1;
    }
    final kids = children(i);
    var n = 1;
    var next = i + 1;
    for (var c = 0; c < kids; c++) {
      if (next >= count) {
        break;
      }
      final sub = subtree(next);
      next += sub;
      n += sub;
    }
    return n;
  }

  int unionArmBytes(int i) {
    final kids = children(i);
    var widest = 0;
    var next = i + 1;
    for (var k = 0; k < kids; k++) {
      final s = size(next);
      if (s > widest) {
        widest = s;
      }
      next += subtree(next);
    }
    return widest;
  }

  int tagBytes(int i) => size(i) - unionArmBytes(i);

  /// THE HASH is fnv1a64 over the layout's bytes exactly as written, and it is
  /// the eight bytes every record carries. A Dart int is a full 64-bit two's
  /// complement value and its multiply wraps, so the hash is the arithmetic
  /// itself with no limb splitting — the one place this port is simpler than
  /// the JavaScript one, whose Number cannot hold it.
  static int hashOf(Uint8List bytes, int at, int length) {
    var h = 0xcbf29ce484222325;
    for (var i = 0; i < length; i++) {
      h = (h ^ bytes[at + i]) * 0x100000001b3;
    }
    return h;
  }

  /// THE CLOSED KIND SET (docs/SPEC-TABLES.md §3, §3.4): §3's own kinds 1..30,
  /// the no-payload variant 32, wstring 33, and the ONE kind this form's
  /// layout adds, the optional wrapper 35. 31 is §3's body framing escape and
  /// 34 is reserved: neither is a kind a declaration spells. A KIND OUTSIDE
  /// THIS SET IS A REFUSAL, not a leaf to step over — the form byte versions
  /// the kinds too, so a kind this build does not know means a form byte this
  /// build never saw, and stepping over it would be trusting a size whose
  /// meaning this reader cannot check.
  static bool knownKind(int kind) {
    if (kind >= 1 && kind <= 30) {
      return true;
    }
    return kind == 32 || kind == 33 || kind == 35;
  }

  /// A LEAF KIND'S ADMITTED SIZES. Kind and size fix each other on this wire
  /// with ONE exception, stated here rather than left to be found: a bits(N)
  /// field rides at its DECLARED STORAGE WIDTH — four bytes for N <= 32 and
  /// eight above — under the unsigned integer kind its BIT COUNT picks, so
  /// kinds 6 and 7 admit four as well as their own width. Answers -1 for a
  /// kind that is not a leaf.
  static int leafSize(int kind, int size) {
    switch (kind) {
      case 1: // bool
      case 2: // i8
        return size == 1 ? 1 : 0;
      case 3: // i16
        return size == 2 ? 1 : 0;
      case 4: // i32
        return size == 4 ? 1 : 0;
      case 5: // i64
        return size == 8 ? 1 : 0;
      case 6: // u8, and bits( 1 .. 8 )
        return size == 1 || size == 4 ? 1 : 0;
      case 7: // u16, and bits( 9 .. 16 )
        return size == 2 || size == 4 ? 1 : 0;
      case 8: // u32, and bits( 17 .. 32 )
        return size == 4 ? 1 : 0;
      case 9: // u64, flags, and bits( 33 .. 64 )
        return size == 8 ? 1 : 0;
      case 10: // f32
        return size == 4 ? 1 : 0;
      case 11: // f64
        return size == 8 ? 1 : 0;
      case 17: // a pointer index, which no fixed table carries
        return size == 4 ? 1 : 0;
      case 18: // i128
      case 19: // u128
        return size == 16 ? 1 : 0;
      case 20: // fixed8
      case 25: // ufixed8
        return size == 1 ? 1 : 0;
      case 21:
      case 26:
        return size == 2 ? 1 : 0;
      case 22:
      case 27:
        return size == 4 ? 1 : 0;
      case 23:
      case 28:
        return size == 8 ? 1 : 0;
      case 24:
      case 29:
        return size == 16 ? 1 : 0;
      case 32: // a variant, and an arm that holds nothing
        return size == 0 ? 1 : 0;
    }
    return -1;
  }

  static bool ordinalWidth(int n) => n == 1 || n == 2 || n == 4 || n == 8;

  void fail(int why) {
    if (refusal == TableFixedRefusal.none) {
      refusal = why;
    }
  }

  /// checkEntry validates the subtree rooted at i and answers how many entries
  /// it occupies, which is the same arithmetic subtree does and is why that
  /// walk is safe to run afterwards and only afterwards.
  int checkEntry(int i, int depth) {
    if (refusal != TableFixedRefusal.none) {
      return 0;
    }
    if (i < 0 || i >= count) {
      fail(TableFixedRefusal.layoutTreeUnclosed);
      return 0;
    }
    if (depth > TableFixedLimits.maxDepth) {
      fail(TableFixedRefusal.layoutTooDeep);
      return 0;
    }
    final myKind = kind(i);
    final mySize = size(i);
    // THE CLOSED KIND SET, FIRST: a kind outside it is a layout of another
    // form and is refused whole, never walked and never stepped over.
    if (!knownKind(myKind)) {
      fail(TableFixedRefusal.layoutKindUnknown);
      return 0;
    }
    if (mySize > TableFixedLimits.recordMaxBytes) {
      fail(TableFixedRefusal.layoutRecordTooLarge);
      return 0;
    }
    // THE CHILDREN FIRST, in the pre-order the layout is written in.
    final kids = children(i);
    var next = i + 1;
    var sum = 0;
    var widest = 0;
    var firstSize = 0;
    var secondSize = 0;
    var firstKind = 0;
    var firstChildren = 0;
    var kidsAreVariants = true;
    for (var k = 0; k < kids; k++) {
      final child = next;
      final used = checkEntry(child, depth + 1);
      if (refusal != TableFixedRefusal.none) {
        return 0;
      }
      final childSize = size(child);
      if (k == 0) {
        firstSize = childSize;
        firstKind = kind(child);
        firstChildren = children(child);
      }
      if (k == 1) {
        secondSize = childSize;
      }
      if (kind(child) != 32) {
        kidsAreVariants = false;
      }
      sum += childSize;
      if (childSize > widest) {
        widest = childSize;
      }
      if (sum > TableFixedLimits.recordMaxBytes) {
        fail(TableFixedRefusal.layoutRecordTooLarge);
        return 0;
      }
      next += used;
    }
    final leaf = leafSize(myKind, mySize);
    if (leaf == 0) {
      fail(TableFixedRefusal.layoutSizeMismatch);
      return 0;
    }
    if (leaf == 1) {
      if (kids != 0) {
        fail(TableFixedRefusal.layoutKindInvalid);
        return 0;
      }
      return next - i;
    }
    switch (myKind) {
      case 13: // a TABLE: its size is the SUM of its fields'
        if (mySize != sum) {
          fail(TableFixedRefusal.layoutSizeMismatch);
          return 0;
        }
        break;
      case 35: // the OPTIONAL WRAPPER: one child, one present byte in front
        if (kids != 1) {
          fail(TableFixedRefusal.layoutKindInvalid);
          return 0;
        }
        if (mySize != sum + 1) {
          fail(TableFixedRefusal.layoutSizeMismatch);
          return 0;
        }
        break;
      case 14: // an ARRAY: a whole number of elements, behind a count or not
        if (kids != 1) {
          fail(TableFixedRefusal.layoutKindInvalid);
          return 0;
        }
        if (firstSize == 0) {
          fail(TableFixedRefusal.layoutSizeMismatch);
          return 0;
        }
        final bare = mySize % firstSize == 0;
        final counted = mySize >= 4 && (mySize - 4) % firstSize == 0;
        if (!bare && !counted) {
          fail(TableFixedRefusal.layoutSizeMismatch);
          return 0;
        }
        break;
      case 16: // an ENUM-KEYED array: the KEY ENUM then the ELEMENT
        if (kids != 2) {
          fail(TableFixedRefusal.layoutKindInvalid);
          return 0;
        }
        if (firstKind != 30) {
          fail(TableFixedRefusal.layoutKindInvalid);
          return 0;
        }
        if (secondSize == 0 || mySize % secondSize != 0) {
          fail(TableFixedRefusal.layoutSizeMismatch);
          return 0;
        }
        // AT LEAST one slot per variant. Not exactly one: an enum widened by
        // an explicit max has more slots than it has names, and the layout
        // carries only the names.
        if (mySize ~/ secondSize < firstChildren) {
          fail(TableFixedRefusal.layoutSizeMismatch);
          return 0;
        }
        break;
      case 15: // a UNION: the TAG at its own width, then the WIDEST ARM
        if (kids == 0) {
          fail(TableFixedRefusal.layoutKindInvalid);
          return 0;
        }
        if (mySize <= widest) {
          fail(TableFixedRefusal.layoutSizeMismatch);
          return 0;
        }
        if (!ordinalWidth(mySize - widest)) {
          fail(TableFixedRefusal.layoutSizeMismatch);
          return 0;
        }
        break;
      case 30: // an ENUM: the ordinal's width, and its children are VARIANTS
        if (!ordinalWidth(mySize)) {
          fail(TableFixedRefusal.layoutSizeMismatch);
          return 0;
        }
        if (kids != 0 && !kidsAreVariants) {
          fail(TableFixedRefusal.layoutKindInvalid);
          return 0;
        }
        break;
      case 12: // string(N): the length, then N bytes
        if (kids != 0) {
          fail(TableFixedRefusal.layoutKindInvalid);
          return 0;
        }
        if (mySize < 4) {
          fail(TableFixedRefusal.layoutSizeMismatch);
          return 0;
        }
        break;
      case 33: // wstring(N): the length in CODE UNITS, then 2N bytes
        if (kids != 0) {
          fail(TableFixedRefusal.layoutKindInvalid);
          return 0;
        }
        if (mySize < 4 || (mySize - 4) % 2 != 0) {
          fail(TableFixedRefusal.layoutSizeMismatch);
          return 0;
        }
        break;
      // Every kind of the closed set is either a leaf above or a case here, so
      // this is unreachable — and it refuses rather than admits, because a
      // kind that reached it is one the two lists disagree about.
      default:
        fail(TableFixedRefusal.layoutKindUnknown);
        return 0;
    }
    return next - i;
  }

  /// THE WHOLE OF WHAT A READER TRUSTS A LAYOUT ON. A LAYOUT ARRIVES FROM AN
  /// UNTRUSTED PEER and it is the one structure a reader must parse before it
  /// knows anything at all, so every rule runs BEFORE a single record byte is
  /// touched and each refuses under its OWN NAME. A layout that fails any of
  /// them SETS NOTHING: no plan is compiled, no counter moves, 'malformed'
  /// does not fire, and the reader is in the state it was in before the bytes
  /// arrived.
  ///
  /// THERE IS NO CYCLE RULE, AND THERE CANNOT BE A CYCLE. A pre-order walk
  /// cannot express one: an entry's children ARE THE ENTRIES THAT FOLLOW IT,
  /// so a child's index is always strictly greater than its parent's and there
  /// is no back reference for a cycle to be made of.
  bool parse(ByteData sourceView, int from, int length) {
    view = null;
    at = 0;
    count = 0;
    refusal = TableFixedRefusal.none;
    if (length < TableFixedLimits.layoutHeaderBytes) {
      refusal = TableFixedRefusal.layoutMalformed;
      return false;
    }
    // 1. THE ENTRY COUNT FITS THE LAYOUT'S LENGTH EXACTLY
    final n = sourceView.getUint32(from, Endian.little);
    if (n == 0 ||
        n * TableFixedLimits.entryBytes + TableFixedLimits.layoutHeaderBytes !=
            length) {
      refusal = TableFixedRefusal.layoutCountMismatch;
      return false;
    }
    view = sourceView;
    at = from;
    count = n;
    // 2. THE ROOT IS A TABLE, and its size is the record's body
    if (kind(0) != 13) {
      refusal = TableFixedRefusal.layoutKindInvalid;
      count = 0;
      return false;
    }
    if (size(0) == 0 || size(0) > TableFixedLimits.recordMaxBytes) {
      refusal = TableFixedRefusal.layoutRecordTooLarge;
      count = 0;
      return false;
    }
    // 3. EVERY KIND IN THE CLOSED SET AND USED AS ITS DEFINITION ALLOWS, EVERY
    //    SIZE THE ONE ITS CHILDREN ACCOUNT FOR, NOTHING PAST THE WALK'S BOUND
    final used = checkEntry(0, 0);
    if (refusal != TableFixedRefusal.none) {
      count = 0;
      return false;
    }
    // 4. THE PRE-ORDER WALK CONSUMES EXACTLY THE ENTRIES: the tree closes and
    //    the layout has nothing left over
    if (used != count) {
      refusal = TableFixedRefusal.layoutTreeUnclosed;
      count = 0;
      return false;
    }
    return true;
  }
}

/// THE PLAN'S STORAGE IS THE CALLER'S, DECLARED BY CAPACITY, AND THE CODEC
/// NEVER ALLOCATES. A caller makes one of these per peer and hands it in; a
/// layout whose plan does not fit is a refusal by name, never a growth.
final class TableFixedPlan {
  TableFixedPlan(int entryCapacity, int imageBytes, int remapCapacity)
    : entries = Int32List(entryCapacity * TableFixedLane.lanes),
      capacity = entryCapacity,
      image = Uint8List(imageBytes),
      remap = Int32List(remapCapacity) {
    imageView = ByteData.sublistView(image);
  }

  final Int32List entries;
  final int capacity;
  int count = 0;

  /// the reader's own storage: this build's declared order at its own widths
  final Uint8List image;
  late final ByteData imageView;

  /// the remap tables an ordinal entry resolves through, laid down here rather
  /// than above the entries: Dart has no pointer to alias two lists through,
  /// so the pool is its own list and aux is an index into it
  final Int32List remap;
  int remapUsed = 0;

  /// an enum's remap, built here and then laid down; a scratch of the plan and
  /// never of the loop
  final Int32List remapScratch = Int32List(256);

  /// the two layout views a compile needs, held here so a compile allocates
  /// nothing either
  final TableFixedLayout theirs = TableFixedLayout();
  final TableFixedLayout mine = TableFixedLayout();

  int recordBytes = 0;
  int hash = 0;
  bool ready = false;
  bool overflow = false;

  /// the sixteen-byte conversion scratch the float rung widens through
  final ByteData conv = ByteData(16);
}

/// A CACHE BY HASH: "the cost of the compile is paid once per peer rather than
/// once per record". The caller owns this too.
final class TableFixedPlanCache {
  final Map<int, TableFixedPlan> plans = <int, TableFixedPlan>{};

  TableFixedPlan? get(int hash) => plans[hash];

  TableFixedPlan put(int hash, TableFixedPlan plan) {
    plans[hash] = plan;
    return plan;
  }
}

/// THE ONE READ LOOP. A READ IS A PREFILL AND THIS LOOP, AND NOTHING ELSE.
/// Which plan it is handed is the only thing that differs between reading this
/// build's own record and reading anybody else's — the owner's ruling, which
/// §3.4 quotes him on: a form whose cost moved when a peer shipped would be a
/// performance cliff at exactly the moment a deployment cannot afford one.
///
/// A RUN IS A SMALL, KNOWN NUMBER OF BYTES. The C++ reference writes the copy
/// as overlapping unaligned word moves because a runtime-length memcpy per
/// entry costs it the measurement the one-reader ruling rests on; the fastest
/// move Dart has for the same job is Uint8List.setRange, which is the typed
/// list's own bulk copy and not a per-byte loop, so that is what this is.
void tableFixedRun(
  Int32List plan,
  int entryCount,
  Uint8List source,
  ByteData sourceView,
  int at,
  Uint8List image,
  ByteData imageView,
  Int32List remap,
  ByteData conv,
  TableFixedReport report,
) {
  for (var i = 0; i < entryCount; i++) {
    final b = i * TableFixedLane.lanes;
    final guard = plan[b + TableFixedLane.guard];
    // an entry belonging to an ARM runs only under its own tag
    if (guard != tableFixedNoGuard &&
        source[at + guard] != plan[b + TableFixedLane.arg]) {
      continue;
    }
    final s = at + plan[b + TableFixedLane.src];
    final d = plan[b + TableFixedLane.dst];
    final size = plan[b + TableFixedLane.size];
    switch (plan[b + TableFixedLane.op]) {
      case TableFixedOp.copy:
        image.setRange(d, d + size, source, s);
        break;
      case TableFixedOp.count:
        var counted = sourceView.getInt32(s, Endian.little);
        if (counted < 0) {
          counted = 0;
          report.clamped++;
        } else if (counted > size) {
          counted = size;
          report.clamped++;
        }
        imageView.setInt32(d, counted, Endian.little);
        break;
      case TableFixedOp.text:
        final unit = plan[b + TableFixedLane.meta] == TableFixedOp.textWide
            ? 2
            : 1;
        final cap = size ~/ unit;
        var used = sourceView.getInt32(s, Endian.little);
        if (used < 0) {
          used = 0;
          report.clamped++;
        } else if (used > cap) {
          used = cap;
          report.clamped++;
        }
        imageView.setInt32(d, used, Endian.little);
        image.setRange(
          plan[b + TableFixedLane.aux],
          plan[b + TableFixedLane.aux] + size,
          source,
          s + 4,
        );
        break;
      case TableFixedOp.ordinal:
        // A VARIANT ORDINAL IS ITS POSITION IN THE LAYOUT, so a writer whose
        // enum gained a variant IN THE MIDDLE is remapped here and never
        // reinterpreted.
        var raw = 0;
        for (var k = 0; k < size; k++) {
          raw |= source[s + k] << (8 * k);
        }
        final remapAt = plan[b + TableFixedLane.aux];
        var landed = 0;
        if (raw != 0 && raw <= remap[remapAt]) {
          landed = remap[remapAt + raw];
        }
        final ordinalWidth = plan[b + TableFixedLane.meta] & 0xff;
        for (var k = 0; k < ordinalWidth; k++) {
          image[d + k] = (landed >>> (8 * k)) & 0xff;
        }
        break;
      case TableFixedOp.widen:
        // TWO'S COMPLEMENT WIDENS BY ITS SIGN BIT, which is the whole reason a
        // widen is an op and not a short copy.
        final wideWidth = plan[b + TableFixedLane.meta] & 0xff;
        final signed = (plan[b + TableFixedLane.meta] >>> 8) & 1;
        var fill = 0;
        if (signed != 0 && (source[s + size - 1] & 0x80) != 0) {
          fill = 0xff;
        }
        for (var k = 0; k < wideWidth; k++) {
          image[d + k] = k < size ? source[s + k] : fill;
        }
        report.widened++;
        break;
      case TableFixedOp.widenFloat:
        // every f32 value is exactly representable in an f64, infinities and
        // NaN payloads included, so there is nothing to round and nothing to
        // lose
        conv.setFloat64(
          0,
          sourceView.getFloat32(s, Endian.little),
          Endian.little,
        );
        for (var k = 0; k < 8; k++) {
          image[d + k] = conv.getUint8(k);
        }
        report.widened++;
        break;
      case TableFixedOp.constant:
        final value = plan[b + TableFixedLane.aux];
        for (var k = 0; k < size; k++) {
          image[d + k] = (value >>> (8 * k)) & 0xff;
        }
        break;
      default:
        break;
    }
  }
}

/// THE PLAN COMPILER. The SAME LOOP runs over this plan as over the identity
/// plan. What the compiler does, ONCE PER PEER, is what a read would otherwise
/// do once per record: map the writer's ids onto this reader's fields; leave a
/// field it cannot name OUT of the plan, which is what skips it, by arithmetic
/// that was going to step past it anyway; and leave a field the writer does
/// not carry out too, which is what defaults it, because the prefill already
/// put the declared default there.
///
/// THE PLAN IS COMPILED ONLY AFTER THE LAYOUT HAS PASSED EVERY RULE, so no
/// arithmetic here is ever performed on a size or a child count a stranger
/// chose and nobody checked.
abstract final class TableFixedCompiler {
  /// §4'S WIDENING RUNGS, and the fixed form spends no rule of its own on
  /// them: a kind that GREW since the writer decodes at the writer's width and
  /// lands exactly, counting one widened. Coming back DOWN the ladder, or
  /// across two of them, is a kind that MOVED and is reported rather than
  /// reinterpreted.
  static bool widens(int from, int to) {
    if (from >= 6 && from <= 9 && to >= 6 && to <= 9) {
      return to > from; // u8 .. u64
    }
    if (from >= 2 && from <= 5 && to >= 2 && to <= 5) {
      return to > from; // i8 .. i64
    }
    return from == 10 && to == 11; // f32 -> f64
  }

  static bool signedKind(int kind) => kind >= 2 && kind <= 5;

  static void push(
    TableFixedPlan plan,
    int op,
    int src,
    int dst,
    int size,
    int aux,
    int guard,
    int arg,
    int meta,
  ) {
    if (plan.count >= plan.capacity) {
      plan.overflow = true;
      return;
    }
    final b = plan.count * TableFixedLane.lanes;
    final e = plan.entries;
    e[b + TableFixedLane.op] = op;
    e[b + TableFixedLane.src] = src;
    e[b + TableFixedLane.dst] = dst;
    e[b + TableFixedLane.size] = size;
    e[b + TableFixedLane.aux] = aux;
    e[b + TableFixedLane.guard] = guard;
    e[b + TableFixedLane.arg] = arg;
    e[b + TableFixedLane.meta] = meta;
    plan.count++;
  }

  static int layRemap(TableFixedPlan plan, Int32List values, int n) {
    final need = n + 1;
    if (plan.remapUsed + need > plan.remap.length) {
      plan.overflow = true;
      return 0;
    }
    final at = plan.remapUsed;
    plan.remap[at] = n;
    for (var i = 0; i < n; i++) {
      plan.remap[at + 1 + i] = values[i];
    }
    plan.remapUsed += need;
    return at;
  }

  /// matchChildren walks a TABLE's children on both sides, by id.
  static void matchChildren(
    TableFixedPlan plan,
    int ti,
    int theirAt,
    int mi,
    Int32List dst,
    int myAt,
    int guard,
    int arg,
    TableFixedReport report,
  ) {
    final theirs = plan.theirs;
    final mine = plan.mine;
    final theirChildren = theirs.children(ti);
    final myChildren = mine.children(mi);
    var myChild = mi + 1;
    for (var k = 0; k < myChildren; k++) {
      var theirChild = ti + 1;
      var theirOff = theirAt;
      for (var j = 0; j < theirChildren; j++) {
        if (theirs.id(theirChild) == mine.id(myChild)) {
          compileEntry(
            plan,
            theirChild,
            theirOff,
            myChild,
            dst,
            myAt,
            guard,
            arg,
            report,
          );
          break;
        }
        theirOff += theirs.size(theirChild);
        theirChild += theirs.subtree(theirChild);
      }
      myChild += mine.subtree(myChild);
    }
    // EVERY FIELD OF THEIRS I COULD NOT NAME IS ONE unknown
    var tc = ti + 1;
    for (var j = 0; j < theirChildren; j++) {
      var named = false;
      var mc = mi + 1;
      for (var k = 0; k < myChildren; k++) {
        if (mine.id(mc) == theirs.id(tc)) {
          named = true;
          break;
        }
        mc += mine.subtree(mc);
      }
      if (!named) {
        report.unknown++;
      }
      tc += theirs.subtree(tc);
    }
  }

  static void compileEntry(
    TableFixedPlan plan,
    int ti,
    int theirAt,
    int mi,
    Int32List dst,
    int myAt,
    int guard,
    int arg,
    TableFixedReport report,
  ) {
    final theirs = plan.theirs;
    final mine = plan.mine;
    final theirKind = theirs.kind(ti);
    final myKind = mine.kind(mi);
    final theirSize = theirs.size(ti);
    final mySize = mine.size(mi);
    final row = mi * TableFixedLane.dstLanes;
    final at = myAt + dst[row + TableFixedLane.dstOffset];
    final auxAt = myAt + dst[row + TableFixedLane.dstAux];
    if (theirKind != myKind) {
      if (widens(theirKind, myKind)) {
        final op = theirKind == 10
            ? TableFixedOp.widenFloat
            : TableFixedOp.widen;
        push(
          plan,
          op,
          theirAt,
          at,
          theirSize,
          0,
          guard,
          arg,
          (mySize & 0xff) | (signedKind(theirKind) ? 0x100 : 0),
        );
        return;
      }
      // A KIND THAT MOVED IS REPORTED AND NEVER REINTERPRETED (§4).
      report.kindMismatch++;
      return;
    }
    switch (myKind) {
      case 35: // the OPTIONAL wrapper: the present byte, then the payload
        push(plan, TableFixedOp.copy, theirAt, auxAt, 1, 0, guard, arg, 0);
        compileEntry(
          plan,
          ti + 1,
          theirAt + 1,
          mi + 1,
          dst,
          myAt,
          guard,
          arg,
          report,
        );
        break;
      case 13: // a nested table: match its fields
        matchChildren(plan, ti, theirAt, mi, dst, at, guard, arg, report);
        break;
      case 14: // an array: the count, then min( their bound, my bound )
        final theirElem = theirs.size(ti + 1);
        final myElem = mine.size(mi + 1);
        final head = dst[row + TableFixedLane.dstCounted] != 0 ? 4 : 0;
        final theirN = theirElem != 0 ? (theirSize - head) ~/ theirElem : 0;
        final myN = myElem != 0 ? (mySize - head) ~/ myElem : 0;
        if (dst[row + TableFixedLane.dstCounted] != 0) {
          push(plan, TableFixedOp.count, theirAt, auxAt, myN, 0, guard, arg, 0);
        }
        final theirBase = theirAt + head;
        final elems = theirN < myN ? theirN : myN;
        for (var i = 0; i < elems; i++) {
          compileEntry(
            plan,
            ti + 1,
            theirBase + i * theirElem,
            mi + 1,
            dst,
            at + i * dst[row + TableFixedLane.dstStride],
            guard,
            arg,
            report,
          );
        }
        break;
      case 16: // an enum-keyed array: every slot, matched by the KEY's id
        final theirKeys = theirs.children(ti + 1);
        final myKeys = mine.children(mi + 1);
        final theirElemAt = ti + 1 + theirs.subtree(ti + 1);
        final myElemAt = mi + 1 + mine.subtree(mi + 1);
        final theirElemSize = theirs.size(theirElemAt);
        for (var k = 0; k < myKeys; k++) {
          for (var j = 0; j < theirKeys; j++) {
            if (theirs.id(ti + 2 + j) != mine.id(mi + 2 + k)) {
              continue;
            }
            compileEntry(
              plan,
              theirElemAt,
              theirAt + j * theirElemSize,
              myElemAt,
              dst,
              at + k * dst[row + TableFixedLane.dstStride],
              guard,
              arg,
              report,
            );
            break;
          }
        }
        break;
      case 15: // a union: the tag, remapped, then each arm matched by id
        final theirTag = theirs.tagBytes(ti);
        final myTag = mine.tagBytes(mi);
        final theirArms = theirs.children(ti);
        final myArms = mine.children(mi);
        var myArm = mi + 1;
        for (var k = 0; k < myArms; k++) {
          var theirArm = ti + 1;
          for (var j = 0; j < theirArms; j++) {
            if (theirs.id(theirArm) == mine.id(myArm)) {
              // MY tag value, written under THEIR tag's guard
              push(
                plan,
                TableFixedOp.constant,
                theirAt,
                auxAt,
                myTag,
                k + 1,
                theirAt,
                j + 1,
                0,
              );
              compileEntry(
                plan,
                theirArm,
                theirAt + theirTag,
                myArm,
                dst,
                at,
                theirAt,
                j + 1,
                report,
              );
              break;
            }
            theirArm += theirs.subtree(theirArm);
          }
          myArm += mine.subtree(myArm);
        }
        break;
      case 30: // an enum: the ordinal is the layout's position, so it remaps
        final theirVariants = theirs.children(ti);
        final myVariants = mine.children(mi);
        final variants = theirVariants < 255 ? theirVariants : 255;
        for (var j = 0; j < variants; j++) {
          var landed = 0;
          for (var k = 0; k < myVariants; k++) {
            if (mine.id(mi + 1 + k) == theirs.id(ti + 1 + j)) {
              landed = k + 1;
              break;
            }
          }
          plan.remapScratch[j] = landed;
        }
        final remapAt = layRemap(plan, plan.remapScratch, variants);
        push(
          plan,
          TableFixedOp.ordinal,
          theirAt,
          at,
          theirSize,
          remapAt,
          guard,
          arg,
          mySize & 0xff,
        );
        break;
      case 12: // string(N)
      case 33: // wstring(N)
        final units = (mySize - 4) < (theirSize - 4)
            ? (mySize - 4)
            : (theirSize - 4);
        push(
          plan,
          TableFixedOp.text,
          theirAt,
          at,
          units,
          auxAt,
          guard,
          arg,
          dst[row + TableFixedLane.dstArg],
        );
        break;
      default:
        if (theirSize == mySize) {
          push(plan, TableFixedOp.copy, theirAt, at, mySize, 0, guard, arg, 0);
        } else if (theirSize < mySize && mySize <= 8) {
          push(
            plan,
            TableFixedOp.widen,
            theirAt,
            at,
            theirSize,
            0,
            guard,
            arg,
            mySize & 0xff,
          );
        } else {
          report.kindMismatch++;
        }
        break;
    }
  }

  /// THE COALESCER, and it is the only optimization a plan compiler performs:
  /// two neighbouring COPY entries whose source and destination both advance
  /// together are one entry. It is performed identically on both sides.
  static void coalesce(TableFixedPlan plan) {
    final e = plan.entries;
    var out = 0;
    for (var i = 0; i < plan.count; i++) {
      final b = i * TableFixedLane.lanes;
      if (out > 0) {
        final p = (out - 1) * TableFixedLane.lanes;
        if (e[p + TableFixedLane.op] == TableFixedOp.copy &&
            e[b + TableFixedLane.op] == TableFixedOp.copy &&
            e[p + TableFixedLane.guard] == e[b + TableFixedLane.guard] &&
            e[p + TableFixedLane.arg] == e[b + TableFixedLane.arg] &&
            e[p + TableFixedLane.src] + e[p + TableFixedLane.size] ==
                e[b + TableFixedLane.src] &&
            e[p + TableFixedLane.dst] + e[p + TableFixedLane.size] ==
                e[b + TableFixedLane.dst]) {
          e[p + TableFixedLane.size] += e[b + TableFixedLane.size];
          continue;
        }
      }
      final o = out * TableFixedLane.lanes;
      for (var k = 0; k < TableFixedLane.lanes; k++) {
        e[o + k] = e[b + k];
      }
      out++;
    }
    plan.count = out;
  }

  /// compile builds the plan for ANOTHER writer's layout against my own and
  /// answers how many entries it wrote, or -1 when it did not fit. The peer's
  /// layout must already have passed TableFixedLayout.parse.
  static int compile(
    TableFixedPlan plan,
    Uint8List myLayout,
    ByteData myLayoutView,
    Int32List myDst,
    TableFixedReport report,
  ) {
    if (!plan.mine.parse(myLayoutView, 0, myLayout.length)) {
      return -1;
    }
    plan.count = 0;
    plan.remapUsed = 0;
    plan.overflow = false;
    matchChildren(plan, 0, 0, 0, myDst, 0, tableFixedNoGuard, 0, report);
    if (plan.overflow) {
      return -1;
    }
    coalesce(plan);
    return plan.count;
  }
}
`
