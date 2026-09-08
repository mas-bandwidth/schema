package main

import "unsafe"

// The harness fixes retention capacity at one MiB and one id per eight bytes.
// Reuse each caller-owned store; LoadRetain resets it before every load.
type retentionCodec struct {
	unit, root string
	run        func([]byte) ([]byte, report, int32, int32, bool, int64)
}

func retainRow[T, R, S, A any](unit, root string, fresh func() *S, loadMeasure func([]byte) int64, load func([]byte, []byte, *S, *R) *T, measure func(*T, *S, ...A) int64, save func(*T, *S, []byte, *R, ...A) int64, snap func(*R) report, counts func(*R) (int32, int32)) retentionCodec {
	var store *S
	return retentionCodec{unit, root, func(wire []byte) ([]byte, report, int32, int32, bool, int64) {
		if store == nil {
			store = fresh()
		}
		var r R
		n := loadMeasure(wire)
		var region []byte
		if n > 0 {
			raw := make([]byte, n+63)
			off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
			region = raw[off : off+uintptr(n)]
		}
		value := load(region, wire, store, &r)
		var out []byte
		if value != nil {
			size := measure(value, store)
			if size >= 0 {
				out = make([]byte, size)
				if save(value, store, out, &r) != size {
					out = nil
				}
			}
		}
		retained, lost := counts(&r)
		return out, snap(&r), retained, lost, value != nil, n
	}}
}
func findRetentionCodec(unit, root string) *retentionCodec {
	for i := range retentionCodecs {
		c := &retentionCodecs[i]
		if c.unit == unit && c.root == root {
			return c
		}
	}
	return nil
}
