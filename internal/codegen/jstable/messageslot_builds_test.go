package jstable

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMessageSlotsIndexedOncePerModule holds the write path's slot lookup to a
// step budget, never to a clock. Every field, arm and enum variant the message
// writer emits asks for its vocabulary slot; when each ask rebuilt the message
// vocabulary (tablewire.MessageVocabulary walks the closure and the whole
// unit vocabulary), generating test/tables/announce4096 — one enum of 4095
// variants keying a fixed table's array — rebuilt it once per variant, about
// 30 CPU-seconds per unit, and TestUnionClassesImportTheTablesTheyBuild ran
// for tens of minutes on a loaded bench. The vocabulary is now indexed once
// per module: the count of builds does not grow with the enum.
//
// The units are announce4096's shape at 16 and 256 variants, so the planted
// per-lookup rebuild answers in moments and goes red on the count.
func TestMessageSlotsIndexedOncePerModule(t *testing.T) {
	builds := func(variants int) int64 {
		t.Helper()
		var b strings.Builder
		fmt.Fprintf(&b, "package slots%d\n\nenum K\n{\n", variants)
		for i := range variants {
			fmt.Fprintf(&b, "    V%d\n", i)
		}
		b.WriteString("}\n\nfixed table Root\n{\n    keys [K]uint8\n}\n")
		dir := filepath.Join(t.TempDir(), fmt.Sprintf("slots%d", variants))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "Slots.schema"), []byte(b.String()), 0o600); err != nil {
			t.Fatal(err)
		}
		u := loadUnitPaths(t, []string{dir})
		before := messageSlotTableBuilds.Load()
		if _, err := Generate(u); err != nil {
			t.Fatalf("%d variants: Generate: %v", variants, err)
		}
		return messageSlotTableBuilds.Load() - before
	}
	small, large := builds(16), builds(256)
	if small < 1 {
		t.Fatalf("a keyed enum built the message slot index %d times: the write path no longer asks it, so this budget measures nothing", small)
	}
	// One index per generated module that writes a message: the unit emits
	// at most its file module and its home module.
	const budget = 2
	if small > budget || large > budget || small != large {
		t.Errorf("the message slot index was built %d times for 16 variants and %d for 256; the budget is %d per unit and must not grow with the enum — a per-lookup rebuild is quadratic in the vocabulary", small, large, budget)
	}
}
