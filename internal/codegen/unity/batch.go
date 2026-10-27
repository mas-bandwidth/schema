// The batched stream form. serialize.cs's WriteBatch and ReadBatch are ref
// structs that lift a stream's packer state into locals at BeginBatch and
// store it back once at End: the same wire, the same validation, the same
// latched errors, with the state in registers across the body instead of
// reloaded from the heap object around every call. Both are plain C# 7.2 ref
// structs, so the Unity dialect (C# 9, netstandard2.1) carries them unchanged;
// under IL2CPP they are C++ structs on the stack, which is the same win.
//
// The plan is the one the --lang cs measurements fixed, re-derived here rather
// than imported (this package shares no emitter with cs):
//
//  1. INLINE-ONLY COMPOSITION. A real call taking `ref WriteBatch` exposes the
//     ref struct's address and keeps its fields out of registers for the
//     whole caller, which measured slower than no batch at all. Every core is
//     [MethodImpl(MethodImplOptions.AggressiveInlining)], and a core reaches a
//     nested type's core by ref, never through its stream entry.
//
//  2. PER-TYPE OPT-IN BY SCALAR DENSITY. Count S, the scalar sites a body
//     serializes on the batch, and B, its delegated bulk sites (string and
//     bytes payloads, a bulk byte array, the fixed-point and 128-bit calls,
//     which the batch hands down to the stream and recaptures). A pair's
//     ENTRY runs a batch iff B == 0 and S >= 2: a delegated site costs a sync
//     and a recapture inside the body, and an empty body cannot pay for Begin
//     and End.
//
// Cores are emitted for the closure of batched pairs over composition, struct
// fields and union arms alike, whatever each child's own density: the parent
// must reach every child by ref, inline. A body that stays on the stream still
// opens a SCOPED batch around one composition site whose child is batched, so
// an array of such children pays one capture and one restore for the whole
// loop rather than one per element.
//
// What is measured for Unity is owed: these laws were measured on CoreCLR.
// The fleet measures this generator against them.
package unity

import (
	"fmt"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// rv is the receiver of the serialize calls being emitted: the stream, or the
// batch inside a core.
func (g *gen) rv() string {
	if g.inBatch {
		return "batch"
	}
	return "stream"
}

// compose renders the call into a nested type's Write or Read: core to core
// by ref inside a batch, the stream entry otherwise.
func (g *gen) compose(dir, typ, name string) string {
	if g.inBatch {
		return fmt.Sprintf("%s%sBatch(ref batch, ref %s)", dir, typ, name)
	}
	return fmt.Sprintf("%s%s(stream, ref %s)", dir, typ, name)
}

// emitPair emits a type's or union's Write and Read entries, honouring the
// batch plan: a batched entry keeps its public stream signature and runs the
// body as its core; a core is emitted for every pair in the closure.
func (g *gen) emitPair(name string, writeBody, readBody func()) {
	for _, dir := range []struct{ fn, stream, batch string }{
		{"Write", "WriteStream", "WriteBatch"},
		{"Read", "ReadStream", "ReadBatch"},
	} {
		body := writeBody
		if dir.fn == "Read" {
			body = readBody
		}
		g.sf("public static bool %s%s(%s stream, ref %s value)\n{\n", dir.fn, name, dir.stream, name)
		if g.batched[name] {
			g.sf("    %s batch = stream.BeginBatch();\n", dir.batch)
			g.sf("    bool result = %s%sBatch(ref batch, ref value);\n", dir.fn, name)
			g.sf("    batch.End();\n    return result;\n")
		} else {
			body()
		}
		g.sf("}\n\n")
		if g.needCore[name] {
			g.needsCompiler = true
			g.sf("// %s%sBatch is the body on a register-resident batch; inline only, so a\n", dir.fn, name)
			g.sf("// caller never takes the batch's address.\n")
			g.sf("[MethodImpl(MethodImplOptions.AggressiveInlining)]\n")
			g.sf("private static bool %s%sBatch(ref %s batch, ref %s value)\n{\n", dir.fn, name, dir.batch, name)
			g.inBatch = true
			body()
			g.inBatch = false
			g.sf("}\n\n")
		}
	}
}

// scopedChild names the batched child a stream-form composition site opens a
// scoped batch around, or "" when the site keeps the stream.
func (g *gen) scopedChild(f *ir.Field) string {
	if g.inBatch || f.Type.Kind != ir.TNamed {
		return ""
	}
	switch f.Type.Ref.(type) {
	case *ir.Struct, *ir.Union:
	default:
		return ""
	}
	if g.batched[f.Type.Name] && g.needCore[f.Type.Name] {
		return f.Type.Name
	}
	return ""
}

// emitScopedBatch opens one batch around a composition site — the call, or
// the whole loop of them. An array's count stays on the stream ahead of it, so
// the scope has one way to fail and End runs before the refusal returns.
func (g *gen) emitScopedBatch(writing bool, f *ir.Field, child, ind string) {
	name := "value." + member(f)
	dir, batchType := "Read", "ReadBatch"
	if writing {
		dir, batchType = "Write", "WriteBatch"
	}
	count := fmt.Sprintf("%d", f.ArrayBound)
	if f.Array == ir.ArrayCounted {
		count = name + "Count"
		if writing {
			g.countWrite(count, f.ArrayMin, f.ArrayBound, ind)
		} else {
			g.call(ind, fmt.Sprintf("stream.SerializeInt(ref %s, %d, %d)", count, f.ArrayMin, f.ArrayBound), " // the count guards the loop (§6.3)")
		}
	}
	g.sf("%s{\n%s    // one batch for the whole site: one capture, one restore\n", ind, ind)
	g.sf("%s    %s batch = stream.BeginBatch();\n", ind, batchType)
	if f.Array == ir.ArrayNone {
		g.sf("%s    bool batchOk = %s%sBatch(ref batch, ref %s);\n", ind, dir, child, name)
	} else {
		span := elemSpan(f)
		g.sf("%s    Span<%s> %s = %s.AsSpan();\n", ind, child, span, name)
		g.sf("%s    bool batchOk = true;\n", ind)
		g.sf("%s    for (int i = 0; i < %s; i++)\n%s    {\n", ind, count, ind)
		g.sf("%s        if (!%s%sBatch(ref batch, ref %s[i]))\n%s        {\n", ind, dir, child, span, ind)
		g.sf("%s            batchOk = false;\n%s            break;\n%s        }\n%s    }\n", ind, ind, ind, ind)
	}
	g.sf("%s    batch.End();\n", ind)
	g.sf("%s    if (!batchOk)\n%s    {\n%s        return false;\n%s    }\n%s}\n", ind, ind, ind, ind, ind)
}

// ---- the plan ----

func batchEntry(s, b int64) bool { return b == 0 && s >= 2 }

func densityStruct(st *ir.Struct) (int64, int64) {
	return densityItems(st.Items, ir.AlignedFixedByteArrays(st))
}

// densityUnion is the tag site plus the heaviest arm's scalar weight; the bulk
// count is the most any one arm carries, since one delegated site in any arm
// is a sync the batch would have to survive.
func densityUnion(u *ir.Union) (int64, int64) {
	var s, b int64
	for _, v := range u.Variants {
		if v.Ref == nil {
			continue
		}
		as, ab := densityStruct(v.Ref)
		s, b = max(s, as), max(b, ab)
	}
	return 1 + s, b
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
			ts, tb := densityItems(item.Then, bulk)
			es, eb := densityItems(item.Else, bulk)
			s, b = s+ts+es, b+tb+eb
		}
	}
	return
}

func densityField(f *ir.Field, bulkBytes bool) (s, b int64) {
	if bulkBytes {
		return 0, 1
	}
	var es, eb int64
	switch f.Type.Kind {
	case ir.TString, ir.TBytes:
		es, eb = 1, 1
	case ir.TFixed:
		es, eb = 0, 1
	case ir.TInt:
		if f.Type.Width == 128 {
			es, eb = 0, 1
		} else {
			es = 1
		}
	case ir.TNamed:
		switch ref := f.Type.Ref.(type) {
		case *ir.Struct:
			es, eb = densityStruct(ref)
		case *ir.Union:
			es, eb = densityUnion(ref)
		default:
			es = 1
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

// batchPlan decides per pair whether its entry is batched, and which pairs
// carry a core: the closure of the batched ones over composition.
func batchPlan(u *ir.Unit) (batched, needCore map[string]bool) {
	batched, needCore = map[string]bool{}, map[string]bool{}
	var pending []string
	open := func(name string) {
		if !needCore[name] {
			needCore[name] = true
			pending = append(pending, name)
		}
	}
	for _, f := range u.Files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ir.Struct:
				if batchEntry(densityStruct(d)) {
					batched[d.Name] = true
					open(d.Name)
				}
			case *ir.Union:
				if batchEntry(densityUnion(d)) {
					batched[d.Name] = true
					open(d.Name)
				}
			}
		}
	}
	for len(pending) > 0 {
		name := pending[0]
		pending = pending[1:]
		if un, ok := u.Unions[name]; ok {
			for _, v := range un.Variants {
				if v.Ref != nil {
					open(v.Ref.Name)
				}
			}
			continue
		}
		for _, f := range u.Structs[name].Fields {
			if f.Type.Kind != ir.TNamed {
				continue
			}
			switch child := f.Type.Ref.(type) {
			case *ir.Struct:
				open(child.Name)
			case *ir.Union:
				open(child.Name)
			}
		}
	}
	return batched, needCore
}
