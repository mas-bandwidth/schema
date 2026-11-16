package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestOrphanCycleVectorPinned builds a cyclic node directory with no root
// pointer. An absent pointer is the canonical spelling of null.
func TestOrphanCycleVectorPinned(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, p := range m.Units[i].Paths {
			if !filepath.IsAbs(p) {
				m.Units[i].Paths[j] = filepath.Join(root, p)
			}
		}
	}
	v := framingVector{
		unit: "graphdemo", root: "Scene",
		ids:  []string{"node-table", "type:ListNode", "next"},
		body: b(1, 12, 10, 2, 2, 4, 3, 17, 2, 0, 2, 1, 0, 0),
	}
	want, err := v.bytes(newUnits(m))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "testdata/wire/tables/fuzz-vectors/pointer_orphan_cycle.bin")
	if os.Getenv("FRAMING_WRITE") == "1" {
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("orphan cycle vector differs from its canonical generator")
	}
}

type blobWitness struct {
	Name         string
	Wire         string
	Saved        string
	Shared       int
	KindMismatch int
}

// TestBlobNullWitnessPins builds the null cases with absent fields. Shared
// blob identities still occur in the partially populated case.
func TestBlobNullWitnessPins(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	path := filepath.Join(root, "testdata/conformance/feature-witnesses/blob-shared-report/cases.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cases []blobWitness
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, p := range m.Units[i].Paths {
			if !filepath.IsAbs(p) {
				m.Units[i].Paths[j] = filepath.Join(root, p)
			}
		}
	}
	units := newUnits(m)
	vectors := map[string]framingVector{
		"null_not_named": {ids: []string{"b", "c", "d", "node-table", "string", "bytes"}, body: b(1, 17, 2, 2, 17, 3, 3, 17, 3, 4, 12, 5, 2, 5, 0, 6, 0, 0)},
		"all_null":       {body: b(0)},
	}
	seen := map[string]bool{}
	for i := range cases {
		c := &cases[i]
		v, ok := vectors[c.Name]
		if !ok {
			continue
		}
		seen[c.Name] = true
		if c.KindMismatch != 0 || (c.Name == "null_not_named" && (c.Shared != 1 || c.Saved != "")) || (c.Name == "all_null" && (c.Shared != 0 || c.Saved != "01000000000000000000")) {
			t.Errorf("%s: witness expectations changed", c.Name)
		}
		wire, err := v.bytes(units)
		if err != nil {
			t.Fatal(err)
		}
		want := hex.EncodeToString(wire)
		if os.Getenv("FRAMING_WRITE") == "1" {
			c.Wire = want
		} else if c.Wire != want {
			t.Errorf("%s: witness differs from canonical null generator", c.Name)
		}
	}
	for name := range vectors {
		if !seen[name] {
			t.Errorf("missing witness %s", name)
		}
	}
	if os.Getenv("FRAMING_WRITE") == "1" {
		out, err := json.MarshalIndent(cases, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// graphChainPastCap spells the 129-node JSON depth boundary, with the final
// node's zero value elided. Every named node and edge is still present.
func graphChainPastCap() framingVector {
	var records [][]byte
	for i := 1; i <= 129; i++ {
		var body []byte
		if i < 129 {
			body = cat(b(6, 4), i32(int32(i)))
		}
		name := []byte("n" + strconv.Itoa(i))
		body = cat(body, b(1, 12), leb(uint64(len(name))), name)
		if i < 129 {
			body = cat(body, b(7, 17), leb(uint64(i+2)))
		}
		body = append(body, 0)
		records = append(records, cat(b(5), leb(uint64(len(body))), body))
	}
	return framingVector{unit: "graphdemo", root: "Scene", ids: []string{"name", "head", "alias", "node-table", "type:ListNode", "value", "next"},
		body: cat(b(1, 12, 5), []byte("chain"), b(2, 17, 2, 3, 17, 65), nodeTable(129, records...), b(0))}
}

func TestGraphChainPastCapPinned(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, p := range m.Units[i].Paths {
			if !filepath.IsAbs(p) {
				m.Units[i].Paths[j] = filepath.Join(root, p)
			}
		}
	}
	want, err := graphChainPastCap().bytes(newUnits(m))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "testdata/wire/tables/graph_chain_past_cap.bin")
	if os.Getenv("FRAMING_WRITE") == "1" {
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("past-cap graph differs from its canonical generator")
	}
	u, err := newUnits(m).get("graphdemo")
	if err != nil {
		t.Fatal(err)
	}
	model := tabletext.NewModel(u)
	inst := model.New(model.Lookup("Scene"))
	var report tabletext.Report
	ok, err := tablewire.Decode(model, inst, got, &report)
	if err != nil || !ok || report.Refused {
		t.Fatalf("past-cap graph read: ok=%v err=%v report=%+v", ok, err, report)
	}
	if _, err := model.Write(inst); err == nil {
		t.Error("129-node graph JSON write unexpectedly succeeded")
	}
}

// TestRetainingFuzzCanonicalPins preserves the original mutations while
// numbering their ID tables by first use. The unknown IDs still name the
// same fields/enum or union arm as in the original fuzz discoveries.
func TestRetainingFuzzCanonicalPins(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	cases := []struct{ name, seed string }{
		{"retain_declared_default", "graph_tree"},
		{"retain_byte_blob_reachability", "blob_small"},
		{"retain_enum_list_unknown", "list_mixed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seed, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/"+tc.seed+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			wire := bytes.Clone(seed)
			f := frameWire(wire)
			if len(f.entries) == 0 || len(f.entries) >= 128 {
				t.Fatal("unexpected seed ID table")
			}
			if tc.name == "retain_enum_list_unknown" {
				found := false
				for _, sp := range f.spots {
					if sp.kind == spotVariant {
						if sp.width != 1 || sp.value != 2 {
							t.Fatal("unexpected first enum variant")
						}
						wire[sp.off] = byte(len(f.entries))
						found = true
						break
					}
				}
				if !found {
					t.Fatal("seed has no enum variant")
				}
			} else {
				if len(wire) < 2 || wire[1] != 1 {
					t.Fatal("unexpected root field")
				}
				wire[1] = byte(len(f.entries))
			}
			wire, err = firstUseOrder(wire)
			if err != nil {
				t.Fatal(err)
			}
			if fault := framingTableFault(framingVector{}, wire); fault != "" {
				t.Fatal(fault)
			}
			path := filepath.Join(root, "testdata/wire/tables/fuzz-vectors/"+tc.name+".bin")
			if os.Getenv("FRAMING_WRITE") == "1" {
				if err := os.WriteFile(path, wire, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, wire) {
				t.Error("retention vector differs from canonical generator")
			}
		})
	}
	t.Run("retain_union_arm_frame", func(t *testing.T) {
		// Feed.id = 7, followed by an unknown union field carrying an unknown
		// uint32 arm (0x12345678). The same unknown ID names field and arm.
		v := framingVector{entries: []uint64{0x08b72e07b55c3ac0, 0x56d7ab194448a4f3}, body: b(1, 8, 7, 0, 0, 0, 2, 15, 2, 8, 4, 0x78, 0x56, 0x34, 0x12, 0)}
		wire, err := v.bytes(nil)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "testdata/wire/tables/fuzz-vectors/retain_union_arm_frame.bin")
		if os.Getenv("FRAMING_WRITE") == "1" {
			if err := os.WriteFile(path, wire, 0o644); err != nil {
				t.Fatal(err)
			}
			return
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, wire) {
			t.Error("union retention vector differs from canonical generator")
		}
	})
}

// TestNestedArmTrailingPins builds the variable-form envelope pair with
// declaration-ordered fields. The extra byte is either inside the union arm
// frame or after the root terminator.
func TestNestedArmTrailingPins(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	for _, tc := range []struct {
		name  string
		place string
	}{{"nested_arm_trailing", "inside"}, {"nested_arm_trailing_top", "outside"}, {"nested_arm_exact", "none"}} {
		t.Run(tc.name, func(t *testing.T) {
			arm := cat(b(4, 8, 4), i32(5))
			if tc.place == "inside" {
				arm = append(arm, 0xee)
			}
			body := cat(b(1, 8), i32(41), b(2, 15, 3, 15), leb(uint64(len(arm))), arm, b(0))
			if tc.place == "outside" {
				body = append(body, 0xee)
			}
			v := framingVector{unit: "messagedemo", ids: []string{"sequence", "body", "origin", "pid"}, body: body}
			wire, err := v.bytes(nil)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "testdata/wire/tables", tc.name+".bin")
			if os.Getenv("FRAMING_WRITE") == "1" {
				if err := os.WriteFile(path, wire, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, wire) {
				t.Errorf("%s pin differs from its builder", tc.name)
			}
		})
	}
}

// TestNestedArmEnvelopeBoundary checks the exact frame and both trailing-byte
// refusals independently for spelling and through the reader.
func TestNestedArmEnvelopeBoundary(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, p := range m.Units[i].Paths {
			if !filepath.IsAbs(p) {
				m.Units[i].Paths[j] = filepath.Join(root, p)
			}
		}
	}
	units := newUnits(m)
	u, err := units.get("messagedemo")
	if err != nil {
		t.Fatal(err)
	}
	st := u.Tables["ToolEnvelope"]
	if st == nil {
		t.Fatal("missing ToolEnvelope")
	}
	for _, tc := range []struct {
		name string
		read bool
	}{{"nested_arm_exact", true}, {"nested_arm_trailing", false}, {"nested_arm_trailing_top", false}} {
		b, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables", tc.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		if fault := framingTableFault(framingVector{}, b); fault != "" {
			t.Errorf("%s: %s", tc.name, fault)
		}
		for _, shapes := range []map[string]bool{rootStructuralSpellings(st, b), rootStoredDefaults(st, b), nestedStructuralSpellings(u, st, b), nestedStoredDefaults(u, st, b)} {
			if len(shapes) > 0 {
				t.Errorf("%s: %v", tc.name, shapes)
			}
		}
		wr, err := newWireRoot(units, "messagedemo", "ToolEnvelope", false, false)
		if err != nil {
			t.Fatal(err)
		}
		var report tabletext.Report
		ok, err := tablewire.Decode(wr.model, wr.model.New(wr.def), b, &report)
		if ok != tc.read || (!tc.read && (err == nil || !strings.Contains(err.Error(), "wire_damaged"))) {
			t.Errorf("%s: read=%v err=%v", tc.name, ok, err)
		}
	}
}

// TestRetainedArrayDuplicatePins isolates duplicate root IDs and pairs them
// with the same single array holding an unknown field for retaining reads.
func TestRetainedArrayDuplicatePins(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	// One Squad table element with a future uint32 field and its terminator.
	element := cat(b(2, 8), i32(9), b(0))
	payload := cat(b(13, 1), leb(uint64(len(element))), element)
	field := cat(b(1, 14), leb(uint64(len(payload))), payload)
	for _, tc := range []struct {
		name      string
		duplicate bool
	}{{"retain_array_replaced", true}, {"retain_array_unique", false}} {
		t.Run(tc.name, func(t *testing.T) {
			body := bytes.Clone(field)
			if tc.duplicate {
				body = append(body, field...)
			}
			body = append(body, 0)
			wire, err := (framingVector{ids: []string{"many", "future"}, body: body}).bytes(nil)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "testdata/wire/tables/fuzz-vectors", tc.name+".bin")
			if os.Getenv("FRAMING_WRITE") == "1" {
				if err := os.WriteFile(path, wire, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, wire) {
				t.Errorf("%s differs from its builder", tc.name)
			}
		})
	}
}

func TestRetainedArrayDuplicateBoundary(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, p := range m.Units[i].Paths {
			if !filepath.IsAbs(p) {
				m.Units[i].Paths[j] = filepath.Join(root, p)
			}
		}
	}
	units := newUnits(m)
	u, err := units.get("mapdemo")
	if err != nil {
		t.Fatal(err)
	}
	st := u.Tables["Depth"]
	for _, tc := range []struct {
		name      string
		duplicate bool
	}{{"retain_array_replaced", true}, {"retain_array_unique", false}} {
		wire, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/fuzz-vectors", tc.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		if fault := framingTableFault(framingVector{}, wire); fault != "" {
			t.Errorf("%s: %s", tc.name, fault)
		}
		for _, shapes := range []map[string]bool{rootStructuralSpellings(st, wire), rootStoredDefaults(st, wire), nestedStructuralSpellings(u, st, wire), nestedStoredDefaults(u, st, wire)} {
			if len(shapes) > 0 {
				t.Errorf("%s: %v", tc.name, shapes)
			}
		}
		wr, err := newWireRoot(units, "mapdemo", "Depth", false, true)
		if err != nil {
			t.Fatal(err)
		}
		value := wr.model.New(wr.def)
		var report tabletext.Report
		store := tablewire.Retain{Capacity: 65536, IdCapacity: 1024}
		ok, err := tablewire.DecodeRetain(wr.model, value, wire, &store, &report)
		if tc.duplicate {
			if ok || err == nil || !strings.Contains(err.Error(), "the same field id twice") {
				t.Errorf("%s: %v %v", tc.name, ok, err)
			}
			continue
		}
		if !ok || err != nil || report.Unknown != 1 || report.Retained != 1 {
			t.Fatalf("%s: %v %v %+v", tc.name, ok, err, report)
		}
		var savedReport tabletext.Report
		saved, err := tablewire.EncodeRetain(wr.model, value, &store, &savedReport)
		if err != nil || savedReport.RetainLost != 0 || !bytes.Equal(saved, wire) {
			t.Errorf("%s: retaining fixed point %v %+v", tc.name, err, savedReport)
		}
	}
}

// TestBlobDuplicateOwnerPin isolates repeated pointer fields without spelling
// an explicit null alongside the duplicate-ID refusal.
func TestBlobDuplicateOwnerPin(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	wire, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/blob_small.bin"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		index byte
	}{{"thumb", 3}, {"head", 2}} {
		f := frameWire(wire)
		slot := 0
		for i, id := range f.entries {
			if id == ir.TableWireId(tc.name) {
				slot = i + 1
			}
		}
		if slot == 0 || slot >= 128 {
			t.Fatalf("missing one-byte %s ID", tc.name)
		}
		at := bytes.Index(wire[:f.entriesAt], []byte{byte(slot), 17})
		if at < 0 {
			t.Fatalf("missing %s pointer", tc.name)
		}
		end := at + 3
		if end > f.entriesAt || wire[at+2] >= 128 {
			t.Fatalf("unexpected %s pointer width", tc.name)
		}
		next := append([]byte(nil), wire[:end]...)
		next = append(next, byte(slot), 17, tc.index)
		wire = append(next, wire[end:]...)
	}
	wire, err = firstUseOrder(wire)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "testdata/wire/tables/fuzz-vectors/blob_table_owner_replaced.bin")
	if os.Getenv("FRAMING_WRITE") == "1" {
		if err := os.WriteFile(path, wire, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, wire) {
		t.Fatal("duplicate pointer fixture differs from its builder")
	}
}

func TestBlobDuplicateOwnerBoundary(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, p := range m.Units[i].Paths {
			if !filepath.IsAbs(p) {
				m.Units[i].Paths[j] = filepath.Join(root, p)
			}
		}
	}
	units := newUnits(m)
	u, err := units.get("blobdemo")
	if err != nil {
		t.Fatal(err)
	}
	st := u.Tables["Catalog"]
	wire, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/fuzz-vectors/blob_table_owner_replaced.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if fault := framingTableFault(framingVector{}, wire); fault != "" {
		t.Error(fault)
	}
	for _, shapes := range []map[string]bool{rootStructuralSpellings(st, wire), rootStoredDefaults(st, wire), nestedStructuralSpellings(u, st, wire), nestedStoredDefaults(u, st, wire)} {
		if len(shapes) > 0 {
			t.Errorf("duplicate pointer fixture: %v", shapes)
		}
	}
	wr, err := newWireRoot(units, "blobdemo", "Catalog", false, true)
	if err != nil {
		t.Fatal(err)
	}
	var report tabletext.Report
	store := tablewire.Retain{Capacity: 65536, IdCapacity: 1024}
	ok, err := tablewire.DecodeRetain(wr.model, wr.model.New(wr.def), wire, &store, &report)
	if ok || err == nil || !strings.Contains(err.Error(), "the same field id twice") {
		t.Fatalf("duplicate pointer: ok=%v err=%v report=%+v", ok, err, report)
	}
}

// TestNegativeZeroListPin keeps the original signed-zero elision premise in a
// complete canonical frame: -0 is distinct from Placement.x's +0 default.
func TestNegativeZeroListPin(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	element := cat(b(2, 10), i32(-2147483648), b(0))
	payload := cat(b(13, 1), leb(uint64(len(element))), element)
	v := framingVector{ids: []string{"placements", "x"}, body: cat(b(1, 14), leb(uint64(len(payload))), payload, b(0))}
	wire, err := v.bytes(nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "testdata/wire/tables/fuzz-vectors/list_float_minus_zero_default.bin")
	if os.Getenv("FRAMING_WRITE") == "1" {
		if err := os.WriteFile(path, wire, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, wire) {
		t.Fatal("negative-zero vector differs from its builder")
	}
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, p := range m.Units[i].Paths {
			if !filepath.IsAbs(p) {
				m.Units[i].Paths[j] = filepath.Join(root, p)
			}
		}
	}
	units := newUnits(m)
	u, err := units.get("listdemo")
	if err != nil {
		t.Fatal(err)
	}
	st := u.Tables["Save"]
	if fault := framingTableFault(framingVector{}, wire); fault != "" {
		t.Error(fault)
	}
	for _, shapes := range []map[string]bool{rootStructuralSpellings(st, wire), rootStoredDefaults(st, wire), nestedStructuralSpellings(u, st, wire), nestedStoredDefaults(u, st, wire)} {
		if len(shapes) > 0 {
			t.Errorf("negative-zero vector: %v", shapes)
		}
	}
	wr, err := newWireRoot(units, "listdemo", "Save", false, false)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := wr.oracle(wire)
	if err != nil || !answer.accepted || !bytes.Equal(answer.encoded, wire) {
		t.Fatalf("negative-zero fixed point: accepted=%v err=%v got=%x", answer.accepted, err, answer.encoded)
	}
}

// TestListDamagedChildPins separates a duplicate field in one element from a
// non-finite float in a later element, keeping a valid sibling in each case.
func TestListDamagedChildPins(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	value := func(bits int32) []byte { return cat(b(2, 10), i32(bits)) }
	duplicate := cat(value(0x3f800000), value(0x40000000), b(0))
	finite := cat(value(0x40000000), b(0))
	nan := cat(value(0x7fc00000), b(0))
	for _, tc := range []struct {
		name          string
		first, second []byte
	}{{"list_damaged_child_reads_on", duplicate, finite}, {"list_nan_child", finite, nan}} {
		t.Run(tc.name, func(t *testing.T) {
			payload := cat(b(13, 2), leb(uint64(len(tc.first))), tc.first, leb(uint64(len(tc.second))), tc.second)
			v := framingVector{ids: []string{"placements", "x"}, body: cat(b(1, 14), leb(uint64(len(payload))), payload, b(0))}
			wire, err := v.bytes(nil)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "testdata/wire/tables/fuzz-vectors", tc.name+".bin")
			if os.Getenv("FRAMING_WRITE") == "1" {
				if err := os.WriteFile(path, wire, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, wire) {
				t.Fatal("child fixture differs from its builder")
			}
		})
	}
}

func TestListDamagedChildBoundary(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, p := range m.Units[i].Paths {
			if !filepath.IsAbs(p) {
				m.Units[i].Paths[j] = filepath.Join(root, p)
			}
		}
	}
	units := newUnits(m)
	u, err := units.get("listdemo")
	if err != nil {
		t.Fatal(err)
	}
	st := u.Tables["Save"]
	for _, tc := range []struct{ name, reason string }{{"list_damaged_child_reads_on", "the same field id twice"}, {"list_nan_child", "non_finite_float"}} {
		wire, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/fuzz-vectors", tc.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		if fault := framingTableFault(framingVector{}, wire); fault != "" {
			t.Errorf("%s: %s", tc.name, fault)
		}
		for _, shapes := range []map[string]bool{rootStructuralSpellings(st, wire), rootStoredDefaults(st, wire), nestedStructuralSpellings(u, st, wire), nestedStoredDefaults(u, st, wire)} {
			if len(shapes) > 0 {
				t.Errorf("%s: %v", tc.name, shapes)
			}
		}
		wr, err := newWireRoot(units, "listdemo", "Save", false, true)
		if err != nil {
			t.Fatal(err)
		}
		var report tabletext.Report
		store := tablewire.Retain{Capacity: 65536, IdCapacity: 1024}
		ok, err := tablewire.DecodeRetain(wr.model, wr.model.New(wr.def), wire, &store, &report)
		if ok || err == nil || !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("%s: ok=%v err=%v report=%+v", tc.name, ok, err, report)
		}
	}
}

// TestUnknownArmTextPin retains the unknown arm's embedded NUL while removing
// the ID that belonged only to the seed's original table-valued arm.
func TestUnknownArmTextPin(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	v := framingVector{entries: []uint64{0x08b32e07b55c3ac0, 0xd8d4335628b35226, 0x3a506cb541761960}, body: b(1, 8, 4, 0, 8, 0, 2, 15, 3, 12, 7, 4, 12, 'C', 'h', 'd', 'r', 0, 0)}
	wire, err := v.bytes(nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "testdata/wire/tables/fuzz-vectors/unknown_arm_text_nul.bin")
	if os.Getenv("FRAMING_WRITE") == "1" {
		if err := os.WriteFile(path, wire, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, wire) {
		t.Fatal("unknown text arm differs from its builder")
	}
	if fault := framingTableFault(framingVector{}, wire); fault != "" {
		t.Error(fault)
	}
}

// TestMapDepthRootKindPin gives the first table-valued field the scalar
// field's ID, then omits the old scalar occurrence. This isolates the intended
// kind mismatch without a duplicate field or unused/out-of-order ID entries.
func TestMapDepthRootKindPin(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	wire, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/map_depth.bin"))
	if err != nil {
		t.Fatal(err)
	}
	f := frameWire(wire)
	if len(f.entries) == 0 || len(f.entries) >= 128 || len(wire) < 2 || wire[1] != 1 {
		t.Fatal("unexpected map seed")
	}
	target := uint64(len(f.entries))
	found := false
	for _, field := range f.fields {
		if len(field.enclosing) != 0 {
			continue
		}
		ref, _ := readLeb(wire, field.start, f.bodyEnd)
		if ref == target {
			wire = append(bytes.Clone(wire[:field.start]), wire[field.end:]...)
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing original scalar field")
	}
	wire[1] = byte(target)
	wire, err = firstUseOrder(wire)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "testdata/wire/tables/fuzz-vectors/map_depth_root_kind.bin")
	if os.Getenv("FRAMING_WRITE") == "1" {
		if err := os.WriteFile(path, wire, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, wire) {
		t.Fatal("root kind mismatch differs from its builder")
	}
	if fault := framingTableFault(framingVector{}, wire); fault != "" {
		t.Error(fault)
	}
}

// TestVariableArmKindPins gives an existing variable Feed a known uint32 arm
// under three foreign kinds and under a short uint32 frame. Its id and parts
// siblings are intact so whole-load refusal can be checked independently.
func TestVariableArmKindPins(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	for _, tc := range []struct {
		name    string
		kind    byte
		payload []byte
		enum    bool
	}{{"stream_tag_enum_mismatch", 30, b(4), true}, {"stream_tag_pointer_mismatch", 17, b(0), false}, {"stream_tag_bytes_mismatch", 14, b(6, 2, 'h', 'i'), false}, {"stream_tag_short_width", 8, b(1, 2, 3), false}} {
		t.Run(tc.name, func(t *testing.T) {
			ids := []string{"id", "frame", "tag"}
			partsRef := byte(4)
			if tc.enum {
				ids = append(ids, "Gold")
				partsRef = 5
			}
			ids = append(ids, "parts")
			body := cat(b(1, 8), i32(11), b(2, 15, 3, tc.kind), leb(uint64(len(tc.payload))), tc.payload, b(partsRef, 14, 3, 17, 1, 0, 0))
			wire, err := (framingVector{ids: ids, body: body}).bytes(nil)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "testdata/wire/tables", tc.name+".bin")
			if os.Getenv("FRAMING_WRITE") == "1" {
				if err := os.WriteFile(path, wire, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, wire) {
				t.Error("variable arm kind fixture differs from its builder")
			}
		})
	}
}

func TestVariableArmKindBoundary(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, p := range m.Units[i].Paths {
			if !filepath.IsAbs(p) {
				m.Units[i].Paths[j] = filepath.Join(root, p)
			}
		}
	}
	units := newUnits(m)
	u, err := units.get("streamdemo")
	if err != nil {
		t.Fatal(err)
	}
	st := u.Tables["Feed"]
	for _, tc := range []struct{ name, reason string }{{"stream_tag_enum_mismatch", "kind_mismatch"}, {"stream_tag_pointer_mismatch", "kind_mismatch"}, {"stream_tag_bytes_mismatch", "kind_mismatch"}, {"stream_tag_short_width", "wire_damaged"}} {
		wire, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables", tc.name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		sh, _ := damageAwareFileSpellings(u, st, wire)
		if len(sh) > 0 {
			t.Errorf("%s: %v", tc.name, sh)
		}
		fault, _ := damageAwareIDTableFault(wire)
		if fault != "" {
			t.Errorf("%s: %s", tc.name, fault)
		}
		wr, err := newWireRoot(units, "streamdemo", "Feed", false, false)
		if err != nil {
			t.Fatal(err)
		}
		var report tabletext.Report
		ok, err := tablewire.Decode(wr.model, wr.model.New(wr.def), wire, &report)
		if ok || err == nil || !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("%s: ok=%v err=%v report=%+v", tc.name, ok, err, report)
		}
	}
}

// TestFixedRootVariablePointerArmPin isolates the foreign pointer kind for
// the explicit A2 variable reader; generic fixed reads still use previous_form.
func TestFixedRootVariablePointerArmPin(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	v := framingVector{ids: []string{"id", "value", "a", "tail"}, body: cat(b(1, 8), i32(11), b(2, 15, 3, 17, 1, 0, 4, 8), i32(22), b(0))}
	wire, err := v.bytes(nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "testdata/wire/tables/a2_arm_pointer_as_int.bin")
	if os.Getenv("FRAMING_WRITE") == "1" {
		if err := os.WriteFile(path, wire, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, wire) {
		t.Fatal("pointer-arm mismatch differs from builder")
	}
	if fault := framingTableFault(framingVector{}, wire); fault != "" {
		t.Error(fault)
	}
}

// TestMessageTruncatedBytesPin isolates the byte-count-before-truncation
// boundary under LoginRequest's own announced vocabulary.
func TestMessageTruncatedBytesPin(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	build := func(n uint64, body []byte, finish bool) []byte {
		w := &bitw{}
		w.put(3, 5)
		w.put(n, 6)
		w.align()
		w.bytes(body)
		if finish {
			w.put(0, 5)
			w.align()
		}
		return append([]byte{ir.TableWireMessageForm, 0}, w.b...)
	}
	wire := build(33, []byte{1, 2, 3}, false)
	path := filepath.Join(root, "testdata/wire/tables/fuzz-vectors/message_bytes_truncated_isolated.bin")
	if os.Getenv("FRAMING_WRITE") == "1" {
		if err := os.WriteFile(path, wire, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, wire) {
		t.Fatal("truncated message differs from bit builder")
	}
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, p := range m.Units[i].Paths {
			if !filepath.IsAbs(p) {
				m.Units[i].Paths[j] = filepath.Join(root, p)
			}
		}
	}
	units := newUnits(m)
	u, err := units.get("backenddemo")
	if err != nil {
		t.Fatal(err)
	}
	wr, err := newWireRoot(units, "backenddemo", "LoginRequest", true, false)
	if err != nil {
		t.Fatal(err)
	}
	announce, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/backend_conn.bin"))
	if err != nil {
		t.Fatal(err)
	}
	shapes, _ := messageStructuralSpellings(u, wr.def, announce, wire)
	if len(shapes) > 0 {
		t.Errorf("truncated message has preceding spelling: %v", shapes)
	}
	for _, tc := range []struct {
		name   string
		data   []byte
		read   bool
		reason string
	}{{"complete at bound", build(32, make([]byte, 32), true), true, ""}, {"complete over bound", build(33, make([]byte, 33), true), false, "length_over_bound"}, {"truncated over bound", wire, false, "wire_damaged"}} {
		var report tabletext.Report
		_, ok, err := tablewire.DecodeMessages(wr.model, []*tabletext.Instance{wr.model.New(wr.def)}, tc.data, wr.vocabulary, &report)
		if ok != tc.read || (tc.read && err != nil) || (!tc.read && (err == nil || !strings.Contains(err.Error(), tc.reason))) {
			t.Errorf("%s: ok=%v err=%v report=%+v", tc.name, ok, err, report)
		}
	}
}

func TestFixedRootPointerArmRefusals(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, p := range m.Units[i].Paths {
			if !filepath.IsAbs(p) {
				m.Units[i].Paths[j] = filepath.Join(root, p)
			}
		}
	}
	wr, err := newWireRoot(newUnits(m), "tbla2", "Root", false, false)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/a2_arm_pointer_as_int.bin"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, reason string
		variable     bool
	}{{"generic fixed", "previous_form", false}, {"explicit variable", "kind_mismatch", true}} {
		var report tabletext.Report
		value := wr.model.New(wr.def)
		var ok bool
		var e error
		if tc.variable {
			ok, e = tablewire.DecodeVariable(wr.model, value, wire, &report)
		} else {
			ok, e = tablewire.Decode(wr.model, value, wire, &report)
		}
		if ok || e == nil || !strings.Contains(e.Error(), tc.reason) {
			t.Errorf("%s: ok=%v err=%v report=%+v", tc.name, ok, e, report)
		}
	}
}

// TestUnknownNodeFixedReaderPin leaves the malformed node record at the end
// of the root rather than adding a second node-placement spelling.
func TestUnknownNodeFixedReaderPin(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, p := range m.Units[i].Paths {
			if !filepath.IsAbs(p) {
				m.Units[i].Paths[j] = filepath.Join(root, p)
			}
		}
	}
	units := newUnits(m)
	var v *framingVector
	for i := range framingVectors {
		if framingVectors[i].name == "node_unknown_to_fixed_reader" {
			v = &framingVectors[i]
			break
		}
	}
	if v == nil {
		t.Fatal("missing node fixture builder")
	}
	wire, err := v.bytes(units)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, framingDir, v.name+".bin")
	if os.Getenv("FRAMING_WRITE") == "1" {
		if err := os.WriteFile(path, wire, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, wire) {
		t.Fatal("unknown node fixture differs from its builder")
	}
}

// TestCollectionKindMismatchPins keeps complete payloads under foreign element
// kinds, so framing damage cannot mask the intended kind mismatches.
func TestCollectionKindMismatchPins(t *testing.T) {
	t.Parallel()
	root := conformanceRoot(t)
	m, err := ReadManifest(filepath.Join(root, defaultManifest), filepath.Join(root, defaultJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	for i := range m.Units {
		for j, p := range m.Units[i].Paths {
			if !filepath.IsAbs(p) {
				m.Units[i].Paths[j] = filepath.Join(root, p)
			}
		}
	}
	units := newUnits(m)
	for _, name := range []string{"array_elem_kind_mismatch", "keyed_elem_kind_mismatch"} {
		var v *framingVector
		for i := range framingVectors {
			if framingVectors[i].name == name {
				v = &framingVectors[i]
				break
			}
		}
		if v == nil {
			t.Fatalf("missing %s", name)
		}
		wire, err := v.bytes(units)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, framingDir, name+".bin")
		if os.Getenv("FRAMING_WRITE") == "1" {
			if err := os.WriteFile(path, wire, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, wire) {
			t.Errorf("%s differs from builder", name)
		}
		u, err := framingVariableUnit(m, "tblr1", "Cfg")
		if err != nil {
			t.Fatal(err)
		}
		sh, e := damageAwareFileSpellings(u, u.Tables["Cfg"], wire)
		if e != nil || len(sh) > 0 {
			t.Errorf("%s: shapes=%v err=%v", name, sh, e)
		}
		fault, e := damageAwareIDTableFault(wire)
		if e != nil || fault != "" {
			t.Errorf("%s: ID fault=%s err=%v", name, fault, e)
		}
		model := tabletext.NewModel(u)
		var report tabletext.Report
		ok, err := tablewire.Decode(model, model.New(u.Tables["Cfg"]), wire, &report)
		if ok || err == nil || !strings.Contains(err.Error(), "kind_mismatch") {
			t.Errorf("%s: ok=%v err=%v report=%+v", name, ok, err, report)
		}
	}
}
