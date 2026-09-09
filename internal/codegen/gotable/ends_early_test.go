package gotable

import (
	"strings"
	"testing"
)

const endsEarlySchema = `package probe
table Root {
 a int32 = 5
 b float32
}
`

func TestEndsEarlyShape(t *testing.T) {
	files := generate(t, endsEarlySchema)
	body := ""
	for name, data := range files {
		if strings.HasSuffix(name, "Table.go") {
			body += string(data)
		}
	}
	for _, want := range []string{
		"before := *report",
		"probe.Offset = 0",
		"probe.EndsEarly()",
		"r.Offset != int64(len(r.Buffer))",
		"report.Verdict = TableOpenDamaged",
		"report.Verdict = TableOpenBodyStopped",
		"func tableOpenFramed(",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("root Load is missing %q", want)
		}
	}
	open, _, _ := strings.Cut(body, "func tableOpenFramed(")
	if strings.Contains(open, "r.EndsEarly()") {
		t.Fatal("tableOpen still pre-walks EndsEarly; the root Load must answer it")
	}
	runGenerated(t, endsEarlySchema, endsEarlyWireTest)
}

const endsEarlyWireTest = `package probe
import ("testing")

func finish(w *TableWriter, trailing int) []byte {
	w.Put8(0)
	for i := 0; i < trailing; i++ {
		w.Put8(0x11)
	}
	w.Trailer()
	return w.Buffer[:w.Offset]
}

func checkEarly(t *testing.T, name string, wire []byte) {
	t.Helper()
	var ignored TableReport
	r, open := tableOpen(wire, &ignored)
	var value Root
	var report TableReport
	ok := RootLoad(&value, wire, &report)
	if open != TableOpenOk {
		if report.Verdict != open {
			t.Fatalf("%s: open %d load %d", name, open, report.Verdict)
		}
		return
	}
	probe := r
	probe.Offset = 0
	early := probe.EndsEarly()
	if (report.Verdict == TableOpenDamaged) != early {
		t.Fatalf("%s: framing early=%v load verdict=%v malformed=%v", name, early, report.Verdict, report.Malformed)
	}
	if !early {
		return
	}
	if ok || !report.Malformed || value.A != 5 || report.Unknown != 0 || report.KindMismatch != 0 || report.Widened != 0 || report.Clamped != 0 || report.Duplicate != 0 {
		t.Fatalf("%s: damaged path kept work: ok=%v a=%d report=%+v", name, ok, value.A, report)
	}
}

func TestRootReadAnswersOwnEarlyEnd(t *testing.T) {
	var src Root
	src.A = 42
	src.B = 3.25
	wire := make([]byte, RootMeasure(&src))
	if n := RootSave(&src, wire); n != int64(len(wire)) {
		t.Fatalf("save %d", n)
	}
	checkEarly(t, "whole", wire)

	var ignored TableReport
	r, open := tableOpen(wire, &ignored)
	if open != TableOpenOk {
		t.Fatalf("open %d", open)
	}
	bodyBytes := len(r.Buffer)
	for k := 0; k < bodyBytes; k++ {
		forged := append([]byte(nil), wire...)
		forged[1+k] = 0
		checkEarly(t, "terminator", forged)
	}
	for i := range wire {
		for _, v := range []byte{0, 1, 2, 127, 128, 255} {
			forged := append([]byte(nil), wire...)
			forged[i] = v
			checkEarly(t, "mutate", forged)
		}
	}

	for trailing := 0; trailing < 3; trailing++ {
		var ids TableIds
		buf := make([]byte, 256)
		w := TableWriter{Buffer: buf, Ids: &ids}
		w.Put8(1)
		w.Id(RootTableFields[0].Id)
		w.Put8(4)
		w.Put32(1)
		w.Id(RootTableFields[1].Id)
		w.Put8(4)
		w.Put32(2)
		got := finish(&w, trailing)
		checkEarly(t, "mismatch-then-end", got)
		var value Root
		var report TableReport
		ok := RootLoad(&value, got, &report)
		if trailing == 0 {
			if !ok || report.Verdict != TableOpenOk || value.A != 1 || report.KindMismatch != 1 || report.Malformed {
				t.Fatalf("two fields no trailing: ok=%v a=%d report=%+v", ok, value.A, report)
			}
		} else if ok || report.Verdict != TableOpenDamaged || value.A != 5 || report.KindMismatch != 0 || !report.Malformed {
			t.Fatalf("two fields trailing %d: ok=%v a=%d report=%+v", trailing, ok, value.A, report)
		}
	}

	{
		var ids TableIds
		buf := make([]byte, 256)
		w := TableWriter{Buffer: buf, Ids: &ids}
		w.Put8(1)
		w.Id(0x1111111111111111)
		w.Put8(4)
		w.Put32(7)
		w.Id(RootTableFields[0].Id)
		w.Put8(4)
		w.Put32(11)
		clean := finish(&w, 0)
		var value Root
		var report TableReport
		if !RootLoad(&value, clean, &report) || report.Unknown != 1 || value.A != 11 || report.Malformed {
			t.Fatalf("unknown kept: a=%d report=%+v", value.A, report)
		}
		var ids2 TableIds
		buf2 := make([]byte, 256)
		w2 := TableWriter{Buffer: buf2, Ids: &ids2}
		w2.Put8(1)
		w2.Id(0x1111111111111111)
		w2.Put8(4)
		w2.Put32(7)
		w2.Id(RootTableFields[0].Id)
		w2.Put8(4)
		w2.Put32(11)
		early := finish(&w2, 1)
		checkEarly(t, "unknown-then-early-end", early)
		var after Root
		var damaged TableReport
		if RootLoad(&after, early, &damaged) || damaged.Verdict != TableOpenDamaged || damaged.Unknown != 0 || after.A != 5 {
			t.Fatalf("unknown then early: a=%d report=%+v", after.A, damaged)
		}
	}

	for trailing := 0; trailing < 3; trailing++ {
		var ids TableIds
		buf := make([]byte, 256)
		w := TableWriter{Buffer: buf, Ids: &ids}
		w.Put8(1)
		w.Id(RootTableFields[0].Id)
		w.Put8(4)
		w.Put32(77)
		w.Id(0xfffffffffffffffe)
		w.Put8(9)
		w.Put64(1)
		got := finish(&w, trailing)
		checkEarly(t, "reserved-id-before-early-end", got)
		var value Root
		var report TableReport
		ok := RootLoad(&value, got, &report)
		if trailing == 0 {
			if ok || report.Verdict != TableOpenBodyStopped || !report.Malformed || value.A != 77 {
				t.Fatalf("reserved trailing 0: ok=%v a=%d report=%+v", ok, value.A, report)
			}
		} else if ok || report.Verdict != TableOpenDamaged || !report.Malformed || value.A != 5 || report.Unknown != 0 || report.KindMismatch != 0 {
			t.Fatalf("reserved trailing %d: ok=%v a=%d report=%+v", trailing, ok, value.A, report)
		}
	}
}
`
