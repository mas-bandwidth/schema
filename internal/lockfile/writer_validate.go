package lockfile

import (
	"fmt"
	"math"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// Validate before invoking recursive IR layout helpers. Memoized expansion is
// capped by the already recorded layout, so a small cyclic or shared graph
// cannot turn into an unbounded layout walk. Extents are checked before any
// multiplication; all named references were resolved by the snapshot parser.
func validateWriterGraph(root *ir.Struct, maxEntries int) error {
	type info struct {
		nodes, bytes int64
		depth        int
	}
	seen := map[ir.Decl]info{}
	active := map[ir.Decl]bool{}
	var decl func(ir.Decl) (info, error)
	var field func(*ir.Field) (info, error)
	bad := func(text string) (info, error) { return info{}, fmt.Errorf("writer snapshot %s", text) }
	checked := func(x info) (info, error) {
		if x.nodes > int64(maxEntries) || x.nodes < 1 {
			return bad("expands past its recorded layout")
		}
		if x.bytes < 0 || x.bytes > ir.TableFixedRecordMaxBytes {
			return bad("has an invalid or oversized extent")
		}
		if x.depth > ir.TableFixedMaxDepth {
			return bad("has an over-depth closure")
		}
		return x, nil
	}
	field = func(f *ir.Field) (info, error) {
		if f.Name == "" {
			return bad("has an unnamed field")
		}
		width := f.Type.Width
		switch f.Type.Kind {
		case ir.TInt, ir.TFixed:
			if width != 8 && width != 16 && width != 32 && width != 64 && width != 128 {
				return bad("has an invalid scalar width")
			}
			if f.Type.Kind == ir.TFixed && (f.Type.IntBits < 0 || f.Type.FracBits < 0 || f.Type.IntBits+f.Type.FracBits != width || f.Type.Signed && f.Type.IntBits < 1) {
				return bad("has an invalid fixed-point scale")
			}
			if f.Type.Timestamp && (!f.Type.Signed || width != 64) {
				return bad("has an invalid timestamp kind")
			}
		case ir.TBits:
			if width < 1 || width > 64 {
				return bad("has an invalid bits width")
			}
		case ir.TString, ir.TBytes:
			if f.Type.Size < 0 || f.Type.Size > ir.TableFixedRecordMaxBytes {
				return bad("has an invalid text/bytes extent")
			}
		case ir.TBool, ir.TFloat32, ir.TFloat64, ir.TNamed:
		default:
			return bad("has an invalid field kind")
		}
		if f.ArrayBound < 0 || f.ArrayBound > math.MaxUint32 || f.ArrayMin < 0 || f.ArrayMin > f.ArrayBound {
			return bad("has invalid array extents")
		}
		if f.HasIntRange && f.Type.Kind != ir.TInt && f.Type.Kind != ir.TBits && f.Type.Kind != ir.TFixed {
			return bad("has integer bounds on a non-integer")
		}
		if f.HasFloatRange {
			steps := math.Ceil((f.FMax - f.FMin) / f.Resolution)
			if f.Type.Kind != ir.TFloat32 || f.Round != "nearest" || steps < 1 || steps > math.MaxUint32 || f.Steps != int64(steps) || !ir.ValidCompressedFloatParams(f.FMin, f.FMax, f.Resolution) || !ir.CompressedFloatDecodeFinite(f.FMin, f.FMax, f.Resolution) {
				return bad("has invalid compressed-float facts")
			}
		} else if f.Round != "" || f.Steps != 0 {
			return bad("has compressed-float facts without a range")
		}
		if f.HasDefault && f.Array != ir.ArrayNone {
			return bad("has an array default instead of its birth count")
		}
		if f.HasDefault && (f.Type.Kind == ir.TFloat32 || f.Type.Kind == ir.TFloat64) && (math.IsNaN(f.DefFloat) || math.IsInf(f.DefFloat, 0) || f.Type.Kind == ir.TFloat32 && math.IsInf(float64(float32(f.DefFloat)), 0)) {
			return bad("has a non-finite default")
		}
		if f.HasDefault && (f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes) && int64(len(f.DefBytes)) > f.Type.Size {
			return bad("has an over-bound default")
		}
		if f.Type.Kind == ir.TInt || f.Type.Kind == ir.TFixed || f.Type.Kind == ir.TBits {
			bits := uint(f.Type.Width)
			lo := big.NewInt(0)
			hi := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bits), big.NewInt(1))
			if f.Type.Signed && f.Type.Kind != ir.TBits {
				lo.Neg(new(big.Int).Lsh(big.NewInt(1), bits-1))
				hi.Sub(new(big.Int).Lsh(big.NewInt(1), bits-1), big.NewInt(1))
			}
			if f.HasIntRange {
				rlo, rhi, ok := ir.TableRawRange(f)
				if !ok || rlo.Cmp(lo) < 0 || rhi.Cmp(hi) > 0 {
					return bad("has integer bounds outside its storage")
				}
				lo, hi = rlo, rhi
			}
			if f.HasDefault && (f.DefInt == nil || f.DefInt.Cmp(lo) < 0 || f.DefInt.Cmp(hi) > 0) {
				return bad("has an invalid integer default")
			}
		}
		var elem info
		var err error
		if f.Type.Kind == ir.TNamed {
			elem, err = decl(f.Type.Ref)
			if err != nil {
				return info{}, err
			}
		} else {
			elem = info{nodes: 1, bytes: ir.TableFixedStorageBytes(f.Type), depth: 0}
		}
		x := elem
		switch {
		case f.KeyEnum != "":
			if f.Array != ir.ArrayFixed || f.KeyEnumRef == nil || f.ArrayBound != f.KeyEnumRef.Max {
				return bad("has an invalid key enum")
			}
			key, err := decl(f.KeyEnumRef)
			if err != nil {
				return info{}, err
			}
			if elem.bytes > 0 && f.ArrayBound > ir.TableFixedRecordMaxBytes/elem.bytes {
				return bad("has an oversized keyed extent")
			}
			x = info{nodes: 1 + key.nodes + elem.nodes, bytes: f.ArrayBound * elem.bytes, depth: 1 + max(key.depth, elem.depth)}
		case f.Array != ir.ArrayNone:
			count := int64(0)
			if f.Array == ir.ArrayCounted {
				count = ir.TableFixedCountBytes
			}
			if elem.bytes > 0 && f.ArrayBound > (ir.TableFixedRecordMaxBytes-count)/elem.bytes {
				return bad("has an oversized array extent")
			}
			x = info{nodes: 1 + elem.nodes, bytes: count + f.ArrayBound*elem.bytes, depth: 1 + elem.depth}
		case f.Type.Kind == ir.TString:
			x = info{nodes: 1, bytes: 4 + f.Type.Size, depth: 0}
		case f.Type.Kind == ir.TBytes:
			x = info{nodes: 2, bytes: 4 + f.Type.Size, depth: 1}
		}
		if f.Type.Optional {
			x.nodes++
			x.bytes++
			x.depth++
		}
		return checked(x)
	}
	decl = func(d ir.Decl) (info, error) {
		if d == nil {
			return bad("has an unresolved named type")
		}
		if active[d] {
			return bad("has a cyclic by-value closure")
		}
		if x, ok := seen[d]; ok {
			return x, nil
		}
		active[d] = true
		defer delete(active, d)
		x := info{nodes: 1, depth: 0}
		switch v := d.(type) {
		case *ir.Struct:
			for _, f := range v.Fields {
				next, err := field(f)
				if err != nil {
					return info{}, err
				}
				x.nodes += next.nodes
				x.bytes += next.bytes
				x.depth = max(x.depth, 1+next.depth)
				if _, err := checked(x); err != nil {
					return info{}, err
				}
			}
		case *ir.Enum:
			if v.Max != int64(len(v.Variants)) || v.Max < 0 || v.StorageBits != ir.StorageBitsFor(v.Max) || len(v.Was) > len(v.Variants) {
				return bad("has an invalid enum")
			}
			x.nodes += int64(len(v.Variants))
			x.bytes = int64(v.StorageBits / 8)
			if len(v.Variants) > 0 {
				x.depth = 1
			}
		case *ir.Flags:
			if len(v.Variants) < 1 || len(v.Variants) > 64 || v.WireBits < len(v.Variants) || v.WireBits > 64 {
				return bad("has invalid flags")
			}
			x.bytes = 8
		case *ir.Union:
			if v.Max != int64(len(v.Variants)) || v.Max < 0 || v.StorageBits != ir.StorageBitsFor(v.Max) {
				return bad("has an invalid union")
			}
			widest := int64(0)
			for _, arm := range v.Variants {
				next := info{nodes: 1, depth: 0}
				if arm.Name == "" {
					return bad("has an unnamed union arm")
				}
				if arm.F != nil {
					var err error
					next, err = field(arm.F)
					if err != nil {
						return info{}, err
					}
					if arm.F.KeyEnum != "" || arm.F.Type.Optional {
						return bad("has an unsupported union arm")
					}
				}
				x.nodes += next.nodes
				widest = max(widest, next.bytes)
				x.depth = max(x.depth, 1+next.depth)
				if x.nodes > int64(maxEntries) {
					return bad("expands past its recorded layout")
				}
			}
			x.bytes = int64(v.StorageBits/8) + widest
		default:
			return bad("has an invalid named declaration")
		}
		x, err := checked(x)
		if err != nil {
			return info{}, err
		}
		seen[d] = x
		return x, nil
	}
	_, err := decl(root)
	return err
}
