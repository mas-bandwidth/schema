// THE COMPILER SHIPS THE LOCK'S LINEAGE (docs/FIXED-FORM-ALGORITHM.md §5.2's
// COMPILE(lock, T), and §5.9 #1: the lineage reaches a backend AS DATA through
// a second entry point, and the CALLER — this package — makes the lock's three
// calls).
//
// The Java port (#920) found what this test is here to pin: every backend that
// grew `GenerateLineage` grew it for its own versioning harness, and NOTHING in
// the tree called it. `schema generate` called the plain `Generate`, so no real
// build on any leg shipped a lineage of more than one entry, however long the
// committed lock's lineage was. A port passing its own harness proved its
// emitter; it proved nothing about the compiler.
//
// So the fixture is a REAL LOCK, written the way an operator writes it — `schema
// lock`, a widening, `schema lock` again — and the assertion is made on the
// bytes `Compiler.Generate` returns: the OLDER layout's wire hash is in the
// emitted source, as static data, beside the current one. That is the only
// statement that distinguishes a backend the driver hands the lineage to from
// one it does not.
package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedLineageShipTargets are the targets whose table backend takes the
// lineage as data today: `c`, `elixir`, `go` and `rust` through `GenerateLineage`,
// and `cpp` through the lock the driver now opens for it. Every other built-in
// target's table backend has no second entry point yet (cs, dart, java, js);
// their `GenerateLineage` lives on the open port branches, and the day one
// lands its target joins this list and nothing else changes.
var fixedLineageShipTargets = []string{"c", "cpp", "elixir", "go", "rust"}

// fixedLineageHashSpelling is ONE layout hash as the target's emitted source
// writes it. It is a per-target NEEDLE and not a per-target assertion: the
// statement being made is the same for every leg — the eight bytes of the older
// layout are in the module, as static data — and only the spelling of a 64-bit
// constant differs by language.
//
// The ELIXIR leg is why this function exists here. Its generated source is `mix
// format`'s own shape, emitted that way rather than checked afterwards, and a hex
// literal there is written in UPPER case — so a lower-case needle found nothing
// in a module that carried every entry. A leg whose 64-bit constant cannot be
// spelled at all (the JavaScript leg's low/high u32 pair) joins this function the
// day it lands, and nothing else changes.
func fixedLineageHashSpelling(target string, hash uint64) string {
	if target == "elixir" {
		return fmt.Sprintf("0x%016X", hash)
	}
	return fmt.Sprintf("0x%016x", hash)
}

// TestFixedLineageEntryPointIsReachedFromTheDriver: the driver must HAVE the
// second entry point for those targets. Before #921 no caller in the tree did —
// every target answered only `Generate`, so the lock's lineage reached no
// backend from any real build.
func TestFixedLineageEntryPointIsReachedFromTheDriver(t *testing.T) {
	c := New()
	for _, target := range fixedLineageShipTargets {
		g, ok := c.gens[target]
		if !ok {
			t.Fatalf("%s is a built-in target", target)
		}
		if _, takes := g.(lineageGenerator); !takes {
			t.Errorf("%s does not take the lineage from the driver (§5.9 #1: the caller makes lockfile.Open/Lineage/Floor and hands the backend the entries)", target)
		}
	}
}

// fixedLineageShipSchema is the fixture's one file: one fixed table whose body
// is the fields given. An APPEND is the widening §5.1 permits, so locking,
// appending and locking again is the shortest path to a lineage of two.
func fixedLineageShipSchema(fields string) string {
	return "package lineageship\n\nfixed table Row\n{\n" + fields + "}\n"
}

// fixedLineageShipHash is the number the file's header and every record carry,
// computed the way every backend computes it.
func fixedLineageShipHash(t *testing.T, u *ir.Unit) uint64 {
	t.Helper()
	st := u.Tables["Row"]
	if st == nil {
		t.Fatal("the fixture declares Row")
	}
	return ir.TableFixedLayoutHash(ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st)), st)
}

// TestFixedLineageIsShippedByEveryTargetThatTakesIt is step 1 of #921: a unit
// with a LOCKED LINEAGE OF TWO, generated through the public driver, and every
// target whose table backend exports `GenerateLineage` must carry BOTH hashes
// as static data. A target still on the plain `Generate` carries one, and is
// named here rather than asserted about, because its backend has no second
// entry point yet.
func TestFixedLineageIsShippedByEveryTargetThatTakesIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Lineage.schema")
	if err := os.WriteFile(path, []byte(fixedLineageShipSchema("    a int32\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	c := New()

	// THE OPERATOR'S THREE COMMANDS: lock, widen, lock.
	first, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	older := fixedLineageShipHash(t, first)
	if _, _, err := UpdateSchemaLock(first, paths); err != nil {
		t.Fatalf("the first lock: %v", err)
	}
	if err := os.WriteFile(path, []byte(fixedLineageShipSchema("    a int32\n    b int32\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	widened, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := UpdateSchemaLock(widened, paths); err != nil {
		t.Fatalf("the second lock: %v", err)
	}
	current := fixedLineageShipHash(t, widened)
	if older == current {
		t.Fatal("the widening must change the layout hash, or the fixture proves nothing")
	}

	// THE LOCK IS THE SOURCE, and it holds two entries OLDEST FIRST. This is
	// the premise every assertion below rests on, so it is checked first.
	lock, ok, err := lockfile.Open(paths)
	if err != nil || !ok {
		t.Fatalf("the fixture's lock: ok=%v err=%v", ok, err)
	}
	lin := lockfile.Lineage(lock, "Row")
	if len(lin) != 2 || lin[0].Wire != older || lin[1].Wire != current {
		t.Fatalf("the lock holds two entries oldest first: %d entries, %v", len(lin), lin)
	}
	if f := lockfile.Floor(lock, "Row"); f != 0 {
		t.Fatalf("nothing is retired, so the floor is 0: %d", f)
	}

	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}

	// EVERY TARGET WHOSE TABLE BACKEND TAKES THE LINEAGE. cpp is here too: its
	// backend reads the lock itself (§5.8 row 1's interim), so it is the
	// CONTROL — green before this change and after it.
	for _, target := range fixedLineageShipTargets {
		files, err := c.Generate(u, target, nil)
		if err != nil {
			t.Fatalf("%s: generate: %v", target, err)
		}
		olderSpelling := fixedLineageHashSpelling(target, older)
		var carries []string
		for name, data := range files {
			if strings.Contains(string(data), olderSpelling) {
				carries = append(carries, name)
			}
		}
		if len(carries) == 0 {
			t.Errorf("%s: the emitted source carries no entry for the OLDER layout 0x%016x — the driver handed the backend no lineage, so a build of this unit reads only its own records (§5.9 #1)", target, older)
		}
		found := false
		for _, data := range files {
			if strings.Contains(string(data), fixedLineageHashSpelling(target, current)) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: the emitted source carries no entry for the CURRENT layout 0x%016x", target, current)
		}
	}
}

// TestFixedLineageWithoutALockIsOneEntry is the other half of §5.9 #1: a unit
// that was never locked PROMISES NOTHING, so the driver hands nothing and every
// table carries the single entry it can always compute — its own. The shared
// lock read must not turn an unlocked unit into an error, and it must not move
// one byte of its output.
func TestFixedLineageWithoutALockIsOneEntry(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Lineage.schema"), []byte(fixedLineageShipSchema("    a int32\n    b int32\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, SchemaLockFileName)); !os.IsNotExist(err) {
		t.Fatal("the fixture has no lock")
	}
	c := New()
	u, err := c.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	own := fixedLineageShipHash(t, u)
	for _, target := range fixedLineageShipTargets {
		files, err := c.Generate(u, target, nil)
		if err != nil {
			t.Fatalf("%s: generate: %v", target, err)
		}
		found := false
		for _, data := range files {
			if strings.Contains(string(data), fixedLineageHashSpelling(target, own)) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: an unlocked unit still carries its own layout 0x%016x", target, own)
		}
	}
}
