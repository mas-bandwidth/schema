package main

import (
	"os"
	"strings"
	"testing"
)

// The streaming protocol may answer roots absent. A successful fuzz pass must
// not hide a missing retaining binding, including the populated array root.
func TestDartRetainingRosterAllRoots(t *testing.T) {
	driver := os.Getenv("SCHEMA_DART_ROSTER_DRIVER")
	if driver == "" {
		t.Skip("run make tables-dart-wire-fuzz to build the native driver")
	}
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
