// Package cardsexample is the separate customer module for the cards facade:
// it imports the generated `cards` package and the Go standard library only,
// proving a local adoption pattern (docs/CARDS.md). It is a source-template
// module boundary, not a published module.
package cardsexample

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"cards"
)

// TestCardsCustomerExample exercises the documented facade: a checked parse,
// defensive wire ownership, strict versus tolerant reads, atomic edits, the
// task hash's field choice, a historical upgrade and a lossy future probe
// (docs/CARDS.md, docs/SPEC-TABLES.md §4, §5, §6.2, §6.6).
func TestCardsCustomerExample(t *testing.T) {
	t.Parallel()

	sourceRoot := os.Getenv("CARDS_SOURCE_ROOT")
	if sourceRoot == "" {
		t.Fatal("CARDS_SOURCE_ROOT is not set")
	}
	futurePath := os.Getenv("CARDS_FUTURE_WIRE")
	if futurePath == "" {
		t.Fatal("CARDS_FUTURE_WIRE is not set")
	}

	// BEGIN CARDS-CUSTOMER-EXAMPLE
	rec, err := cards.Parse([]byte(
		"RESULT: probe-card sha=0123456789ab tier: pro\n" +
			"REPO: mas-bandwidth/schema\n" +
			"PATHS: tables/cards\n" +
			"DEPENDS-ON: dep-alpha\n" +
			"TEST: TestCardsCustomerExample\n" +
			"\ndistinctive body line\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// END CARDS-CUSTOMER-EXAMPLE

	body := []byte("\ndistinctive body line\n")

	// Wire() owns a defensive copy: mutating it cannot change stored bytes.
	before := rec.Wire()
	mutated := rec.Wire()
	mutated[0] ^= 0xff
	if bytes.Equal(mutated, rec.Wire()) {
		t.Fatal("Wire() returned a copy that aliases the stored wire")
	}
	if !bytes.Equal(rec.Wire(), before) {
		t.Fatal("mutating a Wire() copy changed the stored wire")
	}

	// Load is tolerant and preserves the input; ReadStrict accepts a clean read.
	loaded, report, err := cards.Load(rec.Wire())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if report.Unknown != 0 || report.Malformed {
		t.Fatalf("clean read reported loss: %+v", report)
	}
	strict, err := cards.ReadStrict(rec.Wire())
	if err != nil {
		t.Fatalf("ReadStrict refused a clean read: %v", err)
	}
	if strict == nil || loaded == nil {
		t.Fatal("Load/ReadStrict returned nil record")
	}

	// Render preserves the stored body suffix byte for byte.
	text, _, err := cards.Render(strict)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.HasSuffix(text, body) {
		t.Fatalf("Render did not preserve the body suffix: %q", text)
	}

	// ToJSON is a checked known-field view; FromJSON reconstructs the same task.
	json, jreport, err := cards.ToJSON(strict)
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	if jreport.Malformed {
		t.Fatalf("ToJSON report is malformed: %+v", jreport)
	}
	if !bytes.Contains(json, []byte("distinctive body line")) || !bytes.Contains(json, []byte("pro")) {
		t.Fatalf("ToJSON text is missing expected fields/body: %s", json)
	}
	back, err := cards.FromJSON(json)
	if err != nil {
		t.Fatalf("FromJSON: %v", err)
	}
	backText, _, err := cards.Render(back)
	if err != nil {
		t.Fatalf("Render(FromJSON): %v", err)
	}
	if !bytes.HasSuffix(backText, body) {
		t.Fatalf("FromJSON did not reconstruct the body: %q", backText)
	}

	// TaskHash covers body, paths, new, shared, test and done only: needs,
	// tier, priority, column and holder leave it untouched; paths move it.
	base, err := cards.TaskHash(strict)
	if err != nil {
		t.Fatalf("TaskHash: %v", err)
	}
	preserves := []struct {
		name string
		do   func() error
	}{
		{"SetNeeds", func() error { return cards.SetNeeds(strict, []string{"dep-alpha"}) }},
		{"SetTier", func() error { return cards.SetTier(strict, cards.Tierheavy) }},
		{"SetPriority", func() error { return cards.SetPriority(strict, cards.Priorityhigh) }},
		{"SetColumn", func() error { return cards.SetColumn(strict, cards.Columnworking) }},
		{"SetHolder", func() error { return cards.SetHolder(strict, "runner") }},
	}
	for _, p := range preserves {
		if err := p.do(); err != nil {
			t.Fatalf("%s: %v", p.name, err)
		}
		got, err := cards.TaskHash(strict)
		if err != nil {
			t.Fatalf("%s: TaskHash: %v", p.name, err)
		}
		if got != base {
			t.Fatalf("%s changed the task hash", p.name)
		}
	}
	if err := cards.SetPaths(strict, []string{"tables/cards/other"}); err != nil {
		t.Fatalf("SetPaths: %v", err)
	}
	moved, err := cards.TaskHash(strict)
	if err != nil {
		t.Fatalf("TaskHash after SetPaths: %v", err)
	}
	if moved == base {
		t.Fatal("SetPaths did not change the task hash")
	}

	// An over-bound setter refuses and leaves the owned wire unchanged.
	beforeRefusal := strict.Wire()
	if err := cards.SetPaths(strict, []string{strings.Repeat("a", 193)}); err == nil {
		t.Fatal("SetPaths accepted a 193-byte element past the 192-byte bound")
	}
	if !bytes.Equal(strict.Wire(), beforeRefusal) {
		t.Fatal("a refused setter changed the owned wire")
	}

	// A shipped historical fixture upgrades, preserves values and is idempotent.
	hist, err := os.ReadFile(filepath.Join(sourceRoot, "tables", "cards", "corpus", "initial", "populated.wire"))
	if err != nil {
		t.Fatalf("read historical fixture: %v", err)
	}
	up, ureport, err := cards.Upgrade(hist)
	if err != nil {
		t.Fatalf("Upgrade(historical): %v (report %+v)", err, ureport)
	}
	if ureport.RetainLost != 0 || ureport.Malformed {
		t.Fatalf("Upgrade(historical) reported loss: %+v", ureport)
	}
	up2, _, err := cards.Upgrade(up)
	if err != nil {
		t.Fatalf("Upgrade(historical) second pass: %v", err)
	}
	if !bytes.Equal(up, up2) {
		t.Fatal("Upgrade(historical) is not idempotent")
	}

	// The c03 future probe is lossy for the current declaration: Load counts the
	// unknown, ReadStrict refuses it, and Upgrade retains it without claiming it
	// means newer (docs/SPEC-TABLES.md §4, §6.6).
	future, err := os.ReadFile(futurePath)
	if err != nil {
		t.Fatalf("read future probe: %v", err)
	}
	if _, freport, err := cards.Load(future); err != nil {
		t.Fatalf("Load(future): %v", err)
	} else if freport.Unknown == 0 {
		t.Fatalf("Load(future) did not report an unknown field: %+v", freport)
	}
	if _, err := cards.ReadStrict(future); err == nil {
		t.Fatal("ReadStrict accepted a lossy future probe")
	}
	if out, freport, err := cards.Upgrade(future); err != nil {
		t.Fatalf("Upgrade(future): %v (report %+v)", err, freport)
	} else if len(out) == 0 {
		t.Fatal("Upgrade(future) returned no retained bytes")
	}
}
