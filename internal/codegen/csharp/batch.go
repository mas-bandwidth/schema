// The batch opt-in (serialize.cs.s batch surface, emitter half): scalar-dense
// Write/Read pairs keep their public stream signature but run their body as
// an AggressiveInlining batch-form core against WriteBatch/ReadBatch —
// register-resident stream state, stored back once at End. Two measured laws
// from the batch measurements govern the shape:
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
// const/reserved/align items —
// fixed scalar arrays weighted by their schema bound, counted ones by bound
// plus the count site, nested structs transitively) and B = its delegated
// bulk sites (strings, bytes, bulk-byte arrays — each one sync/recapture
// round trip). A pair is batched iff
//
//	S >= 2 + 4*B
//
// The constants anchor on the batch measurements: chat (S=1, B=1) lost 9%, so a
// bulk site must be outweighed by several scalars (the 4); an empty or
// near-empty body (heartbeat) cannot amortize Begin/End at all (the 2). On
// this corpus the rule excludes exactly Heartbeat (S=0), Chat and Block
// (S=1, B=1) and batches every other pair.
//
// A type composed under a batched parent needs a core whatever its own
// density (rule 1: the parent must reach it by ref, inline), so cores are
// emitted for the closure of batched pairs over composition — struct fields
// AND union arms; the entry-form decision stays per-pair.
//
// UNIONS CARRY CORES TOO. They did not until the C# profile of BenchMixed
// convicted the plain path: one union field anywhere in a message put the
// WHOLE message back on the stream, and on the bench shape that meant the
// 8-element entity array and the 80-element stat array each paid a full
// BeginBatch capture / End restore AND a real call PER ELEMENT — 88 of them
// on a 438-byte message. A union's arm dispatch is a switch over calls, and
// a core makes those calls core-to-core by ref like any other composition,
// so nothing about rule 1 stood in the way; only the missing emitter half
// did.
package csharp

import (
	"github.com/mas-bandwidth/schema/v2/ir"
)

// batchWorthwhile is the density threshold — see the package comment above.
func batchWorthwhile(s, b int64) bool {
	return s >= 2+4*b
}

// batchEntry decides whether a pair's own ENTRY runs its whole body on a
// batch. Density is necessary and not sufficient: a body with ANY delegated
// site must Sync the batch state down and Recapture it around that site, and
// between the two it hands the register allocator a ref struct that has to
// survive the whole body. See batch.go's header for the measurement.
func batchEntry(s, b int64) bool {
	return b == 0 && batchWorthwhile(s, b)
}

// densityStruct is the (scalar, bulk) site weight of a struct's wire body.
func densityStruct(st *ir.Struct) (int64, int64) {
	return densityItems(st.Items, ir.AlignedFixedByteArrays(st))
}

// densityUnion is the (scalar, bulk) site weight of a union's wire body: the
// tag site, plus the heaviest arm — a batch spans whichever arm runs, and the
// worst case is the one that has to earn the capture/restore.
func densityUnion(u *ir.Union) (int64, int64) {
	var s, b int64
	for _, v := range u.Variants {
		if v.Ref == nil {
			continue
		}
		as, ab := densityStruct(v.Ref)
		if as+ab > s+b {
			s, b = as, ab
		}
	}
	return 1 + s, b // the tag rides on the batch like any other scalar
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
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			es, eb = densityStruct(ref)
		case *ir.Union:
			es, eb = densityUnion(ref)
		default:
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

// batchPlan decides, per Write/Read pair name, whether the entry runs a batch
// (batched) and whether a *Batch core is emitted (needCore: the closure of
// batched pairs over composition — rule 1 requires every composed type, struct
// field or union arm, to be reachable by ref core-to-core).
func batchPlan(u *ir.Unit) (batched, needCore map[string]bool) {
	batched = map[string]bool{}
	needCore = map[string]bool{}
	var pending []composite
	open := func(name string, c composite) {
		if needCore[name] {
			return
		}
		needCore[name] = true
		pending = append(pending, c)
	}
	for _, f := range u.Files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ir.Struct:
				if s, b := densityStruct(d); batchEntry(s, b) {
					batched[d.Name] = true
					open(d.Name, composite{st: d})
				}
			case *ir.Union:
				if s, b := densityUnion(d); batchEntry(s, b) {
					batched[d.Name] = true
					open(d.Name, composite{un: d})
				}
			}
		}
	}
	// close over composition: every type a core's body calls needs its own
	// core, whatever its density
	for len(pending) > 0 {
		c := pending[0]
		pending = pending[1:]
		for _, child := range c.children() {
			open(child.name(), child)
		}
	}
	return
}

// composite is one node of the composition graph the core closure walks: a
// struct's wire body, or a union's arm dispatch. Exactly one field is set.
type composite struct {
	st *ir.Struct
	un *ir.Union
}

func (c composite) name() string {
	if c.st != nil {
		return c.st.Name
	}
	return c.un.Name
}

// children lists the composite types this one's wire body calls into. A
// synthetic struct view wrapper (Items empty, Fields set) walks its fields
// directly; a union walks its arms.
func (c composite) children() []composite {
	var out []composite
	if c.un != nil {
		for _, v := range c.un.Variants {
			if v.Ref != nil {
				out = append(out, composite{st: v.Ref})
			}
		}
		return out
	}
	st := c.st
	note := func(f *ir.Field) {
		if f.Type.Kind != ir.TNamed {
			return
		}
		switch child := f.Type.Ref.(type) {
		case *ir.Struct:
			out = append(out, composite{st: child})
		case *ir.Union:
			out = append(out, composite{un: child})
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
