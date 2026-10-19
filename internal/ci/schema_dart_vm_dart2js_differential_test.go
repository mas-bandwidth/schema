package ci

// The Dart VM / dart2js differential's fast half (the heavy half is the make
// target in make/schema-dart-vm-dart2js-differential.mk, nightly only).
//
// This test pins the wiring a reader cannot hold by hand: the target exists,
// the tool versions are pinned, the corpus is the Dart generate target's own,
// the runtime lane really compares the VM with node over a web-safe build, and
// the negative control drives the REAL target with a planted run-time
// divergence. The differential runs on a schedule and never on a push. The
// test invokes no Dart.
import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDartVmDart2jsDifferentialWiring(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	mk, err := os.ReadFile(filepath.Join(root, "make", "schema-dart-vm-dart2js-differential.mk"))
	if err != nil {
		t.Fatalf("the card's make file is missing: %v", err)
	}
	mkText := string(mk)
	for _, want := range []string{
		"tables-dart-vm-dart2js-differential:",
		"Dart SDK version: $(DART_SDK_PIN) ",
		"v26.7.0",
		"build/tables-generated-dart/.stamp",
		"test/tables/Bases.schema",
		"test/dart-differential/vmref.dart",
		"test/dart-differential/webints.dart",
		"test/dart-differential/range.dart",
		"range-table-writer=refused:TableWire: 64-bit value is outside the exact web int range (2^53)",
		"range-reader=exact:9007199254740993",
		"range-safe=9007199254740991",
		"range-edge=9007199254740992",
		"test/dart-differential/websafe.py",
		"test/dart-differential/corpus_driver.py",
		"corpus-units=25",
		"corpus_websafe.dart",
		"DART_DIFF_PLANT",
		"gen-websafe",
		"PLANT seen",
		"roundtrip=ok",
		"tables-dart-vm-dart2js-differential-negative-control:",
		"$(MAKE) -s tables-dart-vm-dart2js-differential",
	} {
		if !strings.Contains(mkText, want) {
			t.Errorf("the make file does not carry %q", want)
		}
	}

	// THE INT64 LANE BOUNDS. A declared range bound must fold to the widest
	// exact web int, not to its low 31 bits, or every value in 2^32..2^53 is
	// clamped and the lane silently stops testing the bug it exists for.
	websafe, err := os.ReadFile(filepath.Join(root, "test", "dart-differential", "websafe.py"))
	if err != nil {
		t.Fatalf("the web-safe fold is missing: %v", err)
	}
	ws := string(websafe)
	for _, want := range []string{"SAFE = 2**53 - 1", "def fold_bound", "report.clamped++", "_jsonIntField("} {
		if !strings.Contains(ws, want) {
			t.Errorf("websafe.py does not carry %q: the int64 bounds would be folded to 31 bits and the lane would clamp", want)
		}
	}
	driver, err := os.ReadFile(filepath.Join(root, "test", "dart-differential", "webints.dart"))
	if err != nil {
		t.Fatalf("the runtime lane driver is missing: %v", err)
	}
	for _, want := range []string{"9007199254740991", "-9007199254740991", "roundtrip=", "same('i64Span'", "same('u64Low'"} {
		if !strings.Contains(string(driver), want) {
			t.Errorf("webints.dart does not carry %q: the 2^32..2^53 values must round-trip unclamped", want)
		}
	}

	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "schema-dart-vm-dart2js-differential.yml"))
	if err != nil {
		t.Fatalf("the card's workflow is missing: %v", err)
	}
	wf := string(workflow)
	if !strings.Contains(wf, "schedule:") {
		t.Error("the differential workflow has no schedule trigger")
	}
	if !strings.Contains(wf, "workflow_dispatch:") {
		t.Error("the differential workflow has no workflow_dispatch trigger")
	}
	if strings.Contains(wf, "go run ./tools/negativecontrols") {
		t.Error("the differential workflow types its own negative-control list")
	}
	fast, _, _ := strings.Cut(wf, "  differential:")
	if strings.Contains(fast, "tables-dart-vm-dart2js-differential DART=dart") {
		t.Error("the differential itself runs on the fast push job")
	}

	plan, err := os.ReadFile(filepath.Join(root, "make", "negative-controls.json"))
	if err != nil {
		t.Fatalf("the negative-control plan is missing: %v", err)
	}
	var manifest struct {
		Groups []struct {
			Name    string   `json:"name"`
			When    string   `json:"when"`
			Targets []string `json:"targets"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(plan, &manifest); err != nil {
		t.Fatalf("the negative-control plan does not parse: %v", err)
	}
	found := false
	for _, g := range manifest.Groups {
		for _, target := range g.Targets {
			if target != "tables-dart-vm-dart2js-differential-negative-control" {
				continue
			}
			found = true
			if g.When != "nightly" {
				t.Errorf("the differential control is in the %q group (%s): a differential is heavy and belongs nightly", g.Name, g.When)
			}
		}
	}
	if !found {
		t.Error("tables-dart-vm-dart2js-differential-negative-control is in no negative-control group")
	}
}
