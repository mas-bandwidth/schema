package main

import (
	"strings"
	"testing"
)

// The streaming protocol may answer roots absent. A successful fuzz pass must
// not hide a missing retaining binding, including the populated array root.
func TestDartRetainingRosterAllRoots(t *testing.T) {
	driver := nativeDriver(t, "SCHEMA_DART_ROSTER_DRIVER", "build/conformance-dart", "test/conformance/dart")
	t.Chdir("../../..")
	manifest, err := ReadManifest(defaultManifest, "testdata/conformance/tables/json")
	if err != nil {
		t.Fatal(err)
	}
	seeds, roots, _, _, err := buildWireFuzzRoster(manifest, wireFuzzOptions{retain: true})
	if err != nil {
		t.Fatal(err)
	}
	leg, err := startWireLeg(driver+" wire-fuzz", roots)
	if err != nil {
		t.Fatal(err)
	}
	defer leg.kill()
	missing := []string{}
	features := map[string]bool{"Root": false, "ArrayRoot": false}
	for i, root := range roots {
		if !leg.known[i] {
			missing = append(missing, root.unit+"/"+root.root)
		}
		if root.unit == "featurewitnesses" {
			features[root.root] = true
		}
	}
	if len(missing) > 0 {
		t.Fatalf("retaining codecs absent: %s", strings.Join(missing, ", "))
	}
	for root, seen := range features {
		if !seen {
			t.Fatalf("feature witness %s omitted from retaining roster", root)
		}
	}
	t.Logf("all %d retaining roots advertised, %d seeds, including populated Root and ArrayRoot", len(roots), len(seeds))
}

// THE PLAIN ARM'S ROSTER, held whole as the retaining one is: every root the
// corpus seeds (fixed and variable classes alike) is advertised, so a seed
// the plain arm leaves absent is a red here rather than a count in a summary
// line nobody gates (tblvaluedefaults FixedDefaults and packetunions ShapeMsg
// were absent until the driver bound their fixed codecs, 2026-11-24).
func TestDartPlainRosterAllRoots(t *testing.T) {
	driver := nativeDriver(t, "SCHEMA_DART_ROSTER_DRIVER", "build/conformance-dart", "test/conformance/dart")
	t.Chdir("../../..")
	manifest, err := ReadManifest(defaultManifest, "testdata/conformance/tables/json")
	if err != nil {
		t.Fatal(err)
	}
	seeds, roots, _, skipped, err := buildWireFuzzRoster(manifest, wireFuzzOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 0 {
		t.Fatalf("the plain arm skipped %d seeds: it has no seed outside the arm", skipped)
	}
	leg, err := startWireLeg(driver+" wire-fuzz", roots)
	if err != nil {
		t.Fatal(err)
	}
	defer leg.kill()
	missing := []string{}
	for i, root := range roots {
		if !leg.known[i] {
			missing = append(missing, root.unit+"/"+root.root)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("plain codecs absent: %s", strings.Join(missing, ", "))
	}
	t.Logf("all %d plain roots advertised, %d seeds", len(roots), len(seeds))
}

// These pinned inputs distinguish the builder profile from region decoding
// and prove that retained union frames survive the native adapter.
func TestDartBuilderRefusalAndRetentionVectors(t *testing.T) {
	driver := nativeDriver(t, "SCHEMA_DART_ROSTER_DRIVER", "build/conformance-dart", "test/conformance/dart")
	t.Chdir("../../..")
	manifest, err := ReadManifest(defaultManifest, "testdata/conformance/tables/json")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, unit, root string
		retain           bool
	}{
		{"list_count_cross_length", "listdemo", "Album", false},
		{"list_count_cross_length", "listdemo", "Album", true},
		{"retain_union_arm_frame", "streamdemo", "Feed", true},
	} {
		t.Run(tc.name+map[bool]string{false: "/plain", true: "/retain"}[tc.retain], func(t *testing.T) {
			opts := wireFuzzOptions{driver: driver + " wire-fuzz", builder: true, retain: tc.retain,
				replay: "testdata/wire/tables/fuzz-vectors/" + tc.name + ".bin", unit: tc.unit, root: tc.root}
			if err := wireFuzz(manifest, opts); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// THE HELD RESERVED-TYPE VECTORS (docs/SPEC-TABLES.md §3.1, §3.3, §5) through
// the Dart leg: a node record whose TYPE reference resolves to one of the
// three reserved ids refuses the whole load, in the builder read and the
// region read alike, as the engine does
// (TestHeldReservedTypeVectorsAnswerTheEngine).
func TestDartReservedNodeTypeVectors(t *testing.T) {
	driver := nativeDriver(t, "SCHEMA_DART_ROSTER_DRIVER", "build/conformance-dart", "test/conformance/dart")
	t.Chdir("../../..")
	manifest, err := ReadManifest(defaultManifest, "testdata/conformance/tables/json")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"node_type_node_table_id", "node_type_build_version_id", "node_type_vocabulary_id"} {
		for _, builder := range []bool{false, true} {
			t.Run(name+map[bool]string{false: "/region", true: "/builder"}[builder], func(t *testing.T) {
				opts := wireFuzzOptions{driver: driver + " wire-fuzz", builder: builder,
					replay: framingDir + "/" + name + ".bin", unit: "graphdemo", root: "Scene"}
				if err := wireFuzz(manifest, opts); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
