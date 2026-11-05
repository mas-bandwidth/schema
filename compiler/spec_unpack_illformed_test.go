package compiler

import (
	"os"
	"strings"
	"testing"
)

// TestSpecUnpackDefersIllFormedTextToTheWriter pins §17.3 to §16.3: unpack
// writes through the one text writer, which refuses ill-formed text, so the
// unpack lap has no replacement unit of its own and no carve-out.
func TestSpecUnpackDefersIllFormedTextToTheWriter(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	unpack := specTablesSection(t, page,
		"### 17.3 What comes out", "### 17.4 ")

	if !strings.Contains(unpack, "ill-formed UTF-8 case handled under §16.3") {
		t.Error("§17.3 must defer the unpack writer's ill-formed UTF-8 handling to §16.3")
	}
	if !strings.Contains(unpack, "refused `invalid_value` by\nthe writer, never replaced") {
		t.Error("§17.3 must say the writer refuses ill-formed storage rather than replacing it")
	}
	for _, stale := range []string{"one per bad byte", "U+FFFD", "the field's own bound then clamps it"} {
		if strings.Contains(unpack, stale) {
			t.Errorf("§17.3 still states %q", stale)
		}
	}
}
