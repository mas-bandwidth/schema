package jstable

import "testing"

// TestMessageSlotsIndexedOncePerModule holds the write path's slot lookup to a
// step budget, never to a clock. Every field, arm and enum variant the message
// writer emits asks for its vocabulary slot; when each ask rebuilt the message
// vocabulary (tablewire.MessageVocabulary walks the closure and the whole
// unit vocabulary), generating test/tables/announce4096 — one enum of 4095
// variants — rebuilt it once per variant, about 30 CPU-seconds per unit, and
// TestUnionClassesImportTheTablesTheyBuild ran for tens of minutes on a loaded
// bench. The vocabulary is now indexed once per module: the count of builds
// does not grow with the enum, and generating the 4096-entry unit builds the
// index no more often than a unit with a single variant does.
func TestMessageSlotsIndexedOncePerModule(t *testing.T) {
	builds := func(paths ...string) int64 {
		t.Helper()
		u := loadUnitPaths(t, paths)
		before := messageSlotTableBuilds.Load()
		if _, err := Generate(u); err != nil {
			t.Fatalf("%v: Generate: %v", paths, err)
		}
		return messageSlotTableBuilds.Load() - before
	}
	big := builds(repoPath("test/tables/announce4096"))
	wide := builds(repoPath("test/tables/announce4097"))
	if big < 1 {
		t.Fatalf("announce4096 built the message slot index %d times: the write path no longer asks it, so this budget measures nothing", big)
	}
	// One index per generated module that writes a message; the unit emits
	// at most its file module and its home module.
	const budget = 2
	if big > budget || wide > budget {
		t.Errorf("the message slot index was built %d (announce4096) and %d (announce4097) times; the budget is %d per unit, one per module — a per-lookup rebuild is quadratic in the vocabulary", big, wide, budget)
	}
}
