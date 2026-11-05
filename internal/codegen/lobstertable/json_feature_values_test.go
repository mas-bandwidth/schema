package lobstertable_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// The feature witness is accepted JSON, so read then write must preserve
// its values, irrespective of local diagnostic counters.
func TestLobsterJSONFeatureValues(t *testing.T) {
	t.Parallel()
	g, err := generateLoadUnit(t, []string{filepath.Join(root, "testdata/conformance/feature-witnesses/full/Features.schema")})
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.ReadFile(filepath.Join(root, "testdata/conformance/tables/json/feature_full.json"))
	if err != nil {
		t.Fatal(err)
	}
	expectedWire, err := os.ReadFile(filepath.Join(root, "testdata/conformance/feature-witnesses/full/Root.bin"))
	if err != nil {
		t.Fatal(err)
	}
	var program strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&program, "import .%s\n", mod)
	}
	program.WriteString(`import .json
import .schema_save
` + lobsterrun.HexProbe + `
let input = read_file("input.json")
assert input
let report = schema_rt.LoadReport {}
let value = schema_rt.json_read("Root", input, report)
assert value and not report.malformed
let wire, why = schema_rt.save_reasoned(value)
assert why == ""
hex(wire)
let text, reason = schema_rt.json_write(value)
assert reason == ""
print(text)
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program.String()), 0644); err != nil {
		t.Fatal(err)
	}
	out := string(lobsterrun.Run(t, g.dir, "main.lobster", map[string][]byte{"input.json": input}))
	wireHex, text, ok := strings.Cut(out, "\n")
	if !ok {
		t.Fatalf("missing outputs: %s", out)
	}
	wire, err := hex.DecodeString(strings.TrimSpace(wireHex))
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	if err := json.Unmarshal(input, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("invalid output JSON: %v\n%s", err, text)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("JSON values changed after read/write:\nwant: %s\ngot: %s", input, text)
	}
	if !bytes.Equal(wire, expectedWire) {
		t.Errorf("JSON read/save wire differs: got %d bytes, want %d", len(wire), len(expectedWire))
	}
	runtime := filepath.Join(g.dir, "json.lobster")
	raw, err := os.ReadFile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = `let out, clamped, ok = j_scan_stored(r, if vf.s == "text": -1 else: vf.bound)`
	if strings.Count(string(raw), anchor) != 1 {
		t.Fatal("unbounded text list scan anchor must occur once")
	}
	if err := os.WriteFile(runtime, []byte(strings.Replace(string(raw), anchor, "let out, clamped, ok = j_scan_stored(r, vf.bound)", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	brokenOut := string(lobsterrun.Run(t, g.dir, "main.lobster", map[string][]byte{"input.json": input}))
	brokenWireHex, brokenText, ok := strings.Cut(brokenOut, "\n")
	if !ok {
		t.Fatalf("missing broken outputs: %s", brokenOut)
	}
	brokenWire, err := hex.DecodeString(strings.TrimSpace(brokenWireHex))
	if err != nil {
		t.Fatal(err)
	}
	var brokenGot any
	if err := json.Unmarshal([]byte(brokenText), &brokenGot); err != nil {
		t.Fatalf("invalid broken output JSON: %v\n%s", err, brokenText)
	}
	if bytes.Equal(brokenWire, expectedWire) || reflect.DeepEqual(brokenGot, want) {
		t.Fatal("negative control: bounding unbounded text list elements preserved wire or JSON values")
	}
}
