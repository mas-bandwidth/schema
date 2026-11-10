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

// TestWriteRefusalPacket holds docs/SPEC.md §5.1's packet writer on the Go
// leg: every caller-holdable value the declaration does not admit is refused
// with serialize.ErrValueOutOfRange, and the lawful value at the same boundary
// is written. The negative control changes the emitter's upper bound
// comparison and requires the boundary case to go red.
func TestWriteRefusalPacket(t *testing.T) {
	runWriteRefusalPacket(t)

	out, failed := slowtest.RunSabotaged(t, ".", "./test/conformance/harness",
		"^TestWriteRefusalPacketSurface$",
		slowtest.Sabotage{
			File: "internal/codegen/golang/functions.go",
			From: `parts = append(parts, name+" > "+hi)`,
			To:   `parts = append(parts, name+" >= "+hi)`,
		})
	if !failed {
		t.Fatalf("negative control: the upper-bound comparison moved and the packet surface stayed green\n%s", out)
	}
	if !strings.Contains(out, "scalar_at_max") {
		t.Fatalf("negative control: the packet surface went red, but not at the boundary case\n%s", out)
	}
}

// TestWriteRefusalPacketSurface is the surface alone, so the negative control
// can run it under an overlay without re-entering the control.
func TestWriteRefusalPacketSurface(t *testing.T) {
	runWriteRefusalPacket(t)
}

// writeRefusalPacketSchema carries one field per row §5.1 names: a ranged
// scalar, an enum, a counted array, a string, a union, and a constant and a
// reserved-range field whose only lawful value is the declared one. The real
// const() and reserved() wire items ride beside them so the writer's own
// constant and zero bits are exercised too.
const writeRefusalPacketSchema = `package packetrefusal

enum Grade
{
    Bronze
    Silver
    Gold
}

type Laser
{
    power uint32
}

type Missile
{
    fuel uint32
}

union Shot
{
    laser Laser
    missile Missile
}

type PacketWriteRefusal
{
    scalar         int32   | min = 0, max = 10
    grade          Grade
    window         [2..8]uint32
    text           string(8)
    shot           Shot
    fixed_constant int32 = 5 | min = 5, max = 5
    reserved_zero  int32 = 0 | min = 0, max = 0
    const(0xAB, 8)
    reserved(5)
}
`

// writeRefusalPacketProbe runs every row's refused value and its lawful twin
// through the generated packet writer.
const writeRefusalPacketProbe = `package packetrefusal

import (
	"errors"
	"testing"

	"github.com/mas-bandwidth/serialize.go/v2"
)

func TestWriteRefusalPacketRows(t *testing.T) {
	cases := []struct {
		name string
		want string
		set  func(*PacketWriteRefusal)
	}{
		{"scalar_high", "refused", func(v *PacketWriteRefusal) { v.Scalar = 11 }},
		{"scalar_low", "refused", func(v *PacketWriteRefusal) { v.Scalar = -1 }},
		{"scalar_at_max", "wrote", func(v *PacketWriteRefusal) { v.Scalar = 10 }},
		{"scalar_at_min", "wrote", func(v *PacketWriteRefusal) { v.Scalar = 0 }},
		{"enum_high", "refused", func(v *PacketWriteRefusal) { v.Grade = Grade(4) }},
		{"enum_at_max", "wrote", func(v *PacketWriteRefusal) { v.Grade = GradeGold }},
		{"union_tag_high", "refused", func(v *PacketWriteRefusal) { v.Shot.Type = ShotType(3) }},
		{"union_tag_at_max", "wrote", func(v *PacketWriteRefusal) { v.Shot.Type = ShotTypeMax }},
		{"count_high", "refused", func(v *PacketWriteRefusal) { v.WindowCount = 9 }},
		{"count_low", "refused", func(v *PacketWriteRefusal) { v.WindowCount = 1 }},
		{"count_at_max", "wrote", func(v *PacketWriteRefusal) { v.WindowCount = 8 }},
		{"count_at_min", "wrote", func(v *PacketWriteRefusal) { v.WindowCount = 2 }},
		{"text_long", "refused", func(v *PacketWriteRefusal) { v.TextLength = 9 }},
		{"text_illformed", "refused", func(v *PacketWriteRefusal) { v.Text[0] = 0xff; v.TextLength = 1 }},
		{"text_zero", "refused", func(v *PacketWriteRefusal) { v.Text[0] = 0; v.TextLength = 1 }},
		{"text_at_max", "wrote", func(v *PacketWriteRefusal) {
			for i := range v.Text {
				v.Text[i] = 'a'
			}
			v.TextLength = 8
		}},
		{"const_wrong", "refused", func(v *PacketWriteRefusal) { v.FixedConstant = 6 }},
		{"const_at", "wrote", func(v *PacketWriteRefusal) { v.FixedConstant = 5 }},
		{"reserved_wrong", "refused", func(v *PacketWriteRefusal) { v.ReservedZero = 1 }},
		{"reserved_at", "wrote", func(v *PacketWriteRefusal) { v.ReservedZero = 0 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			value := NewPacketWriteRefusal()
			c.set(&value)
			stream := serialize.NewWriteStream(make([]byte, 256))
			err := WritePacketWriteRefusal(stream, &value)
			switch c.want {
			case "refused":
				if !errors.Is(err, serialize.ErrValueOutOfRange) {
					t.Fatalf("%s: got %v, want serialize.ErrValueOutOfRange", c.name, err)
				}
			case "wrote":
				if err != nil {
					t.Fatalf("%s: the lawful value at the boundary was refused: %v", c.name, err)
				}
			default:
				t.Fatalf("%s: unknown expectation %q", c.name, c.want)
			}
		})
	}
}
`

// runWriteRefusalPacket generates the unit for the Go leg, builds it with the
// serialize.go runtime, and runs the probe. Behind slowtest.Gate — it shells
// out to the Go toolchain.
func runWriteRefusalPacket(t *testing.T) {
	t.Helper()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated packet writer)")
	t.Chdir("../../..")

	c := compiler.New()
	dir := t.TempDir()
	schema := filepath.Join(dir, "PacketWriteRefusal.schema")
	if err := os.WriteFile(schema, []byte(writeRefusalPacketSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	u, err := c.Load([]string{schema})
	if err != nil {
		t.Fatal(err)
	}
	files, err := c.Generate(u, "go", compiler.Options{"runtime": "standalone"})
	if err != nil {
		t.Fatal(err)
	}
	runtime := slowtest.SerializeGo(t, ".")
	files["go.mod"] = []byte(fmt.Sprintf("module packetrefusalprobe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["writerefusal_probe_test.go"] = []byte(writeRefusalPacketProbe)
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated packet writer failed its probe: %v\n%s", err, out)
	}
}
