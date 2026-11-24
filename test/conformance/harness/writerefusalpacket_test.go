package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	wrpacket "github.com/mas-bandwidth/schema/v2/test/conformance/write-refusal-packet"
)

// TestWriteRefusalPacket holds docs/SPEC.md §5.1's packet writer on the Go
// leg over the one shared list, test/conformance/write-refusal-packet/rows.txt
// and its RefusalPacket.schema: every caller-holdable value the declaration
// does not admit is refused with serialize.ErrValueOutOfRange, and the lawful
// value at the same boundary is written. The negative control changes the
// emitter's upper bound comparison and requires the boundary case to go red.
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
}

// TestWriteRefusalPacketSurface is the surface alone, so the negative control
// can run it under an overlay without re-entering the control.
func TestWriteRefusalPacketSurface(t *testing.T) {
	runWriteRefusalPacket(t)
}

// writeRefusalPacketProbe is the Go leg's probe, built as its own main package
// beside the generated unit: for every row of rows.txt it makes a fresh lawful
// PacketWriteRefusal, applies the row's ONE op, runs the generated packet
// writer and prints "<name> refused" for serialize.ErrValueOutOfRange or
// "<name> wrote" for a completed write. Any other error, an unknown op, or an
// argument the field cannot hold ends the run loudly: none is a refusal.
const writeRefusalPacketProbe = `package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	packetrefusal "packetrefusalprobe"

	"github.com/mas-bandwidth/serialize.go/v2"
)

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "write-refusal-packet probe: "+format+"\n", args...)
	os.Exit(2)
}

// set stores arg into an integer field of any width, and refuses a value the
// field cannot hold rather than truncating it.
func set(field reflect.Value, arg string) {
	switch field.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(arg, 10, 64)
		if err != nil || field.OverflowInt(n) {
			die("%q does not fit a %s", arg, field.Type())
		}
		field.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(arg, 10, 64)
		if err != nil || field.OverflowUint(n) {
			die("%q does not fit a %s", arg, field.Type())
		}
		field.SetUint(n)
	default:
		die("a field of kind %s is not an integer", field.Kind())
	}
}

func apply(v *packetrefusal.PacketWriteRefusal, op, arg string) {
	r := reflect.ValueOf(v).Elem()
	switch op {
	case "scalar":
		set(r.FieldByName("Scalar"), arg)
	case "narrow":
		set(r.FieldByName("Narrow"), arg)
	case "grade":
		set(r.FieldByName("Grade"), arg)
	case "shot_tag":
		set(r.FieldByName("Shot").FieldByName("Type"), arg)
	case "window_count":
		set(r.FieldByName("WindowCount"), arg)
	case "text_length":
		set(r.FieldByName("TextLength"), arg)
	case "text_bytes":
		b, err := hex.DecodeString(arg)
		if err != nil || len(b) > len(v.Text) {
			die("text_bytes %q", arg)
		}
		clear(v.Text[:])
		copy(v.Text[:], b)
		set(r.FieldByName("TextLength"), strconv.Itoa(len(b)))
	case "text_fill":
		n, err := strconv.Atoi(arg)
		if err != nil || n < 0 || n > len(v.Text) {
			die("text_fill %q", arg)
		}
		for i := range n {
			v.Text[i] = 'a'
		}
		set(r.FieldByName("TextLength"), arg)
	case "fixed_constant":
		set(r.FieldByName("FixedConstant"), arg)
	case "reserved_zero":
		set(r.FieldByName("ReservedZero"), arg)
	case "perks":
		set(r.FieldByName("Perks"), arg)
	default:
		die("unknown op %q", op)
	}
}

func main() {
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		die("%v", err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 4 {
			die("%q is not <name> <verdict> <op> <arg>", line)
		}
		value := packetrefusal.NewPacketWriteRefusal()
		apply(&value, f[2], f[3])
		stream := serialize.NewWriteStream(make([]byte, 256))
		switch err := packetrefusal.WritePacketWriteRefusal(stream, &value); {
		case err == nil:
			fmt.Println(f[0], "wrote")
		case errors.Is(err, serialize.ErrValueOutOfRange):
			fmt.Println(f[0], "refused")
		default:
			die("%s: the writer erred with something other than a refusal: %v", f[0], err)
		}
	}
}
`

// runWriteRefusalPacket generates the shared RefusalPacket.schema for the Go
// leg, builds it with the serialize.go runtime and runs the probe over
// rows.txt; wrpacket.Check holds the answers. Behind slowtest.Gate — it shells
// out to the Go toolchain.
func runWriteRefusalPacket(t *testing.T) {
	t.Helper()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated packet writer)")
	t.Chdir("../../..")

	const root = "."
	rows := wrpacket.Rows(t, root)
	dir := wrpacket.Generate(t, root, "go", compiler.Options{"runtime": "standalone"})
	runtime := slowtest.SerializeGo(t, root)
	gomod := fmt.Sprintf("module packetrefusalprobe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "probe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "probe", "main.go"), []byte(writeRefusalPacketProbe), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", "./probe", wrpacket.RowsPath(root))
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		stderr := ""
		if errors.As(err, &exit) {
			stderr = string(exit.Stderr)
		}
		t.Fatalf("the probe did not complete (an error is not a refusal): %v\n%s%s", err, out, stderr)
	}
	wrpacket.Check(t, string(out), rows, nil)
}
