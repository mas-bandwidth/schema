package main

import (
	"reflect"
	"strings"
	"unsafe"
)

type regionValue[T any] struct {
	root    *T
	storage []byte
}

func regionRow[T, R, A, C any, O ~uint8](unit, root string,
	loadMeasure func([]byte) int64,
	load func([]byte, []byte, *R) *T,
	measure func(*T, ...C) int64,
	save func(*T, []byte, ...C) int64,
	fromJson func([]byte, *R) (*T, []byte, bool),
	toJsonMeasure func(*T, ...A) int64,
	toJson func(*T, []byte, ...A) int64,
	cookMeasure func(*T, ...C) int64,
	cook func(*T, []byte, O, ...C) bool,
	snap func(*R) report,
	measureReason ...func(*T, ...C) (int64, error),
) codec {
	entry := codec{unit: unit, root: root, loadMeasure: loadMeasure,
		cookMeasure: func(value any) int64 { return cookMeasure(value.(*regionValue[T]).root) },
		cook: func(value any, buffer []byte, big bool) bool {
			order := O(1)
			if big {
				order = 2
			}
			return cook(value.(*regionValue[T]).root, buffer, order)
		},
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
			if e, ok := any(&r).(interface{ Err() error }); ok {
				rep.err = e.Err()
			}
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
	entry.share = func(value any, source, alias string) bool {
		root := value.(*regionValue[T]).root
		if root == nil {
			return false
		}
		return shareBlob(unsafe.Pointer(root), reflect.TypeFor[T](), source, alias)
	}
	if len(measureReason) > 0 {
		entry.measureReason = func(value any) (int64, error) {
			return measureReason[0](value.(*regionValue[T]).root)
		}
	}
	return entry
}

// shareBlob points the alias slot at the blob the source slot names. A region
// slot is a signed self-relative delta, zero for null (§6.3), so the alias's
// new delta is the source's target measured from the alias's own address.
func shareBlob(base unsafe.Pointer, t reflect.Type, source, alias string) bool {
	if source == alias {
		return false
	}
	from, ok := t.FieldByNameFunc(func(n string) bool { return strings.EqualFold(n, source) })
	if !ok || from.Type.Kind() != reflect.Int64 {
		return false
	}
	to, ok := t.FieldByNameFunc(func(n string) bool { return strings.EqualFold(n, alias) })
	if !ok || to.Type.Kind() != reflect.Int64 {
		return false
	}
	fromSlot := (*int64)(unsafe.Add(base, from.Offset))
	toSlot := (*int64)(unsafe.Add(base, to.Offset))
	if *fromSlot == 0 || *toSlot == 0 {
		return false // the recipe needs two populated blob slots
	}
	*toSlot = *fromSlot + int64(from.Offset) - int64(to.Offset)
	return true
}
