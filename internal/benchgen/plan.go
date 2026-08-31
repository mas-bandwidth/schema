package benchgen

import (
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// VaryForm is the shape of one field's LCG mapping. The forms are derived from
// the field's declared wire type — never from a per-shape table — so a schema
// edit re-derives the mapping with the shape (issue #191).
type VaryForm int

const (
	VaryUnsigned   VaryForm = iota // masked extraction assigned raw: bits, uintN, enum, flags, a byte
	VaryRanged                     // min + masked extraction: ranged ints, fixed-point raw units
	VaryBool                       // one bit
	VaryF32Raw                     // a small integral float32
	VaryF64Raw                     // a small half-integral float64
	VaryCompressed                 // a quantized step inside a compressed float's range
	VaryWide                       // 128 bit: both halves from the LCG word
	VaryAscii                      // one ASCII letter, for a string's used bytes
)

// VarySpec is one field's mapping. Every form draws from the SAME LCG word the
// §2.2 scheme threads through the write loop; only the extraction differs.
//
// The extraction is always in range by construction: a masked draw of the
// largest power-of-two sub-range that fits inside [min, max] cannot leave the
// declared bounds, so a varied instance always writes and always writes the
// same number of bits (the runner asserts that — §2.7).
type VarySpec struct {
	Form    VaryForm
	Shift   int  // constant right shift of the LCG word
	PerElem bool // inside an array: the shift is (Shift + i) & 31
	Mask    uint64
	FullU64 bool // the mask is the whole 64 bit word (no mask term)
	Width   int
	Min     *big.Int // VaryRanged
	FMin    float64  // VaryCompressed
	FScale  float64  // VaryCompressed: one step of the masked draw
	Signed  bool
}

// planner assigns each value field a distinct shift of the LCG word.
type planner struct {
	k int
}

// shiftFor spreads the fields across the word: distinct, deterministic, and
// always leaving the field's own width inside the 64 bits.
func (p *planner) shiftFor(width int, perElem bool) int {
	span := 65 - width
	if perElem {
		// the element's own index rides the shift, so keep the base low
		// enough that (base + i) & 31 never walks past the word
		span = min(span, 32)
	}
	span = max(span, 1)
	s := (p.k*13 + 3) % span
	p.k++
	return s
}

// Plan assigns the LCG mapping to every VALUE node of a decoded shape.
// STRUCTURE — array counts, string and bytes used lengths, the union tag, a
// branch gate — is deliberately skipped: holding it fixed is what keeps
// bytes/op constant under variation (§2.7).
func Plan(nodes []*Node) {
	p := &planner{}
	p.walk(nodes, false)
}

func (p *planner) walk(nodes []*Node, inArray bool) {
	for _, n := range nodes {
		p.node(n, inArray)
	}
}

func (p *planner) node(n *Node, inArray bool) {
	switch n.Kind {
	case NScalar:
		if n.Guard {
			return // the branch gate is structure
		}
		n.Vary = p.scalar(n, inArray)
	case NStruct:
		p.walk(n.Sub, inArray)
	case NUnion:
		// the tag is structure; the selected arm's payload varies
		p.walk(n.Sub, inArray)
	case NString:
		// the used length is structure; one used byte varies, ASCII so the
		// writer's UTF-8 validation keeps passing
		if n.Count > 0 {
			n.Vary = &VarySpec{Form: VaryAscii, Shift: p.shiftFor(4, false), Mask: 15, Width: 4}
		}
	case NBytes:
		if n.Count > 0 {
			n.Vary = &VarySpec{Form: VaryUnsigned, Shift: p.shiftFor(8, false), Mask: 0xFF, Width: 8}
		}
	case NArrayScalar:
		// element 0 stands for the buffer: a scalar array is bulk bytes on the
		// wire, and varying every element would charge the write loop for
		// harness work rather than serialize work
		if len(n.Elems) > 0 {
			n.Elems[0].Vary = p.scalar(n.Elems[0], false)
		}
	case NArrayStruct:
		// Every element varies, at stride 2 through its value fields in wire
		// order: §2.7's family convention of a representative subset, made
		// mechanical. The stride keeps the mapping's cost proportional to the
		// element — a full-field mapping over an 80 element array would
		// measure the vary function instead of the serializer.
		for _, el := range n.Elems {
			if el.Index != 0 {
				continue // one plan for the element shape, replayed per element
			}
			p.strided(el, 2)
		}
		// the remaining elements share element 0's plan (the loop body is
		// emitted once, with the index riding the shift)
		for _, el := range n.Elems[1:] {
			copyPlan(n.Elems[0], el)
		}
	}
}

// strided plans every stride'th value field of a struct element.
func (p *planner) strided(n *Node, stride int) {
	i := 0
	var walk func(nodes []*Node)
	walk = func(nodes []*Node) {
		for _, c := range nodes {
			switch c.Kind {
			case NScalar:
				if c.Guard {
					continue
				}
				if i%stride == 0 {
					c.Vary = p.scalar(c, true)
				}
				i++
			case NStruct, NUnion:
				walk(c.Sub)
			}
		}
	}
	if n.Kind == NStruct {
		walk(n.Sub)
		return
	}
	walk([]*Node{n})
}

// copyPlan replays element 0's plan onto the other elements, so a renderer can
// emit ONE loop body for the whole array.
func copyPlan(src, dst *Node) {
	dst.Vary = src.Vary
	for i := range src.Sub {
		if i < len(dst.Sub) {
			copyPlan(src.Sub[i], dst.Sub[i])
		}
	}
	for i := range src.Elems {
		if i < len(dst.Elems) {
			copyPlan(src.Elems[i], dst.Elems[i])
		}
	}
}

func (p *planner) scalar(n *Node, perElem bool) *VarySpec {
	f := n.F
	t := f.Type
	switch t.Kind {
	case ir.TBool:
		return &VarySpec{Form: VaryBool, Shift: p.shiftFor(1, perElem), Mask: 1, Width: 1, PerElem: perElem}
	case ir.TBits:
		return maskedDraw(p, VaryUnsigned, min(t.Width, 64), perElem, nil)
	case ir.TInt:
		if f.HasIntRange {
			if t.Width > 64 {
				// a range only 128 bits holds: a nonnegative draw out of the
				// word's low half is inside it and costs one shift
				spec := maskedDraw(p, VaryWide, 48, false, nil)
				spec.Signed = true
				return spec
			}
			width := subRangeWidth(f.IntMin, f.IntMax)
			return maskedDraw(p, VaryRanged, width, perElem, f.IntMin)
		}
		if t.Width > 64 {
			// both halves of a bare 128 bit field come off the same word
			return maskedDraw(p, VaryWide, 64, false, nil)
		}
		width := t.Width
		if t.Signed {
			width-- // stay nonnegative: a raw signed field takes the low half
		}
		return maskedDraw(p, VaryUnsigned, width, perElem, nil)
	case ir.TFloat32:
		if f.HasFloatRange {
			width := subRangeWidth(big.NewInt(0), big.NewInt(f.Steps))
			spec := maskedDraw(p, VaryCompressed, width, perElem, nil)
			spec.FMin = f.FMin
			spec.FScale = (f.FMax - f.FMin) / float64(uint64(1)<<uint(width))
			return spec
		}
		return maskedDraw(p, VaryF32Raw, 16, perElem, nil)
	case ir.TFloat64:
		return maskedDraw(p, VaryF64Raw, 24, perElem, nil)
	case ir.TFixed:
		rawMin, rawMax := fixedRawRange(f)
		width := subRangeWidth(rawMin, rawMax)
		return maskedDraw(p, VaryRanged, width, perElem, rawMin)
	case ir.TNamed:
		switch ref := t.Ref.(type) {
		case *ir.Enum:
			width := subRangeWidth(big.NewInt(0), big.NewInt(ref.Max))
			return maskedDraw(p, VaryUnsigned, width, perElem, nil)
		case *ir.Flags:
			return maskedDraw(p, VaryUnsigned, ref.WireBits, perElem, nil)
		}
	}
	return nil
}

func maskedDraw(p *planner, form VaryForm, width int, perElem bool, minV *big.Int) *VarySpec {
	width = max(min(width, 64), 1)
	spec := &VarySpec{
		Form:    form,
		Width:   width,
		Shift:   p.shiftFor(width, perElem),
		PerElem: perElem,
		Min:     minV,
	}
	if width >= 64 {
		spec.FullU64 = true
		spec.Mask = ^uint64(0)
	} else {
		spec.Mask = uint64(1)<<uint(width) - 1
	}
	return spec
}

// subRangeWidth is the width of the largest power-of-two sub-range that fits
// inside [min, max]: a masked draw of that width, offset by min, is always in
// range, whatever the declared bounds are.
func subRangeWidth(minV, maxV *big.Int) int {
	size := new(big.Int).Sub(maxV, minV)
	size.Add(size, big.NewInt(1))
	if size.Sign() <= 0 {
		return 1
	}
	return max(size.BitLen()-1, 1)
}

// ValueNodes returns the nodes a renderer folds into the sink or compares in a
// check: every decoded field, structure included (a count the read decoded is
// as much a decoded field as the values it bounds).
func ValueNodes(nodes []*Node) []*Node { return nodes }
