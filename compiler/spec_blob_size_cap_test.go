// TestSpecBlobPastSizeCapRefusesTheWire pins docs/SPEC-TABLES.md §3.1's blob
// record and the size cap: a blob past the cap refuses the whole wire, the
// load returning nothing, and the section must not state the retired sentence
// that counted it malformed with each slot reading null. The page is found the
// same way compiler/docs_test.go finds it: from this package's directory.
package compiler

import (
	"os"
	"strings"
	"testing"
)

// blobCapSection returns the page from heading up to (not including) the next
// heading, so a literal is found where §3.1 states it.
func blobCapSection(t *testing.T, page, heading, next string) string {
	t.Helper()
	data, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	from := strings.Index(text, heading)
	if from < 0 {
		t.Fatalf("%s has no heading %q", page, heading)
	}
	to := strings.Index(text[from:], next)
	if to < 0 {
		t.Fatalf("%s has no heading %q after %q", page, next, heading)
	}
	return text[from : from+to]
}

func TestSpecBlobPastSizeCapRefusesTheWire(t *testing.T) {
	t.Parallel()
	const row = "§3.1 a blob past the size cap"
	section := blobCapSection(t, "../docs/SPEC-TABLES.md", "### 3.1 ", "### 3.2 ")

	wants := []string{
		"refuses the whole wire",
		"§2.5's size cap",
		"2^32 - 1",
		"blob_over_size_cap",
		"-2",
		"the load refuses the same reason and returns nothing",
	}
	stales := []string{
		"§11's derived-size cap",
		"counted `malformed`, with every slot naming it reading null",
	}

	for _, want := range wants {
		if !strings.Contains(section, want) {
			t.Errorf("%s: the section no longer states %q", row, want)
		}
	}
	for _, stale := range stales {
		if strings.Contains(section, stale) {
			t.Errorf("%s: the section still states the stale sentence %q", row, stale)
		}
	}
}
