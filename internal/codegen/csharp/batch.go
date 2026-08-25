// The batch opt-in (serialize.cs PR #3's emitter follow-up): scalar-dense
// Write/Read pairs keep their public stream signature but run their body as
// an AggressiveInlining batch-form core against WriteBatch/ReadBatch —
// register-resident stream state, stored back once at End. Two measured laws
// from #3 govern the shape:
//
//  1. INLINE-ONLY COMPOSITION. A non-inlined call taking `ref WriteBatch`
//     address-exposes the ref struct and enregistration dies for the whole
//     calling scope — measured 0.71x, SLOWER than no batch at all. Every
//     core carries MethodImplOptions.AggressiveInlining, and nested types
//     compose core-to-core by ref, never through the stream entry.
//
//  2. PER-TYPE OPT-IN BY SCALAR DENSITY. A body dominated by bulk work (a
//     length int plus SerializeBytes — the chat shape) pays the batch
//     capture/restore and the per-op sync/recapture of delegated bulk calls
//     without enough scalar traffic to win it back — measured 0.91x write /
//     0.94x read. Such pairs stay on the stream.
//
// The density rule, and the threshold chosen here: count S = the scalar
// serialize sites of the body (register-resident on a batch: fixed-width
// ints/bits/bools/floats, ranged ints, enums, flags, compressed floats,
// const/reserved/align items, quantized components, projected floats —
// fixed scalar arrays weighted by their schema bound, counted ones by bound
// plus the count site, nested structs transitively) and B = its delegated
// bulk sites (strings, bytes, bulk-byte arrays — each one sync/recapture
// round trip). A pair is batched iff
//
//	S >= 2 + 4*B
//
// The constants anchor on #3's measurements: chat (S=1, B=1) lost 9%, so a
// bulk site must be outweighed by several scalars (the 4); an empty or
// near-empty body (heartbeat) cannot amortize Begin/End at all (the 2). On
// this corpus the rule excludes exactly Heartbeat (S=0), Chat and Block
// (S=1, B=1) and batches every other pair.
//
// A type composed under a batched parent needs a core whatever its own
// density (rule 1: the parent must reach it by ref, inline), so cores are
// emitted for the closure of batched pairs over struct composition; the
// entry-form decision stays per-pair.
package csharp

import (
	"github.com/mas-bandwidth/schema/ir"
)

// batchWorthwhile is the density threshold — see the package comment above.
func batchWorthwhile(s, b int64) bool {
	return s >= 2+4*b
}

// reachesUnion reports whether a struct's wire body reaches a union through
// any field, transitively. Such a pair stays on the stream: a union's arm
// dispatch calls the arms' plain stream entries, and composing that under a
// ref-struct batch core would address-expose the batch (rule 1's measured
// 0.71x). Union batch cores are a follow-on if a profile ever convicts the
// plain path — land-simple, not a ruling against.
func reachesUnion(st *ir.Struct, visiting map[string]bool) bool {
	if visiting[st.Name] {
		return false
	}
	visiting[st.Name] = true
	defer delete(visiting, st.Name)
	for _, f := range st.Fields {
		if f.Type.Kind != ir.TNamed {
			continue
		}
		switch ref := f.Type.Ref.(type) {
		case *ir.Union:
			return true
		case *ir.Struct:
			if reachesUnion(ref, visiting) {
				return true
			}
		}
	}
	return false
}

// densityStruct is the (scalar, bulk) site weight of a struct's wire body.
func densityStruct(st *ir.Struct) (int64, int64) {
	return densityItems(st.Items, ir.AlignedFixedByteArrays(st))
}

func densityItems(items []ir.Item, bulk map[*ir.Field]bool) (s, b int64) {
	for _, item := range items {
		switch item := item.(type) {
		case *ir.FieldItem:
			fs, fb := densityField(item.F, bulk[item.F])
			s, b = s+fs, b+fb
		case *ir.ConstItem, *ir.ReservedItem, *ir.AlignItem:
			s++
		case *ir.Branch:
			// both sides weigh in: the branch bool itself is a field, and a
			// batch spans whichever side runs
			ts, tb := densityItems(item.Then, bulk)
			es, eb := densityItems(item.Else, bulk)
			s, b = s+ts+es, b+tb+eb
		}
	}
	return
}

func densityField(f *ir.Field, isBulkByteArray bool) (s, b int64) {
	if isBulkByteArray {
		return 0, 1 // one delegated SerializeBytes round trip
	}
	var es, eb int64 // per-element weight
	switch f.Type.Kind {
	case ir.TString, ir.TBytes:
		es, eb = 1, 1 // the length site, then the delegated byte run
	case ir.TNamed:
		if st, ok := f.Type.Ref.(*ir.Struct); ok {
			es, eb = densityStruct(st)
		} else {
			es = 1 // enum or flags
		}
	default:
		es = 1
	}
	switch f.Array {
	case ir.ArrayFixed:
		return f.ArrayBound * es, f.ArrayBound * eb
	case ir.ArrayCounted:
		return 1 + f.ArrayBound*es, f.ArrayBound * eb
	}
	return es, eb
}

// densityView is densityStruct for an object view's field list.
func densityView(fields []*ir.Field, v ir.View) (s, b int64) {
	for _, f := range fields {
		switch {
		case v == ir.ViewShallow && f.HasQuantize:
			s += int64(len(f.Type.Ref.(*ir.Struct).Fields)) // one ranged int per component
		case v == ir.ViewShallow && f.HasFloatRange:
			s++ // the projected int
		case v == ir.ViewDeep && f.HasFloatRange && f.Interpolate:
			s++ // the bare float
		default:
			fs, fb := densityField(f, false) // view entry alignment is unknown — no bulk marks
			s, b = s+fs, b+fb
		}
	}
	return
}

// batchPlan decides, per Write/Read pair name, whether the entry runs a batch
// (batched) and whether a *Batch core is emitted (needCore: the closure of
// batched pairs over struct composition — rule 1 requires every composed type
// to be reachable by ref core-to-core).
func batchPlan(u *ir.Unit) (batched, needCore map[string]bool) {
	batched = map[string]bool{}
	needCore = map[string]bool{}
	var pending []*ir.Struct
	for _, f := range u.Files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ir.Struct:
				if reachesUnion(d, map[string]bool{}) {
					continue // stays on the stream — see reachesUnion
				}
				if s, b := densityStruct(d); batchWorthwhile(s, b) {
					batched[d.Name] = true
					needCore[d.Name] = true
					pending = append(pending, d)
				}
			case *ir.Object:
				deep, interp := splitObjectFields(d)
				if s, b := densityView(deep, ir.ViewDeep); batchWorthwhile(s, b) {
					name := d.Name + "Data_Deep"
					batched[name] = true
					needCore[name] = true
					pending = append(pending, &ir.Struct{Name: name, Fields: deep})
				}
				if s, b := densityView(interp, ir.ViewShallow); batchWorthwhile(s, b) {
					name := d.Name + "Data_Shallow"
					batched[name] = true
					needCore[name] = true
					// shallow quantized/projected fields serialize inline;
					// only the default-path struct fields compose
					var composed []*ir.Field
					for _, fl := range interp {
						if !fl.HasQuantize && !fl.HasFloatRange {
							composed = append(composed, fl)
						}
					}
					pending = append(pending, &ir.Struct{Name: name, Fields: composed})
				}
			}
		}
	}
	// close over composition: every struct type a core's body calls needs its
	// own core, whatever its density
	for len(pending) > 0 {
		st := pending[0]
		pending = pending[1:]
		for _, child := range childStructs(st) {
			if !needCore[child.Name] {
				needCore[child.Name] = true
				pending = append(pending, child)
			}
		}
	}
	return
}

// childStructs lists the struct types st's wire body calls into. A synthetic
// view wrapper (Items empty, Fields set) walks its fields directly.
func childStructs(st *ir.Struct) []*ir.Struct {
	var out []*ir.Struct
	note := func(f *ir.Field) {
		if f.Type.Kind == ir.TNamed {
			if child, ok := f.Type.Ref.(*ir.Struct); ok {
				out = append(out, child)
			}
		}
	}
	if len(st.Items) == 0 && len(st.Fields) > 0 {
		for _, f := range st.Fields {
			note(f)
		}
		return out
	}
	var walk func(items []ir.Item)
	walk = func(items []ir.Item) {
		for _, item := range items {
			switch item := item.(type) {
			case *ir.FieldItem:
				note(item.F)
			case *ir.Branch:
				walk(item.Then)
				walk(item.Else)
			}
		}
	}
	walk(st.Items)
	return out
}
