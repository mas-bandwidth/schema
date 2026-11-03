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
}
