package gotable

import (
	"strings"
	"testing"
)

const lebSchema = `package probe
table Root {
 payload bytes(20000)
}
`

func TestLebOneByteFastPathShape(t *testing.T) {
	files := generate(t, lebSchema)
	body := ""
	for name, data := range files {
		if strings.HasSuffix(name, "Table.go") {
			body += string(data)
		}
	}
	for _, want := range []string{
		"uint64(at) < uint64(len(r.Buffer))",
		"return uint64(b), true",
		"for i := 0; i < 10; i++",
		"if i > 0 && b == 0",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("Leb is missing %q", want)
		}
	}
	runGenerated(t, lebSchema, lebWireTest)
}

const lebWireTest = `package probe
import ("bytes"; "encoding/binary"; "hash/fnv"; "testing")

func TestLebFastPathAndLoop(t *testing.T) {
	type want struct {
		in     []byte
		value  uint64
		ok     bool
		offset int64
	}
	cases := []want{
		{[]byte{0}, 0, true, 1},
		{[]byte{1}, 1, true, 1},
		{[]byte{127}, 127, true, 1},
		{[]byte{127, 99}, 127, true, 1},
		{[]byte{128, 1}, 128, true, 2},
		{[]byte{255, 127}, 16383, true, 2},
		{[]byte{128, 128, 1}, 16384, true, 3},
		{[]byte{}, 0, false, 0},
		{[]byte{128}, 0, false, 0},
		{[]byte{128, 0}, 0, false, 0},
		{[]byte{128, 128, 0}, 0, false, 0},
		{[]byte{255, 255, 255, 255, 255, 255, 255, 255, 255, 1}, ^uint64(0), true, 10},
		{[]byte{255, 255, 255, 255, 255, 255, 255, 255, 255, 2}, 0, false, 0},
	}
	for _, c := range cases {
		r := TableReader{Buffer: append([]byte(nil), c.in...)}
		v, ok := r.Leb()
		if ok != c.ok || v != c.value || r.Offset != c.offset {
			t.Fatalf("Leb(%x) = %d, %t, offset %d; want %d, %t, offset %d", c.in, v, ok, r.Offset, c.value, c.ok, c.offset)
		}
	}

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
		wantBytes := []byte{1}
		if n > 0 {
			wantBytes = append(wantBytes, 1, 14)
			body := binary.AppendUvarint([]byte{6}, uint64(n))
			body = append(body, value.Payload[:n]...)
			wantBytes = binary.AppendUvarint(wantBytes, uint64(len(body)))
			wantBytes = append(wantBytes, body...)
		}
		wantBytes = append(wantBytes, 0)
		if n > 0 {
			wantBytes = binary.LittleEndian.AppendUint64(wantBytes, payloadId)
			wantBytes = binary.LittleEndian.AppendUint64(wantBytes, 1)
		} else {
			wantBytes = binary.LittleEndian.AppendUint64(wantBytes, 0)
		}
		got := make([]byte, len(wantBytes))
		if RootMeasure(&value) != int64(len(wantBytes)) || RootSave(&value, got) != int64(len(wantBytes)) || !bytes.Equal(got, wantBytes) {
			t.Fatalf("length %d wire differs\n  want %x\n   got %x", n, wantBytes, got)
		}
		var report TableReport
		if !RootLoad(&loaded, wantBytes, &report) || report != (TableReport{}) || loaded.PayloadLength != n || !bytes.Equal(loaded.Payload[:n], value.Payload[:n]) {
			t.Fatalf("length %d load differs: %+v", n, report)
		}
	}
}
`
