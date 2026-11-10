package tablecook_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/gotable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func unitFromSource(t *testing.T, src string) (*ir.Unit, *tabletext.Model) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	c := compiler.New()
	u, err := c.Load([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	return u, tabletext.NewModel(u)
}

func TestUnboundedTextMinimalLayoutAndRoundTrip(t *testing.T) {
	u, m := unitFromSource(t, "package probe\ntable T {\n    a [..2]string\n}\n")
	root := m.Lookup("T")
	inst := m.New(root)
	var r tabletext.Report
	if !m.Read(inst, []byte(`{"a":["x"]}`), &r) || !r.Silent() {
		t.Fatalf("Read: %+v", r)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	for _, big := range []bool{false, true} {
		decoded := m.New(root)
		if ok, err := tablewire.Decode(m, decoded, wire, &r); !ok || err != nil {
			t.Fatalf("big=%v decode: %v %v", big, ok, err)
		}
		cooked, err := tablecook.Cook(m, decoded, tablecook.Options{Big: big})
		if err != nil {
			t.Fatalf("big=%v Cook: %v", big, err)
		}
		h, err := tablecook.ReadHeader(cooked, ir.BuildVersion(u))
		if err != nil {
			t.Fatalf("big=%v ReadHeader: %v", big, err)
		}
		if h.DataLength != 40 {
			t.Fatalf("big=%v DataLength = %d, want 40 (24 record + 16 blob)", big, h.DataLength)
		}
		data := h.Data(cooked)
		ord := h.Order()
		if got := int64(ord.Uint64(data[0:])); got != 24 {
			t.Fatalf("big=%v slot[0] delta = %d, want 24", big, got)
		}
		if got := int64(ord.Uint64(data[8:])); got != 0 {
			t.Fatalf("big=%v slot[1] delta = %d, want 0", big, got)
		}
		if got := int32(ord.Uint32(data[16:])); got != 1 {
			t.Fatalf("big=%v a_count = %d, want 1", big, got)
		}
		if got := ord.Uint32(data[24:]); got != 1 {
			t.Fatalf("big=%v blob length = %d, want 1", big, got)
		}
		if data[32] != byte(120) || data[33] != 0 {
			t.Fatalf("big=%v blob payload = %x, want 7800", big, data[32:34])
		}
		if _, err := tablecook.Check(m, cooked); err != nil {
			t.Fatalf("big=%v Check: %v", big, err)
		}
		back, err := tablecook.Uncook(m, root, cooked)
		if err != nil {
			t.Fatalf("big=%v Uncook: %v", big, err)
		}
		again, err := tablewire.Encode(m, back)
		if err != nil {
			t.Fatalf("big=%v Encode: %v", big, err)
		}
		if !bytes.Equal(wire, again) {
			t.Fatalf("big=%v wire mismatch:\ngot  %x\nwant %x", big, again, wire)
		}
	}
}

func TestUnboundedTextAllShapesAndGeneratedGoEquivalence(t *testing.T) {
	slowtest.Gate(t, "an external toolchain")

	const schema = "package probe\ntable Note {\n    lines []string\n}\ntable Where {\n    capped [..3]string\n    maybe_capped [..3]?string\n    open []string\n    maybe_open []?string\n    by_name map[string]int32\n    note *Note\n}\n"
	u, m := unitFromSource(t, schema)
	root := m.Lookup("Where")
	longA := strings.Repeat("prefix-", 40) + "alpha"
	longB := strings.Repeat("prefix-", 40) + "beta"
	js := `{"capped":["","x","` + longA + `"],` +
		`"maybe_capped":[null,"","y"],` +
		`"open":["hello","","` + longB + `"],` +
		`"maybe_open":[null,"world",""],` +
		`"by_name":{"":1,"` + longB + `":2,"` + longA + `":3}}`
	inst := m.New(root)
	var r tabletext.Report
	if !m.Read(inst, []byte(js), &r) || !r.Silent() {
		t.Fatalf("Read Where: %+v", r)
	}
	note := m.New(m.Lookup("Note"))
	if !m.Read(note, []byte(`{"lines":["child-one","","child-two"]}`), &r) || !r.Silent() {
		t.Fatalf("Read Note: %+v", r)
	}
	setNode(inst, "note", note)

	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}

	var cookedLE, cookedBE []byte
	for _, big := range []bool{false, true} {
		decoded := m.New(root)
		if ok, err := tablewire.Decode(m, decoded, wire, &r); !ok || err != nil {
			t.Fatalf("big=%v Decode: %v %v", big, ok, err)
		}
		cooked, err := tablecook.Cook(m, decoded, tablecook.Options{Big: big})
		if err != nil {
			t.Fatalf("big=%v Cook: %v", big, err)
		}
		if big {
			cookedBE = cooked
		} else {
			cookedLE = cooked
		}
		res, err := tablecook.Check(m, cooked)
		if err != nil {
			t.Fatalf("big=%v Check: %v", big, err)
		}
		if res.Nodes != 2 {
			t.Fatalf("big=%v Nodes = %d, want 2", big, res.Nodes)
		}
		back, err := tablecook.Uncook(m, root, cooked)
		if err != nil {
			t.Fatalf("big=%v Uncook: %v", big, err)
		}
		again, err := tablewire.Encode(m, back)
		if err != nil {
			t.Fatalf("big=%v Encode: %v", big, err)
		}
		if !bytes.Equal(wire, again) {
			t.Fatalf("big=%v round-trip wire mismatch", big)
		}
	}

	if os.Getenv("SCHEMA_SLOW") == "1" {
		dir := t.TempDir()
		goFiles, err := golang.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		tableFiles, err := gotable.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		for name, content := range goFiles {
			if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		for name, content := range tableFiles {
			if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module probe\ngo 1.24\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "wire.bin"), wire, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cooked_le.bin"), cookedLE, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cooked_be.bin"), cookedBE, 0o600); err != nil {
			t.Fatal(err)
		}
		driver := `package probe
import (
	"bytes"
	"os"
	"testing"
	"unsafe"
)
func TestGeneratedMatchesNeutral(t *testing.T) {
	wire, err := os.ReadFile("wire.bin")
	if err != nil { t.Fatal(err) }
	wantLE, err := os.ReadFile("cooked_le.bin")
	if err != nil { t.Fatal(err) }
	wantBE, err := os.ReadFile("cooked_be.bin")
	if err != nil { t.Fatal(err) }

	loadBytes := WhereLoadMeasure(wire)
	if loadBytes != int64(len(wantLE))-64 {
		t.Fatalf("WhereLoadMeasure = %d, want %d", loadBytes, len(wantLE)-64)
	}
	raw := make([]byte, loadBytes+63)
	off := (-uintptr(unsafe.Pointer(&raw[0]))) & 63
	region := raw[off : off+uintptr(loadBytes)]
	var r TableReport
	root := WhereLoad(region, wire, &r)
	if root == nil || r != (TableReport{}) {
		t.Fatalf("WhereLoad failed: root=%v report=%+v", root, r)
	}
	cookBytes := WhereCookMeasure(root)
	if cookBytes != int64(len(wantLE)) {
		t.Fatalf("WhereCookMeasure = %d, want %d", cookBytes, len(wantLE))
	}
	gotLE := make([]byte, cookBytes)
	if !WhereCookFrom(root, gotLE, TableByteOrderLittle) {
		t.Fatal("WhereCookFrom LE failed")
	}
	if !bytes.Equal(gotLE, wantLE) {
		t.Fatalf("generated LE cook differs from neutral tablecook.Cook")
	}
	gotBE := make([]byte, cookBytes)
	if !WhereCookFrom(root, gotBE, TableByteOrderBig) {
		t.Fatal("WhereCookFrom BE failed")
	}
	if !bytes.Equal(gotBE, wantBE) {
		t.Fatalf("generated BE cook differs from neutral tablecook.Cook")
	}
}
`
		if err := os.WriteFile(filepath.Join(dir, "match_test.go"), []byte(driver), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("go", "test", "-timeout=0", "-v", ".")
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("generated Go test failed: %v\n%s", err, out)
		}
	}
}

func TestUnboundedTextHostileControls(t *testing.T) {
	u, m := unitFromSource(t, "package probe\ntable T {\n    capped [..2]string\n    maybe [..2]?string\n    by_name map[string]int32\n}\n")
	root := m.Lookup("T")
	inst := m.New(root)
	var r tabletext.Report
	if !m.Read(inst, []byte(`{"capped":["alpha","beta"],"maybe":[null,"gamma"],"by_name":{"aa":1,"bb":2}}`), &r) || !r.Silent() {
		t.Fatalf("Read: %+v", r)
	}
	valid, err := tablecook.Cook(m, inst, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	h, err := tablecook.ReadHeader(valid, ir.BuildVersion(u))
	if err != nil {
		t.Fatal(err)
	}
	dataOff := h.DataOffset()
	le := binary.LittleEndian

	mustRefuse := func(name string, mutate func(b []byte)) {
		t.Helper()
		bad := append([]byte(nil), valid...)
		mutate(bad)
		if _, err := tablecook.Check(m, bad); err == nil {
			t.Errorf("%s: Check accepted forged cook", name)
		}
		if _, err := tablecook.Uncook(m, root, bad); err == nil && name != "descending_map_keys" && name != "overlapping_blobs" {
			t.Errorf("%s: Uncook accepted forged cook", name)
		}
	}

	mustRefuse("misaligned_blob_delta", func(b []byte) {
		delta := le.Uint64(b[dataOff:])
		le.PutUint64(b[dataOff:], delta+1)
	})
	mustRefuse("blob_delta_past_extent", func(b []byte) {
		le.PutUint64(b[dataOff:], uint64(h.DataLength))
	})
	mustRefuse("non_null_zero_length_blob", func(b []byte) {
		delta := int64(le.Uint64(b[dataOff:]))
		le.PutUint32(b[dataOff+delta:], 0)
	})
	mustRefuse("blob_length_past_extent", func(b []byte) {
		delta := int64(le.Uint64(b[dataOff:]))
		le.PutUint32(b[dataOff+delta:], uint32(h.DataLength))
	})
	mustRefuse("missing_nul_terminator", func(b []byte) {
		delta := int64(le.Uint64(b[dataOff:]))
		b[dataOff+delta+8+5] = 33
	})
	mustRefuse("overlapping_blobs", func(b []byte) {
		delta0 := int64(le.Uint64(b[dataOff:]))
		le.PutUint64(b[dataOff+8:], uint64(delta0-8))
	})
	mustRefuse("absent_optional_non_null", func(b []byte) {
		ml := ir.RecordLayout(u, root)
		maybeOff := ml.FieldByName("maybe").Offset
		delta0 := int64(le.Uint64(b[dataOff:]))
		le.PutUint64(b[dataOff+maybeOff:], uint64(delta0-maybeOff))
	})
	mustRefuse("descending_map_keys", func(b []byte) {
		ml := ir.RecordLayout(u, root)
		mapOff := ml.FieldByName("by_name").Offset
		entryArr := mapOff + int64(le.Uint64(b[dataOff+mapOff:]))
		entrySize := ir.RecordLayout(u, ml.FieldByName("by_name").Field.MapEntry).Size
		k0 := int64(le.Uint64(b[dataOff+entryArr:]))
		k1 := int64(le.Uint64(b[dataOff+entryArr+entrySize:]))
		blob0 := entryArr + k0
		blob1 := entryArr + entrySize + k1
		le.PutUint64(b[dataOff+entryArr:], uint64(blob1-entryArr))
		le.PutUint64(b[dataOff+entryArr+entrySize:], uint64(blob0-(entryArr+entrySize)))
	})

	badInst := m.New(root)
	if !m.Read(badInst, []byte(`{"capped":["alpha","beta"]}`), &r) {
		t.Fatal(r)
	}
	field(badInst, "capped").Count = 1
	if _, err := tablecook.Cook(m, badInst, tablecook.Options{}); err == nil {
		t.Fatal("Cook accepted unreached slot past live count holding non-empty unbounded text")
	}
}

func TestAbsentOptionalMapAndContainerCookRefusal(t *testing.T) {
	_, m := unitFromSource(t, "package probe\ntable Inner {\n    items []int32\n}\ntable Holder {\n    on bool\n    scalar_map ?map[int32]int32\n    text_map ?map[string]int32\n    nested ?Inner\n    if on {\n        guarded_map map[int32]int32\n    }\n}\n")
	root := m.Lookup("Holder")
	var r tabletext.Report

	for _, big := range []bool{false, true} {
		// 1. Absent optional scalar map with non-empty backing entries must be refused by Cook in both byte orders, while wire and JSON omit it.
		inst := m.New(root)
		if !m.Read(inst, []byte(`{"scalar_map":{"1":10,"2":20}}`), &r) || !r.Silent() {
			t.Fatalf("Read scalar_map: %+v", r)
		}
		sm := field(inst, "scalar_map")
		sm.Present = false
		if len(sm.Entries) != 2 {
			t.Fatalf("expected 2 backing entries, got %d", len(sm.Entries))
		}
		if _, err := tablecook.Cook(m, inst, tablecook.Options{Big: big}); err == nil {
			t.Fatalf("big=%v: Cook accepted absent optional scalar_map with %d entries", big, len(sm.Entries))
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("big=%v: wire Encode failed on absent optional scalar_map: %v", big, err)
		}
		decoded := m.New(root)
		if ok, err := tablewire.Decode(m, decoded, wire, &r); !ok || err != nil {
			t.Fatalf("big=%v: wire Decode failed: %v %v", big, ok, err)
		}
		if field(decoded, "scalar_map").Present || len(field(decoded, "scalar_map").Entries) != 0 {
			t.Fatalf("big=%v: wire did not omit absent optional scalar_map", big)
		}
		// Once backing entries are cleared, Cook succeeds and reserves 0 extent bytes.
		sm.Entries = nil
		cooked, err := tablecook.Cook(m, inst, tablecook.Options{Big: big})
		if err != nil {
			t.Fatalf("big=%v: Cook rejected cleared absent optional scalar_map: %v", big, err)
		}
		if _, err := tablecook.Check(m, cooked); err != nil {
			t.Fatalf("big=%v: Check failed on cleared absent optional scalar_map: %v", big, err)
		}

		// 2. Absent optional text-keyed map with non-empty backing entries must be refused by Cook.
		inst2 := m.New(root)
		if !m.Read(inst2, []byte(`{"text_map":{"alpha":1}}`), &r) || !r.Silent() {
			t.Fatalf("Read text_map: %+v", r)
		}
		tm := field(inst2, "text_map")
		tm.Present = false
		if _, err := tablecook.Cook(m, inst2, tablecook.Options{Big: big}); err == nil {
			t.Fatalf("big=%v: Cook accepted absent optional text_map with %d entries", big, len(tm.Entries))
		}

		// 3. Absent optional nested table holding a non-empty list must be refused by Cook.
		inst3 := m.New(root)
		if !m.Read(inst3, []byte(`{"nested":{"items":[7,8]}}`), &r) || !r.Silent() {
			t.Fatalf("Read nested: %+v", r)
		}
		field(inst3, "nested").Present = false
		if _, err := tablecook.Cook(m, inst3, tablecook.Options{Big: big}); err == nil {
			t.Fatalf("big=%v: Cook accepted absent optional nested table holding non-empty list", big)
		}

		// 4. Guarded-off map with non-empty backing entries must be refused by Cook.
		inst4 := m.New(root)
		if !m.Read(inst4, []byte(`{"on":true,"guarded_map":{"5":50}}`), &r) || !r.Silent() {
			t.Fatalf("Read guarded_map: %+v", r)
		}
		field(inst4, "on").Cell.B = false
		if _, err := tablecook.Cook(m, inst4, tablecook.Options{Big: big}); err == nil {
			t.Fatalf("big=%v: Cook accepted guarded-off map with non-empty entries", big)
		}
	}
}
