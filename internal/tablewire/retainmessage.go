// RETAIN-UNKNOWN ON THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3, §6.6).
//
// `LoadRetain` reads a form-`2` body as it reads a file's, the resolving walk
// replacing every reference with the id it names AGAINST THE CONNECTION'S
// VOCABULARY instead of a trailer, and `SaveRetain` writing form `2` refuses by
// name. So retention crosses the forms in ONE DIRECTION, and the round trip a
// caller has is form 2 in and form 1 out: the file carries its own table and
// takes §6.6 unchanged.
//
// THE RECORD IS THE FILE FORM'S OWN, to the byte. A retained record must carry
// the field's identity and bytes WITH EVERY REFERENCE RESOLVED, so that
// re-emitting it into any id table is correct, and the table it is re-emitted
// into is a FILE's. So the capture here is a TRANSCODE as well as a resolve: a
// bitpacked payload is read at the width its announced shape states and written
// at the width the file form spells, and from there it is the same record the
// file's own capture makes and the same emitter writes back. Nothing below
// copies the emit side, and nothing below is a second record layout.
//
// THE SKIP RUNS FIRST AND THE CAPTURE SECOND, over the same bits. The plain
// read's own verdict on an unknown entry is the skip's — a variant reference
// that names an entry carrying a payload is damage whether or not this build
// retains — and running the skip first is what keeps that verdict exactly what
// it was with retention off. The capture then re-reads the bits the skip
// delimited, and a capture that lands anywhere but the skip's own end drops the
// record: two engines that disagree about a payload's width would otherwise
// disagree in silence. It is two flat passes over one record's bits and never a
// walk that doubles at every level, which is what the cost rule forbids (§6.6).
package tablewire

import (
	"encoding/binary"
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// DecodeRetainMessages is [DecodeMessages] with retention on
// (docs/SPEC-TABLES.md §3.3, §6.6). It reads exactly what [DecodeMessages]
// reads and reports exactly what it reports, and beside that it keeps the
// fields this build cannot name, in the caller's own buffers.
//
// A BATCH TAKES ONE REGION AND ONE RETENTION BUFFER A BODY. The batch is one
// region and each body carries its OWN node directory inside it (§3.3), so a
// record's first step is an index into the directory of the body it came from,
// and the buffer that holds it is that body's own: `retains[k]` belongs to
// `insts[k]`, is reset by this call, and is what `EncodeRetain` from that
// instance reads. A caller with fewer buffers than the batch has bodies is
// refused before a body is read, on the batch bound's own terms.
func DecodeRetainMessages(m *tabletext.Model, insts []*tabletext.Instance, data []byte, v *Vocabulary, retains []*Retain, report *tabletext.Report) (int, bool, error) {
	if retains == nil {
		return DecodeMessages(m, insts, data, v, report)
	}
	if len(retains) < len(insts) {
		insts = insts[:len(retains)]
	}
	states := make([]*retainState, len(insts))
	for i := range states {
		if retains[i] != nil {
			states[i] = newRetainState(m, retains[i])
		}
	}
	return decodeMessagesWith(m, insts, data, v, report, states)
}

// captureMessage is the whole of the load side for one unknown entry: the
// outer-kind exclusion, the transcoding walk, the capacity, and the counter
// each answers. `at` is where the payload began and `end` where the skip left
// the reader, and the walk must land on `end` exactly.
func (rt *retainState) captureMessage(d *bitDecoder, inst *tabletext.Instance, entry ir.TableVocabularyEntry, at, end int) {
	if entry.Kind == ir.TableKindPointer {
		// A FIELD WHOSE PAYLOAD CARRIES A NODE INDEX: kind 17 itself, the
		// excluded class at the outer kind (§6.6)
		rt.lost(d.report)
		return
	}
	rs := &resolver{}
	was := d.r.off
	d.r.off = at
	rs.msgPayload(d, entry, 0)
	landed := d.r.off
	d.r.off = was
	if rs.bad || landed != end {
		// THE WALK CAN MEET WHAT THE SKIP STEPPED OVER, and its verdict changes
		// nothing else: the record is dropped, one retain_lost counts, and
		// `malformed` DOES NOT MOVE (§6.6).
		rt.lost(d.report)
		return
	}
	rt.keep(inst, entry.Id, entry.Kind, rs.out, d.report)
}

// msgRef resolves one reference against the CONNECTION'S VOCABULARY and answers
// the sixty-four-bit id it names. It is the file walk's own verdict read
// against an announcement instead of a trailer (§6.6): a reference of zero, one
// above the entry count, and one naming a RESERVED id, which would be
// re-emitted into a nested body where it is malformed, all drop the record.
func (rs *resolver) msgRef(d *bitDecoder) uint64 {
	ref, ok := d.reference()
	if !ok {
		rs.bad = true
		return 0
	}
	return rs.msgId(d, ref)
}

func (rs *resolver) msgId(d *bitDecoder, ref uint64) uint64 {
	entry, named := d.entry(ref)
	if !named || reserved(entry.Id) || entry.Id == 0 {
		rs.bad = true
		return 0
	}
	return entry.Id
}

// msgPayload is the resolving walk over ONE announced payload — a field's, an
// array element's, a union arm's — written out in the FILE form's own spelling.
//
// WHICH KINDS THE WALK TOUCHES is §6.6's list unchanged: kind 13, a body's
// fields; kind 15, an arm id and then the arm's payload under this same rule;
// kind 30, a variant id; kind 14 at its element kind; and kind 16 at EVERY
// element kind, because a keyed body carries a KEY REFERENCE per slot whatever
// the elements are. What the file form COPIES VERBATIM is transcoded here
// instead, because a bitpacked value is not the file's bytes.
//
// KIND 17 IS WHAT THE WALK IS LOOKING FOR AS MUCH AS A REFERENCE IS: meeting
// one anywhere in the payload drops the whole record, which is what keeps a
// retained record from ever carrying a node index.
func (rs *resolver) msgPayload(d *bitDecoder, entry ir.TableVocabularyEntry, depth int) {
	if rs.bad {
		return
	}
	switch int(entry.Kind) {
	case ir.TableKindPointer:
		// THE NODE-INDEX CLASS, met inside a payload rather than at the outer
		// kind: the record is dropped whole (§6.6)
		rs.bad = true
	case 0:
		// A KIND-0 ENTRY IS A NAME (§3.3) and names no payload the file form
		// has a kind for, so a body that used one as a field is a shape this
		// walk cannot frame: the plain read skips it and this drops it.
		rs.bad = true
	case ir.TableKindNoPayload:
		// kind 32 rides no bits here and an empty framed payload on the file
		rs.out = appendLeb(rs.out, 0)
	case ir.TableKindEnum:
		ref, ok := d.reference()
		if !ok {
			rs.bad = true
			return
		}
		if ref == 0 {
			// A VARIANT REFERENCE OF ZERO IS THE ENUM'S None (§3): a value, not
			// a reference, and it rides back as one
			rs.u64(0)
			return
		}
		rs.u64(rs.msgId(d, ref))
	case ir.TableKindUnion:
		ref, ok := d.reference()
		if !ok {
			rs.bad = true
			return
		}
		if ref == 0 {
			rs.u64(0)
			return
		}
		arm, named := d.entry(ref)
		if !named || reserved(arm.Id) || arm.Kind == 0 {
			rs.bad = true
			return
		}
		rs.u64(arm.Id)
		rs.u8(arm.Kind)
		rs.msgFramed(d, arm, depth)
	case ir.TableKindTable, ir.TableKindArray, ir.TableKindKeyed:
		rs.msgFramed(d, entry, depth)
	case ir.TableKindString, ir.TableKindWstring, ir.TableKindEscape:
		// an opaque payload rides behind a CANONICAL LEB128 LENGTH on the file
		b := rs.msgOpaque(d, entry)
		if rs.bad {
			return
		}
		rs.out = appendLeb(rs.out, uint64(len(b)))
		rs.out = append(rs.out, b...)
	default:
		rs.msgScalar(d, entry)
	}
}

// msgOpaque reads one text or blob payload and answers its FILE bytes. Each of
// the three has its own framing here and none of them on the file, where the
// payload's own length frames it: a `string(N)` or a `bytes(N)` is a length at
// its own width, the ALIGN that buys a memcpy, then the bytes; a wide string is
// a length and then SIXTEEN bits a code unit, which is two file bytes each; and
// the ESCAPE aligns, reads a thirty-two bit L and takes L bytes opaque (§3.3).
func (rs *resolver) msgOpaque(d *bitDecoder, entry ir.TableVocabularyEntry) []byte {
	n := 0
	switch int(entry.Kind) {
	case ir.TableKindString, ir.TableKindWstring:
		raw, ok := d.r.get(ir.TableMessageBitsRequired(0, entry.Shape.Max))
		if !ok {
			rs.bad = true
			return nil
		}
		n = int(raw)
		if entry.Kind == ir.TableKindWstring {
			n *= 2
		} else if !d.r.align() {
			rs.bad = true
			return nil
		}
	default:
		if !d.r.align() {
			rs.bad = true
			return nil
		}
		raw, ok := d.r.get(32)
		if !ok {
			rs.bad = true
			return nil
		}
		n = int(raw)
	}
	b, ok := d.r.bytes(n)
	if !ok {
		rs.bad = true
		return nil
	}
	return b
}

// msgFramed writes one CONTENT behind a length. The lengths inside a record are
// this engine's own spelling, so each is written after the content it frames
// and the capture costs one pass (§6.6).
func (rs *resolver) msgFramed(d *bitDecoder, entry ir.TableVocabularyEntry, depth int) {
	at := rs.mark()
	rs.msgContent(d, entry, depth+1)
	rs.patch(at)
}

// msgContent resolves a payload the file form frames by a length: a kind 13,
// 14 or 16 field's own body, a union arm's payload, and an enum-keyed slot's
// element.
func (rs *resolver) msgContent(d *bitDecoder, entry ir.TableVocabularyEntry, depth int) {
	if rs.bad {
		return
	}
	if depth > retainDepthCap {
		// THE WALK'S NESTING CAP: a record past it is dropped on the same rule
		// as any other shape the walk cannot take (§6.6)
		rs.bad = true
		return
	}
	switch int(entry.Kind) {
	case ir.TableKindPointer:
		rs.bad = true
	case ir.TableKindTable:
		rs.msgBody(d, depth)
	case ir.TableKindArray, ir.TableKindKeyed:
		rs.msgElements(d, entry, depth)
	case ir.TableKindUnion, ir.TableKindEnum:
		rs.msgPayload(d, entry, depth)
	case ir.TableKindNoPayload:
		// nothing rides, and the outer length is what says so
	case ir.TableKindString, ir.TableKindEscape, ir.TableKindWstring:
		// a content copied whole carries no inner length: the outer one frames
		// it, which is what a keyed slot's element takes
		rs.msgText(d, entry)
	default:
		rs.msgScalar(d, entry)
	}
}

// msgText is a text or blob content under an outer length: the same bytes
// msgPayload writes, without the inner length the outer one replaces.
func (rs *resolver) msgText(d *bitDecoder, entry ir.TableVocabularyEntry) {
	b := rs.msgOpaque(d, entry)
	if rs.bad {
		return
	}
	rs.out = append(rs.out, b...)
}

// msgElements is one array's or one keyed body's content: the element kind, the
// count, and the elements. A KEYED SLOT IS A TRIPLE — the key reference, the
// element's own length, and the element — and its keys resolve at every element
// kind (§3.2, §6.6).
func (rs *resolver) msgElements(d *bitDecoder, entry ir.TableVocabularyEntry, depth int) {
	shape := entry.Shape
	if shape.Elem == ir.TableKindPointer {
		// AN ARRAY WHOSE ELEMENT KIND IS 17 (§6.6): the excluded class, caught
		// here for a keyed body exactly as for a positional one
		rs.bad = true
		return
	}
	n := uint64(shape.Min)
	width := ir.TableMessageCountBits(shape)
	if entry.Kind == ir.TableKindKeyed {
		width, n = ir.TableMessageBitsRequired(0, shape.Max), 0
	}
	if width > 0 {
		raw, ok := d.r.get(width)
		if !ok {
			rs.bad = true
			return
		}
		n = raw
		if entry.Kind == ir.TableKindArray {
			n += uint64(shape.Min)
		}
	}
	if ir.TableMessageAligns(entry.Kind, shape) && !d.r.align() {
		rs.bad = true
		return
	}
	inner := ir.TableMessageShape{}
	if shape.Inner != nil {
		inner = *shape.Inner
	}
	element := ir.TableVocabularyEntry{Kind: shape.Elem, Shape: inner}
	rs.u8(shape.Elem)
	rs.u64(n)
	for range n {
		if rs.bad {
			return
		}
		if entry.Kind == ir.TableKindKeyed {
			rs.u64(rs.msgRef(d))
			rs.msgFramed(d, element, depth)
			continue
		}
		rs.msgPayload(d, element, depth)
	}
}

// msgBody resolves one nested body: its fields, each an entry REFERENCE and the
// payload that entry's shape frames, ending at the body's own ZERO REFERENCE.
func (rs *resolver) msgBody(d *bitDecoder, depth int) {
	for {
		if rs.bad {
			return
		}
		ref, ok := d.reference()
		if !ok {
			rs.bad = true
			return
		}
		if ref == 0 {
			// THE TERMINATOR IS A REFERENCE, and a reference in the resolved
			// form is a fixed eight-byte id
			rs.u64(0)
			return
		}
		entry, named := d.entry(ref)
		if !named || reserved(entry.Id) || entry.Id == 0 {
			rs.bad = true
			return
		}
		rs.u64(entry.Id)
		rs.u8(entry.Kind)
		rs.msgPayload(d, entry, depth)
	}
}

// msgScalar transcodes ONE announced value: read at the width the shape states,
// written at the width the file form spells. It is the third of the three ways
// the form changes a value (§3.3) taken in the one direction retention has: a
// mask rides at its declared W bits and a compressed float as its QUANTIZED
// INDEX here, and the file carries the mask's own storage width and the float
// the index names.
//
// NO CLAMP FIRES AND NO RANGE IS APPLIED. The value is the SENDER's, under an
// id this reader cannot name, so there is no declaration to bound it by: the
// walk reads kind bytes, lengths and references and takes no branch on a
// payload byte (§6.6, THE SECURITY BOUND).
func (rs *resolver) msgScalar(d *bitDecoder, entry ir.TableVocabularyEntry) {
	kind := int(entry.Kind)
	shape := entry.Shape
	switch kind {
	case ir.TableKindBool:
		v, ok := d.r.get(1)
		if !ok {
			rs.bad = true
			return
		}
		rs.u8(uint8(v))
		return
	case ir.TableKindF64:
		raw, ok := d.r.get(64)
		if !ok {
			rs.bad = true
			return
		}
		rs.out = binary.LittleEndian.AppendUint64(rs.out, raw)
		return
	case ir.TableKindF32:
		rs.msgFloat32(d, shape)
		return
	}
	width := int(ir.TableMessageValueBits(entry.Kind, shape))
	if width < 0 {
		rs.bad = true
		return
	}
	base := shape.Base
	if base == nil {
		base = big.NewInt(0)
	}
	if ir.TableKindWide(kind) {
		var value *big.Int
		if shape.Packing == ir.TableMessageRanged {
			offset, ok := d.r.getBig(width)
			if !ok {
				rs.bad = true
				return
			}
			value = new(big.Int).Add(offset, base)
		} else {
			raw, ok := d.r.bytes(16)
			if !ok {
				rs.bad = true
				return
			}
			value = tabletext.WideFromBytes(raw, kind)
		}
		rs.out = append(rs.out, tabletext.WideBytes(value, kind)...)
		return
	}
	raw, ok := d.r.get(width)
	if !ok {
		rs.bad = true
		return
	}
	signed := ir.TableKindSigned(kind)
	var value uint64
	switch {
	case shape.Packing == ir.TableMessageRanged && signed:
		value = uint64(int64(raw) + base.Int64())
	case shape.Packing == ir.TableMessageRanged:
		value = raw + base.Uint64() // the unsigned domain, whole: the sum wraps into the bits below
	case signed && width < 64 && width > 0:
		shift := uint(64 - width)
		value = uint64(int64(raw<<shift) >> shift)
	default:
		value = raw
	}
	rs.msgWord(value, kind)
}

// msgWord writes one integer at the FILE's own width for its kind, little
// endian, which is two's complement where the kind is signed.
func (rs *resolver) msgWord(value uint64, kind int) {
	switch ir.TableKindWidth(kind) {
	case 1:
		rs.u8(uint8(value))
	case 2:
		rs.out = binary.LittleEndian.AppendUint16(rs.out, uint16(value))
	case 4:
		rs.out = binary.LittleEndian.AppendUint32(rs.out, uint32(value))
	case 8:
		rs.u64(value)
	default:
		rs.bad = true
	}
}

// msgFloat32 reads an f32 at the sender's packing: the IEEE bit pattern where
// it rides raw, and the float its INDEX names where the sender quantized it
// (SPEC.md §4.3). An index above `count` is rejected, as the packet wire
// rejects it, and drops the record.
func (rs *resolver) msgFloat32(d *bitDecoder, shape ir.TableMessageShape) {
	if shape.Packing == ir.TableMessageQuantized {
		index, ok := d.r.get(int(shape.Bits))
		if !ok {
			rs.bad = true
			return
		}
		count, _, derived := ir.TableMessageQuantization(shape)
		if !derived || index > uint64(count) {
			rs.bad = true
			return
		}
		v := ir.TableMessageDequantize(shape, uint32(index))
		rs.out = binary.LittleEndian.AppendUint32(rs.out, math.Float32bits(v))
		return
	}
	raw, ok := d.r.get(32)
	if !ok {
		rs.bad = true
		return
	}
	rs.out = binary.LittleEndian.AppendUint32(rs.out, uint32(raw))
}
