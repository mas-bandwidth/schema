// Payload-free packet union arms carry exactly their tag bits (SPEC §4.8,
// #503): the Lobster leg of test/packet-void, run by make packet-void-lobster.
// The family's one schema (test/packet-void/Void.schema) is generated, and the
// driver test/packet-void/lobster/main.lobster answers every case of the one
// case file (testdata/wire/packet-void/cases.txt) against the Go oracle's
// pins, one "case <name> ok|FAILED: <why>" line each; internal/casegate
// judges the lines. Lobster has no process-level file listing of its own, so
// the driver runs inside this Go test with cases.txt and every pin beside it
// (casegate.Inputs), as the Python and Ruby legs do.
//
// A Lobster writer refuses by trapping, which ends the program, so each
// refuse-write case first runs the driver on its own (`-- write <case>`);
// its exit code and stdout go back in as <case>.trap, and the full run
// judges them (the driver's header says how).
//
// The control adds one bit to the Mixed union's void arm on just the write or
// just the read path of the generated code, and requires exactly that
// direction's set of cases red, mixed-ping on its exact bit count, and every
// other case ok.
package lobster

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/casegate"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// runPacketVoid generates Void.schema, lets patch alter the generated files,
// and runs the driver over the shared corpus: its stdout, the case names and
// the leg's owed lines.
func runPacketVoid(t *testing.T, patch func(map[string][]byte)) ([]byte, []string, map[string]string) {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	src := t.TempDir()
	schema, err := os.ReadFile(filepath.Join(root, "test", "packet-void", "Void.schema"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, src, "Void.schema", string(schema))
	files, err := Generate(unitFromDir(t, src))
	if err != nil {
		t.Fatal(err)
	}
	if patch != nil {
		patch(files)
	}
	inputs, cases, owed, err := casegate.Inputs(filepath.Join(root, "testdata", "wire", "packet-void"), "lobster")
	if err != nil {
		t.Fatal(err)
	}
	driver, err := os.ReadFile(filepath.Join(root, "test", "packet-void", "lobster", "main.lobster"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeAll(t, dir, files)
	writeFile(t, dir, "main.lobster", string(driver))
	// one trap run per refuse-write case; serial because every run shares
	// dir, and each is one short JIT run
	for _, name := range refuseWriteCases(t, string(inputs["cases.txt"])) {
		out, code := lobsterrun.RunExit(t, dir, "main.lobster", nil, "write", name)
		inputs[name+".trap"] = fmt.Appendf(nil, "exit %d\n%s", code, out)
	}
	return lobsterrun.Run(t, dir, "main.lobster", inputs), cases, owed
}

// refuseWriteCases are the names of cases.txt's refuse-write cases.
func refuseWriteCases(t *testing.T, text string) []string {
	t.Helper()
	var names []string
	for line := range strings.SplitSeq(text, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		if len(f) < 3 {
			t.Fatalf("cases.txt line %q has no verdict", line)
		}
		if f[2] == "refuse-write" {
			names = append(names, f[0])
		}
	}
	if len(names) == 0 {
		t.Fatal("cases.txt names no refuse-write case")
	}
	return names
}

func TestLobsterPacketVoid(t *testing.T) {
	t.Parallel()
	out, cases, owed := runPacketVoid(t, nil)
	red, summary := casegate.Verdict("packet-void-lobster", "lobster", 0, out, cases, owed)
	if len(red) > 0 {
		t.Fatalf("%s\n%s\n%s", summary, strings.Join(red, "\n"), out)
	}
	t.Log(summary)
}

// failedCases reads the driver's answers: every case answered exactly once,
// "ok" or "FAILED: <why>", and no other line. It returns the sorted names
// answered FAILED.
func failedCases(t *testing.T, out []byte, cases []string) []string {
	t.Helper()
	answered := map[string]bool{}
	var failed []string
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		rest, ok := strings.CutPrefix(line, "case ")
		name, why, _ := strings.Cut(rest, " ")
		if !ok || name == "" {
			t.Fatalf("the driver printed %q, not a case answer:\n%s", line, out)
		}
		if answered[name] {
			t.Fatalf("case %s answered twice:\n%s", name, out)
		}
		answered[name] = true
		switch {
		case why == "ok":
		case strings.HasPrefix(why, "FAILED: "):
			failed = append(failed, name)
		default:
			t.Fatalf("case %s answered %q, neither ok nor FAILED:\n%s", name, why, out)
		}
	}
	for _, c := range cases {
		if !answered[c] {
			t.Fatalf("case %s has no answer:\n%s", c, out)
		}
		delete(answered, c)
	}
	for name := range answered {
		t.Fatalf("case %s is not in cases.txt:\n%s", name, out)
	}
	slices.Sort(failed)
	return failed
}

func TestLobsterPacketVoidNegativeControls(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		dir, anchor, with, marker string
		// red is exactly the cases the extra bit breaks; every other case
		// answers ok
		red []string
	}{
		{"write", "    s·.serialize_bits(v·.Type, 2) // the union tag (SPEC §4.8)\n    if v·.Type == MixedTypePayload:",
			"    s·.serialize_bits(v·.Type, 2) // the union tag (SPEC §4.8)\n    if v·.Type == MixedTypePing: s·.serialize_bits(0, 1)\n    if v·.Type == MixedTypePayload:",
			"case mixed-ping FAILED: write has 3 bits, want 2",
			// every case that writes a Mixed ping: alone, inside Frame, as a
			// Batch element; select writes payload, and Late is another type
			[]string{"batch-ping", "batch-ping-payload", "frame-ping", "mixed-ping"}},
		{"read", "    value·.Type = MixedType(rv·)\n",
			"    value·.Type = MixedType(rv·)\n    if value·.Type == MixedTypePing:\n        rv·, rok· = stream·.serialize_bits(rv·, 1)\n",
			"case mixed-ping FAILED: read has 3 bits, want 2",
			// every case that reads a Mixed ping, and mixed-padding: its pin
			// 05 is ping with padding bit 2 set, which the extra read bit
			// swallows, so the end check sees clean padding and accepts.
			// mixed-ping-select-payload's read of mixed-ping still takes
			// ping (its extra bit is the zero padding), and it writes payload.
			[]string{"batch-ping", "batch-ping-payload", "frame-ping", "mixed-padding", "mixed-ping"}},
	} {
		t.Run(tc.dir, func(t *testing.T) {
			t.Parallel()
			out, cases, _ := runPacketVoid(t, func(files map[string][]byte) {
				text := string(files["Void.lobster"])
				if strings.Count(text, tc.anchor) != 1 {
					t.Fatalf("the %s anchor matched %d times", tc.dir, strings.Count(text, tc.anchor))
				}
				files["Void.lobster"] = []byte(strings.Replace(text, tc.anchor, tc.with, 1))
			})
			if !slices.Contains(strings.Split(string(out), "\n"), tc.marker) {
				t.Fatalf("an extra void %s bit did not fail %q:\n%s", tc.dir, tc.marker, out)
			}
			if red := failedCases(t, out, cases); !slices.Equal(red, tc.red) {
				t.Fatalf("an extra void %s bit turned %v red, want exactly %v:\n%s", tc.dir, red, tc.red, out)
			}
		})
	}
}
