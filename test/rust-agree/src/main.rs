// THE RUST ACCESSOR/DESCRIPTOR AGREEMENT GATE (docs/PORTING.md J1; issue #421).
//
// Every record of the block and cook corpora is read TWICE, field by field, and
// the two must agree:
//
//   - THROUGH THE ACCESSOR: the generated #[repr(C)] row struct (RenderCameraRow,
//     ListNodeRow, ...), whose members the compiler places and whose reference
//     slots are dereferenced by the generated `<record>_at`;
//   - THROUGH THE DESCRIPTOR: TableBlockInfo / TableCookInfo, read here by
//     offset and width as raw bytes, and nothing the row struct owns.
//
// They are two derivations of one layout: the emitter writes the struct, and
// writes the descriptor from the layout model. A moved accessor or a moved
// descriptor offset is a DISAGREE line naming the record, the row and the field,
// with no pinned dump that happens to cover the field.
//
// Three passes, all counted:
//   block images  every row of block_render.bin and block_padded.bin, reached
//                 through each out-of-line array's triple read at the
//                 DESCRIPTOR's offsets and through the generated row slice
//   cook images   every node of the five cook images, reached through the
//                 reference slots (each deref also resolved here from the
//                 descriptor's own slot offset)
//   synthetic     every record type either descriptor graph reaches, read over a
//                 seeded pseudo-random buffer (all-bits patterns, NaNs), so a
//                 type with no row in an image is still held
//
// usage: rust-agree   (from the repository root)

use std::alloc::{Layout, alloc, dealloc};
use std::collections::HashSet;
use std::fs;
use std::process::exit;

// ---------------------------------------------------------------------------
// the two sides, in one vocabulary
// ---------------------------------------------------------------------------

// one field as a DESCRIPTOR states it (block and cook descriptors converted)
#[derive(Clone)]
struct Fd {
    name: &'static str,
    offset: i64,
    size: i64,
    count_offset: Option<i64>, // the count companion (a used length or a count)
    present_offset: Option<i64>, // the presence companion
    is_pointer: bool,          // an eight-byte signed self-relative delta slot
    is_array: bool,
    array_bound: i64,
    elem_size: i64,
    element: Option<&'static str>, // the record this field names
    // an out-of-line array's triple, read at the descriptor's own offsets:
    // (offset_of offset, count offset, stride offset, the pitch it states)
    triple: Option<(i64, i64, i64, i64)>,
}

struct Rd {
    name: &'static str,
    size: i64,
    fields: Vec<Fd>,
}

fn opt32(v: u32) -> Option<i64> {
    if v == u32::MAX { None } else { Some(v as i64) }
}

fn opt_neg(v: i32) -> Option<i64> {
    if v < 0 { None } else { Some(v as i64) }
}

macro_rules! rd_block {
    ($info:expr) => {{
        let i = $info;
        Rd {
            name: i.name,
            size: i.size as i64,
            fields: i
                .fields
                .iter()
                .map(|f| Fd {
                    name: f.name,
                    offset: f.offset as i64,
                    size: f.size as i64,
                    count_offset: if f.out_of_line {
                        None
                    } else {
                        opt32(f.count_offset)
                    },
                    present_offset: opt32(f.present_offset),
                    is_pointer: false,
                    is_array: f.is_array,
                    array_bound: f.array_bound as i64,
                    elem_size: f.elem_size as i64,
                    element: f.element.map(|e| e().name),
                    triple: if f.out_of_line {
                        Some((
                            f.offset_of_offset as i64,
                            f.count_offset as i64,
                            f.stride_offset as i64,
                            f.stride as i64,
                        ))
                    } else {
                        None
                    },
                })
                .collect(),
        }
    }};
}

macro_rules! rd_cook {
    ($info:expr) => {{
        let i = $info;
        Rd {
            name: i.name,
            size: i.size as i64,
            fields: i
                .fields
                .iter()
                .map(|f| Fd {
                    name: f.name,
                    offset: f.offset as i64,
                    size: f.size as i64,
                    count_offset: opt_neg(f.count_offset),
                    present_offset: opt_neg(f.present_offset),
                    is_pointer: f.is_pointer,
                    is_array: f.is_array,
                    array_bound: f.array_bound as i64,
                    elem_size: f.elem_size as i64,
                    element: f.record.map(|r| r().name),
                    triple: None,
                })
                .collect(),
        }
    }};
}

// one member as the ACCESSOR reads it: where the compiler put it, the bytes of
// the place, and (for a reference slot) where the generated `<record>_at` goes
struct Mem {
    name: &'static str,
    offset: i64,
    bytes: Vec<u8>,
    deref: Option<usize>,
}

fn bytes_of<T>(place: *const T) -> Vec<u8> {
    unsafe { std::slice::from_raw_parts(place as *const u8, std::mem::size_of::<T>()).to_vec() }
}

#[derive(Clone, Copy)]
struct Reg {
    name: &'static str,
    desc: fn() -> Rd,
    // the members of the generated row, with the reference slots dereferenced
    // only when the buffer is a real region (a noise buffer's deltas are not)
    access: fn(*const u8, bool) -> Vec<Mem>,
    // members the descriptor does not describe (a projection's prologue)
    silent: &'static [&'static str],
}

macro_rules! rec {
    ($name:literal, $Row:ty, $desc:expr, $silent:expr, [$($f:ident $(=> $at:path)?),* $(,)?]) => {
        Reg {
            name: $name,
            desc: $desc,
            silent: $silent,
            access: |p, deref| {
                let r = p as *const $Row;
                let mut v: Vec<Mem> = Vec::new();
                $(
                    unsafe {
                        let place = core::ptr::addr_of!((*r).$f);
                        #[allow(unused_mut)]
                        let mut m = Mem {
                            name: stringify!($f),
                            offset: place as usize as i64 - p as usize as i64,
                            bytes: bytes_of(place),
                            deref: None,
                        };
                        $( if deref { m.deref = Some($at(place as *const i64) as usize); } )?
                        v.push(m);
                    }
                )*
                let _ = deref;
                v
            },
        }
    };
}

fn registry() -> Vec<Reg> {
    use blockdemo::*;
    use graphdemo as g;
    const PROLOGUE: &[&str] = &["magic", "build_version", "byte_order"];
    const NONE: &[&str] = &[];
    vec![
        // ---- the block form: blockdemo's projections and rows ----
        rec!(
            "RenderFrame",
            RenderFrameBlockProjection,
            || rd_block!(RenderFrameBlock::type_info()),
            PROLOGUE,
            [
                magic,
                build_version,
                byte_order,
                version,
                cameras,
                ships,
                turrets,
                missiles,
                dynamic_props,
                static_props,
                cosmetic_props,
                lasers,
                explosions
            ]
        ),
        rec!(
            "PaddedFrame",
            PaddedFrameBlockProjection,
            || rd_block!(PaddedFrameBlock::type_info()),
            PROLOGUE,
            [
                magic,
                build_version,
                byte_order,
                marker,
                stamp,
                rows,
                blob,
                blob_length
            ]
        ),
        rec!(
            "RenderVector3",
            RenderVector3Row,
            || rd_block!(render_vector3_block_info()),
            NONE,
            [x, y, z]
        ),
        rec!(
            "RenderQuaternion",
            RenderQuaternionRow,
            || rd_block!(render_quaternion_block_info()),
            NONE,
            [x, y, z, w]
        ),
        rec!(
            "RenderCamera",
            RenderCameraRow,
            || rd_block!(render_camera_block_info()),
            NONE,
            [
                position,
                rotation,
                camera_id,
                camera_type,
                target_object_id,
                fov
            ]
        ),
        rec!(
            "RenderShip",
            RenderShipRow,
            || rd_block!(render_ship_block_info()),
            NONE,
            [
                position,
                rotation,
                flags,
                object_id,
                target_object_id,
                thrust,
                object_sequence,
                ship_type,
                team,
                has_target_lock,
                predicted_explode
            ]
        ),
        rec!(
            "RenderTurret",
            RenderTurretRow,
            || rd_block!(render_turret_block_info()),
            NONE,
            [
                rotation,
                flags,
                object_id,
                parent_object_id,
                turret_index,
                target_object_id,
                object_sequence,
                team,
                has_target_lock
            ]
        ),
        rec!(
            "RenderMissile",
            RenderMissileRow,
            || rd_block!(render_missile_block_info()),
            NONE,
            [
                position,
                rotation,
                flags,
                object_id,
                object_sequence,
                missile_type,
                team
            ]
        ),
        rec!(
            "RenderDynamicProp",
            RenderDynamicPropRow,
            || rd_block!(render_dynamic_prop_block_info()),
            NONE,
            [
                position,
                rotation,
                flags,
                object_id,
                object_sequence,
                prop_type,
                team
            ]
        ),
        rec!(
            "RenderStaticProp",
            RenderStaticPropRow,
            || rd_block!(render_static_prop_block_info()),
            NONE,
            [
                position,
                rotation,
                scale,
                flags,
                static_prop_id,
                prop_type,
                team
            ]
        ),
        rec!(
            "RenderCosmeticProp",
            RenderCosmeticPropRow,
            || rd_block!(render_cosmetic_prop_block_info()),
            NONE,
            [
                position,
                rotation,
                scale,
                flags,
                cosmetic_prop_id,
                prop_sequence,
                prop_type,
                team
            ]
        ),
        rec!(
            "RenderLaser",
            RenderLaserRow,
            || rd_block!(render_laser_block_info()),
            NONE,
            [start, finish, t, laser_id, laser_type, team]
        ),
        rec!(
            "RenderExplosion",
            RenderExplosionRow,
            || rd_block!(render_explosion_block_info()),
            NONE,
            [
                position,
                rotation,
                t,
                explosion_id,
                parent_object_id,
                explosion_type,
                team
            ]
        ),
        rec!(
            "PaddedRow",
            PaddedRowRow,
            || rd_block!(padded_row_block_info()),
            NONE,
            [
                tag,
                value,
                flag,
                id,
                label,
                label_length,
                slots,
                teams,
                counter,
                counter_present
            ]
        ),
        // ---- the cook form: graphdemo's records and their reference slots ----
        rec!(
            "Meta",
            g::MetaRow,
            || rd_cook!(g::meta_cook_info()),
            NONE,
            [build, tag, tag_length]
        ),
        rec!(
            "Settings",
            g::SettingsRow,
            || rd_cook!(g::settings_cook_info()),
            NONE,
            [quality, label, label_length]
        ),
        rec!("ListNode", g::ListNodeRow, || rd_cook!(g::list_node_cook_info()), NONE,
            [value, name, name_length, next => g::list_node_at]),
        rec!("TreeNode", g::TreeNodeRow, || rd_cook!(g::tree_node_cook_info()), NONE,
            [label, label_length, left => g::tree_node_at, right => g::tree_node_at]),
        rec!("Layer", g::LayerRow, || rd_cook!(g::layer_cook_info()), NONE, [depth, head => g::list_node_at]),
        rec!("Scene", g::SceneRow, || rd_cook!(g::scene_cook_info()), NONE,
            [name, name_length, version, head => g::list_node_at, tree => g::tree_node_at,
             settings => g::settings_at, alias => g::list_node_at, ground, layers, layers_count, meta]),
        rec!("Depot", g::DepotRow, || rd_cook!(g::depot_cook_info()), NONE,
            [name, name_length, banks, spare, spare_present, head => g::list_node_at]),
        rec!("Album", g::AlbumRow, || rd_cook!(g::album_cook_info()), NONE,
            [name, name_length, tint, stamp, marker, pin => g::marker_at, head => g::list_node_at]),
        rec!(
            "Tally",
            g::TallyRow,
            || rd_cook!(g::tally_cook_info()),
            NONE,
            [hits]
        ),
        rec!("Marker", g::MarkerRow, || rd_cook!(g::marker_cook_info()), NONE,
            [label, label_length, note => g::tally_at]),
        rec!(
            "Stamp",
            g::StampRow,
            || rd_cook!(g::stamp_cook_info()),
            NONE,
            [tag, tag_length, seq]
        ),
        rec!(
            "Colour",
            g::ColourRow,
            || rd_cook!(g::colour_cook_info()),
            NONE,
            [r, g, b]
        ),
    ]
}

// ---------------------------------------------------------------------------
// the verdict
// ---------------------------------------------------------------------------

struct Cx {
    regs: Vec<Reg>,
    disagreements: u64,
    reads: u64,
    block_rows: u64,
    block_images: u64,
    cook_nodes: u64,
    cook_records: u64,
    synthetic_records: u64,
    // reference targets found while reading a cook, still to be read
    queue: Vec<(usize, &'static str)>,
}

impl Cx {
    fn fail(&mut self, tag: &str, what: &str) {
        self.disagreements += 1;
        println!("DISAGREE  {tag}: {what}");
    }

    fn reg(&self, name: &str) -> Option<Reg> {
        self.regs.iter().find(|r| r.name == name).copied()
    }
}

fn show(b: &[u8]) -> String {
    let head: Vec<String> = b.iter().take(8).map(|x| format!("{x:02x}")).collect();
    format!(
        "bytes[{}]{}{}",
        b.len(),
        head.join(""),
        if b.len() > 8 { ".." } else { "" }
    )
}

// read the record at p both ways. `p` is the record's base; every descriptor
// offset is relative to it, and the accessor's offsets are the compiler's.
fn agree(cx: &mut Cx, rname: &str, rd: &Rd, p: *const u8, at: &str, deref: bool) {
    cx.cook_records += 1;
    let reg = match cx.reg(rname) {
        Some(r) => r,
        None => {
            cx.fail(
                &format!("{} {at}", rd.name),
                "the descriptor graph names a record the accessor side has no read of",
            );
            return;
        }
    };
    let mems = (reg.access)(p, deref);
    let mut claimed: HashSet<&str> = HashSet::new();
    let raw = |offset: i64, len: i64| -> Option<Vec<u8>> {
        if offset < 0 || len < 0 || offset + len > rd.size {
            return None;
        }
        Some(unsafe { std::slice::from_raw_parts(p.add(offset as usize), len as usize).to_vec() })
    };

    for f in rd.fields.iter() {
        cx.reads += 1;
        let tag = format!("{} {at}.{}", rd.name, f.name);
        let m = match mems.iter().find(|m| m.name == f.name) {
            Some(m) => m,
            None => {
                cx.fail(
                    &tag,
                    "the descriptor names it and the accessor has no read of it",
                );
                continue;
            }
        };
        claimed.insert(m.name);
        if m.offset != f.offset {
            cx.fail(
                &tag,
                &format!("accessor at +{} != descriptor at +{}", m.offset, f.offset),
            );
        }
        let len = m.bytes.len() as i64;
        let companion = f.count_offset.is_some() || f.present_offset.is_some();
        if len > f.size || (!companion && len != f.size) {
            cx.fail(
                &tag,
                &format!("accessor width {len} != descriptor width {}", f.size),
            );
        }
        match raw(f.offset, len) {
            Some(via) => {
                if via != m.bytes {
                    cx.fail(
                        &tag,
                        &format!("accessor {} != descriptor {}", show(&m.bytes), show(&via)),
                    );
                }
            }
            None => cx.fail(
                &tag,
                "accessor reads it and the descriptor places it outside its record",
            ),
        }

        // the count companion and the presence companion
        for (suffixes, want) in [
            (&["_length", "_count"][..], f.count_offset),
            (&["_present"][..], f.present_offset),
        ] {
            let found = mems
                .iter()
                .find(|c| suffixes.iter().any(|s| c.name == format!("{}{s}", f.name)));
            match (found, want) {
                (Some(c), Some(w)) => {
                    claimed.insert(c.name);
                    cx.reads += 1;
                    let ctag = format!("{} {at}.{}", rd.name, c.name);
                    if c.offset != w {
                        cx.fail(
                            &ctag,
                            &format!("accessor at +{} != descriptor at +{w}", c.offset),
                        );
                    }
                    match raw(w, c.bytes.len() as i64) {
                        Some(via) if via == c.bytes => {}
                        Some(via) => cx.fail(
                            &ctag,
                            &format!("accessor {} != descriptor {}", show(&c.bytes), show(&via)),
                        ),
                        None => cx.fail(
                            &ctag,
                            "accessor reads it and the descriptor places it outside its record",
                        ),
                    }
                }
                (Some(c), None) => {
                    claimed.insert(c.name);
                    cx.fail(
                        &format!("{} {at}.{}", rd.name, c.name),
                        "the accessor reads a companion and the descriptor names none",
                    );
                }
                (None, Some(_)) => cx.fail(
                    &tag,
                    "the descriptor names a companion and the accessor has none",
                ),
                (None, None) => {}
            }
        }

        // an out-of-line array's triple: the descriptor must place all three of
        // its members inside the projection. What the instance holds in them is
        // data, and the block pass reads it at these same offsets.
        if let Some((o_off, c_off, s_off, _)) = f.triple {
            if raw(o_off, 8).is_none() || raw(c_off, 4).is_none() || raw(s_off, 4).is_none() {
                cx.fail(&tag, "the descriptor places the triple outside its record");
            }
            continue;
        }

        // a reference slot: the delta each side reads, and where it points
        if f.is_pointer && !f.is_array && deref {
            if let (Some(target), Some(delta)) = (m.deref, raw(f.offset, 8)) {
                let delta = i64::from_le_bytes(delta.try_into().unwrap());
                let via = if delta == 0 {
                    0
                } else {
                    (p as usize as i64 + f.offset + delta) as usize
                };
                let want = target;
                if via != want {
                    cx.fail(
                        &tag,
                        &format!("accessor deref {want:#x} != descriptor deref {via:#x}"),
                    );
                }
                if let Some(element) = f.element
                    && want != 0
                {
                    cx.queue.push((want, element));
                }
            }
            continue;
        }

        // a nested record, or an array of them: descend at the descriptor's
        // offset (the accessor's offset was held equal above)
        if let (Some(element), false) = (f.element, f.is_pointer) {
            let (count, stride) = if f.is_array {
                (f.array_bound, f.elem_size)
            } else {
                (1, 0)
            };
            match cx.reg(element) {
                Some(er) => {
                    let erd = (er.desc)();
                    for k in 0..count {
                        let at2 = if f.is_array { format!("{at}.{}[{k}]", f.name) } else { format!("{at}.{}", f.name) };
                        let base = f.offset + k * stride;
                        if base < 0 || base + erd.size > rd.size {
                            cx.fail(&tag, "the nested record leaves its parent");
                            break;
                        }
                        agree(cx, element, &erd, unsafe { p.add(base as usize) }, &at2, deref);
                    }
                }
                None => cx.fail(&tag, &format!("the descriptor names record {element} and the accessor side has no read of it")),
            }
        }
    }

    for m in mems.iter() {
        if !claimed.contains(m.name) && !reg.silent.contains(&m.name) {
            cx.fail(
                &format!("{} {at}.{}", rd.name, m.name),
                "the accessor reads it and the descriptor does not name it",
            );
        }
    }
}

// ---------------------------------------------------------------------------
// aligned storage and the seeded generator
// ---------------------------------------------------------------------------

struct Region {
    ptr: *mut u8,
    layout: Layout,
    len: usize,
}

impl Region {
    fn new(len: usize) -> Region {
        let layout = Layout::from_size_align(len.max(64), 64).unwrap();
        let ptr = unsafe { alloc(layout) };
        if ptr.is_null() {
            eprintln!("out of memory");
            exit(1);
        }
        unsafe { std::ptr::write_bytes(ptr, 0, layout.size()) };
        Region { ptr, layout, len }
    }

    fn from_file(path: &str) -> Region {
        let bytes = match fs::read(path) {
            Ok(b) => b,
            Err(e) => {
                eprintln!("FAILED: cannot read {path}: {e}");
                exit(1);
            }
        };
        let r = Region::new(bytes.len());
        unsafe { std::ptr::copy_nonoverlapping(bytes.as_ptr(), r.ptr, bytes.len()) };
        r
    }
}

impl Drop for Region {
    fn drop(&mut self) {
        unsafe { dealloc(self.ptr, self.layout) };
    }
}

struct Rng(u64);

impl Rng {
    fn next(&mut self) -> u64 {
        self.0 = self.0.wrapping_add(0x9e37_79b9_7f4a_7c15);
        let mut z = self.0;
        z = (z ^ (z >> 30)).wrapping_mul(0xbf58_476d_1ce4_e5b9);
        z = (z ^ (z >> 27)).wrapping_mul(0x94d0_49bb_1331_11eb);
        z ^ (z >> 31)
    }
}

// ---------------------------------------------------------------------------
// the block images
// ---------------------------------------------------------------------------

// the generated row slice for one array: (first row, rows, size of one row)
type RowSlice = Option<(usize, usize, usize)>;

struct BlockUnit {
    name: &'static str,
    file: &'static str,
    projection: &'static str,
    rows: fn(*const u8, i64, &str) -> RowSlice,
    opens: fn(*const u8, i64) -> bool,
}

macro_rules! block_unit {
    ($name:literal, $file:literal, $projection:literal, $Block:ty, [$(($arr:ident, $Row:ty)),* $(,)?]) => {
        BlockUnit {
            name: $name,
            file: $file,
            projection: $projection,
            opens: |base, bytes| unsafe { <$Block>::open(base, bytes).is_some() },
            rows: |base, bytes, array| unsafe {
                let b = <$Block>::open(base, bytes)?;
                match array {
                    $(stringify!($arr) => {
                        let s = b.$arr();
                        Some((s.as_ptr() as usize, s.len(), std::mem::size_of::<$Row>()))
                    })*
                    _ => None,
                }
            },
        }
    };
}

fn block_units() -> Vec<BlockUnit> {
    use blockdemo::*;
    vec![
        block_unit!(
            "render",
            "testdata/wire/tables/block_render.bin",
            "RenderFrame",
            RenderFrameBlock,
            [
                (cameras, RenderCameraRow),
                (ships, RenderShipRow),
                (turrets, RenderTurretRow),
                (missiles, RenderMissileRow),
                (dynamic_props, RenderDynamicPropRow),
                (static_props, RenderStaticPropRow),
                (cosmetic_props, RenderCosmeticPropRow),
                (lasers, RenderLaserRow),
                (explosions, RenderExplosionRow),
            ]
        ),
        block_unit!(
            "padded",
            "testdata/wire/tables/block_padded.bin",
            "PaddedFrame",
            PaddedFrameBlock,
            [(rows, PaddedRowRow),]
        ),
    ]
}

fn block_pass(cx: &mut Cx) {
    for unit in block_units() {
        let image = Region::from_file(unit.file);
        let (base, bytes) = (image.ptr as *const u8, image.len as i64);
        if !(unit.opens)(base, bytes) {
            cx.fail(unit.name, &format!("{} does not open", unit.file));
            continue;
        }
        cx.block_images += 1;
        let reg = cx
            .reg(unit.projection)
            .expect("the projection is registered");
        let rd = (reg.desc)();
        // the projection itself, then every out-of-line array's rows
        agree(
            cx,
            unit.projection,
            &rd,
            base,
            &format!("{}.projection", unit.name),
            false,
        );
        for f in rd.fields.iter() {
            let (o_off, c_off, s_off, stride) = match f.triple {
                Some(t) => t,
                None => continue,
            };
            let tag = format!("{} {}.{}", rd.name, unit.name, f.name);
            let read = |off: i64, len: usize| unsafe {
                std::slice::from_raw_parts(base.add(off as usize), len).to_vec()
            };
            let offset_of = u64::from_le_bytes(read(o_off, 8).try_into().unwrap());
            let count = u32::from_le_bytes(read(c_off, 4).try_into().unwrap()) as usize;
            let instance_stride = u32::from_le_bytes(read(s_off, 4).try_into().unwrap()) as i64;
            if instance_stride != stride {
                cx.fail(
                    &tag,
                    &format!(
                        "the instance's stride {instance_stride} != the descriptor's pitch {stride}"
                    ),
                );
            }
            let (first, rows, row_size) = match (unit.rows)(base, bytes, f.name) {
                Some(r) => r,
                None => {
                    cx.fail(&tag, "the descriptor names an out-of-line array and the accessor side has no rows of it");
                    continue;
                }
            };
            if first != base as usize + offset_of as usize {
                cx.fail(
                    &tag,
                    &format!(
                        "the row slice starts at +{} and the descriptor's triple says +{offset_of}",
                        first - base as usize
                    ),
                );
            }
            if rows != count {
                cx.fail(
                    &tag,
                    &format!(
                        "the row slice holds {rows} rows and the descriptor's triple says {count}"
                    ),
                );
            }
            if row_size as i64 != stride {
                cx.fail(
                    &tag,
                    &format!(
                        "the row type is {row_size} bytes and the descriptor's pitch is {stride}"
                    ),
                );
            }
            let element = match f.element {
                Some(e) => e,
                None => {
                    cx.fail(&tag, "the descriptor names no element record");
                    continue;
                }
            };
            let erg = match cx.reg(element) {
                Some(e) => e,
                None => {
                    cx.fail(&tag, &format!("no accessor read of {element}"));
                    continue;
                }
            };
            let erd = (erg.desc)();
            if erd.size != stride {
                cx.fail(
                    &tag,
                    &format!(
                        "the element descriptor is {} bytes and the pitch is {stride}",
                        erd.size
                    ),
                );
            }
            for i in 0..rows {
                cx.block_rows += 1;
                let row = (first + i * row_size) as *const u8;
                agree(
                    cx,
                    element,
                    &erd,
                    row,
                    &format!("{}.{}[{i}]", unit.name, f.name),
                    false,
                );
            }
        }
    }
}

// ---------------------------------------------------------------------------
// the cook images
// ---------------------------------------------------------------------------

struct CookUnit {
    root: &'static str,
    open: fn(*const u8, u64) -> Option<(*const u8, u64)>,
}

macro_rules! cook_unit {
    ($root:literal, $ty:ty) => {
        CookUnit {
            root: $root,
            open: |bytes, length| unsafe {
                <$ty>::open(bytes, length).map(|c| (c.region(), c.region_length()))
            },
        }
    };
}

fn cook_pass(cx: &mut Cx) {
    let units = [
        cook_unit!("Scene", graphdemo::SceneCook),
        cook_unit!("Depot", graphdemo::DepotCook),
        cook_unit!("Album", graphdemo::AlbumCook),
        cook_unit!("TreeNode", graphdemo::TreeNodeCook),
        cook_unit!("ListNode", graphdemo::ListNodeCook),
    ];
    for unit in units.iter() {
        let path = format!("build/cook-fuzz/{}.cook", unit.root);
        if !std::path::Path::new(&path).exists() {
            cx.fail(
                unit.root,
                &format!("{path} is missing (make build/cook-fuzz/.stamp)"),
            );
            continue;
        }
        let image = Region::from_file(&path);
        let (region, region_length) = match (unit.open)(image.ptr as *const u8, image.len as u64) {
            Some(r) => r,
            None => {
                cx.fail(unit.root, &format!("{path} does not open"));
                continue;
            }
        };
        let (lo, hi) = (region as usize, region as usize + region_length as usize);
        cx.queue.clear();
        cx.queue.push((lo, unit.root));
        let mut seen: HashSet<usize> = HashSet::new();
        while let Some((addr, name)) = cx.queue.pop() {
            if !seen.insert(addr) {
                continue;
            }
            let reg = match cx.reg(name) {
                Some(r) => r,
                None => {
                    cx.fail(unit.root, &format!("a reference names record {name} and the accessor side has no read of it"));
                    continue;
                }
            };
            let rd = (reg.desc)();
            if addr < lo || addr + rd.size as usize > hi {
                cx.fail(
                    &format!("{} @{:#x}", rd.name, addr),
                    "a reference leaves the region",
                );
                continue;
            }
            cx.cook_nodes += 1;
            agree(
                cx,
                name,
                &rd,
                addr as *const u8,
                &format!("{}@+{}", unit.root, addr - lo),
                true,
            );
        }
    }
}

// ---------------------------------------------------------------------------
// every record type, over seeded noise
// ---------------------------------------------------------------------------

fn synthetic_pass(cx: &mut Cx, seed: u64) {
    let mut rng = Rng(seed);
    for reg in cx.regs.clone() {
        let rd = (reg.desc)();
        for _ in 0..16 {
            let region = Region::new(rd.size as usize);
            for i in 0..rd.size as usize {
                unsafe { *region.ptr.add(i) = (rng.next() >> 24) as u8 };
            }
            cx.synthetic_records += 1;
            agree(cx, reg.name, &rd, region.ptr, "synthetic", false);
        }
    }
}

fn main() {
    let mut cx = Cx {
        regs: registry(),
        disagreements: 0,
        reads: 0,
        block_rows: 0,
        block_images: 0,
        cook_nodes: 0,
        cook_records: 0,
        synthetic_records: 0,
        queue: Vec::new(),
    };
    block_pass(&mut cx);
    cook_pass(&mut cx);
    synthetic_pass(&mut cx, 0x5eed_a9ee);

    println!(
        "rust accessor/descriptor agreement: {} field reads through both, {} block images ({} rows), {} cook nodes, {} synthetic records, {} disagreements",
        cx.reads,
        cx.block_images,
        cx.block_rows,
        cx.cook_nodes,
        cx.synthetic_records,
        cx.disagreements
    );
    if cx.block_images == 0
        || cx.block_rows == 0
        || cx.cook_nodes == 0
        || cx.synthetic_records == 0
        || cx.reads == 0
    {
        println!("FAILED: a pass read nothing");
        exit(1);
    }
    if cx.disagreements != 0 {
        exit(1);
    }
}
