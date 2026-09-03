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

use std::alloc::{alloc_zeroed, dealloc, Layout};
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
// the codec table: one row per (unit, root) the corpus names
// ---------------------------------------------------------------------------
//
// Each unit declares its OWN TableReport — the generated surface is one crate
// per unit — so the driver carries one report shape of its own and each row
// copies into it. Five counters is the whole of §4's report.

#[derive(Default, Clone, Copy)]
struct Report {
    unknown: i32,
    kind_mismatch: i32,
    clamped: i32,
    duplicate: i32,
    malformed: bool,
}

struct Codec {
    unit: &'static str,
    root: &'static str,
    // Load the wire bytes, then measure and save: the bytes a round trip
    // produces, or None when the load refused.
    wire: fn(&[u8], &mut Report) -> Option<Vec<u8>>,
    // FromJson the text, then measure and save.
    json_read: fn(&[u8], &mut Report) -> Option<Vec<u8>>,
    // Load the wire bytes, then ToJson.
    json_write: fn(&[u8], &mut Report) -> Option<Vec<u8>>,
}

// One row, spelled once. The crate, the storage type and the six generated
// entry points are all passed by path because Rust has no identifier
// concatenation in macro_rules — which is the honest spelling anyway: every
// name this driver calls is visible at the call site.
macro_rules! codec {
    ($unit:literal, $root:literal, $krate:ident, $ty:ty,
     $measure:path, $save:path, $load:path,
     $from_json:path, $to_json_measure:path, $to_json:path) => {
        Codec {
            unit: $unit,
            root: $root,
            wire: |bytes, out| {
                // boxed: a table's storage is bounded but not small — the wide
                // corpus's root is a quarter of a megabyte, and a driver that
                // put one on the stack would be testing the stack
                let mut value: Box<$ty> = Box::default();
                let mut report = $krate::TableReport::default();
                let ok = $load(&mut value, bytes, &mut report);
                copy_report!(report, out);
                if !ok {
                    return None;
                }
                write_measured(|| $measure(&value), |buffer| $save(&value, buffer))
            },
            json_read: |text, out| {
                let mut value: Box<$ty> = Box::default();
                let mut report = $krate::TableReport::default();
                let ok = $from_json(&mut value, text, &mut report);
                copy_report!(report, out);
                if !ok {
                    return None;
                }
                write_measured(|| $measure(&value), |buffer| $save(&value, buffer))
            },
            json_write: |bytes, out| {
                let mut value: Box<$ty> = Box::default();
                let mut report = $krate::TableReport::default();
                let ok = $load(&mut value, bytes, &mut report);
                copy_report!(report, out);
                if !ok {
                    return None;
                }
                write_measured(
                    || $to_json_measure(&value),
                    |buffer| $to_json(&value, buffer),
                )
            },
        }
    };
}

macro_rules! copy_report {
    ($from:expr, $to:expr) => {
        $to.unknown = $from.unknown;
        $to.kind_mismatch = $from.kind_mismatch;
        $to.clamped = $from.clamped;
        $to.duplicate = $from.duplicate;
        $to.malformed = $from.malformed;
    };
}

// write_measured is the measure/write symmetry, held: the buffer is exactly
// the measured size and the write must fill it exactly.
fn write_measured(measure: impl Fn() -> i64, write: impl Fn(&mut [u8]) -> i64) -> Option<Vec<u8>> {
    let size = measure();
    if size < 0 {
        return None;
    }
    let mut buffer = vec![0u8; size as usize];
    if write(&mut buffer) != size {
        return None;
    }
    Some(buffer)
}

fn codecs() -> Vec<Codec> {
    vec![
        codec!(
            "tabledemo", "RootConfig", tabledemo, tabledemo::RootConfig,
            tabledemo::root_config_measure, tabledemo::root_config_save, tabledemo::root_config_load,
            tabledemo::root_config_from_json, tabledemo::root_config_to_json_measure, tabledemo::root_config_to_json
        ),
        codec!(
            "tabledemo", "ProfileConfig", tabledemo, tabledemo::ProfileConfig,
            tabledemo::profile_config_measure, tabledemo::profile_config_save, tabledemo::profile_config_load,
            tabledemo::profile_config_from_json, tabledemo::profile_config_to_json_measure, tabledemo::profile_config_to_json
        ),
        codec!(
            "tabledemo", "LoadoutConfig", tabledemo, tabledemo::LoadoutConfig,
            tabledemo::loadout_config_measure, tabledemo::loadout_config_save, tabledemo::loadout_config_load,
            tabledemo::loadout_config_from_json, tabledemo::loadout_config_to_json_measure, tabledemo::loadout_config_to_json
        ),
        codec!(
            "tabledemo", "WideBlob", tabledemo, tabledemo::WideBlob,
            tabledemo::wide_blob_measure, tabledemo::wide_blob_save, tabledemo::wide_blob_load,
            tabledemo::wide_blob_from_json, tabledemo::wide_blob_to_json_measure, tabledemo::wide_blob_to_json
        ),
        codec!(
            "tabledemo", "ArchiveConfig", tabledemo, tabledemo::ArchiveConfig,
            tabledemo::archive_config_measure, tabledemo::archive_config_save, tabledemo::archive_config_load,
            tabledemo::archive_config_from_json, tabledemo::archive_config_to_json_measure, tabledemo::archive_config_to_json
        ),
        codec!(
            "tabledemo", "KeyedConfig", tabledemo, tabledemo::KeyedConfig,
            tabledemo::keyed_config_measure, tabledemo::keyed_config_save, tabledemo::keyed_config_load,
            tabledemo::keyed_config_from_json, tabledemo::keyed_config_to_json_measure, tabledemo::keyed_config_to_json
        ),
        codec!(
            "tblv1", "Cfg", tblv1, tblv1::Cfg,
            tblv1::cfg_measure, tblv1::cfg_save, tblv1::cfg_load,
            tblv1::cfg_from_json, tblv1::cfg_to_json_measure, tblv1::cfg_to_json
        ),
        codec!(
            "tblv2", "Cfg", tblv2, tblv2::Cfg,
            tblv2::cfg_measure, tblv2::cfg_save, tblv2::cfg_load,
            tblv2::cfg_from_json, tblv2::cfg_to_json_measure, tblv2::cfg_to_json
        ),
        codec!(
            "tblp1", "Chain", tblp1, tblp1::Chain,
            tblp1::chain_measure, tblp1::chain_save, tblp1::chain_load,
            tblp1::chain_from_json, tblp1::chain_to_json_measure, tblp1::chain_to_json
        ),
        codec!(
            "tblp3", "Chain", tblp3, tblp3::Chain,
            tblp3::chain_measure, tblp3::chain_save, tblp3::chain_load,
            tblp3::chain_from_json, tblp3::chain_to_json_measure, tblp3::chain_to_json
        ),
    ]
}

fn find_codec<'a>(rows: &'a [Codec], unit: &str, root: &str) -> &'a Codec {
    match rows.iter().find(|c| c.unit == unit && c.root == root) {
        Some(c) => c,
        None => fail(&format!("no codec for {unit}.{root}")),
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
        let mut bytes = if extent < 0 { data.len() as i64 } else { extent };
        if bytes < data.len() as i64 {
            bytes = data.len() as i64;
        }
        let layout = Layout::from_size_align(bytes.max(1) as usize, 64).unwrap();
        let base = unsafe { alloc_zeroed(layout) };
        if base.is_null() {
            fail("out of memory");
        }
        unsafe { std::ptr::copy_nonoverlapping(data.as_ptr(), base, data.len()) };
        Aligned {
            base,
            layout,
            bytes,
        }
    }
}

impl Drop for Aligned {
    fn drop(&mut self) {
        unsafe { dealloc(self.base, self.layout) };
    }
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

fn surface_wire(manifest: &Manifest, out: &str) {
    let rows = codecs();
    for f in manifest.of_kind("instance") {
        let codec = find_codec(&rows, &f[2], &f[3]);
        let wire = slurp(&f[4]);
        let mut report = Report::default();
        match (codec.wire)(&wire, &mut report) {
            Some(bytes) => spill(out, &f[1], &bytes),
            None => fail(&format!("{} did not round-trip", f[1])),
        }
    }
}

fn surface_report(manifest: &Manifest, out: &str) {
    let rows = codecs();
    for f in manifest.of_kind("report") {
        let codec = find_codec(&rows, &f[2], &f[3]);
        let wire = slurp(&f[4]);
        let mut report = Report::default();
        let ok = (codec.wire)(&wire, &mut report).is_some();
        let text = format!(
            "{},{},{},{},{}\n",
            report.unknown,
            report.kind_mismatch,
            report.clamped,
            report.duplicate,
            if report.malformed || !ok { "true" } else { "false" }
        );
        spill(out, &f[1], text.as_bytes());
    }
}

fn surface_json_read(manifest: &Manifest, out: &str) {
    let rows = codecs();
    for f in manifest.of_kind("instance") {
        let codec = find_codec(&rows, &f[2], &f[3]);
        let path = format!("testdata/conformance/tables/json/{}.json", f[1]);
        let text = slurp(&path);
        let mut report = Report::default();
        match (codec.json_read)(&text, &mut report) {
            Some(bytes) => spill(out, &f[1], &bytes),
            None => fail(&format!("{} did not read from its text", f[1])),
        }
    }
}

fn surface_json_write(manifest: &Manifest, out: &str) {
    let rows = codecs();
    for f in manifest.of_kind("instance") {
        let codec = find_codec(&rows, &f[2], &f[3]);
        let wire = slurp(&f[4]);
        let mut report = Report::default();
        match (codec.json_write)(&wire, &mut report) {
            Some(text) => spill(out, &format!("{}.json", f[1]), &text),
            None => fail(&format!("{} did not write its text", f[1])),
        }
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

fn surface_forgery(manifest: &Manifest, out: &str) {
    for f in manifest.of_kind("forgery") {
        if f[2] != "block" {
            continue; // the cook's battery is not data yet
        }
        let data = slurp(&f[4]);
        let extent: i64 = parse_int(&f[5]);
        let verdict = if open_block(&f[3], &data, extent) {
            "open\n"
        } else {
            "refuse\n"
        };
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
        let text = dump_cook(&f[1], &f[3]);
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
        println!("wire\nreport\njson-read\njson-write\ncook\nblock\nforgery");
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
        "json-read" => surface_json_read(&manifest, out),
        "json-write" => surface_json_write(&manifest, out),
        "cook" => surface_cook(&manifest, out),
        "block" => surface_block(&manifest, out),
        "forgery" => surface_forgery(&manifest, out),
        _ => exit(2),
    }
}
