package jstable

// A LIST INSIDE A LIST ELEMENT, THROUGH THE MESSAGE FORM (docs/SPEC-TABLES.md
// §3.3): an array of a union whose arms are themselves an array and a text is
// written and read INLINE inside the element loop, so the arm's loop and the
// element's loop are live in one body. Every sample puts every bounded array
// at its declared maximum, and rotates the arms so each arm kind sits at every
// element position.
//
// The Go engine (internal/tablewire) writes each sample as a one-body batch,
// the generated <Root>LoadMessages reads it, and the generated
// <Root>SaveMessages writes it back: the JS batch must equal the engine's, and
// the engine must read the JS batch back to the sample's own value.

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// nestedArmProbe is the schema: a counted and a fixed array of a union whose
// arms are a float array, a text and an int array, an enum-keyed array of
// integers, and a counted array of a record holding an array. A keyed array
// of unions, and a keyed array as an arm, are refused by the checker, so a
// keyed loop is never nested in these bodies.
const nestedArmProbe = `package nestedarmprobe

enum Slot { Red, Blue, Green }

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
    arms  [..4]Arm
    pair  [2]Arm
    keyed [Slot]int16
    rows  [..3]Row
}
`

// nestedArmSamples answers one sample per rotation of the arms: element e of
// arms (e = 0..3) and of pair (e = 4..5) holds arm (e + k) % 3, every arm
// array at its bound, and every value distinct by element and position, so
// each element's bytes are its own. One keyed slot per sample is left at
// zero, so the slots that ride differ by sample.
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
		b.WriteString(`],"keyed":{`)
		for j, name := range []string{"Red", "Blue", "Green"} {
			if j > 0 {
				b.WriteString(",")
			}
			slot := 0
			if j != k {
				slot = 50 + 10*j + k
			}
			fmt.Fprintf(&b, "%q:%d", name, slot)
		}
		b.WriteString(`},"rows":[`)
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

func TestJSMessageFormNestedArmLoops(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")
	t.Parallel()
	node := jsNode(t)
	u := unitFrom(t, nestedArmProbe)
	st := findTable(t, u, "Box")
	m := tabletext.NewModel(u)
	samples := nestedArmSamples()
	names := slices.Sorted(maps.Keys(samples))
	extra := map[string][]byte{"ann.bin": tablewire.Announce(u)}
	batch, text := map[string][]byte{}, map[string][]byte{}
	for _, name := range names {
		inst := m.New(st)
		var rep tabletext.Report
		if !m.Read(inst, []byte(samples[name]), &rep) || rep != (tabletext.Report{}) {
			t.Fatalf("%s: the engine did not read the sample cleanly: %+v", name, rep)
		}
		b, err := tablewire.EncodeMessages(m, []*tabletext.Instance{inst})
		if err != nil {
			t.Fatalf("%s: the engine did not write the batch: %v", name, err)
		}
		tx, err := m.Write(inst)
		if err != nil {
			t.Fatal(err)
		}
		batch[name], text[name] = b, tx
		extra[name+".batch"] = b
	}
	var p strings.Builder
	p.WriteString(messageProbeHeader(u, st, "ann.bin"))
	p.WriteString("import { writeFileSync } from \"node:fs\";\n")
	for _, name := range names {
		fmt.Fprintf(&p, `{
  const storage = [new Box()];
  const batch = new Uint8Array(readFileSync("./%[1]s.batch"));
  const lr = {};
  const count = BoxLoadMessages(storage, v, batch, lr);
  if (count !== 1 || lr.Verdict !== 0 || lr.Damaged || lr.Oor || lr.Unknown || lr.Mismatch) {
    fail("%[1]s: LoadMessages count=" + count + " " + JSON.stringify(lr));
  } else {
    const buf = new Uint8Array(65536);
    const sr = {};
    const n = BoxSaveMessages(storage, buf, sr);
    if (n < 0 || sr.Verdict !== 0) { fail("%[1]s: SaveMessages " + JSON.stringify(sr)); }
    else { writeFileSync("./%[1]s.saved", buf.subarray(0, n)); }
  }
}
`, name)
	}
	p.WriteString(probeTail())
	dir := t.TempDir()
	out, err := jsRunMessageProbeIn(t, node, u, dir, extra, p.String())
	if err != nil {
		t.Fatalf("the probe failed: %v\n%s", err, out)
	}
	var vocab tablewire.Vocabulary
	if err := vocab.AnnounceRead(tablewire.Announce(u), &tabletext.Report{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		saved, err := os.ReadFile(filepath.Join(dir, name+".saved"))
		if err != nil {
			t.Errorf("%s: no saved batch: %v\n%s", name, err, out)
			continue
		}
		if !bytes.Equal(saved, batch[name]) {
			t.Errorf("%s: the leg saved %d bytes that differ from the engine's %d", name, len(saved), len(batch[name]))
			continue
		}
		inst := m.New(st)
		var rep tabletext.Report
		if n, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, saved, &vocab, &rep); n != 1 || !ok || err != nil || rep != (tabletext.Report{}) {
			t.Errorf("%s: the engine did not read the leg's batch cleanly: n=%d ok=%v err=%v rep=%+v", name, n, ok, err, rep)
			continue
		}
		back, err := m.Write(inst)
		if err != nil || !bytes.Equal(back, text[name]) {
			t.Errorf("%s: the value the leg loaded and saved is not the sample's (%v)", name, err)
		}
	}
}

// jsRunMessageProbeIn is jsRunMessageProbe in a directory the caller names,
// so the caller reads back the files the probe wrote.
func jsRunMessageProbeIn(t *testing.T, node string, u *ir.Unit, dir string, extra map[string][]byte, probe string) ([]byte, error) {
	t.Helper()
	files := generatedTree(u)
	maps.Copy(files, extra)
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "probe.mjs"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, filepath.Join(dir, "probe.mjs"))
	cmd.Dir = dir
	return cmd.CombinedOutput()
}
