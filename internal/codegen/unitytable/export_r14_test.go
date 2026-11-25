package unitytable

import (
	"math/big"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// R14Op is one compiled plan or check entry, its guards resolved out of the
// lane's pool, as fixed_r14_grown_tag_test.go reads it.
type R14Op struct {
	Code             int
	Src, Dst         int64
	Size, Width      int64
	Lo, Hi           uint64
	Guards           []R14Guard
	ReadsSourceBytes bool
}

// R14Guard is one guard link: the writer's tag at At, Width bytes wide.
type R14Guard struct {
	At, Width int64
	Value     uint64
	Mode      int
}

// R14Lane is the lane the generator lays for an older writer: its refusal
// name (empty when it compiles whole), its plan and bounds-pass entries, and
// the C# literal the generated unit would hold for it.
type R14Lane struct {
	Why    string
	Ops    []R14Op
	Checks []R14Op
	CS     string
}

// The op codes the R14 row reads by name.
const (
	R14OpCopy   = opCopy
	R14OpConst  = opConst
	R14OpRangeU = opRangeU
)

// r14Spec is the R14 vector's declaration at one union tag width: a table
// (id 1) holding one union (id 7) whose one arm (id 9) is a u32. No real
// declaration has an 8-byte union tag (its width follows the arm count), so
// the row hands the generator's compiler the declaration directly.
func r14Spec(tag int64) *fixedSpec {
	arm := &fixedSpec{name: "a", id: 9, kind: ir.TableKindU32, shape: "int", size: 4, defInt: new(big.Int)}
	un := &fixedSpec{name: "u", id: 7, kind: ir.TableKindUnion, shape: "union", size: tag + 4, tagBytes: tag, children: []*fixedSpec{arm}}
	return &fixedSpec{name: "R", id: 1, kind: ir.TableKindTable, shape: "table", size: tag + 4, children: []*fixedSpec{un}}
}

// R14GrownTagLane lays the lane for the writer's lock layout theirs (a record
// body of theirTag + 4 bytes) read by the R14 reader whose union tag is myTag
// bytes: compileFixedPlan against the reader's declaration, then
// olderWriterLane with the WRITER's declaration for the bounds pass — the
// two steps lineageLane takes for a lock entry at generation time.
func R14GrownTagLane(theirs []byte, hash uint64, theirTag, myTag int64) R14Lane {
	node, ok := parseFixedLayoutTree(theirs)
	if !ok || node == nil {
		return R14Lane{Why: "TableReport.LayoutMalformed"}
	}
	ops, guards, fills, remap, needPrefill, why := compileFixedPlan(node, r14Spec(myTag))
	if why != "" {
		return R14Lane{Why: why}
	}
	lane := olderWriterLane(fixedLane{hash: hash, layout: theirs, record: 8 + node.size}, ops, guards, fills, remap, needPrefill, r14Spec(theirTag))
	var layout strings.Builder
	byteArray(&layout, theirs)
	return R14Lane{
		Ops:    r14Ops(lane.ops, lane.guards),
		Checks: r14Ops(lane.checks, lane.checkGuards),
		CS:     laneLiteral(lane, "0", layout.String(), ""),
	}
}

func r14Ops(ops []fixedOp, pool []fixedGuard) []R14Op {
	out := make([]R14Op, len(ops))
	for i, o := range ops {
		r := R14Op{Code: o.code, Src: o.src, Dst: o.dst, Size: o.size, Width: o.width, Lo: o.lo, Hi: o.hi,
			ReadsSourceBytes: o.code != opConst}
		for _, g := range pool[o.guardOff : o.guardOff+o.guardLen] {
			r.Guards = append(r.Guards, R14Guard{At: g.at, Width: g.width, Value: g.value, Mode: g.mode})
		}
		out[i] = r
	}
	return out
}
