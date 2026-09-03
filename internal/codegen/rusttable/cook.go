// The COOKED FORM's read side for Rust (docs/SPEC-TABLES.md §7): the header
// match, the root where it lies, and the blittable records a region is laid
// out from.
//
// A cook is a LOAD-TRUSTED-DATA-FROM-TOOLS format and not a wire. Tooling
// writes a region for one BUILD VERSION and that build points at it: Open
// matches the header and returns the root where it lies. There is no walk, no
// fix-up and no allocation, which is what makes Open O(1) in the file's size.
package rusttable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// cookModule assembles <base>_cook.rs: the §7 read runtime (emitted once per
// unit, into the first file that declares a table), the <Name>Row records the
// region is laid out from, their layout contract as const asserts, and one
// <Name>Cook per table in the file.
func (g *gen) cookModule() []byte {
	roots := g.cookRoots()
	if len(roots) == 0 {
		return nil
	}
	// a file whose every record reaches a union by value has no cooked form at
	// all, so it gets no module rather than a module of comments
	any := false
	for _, st := range roots {
		if g.cookable(st.Name) {
			any = true
			break
		}
	}
	if !any {
		return nil
	}
	first := g.isCookHome()

	var b strings.Builder
	b.WriteString(g.banner)
	b.WriteString(header(g.file.Base, g.unit.Package, "the COOKED FORM (docs/SPEC-TABLES.md §7): the READ half"))
	b.WriteString(cookModuleBanner)
	b.WriteString("use crate::*;\n\n")
	if first {
		fmt.Fprintf(&b, cookRuntime, ir.BuildVersion(g.unit))
		b.WriteString(cookDescriptorRuntime)
	}

	g.body.Reset()
	g.emitCookRows(roots)
	g.emitCookLayout(roots, first)
	g.emitCookDescriptors(roots)
	for _, st := range roots {
		g.emitCookHandle(st)
	}
	b.WriteString(g.body.String())
	return []byte(b.String())
}

// cookRoots is every closure member declared in this file that a cooked
// region can hold: a root is ANY table, and a record is anything one reaches
// by value.
func (g *gen) cookRoots() []*ir.Struct {
	out := append([]*ir.Struct(nil), orderTables(g.file.Tables)...)
	for _, d := range g.file.Decls {
		if st, ok := d.(*ir.Struct); ok && g.closure[st.Name] {
			out = append(out, st)
		}
	}
	return out
}

// isCookHome reports whether this file is the one that carries the unit's
// shared cook runtime — the first file, by basename, that declares a table.
func (g *gen) isCookHome() bool {
	for _, f := range g.unit.Files {
		if len(f.Tables) > 0 || g.fileHasClosureType(f) {
			return f.Base == g.file.Base
		}
	}
	return false
}

func (g *gen) fileHasClosureType(f *ir.File) bool {
	for _, d := range f.Decls {
		if st, ok := d.(*ir.Struct); ok && g.closure[st.Name] {
			return true
		}
	}
	return false
}

const cookModuleBanner = `//
// A COOK IS TRUSTED INPUT, LOADED FROM DISK. Open's checks are IDENTITY checks
// — is this file for THIS build — and not a trust boundary: there is NO
// PER-NODE VALIDATION AT LOAD, ever. A file whose provenance you doubt is
// ` + "`schema cook-check`" + `'s business, run by a person, once, offline.
//
// THE MEMORY IS THE CONSUMER'S, and it must stay put and stay aligned for as
// long as the handle lives: an mmap, or a buffer the consumer owns. This side
// takes a pointer and a length and points — the block form's contract (§19.2),
// for the same reason. It is UNSAFE by nature, not by taste.
//
// Every size and offset below is the C ABI model #[repr(C)] commits to, and
// every one of them is asserted AT COMPILE TIME against the layout the
// compiler derived. A bool in a cooked record is ONE byte.

// The crate root is a generated glob index, so every module imports it the
// same way; a unit whose cooked records name nothing outside this file needs
// none of it, and reading the same header everywhere is worth the allow.
#![allow(unused_imports)]
#![allow(clippy::missing_safety_doc)]
#![allow(clippy::manual_range_contains)]

`

// cookRuntime is the §7 read runtime, emitted once per unit. %d is the unit's
// build version (§20).
const cookRuntime = `// ---- the cooked form's read runtime (docs/SPEC-TABLES.md §7) ----

// THE BUILD VERSION (docs/SPEC-TABLES.md §20): one digest over every fact the
// bytes this build produces depend on — the type wire's protocol id, every
// record's layout as the compiler's own C ABI model computes it, and the facts
// that decide what a load PUTS in those slots. It is the number a cook's
// header carries and the number Open compares, and the number a block's
// prologue carries and BlockOpen compares: a build version answers "which
// build?" and not "which form?", and what separates the two forms is their
// MAGIC.
pub const BUILD_VERSION: u64 = 0x%016x;

// The cook's MAGIC (§7.1), read BYTEWISE before anything else: it is what
// establishes the byte order every other header word is written in, and it is
// also what separates a COOK from a BLOCK — the two accelerators carry the
// same build version and different magics.
//
// The value is "SCHMCOOK" read as ASCII in the byte order a little-endian
// store produces, so a hex dump of a little-endian cook is legible.
pub const TABLE_COOK_MAGIC: u64 = 0x4b4f4f434d484353;

// THIS BUILD's byte order, as §7.1's order word records it.
pub const TABLE_COOK_BYTE_ORDER: u64 = if cfg!(target_endian = "little") { 1 } else { 2 };

// §7.1's header is 64 bytes of u64 words, and the DATA part begins at
// align_up(64, alignment) — DERIVED and not a header field, because a fact a
// reader computes is a fact two writers cannot disagree about.
pub const TABLE_COOK_HEADER_BYTES: u64 = 64;

// The ceiling on the header's alignment word: the same sixty-four a block's
// base takes (§19.1).
pub const TABLE_COOK_MAX_ALIGN: u64 = 64;

/// The cooked header read BYTEWISE.
///
/// # Safety
/// ` + "`p`" + ` must point at eight readable bytes.
#[inline]
pub unsafe fn table_cook_read64(p: *const u8) -> u64 {
    unsafe { core::ptr::read_unaligned(p as *const u64) }
}

// Open checks the header and POINTS, and this is the WHOLE check (§7): the
// magic read bytewise, the byte order it establishes, the build version, every
// RESERVED word zero, the region ALIGNMENT the header names, the two part
// lengths against the length the caller passed — a truncated file refuses —
// the ROOT's own storage inside the data part, and the alignment of the base.
// Nothing per node, ever: that is what makes this O(1) in the file's size.
//
// EVERY NUMBER BELOW COMES OUT OF THE FILE, so all of the arithmetic is
// UNSIGNED and each term is BOUNDED BEFORE IT IS ADDED.
///
/// # Safety
/// ` + "`bytes`" + ` must point at ` + "`length`" + ` readable bytes.
pub unsafe fn table_cook_open(
    bytes: *const u8,
    length: u64,
    root_size: u64,
    root_align: u64,
) -> Option<(*const u8, u64)> {
    unsafe {
        if bytes.is_null() || length < TABLE_COOK_HEADER_BYTES {
            return None;
        }
        // the MAGIC, bytewise and first: it is what establishes the byte order
        // every other header word is read in, so nothing else may be read
        // before it. A byte-reversed constant is a cook of the other order and
        // refuses here, which is why the order never reaches a fix-up pass.
        if table_cook_read64(bytes) != TABLE_COOK_MAGIC {
            return None;
        }
        if table_cook_read64(bytes.add(16)) != TABLE_COOK_BYTE_ORDER {
            return None;
        }
        if table_cook_read64(bytes.add(8)) != BUILD_VERSION {
            return None;
        }
        // the RESERVED words: a non-zero one means a writer used a form this
        // build does not understand, and Open refuses rather than ignoring it.
        if table_cook_read64(bytes.add(48)) != 0 {
            return None;
        }
        if table_cook_read64(bytes.add(56)) != 0 {
            return None;
        }
        let data_length = table_cook_read64(bytes.add(24));
        let attribution_length = table_cook_read64(bytes.add(32));
        let alignment = table_cook_read64(bytes.add(40));
        // THE ALIGNMENT WORD IS DATA, and it is the one header field the rest
        // of the check does arithmetic WITH rather than only compares against.
        if alignment < 8 || alignment > TABLE_COOK_MAX_ALIGN {
            return None;
        }
        if alignment & (alignment - 1) != 0 {
            return None; // a power of two
        }
        if alignment %% root_align != 0 {
            return None; // and an alignment THE ROOT CAN SIT AT
        }
        // The DATA part begins at align_up(64, alignment). It is DERIVED and
        // not a header field.
        let data_offset = (TABLE_COOK_HEADER_BYTES + alignment - 1) & !(alignment - 1);
        if length < data_offset {
            return None;
        }
        // the two part lengths against the length the caller passed: the whole
        // file is data_offset + data_length + attribution_length, and a length
        // that is not EXACTLY that refuses — truncation and trailing bytes are
        // one refusal, and both terms are subtracted rather than added so no
        // sum can carry.
        if data_length > length - data_offset {
            return None;
        }
        if attribution_length != length - data_offset - data_length {
            return None;
        }
        // the ROOT sits at the region's base, so the region has to hold it.
        if data_length < root_size {
            return None;
        }
        let base = bytes.add(data_offset as usize);
        // the alignment of the BASE.
        if (base as usize as u64) %% alignment != 0 {
            return None;
        }
        Some((base, data_length))
    }
}

`

// ---- the blittable records (docs/SPEC-TABLES.md §7.2, §19.3) ----

// cookable reports whether a record has a Rust cooked form. A UNION does not:
// Rust spells a union as a real enum (SPEC §6.1), which has no committed
// payload layout, and a #[repr(C)] union twin for the cooked family is a
// NAMED FOLLOW-ON rather than something to improvise inside a record. A
// record that reaches one by value has none either, transitively.
func (g *gen) cookable(name string) bool {
	seen := map[string]bool{}
	var walk func(n string) bool
	walk = func(n string) bool {
		if seen[n] {
			return true
		}
		seen[n] = true
		st := g.unit.Tables[n]
		if st == nil {
			st = g.unit.Structs[n]
		}
		if st == nil {
			return false
		}
		for _, f := range st.Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			switch ref := f.Type.Ref.(type) {
			case *ir.Union:
				return false
			case *ir.Struct:
				if !walk(ref.Name) {
					return false
				}
			}
		}
		return true
	}
	return walk(name)
}

// emitCookRows emits one #[repr(C)] <Name>Row per cookable record the file
// declares: the C ABI layout the compiler derived, field for field, with no
// generated padding — Rust's own #[repr(C)] inserts exactly the padding the
// model computes, and the asserts below say so at compile time.
func (g *gen) emitCookRows(members []*ir.Struct) {
	g.pf("// The BLITTABLE records a cooked region is laid out from: the C ABI\n")
	g.pf("// layout §20.3 commits the compiler to. #[repr(C)] IS that layout, so\n")
	g.pf("// there are no generated padding fields — what C# has to spell out, Rust\n")
	g.pf("// gets from the representation, and the const asserts below hold it.\n")
	g.pf("// `Row` is a CLAIMED suffix (docs/SPEC-TABLES.md §11), so no declaration\n")
	g.pf("// in the unit can take it.\n\n")
	for _, st := range members {
		if !g.cookable(st.Name) {
			g.pf("// %s has NO cooked record: its by-value closure reaches a union, and a\n", st.Name)
			g.pf("// Rust union is a real enum with no committed payload layout. The\n")
			g.pf("// #[repr(C)] union twin is a named follow-on (docs/SPEC-TABLES.md §15).\n\n")
			continue
		}
		g.emitCookRow(st)
	}
}

func (g *gen) emitCookRow(st *ir.Struct) {
	layout := ir.RecordLayout(g.unit, st)
	g.pf("// %s — a cooked record: %d bytes, aligned %d.\n", st.Name, layout.Size, layout.Align)
	g.pf("#[repr(C)]\n#[derive(Clone, Copy)]\npub struct %sRow {\n", st.Name)
	if len(st.Fields) == 0 {
		g.pf("    _empty: [u8; 0],\n")
	}
	for _, f := range st.Fields {
		g.emitCookRowField(f)
	}
	g.pf("}\n\n")
	g.pf("impl Default for %sRow {\n", st.Name)
	g.pf("    fn default() -> Self {\n")
	g.pf("        // a cooked record is plain bytes with no niche in it, so the zero\n")
	g.pf("        // form is a value of the type by construction\n")
	g.pf("        unsafe { core::mem::zeroed() }\n")
	g.pf("    }\n}\n\n")
}

func (g *gen) emitCookRowField(f *ir.Field) {
	switch {
	case f.Type.Pointer:
		g.pf("    pub %s: i64, // *%s: signed self-relative delta, zero is null (§6.3)\n", f.Name, f.Type.Name)
	case f.Type.Kind == ir.TString:
		g.pf("    pub %s: [u8; %d], // string(%s): buffer, used length beside it\n",
			f.Name, f.Type.Size+1, ir.RenderExpr(f.Type.SizeExpr))
		g.pf("    pub %s_length: i32,\n", f.Name)
	case f.Type.Kind == ir.TBytes:
		g.pf("    pub %s: [u8; %d], // bytes(%s): buffer, used length beside it\n",
			f.Name, f.Type.Size, ir.RenderExpr(f.Type.SizeExpr))
		g.pf("    pub %s_length: i32,\n", f.Name)
	case f.KeyEnum != "":
		g.pf("    pub %s: [%s; %d], // [%s]: one slot per named variant\n",
			f.Name, cookElemType(f), f.ArrayBound, f.KeyEnum)
	case f.Array == ir.ArrayFixed:
		g.pf("    pub %s: [%s; %d],\n", f.Name, cookElemType(f), f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		g.pf("    pub %s: [%s; %d],\n", f.Name, cookElemType(f), f.ArrayBound)
		g.pf("    pub %s_count: i32,\n", f.Name)
	default:
		g.pf("    pub %s: %s,\n", f.Name, cookElemType(f))
	}
	if f.Type.Optional {
		g.pf("    pub %s_present: bool, // ?%s: one byte, as it is in C++\n", f.Name, tableFieldTypeName(f))
	}
}

// cookElemType is one cooked SLOT's Rust type. An ENUM slot holds the ORDINAL
// at the enum's own derived storage width (docs/SPEC-TABLES.md §7.2), where the
// wire rides the variant-name hash — so the slot is the plain unsigned, not
// the wire newtype.
func cookElemType(f *ir.Field) string {
	switch f.Type.Kind {
	case ir.TBool:
		return "bool"
	case ir.TFloat32:
		return "f32"
	case ir.TFloat64:
		return "f64"
	case ir.TInt:
		if f.Type.Signed {
			return rustInt(f.Type.Width)
		}
		return rustUint(f.Type.Width)
	case ir.TBits:
		if f.Type.Width <= 32 {
			return "u32"
		}
		return "u64"
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			return rustUint(ir.StorageBitsFor(ref.Max))
		case *ir.Flags:
			return "u64"
		case *ir.Struct:
			return ref.Name + "Row"
		}
	}
	return "u8"
}

// emitCookLayout is THE LAYOUT CONTRACT (docs/SPEC-TABLES.md §20.3), as const
// asserts: a cooked region is laid out by the compiler's C ABI model, and a
// runtime that laid one of these records out differently would read a cook at
// the wrong offsets and never know. C++ says this with static_assert; Rust
// says it with a const block, which fails the BUILD rather than a test.
func (g *gen) emitCookLayout(members []*ir.Struct, first bool) {
	g.pf("// THE LAYOUT CONTRACT (docs/SPEC-TABLES.md §20.3), checked at COMPILE\n")
	g.pf("// TIME: every size and every offset below is the number the compiler\n")
	g.pf("// derived from the declaration, and this build agreeing with it is what\n")
	g.pf("// makes a cook's bytes readable here. Neither side's layout is inferred\n")
	g.pf("// from the other's.\n")
	if first {
		g.pf("const _: () = assert!(core::mem::size_of::<bool>() == 1, \"a bool in a cooked record is ONE byte\");\n")
	}
	for _, st := range members {
		if !g.cookable(st.Name) {
			continue
		}
		layout := ir.RecordLayout(g.unit, st)
		g.pf("const _: () = assert!(core::mem::size_of::<%sRow>() == %d, \"%sRow's size moved (docs/SPEC-TABLES.md §20.3)\");\n",
			st.Name, layout.Size, st.Name)
		g.pf("const _: () = assert!(core::mem::align_of::<%sRow>() == %d, \"%sRow's alignment moved (docs/SPEC-TABLES.md §20.3)\");\n",
			st.Name, layout.Align, st.Name)
		for _, fl := range layout.Fields {
			pieces := ir.BlockFieldPieceOffsets(g.unit, fl.Field, fl.Offset, false)
			for i, name := range cookRowMembers(fl.Field) {
				if i >= len(pieces) {
					break
				}
				g.pf("const _: () = assert!(core::mem::offset_of!(%sRow, %s) == %d, \"%sRow.%s moved (docs/SPEC-TABLES.md §20.3)\");\n",
					st.Name, name, pieces[i].Offset, st.Name, name)
			}
		}
	}
	g.pf("\n")
}

// cookRowMembers names one field's generated Row members, in the order
// ir.BlockFieldPieceOffsets returns their pieces.
func cookRowMembers(f *ir.Field) []string {
	var names []string
	names = append(names, f.Name)
	switch {
	case f.Type.Pointer:
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		names = append(names, f.Name+"_length")
	case f.Array == ir.ArrayCounted && f.KeyEnum == "":
		names = append(names, f.Name+"_count")
	}
	if f.Type.Optional {
		names = append(names, f.Name+"_present")
	}
	return names
}

// ---- the cook handles ----

func (g *gen) emitCookHandle(st *ir.Struct) {
	if !g.cookable(st.Name) {
		return
	}
	layout := ir.RecordLayout(g.unit, st)
	align := ir.RegionAlignOf(g.cookRegionAligns(st)...)
	g.pf("// %s's cook: a pointer and a length, and then the root where it lies.\n", st.Name)
	g.pf("// Opening one is a HEADER MATCH and no copy; a reference is one add\n")
	g.pf("// (docs/SPEC-TABLES.md §7). `Cook` is a CLAIMED suffix (§11).\n")
	g.pf("//\n")
	g.pf("// THE MEMORY IS THE CONSUMER'S. Nothing here allocates, nothing here\n")
	g.pf("// copies and nothing here pins: the region must stay put and stay aligned\n")
	g.pf("// for as long as this handle or anything reached through it is used.\n")
	g.pf("#[derive(Clone, Copy)]\npub struct %sCook {\n", st.Name)
	g.pf("    region: *const u8, // the DATA part's base: the root sits at offset zero\n")
	g.pf("    region_length: u64, // data_length, as the header framed it\n")
	g.pf("}\n\n")
	g.pf("impl %sCook {\n", st.Name)
	g.pf("    // §7.1's constants, so a consumer reading this file has the facts and\n")
	g.pf("    // not a description of them.\n")
	g.pf("    pub const REGION_ALIGNMENT: u64 = %d; // the greatest alignof in the region, floor eight\n", align)
	g.pf("    pub const ROOT_SIZE: u64 = %d;\n", layout.Size)
	g.pf("    pub const ROOT_ALIGN: u64 = %d;\n\n", layout.Align)
	g.pf("    /// Open checks the header and POINTS, and that is the WHOLE check.\n")
	g.pf("    ///\n    /// # Safety\n    /// `bytes` must point at `length` readable bytes that outlive the handle.\n")
	g.pf("    pub unsafe fn open(bytes: *const u8, length: u64) -> Option<%sCook> {\n", st.Name)
	g.pf("        unsafe {\n")
	g.pf("            let (region, region_length) =\n")
	g.pf("                table_cook_open(bytes, length, Self::ROOT_SIZE, Self::ROOT_ALIGN)?;\n")
	g.pf("            Some(%sCook {\n                region,\n                region_length,\n            })\n", st.Name)
	g.pf("        }\n    }\n\n")
	g.pf("    pub fn region(&self) -> *const u8 {\n        self.region\n    }\n\n")
	g.pf("    pub fn region_length(&self) -> u64 {\n        self.region_length\n    }\n\n")
	g.pf("    pub fn root(&self) -> *const %sRow {\n        self.region as *const %sRow\n    }\n", st.Name, st.Name)
	g.pf("}\n\n")
	g.pf("// %s dereferences one reference slot: NULL IN A REGION IS A DELTA OF ZERO\n", fn(st.Name, "at"))
	g.pf("// (docs/SPEC-TABLES.md §6.3), and every other delta is one add.\n")
	g.pf("///\n/// # Safety\n/// `slot` must point at a reference slot inside a region this build opened.\n")
	g.pf("pub unsafe fn %s(slot: *const i64) -> *const %sRow {\n", fn(st.Name, "at"), st.Name)
	g.pf("    unsafe {\n")
	g.pf("        let delta = core::ptr::read_unaligned(slot);\n")
	g.pf("        if delta == 0 {\n            return core::ptr::null();\n        }\n")
	g.pf("        (slot as *const u8).offset(delta as isize) as *const %sRow\n", st.Name)
	g.pf("    }\n}\n\n")
}

// cookRegionAligns is every alignment a region rooted at this record can
// hold: the root's own and every record reachable from it.
func (g *gen) cookRegionAligns(st *ir.Struct) []int64 {
	seen := map[string]bool{}
	var aligns []int64
	var walk func(name string)
	walk = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		member := g.unit.Tables[name]
		if member == nil {
			member = g.unit.Structs[name]
		}
		if member == nil {
			return
		}
		aligns = append(aligns, ir.RecordLayout(g.unit, member).Align)
		for _, f := range member.Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			if ref, ok := f.Type.Ref.(*ir.Struct); ok {
				walk(ref.Name)
			}
		}
	}
	walk(st.Name)
	return aligns
}

// ---- reflection over a cooked region (docs/SPEC-TABLES.md §7.5, §8) ----

// cookDescriptorRuntime is the descriptor family a cooked walk reads, emitted
// once per unit beside the read runtime. The TABLE-wire descriptors cannot
// serve here: the wire's storage struct and the cooked record are two Rust
// types (SPEC §6.1 spells string(N) as [u8; N] on the wire and the cooked
// model as char[N + 1]), and a VARIABLE unit has no wire surface at all.
const cookDescriptorRuntime = `// What a cooked SLOT holds, which is not always what the WIRE carries: an ENUM
// slot holds the ORDINAL at the enum's own derived storage width
// (docs/SPEC-TABLES.md §7.2), where the wire rides the variant-name hash. A
// walker reads a slot with the width elem_size gives and the signedness this
// names.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum TableCookStorage {
    Record,    // a nested record, or an array of them: descend through it
    Reference, // an eight-byte SIGNED SELF-RELATIVE delta slot (§6.3)
    Bool,
    Signed,
    Unsigned, // an unsigned integer, an enum ordinal, a bits(N), a flags mask
    Float,
    String, // [u8; N + 1] with an i32 used length beside it
    Bytes,  // [u8; N] with an i32 used length beside it
}

// One record's layout as DATA — the mechanism behind a reflective read of a
// cooked region, and what a gate walks a whole graph with. A field carries the
// facts a region actually has: where it sits, how big it is, whether it is a
// POINTER EDGE, the bound its COUNT COMPANION is checked against, and the
// record it names.
pub struct TableCookFieldInfo {
    pub name: &'static str,
    pub offset: i32,      // the field's offset in the record this describes
    pub size: i32,        // its whole storage there, companions included
    pub elem_size: i32,   // one element's size, for an array; the field's own otherwise
    pub is_array: bool,
    pub array_bound: i32, // the DECLARED bound: a counted array's N, a string's length
    pub is_pointer: bool, // an eight-byte SIGNED SELF-RELATIVE delta slot (§6.3)
    pub count_offset: i32,   // the count companion's offset, or -1
    pub present_offset: i32, // an optional's presence bool, or -1
    pub storage: TableCookStorage,
    // the record this field NAMES, behind a function so the table stays
    // constructible in any order. None when the field is a scalar. Following it
    // is how a walker DESCENDS — a pointer's target, a by-value nesting and an
    // array's element are all reached through this one column.
    pub record: Option<fn() -> &'static TableCookInfo>,
}

// One cooked record's layout as DATA.
pub struct TableCookInfo {
    pub name: &'static str,
    pub size: i32,
    pub align: i32,
    pub num_fields: i32,
    pub fields: &'static [TableCookFieldInfo],
}

`

// emitCookDescriptors emits one <name>_cook_info() per cookable record.
func (g *gen) emitCookDescriptors(members []*ir.Struct) {
	g.pf("// ---- the cooked records as DATA (docs/SPEC-TABLES.md §7.5) ----\n\n")
	for _, st := range members {
		if !g.cookable(st.Name) {
			continue
		}
		layout := ir.RecordLayout(g.unit, st)
		g.pf("pub fn %s() -> &'static TableCookInfo {\n", fn(st.Name, "cook_info"))
		g.pf("    static FIELDS: [TableCookFieldInfo; %d] = [\n", len(st.Fields))
		for _, fl := range layout.Fields {
			g.pf("        %s,\n", g.cookFieldRow(fl))
		}
		g.pf("    ];\n")
		g.pf("    static INFO: TableCookInfo = TableCookInfo {\n")
		g.pf("        name: %q,\n        size: %d,\n        align: %d,\n        num_fields: %d,\n        fields: &FIELDS,\n",
			st.Name, layout.Size, layout.Align, len(st.Fields))
		g.pf("    };\n    &INFO\n}\n\n")
	}
}

func (g *gen) cookFieldRow(fl ir.FieldLayout) string {
	f := fl.Field
	pieces := ir.BlockFieldPieceOffsets(g.unit, f, fl.Offset, false)
	countOffset, presentOffset := int64(-1), int64(-1)
	names := cookRowMembers(f)
	for i, name := range names {
		if i >= len(pieces) {
			break
		}
		if strings.HasSuffix(name, "_length") || strings.HasSuffix(name, "_count") {
			countOffset = pieces[i].Offset
		}
		if strings.HasSuffix(name, "_present") {
			presentOffset = pieces[i].Offset
		}
	}

	var storage string
	var elemSize int64
	record := "None"
	isArray := false
	arrayBound := int64(0)
	switch {
	case f.Type.Pointer:
		storage, elemSize = "TableCookStorage::Reference", 8
		record = fmt.Sprintf("Some(%s)", fn(f.Type.Name, "cook_info"))
	case f.Type.Kind == ir.TString:
		storage, elemSize, arrayBound = "TableCookStorage::String", f.Type.Size+1, f.Type.Size
	case f.Type.Kind == ir.TBytes:
		storage, elemSize, arrayBound, isArray = "TableCookStorage::Bytes", 1, f.Type.Size, true
	default:
		storage, elemSize = cookStorageOf(g.unit, f)
		if isStruct(f) {
			record = fmt.Sprintf("Some(%s)", fn(f.Type.Name, "cook_info"))
		}
		if f.KeyEnum != "" || f.Array != ir.ArrayNone {
			isArray = true
			arrayBound = f.ArrayBound
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "TableCookFieldInfo {\n")
	fmt.Fprintf(&b, "            name: %q,\n", f.Name)
	fmt.Fprintf(&b, "            offset: %d,\n", fl.Offset)
	fmt.Fprintf(&b, "            size: %d,\n", fl.Size)
	fmt.Fprintf(&b, "            elem_size: %d,\n", elemSize)
	fmt.Fprintf(&b, "            is_array: %v,\n", isArray)
	fmt.Fprintf(&b, "            array_bound: %d,\n", arrayBound)
	fmt.Fprintf(&b, "            is_pointer: %v,\n", f.Type.Pointer)
	fmt.Fprintf(&b, "            count_offset: %d,\n", countOffset)
	fmt.Fprintf(&b, "            present_offset: %d,\n", presentOffset)
	fmt.Fprintf(&b, "            storage: %s,\n", storage)
	fmt.Fprintf(&b, "            record: %s,\n", record)
	fmt.Fprintf(&b, "        }")
	return b.String()
}

// cookStorageOf classifies one slot and gives its width.
func cookStorageOf(u *ir.Unit, f *ir.Field) (string, int64) {
	switch f.Type.Kind {
	case ir.TBool:
		return "TableCookStorage::Bool", 1
	case ir.TFloat32:
		return "TableCookStorage::Float", 4
	case ir.TFloat64:
		return "TableCookStorage::Float", 8
	case ir.TInt:
		if f.Type.Signed {
			return "TableCookStorage::Signed", int64(f.Type.Width) / 8
		}
		return "TableCookStorage::Unsigned", int64(f.Type.Width) / 8
	case ir.TBits:
		if f.Type.Width <= 32 {
			return "TableCookStorage::Unsigned", 4
		}
		return "TableCookStorage::Unsigned", 8
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Enum:
			return "TableCookStorage::Unsigned", int64(ir.StorageBitsFor(ref.Max)) / 8
		case *ir.Flags:
			return "TableCookStorage::Unsigned", 8
		case *ir.Struct:
			return "TableCookStorage::Record", ir.RecordLayout(u, ref).Size
		}
	}
	return "TableCookStorage::Unsigned", 1
}
