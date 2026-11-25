package ccommon

import (
	"fmt"
	"strings"
)

// PackedPerLine is how many items one line of a packed initializer holds.
const PackedPerLine = 16

// PackedInitializer lays out the items of a braced initializer PackedPerLine
// to a line, four spaces in, and closes the brace on the last item's line:
//
//	0x04, 0x10, 0x00, ... (16 items),
//	0x00, 0x01 };
//
// THE LAST ITEM CARRIES NO COMMA, and that is the point. clang-format (and
// cfmt, which matches it) lays out a braced list whose last element ends in a
// comma one element to a line, so a 70 KB layout became 70,000 lines.
// Without the comma, ColumnLimit 0 keeps the emitter's line breaks, so the
// packed form is what both formatters write back. The values and their order
// are untouched.
//
// It returns the text after the opening brace's line, ending in "};\n";
// with no items that is "};\n" alone, and a caller for whom an empty
// initializer is not lawful C fills it first.
func PackedInitializer(items []string) string {
	var b strings.Builder
	for i := 0; i < len(items); i += PackedPerLine {
		end := min(i+PackedPerLine, len(items))
		b.WriteString("    ")
		b.WriteString(strings.Join(items[i:end], ", "))
		if end == len(items) {
			b.WriteString(" };\n")
		} else {
			b.WriteString(",\n")
		}
	}
	if len(items) == 0 {
		b.WriteString("};\n")
	}
	return b.String()
}

// PackedBytes is PackedInitializer over bytes spelled 0x%02x.
func PackedBytes(bs []byte) string {
	items := make([]string, len(bs))
	for i, v := range bs {
		items[i] = fmt.Sprintf("0x%02x", v)
	}
	return PackedInitializer(items)
}
