package lobstertable_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// Map keys are data, not field names: both unbounded text and string(N)
// with N > 255 must retain accepted keys past the field-name scanner limit.
func TestLobsterJSONLongMapKeys(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "LongKeys.schema")
	if err := os.WriteFile(path, []byte("package longkeysprobe\ntable Root { loose map[string]int32\n bounded map[string(512)]int32 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	// The ASCII keys share their first 300 bytes; truncating them must not
	// silently merge two entries. UTF-8 and escaped keys exercise stored bytes.
	values := map[string]int{strings.Repeat("a", 300) + "x": 11, strings.Repeat("a", 300) + "y": 22, strings.Repeat("α", 200): 33, strings.Repeat("\"", 300): 44}
	input, err := json.Marshal(map[string]any{"loose": values, "bounded": values})
	if err != nil {
		t.Fatal(err)
	}
	var program strings.Builder
	for _, mod := range g.mods {
		fmt.Fprintf(&program, "import .%s\n", mod)
	}
	program.WriteString(`import .json
let input = read_file("input.json")
assert input
let report = schema_rt.LoadReport {}
let value = schema_rt.json_read("Root", input, report)
assert value and not report.malformed
let text, reason = schema_rt.json_write(value)
assert reason == ""
print(text)
`)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(program.String()), 0644); err != nil {
		t.Fatal(err)
	}
	matches := func() bool {
		out := lobsterrun.Run(t, g.dir, "main.lobster", map[string][]byte{"input.json": input})
		var got, want any
		if err := json.Unmarshal(input, &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatalf("invalid JSON: %v: %s", err, out)
		}
		return reflect.DeepEqual(got, want)
	}
	if !matches() {
		t.Fatal("JSON read/write dropped or changed long map keys")
	}
	runtime := filepath.Join(g.dir, "json.lobster")
	raw, err := os.ReadFile(runtime)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "j_scan_stored(r, key_limit)"
	if strings.Count(string(raw), anchor) != 1 {
		t.Fatal("map-key mutation anchor must occur once")
	}
	if err := os.WriteFile(runtime, []byte(strings.Replace(string(raw), anchor, "j_scan_stored(r, json_max_key)", 1)), 0644); err != nil {
		t.Fatal(err)
	}
	if matches() {
		t.Fatal("negative control: restoring the 255-byte limit preserved long map keys")
	}
}
