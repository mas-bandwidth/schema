package compiler

import (
	"os"
	"strings"
	"testing"
)

// TestSpecUnpackIllFormedIsOneAnswer pins docs/SPEC-TABLES.md §17.3's answer
// for ill-formed text in storage: `unpack`'s writer refuses it under §16.3's
// rule, and §17.3 assigns no replacement of its own.
func TestSpecUnpackIllFormedIsOneAnswer(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	unpack := specTablesSection(t, page,
		"### 17.3 What comes out", "### 17.4 ")

	if !strings.Contains(unpack, "refused by `unpack`'s\nwriter, `invalid_value` (§16.3)") {
		t.Error("§17.3 must defer the unpack writer's ill-formed text to §16.3's refusal")
	}
	for _, stale := range []string{"one per bad byte", "U+FFFD", "maximal subpart"} {
		if strings.Contains(unpack, stale) {
			t.Errorf("§17.3 must not assign a replacement to unpack: it states %q", stale)
		}
	}
}
