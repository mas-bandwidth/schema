// Package benchgen emits the bench harness's shape-dependent code (issue
// #191): the pinned instance, the LCG vary mapping, the §2.7 full-struct sink
// fold and the decoded-field check, per language, derived from the corpus
// schema. It is a SEPARATE emitter from the language backends in
// internal/codegen — those emit the serializers under test and are never
// touched by bench work (bench/LOCK).
//
// The pinned values have ONE source: the wire goldens in testdata/wire. The
// golden IS the pinned instance (BENCH-STANDARD.md §1.5 makes it the oracle
// every leg is gated against), so the emitter DECODES it and emits the values
// it read. No leg transcribes a pin any more, and no second declaration can
// drift from the bytes.
package benchgen

import (
	"fmt"
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// bitReader reads the schema wire form: one LSB-first bit stream over
// little-endian bytes. Every runtime op decomposes to that — a 64 bit field is
// the low dword then the high, a 128 bit field is four dwords low first, and
// both are exactly "the next N bits, low bit first" in this reader (SPEC §6).
type bitReader struct {
	data []byte
	pos  int64 // bit position
	err  error
}

func (r *bitReader) fail(format string, args ...any) {
	if r.err == nil {
		r.err = fmt.Errorf(format, args...)
	}
}

func (r *bitReader) bits(n int) uint64 {
	if n > 64 {
		r.fail("bit reader: %d bit read", n)
		return 0
	}
	if r.pos+int64(n) > int64(len(r.data))*8 {
		r.fail("golden ran out at bit %d (%d bytes)", r.pos, len(r.data))
		return 0
	}
	var v uint64
	for i := range n {
		b := (r.data[r.pos>>3] >> uint(r.pos&7)) & 1
		v |= uint64(b) << uint(i)
		r.pos++
	}
	return v
}

func (r *bitReader) bigBits(n int) *big.Int {
	out := new(big.Int)
	for i := range n {
		if r.pos+1 > int64(len(r.data))*8 {
			r.fail("golden ran out at bit %d (%d bytes)", r.pos, len(r.data))
			return out
		}
		if (r.data[r.pos>>3]>>uint(r.pos&7))&1 != 0 {
			out.SetBit(out, i, 1)
		}
		r.pos++
	}
	return out
}

func (r *bitReader) align() {
	for r.pos%8 != 0 {
		if r.bits(1) != 0 {
			r.fail("nonzero align padding at bit %d", r.pos-1)
		}
	}
}

func (r *bitReader) readBytes(n int64) []byte {
	r.align()
	if r.pos+n*8 > int64(len(r.data))*8 {
		r.fail("golden ran out reading %d bytes at bit %d", n, r.pos)
		return nil
	}
	out := make([]byte, n)
	copy(out, r.data[r.pos/8:r.pos/8+n])
	r.pos += n * 8
	return out
}

// bitsRequired is the runtime's offset-encoding width: the bit length of the
// range, zero for a degenerate one.
func bitsRequired(minV, maxV *big.Int) int {
	rng := new(big.Int).Sub(maxV, minV)
	return rng.BitLen()
}

// ---------------------------------------------------------------------------
// the decoded instance tree
// ---------------------------------------------------------------------------

// NodeKind classifies one decoded element of a shape.
type NodeKind int

const (
	NScalar     NodeKind = iota // int / bits / bool / float / fixed / enum / flags
	NStruct                     // a nested type
	NString                     // string(N): a used length and its bytes
	NBytes                      // bytes(N)
	NArrayScalar                // [N]T / [..N]T over a scalar element
	NArrayStruct                // [N]T / [..N]T over a nested type
	NUnion                      // a tag and the selected arm
)

// ValKind classifies a decoded scalar.
type ValKind int

const (
	VInt   ValKind = iota // integers: ranged, raw, bits, enum, flags, fixed raw
	VBool                 // bool
	VF32                  // float32 (raw or compressed)
	VF64                  // float64
	VWide                 // 128 bit integer, signed per Signed
	VEnum                 // enum-typed: the wire value, spelled as a variant
	VFlags                // flags-typed: the mask
	VFixed                // fixed/ufixed: the RAW scaled integer
)

// Value is one decoded scalar.
type Value struct {
	Kind   ValKind
	I      *big.Int // VInt / VWide / VEnum / VFlags / VFixed
	B      bool
	F32    float32
	F64    float64
	Signed bool
}

// Node is one wire-ordered element of a decoded shape. It carries what every
// renderer needs: the field, the value the golden pinned, the extent an array
// or buffer actually occupies, and how the field varies under the LCG.
type Node struct {
	F     *ir.Field
	Kind  NodeKind
	Index int // element index inside an array (NArray* children)

	Val   Value  // NScalar
	Buf   []byte // NString / NBytes: the used content
	Sub   []*Node
	Count int64 // NArray*: the pinned element count
	Elems []*Node

	Tag    int64 // NUnion
	ArmIdx int
	Arm    *ir.UnionVariant

	// Guard marks a bool field an `if` back-references: STRUCTURE (§2.7), so
	// the pin sets it and vary never touches it.
	Guard bool

	// Counted is true for [..N] / [Min..N] arrays and for string/bytes: their
	// count or used length is a structure field the pin sets and vary holds.
	Counted bool

	// Vary is the LCG mapping for this node's value, nil where the node is
	// structure (a count, a used length, a union tag, a branch gate).
	Vary *VarySpec
}

// Shape is one benchmarked message: the struct, and the instance the golden
// pinned.
type Shape struct {
	Struct *ir.Struct
	Golden string // testdata/wire basename, without .bin
	Bytes  int64  // the golden's size
	Nodes  []*Node
	Unit   *ir.Unit
}

type decoder struct {
	u *ir.Unit
	r *bitReader
}

// Decode walks the schema's wire tree against the golden's bits and returns
// the pinned instance.
func Decode(u *ir.Unit, st *ir.Struct, golden []byte) ([]*Node, error) {
	d := &decoder{u: u, r: &bitReader{data: golden}}
	nodes, err := d.structNodes(st)
	if err != nil {
		return nil, err
	}
	if d.r.err != nil {
		return nil, d.r.err
	}
	// the golden is a whole shape: the reader must land inside the last byte
	if (d.r.pos+7)/8 != int64(len(golden)) {
		return nil, fmt.Errorf("%s: decoded %d bits of a %d byte golden", st.Name, d.r.pos, len(golden))
	}
	return nodes, nil
}

func (d *decoder) structNodes(st *ir.Struct) ([]*Node, error) {
	var out []*Node
	byName := map[string]*Node{}
	if err := d.items(st.Items, &out, byName); err != nil {
		return nil, err
	}
	return out, nil
}

func (d *decoder) items(items []ir.Item, out *[]*Node, byName map[string]*Node) error {
	for _, it := range items {
		switch n := it.(type) {
		case *ir.ConstItem:
			got := d.r.bigBits(int(n.Bits))
			if got.Cmp(n.Value) != 0 {
				return fmt.Errorf("const(%s, %d): golden carries %s", n.Value, n.Bits, got)
			}
		case *ir.ReservedItem:
			if d.r.bigBits(int(n.Bits)).Sign() != 0 {
				return fmt.Errorf("reserved(%d): golden carries nonzero", n.Bits)
			}
		case *ir.AlignItem:
			d.r.align()
		case *ir.FieldItem:
			node, err := d.field(n.F)
			if err != nil {
				return err
			}
			*out = append(*out, node)
			byName[n.F.Name] = node
		case *ir.Branch:
			cond, ok := byName[n.Cond]
			if !ok || cond.Val.Kind != VBool {
				return fmt.Errorf("branch on %q: not a decoded bool field", n.Cond)
			}
			cond.Guard = true
			cond.Vary = nil
			take := cond.Val.B
			if n.Neg {
				take = !take
			}
			body := n.Then
			if !take {
				body = n.Else
			}
			if err := d.items(body, out, byName); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unhandled wire item %T", it)
		}
		if d.r.err != nil {
			return d.r.err
		}
	}
	return nil
}

func (d *decoder) field(f *ir.Field) (*Node, error) {
	switch f.Array {
	case ir.ArrayNone:
		return d.element(f, -1)
	case ir.ArrayFixed:
		return d.array(f, f.ArrayBound, false)
	case ir.ArrayCounted:
		minV := big.NewInt(f.ArrayMin)
		maxV := big.NewInt(f.ArrayBound)
		raw := d.r.bits(bitsRequired(minV, maxV))
		count := f.ArrayMin + int64(raw)
		if count > f.ArrayBound {
			return nil, fmt.Errorf("%s: count %d over bound %d", f.Name, count, f.ArrayBound)
		}
		return d.array(f, count, true)
	}
	return nil, fmt.Errorf("%s: unknown array kind", f.Name)
}

func (d *decoder) array(f *ir.Field, count int64, counted bool) (*Node, error) {
	node := &Node{F: f, Count: count, Counted: counted}
	node.Kind = NArrayScalar
	if st, ok := f.Type.Ref.(*ir.Struct); ok && f.Type.Kind == ir.TNamed {
		node.Kind = NArrayStruct
		_ = st
	}
	for i := range count {
		el, err := d.element(f, int(i))
		if err != nil {
			return nil, err
		}
		node.Elems = append(node.Elems, el)
	}
	return node, nil
}

// element decodes one value of the field's type: the field itself when it is
// not an array, one element otherwise (index >= 0).
func (d *decoder) element(f *ir.Field, index int) (*Node, error) {
	n := &Node{F: f, Index: index}
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		n.Kind = NScalar
		n.Val = Value{Kind: VBool, B: d.r.bits(1) != 0}
	case ir.TBits:
		n.Kind = NScalar
		n.Val = Value{Kind: VInt, I: d.r.bigBits(t.Width)}
	case ir.TInt:
		n.Kind = NScalar
		if f.HasIntRange {
			bits := bitsRequired(f.IntMin, f.IntMax)
			off := d.r.bigBits(bits)
			v := new(big.Int).Add(f.IntMin, off)
			if v.Cmp(f.IntMax) > 0 {
				return nil, fmt.Errorf("%s: golden value %s over max %s", f.Name, v, f.IntMax)
			}
			kind := VInt
			if t.Width > 64 {
				kind = VWide
			}
			n.Val = Value{Kind: kind, I: v, Signed: t.Signed}
		} else {
			raw := d.r.bigBits(t.Width)
			if t.Signed && raw.Bit(t.Width-1) == 1 { // two's complement
				raw.Sub(raw, new(big.Int).Lsh(big.NewInt(1), uint(t.Width)))
			}
			kind := VInt
			if t.Width > 64 {
				kind = VWide
			}
			n.Val = Value{Kind: kind, I: raw, Signed: t.Signed}
		}
	case ir.TFloat32:
		n.Kind = NScalar
		if f.HasFloatRange {
			// the runtime's decode, in float32 exactly as the generated
			// readers spell it: normalized = integer / steps, then scaled
			iv := d.r.bits(bitsRequired(big.NewInt(0), big.NewInt(f.Steps)))
			if int64(iv) > f.Steps {
				return nil, fmt.Errorf("%s: compressed value %d over %d steps", f.Name, iv, f.Steps)
			}
			normalized := float32(iv) / float32(f.Steps)
			n.Val = Value{Kind: VF32, F32: float32(normalized*float32(f.FMax-f.FMin)) + float32(f.FMin)}
		} else {
			n.Val = Value{Kind: VF32, F32: math.Float32frombits(uint32(d.r.bits(32)))}
		}
	case ir.TFloat64:
		n.Kind = NScalar
		n.Val = Value{Kind: VF64, F64: math.Float64frombits(d.r.bits(64))}
	case ir.TFixed:
		n.Kind = NScalar
		rawMin, rawMax := fixedRawRange(f)
		bits := bitsRequired(rawMin, rawMax)
		off := d.r.bigBits(bits)
		raw := new(big.Int).Add(rawMin, off)
		if raw.Cmp(rawMax) > 0 {
			return nil, fmt.Errorf("%s: fixed raw %s over %s", f.Name, raw, rawMax)
		}
		n.Val = Value{Kind: VFixed, I: raw, Signed: t.Signed}
	case ir.TString, ir.TBytes:
		n.Kind = NString
		if t.Kind == ir.TBytes {
			n.Kind = NBytes
		}
		n.Counted = true
		length := int64(d.r.bits(bitsRequired(big.NewInt(0), big.NewInt(t.Size))))
		if length > t.Size {
			return nil, fmt.Errorf("%s: used length %d over %d", f.Name, length, t.Size)
		}
		n.Count = length
		n.Buf = d.r.readBytes(length)
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Struct:
			n.Kind = NStruct
			sub, err := d.structNodes(ref)
			if err != nil {
				return nil, err
			}
			n.Sub = sub
		case *ir.Enum:
			n.Kind = NScalar
			v := d.r.bits(bitsRequired(big.NewInt(0), big.NewInt(ref.Max)))
			if int64(v) > ref.Max {
				return nil, fmt.Errorf("%s: enum value %d over %d", f.Name, v, ref.Max)
			}
			n.Val = Value{Kind: VEnum, I: new(big.Int).SetUint64(v)}
		case *ir.Flags:
			n.Kind = NScalar
			n.Val = Value{Kind: VFlags, I: d.r.bigBits(ref.WireBits)}
		case *ir.Union:
			n.Kind = NUnion
			n.Counted = true
			tag := int64(d.r.bits(bitsRequired(big.NewInt(0), big.NewInt(ref.Max))))
			if tag > ref.Max {
				return nil, fmt.Errorf("%s: union tag %d over %d", f.Name, tag, ref.Max)
			}
			n.Tag = tag
			if tag >= 1 {
				n.ArmIdx = int(tag - 1)
				n.Arm = &ref.Variants[n.ArmIdx]
				sub, err := d.structNodes(n.Arm.Ref)
				if err != nil {
					return nil, err
				}
				n.Sub = sub
			}
		default:
			return nil, fmt.Errorf("%s: unhandled named type", f.Name)
		}
	default:
		return nil, fmt.Errorf("%s: unhandled field kind", f.Name)
	}
	return n, d.r.err
}

// fixedRawRange is the runtime's fixed-point offset domain: the whole-unit
// bounds scaled by 2^fraction, which is what the storage integer carries.
func fixedRawRange(f *ir.Field) (rawMin, rawMax *big.Int) {
	minV, maxV := big.NewInt(0), big.NewInt(0)
	if f.HasIntRange {
		minV, maxV = f.IntMin, f.IntMax
	}
	rawMin = new(big.Int).Lsh(minV, uint(f.Type.FracBits))
	rawMax = new(big.Int).Lsh(maxV, uint(f.Type.FracBits))
	return rawMin, rawMax
}
