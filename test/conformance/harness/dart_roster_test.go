package main

import (
	"os"
	"strings"
	"testing"
)

// The streaming protocol may answer roots absent. A successful fuzz pass must
// not hide a missing retaining binding, including the populated array root.
func TestDartRetainingRosterAllRoots(t *testing.T) {
	t.Chdir("../../..")
	manifest, err := ReadManifest(defaultManifest, "testdata/conformance/tables/json")
	if err != nil {
		t.Fatal(err)
	}
	seeds, roots, _, _, err := buildWireFuzzRoster(manifest, wireFuzzOptions{retain: true})
	if err != nil {
		t.Fatal(err)
	}
	features := map[string]bool{"Root": false, "ArrayRoot": false}
	for _, root := range roots {
		if root.unit == "featurewitnesses" {
			features[root.root] = true
		}
	}
	for root, seen := range features {
		if !seen {
			t.Fatalf("feature witness %s omitted from retaining roster", root)
		}
	}
	driver := os.Getenv("SCHEMA_DART_ROSTER_DRIVER")
	if driver == "" {
		t.Logf("verified %d retaining roots and %d seeds in manifest (including Root and ArrayRoot)", len(roots), len(seeds))
		return
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
		t.Fatalf("retaining codecs absent: %s", strings.Join(missing, ", "))
	}
	t.Logf("all %d retaining roots advertised, %d seeds, including populated Root and ArrayRoot", len(roots), len(seeds))
}

// These pinned inputs distinguish the builder profile from region decoding
// and prove that retained union frames survive the native adapter.
func TestDartBuilderRefusalAndRetentionVectors(t *testing.T) {
	t.Chdir("../../..")
	manifest, err := ReadManifest(defaultManifest, "testdata/conformance/tables/json")
	if err != nil {
		t.Fatal(err)
	}
	driver := os.Getenv("SCHEMA_DART_ROSTER_DRIVER")
	for _, tc := range []struct {
		name, unit, root string
		retain           bool
	}{
		{"list_count_cross_length", "listdemo", "Album", false},
		{"list_count_cross_length", "listdemo", "Album", true},
		{"retain_union_arm_frame", "streamdemo", "Feed", true},
	} {
		t.Run(tc.name+map[bool]string{false: "/plain", true: "/retain"}[tc.retain], func(t *testing.T) {
			path := "testdata/wire/tables/fuzz-vectors/" + tc.name + ".bin"
			forL, err := os.ReadFile(path)
			if err != nil || len(forL) == 0 {
				t.Fatalf("missing pinned vector %s: %v", path, err)
			}
			if driver == "" {
				return
			}
			opts := wireFuzzOptions{driver: driver + " wire-fuzz", builder: true, retain: tc.retain,
				replay: path, unit: tc.unit, root: tc.root}
			if err := wireFuzz(manifest, opts); err != nil {
				t.Fatal(err)
			}
		})
	}
}
