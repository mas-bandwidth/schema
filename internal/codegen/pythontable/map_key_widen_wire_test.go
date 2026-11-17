package pythontable

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func mapKeyWireProbe(t *testing.T, u *ir.Unit, root string, wire, canonical []byte, widened int, why string) {
	t.Helper()
	m := tabletext.NewModel(u)
	value := m.New(m.Lookup(root))
	var report tabletext.Report
	ok, err := tablewire.Decode(m, value, wire, &report)
	if why != "" {
		if ok || string(tablewire.RefusalReason(err)) != why {
			t.Fatalf("Go refusal: %v %+v, want %s", err, report, why)
		}
	} else {
		if !ok || err != nil || report.Widened != widened || report.Unknown != 0 {
			t.Fatalf("Go widening: %v %+v, want %d", err, report, widened)
		}
		got, err := tablewire.Encode(m, value)
		if err != nil || !bytes.Equal(got, canonical) {
			t.Fatalf("Go canonical differs: %v", err)
		}
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	probe := fmt.Sprintf(`from %s import %s, LoadReport, Refused
from pathlib import Path
r=LoadReport()
`, u.Package, root)
	if why != "" {
		probe += fmt.Sprintf(`r.unknown=12;r.widened=13
try: %s.load(Path("wire").read_bytes(),r)
except Refused as e: assert e.reason==%q,(e.reason,%q)
else: raise AssertionError("map key refusal was lost")
assert (r.unknown,r.widened)==(12,13)
`, root, why, why)
	} else {
		probe += fmt.Sprintf(`v=%s.load(Path("wire").read_bytes(),r)
assert (r.unknown,r.widened,r.retained,r.retain_lost)==(0,%d,0,0),r.widened
assert v.save()==Path("canonical").read_bytes()
assert v.measure()==len(v.save())
assert v==%s.load(Path("canonical").read_bytes())
`, root, widened, root)
	}
	probe += "print(\"ok\")\n"
	mapBlobProbe(t, files, probe, map[string][]byte{"wire": wire, "canonical": canonical}, "")
}

func TestPythonMapKeyWidenManifest(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnitPaths(t, "../../../tables/maps")
	wire, err := os.ReadFile("../../../testdata/wire/tables/fuzz-vectors/map_key_widen.bin")
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := os.ReadFile("../../../testdata/conformance/tables/save-wire/map_key_widen.bin")
	if err != nil {
		t.Fatal(err)
	}
	// Actual C++ report/save artifacts; the first key is int8, the second int32.
	mapKeyWireProbe(t, u, "Chunks", wire, canonical, 1, "")
}

func TestPythonMapKeyWidenKinds(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	for _, c := range []struct {
		name, source, target, text, why string
		widened                         int
	}{
		{"signed-many", "int8", "int32", `{"values":{"-3":4,"2":5}}`, "", 1},
		{"unsigned-many", "uint8", "uint64", `{"values":{"1":4,"200":5}}`, "", 1},
		{"signed-32-64", "int32", "int64", `{"values":{"-2147483648":4,"2147483647":5}}`, "", 1},
		{"unsigned-32-64", "uint32", "uint64", `{"values":{"4294967295":5}}`, "", 1},
		{"omitted-zero-key", "int8", "int32", `{"values":{"0":5}}`, "", 0},
		{"exact", "int32", "int32", `{"values":{"-3":4,"2":5}}`, "", 0},
		{"signedness", "int8", "uint32", `{"values":{"2":5}}`, "kind_mismatch", 0},
		{"reverse-signedness", "uint8", "int32", `{"values":{"2":5}}`, "kind_mismatch", 0},
		{"narrowing", "int64", "int32", `{"values":{"2":5}}`, "kind_mismatch", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			schema := func(kind string) string {
				return fmt.Sprintf("package probe\ntable Root { values map[%s]int32 }\n", kind)
			}
			source, target := loadUnit(t, schema(c.source)), loadUnit(t, schema(c.target))
			wire := pythonVTMapEngineWire(t, source, "Root", c.text)
			var canonical []byte
			if c.why == "" {
				canonical = pythonVTMapEngineWire(t, target, "Root", c.text)
			}
			mapKeyWireProbe(t, target, "Root", wire, canonical, c.widened, c.why)
		})
	}
	// Valid framing with duplicate widened keys must still refuse structurally.
	source := loadUnit(t, "package probe\ntable Root { values map[int8]int32 }\n")
	target := loadUnit(t, "package probe\ntable Root { values map[int32]int32 }\n")
	mapKeyWireProbe(t, target, "Root", pythonVTMapKeyedWire(t, source, "Root", []int64{2, 2}), nil, 0, "wire_damaged")
}

func TestPythonMapKeyWidenCountNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters")
	source := loadUnit(t, "package probe\ntable Root { values map[int8]int32 }\n")
	target := loadUnit(t, "package probe\ntable Root { values map[int32]int32 }\n")
	wire := pythonVTMapEngineWire(t, source, "Root", `{"values":{"-3":4,"2":5}}`)
	files, err := Generate(target)
	if err != nil {
		t.Fatal(err)
	}
	const path = "probe/_schema/kind_map.py"
	const anchor = "if read_body(body, entry, ctx, key_field) and not widened:"
	if strings.Count(string(files[path]), anchor) != 1 {
		t.Fatal("missing once-per-map guard")
	}
	files[path] = []byte(strings.Replace(string(files[path]), anchor, "if read_body(body, entry, ctx, key_field):", 1))
	mapBlobProbe(t, files, `from probe import Root, LoadReport
from pathlib import Path
r=LoadReport();Root.load(Path("wire").read_bytes(),r)
assert r.widened==1,"map counted per key"
print("ok")
`, map[string][]byte{"wire": wire}, "map counted per key")
}
