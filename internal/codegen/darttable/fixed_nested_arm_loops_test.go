package darttable

// A LIST INSIDE A LIST ELEMENT, THROUGH THE FIXED FORM (docs/SPEC-TABLES.md
// §3.4): an array of a union whose arms are themselves an array and a text is
// written and read INLINE inside the element loop, with no function call
// between the two loops, so the arm's loop and the element's loop are live in
// one body. Every sample puts every bounded array at its declared maximum, and
// rotates the arms so each arm kind sits at every element position.
//
// The Go engine (internal/tablewire) writes each sample's form-3 file, the
// generated <root>FixedLoad reads it, and the generated <root>FixedSave writes
// it back: the Dart bytes must equal the engine's, and the engine must read the
// Dart bytes back to the sample's own value. The save's finite pass must find a
// NaN planted in any arm array element of any position.

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

// nestedArmEngine reads every sample into the engine and answers its form-3
// bytes and its canonical text, by sample name.
func nestedArmEngine(t *testing.T, u *ir.Unit, root string, encode func(*tabletext.Model, *tabletext.Instance) ([]byte, error)) (m *tabletext.Model, wire, text map[string][]byte) {
	t.Helper()
	m = tabletext.NewModel(u)
	st := m.Lookup(root)
	if st == nil {
		t.Fatalf("no root %s", root)
	}
	wire, text = map[string][]byte{}, map[string][]byte{}
	for name, src := range nestedArmSamples() {
		inst := m.New(st)
		var rep tabletext.Report
		if !m.Read(inst, []byte(src), &rep) || rep != (tabletext.Report{}) {
			t.Fatalf("%s: the engine did not read the sample cleanly: %+v", name, rep)
		}
		b, err := encode(m, inst)
		if err != nil {
			t.Fatalf("%s: the engine did not write the sample: %v", name, err)
		}
		tx, err := m.Write(inst)
		if err != nil {
			t.Fatalf("%s: the engine did not write the sample's text: %v", name, err)
		}
		wire[name], text[name] = b, tx
	}
	return m, wire, text
}

func TestDartFixedFormNestedArmLoops(t *testing.T) {
	slowtest.Gate(t, "dart")
	t.Parallel()
	u := unitFrom(t, nestedArmProbe)
	m, wire, text := nestedArmEngine(t, u, "Box", tablewire.Encode)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, packetFiles(t, u))
	dir := t.TempDir()
	writeFiles(t, dir, files)
	names := slices.Sorted(maps.Keys(wire))
	for _, name := range names {
		if wire[name][0] != 3 {
			t.Fatalf("%s: the engine's file is not form 3", name)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".wire"), wire[name], 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var p strings.Builder
	p.WriteString(`import 'dart:io';
import 'dart:typed_data';
import 'ProbeFixed.dart' as f;

void main() {
  for (final name in [`)
	for i, name := range names {
		if i > 0 {
			p.WriteString(", ")
		}
		fmt.Fprintf(&p, "'%s'", name)
	}
	p.WriteString(`]) {
    final data = File('$name.wire').readAsBytesSync();
    final v = f.Box();
    final rep = f.TableFixedReport();
    final n = f.boxFixedLoad([v], 1, data, data.length, f.boxFixedNewPlan(), rep);
    if (n != 1 || rep.refused != 0 || rep.unknown != 0 || rep.widened != 0) {
      print('FAIL $name: load answered $n refused=${rep.refused}');
      continue;
    }
    final out = Uint8List(f.boxFixedMeasure(1));
    final w = f.boxFixedSave([v], 1, out);
    if (w != out.length) {
      print('FAIL $name: save answered $w of ${out.length}');
      continue;
    }
    File('$name.saved').writeAsBytesSync(out);
    // THE FINITE PASS SEES EVERY ARM ELEMENT: a NaN planted in any float
    // arm element of any position refuses the save.
    var planted = 0;
    for (final arms in [v.arms, v.pair]) {
      for (var e = 0; e < arms.length; e++) {
        final a = arms[e];
        // the tag is an int or a <Union>Type value, by where the union lives
        final tag = a.type.toString();
        if (tag != '1' && !tag.endsWith('.vals')) continue;
        for (var j = 0; j < a.valsCount; j++) {
          final keep = a.vals[j];
          a.vals[j] = double.nan;
          if (f.boxFixedSave([v], 1, Uint8List(out.length)) != -1) {
            print('FAIL $name: a NaN at element $e position $j saved');
          }
          a.vals[j] = keep;
          planted++;
        }
      }
    }
    print('OK $name $planted');
  }
}
`)
	if err := os.WriteFile(filepath.Join(dir, "probe.dart"), []byte(p.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "probe.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	nestedArmCompare(t, m, "Box", names, string(out), dir, wire, text, tablewire.Decode)
}

// nestedArmCompare checks the probe's lines: every sample said OK, the bytes
// the leg saved are the engine's, and the engine reads them to the sample.
func nestedArmCompare(t *testing.T, m *tabletext.Model, root string, names []string, out, dir string, wire, text map[string][]byte,
	decode func(*tabletext.Model, *tabletext.Instance, []byte, *tabletext.Report) (bool, error)) {
	t.Helper()
	ok := map[string]bool{}
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "FAIL ") {
			t.Error(line)
		}
		if rest, found := strings.CutPrefix(line, "OK "); found {
			name, _, _ := strings.Cut(rest, " ")
			ok[name] = true
		}
	}
	for _, name := range names {
		if !ok[name] {
			t.Errorf("%s: the probe did not finish it:\n%s", name, out)
			continue
		}
		saved, err := os.ReadFile(filepath.Join(dir, name+".saved"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(saved, wire[name]) {
			t.Errorf("%s: the leg saved %d bytes that differ from the engine's %d", name, len(saved), len(wire[name]))
			continue
		}
		inst := m.New(m.Lookup(root))
		var rep tabletext.Report
		if ok, err := decode(m, inst, saved, &rep); !ok || err != nil || rep != (tabletext.Report{}) {
			t.Errorf("%s: the engine did not read the leg's bytes cleanly: ok=%v err=%v rep=%+v", name, ok, err, rep)
			continue
		}
		back, err := m.Write(inst)
		if err != nil || !bytes.Equal(back, text[name]) {
			t.Errorf("%s: the value the leg loaded and saved is not the sample's (%v)", name, err)
		}
	}
}
