package elixirtable

import (
	"fmt"
	"math/big"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE WRITER'S RANGE IS THE BOUND (docs/FIXED-FORM-ALGORITHM.md §4.5, §4.6,
// bill §12.5). A ranged integer read through a COMPILED lane is clamped first
// to the range ITS WRITER declared, the lineage entry the file was written
// under, and then to this reader's own range in the projection; each clamp
// that fires counts once. The writer's ranges and their match onto this
// reader's fields are the compiler's own (ir.TableFixedWriterRanges,
// ir.TableFixedWriterField), shared by every leg.

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

// emitWriterBounds lays down the writer ranges per lineage entry as one
// function of the entry's index, each range {offset, bits, sign, lo, hi} on
// the raw scale and at this reader's image offset, with an empty list for an
// entry that bounds nothing. It answers whether the table has any, so a table
// without one emits no function and no pass.
func (g *fixedGen) emitWriterBounds(st *ir.Struct, snake string, entries []FixedLineageEntry, own uint64) bool {
	all, any := fixedLineageWriterBounds(st, entries, own)
	if !any {
		return false
	}
	g.pf("  # THE WRITER'S OWN RANGES BY LINEAGE ENTRY (docs/FIXED-FORM-ALGORITHM.md\n")
	g.pf("  # §4.6, bill §12.5): held on a compiled lane's image after the plan and\n")
	g.pf("  # before the projection, so a forged value past what that writer could\n")
	g.pf("  # have written lands the WRITER's bound and counts, and the projection\n")
	g.pf("  # then holds this reader's own range.\n")
	fn := snake + "_fixed_writer_bounds"
	// `mix format` keeps one-line clauses together and sets a do/end clause
	// apart from its neighbours with a blank line on each side.
	clauses, apart := 0, false
	for i, rows := range all {
		if len(rows) == 0 {
			continue
		}
		items := make([]string, len(rows))
		for k, b := range rows {
			sign := ":unsigned"
			if ir.TableKindSigned(ir.TableScalarKind(b.field)) {
				sign = ":signed"
			}
			items[k] = fmt.Sprintf("{%d, %d, %s, %s, %s}", b.at, 8*ir.TableFixedStorageBytes(b.field.Type),
				sign, fixedIntLiteral(b.lo), fixedIntLiteral(b.hi))
		}
		list := listNode{items: items}
		one := fmt.Sprintf("  defp %s(%d), do: %s", fn, i, list.flat())
		multi := len(one) > formatWidth
		if clauses > 0 && (multi || apart) {
			g.pf("\n")
		}
		clauses++
		apart = multi
		if !multi {
			g.pf("%s\n", one)
			continue
		}
		g.pf("  defp %s(%d) do\n", fn, i)
		g.pf("    %s\n", list.render(4, 4, 0))
		g.pf("  end\n")
	}
	if apart {
		g.pf("\n")
	}
	g.pf("  defp %s(_), do: []\n\n", fn)
	return true
}
