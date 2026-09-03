// The BLOCK FORM for Rust (docs/SPEC-TABLES.md §19): the READ side.
//
// NOTHING DECLARES THIS FORM. Every fixed table has one, and it is emitted on
// the side, in <base>_block.rs, which a consumer calls only if it uses the
// form. The unit's <base>_table.rs carries not one symbol of it.
//
// A block is one flat extent: the table's own instance at the front — the
// PROJECTION, carrying per bounded array of structs where its rows start, how
// many there are and how far apart they sit — and then those rows, each array
// at a fixed pitch. This side reads those three facts and points.
package rusttable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// generateBlocks returns the unit's block modules: one shared runtime module
// and one <base>_block.rs per file that declares a block-form table.
func generateBlocks(u *ir.Unit, blocks *ir.BlockUnit, banner string) (map[string][]byte, error) {
	if len(blocks.Tables) == 0 {
		return nil, nil
	}
	out := map[string][]byte{}
	// the refusal banner rides on EVERY file a refused unit emits, so a
	// consumer meets it wherever they look (§11)
	out[BlockRuntimeModule+".rs"] = []byte(banner + header(runtimeSourceName(u), u.Package,
		"the BLOCK FORM's shared runtime (docs/SPEC-TABLES.md §19)") + blockRuntimeSource)

	byFile := map[string][]*ir.BlockLayout{}
	for _, bl := range blocks.Tables {
		base := u.DeclFile[bl.Table.Name]
		byFile[base] = append(byFile[base], bl)
	}
	for base, tables := range byFile {
		sort.Slice(tables, func(i, j int) bool { return tables[i].Table.Name < tables[j].Table.Name })
		b := &blockGen{unit: u, blocks: blocks}
		for _, bl := range tables {
			b.emitBlock(bl)
		}
		var text strings.Builder
		text.WriteString(banner)
		text.WriteString(header(base, u.Package, "the BLOCK FORM (docs/SPEC-TABLES.md §19)"))
		text.WriteString(blockModuleBanner)
		text.WriteString("use crate::*;\n\n")
		text.WriteString(b.body.String())
		out[strings.ToLower(base)+"_block.rs"] = []byte(text.String())
	}
	return out, nil
}

const blockModuleBanner = `//
// NOTHING DECLARES THIS FORM. Every fixed table has one, and it is emitted on
// the side: call into this module only if you use the block form. The unit's
// table module carries not one symbol of it.
//
// A block is one flat extent: the table's own instance at the front — the
// PROJECTION, carrying per bounded array of structs where its rows start, how
// many there are and how far apart they sit — and then those rows, each array
// at a fixed pitch. This side reads those three facts and points.

#![allow(clippy::missing_safety_doc)]
#![allow(clippy::manual_range_contains)]

`

const blockRuntimeSource = `//
// The block form's shared runtime, emitted once per unit.

// What a table knows about ONE of its out-of-line arrays: where the rows
// start, how many there are, and how far apart they sit. Sixteen bytes with no
// interior padding, sitting at the array field's own position in the
// projection (§2.7). A consumer reads all three FROM THE INSTANCE, never from
// its own constants — that is the difference between a generated pair of
// structs and an ABI (§19.2).
#[repr(C)]
#[derive(Clone, Copy, Default, PartialEq, Eq, Debug)]
pub struct TableBlockTriple {
    pub offset_of: u64, // block-relative: the block relocates by plain copy
    pub count: u32,     // rows the producer filled; rows past it are not part of the block
    pub stride: u32,    // the pitch the consumer indexes with, from the data
}

const _: () = assert!(
    core::mem::size_of::<TableBlockTriple>() == 16,
    "a triple is sixteen bytes with no interior padding (docs/SPEC-TABLES.md §2.7)"
);
const _: () = assert!(core::mem::offset_of!(TableBlockTriple, offset_of) == 0);
const _: () = assert!(core::mem::offset_of!(TableBlockTriple, count) == 8);
const _: () = assert!(core::mem::offset_of!(TableBlockTriple, stride) == 12);

// The block's magic, and the byte-order check with it (§19.1). It is stored in
// the producer's NATIVE order; a consumer that reads back the byte-swapped
// value has found a foreign byte order, and one that reads back anything else
// has not found a block at all.
pub const TABLE_BLOCK_MAGIC: u64 = 0x4b4c42414d484353;

// THIS BUILD's byte order, as the prologue carries it (docs/SPEC-TABLES.md
// §20.3). A block written by a build of the other order is REFUSED by Open: a
// big-endian fix-up path is a named obligation, not something a consumer
// improvises row by row.
pub const TABLE_BLOCK_BYTE_ORDER: u64 = if cfg!(target_endian = "little") { 1 } else { 2 };

/// The prologue read BYTEWISE: the magic is the one field read without
/// assuming the order the rest of the block is in.
///
/// # Safety
/// ` + "`p`" + ` must point at eight readable bytes.
#[inline]
pub unsafe fn table_block_read64(p: *const u8) -> u64 {
    unsafe { core::ptr::read_unaligned(p as *const u64) }
}

#[inline]
pub fn table_block_byteswap64(v: u64) -> u64 {
    v.swap_bytes()
}

// ---- reflection over a block (docs/SPEC-TABLES.md §8, §19.2) ----
//
// The descriptors are the mechanism, and they are what retires a hand-kept
// mirror: a consumer holding them reads the triples out of an instance and
// points at rows, with no hand-written struct per table and no knowledge of
// the spelling that produced any of it. They are constant data, so this costs
// a lookup, not a parse — and they are immutable, so any thread may read them.

pub struct TableBlockFieldInfo {
    pub name: &'static str,
    pub offset: u32,       // the field's offset in the record this describes
    pub size: u32,         // its size there
    pub kind: u8,          // the table-wire kind, as TableFieldInfo carries it
    pub out_of_line: bool, // an out-of-line array: the three members below are live
    pub offset_of_offset: u32, // the triple's offset_of member, or u32::MAX
    pub count_offset: u32,     // its count member, or u32::MAX
    pub stride_offset: u32,    // its stride member, or u32::MAX
    pub stride: u32,           // THIS BUILD's pitch, to assert against — never to index with (§19.2)
    // the ELEMENT's or the nested record's own layout, behind a function so the
    // whole table stays a constant. None when the field is a scalar. Following
    // it is how a walker DESCENDS: an out-of-line array's rows, and a nested
    // record's fields, are both reached through this one column.
    pub element: Option<fn() -> &'static TableBlockInfo>,
}

// One record's layout as DATA — the whole mechanism behind the block form's
// read side. A block-form table's own descriptor describes its PROJECTION; the
// element descriptor of each out-of-line array describes that array's ROW, and
// so on down.
pub struct TableBlockInfo {
    pub name: &'static str,
    pub build_version: u64, // the unit's (docs/SPEC-TABLES.md §20)
    pub size: u32,          // the record's own size: a projection's, or a row's
    pub align: u32,
    pub num_fields: i32,
    pub fields: &'static [TableBlockFieldInfo],
}
`

type blockGen struct {
	unit           *ir.Unit
	blocks         *ir.BlockUnit
	body           strings.Builder
	emittedRecords map[string]bool // one descriptor per record per module
}

func (b *blockGen) pf(format string, args ...any) {
	fmt.Fprintf(&b.body, format, args...)
}

func (b *blockGen) emitBlock(bl *ir.BlockLayout) {
	name := bl.Table.Name
	proj := bl.Projection

	b.pf("// ---- the block form of table %s (docs/SPEC-TABLES.md §19) ----\n\n", name)

	// the PROJECTION
	b.pf("// The PROJECTION: a record like any other, following the same C ABI rule\n")
	b.pf("// (§19.3) — its own offsets are part of the contract, not scaffolding\n")
	b.pf("// around it. It opens with the generated PROLOGUE of three u64s.\n")
	b.pf("// `BlockProjection` is a CLAIMED suffix (§11).\n")
	b.pf("#[repr(C)]\n#[derive(Clone, Copy)]\npub struct %sBlockProjection {\n", name)
	b.pf("    pub magic: u64,         // generated: identifies a schema block\n")
	b.pf("    pub build_version: u64, // generated: the unit's build version (§20)\n")
	b.pf("    pub byte_order: u64,    // generated: 1 little, 2 big — the producer stamps its own\n")
	for _, fl := range proj.Fields {
		b.emitProjectionField(bl, fl)
	}
	b.pf("}\n\n")

	b.pf("impl Default for %sBlockProjection {\n    fn default() -> Self {\n", name)
	b.pf("        // a projection is plain bytes with no niche in it\n")
	b.pf("        unsafe { core::mem::zeroed() }\n    }\n}\n\n")

	// THE LAYOUT CONTRACT
	b.pf("// THE LAYOUT CONTRACT (docs/SPEC-TABLES.md §19.3). The compiler derived\n")
	b.pf("// every number below from the declaration; these asserts are this build\n")
	b.pf("// saying whether it agrees. Neither side's layout is inferred from the\n")
	b.pf("// other's — both are checked against their own compiler's model.\n")
	b.pf("const _: () = assert!(core::mem::size_of::<%sBlockProjection>() == %d, \"%s's block projection size moved (§19.3)\");\n",
		name, proj.Size, name)
	b.pf("const _: () = assert!(core::mem::align_of::<%sBlockProjection>() == %d, \"%s's block projection alignment moved (§19.3)\");\n",
		name, proj.Align, name)
	b.pf("const _: () = assert!(core::mem::offset_of!(%sBlockProjection, magic) == 0, \"the block prologue's magic sits at 0 (§19.1)\");\n", name)
	b.pf("const _: () = assert!(core::mem::offset_of!(%sBlockProjection, build_version) == 8, \"the block prologue's build_version sits at 8 (§19.1, §20)\");\n", name)
	b.pf("const _: () = assert!(core::mem::offset_of!(%sBlockProjection, byte_order) == 16, \"the block prologue's byte_order sits at 16 (§19.1)\");\n", name)
	for _, fl := range proj.Fields {
		for i, member := range b.projectionMembers(bl, fl.Field) {
			pieces := ir.BlockFieldPieceOffsets(b.unit, fl.Field, fl.Offset, true)
			if i >= len(pieces) {
				break
			}
			b.pf("const _: () = assert!(core::mem::offset_of!(%sBlockProjection, %s) == %d, \"%s's projection field %s moved (§19.3)\");\n",
				name, member, pieces[i].Offset, name, member)
		}
	}
	b.pf("\n")

	// the constants
	b.pf("// The block's STORAGE is sized from the declared maxima: one extent,\n")
	b.pf("// allocated once, never grown, never pooled (§19.1). The sum is loose by\n")
	b.pf("// construction — arrays commonly draw from one shared pool, so their\n")
	b.pf("// maxima can add to more than can ever be occupied at once.\n")
	b.pf("pub const %s_BLOCK_MAX_BYTES: i64 = %d;\n\n", ir.RustConstName(name), bl.MaxBytes)

	b.pf("// The block: one extent, 64-byte aligned at its base, the PROJECTION at\n")
	b.pf("// offset 0 and then each out-of-line array in declaration order (§19.1).\n")
	b.pf("//\n")
	b.pf("// A block is valid until the next fill on the SAME storage, which\n")
	b.pf("// invalidates every block over it and every row reference taken from one.\n")
	b.pf("#[derive(Clone, Copy)]\npub struct %sBlock {\n", name)
	b.pf("    base: *const u8, // the extent's base, 64-byte aligned\n")
	b.pf("    bytes: i64,      // the extent in use\n")
	b.pf("}\n\n")

	b.pf("impl %sBlock {\n", name)
	b.pf("    pub const PROJECTION_BYTES: i64 = %d;\n", proj.Size)
	b.pf("    pub const MAX_BYTES: i64 = %s_BLOCK_MAX_BYTES;\n", ir.RustConstName(name))
	for _, a := range bl.Arrays {
		up := ir.RustConstName(a.Field.Name)
		b.pf("\n    // %s: the constants this build asserts against. A consumer INDEXES\n", a.Field.Name)
		b.pf("    // with what it read from the instance, never with these (§19.2).\n")
		b.pf("    pub const %s_STRIDE: i64 = %d;\n", up, a.Stride)
		b.pf("    pub const %s_MAX: i64 = %d;\n", up, a.Max)
		b.pf("    pub const %s_PROJECTION_OFFSET: i64 = %d;\n", up, a.TripleOffset)
	}
	b.pf("\n")
	b.emitBlockOpen(bl)
	b.pf("\n    pub fn base(&self) -> *const u8 {\n        self.base\n    }\n\n")
	b.pf("    pub fn bytes(&self) -> i64 {\n        self.bytes\n    }\n\n")
	b.pf("    pub fn projection(&self) -> &%sBlockProjection {\n", name)
	b.pf("        unsafe { &*(self.base as *const %sBlockProjection) }\n    }\n", name)
	for _, a := range bl.Arrays {
		b.emitBlockRows(bl, a)
	}
	b.emitBlockTypeInfo(bl)
	b.pf("}\n\n")
	b.emitRecordInfos(bl)
}

// emitBlockTypeInfo is the block's own descriptor: the PROJECTION as data
// (docs/SPEC-TABLES.md §8, §19.2).
func (b *blockGen) emitBlockTypeInfo(bl *ir.BlockLayout) {
	name := bl.Table.Name
	b.pf("\n    // this table's block descriptor (docs/SPEC-TABLES.md §8, §19.2):\n")
	b.pf("    // constant data, describing the PROJECTION.\n")
	b.pf("    pub fn type_info() -> &'static TableBlockInfo {\n")
	b.pf("        static FIELDS: [TableBlockFieldInfo; %d] = [\n", len(bl.Projection.Fields))
	for _, fl := range bl.Projection.Fields {
		b.pf("            %s,\n", b.blockFieldRow(bl, fl, true))
	}
	b.pf("        ];\n")
	b.pf("        static INFO: TableBlockInfo = TableBlockInfo {\n")
	b.pf("            name: %q,\n            build_version: BUILD_VERSION,\n            size: %d,\n            align: %d,\n            num_fields: %d,\n            fields: &FIELDS,\n",
		name, bl.Projection.Size, bl.Projection.Align, len(bl.Projection.Fields))
	b.pf("        };\n        &INFO\n    }\n")
}

// emitRecordInfos emits one descriptor per RECORD the block reaches — the row
// types, and anything they nest by value.
func (b *blockGen) emitRecordInfos(bl *ir.BlockLayout) {
	for _, name := range b.recordsOf(bl) {
		st := b.unit.Tables[name]
		if st == nil {
			st = b.unit.Structs[name]
		}
		if st == nil {
			continue
		}
		if b.emittedRecords == nil {
			b.emittedRecords = map[string]bool{}
		}
		if b.emittedRecords[name] {
			continue
		}
		b.emittedRecords[name] = true
		layout := ir.RecordLayout(b.unit, st)
		b.pf("// %s as a block RECORD: the row an out-of-line array holds, or a\n", name)
		b.pf("// record one nests by value (docs/SPEC-TABLES.md §19.2).\n")
		b.pf("pub fn %s() -> &'static TableBlockInfo {\n", fn(name, "block_info"))
		b.pf("    static FIELDS: [TableBlockFieldInfo; %d] = [\n", len(layout.Fields))
		for _, fl := range layout.Fields {
			b.pf("        %s,\n", b.blockFieldRow(nil, fl, false))
		}
		b.pf("    ];\n")
		b.pf("    static INFO: TableBlockInfo = TableBlockInfo {\n")
		b.pf("        name: %q,\n        build_version: BUILD_VERSION,\n        size: %d,\n        align: %d,\n        num_fields: %d,\n        fields: &FIELDS,\n",
			name, layout.Size, layout.Align, len(layout.Fields))
		b.pf("    };\n    &INFO\n}\n\n")
	}
}

// recordsOf is every record one block form reaches, sorted: each out-of-line
// array's element, each by-value nesting in the projection, and everything
// those reach transitively.
func (b *blockGen) recordsOf(bl *ir.BlockLayout) []string {
	seen := map[string]bool{}
	var order []string
	var walk func(name string)
	walk = func(name string) {
		if seen[name] {
			return
		}
		st := b.unit.Tables[name]
		if st == nil {
			st = b.unit.Structs[name]
		}
		if st == nil {
			return
		}
		seen[name] = true
		order = append(order, name)
		for _, f := range st.Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			if ref, ok := f.Type.Ref.(*ir.Struct); ok {
				walk(ref.Name)
			}
		}
	}
	for _, a := range bl.Arrays {
		walk(a.ElemName)
	}
	for _, fl := range bl.Projection.Fields {
		if ir.BlockOutOfLine(fl.Field) {
			continue
		}
		if fl.Field.Type.Kind == ir.TNamed {
			if ref, ok := fl.Field.Type.Ref.(*ir.Struct); ok {
				walk(ref.Name)
			}
		}
	}
	sort.Strings(order)
	return order
}

// blockFieldRow renders one TableBlockFieldInfo literal.
func (b *blockGen) blockFieldRow(bl *ir.BlockLayout, fl ir.FieldLayout, projection bool) string {
	f := fl.Field
	outOfLine := projection && ir.BlockOutOfLine(f)
	offsetOfOffset, countOffset, strideOffset, stride := "u32::MAX", "u32::MAX", "u32::MAX", "0"
	element := "None"
	if outOfLine {
		a := bl.ArrayByName(f.Name)
		offsetOfOffset = fmt.Sprint(a.OffsetOfOffset)
		countOffset = fmt.Sprint(a.CountOffset)
		strideOffset = fmt.Sprint(a.StrideOffset)
		stride = fmt.Sprint(a.Stride)
		element = fmt.Sprintf("Some(%s)", fn(a.ElemName, "block_info"))
	} else if f.Type.Kind == ir.TNamed {
		if ref, ok := f.Type.Ref.(*ir.Struct); ok {
			element = fmt.Sprintf("Some(%s)", fn(ref.Name, "block_info"))
		}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "TableBlockFieldInfo {\n")
	fmt.Fprintf(&out, "            name: %q,\n", f.Name)
	fmt.Fprintf(&out, "            offset: %d,\n", fl.Offset)
	fmt.Fprintf(&out, "            size: %d,\n", fl.Size)
	fmt.Fprintf(&out, "            kind: %d,\n", ir.TableFieldKind(f))
	fmt.Fprintf(&out, "            out_of_line: %v,\n", outOfLine)
	fmt.Fprintf(&out, "            offset_of_offset: %s,\n", offsetOfOffset)
	fmt.Fprintf(&out, "            count_offset: %s,\n", countOffset)
	fmt.Fprintf(&out, "            stride_offset: %s,\n", strideOffset)
	fmt.Fprintf(&out, "            stride: %s,\n", stride)
	fmt.Fprintf(&out, "            element: %s,\n", element)
	fmt.Fprintf(&out, "        }")
	return out.String()
}

// projectionMembers names the projection's generated members for one field,
// in piece order.
func (b *blockGen) projectionMembers(bl *ir.BlockLayout, f *ir.Field) []string {
	if ir.BlockOutOfLine(f) {
		return []string{f.Name}
	}
	var names []string
	names = append(names, f.Name)
	switch {
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

func (b *blockGen) emitProjectionField(bl *ir.BlockLayout, fl ir.FieldLayout) {
	f := fl.Field
	if ir.BlockOutOfLine(f) {
		b.pf("    pub %s: TableBlockTriple, // [..%d]%s laid out of line: (offset_of, count, stride)\n",
			f.Name, f.ArrayBound, f.Type.Name)
		return
	}
	switch {
	case f.Type.Kind == ir.TString:
		b.pf("    pub %s: [u8; %d], // string(%s): buffer, used length beside it\n",
			f.Name, f.Type.Size+1, ir.RenderExpr(f.Type.SizeExpr))
		b.pf("    pub %s_length: i32,\n", f.Name)
	case f.Type.Kind == ir.TBytes:
		b.pf("    pub %s: [u8; %d], // bytes(%s): fixed buffer, used length beside it\n",
			f.Name, f.Type.Size, ir.RenderExpr(f.Type.SizeExpr))
		b.pf("    pub %s_length: i32,\n", f.Name)
	case f.KeyEnum != "":
		b.pf("    pub %s: [%s; %d], // [%s]: one slot per named variant\n",
			f.Name, cookElemType(f), f.ArrayBound, f.KeyEnum)
	case f.Array == ir.ArrayFixed:
		b.pf("    pub %s: [%s; %d],\n", f.Name, cookElemType(f), f.ArrayBound)
	case f.Array == ir.ArrayCounted:
		b.pf("    pub %s: [%s; %d],\n", f.Name, cookElemType(f), f.ArrayBound)
		b.pf("    pub %s_count: i32,\n", f.Name)
	default:
		b.pf("    pub %s: %s,\n", f.Name, cookElemType(f))
	}
	if f.Type.Optional {
		b.pf("    pub %s_present: bool,\n", f.Name)
	}
}

func (b *blockGen) emitBlockOpen(bl *ir.BlockLayout) {
	name := bl.Table.Name
	b.pf("    // Open checks once and POINTS, and this is the WHOLE check (§19.2):\n")
	b.pf("    // the base's alignment, the magic, the build version, the byte order,\n")
	b.pf("    // and then, per out-of-line array, the pitch against this build's, the\n")
	b.pf("    // count against the declared maximum, and the rows inside the extent\n")
	b.pf("    // the caller passed.\n")
	b.pf("    //\n")
	b.pf("    // EVERY NUMBER BELOW COMES FROM THE INSTANCE, so the arithmetic is\n")
	b.pf("    // unsigned and each term is BOUNDED BEFORE IT IS ADDED. A forged\n")
	b.pf("    // offset_of near 2^63 must refuse, and an addition that carried past\n")
	b.pf("    // the top of the type would be the undefined behaviour the check after\n")
	b.pf("    // it was supposed to catch.\n")
	b.pf("    ///\n    /// # Safety\n    /// `base` must point at `bytes` readable bytes that outlive the handle.\n")
	b.pf("    pub unsafe fn open(base: *const u8, bytes: i64) -> Option<%sBlock> {\n", name)
	b.pf("        unsafe {\n")
	b.pf("            if base.is_null() || bytes < %d {\n                return None;\n            }\n", bl.Projection.Size)
	b.pf("            if (base as usize) %% %d != 0 {\n                return None; // the base's alignment\n            }\n", ir.BlockAlign)
	b.pf("            let magic = table_block_read64(base);\n")
	b.pf("            if magic != TABLE_BLOCK_MAGIC {\n")
	b.pf("                // a byte-swapped magic is a FOREIGN BYTE ORDER, and anything\n")
	b.pf("                // else is not a block at all. Both refuse; the distinction is\n")
	b.pf("                // here so a reader of this code knows the check covers the\n")
	b.pf("                // order too.\n")
	b.pf("                let _ = table_block_byteswap64(magic);\n")
	b.pf("                return None;\n            }\n")
	b.pf("            if table_block_read64(base.add(8)) != BUILD_VERSION {\n                return None;\n            }\n")
	b.pf("            if table_block_read64(base.add(16)) != TABLE_BLOCK_BYTE_ORDER {\n")
	b.pf("                return None; // a block of the other byte order: the fix-up path\n")
	b.pf("                             // is a named obligation\n            }\n")
	if len(bl.Arrays) == 0 {
		b.pf("            // this table declares no out-of-line array, so the prologue and\n")
		b.pf("            // the projection's own extent are the whole check\n")
		b.pf("            let used: i64 = %d;\n", bl.Projection.Size)
	} else {
		b.pf("            let projection = &*(base as *const %sBlockProjection);\n", name)
		b.pf("            let mut used: i64 = %d;\n", bl.Projection.Size)
	}
	for _, a := range bl.Arrays {
		up := ir.RustConstName(a.Field.Name)
		b.pf("            {\n")
		b.pf("                let offset_of = projection.%s.offset_of;\n", a.Field.Name)
		b.pf("                let count = projection.%s.count as u64;\n", a.Field.Name)
		b.pf("                let stride = projection.%s.stride as u64;\n", a.Field.Name)
		b.pf("                if stride != core::mem::size_of::<%sRow>() as u64 {\n                    return None;\n                }\n", a.ElemName)
		b.pf("                if count > Self::%s_MAX as u64 {\n", up)
		b.pf("                    return None; // past the DECLARED MAXIMUM: a consumer that\n")
		b.pf("                                 // sized anything by the maximum would overflow\n")
		b.pf("                                 // on a count the maximum does not bound\n                }\n")
		b.pf("                if offset_of < %d || offset_of %% %d != 0 {\n                    return None;\n                }\n",
			bl.Projection.Size, ir.BlockAlign)
		b.pf("                if offset_of > bytes as u64 {\n                    return None;\n                }\n")
		b.pf("                let rows = count * stride; // both bounded above: this cannot carry\n")
		b.pf("                if rows > bytes as u64 - offset_of {\n                    return None;\n                }\n")
		b.pf("                let end = (offset_of + rows) as i64;\n")
		b.pf("                if end > used {\n                    used = end;\n                }\n")
		b.pf("            }\n")
	}
	b.pf("            // the used extent, rounded to 64 WITHOUT the rounding itself\n")
	b.pf("            // carrying past the top of the type: used is already inside bytes,\n")
	b.pf("            // and the padding is paid out of the slack that is left rather\n")
	b.pf("            // than added and compared after.\n")
	b.pf("            let padding = (%d - (used %% %d)) %% %d;\n", ir.BlockAlign, ir.BlockAlign, ir.BlockAlign)
	b.pf("            if padding > bytes - used {\n                return None;\n            }\n")
	b.pf("            let used = used + padding;\n")
	b.pf("            Some(%sBlock {\n                base,\n                bytes: used,\n            })\n", name)
	b.pf("        }\n    }\n")
}

func (b *blockGen) emitBlockRows(bl *ir.BlockLayout, a ir.BlockArray) {
	b.pf("\n    // %s's rows, as a slice over the region at the pitch the INSTANCE\n", a.Field.Name)
	b.pf("    // gives. The pitch IS the element's size rounded to its alignment —\n")
	b.pf("    // derived, always, with no declaration that adjusts it (§2.7) — so the\n")
	b.pf("    // rows are contiguous and a slice is the honest view. The stride still\n")
	b.pf("    // rides in the triple, because it is what a consumer indexes with and\n")
	b.pf("    // it must come from the data (§19.2).\n")
	b.pf("    pub fn %s(&self) -> &[%sRow] {\n", a.Field.Name, a.ElemName)
	b.pf("        let triple = self.projection().%s;\n", a.Field.Name)
	b.pf("        unsafe {\n")
	b.pf("            core::slice::from_raw_parts(\n")
	b.pf("                self.base.add(triple.offset_of as usize) as *const %sRow,\n", a.ElemName)
	b.pf("                triple.count as usize,\n")
	b.pf("            )\n")
	b.pf("        }\n    }\n")
}
