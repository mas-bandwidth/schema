package gotable

import (
	"strings"
	"testing"
)

const matchedWidthSchema = `package probe
table Root {
 a uint16
 b int32
}
`

func TestMatchedScalarWidthShape(t *testing.T) {
	files := generate(t, matchedWidthSchema)
	body := ""
	for name, data := range files {
		if strings.HasSuffix(name, "Table.go") {
			body += string(data)
		}
	}
	load := funcSource(body, "func RootLoadBody")
	if load == "" {
		t.Fatal("RootLoadBody was not emitted")
	}
	for _, want := range []string{
		"r.Get16()",
		"r.Get32()",
		"r.Has(2)",
		"r.Has(4)",
		"tableKindWidens(kind, 7)",
		"tableKindWidens(kind, 4)",
		"r.Unsigned(kind)",
		"r.Signed(kind)",
	} {
		if !strings.Contains(load, want) {
			t.Fatalf("matched-width LoadBody is missing %q", want)
		}
	}
	rest := stripWidenArms(load)
	if strings.Contains(rest, "Unsigned(kind)") {
		t.Fatal("Unsigned(kind) escaped the widen arm")
	}
	if strings.Contains(rest, "Signed(kind)") {
		t.Fatal("Signed(kind) escaped the widen arm")
	}
	if strings.Contains(rest, "tableKindBytes(kind)") {
		t.Fatal("matched arm still sizes the payload through tableKindBytes(kind)")
	}
	runGenerated(t, matchedWidthSchema, matchedWidthWireTest)
}

func funcSource(src, sig string) string {
	start := strings.Index(src, sig)
	if start < 0 {
		return ""
	}
	brace := strings.Index(src[start:], "{")
	if brace < 0 {
		return ""
	}
	i := start + brace
	depth := 0
	for j := i; j < len(src); j++ {
		switch src[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start : j+1]
			}
		}
	}
	return src[start:]
}

func stripWidenArms(src string) string {
	const needle = "tableKindWidens("
	var b strings.Builder
	for {
		i := strings.Index(src, needle)
		if i < 0 {
			b.WriteString(src)
			return b.String()
		}
		b.WriteString(src[:i])
		j := strings.Index(src[i:], "{")
		if j < 0 {
			return b.String()
		}
		j += i
		depth := 0
		closed := false
		for k := j; k < len(src); k++ {
			switch src[k] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					src = src[k+1:]
					closed = true
				}
			}
			if closed {
				break
			}
		}
		if !closed {
			return b.String()
		}
	}
}

const matchedWidthWireTest = `package probe
import "testing"

func finish(w *TableWriter) []byte {
	w.Put8(0)
	w.Trailer()
	return w.Buffer[:w.Offset]
}

func TestMatchedWidthRoundTrip(t *testing.T) {
	var value, loaded Root
	value.A = 0x1234
	value.B = -400
	n := RootMeasure(&value)
	if n < 0 {
		t.Fatal("measure")
	}
	wire := make([]byte, n)
	if RootSave(&value, wire) != n {
		t.Fatal("save")
	}
	var report TableReport
	if !RootLoad(&loaded, wire, &report) || report != (TableReport{}) || loaded.A != 0x1234 || loaded.B != -400 {
		t.Fatalf("round trip %+v loaded=%+v", report, loaded)
	}
}

func TestWidenArmReadsWireWidth(t *testing.T) {
	var ids TableIds
	buf := make([]byte, 256)
	w := TableWriter{Buffer: buf, Ids: &ids}
	w.Put8(1)
	w.Id(RootTableFields[0].Id)
	w.Put8(6)
	w.Put8(9)
	w.Id(RootTableFields[1].Id)
	w.Put8(2)
	w.Put8(7)
	got := finish(&w)
	var loaded Root
	var report TableReport
	if !RootLoad(&loaded, got, &report) || report.Widened != 2 || report.KindMismatch != 0 || report.Malformed || loaded.A != 9 || loaded.B != 7 {
		t.Fatalf("widen: a=%d b=%d report=%+v", loaded.A, loaded.B, report)
	}
}

func TestMatchedWidthMismatchSkips(t *testing.T) {
	var ids TableIds
	buf := make([]byte, 256)
	w := TableWriter{Buffer: buf, Ids: &ids}
	w.Put8(1)
	w.Id(RootTableFields[1].Id)
	w.Put8(1)
	w.Put8(1)
	got := finish(&w)
	var loaded Root
	var report TableReport
	if !RootLoad(&loaded, got, &report) || report.KindMismatch != 1 || report.Widened != 0 || loaded.B != 0 {
		t.Fatalf("mismatch: b=%d report=%+v", loaded.B, report)
	}
}
`
