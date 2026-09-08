package main

import "unsafe"

func regionMessageRow[T, R, V, A any](unit, root string, fresh func() *V, announceMeasure func() int64, announce func([]byte) int64, announceRead func(*V, []byte, *R) bool, loadMeasure func(*V, []byte) int64, load func([]*T, []byte, *V, []byte, *R) (int64, bool), measure func([]*T, *R, ...*A) int64, save func([]*T, []byte, *R, ...*A) int64, snap func(*R) report) messageCodec {
	var own *V
	roots := make([]*T, 256)
	return messageCodec{unit: unit, root: root, loadMeasure: func(wire []byte) int64 { return loadMeasure(own, wire) }, run: func(announcement, wire []byte, fuzz bool) ([]byte, report, bool) {
		var r R
		v := own
		if announcement != nil || v == nil {
			v = fresh()
			if announcement == nil {
				announcement = make([]byte, announceMeasure())
				announce(announcement)
			}
			if !announceRead(v, announcement, &r) {
				return nil, snap(&r), false
			}
			if fuzz {
				own = v
			}
		}
		n := loadMeasure(v, wire)
		var region []byte
		if n > 0 {
			raw := make([]byte, n+63)
			off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
			region = raw[off : off+uintptr(n)]
		}
		clear(roots)
		count, ok := load(roots, region, v, wire, &r)
		rep := snap(&r)
		if fuzz {
			count = 1
			ok = roots[0] != nil
		} else if !ok {
			return nil, rep, false
		}
		if !ok || count < 1 || count > 256 {
			return nil, rep, false
		}
		var ignored R
		size := measure(roots[:count], &ignored)
		if size < 0 {
			return nil, rep, ok
		}
		out := make([]byte, size)
		if save(roots[:count], out, &ignored) != size {
			return nil, rep, ok
		}
		return out, rep, ok
	}}
}
