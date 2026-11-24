package rubytable

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// chunkBoundary is the text fixedRubyLiteralOf writes between two chunks.
const chunkBoundary = "] + ["

func TestFixedRubyLiteralChunksLongArrays(t *testing.T) {
	t.Parallel()
	for _, n := range []int{0, 1, fixedLiteralChunk, fixedLiteralChunk + 1, 3 * fixedLiteralChunk, 100} {
		x := make([]any, n)
		var flat []string
		for i := range x {
			x[i] = int64(i)
			flat = append(flat, strconv.Itoa(i))
		}
		got := fixedRubyLiteralOf(x)
		if n == 0 {
			if got != "_s_fixed_arr" {
				t.Fatalf("empty array is %q", got)
			}
			continue
		}
		// every chunk is an array literal of at most fixedLiteralChunk elements,
		// and the chunks, read in order, hold exactly the elements
		var seen []string
		for chunk := range strings.SplitSeq(got, " + ") {
			if !strings.HasPrefix(chunk, "[") || !strings.HasSuffix(chunk, "]") {
				t.Fatalf("n=%d: chunk %q is not an array literal", n, chunk)
			}
			els := strings.Split(strings.TrimSuffix(strings.TrimPrefix(chunk, "["), "]"), ", ")
			if len(els) > fixedLiteralChunk {
				t.Fatalf("n=%d: a chunk holds %d elements, at most %d", n, len(els), fixedLiteralChunk)
			}
			seen = append(seen, els...)
		}
		if strings.Join(seen, ",") != strings.Join(flat, ",") {
			t.Fatalf("n=%d: the chunks do not hold the elements in order: %s", n, got)
		}
		if want := (n-1)/fixedLiteralChunk + 1; strings.Count(got, chunkBoundary) != want-1 {
			t.Fatalf("n=%d: %d chunk boundaries, want %d", n, strings.Count(got, chunkBoundary), want-1)
		}
	}
}

// a long array inside a hash inside an array is chunked at every level that is long
func TestFixedRubyLiteralChunksNestedLongArrays(t *testing.T) {
	t.Parallel()
	kids := make([]any, 70)
	for i := range kids {
		kids[i] = map[string]any{"id": int64(i)}
	}
	got := fixedRubyLiteralOf([]any{map[string]any{"children": kids}})
	if c := strings.Count(got, chunkBoundary); c != 2 {
		t.Fatalf("70 children are three chunks, got %d boundaries: %s", c, got)
	}
	longest := 0
	for _, run := range regexp.MustCompile(`\{ "id" => \d+ \}(, \{ "id" => \d+ \})*`).FindAllString(got, -1) {
		longest = max(longest, strings.Count(run, "{ \"id\""))
	}
	if longest > fixedLiteralChunk {
		t.Fatalf("a run of %d hashes is one array literal, at most %d", longest, fixedLiteralChunk)
	}
}
