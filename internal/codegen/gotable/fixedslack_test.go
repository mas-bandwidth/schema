package gotable

import (
	"fmt"
	"strings"
	"testing"
)

// THE SLOTS PAST A COUNT AFTER ANY LOAD, LAWFUL OR REFUSED (docs/SPEC-TABLES.md
// §3.4): test/tables/FixedSlack.schema's recipe, run on the generated Go. A
// value that held file A's full arrays, loaded with file B's short ones, reads
// back exactly as a fresh value loaded with B: every slot past every count
// holds its declared default (id 7, v 3), never the writer's zeros and never
// the 77s it held. The identity plan and a lineage plan (an older writer whose
// SlackRoot lacks the reader's appended `tail`) are both read, and a refused
// load still resets every slot to the declared defaults.
func TestGoFixedSlackPastCountHoldsDefaults(t *testing.T) {
	t.Parallel()
	older := readSchema(t, "FixedSlack")
	reader := strings.Replace(older, "nums  [..4]int32\n", "nums  [..4]int32\n    tail  int32\n", 1)
	if reader == older {
		t.Fatal("the fixture's SlackRoot changed shape: the reader's appended field did not land")
	}
	writer, ok := FixedLineageOf(unitOf(t, older), "SlackRoot")
	if !ok {
		t.Fatal("no lineage entry for the older SlackRoot")
	}
	src := fmt.Sprintf(`package probe

import (
	"encoding/binary"
	"reflect"
	"testing"
	"unsafe"
)

func save(t *testing.T, v SlackRoot) []byte {
	t.Helper()
	out := make([]byte, SlackRootFixedMeasure(1))
	if n := SlackRootFixedSave([]SlackRoot{v}, out); n != int64(len(out)) {
		t.Fatalf("save wrote %%d of %%d", n, len(out))
	}
	return out
}

func loadInto(v *SlackRoot, data []byte, report *TableReport) int64 {
	values := unsafe.Slice(v, 1) // the caller's own value, reused
	return SlackRootFixedLoad(values, data, make([]TableFixedEntry, SlackRootFixedPlanMax), report)
}

// older rewrites a file this reader saved as the older writer's: its hash, its
// layout, and the record body without the reader's appended tail.
func older(data []byte) []byte {
	layout := []byte(%q)
	hash := uint64(0x%016x)
	record := %d
	body := data[TableFixedHeaderBytes+4+len(SlackRootFixedLayout)+8:]
	out := make([]byte, TableFixedHeaderBytes+4)
	out[0] = TableFixedForm
	binary.LittleEndian.PutUint64(out[TableFixedHashAt:], hash)
	binary.LittleEndian.PutUint32(out[TableFixedHeaderBytes:], uint32(len(layout)))
	out = append(out, layout...)
	out = binary.LittleEndian.AppendUint64(out, hash)
	return append(out, body[:record-8]...)
}

func TestSlack(t *testing.T) {
	var a, b SlackRoot
	SlackRootReset(&a)
	a.CellsCount = 3
	for i := range a.Cells {
		a.Cells[i].ID = 77
		a.Cells[i].SubsCount = 2
		for j := range a.Cells[i].Subs {
			a.Cells[i].Subs[j].V = 77
		}
	}
	a.NumsCount = 4
	for i := range a.Nums {
		a.Nums[i] = 77
	}
	SlackRootReset(&b)
	b.CellsCount = 1
	b.Cells[0].ID = 77
	b.Cells[0].SubsCount = 1
	b.Cells[0].Subs[0].V = 77
	b.NumsCount = 1
	b.Nums[0] = 77
	fileA, fileB := save(t, a), save(t, b)
	for _, plan := range []struct {
		name string
		a, b []byte
	}{{"identity", fileA, fileB}, {"lineage", older(fileA), older(fileB)}} {
		var report TableReport
		var fresh SlackRoot
		SlackRootReset(&fresh)
		if n := loadInto(&fresh, plan.b, &report); n != 1 {
			t.Fatalf("%%s: B into a fresh value: n=%%d report=%%+v", plan.name, n, report)
		}
		var v SlackRoot
		SlackRootReset(&v)
		if n := loadInto(&v, plan.a, &report); n != 1 || v.CellsCount != 3 || v.Cells[2].Subs[1].V != 77 {
			t.Fatalf("%%s: A did not load whole: n=%%d report=%%+v value=%%+v", plan.name, n, report, v)
		}
		if n := loadInto(&v, plan.b, &report); n != 1 {
			t.Fatalf("%%s: B into the reused value: n=%%d report=%%+v", plan.name, n, report)
		}
		for _, i := range []int{1, 2} {
			c := v.Cells[i]
			if c.ID != 7 || c.SubsCount != 0 || c.Subs[0].V != 3 || c.Subs[1].V != 3 {
				t.Fatalf("%%s: cells[%%d] past the count is %%+v, want id 7, subs count 0, every sub v 3", plan.name, i, c)
			}
		}
		if got := v.Cells[0].Subs[1].V; got != 3 {
			t.Fatalf("%%s: cells[0].subs[1].v past its count is %%d, want 3", plan.name, got)
		}
		for i := 1; i < 4; i++ {
			if v.Nums[i] != 0 {
				t.Fatalf("%%s: nums[%%d] past the count is %%d, want 0", plan.name, i, v.Nums[i])
			}
		}
		if !reflect.DeepEqual(v, fresh) {
			t.Fatalf("%%s: the reused value is not the fresh one:\n reused %%+v\n fresh  %%+v", plan.name, v, fresh)
		}
		// A REFUSED LOAD (a record whose hash is not the header's) resets the
		// value handed in to its declared defaults.
		if n := loadInto(&v, plan.a, &report); n != 1 {
			t.Fatalf("%%s: A again: n=%%d", plan.name, n)
		}
		bad := append([]byte(nil), plan.b...)
		bad[TableFixedHeaderBytes+4+int(binary.LittleEndian.Uint32(bad[TableFixedHeaderBytes:]))] ^= 0xff // the record's hash
		if n := loadInto(&v, bad, &report); n >= 0 || report.Err() == nil {
			t.Fatalf("%%s: the forged record read: n=%%d report=%%+v", plan.name, n, report)
		}
		var reset SlackRoot
		SlackRootReset(&reset)
		if !reflect.DeepEqual(v, reset) || v.Cells[1].ID != 7 || v.Cells[0].Subs[1].V != 3 {
			t.Fatalf("%%s: the refused load left %%+v, want the declared defaults", plan.name, v)
		}
	}
}
`, string(writer.Layout), writer.Wire, writer.Record)
	out, err := runVersionProbe(t, reader, []string{older}, src)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// The emitter pin, with no toolchain: FixedSlack.schema's generated load
// carries the slack pass at both depths, and a unit whose counted arrays all
// have all-zero fresh elements generates no slack code at all.
func TestGoFixedSlackEmitterPin(t *testing.T) {
	t.Parallel()
	text := generatedText(t, readSchema(t, "FixedSlack"))
	for _, want := range []string{
		"func SlackCellFixedSlackBody(value *SlackCell) {",
		"SlackSubReset(&value.Subs[i])",
		"func SlackRootFixedSlackBody(value *SlackRoot) {",
		"SlackCellFixedSlackBody(&value.Cells[i])",
		"SlackCellReset(&value.Cells[i])",
		"SlackRootFixedSlackBody(&values[k : k+1][0])",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the generated FixedSlack unit lacks %q", want)
		}
	}
	zero := generatedText(t, `package zeroslack

fixed table Cell
{
    id   int32
    subs [..2]int16
}

fixed table Root
{
    cells [..3]Cell
    nums  [..4]int32
}
`)
	if strings.Contains(zero, "FixedSlackBody") {
		t.Fatal("a unit whose counted arrays all have all-zero fresh elements generated slack code")
	}
}
