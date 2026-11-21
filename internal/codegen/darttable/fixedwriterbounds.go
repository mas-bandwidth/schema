package darttable

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE WRITER'S RANGE IS THE BOUND (docs/FIXED-FORM-ALGORITHM.md §4.5, §4.6,
// bill §12.5). A ranged integer read through a COMPILED lane is held first
// to the range ITS WRITER declared, the lineage entry the file was written
// under, and then to this reader's own range in the projection; a value past
// either REFUSES the whole load out_of_range (docs/rulings.json
// strict-only-read), never a clamp. A COMPRESSED FLOAT is held to its writer's
// range the same way (FIXED-FORM-ALGORITHM §4.6), compared in the field's own
// type (a float32 against the writer's bounds narrowed to float32). The
// writer's ranges and their match onto this reader's fields are the compiler's
// own (ir.TableFixedWriterRanges, ir.TableFixedWriterField), shared by every
// leg.

// fixedWriterBound is one writer range resolved onto this reader's image: the
// reader's field, the image offset of one leaf (a fixed array has a row per
// element), the writer's raw bounds, and which ends can fire. An end is
// dropped when it sits on the storage width's own limit, where no load can
// fail it, or on the reader's own end, where the projection's own pass refuses
// the same value. A compressed float's row sets flt and carries the writer's
// bounds as literals in flo and fhi; lo, hi, low and high are unused.
type fixedWriterBound struct {
	field     *ir.Field
	at        int64
	lo, hi    *big.Int
	low, high bool
	flt       bool
	flo, fhi  string
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
	n, stride := int64(1), ir.TableFixedElementBytes(f)
	if f.Array == ir.ArrayFixed {
		n = f.ArrayBound
	}
	if flo, fhi, fok := fixedFloatClampEnds(w); fok {
		out := make([]fixedWriterBound, 0, n)
		for i := range n {
			out = append(out, fixedWriterBound{field: f, at: at + i*stride, flt: true, flo: flo, fhi: fhi})
		}
		return out
	}
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
	low := lo.Cmp(smin) > 0 && (!rok || lo.Cmp(rlo) != 0)
	high := hi.Cmp(smax) < 0 && (!rok || hi.Cmp(rhi) != 0)
	if !low && !high {
		return nil
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

// fixedWriterBoundsType is the Dart type of one lineage entry's pass.
const fixedWriterBoundsType = "void Function(ByteData, TableFixedReport)"

// emitWriterBounds lays down one straight-line pass per lineage entry that
// carries a writer range, and the constant list the load indexes by the entry
// it selected (null where an entry bounds nothing). It answers whether the
// table has any, so a table without one emits no pass and no call.
func (g *fixedModule) emitWriterBounds(st *ir.Struct, lower string, entries []FixedLineageEntry, own uint64) bool {
	all, any := fixedLineageWriterBounds(st, entries, own)
	if !any {
		return false
	}
	names := make([]string, len(all))
	for i, rows := range all {
		if len(rows) == 0 {
			names[i] = "null"
			continue
		}
		names[i] = fmt.Sprintf("%sFixedWriterBounds%d", lower, i)
		g.pf("/// LINEAGE ENTRY %d's WRITER RANGES (docs/FIXED-FORM-ALGORITHM.md §4.6,\n", i)
		g.pf("/// bill §12.5): held on a compiled lane's image after the plan and before\n")
		g.pf("/// the projection, so a forged value past what that writer could have\n")
		g.pf("/// written REFUSES out_of_range, and the projection then holds this\n")
		g.pf("/// reader's own range.\n")
		g.emitFn("void", names[i], []string{"ByteData view", "TableFixedReport report"})
		for k, b := range rows {
			at := fmt.Sprintf("%d", b.at)
			n := fmt.Sprintf("n%d", k)
			if b.flt {
				// A compressed float: the getter reads the field's own type
				// (a float32 exactly), the bounds are narrowed to it, and a
				// NaN fails both comparisons, so it refuses too.
				get, _ := fixedGetter(b.field.Type)
				g.call("  ", "final "+n+" = ", "view."+get, []string{at, "Endian.little"}, ";")
				g.pf("  if (!(%s >= %s && %s <= %s)) {\n", n, b.flo, n, b.fhi)
				g.pf("    report.refused = TableFixedRefusal.outOfRange;\n")
				g.pf("  }\n")
				continue
			}
			if fixedIs128(b.field.Type) {
				cls := "UInt128"
				if fixedSigned(b.field.Type) {
					cls = "Int128"
				}
				g.needClass(cls)
				g.pf("  final %s = %s(\n", n, cls)
				g.pf("    view.getUint64(%d, Endian.little),\n", b.at+8)
				g.pf("    view.getUint64(%d, Endian.little),\n", b.at)
				g.pf("  );\n")
			} else {
				get, le := fixedGetter(b.field.Type)
				args := []string{at}
				if le {
					args = append(args, "Endian.little")
				}
				g.call("  ", "final "+n+" = ", "view."+get, args, ";")
			}
			lead := "  if"
			for _, end := range []struct {
				on bool
				op string
				v  *big.Int
			}{{b.low, "<", b.lo}, {b.high, ">", b.hi}} {
				if !end.on {
					continue
				}
				lhs, cmp, assign := g.gen.fixedClampTerms(b.field.Type, n, end.v)
				g.pf("%s (%s %s %s) {\n", lead, lhs, end.op, cmp)
				if fixedIs128(b.field.Type) {
					loHalf, hiHalf := fixed128Halves(end.v)
					g.call("    ", "", "view.setUint64", []string{at, fixedHexLit(loHalf), "Endian.little"}, ";")
					g.call("    ", "", "view.setUint64", []string{fmt.Sprintf("%d", b.at+8), fixedHexLit(hiHalf), "Endian.little"}, ";")
				} else {
					set, le := fixedSetter(b.field.Type)
					put := []string{at, assign}
					if le {
						put = append(put, "Endian.little")
					}
					g.call("    ", "", "view."+set, put, ";")
				}
				g.pf("    report.refused = TableFixedRefusal.outOfRange;\n")
				g.pf("  }")
				lead = " else if"
			}
			g.pf("\n")
		}
		g.pf("}\n\n")
	}
	g.pf("/// THE WRITER'S RANGES BY LINEAGE ENTRY, null where an entry bounds nothing.\n")
	g.pf("%s", fixedFormatOff)
	g.pf("const List<%s?> %sFixedWriterBounds = [\n", fixedWriterBoundsType, lower)
	g.pf("  %s,\n", strings.Join(names, ", "))
	g.pf("];\n")
	g.pf("%s\n", fixedFormatOn)
	return true
}
