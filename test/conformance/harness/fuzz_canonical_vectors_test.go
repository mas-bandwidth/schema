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
