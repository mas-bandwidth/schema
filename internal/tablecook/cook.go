// THE COOKED FORM (SPEC-TABLES.md §7): the header, the data part and the
// attribution part, and the two directions between a cook and the wire.
//
// Cooking is fundamentally an OPTIMIZATION, and every rule here is a
// consequence of that one sentence: don't parse, just point at an mmap'd data
// structure loaded as it stands, and have it work. The header build-locks the
// file, the data part is `Lock`'s region written verbatim with the root at its
// base, and the attribution part is written BESIDE the data for the TOOL — a
// build that ships no tooling need not carry it at all.
package tablecook

import (
	"encoding/binary"
	"fmt"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// Magic identifies a schema COOK, and it carries the byte-order check with it.
// It is stored in the TARGET's order — the order the cook is produced in — so a
// consumer reads it BYTEWISE before anything else: it either is this build's
// constant, or is that constant byte-reversed, which identifies a cook of the
// other order, or is not a cook at all.
//
// The value is "SCHMCOOK" read as ASCII in the low-to-high byte order a
// little-endian store produces, exactly as §19.1's block magic is "SCHMABLK",
// so a hex dump of a little-endian cook is legible and the two accelerators sit
// side by side in one vocabulary. **WHAT SEPARATES A COOK FROM A BLOCK IS THE
// MAGIC** — they share the build version, because a build version answers
// "which build?" and not "which form?" (§7).
const Magic = uint64(0x4B4F4F434D484353)

// Byte-order words, the same two values a block's prologue carries (§19.1). The
// MAGIC is what refuses a foreign order; this word is what RECORDS which order
// wrote it, so a refusal can name the order rather than infer it and a tool
// dumping a cook can read the fact instead of deducing it from a constant.
const (
	ByteOrderLittle = uint64(ir.BlockByteOrderLittle)
	ByteOrderBig    = uint64(ir.BlockByteOrderBig)
)

// HeaderBytes is the cooked header's size. Sixty-four bytes: eight words, of
// which three are reserved, and a size that is already a multiple of every
// alignment this language has — so the data part begins at the header's end for
// every unit, and a mapped file's page alignment covers the base for free (§7).
const HeaderBytes = int64(64)

// The header's words, in order. A cooked file never crosses builds, so most of
// the header's shape is the implementation's business; THREE WIDTHS ARE NOT,
// because each one decides something semantic (§7): the magic is read bytewise
// and establishes the byte order every other field is written in; the BUILD
// VERSION is 64 bits, because under the match-and-point rule it is the sole
// guard between a runtime and a foreign region, and is sized like a digest
// rather than like a version counter; and BOTH PART LENGTHS are 64 bits,
// because `CookMeasure` answers in `int64_t` and a 32-bit field would reimpose
// at cook time the ceiling §3.1 just removed.
const (
	offMagic       = 0
	offBuildVer    = 8
	offByteOrder   = 16
	offDataLen     = 24
	offAttribLen   = 32
	offAlign       = 40
	offReserved0   = 48
	offReserved1   = 56
	directoryEntry = 16 // one attribution entry: offset (u64), type id (u64)
)

// Header is a cooked file's header, decoded.
type Header struct {
	BuildVersion uint64
	ByteOrder    uint64
	DataLength   int64
	AttribLength int64
	Align        int64
}

// DataOffset is where the data part begins, DERIVED rather than stored: the
// header's own size rounded up to the region's alignment. It is a derivation and
// not a field because a fact a reader can compute is a fact two writers cannot
// disagree about, and because `Open` must stay O(1) whatever it is.
func (h Header) DataOffset() int64 { return alignUp(HeaderBytes, h.Align) }

// AttribOffset is where the attribution part begins: immediately after the data
// part, whose length is a multiple of the region's alignment and therefore of
// eight, so the directory's `u64` pairs are aligned with no rule of their own.
func (h Header) AttribOffset() int64 { return h.DataOffset() + h.DataLength }

// Size is the whole file's length.
func (h Header) Size() int64 { return h.AttribOffset() + h.AttribLength }

// Order is the byte order this cook was produced in.
func (h Header) Order() order {
	if h.ByteOrder == ByteOrderBig {
		return binary.BigEndian
	}
	return binary.LittleEndian
}

// Options selects what a cook is produced for.
type Options struct {
	// Big produces a BIG-ENDIAN cook. The byte order is settled at cook time
	// for the target build (§7), so the reading side runs no fix-up pass at
	// all: a big-endian cook has every scalar in it swapped, once, offline, on
	// the writing side.
	Big bool
	// NoAttribution writes the header's attribution length as zero and leaves
	// the directory out, so a build that ships no tooling carries just data
	// (§7). `schema cook-check` then refuses the file and says which part is
	// missing, because a file with no attribution part cannot be checked.
	NoAttribution bool
}

func (o Options) order() order {
	if o.Big {
		return binary.BigEndian
	}
	return binary.LittleEndian
}

func (o Options) byteOrderWord() uint64 {
	if o.Big {
		return ByteOrderBig
	}
	return ByteOrderLittle
}

// Cook converts one root instance into the cooked form: the region `Lock` would
// pack, written verbatim behind the header, with the node directory of §6.3
// beside it as the attribution part.
//
// **The cook of a FIXED root table is the same idea with nothing in it** — a
// fixed-size table is one struct (§6.1), so its cooked form is the struct's
// bytes behind the header. There is still exactly one node, and its directory
// entry is the root at offset zero, because one shape is simpler than two and a
// tool that can check a graph can check a single record for free.
func Cook(m *tabletext.Model, inst *tabletext.Instance, opts Options) ([]byte, error) {
	g, err := tablewire.Number(m, inst)
	if err != nil {
		return nil, err
	}
	region, err := Layout(m, g)
	if err != nil {
		return nil, err
	}
	data, err := region.Write(m, opts.order())
	if err != nil {
		return nil, err
	}

	h := Header{
		BuildVersion: ir.BuildVersion(m.Unit),
		ByteOrder:    opts.byteOrderWord(),
		DataLength:   region.Bytes,
		Align:        region.Align,
	}
	if !opts.NoAttribution {
		h.AttribLength = int64(len(region.Nodes)) * directoryEntry
	}

	out := make([]byte, h.Size())
	ord := opts.order()
	ord.PutUint64(out[offMagic:], Magic)
	ord.PutUint64(out[offBuildVer:], h.BuildVersion)
	ord.PutUint64(out[offByteOrder:], h.ByteOrder)
	ord.PutUint64(out[offDataLen:], uint64(h.DataLength))
	ord.PutUint64(out[offAttribLen:], uint64(h.AttribLength))
	ord.PutUint64(out[offAlign:], uint64(h.Align))
	// the reserved words are RESERVED: a non-zero one means a writer used a form
	// this build does not understand, and `Open` refuses rather than ignoring it
	ord.PutUint64(out[offReserved0:], 0)
	ord.PutUint64(out[offReserved1:], 0)
	copy(out[h.DataOffset():], data)
	if h.AttribLength > 0 {
		at := h.AttribOffset()
		for _, n := range region.Nodes {
			ord.PutUint64(out[at:], uint64(n.Offset))
			ord.PutUint64(out[at+8:], ir.TableTypeId(n.Def.Name))
			at += directoryEntry
		}
	}
	return out, nil
}

// ReadHeader is `Open`'s check, and this is the WHOLE check, because nothing
// else is checked at all: the magic, the byte order it establishes, the build
// version, every RESERVED word zero, the two part lengths against the size the
// caller passed — a truncated file refuses — and the alignment of the base.
//
// On a match the bytes ARE what this build wrote, in this build's layout and
// this build's byte order, so there is nothing to validate and nothing to fix
// up. It is O(1) IN THE FILE'S SIZE: nothing here is per node, which is what
// lets a 1 MB cook and a 1 GB cook open in the same time and what a walk of any
// shape would forfeit.
//
// `build` is the build version to accept. A caller checking a file it did not
// produce — `schema cook-check`, `schema uncook` — passes the unit's own, which
// is the same number a runtime would hold.
func ReadHeader(file []byte, build uint64) (Header, error) {
	var h Header
	if int64(len(file)) < HeaderBytes {
		return h, fmt.Errorf("not a cook: %d bytes is shorter than the %d-byte header", len(file), HeaderBytes)
	}
	// THE MAGIC IS READ BYTEWISE, BEFORE ANYTHING ELSE, since it is what
	// establishes the byte order every other header field is written in
	var ord order
	var wrote uint64
	switch {
	case binary.LittleEndian.Uint64(file) == Magic:
		ord, wrote = binary.LittleEndian, ByteOrderLittle
	case binary.BigEndian.Uint64(file) == Magic:
		ord, wrote = binary.BigEndian, ByteOrderBig
	default:
		return h, fmt.Errorf("not a cook: the magic is 0x%016x, and a cook's is 0x%016x in one order or the other", binary.LittleEndian.Uint64(file), Magic)
	}
	h.BuildVersion = ord.Uint64(file[offBuildVer:])
	h.ByteOrder = ord.Uint64(file[offByteOrder:])
	h.DataLength = int64(ord.Uint64(file[offDataLen:]))
	h.AttribLength = int64(ord.Uint64(file[offAttribLen:]))
	h.Align = int64(ord.Uint64(file[offAlign:]))

	if r0, r1 := ord.Uint64(file[offReserved0:]), ord.Uint64(file[offReserved1:]); r0 != 0 || r1 != 0 {
		return h, fmt.Errorf("a reserved header word is not zero (0x%016x, 0x%016x): this file used a form this build does not understand", r0, r1)
	}
	// the magic settled the order; a recorded order that disagrees with it is a
	// corrupt or hand-edited artifact, and there is no reading that recovers it
	if h.ByteOrder != wrote {
		return h, fmt.Errorf("the magic says %s and the byte-order word says %d: a cook whose magic matched and whose order word did not is corrupt", orderName(wrote), h.ByteOrder)
	}
	if h.BuildVersion != build {
		return h, fmt.Errorf("build version 0x%016x is not this build's 0x%016x: a cook never crosses builds, and the fallback is a wire load", h.BuildVersion, build)
	}
	if h.Align <= 0 || h.Align&(h.Align-1) != 0 || h.Align > 64 {
		return h, fmt.Errorf("the recorded alignment %d is not a power of two in [1, 64]", h.Align)
	}
	// each part length is bounded by the file before any of them is added
	// together, so no arithmetic below can wrap into a length that fits
	if h.DataLength < 0 || h.AttribLength < 0 ||
		h.DataLength > int64(len(file)) || h.AttribLength > int64(len(file)) {
		return h, fmt.Errorf("a part length (%d data, %d attribution) does not fit the file's %d bytes", h.DataLength, h.AttribLength, len(file))
	}
	if h.DataLength%h.Align != 0 {
		return h, fmt.Errorf("the data part is %d bytes, which is not a multiple of the region's alignment %d", h.DataLength, h.Align)
	}
	if h.Size() != int64(len(file)) {
		return h, fmt.Errorf("truncated or trailing: the header describes %d bytes and the file is %d", h.Size(), len(file))
	}
	return h, nil
}

func orderName(w uint64) string {
	if w == ByteOrderBig {
		return "big"
	}
	return "little"
}

// Data is the region the runtime points at.
func (h Header) Data(file []byte) []byte {
	return file[h.DataOffset() : h.DataOffset()+h.DataLength]
}

// Attribution is the node directory written beside the data — nothing that
// READS the structure touches it (§6.3, §7).
func (h Header) Attribution(file []byte) []byte {
	return file[h.AttribOffset():h.Size()]
}

// DirectoryEntry is one node's attribution: where it starts and what type it is.
type DirectoryEntry struct {
	Offset int64
	TypeId uint64
}

// NotMaterialized is the directory's sentinel — a record whose type id the
// loading build could not name (§3.1, §6.3) — distinct from every real offset
// including the root's `0`, so an index resolving through it yields NULL and can
// never fabricate the root. **A COOK CANNOT CARRY ONE**: a cooked file is an
// accelerator and cannot carry a hole, so it refuses at the writer and
// `cook-check` refuses at the reader (§7).
const NotMaterialized = uint64(0xFFFFFFFFFFFFFFFF)

// Directory decodes the attribution part: one entry per numbered node, in index
// order, position `i` describing node index `i + 1`.
func (h Header) Directory(file []byte) ([]DirectoryEntry, error) {
	if h.AttribLength == 0 {
		return nil, fmt.Errorf("this cook carries no ATTRIBUTION part, so there is nothing to check it against: the header records its length as zero and the file is just data (SPEC-TABLES.md §7)")
	}
	if h.AttribLength%directoryEntry != 0 {
		return nil, fmt.Errorf("the attribution part is %d bytes, which is not a whole number of %d-byte directory entries", h.AttribLength, directoryEntry)
	}
	ord := h.Order()
	raw := h.Attribution(file)
	out := make([]DirectoryEntry, h.AttribLength/directoryEntry)
	for i := range out {
		at := i * directoryEntry
		out[i] = DirectoryEntry{
			Offset: int64(ord.Uint64(raw[at:])),
			TypeId: ord.Uint64(raw[at+8:]),
		}
	}
	return out, nil
}
