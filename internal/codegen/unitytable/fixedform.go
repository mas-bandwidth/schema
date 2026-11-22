package unitytable

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"slices"
	"sort"
	"strings"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type FixedLineageEntry struct {
	Wire             uint64
	Layout           []byte
	Digest           []byte
	Record           int64
	Retired          bool
	Reason           string
	Writer           *ir.Struct
	LegacyLayoutOnly bool
}

func FixedLineageOf(u *ir.Unit, name string) (FixedLineageEntry, bool) {
	for _, st := range ir.TableFixedRoots(u) {
		if st.Name != name {
			continue
		}
		layout, hash := ownLayout(st)
		return FixedLineageEntry{
			Wire:   hash,
			Layout: layout,
			Digest: ir.TableFixedDefinitionsDigest(st),
			Record: 8 + ir.TableFixedTypeBytes(st),
			Writer: st,
		}, true
	}
	return FixedLineageEntry{}, false
}

const (
	opCopy          = 1
	opWidenS        = 2
	opWidenU        = 3
	opBool          = 4
	opRangeS        = 5
	opRangeU        = 6
	opWidenF        = 7
	opConst         = 8
	opCountOp       = 9
	opTextOp        = 10
	opOrdinal       = 11
	opDead          = 12
	opCountCheck    = 13
	opBytesCheck    = 14
	opStringCheck   = 15
	opF32Finite     = 16
	opF32Range      = 17
	opF64Finite     = 18
	opF64Range      = 19
	opRangeS128     = 20
	opRangeU128     = 21
	opLoopFixed     = 22
	opLoopCounted   = 23
	fixedGuardEq    = 1
	fixedGuardGt    = 2
	fixedPlanMaxCap = 4096
)

type fixedGuard struct {
	at    int64
	width int64
	value uint64
	mode  int
}

type fixedFill struct {
	dst      int64
	guardOff int
	guardLen int
	image    []byte
}

type fixedOp struct {
	code        int
	src, dst    int64
	size, width int64
	lo, hi      uint64
	guardOff    int
	guardLen    int
	lo2, hi2    uint64
	guards      []fixedGuard
}

func (o fixedOp) cs() string {
	if o.guardLen == 0 && o.lo2 == 0 && o.hi2 == 0 {
		return fmt.Sprintf("new TableWire.FixedOp(%d, %d, %d, %d, %d, 0x%016xUL, 0x%016xUL)",
			o.code, o.src, o.dst, o.size, o.width, o.lo, o.hi)
	}
	if o.lo2 == 0 && o.hi2 == 0 {
		return fmt.Sprintf("new TableWire.FixedOp(%d, %d, %d, %d, %d, 0x%016xUL, 0x%016xUL, %d, %d)",
			o.code, o.src, o.dst, o.size, o.width, o.lo, o.hi, o.guardOff, o.guardLen)
	}
	return fmt.Sprintf("new TableWire.FixedOp(%d, %d, %d, %d, %d, 0x%016xUL, 0x%016xUL, %d, %d, 0x%016xUL, 0x%016xUL)",
		o.code, o.src, o.dst, o.size, o.width, o.lo, o.hi, o.guardOff, o.guardLen, o.lo2, o.hi2)
}

func FixedCarried(u *ir.Unit, st *ir.Struct) bool {
	if st == nil || len(st.Fields) == 0 || !st.CarriesForm("wire") {
		return false
	}
	if st.Integrity != "" && st.Integrity != "crc32c" {
		return false
	}
	return slices.Contains(ir.TableFixedFormRoots(u), st) && ir.TableFixedEmitted(u, st) && hasFixedStorageClosure(u, st)
}

func ownLayout(st *ir.Struct) ([]byte, uint64) {
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	return layout, ir.TableFixedLayoutHash(layout, st)
}

type fixedSpec struct {
	name       string
	id         uint64
	kind       int
	shape      string
	size       int64
	bound      int64
	minimum    int64
	counted    bool
	signed     bool
	hasRange   bool
	loBig      *big.Int
	hiBig      *big.Int
	fmin, fmax float64
	defBool    bool
	defInt     *big.Int
	defFloat   float64
	defBytes   []byte
	tagBytes   int64
	variants   []uint64
	deprecated map[uint64]bool
	keys       []uint64
	children   []*fixedSpec
}

func buildFixedSpecRoot(st *ir.Struct) *fixedSpec {
	return &fixedSpec{
		name:     st.Name,
		id:       ir.TableWireId(st.WireName()),
		kind:     ir.TableKindTable,
		shape:    "table",
		size:     ir.TableFixedTypeBytes(st),
		children: buildFixedSpecFields(st),
	}
}

func buildFixedSpecFields(st *ir.Struct) []*fixedSpec {
	out := make([]*fixedSpec, len(st.Fields))
	for i, f := range st.Fields {
		out[i] = buildFixedSpecField(f, false)
	}
	return out
}

func buildFixedSpecField(f *ir.Field, inElement bool) *fixedSpec {
	s := &fixedSpec{
		name: f.Name,
		id:   ir.TableFieldWireId(f),
		size: ir.TableFixedFieldBytes(f),
	}
	if f.Type.Optional {
		inner := *f
		inner.Type.Optional = false
		s.kind = 35
		s.shape = "optional"
		s.children = []*fixedSpec{buildFixedSpecField(&inner, inElement)}
		return s
	}
	if f.KeyEnum != "" {
		elem := *f
		elem.KeyEnum = ""
		elem.KeyEnumRef = nil
		elem.Array = ir.ArrayNone
		elem.ArrayBound = 0
		elem.ArrayMin = 0
		keys := make([]uint64, len(f.KeyEnumRef.Variants))
		for i := range keys {
			keys[i] = ir.TableWireId(f.KeyEnumRef.VariantWireName(i))
		}
		s.kind = ir.TableKindKeyed
		s.shape = "keyed"
		s.bound = f.KeyEnumRef.Max
		s.keys = keys
		s.children = []*fixedSpec{buildFixedSpecField(&elem, true)}
		return s
	}
	if f.Array == ir.ArrayFixed || f.Array == ir.ArrayCounted {
		elem := *f
		elem.Array = ir.ArrayNone
		elem.ArrayBound = 0
		elem.ArrayMin = 0
		s.kind = ir.TableKindArray
		s.shape = "array"
		s.bound = f.ArrayBound
		s.minimum = f.ArrayMin
		s.counted = f.Array == ir.ArrayCounted
		s.children = []*fixedSpec{buildFixedSpecField(&elem, true)}
		return s
	}
	s.kind = ir.TableFixedLeafKind(f)
	switch f.Type.Kind {
	case ir.TBool:
		s.shape = "bool"
		s.defBool = !inElement && f.HasDefault && f.DefBool
	case ir.TInt, ir.TBits, ir.TFixed:
		s.shape = "int"
		s.signed = f.Type.Kind != ir.TBits && f.Type.Signed
		if f.Type.Kind == ir.TBits {
			w := ir.TableFixedStorageBytes(f.Type)
			if int64(f.Type.Width) < 8*w {
				s.hasRange = true
				s.loBig = new(big.Int)
				s.hiBig = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
			}
		} else if lo, hi, ok := ir.TableRawRange(f); ok {
			s.hasRange = true
			s.loBig = new(big.Int).Set(lo)
			s.hiBig = new(big.Int).Set(hi)
		}
		s.defInt = new(big.Int)
		if !inElement && f.HasDefault && f.DefInt != nil {
			s.defInt.Set(f.DefInt)
		}
	case ir.TFloat32, ir.TFloat64:
		s.shape = "float"
		if !inElement && f.HasDefault {
			s.defFloat = f.DefFloat
		}
		if f.HasFloatRange {
			s.hasRange = true
			s.fmin = f.FMin
			s.fmax = f.FMax
		}
	case ir.TString:
		s.shape = "string"
		s.bound = f.Type.Size
		if !inElement && f.HasDefault {
			s.defBytes = append([]byte(nil), f.DefBytes...)
		}
	case ir.TBytes:
		s.kind = ir.TableKindArray
		s.shape = "bytes"
		s.bound = f.Type.Size
		if !inElement && f.HasDefault {
			s.defBytes = append([]byte(nil), f.DefBytes...)
		}
	case ir.TNamed:
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			s.kind = ir.TableKindTable
			s.shape = "table"
			s.children = buildFixedSpecFields(r)
		case *ir.Union:
			s.kind = ir.TableKindUnion
			s.shape = "union"
			s.tagBytes = ir.TableFixedUnionTagBytes(r)
			s.children = make([]*fixedSpec, len(r.Variants))
			for i, v := range r.Variants {
				if v.F == nil {
					s.children[i] = &fixedSpec{
						name:  v.Name,
						id:    ir.TableWireId(v.WireName()),
						kind:  ir.TableKindNoPayload,
						shape: "void",
						size:  0,
					}
					continue
				}
				arm := buildFixedSpecField(v.F, false)
				arm.id = ir.TableWireId(v.WireName())
				arm.name = v.Name
				s.children[i] = arm
			}
		case *ir.Enum:
			s.kind = ir.TableKindEnum
			s.shape = "enum"
			s.variants = make([]uint64, len(r.Variants))
			s.deprecated = map[uint64]bool{}
			for i := range r.Variants {
				s.variants[i] = ir.TableWireId(r.VariantWireName(i))
				if enumVariantDeprecated(r, i) {
					s.deprecated[uint64(i+1)] = true
				}
			}
			s.defInt = new(big.Int)
			if !inElement && f.HasDefault && f.DefVariant != "" {
				for i, vn := range r.Variants {
					if vn == f.DefVariant {
						s.defInt.SetInt64(int64(i + 1))
						break
					}
				}
			}
		case *ir.Flags:
			s.shape = "int"
			s.signed = false
			// A MASK BIT AT OR ABOVE THE READER'S W REFUSES THE LOAD out_of_range
			// (docs/SPEC-TABLES.md §4, A TABLE LOAD HOLDS A MASK TO ITS WIDTH), on
			// the reader's own image after any lineage plan, as every range is
			if r.WireBits < 64 {
				s.hasRange = true
				s.loBig = new(big.Int)
				s.hiBig = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(r.WireBits)), big.NewInt(1))
			}
			s.defInt = new(big.Int)
			if !inElement && f.HasDefault && f.DefInt != nil {
				s.defInt.Set(f.DefInt)
			}
		}
	}
	return s
}

func seedSpecBytes(s *fixedSpec) []byte {
	out := make([]byte, s.size)
	seedSpecInto(s, out, 0)
	return out
}

func seedSpecInto(s *fixedSpec, out []byte, at int64) {
	switch s.shape {
	case "table":
		cur := at
		for _, c := range s.children {
			seedSpecInto(c, out, cur)
			cur += c.size
		}
	case "array", "keyed":
		cnt := s.bound
		cur := at
		if s.counted {
			cnt = s.minimum
			binary.LittleEndian.PutUint32(out[cur:cur+4], uint32(cnt))
			cur += 4
		}
		elem := s.children[0]
		for i := range cnt {
			seedSpecInto(elem, out, cur+i*elem.size)
		}
	case "bool":
		if s.defBool {
			out[at] = 1
		}
	case "int", "enum":
		if s.defInt == nil || s.defInt.Sign() == 0 {
			return
		}
		u := new(big.Int).Set(s.defInt)
		if u.Sign() < 0 {
			u.Add(u, new(big.Int).Lsh(big.NewInt(1), uint(8*s.size)))
		}
		for i := range s.size {
			out[at+i] = byte(new(big.Int).Rsh(u, uint(8*i)).Uint64())
		}
	case "float":
		if s.size == 4 {
			v := float32(s.defFloat)
			if s.hasRange && v == 0 {
				v = 0
			}
			binary.LittleEndian.PutUint32(out[at:at+4], math.Float32bits(v))
		} else {
			v := s.defFloat
			if s.hasRange && v == 0 {
				v = 0
			}
			binary.LittleEndian.PutUint64(out[at:at+8], math.Float64bits(v))
		}
	case "string", "bytes":
		n := len(s.defBytes)
		binary.LittleEndian.PutUint32(out[at:at+4], uint32(n))
		copy(out[at+4:], s.defBytes)
	}
}

type fixedLayoutNode struct {
	id       uint64
	kind     int
	size     int64
	children []*fixedLayoutNode
}

func parseFixedLayoutTree(data []byte) (*fixedLayoutNode, bool) {
	if lockfile.ValidateLayout(data) != "" {
		return nil, false
	}
	at := 0
	var walk func() *fixedLayoutNode
	walk = func() *fixedLayoutNode {
		b := data[4+at*17:]
		at++
		n := &fixedLayoutNode{
			id:   binary.LittleEndian.Uint64(b),
			kind: int(b[8]),
			size: int64(binary.LittleEndian.Uint32(b[9:])),
		}
		for range binary.LittleEndian.Uint32(b[13:]) {
			n.children = append(n.children, walk())
		}
		return n
	}
	return walk(), true
}

func fixedLadderWidens(from, to int) bool {
	inRun := func(lo, hi int) bool { return from >= lo && from < to && to <= hi }
	return inRun(2, 5) || inRun(6, 9) || inRun(20, 24) || inRun(25, 29) || (from == 10 && to == 11)
}

func fixedKindSigned(kind int) bool {
	return (kind >= 2 && kind <= 5) || kind == 18 || (kind >= 20 && kind <= 24)
}

type rawFill struct {
	dst    int64
	guards []fixedGuard
	image  []byte
}

func guardsEqual(a, b []fixedGuard) bool {
	return slices.Equal(a, b)
}

func compileFixedPlan(writer *fixedLayoutNode, reader *fixedSpec) ([]fixedOp, []fixedGuard, []fixedFill, []uint16, bool, int, string) {
	var ops []fixedOp
	var rawFills []rawFill
	var remapPool []uint16
	needPrefill := false
	unknown := 0
	why := ""

	emit := func(code int, src, dst, size, width int64, lo, hi uint64, guards []fixedGuard) {
		ops = append(ops, fixedOp{
			code:   code,
			src:    src,
			dst:    dst,
			size:   size,
			width:  width,
			lo:     lo,
			hi:     hi,
			guards: append([]fixedGuard(nil), guards...),
		})
	}

	var walk func(w *fixedLayoutNode, r *fixedSpec, src, dst int64, guards []fixedGuard, localDefaults, census bool, depth int)
	walk = func(w *fixedLayoutNode, r *fixedSpec, src, dst int64, guards []fixedGuard, localDefaults, census bool, depth int) {
		if why != "" {
			return
		}
		if depth > ir.TableFixedMaxDepth {
			why = "TableReport.LayoutMalformed"
			return
		}
		if r.kind == 35 && w.kind != 35 {
			emit(opConst, 0, dst, 1, 0, 1, 0, guards)
			walk(w, r.children[0], src, dst+1, guards, true, census, depth+1)
			return
		}
		if r.kind != w.kind {
			if fixedLadderWidens(w.kind, r.kind) && w.size <= r.size {
				code := opWidenU
				if w.kind == 10 && r.kind == 11 {
					code = opWidenF
				} else if fixedKindSigned(w.kind) {
					code = opWidenS
				}
				emit(code, src, dst, w.size, r.size, 0, 0, guards)
				return
			}
			why = "TableReport.LayoutMalformed"
			return
		}
		switch r.shape {
		case "table":
			type hit struct {
				node *fixedLayoutNode
				at   int64
			}
			theirs := map[uint64]hit{}
			at := src
			for _, c := range w.children {
				if _, dup := theirs[c.id]; dup {
					why = "TableReport.LayoutMalformed"
					return
				}
				theirs[c.id] = hit{c, at}
				at += c.size
			}
			mine := map[uint64]bool{}
			at = dst
			for _, c := range r.children {
				mine[c.id] = true
				if h, ok := theirs[c.id]; ok {
					walk(h.node, c, h.at, at, guards, localDefaults, census, depth+1)
				} else if localDefaults {
					rawFills = append(rawFills, rawFill{
						dst:    at,
						guards: append([]fixedGuard(nil), guards...),
						image:  seedSpecBytes(c),
					})
				} else {
					needPrefill = true
				}
				at += c.size
			}
			if census {
				for _, c := range w.children {
					if !mine[c.id] {
						unknown++
					}
				}
			}
		case "optional":
			if len(w.children) != 1 {
				why = "TableReport.LayoutMalformed"
				return
			}
			emit(opCopy, src, dst, 1, 1, 0, 0, guards)
			gs := append(append([]fixedGuard(nil), guards...), fixedGuard{at: src, width: 1, value: 1, mode: fixedGuardEq})
			walk(w.children[0], r.children[0], src+1, dst+1, gs, true, census, depth+1)
		case "array":
			elem := r.children[0]
			if elem.size <= 0 || len(w.children) != 1 {
				why = "TableReport.LayoutMalformed"
				return
			}
			we := w.children[0]
			if we.size <= 0 {
				why = "TableReport.LayoutMalformed"
				return
			}
			head := int64(0)
			if r.counted {
				head = 4
			}
			n := (w.size - head) / we.size
			if n > r.bound || w.size < head || (w.size-head)%we.size != 0 {
				why = "TableReport.LayoutMalformed"
				return
			}
			if head != 0 {
				emit(opCountOp, src, dst, n, 4, 0, 0, guards)
			}
			for i := range n {
				gs := guards
				if head != 0 {
					gs = append(append([]fixedGuard(nil), guards...), fixedGuard{at: src, width: 4, value: uint64(i), mode: fixedGuardGt})
				}
				walk(we, elem, src+head+i*we.size, dst+head+i*elem.size, gs, localDefaults || head != 0, census && i == 0, depth+1)
			}
			if head == 0 && n < r.bound {
				if localDefaults {
					elemImg := seedSpecBytes(elem)
					for i := n; i < r.bound; i++ {
						rawFills = append(rawFills, rawFill{
							dst:    dst + i*elem.size,
							guards: append([]fixedGuard(nil), guards...),
							image:  elemImg,
						})
					}
				} else {
					needPrefill = true
				}
			}
		case "keyed":
			elem := r.children[0]
			if elem.size <= 0 || len(w.children) != 2 {
				why = "TableReport.LayoutMalformed"
				return
			}
			tkeys := w.children[0].children
			we := w.children[1]
			if we.size <= 0 || w.size != int64(len(tkeys))*we.size {
				why = "TableReport.LayoutMalformed"
				return
			}
			theirIdx := map[uint64]int{}
			for j, tk := range tkeys {
				if _, dup := theirIdx[tk.id]; dup {
					why = "TableReport.LayoutMalformed"
					return
				}
				theirIdx[tk.id] = j
			}
			firstKey := true
			var elemImg []byte
			for k, keyID := range r.keys {
				if j, ok := theirIdx[keyID]; ok {
					walk(we, elem, src+int64(j)*we.size, dst+int64(k)*elem.size, guards, localDefaults, census && firstKey, depth+1)
					firstKey = false
				} else if localDefaults {
					if elemImg == nil {
						elemImg = seedSpecBytes(elem)
					}
					rawFills = append(rawFills, rawFill{
						dst:    dst + int64(k)*elem.size,
						guards: append([]fixedGuard(nil), guards...),
						image:  elemImg,
					})
				} else {
					needPrefill = true
				}
			}
		case "bytes":
			if len(w.children) != 1 || w.children[0].kind != ir.TableKindU8 || w.children[0].size != 1 || w.size < 4 || w.size > r.size {
				why = "TableReport.LayoutMalformed"
				return
			}
			emit(opTextOp, src, dst, w.size-4, 0, 0, 0, guards)
		case "string":
			if len(w.children) != 0 || w.size < 4 || w.size > r.size {
				why = "TableReport.LayoutMalformed"
				return
			}
			emit(opTextOp, src, dst, w.size-4, 0, 0, 0, guards)
		case "union":
			widest := int64(0)
			theirArm := map[uint64]int{}
			for j, c := range w.children {
				if _, dup := theirArm[c.id]; dup {
					why = "TableReport.LayoutMalformed"
					return
				}
				theirArm[c.id] = j
				widest = max(widest, c.size)
			}
			wtag := w.size - widest
			if wtag != 1 && wtag != 2 && wtag != 4 && wtag != 8 {
				why = "TableReport.LayoutMalformed"
				return
			}
			emit(opConst, 0, dst, r.tagBytes, 0, 0, 0, guards)
			for k, arm := range r.children {
				j, ok := theirArm[arm.id]
				if !ok {
					continue
				}
				gs := append(append([]fixedGuard(nil), guards...), fixedGuard{at: src, width: wtag, value: uint64(j + 1), mode: fixedGuardEq})
				emit(opConst, 0, dst, r.tagBytes, 0, uint64(k+1), 0, gs)
				walk(w.children[j], arm, src+wtag, dst+r.tagBytes, gs, true, census, depth+1)
			}
		case "enum":
			if len(w.children) > 65535 {
				why = "TableReport.PlanTooLarge"
				return
			}
			readerIdx := map[uint64]uint16{}
			for k, id := range r.variants {
				readerIdx[id] = uint16(k + 1)
			}
			identityMap := true
			table := make([]uint16, len(w.children))
			seen := map[uint64]bool{}
			for j, c := range w.children {
				if seen[c.id] {
					why = "TableReport.LayoutMalformed"
					return
				}
				seen[c.id] = true
				mapped := readerIdx[c.id]
				table[j] = mapped
				if mapped != uint16(j+1) {
					identityMap = false
				}
			}
			switch {
			case identityMap && w.size <= r.size:
				emit(opOrdinal, src, dst, w.size, r.size, 0, 0, guards)
			case w.size <= r.size:
				off := uint64(len(remapPool))
				remapPool = append(remapPool, table...)
				emit(opOrdinal, src, dst, w.size, r.size, off, uint64(len(table)), guards)
			default:
				why = "TableReport.LayoutMalformed"
			}
		case "void":
		default:
			switch {
			case w.size == r.size:
				emit(opCopy, src, dst, w.size, w.size, 0, 0, guards)
			case w.size < r.size && r.size <= 16:
				code := opWidenU
				if fixedKindSigned(w.kind) {
					code = opWidenS
				}
				emit(code, src, dst, w.size, r.size, 0, 0, guards)
			default:
				why = "TableReport.LayoutMalformed"
			}
		}
	}

	walk(writer, reader, 0, 0, nil, false, true, 0)
	if why != "" {
		return nil, nil, nil, nil, false, 0, why
	}

	var ordered []fixedOp
	for _, guarded := range []bool{false, true} {
		for _, o := range ops {
			if (len(o.guards) > 0) == guarded {
				ordered = append(ordered, o)
			}
		}
	}
	var coalesced []fixedOp
	for _, o := range ordered {
		if n := len(coalesced); n > 0 && o.code == opCopy && coalesced[n-1].code == opCopy &&
			coalesced[n-1].src+coalesced[n-1].size == o.src &&
			coalesced[n-1].dst+coalesced[n-1].size == o.dst &&
			guardsEqual(coalesced[n-1].guards, o.guards) {
			coalesced[n-1].size += o.size
			coalesced[n-1].width = coalesced[n-1].size
			continue
		}
		coalesced = append(coalesced, o)
	}
	if len(coalesced)+len(rawFills) > fixedPlanMaxCap {
		return nil, nil, nil, nil, false, 0, "TableReport.PlanTooLarge"
	}

	var guardPool []fixedGuard
	internGuards := func(gs []fixedGuard) (int, int) {
		if len(gs) == 0 {
			return 0, 0
		}
		off := len(guardPool)
		guardPool = append(guardPool, gs...)
		return off, len(gs)
	}
	for i := range coalesced {
		coalesced[i].guardOff, coalesced[i].guardLen = internGuards(coalesced[i].guards)
		coalesced[i].guards = nil
	}
	fills := make([]fixedFill, len(rawFills))
	for i, rf := range rawFills {
		off, gl := internGuards(rf.guards)
		fills[i] = fixedFill{dst: rf.dst, guardOff: off, guardLen: gl, image: rf.image}
	}
	return coalesced, guardPool, fills, remapPool, needPrefill, unknown, ""
}

func big128Halves(v *big.Int) (lo, hi uint64) {
	u := new(big.Int).Set(v)
	if u.Sign() < 0 {
		u.Add(u, new(big.Int).Lsh(big.NewInt(1), 128))
	}
	lo = new(big.Int).And(u, maxU64Big).Uint64()
	hi = new(big.Int).Rsh(u, 64).Uint64()
	return lo, hi
}

func compileFixedChecks(spec *fixedSpec) ([]fixedOp, []fixedGuard) {
	var guardPool []fixedGuard
	internGuards := func(gs []fixedGuard) (int, int) {
		if len(gs) == 0 {
			return 0, 0
		}
		off := len(guardPool)
		guardPool = append(guardPool, gs...)
		return off, len(gs)
	}

	var build func(s *fixedSpec, at int64, guards []fixedGuard) []fixedOp
	build = func(s *fixedSpec, at int64, guards []fixedGuard) []fixedOp {
		var out []fixedOp
		add := func(o fixedOp, gs []fixedGuard) {
			o.guardOff, o.guardLen = internGuards(gs)
			out = append(out, o)
		}
		switch s.shape {
		case "table":
			cur := at
			for _, c := range s.children {
				out = append(out, build(c, cur, guards)...)
				cur += c.size
			}
		case "optional":
			add(fixedOp{code: opBool, src: at, size: 1, width: 1}, guards)
			if s.size > 1 {
				gZero := append(append([]fixedGuard(nil), guards...), fixedGuard{at: at, width: 1, value: 0, mode: fixedGuardEq})
				add(fixedOp{code: opDead, src: at + 1, size: s.size - 1}, gZero)
			}
			gOne := append(append([]fixedGuard(nil), guards...), fixedGuard{at: at, width: 1, value: 1, mode: fixedGuardEq})
			out = append(out, build(s.children[0], at+1, gOne)...)
		case "array", "keyed":
			elem := s.children[0]
			if s.counted {
				add(fixedOp{
					code:  opCountCheck,
					src:   at,
					dst:   at + s.size,
					width: elem.size,
					lo:    uint64(s.minimum),
					hi:    uint64(s.bound),
				}, guards)
				sub := build(elem, 0, nil)
				if len(sub) > 0 && s.bound > 0 {
					add(fixedOp{
						code:  opLoopCounted,
						src:   at,
						dst:   int64(len(sub)),
						size:  s.bound,
						width: elem.size,
					}, guards)
					out = append(out, sub...)
				}
			} else if s.bound > 0 {
				sub := build(elem, 0, nil)
				if len(sub) > 0 {
					if s.bound == 1 {
						out = append(out, build(elem, at, guards)...)
					} else {
						add(fixedOp{
							code:  opLoopFixed,
							src:   at,
							dst:   int64(len(sub)),
							size:  s.bound,
							width: elem.size,
						}, guards)
						out = append(out, sub...)
					}
				}
			}
		case "union":
			maxTag := uint64(len(s.children))
			add(fixedOp{code: opRangeU, src: at, size: s.tagBytes, width: s.tagBytes, lo: 0, hi: maxTag}, guards)
			if s.tagBytes < s.size {
				gNone := append(append([]fixedGuard(nil), guards...), fixedGuard{at: at, width: s.tagBytes, value: 0, mode: fixedGuardEq})
				add(fixedOp{code: opDead, src: at + s.tagBytes, size: s.size - s.tagBytes}, gNone)
			}
			for i, arm := range s.children {
				gArm := append(append([]fixedGuard(nil), guards...), fixedGuard{at: at, width: s.tagBytes, value: uint64(i + 1), mode: fixedGuardEq})
				if s.tagBytes+arm.size < s.size {
					add(fixedOp{code: opDead, src: at + s.tagBytes + arm.size, size: s.size - s.tagBytes - arm.size}, gArm)
				}
				out = append(out, build(arm, at+s.tagBytes, gArm)...)
			}
		case "bool":
			add(fixedOp{code: opBool, src: at, size: 1, width: 1}, guards)
		case "enum":
			add(fixedOp{code: opRangeU, src: at, size: s.size, width: s.size, lo: 0, hi: uint64(len(s.variants))}, guards)
		case "int":
			if s.hasRange {
				switch {
				case s.size == 16:
					lo1, lo2 := big128Halves(s.loBig)
					hi1, hi2 := big128Halves(s.hiBig)
					code := opRangeU128
					if s.signed {
						code = opRangeS128
					}
					add(fixedOp{code: code, src: at, size: 16, width: 16, lo: lo1, lo2: lo2, hi: hi1, hi2: hi2}, guards)
				case s.signed:
					add(fixedOp{code: opRangeS, src: at, size: s.size, width: s.size, lo: uint64(s.loBig.Int64()), hi: uint64(s.hiBig.Int64())}, guards)
				default:
					add(fixedOp{code: opRangeU, src: at, size: s.size, width: s.size, lo: s.loBig.Uint64(), hi: s.hiBig.Uint64()}, guards)
				}
			}
		case "float":
			if s.size == 4 {
				if s.hasRange {
					add(fixedOp{
						code:  opF32Range,
						src:   at,
						size:  4,
						width: 4,
						lo:    uint64(math.Float32bits(float32(s.fmin))),
						hi:    uint64(math.Float32bits(float32(s.fmax))),
					}, guards)
				} else {
					add(fixedOp{code: opF32Finite, src: at, size: 4, width: 4}, guards)
				}
			} else {
				if s.hasRange {
					add(fixedOp{
						code:  opF64Range,
						src:   at,
						size:  8,
						width: 8,
						lo:    math.Float64bits(s.fmin),
						hi:    math.Float64bits(s.fmax),
					}, guards)
				} else {
					add(fixedOp{code: opF64Finite, src: at, size: 8, width: 8}, guards)
				}
			}
		case "string":
			add(fixedOp{code: opStringCheck, src: at, size: s.size, hi: uint64(s.bound)}, guards)
		case "bytes":
			add(fixedOp{code: opBytesCheck, src: at, size: s.size, hi: uint64(s.bound)}, guards)
		}
		return out
	}

	checks := build(spec, 0, nil)
	return checks, guardPool
}

type fixedLane struct {
	hash        uint64
	layout      []byte
	record      int64
	why         string
	identity    bool
	prefill     bool
	widens      int
	unknown     int
	plan        int
	hasGuards   bool
	ops         []fixedOp
	checks      []fixedOp
	guards      []fixedGuard
	checkGuards []fixedGuard
	fills       []fixedFill
	remap       []uint16
}

type fixedRoot struct {
	st          *ir.Struct
	spec        *fixedSpec
	layout      []byte
	digest      []byte
	hash        uint64
	body        int64
	offsets     []int64
	lanes       []fixedLane
	floor       int
	planMax     int
	prefill     bool
	defaultData []byte
}

func (r *fixedRoot) planned() bool {
	for i, lane := range r.lanes {
		if !lane.identity && i >= r.floor && lane.why == "" {
			return true
		}
	}
	return false
}

func fieldOffsets(st *ir.Struct) []int64 {
	out := make([]int64, len(st.Fields))
	off := int64(0)
	for i, f := range st.Fields {
		out[i] = off
		off += ir.TableFixedFieldBytes(f)
	}
	return out
}

func buildFixedRoot(_ *ir.Unit, st *ir.Struct, lineage []FixedLineageEntry) (*fixedRoot, error) {
	spec := buildFixedSpecRoot(st)
	r := &fixedRoot{
		st:          st,
		spec:        spec,
		body:        ir.TableFixedTypeBytes(st),
		offsets:     fieldOffsets(st),
		digest:      ir.TableFixedDefinitionsDigest(st),
		defaultData: seedSpecBytes(spec),
	}
	r.layout, r.hash = ownLayout(st)
	entries := append([]FixedLineageEntry(nil), lineage...)
	if n := len(entries); n == 0 || entries[n-1].Wire != r.hash {
		entries = append(entries, FixedLineageEntry{
			Wire:   r.hash,
			Layout: r.layout,
			Digest: r.digest,
			Record: 8 + r.body,
			Writer: st,
		})
	}
	for i, e := range entries {
		if e.Retired {
			r.floor = i + 1
		}
	}
	ownChecks, ownCheckGuards := compileFixedChecks(spec)
	identity := fixedLane{
		hash:        r.hash,
		layout:      r.layout,
		record:      8 + r.body,
		identity:    true,
		plan:        1,
		checks:      ownChecks,
		checkGuards: ownCheckGuards,
	}
	for i, e := range entries {
		if i < r.floor {
			r.lanes = append(r.lanes, fixedLane{hash: e.Wire, layout: e.Layout, record: e.Record})
			continue
		}
		ownFacts := e.Wire == r.hash && bytes.Equal(e.Layout, r.layout) && (len(e.Digest) == 0 || bytes.Equal(e.Digest, r.digest)) && !e.LegacyLayoutOnly
		if ownFacts && (e.Writer == nil || e.Writer == st) {
			r.lanes = append(r.lanes, identity)
			continue
		}
		lane, err := lineageLane(st, r, e, ownFacts)
		if err != nil {
			return nil, err
		}
		r.lanes = append(r.lanes, lane)
	}
	for _, lane := range r.lanes {
		r.planMax = max(r.planMax, lane.plan)
		r.prefill = r.prefill || lane.prefill
	}
	return r, nil
}

func validateWriterSnapshot(st *ir.Struct, e FixedLineageEntry) error {
	w := e.Writer
	wlayout, _ := ownLayout(w)
	if !bytes.Equal(wlayout, e.Layout) || 8+ir.TableFixedTypeBytes(w) != e.Record {
		return fmt.Errorf("fixed root %s: the writer snapshot of lineage entry 0x%016x does not state the entry's own layout, hash and record size (docs/FIXED-FORM-ALGORITHM.md §5.2)", st.Name, e.Wire)
	}
	if e.LegacyLayoutOnly {
		if len(e.Digest) != 0 || ir.TableFixedLayoutHashRecorded(wlayout, nil) != e.Wire {
			return fmt.Errorf("fixed root %s: the writer snapshot of lineage entry 0x%016x does not state the entry's own layout, hash and record size (docs/FIXED-FORM-ALGORITHM.md §5.2)", st.Name, e.Wire)
		}
		return nil
	}
	recorded, err := ir.TableFixedDefinitionsDigestForRecorded(w, e.Digest)
	if err != nil || !bytes.Equal(recorded, e.Digest) || ir.TableFixedLayoutHashRecorded(wlayout, e.Digest) != e.Wire {
		return fmt.Errorf("fixed root %s: the writer snapshot of lineage entry 0x%016x does not state the entry's own layout, hash and record size (docs/FIXED-FORM-ALGORITHM.md §5.2)", st.Name, e.Wire)
	}
	return nil
}

func lineageLane(st *ir.Struct, r *fixedRoot, e FixedLineageEntry, ownFacts bool) (fixedLane, error) {
	lane := fixedLane{hash: e.Wire, layout: e.Layout, record: e.Record}
	node, ok := parseFixedLayoutTree(e.Layout)
	if !ok || node == nil || e.Record != 8+node.size {
		lane.why = "TableReport.LayoutMalformed"
		return lane, nil
	}
	if binary.LittleEndian.Uint32(e.Layout[0:4]) > fixedPlanMaxCap {
		lane.why = "TableReport.PlanTooLarge"
		lane.plan = int(binary.LittleEndian.Uint32(e.Layout[0:4]))
		return lane, nil
	}
	ops, guards, fills, remap, needPrefill, unknown, why := compileFixedPlan(node, r.spec)
	if why != "" {
		lane.why = why
		return lane, nil
	}
	writerSt := e.Writer
	if writerSt == nil {
		if !ownFacts || e.LegacyLayoutOnly {
			return lane, fmt.Errorf("fixed root %s: lineage entry 0x%016x needs its checked writer declaration snapshot to be read on --lang unity: the bounds pass holds an older file to the writer's own bounds (docs/FIXED-FORM-ALGORITHM.md §4.6, §5.2); supply it with `schema lock --writer`, or retire the entry", st.Name, e.Wire)
		}
		writerSt = st
	} else if err := validateWriterSnapshot(st, e); err != nil {
		return lane, err
	}
	if ownFacts {
		checks, checkGuards := compileFixedChecks(r.spec)
		return fixedLane{
			hash:        r.hash,
			layout:      r.layout,
			record:      8 + r.body,
			identity:    true,
			plan:        1,
			checks:      checks,
			checkGuards: checkGuards,
		}, nil
	}
	checks, checkGuards := compileFixedChecks(buildFixedSpecRoot(writerSt))
	lane.ops = ops
	lane.guards = guards
	lane.fills = fills
	lane.remap = remap
	lane.checks = checks
	lane.checkGuards = checkGuards
	lane.prefill = needPrefill
	lane.unknown = unknown
	lane.hasGuards = len(guards) > 0 || len(fills) > 0
	for _, o := range ops {
		if o.code == opWidenS || o.code == opWidenU || o.code == opWidenF || (o.code == opOrdinal && o.size < o.width) {
			lane.widens++
		}
	}
	lane.plan = len(ops) + len(fills)
	if lane.prefill && len(fills) == 0 {
		lane.plan++
	}
	return lane, nil
}

func byteArray(b *strings.Builder, data []byte) {
	if len(data) == 0 {
		b.WriteString("Array.Empty<byte>()")
		return
	}
	b.WriteString("new byte[]\n{\n")
	for i := 0; i < len(data); i += 16 {
		b.WriteString("    ")
		for j, v := range data[i:min(i+16, len(data))] {
			if j > 0 {
				b.WriteString(" ")
			}
			fmt.Fprintf(b, "0x%02x,", v)
		}
		b.WriteString("\n")
	}
	b.WriteString("}")
}

func opArray(b *strings.Builder, ops []fixedOp) {
	if len(ops) == 0 {
		b.WriteString("Array.Empty<TableWire.FixedOp>()")
		return
	}
	b.WriteString("new TableWire.FixedOp[]\n{\n")
	for _, o := range ops {
		fmt.Fprintf(b, "    %s,\n", o.cs())
	}
	b.WriteString("}")
}

func guardArray(b *strings.Builder, guards []fixedGuard) {
	if len(guards) == 0 {
		b.WriteString("Array.Empty<TableWire.FixedGuard>()")
		return
	}
	b.WriteString("new TableWire.FixedGuard[]\n{\n")
	for _, g := range guards {
		fmt.Fprintf(b, "    new TableWire.FixedGuard(%d, %d, 0x%016xUL, %d),\n", g.at, g.width, g.value, g.mode)
	}
	b.WriteString("}")
}

func fillArray(b *strings.Builder, fills []fixedFill) {
	if len(fills) == 0 {
		b.WriteString("Array.Empty<TableWire.FixedFill>()")
		return
	}
	b.WriteString("new TableWire.FixedFill[]\n{\n")
	for _, fl := range fills {
		var img strings.Builder
		byteArray(&img, fl.image)
		fmt.Fprintf(b, "    new TableWire.FixedFill(%d, %d, %d, %s),\n", fl.dst, fl.guardOff, fl.guardLen, strings.ReplaceAll(img.String(), "\n", " "))
	}
	b.WriteString("}")
}

func ushortArray(b *strings.Builder, vals []uint16) {
	if len(vals) == 0 {
		b.WriteString("Array.Empty<ushort>()")
		return
	}
	b.WriteString("new ushort[]\n{\n")
	for i := 0; i < len(vals); i += 16 {
		b.WriteString("    ")
		for j, v := range vals[i:min(i+16, len(vals))] {
			if j > 0 {
				b.WriteString(" ")
			}
			fmt.Fprintf(b, "%d,", v)
		}
		b.WriteString("\n")
	}
	b.WriteString("}")
}

func emitFixed(b *strings.Builder, u *ir.Unit, r *fixedRoot) {
	st, name := r.st, r.st.Name
	crc := st.Integrity == "crc32c"
	fmt.Fprintf(b, "// ---- %s, the fixed form (form 3, docs/SPEC-TABLES.md §3.4) ----\n\n", name)
	fmt.Fprintf(b, "public const ulong %sFixedHash = 0x%016xUL;\n", name, r.hash)
	fmt.Fprintf(b, "public const int %sFixedBodyBytes = %d;\n", name, r.body)
	fmt.Fprintf(b, "public const int %sFixedRecordBytes = 8 + %sFixedBodyBytes;\n", name, name)
	fmt.Fprintf(b, "public const int %sFixedPlanMax = %d;\n", name, r.planMax)
	fmt.Fprintf(b, "private const int %sFixedFloor = %d;\n\n", name, r.floor)
	fmt.Fprintf(b, "private static readonly byte[] %sFixedLayout = ", name)
	byteArray(b, r.layout)
	b.WriteString(";\n\n")
	if r.prefill {
		fmt.Fprintf(b, "private static readonly byte[] %sFixedDefaults = ", name)
		byteArray(b, r.defaultData)
		b.WriteString(";\n\n")
	}
	fmt.Fprintf(b, "private static readonly TableWire.FixedLane[] %sFixedLanes = new TableWire.FixedLane[]\n{\n", name)
	for i, lane := range r.lanes {
		var l strings.Builder
		why := "0"
		if lane.why != "" {
			why = lane.why
		}
		layout := name + "FixedLayout"
		var lb strings.Builder
		if !lane.identity {
			byteArray(&lb, lane.layout)
			layout = lb.String()
		}
		note := "this build's own"
		switch {
		case i < r.floor:
			note = "retired: below the floor, no plan"
		case lane.why != "":
			note = "not a lawful widening of this reader"
		case !lane.identity:
			note = "an older writer's"
		}
		fmt.Fprintf(&l, "// 0x%016x — %s\n", lane.hash, note)
		simple := !lane.hasGuards && lane.unknown == 0 && len(lane.guards) == 0 && len(lane.checkGuards) == 0 && len(lane.fills) == 0 && len(lane.remap) == 0
		if simple {
			fmt.Fprintf(&l, "new TableWire.FixedLane(0x%016xUL, %d, %s, %t, %t, %d, %d,\n",
				lane.hash, lane.record, why, lane.identity, lane.prefill, lane.widens, lane.plan)
			l.WriteString(indent(layout + ","))
			var ops strings.Builder
			opArray(&ops, lane.ops)
			l.WriteString(indent(ops.String() + ","))
			var checks strings.Builder
			opArray(&checks, lane.checks)
			l.WriteString(indent(checks.String() + "),"))
		} else {
			fmt.Fprintf(&l, "new TableWire.FixedLane(0x%016xUL, %d, %s, %t, %t, %d, %d, %d, %t,\n",
				lane.hash, lane.record, why, lane.identity, lane.prefill, lane.widens, lane.unknown, lane.plan, lane.hasGuards)
			l.WriteString(indent(layout + ","))
			var ops strings.Builder
			opArray(&ops, lane.ops)
			l.WriteString(indent(ops.String() + ","))
			var checks strings.Builder
			opArray(&checks, lane.checks)
			l.WriteString(indent(checks.String() + ","))
			var gs strings.Builder
			guardArray(&gs, lane.guards)
			l.WriteString(indent(gs.String() + ","))
			var cgs strings.Builder
			guardArray(&cgs, lane.checkGuards)
			l.WriteString(indent(cgs.String() + ","))
			var fls strings.Builder
			fillArray(&fls, lane.fills)
			l.WriteString(indent(fls.String() + ","))
			var rmp strings.Builder
			ushortArray(&rmp, lane.remap)
			l.WriteString(indent(rmp.String() + "),"))
		}
		b.WriteString(indent(l.String()))
	}
	b.WriteString("};\n\n")

	fmt.Fprintf(b, "public static long %sFixedMeasure(long count)\n{\n", name)
	if crc {
		fmt.Fprintf(b, "    long head = TableWire.FixedHead + %sFixedLayout.Length + 4;\n", name)
	} else {
		fmt.Fprintf(b, "    long head = TableWire.FixedHead + %sFixedLayout.Length;\n", name)
	}
	fmt.Fprintf(b, "    if (count < 1 || count > (long.MaxValue - head) / %sFixedRecordBytes) { return -1; }\n", name)
	fmt.Fprintf(b, "    return head + count * %sFixedRecordBytes;\n}\n\n", name)

	fmt.Fprintf(b, "public static long %sFixedSave(ReadOnlySpan<%s> values, Span<byte> buffer)\n{\n", name, name)
	var save strings.Builder
	save.WriteString("Debug.Assert(values.Length > 0, \"a fixed-form file holds at least one record\");\n")
	fmt.Fprintf(&save, "long need = %sFixedMeasure(values.Length);\n", name)
	save.WriteString("if (need < 0 || buffer.Length < need) { return -1; }\n")
	save.WriteString("buffer.Slice(0, TableWire.FixedHead).Clear();\n")
	save.WriteString("buffer[0] = TableWire.FixedForm;\n")
	fmt.Fprintf(&save, "TableWire.FixedPut(buffer, 8, 8, %sFixedHash);\n", name)
	fmt.Fprintf(&save, "TableWire.FixedPut(buffer, 16, 4, (ulong)%sFixedLayout.Length);\n", name)
	fmt.Fprintf(&save, "%sFixedLayout.CopyTo(buffer.Slice(TableWire.FixedHead));\n", name)
	fmt.Fprintf(&save, "Span<byte> at = buffer.Slice(TableWire.FixedHead + %sFixedLayout.Length);\n", name)
	save.WriteString("for (int k = 0; k < values.Length; k++)\n{\n")
	fmt.Fprintf(&save, "    Span<byte> record = at.Slice(k * %sFixedRecordBytes, %sFixedRecordBytes);\n", name, name)
	fmt.Fprintf(&save, "    TableWire.FixedPut(record, 0, 8, %sFixedHash);\n", name)
	save.WriteString("    Span<byte> body = record.Slice(8);\n    body.Clear();\n")
	fmt.Fprintf(&save, "    %sFixedStore(body, in values[k]);\n}\n", name)
	if crc {
		save.WriteString("BinaryPrimitives.WriteUInt32LittleEndian(buffer.Slice((int)need - 4, 4), TableWire.IntegrityCrc(buffer.Slice(0, (int)need - 4)));\n")
	}
	save.WriteString("return need;\n")
	b.WriteString(indent(save.String()))
	b.WriteString("}\n\n")

	fmt.Fprintf(b, "public static long %sFixedLoad(Span<%s> values, ReadOnlySpan<byte> data, ref TableReport report)\n{\n", name, name)
	fmt.Fprintf(b, "    return %sFixedLoad(values, data, ref report, %sFixedPlanMax);\n}\n\n", name, name)

	fmt.Fprintf(b, "public static long %sFixedLoad(Span<%s> values, ReadOnlySpan<byte> data, ref TableReport report, int planCapacity)\n{\n", name, name)
	var load strings.Builder
	load.WriteString("report = default;\n")
	if crc {
		fmt.Fprintf(&load, "int why = TableWire.FixedOpen(data, %sFixedLanes, %sFixedFloor, values.Length, planCapacity, out int pick, out int n, out ulong hash, true);\n", name, name)
	} else {
		fmt.Fprintf(&load, "int why = TableWire.FixedOpen(data, %sFixedLanes, %sFixedFloor, values.Length, planCapacity, out int pick, out int n, out ulong hash);\n", name, name)
	}
	load.WriteString("if (why != TableReport.Accepted)\n{\n")
	load.WriteString("    report.Refused = true;\n    report.Reason = why;\n")
	load.WriteString("    if (why == TableReport.LayoutUnknown || why == TableReport.LayoutUnsupported) { report.LayoutHash = hash; }\n")
	load.WriteString("    return -1;\n}\n")
	dataStart := fmt.Sprintf("TableWire.FixedHead + %sFixedLayout.Length", name)
	identityLoop := fmt.Sprintf("for (int k = 0; k < n; k++)\n{\n    %sFixedDecode(at.Slice(k * %sFixedRecordBytes + 8, %sFixedBodyBytes), ref values[k]);\n}\nreturn n;\n", name, name, name)
	if !r.planned() {
		fmt.Fprintf(&load, "ReadOnlySpan<byte> at = data.Slice(%s);\n", dataStart)
		load.WriteString(identityLoop)
	} else {
		fmt.Fprintf(&load, "TableWire.FixedLane lane = %sFixedLanes[pick];\n", name)
		load.WriteString("ReadOnlySpan<byte> at = data.Slice(TableWire.FixedHead + lane.Layout.Length);\n")
		load.WriteString("if (lane.Identity)\n{\n")
		load.WriteString(indent(identityLoop))
		load.WriteString("}\n")
		if r.body <= 512 {
			fmt.Fprintf(&load, "Span<byte> image = stackalloc byte[%sFixedBodyBytes];\n", name)
		} else {
			fmt.Fprintf(&load, "Span<byte> image = new byte[%sFixedBodyBytes];\n", name)
		}
		load.WriteString("int dynWidened = 0;\n")
		load.WriteString("for (int k = 0; k < n; k++)\n{\n")
		if r.prefill {
			fmt.Fprintf(&load, "    if (lane.Prefill) { %sFixedDefaults.CopyTo(image); }\n", name)
			load.WriteString("    else { image.Clear(); }\n")
		} else {
			load.WriteString("    image.Clear();\n")
		}
		load.WriteString("    dynWidened += TableWire.FixedApply(lane, at.Slice(k * lane.RecordBytes + 8, lane.RecordBytes - 8), image);\n")
		fmt.Fprintf(&load, "    %sFixedDecode(image, ref values[k]);\n}\n", name)
		load.WriteString("report.Unknown = lane.Unknown;\nif (!lane.HasGuards)\n{\n    report.Widened = lane.Widens * n;\n}\nelse\n{\n    report.Widened = dynWidened;\n}\nreturn n;\n")
	}
	b.WriteString(indent(load.String()))
	b.WriteString("}\n\n")

	fmt.Fprintf(b, "private static void %sFixedStore(Span<byte> body, in %s value)\n{\n", name, name)
	var store strings.Builder
	emitFixedStructStore(&store, u, st, "value", 0)
	b.WriteString(indent(store.String()))
	b.WriteString("}\n\n")

	fmt.Fprintf(b, "private static void %sFixedDecode(ReadOnlySpan<byte> body, ref %s value)\n{\n", name, name)
	var dec strings.Builder
	fmt.Fprintf(&dec, "Init%s(ref value);\n", name)
	emitFixedStructDecode(&dec, u, st, "value", 0)
	b.WriteString(indent(dec.String()))
	b.WriteString("}\n\n")
}

func emitFixedUnitHelpers(b *strings.Builder, u *ir.Unit) {
	seenS := map[string]bool{}
	seenU := map[string]bool{}
	var structs []*ir.Struct
	var unions []*ir.Union

	var walkField func(f *ir.Field)
	var walkStruct func(st *ir.Struct)
	walkField = func(f *ir.Field) {
		if f == nil || f.Type.Kind != ir.TNamed {
			return
		}
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			if !seenS[r.Name] {
				seenS[r.Name] = true
				walkStruct(r)
				structs = append(structs, r)
			}
		case *ir.Union:
			if !seenU[r.Name] {
				seenU[r.Name] = true
				for _, v := range r.Variants {
					walkField(v.F)
				}
				unions = append(unions, r)
			}
		}
	}
	walkStruct = func(st *ir.Struct) {
		for _, f := range st.Fields {
			walkField(f)
		}
	}

	roots := ir.TableFixedFormRoots(u)
	sort.Slice(roots, func(i, j int) bool { return roots[i].Name < roots[j].Name })
	for _, st := range roots {
		if FixedCarried(u, st) {
			walkStruct(st)
		}
	}

	for _, st := range structs {
		fmt.Fprintf(b, "internal static void FixedStore_%s(Span<byte> body, in %s value)\n{\n", st.Name, st.Name)
		var sb strings.Builder
		emitFixedStructStore(&sb, u, st, "value", 0)
		b.WriteString(indent(sb.String()))
		b.WriteString("}\n\n")

		fmt.Fprintf(b, "internal static void FixedDecode_%s(ReadOnlySpan<byte> body, ref %s value)\n{\n", st.Name, st.Name)
		var db strings.Builder
		if structHasInitFn(st) {
			fmt.Fprintf(&db, "Init%s(ref value);\n", st.Name)
		} else {
			db.WriteString("value = default;\n")
		}
		emitFixedStructDecode(&db, u, st, "value", 0)
		b.WriteString(indent(db.String()))
		b.WriteString("}\n\n")
	}

	for _, un := range unions {
		tagBytes := ir.TableFixedUnionTagBytes(un)
		fmt.Fprintf(b, "internal static void FixedStore_%s(Span<byte> body, in %s value)\n{\n", un.Name, un.Name)
		var sb strings.Builder
		fmt.Fprintf(&sb, "Debug.Assert((ulong)value.Type <= %dUL, \"union %s tag outside declared arms\");\n", len(un.Variants), un.Name)
		fmt.Fprintf(&sb, "TableWire.FixedPut(body, 0, %d, (ulong)value.Type);\n", tagBytes)
		sb.WriteString("switch (value.Type)\n{\n")
		for _, v := range un.Variants {
			armName := ir.GoExportName(v.Name)
			fmt.Fprintf(&sb, "    case %sType.%s:\n    {\n", un.Name, armName)
			if !v.Void() {
				var ab strings.Builder
				emitFixedFieldStore(&ab, u, v.F, "value."+armName, fmt.Sprintf("body.Slice(%d)", tagBytes), 0, 1)
				sb.WriteString(indent(indent(ab.String())))
			}
			sb.WriteString("        break;\n    }\n")
		}
		sb.WriteString("}\n")
		b.WriteString(indent(sb.String()))
		b.WriteString("}\n\n")

		fmt.Fprintf(b, "internal static void FixedDecode_%s(ReadOnlySpan<byte> body, ref %s value)\n{\n", un.Name, un.Name)
		var db strings.Builder
		db.WriteString("value = default;\n")
		fmt.Fprintf(&db, "ulong tag = TableWire.FixedGet(body, 0, %d);\n", tagBytes)
		fmt.Fprintf(&db, "value.Type = (%sType)tag;\n", un.Name)
		db.WriteString("switch (value.Type)\n{\n")
		for _, v := range un.Variants {
			armName := ir.GoExportName(v.Name)
			fmt.Fprintf(&db, "    case %sType.%s:\n    {\n", un.Name, armName)
			if !v.Void() {
				var ab strings.Builder
				if inner, ok := v.F.Type.Ref.(*ir.Struct); ok && v.F.Type.Kind == ir.TNamed && v.F.Array == ir.ArrayNone && structHasInitFn(inner) {
					fmt.Fprintf(&ab, "Init%s(ref value.%s);\n", inner.Name, armName)
				}
				emitFixedFieldDecode(&ab, u, v.F, "value."+armName, fmt.Sprintf("body.Slice(%d)", tagBytes), 0, 1)
				db.WriteString(indent(indent(ab.String())))
			}
			db.WriteString("        break;\n    }\n")
		}
		db.WriteString("}\n")
		b.WriteString(indent(db.String()))
		b.WriteString("}\n\n")
	}
}

func emitFixedStructStore(b *strings.Builder, u *ir.Unit, st *ir.Struct, val string, depth int) {
	off := int64(0)
	for _, f := range st.Fields {
		emitFixedFieldStore(b, u, f, val+"."+member(f), "body", off, depth)
		off += ir.TableFixedFieldBytes(f)
	}
}

func emitFixedFieldStore(b *strings.Builder, u *ir.Unit, f *ir.Field, expr, buf string, off int64, depth int) {
	if f.Type.Optional {
		fmt.Fprintf(b, "if (%sPresent)\n{\n", expr)
		var inner strings.Builder
		fmt.Fprintf(&inner, "%s[%d] = 1;\n", buf, off)
		sub := *f
		sub.Type.Optional = false
		emitFixedFieldStore(&inner, u, &sub, expr, buf, off+1, depth+1)
		b.WriteString(indent(inner.String()))
		b.WriteString("}\n")
		return
	}
	switch {
	case f.Type.Kind == ir.TString:
		n := f.Type.Size
		fmt.Fprintf(b, "{\n    int slen = %sLength;\n", expr)
		fmt.Fprintf(b, "    Debug.Assert(slen >= 0 && slen <= %d, \"%s length outside [0, %d]\");\n", n, f.Name, n)
		fmt.Fprintf(b, "    Buffer%d<byte> sbuf = %s;\n", n, expr)
		fmt.Fprintf(b, "    Debug.Assert(TableWire.TextValid(sbuf.AsSpan().Slice(0, slen)), \"%s is not valid UTF-8\");\n", f.Name)
		fmt.Fprintf(b, "    TableWire.FixedPut(%s, %d, 4, (ulong)slen);\n", buf, off)
		fmt.Fprintf(b, "    if (slen > 0) { sbuf.AsSpan().Slice(0, slen).CopyTo(%s.Slice(%d, slen)); }\n}\n", buf, off+4)
	case f.Type.Kind == ir.TBytes:
		n := f.Type.Size
		fmt.Fprintf(b, "{\n    int blen = %sLength;\n", expr)
		fmt.Fprintf(b, "    Debug.Assert(blen >= 0 && blen <= %d, \"%s length outside [0, %d]\");\n", n, f.Name, n)
		fmt.Fprintf(b, "    Buffer%d<byte> bbuf = %s;\n", n, expr)
		fmt.Fprintf(b, "    TableWire.FixedPut(%s, %d, 4, (ulong)blen);\n", buf, off)
		fmt.Fprintf(b, "    if (blen > 0) { bbuf.AsSpan().Slice(0, blen).CopyTo(%s.Slice(%d, blen)); }\n}\n", buf, off+4)
	case f.Array == ir.ArrayFixed:
		elemBytes := ir.TableFixedElementBytes(f)
		iv := fmt.Sprintf("i%d", depth)
		ev := fmt.Sprintf("elem%d", depth)
		fmt.Fprintf(b, "for (int %s = 0; %s < %d; %s++)\n{\n", iv, iv, f.ArrayBound, iv)
		var loop strings.Builder
		fmt.Fprintf(&loop, "%s %s = %s[%s];\n", scalarStorageType(f.Type), ev, expr, iv)
		elemField := *f
		elemField.Array = ir.ArrayNone
		elemField.ArrayBound = 0
		elemField.KeyEnum = ""
		elemField.KeyEnumRef = nil
		emitFixedElementStore(&loop, u, &elemField, ev, fmt.Sprintf("%s.Slice(%d + %s * %d, %d)", buf, off, iv, elemBytes, elemBytes), 0, depth+1)
		b.WriteString(indent(loop.String()))
		b.WriteString("}\n")
	case f.Array == ir.ArrayCounted:
		elemBytes := ir.TableFixedElementBytes(f)
		iv := fmt.Sprintf("i%d", depth)
		ev := fmt.Sprintf("elem%d", depth)
		fmt.Fprintf(b, "{\n    int cnt = %sCount;\n", expr)
		fmt.Fprintf(b, "    Debug.Assert(cnt >= %d && cnt <= %d, \"%s count outside [%d, %d]\");\n", f.ArrayMin, f.ArrayBound, f.Name, f.ArrayMin, f.ArrayBound)
		fmt.Fprintf(b, "    TableWire.FixedPut(%s, %d, 4, (ulong)cnt);\n", buf, off)
		fmt.Fprintf(b, "    for (int %s = 0; %s < cnt; %s++)\n    {\n", iv, iv, iv)
		var loop strings.Builder
		fmt.Fprintf(&loop, "%s %s = %s[%s];\n", scalarStorageType(f.Type), ev, expr, iv)
		elemField := *f
		elemField.Array = ir.ArrayNone
		elemField.ArrayBound = 0
		elemField.ArrayMin = 0
		emitFixedElementStore(&loop, u, &elemField, ev, fmt.Sprintf("%s.Slice(%d + %s * %d, %d)", buf, off+4, iv, elemBytes, elemBytes), 0, depth+1)
		b.WriteString(indent(indent(loop.String())))
		b.WriteString("    }\n}\n")
	default:
		emitFixedElementStore(b, u, f, expr, buf, off, depth)
	}
}

func emitFixedElementStore(b *strings.Builder, _ *ir.Unit, f *ir.Field, expr, buf string, off int64, _ int) {
	size := ir.TableFixedElementBytes(f)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			fmt.Fprintf(b, "FixedStore_%s(%s.Slice(%d, %d), in %s);\n", r.Name, buf, off, size, expr)
			return
		case *ir.Union:
			fmt.Fprintf(b, "FixedStore_%s(%s.Slice(%d, %d), in %s);\n", r.Name, buf, off, size, expr)
			return
		case *ir.Enum:
			emitScalarAssert(b, f, expr)
			fmt.Fprintf(b, "TableWire.FixedPut(%s, %d, %d, (ulong)%s);\n", buf, off, size, expr)
			return
		case *ir.Flags:
			if r.WireBits < 64 {
				// the write holds the mask to its width W (docs/SPEC-TABLES.md §4), debug only
				fmt.Fprintf(b, "Debug.Assert(((ulong)%s >> %d) == 0, \"%s holds a flags bit at or above its width %d\");\n", expr, r.WireBits, f.Name, r.WireBits)
			}
			fmt.Fprintf(b, "TableWire.FixedPut(%s, %d, %d, (ulong)%s);\n", buf, off, size, expr)
			return
		}
	}
	emitScalarAssert(b, f, expr)
	switch f.Type.Kind {
	case ir.TBool:
		fmt.Fprintf(b, "%s[%d] = %s ? (byte)1 : (byte)0;\n", buf, off, expr)
	case ir.TFloat32:
		v := expr
		if f.HasFloatRange {
			v = fmt.Sprintf("(%s == 0.0f ? 0.0f : %s)", expr, expr)
		}
		fmt.Fprintf(b, "TableWire.FixedPutF32(%s, %d, %s);\n", buf, off, v)
	case ir.TFloat64:
		v := expr
		if f.HasFloatRange {
			v = fmt.Sprintf("(%s == 0.0 ? 0.0 : %s)", expr, expr)
		}
		fmt.Fprintf(b, "TableWire.FixedPutF64(%s, %d, %s);\n", buf, off, v)
	default:
		if f.Type.Width == 128 {
			fmt.Fprintf(b, "TableWire.FixedPut(%s, %d, 8, %s.Lo);\n", buf, off, expr)
			fmt.Fprintf(b, "TableWire.FixedPut(%s, %d, 8, %s.Hi);\n", buf, off+8, expr)
			return
		}
		if f.Type.Kind != ir.TBits && f.Type.Signed {
			fmt.Fprintf(b, "TableWire.FixedPut(%s, %d, %d, unchecked((ulong)(long)%s));\n", buf, off, size, expr)
			return
		}
		fmt.Fprintf(b, "TableWire.FixedPut(%s, %d, %d, (ulong)%s);\n", buf, off, size, expr)
	}
}

func emitFixedStructDecode(b *strings.Builder, u *ir.Unit, st *ir.Struct, val string, depth int) {
	off := int64(0)
	for _, f := range st.Fields {
		emitFixedFieldDecode(b, u, f, val+"."+member(f), "body", off, depth)
		off += ir.TableFixedFieldBytes(f)
	}
}

func emitFixedFieldDecode(b *strings.Builder, u *ir.Unit, f *ir.Field, expr, buf string, off int64, depth int) {
	if f.Type.Optional {
		fmt.Fprintf(b, "{\n    bool p = %s[%d] != 0;\n    %sPresent = p;\n    if (p)\n    {\n", buf, off, expr)
		var inner strings.Builder
		sub := *f
		sub.Type.Optional = false
		if innerSt, ok := sub.Type.Ref.(*ir.Struct); ok && sub.Type.Kind == ir.TNamed && sub.Array == ir.ArrayNone && structHasInitFn(innerSt) {
			fmt.Fprintf(&inner, "Init%s(ref %s);\n", innerSt.Name, expr)
		}
		emitFixedFieldDecode(&inner, u, &sub, expr, buf, off+1, depth+1)
		b.WriteString(indent(indent(inner.String())))
		b.WriteString("    }\n}\n")
		return
	}
	switch {
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		fmt.Fprintf(b, "{\n    int slen = (int)TableWire.FixedGet(%s, %d, 4);\n", buf, off)
		fmt.Fprintf(b, "    %s = default;\n    %sLength = slen;\n", expr, expr)
		fmt.Fprintf(b, "    if (slen > 0) { %s.Slice(%d, slen).CopyTo(%s.AsSpan()); }\n}\n", buf, off+4, expr)
	case f.Array == ir.ArrayFixed:
		elemBytes := ir.TableFixedElementBytes(f)
		iv := fmt.Sprintf("i%d", depth)
		sp := fmt.Sprintf("sp%d", depth)
		fmt.Fprintf(b, "{\n    Span<%s> %s = %s.AsSpan();\n", scalarStorageType(f.Type), sp, expr)
		fmt.Fprintf(b, "    for (int %s = 0; %s < %d; %s++)\n    {\n", iv, iv, f.ArrayBound, iv)
		var loop strings.Builder
		elemField := *f
		elemField.Array = ir.ArrayNone
		elemField.ArrayBound = 0
		elemField.KeyEnum = ""
		elemField.KeyEnumRef = nil
		elemField.HasDefault = false
		elemField.DefVariant = ""
		emitFixedElementDecode(&loop, u, &elemField, fmt.Sprintf("%s[%s]", sp, iv), fmt.Sprintf("%s.Slice(%d + %s * %d, %d)", buf, off, iv, elemBytes, elemBytes), 0, depth+1, true)
		b.WriteString(indent(indent(loop.String())))
		b.WriteString("    }\n}\n")
	case f.Array == ir.ArrayCounted:
		elemBytes := ir.TableFixedElementBytes(f)
		iv := fmt.Sprintf("i%d", depth)
		sp := fmt.Sprintf("sp%d", depth)
		fmt.Fprintf(b, "{\n    int cnt = (int)TableWire.FixedGet(%s, %d, 4);\n", buf, off)
		fmt.Fprintf(b, "    %s = default;\n    %sCount = cnt;\n", expr, expr)
		fmt.Fprintf(b, "    Span<%s> %s = %s.AsSpan();\n", scalarStorageType(f.Type), sp, expr)
		fmt.Fprintf(b, "    for (int %s = 0; %s < cnt; %s++)\n    {\n", iv, iv, iv)
		var loop strings.Builder
		elemField := *f
		elemField.Array = ir.ArrayNone
		elemField.ArrayBound = 0
		elemField.ArrayMin = 0
		elemField.HasDefault = false
		elemField.DefVariant = ""
		emitFixedElementDecode(&loop, u, &elemField, fmt.Sprintf("%s[%s]", sp, iv), fmt.Sprintf("%s.Slice(%d + %s * %d, %d)", buf, off+4, iv, elemBytes, elemBytes), 0, depth+1, true)
		b.WriteString(indent(indent(loop.String())))
		b.WriteString("    }\n}\n")
	default:
		emitFixedElementDecode(b, u, f, expr, buf, off, depth, false)
	}
}

func emitFixedElementDecode(b *strings.Builder, _ *ir.Unit, f *ir.Field, expr, buf string, off int64, _ int, inElement bool) {
	size := ir.TableFixedElementBytes(f)
	if f.Type.Kind == ir.TNamed {
		switch r := f.Type.Ref.(type) {
		case *ir.Struct:
			fmt.Fprintf(b, "FixedDecode_%s(%s.Slice(%d, %d), ref %s);\n", r.Name, buf, off, size, expr)
			return
		case *ir.Union:
			fmt.Fprintf(b, "FixedDecode_%s(%s.Slice(%d, %d), ref %s);\n", r.Name, buf, off, size, expr)
			return
		case *ir.Enum:
			var depConds []string
			for i := range r.Variants {
				if enumVariantDeprecated(r, i) {
					depConds = append(depConds, fmt.Sprintf("ev == %dUL", i+1))
				}
			}
			if len(depConds) > 0 {
				defExpr := r.Name + ".None"
				if !inElement && f.HasDefault && f.DefVariant != "" {
					defExpr = r.Name + "." + f.DefVariant
				}
				fmt.Fprintf(b, "{ ulong ev = TableWire.FixedGet(%s, %d, %d); %s = (%s) ? %s : (%s)ev; }\n",
					buf, off, size, expr, strings.Join(depConds, " || "), defExpr, r.Name)
				return
			}
			fmt.Fprintf(b, "%s = (%s)TableWire.FixedGet(%s, %d, %d);\n", expr, r.Name, buf, off, size)
			return
		case *ir.Flags:
			fmt.Fprintf(b, "%s = (%s)TableWire.FixedGet(%s, %d, %d);\n", expr, r.Name, buf, off, size)
			return
		}
	}
	switch f.Type.Kind {
	case ir.TBool:
		fmt.Fprintf(b, "%s = %s[%d] != 0;\n", expr, buf, off)
	case ir.TFloat32:
		if f.HasFloatRange {
			fmt.Fprintf(b, "{ float fv = TableWire.FixedGetF32(%s, %d); %s = fv == 0.0f ? 0.0f : fv; }\n", buf, off, expr)
		} else {
			fmt.Fprintf(b, "%s = TableWire.FixedGetF32(%s, %d);\n", expr, buf, off)
		}
	case ir.TFloat64:
		if f.HasFloatRange {
			fmt.Fprintf(b, "{ double dv = TableWire.FixedGetF64(%s, %d); %s = dv == 0.0 ? 0.0 : dv; }\n", buf, off, expr)
		} else {
			fmt.Fprintf(b, "%s = TableWire.FixedGetF64(%s, %d);\n", expr, buf, off)
		}
	default:
		if f.Type.Width == 128 {
			typ := "UInt128Value"
			if f.Type.Signed {
				typ = "Int128Value"
			}
			fmt.Fprintf(b, "%s = new %s(TableWire.FixedGet(%s, %d, 8), TableWire.FixedGet(%s, %d, 8));\n",
				expr, typ, buf, off+8, buf, off)
			return
		}
		fmt.Fprintf(b, "%s = unchecked((%s)TableWire.FixedGet(%s, %d, %d));\n", expr, scalarStorageType(f.Type), buf, off, size)
	}
}
