package gotable

import (
	"strings"
	"testing"
)

const putLebSchema = `package probe
table Root {
 payload bytes(20000)
}
`

func TestPutLebAssemblesOnce(t *testing.T) {
	files := generate(t, putLebSchema)
	body := ""
	for name, data := range files {
		if strings.HasSuffix(name, "Table.go") {
			body += string(data)
		}
	}
	for _, want := range []string{"func (w *TableWriter) PutLeb(v uint64)", "func (w *TableWriter) putLebPair(v uint64) bool", "var b [10]byte", "w.Raw(b[:n])"} {
		if !strings.Contains(body, want) {
			t.Fatalf("PutLeb does not assemble then Raw once: missing %q", want)
		}
	}
	if strings.Contains(body, "for v >= 128 { w.Put8") {
		t.Fatal("PutLeb still writes one group at a time through Put8")
	}
	runGenerated(t, putLebSchema, putLebWireTest)
}

const putLebWireTest = `package probe
import ("bytes"; "encoding/binary"; "hash/fnv"; "testing")

func TestPutLebBytesAndOverflow(t *testing.T) {
	h := fnv.New64a()
	h.Write([]byte("payload"))
	payloadId := h.Sum64()
	var value, loaded Root
	for _, n := range []int32{0, 1, 127, 128, 255, 16383, 16384, 20000} {
		RootReset(&value)
		value.PayloadLength = n
		for i := int32(0); i < n; i++ {
			value.Payload[i] = byte(i)
		}
		want := []byte{1}
		if n > 0 {
			want = append(want, 1, 14)
			body := binary.AppendUvarint([]byte{6}, uint64(n))
			body = append(body, value.Payload[:n]...)
			want = binary.AppendUvarint(want, uint64(len(body)))
			want = append(want, body...)
		}
		want = append(want, 0)
		if n > 0 {
			want = binary.LittleEndian.AppendUint64(want, payloadId)
			want = binary.LittleEndian.AppendUint64(want, 1)
		} else {
			want = binary.LittleEndian.AppendUint64(want, 0)
		}
		got := make([]byte, len(want))
		if RootMeasure(&value) != int64(len(want)) || RootSave(&value, got) != int64(len(want)) || !bytes.Equal(got, want) {
			t.Fatalf("length %d wire differs\n  want %x\n   got %x", n, want, got)
		}
		var report TableReport
		if !RootLoad(&loaded, want, &report) || report != (TableReport{}) || loaded.PayloadLength != n || !bytes.Equal(loaded.Payload[:n], value.Payload[:n]) {
			t.Fatalf("length %d load differs: %+v", n, report)
		}
		if RootSave(&value, make([]byte, len(want)-1)) != -1 || RootSave(&value, nil) != -1 {
			t.Fatalf("length %d short buffer accepted", n)
		}
	}
	RootReset(&value)
	value.PayloadLength = 16384
	wire := make([]byte, RootMeasure(&value))
	if n := testing.AllocsPerRun(50, func() {
		if RootMeasure(&value) != int64(len(wire)) || RootSave(&value, wire) != int64(len(wire)) {
			panic("round trip")
		}
	}); n != 0 {
		t.Fatalf("PutLeb allocated %v", n)
	}
}
`
