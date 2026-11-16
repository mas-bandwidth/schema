package pythontable

import (
	"bytes"
	"maps"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func mapBlobProbe(t *testing.T, files map[string][]byte, probe string, inputs map[string][]byte, wantFailure string) {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, files)
	inputs["main.py"] = []byte(pyrun.ImportPrelude + probe)
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", inputs)
		if wantFailure != "" {
			if err == nil || !strings.Contains(stderr, wantFailure) {
				t.Errorf("control failed to detect %s: %v %s %s", wantFailure, err, out, stderr)
			}
		} else if err != nil || strings.TrimSpace(out) != "ok" {
			t.Errorf("probe %s: %v %s %s", pin, err, out, stderr)
		}
	}
}

func TestPythonMapBlobManifest(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and mypy")
	u := loadUnitPaths(t, "../../../tables/maps")
	wire, err := os.ReadFile("../../../testdata/wire/tables/map_chunks.bin")
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile("../../../testdata/conformance/tables/json/map_chunks.json")
	if err != nil {
		t.Fatal(err)
	}
	// This is the actual C++ manifest artifact, independently matched by Go.
	if got := pythonVTMapEngineWire(t, u, "Chunks", string(text)); !bytes.Equal(got, wire) {
		t.Fatal("Go/C++ map_chunks oracle mismatch")
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	for _, module := range []string{"mapdemo", "mapdemo/_schema"} {
		if out, err := pyrun.Analyze(t, dir, module); err != nil {
			t.Fatalf("mypy %s: %v %s", module, err, out)
		}
	}
	bad := append([]byte(nil), wire...)
	if len(bad) < 27 || bad[25] != 17 || bad[26] != 3 {
		t.Fatal("C++ pointer witness changed")
	}
	bad[26] = 2
	model := tabletext.NewModel(u)
	var report tabletext.Report
	if ok, err := tablewire.Decode(model, model.New(model.Lookup("Chunks")), bad, &report); ok || tablewire.RefusalReason(err) != tablewire.ReasonBlobShared {
		t.Fatalf("Go shared-blob oracle: %v %+v", err, report)
	}
	mapBlobProbe(t, files, `from mapdemo import Chunks, LoadReport, Refused
from pathlib import Path
wire=Path("wire").read_bytes()
r=LoadReport();v=Chunks.load(wire,r)
assert v.blobs=={-3:bytes.fromhex("deadbeef"),2:bytes.fromhex("000180ff")} and v.past==8
assert (r.unknown,r.widened,r.retained,r.retain_lost)==(0,0,0,0)
assert v.save()==wire and v.measure()==len(wire)
r=LoadReport();r.unknown=12;r.widened=13
try: Chunks.load(Path("bad").read_bytes(),r)
except Refused as e: assert e.reason=="blob_shared"
else: raise AssertionError("shared wire blob was accepted")
assert (r.unknown,r.widened)==(12,13)
print("ok")
`, map[string][]byte{"wire": wire, "bad": bad}, "")
}

func TestPythonMapBlobSlots(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and mypy")
	u := loadUnit(t, `package probe
 table Child { blobs map[int32]*bytes }
 table Root {
  blobs map[int32]*bytes
  words map[string(8)]*string
  direct *bytes
  p *Child
  q *Child
 }
`)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	probe := `from probe import BlobRef, Child, InvalidValue, Root
r=Root();r.blobs={3:None,2:b"",-1:bytes([0,255])};r.words={"b":None,"a":"","c":"hello"}
wire=r.save();assert r.measure()==len(wire)
v=Root.load(wire);assert v.blobs==r.blobs and v.words==r.words and v.save()==wire
x=BlobRef(b"same")
for mode in ("map-map","map-direct","map-child"):
 r=Root();r.blobs={1:x}
 if mode=="map-map":r.blobs[2]=x
 elif mode=="map-direct":r.direct=x
 else:r.p=Child();r.p.blobs={1:x}
 try:r.save()
 except InvalidValue as e:assert "blob_shared" in e.rule
 else:raise AssertionError("shared map BlobRef was accepted")
r=Root();r.blobs={1:BlobRef(b"same"),2:BlobRef(b"same")};r.save()
r=Root();r.blobs={1:b"same",2:b"same"};r.save()
r=Root();c=Child();c.blobs={1:x};r.p=c;r.q=c;r.save()
r=Root();r.words={"a":BlobRef("same"),"b":BlobRef("same")};r.save()
for value in (3,"wrong",BlobRef("wrong")):
 r=Root();r.blobs={1:value}
 try:r.save()
 except InvalidValue:pass
 else:raise AssertionError("wrong map blob payload was accepted")
r=Root();r.words={"a":"bad\x00text"}
try:r.save()
except InvalidValue:pass
else:raise AssertionError("zero text blob was accepted")
print("ok")
`
	mapBlobProbe(t, files, probe, map[string][]byte{}, "")
	t.Run("map-sharing-negative-control", func(t *testing.T) {
		planted := maps.Clone(files)
		const path = "probe/_schema/save.py"
		const from = "if identity in ctx.blob_refs:\n                        raise InvalidValue(_render(where), \"blob_shared\")"
		if strings.Count(string(planted[path]), from) != 1 {
			t.Fatal("missing map-sharing guard")
		}
		planted[path] = []byte(strings.Replace(string(planted[path]), from, strings.Replace(from, "if identity in ctx.blob_refs:", "if False:", 1), 1))
		mapBlobProbe(t, planted, probe, map[string][]byte{}, "shared map BlobRef was accepted")
	})
	dir := t.TempDir()
	writeTree(t, dir, files)
	for _, module := range []string{"probe", "probe/_schema"} {
		if out, err := pyrun.Analyze(t, dir, module); err != nil {
			t.Fatalf("mypy %s: %v %s", module, err, out)
		}
	}
}
