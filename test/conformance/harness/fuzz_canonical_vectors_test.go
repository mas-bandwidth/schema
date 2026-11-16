package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
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
