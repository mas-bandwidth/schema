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

// TestParseRenderCorpus parses the normalized seed, asserts every mapped header
// value and the exact body suffix, renders it back byte-identically, and proves
// the parse-render-parse round trip keeps the owned wire and a JSON round trip
// keeps every field and the body with a clean report (card STEP 3, STEP 4).
func TestParseRenderCorpus(t *testing.T) {
	t.Parallel()
	fixture := readFixture(t, "normalized-valid.md")
	rec, err := Parse(fixture)
	if err != nil {
		t.Fatalf("Parse(normalized-valid.md): %v", err)
	}
	root := rec.root
	if got := cardString(root.Id[:], root.IdLength); got != "a-cut-ignores-the-previous-tags-cancelled-release-run" {
		t.Errorf("id = %q", got)
	}
	if got := cardString(root.Sha[:], root.ShaLength); got != "000000000000" {
		t.Errorf("sha = %q", got)
	}
	if root.Tier != Tierpro {
		t.Errorf("tier = %v", root.Tier)
	}
	if got := cardString(root.Repo[:], root.RepoLength); got != "mas-bandwidth/nova-tools" {
		t.Errorf("repo = %q", got)
	}
	if got := cardString(root.Base[:], root.BaseLength); got != "dev" {
		t.Errorf("base = %q", got)
	}
	if got := cardString(root.Kind[:], root.KindLength); got != "" {
		t.Errorf("kind = %q, want empty default", got)
	}
	if root.Priority != Priorityhigh {
		t.Errorf("priority = %v", root.Priority)
	}
	wantPaths := []string{"pkg/release/ci.go", "pkg/release/ci_test.go", "pkg/release/release.go", "cmd/nova-update/README.md", "docs/fixes.sexp", "FIXES.md"}
	if got := pathList(&root.Paths); !equalStrings(got, wantPaths) {
		t.Errorf("paths = %q, want %q", got, wantPaths)
	}
	if got := pathList(&root.New); len(got) != 0 {
		t.Errorf("new = %q, want empty", got)
	}
	wantShared := []string{"docs/fixes.sexp", "FIXES.md"}
	if got := pathList(&root.Shared); !equalStrings(got, wantShared) {
		t.Errorf("shared = %q, want %q", got, wantShared)
	}
	if got := idList(&root.Needs); len(got) != 0 {
		t.Errorf("needs = %q, want empty", got)
	}
	if root.DoneWhen != 0 {
		t.Errorf("done_when = %q, want absent", stringData(&root.DoneWhen, nil))
	}
	if root.DeadlineMinutes != 0 {
		t.Errorf("deadline_minutes = %d, want 0", root.DeadlineMinutes)
	}
	if root.Source == 0 {
		t.Error("source is absent, want the SOURCE value")
	} else if got := string(stringData(&root.Source, nil)); got != fixtureField(t, fixture, "SOURCE") {
		t.Errorf("source = %q", got)
	}
	if got := extraPairs(rec); len(got) != 0 {
		t.Errorf("extra = %q, want empty", got)
	}
	wantBody := bodyAfter(t, fixture, "SOURCE")
	if got := cardBody(rec); !bytes.Equal(got, wantBody) {
		t.Errorf("body suffix differs: got %d bytes, want %d", len(got), len(wantBody))
	}

	text, report, err := Render(rec)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.Equal(text, fixture) {
		t.Fatalf("Render does not reproduce the canonical seed:\n%s\n--- vs ---\n%s", text, fixture)
	}
	if report.Malformed {
		t.Errorf("Render report is malformed: %+v", report)
	}

	rec2, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse(Render(...)): %v", err)
	}
	if !bytes.Equal(rec2.Wire(), rec.Wire()) {
		t.Fatal("parse-render-parse round trip changed the owned wire")
	}
	text3, _, err := Render(rec2)
	if err != nil {
		t.Fatalf("second Render: %v", err)
	}
	if !bytes.Equal(text3, text) {
		t.Fatal("repeated round trip grew or rewrote the text")
	}

	json, _, err := ToJSON(rec)
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	rec3, err := FromJSON(json)
	if err != nil {
		t.Fatalf("FromJSON(ToJSON(...)): %v", err)
	}
	if !bytes.Equal(rec3.Wire(), rec.Wire()) {
		t.Fatal("JSON round trip changed the owned wire")
	}
	if got := cardBody(rec3); !bytes.Equal(got, wantBody) {
		t.Error("JSON round trip changed the body")
	}
}

// TestParseExtrasAndDeadline pins the unmapped keys (extras) in arrival order,
// DONE-WHEN, DEADLINE conversion, and the separate tier line's agreement with
// RESULT, through Parse and Render (card HEADER MAPPING).
func TestParseExtrasAndDeadline(t *testing.T) {
	t.Parallel()
	text := "RESULT: extra-card sha=0123456789ab tier: pro\n" +
		"REPO: repo\n" +
		"LEG: go\n" +
		"START: 2026-01-01\n" +
		"TOOLCHAIN: rust\n" +
		"DONE-WHEN: make cards-test\n" +
		"DEADLINE: finish within 90 minutes.\n" +
		"tier: pro\n" +
		"prose line one\n"
	rec, err := Parse([]byte(text))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got := extraPairs(rec)
	want := []extraKV{
		{key: "LEG", value: "go"},
		{key: "START", value: "2026-01-01"},
		{key: "TOOLCHAIN", value: "rust"},
	}
	if !equalExtra(got, want) {
		t.Errorf("extras = %+v, want %+v", got, want)
	}
	if rec.root.DoneWhen == 0 || string(stringData(&rec.root.DoneWhen, nil)) != "make cards-test" {
		t.Errorf("done_when = %q", stringData(&rec.root.DoneWhen, nil))
	}
	if rec.root.DeadlineMinutes != 90 {
		t.Errorf("deadline_minutes = %d, want 90", rec.root.DeadlineMinutes)
	}
	if got := cardBody(rec); string(got) != "prose line one\n" {
		t.Errorf("body = %q", got)
	}

	rendered, _, err := Render(rec)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	wantRendered := "RESULT: extra-card sha=0123456789ab tier: pro\n" +
		"REPO: repo\n" +
		"DONE-WHEN: make cards-test\n" +
		"DEADLINE: finish within 90 minutes.\n" +
		"LEG: go\n" +
		"START: 2026-01-01\n" +
		"TOOLCHAIN: rust\n" +
		"prose line one\n"
	if string(rendered) != wantRendered {
		t.Errorf("Render:\n%s\n--- want ---\n%s", rendered, wantRendered)
	}
	if rec2, err := Parse(rendered); err != nil {
		t.Fatalf("Parse(Render(...)): %v", err)
	} else if !bytes.Equal(rec2.Wire(), rec.Wire()) {
		t.Fatal("extras round trip changed the wire")
	}
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
