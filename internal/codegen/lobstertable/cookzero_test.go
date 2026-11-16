package lobstertable_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// deadSpan is a run of cooked bytes the writer must leave zero: an absent
// optional's payload, or a counted array's slots past its live count.
type deadSpan struct {
	from, to int64
	what     string
}

// cookDeadSpans walks one record's by-value storage at base, as the
// reference's regionWriter does, and names every span that is DEAD BYTES
// (docs/SPEC-TABLES.md §7.2; docs/rulings.json fixed-dead-bytes-are-zero).
func cookDeadSpans(u *ir.Unit, st *ir.Struct, inst *tabletext.Instance, base int64) []deadSpan {
	var out []deadSpan
	ml := ir.RecordLayout(u, st)
	for i := range ml.Fields {
		fl := &ml.Fields[i]
		f := fl.Field
		var fv *tabletext.Field
		for j := range inst.Fields {
			if inst.Fields[j].Def == f {
				fv = &inst.Fields[j]
			}
		}
		pieces := ir.FieldPieces(u, f, base+fl.Offset)
		if fv == nil || len(pieces) == 0 {
			continue
		}
		if f.Type.Optional && !fv.Present {
			// every piece but the presence byte, which is the last
			for _, p := range pieces[:len(pieces)-1] {
				out = append(out, deadSpan{p.Offset, p.Offset + p.Size, "absent optional " + f.Name})
			}
			continue
		}
		if f.IsMap() || f.IsList() || f.Type.Pointer || f.Type.Blob() || f.Type.TextUnbounded() {
			continue
		}
		value := pieces[0]
		if f.Array == ir.ArrayNone {
			out = append(out, cookDeadElem(u, f, &fv.Cell, value.Offset)...)
			continue
		}
		step := value.Size / f.ArrayBound
		live := len(fv.Elems)
		if f.Array == ir.ArrayCounted {
			live = min(fv.Count, live)
			if int64(live) < f.ArrayBound {
				out = append(out, deadSpan{value.Offset + int64(live)*step, value.Offset + f.ArrayBound*step, "dead slots of " + f.Name})
			}
		}
		for k := range live {
			out = append(out, cookDeadElem(u, f, &fv.Elems[k], value.Offset+int64(k)*step)...)
		}
	}
	return out
}

func cookDeadElem(u *ir.Unit, f *ir.Field, cell *tabletext.Cell, at int64) []deadSpan {
	if f.Type.Kind != ir.TNamed || cell.Tab == nil {
		return nil
	}
	if ref, ok := f.Type.Ref.(*ir.Struct); ok {
		return cookDeadSpans(u, ref, cell.Tab, at)
	}
	return nil
}

// TestLobsterCookWriteZeroesDeadAndAbsentBytes cooks keyed_config (absent
// optionals) and root_full (counted arrays below their bound) through the
// generated Lobster writer in both byte orders and requires every absent
// optional's payload and every dead counted slot to be zero, and the whole
// file to be the committed cook-write fixture (docs/SPEC-TABLES.md §7.2,
// §7.6; docs/rulings.json fixed-dead-bytes-are-zero).
func TestLobsterCookWriteZeroesDeadAndAbsentBytes(t *testing.T) {
	t.Parallel()
	want := map[string]string{"keyed_config": "absent optional", "root_full": "dead slots"}
	seen := 0
	for _, r := range cookWriteRows(t) {
		kind, ok := want[r.name]
		if !ok {
			continue
		}
		seen++
		g := generateCookUnit(t, r)
		m := tabletext.NewModel(g.u)
		inst := m.New(m.Lookup(r.root))
		var rep tabletext.Report
		if g.u.Tables[r.root].FixedDeclared {
			text, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "json", r.name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if !m.Read(inst, text, &rep) {
				t.Fatalf("%s: the reference refuses the fixture text", r.name)
			}
		} else {
			wire, err := os.ReadFile(filepath.Join(root, r.wire))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tablewire.Decode(m, inst, wire, &rep); err != nil {
				t.Fatalf("%s: the reference refuses the wire: %v", r.name, err)
			}
		}
		// the root record is the region's first node, after the 64-byte header
		spans := cookDeadSpans(g.u, g.u.Tables[r.root], inst, 64)
		n := 0
		for _, s := range spans {
			if strings.HasPrefix(s.what, kind) {
				n++
			}
		}
		if n == 0 {
			t.Fatalf("%s: the instance holds no %s span to check", r.name, kind)
		}
		out := runCookProbe(t, g, r, []byte{0})
		for _, order := range []struct {
			name    string
			fixture string
			got     []byte
		}{{"little", r.little, out.little}, {"big", r.big, out.big}} {
			fix, err := os.ReadFile(filepath.Join(root, order.fixture))
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range spans {
				if s.to > int64(len(fix)) || !bytes.Equal(fix[s.from:s.to], make([]byte, s.to-s.from)) {
					t.Fatalf("%s %s: the fixture's %s at [%d,%d) is not zero: the span walk is wrong", r.name, order.name, s.what, s.from, s.to)
				}
				if s.to > int64(len(order.got)) || !bytes.Equal(order.got[s.from:s.to], make([]byte, s.to-s.from)) {
					t.Errorf("%s %s: the Lobster cook's %s at [%d,%d) is not zero: %x", r.name, order.name, s.what, s.from, s.to, order.got[min(s.from, int64(len(order.got))):min(s.to, int64(len(order.got)))])
				}
			}
			if !bytes.Equal(order.got, fix) {
				t.Errorf("%s %s: the Lobster cook is not the fixture: %s", r.name, order.name, divergence(string(order.got), string(fix)))
			}
		}
		t.Logf("%s: %d spans checked zero in both orders (%d %s)", r.name, len(spans), n, kind)
	}
	if seen != len(want) {
		t.Fatalf("the manifest names %d of the %d cook-write rows this test reads", seen, len(want))
	}
}
