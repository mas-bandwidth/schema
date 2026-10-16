package javatable

import (
	"path/filepath"
	"strings"
	"testing"

	javapkt "github.com/mas-bandwidth/schema/v2/internal/codegen/java"
)

// THE CANONICAL PASS MOVES LAYOUT AND NEVER A TOKEN (issue #547,
// docs/JAVA-DIALECT.md). Every generated table file goes through
// java.Canonical so that google-java-format --aosp has nothing to change; this
// holds, over the table corpora the format gate formats, that the pass
// changed only whitespace and comments (and dropped only imports nothing
// names), and that a second pass changes nothing. It reads the generator's
// output and runs no Java: the formatter itself is make tables-java-clean.
func TestCanonicalMovesLayoutNotTokens(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	dirs := []string{"tables/examples", "tables/pointers", "tables/blobs", "tables/block", "tables/blockhome", "tables/scalars"}
	for _, d := range dirs {
		u := unitFromDir(t, filepath.Join(root, d))
		raw, err := generateLineage(u, nil, func(b []byte) []byte { return b })
		if err != nil {
			t.Fatalf("%s: %v", d, err)
		}
		canon, err := GenerateLineage(u, nil)
		if err != nil {
			t.Fatalf("%s: %v", d, err)
		}
		if len(raw) != len(canon) {
			t.Fatalf("%s: the canonical pass changed the file set (%d files, then %d)", d, len(raw), len(canon))
		}
		for name, before := range raw {
			after, ok := canon[name]
			if !ok {
				t.Fatalf("%s: %s is missing after the canonical pass", d, name)
			}
			if got, want := javapkt.CodeTokens(string(after)), javapkt.CodeTokens(withoutDroppedImports(string(before), string(after))); got != want {
				t.Errorf("%s/%s: the canonical pass changed a token, not only layout", d, name)
			}
			if again := javapkt.Canonical(after); string(again) != string(after) {
				t.Errorf("%s/%s: the canonical pass is not idempotent", d, name)
			}
			if javapkt.CommentText(string(after)) != javapkt.CommentText(string(before)) {
				t.Errorf("%s/%s: the canonical pass changed the words of a comment", d, name)
			}
		}
	}
}

// withoutDroppedImports removes from before each import line after no
// longer carries, the one token-level change the pass is allowed.
func withoutDroppedImports(before, after string) string {
	var out []string
	for _, l := range strings.Split(before, "\n") {
		if strings.HasPrefix(l, "import ") && !strings.Contains(after, l+"\n") {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}
