package rusttable

// A [Min..N]T OF UNION OR POINTER ELEMENTS IS CARRIED (docs/SPEC-TABLES.md §3's
// elision table, the [Min..N]T row; §4; SPEC §4.6). The Rust variable class
// once refused these by name; it carries them at their birth count, and this
// test holds the Rust leg to the shared corpus
// testdata/conformance/feature-witnesses/counted-minimum-shapes (the engine
// holds the same rows, internal/tablewire TestCountedMinimumShapesWitnessPins):
// every save row's text saves the committed bytes, a fresh value saves the
// fresh row's bytes (Min elements at their zero elide), every text row
// refuses, and every read row's verdict is the row's. If the refusal is
// planted back (variableFieldConstruct naming a counted minimum of union or
// pointer elements), generation refuses the unit and this goes red. Text and
// bytes elements stay the checker's refusal (internal/check
// stringarrays_test.go, [A..N]string(M)). A FUNCTIONAL TEST where it runs
// rustc: no clock, no socket, no network.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const countedMinShapesDir = "../../../testdata/conformance/feature-witnesses/counted-minimum-shapes"

func TestRustCountedMinUnionAndPointerCarried(t *testing.T) {
	t.Parallel()
	keyedRequireRustc(t)
	src, err := os.ReadFile(filepath.Join(countedMinShapesDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(src))
	rows, err := os.ReadFile(filepath.Join(countedMinShapesDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	wire := func(name string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(countedMinShapesDir, name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var body strings.Builder
	var ids []string
	for i, line := range strings.Split(string(rows), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			t.Fatalf("cases.tsv:%d: %d fields, want 5", i+1, len(f))
		}
		kind, name, value, verdict, want := f[0], f[1], f[2], f[3], f[4]
		switch {
		case kind == "save" && verdict == "wrote":
			golden := rustByteVector(wire(want))
			fmt.Fprintf(&body, "    { let mut rep = TableReport::default(); let v = r_from_json(%s, &mut rep);\n", rustRaw(value))
			fmt.Fprintf(&body, "      println!(\"CMIN %s {}\", v.and_then(|v| r_save_checked(&v).ok()).map(|b| b == vec!%s).unwrap_or(false)); }\n", name, golden)
			ids = append(ids, name)
			if name == "save_fresh" {
				fmt.Fprintf(&body, "    println!(\"CMIN %s_default {}\", r_save_checked(&R::default()).map(|b| b == vec!%s).unwrap_or(false));\n", name, golden)
				ids = append(ids, name+"_default")
			}
		case kind == "text" && verdict == "refused":
			fmt.Fprintf(&body, "    { let mut rep = TableReport::default(); println!(\"CMIN %s {}\", r_from_json(%s, &mut rep).is_none()); }\n", name, rustRaw(value))
			ids = append(ids, name)
		case kind == "read" && verdict == "read":
			// read, and save back the same bytes
			golden := rustByteVector(wire(value))
			fmt.Fprintf(&body, "    { let mut rep = TableReport::default(); let v = r_load(&vec!%s, &mut rep);\n", golden)
			fmt.Fprintf(&body, "      println!(\"CMIN %s {}\", !rep.damaged && v.and_then(|v| r_save_checked(&v).ok()).map(|b| b == vec!%s).unwrap_or(false)); }\n", name, golden)
			ids = append(ids, name)
		case kind == "read" && verdict == "refused":
			fmt.Fprintf(&body, "    { let mut rep = TableReport::default(); println!(\"CMIN %s {}\", r_load(&vec!%s, &mut rep).is_none()); }\n", name, rustByteVector(wire(value)))
			ids = append(ids, name)
		default:
			t.Fatalf("%s: row %q/%q", name, kind, verdict)
		}
	}
	if len(ids) < 10 {
		t.Fatalf("cases.tsv yields %d probes, want at least 10", len(ids))
	}
	out, err := rustConstructProbe(t, u, body.String())
	if err != nil {
		t.Fatalf("the probe did not run: %v\n%s", err, out)
	}
	for _, id := range ids {
		if !strings.Contains(out, "CMIN "+id+" true") {
			t.Errorf("%s: the Rust variable form does not carry a [Min..N]T of union or pointer elements as the corpus states:\n%s", id, out)
		}
	}
}
