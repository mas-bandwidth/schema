package golang

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// nestedEndUnit nests a type three ways: a type's field of another type, a
// union's arm, and an array of a type. The variable-length array keeps Inner
// off the flat word codec, so every nested value is read through a call.
const nestedEndUnit = "package nest\n\n" +
	"type Inner\n{\n    a uint8 | max = 5\n    b [..2]uint8\n}\n\n" +
	"union Choice\n{\n    inner Inner\n}\n\n" +
	"type Outer\n{\n    inner Inner\n    items [..3]Inner\n    choice Choice\n}\n"

// exportedReadCall is a call of an exported Read<T>; a declaration's own
// name is excluded by the caller.
var exportedReadCall = regexp.MustCompile(`\bRead[A-Z][A-Za-z0-9_]*\(`)

// TestGoPacketReadEndsWithEndCheck pins SPEC §5 "Read termination" on the
// generated Go: the exported Read<T> of every type and union is the
// one-packet read and ends with the stream's end check, and a nested value is
// read through the per-type body, so the check runs once, after the top-level
// object, and never inside a nested read.
func TestGoPacketReadEndsWithEndCheck(t *testing.T) {
	t.Parallel()
	files := generateGo(t, "Nest", nestedEndUnit)
	for _, name := range []string{"Inner", "Choice", "Outer"} {
		decl := "func Read" + name + "("
		body := strings.TrimRight(funcBody(t, files, decl), "\n\t ")
		if !strings.HasSuffix(body, "return stream.SerializeEnd()") {
			t.Errorf("%s does not end with the stream's end check\n%s", decl, body)
		}
	}
	for file, src := range files {
		for line := range strings.SplitSeq(string(src), "\n") {
			if strings.HasPrefix(line, "func Read") || strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			if exportedReadCall.MatchString(line) {
				t.Errorf("%s: a nested value is read through an exported read, which ends the packet: %s", file, strings.TrimSpace(line))
			}
		}
	}
	t.Run("probe", func(t *testing.T) {
		t.Parallel()
		runPacketEndProbe(t)
	})
}

// runPacketEndProbe compiles the generated Go for test/packet-end/End.schema
// and reads the three shared packets with the exported read, then compiles
// the nested unit and reads a packet whose inner values are read whole.
func runPacketEndProbe(t *testing.T) {
	t.Helper()
	slowtest.Gate(t, "the Go toolchain (it compiles and runs the generated reader)")
	const root = "../../../"
	src, err := os.ReadFile(filepath.Join(root, "test", "packet-end", "End.schema"))
	if err != nil {
		t.Fatal(err)
	}
	packet := func(name string) string {
		data, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "packet-end", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("%#v", data)
	}
	endProbe := `package packetend

import (
	"testing"

	"github.com/mas-bandwidth/serialize.go/v2"
)

func TestPacketEnd(t *testing.T) {
	var v End
	if err := ReadEnd(serialize.NewReadStream(` + packet("exact") + `), &v); err != nil || v.Value != 3 {
		t.Errorf("exact: value = %d, err = %v, want 3 accepted", v.Value, err)
	}
	if err := ReadEnd(serialize.NewReadStream(` + packet("trailing") + `), &v); err == nil {
		t.Errorf("trailing: a whole byte after the object was accepted")
	}
	if err := ReadEnd(serialize.NewReadStream(` + packet("padding") + `), &v); err == nil {
		t.Errorf("padding: non-zero unused bits in the final byte were accepted")
	}
}
`
	runGoProbe(t, root, generateGo(t, "End", string(src)), endProbe)

	const nestProbe = `package nest

import (
	"testing"

	"github.com/mas-bandwidth/serialize.go/v2"
)

func TestNestedPacketEnd(t *testing.T) {
	inner := Inner{A: 3, BCount: 2, B: [2]uint8{7, 9}}
	want := Outer{Inner: inner, ItemsCount: 2, Choice: Choice{Type: ChoiceTypeInner, Inner: inner}}
	want.Items[0] = inner
	want.Items[1] = Inner{A: 5, BCount: 1, B: [2]uint8{1}}
	ws := serialize.NewWriteStream(make([]byte, OuterMaxBytes))
	if err := WriteOuter(ws, &want); err != nil {
		t.Fatalf("write: %v", err)
	}
	ws.Flush()
	wire := ws.Data()
	var got Outer
	if err := ReadOuter(serialize.NewReadStream(wire), &got); err != nil {
		t.Fatalf("a nested packet was refused: %v (wire % x)", err, wire)
	}
	if got != want {
		t.Errorf("read %+v, want %+v", got, want)
	}
	if err := ReadOuter(serialize.NewReadStream(append(wire[:len(wire):len(wire)], 0)), &got); err == nil {
		t.Errorf("a whole byte after the nested packet was accepted")
	}
}
`
	runGoProbe(t, root, generateGo(t, "Nest", nestedEndUnit), nestProbe)
}

// runGoProbe writes the generated files beside probe against the serialize.go
// runtime and runs its tests.
func runGoProbe(t *testing.T, root string, files map[string][]byte, probe string) {
	t.Helper()
	runtime := slowtest.SerializeGo(t, root)
	files["go.mod"] = []byte(fmt.Sprintf("module probe\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\nreplace github.com/mas-bandwidth/serialize.go/v2 => %q\n", runtime))
	files["probe_test.go"] = []byte(probe)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "test", "-count=1", ".")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the generated reader failed its probe: %v\n%s", err, out)
	}
}
