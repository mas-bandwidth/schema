package cpptable

// THE EMITTED C++ JSON RUNTIME'S COMMENTS DESCRIBE WHAT ITS CODE DOES.
// docs/SPEC-TABLES.md §16.14 W0 says an ill-formed UTF-8 sequence is
// refused by the JSON reader and the writer, never replaced. The reader's
// string-unit scanner and the writer both refuse: the scanner sets in.bad
// and the writer sets out.invalid. A comment that still promises a U+FFFD
// reading would invite a maintainer to restore the removed tolerant path,
// so this test reads the emitted bytes and requires no such promise.

import (
	"slices"
	"strings"
	"testing"
)

const cppJsonTextCommentSchema = `package probe
table Text
{
    name string(32)
}
`

// scannerUtf8Comment answers the comment block immediately above the
// string-unit scanner's UTF-8 validity test.
func scannerUtf8Comment(t *testing.T, text string) string {
	t.Helper()
	at := strings.Index(text, "inline bool TableJsonScanUnit(")
	if at < 0 {
		t.Fatal("the emitted runtime has no TableJsonScanUnit")
	}
	rest := text[at:]
	before, _, ok := strings.Cut(rest, "TableUtf8Valid( (const uint8_t *) unit, unit_length )")
	if !ok {
		t.Fatal("TableJsonScanUnit has no UTF-8 test over the scanned unit")
	}
	lines := strings.Split(before, "\n")
	lines = lines[:len(lines)-1]
	var comment []string
	for _, line := range slices.Backward(lines) {
		line := strings.TrimSpace(line)
		if !strings.HasPrefix(line, "//") {
			break
		}
		comment = append([]string{line}, comment...)
	}
	return strings.Join(comment, "\n")
}

func TestCppJsonTextCommentsSayRefused(t *testing.T) {
	t.Parallel()

	u := cppUnitFrom(t, cppJsonTextCommentSchema)
	files, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatalf("GenerateLineage: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("GenerateLineage emitted no files")
	}

	sawScanner := false
	for name, data := range files {
		text := string(data)
		for _, stale := range []string{
			"READS AS U+FFFD",
			"U+FFFD replacement",
		} {
			if strings.Contains(text, stale) {
				t.Errorf("%s: a comment still says an ill-formed sequence is replaced rather than refused (%q); docs/SPEC-TABLES.md §16.14 W0", name, stale)
			}
		}
		if !strings.Contains(text, "inline bool TableJsonScanUnit(") {
			continue
		}
		sawScanner = true
		comment := scannerUtf8Comment(t, text)
		if !strings.Contains(strings.ToLower(comment), "refus") {
			t.Errorf("%s: the comment over the string-unit scanner's UTF-8 test does not say an ill-formed sequence is refused (docs/SPEC-TABLES.md §16.14 W0):\n%s", name, comment)
		}
	}
	if !sawScanner {
		t.Fatal("no emitted file carries the string-unit scanner")
	}
}
