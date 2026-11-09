package jstable

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE WRITER'S RANGE IS THE BOUND (docs/FIXED-FORM-ALGORITHM.md §4.5, §4.6,
// bill §12.5). A ranged integer read through a COMPILED lane is held first
// to the range ITS WRITER declared, the lineage entry the file was written
// under, and then to this reader's own range in the projection; each build
// REFUSES out_of_range rather than clamping. The writer's ranges and their
// match onto this reader's fields are the compiler's own
// (ir.TableFixedWriterRanges, ir.TableFixedWriterField), shared by every leg.

// fixedWriterBound is one writer range resolved onto this reader's image: the
// reader's field, the image offset of one leaf (a fixed array has a row per
// element), the writer's raw bounds, and which ends can fire. An end is
// dropped when it sits on the storage width's own limit, where no load can
// fail it, or on the reader's own end, where the projection's clamp lands the
// same value and counts the same once.
type fixedWriterBound struct {
	field     *ir.Field
	at        int64
	lo, hi    *big.Int
	low, high bool
}

// fixedWriterBounds resolves one lineage entry's writer ranges onto this
// reader's image through ir.TableFixedWriterField.
func fixedWriterBounds(st *ir.Struct, e FixedLineageEntry) []fixedWriterBound {
	var out []fixedWriterBound
	for _, r := range e.Ranges {
		at := int64(0)
		for _, f := range st.Fields {
			w, ok := ir.TableFixedWriterField(f, r)
			if !ok {
				at += ir.TableFixedFieldBytes(f)
				continue
			}
			out = append(out, fixedWriterBoundsOf(f, w, at)...)
			break
		}
	}
	return out
}

// fixedWriterBoundsOf spells one resolved writer field at this reader's image
// offsets: a scalar is one row, a fixed array one per element at its stride.
func fixedWriterBoundsOf(f, w *ir.Field, at int64) []fixedWriterBound {
	lo, hi, ok := ir.TableRawRange(w)
	rlo, rhi, rok := ir.TableRawRange(f)
	if !ok {
		return nil
	}
	bits := uint(8 * ir.TableFixedStorageBytes(f.Type))
	one := big.NewInt(1)
	smin, smax := big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(one, bits), one)
	if ir.TableKindSigned(ir.TableScalarKind(f)) {
		half := new(big.Int).Lsh(one, bits-1)
		smin, smax = new(big.Int).Neg(half), new(big.Int).Sub(half, one)
	}
	low := lo.Cmp(smin) > 0 && !(rok && lo.Cmp(rlo) == 0)
	high := hi.Cmp(smax) < 0 && !(rok && hi.Cmp(rhi) == 0)
	if !low && !high {
		return nil
	}
	n, stride := int64(1), ir.TableFixedElementBytes(f)
	if f.Array == ir.ArrayFixed {
		n = f.ArrayBound
	}
	out := make([]fixedWriterBound, 0, n)
	for i := range n {
		out = append(out, fixedWriterBound{field: f, at: at + i*stride, lo: lo, hi: hi, low: low, high: high})
	}
	return out
}

// fixedLineageWriterBounds is every entry's resolved writer ranges, in lineage
// order, and whether any entry carries one. The reader's own entry carries
// none: its writer IS this reader, and the projection's pass holds it.
func fixedLineageWriterBounds(st *ir.Struct, entries []FixedLineageEntry, own uint64) ([][]fixedWriterBound, bool) {
	all := make([][]fixedWriterBound, len(entries))
	any := false
	for i, e := range entries {
		if e.Wire == own {
			continue
		}
		all[i] = fixedWriterBounds(st, e)
		any = any || len(all[i]) > 0
	}
	return all, any
}

// emitWriterBounds lays down one straight-line pass per lineage entry that
// carries a writer range, and the frozen array the load indexes by the entry
// it selected (null where an entry bounds nothing). It answers whether the
// table has any, so a table without one emits no pass and no call.
func (g *fixedModule) emitWriterBounds(st *ir.Struct, entries []FixedLineageEntry, own uint64) bool {
	all, any := fixedLineageWriterBounds(st, entries, own)
	if !any {
		return false
	}
	g.pf("// THE WRITER'S OWN RANGES (docs/FIXED-FORM-ALGORITHM.md §4.6, bill §12.5):\n")
	g.pf("// one pass per lineage entry whose writer declared a range this reader holds\n")
	g.pf("// wider. It runs on a compiled lane's image after the plan and before the\n")
	g.pf("// projection, so a forged value past what that writer could have written\n")
	g.pf("// REFUSES out_of_range — the writer's range is the bound, never a clamp.\n")
	names := make([]string, len(all))
	for i, rows := range all {
		if len(rows) == 0 {
			names[i] = "null"
			continue
		}
		names[i] = fmt.Sprintf("%sFixedWriterBounds%d", st.Name, i)
		g.pf("function %s(view, report) {\n", names[i])
		for _, b := range rows {
			get, le := fixedGetter(b.field.Type)
			endian := ""
			if le {
				endian = ", true"
			}
			asBig := fixedIsBig(b.field.Type)
			lo, hi := jsIntLit(b.lo, asBig), jsIntLit(b.hi, asBig)
			g.pf("  {\n")
			g.pf("    const n = view.%s(%d%s);\n", get, b.at, endian)
			cond := ""
			switch {
			case b.low && b.high:
				cond = fmt.Sprintf("n < %s || n > %s", lo, hi)
			case b.low:
				cond = fmt.Sprintf("n < %s", lo)
			case b.high:
				cond = fmt.Sprintf("n > %s", hi)
			}
			g.pf("    if (%s) { report.refused = TableFixedRefusal.OutOfRange; return; }\n", cond)
			g.pf("  }\n")
		}
		g.pf("}\n")
	}
	g.pf("const %sFixedWriterBounds = Object.freeze([%s]);\n\n", st.Name, strings.Join(names, ", "))
	return true
}
