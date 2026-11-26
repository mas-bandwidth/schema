package rubystrict

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// rubyLintCopy returns src with every non-ASCII character that sits on a
// comment-only line replaced by '-', one for one. A line that is not a comment
// is returned byte for byte, so the code RuboCop reads is the generated code.
//
// WHY: RuboCop 1.92.2 turns a node into a source range by indexing the file's
// String by character offset, and Ruby indexes a String that holds any
// non-ASCII character in time linear in the offset. Every cop that looks at a
// range therefore costs the size of the file, for each node. The generated
// Ruby is non-ASCII only in comments (the genheader line's em dash, the
// runtime's section signs), and that one em dash made Layout/SpaceAroundOperators
// take 19 s on a 1 MB _fixed_data.rb that takes 0.3 s once the file is ASCII
// (strict-ruby nightly-fast, main run 45330307461). A comment's glyphs are
// nothing a cop in rubocop.yml reads: Style/AsciiComments is off in RuboCop's
// defaults and is not enabled there.
func rubyLintCopy(src []byte) []byte {
	var out strings.Builder
	out.Grow(len(src))
	for _, line := range strings.SplitAfter(string(src), "\n") {
		if !isASCII(line) && strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			for _, r := range line {
				if r >= utf8.RuneSelf {
					out.WriteByte('-')
				} else {
					out.WriteRune(r)
				}
			}
			continue
		}
		out.WriteString(line)
	}
	return []byte(out.String())
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// rubyStrictFastLintTree returns the directory RuboCop lints: rubyLintCopy of
// every path in rcPaths, under the same relative path, in a directory kept in
// the user cache and named by a hash of every path and byte in it.
//
// WHY A KEPT DIRECTORY: RuboCop's result cache is keyed by the file's absolute
// path, its mode, the config and the file's bytes. A fresh temporary directory
// each run is a new path each run, so the cache never hit. The same bytes land
// in the same directory, so RuboCop answers an unchanged file from its cache
// (3 s of CPU for a part that costs 44 s cold), and a file that changed is a
// different hash, a different directory and a cold lint. Two jobs writing one
// directory write the same bytes, each file by rename, and the marker last.
func rubyStrictFastLintTree(t *testing.T, allDir string, rcPaths []string) string {
	t.Helper()
	base, err := os.UserCacheDir()
	if err != nil {
		base = t.TempDir() // no cache directory: lint, with no kept results
	}
	base = filepath.Join(base, "schema-ruby-strict-lint")
	dir := rubyStrictFastLintTreeIn(t, base, allDir, rcPaths)
	pruneRubyStrictLintTrees(base, dir, time.Now().Add(-14*24*time.Hour))
	return dir
}

// rubyStrictFastLintTreeIn is rubyStrictFastLintTree under the directory base.
func rubyStrictFastLintTreeIn(t *testing.T, base, allDir string, rcPaths []string) string {
	t.Helper()
	copies := make([][]byte, len(rcPaths))
	h := sha256.New()
	for i, p := range rcPaths {
		src, err := os.ReadFile(filepath.Join(allDir, filepath.FromSlash(p)))
		if err != nil {
			t.Fatal(err)
		}
		copies[i] = rubyLintCopy(src)
		fmt.Fprintf(h, "%s\x00%d\x00", p, len(copies[i]))
		h.Write(copies[i])
	}
	dir := filepath.Join(base, hex.EncodeToString(h.Sum(nil))[:24])
	marker := filepath.Join(dir, ".complete")
	if _, err := os.Stat(marker); err == nil {
		return dir
	}
	for i, p := range rcPaths {
		dst := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		tmp, err := os.CreateTemp(filepath.Dir(dst), ".lint-*")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tmp.Write(copies[i]); err != nil {
			t.Fatal(err)
		}
		if err := tmp.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(tmp.Name(), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp.Name(), dst); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// pruneRubyStrictLintTrees removes the directories under base, other than keep,
// last changed before cutoff. Best effort: a directory in use by another job
// that is this old is one no job has wanted for weeks.
func pruneRubyStrictLintTrees(base, keep string, cutoff time.Time) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, e := range entries {
		p := filepath.Join(base, e.Name())
		if p == keep || !e.IsDir() {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(p)
		}
	}
}

func TestRubyLintCopy(t *testing.T) {
	t.Parallel()
	src := "# frozen_string_literal: true\n" +
		"# Schema: https://example.com — to change this file\n" +
		"  # see SPEC §8.2\n" +
		"x = \"café — not a comment\" # trailing — is code's line\n" +
		"# last, no newline —"
	got := string(rubyLintCopy([]byte(src)))
	want := "# frozen_string_literal: true\n" +
		"# Schema: https://example.com - to change this file\n" +
		"  # see SPEC -8.2\n" +
		"x = \"café — not a comment\" # trailing — is code's line\n" +
		"# last, no newline -"
	if got != want {
		t.Fatalf("rubyLintCopy:\n got %q\nwant %q", got, want)
	}
	if strings.Count(got, "\n") != strings.Count(src, "\n") || utf8.RuneCountInString(got) != utf8.RuneCountInString(src) {
		t.Fatal("rubyLintCopy changed the number of lines or characters: columns would move")
	}
	// reversed witness: a code line keeps its non-ASCII text, so a string or an
	// identifier is never rewritten to make the lint cheaper
	if !strings.Contains(got, "café — not a comment") {
		t.Fatal("rubyLintCopy rewrote a non-comment line")
	}
}

func TestRubyStrictFastLintTreeIsNamedByItsBytes(t *testing.T) {
	t.Parallel()
	all := t.TempDir()
	write := func(name, body string) {
		p := filepath.Join(all, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("u/a.rb", "# a — b\nx = 1\n")
	write("u/sub/b.rb", "y = 2\n")
	paths := []string{"u/a.rb", "u/sub/b.rb"}
	base := t.TempDir()

	d1 := rubyStrictFastLintTreeIn(t, base, all, paths)
	d2 := rubyStrictFastLintTreeIn(t, base, all, paths)
	if d1 != d2 {
		t.Fatalf("the same bytes went to two directories: %s, %s", d1, d2)
	}
	got, err := os.ReadFile(filepath.Join(d1, "u", "a.rb"))
	if err != nil || string(got) != "# a - b\nx = 1\n" {
		t.Fatalf("u/a.rb in the lint tree is %q (%v), want the ASCII copy", got, err)
	}
	if _, err := os.Stat(filepath.Join(d1, ".complete")); err != nil {
		t.Fatalf("no completion marker: %v", err)
	}
	// reversed witness: one changed byte is another directory, so RuboCop
	// never answers the new file from the old file's cached result
	write("u/sub/b.rb", "y = 3\n")
	if d3 := rubyStrictFastLintTreeIn(t, base, all, paths); d3 == d1 {
		t.Fatal("a changed file landed in the old directory")
	}
	// a path moved under the same bytes is another directory too
	if d4 := rubyStrictFastLintTreeIn(t, base, all, paths[:1]); d4 == d1 {
		t.Fatal("a smaller file set landed in the old directory")
	}
	// pruning removes what is old and keeps what it is told to
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(d1, old, old); err != nil {
		t.Fatal(err)
	}
	pruneRubyStrictLintTrees(base, "", time.Now().Add(-14*24*time.Hour))
	if _, err := os.Stat(d1); !os.IsNotExist(err) {
		t.Fatalf("an old lint tree survived pruning: %v", err)
	}
}
