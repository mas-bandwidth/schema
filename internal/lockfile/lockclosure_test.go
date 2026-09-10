// THE OWNER'S QUESTION, answered both ways (docs/SPEC-TABLES.md §2.10):
// "is there anything schema check cannot catch in a fixed table? can we fix
// that so it does?"
//
// Order and width were the whole of the old file, and they are not the whole
// of what a reader stands on. This file is the rest of it — the default, the
// range, the fixed-point scale, the `?`, which type a nested slot holds, which
// enum or flags type a slot holds, an array's element, the enum a keyed array
// is keyed by, a union arm's payload, and every enum, flags mask, union and
// nested record a fixed table reaches — and every case is measured three ways:
// the edit is REFUSED on the fixed table, the SAME edit on a plain
// variable-length `table` is not the lock's business, and `schema lock` will
// not write the break either.
package lockfile_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/lockfile"
)

// closureBase is the fixture, and it is built in TWO HALVES that mirror each
// other field for field.
//
// `Config` is FIXED, and everything it reaches — Buff, Debuff, Hull, Perks,
// Effect — is in the lock with it. `Bag` holds a `*Bag`, which makes it
// VARIABLE-LENGTH (§2.2), and everything only IT reaches — Ballast, Trim,
// Cargo, Rigging, Freight — is in no lock at all. So every edit below can be
// made twice, once on each half, and the difference in what the compiler says
// is the whole of what this file measures.
const closureBase = `package closuredemo

enum Hull
{
    Interceptor
    Gunship
    Freighter
}

enum Cargo
{
    Ore
    Ice
    Fuel
}

flags Perks { Shielded, Cloaked }

flags Rigging { Sail, Mast }

enum Rank
{
    Scout
    Wing
    Leader
}

enum Stow
{
    Dry
    Frozen
    Liquid
}

// Bay keys a slot array and Deck is what it is repointed at; Carton and Rack
// are their twins on Bag's half. No other row edits their variants, so no
// other row's edit moves the width of the slot array they size — and both
// halves' pairs sit in Manifest and Invoice, so a repointed key is the ONE
// thing the swap changes and not a type leaving the closure as well.
enum Bay
{
    Fore
    Mid
    Aft
}

enum Deck
{
    Upper
    Lower
    Hold
}

enum Carton
{
    Small
    Medium
    Large
}

enum Rack
{
    Low
    High
    Top
}

flags Honors { Medal, Star }

flags Tackle { Hook, Line }

type Manifest
{
    kind   Hull
    rank   Rank
    kit    Perks
    medal  Honors
    bay    Bay
    deck   Deck
}

type Invoice
{
    kind   Cargo
    stow   Stow
    kit    Rigging
    medal  Tackle
    carton Carton
    rack   Rack
}

type Buff
{
    multiplier float32 = 1.0
}

type Debuff
{
    amount int32 = 0 | min = 0, max = 100
}

type Ballast
{
    weight float32 = 1.0
}

type Trim
{
    amount int32 = 0 | min = 0, max = 100
}

union Effect
{
    buff   Buff
    debuff Debuff
}

union Stowage
{
    ballast Ballast
    trim    Trim
}

table Config
{
    tick_rate int32 = 60 | min = 1, max = 240
    scale     fixed(16, 16) | min = -8, max = 8
    reaction  float32 | min = 0, max = 1, resolution = 0.01
    hull      Hull = Gunship
    perks     Perks
    boost     Buff
    gunner    ?Buff
    effect    Effect
    lanes     [4]int32
    manifest  Manifest
    ranks     [4]Hull
    slots     [Bay]int32
}

table Bag
{
    rate    int32 = 60 | min = 1, max = 240
    pitch   fixed(16, 16) | min = -8, max = 8
    lag     float32 | min = 0, max = 1, resolution = 0.01
    cargo   Cargo = Ore
    rigging Rigging
    keel    Ballast
    spare   ?Ballast
    stowage Stowage
    lanes   [4]int32
    invoice Invoice
    holds   [4]Cargo
    bins    [Carton]int32
    tail    *Bag
}
`

// lockedFrom writes before, locks it, rewrites the schema to after, and
// returns the refusals the check produces over the committed lock.
func lockedFrom(t *testing.T, before, after string) (dir string, paths []string, errs []error) {
	t.Helper()
	dir, paths = fixture(t, before)
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatalf("locking the fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(after), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, paths, lockfile.Check(load(t, paths), paths)
}

// edit is one substitution into closureBase, and it must land.
func edit(t *testing.T, src, from, to string) string {
	t.Helper()
	out := strings.Replace(src, from, to, 1)
	if out == src {
		t.Fatalf("the fixture does not carry %q", from)
	}
	return out
}

// ---- what the lock holds now ----

// TestLockHoldsTheWholeClosure is the widened file's shape: the fixed table's
// entries carry every fact a reader of the record assumes, and every type the
// record is MADE of has a block of its own — while the variable-length table
// beside it and everything only it reaches are absent, as they were.
func TestLockHoldsTheWholeClosure(t *testing.T) {
	_, paths := fixture(t, closureBase)
	lk := lockfile.Render(load(t, paths))
	text := lk.Text()

	if lk.Version != 5 {
		t.Errorf("the widened rendering is version 5, got %d", lk.Version)
	}
	for _, want := range []string{
		"fixed table Config layout=0x",
		// the closure, each in its own block
		"type Buff layout=0x",
		"type Debuff layout=0x",
		"type Manifest layout=0x",
		"enum Hull values=0x",
		"enum Rank values=0x",
		"flags Perks values=0x",
		"flags Honors values=0x",
		"union Effect values=0x",
		// the value lists, in declared order — a union arm names its payload
		"    variant Interceptor\n    variant Gunship\n    variant Freighter\n",
		"    variant Shielded\n    variant Cloaked\n",
		"    arm buff payload=Buff@",
		"    arm debuff payload=Debuff@",
		// and the facts on the entries
		"field tick_rate id=0x", "default=60 min=1 max=240",
		"default=0 min=-8 max=8 frac=16",       // the fixed-point scale
		"default=0.0 min=0.0 max=1.0 res=0.01", // the compressed float
		"default=variant:Gunship held=Hull@",   // an enum slot names what it holds
		"default=mask:0 held=Perks@",           // a flags slot names what it holds
		"default=zero held=Buff@",              // kind 13 names what it holds
		"held=Buff@",                           // boost and gunner both
		" optional",                            // the `?` still sits on gunner
		"elem=4/4",                             // kind 14 names its element
		"held=Hull@",                           // hull, Manifest.kind, ranks
		"elem=7/1",                             // an array of enums
		"elem=4/4 key=Bay@",                    // a keyed array's element and its key
		"enum Bay values=0x",                   // a key is reached like anything else
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the lock must carry %q:\n%s", want, text)
		}
	}
	for _, absent := range []string{
		"Bag", "Ballast", "Trim", "Cargo", "Rigging", "Stowage",
		"Invoice", "Stow", "Tackle", "Carton", "Rack",
	} {
		if strings.Contains(text, absent) {
			t.Errorf("a VARIABLE-LENGTH table and what only it reaches are not in the lock, found %q:\n%s", absent, text)
		}
	}
}

// ---- every new refusal, three ways ----

// closureBreaks is the table of edits the widened lock now catches. Each row
// is ONE change spelled twice: once on the FIXED half, where it is refused,
// and once on the VARIABLE-LENGTH half, where it is nobody's business but the
// id-table wire's.
var closureBreaks = []struct {
	name             string
	fixedFrom, fixed string // the edit on Config's half
	varFrom, varTo   string // the same edit on Bag's half
	want             []string
}{
	{
		name:      "a moved default",
		fixedFrom: "    tick_rate int32 = 60 |", fixed: "    tick_rate int32 = 90 |",
		varFrom: "    rate    int32 = 60 |", varTo: "    rate    int32 = 90 |",
		want: []string{"fixed table Config", "entry 1, field tick_rate",
			"defaults to 60 in the lock and 90 in the declaration",
			"an older writer's missing field is filled from this default",
			"deprecate this field and append a new one"},
	},
	{
		name:      "an implicit default given a value",
		fixedFrom: "    reaction  float32 |", fixed: "    reaction  float32 = 0.5 |",
		varFrom: "    lag     float32 |", varTo: "    lag     float32 = 0.5 |",
		want: []string{"fixed table Config", "entry 3, field reaction",
			"defaults to 0.0 in the lock and 0.5 in the declaration"},
	},
	{
		name:      "a widened integer range",
		fixedFrom: "    tick_rate int32 = 60 | min = 1, max = 240", fixed: "    tick_rate int32 = 60 | min = 1, max = 480",
		varFrom: "    rate    int32 = 60 | min = 1, max = 240", varTo: "    rate    int32 = 60 | min = 1, max = 480",
		want: []string{"fixed table Config", "entry 1, field tick_rate",
			"is [1, 240] in the lock and [1, 480] in the declaration",
			"keeps its range", "deprecate this field and append a new one"},
	},
	{
		name:      "a moved resolution",
		fixedFrom: "    reaction  float32 | min = 0, max = 1, resolution = 0.01", fixed: "    reaction  float32 | min = 0, max = 1, resolution = 0.001",
		varFrom: "    lag     float32 | min = 0, max = 1, resolution = 0.01", varTo: "    lag     float32 | min = 0, max = 1, resolution = 0.001",
		want: []string{"fixed table Config", "entry 3, field reaction",
			"resolution 0.01 in the lock", "resolution 0.001 in the declaration",
			"keeps its range"},
	},
	{
		name:      "a moved fixed-point scale at the same width",
		fixedFrom: "    scale     fixed(16, 16) |", fixed: "    scale     fixed(24, 8) |",
		varFrom: "    pitch   fixed(16, 16) |", varTo: "    pitch   fixed(24, 8) |",
		want: []string{"fixed table Config", "entry 2, field scale",
			"16 fractional bits in the lock", "8 fractional bits in the declaration",
			"keeps its range"},
	},
	{
		name:      "an optional turned on",
		fixedFrom: "    boost     Buff\n", fixed: "    boost     ?Buff\n",
		varFrom: "    keel    Ballast\n", varTo: "    keel    ?Ballast\n",
		want: []string{"fixed table Config", "entry 6, field boost",
			"is plain in the lock and optional in the declaration",
			"presence bool beside its value INSIDE the record",
			"deprecate this field and append a new one"},
	},
	{
		name:      "an optional turned off",
		fixedFrom: "    gunner    ?Buff\n", fixed: "    gunner    Buff\n",
		varFrom: "    spare   ?Ballast\n", varTo: "    spare   Ballast\n",
		want: []string{"fixed table Config", "entry 7, field gunner",
			"is optional in the lock and plain in the declaration"},
	},
	{
		name:      "a change inside a nested record",
		fixedFrom: "type Buff\n{\n    multiplier float32 = 1.0\n}", fixed: "type Buff\n{\n    multiplier int32 = 1\n}",
		varFrom: "type Ballast\n{\n    weight float32 = 1.0\n}", varTo: "type Ballast\n{\n    weight int32 = 1\n}",
		want: []string{"type Buff", "entry 1, field multiplier",
			"in the lock and kind", "keeps its type",
			"deprecate this field and append a new one"},
	},
	{
		name:      "a reordered enum",
		fixedFrom: "enum Hull\n{\n    Interceptor\n    Gunship\n", fixed: "enum Hull\n{\n    Gunship\n    Interceptor\n",
		varFrom: "enum Cargo\n{\n    Ore\n    Ice\n", varTo: "enum Cargo\n{\n    Ice\n    Ore\n",
		want: []string{"enum Hull", "variant 1, Interceptor", "is Gunship in the declaration",
			"APPEND-ONLY", "a new variant goes at the END", "never inserted, moved or renamed",
			"restore it, and add the new one at the END"},
	},
	{
		name:      "a removed enum variant",
		fixedFrom: "    Gunship\n    Freighter\n", fixed: "    Gunship\n",
		varFrom: "    Ice\n    Fuel\n", varTo: "    Ice\n",
		want: []string{"enum Hull", "variant 3, Freighter",
			"is in the lock and gone from the declaration",
			"keeps its place forever", "restore it, and add the new one at the END"},
	},
	{
		name:      "a renamed enum variant",
		fixedFrom: "    Freighter\n", fixed: "    Hauler\n",
		varFrom: "    Fuel\n", varTo: "    Coal\n",
		want: []string{"enum Hull", "variant 3, Freighter", "is Hauler in the declaration",
			"never inserted, moved or renamed"},
	},
	{
		name:      "a reordered flags mask",
		fixedFrom: "flags Perks { Shielded, Cloaked }", fixed: "flags Perks { Cloaked, Shielded }",
		varFrom: "flags Rigging { Sail, Mast }", varTo: "flags Rigging { Mast, Sail }",
		want: []string{"flags Perks", "variant 1, Shielded", "is Cloaked in the declaration",
			"a fixed record stores the PLACE"},
	},
	{
		name:      "a reordered union",
		fixedFrom: "union Effect\n{\n    buff   Buff\n    debuff Debuff\n}", fixed: "union Effect\n{\n    debuff Debuff\n    buff   Buff\n}",
		varFrom: "union Stowage\n{\n    ballast Ballast\n    trim    Trim\n}", varTo: "union Stowage\n{\n    trim    Trim\n    ballast Ballast\n}",
		want: []string{"union Effect", "arm 1, buff", "is debuff in the declaration",
			"a new arm goes at the END"},
	},
	{
		name:      "a nested slot holding a different type",
		fixedFrom: "    boost     Buff\n", fixed: "    boost     Debuff\n",
		varFrom: "    keel    Ballast\n", varTo: "    keel    Trim\n",
		want: []string{"fixed table Config", "entry 6, field boost",
			"held Buff in the lock and Debuff in the declaration",
			"keeps the type it holds",
			"deprecate this field and append a new one"},
	},
	{
		name:      "an optional nested slot holding a different type",
		fixedFrom: "    gunner    ?Buff\n", fixed: "    gunner    ?Debuff\n",
		varFrom: "    spare   ?Ballast\n", varTo: "    spare   ?Trim\n",
		want: []string{"fixed table Config", "entry 7, field gunner",
			"held Buff in the lock and Debuff in the declaration",
			"keeps the type it holds",
			"deprecate this field and append a new one"},
	},
	{
		name:      "a union arm's payload type swapped, names kept",
		fixedFrom: "union Effect\n{\n    buff   Buff\n    debuff Debuff\n}", fixed: "union Effect\n{\n    buff   Debuff\n    debuff Buff\n}",
		varFrom: "union Stowage\n{\n    ballast Ballast\n    trim    Trim\n}", varTo: "union Stowage\n{\n    ballast Trim\n    trim    Ballast\n}",
		want: []string{"union Effect", "arm 1, buff",
			"held Buff in the lock and Debuff in the declaration",
			"keeps the type it holds",
			"deprecate this field and append a new one"},
	},
	{
		name:      "an array's element type",
		fixedFrom: "    lanes     [4]int32\n", fixed: "    lanes     [4]float32\n",
		varFrom: "    lanes   [4]int32\n", varTo: "    lanes   [4]float32\n",
		want: []string{"fixed table Config", "entry 9, field lanes",
			"holds int32 elements in the lock and float32 elements in the declaration",
			"keeps its element type",
			"deprecate this field and append a new one"},
	},
	{
		name:      "an enum slot holding a different type",
		fixedFrom: "    hull      Hull = Gunship\n", fixed: "    hull      Rank = Scout\n",
		varFrom: "    cargo   Cargo = Ore\n", varTo: "    cargo   Stow = Dry\n",
		want: []string{"fixed table Config", "entry 4, field hull",
			"held Hull in the lock and Rank in the declaration",
			"keeps the type it holds",
			"deprecate this field and append a new one"},
	},
	{
		name:      "a flags slot holding a different type",
		fixedFrom: "    perks     Perks\n", fixed: "    perks     Honors\n",
		varFrom: "    rigging Rigging\n", varTo: "    rigging Tackle\n",
		want: []string{"fixed table Config", "entry 5, field perks",
			"held Perks in the lock and Honors in the declaration",
			"keeps the type it holds",
			"deprecate this field and append a new one"},
	},
	{
		name:      "an array of enums holding a different type",
		fixedFrom: "    ranks     [4]Hull\n", fixed: "    ranks     [4]Rank\n",
		varFrom: "    holds   [4]Cargo\n", varTo: "    holds   [4]Stow\n",
		want: []string{"fixed table Config", "entry 11, field ranks",
			"held Hull in the lock and Rank in the declaration",
			"keeps the type it holds",
			"deprecate this field and append a new one"},
	},
	// kind 16 is an array too, and its key is a fact of its own: Hull and
	// Rank have the same number of variants, so the slot is the same width
	// and every other line of the entry is unmoved.
	{
		name:      "an enum-keyed array keyed by a different enum",
		fixedFrom: "    slots     [Bay]int32\n", fixed: "    slots     [Deck]int32\n",
		varFrom: "    bins    [Carton]int32\n", varTo: "    bins    [Rack]int32\n",
		want: []string{"fixed table Config", "entry 12, field slots",
			"is keyed by Bay in the lock and Deck in the declaration",
			"keeps the enum it is keyed by",
			"deprecate this field and append a new one"},
	},
	{
		name:      "an enum-keyed array's element respelled at the same width",
		fixedFrom: "    slots     [Bay]int32\n", fixed: "    slots     [Bay]float32\n",
		varFrom: "    bins    [Carton]int32\n", varTo: "    bins    [Carton]float32\n",
		want: []string{"fixed table Config", "entry 12, field slots",
			"holds int32 elements in the lock and float32 elements in the declaration",
			"keeps its element type",
			"deprecate this field and append a new one"},
	},
}

// TestClosureBreakRefused is the first of the three ways: the edit lands on the
// FIXED half and the compile refuses, naming the declaration, the line, the
// reason and the remedy.
func TestClosureBreakRefused(t *testing.T) {
	for _, tc := range closureBreaks {
		t.Run(tc.name, func(t *testing.T) {
			after := edit(t, closureBase, tc.fixedFrom, tc.fixed)
			_, _, errs := lockedFrom(t, closureBase, after)
			refuses(t, errs, tc.want...)
		})
	}
}

// TestClosureBreakOnAVariableTableIsNotTheLocksBusiness is the second: the
// SAME edit on the VARIABLE-LENGTH half passes in silence and moves no line of
// the file. A `table` with a pointer in it rides the id-table wire, where a
// moved default, a widened range and a reordered enum are what the wire is FOR
// (§4) — the lock has no opinion, and this is the proof it has none.
func TestClosureBreakOnAVariableTableIsNotTheLocksBusiness(t *testing.T) {
	for _, tc := range closureBreaks {
		t.Run(tc.name, func(t *testing.T) {
			after := edit(t, closureBase, tc.varFrom, tc.varTo)
			dir, paths, errs := lockedFrom(t, closureBase, after)
			if len(errs) != 0 {
				t.Fatalf("a variable-length table's evolution is the wire's business, not the lock's: %v", errs)
			}
			path := filepath.Join(dir, lockfile.FileName)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, rewrote, err := lockfile.Update(load(t, paths), paths); err != nil || rewrote {
				t.Errorf("the file does not move: rewrote=%v err=%v", rewrote, err)
			}
			if got, _ := os.ReadFile(path); string(got) != string(before) {
				t.Errorf("the file moved:\n--- was ---\n%s\n--- now ---\n%s", before, got)
			}
		})
	}
}

// TestClosureBreakRefusedByLockToo is the third: `schema lock` is not the way
// to make one of these refusals go away. It runs the check's own comparison
// first, refuses with the same sentence, and leaves the file exactly as it sat.
func TestClosureBreakRefusedByLockToo(t *testing.T) {
	for _, tc := range closureBreaks {
		t.Run(tc.name, func(t *testing.T) {
			dir, paths := fixture(t, closureBase)
			if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, lockfile.FileName)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			after := edit(t, closureBase, tc.fixedFrom, tc.fixed)
			if err := os.WriteFile(filepath.Join(dir, "Lock.schema"), []byte(after), 0o644); err != nil {
				t.Fatal(err)
			}
			_, rewrote, err := lockfile.Update(load(t, paths), paths)
			if err == nil || rewrote {
				t.Fatalf("`schema lock` appends, and this is not an append: rewrote=%v err=%v", rewrote, err)
			}
			if !strings.Contains(err.Error(), tc.want[len(tc.want)-1]) || !strings.Contains(err.Error(), "not an append") {
				t.Errorf("the refusal says which break and why: %v", err)
			}
			if got, _ := os.ReadFile(path); string(got) != string(before) {
				t.Errorf("a refused lock writes nothing:\n--- was ---\n%s\n--- now ---\n%s", before, got)
			}
		})
	}
}

// ---- the appends the rule allows, recorded ----

// closureAppends are the changes the APPEND-ONLY rule permits over the widened
// file: a value at the END of an enum, of a flags mask, of a union, and a field
// at the bottom of a nested record. Each is a CHANGE, so the compile before the
// lock is written is refused as stale — and `schema lock` then writes exactly
// it, with every line before it untouched.
var closureAppends = []struct {
	name     string
	from, to string
	stale    []string
	recorded string
}{
	{
		name: "a variant at the end of an enum",
		from: "    Freighter\n}", to: "    Freighter\n    Tanker\n}",
		stale:    []string{"enum Hull", "variant 4, Tanker", "in the declaration and not in the lock", "write it with `schema lock`"},
		recorded: "    variant Tanker\n",
	},
	{
		name: "a variant at the end of a flags mask",
		from: "flags Perks { Shielded, Cloaked }", to: "flags Perks { Shielded, Cloaked, Turbo }",
		stale:    []string{"flags Perks", "variant 3, Turbo", "in the declaration and not in the lock"},
		recorded: "    variant Turbo\n",
	},
	{
		name: "an arm at the end of a union",
		from: "    debuff Debuff\n}", to: "    debuff Debuff\n    stun   Buff\n}",
		stale:    []string{"union Effect", "arm 3, stun", "in the declaration and not in the lock"},
		recorded: "    arm stun payload=Buff@",
	},
}

// TestClosureAppendRefusedUntilLocked is the STRICT reading over the widened
// file: an append to anything the lock holds is allowed by the rule and still
// refused until the record of it is in the tree, and `schema lock` writes it
// with every line before it left where it was.
func TestClosureAppendRefusedUntilLocked(t *testing.T) {
	for _, tc := range closureAppends {
		t.Run(tc.name, func(t *testing.T) {
			after := edit(t, closureBase, tc.from, tc.to)
			dir, paths, errs := lockedFrom(t, closureBase, after)
			refuses(t, errs, tc.stale...)

			path := filepath.Join(dir, lockfile.FileName)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, rewrote, err := lockfile.Update(load(t, paths), paths); err != nil || !rewrote {
				t.Fatalf("`schema lock` writes an append: rewrote=%v err=%v", rewrote, err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(got), tc.recorded) {
				t.Errorf("the append is recorded (%q):\n%s", tc.recorded, got)
			}
			// and with it written, the same compile passes and the file is
			// left alone the next time
			if errs := lockfile.Check(load(t, paths), paths); len(errs) != 0 {
				t.Errorf("the written lock checks clean: %v", errs)
			}
			if _, rewrote, err := lockfile.Update(load(t, paths), paths); err != nil || rewrote {
				t.Errorf("a current lock is left alone: rewrote=%v err=%v", rewrote, err)
			}
			if len(before) >= len(got) {
				t.Errorf("an append grows the file: %d then %d bytes", len(before), len(got))
			}
		})
	}
}

// ---- the closure narrowing ----

// TestTypeLeavingTheClosureRefused is the block-level twin of a withdrawn
// table: a `type`, an enum, a flags mask or a union the lock holds and the
// declaration no longer reaches. A fixed record is MADE of those, so one
// leaving means a field changed what it holds — and the refusal says so and
// names the remedy, rather than letting the block quietly vanish from the file.
func TestTypeLeavingTheClosureRefused(t *testing.T) {
	_, paths := fixture(t, closureBase)
	locked := lockfile.Render(load(t, paths))

	for _, tc := range []struct {
		name string
		drop string
		want []string
	}{
		{"a nested record", "Buff", []string{"type Buff is in the lock", "no longer nest it by value",
			"a nested record's fields ARE the holder's bytes", "deprecate that field and append a new one"}},
		{"an enum", "Hull", []string{"enum Hull is in the lock", "no longer reach it",
			"every type a fixed record is made of", "deprecate that field and append a new one"}},
		{"a flags mask", "Perks", []string{"flags Perks is in the lock", "no longer reach it"}},
		{"a union", "Effect", []string{"union Effect is in the lock", "no longer reach it"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			live := lockfile.Render(load(t, paths))
			live.Tables = dropTable(live.Tables, tc.drop)
			live.Values = dropList(live.Values, tc.drop)
			refuses(t, lockfile.Diff(locked, live, lockfile.Current), tc.want...)
		})
	}
}

func dropTable(in []lockfile.Table, name string) []lockfile.Table {
	var out []lockfile.Table
	for _, t := range in {
		if t.Name != name {
			out = append(out, t)
		}
	}
	return out
}

func dropList(in []lockfile.ValueList, name string) []lockfile.ValueList {
	var out []lockfile.ValueList
	for _, v := range in {
		if v.Name != name {
			out = append(out, v)
		}
	}
	return out
}

// ---- the file caught lying, on its new half ----

// TestHandEditedValueListRefused is the hand-edit check over a value list: the
// values and the hash beside them are one statement made twice, exactly as a
// record's entries and its layout hash are.
func TestHandEditedValueListRefused(t *testing.T) {
	dir, paths := fixture(t, closureBase)
	if _, _, err := lockfile.Update(load(t, paths), paths); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, lockfile.FileName)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lie := strings.Replace(string(data), "    variant Interceptor\n    variant Gunship\n", "    variant Gunship\n    variant Interceptor\n", 1)
	if lie == string(data) {
		t.Fatal("the fixture's Hull block is not what this test edits")
	}
	if err := os.WriteFile(path, []byte(lie), 0o644); err != nil {
		t.Fatal(err)
	}
	refuses(t, lockfile.Check(load(t, paths), paths),
		"enum Hull", "variants that hash to", "a hand-edit does not hold", "schema lock")
}

// TestValueListRoundTrips is the widened format read back as what it wrote:
// the value lists, the nested records and every new fact on an entry line
// survive a parse, or the file the compiler writes is not the file it reads.
func TestValueListRoundTrips(t *testing.T) {
	_, paths := fixture(t, closureBase)
	want := lockfile.Render(load(t, paths))
	got, err := lockfile.Parse("schema.lock", []byte(want.Text()))
	if err != nil {
		t.Fatal(err)
	}
	if got.Text() != want.Text() {
		t.Errorf("round trip:\n--- wrote ---\n%s\n--- read back ---\n%s", want.Text(), got.Text())
	}
	if len(got.Values) != len(want.Values) || len(got.Values) == 0 {
		t.Fatalf("the value lists survive: %d then %d", len(want.Values), len(got.Values))
	}
	if hull := got.List("Hull"); hull == nil || strings.Join(hull.Values, ",") != "Interceptor,Gunship,Freighter" {
		t.Errorf("Hull reads back in declared order: %+v", hull)
	}
}

// TestNestedRecordAppendSlidesTheHolder is the difference between a fixed
// table's own bottom and a nested record's, and it is not a nicety: a field
// appended to a `type` a fixed table holds BY VALUE grows the holder's slot,
// which slides every field after it. So the change the rule allows at the
// bottom of a table is a BREAK one level in, and the compiler says both halves
// — the holder's width first, because that is what a reader stands on, and the
// nested record's own new entry beside it.
func TestNestedRecordAppendSlidesTheHolder(t *testing.T) {
	after := edit(t, closureBase,
		"    multiplier float32 = 1.0\n}", "    multiplier float32 = 1.0\n    stacks     uint8\n}")
	dir, paths, errs := lockedFrom(t, closureBase, after)
	if len(errs) != 2 {
		t.Fatalf("want the holder's width and the nested record's entry, got %d: %v", len(errs), errs)
	}
	first := errs[0].Error()
	for _, w := range []string{"fixed table Config", "entry 6, field boost",
		"4 bytes wide in the lock and 8 in the declaration", "walked by offset"} {
		if !strings.Contains(first, w) {
			t.Errorf("the holder's refusal must name %q:\n%s", w, first)
		}
	}
	if second := errs[1].Error(); !strings.Contains(second, "type Buff") || !strings.Contains(second, "field stacks") {
		t.Errorf("the nested record's own finding names it:\n%s", second)
	}
	// and `schema lock` will not write it away either
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err == nil || rewrote {
		t.Fatalf("`schema lock` appends, and this is not an append: rewrote=%v err=%v", rewrote, err)
	}
	if _, err := os.Stat(filepath.Join(dir, lockfile.FileName)); err != nil {
		t.Fatal(err)
	}
}

// TestALockFromAnOlderRenderingSaysWhatToDo is the one parse refusal that is
// not a hand-edit, and the version bump that widened this file made it live: a
// lock written under an older rendering holds nothing a person can carry
// forward, so both verbs name the remedy that works rather than sending the
// user around a loop.
func TestALockFromAnOlderRenderingSaysWhatToDo(t *testing.T) {
	dir, paths := fixture(t, closureBase)
	path := filepath.Join(dir, lockfile.FileName)
	if err := os.WriteFile(path, []byte("schema-lock 1\npackage closuredemo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refuses(t, lockfile.Check(load(t, paths), paths),
		"rendering version 1 and this compiler writes version 5",
		"delete it and write it again with `schema lock`")
	_, rewrote, err := lockfile.Update(load(t, paths), paths)
	if err == nil || rewrote {
		t.Fatalf("`schema lock` does not write over a lock it cannot read: rewrote=%v err=%v", rewrote, err)
	}
	if !strings.Contains(err.Error(), "delete it and write it again with `schema lock`") {
		t.Errorf("the one writer names the same remedy: %v", err)
	}
	// and once it IS gone, the command locks the unit
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err != nil || !rewrote {
		t.Fatalf("the remedy works: rewrote=%v err=%v", rewrote, err)
	}
	if errs := lockfile.Check(load(t, paths), paths); len(errs) != 0 {
		t.Errorf("the written lock checks clean: %v", errs)
	}
}

// TestALockFromTheRenderingJustBeforeThisOneSaysTheSameThing is the case a
// person actually meets: not a two-line stub, but a WHOLE well-formed lock
// this compiler wrote one version ago. The version sits on the first line and
// is read before any of the body, so a file that parses perfectly still gets
// the one remedy that works — and `schema lock`, the command that remedy names,
// must not send the user around a loop by refusing to write it either.
func TestALockFromTheRenderingJustBeforeThisOneSaysTheSameThing(t *testing.T) {
	dir, paths := fixture(t, closureBase)
	path := filepath.Join(dir, lockfile.FileName)
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err != nil || !rewrote {
		t.Fatalf("lock the unit first: rewrote=%v err=%v", rewrote, err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	head := fmt.Sprintf("schema-lock %d\n", lockfile.Version)
	if !bytes.HasPrefix(current, []byte(head)) {
		t.Fatalf("the lock opens with %q:\n%s", head, current)
	}
	// the same file, one rendering back: every other line still parses
	previous := append([]byte(fmt.Sprintf("schema-lock %d\n", lockfile.Version-1)), current[len(head):]...)
	if err := os.WriteFile(path, previous, 0o644); err != nil {
		t.Fatal(err)
	}
	refuses(t, lockfile.Check(load(t, paths), paths),
		fmt.Sprintf("rendering version %d and this compiler writes version %d", lockfile.Version-1, lockfile.Version),
		"holds nothing a hand can carry forward",
		"delete it and write it again with `schema lock`")
	_, rewrote, err := lockfile.Update(load(t, paths), paths)
	if err == nil || rewrote {
		t.Fatalf("`schema lock` does not write over a lock it cannot read: rewrote=%v err=%v", rewrote, err)
	}
	if !strings.Contains(err.Error(), "delete it and write it again with `schema lock`") {
		t.Errorf("the one writer names the same remedy: %v", err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, previous) {
		t.Errorf("a refused lock writes nothing:\n--- was ---\n%s\n--- now ---\n%s", previous, got)
	}
	// the remedy works, and it writes THIS rendering back
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, rewrote, err := lockfile.Update(load(t, paths), paths); err != nil || !rewrote {
		t.Fatalf("the remedy works: rewrote=%v err=%v", rewrote, err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, current) {
		t.Errorf("the rewritten lock is this rendering, byte for byte:\n--- want ---\n%s\n--- got ---\n%s", current, got)
	}
	if errs := lockfile.Check(load(t, paths), paths); len(errs) != 0 {
		t.Errorf("the written lock checks clean: %v", errs)
	}
}
