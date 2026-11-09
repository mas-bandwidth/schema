package tablewire

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// nestedUnknownFile is a form-1 file whose root body carries one unknown field
// of kind 13, whose body carries another, down to an empty body at level
// `levels` (the root body is level 0). wrap, when set, makes the deepest
// framed payload an array body (kind 14) holding that many int32 elements
// instead, so the array body itself sits at level `levels`.
func nestedUnknownFile(levels int, array bool) []byte {
	var inner []byte
	kind := byte(ir.TableKindTable)
	if array {
		inner = []byte{byte(ir.TableKindI32), 1, 7, 0, 0, 0}
		kind = byte(ir.TableKindArray)
	} else {
		inner = []byte{0}
	}
	for level := levels - 1; level >= 0; level-- {
		body := []byte{1, kind}
		body = binary.AppendUvarint(body, uint64(len(inner)))
		body = append(body, inner...)
		body = append(body, 0)
		inner, kind = body, byte(ir.TableKindTable)
	}
	out := append([]byte{ir.TableWireForm}, inner...)
	out = binary.LittleEndian.AppendUint64(out, 0x1234567890abcdef)
	return binary.LittleEndian.AppendUint64(out, 1)
}

const depthProbeUnit = `package depthprobe

table Probe
{
    z int32
}
`

// TestBinaryDepthBoundEveryPath holds docs/SPEC-TABLES.md §4's one nesting
// bound (binary_max_depth, ir.TableBinaryMaxDepth) on every engine read: a
// framed payload at level 128 inside an unknown field reads (the field is
// skipped and counted), and one at level 129 refuses the whole load, on the
// ordinary, region and retaining reads alike, for a table body and for an
// array body.
func TestBinaryDepthBoundEveryPath(t *testing.T) {
	t.Parallel()
	m := covModel(t, depthProbeUnit)
	root := m.Lookup("Probe")
	paths := []struct {
		name string
		read func([]byte, *tabletext.Report) (bool, error)
	}{
		{"Decode", func(b []byte, r *tabletext.Report) (bool, error) { return Decode(m, m.New(root), b, r) }},
		{"DecodeRegion", func(b []byte, r *tabletext.Report) (bool, error) { return DecodeRegion(m, m.New(root), b, r) }},
		{"DecodeRetain", func(b []byte, r *tabletext.Report) (bool, error) {
			return DecodeRetain(m, m.New(root), b, &Retain{Capacity: 1 << 16, IdCapacity: 64}, r)
		}},
	}
	for _, array := range []bool{false, true} {
		for _, levels := range []int{ir.TableBinaryMaxDepth, ir.TableBinaryMaxDepth + 1} {
			file := nestedUnknownFile(levels, array)
			want := levels <= ir.TableBinaryMaxDepth
			for _, p := range paths {
				var r tabletext.Report
				ok, err := p.read(file, &r)
				if ok != want || (ok && r.Unknown != 1) {
					t.Errorf("%s: array=%v, a framed payload at level %d: ok=%v err=%v %+v, want ok=%v", p.name, array, levels, ok, err, r, want)
				}
			}
		}
	}
}

func depthChainSchema(levels int) string {
	var b strings.Builder
	b.WriteString("package depthchain\n\ntable T0\n{\n    a T1\n}\n")
	for i := 1; i < levels; i++ {
		fmt.Fprintf(&b, "\ntype T%d\n{\n    a T%d\n}\n", i, i+1)
	}
	fmt.Fprintf(&b, "\ntype T%d\n{\n    x int32\n}\n", levels)
	return b.String()
}

// TestBinaryDepthCompilerBound holds the compiler's half of §4's bound: a
// table whose by-value nesting reaches level 128 compiles, and its deepest
// value writes and reads back through the engine; one that reaches 129 is
// refused at check time, naming the bound.
func TestBinaryDepthCompilerBound(t *testing.T) {
	t.Parallel()
	compile := func(levels int) (*ir.Unit, []error) {
		src := depthChainSchema(levels)
		f, perrs := parser.Parse("Chain.schema", []byte(src))
		if len(perrs) > 0 {
			t.Fatalf("parse: %v", perrs[0])
		}
		return check.Unit([]check.SourceFile{{Path: "Chain.schema", Name: "Chain.schema", Base: "Chain", Bytes: []byte(src), AST: f}})
	}
	u, errs := compile(ir.TableBinaryMaxDepth)
	if len(errs) > 0 {
		t.Fatalf("a chain to level %d was refused: %v", ir.TableBinaryMaxDepth, errs[0])
	}
	if got := ir.TableBinaryDepth(u.Tables["T0"]); got != ir.TableBinaryMaxDepth {
		t.Fatalf("TableBinaryDepth(T0) = %d, want %d", got, ir.TableBinaryMaxDepth)
	}
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("T0"))
	cur := inst
	for range ir.TableBinaryMaxDepth {
		cur = cur.Fields[0].Cell.Tab
	}
	cur.Fields[0].Cell.U = 7
	cur.Fields[0].Cell.I = 7
	wire, err := Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	var r tabletext.Report
	if ok, err := Decode(m, m.New(m.Lookup("T0")), wire, &r); !ok || err != nil {
		t.Fatalf("the level-%d chain does not read back: %v %+v", ir.TableBinaryMaxDepth, err, r)
	}
	_, errs = compile(ir.TableBinaryMaxDepth + 1)
	refused := false
	for _, e := range errs {
		refused = refused || strings.Contains(e.Error(), "binary_max_depth")
	}
	if !refused {
		t.Fatalf("a chain to level %d compiled: %v", ir.TableBinaryMaxDepth+1, errs)
	}
}
