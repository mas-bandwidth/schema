package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestEveryCIRowChecksOutItsSiblingRuntime holds each leg's CI row
// (test/conformance/<lang>/ci.json) to the sibling runtime the harness
// requires for it (siblingRuntimes): a leg that compiles or runs against
// ../serialize.<x> names that checkout as "runtime" and the workflow variable
// holding its pin as "runtime_tag", so the nightly, ci-full and leg workflows
// check it out before the leg runs. The C++ reference's ../serialize and Go's
// ../serialize.go are checked out by every such job and need no row.
//
// Nightly 45119714773 ran the js port with no ../serialize.js: the js row
// named no runtime, so the harness, the native js tests and every packet
// program failed on the missing checkout.
func TestEveryCIRowChecksOutItsSiblingRuntime(t *testing.T) {
	dir := "../../../test/conformance"
	workflows := []string{
		"../../../.github/workflows/nightly-conformance.yml",
		"../../../.github/workflows/ci-full.yml",
		"../../../.github/workflows/leg.yml",
	}
	checked := 0
	for lang, s := range siblingRuntimes {
		row := filepath.Join(dir, lang, "ci.json")
		data, err := os.ReadFile(row)
		if os.IsNotExist(err) && lang == referenceLang {
			continue // the reference has its own job, which checks every sibling out
		}
		if err != nil {
			t.Fatalf("%s: the %s leg needs %s and has no readable CI row: %v", row, lang, s.dir, err)
		}
		var fields map[string]string
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatalf("%s: %v", row, err)
		}
		checkout := filepath.Base(s.dir)
		if checkout == "serialize" || checkout == "serialize.go" {
			continue
		}
		checked++
		if fields["runtime"] != checkout {
			t.Errorf("%s: the %s leg runs against %s, and its row names runtime %q, not %q",
				row, lang, s.dir, fields["runtime"], checkout)
			continue
		}
		tag := fields["runtime_tag"]
		if !regexp.MustCompile(`^SERIALIZE_[A-Z]+_TAG$`).MatchString(tag) {
			t.Errorf("%s: runtime_tag %q is not a SERIALIZE_<X>_TAG workflow variable", row, tag)
			continue
		}
		pin := regexp.MustCompile(`(?m)^  ` + tag + `: v[0-9]+\.[0-9]+\.[0-9]+$`)
		for _, wf := range workflows {
			text, err := os.ReadFile(wf)
			if err != nil {
				t.Fatalf("%s: %v", wf, err)
			}
			if !pin.Match(text) {
				t.Errorf("%s: the %s row's runtime_tag %s is not pinned at the top of %s", row, lang, tag, wf)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no leg with a sibling runtime of its own was checked")
	}
}
