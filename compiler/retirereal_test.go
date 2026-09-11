// THE ONE REAL RETIREMENT (Rowan's ruling B, 2026-09-11), asserted on the
// COMMITTED lock of a unit in this repository rather than on a fixture.
//
// Every floor test in the tree until now played the retirement: the per-leg
// versioning probes hand their backend a lineage with `Retired` set by hand
// (`runVersionProbeRetired`'s `retire int`), and the C++ reference moves its
// floor with `T##FixedSetFloorForTest` under a test-only define. Those prove an
// emitter and a runtime. They prove NOTHING about the path an operator walks:
// `schema lock --retire`, a file on disk, and nine regenerated legs.
//
// So this file's fixture is `tables/examples` AS COMMITTED — `KeyedConfig`
// widened by one appended field so its lineage is two entries, then the OLDEST
// entry retired with `schema lock --retire KeyedConfig@0x39fbd26a1a70582b`. The
// walk is docs/SPEC-TABLES.md §21.7; the row is
// docs/FIXED-FORM-VERSIONING-TESTS.md's `retired_keyed`.
package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
)

// The three facts of the retirement, as the committed lock holds them. They are
// written out rather than computed so that a change to the lock that moves any
// of them lands HERE, in one sentence, and not as eight confusing per-leg
// failures.
const (
	retiredRealTable = "KeyedConfig"
	// the OLDEST entry: retired, and STILL KNOWN — that is the difference
	// between `layout_unsupported` and `layout_newer`
	retiredRealRetiredHash = uint64(0x39fbd26a1a70582b)
	// the CURRENT entry, which still reads
	retiredRealCurrentHash = uint64(0xa357cf9e47e391b6)
	// one past the highest retired index
	retiredRealFloor = 1
)

// retiredRealLoad locates `tables/examples` exactly as `schema generate` does.
// The PREMISE every assertion below rests on — two entries, the older retired
// with a reason, the newer not, the floor at 1 — is checked once, in
// [TestRealRetirementIsInTheCommittedLock], so no leg has to restate it.
func retiredRealLoad(t *testing.T) (*Compiler, []string) {
	t.Helper()
	dir := filepath.Join("..", "tables", "examples")
	if _, err := os.Stat(filepath.Join(dir, lockfile.FileName)); err != nil {
		t.Fatalf("this test's fixture is the COMMITTED lock at %s: %v", dir, err)
	}
	paths, err := GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	return New(), paths
}

// TestRealRetirementIsInTheCommittedLock is the premise: the lock on disk says
// what §21.7 says it says. Everything else in this file reads from it.
func TestRealRetirementIsInTheCommittedLock(t *testing.T) {
	_, paths := retiredRealLoad(t)
	lock, ok, err := lockfile.Open(paths)
	if err != nil || !ok {
		t.Fatalf("tables/examples carries a committed lock: ok=%v err=%v", ok, err)
	}
	entries := lockfile.Lineage(lock, retiredRealTable)
	if len(entries) != 2 {
		t.Fatalf("%s's lineage is two entries — one widening, one retirement: %d", retiredRealTable, len(entries))
	}
	if entries[0].Wire != retiredRealRetiredHash || !entries[0].Retired {
		t.Errorf("the OLDEST entry is 0x%016x and RETIRED: %+v", retiredRealRetiredHash, entries[0])
	}
	if strings.TrimSpace(entries[0].Reason) == "" {
		t.Error("a retirement carries the operator's sentence — that is what makes it a declaration (bill §11.4)")
	}
	if entries[1].Wire != retiredRealCurrentHash || entries[1].Retired {
		t.Errorf("the CURRENT entry is 0x%016x and is never retired: %+v", retiredRealCurrentHash, entries[1])
	}
	if f := lockfile.Floor(lock, retiredRealTable); f != retiredRealFloor {
		t.Errorf("the floor is one past the highest retired index: %d, want %d", f, retiredRealFloor)
	}
	// AND THE RETIRED HASH IS STILL IN THE LINEAGE. A deletion would have
	// removed it and a file carrying it would answer `layout_newer` — "ship the
	// reader" — which points the operator the wrong way.
	if lockfile.Lineage(lock, retiredRealTable)[0].Wire != retiredRealRetiredHash {
		t.Error("a retired entry STAYS in the lineage forever (bill §11.4)")
	}
}

// retiredRealFloorNeedle is the floor constant as a leg writes it, read off a
// NORMALIZED copy of the source — lower-cased with `_` removed — so one needle
// covers `KeyedConfigFixedFloor`, `keyed_config_fixed_floor`,
// `KEYED_CONFIG_FIXED_FLOOR`, `keyedConfigFixedFloor` and elixir's
// `@keyed_config_floor` without eight spellings to keep in step with eight
// emitters. The STATEMENT is the same in every language: the floor this unit's
// lock implies is 1, and a leg that carries 0 is serving a layout its operator
// retired.
var retiredRealFloorNeedle = regexp.MustCompile(`keyedconfig(?:fixed)?floor[^0-9]{0,32}1\b`)

func retiredRealNormalize(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "_", ""))
}

// TestRealRetirementIsShippedByEveryTargetThatTakesIt is the ruling's per-leg
// column, made on the bytes `Compiler.Generate` returns for the REAL unit:
//
//  1. the RETIRED hash is in the emitted static data — still KNOWN, so LOAD
//     answers `layout_unsupported` ("upgrade the client") and never
//     `layout_newer` ("ship the reader");
//  2. the CURRENT hash is there, so a file under the newest entry still reads;
//  3. the FLOOR the lock implies, 1, is the floor the leg compiled in.
//
// Three assertions and not two: (1) and (3) together are the retirement, and
// (1) alone is satisfied by a leg that kept the entry and never moved its floor.
//
// The targets are [fixedLineageShipTargets] — every target whose table backend
// takes the lineage from the driver. **The JAVA leg is NOT among them and is not
// asserted about**: `internal/codegen/javatable` has no lineage, no floor and no
// `layout_unsupported` at all (its fixed form reads its own records only), so
// there is nothing for a retirement to reach. That is a gap in the port and it
// is named here rather than skipped silently.
func TestRealRetirementIsShippedByEveryTargetThatTakesIt(t *testing.T) {
	c, paths := retiredRealLoad(t)
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	lock, _, err := lockfile.Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	reason := lockfile.Lineage(lock, retiredRealTable)[0].Reason

	for _, target := range fixedLineageShipTargets {
		t.Run(target, func(t *testing.T) {
			files, err := c.Generate(u, target, nil)
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			// EVERY emitted file, subdirectories included: the Go leg writes
			// its module into a package directory, so a loop that skipped a
			// name with a `/` in it read an empty string and passed nothing.
			var all, norm strings.Builder
			for _, data := range files {
				all.Write(data)
				norm.WriteString(retiredRealNormalize(string(data)))
			}
			src, normalized := all.String(), norm.String()

			// A LEG THAT EMITS NO FIXED FORM FOR THIS UNIT AT ALL is named and
			// not failed. `tables/examples` carries a REGIONAL table
			// (`Guarded.schema`'s `Patrol`), and a regional unit skips the form
			// entirely in some legs (`gotable.hasFixedForm`: "a regional unit
			// skips the form entirely, so a mixed unit's fixed-size tables keep
			// form-1 Load"). There is no known-layout table to carry a
			// retirement into, so the statement this test makes cannot be made
			// here — and it is SAID, with where the coverage is owed, rather
			// than skipped quietly (§5.9 #23).
			// The detector is the TABLE's own data and never the shared
			// runtime: every leg emits `TableFixedKnownLayout` (the type) into a
			// unit that carries no fixed form at all, so a grep for the type
			// reads as "the form is here" when nothing of it is.
			if !strings.Contains(src, fixedLineageHashSpelling(target, retiredRealRetiredHash)) &&
				!strings.Contains(src, fixedLineageHashSpelling(target, retiredRealCurrentHash)) &&
				!strings.Contains(normalized, retiredRealNormalize(retiredRealTable)+"fixedknown") {
				t.Skipf("this leg emits NO fixed-form known-layout table for tables/examples — the unit is regional for it, so the form is not emitted and a retirement has nothing to reach. THE COVERAGE IS OWED at this leg's own versioning gate (`make tables-%s-versioning`, the `floor_below` row), which plays the retirement through GenerateLineage; what is NOT covered anywhere for this leg is the REAL lock driving it, and that is this skip's debt", target)
			}

			// (1) the retired hash is STILL KNOWN
			if !strings.Contains(src, fixedLineageHashSpelling(target, retiredRealRetiredHash)) {
				t.Errorf("the emitted source carries no entry for the RETIRED layout 0x%016x — a retirement KEEPS the entry, and a leg that dropped it answers `layout_newer` where the bill says `layout_unsupported` (bill §11.4)", retiredRealRetiredHash)
			}
			// (2) the current hash reads
			if !strings.Contains(src, fixedLineageHashSpelling(target, retiredRealCurrentHash)) {
				t.Errorf("the emitted source carries no entry for the CURRENT layout 0x%016x", retiredRealCurrentHash)
			}
			// (3) the floor MOVED
			if !retiredRealFloorNeedle.MatchString(normalized) {
				t.Errorf("the emitted source does not carry %s's floor as %d — the retirement reached the known-layout table and not the floor, so this leg still SERVES the layout its operator retired (docs/FIXED-FORM-ALGORITHM.md §5.2)", retiredRealTable, retiredRealFloor)
			}
			// AND THE OPERATOR'S SENTENCE RIDES ALONG where the leg emits it,
			// which is the fact that makes a generated file readable by the
			// person who has to answer "why does my client refuse?". It is a
			// soft assertion — a leg that emits no comment is not wrong — so it
			// is reported and never failed.
			if !strings.Contains(src, reason) {
				t.Logf("note: %s emits no RETIRED reason comment beside the entry (not a failure; the reason is in the lock)", target)
			}
		})
	}
}

// TestRealRetirementNamesTheFileHash is the last third of the ruling, asserted
// where it can be asserted without a toolchain: the refusal a leg writes for a
// hash below the floor carries THE FILE'S HASH. Every leg's generated LOAD does
// this through one call — `tableFixedRefuseHash`, `report->layout_hash = hash`,
// `refuse_layout(..., hash)` — and the shape is checked here so a leg that
// refuses with a bare name is caught by the compiler's own suite and not only by
// a runtime probe that may be skipped for a missing toolchain.
func TestRealRetirementNamesTheFileHash(t *testing.T) {
	c, paths := retiredRealLoad(t)
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range fixedLineageShipTargets {
		t.Run(target, func(t *testing.T) {
			files, err := c.Generate(u, target, nil)
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			var all strings.Builder
			for _, data := range files {
				all.Write(data)
			}
			src := all.String()
			if !strings.Contains(src, fixedLineageHashSpelling(target, retiredRealRetiredHash)) &&
				!strings.Contains(src, fixedLineageHashSpelling(target, retiredRealCurrentHash)) {
				t.Skipf("no fixed form is emitted for tables/examples on this leg (see the sibling test): the refusal site is not in these bytes to read")
			}
			// the NAME, spelled as the leg spells a refusal reason
			named := strings.Contains(src, "layout_unsupported") ||
				strings.Contains(src, "LayoutUnsupported") ||
				strings.Contains(src, "layoutUnsupported") ||
				strings.Contains(src, "LAYOUT_UNSUPPORTED")
			if !named {
				t.Errorf("the emitted LOAD never names `layout_unsupported` — a file under a retired entry would answer something else, and the two names point the operator in opposite directions (bill §11.4)")
			}
			// and the hash RIDES ON IT: the refusal site takes the file's hash
			// as an argument or assigns it
			// the spellings, one per leg family: C/Go/JS call a refuse helper
			// with the hash, C++/C# assign `layout_hash = hash`, Rust calls
			// `refuse_layout(..., hash)`, and ELIXIR merges the file's hash into
			// the report map under its own local name (`layout_hash:
			// file_hash`), which is why the right-hand side is not pinned to the
			// word `hash`.
			carries := regexp.MustCompile(`(?i)(refuse_?layout|refuse_?hash)[^\n]*hash|layout_?hash\s*[:=]\s*\w*hash`)
			if !carries.MatchString(src) {
				t.Errorf("the refusal does not carry the FILE's hash — `%s` without the eight bytes tells the operator nothing about which client to upgrade (bill §12.4)", "layout_unsupported")
			}
		})
	}
	_ = fmt.Sprint
}
