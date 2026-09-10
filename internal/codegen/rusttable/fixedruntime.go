package rusttable

import "github.com/mas-bandwidth/schema/v2/ir"

// fixedRuntimeModule is the FIXED FORM's shared runtime (docs/SPEC-TABLES.md
// §3.4), emitted once per unit that carries a fixed root: the plan entry, the
// ONE read loop, the block reader and the plan compiler.
//
// EVERYTHING HERE IS ALLOCATION-FREE and SAFE. Allocation-free is this
// library's rule everywhere and has no exception on this form: the plan's
// storage is the caller's, declared by capacity, and a block whose plan does
// not fit is a refusal by name. Safe is this PORT's, and it is what the image
// domain buys — the two accelerators need raw pointers because they point INTO
// a foreign extent, and this form copies out of one, so nothing here needs a
// pointer at all.
//
// A FORGED BLOCK REACHES THIS CODE. The block is a stranger's bytes, so every
// offset the compiler derives from it is checked against the record before it
// is read, and a miss is `malformed` rather than a panic.
func fixedRuntimeModule(u *ir.Unit) []byte {
	return []byte(header(runtimeSourceName(u), u.Package,
		"THE FIXED FORM's shared runtime (docs/SPEC-TABLES.md §3.4)") + fixedRuntimeBody)
}

const fixedRuntimeBody = `//
// The fixed form's shared runtime, emitted once per unit.

#![allow(clippy::needless_range_loop)]
#![allow(clippy::too_many_arguments)]

/// The form byte. Forms 1 and 2 do not move and nothing here touches them.
pub const TABLE_FIXED_FORM: u8 = 3;

/// A plan entry that belongs to no union arm carries this instead of a tag
/// offset, and the loop runs it unconditionally.
pub const TABLE_FIXED_NO_GUARD: u32 = u32::MAX;

/// THE OPS ARE THE WHOLE SET. A plan for a record of plain scalars carries only
/// the first, and the identity plan carries exactly ONE of them.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum TableFixedOp {
    /// move ` + "`size`" + ` bytes from ` + "`src`" + ` to ` + "`dst`" + `
    Copy,
    /// a count: clamp it to this reader's own bound in ` + "`size`" + `
    Count,
    /// a length in ` + "`aux`" + ` units, then ` + "`size`" + ` bytes of content behind it
    Text,
    /// a variant ordinal, resolved through the plan's own remap table at ` + "`aux`" + `
    Ordinal,
    /// a narrower source at its own width into a wider destination
    Widen,
    /// f32 into f64, §4's float rung
    WidenF,
    /// a constant this reader's own image takes: a remapped union tag
    Const,
}

/// A PLAN ENTRY. ` + "`src`" + ` and ` + "`dst`" + ` are byte offsets — into the record's body and
/// into THIS BUILD'S OWN RECORD IMAGE. ` + "`guard`" + ` is the byte offset of a union tag
/// when the entry belongs to an arm, and TABLE_FIXED_NO_GUARD when it does not.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub struct TableFixedEntry {
    pub src: u32,
    pub dst: u32,
    pub size: u32,
    pub aux: u32,
    pub guard: u32,
    pub op: TableFixedOp,
    pub arg: u8,
    pub dstsize: u8,
    /// a WIDEN's source is two's complement, so it sign-extends
    pub sign: u8,
}

impl Default for TableFixedEntry {
    fn default() -> Self {
        TableFixedEntry {
            src: 0,
            dst: 0,
            size: 0,
            aux: 0,
            guard: TABLE_FIXED_NO_GUARD,
            op: TableFixedOp::Copy,
            arg: 0,
            dstsize: 0,
            sign: 0,
        }
    }
}

/// EVERY REFUSAL IS BY NAME (docs/SPEC-TABLES.md §3.4). None of these is one of
/// §4's six events: in each of them nothing was decoded and there is nothing to
/// count.
#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]
pub enum TableFixedReason {
    #[default]
    None,
    /// a form byte this build does not carry
    NewerForm,
    /// a record whose hash names no block this reader holds
    NoBlock,
    /// bytes handed to this form as a block that are not one
    BlockMalformed,
    /// a block whose compiled plan does not fit the caller's storage
    PlanTooLarge,
    /// more records than the caller has room for: nothing is decoded
    BatchTooLarge,
}

/// The read report — §4's ledger, and this form's refusals beside it. Silence
/// is the clean read: a load that moves no counter and sets no flag saw
/// nothing worth telling the caller about.
#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]
pub struct TableFixedReport {
    pub unknown: u32,
    pub kind_mismatch: u32,
    pub widened: u32,
    pub clamped: u32,
    pub malformed: bool,
    pub refused: bool,
    pub reason: TableFixedReason,
}

impl TableFixedReport {
    /// A refusal BY NAME: nothing is decoded, no counter moves, and
    /// ` + "`malformed`" + ` does not fire.
    pub fn refuse<T>(&mut self, reason: TableFixedReason) -> Option<T> {
        self.refused = true;
        self.reason = reason;
        None
    }
}

/// THE HASH is fnv1a64 over the block's bytes exactly as written (§3.4). It is
/// a wire identity and not a security claim.
pub fn table_fixed_hash(block: &[u8]) -> u64 {
    let mut h: u64 = 0xcbf2_9ce4_8422_2325;
    for b in block {
        h ^= u64::from(*b);
        h = h.wrapping_mul(0x0000_0100_0000_01b3);
    }
    h
}

// ---- the little-endian loads ------------------------------------------------

fn get32(b: &[u8], at: usize) -> u32 {
    match b.get(at..at + 4) {
        Some(w) => u32::from_le_bytes(w.try_into().expect("four bytes")),
        None => 0,
    }
}

fn get64(b: &[u8], at: usize) -> u64 {
    match b.get(at..at + 8) {
        Some(w) => u64::from_le_bytes(w.try_into().expect("eight bytes")),
        None => 0,
    }
}

/// §4'S WIDENING RUNGS, and this form spends no rule of its own on them: a kind
/// that GREW since the writer decodes at the writer's width and lands exactly,
/// counting one widened. Coming back DOWN the ladder, or across two of them, is
/// a kind that MOVED and is reported rather than reinterpreted.
fn widens(from: u8, to: u8) -> bool {
    if (6..=9).contains(&from) && (6..=9).contains(&to) {
        return to > from; // u8 .. u64
    }
    if (2..=5).contains(&from) && (2..=5).contains(&to) {
        return to > from; // i8 .. i64
    }
    from == 10 && to == 11 // f32 -> f64
}

fn signed_kind(kind: u8) -> bool {
    (2..=5).contains(&kind)
}

// ---- THE ONE READ LOOP ------------------------------------------------------
//
// A READ IS A PREFILL AND THIS LOOP, AND NOTHING ELSE. Which plan it was handed
// is the only thing that differs between reading this build's own record and
// reading anybody else's, which is the owner's ruling that this form's cost
// must not move when a peer ships.
//
// EVERY REACH INTO ` + "`src`" + ` IS CHECKED, because ` + "`src`" + ` is framed by a stranger's
// block: a plan offset past the record is framing damage, and it is reported
// as such rather than trusted.

/// Runs one plan over one record body, landing it in ` + "`image`" + `.
pub fn table_fixed_run(
    plan: &[TableFixedEntry],
    remap: &[u16],
    src: &[u8],
    image: &mut [u8],
    report: &mut TableFixedReport,
) {
    for p in plan {
        if p.guard != TABLE_FIXED_NO_GUARD {
            match src.get(p.guard as usize) {
                Some(tag) if *tag == p.arg => {}
                Some(_) => continue,
                None => {
                    report.malformed = true;
                    return;
                }
            }
        }
        let s = p.src as usize;
        let d = p.dst as usize;
        match p.op {
            TableFixedOp::Copy => {
                let n = p.size as usize;
                match (src.get(s..s + n), image.get_mut(d..d + n)) {
                    (Some(from), Some(into)) => into.copy_from_slice(from),
                    _ => {
                        report.malformed = true;
                        return;
                    }
                }
            }
            TableFixedOp::Count => {
                if src.len() < s + 4 || image.len() < d + 4 {
                    report.malformed = true;
                    return;
                }
                let raw = get32(src, s) as i32;
                let held = raw.clamp(0, p.size as i32);
                if held != raw {
                    report.clamped += 1;
                }
                image[d..d + 4].copy_from_slice(&held.to_le_bytes());
            }
            TableFixedOp::Text => {
                let n = p.size as usize;
                if src.len() < s + 4 + n || image.len() < d + 4 + n {
                    report.malformed = true;
                    return;
                }
                let raw = get32(src, s) as i32;
                let held = raw.clamp(0, p.aux as i32);
                if held != raw {
                    report.clamped += 1;
                }
                image[d..d + 4].copy_from_slice(&held.to_le_bytes());
                let (head, tail) = image.split_at_mut(d + 4);
                let _ = &head;
                tail[..n].copy_from_slice(&src[s + 4..s + 4 + n]);
            }
            TableFixedOp::Ordinal => {
                // A VARIANT ORDINAL IS ITS POSITION IN THE BLOCK, so a writer
                // whose enum gained a variant IN THE MIDDLE is remapped here
                // and never reinterpreted.
                let n = p.size as usize;
                let w = p.dstsize as usize;
                if src.len() < s + n || image.len() < d + w {
                    report.malformed = true;
                    return;
                }
                let mut raw: u64 = 0;
                for i in 0..n {
                    raw |= u64::from(src[s + i]) << (8 * i);
                }
                let base = p.aux as usize;
                let held = match remap.get(base) {
                    Some(count) if raw != 0 && raw <= u64::from(*count) => {
                        u64::from(remap[base + raw as usize])
                    }
                    Some(count) => {
                        // AN ORDINAL PAST THE WRITER'S OWN LAST VARIANT IS OUT
                        // OF RANGE, as a count past its bound is: it lands as
                        // None and counts one clamped. A variant the writer DOES
                        // carry and this reader cannot name is a different thing
                        // — it resolves to None through the table and counts
                        // nothing, because nothing was out of range.
                        if raw > u64::from(*count) {
                            report.clamped += 1;
                        }
                        0
                    }
                    None => 0,
                };
                for i in 0..w {
                    image[d + i] = (held >> (8 * i)) as u8;
                }
            }
            TableFixedOp::Widen => {
                let n = p.size as usize;
                let w = p.dstsize as usize;
                if src.len() < s + n || image.len() < d + w || n == 0 || n > 8 {
                    report.malformed = true;
                    return;
                }
                let mut raw: u64 = 0;
                for i in 0..n {
                    raw |= u64::from(src[s + i]) << (8 * i);
                }
                if p.sign != 0 {
                    // TWO'S COMPLEMENT WIDENS BY ITS SIGN BIT, which is the
                    // whole reason a widen is an op and not a short copy.
                    let top = 1u64 << (n * 8 - 1);
                    if raw & top != 0 {
                        raw |= !((top << 1).wrapping_sub(1));
                    }
                }
                for i in 0..w {
                    image[d + i] = (raw >> (8 * i)) as u8;
                }
                report.widened += 1;
            }
            TableFixedOp::WidenF => {
                if src.len() < s + 4 || image.len() < d + 8 {
                    report.malformed = true;
                    return;
                }
                let f = f32::from_le_bytes(src[s..s + 4].try_into().expect("four bytes"));
                image[d..d + 8].copy_from_slice(&f64::from(f).to_le_bytes());
                report.widened += 1;
            }
            TableFixedOp::Const => {
                let n = p.size as usize;
                if image.len() < d + n || n > 4 {
                    report.malformed = true;
                    return;
                }
                for i in 0..n {
                    image[d + i] = (p.aux >> (8 * i)) as u8;
                }
            }
        }
    }
}

// ---- THE BLOCK --------------------------------------------------------------
//
// An entry is SEVENTEEN BYTES and the block is a count and a run of them. THE
// FORMAT NEVER MOVES, which is what lets a reader of this major parse a later
// major's block and step over a kind it does not know by the size the entry
// states.

const ENTRY_BYTES: usize = 17;
const MAX_DEPTH: u32 = 32;

/// One block entry, read out of the bytes.
#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]
pub struct TableFixedBlockEntry {
    pub id: u64,
    pub size: u32,
    pub children: u32,
    pub kind: u8,
}

/// A parsed block: the bytes and the entry count, and nothing else. Parsing is
/// a bounds check and a proof that the TREE CLOSES, which is what lets every
/// walk below index without re-checking.
#[derive(Clone, Copy)]
pub struct TableFixedBlock<'a> {
    bytes: &'a [u8],
    count: usize,
}

impl<'a> TableFixedBlock<'a> {
    pub fn parse(bytes: &'a [u8]) -> Option<TableFixedBlock<'a>> {
        if bytes.len() < 4 {
            return None;
        }
        let count = get32(bytes, 0) as usize;
        if count == 0 || count.checked_mul(ENTRY_BYTES)? + 4 != bytes.len() {
            return None;
        }
        let block = TableFixedBlock { bytes, count };
        // THE TREE HAS TO CLOSE: the root's subtree is the whole block
        if block.subtree_at(0, 0)? != count {
            return None;
        }
        Some(block)
    }

    pub fn count(&self) -> usize {
        self.count
    }

    pub fn entry(&self, i: usize) -> TableFixedBlockEntry {
        if i >= self.count {
            return TableFixedBlockEntry::default();
        }
        let e = 4 + i * ENTRY_BYTES;
        TableFixedBlockEntry {
            id: get64(self.bytes, e),
            kind: self.bytes[e + 8],
            size: get32(self.bytes, e + 9),
            children: get32(self.bytes, e + 13),
        }
    }

    /// How many entries the subtree rooted at ` + "`i`" + ` occupies, so a walk steps over
    /// a child it does not want without knowing what is in it.
    pub fn subtree(&self, i: usize) -> usize {
        self.subtree_at(i, 0).unwrap_or(1)
    }

    fn subtree_at(&self, i: usize, depth: u32) -> Option<usize> {
        if depth > MAX_DEPTH || i >= self.count {
            return None;
        }
        let e = self.entry(i);
        let mut n = 1usize;
        let mut at = i + 1;
        for _ in 0..e.children {
            if at >= self.count {
                return None;
            }
            let sub = self.subtree_at(at, depth + 1)?;
            at += sub;
            n += sub;
        }
        Some(n)
    }

    /// A union's tag width: what its entry's size has left over the widest arm.
    fn tag_bytes(&self, i: usize) -> u32 {
        self.entry(i).size.saturating_sub(self.widest_arm(i))
    }

    fn widest_arm(&self, i: usize) -> u32 {
        let e = self.entry(i);
        let mut widest = 0;
        let mut at = i + 1;
        for _ in 0..e.children {
            if at >= self.count {
                break;
            }
            let a = self.entry(at);
            if a.size > widest {
                widest = a.size;
            }
            at += self.subtree(at);
        }
        widest
    }
}

// ---- THE PLAN COMPILER ------------------------------------------------------
//
// The SAME LOOP runs over this plan as over the identity plan. What the
// compiler does, ONCE PER PEER, is what a read would otherwise do once per
// record: map the writer's ids onto this reader's fields; leave a field it
// cannot name OUT of the plan, which is what skips it, by arithmetic that was
// going to step past it anyway; and leave a field the writer does not carry out
// too, which is what defaults it, because the prefill already put the declared
// default there.
//
// MY DESTINATION OFFSET IS MY DECLARED OFFSET. That is the whole of what this
// port saves by planning in the image domain: C++ carries a side table of
// storage offsets, strides and text buffers beside its block, and here the same
// walk that produced my block produces my offsets.

struct Compiler<'a> {
    plan: &'a mut [TableFixedEntry],
    remap: &'a mut [u16],
    count: usize,
    remap_used: usize,
    overflow: bool,
}

impl Compiler<'_> {
    fn push(&mut self, e: TableFixedEntry) {
        if self.count >= self.plan.len() {
            self.overflow = true;
            return;
        }
        self.plan[self.count] = e;
        self.count += 1;
    }

    /// Lays a remap table down and answers where it starts: a count, then that
    /// many landed ordinals.
    fn lay(&mut self, values: &[u16]) -> u32 {
        let at = self.remap_used;
        if at + 1 + values.len() > self.remap.len() {
            self.overflow = true;
            return 0;
        }
        self.remap[at] = values.len() as u16;
        self.remap[at + 1..at + 1 + values.len()].copy_from_slice(values);
        self.remap_used = at + 1 + values.len();
        at as u32
    }
}

/// Compiles a plan that reads ` + "`theirs`" + `-shaped records into ` + "`mine`" + `-shaped images.
///
/// ` + "`counted`" + ` is MY side of MY block, one byte an entry, saying whether an array
/// entry carries a live count in front of it — the one storage fact a block
/// entry cannot state. Returns the entries written, or None when the caller's
/// plan or remap storage does not hold it.
pub fn table_fixed_compile(
    theirs: &TableFixedBlock,
    mine: &TableFixedBlock,
    counted: &[u8],
    plan: &mut [TableFixedEntry],
    remap: &mut [u16],
    report: &mut TableFixedReport,
) -> Option<usize> {
    let mut c = Compiler {
        plan,
        remap,
        count: 0,
        remap_used: 0,
        overflow: false,
    };
    match_children(&mut c, theirs, 0, 0, mine, 0, 0, counted, TABLE_FIXED_NO_GUARD, 0, 0, report);
    if c.overflow {
        return None;
    }
    // COALESCE, which is the only optimization a plan compiler performs and is
    // performed identically on both sides: two neighbouring copies whose source
    // and destination both advance together are one entry.
    let count = c.count;
    let mut out = 0usize;
    for i in 0..count {
        if out > 0
            && c.plan[out - 1].op == TableFixedOp::Copy
            && c.plan[i].op == TableFixedOp::Copy
            && c.plan[out - 1].guard == c.plan[i].guard
            && c.plan[out - 1].arg == c.plan[i].arg
            && c.plan[out - 1].src + c.plan[out - 1].size == c.plan[i].src
            && c.plan[out - 1].dst + c.plan[out - 1].size == c.plan[i].dst
        {
            c.plan[out - 1].size += c.plan[i].size;
            continue;
        }
        c.plan[out] = c.plan[i];
        out += 1;
    }
    Some(out)
}

/// Walks a TABLE's children on both sides, matching by id.
fn match_children(
    c: &mut Compiler,
    theirs: &TableFixedBlock,
    ti: usize,
    their_at: u32,
    mine: &TableFixedBlock,
    mi: usize,
    my_at: u32,
    counted: &[u8],
    guard: u32,
    arg: u8,
    depth: u32,
    report: &mut TableFixedReport,
) {
    if depth > MAX_DEPTH {
        c.overflow = true;
        return;
    }
    let te = theirs.entry(ti);
    let me = mine.entry(mi);
    let mut my_child = mi + 1;
    let mut my_off = my_at;
    for _ in 0..me.children {
        let mc = mine.entry(my_child);
        let mut their_child = ti + 1;
        let mut their_off = their_at;
        for _ in 0..te.children {
            let tc = theirs.entry(their_child);
            if tc.id == mc.id {
                compile_entry(
                    c, theirs, their_child, their_off, mine, my_child, my_off, counted, guard, arg,
                    depth + 1, report,
                );
                break;
            }
            their_off += tc.size;
            their_child += theirs.subtree(their_child);
        }
        my_off += mc.size;
        my_child += mine.subtree(my_child);
    }
    // EVERY FIELD OF THEIRS I COULD NOT NAME IS ONE unknown, and skipping it
    // costs nothing because skipping is not an act: the next entry's src is
    // already past it.
    let mut tc_at = ti + 1;
    for _ in 0..te.children {
        let tc = theirs.entry(tc_at);
        let mut named = false;
        let mut mc_at = mi + 1;
        for _ in 0..me.children {
            if mine.entry(mc_at).id == tc.id {
                named = true;
                break;
            }
            mc_at += mine.subtree(mc_at);
        }
        if !named {
            report.unknown += 1;
        }
        tc_at += theirs.subtree(tc_at);
    }
}

fn compile_entry(
    c: &mut Compiler,
    theirs: &TableFixedBlock,
    ti: usize,
    their_at: u32,
    mine: &TableFixedBlock,
    mi: usize,
    my_at: u32,
    counted: &[u8],
    guard: u32,
    arg: u8,
    depth: u32,
    report: &mut TableFixedReport,
) {
    if depth > MAX_DEPTH {
        c.overflow = true;
        return;
    }
    let te = theirs.entry(ti);
    let me = mine.entry(mi);
    if te.kind != me.kind {
        if widens(te.kind, me.kind) {
            c.push(TableFixedEntry {
                src: their_at,
                dst: my_at,
                size: te.size,
                dstsize: me.size as u8,
                guard,
                arg,
                op: if te.kind == 10 {
                    TableFixedOp::WidenF
                } else {
                    TableFixedOp::Widen
                },
                sign: u8::from(signed_kind(te.kind)),
                ..TableFixedEntry::default()
            });
            return;
        }
        // A KIND THAT MOVED IS REPORTED AND NEVER REINTERPRETED (§4).
        report.kind_mismatch += 1;
        return;
    }
    match me.kind {
        35 => {
            // the OPTIONAL wrapper: the present byte, then the payload whole
            c.push(TableFixedEntry {
                src: their_at,
                dst: my_at,
                size: 1,
                guard,
                arg,
                op: TableFixedOp::Copy,
                ..TableFixedEntry::default()
            });
            compile_entry(
                c, theirs, ti + 1, their_at + 1, mine, mi + 1, my_at + 1, counted, guard, arg,
                depth + 1, report,
            );
        }
        13 => match_children(
            c, theirs, ti, their_at, mine, mi, my_at, counted, guard, arg, depth + 1, report,
        ),
        14 => {
            // an array: the count, then min( their bound, my bound ) elements
            let tel = theirs.entry(ti + 1);
            let mel = mine.entry(mi + 1);
            let head = if counted.get(mi).copied().unwrap_or(0) != 0 {
                4u32
            } else {
                0u32
            };
            let their_n = te.size.saturating_sub(head).checked_div(tel.size).unwrap_or(0);
            let my_n = me.size.saturating_sub(head).checked_div(mel.size).unwrap_or(0);
            if head != 0 {
                c.push(TableFixedEntry {
                    src: their_at,
                    dst: my_at,
                    size: my_n,
                    guard,
                    arg,
                    op: TableFixedOp::Count,
                    ..TableFixedEntry::default()
                });
            }
            let n = their_n.min(my_n);
            for i in 0..n {
                compile_entry(
                    c,
                    theirs,
                    ti + 1,
                    their_at + head + i * tel.size,
                    mine,
                    mi + 1,
                    my_at + head + i * mel.size,
                    counted,
                    guard,
                    arg,
                    depth + 1,
                    report,
                );
            }
        }
        16 => {
            // an enum-keyed array: every slot, matched by the KEY's id, because
            // a key that slid is a slot that moved and never a slot that means
            // something else
            let tkey = theirs.entry(ti + 1);
            let mkey = mine.entry(mi + 1);
            let tel = ti + 1 + theirs.subtree(ti + 1);
            let mel = mi + 1 + mine.subtree(mi + 1);
            let tee = theirs.entry(tel);
            let mee = mine.entry(mel);
            for k in 0..mkey.children as usize {
                let key_id = mine.entry(mi + 2 + k).id;
                for j in 0..tkey.children as usize {
                    if theirs.entry(ti + 2 + j).id != key_id {
                        continue;
                    }
                    compile_entry(
                        c,
                        theirs,
                        tel,
                        their_at + j as u32 * tee.size,
                        mine,
                        mel,
                        my_at + k as u32 * mee.size,
                        counted,
                        guard,
                        arg,
                        depth + 1,
                        report,
                    );
                    break;
                }
            }
        }
        15 => {
            // a union: MY tag written under THEIR tag's guard, then each arm
            // matched by id. Nothing in this port declares one yet (the Row
            // follow-on), and a stranger's union still lands correctly here.
            let their_tag = theirs.tag_bytes(ti);
            let my_tag = mine.tag_bytes(mi);
            let mut my_arm = mi + 1;
            for k in 0..me.children as usize {
                let ma = mine.entry(my_arm);
                let mut their_arm = ti + 1;
                for j in 0..te.children as usize {
                    let ta = theirs.entry(their_arm);
                    if ta.id == ma.id {
                        c.push(TableFixedEntry {
                            src: their_at,
                            dst: my_at,
                            size: my_tag,
                            aux: k as u32 + 1,
                            guard: their_at,
                            arg: (j + 1) as u8,
                            op: TableFixedOp::Const,
                            ..TableFixedEntry::default()
                        });
                        compile_entry(
                            c,
                            theirs,
                            their_arm,
                            their_at + their_tag,
                            mine,
                            my_arm,
                            my_at + my_tag,
                            counted,
                            their_at,
                            (j + 1) as u8,
                            depth + 1,
                            report,
                        );
                        break;
                    }
                    their_arm += theirs.subtree(their_arm);
                }
                my_arm += mine.subtree(my_arm);
            }
        }
        30 => {
            // an enum: the ordinal is the block's POSITION, so it remaps by
            // NAME and a variant inserted in the middle never reinterprets one
            let mut map = [0u16; 256];
            let n = (te.children as usize).min(255);
            for j in 0..n {
                let vid = theirs.entry(ti + 1 + j).id;
                let mut landed = 0u16;
                for k in 0..me.children as usize {
                    if mine.entry(mi + 1 + k).id == vid {
                        landed = (k + 1) as u16;
                        break;
                    }
                }
                map[j] = landed;
            }
            let aux = c.lay(&map[..n]);
            c.push(TableFixedEntry {
                src: their_at,
                dst: my_at,
                size: te.size,
                aux,
                guard,
                arg,
                op: TableFixedOp::Ordinal,
                dstsize: me.size as u8,
                ..TableFixedEntry::default()
            });
        }
        12 | 33 => {
            // text: the length, clamped to MY bound in units, then the content
            let unit = if me.kind == 33 { 2u32 } else { 1u32 };
            let bytes = me.size.saturating_sub(4).min(te.size.saturating_sub(4));
            c.push(TableFixedEntry {
                src: their_at,
                dst: my_at,
                size: bytes,
                aux: me.size.saturating_sub(4).checked_div(unit).unwrap_or(0),
                guard,
                arg,
                op: TableFixedOp::Text,
                ..TableFixedEntry::default()
            });
        }
        _ => {
            if te.size == me.size {
                c.push(TableFixedEntry {
                    src: their_at,
                    dst: my_at,
                    size: me.size,
                    guard,
                    arg,
                    op: TableFixedOp::Copy,
                    ..TableFixedEntry::default()
                });
            } else if te.size < me.size && me.size <= 8 {
                c.push(TableFixedEntry {
                    src: their_at,
                    dst: my_at,
                    size: te.size,
                    dstsize: me.size as u8,
                    guard,
                    arg,
                    op: TableFixedOp::Widen,
                    sign: u8::from(signed_kind(te.kind)),
                    ..TableFixedEntry::default()
                });
            } else {
                report.kind_mismatch += 1;
            }
        }
    }
}
`
