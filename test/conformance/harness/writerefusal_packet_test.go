// THE PACKET WRITE-REFUSAL SURFACE (docs/SPEC.md §5.1).
//
// The four write-refusal surfaces drive the table writers. This surface drives
// the packet writer: one type carries the values the contract refuses and the
// lawful value at each boundary beside it, and the generated Go leg executes
// every write. A writer that accepted an out-of-contract value, or refused a
// lawful one, fails by row name.
//
// The negative control removes the emitted high-bound range comparison from a
// copy of the Go emitter and requires this surface to go red. Nothing tracked
// is written to: the copy travels through a Go overlay.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// writeRefusalPacketSchema is the smallest unit that reaches every refused
// row: a ranged integer and a fixed raw value, a counted array, a text length,
// an enum ordinal and a union tag, with the storage-less const and reserved
// items the writer emits beside them.
const writeRefusalPacketSchema = `package refusal

enum Grade { Bronze, Silver, Gold }

type Arm
{
    value int32 | min = 0, max = 5
}

type Plain
{
    value uint16
}

union Choice
{
    arm   Arm
    plain Plain
}

type RefusalPacket
{
    const(0xAB, 8)
    reserved(5)
    ranged   int32        | min = 0, max = 10
    channel  bits(6)
    items    [..4]uint16
    text     string(8)
    blob     bytes(4)
    grade    Grade
    choice   Choice
    fixedval fixed(16, 16) | min = -8, max = 8
}
`

// writeRefusalPacketProbe is the generated module's own test. Each refused row
// owes serialize.ErrValueOutOfRange, and each row beside it carries the lawful
// value at the same boundary, which must be written.
const writeRefusalPacketProbe = `package refusal

import (
	"errors"
	"testing"

	"github.com/mas-bandwidth/serialize.go/v2"
)

func writeRefusalPacket(value *RefusalPacket) error {
	stream := serialize.NewWriteStream(make([]byte, 1024))
	return WriteRefusalPacket(stream, value)
}

func writeRefusalPacketRefused(t *testing.T, row string, value *RefusalPacket) {
	t.Helper()
	if err := writeRefusalPacket(value); !errors.Is(err, serialize.ErrValueOutOfRange) {
		t.Fatalf("write-refusal-packet: %s: got %v, want serialize.ErrValueOutOfRange", row, err)
	}
}

func writeRefusalPacketWrote(t *testing.T, row string, value *RefusalPacket) {
	t.Helper()
	if err := writeRefusalPacket(value); err != nil {
		t.Fatalf("write-refusal-packet: %s: the writer refused a lawful value: %v", row, err)
	}
}

func TestWriteRefusalPacket(t *testing.T) {
	// row 1: a ranged scalar and a fixed raw value, off both ends and at both.
	writeRefusalPacketRefused(t, "ranged-high", &RefusalPacket{Ranged: 11})
	writeRefusalPacketWrote(t, "ranged-max", &RefusalPacket{Ranged: 10})
	writeRefusalPacketRefused(t, "ranged-low", &RefusalPacket{Ranged: -1})
	writeRefusalPacketWrote(t, "ranged-min", &RefusalPacket{Ranged: 0})

	writeRefusalPacketRefused(t, "fixed-high", &RefusalPacket{Fixedval: 9 << 16})
	writeRefusalPacketWrote(t, "fixed-max", &RefusalPacket{Fixedval: 8 << 16})
	writeRefusalPacketRefused(t, "fixed-low", &RefusalPacket{Fixedval: -9 << 16})
	writeRefusalPacketWrote(t, "fixed-min", &RefusalPacket{Fixedval: -8 << 16})

	// row 3: a counted array's used count, off both ends and at both.
	writeRefusalPacketRefused(t, "count-high", &RefusalPacket{ItemsCount: 5})
	writeRefusalPacketWrote(t, "count-max", &RefusalPacket{Items: [4]uint16{1, 2, 3, 4}, ItemsCount: 4})
	writeRefusalPacketRefused(t, "count-low", &RefusalPacket{ItemsCount: -1})
	writeRefusalPacketWrote(t, "count-min", &RefusalPacket{ItemsCount: 0})

	// row 4: a text length, and a byte block's length, off both ends and at both.
	writeRefusalPacketRefused(t, "text-length-high", &RefusalPacket{TextLength: 9})
	writeRefusalPacketWrote(t, "text-length-max", &RefusalPacket{Text: [8]byte{'a', 'a', 'a', 'a', 'a', 'a', 'a', 'a'}, TextLength: 8})
	writeRefusalPacketRefused(t, "text-length-low", &RefusalPacket{TextLength: -1})
	writeRefusalPacketWrote(t, "text-length-min", &RefusalPacket{TextLength: 0})

	writeRefusalPacketRefused(t, "blob-length-high", &RefusalPacket{BlobLength: 5})
	writeRefusalPacketWrote(t, "blob-length-max", &RefusalPacket{Blob: [4]byte{1, 2, 3, 4}, BlobLength: 4})
	writeRefusalPacketRefused(t, "blob-length-low", &RefusalPacket{BlobLength: -1})
	writeRefusalPacketWrote(t, "blob-length-min", &RefusalPacket{BlobLength: 0})

	// row 5: an enum ordinal outside its variant set, and the top of the set.
	writeRefusalPacketRefused(t, "enum-high", &RefusalPacket{Grade: Grade(4)})
	writeRefusalPacketWrote(t, "enum-max", &RefusalPacket{Grade: Grade(3)})
	writeRefusalPacketWrote(t, "enum-none", &RefusalPacket{Grade: Grade(0)})

	// row 6: a union tag outside its arm set, and the top of the set.
	writeRefusalPacketRefused(t, "union-tag-high", &RefusalPacket{Choice: Choice{Type: ChoiceType(3)}})
	writeRefusalPacketWrote(t, "union-tag-max", &RefusalPacket{Choice: Choice{Type: ChoiceTypePlain, Plain: Plain{Value: 7}}})
	writeRefusalPacketWrote(t, "union-tag-none", &RefusalPacket{Choice: Choice{Type: ChoiceTypeNone}})

	// and an arm's ranged payload is the same row on the packet's nested arm.
	writeRefusalPacketRefused(t, "union-arm-high", &RefusalPacket{Choice: Choice{Type: ChoiceTypeArm, Arm: Arm{Value: 6}}})
	writeRefusalPacketWrote(t, "union-arm-max", &RefusalPacket{Choice: Choice{Type: ChoiceTypeArm, Arm: Arm{Value: 5}}})

	// the storage-less items ride with a zero value: the writer emits the
	// declared constant and zero reserved bits, and does not refuse them.
	writeRefusalPacketWrote(t, "const-reserved", &RefusalPacket{})
}
`

// writeRefusalPacketSurface generates the Go leg into a module of its own and
// returns its directory.
func writeRefusalPacketSurface(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "Refusal.schema")
	if err := os.WriteFile(schemaPath, []byte(writeRefusalPacketSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	u, err := c.Load([]string{schemaPath})
	if err != nil {
		t.Fatalf("write-refusal-packet: loading the surface's schema: %v", err)
	}
	files, err := c.Generate(u, "go", compiler.Options{})
	if err != nil {
		t.Fatalf("write-refusal-packet: generating the Go leg: %v", err)
	}
	pkg := filepath.Join(dir, "refusal")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(pkg, name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runtime := slowtest.SerializeGo(t, "../../..")
	goMod := fmt.Sprintf("module refusal\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime)
	if err := os.WriteFile(filepath.Join(pkg, "go.mod"), []byte(goMod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "refusal_test.go"), []byte(writeRefusalPacketProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	return pkg
}

// TestWriteRefusalPacketSurface runs the Go leg of the packet write contract:
// every refused row owes serialize.ErrValueOutOfRange, and every lawful twin at
// the same boundary is written.
func TestWriteRefusalPacketSurface(t *testing.T) {
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated packet writer)")
	pkg := writeRefusalPacketSurface(t)
	cmd := exec.Command("go", "test", "-count=1", "-timeout=300s", ".")
	cmd.Dir = pkg
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("write-refusal-packet: the Go leg failed: %v\n%s", err, out)
	}
}

// TestWriteRefusalPacketSurfaceNegativeControl flips the emitted high-bound
// range comparison in a copy of the Go emitter and requires the surface to go
// red on a lawful boundary value. A surface that stayed green here would not be
// watching the comparison it names.
func TestWriteRefusalPacketSurfaceNegativeControl(t *testing.T) {
	out, failed := slowtest.RunSabotaged(t, "../../..", "./test/conformance/harness", "^TestWriteRefusalPacketSurface$",
		slowtest.Sabotage{
			File: "internal/codegen/golang/functions.go",
			From: `name+" > "+hi`,
			To:   `name+" >= "+hi`,
		})
	if !failed {
		t.Fatalf("NEGATIVE CONTROL FAILED: the surface stayed green with the high-bound comparison flipped\n%s", out)
	}
	if !strings.Contains(out, "write-refusal-packet:") {
		t.Fatalf("NEGATIVE CONTROL FAILED: the surface went red, but not on a packet write-refusal row\n%s", out)
	}
}
