package cards

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseRenderCorpus parses the normalized corpus seed, asserts independent
// expected header values and the exact body suffix, then proves
// Parse(Render(Parse(t))) reproduces the owned wire (the TEXT GRAMMAR, STRANDED
// KEYS and HEADER MAPPING paragraphs; docs/SPEC-TABLES.md §2, §6.2).
func TestParseRenderCorpus(t *testing.T) {
	t.Parallel()
	text, err := os.ReadFile(filepath.Join("testdata", "epoch17", "normalized-valid.md"))
	if err != nil {
		t.Fatalf("normalized seed is missing: %v", err)
	}
	rec, err := Parse(text)
	if err != nil {
		t.Fatalf("Parse(normalized-valid.md): %v", err)
	}
	root := rec.root
	if got := fixedString(root.Id[:], root.IdLength); got != "a-cut-ignores-the-previous-tags-cancelled-release-run" {
		t.Errorf("id = %q", got)
	}
	if got := fixedString(root.Sha[:], root.ShaLength); got != "000000000000" {
		t.Errorf("sha = %q", got)
	}
	if root.Tier != Tierpro {
		t.Errorf("tier = %v", root.Tier)
	}
	if got := fixedString(root.Repo[:], root.RepoLength); got != "mas-bandwidth/nova-tools" {
		t.Errorf("repo = %q", got)
	}
	if got := fixedString(root.Base[:], root.BaseLength); got != "dev" {
		t.Errorf("base = %q", got)
	}
	if root.Priority != Priorityhigh {
		t.Errorf("priority = %v", root.Priority)
	}
	var paths []string
	CardPathsEach(&root.Paths, func(e *PathTextRow) bool {
		paths = append(paths, fixedString(e.S[:], e.SLength))
		return true
	})
	if len(paths) != 6 || paths[0] != "pkg/release/ci.go" || paths[5] != "FIXES.md" {
		t.Errorf("paths = %v", paths)
	}
	var shared []string
	CardSharedEach(&root.Shared, func(e *PathTextRow) bool {
		shared = append(shared, fixedString(e.S[:], e.SLength))
		return true
	})
	if len(shared) != 2 || shared[0] != "docs/fixes.sexp" || shared[1] != "FIXES.md" {
		t.Errorf("shared = %v", shared)
	}
	// body is the exact suffix after SOURCE's terminator: the moved prose line
	// and everything after it, byte for byte.
	srcEnd := bytes.Index(text, []byte("SOURCE: the seat"))
	srcEnd = bytes.IndexByte(text[srcEnd:], '\n') + srcEnd + 1
	wantBody := text[srcEnd:]
	gotBody := stringData(&root.Body, nil)
	if !bytes.Equal(gotBody, wantBody) {
		t.Fatalf("body suffix differs:\n%q\n--- vs ---\n%q", gotBody, wantBody)
	}
	rendered, _, err := Render(rec)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	rec2, err := Parse(rendered)
	if err != nil {
		t.Fatalf("Parse(Render(...)): %v", err)
	}
	if !bytes.Equal(rec2.Wire(), rec.Wire()) {
		t.Fatal("Parse(Render(Parse(t))) changed the owned wire")
	}
	rendered2, _, err := Render(rec2)
	if err != nil {
		t.Fatalf("Render (second): %v", err)
	}
	if !bytes.Equal(rendered, rendered2) {
		t.Fatal("repeated round trips are not stable")
	}
}

// TestParseExtrasOrder pins opaque extra keys and their arrival order through
// Parse/Render (the HEADER MAPPING paragraph).
func TestParseExtrasOrder(t *testing.T) {
	t.Parallel()
	rec, err := Parse([]byte("RESULT: x sha=012345678901 tier: pro\nZZZ: last\nAAA: first\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var keys []string
	CardExtraEach(&rec.root.Extra, func(e *KeyValueRow) bool {
		keys = append(keys, fixedString(e.Key[:], e.KeyLength))
		return true
	})
	if len(keys) != 2 || keys[0] != "ZZZ" || keys[1] != "AAA" {
		t.Fatalf("extra order = %v, want [ZZZ AAA]", keys)
	}
}

// TestParseRefusesAStrandedKey proves a cardHeaderKeys key after prose is
// refused naming key and line, while NOTE: and other unknown example keys in
// prose are opaque and preserved (the STRANDED KEYS paragraph).
func TestParseRefusesAStrandedKey(t *testing.T) {
	t.Parallel()
	text, err := os.ReadFile(filepath.Join("testdata", "epoch17", "rejected-legacy.md"))
	if err != nil {
		t.Fatalf("rejected seed is missing: %v", err)
	}
	if _, err := Parse(text); err == nil {
		t.Fatal("Parse accepted the rejected seed")
	} else if !strings.Contains(err.Error(), "stranded key") {
		t.Fatalf("refusal did not name a stranded key: %v", err)
	}
	minimal := "RESULT: x sha=012345678901 tier: pro\nKIND: feature\nprose\nSOURCE: later\n"
	if _, err := Parse([]byte(minimal)); err == nil {
		t.Fatal("Parse accepted SOURCE after prose")
	} else if !strings.Contains(err.Error(), "SOURCE") {
		t.Fatalf("refusal did not name SOURCE: %v", err)
	}
	ok := "RESULT: x sha=012345678901 tier: pro\nprose\nNOTE: a note\nEXAMPLE-KEY: value\n"
	rec, err := Parse([]byte(ok))
	if err != nil {
		t.Fatalf("Parse refused opaque prose keys: %v", err)
	}
	body := stringData(&rec.root.Body, nil)
	if !bytes.Contains(body, []byte("EXAMPLE-KEY: value")) {
		t.Fatalf("opaque prose keys were not preserved: %q", body)
	}
}

// TestParseBodyBoundary pins inter-header blanks, trailing blank suffix,
// multiple blanks before prose, direct prose, CRLF, empty body and the
// final-newline variants (the TEXT GRAMMAR paragraph).
func TestParseBodyBoundary(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		body string
	}{
		{"empty body", "RESULT: x sha=012345678901 tier: pro\n", ""},
		{"no final newline", "RESULT: x sha=012345678901 tier: pro", ""},
		{"direct prose", "RESULT: x sha=012345678901 tier: pro\nprose\n", "prose\n"},
		{"blanks before prose", "RESULT: x sha=012345678901 tier: pro\nREPO: r\n\n\nprose\n", "\n\nprose\n"},
		{"inter-header blank", "RESULT: x sha=012345678901 tier: pro\nREPO: r\n\nBASE: b\nprose\n", "prose\n"},
		{"trailing blank suffix", "RESULT: x sha=012345678901 tier: pro\nREPO: r\n\n", "\n"},
		{"crlf", "RESULT: x sha=012345678901 tier: pro\r\nREPO: r\r\nprose\r\n", "prose\r\n"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec, err := Parse([]byte(tc.in))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			got := stringData(&rec.root.Body, nil)
			if string(got) != tc.body {
				t.Fatalf("body = %q, want %q", got, tc.body)
			}
		})
	}
}
