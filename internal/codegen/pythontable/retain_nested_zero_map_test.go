package pythontable

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const retainMapZeroWire = "01010e5c0d031d020c06626f6d626572030d10040c06626f6d62657205049001000000001f020c0766696768746572030d11040c076669676874657205046400000000001b020c0573636f7574030d0f040c0573636f757405041e0000000000060e180d020a020807000000031102000a02080c00000003110200071102080e2d0d012a020c036b6974030e200d020e020602030d0709041600000000000e020609030d070904630000000000000a0e220d020f0203fdff030d070904fdffffff00000f02030200030d0709040200000000000b0c13010c10040c06736861726564050432000000000044ade113495c4a29ec105b36194ac93d0000000000000000861b638ebaadbcc4cfa98b28b5d4697f04348de9464c027b5d81b3a4c4a0df6370f0f5f0b3a14f2974a279448ee2e5b1cee3da5f6cdcd86dffffffffffffffff0d4fb1d1d25282750c00000000000000"

const retainMapZeroProbe = `from mapdemo import Fleet, TableRetain, LoadReport
with open('wire','rb') as f: wire=f.read()
keep=TableRetain(bytearray(1<<20),bytearray(24*4096)); report=LoadReport()
value=Fleet.load_retain(wire,keep,report)
assert (report.unknown,report.retained,report.retain_lost)==(8,6,2), 'nested-zero-retention'
saved=value.save_retain(keep,report)
assert value.measure_retain(keep)==len(saved) and report.retain_lost==2
with open('saved','wb') as f: f.write(saved)
print('nested-zero-map')
`

func TestPythonRetainMapNestedZeroHash(t *testing.T) {
	slowtest.Gate(t, "both pinned Python interpreters and Go retaining oracle")
	u := loadUnitPaths(t, "../../../tables/maps")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := hex.DecodeString(retainMapZeroWire)
	if err != nil {
		t.Fatal(err)
	}
	var python []byte
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + retainMapZeroProbe), "wire": wire})
		if err != nil || strings.TrimSpace(out) != "nested-zero-map" {
			t.Fatalf("Python map: %v\n%s\n%s", err, out, stderr)
		}
		got, err := os.ReadFile(filepath.Join(dir, "saved"))
		if err != nil {
			t.Fatal(err)
		}
		if python != nil && !bytes.Equal(python, got) {
			t.Fatal("pinned Python outputs differ")
		}
		python = got
	}
	m := tabletext.NewModel(u)
	value := m.New(m.Lookup("Fleet"))
	keep := tablewire.Retain{Capacity: 1 << 20, IdCapacity: 4096}
	var report tabletext.Report
	ok, err := tablewire.DecodeRegionRetain(m, value, wire, &keep, &report)
	if err != nil || !ok || report.Refused || report.Unknown != 8 || report.Retained != 6 || report.RetainLost != 2 {
		t.Fatalf("Go nested-zero retention: ok=%v err=%v report=%+v", ok, err, report)
	}
	var written tabletext.Report
	expected, err := tablewire.EncodeRetain(m, value, &keep, &written)
	if err != nil || !bytes.Equal(python, expected) {
		t.Fatalf("Go/Python retained bytes differ: %v, Python=%d Go=%d", err, len(python), len(expected))
	}
}

func TestPythonRetainMapNestedZeroHashNegativeControl(t *testing.T) {
	slowtest.Gate(t, "both pinned Python interpreters")
	u := loadUnitPaths(t, "../../../tables/maps")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	path := "mapdemo/_schema/retain.py"
	source := string(files[path])
	anchor := "    if identity in _RESERVED:\n"
	if strings.Count(source, anchor) != 1 {
		t.Fatal("nested-zero control anchor missing")
	}
	files[path] = []byte(strings.Replace(source, anchor, "    if identity == 0 or identity in _RESERVED:\n", 1))
	wire, err := hex.DecodeString(retainMapZeroWire)
	if err != nil {
		t.Fatal(err)
	}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		writeTree(t, dir, files)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(pyrun.ImportPrelude + retainMapZeroProbe), "wire": wire})
		if err == nil || !strings.Contains(stderr, "a retained reference names a reserved id") {
			t.Fatalf("nested-zero sabotage not detected: %v\n%s\n%s", err, out, stderr)
		}
	}
}
