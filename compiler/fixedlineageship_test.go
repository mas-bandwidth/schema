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
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// fixedLineageShipTargets are the targets whose table backend takes the
// lineage as data today: `c`, `go`, `java`, `js` and `rust` through
// `GenerateLineage`, and `cpp` through the lock the driver now opens for it.
// Every other built-in target's table backend has no second entry point yet
// (cs, dart, elixir); their `GenerateLineage` lives on the open port branches,
// and the day one lands its target joins this list and nothing else changes.
var fixedLineageShipTargets = []string{"c", "cpp", "go", "java", "js", "rust"}

// fixedLineageHashSpelling is ONE layout hash as the target's emitted source
// writes it. It is a per-target needle and not a per-target assertion: the
// statement being made is the same for every leg — the eight bytes of the
// older layout are in the module, as static data — and only the spelling of a
// 64-bit constant differs by language.
//
// The JavaScript leg is why this function exists. A JS number holds no 64-bit
// integer, so its known-layout entry carries the hash as the LOW and HIGH u32
// halves it compares against the file's two 32-bit reads
// (`new TableFixedKnownLayout(0x<lo>, 0x<hi>, …)`), and its layout bytes ride
// as base64 — so the hash pair is the ONLY part of an older entry a grep of the
// module can read, and it is exactly the part that matters, because the hash is
// the only fact a file is matched on (§5.2).
func fixedLineageHashSpelling(target string, hash uint64) string {
	if target == "js" {
		return fmt.Sprintf("0x%08x, 0x%08x", uint32(hash), uint32(hash>>32))
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
// fixedLineageShipLockedUnit is THE OPERATOR'S THREE COMMANDS as a fixture —
// `schema lock`, a widening, `schema lock` again — so the committed lock holds a
// lineage of TWO: the older layout first, the declaration's own last. It returns
// the driver, the unit loaded from the locked tree, the two wire hashes and the
// LOCK'S OWN ENTRIES, and it checks the premise every assertion rests on before
// handing them back.
func fixedLineageShipLockedUnit(t *testing.T) (*Compiler, *ir.Unit, uint64, uint64, []lockfile.LineageEntry) {
	t.Helper()
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
	return c, u, older, current, lin
}

// TestFixedLineageIsShippedByEveryTargetThatTakesIt is step 1 of #921: a unit
// with a LOCKED LINEAGE OF TWO, generated through the public driver, and every
// target whose table backend exports `GenerateLineage` must carry BOTH hashes
// as static data. A target still on the plain `Generate` carries one, and is
// named here rather than asserted about, because its backend has no second
// entry point yet.
func TestFixedLineageIsShippedByEveryTargetThatTakesIt(t *testing.T) {
	c, u, older, current, _ := fixedLineageShipLockedUnit(t)

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

// fixedLineageShipKnownRecord is, PER LEG, the known-layout line the leg writes
// for ONE lineage entry, with the record size as its one capture. Each leg
// spells the static data in its own language and nothing shared can be grepped
// for, so the shapes are written out here — the `cpp` one is the REFERENCE's,
// against which the others are read.
var fixedLineageShipKnownRecord = map[string]func(hash uint64) *regexp.Regexp{
	// internal/codegen/gotable/fixedform.go: Hash, an optional RETIRED note,
	// then Record.
	"go": func(h uint64) *regexp.Regexp {
		return regexp.MustCompile(fmt.Sprintf(`Hash:\s+0x%016x,\n(?:\s*//[^\n]*\n)?\s*Record:\s*(\d+),`, h))
	},
	// internal/codegen/ctable/fixedform.go: one brace-initialized row, the
	// layout's length taken by sizeof, the record size last.
	"c": func(h uint64) *regexp.Regexp {
		return regexp.MustCompile(fmt.Sprintf(`\{ 0x%016xull, \w+, \(int64_t\) sizeof\( \w+ \), (\d+) \}`, h))
	},
	// internal/codegen/cpptable/fixedform.go: hash, layout, layout_bytes,
	// record_bytes — §5.9 #19's four members in order.
	"cpp": func(h uint64) *regexp.Regexp {
		return regexp.MustCompile(fmt.Sprintf(`\{ 0x%016xull, \w+, \d+, (\d+) \}`, h))
	},
	// internal/codegen/javatable/fixedform.go: one constructor call per entry,
	// the layout's length taken from the byte array, the record size last.
	"java": func(h uint64) *regexp.Regexp {
		return regexp.MustCompile(fmt.Sprintf(`new TableFixed\.KnownLayout\(0x%016xL, \w+, \w+\.length, (\d+)\)`, h))
	},
	// internal/codegen/rusttable/fixedform.go: a struct literal, one field per
	// line, record after layout.
	"rust": func(h uint64) *regexp.Regexp {
		return regexp.MustCompile(fmt.Sprintf(`hash: 0x%016x,\n\s*layout: [^\n]*\n\s*record: (\d+),`, h))
	},
	// internal/codegen/jstable/fixedmodule.go: one constructor call per entry,
	// the hash as the LOW and HIGH u32 lanes this language compares against the
	// file's two 32-bit reads, then the layout — the current one by name, an
	// OLDER one as base64 through `TableFixedDecodeLayout`, which puts a newline
	// inside the call — then the layout's byte length and the record size last.
	"js": func(h uint64) *regexp.Regexp {
		return regexp.MustCompile(fmt.Sprintf(`new TableFixedKnownLayout\(0x%08x, 0x%08x, (?:\w+FixedLayout|TableFixedDecodeLayout\(\s*"[^"]*"\)), \d+, (\d+)\)`, uint32(h), uint32(h>>32)))
	},
}

// TestFixedLineageRecordSizeIsTheWholeRecord is what the Java wiring found
// (#920): §5.2's `record_bytes` is THE WHOLE RECORD — `8 + C(root)`, the
// record's own eight hash bytes and then the body — and the LOCK records the
// BODY (internal/lockfile/lineage.go, `LineageEntry.Record`,
// `ir.TableFixedTypeBytes`). Every leg's own entry for its own layout is
// `8 + body` already, so a driver that passed the lock's number through
// unchanged shipped a LOCKED unit's older entries EIGHT BYTES SHORT — and step
// 9 divides the tail behind the layout by that number, so every record of a
// file written by the older peer is misread or the read is refused as ragged.
//
// It is invisible in every harness fixture because none of them carries a lock,
// and invisible for the CURRENT layout because the leg's own entry wins for it.
// So the assertion is on the OLDER entry of a REAL LOCK, read back out of the
// emitted source by the shape each leg writes it in.
func TestFixedLineageRecordSizeIsTheWholeRecord(t *testing.T) {
	c, u, older, _, lin := fixedLineageShipLockedUnit(t)
	body := lin[0].Record
	if body <= 0 {
		t.Fatalf("the lock records the older layout's body size: %d", body)
	}
	want := 8 + body
	for _, target := range fixedLineageShipTargets {
		files, err := c.Generate(u, target, nil)
		if err != nil {
			t.Fatalf("%s: generate: %v", target, err)
		}
		shape, ok := fixedLineageShipKnownRecord[target]
		if !ok {
			t.Fatalf("%s: every target that takes the lineage has a known-layout shape here", target)
		}
		re := shape(older)
		matches := 0
		for name, data := range files {
			for _, m := range re.FindAllStringSubmatch(string(data), -1) {
				matches++
				got, err := strconv.ParseInt(m[1], 10, 64)
				if err != nil {
					t.Fatalf("%s: %s: the record size is a number: %q", target, name, m[1])
				}
				if got != want {
					t.Errorf("%s: %s: the OLDER layout 0x%016x is emitted with record_bytes %d, and §5.2's record_bytes is THE WHOLE RECORD — 8 + the lock's body size %d = %d. The lock records the BODY and the driver must add the eight hash bytes once, for every leg (docs/FIXED-FORM-ALGORITHM.md §5.2, compiler/lineage.go)", target, name, older, got, body, want)
				}
			}
		}
		if matches == 0 {
			t.Errorf("%s: no known-layout line for the OLDER layout 0x%016x was found in the emitted source — the lineage did not ship, or this leg's line has changed shape and the grep in fixedLineageShipKnownRecord has to change with it", target, older)
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
