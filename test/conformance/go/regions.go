package main

import "unsafe"

type regionValue[T any] struct {
	root    *T
	storage []byte
}

func regionRow[T, R, A any](unit, root string,
	loadMeasure func([]byte) int64,
	load func([]byte, []byte, *R) *T,
	measure func(*T, ...*A) int64,
	save func(*T, []byte, ...*A) int64,
	fromJson func([]byte, *R) (*T, []byte, bool),
	toJsonMeasure func(*T) int64,
	toJson func(*T, []byte) int64,
	snap func(*R) report,
) codec {
	return codec{unit: unit, root: root, loadMeasure: loadMeasure,
		fresh: func() any { return &regionValue[T]{} },
		load: func(value any, wire []byte, rep *report) bool {
			v := value.(*regionValue[T])
			n := loadMeasure(wire)
			if n < 0 {
				v.root = nil
				return false
			}
			raw := make([]byte, n+63)
			off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
			v.storage = raw[off : off+uintptr(n)]
			var r R
			v.root = load(v.storage, wire, &r)
			*rep = snap(&r)
			return v.root != nil
		},
		fromJson: func(value any, text []byte, rep *report) bool {
			v := value.(*regionValue[T])
			var r R
			var ok bool
			v.root, v.storage, ok = fromJson(text, &r)
			*rep = snap(&r)
			return ok
		},
		toJsonMeasure: func(value any) int64 { return toJsonMeasure(value.(*regionValue[T]).root) },
		toJson:        func(value any, buffer []byte) int64 { return toJson(value.(*regionValue[T]).root, buffer) },
		measure:       func(value any) int64 { return measure(value.(*regionValue[T]).root) },
		save:          func(value any, buffer []byte) int64 { return save(value.(*regionValue[T]).root, buffer) },
	}
}
