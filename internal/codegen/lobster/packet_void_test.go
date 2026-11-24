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
// The control adds one bit to the Mixed union's void arm on just the write or
// just the read path of the generated code, and requires the driver to fail
// mixed-ping by name.
package lobster

import (
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
	return lobsterrun.Run(t, dir, "main.lobster", inputs), cases, owed
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

func TestLobsterPacketVoidNegativeControls(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ dir, anchor, with, marker string }{
		{"write", "    s·.serialize_bits(v·.Type, 2) // the union tag (SPEC §4.8)\n    if v·.Type == MixedTypePayload:",
			"    s·.serialize_bits(v·.Type, 2) // the union tag (SPEC §4.8)\n    if v·.Type == MixedTypePing: s·.serialize_bits(0, 1)\n    if v·.Type == MixedTypePayload:",
			"case mixed-ping FAILED: write has 3 bits, want 2"},
		{"read", "    value·.Type = MixedType(rv·)\n",
			"    value·.Type = MixedType(rv·)\n    if value·.Type == MixedTypePing:\n        rv·, rok· = stream·.serialize_bits(rv·, 1)\n",
			"case mixed-ping FAILED: read has 3 bits, want 2"},
	} {
		t.Run(tc.dir, func(t *testing.T) {
			t.Parallel()
			out, _, _ := runPacketVoid(t, func(files map[string][]byte) {
				text := string(files["Void.lobster"])
				if strings.Count(text, tc.anchor) != 1 {
					t.Fatalf("the %s anchor matched %d times", tc.dir, strings.Count(text, tc.anchor))
				}
				files["Void.lobster"] = []byte(strings.Replace(text, tc.anchor, tc.with, 1))
			})
			if !slices.Contains(strings.Split(string(out), "\n"), tc.marker) {
				t.Fatalf("an extra void %s bit did not fail %q:\n%s", tc.dir, tc.marker, out)
			}
		})
	}
}
