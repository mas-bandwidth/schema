package main

import "unsafe"

// regionMessageRow binds a VARIABLE root's message batch (docs/SPEC-TABLES.md
// §3.3): the region the batch needs is measured from the wire, the tolerant
// read fills it and writes each body's root, and the batch is saved back from
// those roots. A plain `table` rides the form over a region as C++, C and C#
// carry it.
func regionMessageRow[T, R, V, A any](unit, root string, fresh func() *V, announceMeasure func() int64, announce func([]byte) int64, announceRead func(*V, []byte, *R) bool, loadMeasure func(*V, []byte) int64, load func([]*T, []byte, *V, []byte, *R) (int64, bool), measure func([]*T, *R, ...A) int64, save func([]*T, []byte, *R, ...A) int64, snap func(*R) report) messageCodec {
	var own *V
	roots := make([]*T, 256)
	ensure := func() *V {
		if own == nil {
			var r R
			v := fresh()
			announcement := make([]byte, announceMeasure())
			announce(announcement)
			if !announceRead(v, announcement, &r) {
				return nil
			}
			own = v
		}
		return own
	}
	return messageCodec{unit: unit, root: root, loadMeasure: func(wire []byte) int64 {
		v := ensure()
		if v == nil {
			return -1
		}
		return loadMeasure(v, wire)
	}, run: func(announcement, wire []byte, fuzz bool) ([]byte, report, bool) {
		var r R
		var v *V
		if announcement != nil {
			v = fresh()
			if !announceRead(v, announcement, &r) {
				return nil, snap(&r), false
			}
		} else if v = ensure(); v == nil {
			return nil, snap(&r), false
		}
		var region []byte
		if n := loadMeasure(v, wire); n > 0 {
			raw := make([]byte, n+63)
			off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
			region = raw[off : off+uintptr(n)]
		}
		clear(roots)
		count, ok := load(roots, region, v, wire, &r)
		rep := snap(&r)
		if fuzz {
			// UNDER FUZZ the answer is whether the load RETURNED A ROOT, which
			// is what the harness holds a region-sized leg to: damage inside
			// the body still hands back the root it placed, and the report says
			// the batch was malformed (§3.3)
			count = 1
			rep.malformed = rep.malformed || !ok && !rep.refused
			ok = roots[0] != nil
			if !ok {
				return nil, rep, false
			}
		} else if !ok {
			return nil, rep, false
		}
		if count < 1 || count > 256 {
			return nil, rep, false
		}
		// a save the writer refuses is no load failure: under fuzz the root
		// stands and the answer is "refused" (a nil buffer), as the oracle's is
		var ignored R
		n := measure(roots[:count], &ignored)
		if n < 0 {
			return nil, rep, fuzz
		}
		out := make([]byte, n)
		if save(roots[:count], out, &ignored) != n {
			return nil, rep, fuzz
		}
		return out, rep, ok
	}}
}
