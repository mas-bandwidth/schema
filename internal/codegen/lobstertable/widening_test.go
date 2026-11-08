package lobstertable_test

// WIDENING ON READ, AND THE REFUSAL REASONS, for Lobster (docs/SPEC-TABLES.md
// §3, §3.3, §4, §6.5), held to the reference engine's read (internal/tablewire
// Decode), which shares no code with the Lobster runtime:
//
//	TestLobsterWideningLadder  every wire scalar kind under every declared
//	    scalar kind (the 12 x 10 grid), each payload with its top bit set so a
//	    widening that extends by the wrong signedness is seen: the one read's
//	    verdict and complete value are the reference's, and an accepted value
//	    saves to the reference's bytes
//	TestLobsterWideningFloat32UnderFloat64  a float32 payload under a float64
//	    field: a NaN (signaling or quiet, any payload) or an infinity refuses
//	    the read; a finite one (a subnormal and a negative zero included)
//	    widens exactly, as the reference reads it
//
// The refusal reasons are named per language (§6.5): the cross-language
// contract is the verdict and the value, which TestLobsterLoadRefusalPins
// (pins_test.go) holds over the shared reason-pin table and
// TestLobsterStrictRead (strict_and_refusals_test.go) over every refusal
// class. TestLobsterWideningNegativeControl breaks the float rung and the
// int128 rung of the runtime's widens() and requires each to turn exactly
// its own cells red.

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// lobsterLadderKinds are the scalar declarations and their wire kinds.
var lobsterLadderKinds = []struct {
	decl string
	kind byte
}{
	{"int8", 2}, {"int16", 3}, {"int32", 4}, {"int64", 5},
	{"uint8", 6}, {"uint16", 7}, {"uint32", 8}, {"uint64", 9},
	{"float32", 10}, {"float64", 11},
	{"int128 | min = -1267650600228229401496703205376, max = 1267650600228229401496703205376", 18}, {"uint128", 19},
}

// lobsterLadderPayload is a payload of a wire kind whose top bit is set.
func lobsterLadderPayload(kind byte) []byte {
	switch kind {
	case 10:
		return binary.LittleEndian.AppendUint32(nil, math.Float32bits(-1.5))
	case 11:
		return binary.LittleEndian.AppendUint64(nil, math.Float64bits(-2.25))
	}
	width := map[byte]int{2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 2, 8: 4, 9: 8}[kind]
	p := bytes.Repeat([]byte{0xFF}, width)
	p[0] = 0xFD
	return p
}

// lobsterOneFieldWire is a form-1 file of one root field: the form byte, the
// field's reference, kind and payload, the terminator, and the id table.
func lobsterOneFieldWire(name string, kind byte, payload []byte) []byte {
	wire := append([]byte{1, 1, kind}, payload...)
	wire = append(wire, 0)
	wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId(name))
	return binary.LittleEndian.AppendUint64(wire, 1)
}

const wideningRun = `
def widerun(file:string):
    let data = read_file(file)
    assert data
    let rec = schema_rt.load("Root", data, schema_rt.LoadReport {})
    if not rec:
        print("refused|nil|")
        return
    labels = []
    push(labels, rec)
    let out, why = schema_rt.save_reasoned(rec)
    assert why == ""
    print("read|" + drec(rec) + "|" + hexs(out))
`

// wideningCell is one wire read under one declaration.
type wideningCell struct {
	name string
	wire []byte
}

// lobsterWidening runs the cells under one declaration, with patch applied to
// the emitted load runtime when it is set, and answers the cells whose
// verdict, value or saved bytes differ from the reference's, and the number
// of cells the reference widened.
func lobsterWidening(t *testing.T, decl string, cells []wideningCell, patch func(string) string) (map[string]string, int) {
	t.Helper()
	g, err := generateLoadUnit(t, []string{writeSchema(t, "Probe", fmt.Sprintf("package probe\ntable Root { value %s }\n", decl))})
	if err != nil {
		t.Fatalf("the surface refuses %s: %v", decl, err)
	}
	if patch != nil {
		path := filepath.Join(g.dir, "load.lobster")
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(patch(string(src))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\nimport .schema_save\n")
	b.WriteString(keyedRegistrations(g.u))
	b.WriteString(dumpLobster)
	b.WriteString(wideningRun)
	inputs := map[string][]byte{}
	for i, c := range cells {
		name := fmt.Sprintf("w%d", i)
		inputs[name] = c.wire
		fmt.Fprintf(&b, "widerun(%s)\n", lobster.StringLit([]byte(name)))
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	var out []string
	for line := range strings.SplitSeq(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)), "\n"), "\n") {
		if !strings.Contains(line, ": warning: ") {
			out = append(out, line)
		}
	}
	if len(out) != len(cells) {
		t.Fatalf("%s: %d lines for %d cells: %q", decl, len(out), len(cells), out)
	}
	m := tabletext.NewModel(g.u)
	faults := map[string]string{}
	widened := 0
	for i, c := range cells {
		v := m.New(m.Lookup("Root"))
		var r tabletext.Report
		ok, err := tablewire.Decode(m, v, c.wire, &r)
		want := "refused|nil|"
		if ok && err == nil && !r.Refused {
			saved, err := tablewire.Encode(m, v)
			if err != nil {
				t.Fatalf("%s under %s: the reference cannot save what it read: %v", c.name, decl, err)
			}
			_, dump := goLoad(g.u, "Root", c.wire)
			want = "read|" + dump + "|" + hex.EncodeToString(saved)
			widened += r.Widened
		}
		if out[i] != want {
			faults[c.name+" under "+decl] = fmt.Sprintf("lobster %s, the reference %s", out[i], want)
		}
	}
	return faults, widened
}

// wideningLadderFaults is the grid's red list and widened count.
func wideningLadderFaults(t *testing.T, patch func(string) string) (map[string]string, int) {
	t.Helper()
	faults := map[string]string{}
	widened := 0
	for _, decl := range lobsterLadderKinds {
		var cells []wideningCell
		for _, w := range lobsterLadderKinds[:10] {
			cells = append(cells, wideningCell{fmt.Sprintf("kind %d", w.kind), lobsterOneFieldWire("value", w.kind, lobsterLadderPayload(w.kind))})
		}
		f, n := lobsterWidening(t, decl.decl, cells, patch)
		for k, v := range f {
			faults[k] = v
		}
		widened += n
	}
	return faults, widened
}

func TestLobsterWideningLadder(t *testing.T) {
	t.Parallel()
	faults, widened := wideningLadderFaults(t, nil)
	for k, v := range faults {
		t.Errorf("%s: %s", k, v)
	}
	if widened == 0 {
		t.Fatal("the grid exercised no widening; it must exercise one")
	}
	t.Logf("12 declarations x 10 wire kinds: every verdict, value and saved byte is the reference's; %d widened", widened)
}

// float32UnderFloat64Faults is the float rung's red list: NaNs and
// infinities the reference refuses, finite values it widens.
func float32UnderFloat64Faults(t *testing.T, patch func(string) string) (map[string]string, int) {
	t.Helper()
	bits := []uint32{0x7f800001, 0xff812345, 0x7fc12345, 0x7f800000, 0xff800000, 0x3fc00000, 0x80000000, 0x007fffff, 0x80000001}
	var cells []wideningCell
	for _, b := range bits {
		cells = append(cells, wideningCell{fmt.Sprintf("float32 %#08x", b), lobsterOneFieldWire("value", 10, binary.LittleEndian.AppendUint32(nil, b))})
	}
	return lobsterWidening(t, "float64", cells, patch)
}

func TestLobsterWideningFloat32UnderFloat64(t *testing.T) {
	t.Parallel()
	faults, widened := float32UnderFloat64Faults(t, nil)
	for k, v := range faults {
		t.Errorf("%s: %s", k, v)
	}
	if widened != 4 {
		t.Fatalf("the reference widened %d of the four finite payloads", widened)
	}
}

// TestLobsterWideningNegativeControl breaks one rung of widens() at a time
// and requires exactly that rung's cells red.
func TestLobsterWideningNegativeControl(t *testing.T) {
	t.Parallel()
	plant := func(from, to string) func(string) string {
		return func(src string) string {
			if n := strings.Count(src, from); n != 1 {
				t.Fatalf("the widening anchor %q is in the emitted runtime %d times, want 1", from, n)
			}
			return strings.Replace(src, from, to, 1)
		}
	}
	float := plant("    if declared == 11:\n        return kind == 10\n", "    if declared == 11:\n        return false\n")
	faults, _ := float32UnderFloat64Faults(t, float)
	if len(faults) != 4 {
		t.Errorf("the float rung broken: %d float cells red, want the 4 finite ones: %v", len(faults), faults)
	}
	ladder, _ := wideningLadderFaults(t, float)
	for k := range ladder {
		if k != "kind 10 under float64" {
			t.Errorf("the float rung broken turned %s red", k)
		}
	}
	if len(ladder) != 1 {
		t.Errorf("the float rung broken: %d grid cells red, want kind 10 under float64 alone", len(ladder))
	}
	wide := plant("    if declared == 18:\n        return kind >= 2 and kind <= 5\n", "    if declared == 18:\n        return kind >= 2 and kind <= 4\n")
	ladder, _ = wideningLadderFaults(t, wide)
	for k := range ladder {
		if !strings.HasSuffix(k, " under "+lobsterLadderKinds[10].decl) {
			t.Errorf("the int128 rung broken turned %s red", k)
		}
	}
	if len(ladder) == 0 {
		t.Error("the int128 rung broken left every grid cell green")
	}
}
