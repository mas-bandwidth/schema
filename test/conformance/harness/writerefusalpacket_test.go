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

// The shared write-refusal-packet corpus the assert legs answer too: one
// schema, one case list, relative to the repository root.
const (
	writeRefusalPacketSchema = "test/packet-write-refusal/RefusalPacket.schema"
	writeRefusalPacketCases  = "test/packet-write-refusal/cases.txt"
)

// TestWriteRefusalPacket holds docs/SPEC.md §5.1's packet writer on the Go
// leg: every caller-holdable value the declaration does not admit is refused
// with serialize.ErrValueOutOfRange, never a panic, and the lawful value at
// the same boundary is written. Two negative controls: one changes the
// emitter's upper bound comparison and requires the boundary case to go red;
// the other drops the generated 128-bit and fixed-point range refusals, which
// leaves the runtime's panic, and requires the int128 and fixed cases to go
// red.
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
	if !strings.Contains(out, "FAILED: scalar_at_max") {
		t.Fatalf("negative control: the packet surface went red, but not at the boundary case\n%s", out)
	}

	out, failed = slowtest.RunSabotaged(t, ".", "./test/conformance/harness",
		"^TestWriteRefusalPacketSurface$",
		slowtest.Sabotage{
			File: "internal/codegen/golang/functions.go",
			From: `g.pf("%sif %s {\n%s\treturn serialize.ErrValueOutOfRange\n%s}\n", ind, strings.Join(parts, " || "), ind, ind)`,
			To:   `_ = parts`,
			All:  true,
		})
	if !failed {
		t.Fatalf("negative control: the 128-bit and fixed-point write refusals were dropped and the packet surface stayed green\n%s", out)
	}
	for _, want := range []string{"FAILED: int128_high", "FAILED: fixed_high", "FAILED: ufixed_high"} {
		if !strings.Contains(out, want) {
			t.Fatalf("negative control: the packet surface went red, but not at %s\n%s", strings.TrimPrefix(want, "FAILED: "), out)
		}
	}
}

// TestWriteRefusalPacketSurface is the surface alone, so the negative control
// can run it under an overlay without re-entering the control.
func TestWriteRefusalPacketSurface(t *testing.T) {
	runWriteRefusalPacket(t)
}

// writeRefusalPacketProbe runs every case of the shared cases.txt (its path
// in PACKET_REFUSAL_CASES_FILE) through the generated packet writer. A case the
// file names that the probe has no setter for fails, and so does a setter the
// file does not name. A panic is never a refusal: it is caught per case and
// fails that case by name.
const writeRefusalPacketProbe = `package packetwriterefusal

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/serialize.go/v2"
)

var setters = map[string]func(*PacketWriteRefusal){
	"scalar_high":      func(v *PacketWriteRefusal) { v.Scalar = 11 },
	"scalar_low":       func(v *PacketWriteRefusal) { v.Scalar = -1 },
	"scalar_at_max":    func(v *PacketWriteRefusal) { v.Scalar = 10 },
	"scalar_at_min":    func(v *PacketWriteRefusal) { v.Scalar = 0 },
	"bits_wide":        func(v *PacketWriteRefusal) { v.Narrow = 200 },
	"bits_at_max":      func(v *PacketWriteRefusal) { v.Narrow = 127 },
	"enum_high":        func(v *PacketWriteRefusal) { v.Grade = Grade(4) },
	"enum_at_max":      func(v *PacketWriteRefusal) { v.Grade = GradeGold },
	"union_tag_high":   func(v *PacketWriteRefusal) { v.Shot.Type = ShotType(3) },
	"union_tag_at_max": func(v *PacketWriteRefusal) { v.Shot.Type = ShotTypeMax },
	"count_high":       func(v *PacketWriteRefusal) { v.WindowCount = 9 },
	"count_low":        func(v *PacketWriteRefusal) { v.WindowCount = 1 },
	"count_at_max":     func(v *PacketWriteRefusal) { v.WindowCount = 8 },
	"count_at_min":     func(v *PacketWriteRefusal) { v.WindowCount = 2 },
	"text_long":        func(v *PacketWriteRefusal) { v.TextLength = 9 },
	"text_illformed":   func(v *PacketWriteRefusal) { v.Text[0] = 0xff; v.TextLength = 1 },
	"text_zero":        func(v *PacketWriteRefusal) { v.Text[0] = 0; v.TextLength = 1 },
	"text_at_max": func(v *PacketWriteRefusal) {
		for i := range v.Text {
			v.Text[i] = 'a'
		}
		v.TextLength = 8
	},
	"const_wrong":    func(v *PacketWriteRefusal) { v.FixedConstant = 6 },
	"const_at":       func(v *PacketWriteRefusal) { v.FixedConstant = 5 },
	"reserved_wrong": func(v *PacketWriteRefusal) { v.ReservedZero = 1 },
	"reserved_at":    func(v *PacketWriteRefusal) { v.ReservedZero = 0 },
	"int128_high":    func(v *PacketWriteRefusal) { v.Wide = serialize.Int128{Lo: 0x6bc75e2d63100001, Hi: 5} },
	"int128_low":     func(v *PacketWriteRefusal) { v.Wide = serialize.Int128From64(-11) },
	"int128_at_max":  func(v *PacketWriteRefusal) { v.Wide = serialize.Int128{Lo: 0x6bc75e2d63100000, Hi: 5} },
	"int128_at_min":  func(v *PacketWriteRefusal) { v.Wide = serialize.Int128From64(-10) },
	"fixed_high":     func(v *PacketWriteRefusal) { v.Fx = 655361 },
	"fixed_low":      func(v *PacketWriteRefusal) { v.Fx = -655361 },
	"fixed_at_max":   func(v *PacketWriteRefusal) { v.Fx = 655360 },
	"fixed_at_min":   func(v *PacketWriteRefusal) { v.Fx = -655360 },
	"ufixed_high":    func(v *PacketWriteRefusal) { v.Ufx = 0xee6b280000000001 },
	"ufixed_low":     func(v *PacketWriteRefusal) { v.Ufx = 0xffffffff },
	"ufixed_at_max":  func(v *PacketWriteRefusal) { v.Ufx = 0xee6b280000000000 },
	"ufixed_at_min":  func(v *PacketWriteRefusal) { v.Ufx = 0x100000000 },
}

// write runs the packet writer, turning a panic into an error that is not
// ErrValueOutOfRange, so it can never pass as the refusal.
func write(value *PacketWriteRefusal) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("panicked: " + strings.TrimSpace(fmtAny(r)))
		}
	}()
	stream := serialize.NewWriteStream(make([]byte, 256))
	return WritePacketWriteRefusal(stream, value)
}

func fmtAny(r any) string {
	if s, ok := r.(string); ok {
		return s
	}
	if e, ok := r.(error); ok {
		return e.Error()
	}
	return "a non-string panic"
}

func TestWriteRefusalPacketRows(t *testing.T) {
	data, err := os.ReadFile(os.Getenv("PACKET_REFUSAL_CASES_FILE"))
	if err != nil {
		t.Fatal(err)
	}
	named := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, verdict, ok := strings.Cut(line, "|")
		if !ok {
			t.Fatalf("cases.txt: %q is not <name> | <verdict>", line)
		}
		name, verdict = strings.TrimSpace(name), strings.TrimSpace(verdict)
		named[name] = true
		set, ok := setters[name]
		if !ok {
			t.Errorf("FAILED: %s: neither run nor named not representable in Go", name)
			continue
		}
		value := NewPacketWriteRefusal()
		set(&value)
		err := write(&value)
		switch verdict {
		case "refused":
			if !errors.Is(err, serialize.ErrValueOutOfRange) {
				t.Errorf("FAILED: %s: wrote or erred wrongly, got %v, want serialize.ErrValueOutOfRange", name, err)
			}
		case "wrote":
			if err != nil {
				t.Errorf("FAILED: %s: the lawful value at the boundary was refused: %v", name, err)
			}
		default:
			t.Errorf("FAILED: %s: unknown verdict %q", name, verdict)
		}
	}
	for name := range setters {
		if !named[name] {
			t.Errorf("FAILED: %s: driver case missing from cases.txt", name)
		}
	}
}
`

// runWriteRefusalPacket generates the shared unit for the Go leg, builds it
// with the serialize.go runtime, and runs the probe over the shared cases.
// Behind slowtest.Gate — it shells out to the Go toolchain.
func runWriteRefusalPacket(t *testing.T) {
	t.Helper()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated packet writer)")
	t.Chdir("../../..")

	c := compiler.New()
	u, err := c.Load([]string{writeRefusalPacketSchema})
	if err != nil {
		t.Fatal(err)
	}
	files, err := c.Generate(u, "go", compiler.Options{"runtime": "standalone"})
	if err != nil {
		t.Fatal(err)
	}
	cases, err := filepath.Abs(writeRefusalPacketCases)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
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
	cmd.Env = append(os.Environ(), "PACKET_REFUSAL_CASES_FILE="+cases)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated packet writer failed its probe: %v\n%s", err, out)
	}
}
