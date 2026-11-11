package jstable

// The fixed form's check pass refuses over LIVE data before the decode writes
// anything (docs/FIXED-FORM-ALGORITHM.md §5.3 step 11). An enclosing loop or
// branch earns its place only when the element or payload inside carries a
// check, so a counted array of plain integers and an optional plain integer
// leave the count and present-byte refusals in place and emit no empty block.

import (
	"regexp"
	"strings"
	"testing"
)

const jsFixedCheckEmptyBlocksSchema = `package probe

type Label {
  text string(4)
}

fixed table R {
  ints [..4]uint16
  octets [4]uint8
  opt ?uint32
  labels [..2]Label
  f ?float32
}
`

func TestJsFixedCheckEmitsNoEmptyBlocks(t *testing.T) {
	t.Parallel()
	u := loadSchemaString(t, "check_empty_blocks.schema", jsFixedCheckEmptyBlocksSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var decode string
	for _, b := range files {
		if body := jsFn(string(b), "RFixedDecode"); body != "" {
			decode = body
			break
		}
	}
	if decode == "" {
		t.Fatal("no RFixedDecode in the generated unit")
	}

	// The check pass ends where the first destination byte is written.
	check := decode
	if i := strings.Index(check, "value."); i >= 0 {
		check = check[:i]
	}
	if loc := regexp.MustCompile(`(?s)\{\s*\}`).FindStringIndex(check); loc != nil {
		t.Fatalf("the fixed check pass emits an empty block: %q", check[loc[0]:loc[1]])
	}
	if !strings.Contains(check, "TableFixedRefusal.TextIllFormed") {
		t.Error("the counted text-bearing array lost its text_ill_formed check")
	}
	if !strings.Contains(check, "TableFixedRefusal.NonFiniteFloat") {
		t.Error("the optional float lost its non_finite_float check")
	}
}
