package cards

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readFixture loads a corpus seed from testdata/epoch17 by name, selected by
// name rather than by a glob that would require every file to parse (card
// STEP 2).
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "epoch17", name))
	if err != nil {
		t.Fatalf("fixture %s is missing: %v", name, err)
	}
	return b
}

// fixtureField extracts one header value from a fixture independently of the
// parser: the text after the first `<key>:` line's colon, trimmed.
func fixtureField(t *testing.T, fixture []byte, key string) string {
	t.Helper()
	for _, line := range bytes.Split(fixture, []byte("\n")) {
		if bytes.HasPrefix(line, []byte(key+":")) {
			return strings.TrimSpace(string(line[len(key)+1:]))
		}
	}
	t.Fatalf("fixture has no %s header", key)
	return ""
}

// bodyAfter returns the exact suffix after the first `<key>:` line's LF, the
// independently located body boundary (card TEXT GRAMMAR).
func bodyAfter(t *testing.T, fixture []byte, key string) []byte {
	t.Helper()
	i := bytes.Index(fixture, []byte(key+":"))
	if i < 0 {
		t.Fatalf("fixture has no %s header", key)
	}
	j := bytes.IndexByte(fixture[i:], '\n')
	if j < 0 {
		t.Fatalf("fixture %s header has no terminator", key)
	}
	return fixture[i+j+1:]
}

// extraPairs reads a record's extra list into key/value pairs, in stored order.
func extraPairs(rec *Record) []extraKV {
	var out []extraKV
	CardExtraEach(&rec.root.Extra, func(e *KeyValueRow) bool {
		kv := extraKV{key: cardString(e.Key[:], e.KeyLength)}
		if e.Value != 0 {
			kv.value = string(stringData(&e.Value, nil))
		}
		out = append(out, kv)
		return true
	})
	return out
}

// cardBody reads a record's body bytes (nil when absent).
func cardBody(rec *Record) []byte {
	if rec.root.Body == 0 {
		return nil
	}
	return stringData(&rec.root.Body, nil)
}

// TestParseRefusesAStrandedKey proves the malformed legacy seed and a minimal
// SOURCE-after-prose brief are refused naming the key and line, while NOTE: and
// unknown key-shaped lines within prose stay opaque (card STRANDED KEYS).
func TestParseRefusesAStrandedKey(t *testing.T) {
	t.Parallel()

	t.Run("rejected legacy seed", func(t *testing.T) {
		t.Parallel()
		_, err := Parse(readFixture(t, "rejected-legacy.md"))
		if err == nil {
			t.Fatal("Parse(rejected-legacy.md) accepted a stranded key")
		}
		if !strings.Contains(err.Error(), "PRIORITY") || !strings.Contains(err.Error(), "line 5") {
			t.Fatalf("refusal does not name the stranded key and line: %v", err)
		}
	})

	t.Run("minimal SOURCE after prose", func(t *testing.T) {
		t.Parallel()
		text := "RESULT: x sha=0123456789ab tier: pro\nprose here\nSOURCE: the source\n"
		_, err := Parse([]byte(text))
		if err == nil {
			t.Fatal("Parse accepted SOURCE after prose")
		}
		if !strings.Contains(err.Error(), "SOURCE") || !strings.Contains(err.Error(), "line 3") {
			t.Fatalf("refusal does not name SOURCE and line 3: %v", err)
		}
	})

	t.Run("NOTE and example keys within prose stay opaque", func(t *testing.T) {
		t.Parallel()
		text := "RESULT: x sha=0123456789ab tier: pro\n" +
			"REPO: r\n" +
			"prose\n" +
			"NOTE: this is a note\n" +
			"EXAMPLE: an example key\n"
		rec, err := Parse([]byte(text))
		if err != nil {
			t.Fatalf("Parse refused opaque prose keys: %v", err)
		}
		if got := cardBody(rec); string(got) != "prose\nNOTE: this is a note\nEXAMPLE: an example key\n" {
			t.Errorf("body = %q", got)
		}
	})
}

// TestParseRefusals pins the RESULT, duplicate, enum, sha, deadline, list and
// overflow refusals as errors with a nil Record (card HEADER MAPPING; card
// STEP 3).
func TestParseRefusals(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		text string
		want string
	}{
		{"not a RESULT line", "REPO: r\n", "RESULT"},
		{"short sha", "RESULT: x sha=abc tier: pro\n", "sha"},
		{"bad tier", "RESULT: x sha=0123456789ab tier: bogus\n", "tier"},
		{"bad priority", "RESULT: x sha=0123456789ab tier: pro\nPRIORITY: bogus\n", "priority"},
		{"duplicate header", "RESULT: x sha=0123456789ab tier: pro\nREPO: a\nREPO: b\n", "duplicate"},
		{"tier disagrees", "RESULT: x sha=0123456789ab tier: pro\ntier: heavy\n", "disagrees"},
		{"bad deadline", "RESULT: x sha=0123456789ab tier: pro\nDEADLINE: tomorrow\n", "DEADLINE"},
		{"deadline out of range", "RESULT: x sha=0123456789ab tier: pro\nDEADLINE: finish within 100001 minutes.\n", "DEADLINE"},
		{"over-bound id", "RESULT: " + strings.Repeat("i", 65) + " sha=0123456789ab tier: pro\n", "id"},
		{"over-bound repo", "RESULT: x sha=0123456789ab tier: pro\nREPO: " + strings.Repeat("r", 97) + "\n", "REPO"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec, err := Parse([]byte(tc.text))
			if err == nil {
				t.Fatalf("Parse accepted %q", tc.text)
			}
			if rec != nil {
				t.Fatalf("refusal returned a non-nil Record")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refusal %q does not name %q", err, tc.want)
			}
		})
	}

	t.Run("body overflow", func(t *testing.T) {
		t.Parallel()
		text := "RESULT: x sha=0123456789ab tier: pro\n" + strings.Repeat("b", MaxBodyBytes+1) + "\n"
		if _, err := Parse([]byte(text)); err == nil {
			t.Fatal("Parse accepted a body past the ceiling")
		}
	})
	t.Run("extras overflow", func(t *testing.T) {
		t.Parallel()
		var b strings.Builder
		b.WriteString("RESULT: x sha=0123456789ab tier: pro\n")
		for i := 0; i <= MaxListEntries; i++ {
			b.WriteString("K" + strings.Repeat("x", i%10) + ": v\n")
		}
		if _, err := Parse([]byte(b.String())); err == nil {
			t.Fatal("Parse accepted extra entries past the ceiling")
		}
	})
}

// TestParsePathEmptySpellings pins PATHS `none`, `-` and empty as empty lists
// through Parse and Render, and never a literal scope entry (card HEADER
// MAPPING).
func TestParsePathEmptySpellings(t *testing.T) {
	t.Parallel()
	for _, spelling := range []string{"none", "-", "", "   "} {
		spelling := spelling
		t.Run(spelling, func(t *testing.T) {
			t.Parallel()
			text := "RESULT: x sha=0123456789ab tier: pro\nPATHS: " + spelling + "\nbody\n"
			rec, err := Parse([]byte(text))
			if err != nil {
				t.Fatalf("Parse(PATHS: %q): %v", spelling, err)
			}
			if got := pathList(&rec.root.Paths); len(got) != 0 {
				t.Fatalf("PATHS: %q parsed as %q, want empty", spelling, got)
			}
		})
	}
	// `none` is a literal element for the other lists, never generalized.
	rec, err := Parse([]byte("RESULT: x sha=0123456789ab tier: pro\nSHARED: none\nbody\n"))
	if err != nil {
		t.Fatalf("Parse(SHARED: none): %v", err)
	}
	if got := pathList(&rec.root.Shared); !equalStrings(got, []string{"none"}) {
		t.Fatalf("SHARED: none parsed as %q, want [none]", got)
	}
}

// TestParseBodyBoundary holds the exact body suffix across inter-header
// blanks, trailing blank suffix, multiple blanks before prose, direct prose,
// CRLF, empty body, and final-newline variants (card TEXT GRAMMAR).
func TestParseBodyBoundary(t *testing.T) {
	t.Parallel()
	const result = "RESULT: x sha=0123456789ab tier: pro\n"
	cases := []struct {
		name string
		text string
		want string
	}{
		{"inter-header blanks are structural", result + "\nREPO: r\nprose\n", "prose\n"},
		{"trailing blank suffix", result + "prose\n\n", "prose\n\n"},
		{"multiple blanks before prose", result + "\n\nprose\n", "\n\nprose\n"},
		{"direct prose", result + "prose\n", "prose\n"},
		{"CRLF terminators", "RESULT: x sha=0123456789ab tier: pro\r\nREPO: r\r\nprose\r\n", "prose\r\n"},
		{"empty body", result, ""},
		{"no final newline", result + "prose", "prose"},
		{"final newline", result + "prose\n", "prose\n"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec, err := Parse([]byte(tc.text))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got := string(cardBody(rec)); got != tc.want {
				t.Errorf("body = %q, want %q", got, tc.want)
			}
		})
	}
}

func pathList(list *TableList[PathTextRow]) []string {
	var out []string
	CardPathsEach(list, func(e *PathTextRow) bool {
		out = append(out, cardString(e.S[:], e.SLength))
		return true
	})
	return out
}

func idList(list *TableList[CardIDRow]) []string {
	var out []string
	CardNeedsEach(list, func(e *CardIDRow) bool {
		out = append(out, cardString(e.S[:], e.SLength))
		return true
	})
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalExtra(a, b []extraKV) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
