package darttable

import (
	"strings"
	"testing"
)

// TestWireFileEndsWithOneNewline holds the form-1 wire library <Home>Wire.dart
// to one terminal newline. The descriptor block is the file's last section and
// closed with a blank line, so the committed generated/bench/paired/dart/
// BenchWire.dart carried a blank line at EOF that `git diff --check` refuses.
// Both units are ones the repository commits output for: the card closure and
// the paired bench unit (bench/corpus/Bench.schema with FixedTable.schema).
func TestWireFileEndsWithOneNewline(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		file string
		load func(*testing.T) map[string][]byte
	}{
		{"cards", "CardsWire.dart", func(t *testing.T) map[string][]byte {
			files, err := Generate(cardsUnit(t))
			if err != nil {
				t.Fatalf("Generate cards: %v", err)
			}
			return files
		}},
		{"paired bench", "BenchWire.dart", func(t *testing.T) map[string][]byte {
			u := loadUnit(t, "../../../bench/corpus/Bench.schema", "../../../bench/corpus/FixedTable.schema")
			files, err := Generate(u)
			if err != nil {
				t.Fatalf("Generate paired bench: %v", err)
			}
			return files
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src, ok := c.load(t)[c.file]
			if !ok || len(src) == 0 {
				t.Fatalf("Generate emitted no %s", c.file)
			}
			s := string(src)
			if !strings.HasSuffix(s, "\n") || strings.HasSuffix(s, "\n\n") {
				t.Fatalf("%s must end with exactly one newline; its last bytes are %q", c.file, s[max(0, len(s)-16):])
			}
		})
	}
}
