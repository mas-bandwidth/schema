package unitytable

import (
	"fmt"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedInt is an integer of at most 64 bits on the fixed form, and a bits(N):
// the raw value at its declared storage width, little-endian (§3). An older
// writer's leaf of the same signedness at a narrower width WIDENS (§1, the
// ladder) and counts one widened per value landed; a wider or other-signed
// leaf is not a lawful pair. The check pass holds a ranged value or a
// bits(N) to the WRITER's own bounds, read at the writer's width and
// signedness (§4.6).
type fixedInt struct{}

func init() { registerFixed("int", fixedInt{}) }

func storageBytes(f *ir.Field) int64 { return ir.TableFixedFieldBytes(f) }

func (fixedInt) store(b *strings.Builder, f *ir.Field, v string, off int64) {
	if signedStorage(f) {
		fmt.Fprintf(b, "TableWire.FixedPut(body, %d, %d, unchecked((ulong)(long)%s));\n", off, storageBytes(f), v)
		return
	}
	fmt.Fprintf(b, "TableWire.FixedPut(body, %d, %d, (ulong)%s);\n", off, storageBytes(f), v)
}

func (fixedInt) decode(b *strings.Builder, f *ir.Field, v string, off int64) {
	fmt.Fprintf(b, "%s = unchecked((%s)TableWire.FixedGet(body, %d, %d));\n", v, kindInt{}.storage(f), off, storageBytes(f))
}

// signedKind reports whether a layout kind is of the signed integer family.
func signedKind(kind int) bool { return kind >= ir.TableKindI8 && kind <= ir.TableKindI64 }

func (fixedInt) pair(w *ir.Field, wkind int, wsize, src int64, f *ir.Field, dst int64) ([]fixedOp, []fixedOp, bool) {
	if kindOf(w).name() != "int" || wkind < ir.TableKindI8 || wkind > ir.TableKindU64 {
		return nil, nil, false
	}
	signed := signedKind(wkind)
	if signed != signedStorage(w) || signed != signedStorage(f) || wsize != storageBytes(w) {
		return nil, nil, false
	}
	rsize := storageBytes(f)
	var op fixedOp
	switch {
	case wsize == rsize:
		op = fixedOp{code: opCopy, src: src, dst: dst, size: wsize, width: wsize}
	case wsize < rsize && signed:
		op = fixedOp{code: opWidenS, src: src, dst: dst, size: wsize, width: rsize}
	case wsize < rsize:
		op = fixedOp{code: opWidenU, src: src, dst: dst, size: wsize, width: rsize}
	default:
		return nil, nil, false
	}
	var checks []fixedOp
	if lo, hi, ok := valueRange(w); ok {
		check := fixedOp{code: opRangeU, src: src, size: wsize, width: wsize, lo: lo.Uint64(), hi: hi.Uint64()}
		if signed {
			check = fixedOp{code: opRangeS, src: src, size: wsize, width: wsize, lo: uint64(lo.Int64()), hi: uint64(hi.Int64())}
		}
		checks = append(checks, check)
	}
	return []fixedOp{op}, checks, true
}

func (fixedInt) image(f *ir.Field) []byte {
	return putLE(kindInt{}.defaultBits(f), storageBytes(f))
}
