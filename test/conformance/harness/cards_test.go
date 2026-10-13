package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCardsCorpus builds a small card corpus in dir: a manifest naming two
// surfaces (render, one case; load, one case) and the expected answers.
func writeCardsCorpus(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "expected"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "render a x\nload b y\n"
	if err := os.WriteFile(filepath.Join(dir, "MANIFEST.txt"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "expected", "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "expected", "b.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "expected", "b.report"), []byte("Retained=0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// goodCardsDriver answers both surfaces of the corpus with the exact expected
// bytes.
func goodCardsDriver(manifest, surface, outdir string) int {
	switch surface {
	case "render":
		_ = os.WriteFile(filepath.Join(outdir, "a.txt"), []byte("hello\n"), 0o644)
	case "load":
		_ = os.WriteFile(filepath.Join(outdir, "b.json"), []byte("{}\n"), 0o644)
		_ = os.WriteFile(filepath.Join(outdir, "b.report"), []byte("Retained=0\n"), 0o644)
	}
	return 0
}

// TestCardsCheckComparesEverySurface drives an in-process fake driver (no
// subprocess, no network, no clock) through the four outcomes the check must
// distinguish: a clean pass, one changed byte, a missing answer, and exit 2.
func TestCardsCheckComparesEverySurface(t *testing.T) {
	t.Parallel()

	t.Run("pass", func(t *testing.T) {
		corpus := t.TempDir()
		writeCardsCorpus(t, corpus)
		var buf bytes.Buffer
		ok, err := runCards(&buf, corpus, filepath.Join(corpus, "expected"), "", "", t.TempDir(), goodCardsDriver)
		if err != nil {
			t.Fatalf("runCards: %v", err)
		}
		if !ok {
			t.Fatalf("a driver answering every expected byte was not green:\n%s", buf.String())
		}
		for _, want := range []string{"cards render pass 1/1", "cards load pass 1/1"} {
			if !strings.Contains(buf.String(), want) {
				t.Fatalf("output %q does not contain %q", buf.String(), want)
			}
		}
	})

	t.Run("one changed byte", func(t *testing.T) {
		corpus := t.TempDir()
		writeCardsCorpus(t, corpus)
		driver := func(manifest, surface, outdir string) int {
			if surface == "render" {
				_ = os.WriteFile(filepath.Join(outdir, "a.txt"), []byte("hEllo\n"), 0o644)
				return 0
			}
			return goodCardsDriver(manifest, surface, outdir)
		}
		var buf bytes.Buffer
		ok, err := runCards(&buf, corpus, filepath.Join(corpus, "expected"), "", "", t.TempDir(), driver)
		if err != nil {
			t.Fatalf("runCards: %v", err)
		}
		if ok {
			t.Fatalf("a changed byte stayed green:\n%s", buf.String())
		}
		if !strings.Contains(buf.String(), "cards render FAIL") || !strings.Contains(buf.String(), "byte 1") {
			t.Fatalf("the changed byte did not name render and its offset:\n%s", buf.String())
		}
		if !strings.Contains(buf.String(), "cards load pass 1/1") {
			t.Fatalf("the untouched surface did not stay green:\n%s", buf.String())
		}
	})

	t.Run("missing answer", func(t *testing.T) {
		corpus := t.TempDir()
		writeCardsCorpus(t, corpus)
		driver := func(manifest, surface, outdir string) int {
			if surface == "render" {
				return 0 // writes nothing
			}
			return goodCardsDriver(manifest, surface, outdir)
		}
		var buf bytes.Buffer
		ok, err := runCards(&buf, corpus, filepath.Join(corpus, "expected"), "", "", t.TempDir(), driver)
		if err != nil {
			t.Fatalf("runCards: %v", err)
		}
		if ok {
			t.Fatalf("a driver writing nothing stayed green:\n%s", buf.String())
		}
		if !strings.Contains(buf.String(), "the driver wrote nothing") {
			t.Fatalf("the missing answer did not say so:\n%s", buf.String())
		}
	})

	t.Run("exit 2", func(t *testing.T) {
		corpus := t.TempDir()
		writeCardsCorpus(t, corpus)
		driver := func(manifest, surface, outdir string) int { return 2 }
		var buf bytes.Buffer
		ok, err := runCards(&buf, corpus, filepath.Join(corpus, "expected"), "", "render,load", t.TempDir(), driver)
		if err != nil {
			t.Fatalf("runCards: %v", err)
		}
		if ok {
			t.Fatalf("a required surface answered absent stayed green:\n%s", buf.String())
		}
		if !strings.Contains(buf.String(), "cards render FAIL") {
			t.Fatalf("the absence did not name render:\n%s", buf.String())
		}
	})
}

// TestCardsCheckRefusesARequiredAbsence pins the required-absence rule at both
// grains: a whole surface answered exit 2, and a single case answered
// <case>.absent — each fails a required surface, while a non-required absence
// is printed and green.
func TestCardsCheckRefusesARequiredAbsence(t *testing.T) {
	t.Parallel()

	t.Run("whole surface", func(t *testing.T) {
		corpus := t.TempDir()
		writeCardsCorpus(t, corpus)
		driver := func(manifest, surface, outdir string) int {
			if surface == "render" {
				return 2
			}
			return goodCardsDriver(manifest, surface, outdir)
		}
		var buf bytes.Buffer
		ok, err := runCards(&buf, corpus, filepath.Join(corpus, "expected"), "", "render,load", t.TempDir(), driver)
		if err != nil {
			t.Fatalf("runCards: %v", err)
		}
		if ok {
			t.Fatalf("a required surface's exit-2 absence stayed green:\n%s", buf.String())
		}
	})

	t.Run("single case", func(t *testing.T) {
		corpus := t.TempDir()
		writeCardsCorpus(t, corpus)
		driver := func(manifest, surface, outdir string) int {
			if surface == "render" {
				_ = os.WriteFile(filepath.Join(outdir, "a.txt.absent"), nil, 0o644)
				return 0
			}
			return goodCardsDriver(manifest, surface, outdir)
		}
		var buf bytes.Buffer
		ok, err := runCards(&buf, corpus, filepath.Join(corpus, "expected"), "", "render,load", t.TempDir(), driver)
		if err != nil {
			t.Fatalf("runCards: %v", err)
		}
		if ok {
			t.Fatalf("a required surface's per-case absence stayed green:\n%s", buf.String())
		}
		if !strings.Contains(buf.String(), "cards render FAIL") {
			t.Fatalf("the per-case absence did not name render:\n%s", buf.String())
		}
	})

	t.Run("non-required absence is green", func(t *testing.T) {
		corpus := t.TempDir()
		writeCardsCorpus(t, corpus)
		driver := func(manifest, surface, outdir string) int {
			if surface == "render" {
				return 2
			}
			return goodCardsDriver(manifest, surface, outdir)
		}
		var buf bytes.Buffer
		ok, err := runCards(&buf, corpus, filepath.Join(corpus, "expected"), "", "load", t.TempDir(), driver)
		if err != nil {
			t.Fatalf("runCards: %v", err)
		}
		if !ok {
			t.Fatalf("a non-required surface's absence went red:\n%s", buf.String())
		}
		if !strings.Contains(buf.String(), "cards render absent") || !strings.Contains(buf.String(), "cards load pass 1/1") {
			t.Fatalf("the non-required absence was not printed beside a green pass:\n%s", buf.String())
		}
	})
}
