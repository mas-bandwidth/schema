package jstable

// fixedRuntime is the FIXED FORM's shared JavaScript runtime
// (docs/SPEC-TABLES.md §3.4): the plan, the ONE read loop, the layout reader,
// the plan compiler and the sixty-four-bit hash. It is emitted ONCE PER UNIT
// into the unit's <Package>Table.js, exactly as the block form's runtime is
// (block.go), and every other module of the unit imports from there.
//
// THE READER'S OWN STORAGE IN THIS LANGUAGE IS THE CANONICAL BODY IMAGE.
// JavaScript has no struct layout — no offsetof, no sizeof, no way to place a
// field (jstable.go's opening statement) — so where the C++ reference's plan
// lands bytes at `offsetof( T, member )` inside a struct, this one lands them
// in a Uint8Array holding THIS BUILD's own declared order at THIS BUILD's own
// widths. That image IS the wire's layout, so the identity plan's source and
// destination are the same offsets and it coalesces to a single run; a plan
// compiled from another writer's layout moves, widens, clamps and remaps into
// the same image through the same loop. A generated straight-line decode then
// projects the image into the language's own objects, which is the step C++
// gets for free because there a struct IS its bytes.
//
// NOTHING HERE ALLOCATES ON THE READ PATH. The plan's storage is the caller's,
// declared by capacity, and a layout whose plan does not fit is a refusal by
// name — §3.3's rule for a resolved vocabulary, holding here unchanged.
const fixedRuntime = `
// ---------------------------------------------------------------------------
// THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4)
// ---------------------------------------------------------------------------

export const TableFixedForm = 3;

// THE TWO NEIGHBOURS IN THE FORM REGISTRY, which this backend does not carry
// and names anyway (docs/SPEC-TABLES.md §3). The registry is ORDERED, so a form
// byte this reader cannot read is named by WHERE IT SITS relative to this one —
// a form that came before, the message form handed to a file reader, or a form
// newer than this build — and never by one word for all three.
export const TableFixedVariableForm = 1;
export const TableFixedMessageForm = 2;

// THE HEADER IS ONE RULE FOR ALL FIVE FORMS (docs/SPEC-TABLES.md §3): the form
// byte at 0, seven reserved bytes that are zero, the LAYOUT HASH at 8, and the
// body at 16. What the fixed form puts in that body is the layout behind its
// own u32 length, and then the records to the end of the file.
export const TableFixedHeaderBytes = 16;
export const TableFixedHashAt = 8;

// An entry is SEVENTEEN BYTES and the layout is a count and a run of them. THE
// FORMAT NEVER MOVES, which is what lets a reader of this major parse a later
// major's layout and step over a kind it does not know by the size the entry
// states.
export const TableFixedEntryBytes = 17;

// the layout's own framing: the u32 entry count, and nothing else
export const TableFixedLayoutHeaderBytes = 4;

// A PLAN ENTRY is eight int32 lanes of one flat Int32Array. A flat array of a
// fixed stride is what keeps the read loop monomorphic: every lane is a
// Smi, the loop never touches a property name, and nothing is allocated.
export const TableFixedLanes = 8;
// ONE BINDING PER LINE, and that is not a style choice: §11's claim is made
// name by name, and a scan that reads a module's bindings sees the FIRST name
// of a comma list. A lane folded into a list beside another is a lane nobody
// claimed, so each stands on its own line and each is registered.
const TableFixedLaneOp = 0;
const TableFixedLaneSrc = 1;
const TableFixedLaneDst = 2;
const TableFixedLaneSize = 3;
const TableFixedLaneAux = 4;
const TableFixedLaneGuard = 5;
const TableFixedLaneArg = 6;
const TableFixedLaneMeta = 7; // a widen's width and sign, a text entry's flavour

// THE OPS ARE THE WHOLE SET. The IDENTITY plan carries only the first; the
// others are what a plan compiled from another writer's layout adds.
const TableFixedOpCopy = 0;    // move size bytes
const TableFixedOpCount = 1;   // a count: clamp it to the reader's own bound
const TableFixedOpText = 2;    // a length, then the units
const TableFixedOpOrdinal = 3; // a variant ordinal, remapped through the plan's own table
const TableFixedOpWiden = 4;   // a narrower source into a wider destination
const TableFixedOpConst = 5;   // a constant this reader's own storage takes: a remapped union tag
const TableFixedOpWidenF = 6;  // f32 into f64, SPEC-TABLES §4's float rung

// the flavours an TableFixedOpText entry lands its units under
const TableFixedTextUtf8 = 1;
const TableFixedTextWide = 2;
const TableFixedTextBytes = 3;

const TableFixedNoGuard = -1;

// WHY A READ WAS REFUSED, by name. None of these is one of §4's six events:
// in each of them nothing was decoded and there is nothing to count.
export const TableFixedRefusal = Object.freeze({
  None: 0,
  // THE THREE DIRECTIONS a form byte can be wrong in, named apart (§3)
  PreviousForm: 1,      // form 1, the variable form: a wire this backend does not carry
  MessageFormAsFile: 2, // form 2, the message form, where a FILE was expected
  NewerForm: 3,         // a form byte newer than this build
  NoLayout: 4,          // a hash naming no layout this reader holds
  LayoutMalformed: 5,   // bytes handed to this form as a layout that are not one
  PlanTooLarge: 6,      // a layout whose compiled plan does not fit the caller's storage
  BatchTooLarge: 7,     // more records than the caller's capacity
});

// A REPORT is §4's six counters and the refusal beside them. The caller owns
// it and the codec never makes one.
export class TableFixedReport {
  constructor() {
    this.malformed = false;
    this.refused = 0;
    this.unknown = 0;
    this.kindMismatch = 0;
    this.clamped = 0;
    this.widened = 0;
    this.duplicate = 0;
    this.hashLo = 0;
    this.hashHi = 0;
  }
}

export function TableFixedResetReport(report) {
  report.malformed = false;
  report.refused = 0;
  report.unknown = 0;
  report.kindMismatch = 0;
  report.clamped = 0;
  report.widened = 0;
  report.duplicate = 0;
  report.hashLo = 0;
  report.hashHi = 0;
  return report;
}

// THE PLAN'S STORAGE IS THE CALLER'S, DECLARED BY CAPACITY, AND THE CODEC
// NEVER ALLOCATES. A caller makes one of these per peer and hands it in; a
// layout whose plan does not fit is a refusal by name, never a growth.
export class TableFixedPlan {
  constructor(entryCapacity, imageBytes, remapCapacity) {
    this.entries = new Int32Array(entryCapacity * TableFixedLanes);
    this.capacity = entryCapacity | 0;
    this.count = 0;
    // the reader's own storage: this build's declared order at its own widths
    this.image = new Uint8Array(imageBytes | 0);
    this.view = new DataView(this.image.buffer);
    // the remap tables an TableFixedOpOrdinal entry resolves through, laid down here
    // rather than above the entries: JavaScript has no pointer to alias two
    // arrays through, so the pool is its own array and aux is an index into it
    this.remap = new Int32Array((remapCapacity | 0) || 1024);
    this.remapUsed = 0;
    this.recordBytes = 0;
    this.hashLo = 0;
    this.hashHi = 0;
    this.ready = false;
    this.overflow = false;
  }
}

// A CACHE BY HASH: "the cost of the compile is paid once per peer rather than
// once per record". The caller owns this too.
export class TableFixedPlanCache {
  constructor() {
    this.plans = new Map();
  }
  get(hashLo, hashHi) {
    return this.plans.get(hashHi * 4294967296 + (hashLo >>> 0));
  }
  put(hashLo, hashHi, plan) {
    this.plans.set(hashHi * 4294967296 + (hashLo >>> 0), plan);
    return plan;
  }
}

// ---- the sixty-four-bit hash, without a BigInt -----------------------------
//
// THE HASH IS fnv1a64 OVER THE LAYOUT'S BYTES, EXACTLY AS WRITTEN, and it is
// the eight bytes every record carries. It is a wire identity and not a
// security claim.
//
// It is carried as two uint32 lanes and multiplied in sixteen-bit limbs
// because a BigInt would allocate, and a hash is compared ONCE PER RECORD:
// getBigUint64 on that path is one allocation per record, which is the same
// reason the block form reads its own sixty-four-bit fields as two uint32s.
const TableFixedHashOut = new Int32Array(2); // single threaded per realm, consumed in the call that fills it

export function TableFixedHashOf(bytes, at, length) {
  let hLo = 0x84222325, hHi = 0xcbf29ce4; // the fnv1a64 offset basis
  for (let i = 0; i < length; i++) {
    hLo = (hLo ^ bytes[at + i]) >>> 0;
    // h *= 0x100000001b3, keeping the low sixty-four bits
    const a0 = hLo & 0xffff, a1 = hLo >>> 16;
    const p00 = a0 * 0x01b3;
    const p01 = a0 * 0x0000;
    const p10 = a1 * 0x01b3;
    const p11 = a1 * 0x0000;
    const mid = (p00 >>> 16) + (p01 & 0xffff) + (p10 & 0xffff);
    const lo = ((p00 & 0xffff) | (mid << 16)) >>> 0;
    const hi = (p11 + (p01 >>> 16) + (p10 >>> 16) + (mid >>> 16)) >>> 0;
    hHi = (hi + Math.imul(hLo, 0x00000100) + Math.imul(hHi, 0x000001b3)) >>> 0;
    hLo = lo;
  }
  TableFixedHashOut[0] = hLo | 0;
  TableFixedHashOut[1] = hHi | 0;
  return TableFixedHashOut;
}

// ---- the little-endian reads the framing needs -----------------------------

function TableFixedGetU32(bytes, at) {
  return (bytes[at] | (bytes[at + 1] << 8) | (bytes[at + 2] << 16) | (bytes[at + 3] << 24)) >>> 0;
}

// ---- THE ONE READ LOOP -----------------------------------------------------
//
// A READ IS A PREFILL AND THIS LOOP, AND NOTHING ELSE. Which plan it is handed
// is the only thing that differs between reading this build's own record and
// reading anybody else's — the owner's ruling, which §3.4 quotes him on: a
// form whose cost moved when a peer shipped would be a performance cliff at
// exactly the moment a deployment cannot afford one.
export function TableFixedRun(plan, entryCount, src, srcView, srcAt, dst, dstView, remap, report) {
  const e = plan;
  for (let i = 0; i < entryCount; i++) {
    const b = i * TableFixedLanes;
    const guard = e[b + TableFixedLaneGuard];
    // an entry belonging to an ARM runs only under its own tag
    if (guard !== TableFixedNoGuard && src[srcAt + guard] !== e[b + TableFixedLaneArg]) { continue; }
    const s = srcAt + e[b + TableFixedLaneSrc];
    const d = e[b + TableFixedLaneDst];
    const size = e[b + TableFixedLaneSize];
    switch (e[b + TableFixedLaneOp]) {
      case TableFixedOpCopy: {
        // A RUN IS A SMALL, KNOWN NUMBER OF BYTES, AND IT MOVES IN 32-BIT
        // LANES. The C++ reference writes this as overlapping unaligned word
        // moves. JavaScript has no unaligned word move as an operator, but a
        // DataView HAS ONE: getUint32/setUint32 take any byte offset, and the
        // engine emits the unaligned load underneath. So the run is lanes and
        // then a tail of at most three bytes.
        //
        // THE VIEWS ARE THE CALLER'S, MADE ONCE PER READ. Making one here, or
        // reaching for set(subarray()), would allocate PER RECORD on the one
        // path that must not allocate — which is why they are parameters and
        // not locals, and why tables-js-alloc still reads zero.
        //
        // WHY IT WAS WORTH MOVING: on the identity path the WHOLE BODY is one
        // coalesced copy, so this loop is the read. It measured 938 ns of a
        // 1,320 ns load — 71% of it — against 250 ns for the same bytes in
        // 32-bit lanes, allocating nothing either way.
        const end = s + size;
        const lanes = end - 3;
        let k = s, o = d;
        while (k < lanes) {
          dstView.setUint32(o, srcView.getUint32(k, true), true);
          k += 4; o += 4;
        }
        while (k < end) { dst[o] = src[k]; k++; o++; }
        break;
      }
      case TableFixedOpCount: {
        let v = TableFixedGetU32(src, s) | 0;
        if (v < 0) { v = 0; report.clamped++; }
        else if (v > size) { v = size; report.clamped++; }
        dst[d] = v & 0xff; dst[d + 1] = (v >>> 8) & 0xff;
        dst[d + 2] = (v >>> 16) & 0xff; dst[d + 3] = (v >>> 24) & 0xff;
        break;
      }
      case TableFixedOpText: {
        // THE FLAVOUR RIDES IN META, NOT IN ARG. Arg is the GUARD'S VALUE —
        // the union tag an arm's entry answers to — and it is read as that by
        // the guard test above, on every entry, before any op runs. A text
        // field under a union arm needs BOTH, so they cannot share a lane
        // (docs/SPEC-TABLES.md §3.4).
        const unit = e[b + TableFixedLaneMeta] === TableFixedTextWide ? 2 : 1;
        const cap = (size / unit) | 0;
        let v = TableFixedGetU32(src, s) | 0;
        if (v < 0) { v = 0; report.clamped++; }
        else if (v > cap) { v = cap; report.clamped++; }
        dst[d] = v & 0xff; dst[d + 1] = (v >>> 8) & 0xff;
        dst[d + 2] = (v >>> 16) & 0xff; dst[d + 3] = (v >>> 24) & 0xff;
        const aux = e[b + TableFixedLaneAux];
        for (let k = 0; k < size; k++) { dst[aux + k] = src[s + 4 + k]; }
        break;
      }
      case TableFixedOpOrdinal: {
        // A VARIANT ORDINAL IS ITS POSITION IN THE LAYOUT, so a writer whose
        // enum gained a variant IN THE MIDDLE is remapped here and never
        // reinterpreted.
        let raw = 0;
        for (let k = 0; k < size; k++) { raw |= src[s + k] << (8 * k); }
        raw = raw >>> 0;
        const table = e[b + TableFixedLaneAux];
        let v = 0;
        if (raw !== 0 && raw <= remap[table]) { v = remap[table + raw]; }
        const width = e[b + TableFixedLaneMeta] & 0xff;
        for (let k = 0; k < width; k++) { dst[d + k] = (v >>> (8 * k)) & 0xff; }
        break;
      }
      case TableFixedOpWiden: {
        // TWO'S COMPLEMENT WIDENS BY ITS SIGN BIT, which is the whole reason a
        // widen is an op and not a short copy.
        const width = e[b + TableFixedLaneMeta] & 0xff;
        const sign = (e[b + TableFixedLaneMeta] >>> 8) & 1;
        let fill = 0;
        if (sign !== 0 && (src[s + size - 1] & 0x80) !== 0) { fill = 0xff; }
        for (let k = 0; k < width; k++) { dst[d + k] = k < size ? src[s + k] : fill; }
        report.widened++;
        break;
      }
      case TableFixedOpWidenF: {
        // every f32 value is exactly representable in an f64, infinities and
        // NaN payloads included, so there is nothing to round and nothing to lose
        TableFixedConv.setUint8(0, src[s]); TableFixedConv.setUint8(1, src[s + 1]);
        TableFixedConv.setUint8(2, src[s + 2]); TableFixedConv.setUint8(3, src[s + 3]);
        TableFixedConv.setFloat64(8, TableFixedConv.getFloat32(0, true), true);
        for (let k = 0; k < 8; k++) { dst[d + k] = TableFixedConv.getUint8(8 + k); }
        report.widened++;
        break;
      }
      case TableFixedOpConst: {
        const v = e[b + TableFixedLaneAux];
        for (let k = 0; k < size; k++) { dst[d + k] = (v >>> (8 * k)) & 0xff; }
        break;
      }
      default: break;
    }
  }
}

// the sixteen-byte conversion scratch — the flat packet tier's SC twin. Module
// scope is safe: single threaded per realm, consumed in the same op that fills it.
const TableFixedConv = new DataView(new ArrayBuffer(16));

// ---- THE LAYOUT ------------------------------------------------------------

// A LAYOUT VIEW is the bytes and the entry count. Every read of an entry is
// arithmetic on the seventeen-byte stride, so nothing is parsed twice and
// nothing is materialised.
export class TableFixedLayoutView {
  constructor() {
    this.bytes = null;
    this.at = 0;
    this.count = 0;
  }
}

// AN ID IS COMPARED, NEVER ARITHMETIC, so it rides as two uint32 lanes and a
// comparison is two comparisons — no BigInt on a path walked per entry.
export function TableFixedIdLo(b, i) { return TableFixedGetU32(b.bytes, b.at + TableFixedLayoutHeaderBytes + i * TableFixedEntryBytes); }
export function TableFixedIdHi(b, i) { return TableFixedGetU32(b.bytes, b.at + 8 + i * TableFixedEntryBytes); }
export function TableFixedKind(b, i) { return b.bytes[b.at + TableFixedLayoutHeaderBytes + i * TableFixedEntryBytes + 8]; }
export function TableFixedSize(b, i) { return TableFixedGetU32(b.bytes, b.at + TableFixedLayoutHeaderBytes + i * TableFixedEntryBytes + 9); }
export function TableFixedChildren(b, i) { return TableFixedGetU32(b.bytes, b.at + TableFixedLayoutHeaderBytes + i * TableFixedEntryBytes + 13); }

function TableFixedSameId(a, ai, b, bi) {
  return TableFixedIdLo(a, ai) === TableFixedIdLo(b, bi) && TableFixedIdHi(a, ai) === TableFixedIdHi(b, bi);
}

// TableFixedSubtree is how many entries the subtree rooted at i occupies, so a
// walk steps over a child it does not want without knowing what is in it.
// THIS IS WHAT MAKES SKIPPING FREE: an unknown field, and an unknown NESTED
// TYPE, are stepped over by the size their entry states.
export function TableFixedSubtree(b, i) {
  if (i < 0 || i >= b.count) { return 1; }
  const children = TableFixedChildren(b, i);
  let n = 1, at = i + 1;
  for (let c = 0; c < children; c++) {
    // A CHILD COUNT THAT DOES NOT CLOSE IS layout_malformed (§3.4), so the
    // overrun is SIGNALLED and not stepped around: an entry claiming a child
    // the layout does not hold must fail the whole-layout check, or the plan
    // compiler walks past the last entry it has.
    if (at >= b.count) { return -1; }
    const sub = TableFixedSubtree(b, at);
    if (sub < 0) { return -1; }
    at += sub;
    n += sub;
  }
  return n;
}

function TableFixedUnionArmBytes(b, i) {
  const children = TableFixedChildren(b, i);
  let widest = 0, at = i + 1;
  for (let k = 0; k < children; k++) {
    const s = TableFixedSize(b, at);
    if (s > widest) { widest = s; }
    at += TableFixedSubtree(b, at);
  }
  return widest;
}

function TableFixedTagBytes(b, i) { return TableFixedSize(b, i) - TableFixedUnionArmBytes(b, i); }

// A LAYOUT IS REFUSED WHOLE OR NOT AT ALL: a count that overruns, a tree that
// does not close. It sets nothing on its way out.
export function TableFixedParseLayout(bytes, at, length, out) {
  if (bytes === null || length < TableFixedLayoutHeaderBytes) { return false; }
  const count = TableFixedGetU32(bytes, at);
  if (count === 0 || count * TableFixedEntryBytes + TableFixedLayoutHeaderBytes !== length) { return false; }
  out.bytes = bytes;
  out.at = at;
  out.count = count;
  // THE TREE HAS TO CLOSE: the root's subtree is the whole layout
  if (TableFixedSubtree(out, 0) !== out.count) {
    out.bytes = null; out.at = 0; out.count = 0;
    return false;
  }
  return true;
}

// ---- THE PLAN COMPILER -----------------------------------------------------
//
// The SAME LOOP runs over this plan as over the identity plan. What the
// compiler does, ONCE PER PEER, is what a read would otherwise do once per
// record: map the writer's ids onto this reader's fields; leave a field it
// cannot name OUT of the plan, which is what skips it, by arithmetic that was
// going to step past it anyway; and leave a field the writer does not carry
// out too, which is what defaults it, because the prefill already put the
// declared default there.

// MY SIDE of the layout, five int32 lanes per entry of my own layout: the
// storage facts a layout entry cannot carry. In C++ these are offsetof rows;
// here they are offsets into the canonical image.
const TableFixedDstLanes = 5;
const TableFixedDstOff = 0;
const TableFixedDstStride = 1;
const TableFixedDstAux = 2;
const TableFixedDstCounted = 3;
const TableFixedDstArg = 4;

function TableFixedPush(plan, op, src, dst, size, aux, guard, arg, meta) {
  if (plan.count >= plan.capacity) { plan.overflow = true; return; }
  const b = plan.count * TableFixedLanes;
  const e = plan.entries;
  e[b + TableFixedLaneOp] = op;
  e[b + TableFixedLaneSrc] = src;
  e[b + TableFixedLaneDst] = dst;
  e[b + TableFixedLaneSize] = size;
  e[b + TableFixedLaneAux] = aux;
  e[b + TableFixedLaneGuard] = guard;
  e[b + TableFixedLaneArg] = arg;
  e[b + TableFixedLaneMeta] = meta;
  plan.count++;
}

// §4'S WIDENING RUNGS, and the fixed form spends no rule of its own on them: a
// kind that GREW since the writer decodes at the writer's width and lands
// exactly, counting one widened. Coming back DOWN the ladder, or across two of
// them, is a kind that MOVED and is reported rather than reinterpreted.
function TableFixedWidens(from, to) {
  if (from >= 6 && from <= 9 && to >= 6 && to <= 9) { return to > from; } // u8 .. u64
  if (from >= 2 && from <= 5 && to >= 2 && to <= 5) { return to > from; } // i8 .. i64
  return from === 10 && to === 11;                                        // f32 -> f64
}

function TableFixedSignedKind(kind) { return kind >= 2 && kind <= 5; }

function TableFixedLayRemap(plan, values, n) {
  const need = n + 1;
  if (plan.remapUsed + need > plan.remap.length) { plan.overflow = true; return 0; }
  const at = plan.remapUsed;
  plan.remap[at] = n;
  for (let i = 0; i < n; i++) { plan.remap[at + 1 + i] = values[i]; }
  plan.remapUsed += need;
  return at;
}

const TableFixedRemapScratch = new Int32Array(256); // an enum's remap, built then laid down

// matchChildren walks a TABLE's children on both sides, by id.
function TableFixedMatchChildren(plan, theirs, ti, theirAt, mine, mi, dst, myAt, guard, arg, report) {
  const theirChildren = TableFixedChildren(theirs, ti);
  const myChildren = TableFixedChildren(mine, mi);
  let myChild = mi + 1;
  for (let k = 0; k < myChildren; k++) {
    let theirChild = ti + 1;
    let theirOff = theirAt;
    for (let j = 0; j < theirChildren; j++) {
      if (TableFixedSameId(theirs, theirChild, mine, myChild)) {
        TableFixedCompileEntry(plan, theirs, theirChild, theirOff, mine, myChild, dst, myAt, guard, arg, report);
        break;
      }
      theirOff += TableFixedSize(theirs, theirChild);
      theirChild += TableFixedSubtree(theirs, theirChild);
    }
    myChild += TableFixedSubtree(mine, myChild);
  }
  // EVERY FIELD OF THEIRS I COULD NOT NAME IS ONE unknown
  let tcAt = ti + 1;
  for (let j = 0; j < theirChildren; j++) {
    let named = false;
    let mcAt = mi + 1;
    for (let k = 0; k < myChildren; k++) {
      if (TableFixedSameId(mine, mcAt, theirs, tcAt)) { named = true; break; }
      mcAt += TableFixedSubtree(mine, mcAt);
    }
    if (!named) { report.unknown++; }
    tcAt += TableFixedSubtree(theirs, tcAt);
  }
}

function TableFixedCompileEntry(plan, theirs, ti, theirAt, mine, mi, dst, myAt, guard, arg, report) {
  const theirKind = TableFixedKind(theirs, ti);
  const myKind = TableFixedKind(mine, mi);
  const theirSize = TableFixedSize(theirs, ti);
  const mySize = TableFixedSize(mine, mi);
  const row = mi * TableFixedDstLanes;
  const at = myAt + dst[row + TableFixedDstOff];
  const auxAt = myAt + dst[row + TableFixedDstAux];
  if (theirKind !== myKind) {
    if (TableFixedWidens(theirKind, myKind)) {
      const op = theirKind === 10 ? TableFixedOpWidenF : TableFixedOpWiden;
      TableFixedPush(plan, op, theirAt, at, theirSize, 0, guard, arg,
        (mySize & 0xff) | (TableFixedSignedKind(theirKind) ? 0x100 : 0));
      return;
    }
    // A KIND THAT MOVED IS REPORTED AND NEVER REINTERPRETED (§4).
    report.kindMismatch++;
    return;
  }
  switch (myKind) {
    case 35: { // the OPTIONAL wrapper: the present byte, then the payload whole
      TableFixedPush(plan, TableFixedOpCopy, theirAt, auxAt, 1, 0, guard, arg, 0);
      TableFixedCompileEntry(plan, theirs, ti + 1, theirAt + 1, mine, mi + 1, dst, myAt, guard, arg, report);
      break;
    }
    case 13: { // a nested table: match its fields
      TableFixedMatchChildren(plan, theirs, ti, theirAt, mine, mi, dst, at, guard, arg, report);
      break;
    }
    case 14: { // an array: the count, then min( their bound, my bound ) elements
      const theirElem = TableFixedSize(theirs, ti + 1);
      const myElem = TableFixedSize(mine, mi + 1);
      const head = dst[row + TableFixedDstCounted] ? 4 : 0;
      const theirN = theirElem ? ((theirSize - head) / theirElem) | 0 : 0;
      const myN = myElem ? ((mySize - head) / myElem) | 0 : 0;
      if (dst[row + TableFixedDstCounted]) {
        TableFixedPush(plan, TableFixedOpCount, theirAt, auxAt, myN, 0, guard, arg, 0);
      }
      const theirBase = theirAt + head;
      const n = theirN < myN ? theirN : myN;
      for (let i = 0; i < n; i++) {
        TableFixedCompileEntry(plan, theirs, ti + 1, theirBase + i * theirElem,
          mine, mi + 1, dst, at + i * dst[row + TableFixedDstStride], guard, arg, report);
      }
      break;
    }
    case 16: { // an enum-keyed array: every slot, matched by the KEY's id
      const theirKeyChildren = TableFixedChildren(theirs, ti + 1);
      const myKeyChildren = TableFixedChildren(mine, mi + 1);
      const theirElemAt = ti + 1 + TableFixedSubtree(theirs, ti + 1);
      const myElemAt = mi + 1 + TableFixedSubtree(mine, mi + 1);
      const theirElemSize = TableFixedSize(theirs, theirElemAt);
      for (let k = 0; k < myKeyChildren; k++) {
        for (let j = 0; j < theirKeyChildren; j++) {
          if (!TableFixedSameId(theirs, ti + 2 + j, mine, mi + 2 + k)) { continue; }
          TableFixedCompileEntry(plan, theirs, theirElemAt, theirAt + j * theirElemSize,
            mine, myElemAt, dst, at + k * dst[row + TableFixedDstStride], guard, arg, report);
          break;
        }
      }
      break;
    }
    case 15: { // a union: the tag, remapped, then each arm matched by id
      const theirTag = TableFixedTagBytes(theirs, ti);
      const myTag = TableFixedTagBytes(mine, mi);
      const theirArms = TableFixedChildren(theirs, ti);
      const myArms = TableFixedChildren(mine, mi);
      let myArm = mi + 1;
      for (let k = 0; k < myArms; k++) {
        let theirArm = ti + 1;
        for (let j = 0; j < theirArms; j++) {
          if (TableFixedSameId(theirs, theirArm, mine, myArm)) {
            // MY tag value, written under THEIR tag's guard
            TableFixedPush(plan, TableFixedOpConst, theirAt, auxAt, myTag, k + 1, theirAt, j + 1, 0);
            TableFixedCompileEntry(plan, theirs, theirArm, theirAt + theirTag,
              mine, myArm, dst, at, theirAt, j + 1, report);
            break;
          }
          theirArm += TableFixedSubtree(theirs, theirArm);
        }
        myArm += TableFixedSubtree(mine, myArm);
      }
      break;
    }
    case 30: { // an enum: the ordinal is the layout's position, so it remaps
      const theirVariants = TableFixedChildren(theirs, ti);
      const myVariants = TableFixedChildren(mine, mi);
      const n = theirVariants < 255 ? theirVariants : 255;
      for (let j = 0; j < n; j++) {
        let landed = 0;
        for (let k = 0; k < myVariants; k++) {
          if (TableFixedSameId(mine, mi + 1 + k, theirs, ti + 1 + j)) { landed = k + 1; break; }
        }
        TableFixedRemapScratch[j] = landed;
      }
      const table = TableFixedLayRemap(plan, TableFixedRemapScratch, n);
      TableFixedPush(plan, TableFixedOpOrdinal, theirAt, at, theirSize, table, guard, arg, mySize & 0xff);
      break;
    }
    case 12: case 33: { // text
      // ARG STAYS THE ARM'S, AND THE FLAVOUR GOES IN META. Writing the flavour
      // into arg was a text field under a union arm losing its guard value: the
      // read loop would then compare the writer's TAG BYTE against a text
      // flavour, so the entry either never ran under its own arm or ran under
      // somebody else's and smeared its bytes across a sibling's storage. The
      // two facts are independent, so they get independent lanes.
      const units = (mySize - 4) < (theirSize - 4) ? (mySize - 4) : (theirSize - 4);
      TableFixedPush(plan, TableFixedOpText, theirAt, at, units, auxAt, guard, arg, dst[row + TableFixedDstArg]);
      break;
    }
    default: {
      if (theirSize === mySize) {
        TableFixedPush(plan, TableFixedOpCopy, theirAt, at, mySize, 0, guard, arg, 0);
      } else if (theirSize < mySize && mySize <= 8) {
        TableFixedPush(plan, TableFixedOpWiden, theirAt, at, theirSize, 0, guard, arg, mySize & 0xff);
      } else {
        report.kindMismatch++;
      }
      break;
    }
  }
}

// THE COALESCER, and it is the only optimization a plan compiler performs: two
// neighbouring COPY entries whose source and destination both advance together
// are one entry. It is performed identically on both sides.
function TableFixedCoalesce(plan) {
  const e = plan.entries;
  let out = 0;
  for (let i = 0; i < plan.count; i++) {
    const b = i * TableFixedLanes;
    if (out > 0) {
      const p = (out - 1) * TableFixedLanes;
      if (e[p + TableFixedLaneOp] === TableFixedOpCopy && e[b + TableFixedLaneOp] === TableFixedOpCopy &&
          e[p + TableFixedLaneGuard] === e[b + TableFixedLaneGuard] && e[p + TableFixedLaneArg] === e[b + TableFixedLaneArg] &&
          e[p + TableFixedLaneSrc] + e[p + TableFixedLaneSize] === e[b + TableFixedLaneSrc] &&
          e[p + TableFixedLaneDst] + e[p + TableFixedLaneSize] === e[b + TableFixedLaneDst]) {
        e[p + TableFixedLaneSize] += e[b + TableFixedLaneSize];
        continue;
      }
    }
    const o = out * TableFixedLanes;
    for (let k = 0; k < TableFixedLanes; k++) { e[o + k] = e[b + k]; }
    out++;
  }
  plan.count = out;
}

// TableFixedCompile builds the plan for ANOTHER writer's layout against my own,
// and answers how many entries it wrote, or -1 when it did not fit.
export function TableFixedCompile(theirs, myLayout, myDst, plan, report) {
  const mine = new TableFixedLayoutView();
  if (!TableFixedParseLayout(myLayout, 0, myLayout.length, mine)) { return -1; }
  plan.count = 0;
  plan.remapUsed = 0;
  plan.overflow = false;
  TableFixedMatchChildren(plan, theirs, 0, 0, mine, 0, myDst, 0, TableFixedNoGuard, 0, report);
  if (plan.overflow) { return -1; }
  TableFixedCoalesce(plan);
  return plan.count;
}
`
