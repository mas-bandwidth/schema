//go:build schema_leg_squirrel

// TestSquirrelCarriesUnboundedText proves Squirrel carries the unbounded-text
// element and map key (docs/SPEC-TABLES.md §2.13.1): the construct's probe
// unit generates, the emitted module reads and writes it under the pinned
// interpreter, a value saved and loaded back equals itself, its saved bytes
// are internal/tablewire's for the same value, and its JSON text is
// internal/tabletext's byte for byte.
package compiler

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// squirrelUnboundedTextProbe is the probe unit for this construct, so the
// construct this card lifts is the construct that is run.
const squirrelUnboundedTextProbe = "package probe\n\ntable L\n{\n    rules []string\n    recent [..2]string\n    byname map[string]int32\n}\n"

func TestSquirrelCarriesUnboundedText(t *testing.T) {
	ensurePinnedSquirrel(t)
	t.Parallel()

	u := unitFromSource(t, squirrelUnboundedTextProbe)

	// the probe unit generates for squirrel with no error
	files, err := New().Generate(u, "squirrel", Options{})
	if err != nil {
		t.Fatalf("squirrel refused the unbounded text the card lifts: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("squirrel emitted no files for the unbounded text")
	}

	// THE ORACLE: the same value through the Go engine — its wire and its JSON
	// text (docs/SPEC-TABLES.md §3, §16.1).
	m := tabletext.NewModel(u)
	v := m.New(m.Lookup("L"))
	var report tabletext.Report
	json := `{"rules":["alpha","beta"],"recent":["c"],"byname":{"k1":1,"k2":2}}`
	if !m.Read(v, []byte(json), &report) {
		t.Fatalf("tabletext refused the probe text: %v", report)
	}
	wantWire, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatalf("tablewire.Encode: %v", err)
	}
	wantJSON, err := m.Write(v)
	if err != nil {
		t.Fatalf("tabletext.Write: %v", err)
	}
	if !bytes.HasSuffix(wantJSON, []byte("\n")) {
		wantJSON = append(wantJSON, '\n')
	}

	dir := t.TempDir()
	unitDir := filepath.Join(dir, "probe")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(unitDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	main := squirrelProbe("L", squirreltable.ModuleName(u), true)
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := sqrun.Run(t, dir, "main.nut", map[string][]byte{"wire": wantWire})

	var gotWire, gotJSON []byte
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "wire:"):
			gotWire = decodeHex(t, strings.TrimPrefix(line, "wire:"))
		case strings.HasPrefix(line, "json:"):
			gotJSON = decodeHex(t, strings.TrimPrefix(line, "json:"))
		case line != "":
			t.Fatalf("the Squirrel probe failed: %s", line)
		}
	}
	if !bytes.Equal(gotWire, wantWire) {
		t.Fatalf("the Squirrel save differs from tablewire.Encode\n got  %x\n want %x", gotWire, wantWire)
	}
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("the Squirrel ToJson differs from tabletext.Write\n got  %q\n want %q", gotJSON, wantJSON)
	}

	if !carries("squirrel", carryUnboundedText) {
		t.Fatalf("the Squirrel target is not registered as carrying the unbounded text (compiler/target_squirrel_unboundedtext.go, docs/SPEC-TABLES.md §2.13.1)")
	}
	if _, err := New().Generate(u, "c", Options{}); err == nil {
		t.Fatalf("an unregistered target generated the unbounded text; only squirrel carries it")
	}
}
