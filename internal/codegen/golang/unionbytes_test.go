package golang

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestGoUnionThenFixedByteArrayWire writes a packet whose fixed byte array
// follows a union at None. The union occupies one bit, so the four bytes ride
// in bits 1..32 and the packet is 33 bits. The probe pins the per-byte layout
// and reads the value back.
func TestGoUnionThenFixedByteArrayWire(t *testing.T) {
	t.Parallel()
	files := generateGo(t, "UnionBytes", "package t\n\n"+
		"type Small {\n    v uint8 | min = 0, max = 127\n}\n\n"+
		"union U {\n    a Small\n}\n\n"+
		"type T {\n    align\n    u U\n    data [4]uint8\n}\n")
	runUnionThenByteArrayProbe(t, files)
}

// runUnionThenByteArrayProbe compiles the generated Go and runs it against the
// serialize.go runtime: T with u at None and data {1, 2, 3, 4} writes 33 bits —
// the one-bit tag then four bytes packed LSB-first — and reads back unchanged.
// Behind slowtest.Gate — it shells out to the Go toolchain.
func runUnionThenByteArrayProbe(t *testing.T, files map[string][]byte) {
	t.Helper()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated writer)")
	const probe = `package t

import (
	"bytes"
	"testing"

	"github.com/mas-bandwidth/serialize.go/v2"
)

func TestUnionThenByteArrayWire(t *testing.T) {
	value := T{U: U{Type: UTypeNone}, Data: [4]uint8{1, 2, 3, 4}}
	stream := serialize.NewWriteStream(make([]byte, 4096))
	if err := WriteT(stream, &value); err != nil {
		t.Fatalf("write: %v", err)
	}
	stream.Flush()
	if err := stream.Err(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if got := stream.BitsProcessed(); got != 33 {
		t.Errorf("wrote %d bits, want 33 — the one-bit tag then four bytes", got)
	}
	want := []byte{0x02, 0x04, 0x06, 0x08, 0x00}
	if wire := stream.Data(); !bytes.Equal(wire, want) {
		t.Errorf("wire = % x, want % x", wire, want)
	}
	var got T
	if err := ReadT(serialize.NewReadStream(stream.Data()), &got); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.U.Type != UTypeNone {
		t.Errorf("read u.Type = %v, want None", got.U.Type)
	}
	if got.Data != value.Data {
		t.Errorf("read data = %v, want %v", got.Data, value.Data)
	}
}
`
	runtime := slowtest.SerializeGo(t, "../../../")
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["unionbytes_test.go"] = []byte(probe)

	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated writer failed its probe: %v\n%s", err, out)
	}
}
