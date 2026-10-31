// THE RUST CONFORMANCE DRIVER (test/conformance/README.md).
//
// One process per surface. The harness hands it the derived manifest, the
// surface name and an output directory; the driver writes one file per case
// and says nothing else. Every expectation lives in the DATA — this file holds
// no literal instance, no expected byte and no expected count.
//
//   driver <manifest> list
//   driver <manifest> <surface> <outdir>
//
// Exit 0 means the surface ran. Exit 2 means this backend does not implement
// it, which the matrix prints as ABSENT rather than as a failure.
//
// The cook's node dump is HERE rather than in a second binary: the Rust leg's
// cook reader is a generated module like every other, so there is nothing for
// a separate process to own, and one exec per surface is what keeps the leg
// inside the two-minute rule (#320).

use std::alloc::{Layout, alloc_zeroed, dealloc};
use std::fs;
use std::io::Write;
use std::path::Path;
use std::process::exit;

// ---------------------------------------------------------------------------
// the manifest, read exactly as testdata/conformance/tables/FORMAT.md states it
// ---------------------------------------------------------------------------

struct Manifest {
    lines: Vec<Vec<String>>,
}

impl Manifest {
    fn read(path: &str) -> Manifest {
        let text = match fs::read_to_string(path) {
            Ok(t) => t,
            Err(e) => fail(&format!("cannot open {path}: {e}")),
        };
        let mut lines = Vec::new();
        for line in text.lines() {
            if line.is_empty() || line.starts_with('#') {
                continue;
            }
            let fields: Vec<String> = line
                .split(|c: char| c == ' ' || c == '\t' || c == '\r')
                .filter(|s| !s.is_empty())
                .map(|s| s.to_string())
                .collect();
            if fields.is_empty() || fields[0].starts_with('#') {
                continue;
            }
            lines.push(fields);
        }
        Manifest { lines }
    }

    fn of_kind<'a>(&'a self, kind: &'a str) -> impl Iterator<Item = &'a Vec<String>> {
        self.lines.iter().filter(move |f| f[0] == kind)
    }
}

fn fail(what: &str) -> ! {
    eprintln!("driver: {what}");
    exit(1)
}

fn slurp(path: &str) -> Vec<u8> {
    match fs::read(path) {
        Ok(b) => b,
        Err(e) => fail(&format!("cannot read {path}: {e}")),
    }
}

fn spill(dir: &str, name: &str, data: &[u8]) {
    let path = Path::new(dir).join(name);
    let mut file = match fs::File::create(&path) {
        Ok(f) => f,
        Err(e) => fail(&format!("cannot write {}: {e}", path.display())),
    };
    if let Err(e) = file.write_all(data) {
        fail(&format!("cannot write {}: {e}", path.display()));
    }
}



// ---------------------------------------------------------------------------
// aligned storage
// ---------------------------------------------------------------------------
//
// A block's base is 64-byte aligned by construction (§19.1) and a cook's is
// whatever its header names, so the bytes are copied once into aligned
// storage — which is what a host engine's boundary looks like, and it keeps
// the alignment checks real ones.

struct Aligned {
    base: *mut u8,
    layout: Layout,
    bytes: i64,
}

impl Aligned {
    // `extent` is the length the CALLER claims, which a forgery may set past
    // the bytes it carries: that is the fact two rows of the block battery are
    // about, and a file alone cannot carry it. The allocation IS the claim, so
    // a reader that walks past what it was given walks off the end of a real
    // allocation rather than into a neighbour.
    fn new(data: &[u8], extent: i64) -> Aligned {
        // A CLAIM SHORTER THAN THE IMAGE IS A TRUNCATION and is the caller's
        // own answer: `placed` copies what fits and zeroes the rest, so the
        // allocation stays the claim in that direction too.
        let bytes = if extent < 0 { data.len() as i64 } else { extent };
        Aligned::placed(data, bytes, 0)
    }

    // The buffer the CALLER holds: exactly `claim` bytes, `lead` bytes past an
    // aligned base, with what fits copied in and the rest zeroed. A claim
    // SHORT of the file is a truncation, and the allocation IS the claim, so a
    // reader that walks past what it was given walks off the end of a real
    // allocation.
    fn placed(data: &[u8], claim: i64, lead: usize) -> Aligned {
        let want = (claim as usize + lead).max(1);
        let layout = Layout::from_size_align(want, 64).unwrap();
        let allocation = unsafe { alloc_zeroed(layout) };
        if allocation.is_null() {
            fail("out of memory");
        }
        let base = unsafe { allocation.add(lead) };
        let copy = (claim as usize).min(data.len());
        unsafe { std::ptr::copy_nonoverlapping(data.as_ptr(), base, copy) };
        Aligned {
            base: allocation,
            layout,
            bytes: claim,
        }
    }

    // the base the caller passes, which is `lead` bytes into the allocation
    fn at(&self, lead: usize) -> *mut u8 {
        unsafe { self.base.add(lead) }
    }
}

impl Drop for Aligned {
    fn drop(&mut self) {
        unsafe { dealloc(self.base, self.layout) };
    }
}

// ---------------------------------------------------------------------------
// the block's canonical ROW dump (docs/SPEC-TABLES.md §19.2)
// ---------------------------------------------------------------------------
//
// The `block` surface says only that an image OPENS, which a reader passes by
// checking the prologue and stopping. This is the value-for-value read, so two
// implementations' reads of the same bytes are byte-compared — and it is
// produced from §8's DESCRIPTORS and nothing else, no generated row struct,
// because that is the claim §19.2 makes for them.
//
// A FLOAT is its IEEE-754 BIT PATTERN. A block row is a byte-identical
// projection, so its bits are the fact; a decimal spelling would be a rounding
// rule two languages have to agree on for no gain.

use blockdemo::TableBlockInfo;

unsafe fn dump_scalar_block(at: *const u8, kind: u8, width: u32) -> String {
    unsafe {
        match kind {
            1 => {
                if *at != 0 {
                    "true".to_string()
                } else {
                    "false".to_string()
                }
            }
            10 => format!("{:#010x}", std::ptr::read_unaligned(at as *const u32)),
            11 => format!("{:#018x}", std::ptr::read_unaligned(at as *const u64)),
            2..=5 => {
                let v: i64 = match width {
                    1 => std::ptr::read_unaligned(at as *const i8) as i64,
                    2 => std::ptr::read_unaligned(at as *const i16) as i64,
                    4 => std::ptr::read_unaligned(at as *const i32) as i64,
                    _ => std::ptr::read_unaligned(at as *const i64),
                };
                format!("{v}")
            }
            _ => {
                let v: u64 = match width {
                    1 => *at as u64,
                    2 => std::ptr::read_unaligned(at as *const u16) as u64,
                    4 => std::ptr::read_unaligned(at as *const u32) as u64,
                    _ => std::ptr::read_unaligned(at as *const u64),
                };
                format!("{v}")
            }
        }
    }
}

// One record's leaves, at two spaces, in descriptor order. Out-of-line arrays
// are the caller's business: they are a section of their own, not a leaf.
unsafe fn dump_block_record(out: &mut String, storage: *const u8, info: &'static TableBlockInfo, path: &str) {
    unsafe {
        for f in info.fields.iter() {
            if f.out_of_line {
                continue;
            }
            let name = join(path, f.name);
            if f.counted {
                // a string or a `bytes`: the used length lives at count_offset
                let used = read_i32(storage.add(f.count_offset as usize));
                if used < 0 || used > f.array_bound {
                    fail(&format!(
                        "{}.{} carries a used length of {used}, outside [ 0, {} ]",
                        info.name, f.name, f.array_bound
                    ));
                }
                out.push_str(&format!("  {name} = {}\n", dump_text(storage.add(f.offset as usize), used)));
            } else {
                let slots = if f.is_array { f.array_bound as i64 } else { 1 };
                for slot in 0..slots {
                    let at = if f.is_array {
                        format!("{name}[{slot}]")
                    } else {
                        name.clone()
                    };
                    let value = storage.add(f.offset as usize + (slot * f.elem_size as i64) as usize);
                    match f.element {
                        Some(element) => dump_block_record(out, value, element(), &at),
                        None => out.push_str(&format!(
                            "  {at} = {}\n",
                            dump_scalar_block(value, f.kind, f.elem_size)
                        )),
                    }
                }
            }
            if f.optional {
                let present = *storage.add(f.present_offset as usize) != 0;
                out.push_str(&format!(
                    "  {name}#present = {}\n",
                    if present { "true" } else { "false" }
                ));
            }
        }
    }
}

// the whole dump of one opened block: the projection's own fields, then every
// out-of-line array in declaration order, row by row
unsafe fn dump_block(base: *const u8, info: &'static TableBlockInfo) -> String {
    unsafe {
        let mut out = format!("projection {} @0\n", info.name);
        dump_block_record(&mut out, base, info, "");
        for f in info.fields.iter() {
            if !f.out_of_line {
                continue;
            }
            let offset_of = read_u64(base.add(f.offset_of_offset as usize));
            let count = read_u32(base.add(f.count_offset as usize));
            let stride = read_u32(base.add(f.stride_offset as usize));
            let row = match f.element {
                Some(element) => element(),
                None => fail(&format!("{} names no element", f.name)),
            };
            out.push_str(&format!(
                "array {} {} @{offset_of} count={count} stride={stride}\n",
                f.name, row.name
            ));
            for r in 0..count {
                let at = offset_of + r as u64 * stride as u64;
                out.push_str(&format!("row {r} @{at}\n"));
                dump_block_record(&mut out, base.add(at as usize), row, "");
            }
        }
        out
    }
}

fn block_dump(name: &str, data: &[u8]) -> String {
    let storage = Aligned::new(data, -1);
    unsafe {
        if name.starts_with("block_render") {
            match blockdemo::RenderFrameBlock::open(storage.base, storage.bytes) {
                Some(block) => dump_block(block.base(), blockdemo::RenderFrameBlock::type_info()),
                None => fail(&format!("{name} did not open")),
            }
        } else if name.starts_with("block_padded") {
            match blockdemo::PaddedFrameBlock::open(storage.base, storage.bytes) {
                Some(block) => dump_block(block.base(), blockdemo::PaddedFrameBlock::type_info()),
                None => fail(&format!("{name} did not open")),
            }
        } else {
            fail(&format!("no block named {name}"))
        }
    }
}

fn read_u64(p: *const u8) -> u64 {
    unsafe { std::ptr::read_unaligned(p as *const u64) }
}

fn read_u32(p: *const u8) -> u32 {
    unsafe { std::ptr::read_unaligned(p as *const u32) }
}

// `lead` is the POINTER column: 0 an aligned base, 1..63 that many bytes past
// one. An unaligned base is a pointer fact and not a file fact, so the bytes are
// copied AT the base the column states and the base-alignment clause is the one
// that must refuse them.
fn open_block(name: &str, data: &[u8], extent: i64, lead: usize) -> bool {
    let claim = if extent < 0 { data.len() as i64 } else { extent };
    let storage = Aligned::placed(data, claim, lead);
    let base = storage.at(lead);
    unsafe {
        if name.starts_with("block_render") {
            blockdemo::RenderFrameBlock::open(base, storage.bytes).is_some()
        } else if name.starts_with("block_padded") {
            blockdemo::PaddedFrameBlock::open(base, storage.bytes).is_some()
        } else {
            fail(&format!("no block named {name}"))
        }
    }
}

// ---------------------------------------------------------------------------
// the surfaces
// ---------------------------------------------------------------------------


fn surface_block_dump(manifest: &Manifest, out: &str) {
    for f in manifest.of_kind("block") {
        let data = slurp(&f[3]);
        let text = block_dump(&f[1], &data);
        spill(out, &f[1], text.as_bytes());
    }
}


fn surface_block(manifest: &Manifest, out: &str) {
    for f in manifest.of_kind("block") {
        let data = slurp(&f[3]);
        let verdict = if open_block(&f[1], &data, -1, 0) {
            "open\n"
        } else {
            "refuse\n"
        };
        spill(out, &f[1], verdict.as_bytes());
    }
}

// THE FOREIGN SURFACES (test/conformance/README.md): a cook and a block are
// produced in the byte order of the build that wrote them (docs/SPEC-TABLES.md
// §7, §19.1), and each carries a MAGIC word read bytewise so a reader meets a
// foreign file at the first eight bytes and stops there. The fixture is made
// foreign by REVERSING those eight bytes, which makes it foreign to whoever
// reads it rather than foreign to one host — so `refuse` is the answer on every
// leg on every host, and a big-endian leg can be GREEN here rather than absent.
fn foreign(data: &[u8]) -> Vec<u8> {
    let mut out = data.to_vec();
    if out.len() >= 8 {
        out[..8].reverse();
    }
    out
}

fn surface_block_foreign(manifest: &Manifest, out: &str) {
    for f in manifest.of_kind("block") {
        let data = foreign(&slurp(&f[3]));
        let verdict = if open_block(&f[1], &data, -1, 0) {
            "open\n"
        } else {
            "refuse\n"
        };
        spill(out, &f[1], verdict.as_bytes());
    }
}

fn surface_cook_foreign(manifest: &Manifest, out: &str) {
    for f in manifest.of_kind("cook") {
        let data = foreign(&slurp(&f[4]));
        let storage = Aligned::new(&data, -1);
        let opened =
            unsafe { open_cook_at(&f[3], storage.base, storage.bytes as u64).is_some() };
        let verdict = if opened { "open\n" } else { "refuse\n" };
        spill(out, &f[1], verdict.as_bytes());
    }
}

fn surface_forgery(manifest: &Manifest, out: &str) {
    for f in manifest.of_kind("forgery") {
        if f[2] != "block" {
            continue; // the cook's battery is the surface beside this one
        }
        let data = slurp(&f[4]);
        let extent: i64 = parse_int(&f[5]);
        // the POINTER column is the buffer the caller holds: 0 an aligned base,
        // 1..63 that many bytes past one. The block battery carries one such
        // row, and it is read here rather than assumed.
        let null_buffer = f[6] == "null";
        let lead = if null_buffer { 0 } else { parse_int(&f[6]) as usize };
        let verdict = if null_buffer {
            "refuse\n"
        } else if open_block(&f[3], &data, extent, lead) {
            "open\n"
        } else {
            "refuse\n"
        };
        spill(out, &f[1], verdict.as_bytes());
    }
}

// The COOK forgery battery: the same shape as the block one, over the kind
// this driver's cook reader answers. The POINTER column is the buffer the
// caller holds — 0 an aligned base, 1..63 that many bytes past one, `null` no
// buffer at all — because an unaligned base is a pointer fact and not a file
// fact, and a file alone cannot carry it.
fn surface_cook_forgery(manifest: &Manifest, out: &str) {
    for f in manifest.of_kind("forgery") {
        if f[2] != "cook" {
            continue;
        }
        let data = slurp(&f[4]);
        let extent: i64 = parse_int(&f[5]);
        let claim = if extent < 0 { data.len() as i64 } else { extent };
        let null_buffer = f[6] == "null";
        let lead = if null_buffer { 0 } else { parse_int(&f[6]) as usize };
        let storage = Aligned::placed(&data, claim, lead);
        let opened = unsafe {
            if null_buffer {
                open_cook_at(&f[3], std::ptr::null(), claim as u64).is_some()
            } else {
                open_cook_at(&f[3], storage.at(lead), claim as u64).is_some()
            }
        };
        let verdict = if opened { "open\n" } else { "refuse\n" };
        spill(out, &f[1], verdict.as_bytes());
    }
}

// The reason surfaces use the same forged pointer and caller extent as the
// open surfaces, then name the first failed clause (§7, §19.2).
fn surface_reason(manifest: &Manifest, out: &str, kind: &str) {
    for f in manifest.of_kind("forgery") {
        if f[2] != kind { continue; }
        let data = slurp(&f[4]);
        let extent: i64 = parse_int(&f[5]);
        let claim = if extent < 0 { data.len() as i64 } else { extent };
        let null_buffer = f[6] == "null";
        let lead = if null_buffer { 0 } else { parse_int(&f[6]) as usize };
        let storage = Aligned::placed(&data, claim, lead);
        let base = if null_buffer { std::ptr::null() } else { storage.at(lead) as *const u8 };
        let reason = unsafe {
            if kind == "block" {
                if f[3].starts_with("block_render") {
                    blockdemo::RenderFrameBlock::open_reason(base, claim)
                } else if f[3].starts_with("block_padded") {
                    blockdemo::PaddedFrameBlock::open_reason(base, claim)
                } else { fail(&format!("no block named {}", f[3])) }
            } else {
                let (size, align) = match f[3].as_str() {
                    "Scene" => (graphdemo::SceneCook::ROOT_SIZE, graphdemo::SceneCook::ROOT_ALIGN),
                    "Depot" => (graphdemo::DepotCook::ROOT_SIZE, graphdemo::DepotCook::ROOT_ALIGN),
                    "Album" => (graphdemo::AlbumCook::ROOT_SIZE, graphdemo::AlbumCook::ROOT_ALIGN),
                    "TreeNode" => (graphdemo::TreeNodeCook::ROOT_SIZE, graphdemo::TreeNodeCook::ROOT_ALIGN),
                    "ListNode" => (graphdemo::ListNodeCook::ROOT_SIZE, graphdemo::ListNodeCook::ROOT_ALIGN),
                    _ => fail(&format!("no cook root named {}", f[3])),
                };
                graphdemo::table_cook_open_reason(base, claim as u64, size, align)
            }
        };
        spill(out, &f[1], format!("{reason}\n").as_bytes());
    }
}

fn parse_int(text: &str) -> i64 {
    let text = text.trim();
    let (negative, digits) = match text.strip_prefix('-') {
        Some(rest) => (true, rest),
        None => (false, text),
    };
    let value = if let Some(hex) = digits.strip_prefix("0x").or(digits.strip_prefix("0X")) {
        i64::from_str_radix(hex, 16)
    } else {
        digits.parse::<i64>()
    };
    match value {
        Ok(v) => {
            if negative {
                -v
            } else {
                v
            }
        }
        Err(_) => fail(&format!("not a number: {text}")),
    }
}

// ---------------------------------------------------------------------------
// the cook's canonical node dump (docs/SPEC-TABLES.md §7.5)
// ---------------------------------------------------------------------------
//
// The walk every reader makes through its OWN derefs, written as text, so two
// implementations' walks are byte-compared rather than merely both succeeding.
// A record laid out one byte differently INSIDE a node moves no node offset
// and no directory entry, so this is the gate the attribution check cannot be.

use graphdemo::{TableCookFieldInfo, TableCookInfo, TableCookStorage};

struct Walk {
    region: *const u8,
    data_length: u64,
    reached: Vec<(u64, &'static str)>,
    out: String,
}

impl Walk {
    fn node(&mut self, offset: u64, info: &'static TableCookInfo, depth: i32) {
        if depth > 4096 {
            fail("the walk nested past any depth a region can hold — a cycle the deref did not close");
        }
        if let Some((_, name)) = self.reached.iter().find(|(at, _)| *at == offset) {
            if *name != info.name {
                fail(&format!(
                    "two references name the node at offset {offset} as two different tables: {name} and {}",
                    info.name
                ));
            }
            return; // one node, one visit: sharing and a back-reference are one fact (§6.3)
        }
        if offset > self.data_length || info.size as u64 > self.data_length - offset {
            fail(&format!(
                "the node at offset {offset} ({}, size {}) does not fit inside the region's {} bytes",
                info.name, info.size, self.data_length
            ));
        }
        let index = self.reached.len();
        self.reached.push((offset, info.name));
        self.out
            .push_str(&format!("node {index} {} @{offset}\n", info.name));
        let storage = unsafe { self.region.add(offset as usize) };
        self.storage(storage, info, depth, "");
    }

    fn storage(&mut self, storage: *const u8, info: &'static TableCookInfo, depth: i32, path: &str) {
        for f in info.fields.iter() {
            let name = join(path, f.name);

            // every COUNT COMPANION, against its declared bound, and a negative
            // one refuses too — an extent is never negative, and a walker handed
            // one indexes backwards out of the region (§7.4's pass two).
            let mut used: i32 = -1;
            if f.count_offset >= 0 {
                used = unsafe { read_i32(storage.add(f.count_offset as usize)) };
                if used < 0 || used > f.array_bound {
                    fail(&format!(
                        "{}.{} carries a count companion of {used}, outside [ 0, {} ]",
                        info.name, f.name, f.array_bound
                    ));
                }
            }

            if f.is_pointer {
                let slot = unsafe { storage.add(f.offset as usize) };
                let delta = unsafe { read_i64(slot) };
                if delta == 0 {
                    // NULL IN A REGION IS A DELTA OF ZERO (§6.3)
                    self.line(&name, "null");
                    continue;
                }
                let target = unsafe { slot.offset(delta as isize) };
                if target < self.region
                    || target >= unsafe { self.region.add(self.data_length as usize) }
                {
                    fail(&format!(
                        "{}.{} resolves outside the region — a delta of {delta}",
                        info.name, f.name
                    ));
                }
                let record = match f.record {
                    Some(r) => r(),
                    None => fail(&format!(
                        "{}.{} is a pointer whose descriptor names no record",
                        info.name, f.name
                    )),
                };
                let target_offset = (target as usize - self.region as usize) as u64;
                self.line(&name, &format!("-> @{target_offset}"));
                self.node(target_offset, record, depth + 1);
                continue;
            }

            match f.storage {
                TableCookStorage::String | TableCookStorage::Bytes => {
                    // a string's or a bytes' USED bytes, without the zero tail (§7.2)
                    let text = unsafe { dump_text(storage.add(f.offset as usize), used) };
                    self.line(&name, &text);
                }
                TableCookStorage::Record => {
                    // a nested record — by value, or every slot of an array of
                    // them. A COUNTED array writes all N slots (§7.2), and a slot
                    // past the live count holds the value-initialised element.
                    let record = match f.record {
                        Some(r) => r(),
                        None => fail(&format!("{}.{} names no record", info.name, f.name)),
                    };
                    for slot in 0..field_slots(f) {
                        let slot_path = if f.is_array {
                            format!("{name}[{slot}]")
                        } else {
                            name.clone()
                        };
                        let at = unsafe {
                            storage.add(f.offset as usize + (slot * f.elem_size as i64) as usize)
                        };
                        self.storage(at, record, depth, &slot_path);
                    }
                }
                _ => {
                    for slot in 0..field_slots(f) {
                        let slot_path = if f.is_array {
                            format!("{name}[{slot}]")
                        } else {
                            name.clone()
                        };
                        let at = unsafe {
                            storage.add(f.offset as usize + (slot * f.elem_size as i64) as usize)
                        };
                        let value = unsafe { dump_scalar(at, f.storage, f.elem_size as u32) };
                        self.line(&slot_path, &value);
                    }
                }
            }

            let is_text = matches!(
                f.storage,
                TableCookStorage::String | TableCookStorage::Bytes
            );
            if f.count_offset >= 0 && !is_text {
                self.line(&format!("{name}#count"), &format!("{used}"));
            }
            if f.present_offset >= 0 {
                let present = unsafe { *storage.add(f.present_offset as usize) != 0 };
                self.line(
                    &format!("{name}#present"),
                    if present { "true" } else { "false" },
                );
            }
        }
    }

    fn line(&mut self, path: &str, value: &str) {
        self.out.push_str(&format!("  {path} = {value}\n"));
    }
}

// The number of storage slots a field has, which is what a cook writes: a
// COUNTED array writes all N slots (§7.2), a keyed array writes one per named
// variant, and a fixed array writes N.
fn field_slots(f: &TableCookFieldInfo) -> i64 {
    if !f.is_array {
        return 1;
    }
    f.array_bound as i64
}

fn join(prefix: &str, name: &str) -> String {
    if prefix.is_empty() {
        name.to_string()
    } else {
        format!("{prefix}.{name}")
    }
}

unsafe fn read_i32(p: *const u8) -> i32 {
    unsafe { std::ptr::read_unaligned(p as *const i32) }
}

unsafe fn read_i64(p: *const u8) -> i64 {
    unsafe { std::ptr::read_unaligned(p as *const i64) }
}

unsafe fn dump_text(at: *const u8, used: i32) -> String {
    let used = used.max(0) as usize;
    let mut out = String::from("\"");
    for i in 0..used {
        let c = unsafe { *at.add(i) };
        if (0x20..0x7f).contains(&c) && c != b'"' && c != b'\\' {
            out.push(c as char);
        } else {
            out.push_str(&format!("\\x{c:02x}"));
        }
    }
    out.push('"');
    out.push_str(&format!(" len={used}"));
    out
}

// What a cooked SLOT holds, at `width` bytes. The width comes from elem_size,
// because an enum's slot holds its ORDINAL at the enum's own derived storage
// width and not the u16 hash the wire rides (§7.2).
unsafe fn dump_scalar(at: *const u8, storage: TableCookStorage, width: u32) -> String {
    unsafe {
        match storage {
            TableCookStorage::Float => {
                fail("the dump met a float, whose canonical cross-language spelling this gate does not fix")
            }
            TableCookStorage::Bool => {
                if *at != 0 {
                    "true".to_string()
                } else {
                    "false".to_string()
                }
            }
            TableCookStorage::Signed => {
                let v: i64 = match width {
                    1 => std::ptr::read_unaligned(at as *const i8) as i64,
                    2 => std::ptr::read_unaligned(at as *const i16) as i64,
                    4 => std::ptr::read_unaligned(at as *const i32) as i64,
                    _ => std::ptr::read_unaligned(at as *const i64),
                };
                format!("{v}")
            }
            _ => {
                let v: u64 = match width {
                    1 => *at as u64,
                    2 => std::ptr::read_unaligned(at as *const u16) as u64,
                    4 => std::ptr::read_unaligned(at as *const u32) as u64,
                    _ => std::ptr::read_unaligned(at as *const u64),
                };
                format!("{v}")
            }
        }
    }
}

// One cook root: open the file where it lies and dump it. The roots are the
// manifest's, so this names no fixture of its own.
fn dump_cook(root: &str, path: &str) -> String {
    let data = slurp(path);
    let storage = Aligned::new(&data, -1);
    let (region, data_length, info) = unsafe { open_cook(root, storage.base, storage.bytes as u64) };
    let mut walk = Walk {
        region,
        data_length,
        reached: Vec::new(),
        out: String::new(),
    };
    walk.node(0, info, 0);
    walk.out
}

// The same Open, answering whether it opened rather than failing: the forgery
// battery's verdict is exactly that question.
unsafe fn open_cook_at(root: &str, base: *const u8, length: u64) -> Option<(*const u8, u64)> {
    unsafe {
        macro_rules! try_root {
            ($name:literal, $cook:ty) => {
                if root == $name {
                    return <$cook>::open(base, length).map(|c| (c.region(), c.region_length()));
                }
            };
        }
        try_root!("Scene", graphdemo::SceneCook);
        try_root!("Depot", graphdemo::DepotCook);
        try_root!("Album", graphdemo::AlbumCook);
        try_root!("TreeNode", graphdemo::TreeNodeCook);
        try_root!("ListNode", graphdemo::ListNodeCook);
        fail(&format!("no cook root named {root}"))
    }
}

unsafe fn open_cook(root: &str, base: *mut u8, length: u64) -> (*const u8, u64, &'static TableCookInfo) {
    unsafe {
        macro_rules! try_root {
            ($name:literal, $cook:ty, $info:path) => {
                if root == $name {
                    let cook = match <$cook>::open(base, length) {
                        Some(c) => c,
                        None => fail(&format!(
                            "the cook {} did not open — the tool wrote it and this build cannot point at it",
                            $name
                        )),
                    };
                    return (cook.region(), cook.region_length(), $info());
                }
            };
        }
        try_root!("Scene", graphdemo::SceneCook, graphdemo::scene_cook_info);
        try_root!("Depot", graphdemo::DepotCook, graphdemo::depot_cook_info);
        try_root!("Album", graphdemo::AlbumCook, graphdemo::album_cook_info);
        try_root!("TreeNode", graphdemo::TreeNodeCook, graphdemo::tree_node_cook_info);
        try_root!("ListNode", graphdemo::ListNodeCook, graphdemo::list_node_cook_info);
        fail(&format!("no cook root named {root}"))
    }
}

fn surface_cook(manifest: &Manifest, out: &str) {
    for f in manifest.of_kind("cook") {
        // cook <case> <unit> <root> <file>: the CASE names the dump and the
        // ROOT names the reader, and they differ wherever one root has more
        // than one fixture
        let text = dump_cook(&f[3], &f[4]);
        spill(out, &f[1], text.as_bytes());
    }
}

// ---------------------------------------------------------------------------
// the WIRE-CARRYING surfaces (docs/SPEC-TABLES.md §3, §3.1, §16, §16.7)
// ---------------------------------------------------------------------------
//
// The variable form, form byte 1, over the roots this leg's variable wire
// carries: the pointer corpus's Scene (graphdemo), the byte-buffer corpus's
// Catalog (blobdemo), the map corpus's roots (mapdemo) and the fixed roots of
// the three evolution pairs (tblv1/tblv2, tblw1/tblw2, tblr1/tblr2) and the
// wide-text unit's roots (widedemo), and the wide scalars' SimState under both
// builds (scalars, tblscalars2: the fixed-point family and the 128-bit
// integers, §3 kinds 18–29). Every
// other case is answered `<case>.absent` — a root this driver does not name
// yet, or a unit the compiler refuses for rust whole, as tables/lists is for
// its union — which the matrix counts as the row of work that is left, never
// as a pass.

// One root's codec, by its unit and root name: the four verbs the surfaces
// drive, each answering through the root's own generated functions. The
// report type is inferred from the generated load, so the driver names no
// runtime type that two of a unit's runtimes both export.
macro_rules! with_root {
    ($unit:expr, $root:expr, |$load:ident, $save:ident, $to_json:ident, $from_json:ident| $body:expr) => {{
        match ($unit, $root) {
            ("scalars", "SimState") => {
                #[allow(unused_imports)]
                use scalars::{sim_state_from_json_bytes as $from_json, sim_state_load as $load, sim_state_save_checked as $save, sim_state_to_json_checked as $to_json};
                Some($body)
            }
            ("tblscalars2", "SimState") => {
                #[allow(unused_imports)]
                use tblscalars2::{sim_state_from_json_bytes as $from_json, sim_state_load as $load, sim_state_save_checked as $save, sim_state_to_json_checked as $to_json};
                Some($body)
            }
            ("graphdemo", "Scene") => {
                #[allow(unused_imports)]
                use graphdemo::{scene_from_json_bytes as $from_json, scene_load as $load, scene_save_checked as $save, scene_to_json_checked as $to_json};
                Some($body)
            }
            ("blobdemo", "Catalog") => {
                #[allow(unused_imports)]
                use blobdemo::{catalog_from_json_bytes as $from_json, catalog_load as $load, catalog_save_checked as $save, catalog_to_json_checked as $to_json};
                Some($body)
            }
            ("mapdemo", "Cells") => {
                #[allow(unused_imports)]
                use mapdemo::{cells_from_json_bytes as $from_json, cells_load as $load, cells_save_checked as $save, cells_to_json_checked as $to_json};
                Some($body)
            }
            ("mapdemo", "Chunks") => {
                #[allow(unused_imports)]
                use mapdemo::{chunks_from_json_bytes as $from_json, chunks_load as $load, chunks_save_checked as $save, chunks_to_json_checked as $to_json};
                Some($body)
            }
            ("mapdemo", "Crews") => {
                #[allow(unused_imports)]
                use mapdemo::{crews_from_json_bytes as $from_json, crews_load as $load, crews_save_checked as $save, crews_to_json_checked as $to_json};
                Some($body)
            }
            ("mapdemo", "Depth") => {
                #[allow(unused_imports)]
                use mapdemo::{depth_from_json_bytes as $from_json, depth_load as $load, depth_save_checked as $save, depth_to_json_checked as $to_json};
                Some($body)
            }
            ("mapdemo", "Docs") => {
                #[allow(unused_imports)]
                use mapdemo::{docs_from_json_bytes as $from_json, docs_load as $load, docs_save_checked as $save, docs_to_json_checked as $to_json};
                Some($body)
            }
            ("mapdemo", "Fleet") => {
                #[allow(unused_imports)]
                use mapdemo::{fleet_from_json_bytes as $from_json, fleet_load as $load, fleet_save_checked as $save, fleet_to_json_checked as $to_json};
                Some($body)
            }
            ("mapdemo", "Pairs") => {
                #[allow(unused_imports)]
                use mapdemo::{pairs_from_json_bytes as $from_json, pairs_load as $load, pairs_save_checked as $save, pairs_to_json_checked as $to_json};
                Some($body)
            }
            ("mapdemo", "Slots") => {
                #[allow(unused_imports)]
                use mapdemo::{slots_from_json_bytes as $from_json, slots_load as $load, slots_save_checked as $save, slots_to_json_checked as $to_json};
                Some($body)
            }
            ("mapdemo", "Spans") => {
                #[allow(unused_imports)]
                use mapdemo::{spans_from_json_bytes as $from_json, spans_load as $load, spans_save_checked as $save, spans_to_json_checked as $to_json};
                Some($body)
            }
            ("mapdemo", "Trails") => {
                #[allow(unused_imports)]
                use mapdemo::{trails_from_json_bytes as $from_json, trails_load as $load, trails_save_checked as $save, trails_to_json_checked as $to_json};
                Some($body)
            }
            ("mapdemo", "Runs") => {
                #[allow(unused_imports)]
                use mapdemo::{runs_from_json_bytes as $from_json, runs_load as $load, runs_save_checked as $save, runs_to_json_checked as $to_json};
                Some($body)
            }
            ("tabledemo", "RootConfig") => {
                #[allow(unused_imports)]
                use tabledemo::{root_config_from_json_bytes as $from_json, root_config_load as $load, root_config_save_checked as $save, root_config_to_json_checked as $to_json};
                Some($body)
            }
            ("tabledemo", "PackConfig") => {
                #[allow(unused_imports)]
                use tabledemo::{pack_config_from_json_bytes as $from_json, pack_config_load as $load, pack_config_save_checked as $save, pack_config_to_json_checked as $to_json};
                Some($body)
            }
            ("tabledemo", "LoadoutConfig") => {
                #[allow(unused_imports)]
                use tabledemo::{loadout_config_from_json_bytes as $from_json, loadout_config_load as $load, loadout_config_save_checked as $save, loadout_config_to_json_checked as $to_json};
                Some($body)
            }
            ("tabledemo", "ProfileConfig") => {
                #[allow(unused_imports)]
                use tabledemo::{profile_config_from_json_bytes as $from_json, profile_config_load as $load, profile_config_save_checked as $save, profile_config_to_json_checked as $to_json};
                Some($body)
            }
            ("tabledemo", "ArchiveConfig") => {
                #[allow(unused_imports)]
                use tabledemo::{archive_config_from_json_bytes as $from_json, archive_config_load as $load, archive_config_save_checked as $save, archive_config_to_json_checked as $to_json};
                Some($body)
            }
            ("tabledemo", "KeyedConfig") => {
                #[allow(unused_imports)]
                use tabledemo::{keyed_config_from_json_bytes as $from_json, keyed_config_load as $load, keyed_config_save_checked as $save, keyed_config_to_json_checked as $to_json};
                Some($body)
            }
            ("tabledemo", "WideBlob") => {
                #[allow(unused_imports)]
                use tabledemo::{wide_blob_from_json_bytes as $from_json, wide_blob_load as $load, wide_blob_save_checked as $save, wide_blob_to_json_checked as $to_json};
                Some($body)
            }
            ("tblg1", "Guarded") => {
                #[allow(unused_imports)]
                use tblg1::{guarded_from_json_bytes as $from_json, guarded_load as $load, guarded_save_checked as $save, guarded_to_json_checked as $to_json};
                Some($body)
            }
            ("streamdemo", "Feed") => {
                #[allow(unused_imports)]
                use streamdemo::{feed_from_json_bytes as $from_json, feed_load as $load, feed_save_checked as $save, feed_to_json_checked as $to_json};
                Some($body)
            }
            ("listdemo", "Album") => {
                #[allow(unused_imports)]
                use listdemo::{album_from_json_bytes as $from_json, album_load as $load, album_save_checked as $save, album_to_json_checked as $to_json};
                Some($body)
            }
            ("listdemo", "Army") => {
                #[allow(unused_imports)]
                use listdemo::{army_from_json_bytes as $from_json, army_load as $load, army_save_checked as $save, army_to_json_checked as $to_json};
                Some($body)
            }
            ("listdemo", "Mixed") => {
                #[allow(unused_imports)]
                use listdemo::{mixed_from_json_bytes as $from_json, mixed_load as $load, mixed_save_checked as $save, mixed_to_json_checked as $to_json};
                Some($body)
            }
            ("listdemo", "Save") => {
                #[allow(unused_imports)]
                use listdemo::{save_from_json_bytes as $from_json, save_load as $load, save_save_checked as $save, save_to_json_checked as $to_json};
                Some($body)
            }
            ("listdemo", "Sheet") => {
                #[allow(unused_imports)]
                use listdemo::{sheet_from_json_bytes as $from_json, sheet_load as $load, sheet_save_checked as $save, sheet_to_json_checked as $to_json};
                Some($body)
            }
            ("listdemo", "Unbounded") => {
                #[allow(unused_imports)]
                use listdemo::{unbounded_from_json_bytes as $from_json, unbounded_load as $load, unbounded_save_checked as $save, unbounded_to_json_checked as $to_json};
                Some($body)
            }
            ("messagedemo", "ToolMessage") => {
                #[allow(unused_imports)]
                use messagedemo::{tool_message_from_json_bytes as $from_json, tool_message_load as $load, tool_message_save_checked as $save, tool_message_to_json_checked as $to_json};
                Some($body)
            }
            ("backenddemo", "LoginRequest") => {
                #[allow(unused_imports)]
                use backenddemo::{login_request_from_json_bytes as $from_json, login_request_load as $load, login_request_save_checked as $save, login_request_to_json_checked as $to_json};
                Some($body)
            }
            ("backenddemo", "MatchResult") => {
                #[allow(unused_imports)]
                use backenddemo::{match_result_from_json_bytes as $from_json, match_result_load as $load, match_result_save_checked as $save, match_result_to_json_checked as $to_json};
                Some($body)
            }
            ("backenddemo", "StorePurchase") => {
                #[allow(unused_imports)]
                use backenddemo::{store_purchase_from_json_bytes as $from_json, store_purchase_load as $load, store_purchase_save_checked as $save, store_purchase_to_json_checked as $to_json};
                Some($body)
            }
            ("vocabdemo", "Wide00") => {
                #[allow(unused_imports)]
                use vocabdemo::{wide00_from_json_bytes as $from_json, wide00_load as $load, wide00_save_checked as $save, wide00_to_json_checked as $to_json};
                Some($body)
            }
            ("vocabdemo", "Wide09") => {
                #[allow(unused_imports)]
                use vocabdemo::{wide09_from_json_bytes as $from_json, wide09_load as $load, wide09_save_checked as $save, wide09_to_json_checked as $to_json};
                Some($body)
            }
            ("vocab9demo", "Wide00") => {
                #[allow(unused_imports)]
                use vocab9demo::{wide00_from_json_bytes as $from_json, wide00_load as $load, wide00_save_checked as $save, wide00_to_json_checked as $to_json};
                Some($body)
            }
            ("vocab9demo", "Wide19") => {
                #[allow(unused_imports)]
                use vocab9demo::{wide19_from_json_bytes as $from_json, wide19_load as $load, wide19_save_checked as $save, wide19_to_json_checked as $to_json};
                Some($body)
            }
            ("tblv1", "Cfg") => {
                #[allow(unused_imports)]
                use tblv1::{cfg_from_json_bytes as $from_json, cfg_load as $load, cfg_save_checked as $save, cfg_to_json_checked as $to_json};
                Some($body)
            }
            ("tblv2", "Cfg") => {
                #[allow(unused_imports)]
                use tblv2::{cfg_from_json_bytes as $from_json, cfg_load as $load, cfg_save_checked as $save, cfg_to_json_checked as $to_json};
                Some($body)
            }
            ("tblp1", "Chain") => {
                #[allow(unused_imports)]
                use tblp1::{chain_from_json_bytes as $from_json, chain_load as $load, chain_save_checked as $save, chain_to_json_checked as $to_json};
                Some($body)
            }
            ("tblp2", "Chain") => {
                #[allow(unused_imports)]
                use tblp2::{chain_from_json_bytes as $from_json, chain_load as $load, chain_save_checked as $save, chain_to_json_checked as $to_json};
                Some($body)
            }
            ("tblp3", "Chain") => {
                #[allow(unused_imports)]
                use tblp3::{chain_from_json_bytes as $from_json, chain_load as $load, chain_save_checked as $save, chain_to_json_checked as $to_json};
                Some($body)
            }
            ("tblm1", "Msg") => {
                #[allow(unused_imports)]
                use tblm1::{msg_from_json_bytes as $from_json, msg_load as $load, msg_save_checked as $save, msg_to_json_checked as $to_json};
                Some($body)
            }
            ("tblm2", "Msg") => {
                #[allow(unused_imports)]
                use tblm2::{msg_from_json_bytes as $from_json, msg_load as $load, msg_save_checked as $save, msg_to_json_checked as $to_json};
                Some($body)
            }
            ("tbla2", "Root") => {
                #[allow(unused_imports)]
                use tbla2::{root_from_json_bytes as $from_json, root_load as $load, root_save_checked as $save, root_to_json_checked as $to_json};
                Some($body)
            }
            ("tblk1", "Root") => {
                #[allow(unused_imports)]
                use tblk1::{root_from_json_bytes as $from_json, root_load as $load, root_save_checked as $save, root_to_json_checked as $to_json};
                Some($body)
            }
            ("tblk2", "Root") => {
                #[allow(unused_imports)]
                use tblk2::{root_from_json_bytes as $from_json, root_load as $load, root_save_checked as $save, root_to_json_checked as $to_json};
                Some($body)
            }
            ("tblw1", "Fleet") => {
                #[allow(unused_imports)]
                use tblw1::{fleet_from_json_bytes as $from_json, fleet_load as $load, fleet_save_checked as $save, fleet_to_json_checked as $to_json};
                Some($body)
            }
            ("tblw2", "Fleet") => {
                #[allow(unused_imports)]
                use tblw2::{fleet_from_json_bytes as $from_json, fleet_load as $load, fleet_save_checked as $save, fleet_to_json_checked as $to_json};
                Some($body)
            }
            ("tblr1", "Cfg") => {
                #[allow(unused_imports)]
                use tblr1::{cfg_from_json_bytes as $from_json, cfg_load as $load, cfg_save_checked as $save, cfg_to_json_checked as $to_json};
                Some($body)
            }
            ("tblr2", "Cfg") => {
                #[allow(unused_imports)]
                use tblr2::{cfg_from_json_bytes as $from_json, cfg_load as $load, cfg_save_checked as $save, cfg_to_json_checked as $to_json};
                Some($body)
            }
            // wstring(N) ON THE TABLE WIRE (docs/SPEC-TABLES.md §3 kind 33,
            // SPEC.md §4.12): widedemo's fixed roots ride the variable form
            // (§2.2), so Caption carries the kind at every site the wire has
            // (its own field, a `type` it reaches, an element and a union arm)
            // and Stamp beside it; map_text is the kind as a MAP VALUE (§2.8)
            ("widedemo", "Caption") => {
                #[allow(unused_imports)]
                use widedemo::{caption_from_json_bytes as $from_json, caption_load as $load, caption_save_checked as $save, caption_to_json_checked as $to_json};
                Some($body)
            }
            ("widedemo", "Stamp") => {
                #[allow(unused_imports)]
                use widedemo::{stamp_from_json_bytes as $from_json, stamp_load as $load, stamp_save_checked as $save, stamp_to_json_checked as $to_json};
                Some($body)
            }
            ("mapdemo", "Text") => {
                #[allow(unused_imports)]
                use mapdemo::{text_from_json_bytes as $from_json, text_load as $load, text_save_checked as $save, text_to_json_checked as $to_json};
                Some($body)
            }
            _ => None,
        }
    }};
}

fn spill_absent(dir: &str, name: &str) {
    spill(dir, &format!("{name}.absent"), b"");
}

fn no_text(f: &[String]) -> bool {
    f.len() > 5 && f[5] == "no-text"
}

fn surface_wire(manifest: &Manifest, out: &str) {
    for f in manifest.of_kind("instance") {
        let data = slurp(&f[4]);
        let answer = with_root!(f[2].as_str(), f[3].as_str(), |load, save, _to, _from| {
            let mut report = Default::default();
            match load(&data, &mut report) {
                // the writer's NAMED refusal (docs/SPEC-TABLES.md §2.5, §3): a
                // blob two slots name, and nothing else, answers absent by name
                Some(v) => save(&v),
                None => fail(&format!("{}: load refused {}", f[1], f[4])),
            }
        });
        match answer {
            Some(Ok(bytes)) => spill(out, &f[1], &bytes),
            Some(Err(why)) => spill(out, &format!("{}.absent", f[1]), why.as_bytes()),
            None => spill_absent(out, &f[1]),
        }
    }
}

fn surface_report(manifest: &Manifest, out: &str) {
    for f in manifest.of_kind("report") {
        let data = slurp(&f[4]);
        let answer = with_root!(f[2].as_str(), f[3].as_str(), |load, _save, _to, _from| {
            let mut report = Default::default();
            let ok = load(&data, &mut report).is_some();
            let malformed = report.malformed || (!ok && !report.refused);
            let verdict = if report.refused { "refused" } else { "read" };
            format!(
                "{},{},{},{},{},{},{}\n",
                report.unknown, report.kind_mismatch, report.widened, report.clamped, report.duplicate, malformed, verdict
            )
        });
        match answer {
            Some(text) => spill(out, &f[1], text.as_bytes()),
            None => spill_absent(out, &f[1]),
        }
    }
}

// The file-form retention rows exercise §6.6 with caller-owned capacities.
// A short store is measured from the full retaining read, then reduced by one
// byte so the loss is caused by capacity rather than a different input wire.
fn surface_retain(manifest: &Manifest, out: &str, saves: bool) {
    for f in manifest.of_kind("retain") {
        if saves && f.last().is_some_and(|v| v == "-") {
            continue;
        }
        if f[2] != "tblrt1" || f[3] != "Node" {
            spill_absent(out, &f[1]);
            continue;
        }
        use tblrt1::{TableRetain, TableReport, node_load_retain, node_save_retain};
        let wire = slurp(&f[4]);
        let id_capacity = if f[6] == "full" { 1 << 17 } else {
            f[6].parse::<usize>().unwrap_or_else(|_| fail(&format!("{}: invalid id capacity", f[1])))
        };
        let mut capacity = 1 << 20;
        if f[5] == "short" {
            let mut probe = TableRetain { bytes: Vec::with_capacity(capacity), ids: Vec::with_capacity(id_capacity) };
            let mut report = TableReport::default();
            let _ = node_load_retain(&wire, &mut probe, &mut report)
                .unwrap_or_else(|| fail(&format!("{}: capacity probe refused", f[1])));
            capacity = probe.bytes.len().saturating_sub(1);
        }
        let mut store = TableRetain { bytes: Vec::with_capacity(capacity), ids: Vec::with_capacity(id_capacity) };
        let mut report = TableReport::default();
        let value = node_load_retain(&wire, &mut store, &mut report)
            .unwrap_or_else(|| fail(&format!("{}: retaining load refused", f[1])));
        if report.malformed { fail(&format!("{}: retaining load malformed", f[1])); }
        let retained = report.retained;
        let lost = report.retain_lost;
        let unknown = report.unknown;
        let saved = node_save_retain(&value, &mut store, &mut report);
        let save_lost = report.retain_lost - lost;
        let mut second = TableReport::default();
        let again = node_save_retain(&value, &mut store, &mut second);
        if again != saved || second.retain_lost != save_lost {
            fail(&format!("{}: retaining save is not repeatable", f[1]));
        }
        if saves {
            spill(out, &f[1], &saved);
        } else {
            spill(out, &f[1], format!("{retained},{lost},{unknown} {save_lost}\n").as_bytes());
        }
    }
}

fn surface_json_read(manifest: &Manifest, out: &str) {
    for f in manifest.of_kind("instance") {
        if no_text(f) {
            continue;
        }
        let path = format!("testdata/conformance/tables/json/{}.json", f[1]);
        let answer = with_root!(f[2].as_str(), f[3].as_str(), |_load, save, _to, from_json| {
            let text = match fs::read_to_string(&path) {
                Ok(t) => t,
                Err(e) => fail(&format!("cannot read {path}: {e}")),
            };
            let mut report = Default::default();
            match from_json(text.as_bytes(), &mut report) {
                Some(v) => save(&v).unwrap_or_else(|why| fail(&format!("{}: the save refused the value: {why}", f[1]))),
                None => fail(&format!("{}: FromJson refused the text", f[1])),
            }
        });
        match answer {
            Some(bytes) => spill(out, &f[1], &bytes),
            None => spill_absent(out, &f[1]),
        }
    }
}

fn surface_json_write(manifest: &Manifest, out: &str) {
    for f in manifest.of_kind("instance") {
        if no_text(f) {
            continue;
        }
        let data = slurp(&f[4]);
        let answer = with_root!(f[2].as_str(), f[3].as_str(), |load, _save, to_json, _from| {
            let mut report = Default::default();
            match load(&data, &mut report) {
                Some(v) => match to_json(&v) {
                    Some(text) => text,
                    None => fail(&format!("{}: ToJson refused the value", f[1])),
                },
                None => fail(&format!("{}: load refused {}", f[1], f[4])),
            }
        });
        let name = format!("{}.json", f[1]);
        match answer {
            Some(text) => spill(out, &name, text.as_bytes()),
            None => spill_absent(out, &name),
        }
    }
}

// JSON-HOSTILE (docs/SPEC-TABLES.md §16.2, §16.3; FORMAT.md): one tree per
// rule the text form states, and the verdict is `refused` or the §4 report the
// read produces, `u,k,w,c,d,false,read`. The tree is what `schema pack` reads,
// so the text is <tree>/<root>.json (§17). A root this leg's text form does not
// carry answers ABSENT, one row at a time, exactly as json-read does.
fn surface_json_hostile(manifest: &Manifest, out: &str) {
    for f in manifest.of_kind("json-hostile") {
        let path = format!("{}/{}.json", f[4], f[3]);
        let answer = with_root!(f[2].as_str(), f[3].as_str(), |_load, _save, _to, from_json| {
            let text = match fs::read(&path) {
                Ok(t) => t,
                Err(e) => fail(&format!("cannot read {path}: {e}")),
            };
            // the RAW bytes: a lossy decode here would hide an ill-formed
            // byte from the reader that must refuse it (§16.3)
            let mut report = Default::default();
            let value = from_json(&text, &mut report);
            let r = &report;
            if value.is_none() || r.malformed || r.refused {
                "refused\n".to_string()
            } else {
                format!(
                    "{},{},{},{},{},false,read\n",
                    r.unknown, r.kind_mismatch, r.widened, r.clamped, r.duplicate
                )
            }
        });
        match answer {
            Some(verdict) => spill(out, &f[1], verdict.as_bytes()),
            None => spill_absent(out, &f[1]),
        }
    }
}

// COOK WRITE (docs/SPEC-TABLES.md §7.6): load the instance's wire with the
// root's form-1 reader, then cook the VALUE that load produced in each byte
// order with the root's generated <t>_cook_write — the region writer over the
// storage structs (internal/codegen/rusttable/cookwrite.go), which lays the
// numbering's nodes out by the compiler's C ABI model exactly as `schema cook`
// does. The expectation is the tool's two files. A root this leg's writer does
// not reach answers `<case>.absent` for both orders, counted as work left.
macro_rules! with_cook_root {
    ($unit:expr, $root:expr, |$load:ident, $cook:ident| $body:expr) => {{
        match ($unit, $root) {
            ("tabledemo", "RootConfig") => { use tabledemo::{root_config_load as $load, root_config_cook_write as $cook}; Some($body) }
            ("tabledemo", "ProfileConfig") => { use tabledemo::{profile_config_load as $load, profile_config_cook_write as $cook}; Some($body) }
            ("tabledemo", "LoadoutConfig") => { use tabledemo::{loadout_config_load as $load, loadout_config_cook_write as $cook}; Some($body) }
            ("tabledemo", "ArchiveConfig") => { use tabledemo::{archive_config_load as $load, archive_config_cook_write as $cook}; Some($body) }
            ("tabledemo", "KeyedConfig") => { use tabledemo::{keyed_config_load as $load, keyed_config_cook_write as $cook}; Some($body) }
            ("tabledemo", "WideBlob") => { use tabledemo::{wide_blob_load as $load, wide_blob_cook_write as $cook}; Some($body) }
            ("tblv1", "Cfg") => { use tblv1::{cfg_load as $load, cfg_cook_write as $cook}; Some($body) }
            ("tblv2", "Cfg") => { use tblv2::{cfg_load as $load, cfg_cook_write as $cook}; Some($body) }
            ("tblp1", "Chain") => { use tblp1::{chain_load as $load, chain_cook_write as $cook}; Some($body) }
            ("tblp3", "Chain") => { use tblp3::{chain_load as $load, chain_cook_write as $cook}; Some($body) }
            ("tblm1", "Msg") => { use tblm1::{msg_load as $load, msg_cook_write as $cook}; Some($body) }
            ("tblm2", "Msg") => { use tblm2::{msg_load as $load, msg_cook_write as $cook}; Some($body) }
            ("graphdemo", "Scene") => { use graphdemo::{scene_load as $load, scene_cook_write as $cook}; Some($body) }
            ("blobdemo", "Catalog") => { use blobdemo::{catalog_load as $load, catalog_cook_write as $cook}; Some($body) }
            ("streamdemo", "Feed") => { use streamdemo::{feed_load as $load, feed_cook_write as $cook}; Some($body) }
            ("messagedemo", "ToolMessage") => { use messagedemo::{tool_message_load as $load, tool_message_cook_write as $cook}; Some($body) }
            ("scalars", "SimState") => { use scalars::{sim_state_load as $load, sim_state_cook_write as $cook}; Some($body) }
            _ => None,
        }
    }};
}

fn surface_cook_write(manifest: &Manifest, out: &str) {
    for f in manifest.of_kind("cook-write") {
        let Some(inst) = manifest.of_kind("instance").find(|i| i[1] == f[1]) else {
            fail(&format!("cook-write {} names no instance", f[1]));
        };
        let data = slurp(&inst[4]);
        let answer = with_cook_root!(inst[2].as_str(), inst[3].as_str(), |load, cook| {
            let mut report = Default::default();
            let Some(v) = load(&data, &mut report) else {
                fail(&format!("{}: load refused {}", f[1], inst[4]));
            };
            let little = cook(&v, 1).unwrap_or_else(|why| fail(&format!("{}: the cook refused: {why}", f[1])));
            let big = cook(&v, 2).unwrap_or_else(|why| fail(&format!("{}: the big-endian cook refused: {why}", f[1])));
            (little, big)
        });
        match answer {
            Some((little, big)) => {
                spill(out, &f[1], &little);
                spill(out, &format!("{}-be", f[1]), &big);
            }
            None => {
                spill_absent(out, &f[1]);
                spill_absent(out, &format!("{}-be", f[1]));
            }
        }
    }
}

// ---------------------------------------------------------------------------
// the MESSAGE surface (docs/SPEC-TABLES.md §3.3, test/conformance/README.md)
// ---------------------------------------------------------------------------
//
// The one surface that reads two files: the CONNECTION's announcement into one
// direction's vocabulary, then the message-form batch against it. The verbs are
// PLURAL — LoadMessages, MeasureMessages and SaveMessages over the batch — and
// the bytes written are SaveMessages' own. A row whose root is a variable table
// carries no verbs ("`fixed table T` may ride here and a variable table may
// not", §3.3), so it is answered ABSENT with the reason, as the Go driver does.

macro_rules! message_round_trip {
    ($krate:ident, $row:ident, $load:ident, $measure:ident, $save:ident, $ann:expr, $wire:expr) => {{
        use $krate as k;
        let mut storage = vec![k::TableMessageEntry::default(); 4096];
        let mut vocabulary = k::TableVocabulary::new(&mut storage);
        let mut report = k::TableReport::default();
        if !k::announce_read(&mut vocabulary, $ann, &mut report) {
            None
        } else {
            let mut values = vec![k::$row::default(); k::TABLE_MESSAGE_BATCH_MAX];
            let (count, ok) = k::$load(&mut values, &vocabulary, $wire, &mut report);
            if !ok || count < 1 {
                None
            } else {
                let mut ignored = k::TableReport::default();
                let n = k::$measure(&values[..count], &mut ignored);
                let mut bytes = vec![0u8; n.max(0) as usize];
                if n < 0 || k::$save(&values[..count], &mut bytes, &mut ignored) != n {
                    None
                } else {
                    Some(bytes)
                }
            }
        }
    }};
}

// One message root's round trip, by its connection's unit and the row's root:
// None for a root this leg carries no message verbs for, Some(None) for a batch
// it refused.
fn message_codec(unit: &str, root: &str, ann: &[u8], wire: &[u8]) -> Option<Option<Vec<u8>>> {
    Some(match (unit, root) {
        ("backenddemo", "LoginRequest") => message_round_trip!(backenddemo, LoginRequestRow, login_request_load_messages, login_request_measure_messages, login_request_save_messages, ann, wire),
        ("backenddemo", "MatchResult") => message_round_trip!(backenddemo, MatchResultRow, match_result_load_messages, match_result_measure_messages, match_result_save_messages, ann, wire),
        ("backenddemo", "StorePurchase") => message_round_trip!(backenddemo, StorePurchaseRow, store_purchase_load_messages, store_purchase_measure_messages, store_purchase_save_messages, ann, wire),
        ("backenddemo", "Envelope") => message_round_trip!(backenddemo, EnvelopeRow, envelope_load_messages, envelope_measure_messages, envelope_save_messages, ann, wire),
        ("vocabdemo", "Wide00") => message_round_trip!(vocabdemo, Wide00Row, wide00_load_messages, wide00_measure_messages, wide00_save_messages, ann, wire),
        ("vocabdemo", "Wide09") => message_round_trip!(vocabdemo, Wide09Row, wide09_load_messages, wide09_measure_messages, wide09_save_messages, ann, wire),
        ("vocab9demo", "Wide00") => message_round_trip!(vocab9demo, Wide00Row, wide00_load_messages, wide00_measure_messages, wide00_save_messages, ann, wire),
        ("vocab9demo", "Wide19") => message_round_trip!(vocab9demo, Wide19Row, wide19_load_messages, wide19_measure_messages, wide19_save_messages, ann, wire),
        _ => return None,
    })
}

fn surface_message(manifest: &Manifest, out: &str) {
    for f in manifest.of_kind("message") {
        let Some(c) = manifest.of_kind("connection").find(|c| c[1] == f[2]) else {
            fail(&format!("message {} names no connection {}", f[1], f[2]));
        };
        if f[5] == "-" {
            // a named refusal row: the root is a variable table, and the
            // message form is fixed tables only (docs/SPEC-TABLES.md §3.3)
            spill(out, &format!("{}.absent", f[1]), "variable table: the message form is fixed tables only (SPEC-TABLES §3.3)".as_bytes());
            continue;
        }
        let ann = slurp(&c[4]);
        let wire = slurp(&f[5]);
        match message_codec(c[2].as_str(), f[3].as_str(), &ann, &wire) {
            Some(Some(bytes)) => spill(out, &f[1], &bytes),
            Some(None) => fail(&format!("{}: message load/save refused {}", f[1], f[5])),
            None => spill_absent(out, &f[1]),
        }
    }
}

// ---------------------------------------------------------------------------

fn main() {
    let args: Vec<String> = std::env::args().collect();
    if args.len() < 3 {
        eprintln!(
            "usage: {0} <manifest> list\n       {0} <manifest> <surface> <outdir>",
            args[0]
        );
        exit(2);
    }
    let manifest = Manifest::read(&args[1]);
    let surface = args[2].as_str();
    if surface == "list" {
        println!(
            // the WIRE-CARRYING surfaces ride the id-table form (docs/SPEC-TABLES.md
            // §3, §3.1, §16.7) for the roots this leg's variable wire carries —
            // graphdemo's Scene and blobdemo's Catalog — and answer every other
            // case ABSENT, one by one; the message surface rides the message form's
            // batch for the fixed roots of backenddemo, vocabdemo and vocab9demo
            // (§3.3); the retaining file rows read through tblrt1 (§6.6).
            "wire\nreport\nretain\nretain-save\njson-read\njson-write\njson-hostile\ncook\ncook-foreign\nblock\nblock-foreign\nblock-dump\nforgery\ncook-forgery\ncook-write\nmessage\nblock-reason\ncook-reason"
        );
        return;
    }
    if args.len() < 4 {
        eprintln!("usage: {} <manifest> <surface> <outdir>", args[0]);
        exit(2);
    }
    let out = args[3].as_str();
    match surface {
        "wire" => surface_wire(&manifest, out),
        "report" => surface_report(&manifest, out),
        "retain" => surface_retain(&manifest, out, false),
        "retain-save" => surface_retain(&manifest, out, true),
        "json-read" => surface_json_read(&manifest, out),
        "json-write" => surface_json_write(&manifest, out),
        "json-hostile" => surface_json_hostile(&manifest, out),
        "cook" => surface_cook(&manifest, out),
        "cook-write" => surface_cook_write(&manifest, out),
        "cook-foreign" => surface_cook_foreign(&manifest, out),
        "block-foreign" => surface_block_foreign(&manifest, out),
        "block-dump" => surface_block_dump(&manifest, out),
        "cook-forgery" => surface_cook_forgery(&manifest, out),
        "block" => surface_block(&manifest, out),
        "forgery" => surface_forgery(&manifest, out),
        "block-reason" => surface_reason(&manifest, out, "block"),
        "cook-reason" => surface_reason(&manifest, out, "cook"),
        "message" => surface_message(&manifest, out),
        _ => exit(2),
    }
}
