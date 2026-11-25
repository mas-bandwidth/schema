package ccommon

import (
	"strings"
	"testing"
)

// TestPackedInitializerShape holds the packed layout: 16 items to a line, the
// last line closing the brace, and NO COMMA after the last item. A trailing
// comma there makes clang-format and cfmt lay the list out one item to a line,
// the shape that cost tables-c-clean 41-50 s on one header.
func TestPackedInitializerShape(t *testing.T) {
	bs := make([]byte, 35)
	for i := range bs {
		bs[i] = byte(i)
	}
	got := PackedBytes(bs)
	want := "    0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,\n" +
		"    0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,\n" +
		"    0x20, 0x21, 0x22 };\n"
	if got != want {
		t.Fatalf("packed bytes:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, ", };") || strings.Contains(got, ",\n};") {
		t.Fatalf("the last item carries a comma, which lays the list out one to a line:\n%s", got)
	}
}

func TestPackedInitializerEdges(t *testing.T) {
	sixteen := strings.Split("1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16", " ")
	for _, tc := range []struct {
		name  string
		items []string
		want  string
	}{
		{"none", nil, "};\n"},
		{"one", []string{"7"}, "    7 };\n"},
		{"exactly a line", sixteen, "    1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16 };\n"},
		{"a line and one", append(append([]string{}, sixteen...), "17"), "    1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,\n    17 };\n"},
	} {
		if got := PackedInitializer(tc.items); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
