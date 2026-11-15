// TestSpecJsonCommentTextRules pins docs/SPEC-TABLES.md §16.2's JSON comment
// text rules and §16.14 W2's read clause, docs/rulings.json's `json-comment-text`
// entry, and the vectors the engine and every port card read: a comment ends at
// a line feed or the end of input, holds only well-formed UTF-8 and no NUL, and
// the read's depth cap counts every object and array it walks.
package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpecJsonCommentTextRules(t *testing.T) {
	t.Parallel()

	t.Run("§16.2 states the comment text rules", func(t *testing.T) {
		t.Parallel()
		page := readSpecTables(t)
		comments := specTablesSection(t, page,
			"### 16.2 The mapping, field kind by field kind", "### 16.3 ")

		mustState(t, "§16.2 a line ends at a line feed or the end of input",
			comments, "a LINE FEED (`0x0A`) or to the END OF INPUT")
		mustState(t, "§16.2 a lone carriage return does not end a line comment",
			comments, "carriage return alone (`0x0D`) does not end it")
		mustState(t, "§16.2 a CR LF line comment ends at the line feed",
			comments, "ended by `CR LF` ends at the `LF`")
		mustState(t, "§16.2 a comment holds only well-formed UTF-8",
			comments, "block, are well-formed UTF-8 and hold no NUL (`0x00`)")
		mustState(t, "§16.2 an ill-formed comment sequence is refused by name",
			comments, "ill-formed sequence refuses the read `malformed`, with the recommended name")
		mustState(t, "§16.2 the ill-formed comment name is §16.3's",
			comments, "`string_not_utf8` that §16.3 gives ill-formed UTF-8 at a string site")
		mustState(t, "§16.2 a NUL in a comment is refused malformed",
			comments, "comment holding a NUL refuses it `malformed`")
		mustState(t, "§16.2 the read's depth cap counts every object and array",
			comments, "The read's cap counts every object and array the reader walks, placed or skipped")
		mustState(t, "§16.2 the read's depth cap counts the value under an unknown key",
			comments, "the value under an unknown key included")
	})

	t.Run("W2's write cap and the read's cap are different units", func(t *testing.T) {
		t.Parallel()
		page := readSpecTables(t)
		w2 := specTablesSection(t, page,
			"**W2. The document.**", "**W3. A table or type value**")

		mustState(t, "W2 the write's cap counts table values",
			w2, "THE DEPTH CAP IS 128, AND ON THE WRITE IT COUNTS TABLE VALUES")
		mustState(t, "W2 the read's cap counts every object and array it walks",
			w2, "THE READ'S CAP AT THE SAME NUMBER COUNTS EVERY OBJECT AND ARRAY IT WALKS")
		mustNotState(t, "W2 no longer says the read counts an object's table depth",
			w2, "and so does the read of an object that deep")
	})

	t.Run("docs/rulings.json carries json-comment-text", func(t *testing.T) {
		t.Parallel()
		r, ok := readRulings(t)["json-comment-text"]
		if !ok {
			t.Fatal("docs/rulings.json carries no ruling \"json-comment-text\"")
		}
		if r.Date != "2026-11-10" {
			t.Errorf("json-comment-text date is %q, want 2026-11-10", r.Date)
		}
		const rulingText = "ruled 2026-11-10: a JSON comment ends at a line feed or the end of input, holds only well-formed UTF-8 and no NUL, and the read's depth cap counts every object and array"
		if len(r.Quotes) != 1 || r.Quotes[0] != rulingText {
			t.Errorf("json-comment-text quotes are %q, want exactly the ruling text %q", r.Quotes, rulingText)
		}
		const source = "the owner's standing rule of 2026-11-10 that where the standard is silent or lenient, strict wins, applied by the coordinator"
		if r.Source != source {
			t.Errorf("json-comment-text source is %q, want %q", r.Source, source)
		}
	})

	t.Run("the vectors exist and the manifest names each verdict", func(t *testing.T) {
		t.Parallel()
		const dir = "../testdata/wire/json-comment-text-rules"
		want := []struct {
			name    string
			verdict string
		}{
			{"line-comment-utf8.read.json", "read"},
			{"block-comment-utf8.read.json", "read"},
			{"line-comment-lone-continuation-0x80.refused.json", "refused"},
			{"block-comment-lone-continuation-0x80.refused.json", "refused"},
			{"line-comment-overlong-c0-af.refused.json", "refused"},
			{"block-comment-overlong-c0-af.refused.json", "refused"},
			{"line-comment-truncated-three-byte.refused.json", "refused"},
			{"block-comment-truncated-three-byte.refused.json", "refused"},
			{"line-comment-encoded-surrogate-ed-a0-80.refused.json", "refused"},
			{"block-comment-encoded-surrogate-ed-a0-80.refused.json", "refused"},
			{"line-comment-nul.refused.json", "refused"},
			{"block-comment-nul.refused.json", "refused"},
			{"line-comment-lone-cr-key-inside.refused.json", "refused"},
			{"line-comment-crlf.read.json", "read"},
			{"unknown-key-128-nested-arrays.read.json", "read"},
			{"unknown-key-129-nested-arrays.refused.json", "refused"},
		}

		data, err := os.ReadFile(filepath.Join(dir, "manifest.tsv"))
		if err != nil {
			t.Fatalf("the vectors' manifest: %v", err)
		}
		named := make(map[string]string)
		for i, line := range strings.Split(string(data), "\n") {
			line = strings.TrimRight(line, "\r")
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Split(line, "\t")
			if len(fields) != 3 || fields[0] == "" || fields[1] == "" || fields[2] == "" {
				t.Fatalf("%s/manifest.tsv:%d: a vector line is <file> TAB <verdict> TAB <what it holds>, got %q", dir, i+1, line)
			}
			name, verdict := fields[0], fields[1]
			if verdict != "read" && verdict != "refused" {
				t.Errorf("%s/manifest.tsv:%d: %q has verdict %q, want read or refused", dir, i+1, name, verdict)
			}
			if _, dup := named[name]; dup {
				t.Errorf("%s/manifest.tsv:%d: %q is named twice", dir, i+1, name)
			}
			named[name] = verdict
		}

		if len(named) != len(want) {
			t.Errorf("%s/manifest.tsv names %d vectors, want %d", dir, len(named), len(want))
		}
		for _, v := range want {
			verdict, ok := named[v.name]
			if !ok {
				t.Errorf("%s/manifest.tsv names no vector %q", dir, v.name)
			} else if verdict != v.verdict {
				t.Errorf("%s/manifest.tsv gives %q verdict %q, want %q", dir, v.name, verdict, v.verdict)
			}
			if info, err := os.Stat(filepath.Join(dir, v.name)); err != nil {
				t.Errorf("the vector %q: %v", v.name, err)
			} else if info.IsDir() || info.Size() == 0 {
				t.Errorf("the vector %q is not a non-empty file", v.name)
			}
			if !strings.HasSuffix(v.name, "."+v.verdict+".json") {
				t.Errorf("the vector %q is not named by its verdict %q", v.name, v.verdict)
			}
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("the vectors' directory: %v", err)
		}
		for _, e := range entries {
			if e.IsDir() || e.Name() == "manifest.tsv" {
				continue
			}
			if _, ok := named[e.Name()]; !ok {
				t.Errorf("the vector %q is not named in manifest.tsv", e.Name())
			}
		}
	})
}
