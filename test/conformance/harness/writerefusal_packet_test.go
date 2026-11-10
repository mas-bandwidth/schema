// Packet write refusal (docs/SPEC.md §5.1).
//
// The table write-refusal surfaces do not execute a packet Write. This one
// does: the Go leg generates one type and runs Write on a value the
// declaration does not admit, and on the lawful value at the same boundary.
//
// The negative control flips the high bound in a copy of the Go emitter and
// requires the lawful ranged maximum to be refused. The emitter on disk is
// not modified.
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

// writeRefusalPacketSchema carries the values a packet Write refuses, and the
// storage-less const and reserved items the writer emits beside them.
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
    ranged   int32 | min = 0, max = 10
    channel  bits(6)
    items    [..4]int32 | min = 0, max = 7
    text     string(8)
    blob     bytes(4)
    grade    Grade
    choice   Choice
    fixedval fixed(16, 16) | min = -8, max = 8
}
`

// writeRefusalPacketProbe is the generated module's test. A refused row owes
// serialize.ErrValueOutOfRange, except a fixed raw value, which the generated
// writer hands to SerializeFixed64 and the runtime refuses by panicking. Each
// boundary has a lawful twin that must be written.
const writeRefusalPacketProbe = `package refusal

import (
	"errors"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/serialize.go/v2"
)

func writeRefusalPacket(value *RefusalPacket) (err error, panicked any) {
	defer func() {
		if r := recover(); r != nil {
			panicked = r
		}
	}()
	stream := serialize.NewWriteStream(make([]byte, 1024))
	return WriteRefusalPacket(stream, value), nil
}

func writeRefusalPacketRefused(t *testing.T, row string, value *RefusalPacket) {
	t.Helper()
	err, panicked := writeRefusalPacket(value)
	if errors.Is(err, serialize.ErrValueOutOfRange) {
		return
	}
	t.Fatalf("write-refusal-packet: %s: got err=%v panic=%v, want serialize.ErrValueOutOfRange", row, err, panicked)
}

func writeRefusalPacketRefusedFixed(t *testing.T, row string, value *RefusalPacket) {
	t.Helper()
	err, panicked := writeRefusalPacket(value)
	if errors.Is(err, serialize.ErrValueOutOfRange) {
		return
	}
	if fmt.Sprint(panicked) == "serialize: write of a value outside its declared range" {
		return
	}
	t.Fatalf("write-refusal-packet: %s: got err=%v panic=%v, want a fixed-range refusal", row, err, panicked)
}

func writeRefusalPacketWrote(t *testing.T, row string, value *RefusalPacket) {
	t.Helper()
	err, panicked := writeRefusalPacket(value)
	if err != nil || panicked != nil {
		t.Fatalf("write-refusal-packet: %s: the writer refused a lawful value: err=%v panic=%v", row, err, panicked)
	}
}

func TestWriteRefusalPacket(t *testing.T) {
	// Ranged scalar, off both ends and at both. The high bound is rangeGuard.
	writeRefusalPacketRefused(t, "ranged-high", &RefusalPacket{Ranged: 11})
	writeRefusalPacketWrote(t, "ranged-max", &RefusalPacket{Ranged: 10})
	writeRefusalPacketRefused(t, "ranged-low", &RefusalPacket{Ranged: -1})
	writeRefusalPacketWrote(t, "ranged-min", &RefusalPacket{Ranged: 0})

	// Fixed raw value, off both ends and at both.
	writeRefusalPacketRefusedFixed(t, "fixed-high", &RefusalPacket{Fixedval: 9 << 16})
	writeRefusalPacketWrote(t, "fixed-max", &RefusalPacket{Fixedval: 8 << 16})
	writeRefusalPacketRefusedFixed(t, "fixed-low", &RefusalPacket{Fixedval: -9 << 16})
	writeRefusalPacketWrote(t, "fixed-min", &RefusalPacket{Fixedval: -8 << 16})

	// bits(6): 63 fits. A wider value is masked in this flat run, so it is
	// not a returned refusal here.
	writeRefusalPacketWrote(t, "bits-max", &RefusalPacket{Channel: 63})

	// Counted array count, and one ranged element, off the bound and at it.
	writeRefusalPacketRefused(t, "count-high", &RefusalPacket{ItemsCount: 5})
	writeRefusalPacketWrote(t, "count-max", &RefusalPacket{Items: [4]int32{7, 7, 7, 7}, ItemsCount: 4})
	writeRefusalPacketRefused(t, "count-low", &RefusalPacket{ItemsCount: -1})
	writeRefusalPacketWrote(t, "count-min", &RefusalPacket{ItemsCount: 0})
	writeRefusalPacketRefused(t, "element-high", &RefusalPacket{Items: [4]int32{8}, ItemsCount: 1})
	writeRefusalPacketWrote(t, "element-max", &RefusalPacket{Items: [4]int32{7}, ItemsCount: 1})
	writeRefusalPacketRefused(t, "element-low", &RefusalPacket{Items: [4]int32{-1}, ItemsCount: 1})

	// String length and byte-block length, off both ends and at both.
	writeRefusalPacketRefused(t, "text-length-high", &RefusalPacket{TextLength: 9})
	writeRefusalPacketWrote(t, "text-length-max", &RefusalPacket{Text: [8]byte{'a', 'a', 'a', 'a', 'a', 'a', 'a', 'a'}, TextLength: 8})
	writeRefusalPacketRefused(t, "text-length-low", &RefusalPacket{TextLength: -1})
	writeRefusalPacketWrote(t, "text-length-min", &RefusalPacket{TextLength: 0})
	writeRefusalPacketRefused(t, "blob-length-high", &RefusalPacket{BlobLength: 5})
	writeRefusalPacketWrote(t, "blob-length-max", &RefusalPacket{Blob: [4]byte{1, 2, 3, 4}, BlobLength: 4})
	writeRefusalPacketRefused(t, "blob-length-low", &RefusalPacket{BlobLength: -1})
	writeRefusalPacketWrote(t, "blob-length-min", &RefusalPacket{BlobLength: 0})

	// Enum ordinal outside the variant set, and the top of the set.
	writeRefusalPacketRefused(t, "enum-high", &RefusalPacket{Grade: Grade(4)})
	writeRefusalPacketWrote(t, "enum-max", &RefusalPacket{Grade: GradeGold})
	writeRefusalPacketWrote(t, "enum-none", &RefusalPacket{Grade: GradeNone})

	// Union tag outside the arm set, and a ranged arm payload.
	writeRefusalPacketRefused(t, "union-tag-high", &RefusalPacket{Choice: Choice{Type: ChoiceType(3)}})
	writeRefusalPacketWrote(t, "union-tag-max", &RefusalPacket{Choice: Choice{Type: ChoiceTypePlain, Plain: Plain{Value: 7}}})
	writeRefusalPacketWrote(t, "union-tag-none", &RefusalPacket{Choice: Choice{Type: ChoiceTypeNone}})
	writeRefusalPacketRefused(t, "union-arm-high", &RefusalPacket{Choice: Choice{Type: ChoiceTypeArm, Arm: Arm{Value: 6}}})
	writeRefusalPacketWrote(t, "union-arm-max", &RefusalPacket{Choice: Choice{Type: ChoiceTypeArm, Arm: Arm{Value: 5}}})

	// const and reserved are not caller fields. A zero value must still emit
	// the declared constant and zero reserved bits, and that packet must read.
	stream := serialize.NewWriteStream(make([]byte, 1024))
	if err := WriteRefusalPacket(stream, &RefusalPacket{}); err != nil {
		t.Fatalf("write-refusal-packet: const-reserved: %v", err)
	}
	stream.Flush()
	if err := stream.Err(); err != nil {
		t.Fatalf("write-refusal-packet: const-reserved: flush: %v", err)
	}
	data := stream.Data()
	if len(data) < 2 || data[0] != 0xAB || data[1]&0x1f != 0 {
		t.Fatalf("write-refusal-packet: const-reserved: wire %x, want const 0xAB and zero reserved bits", data)
	}
	var back RefusalPacket
	if err := ReadRefusalPacket(serialize.NewReadStream(data), &back); err != nil {
		t.Fatalf("write-refusal-packet: const-reserved: read: %v", err)
	}
}
`

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
		t.Fatalf("packet writer surface: loading the schema: %v", err)
	}
	files, err := c.Generate(u, "go", compiler.Options{})
	if err != nil {
		t.Fatalf("packet writer surface: generating the Go leg: %v", err)
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

// TestWriteRefusalPacketSurface runs the generated packet Write over rows the
// declaration does not admit, and over the lawful value at each boundary.
func TestWriteRefusalPacketSurface(t *testing.T) {
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated packet writer)")
	pkg := writeRefusalPacketSurface(t)
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "test", "-mod=mod", "-count=1", "-timeout=180s", ".")
	cmd.Dir = pkg
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("packet writer surface: the Go leg failed: %v\n%s", err, out)
	}
}

// TestWriteRefusalPacketSurfaceNegativeControl plants a flipped high bound in
// a copy of the emitter. The lawful ranged maximum must then be refused. The
// product rangeGuard comparison stays a strict greater-than.
func TestWriteRefusalPacketSurfaceNegativeControl(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	out, failed := slowtest.RunSabotaged(t, root, "./test/conformance/harness", "^TestWriteRefusalPacketSurface$",
		slowtest.Sabotage{
			File: "internal/codegen/golang/functions.go",
			From: `name+" > "+hi`,
			To:   `name+" >= "+hi`,
		})
	if !failed {
		t.Fatalf("negative control: the surface stayed green with the high bound flipped\n%s", out)
	}
	if !strings.Contains(out, "write-refusal-packet: ranged-max:") {
		t.Fatalf("negative control: the surface went red, but not on the lawful ranged maximum\n%s", out)
	}
	product, err := os.ReadFile(filepath.Join(root, "internal/codegen/golang/functions.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(product), `name+" > "+hi`) || strings.Contains(string(product), `name+" >= "+hi`) {
		t.Fatal("negative control: the product rangeGuard comparison was left flipped")
	}
}
