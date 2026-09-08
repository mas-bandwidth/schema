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

fn open_block(name: &str, data: &[u8], extent: i64) -> bool {
    let storage = Aligned::new(data, extent);
    unsafe {
        if name.starts_with("block_render") {
            blockdemo::RenderFrameBlock::open(storage.base, storage.bytes).is_some()
        } else if name.starts_with("block_padded") {
            blockdemo::PaddedFrameBlock::open(storage.base, storage.bytes).is_some()
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
        let verdict = if open_block(&f[1], &data, -1) {
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
        let verdict = if open_block(&f[1], &data, -1) {
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
        // every block row is read out of an ALIGNED base; the pointer column
        // is the cook battery's, and it is read here rather than assumed
        if f[6] != "0" {
            fail(&format!("{}: a block forgery with pointer {}", f[1], f[6]));
        }
        let verdict = if open_block(&f[3], &data, extent) {
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
            // the five WIRE-CARRYING surfaces are ABSENT: this port writes the wire's
            // PREVIOUS form and the corpus is pinned in the id-table form
            // (docs/SPEC-TABLES.md §3). schema#518 is the port's row.
            "cook\ncook-foreign\nblock\nblock-foreign\nblock-dump\nforgery\ncook-forgery"
        );
        return;
    }
    if args.len() < 4 {
        eprintln!("usage: {} <manifest> <surface> <outdir>", args[0]);
        exit(2);
    }
    let out = args[3].as_str();
    match surface {
        "cook" => surface_cook(&manifest, out),
        "cook-foreign" => surface_cook_foreign(&manifest, out),
        "block-foreign" => surface_block_foreign(&manifest, out),
        "block-dump" => surface_block_dump(&manifest, out),
        "cook-forgery" => surface_cook_forgery(&manifest, out),
        "block" => surface_block(&manifest, out),
        "forgery" => surface_forgery(&manifest, out),
        _ => exit(2),
    }
}
