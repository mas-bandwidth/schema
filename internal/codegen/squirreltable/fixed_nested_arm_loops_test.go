package squirreltable_test

// A LIST INSIDE A LIST ELEMENT, THROUGH THE FIXED FORM (docs/SPEC-TABLES.md
// §3.4): an array of a union whose arms are themselves an array and a text is
// written and read INLINE inside the element loop, so the arm's loop and the
// element's loop are live in one body. Every sample puts every bounded array
// at its declared maximum, and rotates the arms so each arm kind sits at every
// element position.
//
// The Go engine (internal/tablewire) writes each sample's form-3 file, the
// generated <Root>FixedLoad reads it, and the generated <Root>FixedSave writes
// it back: the Squirrel bytes must equal the engine's, and the engine must
// read the Squirrel bytes back to the sample's own value.

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// nestedArmProbe is the schema: a counted and a fixed array of a union whose
// arms are a float array, a text and an int array, and a counted array of a
// record holding an array.
const nestedArmProbe = `package probe

union Arm
{
    vals  [..2]float32
    label string(8)
    nums  [..3]int32
}

fixed table Row
{
    xs [..2]int16
}

fixed table Box
{
    arms [..4]Arm
    pair [2]Arm
    rows [..3]Row
}
`

// nestedArmSamples answers one sample per rotation of the arms: element e of
// arms (e = 0..3) and of pair (e = 4..5) holds arm (e + k) % 3, every arm
// array at its bound, and every value distinct by element and position, so
// each element's bytes are its own.
func nestedArmSamples() map[string]string {
	out := map[string]string{}
	for k := range 3 {
		arm := func(e int) string {
			switch (e + k) % 3 {
			case 0:
				return fmt.Sprintf(`{"vals":[%d.5,%d.25]}`, 10*e+1, 10*e+2)
			case 1:
				label := make([]byte, 8)
				for j := range label {
					label[j] = byte('a' + e + 3*j)
				}
				return fmt.Sprintf(`{"label":%q}`, label)
			default:
				return fmt.Sprintf(`{"nums":[%d,%d,%d]}`, 100*e+1, 100*e+2, 100*e+3)
			}
		}
		var b strings.Builder
		b.WriteString(`{"arms":[`)
		for e := range 4 {
			if e > 0 {
				b.WriteString(",")
			}
			b.WriteString(arm(e))
		}
		b.WriteString(`],"pair":[`)
		for e := 4; e < 6; e++ {
			if e > 4 {
				b.WriteString(",")
			}
			b.WriteString(arm(e))
		}
		b.WriteString(`],"rows":[`)
		for e := range 3 {
			if e > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"xs":[%d,%d]}`, 10*e+k+1, 10*e+k+2)
		}
		b.WriteString(`]}`)
		out[fmt.Sprintf("rotate%d", k)] = b.String()
	}
	return out
}

func TestSquirrelFixedFormNestedArmLoops(t *testing.T) {
	slowtest.Gate(t, "the pinned Squirrel interpreter")
	t.Parallel()
	u := unitFromSchema(t, nestedArmProbe)
	m := tabletext.NewModel(u)
	st := m.Lookup("Box")
	if st == nil {
		t.Fatal("no root Box")
	}
	samples := nestedArmSamples()
	names := slices.Sorted(maps.Keys(samples))
	wire, text := map[string][]byte{}, map[string][]byte{}
	for _, name := range names {
		inst := m.New(st)
		var rep tabletext.Report
		if !m.Read(inst, []byte(samples[name]), &rep) || rep != (tabletext.Report{}) {
			t.Fatalf("%s: the engine did not read the sample cleanly: %+v", name, rep)
		}
		b, err := tablewire.Encode(m, inst)
		if err != nil || len(b) == 0 || b[0] != 3 {
			t.Fatalf("%s: the engine did not write a form-3 file: %v", name, err)
		}
		tx, err := m.Write(inst)
		if err != nil {
			t.Fatal(err)
		}
		wire[name], text[name] = b, tx
	}
	dir := t.TempDir()
	writeGenerated(t, dir, u)
	var p strings.Builder
	p.WriteString("local inputs = dofile(\"inputs.nut\", true);\n")
	p.WriteString("local S = dofile(\"schema_module.nut\", true); local T = S.require(\"ProbeTables\");\n")
	for _, name := range names {
		fmt.Fprintf(&p, "{ local b = inputs[%q]; local back = [T.Box()];\n", name)
		fmt.Fprintf(&p, "  local got = T.BoxFixedLoad(b, back, null);\n")
		fmt.Fprintf(&p, "  if (got != 1) { print(\"FAIL %s: load answered \" + got + \":\" + T.FixedReason + \"\\n\"); }\n", name)
		fmt.Fprintf(&p, "  else { local out = blob(b.len()); local w = T.BoxFixedSave(out, back);\n")
		fmt.Fprintf(&p, "    if (w != b.len()) { print(\"FAIL %s: save answered \" + w + \":\" + T.FixedReason + \"\\n\"); }\n", name)
		fmt.Fprintf(&p, "    else { print(\"OK %s \"); ::hexline(out); } }\n", name)
		p.WriteString("}\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(p.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := string(sqrun.Run(t, dir, "main.nut", wire))
	saved := map[string][]byte{}
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "FAIL ") {
			t.Error(line)
			continue
		}
		rest, ok := strings.CutPrefix(line, "OK ")
		if !ok {
			continue
		}
		name, hex, _ := strings.Cut(rest, " ")
		b, err := decodeHex(hex)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		saved[name] = b
	}
	for _, name := range names {
		got, ok := saved[name]
		if !ok {
			t.Errorf("%s: the probe did not finish it:\n%s", name, out)
			continue
		}
		if !bytes.Equal(got, wire[name]) {
			t.Errorf("%s: the leg saved %d bytes that differ from the engine's %d", name, len(got), len(wire[name]))
			continue
		}
		inst := m.New(st)
		var rep tabletext.Report
		if ok, err := tablewire.Decode(m, inst, got, &rep); !ok || err != nil || rep != (tabletext.Report{}) {
			t.Errorf("%s: the engine did not read the leg's bytes cleanly: ok=%v err=%v rep=%+v", name, ok, err, rep)
			continue
		}
		back, err := m.Write(inst)
		if err != nil || !bytes.Equal(back, text[name]) {
			t.Errorf("%s: the value the leg loaded and saved is not the sample's (%v)", name, err)
		}
	}
}
